//go:build giftgpu

package gifttest

import (
	"image"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/geom"
)

// This is the only file in the package that imports a backend, and the only
// one that needs a GPU. The dependency direction of the project plan, section
// 3, allows it here and nowhere else: gifttest may know about backend/ebiten
// in its tagged half, because the tagged half is the one that renders.

// AssertGolden renders the current frame offscreen and compares it against
// testdata/<name>.png.
//
// # The comparison
//
// Per channel, with a tolerance of [GoldenTolerance] and no budget for
// outlying pixels. The rationale for that number, and the reason there is no
// percentage budget, are on the constant. Ebitengine's own documentation of
// ReadPixels warns that returned values "can include very slight differences
// between some machines", which is precisely what the tolerance is sized for.
//
// # On failure
//
// The image this run produced and a visual diff are written next to the
// expected one, and all three paths appear in the failure message. The diff is
// the expected image dimmed with every offending pixel in magenta, so that the
// shape of the difference is legible rather than a nearly black subtraction.
//
// # Rewriting
//
// GIFT_UPDATE_GOLDEN=1 rewrites the golden instead of comparing. A run that
// did so logs "GOLDEN REWRITTEN" for every file it touched and the test that
// verified nothing says so in its own output, so a CI log in which goldens
// were rewritten is obvious rather than green.
func (h *Harness) AssertGolden(name string) {
	h.t.Helper()
	img := h.Image()
	ok, msg, _ := compareGolden(name, img)
	switch {
	case ok && msg != "":
		// A rewrite. Logged, never silent.
		h.t.Log(msg)
	case ok:
	default:
		h.t.Errorf("gifttest: golden %q does not match.\n%s", name, msg)
	}
}

// Image renders the current frame into an offscreen image and returns a copy
// of its pixels.
//
// It renders the display list of the last frame through the real
// [backend.Renderer], so what it compares is what the application draws, not a
// second software path that could agree with the golden while the window shows
// something else.
func (h *Harness) Image() image.Image {
	h.t.Helper()
	r := h.backendRenderer()
	if r == nil {
		return nil
	}
	// Physical pixels: the viewport times the density, which is what
	// [game.LayoutF] asks Ebitengine for in a real window. A golden at
	// density 2 is therefore four times the area of the same golden at
	// density 1, and comparing the two is a comparison of two different
	// images by design — see [Options.Density].
	w, hgt := h.deviceSize()
	if w <= 0 || hgt <= 0 {
		h.t.Fatalf("gifttest: the viewport is %gx%g; a golden image needs a positive size", h.size.W, h.size.H)
		return nil
	}
	dst := eb.NewImage(w, hgt)
	// Not cleared to anything. A fresh Ebitengine image is transparent black,
	// and transparent black is exactly what Ebitengine hands a real window at
	// the top of every Draw; see the project plan, section 6. So the only
	// pixels in this image are pixels the application painted.
	//
	// This used to be a Fill with [Options.Background], defaulting to opaque
	// white, and that one line made every golden in this module a picture of
	// the application plus a rectangle it never drew. It hid the fact that
	// four demo screens had no window background at all. An application paints
	// its own — see ui.Window — and [Harness.AssertOpaque] is the gate.

	r.SetTarget(dst)
	// BeginFrame, then paint, then submit: exactly the order backend.Run
	// uses, and the order matters. The upload budget of the project plan,
	// section 11, is per *drawn* frame, it is reset by BeginFrame, and the
	// painters that spend it run inside App.Paint. Submitting the list of the
	// previous frame instead would never let a painter upload anything, and
	// every golden of a picture would quietly be a golden of a placeholder.
	r.BeginFrame(geom.Sz(float32(w), float32(hgt)))
	h.list = h.app.Paint()
	r.Submit(h.list)
	r.EndFrame()

	px := make([]byte, 4*w*hgt)
	dst.ReadPixels(px)
	out := image.NewRGBA(image.Rect(0, 0, w, hgt))
	copy(out.Pix, px)
	return out
}

// Warm renders one frame through the backend and throws the pixels away.
//
// It exists for pictures. The upload budget of the project plan, section 11,
// is per *drawn* frame, so the frame in which a painter first resolves an
// image handle is the frame that requests the upload, and the texture is
// resident only from the next one. A golden taken without this would be a
// golden of a placeholder — and would pass for ever, which is the failure mode
// [Harness.backendRenderer] exists to prevent in the first place.
//
// It is a no-op without the giftgpu tag, where there is no texture residency
// to warm.
func (h *Harness) Warm() {
	h.t.Helper()
	r := h.backendRenderer()
	if r == nil {
		return
	}
	w, hgt := h.deviceSize()
	if w <= 0 || hgt <= 0 {
		return
	}
	dst := eb.NewImage(w, hgt)
	defer dst.Deallocate()
	r.SetTarget(dst)
	r.BeginFrame(geom.Sz(float32(w), float32(hgt)))
	h.list = h.app.Paint()
	r.Submit(h.list)
	r.EndFrame()
}

// framebuffer renders the current frame and returns its pixels. With a
// graphics context there is always one; see the stub for the other half.
func (h *Harness) framebuffer(string) (image.Image, bool) {
	h.t.Helper()
	return h.Image(), true
}

// backendRenderer returns the harness's renderer, creating it and wiring its
// image service into the application on first use.
//
// The wiring is the point. Without it [gift.PaintContext.Images] is nil, every
// picture falls back to its placeholder, and a golden of a gallery full of
// photographs would be a golden of coloured rectangles that passes for ever.
func (h *Harness) backendRenderer() *backend.Renderer {
	h.t.Helper()
	if r, ok := h.renderer.(*backend.Renderer); ok {
		return r
	}
	r, err := backend.NewRenderer()
	if err != nil {
		h.t.Fatalf("gifttest: creating the renderer: %v", err)
		return nil
	}
	h.renderer = r
	h.app.SetImages(r.Images())
	return r
}

// Main runs the test binary inside Ebitengine's loop, which is what makes
// reading pixels back legal: the pinned source says at
// ebiten.Image.ReadPixels that it "can't be called outside the main loop".
//
// A package with golden tests installs it and gets both modes from one line:
//
//	func TestMain(m *testing.M) { gifttest.Main(m) }
func Main(m *testing.M) {
	g := &loopGame{m: m, code: 1}
	if err := eb.RunGame(g); err != nil {
		panic(err)
	}
	osExit(g.code)
}

type loopGame struct {
	m    *testing.M
	code int
}

func (g *loopGame) Update() error {
	g.code = g.m.Run()
	return eb.Termination
}

func (*loopGame) Draw(*eb.Image)             {}
func (*loopGame) Layout(int, int) (int, int) { return 64, 64 }
