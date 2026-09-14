package gift_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// This file covers the primitives WU-N added to the runtime contract so that a
// view can virtualise a scroll container. Each of them exists because the
// gallery of the project plan, section 10, could not be written without it;
// each is tested here, in the package that owns the contract, rather than only
// through the gallery that happens to be its first caller.

// vscrollNode is a minimal virtualising container: one child, placed at the
// document position the scroll offset selects.
type vscrollNode struct {
	virtual bool
	extent  float64

	// layouts counts the passes, offsets records what each pass saw.
	layouts int
	lastOff float64

	// wantMore makes the layouter ask for another pass, up to steps times.
	steps int

	// setOff, when set, is written into the container on the next pass.
	setOff    float64
	hasSetOff bool

	// buildAt asks for a rebuild on pass number buildAt.
	buildAt int
}

func (n *vscrollNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.layouts++
	size := c.Constrain(geom.Sz(200, 100))
	ctx.ReportScrollContent(n.extent, 0)
	if n.hasSetOff {
		n.hasSetOff = false
		ctx.SetScrollOffset(n.setOff)
	}
	n.lastOff = ctx.ScrollOffset()
	if n.steps > 0 {
		n.steps--
		ctx.RequestLayout()
	}
	if n.buildAt > 0 && n.layouts == n.buildAt {
		ctx.RequestBuild()
	}
	for i := range ctx.ChildCount() {
		ctx.Measure(i, geom.Tight(geom.Sz(50, 50)))
		ctx.Place(i, geom.Pt(0, float32(-n.lastOff)))
	}
	return size
}

type vscrollView struct {
	n     *vscrollNode
	kids  int
	built *int
}

var vscrollType = gift.RegisterType("test.VScroll")

func (vscrollView) ViewType() gift.TypeID { return vscrollType }

func (v vscrollView) Build(*gift.BuildContext) gift.Element {
	if v.built != nil {
		*v.built++
	}
	kids := make([]gift.View, v.kids)
	for i := range kids {
		kids[i] = leafView{}
	}
	return gift.Element{
		Layouter: v.n,
		Children: kids,
		Scroll:   &gift.ScrollSpec{Axis: gift.ScrollVertical, Virtual: v.n.virtual},
	}
}

var leafType = gift.RegisterType("test.VirtualLeaf")

type leafView struct{}

func (leafView) ViewType() gift.TypeID { return leafType }
func (leafView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: leafLayouter{}}
}

type leafLayouter struct{}

func (leafLayouter) Layout(_ *gift.LayoutContext, c geom.Constraints) geom.Size {
	return c.Constrain(c.Min)
}

