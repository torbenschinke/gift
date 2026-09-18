package asset_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/worldiety/gift/asset"
)

// baseConfig is a pipeline with no disk cache and a synchronous, drained
// delivery executor.
func baseConfig(c *collector) asset.Config {
	return asset.Config{
		Workers: 2,
		Sizes:   []int{64, 128, 256},
		Deliver: c.Deliver,
	}
}

func TestFileSourceIdentityIsStableAndDoesNoIO(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.jpg", jpegBytes(t, 40, 20))

	s := asset.File(path)
	m := s.Metadata()
	if m.ID == "" || !strings.HasPrefix(string(m.ID), "file://") {
		t.Fatalf("ID = %q, want a file:// identity", m.ID)
	}
	if m.MIMEType != asset.MIMEJPEG {
		t.Errorf("MIMEType = %q", m.MIMEType)
	}
	// The project plan, section 9: the revision is empty until probing, and
	// empty means "not validated yet", not "immutable".
	if m.Revision != "" {
		t.Errorf("Revision = %q before probing, want empty", m.Revision)
	}
	if m.Width != 0 || m.Height != 0 {
		t.Errorf("dimensions known before probing: %dx%d", m.Width, m.Height)
	}

	// A source for a file that does not exist constructs fine and fails only
	// when used.
	missing := asset.File(dir + "/nope.jpg")
	if missing.Metadata().ID == "" {
		t.Error("a missing file still needs an identity")
	}
	if _, err := missing.Probe(t.Context()); err == nil {
		t.Error("probing a missing file should fail")
	}

	pr, err := s.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if pr.Revision == "" {
		t.Error("probe produced no revision")
	}
	if pr.Size <= 0 {
		t.Error("probe produced no size")
	}
}

func TestPipelineDecodesJPEGAndPNG(t *testing.T) {
	dir := t.TempDir()
	jpgPath := writeFile(t, dir, "a.jpg", jpegBytes(t, 400, 200))
	pngPath := writeFile(t, dir, "b.png", pngBytes(t, 100, 300))

	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()

	p.Request(asset.Request{Source: asset.File(jpgPath), Size: 128,
		Priority: asset.Visible, Generation: 7, OnResult: c.onResult})
	p.Request(asset.Request{Source: asset.File(pngPath), Size: 128,
		Priority: asset.Visible, Generation: 8, OnResult: c.onResult})

	res := c.waitFor(t, 2)
	for _, r := range res {
		if r.Err() != nil {
			t.Fatalf("%s: %v", r.ID, r.Err())
		}
		if r.Image == nil {
			t.Fatalf("%s: no image", r.ID)
		}
		if r.Size != 128 {
			t.Errorf("%s: rung = %d, want 128", r.ID, r.Size)
		}
		if strings.HasSuffix(string(r.ID), ".jpg") {
			if r.Generation != 7 {
				t.Errorf("generation = %d, want the one passed in", r.Generation)
			}
			if r.Metadata.Width != 400 || r.Metadata.Height != 200 {
				t.Errorf("metadata = %dx%d, want 400x200", r.Metadata.Width, r.Metadata.Height)
			}
			if r.Image.Width() != 128 || r.Image.Height() != 64 {
				t.Errorf("thumbnail = %dx%d, want 128x64", r.Image.Width(), r.Image.Height())
			}
		} else {
			if r.Metadata.Width != 100 || r.Metadata.Height != 300 {
				t.Errorf("metadata = %dx%d, want 100x300", r.Metadata.Width, r.Metadata.Height)
			}
			if r.Image.Width() != 43 || r.Image.Height() != 128 {
				t.Errorf("thumbnail = %dx%d, want 43x128", r.Image.Width(), r.Image.Height())
			}
		}
		if got, want := len(r.Image.Pix()), r.Image.Width()*r.Image.Height()*4; got != want {
			t.Errorf("pixel slice is %d bytes, want %d", got, want)
		}
	}
}

