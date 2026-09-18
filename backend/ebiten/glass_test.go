package ebiten

import (
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// addGlass appends a material region to l and returns nothing; the caller
// asserts on the renderer counters.
func addGlass(l *render.List, r geom.Rect, radius float32, g render.Glass) {
	l.Add(render.Op{
		Kind:         render.OpMaterial,
		Bounds:       r,
		CornerRadius: radius,
		Material:     l.AddMaterial(g.Material()),
	})
}

// glassTrace installs a pass recorder and returns a function that reports the
// passes of the frame as a comma separated string.
func glassTrace(r *Renderer) func() string {
	var got []glassPass
	r.passFn = func(p glassPass) { got = append(got, p) }
	return func() string {
		var b strings.Builder
		for i, p := range got {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(p.String())
		}
		got = got[:0]
		return b.String()
	}
}

// sceneRenderer is a headless renderer with a destination, so that the scene
// target and the whole pass chain are reached. The draw calls are still
// captured rather than issued; see [Renderer.drawFn].
func sceneRenderer(t testing.TB, w, h int) (*Renderer, *capture) {
	t.Helper()
	r, c := newHeadlessRenderer(t)
	r.SetTarget(eb.NewImage(w, h))
	return r, c
}

// TestMaterialIsABatchingBarrier is the claim of [render.OpMaterial] stated as
// a draw call count.
//
// Everything before a material has to reach the target before its backdrop can
// be copied, so the shape run cannot continue across it. This is the cost the
// project plan, section 11, would otherwise forbid paying — it forbids
// *reordering* transparent content to merge batches, and a barrier is the
// opposite of that — and it is the number an application should know.
func TestMaterialIsABatchingBarrier(t *testing.T) {
	r, c := sceneRenderer(t, 400, 300)
	var l render.List
	l.Reset()
	for i := range 20 {
		f := float32(i)
		addRect(&l, geom.Rc(f, f, f+10, f+10), render.RGB(1, 2, 3))
	}
	addGlass(&l, geom.Rc(20, 20, 220, 120), 12, render.NewGlass().Quality(render.Reduced))
	for i := range 20 {
		f := float32(i) + 100
		addRect(&l, geom.Rc(f, f, f+10, f+10), render.RGB(3, 2, 1))
	}

	submit(t, r, c, &l)

	// shapes, copy, composite, shapes, and the scene to screen blit. Without
	// the material the same scene is one draw call.
	want := []Material{MaterialShape, MaterialGlass, MaterialGlass, MaterialShape, MaterialGlass}
	if len(c.mats) != len(want) {
		t.Fatalf("draw calls %v, want %v", c.mats, want)
	}
	for i := range want {
		if c.mats[i] != want[i] {
			t.Fatalf("draw calls %v, want %v", c.mats, want)
		}
	}

	// Without the material: one call, no scene target, no blit.
	r2, c2 := sceneRenderer(t, 400, 300)
	var l2 render.List
	l2.Reset()
	for i := range 40 {
		f := float32(i)
		addRect(&l2, geom.Rc(f, f, f+10, f+10), render.RGB(1, 2, 3))
	}
	submit(t, r2, c2, &l2)
	if c2.batches != 1 {
		t.Errorf("the same scene without a material took %d draw calls, want 1", c2.batches)
	}
}

// TestReducedAndFullPassChains pins the shape of both levels, which is the
// structure the project plan, section 8, specifies: Reduced is a region copy
// and a composite, Full adds a dual-Kawase down and up chain in between.
func TestReducedAndFullPassChains(t *testing.T) {
	for _, tc := range []struct {
		name  string
		glass render.Glass
		want  string
	}{
		{"reduced", render.NewGlass().Quality(render.Reduced), "copy,composite,scene"},
		{"full blur 4", render.NewGlass().Quality(render.Full).Blur(4), "copy,down,up,composite,scene"},
		{"full blur 16", render.NewGlass().Quality(render.Full).Blur(16),
			"copy,down,down,down,up,up,up,composite,scene"},
		{"full blur 64", render.NewGlass().Quality(render.Full).Blur(64),
			"copy,down,down,down,down,up,up,up,up,composite,scene"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, c := sceneRenderer(t, 800, 600)
			trace := glassTrace(r)
			var l render.List
			l.Reset()
			addGlass(&l, geom.Rc(0, 0, 400, 200), 16, tc.glass)
			submit(t, r, c, &l)
			if got := trace(); got != tc.want {
				t.Errorf("passes %q, want %q", got, tc.want)
			}
			if got := r.Stats().GlassFallbacks; got != 0 {
				t.Errorf("%d fallbacks; the chain degraded", got)
			}
		})
	}
}

