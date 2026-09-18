package asset_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/worldiety/gift/asset"
)

// --- test pictures -----------------------------------------------------------

// testImage paints a deterministic gradient so that a decoded thumbnail can be
// checked for being the right picture rather than merely the right size.
func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / max(1, w-1)),
				G: uint8(y * 255 / max(1, h-1)),
				B: 0x40, A: 0xFF,
			})
		}
	}
	return img
}

func jpegBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testImage(w, h), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage(w, h)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// withEXIFOrientation splices an APP1 EXIF segment carrying orientation o
// directly after the SOI of a JPEG.
//
// Building it here rather than checking in a photograph keeps the test data
// generated, as the brief requires, and makes all eight orientations testable
// without eight fixtures.
func withEXIFOrientation(t testing.TB, jpg []byte, o asset.Orientation) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatalf("not a JPEG")
	}
	// A minimal little endian TIFF header with one IFD entry.
	var tiff []byte
	tiff = append(tiff, 'I', 'I')
	tiff = binary.LittleEndian.AppendUint16(tiff, 42)
	tiff = binary.LittleEndian.AppendUint32(tiff, 8) // IFD0 at offset 8
	tiff = binary.LittleEndian.AppendUint16(tiff, 1) // one entry
	tiff = binary.LittleEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.LittleEndian.AppendUint16(tiff, 3) // SHORT
	tiff = binary.LittleEndian.AppendUint32(tiff, 1)
	tiff = binary.LittleEndian.AppendUint16(tiff, uint16(o))
	tiff = binary.LittleEndian.AppendUint16(tiff, 0)
	tiff = binary.LittleEndian.AppendUint32(tiff, 0) // no next IFD

	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	seg = append(seg, payload...)

	out := make([]byte, 0, len(jpg)+len(seg))
	out = append(out, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, jpg[2:]...)
	return out
}

func writeFile(t testing.TB, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// --- result collection -------------------------------------------------------

// collector is the stand-in for the UI executor. Real applications pass
// App.Post here; a test drains it by hand, which is also what makes the
// delivery order observable.
type collector struct {
	mu      sync.Mutex
	pending []func()
	ch      chan struct{}

	resMu   sync.Mutex
	results []asset.Result
}

func newCollector() *collector {
	return &collector{ch: make(chan struct{}, 1024)}
}

// Deliver is the [asset.Config.Deliver] seam.
func (c *collector) Deliver(fn func()) {
	c.mu.Lock()
	c.pending = append(c.pending, fn)
	c.mu.Unlock()
	select {
	case c.ch <- struct{}{}:
	default:
	}
}

// drain runs every posted closure, like App.Update does.
func (c *collector) drain() int {
	c.mu.Lock()
	todo := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, fn := range todo {
		fn()
	}
	return len(todo)
}

// onResult is the callback a request is made with. It copies what it needs,
// exactly as a real consumer must: the thumbnail is only valid for the
// duration of the call unless it is retained.
func (c *collector) onResult(r asset.Result) {
	c.resMu.Lock()
	defer c.resMu.Unlock()
	c.results = append(c.results, r)
}

// waitFor drains until n results have arrived or the deadline passes.
func (c *collector) waitFor(t testing.TB, n int) []asset.Result {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c.drain()
		c.resMu.Lock()
		got := len(c.results)
		c.resMu.Unlock()
		if got >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d results, got %d", n, got)
		}
		select {
		case <-c.ch:
		case <-time.After(2 * time.Millisecond):
		}
	}
	c.resMu.Lock()
	defer c.resMu.Unlock()
	out := make([]asset.Result, len(c.results))
	copy(out, c.results)
	return out
}

// waitOne drains until exactly one more result has arrived and discards it.
// It is the benchmark's synchronisation point.
func (c *collector) waitOne(t testing.TB) {
	c.waitFor(t, 1)
	c.reset()
}

func (c *collector) reset() {
	c.resMu.Lock()
	c.results = nil
	c.resMu.Unlock()
}

// --- sources for tests -------------------------------------------------------

// blockingSource gates Open so that a test can hold a worker and observe the
// queue behind it.
type blockingSource struct {
	meta  asset.Metadata
	data  []byte
	gate  chan struct{}
	opens int
	mu    sync.Mutex
	rev   string
}

func newBlocking(id string, data []byte) *blockingSource {
	return &blockingSource{
		meta: asset.Metadata{ID: asset.ID(id), MIMEType: asset.MIMEJPEG},
		data: data,
		gate: make(chan struct{}),
		rev:  "r1",
	}
}

func (s *blockingSource) Metadata() asset.Metadata { return s.meta }

func (s *blockingSource) Open(ctx context.Context) (io.ReadCloser, error) {
	s.mu.Lock()
	s.opens++
	s.mu.Unlock()
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func (s *blockingSource) Probe(ctx context.Context) (asset.ProbeResult, error) {
	return asset.ProbeResult{Revision: s.rev, Size: int64(len(s.data)),
		MIMEType: asset.MIMEJPEG}, nil
}

func (s *blockingSource) openCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opens
}

func (s *blockingSource) release() { close(s.gate) }

// failingSource fails every open, and counts how often it was asked.
type failingSource struct {
	id   asset.ID
	rev  string
	mu   sync.Mutex
	n    int
	err  error
	temp bool
}

func (s *failingSource) Metadata() asset.Metadata { return asset.Metadata{ID: s.id} }

func (s *failingSource) Open(ctx context.Context) (io.ReadCloser, error) {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return nil, s.err
}

func (s *failingSource) Probe(ctx context.Context) (asset.ProbeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return asset.ProbeResult{Revision: s.rev, Size: 1024, MIMEType: asset.MIMEJPEG}, nil
}

func (s *failingSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// tempError is an error that reports itself as worth retrying.
type tempError struct{ msg string }

func (e tempError) Error() string   { return e.msg }
func (e tempError) Temporary() bool { return true }
