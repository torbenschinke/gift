//go:build giftgpu

// These tests need a real graphics context: reading a pixel back requires
// Ebitengine's run loop, which requires a window. They are behind the build
// tag giftgpu so that `go test ./...` passes headless, as the project plan,
// section 12, criterion 4, requires.
//
//	go test -tags giftgpu ./backend/ebiten/
package ebiten

import (
	"image/color"
	"os"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// runLoopGame runs the test binary inside Ebitengine's loop, which is what
// makes (*ebiten.Image).At available.
type runLoopGame struct {
	m    *testing.M
	code int
}

func (g *runLoopGame) Update() error {
	g.code = g.m.Run()
	return eb.Termination
}

func (*runLoopGame) Draw(*eb.Image)             {}
func (*runLoopGame) Layout(int, int) (int, int) { return 64, 64 }

func TestMain(m *testing.M) {
	g := &runLoopGame{m: m, code: 1}
	if err := eb.RunGame(g); err != nil {
		panic(err)
	}
	os.Exit(g.code)
}

func drawList(t *testing.T, w, h int, bg color.RGBA, build func(l *render.List)) *eb.Image {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	dst := eb.NewImage(w, h)
	dst.Fill(bg)

	var l render.List
	l.Reset()
	build(&l)

	r.SetTarget(dst)
	r.BeginFrame(geom.Sz(float32(w), float32(h)))
	r.Submit(&l)
	r.EndFrame()
	return dst
}

// TestPremultipliedAlphaEndToEnd is the verification the whole colour
// convention rests on.
//
// render.Color is premultiplied, Ebitengine is premultiplied, and the vertex
// path copies the floats through untouched. If any of the three were wrong the
// result of blending a half transparent red over an opaque blue would be
// visibly off, and this test says by how much.
//
// Expectation, source over with premultiplied source (0.5, 0, 0, 0.5) on
// destination (0, 0, 1, 1):
//
//	out = src + dst*(1-src.a) = (0.5, 0, 0.5, 1)
//
// which is (128, 0, 128, 255) in eight bit, up to rounding.
func TestPremultipliedAlphaEndToEnd(t *testing.T) {
	dst := drawList(t, 16, 16, color.RGBA{0, 0, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(0, 0, 16, 16),
			Color:  render.RGBA(255, 0, 0, 128),
		})
	})

	got := dst.At(8, 8).(color.RGBA)
	t.Logf("50%% red over opaque blue = %v", got)

	want := color.RGBA{128, 0, 127, 255}
	const tol = 2
	if abs8(got.R, want.R) > tol || abs8(got.G, want.G) > tol ||
		abs8(got.B, want.B) > tol || abs8(got.A, want.A) > tol {
		t.Fatalf("got %v, want about %v; the premultiplied alpha convention is broken somewhere", got, want)
	}
}

// TestOpaqueFillIsExact checks that a fully opaque fill reaches the target
// unchanged, which is the other half of the convention: no accidental
// unpremultiply, no colour scale applied twice.
func TestOpaqueFillIsExact(t *testing.T) {
	dst := drawList(t, 16, 16, color.RGBA{0, 0, 0, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(0, 0, 16, 16),
			Color:  render.RGB(10, 200, 30),
		})
	})
	got := dst.At(8, 8).(color.RGBA)
	t.Logf("opaque fill = %v", got)
	if abs8(got.R, 10) > 1 || abs8(got.G, 200) > 1 || abs8(got.B, 30) > 1 || got.A != 255 {
		t.Fatalf("got %v, want {10 200 30 255}", got)
	}
}

// TestFillRectIsExactlyBounded checks the geometry: the pixel just outside the
// rectangle must be untouched, and the pixel just inside must be the fill.
func TestFillRectIsExactlyBounded(t *testing.T) {
	dst := drawList(t, 16, 16, color.RGBA{0, 0, 0, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(4, 4, 12, 12),
			Color:  render.RGB(255, 255, 255),
		})
	})
	if got := dst.At(3, 8).(color.RGBA); got.R != 0 {
		t.Errorf("pixel left of the rectangle = %v, want black", got)
	}
	if got := dst.At(4, 8).(color.RGBA); got.R != 255 {
		t.Errorf("first pixel of the rectangle = %v, want white", got)
	}
	if got := dst.At(11, 8).(color.RGBA); got.R != 255 {
		t.Errorf("last pixel of the rectangle = %v, want white", got)
	}
	if got := dst.At(12, 8).(color.RGBA); got.R != 0 {
		t.Errorf("pixel right of the rectangle = %v, want black", got)
	}
}

