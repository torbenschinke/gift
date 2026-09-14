package gift

import (
	"time"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// PointerKind tells a mouse from a finger.
//
// The project plan, section 7, asks for "ein gemeinsames Pointer-Modell" that
// abstracts mouse and touch, and in the same sentence forbids touch from
// synthesising hover states. Both halves live here: every pointer travels
// through the same events and the same capture rules, and the one place the
// two differ — hover — is a switch on this value in exactly one function,
// [App.updateHover].
type PointerKind uint8

const (
	// PointerMouse is a mouse or trackpad. It has a position even when no
	// button is down, and therefore a hover state.
	PointerMouse PointerKind = iota
	// PointerTouch is a finger. It exists only while it is down and never
	// produces a hover state.
	PointerTouch
)

// String returns "mouse" or "touch".
func (k PointerKind) String() string {
	if k == PointerTouch {
		return "touch"
	}
	return "mouse"
}

// PointerID identifies one pointer.
//
// The mouse is always [MousePointer]. A touch carries whatever identifier the
// platform assigned to the finger; gift only ever compares it for equality.
type PointerID int64

// MousePointer is the identifier of the mouse pointer. Platform touch
// identifiers are non negative, so a negative constant cannot collide.
const MousePointer PointerID = -1

// EventKind discriminates an [Event].
type EventKind uint8

const (
	// EventNone is the zero value and is never dispatched.
	EventNone EventKind = iota

	// EventPointerEnter says the mouse moved onto this node. It is never
	// produced by a touch; see [PointerKind].
	EventPointerEnter
	// EventPointerLeave says the mouse moved off this node.
	EventPointerLeave
	// EventPointerMove is a position change. While a pointer is captured it
	// goes to the capturing node even when the position is outside it; see
	// [Event.Inside].
	EventPointerMove
	// EventPointerDown is a press. The node that is hit captures the
	// pointer until the matching up or cancel.
	EventPointerDown
	// EventPointerUp is a release. It is delivered to the capturing node,
	// inside or not.
	EventPointerUp
	// EventPointerCancel says the pointer went away without a release: the
	// window lost focus, or the platform took the gesture over. A node that
	// treats it like an up would activate on something the user never
	// completed.
	//
	// A node that is unmounted while it holds a press does *not* receive one,
	// and cannot: by the time gift knows, the node is gone and there is
	// nobody to deliver to. gift ends the press instead — see [App.forgetNode]
	// — so the pointer is genuinely up as far as the rest of the runtime is
	// concerned, and the release that eventually arrives is dropped. This
	// list used to name that case as a cause, and no code path produced it.
	EventPointerCancel
	// EventLongPress is delivered once per press to the capturing node, when
	// the pointer has been down for [LongPressDelay] without moving further
	// than [DragSlop].
	EventLongPress
	// EventWheel is a scroll wheel or two finger scroll. It is delivered to
	// the node under the pointer and bubbles to its ancestors, because the
	// node that scrolls is usually not the leaf under the cursor.
	EventWheel

	// EventKeyDown is a key press, delivered to the focused node and bubbled
	// to its ancestors.
	EventKeyDown
	// EventKeyUp is a key release, delivered the same way.
	EventKeyUp

	// EventFocusGained and EventFocusLost bracket the time a node holds the
	// keyboard focus.
	EventFocusGained
	// EventFocusLost is the counterpart of EventFocusGained.
	EventFocusLost
)

// Key is a physical key, in the small set gift's own navigation and
// activation need.
//
// It is deliberately not a full keyboard map. The project plan, section 7,
// scopes keyboard input to focus order, disabled, activation with space and
// enter and arrow navigation; a text editor and an IME are excluded by
// section 14. A backend that sees a key gift has no constant for reports
// [KeyOther], which nothing in gift acts on but an application may.
type Key uint8

const (
	// KeyOther is any key gift has no name for.
	KeyOther Key = iota
	KeyTab
	KeySpace
	KeyEnter
	KeyEscape
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
)

// Mods is the set of modifier keys held while an event happened.
type Mods uint8

const (
	// ModShift is held. gift itself reads it for reverse tab order.
	ModShift Mods = 1 << iota
	// ModControl is held.
	ModControl
	// ModAlt is held.
	ModAlt
	// ModMeta is the command or windows key.
	ModMeta
)

// Has reports whether every modifier in m is set in s.
func (s Mods) Has(m Mods) bool { return s&m == m }

// Event is one input event as an [Interactor] sees it.
//
// It is a plain value and is passed by value. Nothing in it points at
// anything, which is what keeps dispatch inside the zero allocation contract
// of the project plan, section 11.
type Event struct {
	// Kind selects which of the remaining fields mean anything.
	Kind EventKind

	// Pointer and Device identify the pointer of a pointer event.
	Pointer PointerID
	Device  PointerKind

	// Pos is the position of a pointer event in device space, the same space
	// the bounds of a node and the clip rectangles of the display list live
	// in; see the project plan, section 7.
	Pos geom.Point

	// Delta is the movement since the previous event for a move, and the
	// scroll amount for [EventWheel].
	//
	// For a wheel, positive Y is the wheel pushed *away* from the user, which
	// is what GLFW and therefore Ebitengine report, and it moves towards the
	// beginning of the document — so a scroll container subtracts it from its
	// offset. The previous wording, "positive Y scrolls the content up", said
	// nothing a reader could act on: content moving up on screen and the view
	// moving up the document are opposite directions and it did not say
	// which. The sign is fixed here because [scrollHandler] is now the first
	// consumer and something had to be true.
	Delta geom.Point

	// Inside reports whether Pos lies inside the receiving node, taking the
	// clips of its ancestors into account.
	//
	// It is the whole point of pointer capture: a press that starts on a
	// button and is released somewhere else still reaches the button, with
	// Inside false, so the button can decline to activate instead of never
	// hearing about the release at all.
	Inside bool

	// Dragged reports whether the pointer moved further than [DragSlop] from
	// the position of the press. A tap is an up with Inside true and Dragged
	// false.
	Dragged bool

	// Key and Mods describe a key event. Mods is filled in for pointer
	// events too, so that a modified click can be told apart.
	Key  Key
	Mods Mods

	// Time is the timestamp the backend supplied in [App.BeginInput], as a
	// duration since an arbitrary but fixed origin. Differences are
	// meaningful, the absolute value is not.
	Time time.Duration
}

// IsPointer reports whether e is a pointer event, that is one with a
// meaningful Pos.
func (e Event) IsPointer() bool {
	return e.Kind >= EventPointerEnter && e.Kind <= EventWheel
}

// Interaction is the presentation state gift maintains for an interactive
// node.
//
// It is owned by the retained node, not by the view, which is what makes
// hover and press free of rebuilds: the dispatcher writes these three bools
// into the node payload and marks the node for repaint, and the painter reads
// them back. No view function runs and [Diagnostics.Builds] does not move.
// The project plan, section 5, requires exactly that — "Scrolloffset, Hover,
// Pressed und Animation sind lokaler Praesentationszustand. Sie sollen keinen
// fachlichen Root-Rebuild erzwingen."
//
// It also survives a rebuild that happens for an unrelated reason, because it
// lives in the node and a rebuild replaces the view, not the node.
type Interaction struct {
	// Hover is set while a mouse is over the node and no other node has
	// captured the pointer. A touch never sets it.
	Hover bool
	// Pressed is set while a pointer this node captured is down and inside
	// it. A press that has wandered off the node clears it and sets it again
	// when the pointer comes back, which is the behaviour every platform
	// button has.
	Pressed bool
	// Focused is set while the node holds the keyboard focus.
	Focused bool
	// Disabled mirrors [Element.Disabled]. It is here so that a painter
	// needs to consult one value rather than two.
	Disabled bool
}

// Interactor is the retained half of a view that wants input.
//
// # Opting in
//
// A node takes part in hit testing exactly when its [Element] carries an
// Interactor. A nil Interactor makes the node *transparent* to input: it is
// not a hit target itself, and its children are tested exactly as they would
// be otherwise.
//
// That is deliberately the opposite polarity of the nil painter rule, and the
// reason is which mistake each default causes. A nil painter that drew
// nothing would hide a whole subtree with no symptom but a blank window,
// which is why a nil painter paints its children. A nil interactor that
// *swallowed* input would make every plain container eat the clicks meant for
// what is behind it, again with no symptom; so a nil interactor swallows
// nothing. In both cases the default is the permissive one, and in both cases
// the failure of forgetting the non nil case is local and loud: a button with
// no Interactor does nothing when clicked, one view away from the mistake.
//
// A node with an Interactor is a hit target over its whole [Node bounds],
// intersected with the clips of its ancestors. Rounded corners are not
// followed; a click in the corner of a rounded button hits the button.
type Interactor interface {
	// HandleEvent processes one event and reports whether it was handled.
	//
	// An unhandled pointer or key event bubbles to the nearest ancestor with
	// an Interactor, which is how a scroll container sees a wheel event that
	// happened over a label inside it. Enter, leave, focus and cancel events
	// do not bubble: they are statements about one node.
	//
	// It runs inside Ebitengine's Update, never during Draw, so writing
	// state from here is normal and is the documented way to react; see the
	// project plan, sections 5 and 6. It must not allocate: the project plan,
	// section 11, puts input processing inside the zero allocation contract.
	HandleEvent(ctx *EventContext, e Event) bool
}

// Gesture recognition constants.
//
// They are package level values rather than configuration because a gesture
// threshold is a property of the human hand, not of the application, and
// because making them per App would put a pointer dereference in the dispatch
// path for a number that never changes.
const (
	// LongPressDelay is how long a pointer has to stay down, without moving
	// further than DragSlop, before [EventLongPress] is delivered.
	LongPressDelay = 500 * time.Millisecond
	// DragSlop is how far a pointer may travel from the press position and
	// still count as a tap rather than a drag, in logical pixels.
	DragSlop float32 = 8
)

// EventContext is the interface of an [Interactor] to the runtime.
//
// There is exactly one instance per App; it is reused for every node and
// every event and is valid only for the duration of a [Interactor.HandleEvent]
// call.
type EventContext struct {
	app *App
	cur scene.Handle
	nd  *nodeData
}

// Bounds returns the absolute rectangle of the receiving node, as computed by
// the last layout pass.
func (c *EventContext) Bounds() geom.Rect { return c.app.store.Get(c.cur).Bounds }

// Interaction returns the hover, press, focus and disabled state gift
// maintains for the receiving node.
func (c *EventContext) Interaction() Interaction { return c.nd.ia }

// RequestFocus moves the keyboard focus to the receiving node, if it declared
// itself focusable. A disabled node cannot take the focus.
func (c *EventContext) RequestFocus() { c.app.setFocus(c.cur) }

// ClearFocus removes the keyboard focus from whatever holds it.
func (c *EventContext) ClearFocus() { c.app.setFocus(scene.Handle{}) }

// StealPointer moves the capture of the pointer whose event is being
// dispatched to the receiving node, and reports whether the node holds it
// afterwards.
//
// # What it is for
//
// The classic mobile interaction: a finger comes down on a button inside a
// scrolling list, travels further than [DragSlop] and turns into a scroll. The
// button took the press, so every move is delivered to it and bubbles up to
// the scroller; the scroller recognises the drag and calls this. The button is
// then told [EventPointerCancel] — its gesture really is over — loses its
// pressed look and never sees the release, so it cannot activate. Without it
// the release would arrive at the button with [Event.Dragged] set, which a
// well written control declines, but a control that merely checks
// [Event.Inside] would fire, and the pressed highlight would follow the finger
// down the whole list.
//
// # What it is not
//
// It is not a gesture arena. There is no negotiation, no deferred resolution
// and no way to hand the pointer back: whoever calls it last wins, and the
// previous holder is told the gesture ended. That is enough for the one
// conflict gift has — a scroll against everything else — and a real arena is
// the kind of thing to build when there is a second conflict to arbitrate.
//
// The cancel is delivered synchronously, which means the previous holder's
// HandleEvent may still be on the stack above this call: it is the frame that
// bubbled the event here in the first place. A handler therefore has to
// tolerate receiving a cancel from inside its own dispatch, which is why
// cancel handling is required to be a state reset and not a state machine
// transition.
//
// It does nothing when no pointer is down, so calling it from a key handler is
// harmless rather than a way to corrupt the capture.
func (c *EventContext) StealPointer() bool { return c.app.stealPointer(c.cur) }

func (a *App) stealPointer(h scene.Handle) bool {
	p := a.in.cur
	if p == nil || !p.down || !a.store.Valid(h) {
		return false
	}
	if p.capture == h {
		return true
	}
	prev := p.capture
	p.capture = h
	p.insideCap = a.hits(h, p.pos)
	if a.store.Valid(prev) {
		a.setPressed(prev, false)
		a.deliver(prev, a.pointerEvent(EventPointerCancel, p), true)
	}
	return true
}

// Repaint marks the receiving node as needing to be drawn again, without
// rebuilding or re measuring anything.
//
// gift redraws the whole visible list every frame anyway — the project plan,
// section 6 — so this changes no pixels by itself. What it does is keep
// [App.NeedsPaint], and therefore the idle tick policy of the backend, honest
// about the fact that something changed. An interactor that keeps its own
// presentation state calls it; one that only reads [EventContext.Interaction]
// does not have to, because the dispatcher already did.
func (c *EventContext) Repaint() { c.app.markNeedsPaint(c.cur) }

// --- the pointer state machine ---------------------------------------------

// pointer is one tracked pointer.
//
// The mouse lives in slot zero of [inputState.pointers] for its whole life.
// A touch claims the one remaining slot when it goes down and gives it back
// when it goes up: the project plan, section 7, limits multitouch to
// "Erkennung und Verwerfung zusaetzlicher Finger", so a second finger is
// counted in [Diagnostics.DiscardedTouches] and dropped.
type pointer struct {
	id     PointerID
	kind   PointerKind
	active bool

	pos geom.Point

	// over is the node the mouse is hovering. It is always the zero handle
	// for a touch.
	over scene.Handle
	// capture is the node that took the press, zero while no button is down.
	capture scene.Handle

	down      bool
	downPos   geom.Point
	downAt    time.Duration
	dragged   bool
	longFired bool
	insideCap bool
}

// inputState is everything the dispatcher remembers between events. It is a
// plain struct inside the App, so nothing here allocates after warmup.
type inputState struct {
	// pointers[0] is the mouse, pointers[1] the one accepted touch.
	pointers [2]pointer
	now      time.Duration
	mods     Mods

	focus scene.Handle
	ectx  EventContext

	// cur is the pointer whose event is currently being dispatched, nil
	// outside a pointer dispatch. It exists so that
	// [EventContext.StealPointer] knows which capture to move without every
	// event having to carry a pointer back reference.
	cur *pointer

	// flings is the set of scroll containers with a running kinetic
	// animation. It is a reused slice compacted in place, so a fling costs
	// no allocation per frame; see [App.tickScrolls].
	flings []scene.Handle

	// focusScan is the reusable stack of the focus traversal; see
	// [App.focusNeighbour].
	focusScan []scene.Handle
}

// BeginInput opens the input phase of one tick and advances the clock to now.
//
// It must be called once per Ebitengine update, before the pointer and key
// methods and before [App.Update]. It is where time based gestures fire: a
// long press is recognised here, because nothing else happens while a finger
// rests on the screen and a gesture that needed an event to notice the
// passage of time would never fire at all.
//
// It is also where kinetic scrolling is advanced, for exactly the same
// reason: a fling has to keep moving while the user does nothing at all. The
// clock is the parameter and never the wall clock, so gifttest.Advance can
// step a fling deterministically; the project plan, section 13, requires that.
//
// now is a monotonic timestamp; differences are meaningful, the absolute
// value is not. A backend normally passes time.Since of a start instant.
func (a *App) BeginInput(now time.Duration) {
	a.assertInputPhase("BeginInput")
	a.in.now = now
	a.tickScrolls(now)
	for i := range a.in.pointers {
		p := &a.in.pointers[i]
		if !p.active || !p.down || p.longFired || p.dragged {
			continue
		}
		if now-p.downAt < LongPressDelay {
			continue
		}
		p.longFired = true
		if a.store.Valid(p.capture) {
			a.deliver(p.capture, a.pointerEvent(EventLongPress, p), true)
		}
	}
}

// SetModifiers records which modifier keys are currently held. Every event
// dispatched afterwards carries them.
func (a *App) SetModifiers(m Mods) {
	a.assertInputPhase("SetModifiers")
	a.in.mods = m
}

// PointerMove reports that the pointer id moved to pos, in device space.
//
// For the mouse this also maintains the hover state: the node under the
// cursor gets [EventPointerEnter], the one it left gets [EventPointerLeave]
// and [Interaction.Hover] follows. While a pointer is *down* the hover set is
// frozen, because a drag that passes over other buttons must not light them
// up. That holds whether or not anything captured the press: a drag that
// started on empty space, and a drag whose captured node was unmounted
// underneath it, both leave the hover alone until the release.
func (a *App) PointerMove(id PointerID, kind PointerKind, pos geom.Point) {
	a.assertInputPhase("PointerMove")
	p := a.pointerFor(id, kind, false)
	if p == nil {
		return
	}
	a.in.cur = p
	defer func() { a.in.cur = nil }()
	delta := geom.Pt(pos.X-p.pos.X, pos.Y-p.pos.Y)
	p.pos = pos
	if p.down && !p.dragged && dist2(pos, p.downPos) > DragSlop*DragSlop {
		p.dragged = true
	}

	if a.store.Valid(p.capture) {
		inside := a.hits(p.capture, pos)
		a.setPressed(p.capture, p.down && inside)
		p.insideCap = inside
		e := a.pointerEvent(EventPointerMove, p)
		e.Delta = delta
		e.Inside = inside
		a.deliver(p.capture, e, false)
		return
	}
	if p.down {
		// Down but capturing nothing: the press landed on empty space, or the
		// node that took it was unmounted and [App.forgetNode] dropped the
		// capture. Either way the hover set stays frozen, which is what this
		// method's own documentation promises while a pointer is down.
		//
		// Falling through to updateHover here was the defect: a finger held
		// on a button that then disappeared, or a drag started on the
		// background, lit up every control it passed over. The release clears
		// p.down and the hover resumes there.
		return
	}
	a.updateHover(p)
	if !p.over.IsZero() {
		e := a.pointerEvent(EventPointerMove, p)
		e.Delta = delta
		e.Inside = true
		a.deliver(p.over, e, false)
	}
}

// PointerDown reports a press of the pointer id at pos.
//
// The node that is hit captures the pointer: every move, the release and a
// cancel go to it and to nobody else, until the pointer comes up. That is what
// makes "press on the button, release next to it" not an activation while
// still telling the button that the press ended.
//
// A touch that arrives while another touch is already down is counted in
// [Diagnostics.DiscardedTouches] and ignored. The project plan, section 7,
// limits multitouch to recognising and discarding additional fingers.
func (a *App) PointerDown(id PointerID, kind PointerKind, pos geom.Point) {
	a.assertInputPhase("PointerDown")
	p := a.pointerFor(id, kind, true)
	if p == nil {
		a.diag.DiscardedTouches++
		return
	}
	a.in.cur = p
	defer func() { a.in.cur = nil }()
	p.pos = pos
	p.down = true
	p.downPos = pos
	p.downAt = a.in.now
	p.dragged = false
	p.longFired = false

	if kind == PointerMouse {
		a.updateHover(p)
	}
	h := a.hitTest(pos)
	p.capture = h
	p.insideCap = !h.IsZero()
	if h.IsZero() {
		// A press on empty space drops the keyboard focus, which is what
		// every desktop toolkit does and what keeps a stale focus ring from
		// surviving a click on the background.
		a.setFocus(scene.Handle{})
		return
	}
	a.setPressed(h, true)
	e := a.pointerEvent(EventPointerDown, p)
	e.Inside = true
	a.deliver(h, e, false)
}

// PointerUp reports a release of the pointer id at pos.
//
// The event goes to the capturing node with [Event.Inside] telling it whether
// the release happened over it. A touch pointer stops existing here; the
// mouse keeps its position and its hover.
func (a *App) PointerUp(id PointerID, kind PointerKind, pos geom.Point) {
	a.assertInputPhase("PointerUp")
	p := a.pointerFor(id, kind, false)
	if p == nil || !p.down {
		return
	}
	a.in.cur = p
	defer func() { a.in.cur = nil }()
	p.pos = pos
	p.down = false
	cap := p.capture
	p.capture = scene.Handle{}

	if a.store.Valid(cap) {
		inside := a.hits(cap, pos)
		a.setPressed(cap, false)
		e := a.pointerEvent(EventPointerUp, p)
		e.Inside = inside
		a.deliver(cap, e, false)
	}
	if kind == PointerTouch {
		a.releasePointer(p)
		return
	}
	a.updateHover(p)
}

// PointerCancel ends the pointer id without a release.
//
// It is the right call when the window loses focus, when the platform takes a
// gesture over or when a touch is lost. The capturing node hears
// [EventPointerCancel] and must not treat it as an activation.
func (a *App) PointerCancel(id PointerID) {
	a.assertInputPhase("PointerCancel")
	p := a.findPointer(id)
	if p == nil {
		return
	}
	a.in.cur = p
	defer func() { a.in.cur = nil }()
	cap := p.capture
	p.capture = scene.Handle{}
	p.down = false
	if a.store.Valid(cap) {
		a.setPressed(cap, false)
		a.deliver(cap, a.pointerEvent(EventPointerCancel, p), true)
	}
	if p.kind == PointerTouch {
		a.releasePointer(p)
		return
	}
	a.clearHover(p)
}

// PointerWheel reports a scroll of delta at pos.
//
// The event goes to the node under pos and bubbles up to the root until an
// interactor handles it, because the node that scrolls is an ancestor of the
// leaf the cursor happens to be over.
//
// A scroll container consumes it when it can still move in the requested
// direction and lets it bubble when it cannot; see the overscroll chaining
// rule on gift's scroll handler. An event that reaches the root unconsumed is
// simply dropped.
func (a *App) PointerWheel(pos geom.Point, delta geom.Point) {
	a.assertInputPhase("PointerWheel")
	// Through pointerFor like every other mouse entry point, and not by
	// indexing the slot directly. Slot zero is the mouse's for its whole life,
	// but it is only *initialised* — id MousePointer, kind PointerMouse — by
	// pointerFor, so a wheel that arrived before the first PointerMove used to
	// carry the zero PointerID. Zero is in the range platform touch
	// identifiers use, so the event claimed to come from a finger that had
	// never touched anything.
	p := a.pointerFor(MousePointer, PointerMouse, false)
	if p == nil {
		return
	}
	a.in.cur = p
	defer func() { a.in.cur = nil }()
	// The hover follows the position, for the same reason PointerMove
	// maintains it: writing p.pos without it left p.over pointing at whatever
	// node the mouse was last over, so the next real move computed its delta
	// and its enter/leave pair from a position the mouse never visited. A
	// wheel event does move the cursor as far as the platform is concerned.
	p.pos = pos
	if !a.store.Valid(p.capture) {
		a.updateHover(p)
	}
	h := a.hitTest(pos)
	if h.IsZero() {
		return
	}
	e := a.pointerEvent(EventWheel, p)
	e.Pos = pos
	e.Delta = delta
	e.Inside = true
	a.deliver(h, e, false)
}

// KeyDown reports a key press.
//
// The event goes to the focused node and bubbles to its ancestors. Tab and
// shift-tab move the focus when nothing consumed them, which is why a focused
// node that wants to handle tab itself simply returns true for it.
func (a *App) KeyDown(k Key, mods Mods) {
	a.assertInputPhase("KeyDown")
	a.in.mods = mods
	e := Event{Kind: EventKeyDown, Key: k, Mods: mods, Time: a.in.now, Pointer: MousePointer}
	if a.deliverKey(e) {
		return
	}
	if k == KeyTab {
		a.MoveFocus(!mods.Has(ModShift))
	}
}

// KeyUp reports a key release, delivered like [App.KeyDown] but never acted
// on by gift itself.
func (a *App) KeyUp(k Key, mods Mods) {
	a.assertInputPhase("KeyUp")
	a.in.mods = mods
	a.deliverKey(Event{Kind: EventKeyUp, Key: k, Mods: mods, Time: a.in.now, Pointer: MousePointer})
}

// --- internals --------------------------------------------------------------

// assertInputPhase rejects input dispatched from the wrong place.
//
// Input is read and resolved in Ebitengine's Update, before gift builds and
// lays out; the project plan, section 6, puts it there and nowhere else. An
// event handler writes state, and a state write during Paint is already a
// panic, so dispatching during Paint would produce that panic several frames
// away from the mistake.
func (a *App) assertInputPhase(what string) {
	a.assertUIGoroutine(what)
	if a.painting {
		panic("gift: App." + what + " during App.Paint; input is resolved in Update, see the project plan, section 6")
	}
}

// pointerFor returns the slot of id, creating it on a press. It returns nil
// for a touch that arrives while another one is already down.
func (a *App) pointerFor(id PointerID, kind PointerKind, press bool) *pointer {
	if kind == PointerMouse {
		p := &a.in.pointers[0]
		if !p.active {
			p.active, p.id, p.kind = true, MousePointer, PointerMouse
		}
		return p
	}
	p := &a.in.pointers[1]
	if p.active {
		if p.id == id {
			return p
		}
		return nil
	}
	if !press {
		return nil
	}
	*p = pointer{id: id, kind: PointerTouch, active: true}
	return p
}

func (a *App) findPointer(id PointerID) *pointer {
	for i := range a.in.pointers {
		if p := &a.in.pointers[i]; p.active && p.id == id {
			return p
		}
	}
	return nil
}

func (a *App) releasePointer(p *pointer) { *p = pointer{} }

func (a *App) pointerEvent(kind EventKind, p *pointer) Event {
	return Event{
		Kind:    kind,
		Pointer: p.id,
		Device:  p.kind,
		Pos:     p.pos,
		Dragged: p.dragged,
		Mods:    a.in.mods,
		Time:    a.in.now,
	}
}

// updateHover recomputes which node the mouse is over and emits the enter and
// leave pair when it changed.
//
// This is the one function that distinguishes mouse from touch, and it does so
// by returning immediately for a touch. The project plan, section 7:
// "Touch-Ereignisse erzeugen keine synthetischen Hover-Zustaende."
func (a *App) updateHover(p *pointer) {
	if p.kind != PointerMouse {
		return
	}
	h := a.hitTest(p.pos)
	if h == p.over {
		return
	}
	prev := p.over
	p.over = h
	if a.store.Valid(prev) {
		a.setHover(prev, false)
		a.deliver(prev, a.pointerEvent(EventPointerLeave, p), true)
	}
	if !h.IsZero() {
		a.setHover(h, true)
		e := a.pointerEvent(EventPointerEnter, p)
		e.Inside = true
		a.deliver(h, e, true)
	}
}

// clearHover drops the hover of p without looking for a new target. It is
// what a cancelled or departed pointer needs.
func (a *App) clearHover(p *pointer) {
	prev := p.over
	p.over = scene.Handle{}
	if a.store.Valid(prev) {
		a.setHover(prev, false)
		a.deliver(prev, a.pointerEvent(EventPointerLeave, p), true)
	}
}

func (a *App) setHover(h scene.Handle, v bool) {
	nd := a.data(h)
	if nd.ia.Hover == v {
		return
	}
	nd.ia.Hover = v
	a.markNeedsPaint(h)
}

func (a *App) setPressed(h scene.Handle, v bool) {
	if !a.store.Valid(h) {
		return
	}
	nd := a.data(h)
	if nd.ia.Pressed == v {
		return
	}
	nd.ia.Pressed = v
	a.markNeedsPaint(h)
}

// deliver hands e to the interactor of h and, unless direct is set, to its
// ancestors until one handles it.
func (a *App) deliver(h scene.Handle, e Event, direct bool) bool {
	for depth := 0; !h.IsZero() && a.store.Valid(h); depth++ {
		if depth > scene.MaxDepth {
			panic("gift: event bubbling deeper than the maximum tree depth")
		}
		n := a.store.Get(h)
		nd := &n.Payload
		if nd.interactor != nil && !nd.disabled {
			a.diag.InputEvents++
			c := &a.in.ectx
			prevH, prevD := c.cur, c.nd
			c.cur, c.nd = h, nd
			handled := nd.interactor.HandleEvent(c, e)
			c.cur, c.nd = prevH, prevD
			if handled || direct {
				return handled
			}
		} else if direct {
			return false
		}
		h = n.Parent
	}
	return false
}

// deliverKey sends e to the focused node and up its ancestor chain.
func (a *App) deliverKey(e Event) bool {
	h := a.in.focus
	if !a.store.Valid(h) {
		return false
	}
	return a.deliver(h, e, false)
}

// markNeedsPaint flags h and its ancestors as needing to be drawn again. It
// does not mark anything as needing a build or a layout, which is what makes
// hover and press free of rebuilds.
func (a *App) markNeedsPaint(h scene.Handle) {
	a.needsPaint = true
	for depth := 0; !h.IsZero() && a.store.Valid(h); depth++ {
		if depth > scene.MaxDepth {
			return
		}
		n := a.store.Get(h)
		if n.Flags&scene.FlagNeedsPaint != 0 {
			return
		}
		n.Flags |= scene.FlagNeedsPaint
		h = n.Parent
	}
}

// forgetNode drops every reference the dispatcher holds to h. It runs when a
// node is unmounted, so that a hover, a capture or the focus cannot survive
// the node it pointed at.
func (a *App) forgetNode(h scene.Handle) {
	if a.in.focus == h {
		a.in.focus = scene.Handle{}
	}
	// A fling on an unmounted container has nothing left to move. The tick
	// loop already skips invalid handles, so this only keeps the slice from
	// growing across a long sequence of mounts and unmounts.
	for i, f := range a.in.flings {
		if f == h {
			a.in.flings = append(a.in.flings[:i], a.in.flings[i+1:]...)
			break
		}
	}
	for i := range a.in.pointers {
		p := &a.in.pointers[i]
		if p.over == h {
			p.over = scene.Handle{}
		}
		if p.capture == h {
			// The node the press belongs to is gone. The pointer keeps
			// existing and stays down — the physical button really is still
			// held — but it captures nothing, so the eventual release is
			// delivered nowhere rather than to whatever took the slot, and
			// [App.PointerMove] keeps the hover frozen until that release.
			//
			// Nothing is dispatched here. The only node entitled to an
			// [EventPointerCancel] is the one that took the press, and it no
			// longer exists; an ancestor did not take it and has no gesture
			// to cancel. See [EventPointerCancel].
			p.capture = scene.Handle{}
		}
	}
}

func dist2(a, b geom.Point) float32 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}
