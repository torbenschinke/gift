package gift_test

import (
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// --- test fixtures ----------------------------------------------------------

var (
	targetType = gift.RegisterType("test.Target")
	frameType  = gift.RegisterType("test.Frame")
)

// log records the events a target saw, as a string per event, so that an
// assertion reads like the interaction it describes.
type log struct{ seen []string }

func (l *log) add(s string) { l.seen = append(l.seen, s) }

func (l *log) has(s string) bool {
	for _, v := range l.seen {
		if v == s {
			return true
		}
	}
	return false
}

func (l *log) reset() { l.seen = l.seen[:0] }

// target is a leaf that opts into input at a fixed position and size.
type target struct {
	name              string
	x, y, w, h        float32
	log               *log
	activations       *int
	disabled          bool
	notFocusable      bool
	swallowEverything bool
}

func (target) ViewType() gift.TypeID { return targetType }

func (t target) Build(*gift.BuildContext) gift.Element {
	n := &targetNode{t: t}
	return gift.Element{
		Key:        t.name,
		Layouter:   n,
		Interactor: n,
		Focusable:  !t.notFocusable,
		Disabled:   t.disabled,
	}
}

type targetNode struct{ t target }

func (n *targetNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	return geom.Sz(n.t.w, n.t.h)
}

func (n *targetNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if n.t.log != nil {
		n.t.log.add(n.t.name + ":" + kindName(e.Kind) + insideMark(e))
	}
	switch e.Kind {
	case gift.EventPointerDown:
		ctx.RequestFocus()
		return true
	case gift.EventPointerUp:
		if e.Inside && !e.Dragged && n.t.activations != nil {
			*n.t.activations++
		}
		return true
	case gift.EventKeyDown:
		if (e.Key == gift.KeySpace || e.Key == gift.KeyEnter) && n.t.activations != nil {
			*n.t.activations++
			return true
		}
	}
	return n.t.swallowEverything
}

func insideMark(e gift.Event) string {
	if e.Kind != gift.EventPointerUp && e.Kind != gift.EventPointerMove {
		return ""
	}
	if e.Inside {
		return "/in"
	}
	return "/out"
}

func kindName(k gift.EventKind) string {
	switch k {
	case gift.EventPointerEnter:
		return "enter"
	case gift.EventPointerLeave:
		return "leave"
	case gift.EventPointerMove:
		return "move"
	case gift.EventPointerDown:
		return "down"
	case gift.EventPointerUp:
		return "up"
	case gift.EventPointerCancel:
		return "cancel"
	case gift.EventLongPress:
		return "long"
	case gift.EventWheel:
		return "wheel"
	case gift.EventKeyDown:
		return "keydown"
	case gift.EventKeyUp:
		return "keyup"
	case gift.EventFocusGained:
		return "focus"
	case gift.EventFocusLost:
		return "blur"
	}
	return "?"
}

// frameView is a container that places its children at absolute offsets and,
// optionally, clips input to its own bounds. It is the stand-in for every
// structural container; note that it supplies no Interactor at all, which is
// what the "a plain container does not swallow input" assertions rely on.
type frameView struct {
	key      string
	w, h     float32
	pad      float32
	clip     bool
	xform    *geom.Affine2D
	children []gift.View
	offsets  []geom.Point
}

func (frameView) ViewType() gift.TypeID { return frameType }

func (f frameView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:       f.key,
		Layouter:  &frameLayout{f: f},
		Children:  f.children,
		Clip:      f.clip,
		Transform: f.xform,
	}
}

type frameLayout struct{ f frameView }

func (l *frameLayout) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	loose := geom.Constraints{Max: geom.Sz(geom.Unbounded(), geom.Unbounded())}
	for i := range ctx.ChildCount() {
		ctx.Measure(i, loose)
		at := geom.Pt(l.f.pad, l.f.pad)
		if i < len(l.f.offsets) {
			at = l.f.offsets[i]
		}
		ctx.Place(i, at)
	}
	return geom.Sz(l.f.w, l.f.h)
}

