//go:build giftgpu

// These tests need a real graphics context: reading a pixel back requires
// Ebitengine's run loop, which requires a window. They are behind the build
// tag giftgpu so that `go test ./...` passes headless, as the project plan,
// section 12, criterion 4, requires.
//
//	go test -tags giftgpu ./backend/ebiten/
package ebiten

import (
	"image"
	"image/color"
	"os"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
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

// --- text -------------------------------------------------------------------

// textList appends a glyph run to l with its paragraph top left at (x, top),
// which is the same arithmetic ui.Text does: the glyph positions shapeGlyphs
// returns are already relative to the top of the paragraph.
func textList(t *testing.T, l *render.List, s string, size, x, top float32, col render.Color) {
	t.Helper()
	first := l.GlyphsLen()
	for _, g := range shapeGlyphs(t, s, size) {
		g.X += x
		g.Y += top
		l.AppendGlyph(g)
	}
	l.Add(render.Op{
		Kind:   render.OpGlyphs,
		Bounds: geom.Rc(x, top, x+200, top+2*size),
		Color:  col,
		Glyphs: first, GlyphCount: l.GlyphsLen() - first,
	})
}

// baselineOf is where the first baseline of a paragraph sits below its top.
func baselineOf(t *testing.T, size float32) float32 {
	t.Helper()
	return testFont(t).Metrics(size).FirstBaseline
}

// inkIn counts the pixels in r that differ from the background colour.
func inkIn(img *eb.Image, r image.Rectangle, bg color.RGBA) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.At(x, y).(color.RGBA) != bg {
				n++
			}
		}
	}
	return n
}

// TestGlyphsActuallyReachThePixels is the end to end proof for text: shape a
// known string, run it through the atlas and the second batch, and read the
// framebuffer back. Every layer between ui.Text and the screen is exercised
// except ui itself, which is tested headless.
func TestGlyphsActuallyReachThePixels(t *testing.T) {
	bg := color.RGBA{0, 0, 0, 255}
	const top = 6
	base := int(top + baselineOf(t, 32))
	dst := drawList(t, 160, 64, bg, func(l *render.List) {
		textList(t, l, "HELLO", 32, 8, top, render.RGB(255, 255, 255))
	})

	// The glyphs sit on the first baseline starting at x=8, so the ink is in
	// the band above that baseline and to the right of x=8.
	inside := inkIn(dst, image.Rect(8, top, 152, base+1), bg)
	if inside < 100 {
		t.Fatalf("only %d non background pixels where the text should be; nothing was drawn", inside)
	}

	// And nowhere else: not below the baseline, where these five capitals have
	// no descender, and not to the left of the origin.
	if n := inkIn(dst, image.Rect(0, 0, 8, 64), bg); n != 0 {
		t.Errorf("%d pixels of ink left of the text origin", n)
	}
	if n := inkIn(dst, image.Rect(0, base+2, 160, 64), bg); n != 0 {
		t.Errorf("%d pixels of ink below the baseline of a string with no descenders", n)
	}
	t.Logf("HELLO at 32 px: %d inked pixels in the expected band", inside)
}

// TestGlyphsAreNotDrawnAsOpaqueBoxes guards the one failure that would still
// pass a "some pixels changed" assertion: an atlas whose coverage was ignored
// would fill every glyph's bounding box solid.
func TestGlyphsAreNotDrawnAsOpaqueBoxes(t *testing.T) {
	bg := color.RGBA{0, 0, 0, 255}
	base := int(baselineOf(t, 48))
	dst := drawList(t, 64, 80, bg, func(l *render.List) {
		textList(t, l, "O", 48, 8, 0, render.RGB(255, 255, 255))
	})
	box := image.Rect(8, 2, 48, base+1)
	ink := inkIn(dst, box, bg)
	total := box.Dx() * box.Dy()
	if ink == 0 {
		t.Fatal("the glyph produced no ink at all")
	}
	if ink > total*3/4 {
		t.Fatalf("%d of %d pixels in the glyph box are inked; the coverage mask is being ignored", ink, total)
	}
	t.Logf("a 48 px O covers %d of %d pixels of its box", ink, total)
}

// TestTextPremultipliedAlphaOverABackground is the colour convention for text.
//
// render.Color is premultiplied, the atlas holds premultiplied white coverage,
// and the vertex colour scale is declared premultiplied. Fully covered pixels
// of a half transparent red glyph over an opaque blue background must
// therefore come out at src + dst*(1-src.a) = (0.5, 0, 0.5, 1), exactly as for
// a rectangle. Anything else means a straight alpha value is being multiplied
// somewhere.
func TestTextPremultipliedAlphaOverABackground(t *testing.T) {
	bg := color.RGBA{0, 0, 255, 255}
	dst := drawList(t, 128, 80, bg, func(l *render.List) {
		textList(t, l, "MM", 48, 4, 4, render.RGBA(255, 0, 0, 128))
	})

	// Find the pixel with the most red: the interior of a stem is fully
	// covered, so it shows the blend at coverage 1.
	var best color.RGBA
	for y := 4; y < 60; y++ {
		for x := 4; x < 120; x++ {
			c := dst.At(x, y).(color.RGBA)
			if c.R > best.R {
				best = c
			}
		}
	}
	t.Logf("most covered pixel of 50%% red text over opaque blue = %v", best)
	if best.R < 100 {
		t.Fatalf("no pixel reached full coverage: %v; the text did not draw", best)
	}
	want := color.RGBA{128, 0, 127, 255}
	const tol = 3
	if abs8(best.R, want.R) > tol || abs8(best.G, want.G) > tol ||
		abs8(best.B, want.B) > tol || abs8(best.A, want.A) > tol {
		t.Fatalf("got %v, want about %v; premultiplied alpha is broken on the text path", best, want)
	}

	// The uncovered background is untouched, which is the other half: a glyph
	// quad must not tint its own empty corners.
	if c := dst.At(126, 78).(color.RGBA); c != bg {
		t.Errorf("background outside the text is %v, want %v", c, bg)
	}
}

// TestMixedSceneKeepsDisplayListOrderOnScreen is the visible form of the
// interleaving rule: a shape declared *after* text covers it. If the backend
// sorted text into its own pass this would come out the other way round, and
// only in scenes where two things overlap.
func TestMixedSceneKeepsDisplayListOrderOnScreen(t *testing.T) {
	bg := color.RGBA{0, 0, 0, 255}
	dst := drawList(t, 64, 64, bg, func(l *render.List) {
		textList(t, l, "WW", 40, 2, 4, render.RGB(255, 255, 255))
		l.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(0, 0, 64, 64),
			Color:  render.RGB(0, 255, 0),
		})
	})
	c := dst.At(32, 32).(color.RGBA)
	if c.G != 255 || c.R != 0 {
		t.Fatalf("the rectangle declared after the text did not cover it: %v", c)
	}
}
