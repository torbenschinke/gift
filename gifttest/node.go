package gifttest

import (
	"fmt"
	"time"
	"unicode"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// touchID is the pointer identifier every touch action uses. gift only ever
// compares it for equality, and a second simultaneous finger is out of scope
// by the project plan, section 7, so one constant is enough.
const touchID gift.PointerID = 1

// dragSteps is how many intermediate moves a drag or a swipe emits.
//
// One move would be a teleport, and a gesture recogniser that measures
// velocity or crosses a slop threshold would see something no finger can do.
// Eight is enough to cross [gift.DragSlop] gradually at any plausible distance
// and few enough that the frames it costs are invisible in a test run.
const dragSteps = 8

// Node is one node of the retained tree, as a test refers to it.
//
// It is a value: copy it freely. It holds a [gift.NodeRef], which goes stale
// when the node is unmounted — a rebuild that replaces a subtree invalidates
// the nodes a test found before it. Every method checks, and a stale node
// fails with that word in the message rather than with a zero rectangle.
type Node struct {
	h   *Harness
	ref gift.NodeRef
}

// Ref returns the underlying [gift.NodeRef], for the escape hatch through
// [Harness.App].
func (n Node) Ref() gift.NodeRef { return n.ref }

// IsZero reports whether n refers to no node at all.
func (n Node) IsZero() bool { return n.h == nil || n.ref.IsZero() }

// Text returns [gift.Element.Label] of the node, which is what [ByText]
// matches against.
func (n Node) Text() string { n.check("Text"); return n.h.app.NodeLabel(n.ref) }

// Key returns the reconciliation key of the node, or "".
func (n Node) Key() string { n.check("Key"); return n.h.app.NodeKey(n.ref) }

// Type returns the registered name of the view type the node was built from,
// such as "ui.Button".
func (n Node) Type() string {
	n.check("Type")
	return gift.TypeName(n.h.app.NodeType(n.ref))
}

// Bounds returns the rectangle of the node in device space: where it actually
// is on screen, after the scroll offsets and transforms above it.
//
// It is [gift.App.NodeDeviceBounds]. Before scrolling existed it was the raw
// layout rectangle and the two were the same number; inside a scroll container
// they are not, and a test that aimed at the layout rectangle would click
// where the node used to be. [Node.LayoutBounds] is the raw one for a test
// whose subject really is the layout.
func (n Node) Bounds() geom.Rect { n.check("Bounds"); return n.h.app.NodeDeviceBounds(n.ref) }

// LayoutBounds returns the rectangle the layout gave the node, without the
// transforms of its ancestors. It is the right assertion for "the stack put
// the second row 40 pixels down" and the wrong one for "the button is here".
func (n Node) LayoutBounds() geom.Rect { n.check("LayoutBounds"); return n.h.app.NodeBounds(n.ref) }

// VisibleBounds returns the part of the node that survives the clips of its
// ancestors, and whether any of it does.
func (n Node) VisibleBounds() (geom.Rect, bool) {
	n.check("VisibleBounds")
	return n.h.app.NodeVisibleBounds(n.ref)
}

// IsVisible reports whether any part of the node survives the clips above it.
//
// It is not an occlusion test. A node covered by an opaque sibling is visible
// by this definition, because "is it clipped away" and "is something on top of
// it" are different questions with different answers and different fixes; the
// second one is what [Node.aim] checks with a hit test.
func (n Node) IsVisible() bool {
	_, ok := n.VisibleBounds()
	return ok
}

// Center returns the point the pointer actions aim at: the middle of the
// *visible* part of the node.
//
// The middle of the full bounds would be a point outside the viewport for a
// node that is only half scrolled into view, and an action there would be
// clipped away and land on nothing. Review Gate 3 named this specifically. For
// a node that is entirely clipped away there is no visible part, and the
// centre of the device bounds is returned so that the aim check further on can
// produce the honest failure rather than this method producing a confusing
// one.
func (n Node) Center() geom.Point {
	if vis, ok := n.VisibleBounds(); ok {
		return center(vis)
	}
	return center(n.Bounds())
}

// --- scrolling ---------------------------------------------------------------

// Scroller returns the nearest scroll container at or above this node, and
// fails when there is none.
func (n Node) Scroller() Node {
	n.h.t.Helper()
	n.check("Scroller")
	r, ok := n.h.app.NodeScroller(n.ref)
	if !ok {
		n.h.t.Fatalf("gifttest: Scroller on %s, which is not inside a scroll container.\n%s",
			n.describe(), n.h.Dump())
		return Node{}
	}
	return Node{h: n.h, ref: r}
}

// ScrollIntoView scrolls every container above this node until it is visible.
// It is a jump, not an animation; see [gift.App.ScrollIntoView].
//
// The pointer actions call it themselves, so a test normally does not have to.
// It is exported for the case where the scroll itself is the subject.
func (n Node) ScrollIntoView() Node {
	n.h.t.Helper()
	n.check("ScrollIntoView")
	if n.h.app.ScrollIntoView(n.ref) {
		n.h.Settle()
	}
	return n
}

// ScrollOffset returns the document offset of the nearest scroll container at
// or above this node.
func (n Node) ScrollOffset() float64 {
	n.h.t.Helper()
	info, ok := n.h.app.ScrollInfo(n.Scroller().ref)
	if !ok {
		return 0
	}
	return info.Offset
}

// ScrollInfo returns the full state of the nearest scroll container at or
// above this node.
func (n Node) ScrollInfo() gift.ScrollInfo {
	n.h.t.Helper()
	info, _ := n.h.app.ScrollInfo(n.Scroller().ref)
	return info
}

// ScrollTo moves the nearest scroll container at or above this node to the
// document offset off, clamped to its bounds.
func (n Node) ScrollTo(off float64) Node {
	n.h.t.Helper()
	s := n.Scroller()
	n.h.app.ScrollTo(s.ref, off)
	n.h.Settle()
	return n
}

// ScrollBy moves the nearest scroll container at or above this node by d
// document units.
func (n Node) ScrollBy(d float64) Node {
	n.h.t.Helper()
	s := n.Scroller()
	n.h.app.ScrollBy(s.ref, d)
	n.h.Settle()
	return n
}

// Interaction returns the hover, press, focus and disabled state gift keeps
// for the node.
func (n Node) Interaction() gift.Interaction {
	n.check("Interaction")
	return n.h.app.NodeInteraction(n.ref)
}

// Parent returns the parent node. The parent of the root is the zero [Node].
func (n Node) Parent() Node {
	n.check("Parent")
	return Node{h: n.h, ref: n.h.app.NodeParent(n.ref)}
}

// Children returns the children of the node, in order.
func (n Node) Children() []Node {
	n.check("Children")
	refs := n.h.app.NodeChildren(n.ref, nil)
	out := make([]Node, len(refs))
	for i, r := range refs {
		out[i] = Node{h: n.h, ref: r}
	}
	return out
}

// Find returns the single node in this node's subtree, including itself, that
// matches s, and fails when there is not exactly one. It is [Harness.Find]
// scoped to a subtree.
func (n Node) Find(s Selector) Node {
	n.h.t.Helper()
	n.check("Find")
	return n.h.Find(s.And(Under(sameNode(n)).Or(sameNode(n))))
}

// sameNode is the selector "is exactly this node", used to scope a subtree
// query.
func sameNode(n Node) Selector {
	return Selector{
		desc:  "self",
		match: func(_ *Harness, r gift.NodeRef) bool { return r == n.ref },
	}
}

// check fails the test when the node has gone stale, naming the method that
// asked. A stale node is almost always a test that found something, rebuilt
// the tree and then used the old handle; saying so is more useful than
// returning a zero rectangle and failing an assertion three lines later.
func (n Node) check(what string) {
	if n.h == nil {
		panic("gifttest: the zero Node has no " + what)
	}
	n.h.t.Helper()
	if !n.h.app.NodeValid(n.ref) {
		n.h.t.Fatalf("gifttest: Node.%s on a node that no longer exists. "+
			"A rebuild replaced it; find it again after the action that changed the tree.", what)
	}
}

// target resolves the node an action acts on.
//
// A [ByText] selector normally lands on the [ui.Text] *inside* a button,
// because that is where the string is. Clicking that text node directly would
// be meaningless — it has no interactor and the hit test would walk past it —
// so the actions climb to the nearest interactive ancestor. The alternative,
// making the test write ByText("+").And(ByType("ui.Button")) everywhere, moves
// a detail of the widget's internal structure into every test that uses it.
//
// The climb is reported in failures, so a test that accidentally clicked the
// wrong ancestor can see that it did.
func (n Node) target(what string) Node {
	n.h.t.Helper()
	n.check(what)
	cur := n.ref
	for depth := 0; !cur.IsZero(); depth++ {
		if n.h.app.NodeInteractive(cur) {
			return Node{h: n.h, ref: cur}
		}
		cur = n.h.app.NodeParent(cur)
	}
	n.h.t.Fatalf("gifttest: %s on %s, which is not interactive and has no interactive ancestor.\n"+
		"Only a node whose view declared a gift.Interactor — a ui.Button, for instance — can be "+
		"clicked, hovered or focused.\n%s",
		what, n.describe(), n.h.Dump())
	return Node{}
}

// aim returns the point an action on n injects its pointer event at, after
// checking that a hit test of that point really lands on n.
//
// # Why this check exists
//
// Without it every action in this package was a lie by omission. The action
// computes the centre of the intended node, injects a pointer event there and
// gift dispatches it to whatever its own hit test finds — which, for a node
// covered by a later sibling, by an overlay, or by a transparent full bleed
// [ui.Box] somebody put in a ZStack, is a different node entirely. The
// harness then reported nothing at all: the covering control activated, the
// assertion about the intended one failed three lines later with "the counter
// is 0", and the diagnosis pointed at the application instead of at the test.
//
// So the aim is verified. The point is hit tested with the very dispatcher
// [gift.App] uses for a real pointer event, and anything other than the
// intended node is a failure that names both.
//
// # The deliberate opt out
//
// Clicking through something on purpose is a real thing to want — asserting
// that an overlay swallows the click underneath it, for instance. That is what
// [Harness.ClickAt], [Harness.PressAt], [Harness.ReleaseAt] and
// [Harness.MoveTo] are for: they take a coordinate, they are documented as the
// coordinate-is-the-subject escape hatch, and they perform no aim check
// because there is no intended node to check against. [Harness.At] answers
// "what is actually at this point" without dispatching anything.
func (n Node) aim(what string) geom.Point {
	n.h.t.Helper()
	// Bring the node into view before working out where to click. Without
	// this an action on a node below the fold aims at a point its own viewport
	// clips away, the hit test below reaches nothing, and the failure blames
	// the application for a test that never scrolled. Review Gate 3 named this
	// as the thing that breaks without a scroll container.
	//
	// It happens before the aim check rather than instead of it, so the two do
	// not fight: the scroll decides *where* the node is and the hit test
	// decides whether anything is on top of it there. Scrolling changes no
	// bounds and rebuilds nothing, so the node reference stays valid.
	if n.h.app.ScrollIntoView(n.ref) {
		n.h.Settle()
	}
	p := n.Center()
	got, ok := n.h.app.HitTest(p)
	switch {
	case ok && got == n.ref:
		return p
	case !ok:
		n.h.t.Fatalf("gifttest: %s aimed at %s,\n"+
			"but a hit test at its centre (%g, %g) reaches no interactive node at all.\n"+
			"The node is covered by a clip, or an ancestor clips it away; the event would have "+
			"gone nowhere.\n"+
			"Use Harness.%s if the coordinate is what the test is about.\n%s",
			what, n.describe(), p.X, p.Y, coordinateVerb(what), n.h.Dump())
	default:
		other := Node{h: n.h, ref: got}
		n.h.t.Fatalf("gifttest: %s aimed at %s,\n"+
			"but a hit test at its centre (%g, %g) reaches %s instead.\n"+
			"The intended node is covered at that point, so the event would have gone to the "+
			"wrong control and this test would have passed while the application was broken.\n"+
			"If the click through is the subject, use Harness.%s with an explicit coordinate, or "+
			"assert on Harness.At(...) directly.\n%s",
			what, n.describe(), p.X, p.Y, other.describe(),
			coordinateVerb(what), n.h.dumpMarkedRefs(n.ref, got))
	}
	return p
}

// coordinateVerb names the coordinate taking counterpart of an action, for the
// opt out sentence of [Node.aim].
func coordinateVerb(what string) string {
	switch what {
	case "Hover":
		return "MoveTo"
	case "Press":
		return "PressAt"
	case "Drag", "DragTo", "Swipe":
		return "PressAt/MoveTo/ReleaseAt"
	default:
		return "ClickAt"
	}
}

// --- pointer actions --------------------------------------------------------

// Click moves the mouse onto the node, presses and releases it.
//
// The move is part of it: a real click hovers first, and a control whose
// pressed look depends on the hover would otherwise be tested in a state no
// user can produce. The point is the centre of the node's bounds, computed by
// the harness — a test never writes it.
func (n Node) Click() Node {
	n.h.t.Helper()
	t := n.target("Click")
	p := t.aim("Click")
	n.h.beginInput()
	n.h.app.PointerMove(gift.MousePointer, gift.PointerMouse, p)
	n.h.app.PointerDown(gift.MousePointer, gift.PointerMouse, p)
	n.h.app.PointerUp(gift.MousePointer, gift.PointerMouse, p)
	n.h.mouse = p
	n.h.Settle()
	return n
}

// Press moves the mouse onto the node and presses the button, leaving it down.
//
// The node captures the pointer until the matching [Harness.Release] or
// [Harness.CancelPointer], which is what makes "press here, release there"
// testable at all.
func (n Node) Press() Node {
	n.h.t.Helper()
	t := n.target("Press")
	p := t.aim("Press")
	n.h.beginInput()
	n.h.app.PointerMove(gift.MousePointer, gift.PointerMouse, p)
	n.h.app.PointerDown(gift.MousePointer, gift.PointerMouse, p)
	n.h.mouse = p
	n.h.Settle()
	return n
}

// Release releases the mouse button over this node.
func (n Node) Release() Node {
	n.h.t.Helper()
	t := n.target("Release")
	n.h.ReleaseAt(t.Center())
	return n
}

// Hover moves the mouse onto the node without pressing.
func (n Node) Hover() Node {
	n.h.t.Helper()
	t := n.target("Hover")
	n.h.MoveTo(t.aim("Hover"))
	return n
}

// Tap is a touch: a finger goes down on the node and comes straight up again.
//
// It is not a mouse click with another name. A touch produces no hover, ever —
// the project plan, section 7, forbids synthesising one — so a control that
// only reacts when hovered is broken under touch, and this is the action that
// catches it.
func (n Node) Tap() Node {
	n.h.t.Helper()
	t := n.target("Tap")
	p := t.aim("Tap")
	n.h.beginInput()
	n.h.app.PointerDown(touchID, gift.PointerTouch, p)
	n.h.app.PointerUp(touchID, gift.PointerTouch, p)
	n.h.Settle()
	return n
}

// LongPress puts a finger on the node, holds it past [gift.LongPressDelay] and
// lifts it.
//
// The hold is a move of the injected clock, not a sleep: the test takes
// microseconds. gift recognises the gesture in [gift.App.BeginInput], which is
// exactly what [Harness.Advance] triggers, so this is the real code path and
// not a synthesised event.
//
// The finger does not move, because a long press that drifts further than
// [gift.DragSlop] is a drag by definition and would not fire.
func (n Node) LongPress() Node {
	n.h.t.Helper()
	t := n.target("LongPress")
	p := t.aim("LongPress")
	n.h.beginInput()
	n.h.app.PointerDown(touchID, gift.PointerTouch, p)
	n.h.Settle()
	// One millisecond past the threshold: gift fires at >= the delay, and
	// landing exactly on it would make the test depend on a comparison this
	// package does not own.
	n.h.Advance(gift.LongPressDelay + time.Millisecond)
	n.h.beginInput()
	n.h.app.PointerUp(touchID, gift.PointerTouch, p)
	n.h.Settle()
	return n
}

// Drag presses the mouse on this node, moves it to the centre of dst in
// several steps and releases it there.
func (n Node) Drag(dst Node) Node {
	n.h.t.Helper()
	d := dst.target("Drag")
	// Both ends are aimed at a node, so both are checked. A drop onto a
	// covered target is the same defect as a click on a covered button.
	to := d.aim("Drag")
	n.h.t.Helper()
	return n.dragTo("Drag", to)
}

// DragTo presses the mouse on this node, moves it to the device space point p
// in several steps and releases it there.
//
// The intermediate moves are real: a recogniser that measures a velocity or
// waits for [gift.DragSlop] sees a plausible path rather than a teleport.
func (n Node) DragTo(p geom.Point) Node {
	n.h.t.Helper()
	return n.dragTo("DragTo", p)
}

// dragTo is the body of [Node.DragTo] and [Node.Drag], parameterised with the
// name the failures report.
func (n Node) dragTo(what string, p geom.Point) Node {
	n.h.t.Helper()
	t := n.target(what)
	from := t.aim(what)
	n.h.beginInput()
	n.h.app.PointerMove(gift.MousePointer, gift.PointerMouse, from)
	n.h.app.PointerDown(gift.MousePointer, gift.PointerMouse, from)
	n.h.Settle()
	for i := 1; i <= dragSteps; i++ {
		n.h.beginInput()
		n.h.app.PointerMove(gift.MousePointer, gift.PointerMouse, lerp(from, p, float32(i)/dragSteps))
		n.h.Settle()
	}
	n.h.beginInput()
	n.h.app.PointerUp(gift.MousePointer, gift.PointerMouse, p)
	n.h.mouse = p
	n.h.Settle()
	return n
}

// Swipe is a drag with a finger: down on this node, along d, up.
//
// It is the touch counterpart of [Node.DragTo] and, like [Node.Tap], produces
// no hover anywhere along the way.
func (n Node) Swipe(d geom.Point) Node {
	n.h.t.Helper()
	t := n.target("Swipe")
	from := t.aim("Swipe")
	to := from.Add(d)
	n.h.beginInput()
	n.h.app.PointerDown(touchID, gift.PointerTouch, from)
	n.h.Settle()
	for i := 1; i <= dragSteps; i++ {
		n.h.beginInput()
		n.h.app.PointerMove(touchID, gift.PointerTouch, lerp(from, to, float32(i)/dragSteps))
		n.h.Settle()
	}
	n.h.beginInput()
	n.h.app.PointerUp(touchID, gift.PointerTouch, to)
	n.h.Settle()
	return n
}

// Wheel scrolls over the node by d. Positive Y is the wheel pushed away from
// the user, which moves towards the beginning of the document.
//
// gift delivers it to the node under the pointer and bubbles it to the nearest
// scrollable ancestor that can still move in that direction; see
// [gift.Event.Delta] and the overscroll chaining rule in gift's scroll
// handler.
func (n Node) Wheel(d geom.Point) Node {
	n.h.t.Helper()
	n.check("Wheel")
	n.h.Wheel(n.Center(), d)
	return n
}

// --- keyboard actions -------------------------------------------------------

// Focus moves the keyboard focus to this node and fails when it cannot take
// it, which is the honest answer for a disabled or non focusable node.
func (n Node) Focus() Node {
	n.h.t.Helper()
	t := n.target("Focus")
	if !n.h.app.NodeFocusable(t.ref) {
		n.h.t.Fatalf("gifttest: Focus on %s, which cannot take the keyboard focus. "+
			"A node is focusable only when its element sets Focusable and is not disabled.",
			t.describe())
		return n
	}
	// The focus is moved through the public traversal rather than by fiat, so
	// that the test exercises the same order a user's tab key would.
	for i := 0; i <= n.h.focusableCount(); i++ {
		if f, ok := n.h.app.Focus(); ok && f == t.ref {
			n.h.Settle()
			return n
		}
		n.h.beginInput()
		if !n.h.app.MoveFocus(true) {
			break
		}
	}
	n.h.t.Fatalf("gifttest: Focus could not reach %s by tabbing; the focus order does not contain it.\n%s",
		t.describe(), n.h.Dump())
	return n
}

// PressKey focuses the node and sends one key press and release to it. It is
// the two line "tab here, press space" spelled once.
//
// It is not called Key because that name belongs to the accessor for the
// reconciliation key, and a method that reads a property and a method that
// drives the keyboard must not be one letter apart.
func (n Node) PressKey(k gift.Key, mods ...gift.Mods) Node {
	n.h.t.Helper()
	n.Focus()
	n.h.Key(k, mods...)
	return n
}

// --- harness level input ----------------------------------------------------

// MoveTo moves the mouse to the device space point p.
func (h *Harness) MoveTo(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerMove(gift.MousePointer, gift.PointerMouse, p)
	h.mouse = p
	h.Settle()
}

// PressAt presses the mouse at p.
func (h *Harness) PressAt(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerMove(gift.MousePointer, gift.PointerMouse, p)
	h.app.PointerDown(gift.MousePointer, gift.PointerMouse, p)
	h.mouse = p
	h.Settle()
}

// ReleaseAt releases the mouse at p. The event goes to whatever captured the
// press, with [gift.Event.Inside] saying whether p is over it.
func (h *Harness) ReleaseAt(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerUp(gift.MousePointer, gift.PointerMouse, p)
	h.mouse = p
	h.Settle()
}

// Release releases the mouse wherever it currently is.
func (h *Harness) Release() {
	h.t.Helper()
	h.ReleaseAt(h.mouse)
}

// ClickAt clicks at a device space point. Prefer a selector; this is for the
// cases where the coordinate is the subject, such as clicking the background.
func (h *Harness) ClickAt(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerMove(gift.MousePointer, gift.PointerMouse, p)
	h.app.PointerDown(gift.MousePointer, gift.PointerMouse, p)
	h.app.PointerUp(gift.MousePointer, gift.PointerMouse, p)
	h.mouse = p
	h.Settle()
}

// TapAt is a touch at a device space point: a finger goes down there and comes
// straight up again.
//
// It is the touch counterpart of [Harness.ClickAt] and exists for the same
// reason: [Node.Tap] aims at the centre of the nearest *interactive* node,
// which for a control made of several regions — a segment of a
// [ui.SegmentedControl], a point on a slider's track — is not the place the
// test means. A coordinate is the subject here, so there is no aim check; see
// [Node.aim].
//
// It does not move the mouse cursor: h.mouse is left where it was, so a later
// [Harness.Release] or [Harness.Tap] still refers to the last *mouse* position
// and not to p. That is correct rather than an omission — a finger that has
// lifted is nowhere, and a touchscreen has no cursor to leave behind — but it
// means a test cannot mix TapAt with the mouse verbs and expect them to share
// a position.
func (h *Harness) TapAt(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerDown(touchID, gift.PointerTouch, p)
	h.app.PointerUp(touchID, gift.PointerTouch, p)
	h.Settle()
}

// TouchDownAt puts a finger down at the device space point p and leaves it
// there.
//
// It is the touch counterpart of [Harness.PressAt], and the difference between
// the two is not cosmetic: PressAt moves the *mouse* to p first, because a
// real mouse is somewhere before it is pressed, and that move produces hover.
// A finger has no hover — the kiosk of the project plan, section 1, delivers a
// press and nothing before it — so a widget that is only reachable after a
// hover is unreachable on the target hardware, and a test that used PressAt
// would never find out. See [ui.ScrollBar] for the defect that was.
func (h *Harness) TouchDownAt(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerDown(touchID, gift.PointerTouch, p)
	h.Settle()
}

// TouchMoveTo moves the finger that is down to p.
func (h *Harness) TouchMoveTo(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerMove(touchID, gift.PointerTouch, p)
	h.Settle()
}

// TouchUpAt lifts the finger at p.
func (h *Harness) TouchUpAt(p geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerUp(touchID, gift.PointerTouch, p)
	h.Settle()
}

// CancelPointer ends the current mouse press without a release, as the window
// manager does when the window loses focus. A control that treats it as an
// activation is broken, and this is how a test says so.
func (h *Harness) CancelPointer() {
	h.t.Helper()
	h.beginInput()
	h.app.PointerCancel(gift.MousePointer)
	h.Settle()
}

// CancelTouch ends the current touch without a release.
func (h *Harness) CancelTouch() {
	h.t.Helper()
	h.beginInput()
	h.app.PointerCancel(touchID)
	h.Settle()
}

// Wheel scrolls at the device space point p by d.
func (h *Harness) Wheel(p geom.Point, d geom.Point) {
	h.t.Helper()
	h.beginInput()
	h.app.PointerWheel(p, d)
	h.Settle()
}

// Tab moves the keyboard focus to the next focusable node, by actually
// pressing tab.
func (h *Harness) Tab() {
	h.t.Helper()
	h.Key(gift.KeyTab)
}

// ShiftTab moves the keyboard focus to the previous focusable node.
func (h *Harness) ShiftTab() {
	h.t.Helper()
	h.Key(gift.KeyTab, gift.ModShift)
}

// Key presses and releases one key, with the given modifiers held.
func (h *Harness) Key(k gift.Key, mods ...gift.Mods) {
	h.t.Helper()
	m := combine(mods)
	h.beginInput()
	h.app.KeyDown(k, m)
	h.app.KeyUp(k, m)
	h.Settle()
}

// KeyDown presses a key and leaves it down.
func (h *Harness) KeyDown(k gift.Key, mods ...gift.Mods) {
	h.t.Helper()
	h.beginInput()
	h.app.KeyDown(k, combine(mods))
	h.Settle()
}

// KeyUp releases a key.
func (h *Harness) KeyUp(k gift.Key, mods ...gift.Mods) {
	h.t.Helper()
	h.beginInput()
	h.app.KeyUp(k, combine(mods))
	h.Settle()
}

// SetModifiers records which modifier keys are held from now on.
func (h *Harness) SetModifiers(mods ...gift.Mods) {
	h.t.Helper()
	h.beginInput()
	h.app.SetModifiers(combine(mods))
}

// Focused returns the node that currently holds the keyboard focus, and
// whether anything does.
func (h *Harness) Focused() (Node, bool) {
	r, ok := h.app.Focus()
	if !ok {
		return Node{}, false
	}
	return Node{h: h, ref: r}, true
}

// TypeText types the string, one character at a time, as a keyboard with the
// user's layout would deliver it.
//
// It is the counterpart of [Harness.Key] and the two are different channels,
// not two spellings of one: Key presses a *physical* key and TypeText produces
// *characters*. Typing " " is not pressing space, and a test that means "the
// user pressed the space bar on this button" must say [Harness.Key]. See
// [gift.App.TypeRune].
//
// Every character goes to whatever holds the keyboard focus, bubbling to the
// ancestors like a key event; nothing is typed into a node this method picks
// out on its own. Focus something first — Node.Focus, a click, or Tab.
//
// The clock does not move. Typing is a sequence of discrete events and gift's
// time based behaviour, key repeat included, is driven by [Harness.Advance]
// and [Harness.AdvanceTicks]; a TypeText that silently advanced time would
// make a repeat test depend on how many characters the string happened to
// have.
//
// # Non printable runes fail rather than being typed
//
// [gift.Event] documents Rune as always printable, because Ebitengine's
// character callback filters everything else out before gift sees it — a
// backspace is a [gift.KeyBackspace] and never a '\b'. A harness that let a
// test type "a\bb" would let that test drive an input path no keyboard can
// produce, and the text model it exercised would then meet real input it never
// handles. So a non printable rune is a test failure that names the character
// and points at [Harness.Key].
func (h *Harness) TypeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.TypeRune(r)
	}
}