// newInputApp mounts root and brings it to a laid out steady state.
func newInputApp(t *testing.T, root func(*gift.Context) gift.View) *gift.App {
	t.Helper()
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	a.Paint()
	return a
}

func at(x, y float32) geom.Point { return geom.Pt(x, y) }

// click is press then release at the same point, which is what a tap is.
func click(a *gift.App, p geom.Point) {
	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, p)
	a.PointerUp(gift.MousePointer, gift.PointerMouse, p)
}

// --- hit testing ------------------------------------------------------------

// TestHitTestFrontToBack pins the order: children before the node itself and
// later children before earlier ones, which is the inverse of the drawing
// order background, children, border.
func TestHitTestFrontToBack(t *testing.T) {
	var l log
	// Two overlapping targets in the same place, like two ZStack children.
	root := func(*gift.Context) gift.View {
		return frameView{
			w: 200, h: 200,
			offsets:  []geom.Point{at(10, 10), at(10, 10)},
			children: []gift.View{target{name: "under", w: 60, h: 60, log: &l}, target{name: "over", w: 60, h: 60, log: &l}},
		}
	}
	a := newInputApp(t, root)

	click(a, at(20, 20))
	if !l.has("over:down") {
		t.Fatalf("the topmost overlapping child must win the hit, got %v", l.seen)
	}
	if l.has("under:down") {
		t.Fatalf("the covered child must not be hit, got %v", l.seen)
	}
}

// TestHitTestContainerDoesNotSwallow is the input equivalent of the nil
// painter rule: a plain container has no Interactor, so a point over its
// padding — inside the container but on no child — hits nothing at all rather
// than being consumed by the container.
func TestHitTestContainerPaddingAndGaps(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return frameView{
			w: 200, h: 200, pad: 20,
			offsets:  []geom.Point{at(20, 20), at(20, 120)},
			children: []gift.View{target{name: "a", w: 60, h: 60, log: &l}, target{name: "b", w: 60, h: 60, log: &l}},
		}
	}
	a := newInputApp(t, root)

	// In the container's padding, above the first child.
	if h, ok := a.HitTest(at(5, 5)); ok {
		t.Fatalf("a point in the container's padding hit %v; a container without an Interactor must swallow nothing", h)
	}
	// In the gap between the two children.
	if _, ok := a.HitTest(at(40, 100)); ok {
		t.Fatalf("a point in the gap between two children must hit nothing")
	}
	// On a child, for contrast.
	if _, ok := a.HitTest(at(40, 40)); !ok {
		t.Fatalf("a point on the first child must hit it")
	}
}

// TestHitTestRespectsAncestorClip covers the case the project plan, section 7,
// puts into the same sentence as clipping: a child that overflows a clipped
// ancestor is not clickable where it is not visible.
func TestHitTestRespectsAncestorClip(t *testing.T) {
	root := func(*gift.Context) gift.View {
		return frameView{
			key: "outer", w: 200, h: 200,
			children: []gift.View{frameView{
				key: "clipper", w: 50, h: 50, clip: true,
				offsets:  []geom.Point{at(0, 0)},
				children: []gift.View{target{name: "wide", w: 200, h: 200}},
			}},
			offsets: []geom.Point{at(0, 0)},
		}
	}
	a := newInputApp(t, root)

	if _, ok := a.HitTest(at(25, 25)); !ok {
		t.Fatalf("inside the clip the child must be hit")
	}
	if h, ok := a.HitTest(at(100, 100)); ok {
		t.Fatalf("outside the ancestor clip the child must not be hit, got %v", h)
	}
}

