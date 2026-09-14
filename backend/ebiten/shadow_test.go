package ebiten

import (
	"testing"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// shadowOp is a blurred black shadow of r.
func shadowOp(r geom.Rect, blur, radius float32) render.Op {
	return render.Op{
		Kind: render.OpShadow, Bounds: r, Blur: blur, CornerRadius: radius,
		Color: render.RGBA(0, 0, 0, 70),
	}
}

// TestShadowQuadGrowsByThreeSigma is the geometric half of the analytic
// shadow: the operation carries the shape, and the backend grows the quad so
// that the falloff has somewhere to land.
//
// A shadow is the only kind whose emitted geometry is bigger than its bounds
// by more than the one pixel antialiasing pad, so this is also the check that
// the pad rule did not get applied twice.
func TestShadowQuadGrowsByThreeSigma(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(shadowOp(geom.Rc(100, 100, 200, 150), 16, 0))

	r.BeginFrame(geom.Sz(400, 400))
	r.Submit(&l)
	r.EndFrame()

	if len(c.verts) != 4 {
		t.Fatalf("got %d vertices, want 4", len(c.verts))
	}
	// sigma 8, three sigma is 24.
	want := [4][2]float32{{76, 76}, {224, 76}, {224, 174}, {76, 174}}
	for i, v := range c.verts {
		if v.DstX != want[i][0] || v.DstY != want[i][1] {
			t.Errorf("vertex %d at (%v, %v), want (%v, %v)", i, v.DstX, v.DstY, want[i][0], want[i][1])
		}
	}
	// The half extents are those of the shape, not of the padded quad, and the
	// stroke slot carries the negated sigma. That encoding is what lets a
	// shadow share the shape shader and therefore the batch; see shape.kage.
	v := c.verts[0]
	if v.Custom0 != 50 || v.Custom1 != 25 {
		t.Errorf("half extents (%v, %v), want (50, 25)", v.Custom0, v.Custom1)
	}
	if v.Custom3 != -8 {
		t.Errorf("stroke slot = %v, want -8, the negated sigma", v.Custom3)
	}
	// The local position is measured from the shape origin, so the padded
	// corner is negative and the shader's p = src0Pos - half lands outside the
	// box, which is where the falloff is.
	if v.SrcX != -24 || v.SrcY != -24 {
		t.Errorf("local position (%v, %v), want (-24, -24)", v.SrcX, v.SrcY)
	}
}

// TestShadowIsScaledToDevicePixels. The shape shader has worked in device
// pixels since WU-E, and sigma has to follow, or a shadow would change width
// with the window scale while the box it belongs to did not.
func TestShadowIsScaledToDevicePixels(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	xf := l.PushXform(geom.Scale(2, 2))
	op := shadowOp(geom.Rc(10, 10, 30, 20), 8, 4)
	op.Xform = xf
	l.Add(op)

	r.BeginFrame(geom.Sz(400, 400))
	r.Submit(&l)
	r.EndFrame()

	v := c.verts[0]
	if v.Custom0 != 20 || v.Custom1 != 10 {
		t.Errorf("half extents (%v, %v), want (20, 10) device pixels", v.Custom0, v.Custom1)
	}
	if v.Custom2 != 8 {
		t.Errorf("radius = %v, want 8 device pixels", v.Custom2)
	}
	if v.Custom3 != -8 {
		t.Errorf("stroke slot = %v, want -8: sigma 4 local is 8 device pixels", v.Custom3)
	}
	// Three device sigma is 24 device pixels, which is 12 local, and the
	// transform doubles it again on the way to the screen.
	if got, want := v.DstX, float32(2*(10-12)); got != want {
		t.Errorf("padded corner at %v, want %v", got, want)
	}
}

// TestUnblurredShadowIsAPlainRoundedFill. A spread with no blur is a
// legitimate shadow, and it must not take the falloff branch of the shader.
func TestUnblurredShadowIsAPlainRoundedFill(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(shadowOp(geom.Rc(0, 0, 40, 20), 0, 6))

	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()

	if got := c.verts[0].Custom3; got != 0 {
		t.Errorf("stroke slot = %v, want 0; a negative value would take the shadow branch", got)
	}
	s := r.Stats()
	if s.ShadowOps != 1 || s.ShadowSharpOps != 1 {
		t.Errorf("ShadowOps/ShadowSharpOps = %d/%d, want 1/1", s.ShadowOps, s.ShadowSharpOps)
	}
}

// TestShadowsShareTheShapeBatch is the draw call claim of this work unit.
//
// A shadow is not a third material. It is the shape shader with a sign bit, so
// a screen full of shadowed, filled, stroked panels is still one draw call —
// and the project plan, section 11, which forbids reordering transparent
// content to merge materials, therefore costs nothing here.
func TestShadowsShareTheShapeBatch(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	for i := range 20 {
		f := float32(i) * 30
		b := geom.Rc(f, f, f+120, f+60)
		l.Add(shadowOp(b, 16, 12))
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: b, CornerRadius: 12, Color: render.RGB(255, 255, 255)})
		l.Add(render.Op{Kind: render.OpStrokeRoundRect, Bounds: b, CornerRadius: 12, StrokeWidth: 1, Color: render.RGB(0, 0, 0)})
	}
	r.BeginFrame(geom.Sz(1920, 1080))
	r.Submit(&l)
	r.EndFrame()

	if c.batches != 1 {
		t.Fatalf("20 shadowed panels produced %d draw calls, want 1: %v", c.batches, c.mats)
	}
	s := r.Stats()
	if s.Ops != 60 || s.ShadowOps != 20 || s.ShadowSharpOps != 0 {
		t.Errorf("Ops=%d ShadowOps=%d sharp=%d, want 60, 20, 0", s.Ops, s.ShadowOps, s.ShadowSharpOps)
	}
	if s.Accounted() != 60 {
		t.Errorf("Accounted = %d, want 60; the shadow kind broke the total accounting", s.Accounted())
	}
}

