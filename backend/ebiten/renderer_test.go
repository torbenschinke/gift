package ebiten

import (
	"math"
	"strings"
	"testing"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// newHeadlessRenderer returns a renderer whose draw call is captured instead
// of issued, so the whole translation path can be tested without a window.
func newHeadlessRenderer(t *testing.T) (*Renderer, *capture) {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	c := &capture{}
	r.drawFn = c.draw
	return r, c
}

type capture struct {
	verts   []eb.Vertex
	idx     []uint32
	batches int
}

func (c *capture) draw(verts []eb.Vertex, idx []uint32) {
	c.batches++
	c.verts = append(c.verts[:0], verts...)
	c.idx = append(c.idx[:0], idx...)
}

func (c *capture) reset() { c.verts, c.idx, c.batches = c.verts[:0], c.idx[:0], 0 }

// TestShaderCompiles checks the Kage source. ebiten.NewShader needs no GPU, so
// a syntax error in the shader is caught by the ordinary headless test run.
func TestShaderCompiles(t *testing.T) {
	if _, err := eb.NewShader(ShapeShaderSource()); err != nil {
		t.Fatalf("shape shader does not compile: %v", err)
	}
}

// TestShaderUsesNoScreenSpaceDerivatives guards the decision of WU-E at the
// source level.
//
// ebiten.NewShader compiles Kage on the CPU, but the translation to the
// platform's shading language happens in the driver, so a shader that uses
// dfdx or dfdy compiles here and then fails **at draw time** on a GL ES 1.00
// context without OES_standard_derivatives — a Raspberry Pi 4 with Mesa, for
// instance. No headless test can catch that, so the only thing that can be
// asserted without the hardware is that the construct is absent. See
// shape.kage and the project plan, section 12, step 2.
func TestShaderUsesNoScreenSpaceDerivatives(t *testing.T) {
	src := shaderCode(string(ShapeShaderSource()))
	for _, fn := range []string{"fwidth", "dfdx", "dfdy"} {
		if strings.Contains(src, fn) {
			t.Errorf("the shape shader uses %s; that requires OES_standard_derivatives on GL ES 1.00", fn)
		}
	}
	// The same reasoning applies to constructs GLSL ES 1.00 restricts.
	// Dynamic indexing and non-constant loop bounds are version gated there
	// and would also fail only in the driver.
	for _, fn := range []string{"for ", "discard", "texture", "imageSrc"} {
		if strings.Contains(src, fn) {
			t.Errorf("the shape shader uses %q, which is not needed and is a portability risk", fn)
		}
	}
}

// shaderCode strips the line comments from a Kage source so that a guard can
// look at what the shader does rather than at what it says about itself. The
// shader's own documentation names the functions it avoids, which is the
// point of it.
func shaderCode(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestFillRectGeometry(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(10, 20, 110, 70), Color: render.RGB(255, 0, 0)})

	r.BeginFrame(geom.Sz(200, 200))
	r.Submit(&l)
	r.EndFrame()

	if len(c.verts) != 4 || len(c.idx) != 6 {
		t.Fatalf("got %d vertices and %d indices, want 4 and 6", len(c.verts), len(c.idx))
	}
	// A plain fill is not padded: the quad is the rectangle.
	wantDst := [4][2]float32{{10, 20}, {110, 20}, {110, 70}, {10, 70}}
	wantSrc := [4][2]float32{{0, 0}, {100, 0}, {100, 50}, {0, 50}}
	for i, v := range c.verts {
		if v.DstX != wantDst[i][0] || v.DstY != wantDst[i][1] {
			t.Errorf("vertex %d dst = (%v, %v), want %v", i, v.DstX, v.DstY, wantDst[i])
		}
		if v.SrcX != wantSrc[i][0] || v.SrcY != wantSrc[i][1] {
			t.Errorf("vertex %d src = (%v, %v), want %v", i, v.SrcX, v.SrcY, wantSrc[i])
		}
		if v.Custom0 != 50 || v.Custom1 != 25 || v.Custom2 != 0 || v.Custom3 != 0 {
			t.Errorf("vertex %d shape params = %v %v %v %v, want 50 25 0 0",
				i, v.Custom0, v.Custom1, v.Custom2, v.Custom3)
		}
	}
}