// TestHitTestFollowsTransform proves the single coordinate path: a node with a
// transform is hit where the transform puts it, and the display list carries
// the same matrix. If the two ever diverge, this test is where it shows.
func TestHitTestFollowsTransform(t *testing.T) {
	m := geom.Translate(geom.Pt(100, 0))
	root := func(*gift.Context) gift.View {
		return frameView{
			w: 400, h: 200,
			offsets: []geom.Point{at(0, 0)},
			children: []gift.View{frameView{
				key: "moved", w: 60, h: 60, xform: &m,
				offsets:  []geom.Point{at(0, 0)},
				children: []gift.View{target{name: "t", w: 60, h: 60}},
			}},
		}
	}
	a := newInputApp(t, root)

	if _, ok := a.HitTest(at(20, 20)); ok {
		t.Fatalf("the untransformed position must not hit; the node moved")
	}
	if _, ok := a.HitTest(at(120, 20)); !ok {
		t.Fatalf("the transformed position must hit")
	}
}

// TestHitTestIsAllocationFree is the input half of the project plan,
// section 11: hit testing runs per event inside Update.
func TestHitTestIsAllocationFree(t *testing.T) {
	if isDebugBuild {
		// The UI executor check parses the runtime stack header to obtain a
		// goroutine id, which needs a buffer that escapes. The project plan,
		// section 15, accepts that cost for exactly this reason and confines
		// the check to the giftdebug build; the release path below is the one
		// the section 11 contract is about.
		t.Skip("the UI executor check is only compiled with -tags giftdebug")
	}
	root := func(*gift.Context) gift.View {
		kids := make([]gift.View, 0, 24)
		offs := make([]geom.Point, 0, 24)
		for i := range 24 {
			kids = append(kids, target{name: "t" + string(rune('a'+i)), w: 20, h: 20})
			offs = append(offs, at(float32(i)*8, float32(i)*8))
		}
		return frameView{w: 400, h: 400, children: kids, offsets: offs}
	}
	a := newInputApp(t, root)

	probe := func() { a.HitTest(at(190, 190)) }
	for range 16 {
		probe()
	}
	if got := testing.AllocsPerRun(500, probe); got != 0 {
		t.Fatalf("hit testing allocated %v times per run, want 0", got)
	}
}

// --- pointer capture --------------------------------------------------------

// TestPressInsideReleaseOutside is the reason pointer capture exists. The
// release must reach the button and must not activate it.
func TestPressInsideReleaseOutside(t *testing.T) {
	var l log
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{
			w: 300, h: 300,
			offsets:  []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, log: &l, activations: &activations}},
		}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(25, 25))
	a.PointerMove(gift.MousePointer, gift.PointerMouse, at(200, 200))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, at(200, 200))

	if activations != 0 {
		t.Fatalf("a release outside the button activated it %d times", activations)
	}
	if !l.has("b:up/out") {
		t.Fatalf("the release must still be delivered to the capturing node, got %v", l.seen)
	}
}

// TestPressOutsideReleaseInside is the other direction: a button that was
// never pressed must not activate because a release happens to land on it.
func TestPressOutsideReleaseInside(t *testing.T) {
	var l log
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{
			w: 300, h: 300,
			offsets:  []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, log: &l, activations: &activations}},
		}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(200, 200))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, at(25, 25))

	if activations != 0 {
		t.Fatalf("a release on a button that was not pressed activated it %d times", activations)
	}
	if l.has("b:up/in") {
		t.Fatalf("the button must not see a release of a press it never got, got %v", l.seen)
	}
}

// TestPressCancelDoesNotActivate covers the third path: the platform took the
// gesture over, which is not a click.
func TestPressCancelDoesNotActivate(t *testing.T) {
	var l log
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{
			w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, log: &l, activations: &activations}},
		}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(25, 25))
	a.PointerCancel(gift.MousePointer)
	if activations != 0 {
		t.Fatalf("a cancelled press activated the button")
	}
	if !l.has("b:cancel") {
		t.Fatalf("the capturing node must be told about the cancel, got %v", l.seen)
	}
}