func mountVScroll(t *testing.T, n *vscrollNode, kids int, built *int) (*gift.App, gift.NodeRef) {
	t.Helper()
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return vscrollView{n: n, kids: kids, built: built}
	}})
	for range 4 {
		if err := a.Update(geom.Sz(200, 100)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	var kidsBuf []gift.NodeRef
	ref := a.Root()
	for !a.IsScrollable(ref) {
		cs := a.NodeChildren(ref, kidsBuf[:0])
		if len(cs) == 0 {
			t.Fatal("no scroll container in the tree")
		}
		ref = cs[0]
	}
	return a, ref
}

// TestVirtualScrollInvalidatesLayout is the whole of [gift.ScrollSpec.Virtual]:
// an ordinary container repaints on a scroll, a virtual one relayouts, and
// neither rebuilds.
func TestVirtualScrollInvalidatesLayout(t *testing.T) {
	for _, virtual := range []bool{false, true} {
		n := &vscrollNode{virtual: virtual, extent: 10000}
		built := 0
		a, ref := mountVScroll(t, n, 1, &built)

		before := a.Diagnostics()
		builtBefore := built
		passesBefore := n.layouts
		a.ScrollBy(ref, 250)
		if err := a.Update(geom.Sz(200, 100)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
		after := a.Diagnostics()

		if after.Scrolls == before.Scrolls {
			t.Fatalf("virtual=%v: the offset did not move", virtual)
		}
		if built != builtBefore {
			t.Errorf("virtual=%v: a scroll rebuilt the scope %d times", virtual, built-builtBefore)
		}
		gotPasses := n.layouts - passesBefore
		switch {
		case virtual && gotPasses == 0:
			t.Error("a virtual container was not laid out again after its offset changed")
		case !virtual && gotPasses != 0:
			t.Errorf("an ordinary container ran its layouter %d times for a pure scroll", gotPasses)
		}
		if virtual && n.lastOff != 250 {
			t.Errorf("the virtual layouter saw offset %g, want 250", n.lastOff)
		}
		if info, _ := a.ScrollInfo(ref); info.Virtual != virtual {
			t.Errorf("ScrollInfo reports Virtual=%v", info.Virtual)
		}
	}
}

// TestRequestLayoutSurvivesTheRunningPass is the property that made the
// incremental reflow of the gallery work at all: the mark a layouter sets for
// itself must not be cleared by the pass that is setting it.
func TestRequestLayoutSurvivesTheRunningPass(t *testing.T) {
	n := &vscrollNode{extent: 1000}
	a, _ := mountVScroll(t, n, 1, nil)
	start := n.layouts
	// Ask for three more passes from inside the next one.
	n.steps = 3
	a.Invalidate()
	for range 8 {
		if err := a.Update(geom.Sz(200, 100)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	if n.steps != 0 {
		t.Fatalf("%d requested passes never happened", n.steps)
	}
	if got := n.layouts - start; got < 4 {
		t.Errorf("three requested passes produced %d layouter calls", got)
	}
	// And it settles: once the requests stop, so do the passes.
	quiet := n.layouts
	for range 4 {
		if err := a.Update(geom.Sz(200, 100)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	if n.layouts != quiet {
		t.Errorf("the layouter kept running %d extra passes after it stopped asking", n.layouts-quiet)
	}
}

// TestRequestBuildRebuildsTheOwningScope covers the one thing a layouter
// cannot do for itself: change the number of its children.
func TestRequestBuildRebuildsTheOwningScope(t *testing.T) {
	built := 0
	n := &vscrollNode{extent: 1000}
	a, _ := mountVScroll(t, n, 2, &built)
	before := built

	n.buildAt = n.layouts + 1
	a.Invalidate() // provoke one more layout pass
	for range 3 {
		if err := a.Update(geom.Sz(200, 100)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	if built <= before {
		t.Errorf("RequestBuild did not rebuild the owning scope (%d builds)", built-before)
	}
}

// TestSetScrollOffsetFromLayout is the anchor primitive: a layouter may move
// the viewport it is laying out, and the value it then reads back is the one
// it wrote, clamped against the content it just reported.
func TestSetScrollOffsetFromLayout(t *testing.T) {
	n := &vscrollNode{virtual: true, extent: 1000}
	a, ref := mountVScroll(t, n, 1, nil)

	n.setOff, n.hasSetOff = 400, true
	a.Invalidate()
	if err := a.Update(geom.Sz(200, 100)); err != nil {
		t.Fatal(err)
	}
	if n.lastOff != 400 {
		t.Errorf("the layouter read back %g after writing 400", n.lastOff)
	}
	if info, _ := a.ScrollInfo(ref); info.Offset != 400 {
		t.Errorf("ScrollInfo says %g", info.Offset)
	}

	// Clamped against the content extent: 1000 of document, 100 of viewport.
	n.setOff, n.hasSetOff = 1e9, true
	a.Invalidate()
	if err := a.Update(geom.Sz(200, 100)); err != nil {
		t.Fatal(err)
	}
	if n.lastOff != 900 {
		t.Errorf("an offset past the end came back as %g, want the clamped 900", n.lastOff)
	}
}

// TestInvalidatorMarksTheNodeAndIsStable checks both halves of
// [gift.LayoutContext.Invalidator]: it works from outside the frame, and it is
// the same closure every pass so that asking for it per pass is free.
func TestInvalidatorMarksTheNodeAndIsStable(t *testing.T) {
	var seen []func()
	ln := &invalidatorNode{seen: &seen}
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return invalidatorView{n: ln}
	}})
	for range 3 {
		if err := a.Update(geom.Sz(200, 100)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	if len(seen) < 1 {
		t.Fatalf("the layouter never ran")
	}
	// Function values are only comparable against nil, so the stability is
	// checked through the allocation count instead.
	if got := testing.AllocsPerRun(50, func() { ln.pull() }); got != 0 {
		t.Errorf("Invalidator allocated %v times per call after the first, want 0", got)
	}

	before := a.Diagnostics().Layouts
	if err := a.Update(geom.Sz(200, 100)); err != nil {
		t.Fatal(err)
	}
	if a.Diagnostics().Layouts != before {
		t.Fatal("the tree was not quiet before the test")
	}
	ln.invalidate()
	if err := a.Update(geom.Sz(200, 100)); err != nil {
		t.Fatal(err)
	}
	if a.Diagnostics().Layouts == before {
		t.Error("the invalidator did not produce a layout pass")
	}
}

type invalidatorNode struct {
	seen       *[]func()
	ctx        *gift.LayoutContext
	invalidate func()
}

func (n *invalidatorNode) pull() { n.invalidate = n.ctx.Invalidator() }

func (n *invalidatorNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.ctx = ctx
	n.invalidate = ctx.Invalidator()
	*n.seen = append(*n.seen, n.invalidate)
	return c.Constrain(geom.Sz(50, 50))
}

var invalidatorType = gift.RegisterType("test.Invalidator")

type invalidatorView struct{ n *invalidatorNode }

func (invalidatorView) ViewType() gift.TypeID { return invalidatorType }
func (v invalidatorView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: v.n}
}

// TestScrollInteractorIsSharedAndStateless pins the property that makes
// delegation affordable: obtaining the built in handler allocates nothing.
func TestScrollInteractorIsSharedAndStateless(t *testing.T) {
	if gift.ScrollInteractor() == nil {
		t.Fatal("ScrollInteractor returned nil")
	}
	if got := testing.AllocsPerRun(100, func() { _ = gift.ScrollInteractor() }); got != 0 {
		t.Errorf("ScrollInteractor allocated %v times per call, want 0", got)
	}
}
