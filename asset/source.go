package asset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Source is one addressable picture: what is known about it without opening
// it, and a way to open it.
//
// It is the interface of the project plan, section 9, unchanged:
//
//	type Source interface {
//	    Metadata() Metadata
//	    Open(context.Context) (io.ReadCloser, error)
//	}
//
// [Source.Metadata] must not block and must not perform input or output.
// Unknown fields stay empty; see [Metadata]. Known dimensions describe the
// *oriented* presentation, so a picture stored sideways with an EXIF
// orientation of 6 reports the dimensions a viewer sees, not the ones the
// codec reads. See [Orientation].
//
// [Source.Open] returns a fresh reader every time. Gift closes it. The reader
// need not be seekable and need not support concurrent use; the pipeline opens
// a source at most once at a time.
//
// # Extensions
//
// A plain Source is enough to display a picture but not enough to cache one:
// without a revision there is nothing to key a cache entry by, and the project
// plan is explicit that a URL without a validator is not an eternal revision.
// Sources may therefore additionally implement [Prober] or [Fetcher]. See
// those types for what each one buys.
type Source interface {
	// Metadata returns what is known without I/O. It must not block.
	Metadata() Metadata

	// Open returns a fresh reader over the encoded bytes. The caller closes
	// it.
	Open(ctx context.Context) (io.ReadCloser, error)
}

// ProbeResult is what a [Prober] learned about a source without transferring
// its bytes.
type ProbeResult struct {
	// Revision is the content version; see [Metadata.Revision]. An empty
	// revision means the probe could not establish one, which disables disk
	// caching for this source.
	Revision string
	// Size is the encoded size in bytes, or zero when it is unknown. The
	// pipeline reserves its input budget from this number, so a source that
	// knows it should say so: an unknown size costs a reservation of the
	// whole configured input limit.
	Size int64
	// MIMEType is the media type, or empty when unknown.
	MIMEType string
	// Fresh is how long the revision may be trusted without probing again.
	// Zero means "this request only", which is the right answer for a local
	// file, where probing is a stat.
	Fresh time.Duration
}

// Prober is the optional extension for a source whose revision is cheap to
// establish: a local file, where a probe is a stat.
//
// The pipeline probes first, keys the cache with the result and never
// transfers bytes for a picture whose thumbnail it already has. This is the
// "Metadaten-Probing und Validierung der Quelle" stage of the project plan,
// section 9.
type Prober interface {
	Probe(ctx context.Context) (ProbeResult, error)
}

// FetchResult is the answer to a conditional fetch.
type FetchResult struct {
	// NotModified reports that the revision passed in is still current. Body
	// is then nil and nothing was transferred.
	NotModified bool
	// Revision, Size, MIMEType and Fresh have the meaning they have in
	// [ProbeResult].
	Revision string
	Size     int64
	MIMEType string
	Fresh    time.Duration
	// Body is the encoded picture. The caller closes it. It is nil when
	// NotModified is set.
	Body io.ReadCloser
}

// Fetcher is the optional extension for a source whose revision only becomes
// known by asking for the bytes, and which can be asked conditionally: HTTP.
//
// A separate round trip purely to validate would double the request count of a
// cold gallery, so the pipeline does not probe these sources. It remembers the
// last revision it saw in a small persistent hint next to the thumbnail cache,
// serves the cached thumbnail directly while that hint is fresh, and issues one
// conditional fetch when it is not. A NotModified answer refreshes the hint and
// costs no pixels. That is the "definierte Freshness-Policy" of the project
// plan, section 9.
type Fetcher interface {
	// Fetch transfers the picture. ifRevision, when not empty, is the
	// revision the caller already has; the implementation should ask
	// conditionally and may answer NotModified.
	Fetch(ctx context.Context, ifRevision string) (FetchResult, error)
}

// Namespacer is the optional extension for a source that must not share cache
// entries with other sources of the same URL.
//
// Two users of the same authenticated endpoint see different pictures behind
// the same address. Their cache entries must not collide, and the project plan,
// section 9, requires a separate namespace for exactly that. The returned
// string is a *credential fingerprint chosen by the source*; the pipeline
// hashes it into the cache key and never writes it anywhere. See
// [HTTPSource.WithCredential].
type Namespacer interface {
	CacheNamespace() string
}

