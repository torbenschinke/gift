package gifttest

import (
	"fmt"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
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

// Bounds returns the absolute rectangle of the node as of the last layout.
func (n Node) Bounds() geom.Rect { n.check("Bounds"); return n.h.app.NodeBounds(n.ref) }

// Center returns the point in the middle of the node's bounds, which is where
// the pointer actions aim.
func (n Node) Center() geom.Point { return center(n.Bounds()) }

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
	p := t.Center()
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
	p := t.Center()
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
	n.h.MoveTo(t.Center())
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
	p := t.Center()
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
	p := t.Center()
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
	return n.DragTo(d.Center())
}

// DragTo presses the mouse on this node, moves it to the device space point p
// in several steps and releases it there.
//
// The intermediate moves are real: a recogniser that measures a velocity or
// waits for [gift.DragSlop] sees a plausible path rather than a teleport.
func (n Node) DragTo(p geom.Point) Node {
	n.h.t.Helper()
	t := n.target("DragTo")
	from := t.Center()
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
	from := t.Center()
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

// Wheel scrolls over the node by d. Positive Y is "wheel away from the user".
//
// gift delivers it to the node under the pointer and bubbles it; nothing in
// gift consumes it yet, because there is no scrollable view before step 3 of
// the project plan. An application's own scroll container can be tested with
// it today.
func (n Node) Wheel(d geom.Point) Node {
	n.h.t.Helper()
	n.check("Wheel")
	p := center(n.h.app.NodeBounds(n.ref))
	n.h.Wheel(p, d)
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

// TypeText is not implemented, and this is where that is said out loud.
//
// # Why
//
// Typing needs two things gift does not have. There is no text input view —
// the project plan, section 14, excludes a text editor and an IME from the
// MVP — so there is nothing on screen that could receive characters. And there
// is no character event: [gift.Key] is a dozen named keys for navigation and
// activation, deliberately not a keyboard map, and [gift.App] has no
// equivalent of a "text input" or "rune typed" entry point at all. gift also
// has no key repeat; the project plan, section 7, checked that in the pinned
// Ebitengine source and recorded it.
//
// Faking it here would mean inventing an event the runtime does not deliver,
// and a harness that can drive an input path no real keyboard can reach is
// worse than one that cannot: the test would pass and the application would
// not work.
//
// # What it would take
//
// A rune bearing event on [gift.App] (`App.TypeRune(r rune)` or a text
// composition entry point), delivered to the focused node like a key event;
// a view that consumes it; and, for a real editor, the key repeat that has to
// be built on inpututil.KeyPressDuration. When that exists this method becomes
// four lines and its signature does not change.
func (h *Harness) TypeText(string) {
	h.t.Helper()
	h.t.Fatalf("gifttest: TypeText is not implemented because gift cannot receive text yet.\n" +
		"There is no text input view (excluded from the MVP by the project plan, section 14) and " +
		"no character event on gift.App: gift.Key is a small set of navigation and activation keys, " +
		"not a keyboard map.\n" +
		"Use Harness.Key for the keys that do exist — space, enter, tab, the arrows — and drive a " +
		"text model directly until gift grows a text field.")
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

// describe is the one line form of a node used in failure messages.
func (n Node) describe() string {
	if n.h == nil || n.ref.IsZero() {
		return "<no node>"
	}
	if !n.h.app.NodeValid(n.ref) {
		return "<stale node>"
	}
	b := n.h.app.NodeBounds(n.ref)
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