// TestBlurChainStaysInsideTheRegion is the area argument of the project plan,
// section 8, checked rather than quoted: "kostet die gesamte Kette unter 1,5x
// der Flaeche der Materialregion, nicht des Bildschirms."
//
// The shaded area is summed from the quads the passes actually emit. The copy
// and the composite are one region area each and are not part of the chain;
// the chain is the down and up passes, and it is those that section 8 bounds.
func TestBlurChainStaysInsideTheRegion(t *testing.T) {
	const rw, rh = 1920.0, 200.0
	r, _ := sceneRenderer(t, 1920, 1080)

	var chain, total float64
	var stage glassPass
	r.passFn = func(p glassPass) { stage = p }
	r.drawFn = func(m Material, verts []eb.Vertex, idx []uint32) {
		if m != MaterialGlass || len(verts) != 4 {
			return
		}
		a := float64((verts[2].DstX - verts[0].DstX) * (verts[2].DstY - verts[0].DstY))
		total += a
		if stage == glassPassScene {
			// The scene to screen blit is a property of the frame and not of
			// this material; see glassPassScene.
			total -= a
			return
		}
		if stage == glassPassDown || stage == glassPassUp {
			chain += a
		}
	}

	var l render.List
	l.Reset()
	addGlass(&l, geom.Rc(0, 0, rw, rh), 18, render.NewGlass().Quality(render.Full).Blur(16))
	submit(t, r, &capture{}, &l)

	region := float64(rw * rh)
	screen := 1920.0 * 1080.0
	// # A correction to the project plan, section 8
	//
	// Section 8 says the whole chain costs "unter 1,5x der Flaeche der
	// Materialregion". That cannot be met by a dual-Kawase chain that ends at
	// the region's own resolution, and the arithmetic is short enough to
	// settle here. The down passes write 1/4 + 1/16 + ... of the region; the
	// up passes write the same areas again *plus* the full region, because
	// the last one has to land at region resolution for the composite to
	// sample it. So the floor is 1.25x with one level, 1.56x with two and
	// 1.64x with three — and section 8's own worked example agrees with this
	// rather than with its own sentence: it quotes "rund 0,6 MPixel" for a
	// 1920x200 panel, which is 0.384 MPixel, and 0.6/0.384 is 1.56.
	//
	// The bound asserted here is therefore 1.7x, which is the three level
	// chain the default blur of 16 asks for, and the disagreement with the
	// sentence is recorded rather than quietly met by counting fewer passes.
	if chain > 1.7*region {
		t.Errorf("the blur chain shaded %.0f pixels, more than 1.7x the region area %.0f", chain, region)
	}
	// The whole material, chain plus copy plus composite, is what the frame
	// actually pays. It is not what section 8 bounds, and it is reported here
	// so that the number is not mistaken for the other one.
	t.Logf("region %.0f px (%.1f%% of a 1080p screen); chain %.2fx region, whole material %.2fx region",
		region, 100*region/screen, chain/region, total/region)
	// Copy (1x) plus chain (1.64x) plus composite (1x) is 3.64x, and that is
	// the number a frame actually pays for this material. It is stated here
	// rather than asserted against a rounder figure, because the rounder
	// figure would be the one section 8 quotes and this is not it.
	if total > 3.7*region {
		t.Errorf("the whole material shaded %.2fx the region area, more than copy plus chain plus composite",
			total/region)
	}
}

// TestMaterialHonoursTheParentClip. The composite quad is the intersection of
// the region with the clip, so a clipped panel pays fragments only for what is
// on screen and, more importantly, does not paint outside its parent.
func TestMaterialHonoursTheParentClip(t *testing.T) {
	r, _ := sceneRenderer(t, 800, 600)
	var quad geom.Rect
	var stage glassPass
	r.passFn = func(p glassPass) { stage = p }
	r.drawFn = func(m Material, verts []eb.Vertex, idx []uint32) {
		if stage == glassPassComposite && len(verts) == 4 {
			quad = geom.Rc(verts[0].DstX, verts[0].DstY, verts[2].DstX, verts[2].DstY)
		}
	}

	var l render.List
	l.Reset()
	l.PushClip(geom.Rc(50, 50, 150, 100))
	l.Add(render.Op{
		Kind: render.OpMaterial, Bounds: geom.Rc(0, 0, 400, 200), CornerRadius: 10,
		Clip:     l.CurrentClip(),
		Material: l.AddMaterial(render.NewGlass().Quality(render.Reduced).Material()),
	})
	l.PopClip()
	submit(t, r, &capture{}, &l)

	if want := geom.Rc(50, 50, 150, 100); quad != want {
		t.Errorf("the composite quad is %v, want the clipped region %v", quad, want)
	}
}

// TestMaterialEntirelyOutsideItsClipDrawsNothing, and costs no passes and no
// targets: the barrier still flushes, but the region never leases anything.
func TestMaterialEntirelyOutsideItsClipDrawsNothing(t *testing.T) {
	r, c := sceneRenderer(t, 800, 600)
	trace := glassTrace(r)
	var l render.List
	l.Reset()
	l.PushClip(geom.Rc(500, 500, 600, 600))
	l.Add(render.Op{
		Kind: render.OpMaterial, Bounds: geom.Rc(0, 0, 100, 100),
		Clip:     l.CurrentClip(),
		Material: l.AddMaterial(render.NewGlass().Material()),
	})
	l.PopClip()
	submit(t, r, c, &l)

	// Nothing at all: no scene target, no clear, no full screen blit. The
	// material is in the list but is not visible, and the project plan,
	// section 11, as amended after WU-S, makes that distinction the
	// condition for paying for the screen sized target.
	if got := trace(); got != "" {
		t.Errorf("passes %q, want none: an invisible material costs no scene target", got)
	}
	if n := r.Targets().Stats().Leases; n != 0 {
		t.Errorf("%d target lease(s) for a material nobody can see, want 0", n)
	}
	s := r.Stats()
	if s.GlassOps != 0 {
		t.Errorf("GlassOps = %d, want 0", s.GlassOps)
	}
	if s.SkippedOutsideClip != 1 {
		t.Errorf("SkippedOutsideClip = %d, want 1", s.SkippedOutsideClip)
	}
}

// TestMaterialWithoutABackdropDegradesVisibly is the honest failure path: no
// screen to render the scene into, so no backdrop, so a plain tinted rounded
// rectangle and a counter that says so.
//
// This is also what every headless test without a target sees, which is why it
// has to be a defined behaviour and not an accident.
func TestMaterialWithoutABackdropDegradesVisibly(t *testing.T) {
	r, c := newHeadlessRenderer(t) // no SetTarget
	trace := glassTrace(r)
	var l render.List
	l.Reset()
	addGlass(&l, geom.Rc(0, 0, 100, 50), 8, render.NewGlass().Quality(render.Full))
	submit(t, r, c, &l)

	if got := trace(); got != "fallback" {
		t.Errorf("passes %q, want fallback", got)
	}
	s := r.Stats()
	if s.GlassOps != 1 || s.GlassFallbacks != 1 {
		t.Errorf("GlassOps=%d GlassFallbacks=%d, want 1 and 1", s.GlassOps, s.GlassFallbacks)
	}
	// It is drawn, not skipped: the fallback is in the shape batch.
	if c.batches != 1 || c.mats[0] != MaterialShape {
		t.Errorf("draw calls %v, want one shape batch", c.mats)
	}
}