// TestShadowIsCutByItsClip: a clip is a rectangle intersection in device space
// and applies to the grown quad, not to the shape. "Eltern-Clips gelten auch
// fuer den Schatten", project plan, section 8.
func TestShadowIsCutByItsClip(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	clip := l.PushClip(geom.Rc(0, 0, 150, 400))
	op := shadowOp(geom.Rc(100, 100, 200, 150), 16, 0)
	op.Clip = clip
	l.Add(op)
	l.PopClip()

	r.BeginFrame(geom.Sz(400, 400))
	r.Submit(&l)
	r.EndFrame()

	for i, v := range c.verts {
		if v.DstX > 150 {
			t.Errorf("vertex %d at x=%v, beyond the clip at 150", i, v.DstX)
		}
	}
	// Entirely outside is entirely gone, and counted as such.
	r2, c2 := newHeadlessRenderer(t)
	var l2 render.List
	l2.Reset()
	cl := l2.PushClip(geom.Rc(0, 0, 10, 10))
	op2 := shadowOp(geom.Rc(100, 100, 200, 150), 16, 0)
	op2.Clip = cl
	l2.Add(op2)
	l2.PopClip()
	r2.BeginFrame(geom.Sz(400, 400))
	r2.Submit(&l2)
	r2.EndFrame()
	if c2.batches != 0 {
		t.Errorf("a fully clipped shadow produced %d draw calls, want 0", c2.batches)
	}
	if got := r2.Stats().SkippedOutsideClip; got != 1 {
		t.Errorf("SkippedOutsideClip = %d, want 1", got)
	}
}

// TestTransparentShadowIsSkipped keeps the accounting total for the new kind.
func TestTransparentShadowIsSkipped(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpShadow, Bounds: geom.Rc(0, 0, 10, 10), Blur: 8})
	r.BeginFrame(geom.Sz(100, 100))
	r.Submit(&l)
	r.EndFrame()
	s := r.Stats()
	if s.SkippedTransparent != 1 || s.ShadowOps != 0 || s.Accounted() != 1 {
		t.Errorf("transparent shadow: skipped=%d shadowOps=%d accounted=%d, want 1, 0, 1",
			s.SkippedTransparent, s.ShadowOps, s.Accounted())
	}
}

// TestShadowSubmitDoesNotAllocate is the frame path contract of the project
// plan, section 11, for the one kind this work unit added.
//
// The number that matters is not merely zero but *why* it is zero: there is no
// mask, no staging buffer and no image, so there is nothing in the shadow path
// that could allocate on a cache miss, because there is no cache and therefore
// no miss. The first frame costs the same as the thousandth.
func TestShadowSubmitDoesNotAllocate(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	for i := range 20 {
		f := float32(i) * 30
		b := geom.Rc(f, f, f+120, f+60)
		l.Add(shadowOp(b, 16, 12))
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: b, CornerRadius: 12, Color: render.RGB(255, 255, 255)})
	}
	submitOnce := func() {
		r.BeginFrame(geom.Sz(1920, 1080))
		r.Submit(&l)
		r.EndFrame()
	}
	submitOnce() // grow the vertex buffers
	if n := testing.AllocsPerRun(200, submitOnce); n != 0 {
		t.Errorf("submitting 20 shadows allocated %v times per frame, want 0", n)
	}
}

