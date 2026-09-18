package gift_test

import (
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// The core rules of [gift.TransitionSpec], written against gift's own element
// API rather than against ui's three navigation containers.
//
// They are here and not next to ui.TabBar because all three containers produce
// the same shape — a keyed layer with Hidden and a spec — so a test built out
// of them cannot tell "the rule holds" from "all three widgets make the same
// mistake". What is under test here is the rule.

var transType = gift.RegisterType("test.Transition")

// transLeaf is a node that paints one rectangle and may be hidden, with or
// without a transition.
type transLeaf struct {
	key      string
	hidden   bool
	spec     gift.TransitionSpec
	colour   render.Color
	children []gift.View
}

func (v transLeaf) ViewType() gift.TypeID { return transType }

func (v transLeaf) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:        v.key,
		Layouter:   fixedLayouter{geom.Sz(100, 50)},
		Painter:    transPainter{v.colour},
		Interactor: noopInteractor{},
		Hidden:     v.hidden,
		Transition: v.spec,
		Children:   v.children,
	}
}

// transPainter fills the node's bounds, so that a test can read back where the
// node was drawn through the display list's own transform table.
type transPainter struct{ c render.Color }

func (p transPainter) Paint(ctx *gift.PaintContext) {
	ctx.Add(render.Op{Kind: render.OpFillRect, Bounds: ctx.Bounds(), Color: p.c})
	ctx.PaintChildren()
}

var transColour = render.Color{R: 1, G: 0, B: 0, A: 1}

// drawnAt returns where the fill of the transitioning node landed on the
// device, and whether it was drawn at all.
func drawnAt(list *render.List) (geom.Rect, bool) {
	for _, op := range list.Ops() {
		if op.Kind == render.OpFillRect && op.Color == transColour {
			return list.Xform(op.Xform).TransformRect(op.Bounds), true
		}
	}
	return geom.Rect{}, false
}

// transApp drives one node whose Hidden flag a test flips, on a clock the test
// owns.
type transApp struct {
	app    *gift.App
	t      *testing.T
	now    time.Duration
	hidden bool
	spec   gift.TransitionSpec
}

func newTransApp(t *testing.T, spec gift.TransitionSpec, hidden bool) *transApp {
	ta := &transApp{t: t, hidden: hidden, spec: spec}
	ta.app = gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return transLeaf{key: "leaf", hidden: ta.hidden, spec: ta.spec, colour: transColour}
	}})
	ta.frame()
	return ta
}

// frame runs one whole frame on the current clock and returns the list.
func (ta *transApp) frame() *render.List {
	ta.t.Helper()
	ta.app.BeginInput(ta.now)
	if err := ta.app.Update(geom.Sz(200, 200)); err != nil {
		ta.t.Fatal(err)
	}
	return ta.app.Paint()
}

// advance moves the clock and runs a frame.
func (ta *transApp) advance(d time.Duration) *render.List {
	ta.t.Helper()
	ta.now += d
	return ta.frame()
}

// set flips the flag and runs the frame that applies it.
func (ta *transApp) set(hidden bool) *render.List {
	ta.t.Helper()
	ta.hidden = hidden
	ta.app.Invalidate()
	return ta.frame()
}

const testTransition = 180 * time.Millisecond

// TestANodeThatIsBornHiddenIsParkedAtOnceAndAsksForNoFrames is the rule
// [gift.ControlState.Armed] exists for one level down: the first value a node
// is given is adopted, not animated towards.
//
// Without it every inactive tab of an application would be seen sliding out of
// the window in the first frames after start-up, and the device would be held
// awake for a transition nobody asked for.
func TestANodeThatIsBornHiddenIsParkedAtOnceAndAsksForNoFrames(t *testing.T) {
	ta := newTransApp(t, gift.TransitionSpec{Parked: geom.Pt(1, 0), Duration: testTransition}, true)
	if _, ok := drawnAt(ta.frame()); ok {
		t.Error("a node that was born hidden was painted; it is parked, not leaving")
	}
	if d := ta.app.Diagnostics(); d.Animating {
		t.Errorf("a node that was born hidden left %d animation(s) running", d.Animations)
	}
}

// TestAHiddenNodeWithNoTransitionIsStillCutOutOfTheFrame pins the default. The
// zero [gift.TransitionSpec] is the behaviour every node had before
// transitions existed, and an application that declares none must not start
// paying for one.
func TestAHiddenNodeWithNoTransitionIsStillCutOutOfTheFrame(t *testing.T) {
	ta := newTransApp(t, gift.TransitionSpec{}, false)
	if _, ok := drawnAt(ta.frame()); !ok {
		t.Fatal("the visible node was not painted; nothing here measures anything")
	}
	if _, ok := drawnAt(ta.set(true)); ok {
		t.Error("a node with no transition was still painted in the frame that hid it")
	}
	if d := ta.app.Diagnostics(); d.Animating {
		t.Error("hiding a node with no transition started an animation")
	}
}