// TestColorIsCopiedPremultiplied pins the convention at the boundary: both
// sides are premultiplied, so no conversion may happen here.
func TestColorIsCopiedPremultiplied(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	col := render.RGBA(255, 0, 0, 128) // half transparent red
	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: col})

	r.BeginFrame(geom.Sz(10, 10))
	r.Submit(&l)
	r.EndFrame()

	v := c.verts[0]
	if v.ColorR != col.R || v.ColorG != col.G || v.ColorB != col.B || v.ColorA != col.A {
		t.Fatalf("vertex colour = %v %v %v %v, want %v", v.ColorR, v.ColorG, v.ColorB, v.ColorA, col)
	}
	// Sanity: premultiplied red at 50 % alpha has R == A, not R == 1.
	if math.Abs(float64(col.R-col.A)) > 1e-6 {
		t.Fatalf("render.RGBA is not premultiplied: R=%v A=%v", col.R, col.A)
	}
}

func TestRoundRectIsPaddedForAntialiasing(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: geom.Rc(0, 0, 100, 100), Color: render.RGB(1, 2, 3), CornerRadius: 10})

	r.BeginFrame(geom.Sz(200, 200))
	r.Submit(&l)
	r.EndFrame()

	if c.verts[0].DstX != -aaPad || c.verts[0].DstY != -aaPad {
		t.Fatalf("top left vertex = (%v, %v), want the bounds grown by %v", c.verts[0].DstX, c.verts[0].DstY, aaPad)
	}
	if c.verts[0].SrcX != -aaPad {
		t.Fatalf("local coordinate = %v, want %v: the distance field origin is the unpadded bounds", c.verts[0].SrcX, -aaPad)
	}
	if c.verts[0].Custom2 != 10 {
		t.Fatalf("radius = %v, want 10", c.verts[0].Custom2)
	}
}

func TestRadiusAndStrokeAreClamped(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(render.Op{
		Kind: render.OpStrokeRoundRect, Bounds: geom.Rc(0, 0, 40, 20),
		Color: render.RGB(255, 255, 255), CornerRadius: 999, StrokeWidth: 999,
	})
	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	// Half of the smaller edge is 10.
	if c.verts[0].Custom2 != 10 {
		t.Errorf("radius = %v, want it clamped to 10", c.verts[0].Custom2)
	}
	if c.verts[0].Custom3 != 10 {
		t.Errorf("stroke = %v, want it clamped to 10", c.verts[0].Custom3)
	}
}

// TestInvisibleOpsAreSkipped also pins which reason each case is attributed
// to. One conflated Skipped counter is what let a collapsed container hide
// behind "the application asked for something invisible"; see [RendererStats].
func TestInvisibleOpsAreSkipped(t *testing.T) {
	cases := []struct {
		name   string
		op     render.Op
		reason func(RendererStats) uint64
	}{
		{"none",
			render.Op{Kind: render.OpNone, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)},
			func(s RendererStats) uint64 { return s.SkippedNone }},
		{"transparent",
			render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10)},
			func(s RendererStats) uint64 { return s.SkippedTransparent }},
		{"empty bounds",
			render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(10, 10, 10, 10), Color: render.RGB(255, 0, 0)},
			func(s RendererStats) uint64 { return s.SkippedEmptyBounds }},
		{"zero stroke",
			render.Op{Kind: render.OpStrokeRoundRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)},
			func(s RendererStats) uint64 { return s.SkippedZeroStroke }},
		{"unknown kind",
			render.Op{Kind: render.OpKind(200), Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)},
			func(s RendererStats) uint64 { return s.UnknownKinds }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newHeadlessRenderer(t)
			var l render.List
			l.Reset()
			l.Add(tc.op)
			r.BeginFrame(geom.Sz(100, 100))
			r.Submit(&l)
			r.EndFrame()
			if c.batches != 0 {
				t.Fatalf("issued %d draw calls, want none", c.batches)
			}
			s := r.Stats()
			if got := tc.reason(s); got != 1 {
				t.Errorf("the op was not attributed to its own reason: %+v", s)
			}
			if s.Accounted() != 1 {
				t.Errorf("Accounted() = %d, want the 1 submitted op: %+v", s.Accounted(), s)
			}
		})
	}
}