// TypeRune types a single character. See [Harness.TypeText], which is this in
// a loop and is what a test normally wants.
func (h *Harness) TypeRune(r rune) {
	h.t.Helper()
	if !unicode.IsPrint(r) {
		h.t.Fatalf("gifttest: TypeRune(%q): gift only ever receives printable characters.\n"+
			"Ebitengine's character callback drops everything else before gift sees it, and "+
			"gift.Event documents Rune as printable, so a keyboard cannot produce this.\n"+
			"Backspace, delete, enter, tab and the arrows are keys: use Harness.Key(gift.KeyBackspace) "+
			"and the rest of gift.Key for them.", r)
		return
	}
	h.beginInput()
	h.app.TypeRune(r)
	h.Settle()
}

// AdvanceTicks moves the injected clock forward n times by step, opening an
// input phase and settling at each stop.
//
// It is [Harness.Advance] at a cadence instead of in one jump, and it exists
// for the behaviour that is defined per tick rather than per elapsed duration.
// Key repeat is the case: [gift.App] delivers at most a bounded number of
// synthetic presses per tick on purpose — see gift's maxRepeatsPerTick — so a
// single Advance of one second yields the bound and not thirty presses. A test
// that wants the rate a user would see steps at the frame interval, which is
// what a backend does.
func (h *Harness) AdvanceTicks(n int, step time.Duration) {
	h.t.Helper()
	for range n {
		h.Advance(step)
	}
}