// TestMaterialAccountingIsTotal. The invariant of [RendererStats] extends to
// the new kind: every submitted operation is counted exactly once.
func TestMaterialAccountingIsTotal(t *testing.T) {
	r, c := sceneRenderer(t, 400, 300)
	var l render.List
	l.Reset()
	addRect(&l, geom.Rc(0, 0, 10, 10), render.RGB(1, 2, 3))
	addGlass(&l, geom.Rc(0, 0, 100, 50), 8, render.NewGlass())
	// A material operation with no side table entry: malformed, and counted
	// as an unknown rather than dropped.
	l.Add(render.Op{Kind: render.OpMaterial, Bounds: geom.Rc(0, 0, 10, 10)})
	addGlass(&l, geom.Rc(0, 0, 0, 0), 0, render.NewGlass()) // empty bounds
	submit(t, r, c, &l)

	s := r.Stats()
	if got, want := s.Accounted(), uint64(l.Len()); got != want {
		t.Errorf("accounted %d of %d operations", got, want)
	}
	if s.UnknownKinds != 1 {
		t.Errorf("UnknownKinds = %d, want 1 for the material with no side table entry", s.UnknownKinds)
	}
	if s.SkippedEmptyBounds != 1 {
		t.Errorf("SkippedEmptyBounds = %d, want 1", s.SkippedEmptyBounds)
	}
	if s.GlassOps != 1 {
		t.Errorf("GlassOps = %d, want 1", s.GlassOps)
	}
}

// TestTargetsAreReusedAcrossFramesAndPanels is the bound the project plan,
// section 11, cares about: no target per widget, and no growth per frame.
func TestTargetsAreReusedAcrossFramesAndPanels(t *testing.T) {
	r, c := sceneRenderer(t, 800, 600)
	var l render.List
	l.Reset()
	// Four panels of the same size, which is the case a naive implementation
	// gives four sets of targets.
	for i := range 4 {
		y := float32(i) * 120
		addGlass(&l, geom.Rc(0, y, 400, y+100), 12,
			render.NewGlass().Quality(render.Full).Blur(16))
	}

	for range 3 {
		r.SetTarget(eb.NewImage(800, 600))
		submit(t, r, c, &l)
	}

	s := r.Targets().Stats()
	// One scene target plus the region and its three blur levels: five.
	if s.Targets > 5 {
		t.Errorf("%d targets resident, want at most 5 for four identical panels over three frames", s.Targets)
	}
	if s.Allocations > 5 {
		t.Errorf("%d allocations for twelve panel-frames, want at most 5", s.Allocations)
	}
	if s.Reuses == 0 {
		t.Error("no target was ever reused")
	}
	if got := r.Targets().Leased(); got != 0 {
		t.Errorf("%d targets still leased between frames, want 0", got)
	}
}

// TestTargetPoolReleasesExplicitly is the project plan, section 11,
// requirement applied to render targets: Deallocate, not a cleanup function.
func TestTargetPoolReleasesExplicitly(t *testing.T) {
	var freed []*eb.Image
	p := NewTargetPool(TargetConfig{MaxBytes: 64 * 64 * 4 * 2, MaxAge: 1})
	p.onDeallocate = func(i *eb.Image) { freed = append(freed, i) }

	a := p.Acquire(64, 64)
	if a == nil {
		t.Fatal("the first lease was refused")
	}
	p.Release(a)
	// Budget pressure: a second, differently sized target needs the room.
	b := p.Acquire(60, 60) // same bucket, so it is the same target
	if b != a {
		t.Error("a released target of the same bucket was not reused")
	}
	p.Release(b)

	c := p.Acquire(128, 64)
	if c == nil {
		t.Fatal("the second lease was refused; the pool did not evict to make room")
	}
	if len(freed) != 1 || freed[0] != a {
		t.Errorf("deallocated %v, want the first target", freed)
	}
	p.Release(c)

	if got := p.Stats().Evictions; got != 1 {
		t.Errorf("Evictions = %d, want 1", got)
	}
	p.Close()
	if len(freed) != 2 {
		t.Errorf("Close deallocated %d targets, want the remaining one", len(freed)-1)
	}
	if s := p.Stats(); s.Deallocations != 2 || s.Targets != 0 {
		t.Errorf("after Close: %d deallocations, %d resident; want 2 and 0", s.Deallocations, s.Targets)
	}
}

// TestTargetPoolAgesOutIdleTargets. A glass panel that closes must give the
// memory back; two seconds of a screen sized target is the whole point of the
// much shorter MaxAge here than in the texture cache.
func TestTargetPoolAgesOutIdleTargets(t *testing.T) {
	p := NewTargetPool(TargetConfig{MaxAge: 3})
	p.Release(p.Acquire(64, 64))
	for range 5 {
		p.Tick()
	}
	if got := p.Stats().Targets; got != 0 {
		t.Errorf("%d targets left after five ticks with MaxAge 3, want 0", got)
	}
	if got := p.Stats().AgeEvictions; got != 1 {
		t.Errorf("AgeEvictions = %d, want 1", got)
	}
}

// TestTargetPoolRefusesRatherThanGrowing. A budget that grows to fit whatever
// was asked for is not a budget; the caller degrades instead.
func TestTargetPoolRefusesRatherThanGrowing(t *testing.T) {
	p := NewTargetPool(TargetConfig{MaxBytes: 64 * 64 * 4})
	a := p.Acquire(64, 64)
	if a == nil {
		t.Fatal("the first lease was refused")
	}
	// Still leased, so nothing can be evicted for the second one.
	if b := p.Acquire(64, 64); b != nil {
		t.Error("the pool exceeded its budget rather than refusing")
	}
	if got := p.Stats().Rejected; got != 1 {
		t.Errorf("Rejected = %d, want 1", got)
	}
	p.Release(a)
}