// TestSkipAccountingIsTotal is the property the OpNone hole violated:
// Ops + Skipped + UnknownKinds must equal the number of operations submitted,
// whatever the mix.
func TestSkipAccountingIsTotal(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	ops := []render.Op{
		{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)},
		{Kind: render.OpNone},
		{Kind: render.OpNone},
		{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10)},
		{Kind: render.OpFillRect, Bounds: geom.Rc(5, 5, 5, 5), Color: render.RGB(1, 2, 3)},
		{Kind: render.OpStrokeRoundRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(1, 2, 3)},
		{Kind: render.OpKind(99)},
	}
	clip := l.PushClip(geom.Rc(500, 500, 600, 600))
	ops = append(ops, render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(1, 2, 3), Clip: clip})
	for _, op := range ops {
		l.Add(op)
	}
	l.PopClip()

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	s := r.Stats()
	if got, want := s.Accounted(), uint64(len(l.Ops())); got != want {
		t.Fatalf("Accounted() = %d, want %d: %+v", got, want, s)
	}
	if s.SkippedNone != 2 {
		t.Errorf("SkippedNone = %d, want 2; OpNone used to be dropped without a counter", s.SkippedNone)
	}
	if s.SkippedOutsideClip != 1 {
		t.Errorf("SkippedOutsideClip = %d, want 1: %+v", s.SkippedOutsideClip, s)
	}
	if s.SkippedEmptyBounds != 1 {
		t.Errorf("SkippedEmptyBounds = %d, want 1: %+v", s.SkippedEmptyBounds, s)
	}
}

func TestClipTrimsGeometry(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	clip := l.PushClip(geom.Rc(20, 20, 60, 60))
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 100, 100), Color: render.RGB(255, 0, 0), Clip: clip})
	l.PopClip()

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	if len(c.verts) != 4 {
		t.Fatalf("got %d vertices, want 4", len(c.verts))
	}
	if c.verts[0].DstX != 20 || c.verts[0].DstY != 20 || c.verts[2].DstX != 60 || c.verts[2].DstY != 60 {
		t.Fatalf("geometry not clipped: %v .. %v", c.verts[0], c.verts[2])
	}
	// The local coordinates must follow, or the distance field would be
	// evaluated for the wrong part of the shape.
	if c.verts[0].SrcX != 20 || c.verts[2].SrcX != 60 {
		t.Fatalf("local coordinates not clipped along: %v %v", c.verts[0].SrcX, c.verts[2].SrcX)
	}
}

func TestEmptyClipEmitsNothing(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	a := l.PushClip(geom.Rc(0, 0, 10, 10))
	b := l.PushClip(geom.Rc(50, 50, 60, 60)) // disjoint, so the intersection is empty
	_ = a
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 100, 100), Color: render.RGB(255, 0, 0), Clip: b})
	l.PopClip()
	l.PopClip()

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()
	if c.batches != 0 {
		t.Fatalf("issued %d draw calls for an empty clip, want none", c.batches)
	}
}

func TestOpOutsideClipEmitsNothing(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	clip := l.PushClip(geom.Rc(0, 0, 10, 10))
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(50, 50, 60, 60), Color: render.RGB(255, 0, 0), Clip: clip})
	l.PopClip()

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()
	if c.batches != 0 {
		t.Fatalf("issued %d draw calls, want none", c.batches)
	}
}