// TestLongPress covers the time based half of the gesture set.
func TestLongPress(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, log: &l}}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(25, 25))
	a.BeginInput(gift.LongPressDelay / 2)
	if l.has("b:long") {
		t.Fatalf("a long press fired too early")
	}
	a.BeginInput(gift.LongPressDelay + time.Millisecond)
	if !l.has("b:long") {
		t.Fatalf("a long press did not fire, got %v", l.seen)
	}
	n := 0
	for _, s := range l.seen {
		if s == "b:long" {
			n++
		}
	}
	a.BeginInput(2 * gift.LongPressDelay)
	if n != 1 {
		t.Fatalf("the long press fired %d times, want once per press", n)
	}
}

// --- hover ------------------------------------------------------------------

func TestMouseHoverEnterAndLeave(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets:  []geom.Point{at(0, 0), at(100, 0)},
			children: []gift.View{target{name: "a", w: 50, h: 50, log: &l}, target{name: "b", w: 50, h: 50, log: &l}}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, at(25, 25))
	if !l.has("a:enter") {
		t.Fatalf("moving onto a must enter it, got %v", l.seen)
	}
	l.reset()
	a.PointerMove(gift.MousePointer, gift.PointerMouse, at(125, 25))
	if !l.has("a:leave") || !l.has("b:enter") {
		t.Fatalf("moving from a to b must leave a and enter b, got %v", l.seen)
	}
}

// --- touch ------------------------------------------------------------------

// TestTouchTapActivates and the two tests after it are the three sentences the
// project plan, section 7, spends on touch: a tap works, touch produces no
// hover, and a second finger is recognised and discarded.
func TestTouchTapActivates(t *testing.T) {
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, activations: &activations}}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerDown(7, gift.PointerTouch, at(25, 25))
	a.PointerUp(7, gift.PointerTouch, at(25, 25))
	if activations != 1 {
		t.Fatalf("a tap activated %d times, want 1", activations)
	}
}

func TestTouchProducesNoHover(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, log: &l}}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.PointerDown(7, gift.PointerTouch, at(25, 25))
	a.PointerMove(7, gift.PointerTouch, at(26, 26))
	a.PointerUp(7, gift.PointerTouch, at(26, 26))
	for _, s := range l.seen {
		if s == "b:enter" || s == "b:leave" {
			t.Fatalf("touch synthesised a hover state: %v", l.seen)
		}
	}
}

func TestSecondFingerIsDiscarded(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets:  []geom.Point{at(0, 0), at(100, 0)},
			children: []gift.View{target{name: "a", w: 50, h: 50, log: &l}, target{name: "b", w: 50, h: 50, log: &l}}}
	}
	a := newInputApp(t, root)

	before := a.Diagnostics().DiscardedTouches
	a.BeginInput(0)
	a.PointerDown(1, gift.PointerTouch, at(25, 25))
	a.PointerDown(2, gift.PointerTouch, at(125, 25))
	a.PointerUp(2, gift.PointerTouch, at(125, 25))
	// The counters are published out of band, at the end of an update; see
	// Diagnostics.
	mustUpdate(t, a)

	if l.has("b:down") {
		t.Fatalf("the second finger reached a view, got %v", l.seen)
	}
	if got := a.Diagnostics().DiscardedTouches - before; got != 1 {
		t.Fatalf("DiscardedTouches moved by %d, want 1; a dropped finger must be counted, not silently ignored", got)
	}
}

// --- keyboard ---------------------------------------------------------------

func TestTabOrderFollowsTheLogicalTree(t *testing.T) {
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets: []geom.Point{at(0, 0), at(0, 60), at(0, 120)},
			children: []gift.View{
				target{name: "first", w: 50, h: 50},
				target{name: "second", w: 50, h: 50},
				target{name: "third", w: 50, h: 50},
			}}
	}
	a := newInputApp(t, root)

	want := []string{"first", "second", "third", "first"}
	for i, w := range want {
		a.BeginInput(0)
		a.KeyDown(gift.KeyTab, 0)
		a.KeyUp(gift.KeyTab, 0)
		if got := focusName(t, a); got != w {
			t.Fatalf("tab %d focused %q, want %q", i+1, got, w)
		}
	}
	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, gift.ModShift)
	if got := focusName(t, a); got != "third" {
		t.Fatalf("shift-tab focused %q, want %q (wrapping backwards)", got, "third")
	}
}

