package gift_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// The tests of the three focus rules a human found broken while driving the
// real kitchen sink binary: a focus ring that a finger leaves behind, a press
// that does not blur what it lands next to, and a key that reaches nobody
// because nothing on a touch panel is ever focused.
//
// They live beside input_test.go and use its fixtures; see [target].

// focusVisible reports whether the node holding the focus is showing a ring.
// It reads the node's own [gift.Interaction], which is what a painter reads,
// rather than the App level answer, so a test cannot pass because the two
// disagree.
func focusVisible(t *testing.T, a *gift.App) bool {
	t.Helper()
	h, ok := a.Focus()
	if !ok {
		return false
	}
	ia := a.NodeInteraction(h)
	if !ia.Focused {
		t.Fatalf("the focused node does not carry Interaction.Focused")
	}
	if ia.FocusVisible != a.FocusVisible() {
		t.Fatalf("the node says FocusVisible=%v and App.FocusVisible says %v",
			ia.FocusVisible, a.FocusVisible())
	}
	return ia.FocusVisible
}

func twoTargets(t *testing.T, second gift.View) *gift.App {
	t.Helper()
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300,
			offsets: []geom.Point{at(0, 0), at(100, 0)},
			children: []gift.View{
				target{name: "first", w: 50, h: 50},
				second,
			}}
	}
	return newInputApp(t, root)
}

// TestAPointerPressTakesTheFocusWithoutShowingTheRing is the first half of the
// :focus-visible rule of [gift.Interaction.FocusVisible]: a touch or a click
// moves the focus, because that is where the keys have to go, and leaves no
// ring, because the user is not using keys.
func TestAPointerPressTakesTheFocusWithoutShowingTheRing(t *testing.T) {
	a := twoTargets(t, target{name: "second", w: 50, h: 50})

	a.BeginInput(0)
	a.PointerDown(1, gift.PointerTouch, at(125, 25))
	a.PointerUp(1, gift.PointerTouch, at(125, 25))

	if got := focusName(t, a); got != "second" {
		t.Fatalf("the tap focused %q, want %q; a press has to move the focus", got, "second")
	}
	if focusVisible(t, a) {
		t.Fatal("a tap left a focus ring behind. On a touchscreen kiosk every tap would " +
			"then leave one, which is the defect Interaction.FocusVisible exists to fix")
	}
}

// TestKeyboardFocusShowsTheRing is the other half: the focus that arrives from
// the keyboard is the one a keyboard user has to be able to see.
func TestKeyboardFocusShowsTheRing(t *testing.T) {
	a := twoTargets(t, target{name: "second", w: 50, h: 50})

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)

	if got := focusName(t, a); got != "first" {
		t.Fatalf("tab focused %q, want %q", got, "first")
	}
	if !focusVisible(t, a) {
		t.Fatal("tab moved the focus and showed no ring; the framework is then not " +
			"keyboard operable, because nothing on the screen says where the keys go")
	}
}

// TestTouchingTheNodeThatTabReachedTakesTheRingAwayAgain is the transition
// between the two, in the direction that has no focus change to hang off: the
// node does not change, only the provenance does.
func TestTouchingTheNodeThatTabReachedTakesTheRingAwayAgain(t *testing.T) {
	a := twoTargets(t, target{name: "second", w: 50, h: 50})

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	if !focusVisible(t, a) {
		t.Fatal("tab did not show the ring; the fixture is wrong")
	}

	a.BeginInput(0)
	a.PointerDown(1, gift.PointerTouch, at(25, 25))
	a.PointerUp(1, gift.PointerTouch, at(25, 25))

	if got := focusName(t, a); got != "first" {
		t.Fatalf("the tap moved the focus to %q; it was already on %q", got, "first")
	}
	if focusVisible(t, a) {
		t.Fatal("touching the node the keyboard had reached kept its ring; the ring is " +
			"then not a statement about the keyboard but a scar of having once used one")
	}
}