func TestTranslationXform(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	x := l.PushXform(geom.Translate(geom.Point{X: 100, Y: 5}))
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0), Xform: x})

	r.BeginFrame(geom.Sz(200, 200))
	r.Submit(&l)
	r.EndFrame()

	if c.verts[0].DstX != 100 || c.verts[0].DstY != 5 {
		t.Fatalf("transform not applied: %v %v", c.verts[0].DstX, c.verts[0].DstY)
	}
	// The shape is still defined in local space, so the local coordinate is
	// unaffected by the transform.
	if c.verts[0].SrcX != 0 || c.verts[0].SrcY != 0 {
		t.Fatalf("local coordinate = (%v, %v), want the untransformed origin", c.verts[0].SrcX, c.verts[0].SrcY)
	}
}

func TestScaleXformWithClip(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	x := l.PushXform(geom.Scale(2, 2))
	clip := l.PushClip(geom.Rc(0, 0, 10, 10))
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 100, 100), Color: render.RGB(255, 0, 0), Clip: clip, Xform: x})
	l.PopClip()

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	// The clip is device space, so the visible device rectangle is 0..10.
	// The local rectangle behind it is 0..5, but the local coordinate is
	// emitted in device pixels — the scale is baked in so that the shader
	// needs no screen space derivative — so it reads 10 again.
	if c.verts[2].DstX != 10 || c.verts[2].SrcX != 10 {
		t.Fatalf("dst = %v, src = %v; want 10 and 10", c.verts[2].DstX, c.verts[2].SrcX)
	}
}

// TestScaleIsBakedIntoTheShapeAttributes pins the contract the derivative free
// shader depends on: every geometric attribute leaves the CPU in device
// pixels.
func TestScaleIsBakedIntoTheShapeAttributes(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	x := l.PushXform(geom.Scale(2, 2))
	l.Add(render.Op{
		Kind: render.OpStrokeRoundRect, Bounds: geom.Rc(0, 0, 40, 20),
		Color: render.RGB(255, 255, 255), CornerRadius: 4, StrokeWidth: 2, Xform: x,
	})

	r.BeginFrame(geom.Sz(200, 200))
	r.Submit(&l)
	r.EndFrame()

	v := c.verts[0]
	if v.Custom0 != 40 || v.Custom1 != 20 || v.Custom2 != 8 || v.Custom3 != 4 {
		t.Fatalf("attributes = (%v, %v, %v, %v), want (40, 20, 8, 4) in device pixels",
			v.Custom0, v.Custom1, v.Custom2, v.Custom3)
	}
	// The pad is one device pixel, so it is half a local unit under a scale
	// of two, and the local coordinate of the padded corner is -1 again.
	if v.DstX != -aaPad || v.SrcX != -aaPad {
		t.Fatalf("padded corner = (%v, %v), want (%v, %v)", v.DstX, v.SrcX, -aaPad, -aaPad)
	}
}

// TestNonUniformScaleClampsTheRadiusWithTheSmallerFactor documents the one
// approximation of the bake; see deviceScale.
func TestNonUniformScaleClampsTheRadiusWithTheSmallerFactor(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	x := l.PushXform(geom.Scale(4, 1))
	l.Add(render.Op{
		Kind: render.OpFillRoundRect, Bounds: geom.Rc(0, 0, 40, 20),
		Color: render.RGB(255, 255, 255), CornerRadius: 10, Xform: x,
	})

	r.BeginFrame(geom.Sz(400, 200))
	r.Submit(&l)
	r.EndFrame()

	v := c.verts[0]
	// Half extents 80 by 10; the radius must stay within the smaller of them
	// or the rounded box is not a valid shape any more.
	if v.Custom0 != 80 || v.Custom1 != 10 || v.Custom2 != 10 {
		t.Fatalf("attributes = (%v, %v, %v), want (80, 10, 10)", v.Custom0, v.Custom1, v.Custom2)
	}
}

