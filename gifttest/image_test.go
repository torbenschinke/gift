package gifttest_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The picture half of the harness's coverage of step 4 of the project plan,
// section 12. A gift application that shows photographs is the reason step 4
// exists, and the harness had no way to say "a real picture reached the
// framebuffer" at all: every assertion it offered was satisfied just as well
// by the placeholder rectangle a tile draws while it waits.

// TestImageGolden takes a real file through the real pipeline — probe, decode,
// thumbnail, admission, upload — and compares the pixels.
//
// The picture is written rather than checked in, for the reason ui's own image
// tests give: generated data cannot drift from what the test claims about it.
// The golden is of the *composition*, so it is testdata like any other.
func TestImageGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quadrants.png")
	// Four flat quadrants: a resample cannot hide a rotation, a mirror or a
	// channel swap, and a reader can check the golden by eye in one glance.
	pic := quadrants(32,
		color.RGBA{220, 40, 40, 255}, color.RGBA{40, 200, 80, 255},
		color.RGBA{60, 90, 230, 255}, color.RGBA{240, 200, 40, 255})
	if err := os.WriteFile(path, encodePNG(t, pic), 0o600); err != nil {
		t.Fatal(err)
	}

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Sizes: []int{64}, Workers: 1})
	defer func() { pipe.Close(); del.drain() }()
	ui.SetImagePipeline(nil)
	ui.SetImagePipeline(pipe)
	defer ui.SetImagePipeline(nil)

	h := gifttest.New(t, gifttest.Options{
		View: ui.Image(asset.File(path)).Key("picture").Size(64).Frame(64, 64).
			Fit(ui.FitStretch).Placeholder(ui.RGB(0, 0, 0)),
		Size:       geom.Sz(96, 96),
		Background: ui.RGB(20, 24, 34),
	})
	del.waitFor(t, 1)
	del.drain()
	h.Frame()
	// The frame that spends the upload budget. Without it the golden below
	// would be a golden of the placeholder, and it would pass for ever.
	h.Warm()

	h.AssertGolden("image")
}

// TestImagePlaceholderIsNotAPicture is the structural assertion the golden
// cannot make, and it runs without a GPU.
//
// Before a thumbnail arrives a tile paints its placeholder colour and no
// picture at all. That is the state every image test is accidentally in, and
// it is indistinguishable from success unless something says so.
func TestImagePlaceholderIsNotAPicture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solid.png")
	if err := os.WriteFile(path, encodePNG(t, quadrants(16,
		color.RGBA{200, 60, 40, 255}, color.RGBA{200, 60, 40, 255},
		color.RGBA{200, 60, 40, 255}, color.RGBA{200, 60, 40, 255})), 0o600); err != nil {
		t.Fatal(err)
	}
	ui.SetImagePipeline(nil)
	defer ui.SetImagePipeline(nil)

	placeholder := ui.RGB(70, 74, 88)
	h := gifttest.New(t, gifttest.Options{
		View: ui.Image(asset.File(path)).Key("picture").Size(64).Frame(64, 64).
			Placeholder(placeholder),
		Size: geom.Sz(96, 96),
	})
	pic := h.Find(gifttest.ByKey("picture"))
	pic.AssertBackground(placeholder)
	h.AssertOps("a picture", 0, func(op render.Op) bool {
		return op.Kind == render.OpImage && op.Image != 0
	})
}

// --- fixtures ---------------------------------------------------------------

// deliverer collects the pipeline's completion closures so that a test runs
// them on its own goroutine, which is where gift's state lives.
type deliverer struct{ ch chan func() }

func newDeliverer() *deliverer { return &deliverer{ch: make(chan func(), 64)} }

func (d *deliverer) deliver(fn func()) { d.ch <- fn }

// waitFor blocks until at least n closures are waiting, without running them.
func (d *deliverer) waitFor(t testing.TB, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(d.ch) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d pipeline results arrived within ten seconds", len(d.ch), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// drain runs every waiting closure and returns how many there were.
func (d *deliverer) drain() int {
	n := 0
	for {
		select {
		case fn := <-d.ch:
			fn()
			n++
		default:
			return n
		}
	}
}

// quadrants builds a square image with four flat corners.
func quadrants(size int, tl, tr, bl, br color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	half := size / 2
	for y := range size {
		for x := range size {
			c := tl
			switch {
			case x >= half && y < half:
				c = tr
			case x < half && y >= half:
				c = bl
			case x >= half && y >= half:
				c = br
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the test picture: %v", err)
	}
	return buf.Bytes()
}