// TestRoundRectCornerIsCut checks the distance field: the centre is filled,
// the corner is not, and the edge midpoint still is.
func TestRoundRectCornerIsCut(t *testing.T) {
	dst := drawList(t, 32, 32, color.RGBA{0, 0, 0, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:         render.OpFillRoundRect,
			Bounds:       geom.Rc(0, 0, 32, 32),
			Color:        render.RGB(255, 255, 255),
			CornerRadius: 12,
		})
	})
	if got := dst.At(16, 16).(color.RGBA); got.R < 250 {
		t.Errorf("centre = %v, want white", got)
	}
	if got := dst.At(0, 0).(color.RGBA); got.R > 5 {
		t.Errorf("corner = %v, want the rounding to have cut it away", got)
	}
	if got := dst.At(16, 1).(color.RGBA); got.R < 250 {
		t.Errorf("top edge midpoint = %v, want white", got)
	}
	// The antialiasing padding must not leak outside the bounds either; a
	// round rect that filled its padded quad would be a square.
	if got := dst.At(1, 1).(color.RGBA); got.R > 60 {
		t.Errorf("near corner = %v, want it mostly outside the shape", got)
	}
}

// TestStrokeIsInsideTheBounds pins the rule of the project plan, section 8:
// the stroke lies inside, so the centre stays empty and the edge is drawn.
func TestStrokeIsInsideTheBounds(t *testing.T) {
	dst := drawList(t, 32, 32, color.RGBA{0, 0, 0, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:        render.OpStrokeRoundRect,
			Bounds:      geom.Rc(0, 0, 32, 32),
			Color:       render.RGB(255, 255, 255),
			StrokeWidth: 4,
		})
	})
	if got := dst.At(16, 16).(color.RGBA); got.R > 5 {
		t.Errorf("centre = %v, want the stroke not to fill", got)
	}
	if got := dst.At(16, 1).(color.RGBA); got.R < 250 {
		t.Errorf("inside the top stroke = %v, want white", got)
	}
	if got := dst.At(16, 6).(color.RGBA); got.R > 5 {
		t.Errorf("below the stroke = %v, want black", got)
	}
}

// TestClipIsHonouredOnTheGPU verifies that the geometric clip actually cuts
// pixels, not just vertices.
func TestClipIsHonouredOnTheGPU(t *testing.T) {
	dst := drawList(t, 32, 32, color.RGBA{0, 0, 0, 255}, func(l *render.List) {
		c := l.PushClip(geom.Rc(0, 0, 16, 32))
		l.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(0, 0, 32, 32),
			Color:  render.RGB(255, 255, 255),
			Clip:   c,
		})
		l.PopClip()
	})
	if got := dst.At(8, 16).(color.RGBA); got.R != 255 {
		t.Errorf("inside the clip = %v, want white", got)
	}
	if got := dst.At(24, 16).(color.RGBA); got.R != 0 {
		t.Errorf("outside the clip = %v, want black", got)
	}
}

// TestSubmitAllocationsWithARealTarget reports what the real draw call costs.
// It is not a hard assertion: allocations inside Ebitengine are explicitly
// outside gift's contract; see the project plan, section 11.
func TestSubmitAllocationsWithARealTarget(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	dst := eb.NewImage(256, 256)
	var l render.List
	l.Reset()
	for i := 0; i < 200; i++ {
		f := float32(i % 200)
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: geom.Rc(f, f, f+20, f+20), Color: render.RGB(1, 2, 3), CornerRadius: 4})
	}
	frame := func() {
		r.SetTarget(dst)
		r.BeginFrame(geom.Sz(256, 256))
		r.Submit(&l)
		r.EndFrame()
	}
	for i := 0; i < 10; i++ {
		frame()
	}
	t.Logf("allocations per frame including Ebitengine's own: %v", testing.AllocsPerRun(20, frame))
}

func abs8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