// A picture smaller than the rung is not enlarged: that would spend memory and
// upload bandwidth to add no information.
func TestSmallPictureIsNotEnlarged(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "tiny.png", pngBytes(t, 16, 9))
	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()
	p.Request(asset.Request{Source: asset.File(path), Size: 256,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Image.Width() != 16 || r.Image.Height() != 9 {
		t.Errorf("thumbnail = %dx%d, want the original 16x9", r.Image.Width(), r.Image.Height())
	}
}

// --- limits ------------------------------------------------------------------

func TestPixelLimitIsRefusedBeforeAllocating(t *testing.T) {
	dir := t.TempDir()
	// 400x200 is 80 000 pixels; the limit below is 10 000.
	path := writeFile(t, dir, "big.jpg", jpegBytes(t, 400, 200))

	c := newCollector()
	cfg := baseConfig(c)
	cfg.MaxPixels = 10000
	// A decode budget far too small to hold the picture proves the refusal
	// happened before any reservation: if the pixel limit were checked after
	// the decode, this test would either allocate or deadlock.
	cfg.DecodeBudget = 4096
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if !errors.Is(r.Err(), asset.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", r.Err())
	}
	if r.Image != nil {
		t.Error("a refused picture must not carry an image")
	}
	st := p.Stats()
	if st.Decodes != 0 {
		t.Errorf("Decodes = %d, want 0: nothing should have been decoded", st.Decodes)
	}
	if st.Decode.Peak != 0 {
		t.Errorf("decode budget peak = %d, want 0: nothing should have been reserved", st.Decode.Peak)
	}
}

func TestEncodedSizeLimitIsEnforced(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.png", pngBytes(t, 200, 200))

	c := newCollector()
	cfg := baseConfig(c)
	cfg.MaxEncodedBytes = 128
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if !errors.Is(r.Err(), asset.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", r.Err())
	}
}

func TestCorruptPictureFails(t *testing.T) {
	dir := t.TempDir()
	good := jpegBytes(t, 64, 64)
	truncated := good[:len(good)/3]
	garbage := []byte("this is not a picture at all, not even close")

	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()

	p.Request(asset.Request{Source: asset.File(writeFile(t, dir, "t.jpg", truncated)),
		Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	p.Request(asset.Request{Source: asset.File(writeFile(t, dir, "g.jpg", garbage)),
		Size: 64, Priority: asset.Visible, OnResult: c.onResult})

	for _, r := range c.waitFor(t, 2) {
		if r.Err() == nil {
			t.Errorf("%s: decoded successfully, want an error", r.ID)
		}
		if r.Image != nil {
			t.Errorf("%s: a failed decode must not carry an image", r.ID)
		}
	}
	// A failure is a value and never a panic; reaching this line is the
	// assertion for the project plan, section 15.
}

// --- orientation -------------------------------------------------------------

func TestOrientationSwapsProbedDimensions(t *testing.T) {
	dir := t.TempDir()
	// Stored 400 wide by 200 high; orientation 6 is a 90 degree rotation, so
	// the presentation is 200 by 400.
	raw := withEXIFOrientation(t, jpegBytes(t, 400, 200), asset.OrientationRightTop)
	path := writeFile(t, dir, "rot.jpg", raw)

	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()
	p.Request(asset.Request{Source: asset.File(path), Size: 128,
		Priority: asset.Visible, OnResult: c.onResult})

	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.Orientation != asset.OrientationRightTop {
		t.Fatalf("Orientation = %v, want right-top", r.Orientation)
	}
	// This is the agreement the brief demands between probing and the
	// gallery's Correction: the metadata, and therefore the correction, is
	// the oriented presentation.
	if r.Metadata.Width != 200 || r.Metadata.Height != 400 {
		t.Errorf("metadata = %dx%d, want the oriented 200x400",
			r.Metadata.Width, r.Metadata.Height)
	}
	corr := r.Correction()
	if corr.Width != 200 || corr.Height != 400 || corr.ID != r.ID {
		t.Errorf("Correction = %+v", corr)
	}
	if r.Image.Width() != 64 || r.Image.Height() != 128 {
		t.Errorf("thumbnail = %dx%d, want a portrait 64x128",
			r.Image.Width(), r.Image.Height())
	}
}

func TestOrientationPixelsAreActuallyRotated(t *testing.T) {
	dir := t.TempDir()
	// A PNG is lossless, so the corner colours survive; orientation is only
	// read from JPEG, so the rotation is applied to a JPEG and compared
	// against what the transform is defined to do.
	base := jpegBytes(t, 64, 32)
	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()

	// Orientation 3 is a 180 degree rotation: the top left of the result is
	// the bottom right of the original. The gradient of testImage makes that
	// checkable: red grows to the right, green downward, so the top left
	// pixel of a rotated picture is bright in both.
	p.Request(asset.Request{Source: asset.File(writeFile(t, dir, "n.jpg", base)),
		Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	plain := c.waitFor(t, 1)[0]
	c.reset()

	rot := withEXIFOrientation(t, base, asset.OrientationBottomRight)
	p.Request(asset.Request{Source: asset.File(writeFile(t, dir, "r.jpg", rot)),
		Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	turned := c.waitFor(t, 1)[0]

	if plain.Err() != nil || turned.Err() != nil {
		t.Fatalf("errors: %v / %v", plain.Err(), turned.Err())
	}
	pp, tp := plain.Image.Pix(), turned.Image.Pix()
	w, h := plain.Image.Width(), plain.Image.Height()
	if w != turned.Image.Width() || h != turned.Image.Height() {
		t.Fatalf("sizes differ: %dx%d vs %dx%d", w, h, turned.Image.Width(), turned.Image.Height())
	}
	at := func(pix []byte, x, y int) [4]byte {
		i := y*w*4 + x*4
		return [4]byte{pix[i], pix[i+1], pix[i+2], pix[i+3]}
	}
	// Allow a little slack for JPEG and the scaler.
	near := func(a, b [4]byte) bool {
		for i := range 3 {
			d := int(a[i]) - int(b[i])
			if d < -12 || d > 12 {
				return false
			}
		}
		return true
	}
	if !near(at(tp, 0, 0), at(pp, w-1, h-1)) {
		t.Errorf("top left of the rotated picture is %v, want the bottom right %v of the original",
			at(tp, 0, 0), at(pp, w-1, h-1))
	}
	if near(at(tp, 0, 0), at(pp, 0, 0)) {
		t.Error("the rotated picture equals the original at the top left; nothing was rotated")
	}
}

func TestOrientationOfEveryValue(t *testing.T) {
	for o := asset.OrientationTopLeft; o <= asset.OrientationLeftBottom; o++ {
		w, h := o.Oriented(100, 50)
		if o.SwapsAxes() {
			if w != 50 || h != 100 {
				t.Errorf("%v: Oriented = %dx%d, want 50x100", o, w, h)
			}
		} else if w != 100 || h != 50 {
			t.Errorf("%v: Oriented = %dx%d, want 100x50", o, w, h)
		}
	}
	if asset.OrientationUnknown.Normalised() != asset.OrientationTopLeft {
		t.Error("unknown must normalise to top-left")
	}
	if asset.Orientation(99).Normalised() != asset.OrientationTopLeft {
		t.Error("an out of range orientation must normalise to top-left")
	}
}

// A PNG never carries an orientation, which is documented behaviour and not an
// oversight; assert it so that a future change to the decoder is noticed.
func TestPNGIsAlwaysUpright(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "p.png", pngBytes(t, 80, 40))
	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()
	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Orientation != asset.OrientationUnknown {
		t.Errorf("Orientation = %v, want unknown for a PNG", r.Orientation)
	}
	if r.Metadata.Width != 80 || r.Metadata.Height != 40 {
		t.Errorf("metadata = %dx%d", r.Metadata.Width, r.Metadata.Height)
	}
}

// --- deduplication, priority, cancellation -----------------------------------

func TestRequestsAreDeduplicated(t *testing.T) {
	src := newBlocking("dedup", jpegBytes(t, 200, 100))
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 4
	p := asset.NewPipeline(cfg)
	defer p.Close()

	const n = 16
	for i := range n {
		p.Request(asset.Request{Source: src, Size: 128, Priority: asset.Visible,
			Generation: uint64(i), OnResult: c.onResult})
	}
	src.release()

	res := c.waitFor(t, n)
	if len(res) != n {
		t.Fatalf("got %d results, want %d", len(res), n)
	}
	if got := src.openCount(); got != 1 {
		t.Errorf("the source was opened %d times, want exactly 1", got)
	}
	seen := make(map[uint64]bool)
	for _, r := range res {
		if r.Err() != nil {
			t.Fatalf("result error: %v", r.Err())
		}
		if seen[r.Generation] {
			t.Errorf("generation %d delivered twice", r.Generation)
		}
		seen[r.Generation] = true
	}
	if st := p.Stats(); st.Deduplicated != n-1 {
		t.Errorf("Deduplicated = %d, want %d", st.Deduplicated, n-1)
	}
	if st := p.Stats(); st.Decodes != 1 {
		t.Errorf("Decodes = %d, want 1", st.Decodes)
	}
}

func TestVisibleOvertakesQueuedPrefetch(t *testing.T) {
	held := newBlocking("held", jpegBytes(t, 64, 64))
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1 // one worker, so the queue order is the delivery order
	p := asset.NewPipeline(cfg)
	defer p.Close()

	// Occupy the single worker.
	p.Request(asset.Request{Source: held, Size: 64, Priority: asset.Visible,
		Generation: 0, OnResult: c.onResult})
	waitUntil(t, func() bool { return held.openCount() == 1 })

	// Queue prefetches behind it, then one visible request last.
	pre := make([]*blockingSource, 4)
	for i := range pre {
		pre[i] = newBlocking("pre"+string(rune('a'+i)), jpegBytes(t, 64, 64))
		pre[i].gate = nil
		p.Request(asset.Request{Source: pre[i], Size: 64, Priority: asset.Prefetch,
			Generation: uint64(100 + i), OnResult: c.onResult})
	}
	vis := newBlocking("visible", jpegBytes(t, 64, 64))
	vis.gate = nil
	p.Request(asset.Request{Source: vis, Size: 64, Priority: asset.Visible,
		Generation: 999, OnResult: c.onResult})

	held.release()
	res := c.waitFor(t, 6)

	// Result 0 is the held job. Result 1 must be the visible one that was
	// queued last, ahead of four prefetches queued before it.
	if res[1].Generation != 999 {
		gens := make([]uint64, len(res))
		for i, r := range res {
			gens[i] = r.Generation
		}
		t.Fatalf("delivery order %v: the visible request did not overtake the prefetches", gens)
	}
}

func TestQueuedPrefetchIsPromotedNotDuplicated(t *testing.T) {
	held := newBlocking("held", jpegBytes(t, 64, 64))
	target := newBlocking("target", jpegBytes(t, 64, 64))
	target.gate = nil

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: held, Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	waitUntil(t, func() bool { return held.openCount() == 1 })

	filler := make([]*blockingSource, 3)
	for i := range filler {
		filler[i] = newBlocking("f"+string(rune('a'+i)), jpegBytes(t, 64, 64))
		filler[i].gate = nil
		p.Request(asset.Request{Source: filler[i], Size: 64, Priority: asset.Prefetch,
			Generation: uint64(10 + i), OnResult: c.onResult})
	}
	// target is queued as a prefetch first, then wanted as visible.
	p.Request(asset.Request{Source: target, Size: 64, Priority: asset.Prefetch,
		Generation: 1, OnResult: c.onResult})
	p.Request(asset.Request{Source: target, Size: 64, Priority: asset.Visible,
		Generation: 2, OnResult: c.onResult})

	held.release()
	res := c.waitFor(t, 6)

	if st := p.Stats(); st.Promotions != 1 {
		t.Errorf("Promotions = %d, want 1", st.Promotions)
	}
	if got := target.openCount(); got != 1 {
		t.Errorf("the promoted source was opened %d times, want 1", got)
	}
	// Both generations are answered, and they come before the fillers that
	// were queued earlier.
	pos := map[uint64]int{}
	for i, r := range res {
		pos[r.Generation] = i
	}
	if pos[1] > pos[10] || pos[2] > pos[10] {
		t.Errorf("promoted job was delivered after a prefetch queued before it: %v", pos)
	}
}

func TestCancellationDropsQueuedWorkAndSuppressesTheResult(t *testing.T) {
	held := newBlocking("held", jpegBytes(t, 64, 64))
	doomed := newBlocking("doomed", jpegBytes(t, 64, 64))
	doomed.gate = nil

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: held, Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	waitUntil(t, func() bool { return held.openCount() == 1 })

	tk := p.Request(asset.Request{Source: doomed, Size: 64, Priority: asset.Prefetch,
		Generation: 42, OnResult: c.onResult})
	tk.Cancel()

	held.release()
	res := c.waitFor(t, 1)
	// Give the worker a chance to produce a second result, if it were going
	// to.
	time.Sleep(50 * time.Millisecond)
	c.drain()

	for _, r := range res {
		if r.Generation == 42 {
			t.Fatal("a cancelled request delivered a result")
		}
	}
	if doomed.openCount() != 0 {
		t.Errorf("a cancelled queued job was opened %d times, want 0", doomed.openCount())
	}
	if st := p.Stats(); st.Cancelled != 1 {
		t.Errorf("Cancelled = %d, want 1", st.Cancelled)
	}
}

// The generation is echoed back untouched, which is what lets the consumer
// reject a stale result. The pipeline must not interpret it — that is the
// separation of content version, request generation and slot generation the
// project plan, section 9, insists on.
func TestGenerationIsEchoedAndCheckedByTheConsumer(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.jpg", jpegBytes(t, 64, 64))
	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()

	// The consumer's model of a tile: it advanced its generation while the
	// request was in flight, so the answer belongs to a picture the tile no
	// longer shows.
	var current uint64 = 5
	var applied, rejected int
	accept := func(r asset.Result) {
		if r.Generation != current {
			rejected++
			return
		}
		applied++
	}
	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, Generation: 4, OnResult: accept})
	p.Request(asset.Request{Source: asset.File(path), Size: 128,
		Priority: asset.Visible, Generation: 5, OnResult: accept})

	deadline := time.Now().Add(5 * time.Second)
	for applied+rejected < 2 && time.Now().Before(deadline) {
		c.drain()
		time.Sleep(time.Millisecond)
	}
	if applied != 1 || rejected != 1 {
		t.Fatalf("applied = %d, rejected = %d, want 1 and 1", applied, rejected)
	}
}

// --- backoff -----------------------------------------------------------------

func TestFailingSourceIsNotHammered(t *testing.T) {
	src := &failingSource{id: "bad", rev: "r1", err: tempError{"network is down"}}
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1
	cfg.Backoff = asset.BackoffPolicy{Base: time.Hour, Max: time.Hour, Attempts: 3}
	p := asset.NewPipeline(cfg)
	defer p.Close()

	const n = 20
	for i := range n {
		p.Request(asset.Request{Source: src, Size: 64, Priority: asset.Visible,
			Generation: uint64(i), OnResult: c.onResult})
		c.waitFor(t, i+1)
	}
	if got := src.count(); got != 1 {
		t.Errorf("the failing source was opened %d times for %d requests, want 1", got, n)
	}
	res := c.waitFor(t, n)
	backoffs := 0
	for _, r := range res {
		if r.Err() == nil {
			t.Fatal("a failing source produced a success")
		}
		if errors.Is(r.Err(), asset.ErrBackoff) {
			backoffs++
		}
	}
	if backoffs != n-1 {
		t.Errorf("%d results carried ErrBackoff, want %d", backoffs, n-1)
	}
	if st := p.Stats(); st.BackoffRefused != uint64(n-1) {
		t.Errorf("BackoffRefused = %d, want %d", st.BackoffRefused, n-1)
	}
}

// A failure that a retry cannot fix quarantines the source until its revision
// changes, which is what the project plan, section 15, asks for literally.
func TestPermanentFailureIsRetriedOnlyAfterARevisionChange(t *testing.T) {
	src := &failingSource{id: "gone", rev: "r1", err: &asset.StatusError{Status: 404}}
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1
	p := asset.NewPipeline(cfg)
	defer p.Close()

	req := func(gen uint64) asset.Result {
		c.reset()
		p.Request(asset.Request{Source: src, Size: 64, Priority: asset.Visible,
			Generation: gen, OnResult: c.onResult})
		return c.waitFor(t, 1)[0]
	}
	req(1)
	if got := src.count(); got != 1 {
		t.Fatalf("first request opened %d times", got)
	}
	r := req(2)
	if !errors.Is(r.Err(), asset.ErrBackoff) {
		t.Errorf("second request err = %v, want ErrBackoff", r.Err())
	}
	if got := src.count(); got != 1 {
		t.Errorf("quarantined source was opened again (%d)", got)
	}

	// The content changed. The quarantine lifts.
	src.mu.Lock()
	src.rev = "r2"
	src.mu.Unlock()
	req(3)
	if got := src.count(); got != 2 {
		t.Errorf("after a revision change the source was opened %d times, want 2", got)
	}

	// Forget is the manual override.
	src.mu.Lock()
	src.rev = "r2"
	src.mu.Unlock()
	p.Forget("gone")
	req(4)
	if got := src.count(); got != 3 {
		t.Errorf("after Forget the source was opened %d times, want 3", got)
	}
}

func waitUntil(t testing.TB, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never became true")
		}
		time.Sleep(time.Millisecond)
	}
}

// A catalogue built without dimensions is corrected from probed results, by
// stable ID and in a batch, which is the hand-over the project plan,
// section 10, prescribes between the pipeline and the gallery.
func TestUnknownDimensionsAreCorrectedThroughTheCollection(t *testing.T) {
	dir := t.TempDir()
	sizes := [][2]int{{400, 200}, {100, 300}, {160, 80}}
	var items []asset.Metadata
	var srcs []asset.Source
	for i, wh := range sizes {
		var data []byte
		if i == 2 {
			// Stored 160x80 with a 90 degree rotation: the catalogue must
			// end up with the *oriented* 80x160, or every such picture
			// gets the wrong aspect ratio in the masonry column.
			data = withEXIFOrientation(t, jpegBytes(t, wh[0], wh[1]), asset.OrientationRightTop)
		} else {
			data = jpegBytes(t, wh[0], wh[1])
		}
		s := asset.File(writeFile(t, dir, string(rune('a'+i))+".jpg", data))
		srcs = append(srcs, s)
		// The catalogue knows nothing but the identity, which is the normal
		// state before probing.
		items = append(items, asset.Metadata{ID: s.Metadata().ID})
	}
	coll := asset.NewCollection(items)
	if coll.At(0).HasDimensions() {
		t.Fatal("the catalogue should start without dimensions")
	}
	beforeMeta := coll.MetadataVersion()
	beforeStruct := coll.StructureVersion()

	c := newCollector()
	p := asset.NewPipeline(baseConfig(c))
	defer p.Close()
	var batch []asset.Correction
	for i, s := range srcs {
		p.Request(asset.Request{Source: s, Size: 64, Priority: asset.Visible,
			Generation: uint64(i), OnResult: func(r asset.Result) {
				if r.Err() != nil {
					t.Errorf("%s: %v", r.ID, r.Err())
					return
				}
				batch = append(batch, r.Correction())
				c.onResult(r)
			}})
	}
	c.waitFor(t, len(srcs))

	if n := coll.ApplyCorrections(batch); n != len(srcs) {
		t.Fatalf("applied %d corrections, want %d", n, len(srcs))
	}
	want := [][2]uint32{{400, 200}, {100, 300}, {80, 160}}
	for i, w := range want {
		got := coll.At(i)
		if got.Width != w[0] || got.Height != w[1] {
			t.Errorf("entry %d = %dx%d, want %dx%d", i, got.Width, got.Height, w[0], w[1])
		}
		if got.Revision == "" {
			t.Errorf("entry %d has no revision after probing", i)
		}
	}
	if coll.MetadataVersion() == beforeMeta {
		t.Error("the metadata version did not move")
	}
	// And the structure version did not: nothing moved, so a gallery may
	// keep its tile bindings. See Collection.StructureVersion.
	if coll.StructureVersion() != beforeStruct {
		t.Error("a correction batch moved the structure version; tiles would be unbound for nothing")
	}
}

// --- WU-R: every EXIF orientation, in pixels ---------------------------------

// edge names where the EXIF specification puts the stored 0th row and 0th
// column of a picture. That sentence *is* the definition of tag 0x0112, and
// deriving the transform from it here rather than from the eight cases in
// asset/orientation.go is the point: a test that repeats the implementation's
// arithmetic proves that the code equals itself.
type edge int

const (
	edgeTop edge = iota
	edgeBottom
	edgeLeft
	edgeRight
)

// exifPlacement is the table of EXIF 2.32, tag Orientation, verbatim:
// where the 0th row and the 0th column of the stored grid end up in the
// picture the viewer sees.
var exifPlacement = map[asset.Orientation]struct{ row, col edge }{
	asset.OrientationTopLeft:     {edgeTop, edgeLeft},
	asset.OrientationTopRight:    {edgeTop, edgeRight},
	asset.OrientationBottomRight: {edgeBottom, edgeRight},
	asset.OrientationBottomLeft:  {edgeBottom, edgeLeft},
	asset.OrientationLeftTop:     {edgeLeft, edgeTop},
	asset.OrientationRightTop:    {edgeRight, edgeTop},
	asset.OrientationRightBottom: {edgeRight, edgeBottom},
	asset.OrientationLeftBottom:  {edgeLeft, edgeBottom},
}

// displayPos maps a stored pixel to its displayed position, from the placement
// sentence alone.
//
// The stored row index runs along whichever display axis its 0th row was
// placed on, and likewise the column index. A placement on "top" or "left"
// counts up from there, one on "bottom" or "right" counts down — and the
// extent it counts down from is the *displayed* extent, which is why the axis
// swapping cases use the other dimension.
func displayPos(o asset.Orientation, x, y, w, h int) (int, int) {
	pl := exifPlacement[o]
	dw, dh := o.Oriented(w, h)
	var dx, dy int
	switch pl.row {
	case edgeTop:
		dy = y
	case edgeBottom:
		dy = dh - 1 - y
	case edgeLeft:
		dx = y
	case edgeRight:
		dx = dw - 1 - y
	}
	switch pl.col {
	case edgeLeft:
		dx = x
	case edgeRight:
		dx = dw - 1 - x
	case edgeTop:
		dy = x
	case edgeBottom:
		dy = dh - 1 - x
	}
	return dx, dy
}

// TestEveryOrientationProducesTheRightPixels is the test WU-R adds because
// TestOrientationOfEveryValue promises "every value" and checks enum
// arithmetic. Only orientation 3 had a pixel test, and 3 is its own inverse,
// so it is the one value that cannot tell a rotation from a mirror plus a
// rotation. The four mirrored values — where transpose and transverse are
// classically swapped — had no pixel coverage at all.
func TestEveryOrientationProducesTheRightPixels(t *testing.T) {
	const w, h = 64, 32
	// Four flat quadrants, so that a lossy codec still answers the question
	// "which corner is this" unambiguously.
	quad := [4]color.RGBA{
		{220, 30, 30, 255},  // stored top left
		{30, 200, 60, 255},  // stored top right
		{40, 60, 220, 255},  // stored bottom left
		{230, 210, 40, 255}, // stored bottom right
	}
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := 0
			if x >= w/2 {
				i |= 1
			}
			if y >= h/2 {
				i |= 2
			}
			src.SetRGBA(x, y, quad[i])
		}
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for o := asset.OrientationTopLeft; o <= asset.OrientationLeftBottom; o++ {
		t.Run(o.String(), func(t *testing.T) {
			path := writeFile(t, dir, fmt.Sprintf("o%d.jpg", o),
				withEXIFOrientation(t, enc.Bytes(), o))
			c := newCollector()
			cfg := baseConfig(c)
			cfg.Sizes = []int{64}
			p := asset.NewPipeline(cfg)
			defer p.Close()
			p.Request(asset.Request{Source: asset.File(path), Size: 64,
				Priority: asset.Visible, OnResult: c.onResult})
			r := c.waitFor(t, 1)[0]
			if r.Err() != nil {
				t.Fatal(r.Err())
			}
			if r.Orientation != o {
				t.Fatalf("Orientation = %v, want %v", r.Orientation, o)
			}
			tn := r.Image
			if tn == nil {
				t.Fatal("no thumbnail")
			}
			dw, dh := o.Oriented(w, h)
			if tn.Width() != dw || tn.Height() != dh {
				t.Fatalf("thumbnail is %dx%d, want the oriented %dx%d",
					tn.Width(), tn.Height(), dw, dh)
			}
			if r.Metadata.Width != uint32(dw) || r.Metadata.Height != uint32(dh) {
				t.Errorf("metadata reports %dx%d, want %dx%d",
					r.Metadata.Width, r.Metadata.Height, dw, dh)
			}
			// The centre of each stored quadrant must appear where the
			// specification puts it, and nowhere else.
			for i, want := range quad {
				sx, sy := w/4, h/4
				if i&1 != 0 {
					sx = 3 * w / 4
				}
				if i&2 != 0 {
					sy = 3 * h / 4
				}
				dx, dy := displayPos(o, sx, sy, w, h)
				got := thumbPixel(t, tn, dx, dy)
				if !nearColor(got, want, 24) {
					t.Errorf("stored quadrant %d (%d,%d) shows %v at displayed (%d,%d), want %v",
						i, sx, sy, got, dx, dy, want)
				}
			}
		})
	}
}

func thumbPixel(t testing.TB, tn *asset.Thumbnail, x, y int) color.RGBA {
	t.Helper()
	if x < 0 || y < 0 || x >= tn.Width() || y >= tn.Height() {
		t.Fatalf("sample (%d,%d) is outside the %dx%d thumbnail", x, y, tn.Width(), tn.Height())
	}
	i := y*tn.Stride() + x*4
	p := tn.Pix()
	return color.RGBA{p[i], p[i+1], p[i+2], p[i+3]}
}

func nearColor(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x) - int(y)
		}
		return int(y) - int(x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}