// --- local files -------------------------------------------------------------

// FileSource is a picture in the local file system.
//
// Its [ID] is the cleaned absolute path with a file:// scheme, which is what
// makes it survive a restart and a re-scan of the directory. Its revision is
// the modification time and size, established by [FileSource.Probe]; before
// the first probe it is empty, which means "not validated yet" and explicitly
// not "immutable".
type FileSource struct {
	path string
	id   ID
	mime string
}

// File returns a source for the picture at path.
//
// It performs no blocking I/O: the path is cleaned and made absolute, and the
// media type is guessed from the extension. Whether the file exists is not
// known until the pipeline probes it. A relative path is resolved against the
// working directory at the time of the call, because an ID that depended on
// the working directory at load time would not be stable.
func File(path string) *FileSource {
	abs, err := filepath.Abs(path)
	if err != nil {
		// Only possible when the working directory is gone. Fall back to
		// the cleaned relative path rather than failing here: Open will
		// produce the real error with the real reason.
		abs = filepath.Clean(path)
	}
	return &FileSource{
		path: abs,
		id:   ID("file://" + filepath.ToSlash(abs)),
		mime: mimeByExtension(abs),
	}
}

// Path is the cleaned absolute path.
func (s *FileSource) Path() string { return s.path }

// Metadata returns the identity and the guessed media type. Dimensions and
// revision stay empty until the pipeline has probed the file.
func (s *FileSource) Metadata() Metadata {
	return Metadata{ID: s.id, MIMEType: s.mime}
}

// Open opens the file for reading.
func (s *FileSource) Open(ctx context.Context) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("asset: open %s: %w", s.id, unwrapPathError(err))
	}
	return f, nil
}

// Probe stats the file. The revision is its modification time in nanoseconds
// and its size, which changes whenever the content can have changed.
//
// Fresh is zero: a stat costs microseconds, so there is no reason to trust a
// remembered answer, and trusting one is how a cache serves the previous
// version of a file that was just overwritten.
func (s *FileSource) Probe(ctx context.Context) (ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("asset: stat %s: %w", s.id, unwrapPathError(err))
	}
	if fi.IsDir() {
		return ProbeResult{}, fmt.Errorf("asset: %s: %w", s.id, ErrNotAPicture)
	}
	return ProbeResult{
		Revision: strconv.FormatInt(fi.ModTime().UnixNano(), 36) + "-" +
			strconv.FormatInt(fi.Size(), 36),
		Size:     fi.Size(),
		MIMEType: s.mime,
	}, nil
}

// String is the redacted description used in diagnostics.
func (s *FileSource) String() string { return string(s.id) }

// unwrapPathError drops the wrapper that repeats the path, which the caller
// has already put in the message in its sanitised form.
func unwrapPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func mimeByExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".jpe":
		return MIMEJPEG
	case ".png":
		return MIMEPNG
	default:
		return ""
	}
}

// --- HTTP --------------------------------------------------------------------

// DefaultHTTPFreshness is how long an HTTP revision is trusted when the
// response carries a validator but no explicit lifetime.
//
// The number is a policy and not a standard. A response with an ETag and no
// Cache-Control is revalidated after this long; until then the cached
// thumbnail is served without touching the network.
const DefaultHTTPFreshness = 5 * time.Minute

// MaxHTTPFreshness caps whatever a server asks for. A max-age of a year is a
// promise a gallery should not keep across a restart.
const MaxHTTPFreshness = 24 * time.Hour