// TestRotationLeavesLengthsAlone: a rotation changes no length, so nothing is
// scaled and the attributes are the plain local ones.
func TestRotationLeavesLengthsAlone(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	x := l.PushXform(geom.Rotate(math.Pi / 6))
	l.Add(render.Op{
		Kind: render.OpFillRoundRect, Bounds: geom.Rc(-20, -10, 20, 10),
		Color: render.RGB(255, 255, 255), CornerRadius: 5, Xform: x,
	})

	r.BeginFrame(geom.Sz(200, 200))
	r.Submit(&l)
	r.EndFrame()

	v := c.verts[0]
	const eps = 1e-5
	if absf(v.Custom0-20) > eps || absf(v.Custom1-10) > eps || absf(v.Custom2-5) > eps {
		t.Fatalf("attributes = (%v, %v, %v), want (20, 10, 5)", v.Custom0, v.Custom1, v.Custom2)
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestRotatedXformUsesPolygonClip exercises the general path. gift does not
// emit a rotation yet, so this is the only coverage it has; see the package
// documentation.
func TestRotatedXformUsesPolygonClip(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	x := l.PushXform(geom.Rotate(math.Pi / 4))
	clip := l.PushClip(geom.Rc(-100, 0, 100, 100))
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(-50, -50, 50, 50), Color: render.RGB(255, 0, 0), Clip: clip, Xform: x})
	l.PopClip()

	r.BeginFrame(geom.Sz(200, 200))
	r.Submit(&l)
	r.EndFrame()

	if len(c.verts) < 3 {
		t.Fatalf("got %d vertices, want at least a triangle", len(c.verts))
	}
	if len(c.idx) != 3*(len(c.verts)-2) {
		t.Fatalf("got %d indices for %d vertices, want a triangle fan", len(c.idx), len(c.verts))
	}
	for i, v := range c.verts {
		if v.DstY < -1e-3 {
			t.Errorf("vertex %d at y=%v is outside the clip", i, v.DstY)
		}
		// The local coordinates must stay inside the shape, or the clipping
		// did not interpolate them along with the position.
		if v.SrcX < -1e-3 || v.SrcX > 100+1e-3 || v.SrcY < -1e-3 || v.SrcY > 100+1e-3 {
			t.Errorf("vertex %d has local coordinate (%v, %v) outside 0..100", i, v.SrcX, v.SrcY)
		}
	}
}

// TestSubmitDoesNotRetainTheList is the guard for the lifetime rule of
// [render.Backend.Submit]. The list is recycled right after the call, so a
// backend that translated it lazily would draw the wrong frame.
func TestSubmitDoesNotRetainTheList(t *testing.T) {
	r, c := newHeadlessRenderer(t)

	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)})

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)

	// The producer recycles the list before the frame ends. Everything the
	// backend still needs must already be in its own memory.
	l.Reset()
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(900, 900, 910, 910), Color: render.RGB(0, 255, 0)})

	r.EndFrame()

	if len(c.verts) != 4 {
		t.Fatalf("got %d vertices, want 4", len(c.verts))
	}
	if c.verts[0].DstX != 0 || c.verts[0].DstY != 0 {
		t.Fatalf("the backend drew the recycled list: first vertex is (%v, %v), want (0, 0)",
			c.verts[0].DstX, c.verts[0].DstY)
	}
	if c.verts[0].ColorG != 0 {
		t.Fatalf("the backend read a colour from the recycled list")
	}
}

func TestFlushSplitsOversizedBatches(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var batches int
	r.drawFn = func([]eb.Vertex, []uint32) { batches++ }

	var l render.List
	l.Reset()
	for i := 0; i < maxBatchVertices/4+10; i++ {
		l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 4, 4), Color: render.RGB(255, 0, 0)})
	}
	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	if batches != 2 {
		t.Fatalf("got %d batches, want 2", batches)
	}
	if r.Stats().Ops != uint64(maxBatchVertices/4+10) {
		t.Fatalf("Stats().Ops = %d", r.Stats().Ops)
	}
}

