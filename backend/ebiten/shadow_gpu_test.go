//go:build giftgpu

package ebiten

import (
	"image/color"
	"math"
	"testing"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// These are the pixel level checks of the analytic shadow. They exist because
// everything else about the shadow is arithmetic that could be arithmetically
// wrong in a consistent way; only a rendered pixel says whether the shader
// branch is reached and whether it produces light.

// TestShadowAppearsAndFallsOff is the basic claim: there is a shadow, it is
// darkest at the shape and it fades with distance.
func TestShadowAppearsAndFallsOff(t *testing.T) {
	// A 64x64 target with an opaque black shadow shape in the middle and no
	// background over it, so the pixels are the shadow alone.
	dst := drawList(t, 64, 64, color.RGBA{255, 255, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:   render.OpShadow,
			Bounds: geom.Rc(24, 24, 40, 40),
			Blur:   16, // sigma 8, extent 24
			Color:  render.RGB(0, 0, 0),
		})
	})

	// Luminance walking straight up from the top edge of the shape at y=24,
	// on the vertical centre line. Higher is lighter, so this sequence has to
	// increase all the way out.
	ys := []int{23, 20, 16, 12, 8, 4, 1}
	var lum []int
	for _, y := range ys {
		lum = append(lum, int(dst.At(32, y).(color.RGBA).R))
	}
	t.Logf("luminance above the shape at y=%v: %v", ys, lum)

	if lum[0] > 200 {
		t.Errorf("the pixel just outside the shape's edge is %d, want it clearly darkened", lum[0])
	}
	for i := 1; i < len(lum); i++ {
		if lum[i] < lum[i-1] {
			t.Errorf("luminance went from %d down to %d at y=%d; a shadow must fade "+
				"monotonically with distance", lum[i-1], lum[i], ys[i])
		}
	}
	if lum[len(lum)-1] < 245 {
		t.Errorf("three sigma out the pixel is still %d, want nearly white; the falloff does not "+
			"reach zero inside the quad", lum[len(lum)-1])
	}
	// Inside the shape it is darker than anywhere outside it. Not *black*:
	// this shape is only two sigma wide, so the two edges of the Gaussian
	// overlap in the middle and the honest coverage there is about 0.84. A
	// test that demanded black here would be demanding a wrong blur.
	if got := int(dst.At(32, 32).(color.RGBA).R); got >= lum[0] {
		t.Errorf("the centre of the shape is %d and the pixel outside its edge %d; "+
			"the falloff is pointing the wrong way", got, lum[0])
	}
	// And the quad itself must not be visible as a square: the corner of the
	// padded quad is 24 pixels diagonally out and should be untouched.
	if got := dst.At(1, 1).(color.RGBA).R; got < 250 {
		t.Errorf("the corner of the padded quad is %d, want white; the shadow is filling its quad", got)
	}
}

// TestShadowMatchesTheGaussianItClaimsToBe checks the shader against the
// closed form, on the straight edge where the two are supposed to agree
// exactly.
//
// This is the check that a wrong constant in the logistic approximation, or a
// sigma that is really a blur diameter, cannot survive: at one sigma out the
// coverage of a half plane is 1-Phi(1) = 0.1587, at two 0.0228, and those are
// numbers, not shapes.
func TestShadowMatchesTheGaussianItClaimsToBe(t *testing.T) {
	// A wide, short shape so that the sampling column is far from both
	// corners and the half plane formula is the exact answer.
	dst := drawList(t, 96, 96, color.RGBA{255, 255, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind:   render.OpShadow,
			Bounds: geom.Rc(8, 48, 88, 90),
			Blur:   16, // sigma 8
			Color:  render.RGB(0, 0, 0),
		})
	})
	const sigma = 8.0
	for _, d := range []float64{4, 8, 12, 16, 20} {
		// Pixel centres: the boundary is at y=48, so the centre of the pixel
		// at row y sits at y+0.5 and its distance is 48-(y+0.5).
		y := int(48 - d)
		dist := 48 - (float64(y) + 0.5)
		// Coverage of a half plane at distance dist *outside* it.
		want := phi(-dist / sigma)
		got := 1 - float64(dst.At(48, y).(color.RGBA).R)/255
		t.Logf("d=%.1f sigma: coverage %.4f, Gaussian %.4f", dist/sigma, got, want)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("at %.2f sigma the coverage is %.4f, want %.4f (tolerance 0.01)", dist/sigma, got, want)
		}
	}
}