// HTTPSource is a picture behind an HTTP or HTTPS URL.
//
// # Credentials
//
// The URL used for the request and the URL used for identity are not the same
// string. What the identity contains is exactly this:
//
//   - The scheme, the lowercased host and port, and the path, verbatim.
//   - No userinfo, and no fragment.
//   - No query, unless a parameter was named by [HTTPSource.WithPublicQuery].
//     Everything else collapses into a single opaque parameter holding a
//     truncated SHA-256 of the redacted parameters, so that two URLs that
//     differ only in their query still have different identities and their own
//     cache entries, while nothing of what they differ in is legible.
//
// Redacting the query by default is a change WU-R made after a review
// demonstrated the previous rule leaking. The previous rule redacted only the
// parameters an application had declared with [HTTPSource.WithSecretQuery],
// which makes the safe behaviour the one you have to remember: a pre-signed
// URL whose signature parameter is spelled "signature" rather than "sig"
// carried the signature into the ID, into every log line and into the fetch
// error. The default is now redaction and legibility is the opt-in.
//
// # What is *not* redacted, and cannot be
//
// The path. It is the only part of a URL that distinguishes two pictures in a
// readable way, and an ID that hashed it would be useless in a diagnostic and
// would still not be a secret store. A credential that travels in a path
// segment therefore appears in the [ID], in [HTTPSource.String] and in any
// error naming the source. Put credentials in a header, in the query or in a
// signing transport plus [HTTPSource.WithCredential]; do not put them in a
// path.
//
// Errors from net/http are unwrapped rather than passed on, because a
// *url.Error carries the requested URL. The project plan, section 9 and
// section 15, forbids credentials in cache file names and in diagnostic
// output; see TestCredentialsNeverLeak.
//
// The credentials still have to reach the cache *key*, or two users of the same
// endpoint would share entries. A query credential reaches it by itself,
// through the fingerprint above. One that travels by another route reaches it
// through [HTTPSource.WithHeader] or [HTTPSource.WithCredential], which the
// pipeline hashes and never stores.
type HTTPSource struct {
	req    *url.URL // the real URL, including any credentials
	id     ID       // the redacted identity
	ns     string   // credential fingerprint, hashed into the cache key
	header http.Header
	client *http.Client
	secret []string
	public []string
	method string
}

// HTTP returns a source for the picture at rawURL.
//
// It performs no blocking I/O; an unparseable or non-HTTP URL is reported by
// the first [HTTPSource.Open] or [HTTPSource.Fetch] rather than here, because
// a constructor that can fail would force error handling into every list of
// pictures an application builds.
func HTTP(rawURL string) *HTTPSource {
	s := &HTTPSource{method: http.MethodGet}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		// Keep something identifying but obviously broken, and redact it
		// the same way a valid URL is redacted: a malformed URL can still
		// contain a password.
		s.id = ID("http-invalid://" + redactRawURL(rawURL))
		return s
	}
	s.req = u
	s.reid()
	return s
}

// WithClient sets the HTTP client. The zero value uses [http.DefaultClient].
// The client is not copied; it must be safe for concurrent use, which the
// net/http one is.
func (s *HTTPSource) WithClient(c *http.Client) *HTTPSource { s.client = c; return s }

// WithHeader adds a request header.
//
// A header whose name is a well known credential carrier — Authorization,
// Proxy-Authorization, Cookie — additionally contributes to the cache
// namespace, so two sources that differ only in their token do not share cache
// entries. The value itself is never stored; only a hash of it reaches the key.
func (s *HTTPSource) WithHeader(name, value string) *HTTPSource {
	if s.header == nil {
		s.header = make(http.Header, 4)
	}
	s.header.Add(name, value)
	if isCredentialHeader(name) {
		s.ns = fingerprint(s.ns, name, value)
	}
	return s
}

// WithCredential adds an opaque credential fingerprint to the cache namespace
// without sending anything.
//
// Use it when the credential travels by a route gift cannot see, for example a
// cookie jar on the client or a signing transport. Any stable string that
// differs per identity will do; it is hashed into the cache key and is never
// written to disk or to a log.
func (s *HTTPSource) WithCredential(fp string) *HTTPSource {
	s.ns = fingerprint(s.ns, "credential", fp)
	return s
}

// WithPublicQuery names query parameters that are safe to keep legible in the
// identity, for example a width or a format.
//
// Every other parameter is redacted; see the type documentation. A parameter
// named here is kept verbatim in the [ID], in [HTTPSource.String] and in every
// error, so name only parameters that are certainly not credentials.
func (s *HTTPSource) WithPublicQuery(names ...string) *HTTPSource {
	s.public = append(s.public, names...)
	if s.req != nil {
		s.reid()
	}
	return s
}

