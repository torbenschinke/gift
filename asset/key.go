package asset

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"strconv"
)

// ProcessingVersion is the version of gift's own thumbnail production:
// decoders, scaler, orientation handling and pixel format.
//
// It is part of every cache key, so raising it invalidates every cached
// thumbnail on disk without deleting anything. Raise it whenever a change
// would make the same input produce different pixels; otherwise a user who
// upgrades gift keeps looking at thumbnails produced by the old code for as
// long as their files do not change.
const ProcessingVersion uint32 = 1

// Key identifies one cached thumbnail.
//
// It carries exactly what the project plan, section 9, requires — "Cache-Keys
// enthalten Revision, Groesse, Orientierung und Verarbeitungsversion" — plus
// the namespace that keeps authenticated sources apart.
//
// # What is in it and why
//
//   - Namespace separates sources that share an address but not a credential.
//     It is already a hash when it gets here; see [Key.Namespace].
//   - ID is the stable identity of the picture.
//   - Revision is its content version. An empty revision means the source was
//     never validated, and an entry is then not written to disk at all: the
//     project plan refuses to treat an unvalidated URL as an eternal revision.
//   - Size is the ladder rung in pixels of the longest edge.
//   - Orientation is the transform that was applied. It is normalised, so
//     "no EXIF" and "EXIF said 1" share an entry, which is correct because
//     they produce the same pixels.
//   - ProcessingVersion is gift's own version; see [ProcessingVersion].
type Key struct {
	// Namespace is a hash of the credential a source carries, or the empty
	// string for an unauthenticated one. It is never a credential itself;
	// see [HTTPSource.WithCredential].
	Namespace string
	// ID is the stable identity of the picture.
	ID ID
	// Revision is the content version; empty means "not validated".
	Revision string
	// Size is the ladder rung, in pixels of the longest edge.
	Size int
	// Orientation is the applied EXIF orientation, normalised.
	Orientation Orientation
	// ProcessingVersion is the version of gift's thumbnail production.
	ProcessingVersion uint32
}

// String is a stable, printable form of the key.
//
// It is safe to log: every component of it is either a redacted identity or
// already a hash. See TestCredentialsNeverLeak.
func (k Key) String() string {
	return string(k.ID) + "@" + k.Revision +
		"/" + strconv.Itoa(k.Size) +
		"/o" + strconv.Itoa(int(k.Orientation.Normalised())) +
		"/p" + strconv.FormatUint(uint64(k.ProcessingVersion), 10) +
		"/n" + k.Namespace
}

// fileName is the name of this key's entry in the disk cache.
//
// It is a hash and nothing else. The project plan, sections 9 and 15, forbids
// credentials in cache file names, and the only way to be sure of that is for
// the name to contain no input at all in clear — not a path, not a host, not a
// query. A hash also removes every question about path separators, case
// insensitive file systems, reserved names and length limits.
func (k Key) fileName() string {
	h := sha256.New()
	writeKey(h, k)
	var sum [sha256.Size]byte
	return cacheEncoding.EncodeToString(h.Sum(sum[:0])[:20]) + cacheExt
}

// writeKey feeds the key to a hash with unambiguous framing: every component
// is length prefixed, so no two different keys can produce the same byte
// stream by rearranging their contents.
func writeKey(h interface{ Write([]byte) (int, error) }, k Key) {
	var n [8]byte
	put := func(s string) {
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		_, _ = h.Write(n[:])
		_, _ = h.Write([]byte(s))
	}
	put("gift/asset/thumb/v1")
	put(k.Namespace)
	put(string(k.ID))
	put(k.Revision)
	put(strconv.Itoa(k.Size))
	put(strconv.Itoa(int(k.Orientation.Normalised())))
	put(strconv.FormatUint(uint64(k.ProcessingVersion), 10))
}

// hintName is the name of the revision hint for one picture. The hint is keyed
// without the revision, since finding out the revision is the point of it.
func hintName(namespace string, id ID) string {
	h := sha256.New()
	var n [8]byte
	put := func(s string) {
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		_, _ = h.Write(n[:])
		_, _ = h.Write([]byte(s))
	}
	put("gift/asset/hint/v1")
	put(namespace)
	put(string(id))
	var sum [sha256.Size]byte
	return cacheEncoding.EncodeToString(h.Sum(sum[:0])[:20]) + hintExt
}

// cacheEncoding is lower case base32 without padding: case insensitive file
// systems cannot collide two names, and there is no separator character.
var cacheEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

const (
	cacheExt = ".thumb"
	hintExt  = ".rev"
)

// fingerprint folds a credential into an opaque namespace.
//
// The result is a hash and the input is not recoverable from it. Folding
// rather than replacing means several credentials — a header, a signed query
// parameter and userinfo — combine into one namespace, each hashed together
// with the name it arrived under. The fold is a chain, so it depends on the
// order the credentials were added in; two sources constructed the same way
// with the same credentials agree, and that is all a cache namespace needs.
func fingerprint(prev, name, value string) string {
	h := sha256.New()
	var n [8]byte
	put := func(s string) {
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		_, _ = h.Write(n[:])
		_, _ = h.Write([]byte(s))
	}
	put("gift/asset/ns/v1")
	put(prev)
	put(name)
	put(value)
	var sum [sha256.Size]byte
	return cacheEncoding.EncodeToString(h.Sum(sum[:0])[:16])
}