func TestStatsCountSkips(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)})
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10)}) // transparent
	l.Add(render.Op{Kind: render.OpKind(9)})                                 // unknown

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	s := r.Stats()
	if s.Frames != 1 || s.Batches != 1 || s.Ops != 1 || s.Skipped() != 1 || s.UnknownKinds != 1 {
		t.Fatalf("Stats() = %+v", s)
	}
	if s.SkippedTransparent != 1 {
		t.Fatalf("the transparent op was attributed to the wrong reason: %+v", s)
	}
}

// TestSubmitDoesNotAllocate is go/no-go criterion 3 of the project plan,
// section 12, for the backend half of the frame path.
func TestSubmitDoesNotAllocate(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	r.drawFn = func([]eb.Vertex, []uint32) {}

	var l render.List
	l.Reset()
	clip := l.PushClip(geom.Rc(0, 0, 500, 500))
	x := l.PushXform(geom.Translate(geom.Point{X: 3, Y: 7}))
	for i := 0; i < 300; i++ {
		f := float32(i)
		l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(f, f, f+20, f+20), Color: render.RGB(10, 20, 30), Clip: clip})
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: geom.Rc(f, f, f+20, f+20), Color: render.RGB(10, 20, 30), CornerRadius: 4, Clip: clip, Xform: x})
		l.Add(render.Op{Kind: render.OpStrokeRoundRect, Bounds: geom.Rc(f, f, f+20, f+20), Color: render.RGB(10, 20, 30), CornerRadius: 4, StrokeWidth: 1, Clip: clip})
	}

	frame := func() {
		r.BeginFrame(geom.Sz(500, 500))
		r.Submit(&l)
		r.EndFrame()
	}
	for i := 0; i < 8; i++ { // warm up the vertex buffers
		frame()
	}
	if n := testing.AllocsPerRun(50, frame); n != 0 {
		t.Fatalf("Submit allocates %v times per frame over a warm list, want 0", n)
	}
}

// TestRotatedSubmitDoesNotAllocate covers the polygon clipping path, which
// uses fixed size scratch arrays for exactly this reason.
func TestRotatedSubmitDoesNotAllocate(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	r.drawFn = func([]eb.Vertex, []uint32) {}

	var l render.List
	l.Reset()
	x := l.PushXform(geom.Rotate(0.3))
	clip := l.PushClip(geom.Rc(0, 0, 200, 200))
	for i := 0; i < 200; i++ {
		f := float32(i)
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: geom.Rc(f, f, f+30, f+30), Color: render.RGB(1, 2, 3), CornerRadius: 5, Clip: clip, Xform: x})
	}

	frame := func() {
		r.BeginFrame(geom.Sz(200, 200))
		r.Submit(&l)
		r.EndFrame()
	}
	for i := 0; i < 8; i++ {
		frame()
	}
	if n := testing.AllocsPerRun(50, frame); n != 0 {
		t.Fatalf("the rotated path allocates %v times per frame, want 0", n)
	}
}

func TestBeginFrameTwicePanics(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	r.BeginFrame(geom.Sz(10, 10))
	defer func() {
		if recover() == nil {
			t.Fatal("BeginFrame without EndFrame did not panic")
		}
	}()
	r.BeginFrame(geom.Sz(10, 10))
}

func TestSubmitOutsideFramePanics(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Submit outside a frame did not panic")
		}
	}()
	var l render.List
	l.Reset()
	r.Submit(&l)
}

// TestNominalForIsThePlansNumber: the project plan, section 13, states the
// nominal interval of a sixty hertz display as 16.667 ms and the missed
// threshold as 17.167 ms. The bare quotient is 16.666666 ms, which would put a
// threshold in every report that does not match the one in the plan.
func TestNominalForIsThePlansNumber(t *testing.T) {
	if got := nominalFor(60); got != 16667*time.Microsecond {
		t.Fatalf("nominalFor(60) = %v, want 16.667ms", got)
	}
	if got := nominalFor(0); got != 16667*time.Microsecond {
		t.Fatalf("nominalFor(0) = %v, want the sixty hertz default", got)
	}
	if got := nominalFor(50); got != 20*time.Millisecond {
		t.Fatalf("nominalFor(50) = %v, want 20ms", got)
	}
}