// TestGlassPolicyDownAndUp is the hysteresis of the project plan, section 8:
// it drops under load, climbs back when the load goes away, and the two
// thresholds are different numbers.
func TestGlassPolicyDownAndUp(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{Window: 10, MinDwellFrames: 15})
	feed := func(d time.Duration, n int) render.GlassQuality {
		var q render.GlassQuality
		for range n {
			p.RecordInterval(d)
			q = p.BeginFrame(1000)
		}
		return q
	}

	if got := p.Level(); got != render.Full {
		t.Fatalf("a fresh policy is at %v, want full", got)
	}
	// Comfortable: it stays.
	if got := feed(16*time.Millisecond, 40); got != render.Full {
		t.Errorf("at 16 ms the policy went to %v, want full", got)
	}
	// Overloaded: it drops, but not before the dwell has run.
	if got := feed(30*time.Millisecond, 5); got != render.Full {
		t.Errorf("the policy dropped after 5 frames of load; the dwell time did not hold")
	}
	if got := feed(30*time.Millisecond, 40); got != render.Reduced {
		t.Errorf("at 30 ms the policy stayed at %v, want reduced", got)
	}
	// In between the two thresholds: it stays down. This is the hysteresis,
	// and it is the assertion that a single threshold would fail.
	if got := feed(19*time.Millisecond, 60); got != render.Reduced {
		t.Errorf("at 19 ms, between the 18 ms up and 20 ms down thresholds, the policy went to %v, want reduced", got)
	}
	// Comfortable again: it climbs back.
	if got := feed(16*time.Millisecond, 60); got != render.Full {
		t.Errorf("at 16 ms the policy stayed at %v, want full", got)
	}
	s := p.Stats()
	if s.Downgrades != 1 || s.Upgrades != 1 {
		t.Errorf("%d downgrades and %d upgrades, want 1 and 1", s.Downgrades, s.Upgrades)
	}
}

// TestGlassPolicyDoesNotFlicker is the property the project plan, section 13,
// names outright: "Full-/Reduced-Fallback inklusive Hysterese ohne Flackern."
//
// The adversarial input is a frame time that sits exactly between the two
// thresholds and jitters across both of them, which is the signal a policy
// without hysteresis or without a dwell time oscillates on. One change is
// allowed — the policy starts optimistic and has to find out — and no more.
func TestGlassPolicyDoesNotFlicker(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{})
	d := []time.Duration{
		16 * time.Millisecond, 25 * time.Millisecond,
		17 * time.Millisecond, 24 * time.Millisecond,
		19 * time.Millisecond, 21 * time.Millisecond,
	}
	for i := range 3600 { // a minute at sixty hertz
		p.RecordInterval(d[i%len(d)])
		p.BeginFrame(1000)
	}
	if got := p.Stats().Changes; got > 1 {
		t.Errorf("the level changed %d times in a minute of jitter around the thresholds; "+
			"the hysteresis and the dwell time exist to make that at most 1", got)
	}
}

// TestGlassPolicyAreaBudget is the other signal, and it is the one the project
// plan, section 13, actually binds the Full threshold under: a material area
// above a quarter of the screen is outside the envelope the 4 ms was stated
// for, so Full is refused immediately rather than after a dwell.
func TestGlassPolicyAreaBudget(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{})
	p.AddArea(300)
	if got := p.BeginFrame(1000); got != render.Reduced {
		t.Errorf("30 %% of the screen gave %v, want reduced immediately", got)
	}
	if got := p.Stats().AreaDowngrades; got != 1 {
		t.Errorf("AreaDowngrades = %d, want 1", got)
	}
	// Coming back is subject to the dwell like everything else, so a panel
	// hovering around the limit cannot flicker.
	for range 30 {
		p.AddArea(100)
		p.RecordInterval(16 * time.Millisecond)
		if got := p.BeginFrame(1000); got != render.Reduced {
			t.Fatalf("the policy climbed back inside the dwell period")
		}
	}
}

// TestGlassPolicyDoesNotFlickerOnArea is the mirror of
// [TestGlassPolicyDoesNotFlicker] for the second signal.
//
// The interval half of the policy had a carefully argued down/up gap and a
// dwell; the area half had one threshold for both the immediate veto and the
// climb-back gate, and the suite tested only the safe direction. With a
// material area oscillating by four percent around the 25 % limit — which is
// an ordinary panel over a scrolling gallery — that produced 39 level changes
// in thirty simulated seconds, one every 0.77 s. The eye is very good at
// seeing a background change and very bad at seeing how blurred it is, so
// that is the worst failure mode the policy has.
//
// The frame interval is held comfortably healthy throughout, so the only
// signal moving is the area and the number below is attributable to it.
func TestGlassPolicyDoesNotFlickerOnArea(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{})
	// ±4 % around the limit, in the units BeginFrame takes: a screen area of
	// 1000 and material areas of 240 and 260, that is 24 % and 26 %.
	areas := []float64{260, 240, 258, 242, 255, 245}
	const frames = 1800 // thirty seconds at sixty hertz
	for i := range frames {
		p.RecordInterval(16 * time.Millisecond)
		p.AddArea(areas[i%len(areas)])
		p.BeginFrame(1000)
	}
	if got := p.Stats().Changes; got > 1 {
		t.Errorf("the level changed %d times in thirty seconds of an area oscillating around "+
			"the limit; the area band and the area window exist to make that at most 1.\n"+
			"stats: %+v", got, p.Stats())
	}
	// And the level it settled on is the safe one: the area exceeds the veto
	// threshold in half the frames, so Full is not available.
	if got := p.Level(); got != render.Reduced {
		t.Errorf("the policy settled on %v, want reduced: the area is over the limit half the time", got)
	}
}