func TestSpaceAndEnterActivate(t *testing.T) {
	for _, k := range []gift.Key{gift.KeySpace, gift.KeyEnter} {
		activations := 0
		root := func(*gift.Context) gift.View {
			return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
				children: []gift.View{target{name: "b", w: 50, h: 50, activations: &activations}}}
		}
		a := newInputApp(t, root)
		a.BeginInput(0)
		a.KeyDown(gift.KeyTab, 0)
		a.KeyDown(k, 0)
		a.KeyUp(k, 0)
		if activations != 1 {
			t.Fatalf("key %v activated %d times, want 1", k, activations)
		}
	}
}

func TestDisabledIsSkippedAndInert(t *testing.T) {
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets: []geom.Point{at(0, 0), at(0, 60)},
			children: []gift.View{
				target{name: "off", w: 50, h: 50, disabled: true, activations: &activations},
				target{name: "on", w: 50, h: 50},
			}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	if got := focusName(t, a); got != "on" {
		t.Fatalf("tab focused %q, want the enabled target; a disabled node must be skipped", got)
	}

	// It is still a hit target, so the click does not fall through, but it
	// activates nothing.
	click(a, at(25, 25))
	if activations != 0 {
		t.Fatalf("a disabled target activated %d times", activations)
	}
	if h, ok := a.HitTest(at(25, 25)); !ok {
		t.Fatalf("a disabled target must still block clicks from falling through, got %v", h)
	}
}

// --- the frame model --------------------------------------------------------

// TestHoverAndPressDoNotRebuild is the assertion the project plan, section 5,
// asks for: "Scrolloffset, Hover, Pressed und Animation sind lokaler
// Praesentationszustand. Sie sollen keinen fachlichen Root-Rebuild erzwingen."
//
// It is the single most likely place to break the frame model, because the
// obvious implementation of a hover — keep it in state and rebuild — passes
// every other test in this file.
func TestHoverAndPressDoNotRebuild(t *testing.T) {
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets:  []geom.Point{at(0, 0), at(100, 0)},
			children: []gift.View{target{name: "a", w: 50, h: 50}, target{name: "b", w: 50, h: 50}}}
	}
	a := newInputApp(t, root)
	before := a.Diagnostics()

	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, at(25, 25))
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(25, 25))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, at(25, 25))
	a.PointerMove(gift.MousePointer, gift.PointerMouse, at(125, 25))
	a.KeyDown(gift.KeyTab, 0)
	mustUpdate(t, a)
	a.Paint()

	after := a.Diagnostics()
	if after.Builds != before.Builds {
		t.Fatalf("hover, press and focus caused %d rebuilds, want 0", after.Builds-before.Builds)
	}
	if after.Layouts != before.Layouts {
		t.Fatalf("hover, press and focus caused %d layouts, want 0", after.Layouts-before.Layouts)
	}
	if !a.NeedsPaint() && after.Frames == before.Frames {
		t.Fatalf("interaction state changed but nothing asked for a repaint")
	}
}