// BenchmarkTwentyShadows is the scenario of the project plan, section 13:
// "Shadow, 20 sichtbare Instanzen | Zusatzkosten < 0,5 ms je Frame nach
// Cache-Aufwaermung".
//
// Two honest caveats, both of which the plan itself insists on.
//
// First, this machine is not a Raspberry Pi 4, which is what the threshold is
// defined against, so the number below is an upper bound on the CPU side of
// the cost and says nothing at all about the Pi.
//
// Second, and more importantly, this measures the CPU work of turning twenty
// shadows into vertices — not the fill rate of drawing them. The project plan,
// section 11, is explicit that CPU time at the draw call is not a GPU time
// measurement, and for an analytic shadow the GPU side is the whole cost: 20
// shadows of a 300x200 panel with blur 16 cover about 20 * 348 * 248 = 1.7
// MPixel, which is the number that has to be paid on a VideoCore VI and which
// only the target hardware can answer. The "Cache-Aufwaermung" in the plan's
// wording has no counterpart here, because there is no cache; the first frame
// and the warm frame are the same frame.
func BenchmarkTwentyShadows(b *testing.B) {
	r, _ := newHeadlessRenderer(b)
	var l render.List
	l.Reset()
	for i := range 20 {
		x := float32(i%5) * 380
		y := float32(i/5) * 260
		rc := geom.Rc(x+20, y+20, x+320, y+220)
		l.Add(shadowOp(rc, 16, 12))
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: rc, CornerRadius: 12, Color: render.RGB(255, 255, 255)})
	}
	r.BeginFrame(geom.Sz(1920, 1080))
	r.Submit(&l)
	r.EndFrame()

	b.ReportAllocs()
	for b.Loop() {
		r.BeginFrame(geom.Sz(1920, 1080))
		r.Submit(&l)
		r.EndFrame()
	}
}

// BenchmarkTwentyShadowsWithoutShadows is the control: the same scene with the
// shadow operations removed. The difference between the two is the additional
// per frame CPU cost the plan's threshold is about.
func BenchmarkTwentyShadowsWithoutShadows(b *testing.B) {
	r, _ := newHeadlessRenderer(b)
	var l render.List
	l.Reset()
	for i := range 20 {
		x := float32(i%5) * 380
		y := float32(i/5) * 260
		rc := geom.Rc(x+20, y+20, x+320, y+220)
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: rc, CornerRadius: 12, Color: render.RGB(255, 255, 255)})
	}
	r.BeginFrame(geom.Sz(1920, 1080))
	r.Submit(&l)
	r.EndFrame()

	b.ReportAllocs()
	for b.Loop() {
		r.BeginFrame(geom.Sz(1920, 1080))
		r.Submit(&l)
		r.EndFrame()
	}
}

// TestRealisticMixedSceneDrawCalls reports the draw call count of a scene that
// mixes all three things gift can draw: shapes, text and shadows.
//
// The number is the point of the shadow encoding. Shadows share the shape
// material, so they cost no extra batch at all; the batches that do exist are
// the text runs, one per run in display list order, exactly as before this
// work unit. The project plan, section 11, forbids merging them by reordering.
func TestRealisticMixedSceneDrawCalls(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	// A window: a background, a toolbar, twelve shadowed cards each with a
	// title, and a shadowed floating panel over them.
	addRect(&l, geom.Rc(0, 0, 800, 600), render.RGB(18, 20, 26))
	l.Add(shadowOp(geom.Rc(0, 0, 800, 44), 12, 0))
	addRect(&l, geom.Rc(0, 0, 800, 44), render.RGB(38, 43, 56))
	addText(t, &l, "Library", 18, geom.Point{X: 16, Y: 30}, render.RGB(240, 240, 240))
	for i := range 12 {
		y := float32(60 + 42*i)
		card := geom.Rc(12, y, 788, y+36)
		l.Add(shadowOp(card, 16, 8))
		l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: card, CornerRadius: 8, Color: render.RGB(38, 43, 56)})
		addText(t, &l, "The quick brown fox", 14, geom.Point{X: 24, Y: y + 24}, render.RGB(240, 240, 240))
	}
	panel := geom.Rc(500, 400, 780, 560)
	l.Add(shadowOp(panel, 32, 16))
	l.Add(render.Op{Kind: render.OpFillRoundRect, Bounds: panel, CornerRadius: 16, Color: render.RGB(58, 63, 76)})

	r.BeginFrame(geom.Sz(800, 600))
	r.Submit(&l)
	r.EndFrame()

	s := r.Stats()
	t.Logf("realistic mixed scene: %d ops, %d draw calls (%d shape, %d glyph), %d shadows, %d glyph quads",
		s.Ops, s.Batches, s.ShapeBatches, s.GlyphBatches, s.ShadowOps, s.GlyphQuads)

	// 13 text runs, each of which ends a shape run and starts a new one:
	// shape, text, shape, text, ... which is 14 shape batches and 13 glyph
	// batches. The assertion is on the shape side, because that is the one the
	// shadows could have changed.
	if got, want := s.ShapeBatches, uint64(14); got != want {
		t.Errorf("ShapeBatches = %d, want %d; a shadow started a batch of its own", got, want)
	}
	if s.ShadowOps != 14 {
		t.Errorf("ShadowOps = %d, want 14", s.ShadowOps)
	}
}