// TestFocusRequestedFromAKeyHandlerIsVisible covers the route a widget takes
// rather than the route gift takes: [gift.EventContext.RequestFocus] out of a
// key handler is keyboard focus even though nothing called MoveFocus.
func TestFocusRequestedFromAKeyHandlerIsVisible(t *testing.T) {
	root := func(*gift.Context) gift.View {
		return keyFocusFrame{name: "frame", child: target{name: "leaf", w: 50, h: 50}}
	}
	a := newInputApp(t, root)

	// Focus the leaf with a finger, so the ring is off, and then press a key.
	// The leaf declines it, it bubbles to the frame, and the frame takes the
	// focus from inside its key handler.
	a.BeginInput(0)
	a.PointerDown(1, gift.PointerTouch, at(25, 25))
	a.PointerUp(1, gift.PointerTouch, at(25, 25))
	if got := focusName(t, a); got != "leaf" {
		t.Fatalf("the tap focused %q, want %q; the fixture is wrong", got, "leaf")
	}
	if focusVisible(t, a) {
		t.Fatal("the pointer press showed a ring; the fixture is wrong")
	}

	a.BeginInput(0)
	a.KeyDown(gift.KeyDown, 0)

	if got := focusName(t, a); got != "frame" {
		t.Fatalf("the key handler moved the focus to %q, want %q", got, "frame")
	}
	if !focusVisible(t, a) {
		t.Fatal("a focus taken from inside a key handler is keyboard focus and has to " +
			"show a ring; gift decides that from the phase it is in, not from the caller")
	}
}

// keyFocusFrame is a focusable container that grabs the focus from its own
// key handler, which is the "an arrow key moves the selection here" shape.
type keyFocusFrame struct {
	name  string
	child gift.View
}

func (keyFocusFrame) ViewType() gift.TypeID { return frameType }

func (t keyFocusFrame) Build(*gift.BuildContext) gift.Element {
	n := &keyFocusNode{}
	return gift.Element{
		Key: t.name, Layouter: n, Interactor: n, Focusable: true,
		Children: []gift.View{t.child},
	}
}

type keyFocusNode struct{}

func (n *keyFocusNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	if ctx.ChildCount() > 0 {
		ctx.Measure(0, c)
		ctx.Place(0, geom.Pt(0, 0))
	}
	return geom.Sz(300, 300)
}

func (n *keyFocusNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if e.Kind == gift.EventKeyDown {
		ctx.RequestFocus()
		return true
	}
	return false
}

// TestAPressOnANodeThatCannotTakeTheFocusClearsIt is the sentence
// [gift.App.setFocus] has always carried and the code did not honour: a press
// on a hit target that is not focusable blurs, exactly as a press on empty
// space does.
//
// Without it a kiosk cannot put gift's on-screen keyboard away by tapping next
// to the field, because the tap lands on the scroll container of the form,
// which is a hit target and is not focusable, and the field keeps the focus
// and the keyboard with it.
func TestAPressOnANodeThatCannotTakeTheFocusClearsIt(t *testing.T) {
	a := twoTargets(t, passiveTarget{name: "second", w: 50, h: 50})

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	if got := focusName(t, a); got != "first" {
		t.Fatalf("tab focused %q, want %q; the fixture is wrong", got, "first")
	}

	a.BeginInput(0)
	a.PointerDown(1, gift.PointerTouch, at(125, 25))
	a.PointerUp(1, gift.PointerTouch, at(125, 25))

	if _, ok := a.Focus(); ok {
		t.Fatalf("pressing a node that cannot take the focus left it on %q", focusName(t, a))
	}
}

// passiveTarget is a hit target that is not focusable and does not ask for the
// focus either — a scroll container, a row, a card: most of a screen. It is
// deliberately *not* [target] with notFocusable set, because that one calls
// [gift.EventContext.RequestFocus] on every press, and a request for a node
// that cannot be focused already cleared the focus before this fix. The whole
// defect is about the presses that ask for nothing.
type passiveTarget struct {
	name string
	w, h float32
}

func (passiveTarget) ViewType() gift.TypeID { return targetType }

func (t passiveTarget) Build(*gift.BuildContext) gift.Element {
	n := &passiveNode{t: t}
	return gift.Element{Key: t.name, Layouter: n, Interactor: n}
}

type passiveNode struct{ t passiveTarget }

func (n *passiveNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(n.t.w, n.t.h)
}

func (n *passiveNode) HandleEvent(*gift.EventContext, gift.Event) bool { return true }

// TestPreservesFocusKeepsTheFocusWhereItIs is the opt out that keeps the
// previous rule from breaking the one surface that types into the focused node
// instead of being focused itself; see [gift.Element.PreservesFocus].
func TestPreservesFocusKeepsTheFocusWhereItIs(t *testing.T) {
	a := twoTargets(t, keeperTarget{name: "second", w: 50, h: 50})

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	if got := focusName(t, a); got != "first" {
		t.Fatalf("tab focused %q, want %q; the fixture is wrong", got, "first")
	}

	a.BeginInput(0)
	a.PointerDown(1, gift.PointerTouch, at(125, 25))

	if got := focusName(t, a); got != "first" {
		t.Fatalf("a press on a PreservesFocus node moved the focus to %q; the surface that "+
			"types into the focused node would then blur it with its first key", got)
	}
}