// TestGlassPolicyAreaRecoversWhenTheAreaReallyShrinks is the other side of the
// same coin. A band and a window that never let the policy climb back would
// be a policy that only goes down, which is not hysteresis but a ratchet.
func TestGlassPolicyAreaRecoversWhenTheAreaReallyShrinks(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{})
	// Over the limit long enough to be vetoed.
	for range 10 {
		p.RecordInterval(16 * time.Millisecond)
		p.AddArea(300)
		p.BeginFrame(1000)
	}
	if p.Level() != render.Reduced {
		t.Fatalf("the veto did not fire; level is %v", p.Level())
	}
	// The panel closed: well under the up threshold, for longer than the
	// dwell and the area window together.
	for range DefaultGlassDwellFrames + DefaultGlassWindow + 10 {
		p.RecordInterval(16 * time.Millisecond)
		p.AddArea(50)
		p.BeginFrame(1000)
	}
	if got := p.Level(); got != render.Full {
		t.Errorf("the policy stayed at %v after the material area fell to 5 %% for two seconds; "+
			"hysteresis must not be a ratchet.\nstats: %+v", got, p.Stats())
	}
}

// TestGlassPolicyAreaWindowIsWhatStopsTheFlicker isolates the second half of
// the fix. A band alone is not enough: with AreaWindow at one frame the climb
// back is decided on whatever the area happened to be in the single frame the
// dwell expired on, and the oscillation returns. This is the measurement the
// reviewer took, reproduced as a test so that removing the window is a
// failure rather than a regression nobody sees.
func TestGlassPolicyAreaWindowIsWhatStopsTheFlicker(t *testing.T) {
	// AreaWindow: -1 means one frame, which is the old behaviour, and
	// UpAreaFraction equal to the old single threshold.
	p := NewGlassPolicy(GlassPolicyConfig{AreaWindow: -1, UpAreaFraction: 0.2499})
	areas := []float64{260, 240, 258, 242, 255, 245}
	for i := range 1800 {
		p.RecordInterval(16 * time.Millisecond)
		p.AddArea(areas[i%len(areas)])
		p.BeginFrame(1000)
	}
	if got := p.Stats().Changes; got < 10 {
		t.Fatalf("the degenerate configuration produced only %d changes; this test no longer "+
			"reproduces the defect it pins and has stopped being evidence", got)
	}
	t.Logf("with a one frame area gate the level changes %d times in thirty seconds",
		p.Stats().Changes)
}

// TestPinnedGlassLevelNeverChanges. The project plan, section 13, makes
// pinning mandatory for comparable measurements, which is worth nothing unless
// a pin actually holds under exactly the conditions that would otherwise move
// it.
func TestPinnedGlassLevelNeverChanges(t *testing.T) {
	for _, want := range []render.GlassQuality{render.Reduced, render.Full} {
		p := NewGlassPolicy(GlassPolicyConfig{Window: 5, MinDwellFrames: 1})
		p.Pin(want)
		for i := range 500 {
			d := 60 * time.Millisecond
			if i%2 == 0 {
				d = 8 * time.Millisecond
			}
			p.RecordInterval(d)
			p.AddArea(900)
			if got := p.BeginFrame(1000); got != want {
				t.Fatalf("a pinned %v policy returned %v at frame %d", want, got, i)
			}
		}
		if !p.IsPinned() {
			t.Error("IsPinned is false after Pin")
		}
	}
	// Unpinning keeps the level and restarts the dwell, so it cannot cause an
	// immediate jump.
	p := NewGlassPolicy(GlassPolicyConfig{Window: 5, MinDwellFrames: 100})
	p.Pin(render.Reduced)
	p.Unpin()
	if p.IsPinned() {
		t.Error("IsPinned is true after Unpin")
	}
	for range 50 {
		p.RecordInterval(8 * time.Millisecond)
		if got := p.BeginFrame(1000); got != render.Reduced {
			t.Fatalf("the policy jumped to %v immediately after Unpin", got)
		}
	}
}

// TestGlassPolicyRejectsAThresholdWithoutHysteresis. A configuration with no
// gap between the two thresholds has no hysteresis, and a policy without
// hysteresis flickers; refusing it at construction beats documenting it.
func TestGlassPolicyRejectsAThresholdWithoutHysteresis(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("equal thresholds were accepted")
		}
	}()
	NewGlassPolicy(GlassPolicyConfig{
		UpInterval: 20 * time.Millisecond, DownInterval: 20 * time.Millisecond,
	})
}

// TestGlassPolicyDiscardsSubFrameIntervals. Two draw callbacks in a row are
// not two presentations, and counting them would pull the median down and hold
// a stuttering machine at Full. The project plan, section 13, makes the same
// rule for the frame timer, and it is counted rather than silently dropped.
func TestGlassPolicyDiscardsSubFrameIntervals(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{Window: 4})
	p.RecordInterval(100 * time.Microsecond)
	p.RecordInterval(20 * time.Millisecond)
	if got := p.Median(); got != 20*time.Millisecond {
		t.Errorf("median = %v, want 20 ms; the sub-frame interval entered the window", got)
	}
	if got := p.Stats().SubFrameIntervals; got != 1 {
		t.Errorf("SubFrameIntervals = %d, want 1", got)
	}
}

// TestGlassPolicyMedianDoesNotAllocate. It runs once per drawn frame, so it is
// in the frame path of the project plan, section 11.
func TestGlassPolicyMedianDoesNotAllocate(t *testing.T) {
	p := NewGlassPolicy(GlassPolicyConfig{})
	for i := range 200 {
		p.RecordInterval(time.Duration(16+i%5) * time.Millisecond)
	}
	if got := testing.AllocsPerRun(200, func() { p.Median() }); got != 0 {
		t.Errorf("Median allocates %v times per call, want 0", got)
	}
}

