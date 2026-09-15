package gift

import (
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// This file is gift's whole contribution to the on-screen keyboard of the
// project plan, section 19, and it is deliberately three small mechanisms
// rather than a keyboard:
//
//   - a declaration that the focused node wants character input, so that
//     something else in the process can put a keyboard on the screen. gift
//     draws nothing and knows no layout; ui.OnScreenKeyboard does both.
//   - three forwarders that let an interactor inject input through the very
//     entry points a backend uses, so that a drawn key is the same event as a
//     physical one rather than a parallel path with its own bugs.
//   - an obstruction, which is the part [App.ScrollIntoView] needs in order to
//     keep the focused field clear of whatever was put on top of it.
//
// None of it mentions a keyboard layout, a language or a widget, because none
// of that belongs below ui.

// softInputState is the process's answer to "does the focused node want
// characters, and what is covering the screen because of it".
type softInputState struct {
	// wanted is the last value passed to [EventContext.RequestSoftKeyboard].
	wanted bool

	// obstruct is the node that most recently declared [Element.Obstructs],
	// or the zero handle when none is mounted.
	obstruct scene.Handle

	// revealPending is set when obstruct changed — a keyboard appeared, moved
	// or went away — and cleared by the reveal [App.BeginInput] performs for
	// it.
	//
	// The deferral is not a nicety, it is the only order that can work. A
	// field asks for the keyboard from its own EventFocusGained handler, in
	// the input phase; the keyboard is built and laid out afterwards, in the
	// same frame's update; and its bounds are assigned after that. So at the
	// moment the field calls [EventContext.ScrollIntoView] there is nothing
	// to avoid yet, and the scroll that avoids it can only happen one frame
	// later. That frame is this flag.
	revealPending bool
}

// RequestSoftKeyboard declares whether the receiving node wants an on-screen
// keyboard, and asks for a rebuild when the answer changed.
//
// # What it is
//
// It is a declaration and not a command. gift has no keyboard to raise:
// Ebitengine offers no way to open a native one and the kiosk of the project
// plan, section 1, has no operating system keyboard to open. What this does is
// publish one bit that [BuildContext.SoftKeyboardRequested] reads, so that a
// view somewhere else in the tree — ui.OnScreenKeyboard is the one gift ships
// — can decide to draw one. A backend on a platform that *does* have a native
// keyboard would read the same bit.
//
// The rebuild is included for the reason [ui.SetTheme] includes its
// [App.Invalidate]: nothing on the screen changes until the views are built
// again, and a caller who had to remember that separately would have a
// keyboard that appears one unrelated interaction late.
//
// # Policy is not here
//
// gift does not decide whether the keyboard is appropriate. A text field on a
// desktop calls this only when the application turned the kiosk switch on; see
// ui.SetOnScreenKeyboard for why that switch exists and why it is emphatically
// not [Event.Device] or [PointerKind].
func (c *EventContext) RequestSoftKeyboard(want bool) { c.app.requestSoftKeyboard(want) }

func (a *App) requestSoftKeyboard(want bool) {
	if a.in.soft.wanted == want {
		return
	}
	a.in.soft.wanted = want
	a.Invalidate()
}

// SoftKeyboardRequested reports whether the focused node asked for an
// on-screen keyboard; see [EventContext.RequestSoftKeyboard].
func (a *App) SoftKeyboardRequested() bool { return a.in.soft.wanted }

// SoftKeyboardRequested reports whether the focused node asked for an
// on-screen keyboard. It is [App.SoftKeyboardRequested] read from inside a
// build, which is where a view that draws one has to decide.
//
// A nil BuildContext answers false. Views in this project take the context by
// pointer and most of them ignore it entirely, so a unit test that calls Build
// directly passes nil; answering false there is the same as "nobody is typing"
// and is strictly better than a nil dereference inside a widget.
func (b *BuildContext) SoftKeyboardRequested() bool {
	if b == nil {
		return false
	}
	return b.app.in.soft.wanted
}

// TypeRune injects one character as though the platform's keyboard had
// produced it. It is [App.TypeRune] called from inside an event handler.
//
// This is how a drawn key becomes text, and the indirection is the whole
// point: the rune travels the ordinary [EventRune] path to the focused node
// and bubbles like any other, so a widget cannot tell it from a physical key
// press and therefore cannot handle the two differently. There is no second
// code path to keep in step.
//
// The one honest difference from a physical key, stated rather than hidden: a
// physical 'a' also produces a [EventKeyDown] carrying [KeyA], because a
// keyboard reports a position as well as a character. A drawn letter key sends
// only the character. Nothing in gift reads a letter key except as the operand
// of [ShortcutModifier] — see [Key] — and an on-screen keyboard offers no such
// modifier, so the missing press has no reader. A drawn key that *is* a
// position, backspace and enter and space, sends the press as well; see
// [EventContext.KeyDown].
//
// Re-entrancy is fine and is what this relies on: [App.deliver] saves and
// restores the receiving node of the context around every handler, so
// dispatching from inside a handler leaves this one exactly as it found it.
func (c *EventContext) TypeRune(r rune) { c.app.TypeRune(r) }

// KeyDown injects a key press, as [App.KeyDown] does for the backend. It is
// the other half of [EventContext.TypeRune] and exists for the keys that are
// positions rather than characters: backspace and enter carry no rune at all.
//
// A press injected here arms gift's key repeat exactly like a physical one, so
// a finger resting on a drawn backspace deletes at [KeyRepeatInterval] without
// the widget owning a timer. The release must be injected too, or the repeat
// runs until something else cancels it; see [EventContext.KeyUp].
func (c *EventContext) KeyDown(k Key, mods Mods) { c.app.KeyDown(k, mods) }

// KeyUp injects a key release; see [EventContext.KeyDown].
func (c *EventContext) KeyUp(k Key, mods Mods) { c.app.KeyUp(k, mods) }

// setObstruction records h as the node that covers part of the viewport, or
// clears the record when h is zero. It runs during reconciliation.
func (a *App) setObstruction(h scene.Handle) {
	if a.in.soft.obstruct == h {
		return
	}
	a.in.soft.obstruct = h
	a.in.soft.revealPending = true
}

// obstruction returns the device space rectangle the scroll reveal has to keep
// the target clear of, and false when nothing obstructs.
func (a *App) obstruction() (geom.Rect, bool) {
	h := a.in.soft.obstruct
	if !a.store.Valid(h) {
		return geom.Rect{}, false
	}
	_, m, ok := a.deviceSpace(h)
	if !ok {
		return geom.Rect{}, false
	}
	r := m.TransformRect(a.store.Get(h).Bounds)
	if r.IsEmpty() {
		return geom.Rect{}, false
	}
	return r, true
}

// tickReveal performs the one scroll that an obstruction appearing or
// disappearing calls for. It runs once in [App.BeginInput], before any event
// of the new frame is delivered.
//
// It allocates nothing and does nothing at all in the steady state: the flag
// is false on every frame in which no keyboard came or went, which is all of
// them except two per focus change.
func (a *App) tickReveal() {
	if !a.in.soft.revealPending {
		return
	}
	a.in.soft.revealPending = false
	if h := a.in.focus; a.store.Valid(h) {
		a.ScrollIntoView(NodeRef{h})
	}
}
