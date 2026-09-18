//go:build giftgpu

package ebiten

import (
	"image/color"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// legacyShapeShaderSrc is the shape shader as it was before WU-E, deriving the
// coverage width from fwidth(d) with the shape parameters in local units.
//
// It exists only here, in a test file behind the giftgpu tag, and is not
// shipped. Its single purpose is to prove that removing the screen space
// derivative did not change what gift draws; see
// TestDerivativeFreeShaderMatchesTheDerivativeShader. Once the Raspberry Pi 4
// has confirmed the new shader in the field this file may be deleted, but not
// before: a claim of pixel equivalence that nothing checks is worth nothing.
const legacyShapeShaderSrc = `//kage:unit pixels

package main

func sdRoundBox(p vec2, b vec2, r float) float {
	q := abs(p) - b + vec2(r)
	return length(max(q, vec2(0))) + min(max(q.x, q.y), 0.0) - r
}

func Fragment(dstPos vec4, src0Pos vec2, color vec4, custom vec4) vec4 {
	half := custom.xy
	radius := custom.z
	stroke := custom.w

	if radius <= 0.0 {
		if stroke <= 0.0 {
			return color
		}
	}

	p := src0Pos - half
	d := sdRoundBox(p, half, radius)

	w := fwidth(d)
	if w <= 0.0 {
		w = 0.0001
	}

	if stroke > 0.0 {
		d = abs(d+stroke*0.5) - stroke*0.5
	}

	return color * clamp(0.5-d/w, 0.0, 1.0)
}
`

// renderBoth draws the same display list twice into two fresh images: once
// through the production path with the derivative free shader, and once with
// the legacy shader fed the vertex stream the legacy CPU path would have
// produced.
//
// The second stream is derived from the first by undoing the bake, which is
// exactly the inverse of what appendOp now does: divide the half extents, the
// radius, the stroke width and the local position by the scale factors again.
// For an identity or a translation the factors are 1 and the two streams are
// bit identical, so those cases compare the two shaders and nothing else.
func renderBoth(t *testing.T, w, h int, sx, sy float32, build func(l *render.List)) (fresh, legacy *eb.Image) {
	t.Helper()

	legacyShader, err := eb.NewShader([]byte(legacyShapeShaderSrc))
	if err != nil {
		t.Fatalf("compiling the legacy shader: %v", err)
	}

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	var l render.List
	l.Reset()
	build(&l)

	// Capture the baked stream.
	var verts []eb.Vertex
	var idx []uint32
	r.drawFn = func(_ Material, v []eb.Vertex, i []uint32) {
		verts = append(verts[:0], v...)
		idx = append(idx[:0], i...)
	}
	r.BeginFrame(geom.Sz(float32(w), float32(h)))
	r.Submit(&l)
	r.EndFrame()
	r.drawFn = nil

	if len(idx) == 0 {
		t.Fatal("the list produced no geometry")
	}

	fresh = eb.NewImage(w, h)
	fresh.Fill(color.RGBA{0, 0, 0, 255})
	fresh.DrawTrianglesShader32(verts, idx, r.shader, &r.opts)

	sr := sx
	if sy < sr {
		sr = sy
	}
	unbaked := make([]eb.Vertex, len(verts))
	for i, v := range verts {
		v.SrcX /= sx
		v.SrcY /= sy
		v.Custom0 /= sx
		v.Custom1 /= sy
		v.Custom2 /= sr
		v.Custom3 /= sr
		unbaked[i] = v
	}
	legacy = eb.NewImage(w, h)
	legacy.Fill(color.RGBA{0, 0, 0, 255})
	legacy.DrawTrianglesShader32(unbaked, idx, legacyShader, &r.opts)
	return fresh, legacy
}

// maxDiff returns the largest per channel absolute difference between a and b
// and where it occurs.
func maxDiff(a, b *eb.Image) (d int, at [2]int, ga, gb color.RGBA) {
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pa := a.At(x, y).(color.RGBA)
			pb := b.At(x, y).(color.RGBA)
			for _, c := range [4][2]uint8{
				{pa.R, pb.R}, {pa.G, pb.G}, {pa.B, pb.B}, {pa.A, pb.A},
			} {
				if v := abs8(c[0], c[1]); v > d {
					d, at, ga, gb = v, [2]int{x, y}, pa, pb
				}
			}
		}
	}
	return d, at, ga, gb
}