// TestBlurLevelsFromRadius documents the mapping in one place, because the
// pass chain test asserts on its consequences and a reader should be able to
// check the arithmetic without reading the chain.
func TestBlurLevelsFromRadius(t *testing.T) {
	for _, tc := range []struct {
		radius float32
		want   int
	}{{0, 1}, {1, 1}, {4, 1}, {8, 2}, {16, 3}, {32, 4}, {128, 4}} {
		if got := blurLevels(tc.radius); got != tc.want {
			t.Errorf("blurLevels(%v) = %d, want %d", tc.radius, got, tc.want)
		}
	}
}

// TestPackGlassStyleRoundTrips is the check on the one genuinely lossy thing
// in the material path.
//
// Three scalars share one float32 vertex attribute because a vertex has four
// and glass needs six; see glass.kage. The packing is only sound while every
// packed value stays under 2^24, which is where a float32 stops representing
// integers exactly, and this is the assertion that the clamps in render.Glass
// keep it there.
func TestPackGlassStyleRoundTrips(t *testing.T) {
	unpack := func(v float32) (r, h, g float32) {
		rq := float32(int(v) >> 16)
		rest := v - rq*65536
		hq := float32(int(rest) >> 8)
		gq := rest - hq*256
		return rq, hq / 255, gq / 255
	}
	for _, tc := range []struct{ r, h, g float32 }{
		{0, 0, 0}, {63, 1, 1}, {6, 0.5, 0.06}, {1, 0.25, 0.75},
	} {
		v := packGlassStyle(tc.r, tc.h, tc.g)
		if v >= 1<<24 {
			t.Fatalf("packed %v exceeds the exact integer range of a float32", v)
		}
		if float32(int(v)) != v {
			t.Fatalf("packed %v is not an integer", v)
		}
		r, h, g := unpack(v)
		if r != tc.r {
			t.Errorf("refraction %v round tripped to %v", tc.r, r)
		}
		if diff := h - tc.h; diff > 1.0/255 || diff < -1.0/255 {
			t.Errorf("highlight %v round tripped to %v", tc.h, h)
		}
		if diff := g - tc.g; diff > 1.0/255 || diff < -1.0/255 {
			t.Errorf("grain %v round tripped to %v", tc.g, g)
		}
	}
}

// TestGlassShadersCompile. Kage is compiled on the CPU, so this needs no
// window, and a broken shader is otherwise a blank panel at run time.
func TestGlassShadersCompile(t *testing.T) {
	down, up := KawaseShaderSources()
	for name, src := range map[string][]byte{
		"glass": GlassShaderSource(), "kawase_down": down, "kawase_up": up,
	} {
		if _, err := eb.NewShader(src); err != nil {
			t.Errorf("%s.kage: %v", name, err)
		}
	}
}

// TestEffectiveGlassLevelIsVisible is the requirement of the project plan,
// section 8, that the effective level appears in the diagnostics, and of
// section 15 that it is a counter rather than a log line.
func TestEffectiveGlassLevelIsVisible(t *testing.T) {
	r, c := sceneRenderer(t, 400, 300)
	r.PinGlassQuality(render.Reduced)
	var l render.List
	l.Reset()
	addGlass(&l, geom.Rc(0, 0, 100, 50), 8, render.NewGlass())
	submit(t, r, c, &l)

	s := r.Stats()
	if s.GlassLevel != render.Reduced || !s.GlassPinned {
		t.Errorf("GlassLevel=%v pinned=%v, want reduced and true", s.GlassLevel, s.GlassPinned)
	}
	if s.GlassReducedOps != 1 || s.GlassFullOps != 0 {
		t.Errorf("reduced=%d full=%d, want 1 and 0", s.GlassReducedOps, s.GlassFullOps)
	}
	if got := r.GlassLevel(); got != render.Reduced {
		t.Errorf("GlassLevel() = %v, want reduced", got)
	}
}

// TestMaterialLevelOverridesAnAdaptivePolicy. A material may pin its own
// level, which is what lets example-effects show both side by side and what
// section 13 needs for an isolated measurement.
func TestMaterialLevelOverridesAnAdaptivePolicy(t *testing.T) {
	r, c := sceneRenderer(t, 800, 600)
	// Adaptive, and forced to Reduced so that the two panels below would
	// otherwise be identical.
	r.SetGlassPolicy(NewGlassPolicy(GlassPolicyConfig{}))
	r.GlassPolicy().Pin(render.Reduced)
	r.GlassPolicy().Unpin()
	var l render.List
	l.Reset()
	addGlass(&l, geom.Rc(0, 0, 200, 100), 8, render.NewGlass().Quality(render.Full).Blur(16))
	addGlass(&l, geom.Rc(0, 200, 200, 300), 8, render.NewGlass().Quality(render.Adaptive))
	submit(t, r, c, &l)

	s := r.Stats()
	if s.GlassFullOps != 1 || s.GlassReducedOps != 1 {
		t.Errorf("full=%d reduced=%d, want 1 each", s.GlassFullOps, s.GlassReducedOps)
	}
}

// TestAPinnedPolicyBeatsAMaterialLevel is the other half, and the one that was
// wrong: a material's own level used to override a pinned *policy*, after
// which [RendererStats.GlassLevel] reported a level the frame had not used.
//
// The project plan, section 13, makes a fixed level a precondition of
// comparing one measurement with another. A pin that a display list can
// silently escape is not a fixed level.
func TestAPinnedPolicyBeatsAMaterialLevel(t *testing.T) {
	r, c := sceneRenderer(t, 800, 600)
	r.PinGlassQuality(render.Reduced)
	var l render.List
	l.Reset()
	addGlass(&l, geom.Rc(0, 0, 200, 100), 8, render.NewGlass().Quality(render.Full).Blur(16))
	addGlass(&l, geom.Rc(0, 200, 200, 300), 8, render.NewGlass().Quality(render.Adaptive))
	submit(t, r, c, &l)

	s := r.Stats()
	if s.GlassReducedOps != 2 || s.GlassFullOps != 0 {
		t.Errorf("reduced=%d full=%d, want 2 and 0: a pinned policy is not negotiable",
			s.GlassReducedOps, s.GlassFullOps)
	}
	if s.GlassLevel != render.Reduced || !s.GlassPinned {
		t.Errorf("GlassLevel=%v pinned=%v, want reduced and true", s.GlassLevel, s.GlassPinned)
	}
}

