package gift

import (
	"runtime"
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
	// EventRune is one character the user typed, delivered to the focused
	// node and bubbled like a key event. [Event.Rune] carries it.
	//
	// It is a different channel from [EventKeyDown] on purpose, and the
	// difference is the whole reason this event exists. A [Key] is a
	// *physical* key identified by its position on a US keyboard —
	// Ebitengine says so of its own Key type, "KeyQ represents Q key on US
	// keyboards and ' (quote) key on Dvorak keyboards" — while a rune is
	// what the platform's keyboard layout, its dead keys and its AltGr
	// combinations produced from that key press. On a German layout the key
	// in the position of the US semicolon yields 'ö', and no mapping of key
	// codes to characters inside gift could know that.
	//
	// So: a view that edits text listens to EventRune for the text and to
	// EventKeyDown for the commands — backspace, the arrows, the shortcuts.
	// A view that never listens to EventRune sees no characters at all,
	// which is the correct behaviour for a button.
	//
	// There is no guaranteed one to one relation with key events in either
	// direction. One key press can produce no rune (a dead key waiting for
	// the next one), one rune (the normal case), or a rune that arrives on a
	// later key press (the dead key resolving). Holding a key produces
	// further runes at whatever rate the platform repeats at, which is not
	// gift's [KeyRepeatInterval]; see [App.TypeRune].
	EventRune

	// EventFocusGained and EventFocusLost bracket the time a node holds the
	// keyboard focus.
	EventFocusGained
	// EventFocusLost is the counterpart of EventFocusGained.
	EventFocusLost
)

// Key is a physical key, in the small set gift's own navigation, activation
// and text editing need.
//
// # It is still not a keyboard map, and the boundary moved once
//
// A Key names the *position* of a key on a US keyboard, not the character it
// produces; that is Ebitengine's definition and gift inherits it rather than
// inventing a second one. Characters travel on their own channel,
// [EventRune], because only the platform knows what a layout makes of a key
// press. See [App.TypeRune].
//
// The original boundary was "what focus order and activation need": tab,
// space, enter, escape, the arrows, home, end and the two page keys. The
// project plan, section 19, widened it, and the new boundary is:
//
//   - the keys that move a cursor or delete text, because a text field cannot
//     be written without them and because they are the keys gift itself
//     repeats; see [KeyRepeatDelay].
//   - the letters that appear in the five editing shortcuts every platform
//     agrees on — select all, copy, cut, paste, undo — and nothing else. They
//     are here as the operand of a modifier, never as a source of text: a
//     handler that reads [KeyA] to insert an 'a' is wrong on every keyboard
//     outside the United States, and that is why the alphabet is not here in
//     full.
//
// Everything else a backend sees is reported as [KeyOther], which nothing in
// gift acts on but an application may. The cost of widening the enum is a
// larger table in every backend and one more poll per tick per entry, so it
// is widened for a named need and not for completeness.
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

	// KeyBackspace deletes backwards, KeyDelete forwards. Both repeat; see
	// [KeyRepeatDelay].
	KeyBackspace
	KeyDelete

	// KeyA, KeyC, KeyV, KeyX and KeyZ are the operands of select all, copy,
	// paste, cut and undo. They are physical key positions, so on a Dvorak or
	// an AZERTY keyboard they sit wherever that keyboard's US-QWERTY
	// equivalent sits — which is deliberately what every toolkit does with
	// these five shortcuts, because the user learned the finger position and
	// not the letter.
	//
	// Redo has no sixth letter here: the combination that redoes is shift
	// plus the undo one on macOS and on GTK, and KeyY would only be needed
	// for the Windows convention, which section 1 of the project plan does
	// not list as a target.
	KeyA
	KeyC
	KeyV
	KeyX
	KeyZ
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