func combine(mods []gift.Mods) gift.Mods {
	var m gift.Mods
	for _, v := range mods {
		m |= v
	}
	return m
}

func lerp(a, b geom.Point, t float32) geom.Point {
	return geom.Pt(a.X+(b.X-a.X)*t, a.Y+(b.Y-a.Y)*t)
}

// focusableCount is the number of nodes in the focus order, used to bound the
// tab loop in [Node.Focus].
func (h *Harness) focusableCount() int {
	n := 0
	h.walk(func(r gift.NodeRef, _ int) {
		if h.app.NodeFocusable(r) {
			n++
		}
	})
	return n
}

// Describe returns the one line form of the node used in failure messages:
// type, key, label and device bounds. It is exported so that an application's
// own assertion can name a node the same way this package does.
func (n Node) Describe() string { return n.describe() }

// describe is the one line form of a node used in failure messages.
func (n Node) describe() string {
	if n.h == nil || n.ref.IsZero() {
		return "<no node>"
	}
	if !n.h.app.NodeValid(n.ref) {
		return "<stale node>"
	}
	b := n.h.app.NodeDeviceBounds(n.ref)
	s := gift.TypeName(n.h.app.NodeType(n.ref))
	if k := n.h.app.NodeKey(n.ref); k != "" {
		s += " key=" + quote(k)
	}
	if l := n.h.app.NodeLabel(n.ref); l != "" {
		s += " text=" + quote(l)
	}
	return fmt.Sprintf("%s bounds=%s", s, rectString(b))
}