// TestGlassSubmitPathIsAllocationFree is the backend half of the allocation
// contract of the project plan, section 11, with a material in the list.
//
// The three things that could have broken it are all here: the pass chain
// leases and releases targets every frame, the composite writes four vertices
// into a buffer that must not grow, and Ebitengine's Uniforms is a
// map[string]any which allocates the moment anything is put in it. The glass
// shaders take no uniforms at all — every parameter is in a vertex attribute,
// three of them packed into one — and this is the assertion that says so.
func TestGlassSubmitPathIsAllocationFree(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	r.drawFn = func(Material, []eb.Vertex, []uint32) {}
	screen := eb.NewImage(800, 600)

	var l render.List
	l.Reset()
	for i := range 50 {
		f := float32(i)
		addRect(&l, geom.Rc(f, f, f+10, f+10), render.RGB(1, 2, 3))
	}
	addGlass(&l, geom.Rc(0, 0, 400, 200), 18, render.NewGlass().Quality(render.Full).Blur(16))
	for i := range 50 {
		f := float32(i) + 200
		addRect(&l, geom.Rc(f, f, f+10, f+10), render.RGB(3, 2, 1))
	}

	step := func() {
		r.SetTarget(screen)
		r.BeginFrame(geom.Sz(800, 600))
		r.Submit(&l)
		r.EndFrame()
	}
	for range 32 {
		step()
	}
	if got := r.Stats().GlassFullOps; got == 0 {
		t.Fatal("no Full material was drawn; the measurement would be meaningless")
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("the submit path with a Full material allocated %v times per run, want 0", got)
	}
	if got := r.Targets().Stats().Allocations; got > 5 {
		t.Errorf("%d target allocations over %d frames, want at most 5", got, 232)
	}
}

// TestInvisibleMaterialCostsNoScreenTarget is the project plan, section 11, as
// amended after WU-S, expressed as a number of allocations.
//
// The scan that decides whether a frame needs the screen sized scene target
// used to vote yes on the mere presence of an OpMaterial in the list, before
// any operation had been translated, so it never learned that the material was
// clipped away, had empty bounds or was off screen. Measured: one glass op at
// (1000,1000) inside a 10x10 clip still leased a full screen target. At 1080p
// that is eight megabytes, a full screen clear and a full screen blit every
// frame for a glass header somebody scrolled out of view.
func TestInvisibleMaterialCostsNoScreenTarget(t *testing.T) {
	cases := map[string]func(l *render.List){
		"clipped away": func(l *render.List) {
			l.PushClip(geom.Rc(0, 0, 10, 10))
			l.Add(render.Op{
				Kind: render.OpMaterial, Bounds: geom.Rc(1000, 1000, 1200, 1100),
				Clip:     l.CurrentClip(),
				Material: l.AddMaterial(render.NewGlass().Material()),
			})
			l.PopClip()
		},
		"empty bounds": func(l *render.List) {
			addGlass(l, geom.Rc(20, 20, 20, 120), 8, render.NewGlass())
		},
		"off screen": func(l *render.List) {
			addGlass(l, geom.Rc(2000, 2000, 2200, 2100), 8, render.NewGlass())
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			r, c := sceneRenderer(t, 800, 600)
			var l render.List
			l.Reset()
			build(&l)
			submit(t, r, c, &l)

			ts := r.Targets().Stats()
			if ts.Leases != 0 || ts.Allocations != 0 {
				t.Errorf("%d lease(s) and %d allocation(s) for a material nobody can see, want none: %+v",
					ts.Leases, ts.Allocations, ts)
			}
			if got := r.Stats().GlassPasses; got != 0 {
				t.Errorf("%d material pass(es), want none", got)
			}
		})
	}

	// And the control: a material that *is* visible still pays for the
	// target, or the test above would pass on a renderer that had stopped
	// supporting glass altogether.
	t.Run("visible", func(t *testing.T) {
		r, c := sceneRenderer(t, 800, 600)
		var l render.List
		l.Reset()
		addGlass(&l, geom.Rc(20, 20, 220, 120), 8, render.NewGlass())
		submit(t, r, c, &l)
		if got := r.Targets().Stats().Leases; got == 0 {
			t.Error("a visible material leased no target at all")
		}
	})
}

// TestGlassStylePackingBoundaries checks the packing against the unpacking the
// shader performs, at the extremes the clamps allow.
//
// The comment on packGlassStyle used to claim a maximum of 16777215, which
// describes a scheme with a tighter margin than the code has: the refraction
// field is quantised to 63, not 255, so the real maximum is 4194303. The
// packing is sound either way, and this is the arithmetic that says so rather
// than a sentence.
func TestGlassStylePackingBoundaries(t *testing.T) {
	// unpack mirrors glass.kage exactly.
	unpack := func(p float32) (r, h, g float32) {
		rq := float32(math.Floor(float64(p) / 65536))
		rest := p - rq*65536
		hq := float32(math.Floor(float64(rest) / 256))
		gq := rest - hq*256
		return rq, hq / 255, gq / 255
	}
	if got := packGlassStyle(63, 1, 1); got != 4194303 {
		t.Errorf("the largest packed value is %v, want 4194303", got)
	}
	if got := packGlassStyle(1e9, 1e9, 1e9); got != 4194303 {
		t.Errorf("an out of range packing gave %v, want the clamped maximum 4194303", got)
	}
	for _, c := range []struct{ r, h, g float32 }{
		{0, 0, 0}, {63, 1, 1}, {1, 0, 1}, {62, 0.5, 0.25}, {0, 1, 0},
	} {
		p := packGlassStyle(c.r, c.h, c.g)
		if float64(p) != math.Trunc(float64(p)) || p > 1<<24-1 {
			t.Fatalf("packed %v is not an exact integer below 2^24-1", p)
		}
		r, h, g := unpack(p)
		if r != c.r {
			t.Errorf("refraction %v round tripped to %v", c.r, r)
		}
		if d := h - c.h; d > 1.0/255 || d < -1.0/255 {
			t.Errorf("highlight %v round tripped to %v", c.h, h)
		}
		if d := g - c.g; d > 1.0/255 || d < -1.0/255 {
			t.Errorf("grain %v round tripped to %v", c.g, g)
		}
	}
}