// ShortcutModifier is the modifier that turns a letter into an editing
// command: command on macOS, control everywhere else.
//
// # Why this is one variable and not a rule each view invents
//
// Copy is command-C on a Mac and control-C on Linux. Both modifiers already
// exist in [Mods], so every view *could* decide for itself — and then every
// view would decide slightly differently, and a kiosk with a Mac keyboard
// attached to a Raspberry Pi would work in half of them. The decision is made
// once, here.
//
// The default follows [runtime.GOOS], because that is the only evidence gift
// has: Ebitengine reports the physical modifier keys and says nothing about
// what the desktop means by them. An application whose hardware disagrees —
// the Mac keyboard on the Pi above — assigns to this variable before the
// first frame.
//
// # Why a package variable and not a field of Options
//
// Not for the reason [ScrollIndicatorLinger] is one. That argument is about
// the dispatch path, and it does not transfer, because gift never reads this
// value at all: it is read by *views*, in their own event handlers, and a
// handler has an [Event] and no application handle. A field of [Options]
// would therefore have to be copied onto every event or reached through a
// context that gift's [Interactor] contract does not pass, and both of those
// are real costs paid so that a value which is a property of the keyboard
// could pretend to be a property of a window.
//
// The keyboard is the right scope. A machine has one convention for what
// "the shortcut key" is, and two windows of one process disagreeing about it
// would be a defect, not a feature. Section 1 of the project plan has one
// window anyway.
//
// # It is convention, not enforcement
//
// gift dispatches shortcuts nowhere. The five letters in
// backend/ebiten's key table reach a view as ordinary key presses with
// modifiers, and what copy means is the view's business. This variable is the
// shared answer to "which modifier", so that two views in one program cannot
// answer differently.
//
// A view uses it as e.Mods.Has(gift.ShortcutModifier), which reads the same on
// both platforms and is right on both.
var ShortcutModifier = defaultShortcutModifier()

// WordModifier is the modifier that turns a caret move into a word move:
// option on macOS, control everywhere else.
//
// It is the sibling of [ShortcutModifier] and everything that variable says
// about itself applies here: it is a property of the keyboard and not of a
// window, gift dispatches nothing on it, and an application whose hardware
// disagrees with [runtime.GOOS] assigns to it before the first frame.
//
// The two are deliberately *different* modifiers on macOS and deliberately the
// *same* one on everything else, and that is not an inconsistency — it is what
// the two conventions are. On macOS command-left is the start of the line and
// option-left is the previous word; on X11 and Windows control-left is the
// previous word and there is no chord for the start of the line beyond home.
// A field that hard coded either convention would be wrong on the other
// platform, and a field that invented a third would be wrong on both.
//
// # When the two collide, the word wins
//
// Binding rule, and it belongs here rather than in each view, because the
// collision is a property of these two values and not of any one widget: where
// this modifier and [ShortcutModifier] are the same key — which is every
// platform except macOS, and therefore the platform of the project plan,
// section 1 — a caret motion key held with it means the *word* motion. Home
// and end are then the only way to the ends of the line, which is exactly what
// the paragraph above says the X11 convention is.
//
// It cannot be resolved here, by making the two values distinct. Control is
// genuinely both "the shortcut key" and "the word key" on X11: copy is
// control-C and the previous word is control-left, and moving either of them
// to another key would invent the third convention that is wrong on both
// platforms. So what is fixed here is the precedence, and a view spells it as
// testing [WordModifier] before [ShortcutModifier] in its motion keys; see
// ui.TextField, whose key handler is the only consumer today. Shortcuts that
// are not caret motions — the letters of copy, cut, paste and select all — are
// unaffected: there is no word motion on a letter key to collide with.
var WordModifier = defaultWordModifier()

func defaultShortcutModifier() Mods {
	if runtime.GOOS == "darwin" {
		return ModMeta
	}
	return ModControl
}

func defaultWordModifier() Mods {
	if runtime.GOOS == "darwin" {
		return ModAlt
	}
	return ModControl
}

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

	// Rune is the character of an [EventRune] and is meaningless otherwise.
	//
	// It is always a printable rune: the platform filters control characters
	// out before gift ever sees them, because backspace and enter are keys
	// and arrive as such, and a text model that had to reject '\b' as well as
	// handle [KeyBackspace] would have two code paths for one user action.
	Rune rune

	// Repeat is set on an [EventKeyDown] that gift synthesised because the
	// key is being held; see [KeyRepeatDelay].
	//
	// A handler that moves a cursor ignores it — that is the point of the
	// repeat. A handler that *activates* something must not: holding enter on
	// a button would otherwise submit a form thirty times a second. The flag
	// is what lets both be written, and it is on the event rather than
	// suppressed inside the runtime because gift cannot know which of the two
	// a given interactor is.
	Repeat bool

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
//
// It is in *local* space and is therefore not the space [Event.Pos] lives in:
// the two are the same rectangle until something above the node scrolls, and
// then they differ by that offset. An interactor that compares a pointer
// position against a rectangle of its own wants [EventContext.DeviceBounds].
func (c *EventContext) Bounds() geom.Rect { return c.app.store.Get(c.cur).Bounds }