func rectString(r geom.Rect) string {
	return fmt.Sprintf("(%g,%g)-(%g,%g)", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
}

// Fling is a touch drag along d that takes over milliseconds of the injected
// clock, so that gift can measure a release velocity and start a kinetic
// scroll.
//
// [Node.Swipe] deliberately does not move the clock: every one of its moves
// carries the same timestamp, the measured velocity is therefore zero, and a
// swipe ends in a stop. That is the right default for a gesture test whose
// subject is the drag. A test whose subject is the kinetic part needs time to
// pass between the samples, and this is the action that makes it pass —
// deterministically, in the injected clock, without a single sleep.
//
// The fling is not settled afterwards: the release starts an animation that
// runs for as long as the friction says. Drive it with [Harness.Advance] and
// assert on the offset in between, which is the whole point of an injectable
// clock.
func (n Node) Fling(d geom.Point, over time.Duration) Node {
	n.h.t.Helper()
	t := n.target("Fling")
	from := t.aim("Fling")
	to := from.Add(d)
	step := over / dragSteps
	n.h.beginInput()
	n.h.app.PointerDown(touchID, gift.PointerTouch, from)
	n.h.Settle()
	for i := 1; i <= dragSteps; i++ {
		n.h.now += step
		n.h.beginInput()
		n.h.app.PointerMove(touchID, gift.PointerTouch, lerp(from, to, float32(i)/dragSteps))
		n.h.Settle()
	}
	n.h.beginInput()
	n.h.app.PointerUp(touchID, gift.PointerTouch, to)
	n.h.Settle()
	return n
}