// TestRefractionIsScaledToDevicePixels. The shader adds the refraction to a
// device space sample position, so it has to arrive in device pixels like the
// corner radius and the blur radius beside it. It was packed raw, which was
// latent only because nothing in gift emits a scale transform yet — the same
// class of defect WU-E fixed for the shape shader.
func TestRefractionIsScaledToDevicePixels(t *testing.T) {
	packedAt := func(scale float32) float32 {
		r, _ := newHeadlessRenderer(t)
		r.SetTarget(eb.NewImage(800, 600))
		var got float32
		var stage glassPass
		r.passFn = func(p glassPass) { stage = p }
		r.drawFn = func(m Material, verts []eb.Vertex, idx []uint32) {
			if stage == glassPassComposite && len(verts) == 4 {
				got = verts[0].Custom3
			}
		}
		var l render.List
		l.Reset()
		xf := l.PushXform(geom.Affine2D{A: scale, D: scale})
		l.Add(render.Op{
			Kind: render.OpMaterial, Bounds: geom.Rc(0, 0, 100, 50), CornerRadius: 8,
			Xform:    xf,
			Clip:     l.CurrentClip(),
			Material: l.AddMaterial(render.NewGlass().Refraction(6).Highlight(0).Grain(0).Material()),
		})
		r.BeginFrame(geom.Sz(800, 600))
		r.Submit(&l)
		r.EndFrame()
		return got
	}
	one, two := packedAt(1), packedAt(2)
	// The refraction occupies the high field, so the packed value is the
	// device refraction times 65536 when highlight and grain are zero.
	if want := float32(6 * 65536); one != want {
		t.Errorf("at scale 1 the packed style is %v, want %v", one, want)
	}
	if want := float32(12 * 65536); two != want {
		t.Errorf("at scale 2 the packed style is %v, want %v; refraction is a length and the "+
			"shader consumes it in device pixels", two, want)
	}
}

// TestGlassFallbackIsVisibleForAClearPane is the visibility half of the
// degradation contract.
//
// drawGlassFallback used to return early when the tint was fully transparent,
// so a clear pane that lost its backdrop drew absolutely nothing while a
// default tinted one drew a ghost. Both are degradations, both are counted,
// and only one of them was visible — the other left a hole in the layout whose
// only evidence was a counter behind the giftmetrics build tag.
func TestGlassFallbackIsVisibleForAClearPane(t *testing.T) {
	// No target at all, so there is no scene and every material degrades.
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	addGlass(&l, geom.Rc(10, 10, 110, 60), 8,
		render.NewGlass().Tint(render.RGBA(0, 0, 0, 0)))
	submit(t, r, c, &l)

	s := r.Stats()
	if s.GlassFallbacks != 1 {
		t.Fatalf("GlassFallbacks = %d, want 1", s.GlassFallbacks)
	}
	if len(c.mats) != 1 || c.mats[0] != MaterialShape {
		t.Fatalf("the fallback issued batches %v, want one shape batch", c.mats)
	}
	verts := c.verts
	if len(verts) != 4 {
		t.Fatalf("the fallback emitted %d vertices, want a quad; a counted degradation that "+
			"draws nothing is not visible", len(verts))
	}
	if verts[0].ColorA <= 0 {
		t.Errorf("the fallback quad is fully transparent (alpha %v); a clear pane without a "+
			"backdrop must still show that there is a pane", verts[0].ColorA)
	}
}

// TestGlassLevelChangeIsReportedOnceOutOfBand is the logging half.
//
// The project plan, section 15, asks for both a counter in the hot path *and*
// a `Warn` on degraded quality, naming a fall back to Glass Reduced as its
// example. Nothing logged a downgrade at all. The frame path still only stores
// an enum and a bool; this is the drain that turns it into one line.
func TestGlassLevelChangeIsReportedOnceOutOfBand(t *testing.T) {
	var buf strings.Builder
	g := &game{
		r:   mustRenderer(t),
		log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	// Nothing has changed yet, so nothing is logged and no attribute is even
	// constructed.
	g.logGlassLevel()
	if buf.Len() != 0 {
		t.Fatalf("an unchanged policy logged %q", buf.String())
	}

	p := g.r.GlassPolicy()
	p.setLevel(render.Reduced)
	g.logGlassLevel()
	first := buf.String()
	if !strings.Contains(first, "level=WARN") || !strings.Contains(first, "degraded") {
		t.Errorf("the downgrade was logged as %q, want a Warn naming the degradation", first)
	}
	if !strings.Contains(first, "level=reduced") {
		t.Errorf("the downgrade line %q does not name the level", first)
	}

	// Once per change and not once per frame: a hundred more updates with no
	// further change say nothing at all.
	for range 100 {
		g.logGlassLevel()
	}
	if got := buf.String(); got != first {
		t.Errorf("a level change was reported more than once:\n%s", got)
	}

	// And the way back is Info, not Warn: restored quality is not a warning.
	p.setLevel(render.Full)
	g.logGlassLevel()
	rest := strings.TrimPrefix(buf.String(), first)
	if !strings.Contains(rest, "level=INFO") || !strings.Contains(rest, "restored") {
		t.Errorf("the upgrade was logged as %q, want an Info naming the restoration", rest)
	}
}

func mustRenderer(t testing.TB) *Renderer {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r
}