// DeviceBounds returns the rectangle of the receiving node mapped through the
// transforms of its ancestors, which is the space [Event.Pos] lives in.
//
// It is the input half of [PaintContext.DeviceBounds] and exists for the same
// reason: the two rectangles are the same one until something above the node
// scrolls, and then they differ by that offset. An interactor that compares a
// pointer position against a rectangle of its own — a scroll indicator does —
// has to compare in one space, and [Event.Pos] fixes which one.
func (c *EventContext) DeviceBounds() geom.Rect {
	_, m, ok := c.app.deviceSpace(c.cur)
	if !ok {
		return geom.Rect{}
	}
	return m.TransformRect(c.app.store.Get(c.cur).Bounds)
}

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

// RequestLayout marks the receiving node as needing another layout pass in the
// next update, without rebuilding anything.
//
// Most interactors do not need it: hover, press and focus are read back by the
// painter and change no size. A *virtualising* container does, because its
// layouter is what turns state into placed children — the gallery of the
// project plan, section 10, decides in its layouter which tile stands for
// which item and where the viewport has to move so that a keyboard cursor is
// on screen. An interactor that moves such a cursor has changed a layout
// input, and this is how it says so.
//
// It is deliberately not the same as [EventContext.Repaint]. Asking for a
// layout when a repaint would do costs a measure pass every keystroke;
// asking for a repaint when a layout was needed shows the previous frame's
// geometry, which is the harder bug to see.
func (c *EventContext) RequestLayout() { c.app.markNeedsLayout(c.cur) }

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

	down    bool
	downPos geom.Point
	downAt  time.Duration
	// dragged says the pointer has travelled further than [DragSlop] since
	// the press. It is set in [App.PointerMove], read by [App.pointerEvent]
	// into [Event.Dragged], and cleared by [pointer.endGesture] — that is,
	// by every one of the three ways a gesture can end, and not only by the
	// next press.
	//
	// It used to be cleared in [App.PointerDown] alone. A touch escaped the
	// consequence because its slot is zeroed on release, but the mouse keeps
	// slot zero for the life of the process, so after one drag every
	// button-less hover move carried Dragged and a scroll container read it
	// as the continuation of a drag. See [pointer.endGesture].
	dragged bool
	// longFired says [EventLongPress] has already been delivered for the
	// current press. It is deliberately *not* cleared by
	// [pointer.endGesture]: every reader of it in [App.BeginInput] is behind
	// a p.down test, so a stale true cannot be observed between a release and
	// the next press, and [App.PointerDown] clears it before the press it
	// belongs to can be timed. Clearing it in a second place would look
	// symmetric and would pin no behaviour any test could show.
	longFired bool
	insideCap bool
}

// endGesture records that the pointer no longer has a gesture in flight.
//
// It is called by [App.PointerUp] and [App.PointerCancel] *after* the
// terminating event has been delivered, because that event still describes the
// gesture that is ending: a button declines a release whose [Event.Dragged] is
// set, and clearing the flag first would turn a drag that ended over a button
// into a click on it.
func (p *pointer) endGesture() {
	p.down = false
	p.dragged = false
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

	// indicators is the set of scroll containers inside their
	// [ScrollIndicatorLinger] window. Same shape and same reason as flings;
	// see [App.tickIndicators].
	indicators []scene.Handle

	// anims is the set of nodes inside their [EventContext.Animate] window.
	// Same shape and same reason as indicators; see [App.tickAnimations].
	anims []animation

	// focusScan is the reusable stack of the focus traversal; see
	// [App.focusNeighbour].
	focusScan []scene.Handle

	// notices are the notifications the core owes a node but could not
	// deliver where they arose, because they arose during a build; see
	// pending.go. noticeSpare is the slice flushNotices swapped out last
	// time, kept so that the delivery allocates nothing after warmup.
	notices     []notice
	noticeSpare []notice

	// repeat is the key gift is currently repeating, if any; see
	// [KeyRepeatDelay].
	repeat keyRepeat

	// soft is the on-screen keyboard state of the project plan, section 19:
	// whether the focused node wants characters, and what is covering the
	// screen because of it. See softinput.go.
	soft softInputState
}