// phi is the exact normal cumulative distribution, via the error function, for
// the test to compare the shader's approximation against. It is deliberately
// not the same formula the shader uses.
func phi(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// TestShadowSitsBehindTheBackground is the drawing order of the project plan,
// section 8, verified in pixels rather than in display list indices: the
// shadow must not darken the opaque fill that comes after it.
func TestShadowSitsBehindTheBackground(t *testing.T) {
	box := geom.Rc(16, 16, 48, 48)
	dst := drawList(t, 64, 64, color.RGBA{255, 255, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind: render.OpShadow, Bounds: box, Blur: 16,
			Color: render.RGB(0, 0, 0),
		})
		l.Add(render.Op{Kind: render.OpFillRect, Bounds: box, Color: render.RGB(255, 0, 0)})
	})
	got := dst.At(32, 32).(color.RGBA)
	t.Logf("inside the box = %v", got)
	if got.R < 250 || got.G > 5 || got.B > 5 {
		t.Errorf("inside the box = %v, want pure red; the shadow is drawn over the background", got)
	}
	// Just outside it the shadow is visible, which is what makes the check
	// above mean something.
	if out := dst.At(32, 52).(color.RGBA); out.R > 240 {
		t.Errorf("just below the box = %v, want the shadow to show", out)
	}
}

// TestShadowIsPremultiplied. The shader scales the whole premultiplied vector
// by the coverage, so a half transparent shadow over an opaque background has
// to blend exactly like a fill of the same effective alpha.
//
// The comparison is against a plain fill rather than against a hand computed
// constant on purpose: what is being checked is that the shadow path does not
// break a convention the fill path is already known to honour.
func TestShadowIsPremultiplied(t *testing.T) {
	// Deep inside a shape much larger than sigma, coverage is 1 to within a
	// part in ten thousand, so the shadow reduces to its own colour.
	shadow := drawList(t, 64, 64, color.RGBA{0, 0, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind: render.OpShadow, Bounds: geom.Rc(8, 8, 56, 56), Blur: 8,
			Color: render.RGBA(255, 0, 0, 128),
		})
	}).At(32, 32).(color.RGBA)
	fill := drawList(t, 64, 64, color.RGBA{0, 0, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind: render.OpFillRect, Bounds: geom.Rc(8, 8, 56, 56),
			Color: render.RGBA(255, 0, 0, 128),
		})
	}).At(32, 32).(color.RGBA)

	t.Logf("shadow %v, equivalent fill %v", shadow, fill)
	const tol = 2
	if abs8(shadow.R, fill.R) > tol || abs8(shadow.G, fill.G) > tol ||
		abs8(shadow.B, fill.B) > tol || abs8(shadow.A, fill.A) > tol {
		t.Fatalf("a fully covered shadow blended to %v where the same colour as a fill blended to %v; "+
			"the shadow path is not premultiplied", shadow, fill)
	}
}

// TestShadowRoundedCornerIsRounded. The distance field is shared with the
// round rect path, so the only thing that could go wrong here is the corner
// radius not reaching the shadow — which is exactly what a spread adjusted
// radius makes easy to get wrong.
func TestShadowRoundedCornerIsRounded(t *testing.T) {
	dst := drawList(t, 64, 64, color.RGBA{255, 255, 255, 255}, func(l *render.List) {
		l.Add(render.Op{
			Kind: render.OpShadow, Bounds: geom.Rc(16, 16, 48, 48),
			CornerRadius: 16, Blur: 4, // sigma 2, a tight shadow
			Color: render.RGB(0, 0, 0),
		})
	})
	// With a radius of 16 on a 32x32 box the shape is a circle, so the corner
	// of the box is well outside it and the centre of an edge is inside.
	corner := dst.At(18, 18).(color.RGBA).R
	edge := dst.At(32, 18).(color.RGBA).R
	t.Logf("corner %d, edge midpoint %d", corner, edge)
	if !(corner > edge+40) {
		t.Errorf("corner %d is not clearly lighter than the edge midpoint %d; the corner radius "+
			"did not reach the shadow", corner, edge)
	}
}

// TestShadowClipCutsPixels is the "Eltern-Clips gelten auch fuer den Schatten"
// sentence of the project plan, section 8, checked where it actually matters.
func TestShadowClipCutsPixels(t *testing.T) {
	dst := drawList(t, 64, 64, color.RGBA{255, 255, 255, 255}, func(l *render.List) {
		c := l.PushClip(geom.Rc(0, 0, 64, 32))
		l.Add(render.Op{
			Kind: render.OpShadow, Bounds: geom.Rc(24, 24, 40, 40), Blur: 16,
			Color: render.RGB(0, 0, 0), Clip: c,
		})
		l.PopClip()
	})
	// Above the clip edge, inside the shape: the shadow is there.
	if got := dst.At(32, 30).(color.RGBA).R; got > 200 {
		t.Errorf("just above the clip edge the pixel is %d, want the shadow; the clip cut too much", got)
	}
	// Below it, both the part of the shadow that is inside the shape and the
	// part that is in the falloff are gone.
	for _, y := range []int{34, 44, 56} {
		if got := dst.At(32, y).(color.RGBA).R; got != 255 {
			t.Errorf("below the clip edge, y=%d, the pixel is %d, want untouched white", y, got)
		}
	}
}