// WithSecretQuery declares query parameters as credentials.
//
// Since WU-R it is no longer what keeps them out of the identity — the default
// does that for every parameter — and it is not needed for cache separation
// either, because a differing query already yields a differing [ID]. What it
// still does is fold the values into the cache namespace, which matters when
// the same picture is reachable under several equivalent signatures and the
// application wants them separated by credential rather than by URL. It also
// overrides [HTTPSource.WithPublicQuery] for the same name, so that a list of
// public parameters cannot accidentally expose one.
//
// They are still sent.
func (s *HTTPSource) WithSecretQuery(names ...string) *HTTPSource {
	s.secret = append(s.secret, names...)
	if s.req != nil {
		q := s.req.Query()
		for _, n := range names {
			for _, v := range q[n] {
				s.ns = fingerprint(s.ns, n, v)
			}
		}
		s.reid()
	}
	return s
}

// reid recomputes the redacted identity from the real URL.
func (s *HTTPSource) reid() {
	u := *s.req
	u.User = nil
	if u.Host != "" {
		u.Host = strings.ToLower(u.Host)
	}
	u.RawQuery = redactQuery(u.RawQuery, s.public, s.secret)
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	s.id = ID(u.String())
	if s.req.User != nil {
		// The user name alone identifies a namespace even without the
		// password, and the password must never reach the key in clear.
		pw, _ := s.req.User.Password()
		s.ns = fingerprint(s.ns, "userinfo", s.req.User.Username()+":"+pw)
	}
}

// Metadata returns the redacted identity. Nothing else is known before a
// fetch.
func (s *HTTPSource) Metadata() Metadata { return Metadata{ID: s.id} }

// CacheNamespace is the credential fingerprint; see [Namespacer].
func (s *HTTPSource) CacheNamespace() string { return s.ns }

// String is the redacted URL. It never contains a credential.
func (s *HTTPSource) String() string { return string(s.id) }

// Open performs an unconditional GET. It exists to satisfy [Source]; the
// pipeline uses [HTTPSource.Fetch], which can revalidate.
func (s *HTTPSource) Open(ctx context.Context) (io.ReadCloser, error) {
	r, err := s.Fetch(ctx, "")
	if err != nil {
		return nil, err
	}
	return r.Body, nil
}

// Fetch performs a conditional GET; see [Fetcher].
func (s *HTTPSource) Fetch(ctx context.Context, ifRevision string) (FetchResult, error) {
	if s.req == nil {
		return FetchResult{}, fmt.Errorf("asset: fetch %s: %w", s.id, ErrBadURL)
	}
	if s.req.Scheme != "http" && s.req.Scheme != "https" {
		return FetchResult{}, fmt.Errorf("asset: fetch %s: %w", s.id, ErrBadURL)
	}
	req, err := http.NewRequestWithContext(ctx, s.method, s.req.String(), nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("asset: fetch %s: %w", s.id, redactHTTPError(err))
	}
	for k, vs := range s.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if tag, date, ok := splitRevision(ifRevision); ok {
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		} else if date != "" {
			req.Header.Set("If-Modified-Since", date)
		}
	}
	client := s.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("asset: fetch %s: %w", s.id, redactHTTPError(err))
	}
	switch {
	case resp.StatusCode == http.StatusNotModified:
		resp.Body.Close()
		return FetchResult{
			NotModified: true,
			Revision:    ifRevision,
			Fresh:       freshnessOf(resp.Header),
		}, nil
	case resp.StatusCode != http.StatusOK:
		resp.Body.Close()
		return FetchResult{}, fmt.Errorf("asset: fetch %s: %w",
			s.id, &StatusError{Status: resp.StatusCode})
	}
	return FetchResult{
		Revision: revisionOf(resp.Header),
		Size:     resp.ContentLength,
		MIMEType: mimeOf(resp.Header),
		Fresh:    freshnessOf(resp.Header),
		Body:     resp.Body,
	}, nil
}

// StatusError is a non-OK HTTP status. It carries no URL, so it is safe to
// print.
type StatusError struct{ Status int }

func (e *StatusError) Error() string {
	return "http status " + strconv.Itoa(e.Status) + " " + http.StatusText(e.Status)
}