// keyRepeat is the state of the one key gift repeats.
//
// One, not a set: every desktop keyboard driver repeats the most recently
// pressed key and forgets the earlier ones, because a user who presses a
// second key while holding the first means the second. Tracking a set would be
// more code and would produce behaviour no keyboard has.
type keyRepeat struct {
	key    Key
	mods   Mods
	active bool
	// nextAt is the timestamp of the next synthetic press, in the clock
	// [App.BeginInput] is given.
	nextAt time.Duration
}

// Key repeat constants.
//
// # Why gift has to do this at all
//
// Ebitengine has no key repeat. A search of the pinned module for "repeat"
// finds nothing outside gamepad code and one GLFW mouse callback that discards
// glfw.Repeat; the closest thing on offer is inpututil.KeyPressDuration, which
// counts the ticks a key has been held and leaves the delay and the rate to
// the caller. The backend bridge declines inpututil for allocation reasons, so
// that tick count is not available there either — and the backend would be the
// wrong place anyway, because the repeat is a property of gift's event model
// and every backend would otherwise reinvent it. It therefore lives here,
// driven by the same injected clock as the long press, which is what makes it
// testable without a sleep.
//
// # Only keys, never characters
//
// gift repeats [KeyBackspace], [KeyDelete], the four arrows and the two page
// keys, and nothing else; see [repeatsWhenHeld] for the exclusions. The
// important one is that gift never synthesises a repeated *character*. It
// could not do it correctly: it does not know what layout, dead key or compose
// sequence produced the last rune, and re-emitting that rune would type the
// wrong thing after every dead key. Where the platform repeats characters —
// GLFW's character callback fires again for an operating system auto repeat —
// they arrive through the ordinary rune channel and gift passes them on
// without noticing that they are repeats.
const (
	// KeyRepeatDelay is how long a key must be held before the first
	// synthetic press.
	//
	// 400 ms sits between the two conventions it has to live with: X11's
	// default auto repeat delay is 500 ms and macOS's slider ranges from
	// roughly 250 to 900 ms. Shorter than about 300 ms and a deliberate
	// single press of an arrow key occasionally moves two cells; longer than
	// about 500 ms and holding a key feels broken before it starts.
	KeyRepeatDelay = 400 * time.Millisecond

	// KeyRepeatInterval is the period between synthetic presses once the
	// delay has passed, that is thirty per second.
	//
	// It is deliberately below the frame rate: at 60 Hz every second tick
	// produces one repeat, so the rate is stable under the tick loop rather
	// than being whatever the tick rate happens to be. A rate at or above the
	// frame rate would make the speed of a held arrow key depend on how busy
	// the machine is.
	KeyRepeatInterval = time.Second / 30

	// maxRepeatsPerTick bounds the catch up after a tick that took a long
	// time — a stalled frame, a debugger breakpoint, or a backend that
	// dropped to Config.IdleTPS in spite of the repaint request in
	// [App.tickKeyRepeat]. Without it, one second of stall would deliver
	// thirty presses in a single tick and a held backspace would eat a
	// paragraph.
	maxRepeatsPerTick = 4
)