// TestATransitionMovesTheNodeAndThenStopsPaintingIt is the mechanism in one
// test: three positions and an end.
func TestATransitionMovesTheNodeAndThenStopsPaintingIt(t *testing.T) {
	ta := newTransApp(t, gift.TransitionSpec{Parked: geom.Pt(1, 0), Duration: testTransition}, false)
	atRest, ok := drawnAt(ta.frame())
	if !ok {
		t.Fatal("the visible node was not painted")
	}

	first, ok := drawnAt(ta.set(true))
	if !ok {
		t.Fatal("the node was cut out of the frame that hid it instead of leaving it")
	}
	if first != atRest {
		t.Errorf("the first frame of the exit drew the node at %v, want the place it was "+
			"standing, %v: a transition starts where the node is", first, atRest)
	}

	mid, ok := drawnAt(ta.advance(90 * time.Millisecond))
	if !ok {
		t.Fatal("the node stopped being painted halfway through its exit")
	}
	if !(mid.Min.X > atRest.Min.X && mid.Min.X < atRest.Min.X+atRest.Width()) {
		t.Errorf("halfway out the node is at x=%v, which is neither moved nor on its way to "+
			"one width along from x=%v", mid.Min.X, atRest.Min.X)
	}

	ta.advance(testTransition)
	if _, ok := drawnAt(ta.frame()); ok {
		t.Error("the node is still painted after its exit finished; the guarantee of " +
			"Element.Hidden is that it costs nothing per frame")
	}
	if d := ta.app.Diagnostics(); d.Animating {
		t.Errorf("%d animation(s) outlived the transition", d.Animations)
	}
}

// TestATransitionDoesNotMoveTheHitTest is the documented divergence of
// [gift.TransitionSpec], pinned so that it is a decision and not a drift: a
// node that is arriving answers at its final position from the first frame.
//
// It is the only place in gift where paint and input disagree about where a
// node is, and the argument for it — a kiosk user taps at the screen that is
// arriving, not at a moving target — is only worth anything if the disagreement
// is exactly this one and is deliberate.
func TestATransitionDoesNotMoveTheHitTest(t *testing.T) {
	ta := newTransApp(t, gift.TransitionSpec{Parked: geom.Pt(1, 0), Duration: testTransition}, false)
	ta.set(true)
	ta.advance(testTransition + 32*time.Millisecond)

	// Arriving: hidden goes false, so the node is drawn a full width to the
	// right of where it is hit tested.
	ta.set(false)
	drawn, ok := drawnAt(ta.frame())
	if !ok {
		t.Fatal("the arriving node was not painted")
	}
	if drawn.Min.X < 100 {
		t.Fatalf("the arriving node is drawn at x=%v, so it is not parked off to the side "+
			"and this test cannot tell the two positions apart", drawn.Min.X)
	}
	leaf := nodeByKey(t, ta.app, "leaf")
	if b := ta.app.NodeBounds(leaf); b.Min.X != 0 {
		t.Errorf("the arriving node's layout bounds are at x=%v, want 0: a transition is a "+
			"transform and must not move the layout", b.Min.X)
	}
	// The hit test answers where the node is *going*, not where it is drawn.
	if hit, ok := ta.app.HitTest(geom.Pt(50, 25)); !ok || hit != leaf {
		t.Errorf("a tap at the arriving node's final place hit %v (found=%v); a node that is "+
			"on its way in has to answer at the place it is arriving at, or a kiosk user "+
			"aiming at it is aiming at a moving target", ta.app.NodeKey(hit), ok)
	}
	if hit, ok := ta.app.HitTest(geom.Pt(drawn.Min.X+10, 25)); ok && hit == leaf {
		t.Errorf("a tap on the pixels of the arriving node, at x=%v, reached it; the hit "+
			"test must not follow the picture, because the picture moves under the finger",
			drawn.Min.X+10)
	}
}

// nodeByKey finds the one node with the given key, for the tests above.
func nodeByKey(t *testing.T, app *gift.App, key string) gift.NodeRef {
	t.Helper()
	var out gift.NodeRef
	var walk func(r gift.NodeRef)
	walk = func(r gift.NodeRef) {
		if app.NodeKey(r) == key {
			out = r
		}
		for _, c := range app.NodeChildren(r, nil) {
			walk(c)
		}
	}
	walk(app.Root())
	if out.IsZero() {
		t.Fatalf("no node with key %q", key)
	}
	return out
}