// TestDerivativeFreeShaderMatchesTheDerivativeShader is the verification WU-E
// exists for.
//
// # Why a tolerance at all
//
// The two shaders compute the same coverage by two routes. The old one divides
// the local distance by fwidth(d), which the GPU evaluates per 2x2 quad of
// fragments as a finite difference; the new one takes the distance already in
// device pixels and uses a band of exactly 1. On a straight edge these agree
// analytically, because |grad d| is 1 there. They disagree slightly in two
// places:
//
//   - Inside a rounded corner the gradient of the box SDF is still unit length
//     but the *finite difference* across a 2x2 quad is not, so fwidth returns
//     a little more than 1 where the field curves. The old shader was
//     therefore very slightly softer at the corners than the ideal.
//   - At the one fragment where the distance field has a kink — the diagonal
//     through a corner centre, and the centre line of a thin stroke — the
//     finite difference can be much larger than 1 and the old shader blurred
//     that fragment. The new one does not.
//
// Both are antialiasing differences confined to the coverage ramp, that is to
// a band about one pixel wide along the outline. The interior and the exterior
// are saturated in both and must be bit identical.
//
// # The tolerance
//
// The test asserts two different things, and the strong one is the second.
//
//   - Where both shaders call a pixel fully inside or fully outside, they must
//     agree bit for bit. Not "closely": exactly. A difference there would mean
//     the shape or the colour moved, which is the failure this work unit has
//     to rule out, and that check is what actually guards the geometry.
//   - Everywhere else, on the coverage ramp, at most 96 of 255 per channel.
//     The observed maximum over all cases below is 64, and it occurs only at a
//     kink of the distance field — a pixel on a corner diagonal or on the
//     centre line of a stroke band — where the old shader's fwidth is
//     unbounded and it blurred a pixel that is in truth fully covered. There
//     the new shader is the correct one, so the difference is an improvement
//     and not a regression; the headroom to 96 is there so that a different
//     driver's 2x2 quad layout does not turn a known-good improvement into a
//     red test. Anything past that would mean an edge moved rather than
//     changed hardness, and the saturated-pixel check would very likely have
//     fired first anyway.
func TestDerivativeFreeShaderMatchesTheDerivativeShader(t *testing.T) {
	const (
		w, h = 64, 64
		// See the doc comment.
		edgeTol = 96
	)

	white := render.RGB(255, 255, 255)

	cases := []struct {
		name   string
		sx, sy float32
		build  func(l *render.List)
	}{
		{"plain fill", 1, 1, func(l *render.List) {
			l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(8, 8, 56, 40), Color: white})
		}},
		{"rounded r=2", 1, 1, roundFill(white, geom.Rc(8, 8, 56, 40), 2)},
		{"rounded r=8", 1, 1, roundFill(white, geom.Rc(8, 8, 56, 40), 8)},
		{"rounded r=16 = half the short side", 1, 1, roundFill(white, geom.Rc(8, 8, 56, 40), 16)},
		{"rounded r=999 clamped past half", 1, 1, roundFill(white, geom.Rc(8, 8, 56, 40), 999)},
		{"stroke w=1", 1, 1, strokeOp(white, geom.Rc(8, 8, 56, 40), 6, 1)},
		{"stroke w=3", 1, 1, strokeOp(white, geom.Rc(8, 8, 56, 40), 6, 3)},
		{"stroke w=8", 1, 1, strokeOp(white, geom.Rc(8, 8, 56, 40), 6, 8)},
		{"stroke without radius", 1, 1, strokeOp(white, geom.Rc(8, 8, 56, 40), 0, 4)},
		{"tiny shape, radius dominates", 1, 1, roundFill(white, geom.Rc(28, 28, 33, 33), 999)},
		{"translated", 1, 1, xformed(geom.Translate(geom.Pt(7, 5)), roundFill(white, geom.Rc(4, 4, 40, 30), 6))},
		{"scaled 2x uniform", 2, 2, xformed(geom.Scale(2, 2), roundFill(white, geom.Rc(4, 4, 28, 20), 5))},
		{"scaled 0.5x uniform", 0.5, 0.5, xformed(geom.Scale(0.5, 0.5), roundFill(white, geom.Rc(8, 8, 112, 80), 20))},
		{"scaled 2x and translated", 2, 2, xformed(
			geom.Scale(2, 2).Mul(geom.Translate(geom.Pt(3, 3))),
			strokeOp(white, geom.Rc(2, 2, 26, 18), 4, 2))},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fresh, legacy := renderBoth(t, w, h, c.sx, c.sy, c.build)

			// The whole image, for the record.
			d, at, ga, gb := maxDiff(fresh, legacy)
			t.Logf("max difference %d at %v: new %v legacy %v", d, at, ga, gb)
			if d > edgeTol {
				t.Errorf("max per channel difference %d at %v exceeds %d: new %v, legacy %v",
					d, at, edgeTol, ga, gb)
			}

			// The saturated regions must agree exactly. A pixel counts as
			// saturated when both shaders call it fully inside or fully
			// outside; those are the pixels no coverage ramp touches.
			var interior, differing int
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					pa := fresh.At(x, y).(color.RGBA)
					pb := legacy.At(x, y).(color.RGBA)
					if !saturated(pa) || !saturated(pb) {
						continue
					}
					interior++
					if pa != pb {
						differing++
						if differing <= 5 {
							t.Errorf("saturated pixel (%d, %d): new %v, legacy %v", x, y, pa, pb)
						}
					}
				}
			}
			if interior < 16 {
				t.Fatalf("only %d saturated pixels; the case shades nothing and proves nothing", interior)
			}
			if differing != 0 {
				t.Errorf("%d of %d saturated pixels differ; the shape itself changed", differing, interior)
			}
		})
	}
}

// saturated reports whether p is fully the background or fully the fill, so
// that no antialiasing ramp can be responsible for it.
func saturated(p color.RGBA) bool {
	return p == color.RGBA{0, 0, 0, 255} || p == color.RGBA{255, 255, 255, 255}
}

func roundFill(c render.Color, b geom.Rect, r float32) func(*render.List) {
	return func(l *render.List) {
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: b, Color: c, CornerRadius: r})
	}
}

func strokeOp(c render.Color, b geom.Rect, r, w float32) func(*render.List) {
	return func(l *render.List) {
		l.Add(render.Op{Kind: render.OpStrokeRoundRect, Bounds: b, Color: c, CornerRadius: r, StrokeWidth: w})
	}
}

// xformed wraps a builder in a transform. The inner builder must add exactly
// one operation with the zero Xform, which every helper above does.
func xformed(m geom.Affine2D, inner func(*render.List)) func(*render.List) {
	return func(l *render.List) {
		x := l.PushXform(m)
		var tmp render.List
		tmp.Reset()
		inner(&tmp)
		for _, op := range tmp.Ops() {
			op.Xform = x
			l.Add(op)
		}
	}
}