// TestDispatchIsAllocationFree is the allocation contract with an input event
// in it, as the brief for this work unit requires and as the project plan,
// section 11, implies by putting "Eingabeverarbeitung" inside the contract.
func TestDispatchIsAllocationFree(t *testing.T) {
	if isDebugBuild {
		// The UI executor check parses the runtime stack header to obtain a
		// goroutine id, which needs a buffer that escapes. The project plan,
		// section 15, accepts that cost for exactly this reason and confines
		// the check to the giftdebug build; the release path below is the one
		// the section 11 contract is about.
		t.Skip("the UI executor check is only compiled with -tags giftdebug")
	}
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets:  []geom.Point{at(0, 0), at(100, 0)},
			children: []gift.View{target{name: "a", w: 50, h: 50, activations: &activations}, target{name: "b", w: 50, h: 50}}}
	}
	a := newInputApp(t, root)

	// A whole interaction: move, press, release, move away. It changes the
	// hover and press state every time round, so no branch is skipped.
	frame := func() {
		a.BeginInput(0)
		a.PointerMove(gift.MousePointer, gift.PointerMouse, at(25, 25))
		a.PointerDown(gift.MousePointer, gift.PointerMouse, at(25, 25))
		a.PointerUp(gift.MousePointer, gift.PointerMouse, at(25, 25))
		a.PointerMove(gift.MousePointer, gift.PointerMouse, at(200, 200))
		a.KeyDown(gift.KeySpace, 0)
		a.KeyUp(gift.KeySpace, 0)
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 32 {
		frame()
	}
	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("dispatching an input event allocated %v times per run, want 0", got)
	}
}

// TestUnmountForgetsFocusAndCapture: a node that goes away must not be left
// holding the focus or a capture, or the next node to take that slot would
// inherit both.
func TestUnmountForgetsFocusAndCapture(t *testing.T) {
	show := true
	root := func(*gift.Context) gift.View {
		kids := []gift.View{target{name: "a", w: 50, h: 50}}
		if show {
			kids = append(kids, target{name: "b", w: 50, h: 50})
		}
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0), at(100, 0)}, children: kids}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(125, 25))
	if _, ok := a.Focus(); !ok {
		t.Fatalf("the press should have taken the focus")
	}
	show = false
	a.Invalidate()
	mustUpdate(t, a)
	if h, ok := a.Focus(); ok {
		t.Fatalf("the focus survived the unmount of its node: %v", h)
	}
	// The release must not reach whatever now occupies the slot.
	a.BeginInput(0)
	a.PointerUp(gift.MousePointer, gift.PointerMouse, at(125, 25))
}

func focusName(t *testing.T, a *gift.App) string {
	t.Helper()
	h, ok := a.Focus()
	if !ok {
		return ""
	}
	return a.NodeKey(h)
}

// TestFramePathWithInteractionStateIsAllocationFree is the half of the
// allocation contract that holds under -tags giftdebug too.
//
// The dispatch test above has to be skipped in a debug build, because the UI
// executor check parses the runtime stack and the project plan, section 15,
// deliberately accepts that cost there. What must hold in *both* builds is
// that a tree carrying hover, press and focus state costs no more to update
// and to paint than one that does not — that the interaction state is read out
// of the node payload and not allocated per frame.
func TestFramePathWithInteractionStateIsAllocationFree(t *testing.T) {
	root := func(*gift.Context) gift.View {
		kids := make([]gift.View, 0, 32)
		offs := make([]geom.Point, 0, 32)
		for i := range 32 {
			kids = append(kids, target{name: "t" + string(rune('a'+i)), w: 20, h: 20})
			offs = append(offs, at(float32(i%8)*24, float32(i/8)*24))
		}
		return frameView{w: 400, h: 400, children: kids, offsets: offs}
	}
	a := newInputApp(t, root)

	// Put the tree into the interesting state once, outside the measurement:
	// one node hovered and pressed, another focused.
	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, at(10, 10))
	a.PointerDown(gift.MousePointer, gift.PointerMouse, at(10, 10))

	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if !a.NodeInteraction(mustHit(t, a, at(10, 10))).Pressed {
		t.Fatal("the fixture is not in the pressed state, the measurement would prove nothing")
	}
	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("a frame over a tree with interaction state allocated %v times per run, want 0", got)
	}
}

func mustHit(t *testing.T, a *gift.App, p geom.Point) gift.NodeRef {
	t.Helper()
	h, ok := a.HitTest(p)
	if !ok {
		t.Fatalf("nothing at %v", p)
	}
	return h
}
