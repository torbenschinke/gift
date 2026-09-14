package ebiten

import (
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// inputBridge translates Ebitengine's polled input state into gift's event
// model.
//
// # Why a translation and not a forwarding
//
// Ebitengine has no event queue. Everything about input is a *poll*: the
// cursor position, whether a key is down, which touches exist right now. Even
// inpututil, the official helper, is nothing but a snapshot of the previous
// tick kept in a map so that "just pressed" can be computed by comparison —
// see its inputState.update in the pinned source. gift, on the other hand, is
// event driven, because press, release, capture and focus are transitions and
// not levels.
//
// So somebody has to do the edge detection, and this is that somebody. It
// keeps the previous tick's state in fixed size arrays, compares, and calls
// the matching [gift.App] methods.
//
// inpututil is deliberately not used. It installs a process wide
// BeforeUpdate hook and keeps its touch and gamepad state in maps, which
// allocate as fingers come and go; and it would only give back what is
// computed here in a few dozen lines from the same three primitives. The
// project plan, section 11, puts input processing inside the zero allocation
// contract, and a map keyed by touch id is not that.
//
// # What Ebitengine 2.10.1 actually offers
//
//   - Touch: [eb.AppendTouchIDs] with a caller supplied buffer, and
//     [eb.TouchPositionF]. Its own documentation says "AppendTouchIDs always
//     does nothing on desktops", so the touch path here is exercised on
//     Android and iOS builds and is dead code on the Raspberry Pi's X11
//     session. A Pi touchscreen arrives as a mouse through the display
//     server, which is the honest state of affairs and not a gap in this
//     file.
//   - Key repeat: there is none. A search of the module for "repeat" finds
//     nothing outside gamepad code; the closest thing is
//     inpututil.KeyPressDuration, which counts *ticks* a key has been held.
//     Anything with an initial delay and a rate has to be built on top of
//     that tick count, and gift does not build it, because nothing in this
//     work unit repeats: space and enter activate on the edge, and tab moves
//     the focus on the edge. A text field would need it; there is no text
//     field, and section 14 excludes the editor.
type inputBridge struct {
	app   *gift.App
	start time.Time

	// keys is the set of keys gift has a name for, together with the
	// Ebitengine key that produces it. A fixed table rather than a scan of
	// all 100-odd key codes: gift acts on nine keys and polling nine
	// booleans per tick is free, polling a hundred is not.
	down [len(trackedKeys)]bool

	mouse [3]bool

	// ids and pos are this tick's touches, prevIDs and prevPos the previous
	// tick's. The two pairs are swapped at the end of each tick, so the four
	// buffers are allocated once and never again.
	ids, prevIDs []eb.TouchID
	pos, prevPos []geom.Point
}

// trackedKeys maps the keys gift knows to Ebitengine's codes.
//
// Both space and enter have a second physical key that means the same thing —
// the numeric keypad enter — and both are listed, because a user who presses
// the one on the keypad means enter.
var trackedKeys = [...]struct {
	eb  eb.Key
	key gift.Key
}{
	{eb.KeyTab, gift.KeyTab},
	{eb.KeySpace, gift.KeySpace},
	{eb.KeyEnter, gift.KeyEnter},
	{eb.KeyNumpadEnter, gift.KeyEnter},
	{eb.KeyEscape, gift.KeyEscape},
	{eb.KeyArrowLeft, gift.KeyLeft},
	{eb.KeyArrowRight, gift.KeyRight},
	{eb.KeyArrowUp, gift.KeyUp},
	{eb.KeyArrowDown, gift.KeyDown},
	{eb.KeyHome, gift.KeyHome},
	{eb.KeyEnd, gift.KeyEnd},
	{eb.KeyPageUp, gift.KeyPageUp},
	{eb.KeyPageDown, gift.KeyPageDown},
}

var trackedButtons = [...]eb.MouseButton{eb.MouseButtonLeft, eb.MouseButtonRight, eb.MouseButtonMiddle}

func newInputBridge(app *gift.App) *inputBridge {
	return &inputBridge{
		app:     app,
		start:   time.Now(),
		ids:     make([]eb.TouchID, 0, 8),
		prevIDs: make([]eb.TouchID, 0, 8),
		pos:     make([]geom.Point, 0, 8),
		prevPos: make([]geom.Point, 0, 8),
	}
}

// poll reads the current input state and dispatches the transitions into gift.
//
// It runs at the beginning of Ebitengine's Update, before [gift.App.Update],
// so every handler it triggers runs inside Update and every state write it
// causes is picked up by the build that follows in the same tick. The project
// plan, section 6, allows nothing else.
//
// It allocates nothing: the touch buffers are reused and every other reading
// is a scalar.
func (b *inputBridge) poll() {
	b.app.BeginInput(time.Since(b.start))
	b.app.SetModifiers(modifiers())
	b.pollKeys()
	b.pollMouse()
	b.pollTouches()
}

func (b *inputBridge) pollKeys() {
	mods := modifiers()
	for i, k := range trackedKeys {
		now := eb.IsKeyPressed(k.eb)
		if now == b.down[i] {
			continue
		}
		b.down[i] = now
		if now {
			b.app.KeyDown(k.key, mods)
		} else {
			b.app.KeyUp(k.key, mods)
		}
	}
}

func modifiers() gift.Mods {
	var m gift.Mods
	if eb.IsKeyPressed(eb.KeyShift) {
		m |= gift.ModShift
	}
	if eb.IsKeyPressed(eb.KeyControl) {
		m |= gift.ModControl
	}
	if eb.IsKeyPressed(eb.KeyAlt) {
		m |= gift.ModAlt
	}
	if eb.IsKeyPressed(eb.KeyMeta) {
		m |= gift.ModMeta
	}
	return m
}

func (b *inputBridge) pollMouse() {
	x, y := eb.CursorPositionF()
	pos := geom.Pt(float32(x), float32(y))
	b.app.PointerMove(gift.MousePointer, gift.PointerMouse, pos)

	for i, btn := range trackedButtons {
		now := eb.IsMouseButtonPressed(btn)
		if now == b.mouse[i] {
			continue
		}
		b.mouse[i] = now
		// Only the primary button drives the pointer model. gift has no
		// notion of a secondary click yet, and inventing one that no view
		// reads would be a modifier nobody honours.
		if btn != eb.MouseButtonLeft {
			continue
		}
		if now {
			b.app.PointerDown(gift.MousePointer, gift.PointerMouse, pos)
		} else {
			b.app.PointerUp(gift.MousePointer, gift.PointerMouse, pos)
		}
	}

	if wx, wy := eb.Wheel(); wx != 0 || wy != 0 {
		b.app.PointerWheel(pos, geom.Pt(float32(wx), float32(wy)))
	}
}

// pollTouches turns the current set of touch identifiers into down, move and
// up events by comparing it with the set of the previous tick.
//
// Additional fingers are passed to gift and rejected there rather than being
// filtered here, so that the discard is counted in one place and the rule
// lives with the pointer model instead of with the platform adapter; see
// [gift.Diagnostics.DiscardedTouches].
func (b *inputBridge) pollTouches() {
	b.ids = eb.AppendTouchIDs(b.ids[:0])
	if len(b.ids) == 0 && len(b.prevIDs) == 0 {
		return
	}
	b.pos = b.pos[:0]
	for _, id := range b.ids {
		x, y := eb.TouchPositionF(id)
		pos := geom.Pt(float32(x), float32(y))
		b.pos = append(b.pos, pos)
		if indexOf(b.prevIDs, id) >= 0 {
			b.app.PointerMove(gift.PointerID(id), gift.PointerTouch, pos)
			continue
		}
		b.app.PointerDown(gift.PointerID(id), gift.PointerTouch, pos)
	}
	for i, id := range b.prevIDs {
		if indexOf(b.ids, id) >= 0 {
			continue
		}
		// The remembered position, not a fresh query: Ebitengine forgets a
		// touch the moment it ends and then answers (0, 0) for it, which
		// would report every release in the top left corner and turn a tap
		// into a press that was dragged away.
		b.app.PointerUp(gift.PointerID(id), gift.PointerTouch, b.prevPos[i])
	}
	b.ids, b.prevIDs = b.prevIDs, b.ids
	b.pos, b.prevPos = b.prevPos, b.pos
}

func indexOf(ids []eb.TouchID, id eb.TouchID) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}