// keeperTarget is a non focusable hit target that declares
// [gift.Element.PreservesFocus]; ui.OnScreenKeyboard is the real one.
type keeperTarget struct {
	name string
	w, h float32
}

func (keeperTarget) ViewType() gift.TypeID { return targetType }

func (t keeperTarget) Build(*gift.BuildContext) gift.Element {
	n := &keeperNode{t: t}
	return gift.Element{Key: t.name, Layouter: n, Interactor: n, PreservesFocus: true}
}

type keeperNode struct{ t keeperTarget }

func (n *keeperNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(n.t.w, n.t.h)
}

func (n *keeperNode) HandleEvent(*gift.EventContext, gift.Event) bool { return true }

// --- the unfocused key ------------------------------------------------------

// TestAKeyWithNothingFocusedGoesToTheKeyFallbackNode is defect 4 in the small:
// on a touch panel nothing is ever focused, so a key that is only ever
// delivered to the focused node reaches nobody. [gift.Element.KeyFallback] is
// where it goes instead.
func TestAKeyWithNothingFocusedGoesToTheKeyFallbackNode(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return fallbackFrame{log: &l, fallback: true,
			child: target{name: "leaf", w: 50, h: 50, notFocusable: true}}
	}
	a := newInputApp(t, root)

	if _, ok := a.Focus(); ok {
		t.Fatal("something holds the focus; the fixture is wrong")
	}
	a.BeginInput(0)
	a.KeyDown(gift.KeyEscape, 0)

	if !l.has("frame:keydown") {
		t.Fatalf("an unfocused escape reached nobody, saw %v. A navigation stack that tells "+
			"the user to press escape then does nothing when they do", l.seen)
	}
}

// TestAKeyWithNothingFocusedAndNoFallbackReachesNobody is the other side of
// the same rule: the flag is opt in, and an application that never declares it
// keeps the behaviour it had.
func TestAKeyWithNothingFocusedAndNoFallbackReachesNobody(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return fallbackFrame{log: &l, fallback: false,
			child: target{name: "leaf", w: 50, h: 50, notFocusable: true}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.KeyDown(gift.KeyEscape, 0)

	if l.has("frame:keydown") {
		t.Fatalf("a node that did not declare KeyFallback received an unfocused key: %v", l.seen)
	}
}

// TestTheFocusedNodeStillGetsTheKeyBeforeAnyFallback states the precedence:
// the fallback is what happens when there is no focus, and not a second
// recipient.
func TestTheFocusedNodeStillGetsTheKeyBeforeAnyFallback(t *testing.T) {
	var l log
	root := func(*gift.Context) gift.View {
		return fallbackFrame{log: &l, fallback: true,
			child: target{name: "leaf", w: 50, h: 50, log: &l, swallowEverything: true}}
	}
	a := newInputApp(t, root)

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	if got := focusName(t, a); got != "leaf" {
		t.Fatalf("tab focused %q, want %q; the fixture is wrong", got, "leaf")
	}
	l.reset()

	a.BeginInput(0)
	a.KeyDown(gift.KeyEscape, 0)

	if !l.has("leaf:keydown") {
		t.Fatalf("the focused node did not get the key, saw %v", l.seen)
	}
	if l.has("frame:keydown") {
		t.Fatalf("the key was delivered to the focused node *and* to the fallback: %v", l.seen)
	}
}

// fallbackFrame is a one child frame that logs the keys it sees and optionally
// declares [gift.Element.KeyFallback]. ui.NavigationStack is the real one.
type fallbackFrame struct {
	log      *log
	fallback bool
	child    gift.View
}

func (fallbackFrame) ViewType() gift.TypeID { return frameType }

func (f fallbackFrame) Build(*gift.BuildContext) gift.Element {
	n := &fallbackNode{log: f.log}
	return gift.Element{
		Key:         "frame",
		Layouter:    n,
		Interactor:  n,
		KeyFallback: f.fallback,
		Children:    []gift.View{f.child},
	}
}

type fallbackNode struct{ log *log }

func (n *fallbackNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	if ctx.ChildCount() > 0 {
		ctx.Measure(0, c)
		ctx.Place(0, geom.Pt(0, 0))
	}
	return geom.Sz(300, 300)
}

// HandleEvent answers only for escape, which is what ui.NavigationStack's own
// interactor does. A fallback that consumed every key would swallow the tab
// that has to reach [gift.App.MoveFocus] while nothing is focused, and the
// keyboard could then never get started.
func (n *fallbackNode) HandleEvent(_ *gift.EventContext, e gift.Event) bool {
	if e.Kind == gift.EventKeyDown && e.Key == gift.KeyEscape {
		n.log.add("frame:" + kindName(e.Kind))
		return true
	}
	return false
}
