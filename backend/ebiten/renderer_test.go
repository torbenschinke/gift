package ebiten

import (
	"math"
	"testing"

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

func TestInvisibleOpsAreSkipped(t *testing.T) {
	cases := []struct {
		name string
		op   render.Op
	}{
		{"none", render.Op{Kind: render.OpNone, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)}},
		{"transparent", render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 10, 10)}},
		{"empty bounds", render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(10, 10, 10, 10), Color: render.RGB(255, 0, 0)}},
		{"zero stroke", render.Op{Kind: render.OpStrokeRoundRect, Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)}},
		{"unknown kind", render.Op{Kind: render.OpKind(200), Bounds: geom.Rc(0, 0, 10, 10), Color: render.RGB(255, 0, 0)}},
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
		})
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

	// The clip is device space, so the visible device rectangle is 0..10 and
	// the local rectangle behind it is 0..5.
	if c.verts[2].DstX != 10 || c.verts[2].SrcX != 5 {
		t.Fatalf("dst = %v, src = %v; want 10 and 5", c.verts[2].DstX, c.verts[2].SrcX)
	}
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
	if s.Frames != 1 || s.Batches != 1 || s.Ops != 1 || s.Skipped != 1 || s.UnknownKinds != 1 {
		t.Fatalf("Stats() = %+v", s)
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