// repeatsWhenHeld reports whether holding k produces synthetic presses.
//
// The set is exactly the keys whose action is meaningfully repeatable:
// deleting a character, and moving a cursor or a viewport by one step.
// Deliberately absent:
//
//   - space, enter and escape, because they *activate*. Repeating them would
//     submit a form or dismiss a dialog many times per second, and the one
//     case in which a user holds them — leaning on the key — is precisely the
//     case in which they mean it once.
//   - tab, because focus movement is a navigation the user counts out. A
//     repeating tab overshoots the field being aimed at.
//   - home and end, because a second one does nothing. Repeating an idempotent
//     action costs events and changes no pixel.
//   - the five shortcut letters, because they are only ever pressed with a
//     modifier and none of the five operations wants to happen thirty times a
//     second.
func repeatsWhenHeld(k Key) bool {
	switch k {
	case KeyLeft, KeyRight, KeyUp, KeyDown, KeyPageUp, KeyPageDown, KeyBackspace, KeyDelete:
		return true
	}
	return false
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
	a.tickIndicators(now)
	a.tickAnimations(now)
	a.tickKeyRepeat(now)
	a.tickReveal()
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
//
// It also ends the gesture, which for the mouse is not the same thing as
// ceasing to exist: see [pointer.endGesture] for what that costs when it is
// forgotten.
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
	// After the release was delivered, never before it; see
	// [pointer.endGesture].
	p.endGesture()
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
	p.endGesture()
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
//
// A key in the repeat set arms the repeat; see [KeyRepeatDelay]. The backend
// calls this once per physical press, not once per tick the key is held: the
// repeat is gift's and edge detection stays the backend's.
func (a *App) KeyDown(k Key, mods Mods) {
	a.assertInputPhase("KeyDown")
	a.in.mods = mods
	a.armKeyRepeat(k, mods)
	a.dispatchKeyDown(k, mods, false)
}

// dispatchKeyDown delivers one press, real or synthesised.
//
// The split exists so that a repeat does not re-arm the timer it was fired
// from — that would make the rate a function of how long dispatch took — and
// so that everything else about a repeat, bubbling and the tab fallback
// included, is provably the same code as a real press.
func (a *App) dispatchKeyDown(k Key, mods Mods, repeat bool) {
	e := Event{Kind: EventKeyDown, Key: k, Mods: mods, Repeat: repeat, Time: a.in.now, Pointer: MousePointer}
	if a.deliverKey(e) {
		return
	}
	if k == KeyTab {
		a.MoveFocus(!mods.Has(ModShift))
	}
}

// KeyUp reports a key release, delivered like [App.KeyDown] but never acted
// on by gift itself beyond ending a repeat.
func (a *App) KeyUp(k Key, mods Mods) {
	a.assertInputPhase("KeyUp")
	a.in.mods = mods
	if a.in.repeat.active && a.in.repeat.key == k {
		a.in.repeat.active = false
	}
	a.deliverKey(Event{Kind: EventKeyUp, Key: k, Mods: mods, Time: a.in.now, Pointer: MousePointer})
}

// TypeRune reports one character the user typed, as the platform's keyboard
// layout produced it.
//
// It is the counterpart of [App.KeyDown] and deliberately not a variant of it.
// A key press says *which key*; this says *which character*, and on every
// keyboard outside the United States the two are different questions. The
// backend gets the answer from the platform — Ebitengine's AppendInputChars,
// whose own documentation calls it "the environment's locale-dependent
// translation of keyboard input to Unicode characters" — and hands it here
// one rune at a time. The event is [EventRune]; it goes to the focused node
// and bubbles exactly like a key event.
//
// # No modifier parameter, on purpose
//
// [App.KeyDown] takes the modifiers because a key press without them is
// ambiguous. A rune is the *result* of the modifiers: shift-a is already 'A',
// and AltGr-q is already '@'. Passing them again would invite a handler to
// apply them a second time. The modifiers of the current tick are still on the
// event, from [App.SetModifiers], for the one honest use — telling a typed
// character from the tail of a shortcut — and gift itself does not filter on
// them, because the platform already does: Ebitengine's character callback
// "skips the characters that are produced with the modifier combinations the
// platform treats as shortcuts".
//
// # What this is not
//
// It is not an IME entry point. There is no composition, no preedit string and
// no candidate window; the project plan, section 14, excludes CJK composition
// and this does not sneak it in. What it does cover is everything a Latin
// keyboard needs and the previous design dropped on the floor: umlauts,
// accents, AltGr and dead keys are not an IME, they are the ordinary
// translation of key presses to characters, and they arrive here fully
// resolved — a dead key produces no rune of its own and the following key
// produces the composed one.
func (a *App) TypeRune(r rune) {
	a.assertInputPhase("TypeRune")
	a.diag.RunesTyped++
	a.deliverKey(Event{Kind: EventRune, Rune: r, Mods: a.in.mods, Time: a.in.now, Pointer: MousePointer})
}

// armKeyRepeat starts the repeat clock for k, if k repeats at all.
//
// It refuses to arm without a focused node. A repeat exists to deliver events,
// and [App.deliverKey] drops everything when nothing is focused; arming
// anyway would keep the backend at its busy tick rate for as long as somebody
// leans on an arrow key in an application that has no focus. See
// [App.tickKeyRepeat] for the repaint request that costs.
func (a *App) armKeyRepeat(k Key, mods Mods) {
	r := &a.in.repeat
	if !repeatsWhenHeld(k) || !a.store.Valid(a.in.focus) {
		r.active = false
		return
	}
	r.key, r.mods, r.active = k, mods, true
	r.nextAt = a.in.now + KeyRepeatDelay
}

// cancelKeyRepeat stops the repeat. It is called when the focus moves, because
// the node the held key was typing into is no longer the node that would
// receive the next synthetic press — and a backspace that continues into a
// different text field is the worst version of this feature.
func (a *App) cancelKeyRepeat() { a.in.repeat.active = false }

// tickKeyRepeat delivers the synthetic presses that are due, and keeps the
// application awake while a key is held.
//
// The repaint request is the same device [App.tickIndicators] uses for the
// scroll indicator linger, and for the same reason: it is [App.NeedsPaint]
// that the backend's idle policy reads, and a backend at Config.IdleTPS would
// sample this function ten times a second and deliver the repeats in visible
// bursts of three. Unlike the indicator linger the cost is not bounded by a
// timer but by the user letting go of the key, which is the one form of "keep
// ticking" a user can see themselves causing.
//
// It allocates nothing: the state is a struct inside the App and the events
// are values.
func (a *App) tickKeyRepeat(now time.Duration) {
	r := &a.in.repeat
	if !r.active {
		return
	}
	if !a.store.Valid(a.in.focus) {
		// The focused node was unmounted under the held key. There is
		// nowhere to deliver and nothing to repaint.
		r.active = false
		return
	}
	a.markNeedsPaint(a.in.focus)
	for n := 0; r.active && now >= r.nextAt; n++ {
		if n >= maxRepeatsPerTick {
			// Drop the backlog rather than work through it; see
			// [maxRepeatsPerTick].
			r.nextAt = now + KeyRepeatInterval
			return
		}
		r.nextAt += KeyRepeatInterval
		a.diag.KeyRepeats++
		a.dispatchKeyDown(r.key, r.mods, true)
	}
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
	// Nothing can be told anything about a node that is gone. This is the one
	// place a deferred notification is genuinely dropped rather than delayed,
	// and it is the case in which there is no recipient; see pending.go.
	a.forgetNotices(h)
	if a.in.focus == h {
		a.in.focus = scene.Handle{}
		// The focused node was unmounted, so it never got its
		// [EventFocusLost] and never withdrew its own request for an
		// on-screen keyboard; see [EventContext.RequestSoftKeyboard], which
		// is the only thing that ever sets that bit and is only ever called
		// by the node that has the focus. Deferring the notification, which
		// is what a node that merely stopped being focusable gets, is not
		// available here: by the time the queue is flushed the node does not
		// exist.
		//
		// Without this a navigation that replaces a screen while a text
		// field on it is focused leaves a keyboard on a kiosk with nothing
		// to type into: ui.OnScreenKeyboard keeps building itself, every
		// drawn key delivers a rune that [App.deliverKey] drops on the
		// floor, and there is no interaction that takes it away again.
		//
		// The caret enrolment and the field's own drag state need no
		// equivalent: [App.tickAnimations] drops an enrolment whose node is
		// no longer valid, and the drag mode dies with the node that held
		// it. The soft keyboard request is the only piece of state a node
		// can set that outlives the node.
		a.requestSoftKeyboard(false)
	}
	if a.in.soft.obstruct == h {
		// The keyboard was unmounted. What it covered is visible again, so
		// the reveal that pushed the focused field up may now be undone by
		// nothing at all — a scroll container does not scroll back on its
		// own, and deliberately so: see [App.tickReveal].
		a.setObstruction(scene.Handle{})
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
	// The same for the indicator linger: an unmounted container has no bar to
	// fade out. The tick already skips invalid handles; this keeps the slice
	// from growing across a long sequence of mounts and unmounts.
	for i, f := range a.in.indicators {
		if f == h {
			a.in.indicators = append(a.in.indicators[:i], a.in.indicators[i+1:]...)
			break
		}
	}
	// An unmounted node has no painter left to animate. The tick already
	// skips invalid handles; this keeps the slice from growing across a long
	// sequence of mounts and unmounts, exactly like the two above.
	a.stopAnimating(h)
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
