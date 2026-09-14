package asset_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torbenschinke/gift/asset"
)

// pictureServer serves one picture with an ETag and counts what was asked of
// it. Nothing in these tests touches a real network.
type pictureServer struct {
	gets     atomic.Int64
	condGets atomic.Int64
	notMod   atomic.Int64
	etag     atomic.Value // string
	body     atomic.Value // []byte
	maxAge   atomic.Int64
	status   atomic.Int64
}

func newPictureServer(t *testing.T, body []byte, etag string) (*pictureServer, *httptest.Server) {
	ps := &pictureServer{}
	ps.etag.Store(etag)
	ps.body.Store(body)
	ps.status.Store(200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.gets.Add(1)
		if s := ps.status.Load(); s != 200 {
			w.WriteHeader(int(s))
			return
		}
		tag, _ := ps.etag.Load().(string)
		if tag != "" {
			w.Header().Set("ETag", tag)
		}
		if ma := ps.maxAge.Load(); ma > 0 {
			w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", ma))
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			ps.condGets.Add(1)
			if inm == tag {
				ps.notMod.Add(1)
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		b, _ := ps.body.Load().([]byte)
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.WriteHeader(200)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return ps, srv
}

func TestHTTPSourceLoadsAPicture(t *testing.T) {
	ps, srv := newPictureServer(t, jpegBytes(t, 200, 100), `"v1"`)
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: asset.HTTP(srv.URL + "/a.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Metadata.Revision != `etag:"v1"` {
		t.Errorf("Revision = %q, want the ETag", r.Metadata.Revision)
	}
	if r.Metadata.Width != 200 || r.Metadata.Height != 100 {
		t.Errorf("metadata = %dx%d", r.Metadata.Width, r.Metadata.Height)
	}
	if ps.gets.Load() != 1 {
		t.Errorf("the server saw %d requests, want 1", ps.gets.Load())
	}
}

// A fresh revision hint serves the cached thumbnail without touching the
// network at all. That is the freshness policy doing its job.
func TestHTTPFreshHintSkipsTheNetwork(t *testing.T) {
	ps, srv := newPictureServer(t, jpegBytes(t, 200, 100), `"v1"`)
	ps.maxAge.Store(300)
	cache := t.TempDir()

	c := newCollector()
	p1 := asset.NewPipeline(diskConfig(c, cache, 0))
	p1.Request(asset.Request{Source: asset.HTTP(srv.URL + "/a.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; r.Err() != nil {
		t.Fatal(r.Err())
	}
	p1.Close()
	if ps.gets.Load() != 1 {
		t.Fatalf("first run made %d requests", ps.gets.Load())
	}

	c.reset()
	p2 := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p2.Close()
	p2.Request(asset.Request{Source: asset.HTTP(srv.URL + "/a.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if !r.FromDisk {
		t.Error("the warm start did not come from the disk cache")
	}
	if got := ps.gets.Load(); got != 1 {
		t.Errorf("the server saw %d requests, want 1: a fresh hint must not revalidate", got)
	}
}

// A stale hint revalidates conditionally; a 304 costs a round trip and no
// pixels.
func TestHTTPStaleHintRevalidatesAndAcceptsNotModified(t *testing.T) {
	ps, srv := newPictureServer(t, jpegBytes(t, 200, 100), `"v1"`)
	ps.maxAge.Store(0) // no-cache: always revalidate
	cache := t.TempDir()

	c := newCollector()
	p1 := asset.NewPipeline(diskConfig(c, cache, 0))
	p1.Request(asset.Request{Source: asset.HTTP(srv.URL + "/a.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; r.Err() != nil {
		t.Fatal(r.Err())
	}
	p1.Close()

	c.reset()
	p2 := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p2.Close()
	p2.Request(asset.Request{Source: asset.HTTP(srv.URL + "/a.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if ps.condGets.Load() != 1 || ps.notMod.Load() != 1 {
		t.Errorf("conditional=%d notModified=%d, want 1 and 1",
			ps.condGets.Load(), ps.notMod.Load())
	}
	if !r.FromDisk {
		t.Error("after a 304 the thumbnail must come from the cache")
	}
	st := p2.Stats()
	if st.Decodes != 0 {
		t.Errorf("Decodes = %d after a 304, want 0", st.Decodes)
	}
	if st.NotModified != 1 {
		t.Errorf("NotModified = %d, want 1", st.NotModified)
	}
}

// A changed ETag is a changed revision: the picture is fetched and decoded
// again and the new one is served.
func TestHTTPChangedValidatorRefetches(t *testing.T) {
	ps, srv := newPictureServer(t, jpegBytes(t, 200, 100), `"v1"`)
	cache := t.TempDir()
	c := newCollector()
	p := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p.Close()

	url := srv.URL + "/a.jpg"
	p.Request(asset.Request{Source: asset.HTTP(url), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; r.Err() != nil {
		t.Fatal(r.Err())
	}

	ps.etag.Store(`"v2"`)
	ps.body.Store(jpegBytes(t, 100, 400))
	c.reset()
	p.Invalidate(asset.HTTP(url).Metadata().ID)
	p.Request(asset.Request{Source: asset.HTTP(url), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Metadata.Revision != `etag:"v2"` {
		t.Errorf("Revision = %q, want the new ETag", r.Metadata.Revision)
	}
	if r.Metadata.Width != 100 || r.Metadata.Height != 400 {
		t.Errorf("served %dx%d, want the new 100x400", r.Metadata.Width, r.Metadata.Height)
	}
}

// Without a validator there is no revision, and without a revision nothing is
// written to disk: the project plan refuses to treat a bare URL as an eternal
// content version.
func TestHTTPWithoutValidatorIsNotDiskCached(t *testing.T) {
	_, srv := newPictureServer(t, jpegBytes(t, 100, 100), "")
	cache := t.TempDir()
	c := newCollector()
	p := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p.Close()

	p.Request(asset.Request{Source: asset.HTTP(srv.URL + "/a.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Metadata.Revision != "" {
		t.Errorf("Revision = %q, want empty without a validator", r.Metadata.Revision)
	}
	if st := p.Stats(); st.Disk.Writes != 0 {
		t.Errorf("Disk.Writes = %d, want 0 without a revision", st.Disk.Writes)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	ps, srv := newPictureServer(t, jpegBytes(t, 64, 64), `"v1"`)
	ps.status.Store(404)
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: asset.HTTP(srv.URL + "/missing.jpg"), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() == nil {
		t.Fatal("a 404 produced a success")
	}
	var se *asset.StatusError
	if !errors.As(r.Err(), &se) || se.Status != 404 {
		t.Fatalf("err = %v, want a StatusError 404", r.Err())
	}
	if se.Temporary() {
		t.Error("a 404 must not be temporary")
	}
}

func TestHTTPBadURL(t *testing.T) {
	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()
	for _, u := range []string{"::::", "ftp://host/a.jpg", "not a url at all"} {
		c.reset()
		p.Request(asset.Request{Source: asset.HTTP(u), Size: 64,
			Priority: asset.Visible, OnResult: c.onResult})
		r := c.waitFor(t, 1)[0]
		if r.Err() == nil {
			t.Errorf("%q produced a success", u)
		}
	}
}

// --- credentials -------------------------------------------------------------

// The secrets that must never appear anywhere.
var secrets = []string{
	"hunter2", "s3cr3t-token", "AKIAIOSFODNN7EXAMPLE", "sigv4signature",
	"Bearer s3cr3t-token", "sessioncookievalue",
	// The undeclared one. WU-R: a signature parameter that nobody thought to
	// name in WithSecretQuery used to travel into the ID and into every
	// error, which made the safe behaviour the one the caller has to
	// remember. Redaction is now the default and this string must not appear
	// anywhere either.
	"SEKRIT",
}

func containsSecret(s string) string {
	for _, sec := range secrets {
		if strings.Contains(s, sec) {
			return sec
		}
	}
	return ""
}

// TestCredentialsNeverLeak is the assertion the brief demands: no credential in
// a cache file name and none in any diagnostic string.
func TestCredentialsNeverLeak(t *testing.T) {
	const rawURL = "https://user:hunter2@images.example.com/private/a.jpg?" +
		"sig=sigv4signature&signature=SEKRIT&w=200"
	src := asset.HTTP(rawURL).
		WithSecretQuery("sig").
		WithPublicQuery("w").
		WithHeader("Authorization", "Bearer s3cr3t-token").
		WithHeader("Cookie", "session=sessioncookievalue").
		WithCredential("AKIAIOSFODNN7EXAMPLE")

	id := string(src.Metadata().ID)
	if s := containsSecret(id); s != "" {
		t.Fatalf("the ID %q contains the secret %q", id, s)
	}
	if strings.Contains(id, "user:") || strings.Contains(id, "@") {
		t.Errorf("the ID %q still carries userinfo", id)
	}
	if !strings.Contains(id, "images.example.com") {
		t.Errorf("the ID %q is not recognisable any more", id)
	}
	if s := containsSecret(src.String()); s != "" {
		t.Fatalf("String() contains the secret %q", s)
	}

	ns := src.CacheNamespace()
	if ns == "" {
		t.Fatal("credentials did not reach the cache namespace at all; two users would share entries")
	}
	if s := containsSecret(ns); s != "" {
		t.Fatalf("the namespace contains the secret %q", s)
	}

	key := asset.Key{Namespace: ns, ID: src.Metadata().ID, Revision: `etag:"x"`,
		Size: 256, Orientation: asset.OrientationTopLeft, ProcessingVersion: 1}
	if s := containsSecret(key.String()); s != "" {
		t.Fatalf("Key.String() contains the secret %q", s)
	}
	for _, name := range []string{asset.FileNameForTest(key), asset.HintNameForTest(ns, src.Metadata().ID)} {
		if s := containsSecret(name); s != "" {
			t.Fatalf("the cache file name %q contains the secret %q", name, s)
		}
		// The name is a hash: no separator, no host, no path.
		if strings.ContainsAny(name, "/:@?&=") {
			t.Errorf("the cache file name %q is not an opaque hash", name)
		}
	}

	// The parameter that was declared public is still legible, which is what
	// makes the ID usable in a diagnostic at all.
	if !strings.Contains(id, "w=200") {
		t.Errorf("the ID %q dropped the parameter declared public", id)
	}

	// Redaction must not collapse two different URLs into one identity, or
	// the second picture would be served the first one's thumbnail.
	a := asset.HTTP("https://images.example.com/p/a.jpg?token=aaa")
	b := asset.HTTP("https://images.example.com/p/a.jpg?token=bbb")
	if a.Metadata().ID == b.Metadata().ID {
		t.Error("two URLs with different undeclared queries produced the same ID")
	}
	if strings.Contains(string(a.Metadata().ID), "aaa") {
		t.Errorf("an undeclared query value survived in %q", a.Metadata().ID)
	}
	if asset.HTTP("https://images.example.com/p/a.jpg?token=aaa").Metadata().ID != a.Metadata().ID {
		t.Error("the same URL built twice produced different identities")
	}

	// Two sources that differ only in their credential must not share a key.
	other := asset.HTTP("https://images.example.com/private/a.jpg?w=200").
		WithHeader("Authorization", "Bearer someone-else")
	if other.CacheNamespace() == ns {
		t.Error("two different credentials produced the same cache namespace")
	}
	// And the same credential twice must produce the same key, or nothing
	// would ever hit.
	again := asset.HTTP(rawURL).
		WithSecretQuery("sig").
		WithPublicQuery("w").
		WithHeader("Authorization", "Bearer s3cr3t-token").
		WithHeader("Cookie", "session=sessioncookievalue").
		WithCredential("AKIAIOSFODNN7EXAMPLE")
	if again.CacheNamespace() != ns || again.Metadata().ID != src.Metadata().ID {
		t.Error("the same source built twice produced different cache identities")
	}
}

// The whole error path is checked too, including the one net/http produces for
// itself: a *url.Error carries the requested URL with only the password
// blanked, and the user name and any signed query survive in it.
func TestCredentialsDoNotLeakThroughErrors(t *testing.T) {
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	cfg.Timeout = 2 * time.Second
	p := asset.NewPipeline(cfg)
	defer p.Close()

	// 127.0.0.1:1 refuses connections, which produces a transport error.
	src := asset.HTTP("http://user:hunter2@127.0.0.1:1/a.jpg?sig=sigv4signature&signature=SEKRIT").
		WithSecretQuery("sig").
		WithHeader("Authorization", "Bearer s3cr3t-token")
	p.Request(asset.Request{Source: src, Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() == nil {
		t.Fatal("connecting to a closed port succeeded")
	}
	if s := containsSecret(r.Err().Error()); s != "" {
		t.Fatalf("the error %q contains the secret %q", r.Err(), s)
	}
	if s := containsSecret(string(r.ID)); s != "" {
		t.Fatalf("the result ID contains the secret %q", s)
	}
}

// And nothing lands on disk under a name that contains one either.
func TestNoCredentialInAnyCacheFileName(t *testing.T) {
	body := jpegBytes(t, 100, 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cache := t.TempDir()
	c := newCollector()
	p := asset.NewPipeline(diskConfig(c, cache, 0))
	src := asset.HTTP(srv.URL+"/a.jpg?sig=sigv4signature&signature=SEKRIT").
		WithSecretQuery("sig").
		WithHeader("Authorization", "Bearer s3cr3t-token")
	p.Request(asset.Request{Source: src, Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; r.Err() != nil {
		t.Fatal(r.Err())
	}
	p.Close()

	files := 0
	err := filepath.Walk(cache, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if s := containsSecret(path); s != "" {
			t.Errorf("the path %q contains the secret %q", path, s)
		}
		if !fi.IsDir() {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("nothing was written, so the assertion proves nothing")
	}
}

// TestConcurrentHTTPFetchesShareTheInputBudget is WU-R's regression test for a
// reservation that ignored what it was reserving for.
//
// [asset.Fetcher] does not know its size in stage one, and the pipeline used
// to reserve [asset.Config.MaxEncodedBytes] for it — 64 MiB of a 96 MiB
// default budget. Two HTTP fetches could then never be in flight at once
// whatever Workers said, which the live example reported as an input peak of
// 67108877 bytes against a limit of 100663296.
//
// The server here blocks until every worker has arrived, so the test cannot
// pass by being fast: if the budget serialises the fetches, nobody arrives
// second and it times out.
func TestConcurrentHTTPFetchesShareTheInputBudget(t *testing.T) {
	const workers = 4
	body := jpegBytes(t, 120, 90)
	var inFlight atomic.Int64
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inFlight.Add(1) >= workers {
			once.Do(func() { close(arrived) })
		}
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
		}
		<-release
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	cfg.Workers = workers
	cfg.MaxEncodedBytes = 64 << 20
	cfg.InputBudget = 96 << 20
	p := asset.NewPipeline(cfg)
	defer p.Close()

	for i := range workers {
		p.Request(asset.Request{Source: asset.HTTP(fmt.Sprintf("%s/p%d.jpg", srv.URL, i)),
			Size: 64, Priority: asset.Visible, Generation: uint64(i), OnResult: c.onResult})
	}
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatalf("only %d of %d fetches were in flight at once: the input budget serialised them",
			inFlight.Load(), workers)
	}
	close(release)

	for _, r := range c.waitFor(t, workers) {
		if r.Err() != nil {
			t.Fatalf("generation %d: %v", r.Generation, r.Err())
		}
	}
	// And the reservation is the size of the pictures, not the size of the
	// limit: four fetches of a few kilobytes each must not peak near 64 MiB.
	if st := p.Stats(); st.Input.Peak > int64(workers*len(body)+4*64<<10) {
		t.Errorf("input peak %d bytes for %d pictures of %d bytes",
			st.Input.Peak, workers, len(body))
	}
}