// Temporary reports whether retrying could plausibly help. A 404 is not
// temporary; a 503 is. The pipeline uses it to decide between a backoff and a
// quarantine; see [BackoffPolicy].
func (e *StatusError) Temporary() bool {
	return e.Status == http.StatusRequestTimeout ||
		e.Status == http.StatusTooManyRequests ||
		e.Status >= 500
}

// revisionOf builds a revision from whatever validators a response carries.
//
// The encoding keeps the two apart so that a later conditional request can put
// each one in its own header. No validator yields an empty revision, and an
// empty revision disables disk caching: the project plan, section 9, is
// explicit that "ohne Validator ist eine URL keine ewige Revision".
func revisionOf(h http.Header) string {
	if tag := strings.TrimSpace(h.Get("ETag")); tag != "" {
		return "etag:" + tag
	}
	if lm := strings.TrimSpace(h.Get("Last-Modified")); lm != "" {
		return "lm:" + lm
	}
	return ""
}

func splitRevision(rev string) (etag, date string, ok bool) {
	switch {
	case strings.HasPrefix(rev, "etag:"):
		return rev[len("etag:"):], "", true
	case strings.HasPrefix(rev, "lm:"):
		return "", rev[len("lm:"):], true
	}
	return "", "", false
}

func mimeOf(h http.Header) string {
	ct := h.Get("Content-Type")
	if ct == "" {
		return ""
	}
	t, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return ""
	}
	return t
}

// freshnessOf implements the freshness policy: an explicit max-age wins, no-store
// and no-cache force revalidation, and anything else gets the default. The
// result is clamped to [MaxHTTPFreshness].
func freshnessOf(h http.Header) time.Duration {
	cc := strings.ToLower(h.Get("Cache-Control"))
	for _, part := range strings.Split(cc, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "no-store", part == "no-cache", part == "must-revalidate":
			return 0
		case strings.HasPrefix(part, "max-age="):
			n, err := strconv.ParseInt(part[len("max-age="):], 10, 64)
			if err != nil || n <= 0 {
				return 0
			}
			d := time.Duration(n) * time.Second
			return min(d, MaxHTTPFreshness)
		}
	}
	if cc != "" {
		return DefaultHTTPFreshness
	}
	if exp := h.Get("Expires"); exp != "" {
		t, err := http.ParseTime(exp)
		if err != nil {
			return 0
		}
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		return min(d, MaxHTTPFreshness)
	}
	return DefaultHTTPFreshness
}

func isCredentialHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "Proxy-Authorization", "Cookie", "X-Api-Key":
		return true
	}
	return false
}

// redactHTTPError removes the URL net/http puts into every transport error.
//
// net/http returns a *url.Error whose URL field is the requested URL with only
// the password blanked; the user name, and any signature in the query, survive.
// Only the inner error is kept.
func redactHTTPError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// redactedQueryParam holds the fingerprint of everything that was redacted.
// It is named so that a reader of a log line can see that a query was there.
const redactedQueryParam = "gift_redacted"

// redactQuery keeps the allow-listed parameters and replaces the rest with one
// fingerprint of them.
//
// The fingerprint, not a fixed placeholder: a placeholder would give two
// different pre-signed URLs for two different pictures the same identity, and
// the second one would be served the first one's thumbnail out of the cache.
// Identity has to survive redaction; legibility does not.
func redactQuery(raw string, public, secret []string) string {
	if raw == "" {
		return ""
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		// Unparseable: nothing can be classified, so nothing is kept.
		return redactedQueryParam + "=" + fingerprint("", "query", raw)
	}
	keep := make(url.Values, len(public))
	hide := make(url.Values, len(q))
	for name, vs := range q {
		if slices.Contains(public, name) && !slices.Contains(secret, name) {
			keep[name] = vs
			continue
		}
		hide[name] = vs
	}
	if len(hide) > 0 {
		keep.Set(redactedQueryParam, fingerprint("", "query", hide.Encode()))
	}
	return keep.Encode()
}

// redactRawURL keeps enough of an unparseable URL to recognise it and drops
// anything that could be a secret.
func redactRawURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i] + "?REDACTED"
	}
	if i := strings.Index(raw, "@"); i >= 0 {
		raw = "REDACTED@" + raw[i+1:]
	}
	return raw
}
