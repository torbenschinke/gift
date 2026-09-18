package ebiten

import (
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
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
//   - Characters: [eb.AppendInputChars], which appends into a caller supplied
//     slice and whose documentation names it "the environment's
//     locale-dependent translation of keyboard input to Unicode characters",
//     as opposed to a Key, which "represents a physical key of US keyboard
//     layout". These are two different channels and this file polls both:
//     see [inputBridge.pollRunes]. Until the project plan, section 19, made
//     the point, only the key half existed here, which meant that no
//     printable character reached gift at all.
//   - Key repeat: there is none. A search of the module for "repeat" finds
//     nothing outside gamepad code and one GLFW mouse callback that discards
//     glfw.Repeat; the closest thing is inpututil.KeyPressDuration, which
//     counts *ticks* a key has been held. gift builds the repeat itself, on
//     its own clock and above this file — see [gift.KeyRepeatDelay] — so the
//     bridge stays what it is, an edge detector, and every backend gets the
//     same delay and rate without writing it again.
//
// # The function values
//
// Every Ebitengine reading this file takes goes through a field of this struct
// rather than through the package function directly — [inputBridge.keyPressed]
// and [inputBridge.appendChars] for the keyboard, [inputBridge.cursor] and
// [inputBridge.wheel] for the mouse. They are set once, in [newInputBridge],
// and they exist so that the edge detection, the rune forwarding, the key
// table and the density conversion can be driven from a test: an Ebitengine
// global means nothing outside a running game loop, and before this seam
// existed the bridge's own test had to *reimplement* the diff it was testing.
// A func value of a package level function is a pointer to a static funcval,
// so the seam costs one indirect call per reading and allocates nothing.
//
// # Coordinates
//
// Everything Ebitengine reports is in the coordinate space of the screen
// image, which after [game.LayoutF] is the *physical* framebuffer: its own
// documentation of CursorPositionF says the position "is 'logical' position
// and this considers the scale of the screen", and the screen gift asks for
// is the device sized one. gift's pointer model, its hit testing and its
// scroll offsets are in logical pixels, so this is the one boundary where the
// density is divided out again. See [gift.App.SetDensity] for why the input
// half is not scaled instead.
type inputBridge struct {
	app   *gift.App
	start time.Time

	// density is the factor physical positions are divided by. It is
	// refreshed from the App once per poll, because a window dragged to
	// another monitor changes it between two ticks.
	density float32

	// keyPressed and appendChars are the keyboard readings; see the type
	// comment.
	keyPressed  func(eb.Key) bool
	appendChars func([]rune) []rune

	// cursor and wheel are the mouse readings, for the same reason and at
	// the same cost.
	//
	// They carry more weight than convenience. The cursor arrives in device
	// pixels and has to be divided by the density, and the wheel arrives as
	// a notch count and must *not* be; the difference between the two was
	// stated only in a comment until WU-AA, and a comment is not a test.
	// TestPollMouseConvertsThePositionAndNotTheWheel is.
	cursor func() (float64, float64)
	wheel  func() (float64, float64)

	// runes is the buffer [eb.AppendInputChars] appends this tick's
	// characters into. It is reused and never handed out, which is what
	// keeps the rune path inside the zero allocation contract of the project
	// plan, section 11: Ebitengine's own documentation promises that "giving
	// a slice that already has enough capacity works efficiently", and its
	// implementation is one append of the tick's runes onto the slice it was
	// given, so the promise is structural and not a hope.
	//
	// It is measured rather than believed, in two halves, because neither
	// half alone is honest. BenchmarkPollRunes drives this function through
	// the appendChars seam with a stand in that appends exactly the way
	// Ebitengine's does, and is 0 B/op with characters actually flowing.
	// BenchmarkAppendInputChars calls the real function, which outside a
	// running game loop has no characters to report and therefore only
	// proves that the empty case allocates nothing. A test binary cannot
	// press a key on the host keyboard, and saying so is better than a
	// benchmark that looks like it did.
	//
	// The capacity is what a human can type between two ticks at 60 Hz plus
	// a wide margin; a paste does not arrive through this channel at all.
	// Overflowing it is not an error, it grows once and stays grown.
	runes []rune

	// keys is the set of keys gift has a name for, together with the
	// Ebitengine key that produces it. A fixed table rather than a scan of
	// all 100-odd key codes: gift names a couple of dozen keys — see
	// [trackedKeys], which section 19 of the project plan widened by the
	// editing keys and the five shortcut letters — and polling that many
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

	// Editing. Backspace and delete are what a text field cannot do without,
	// and both are keys rather than characters on every platform: the
	// character callback never reports them, because they are not printable.
	{eb.KeyBackspace, gift.KeyBackspace},
	{eb.KeyDelete, gift.KeyDelete},

	// The five shortcut letters. These entries are the one place in gift
	// where a physical key really is meant as a letter, and the reason is
	// that the shortcut is a finger position: a user on an AZERTY keyboard
	// presses the key labelled Q for select all, exactly as every other
	// application on that machine expects. Text never comes from here; it
	// comes from [inputBridge.pollRunes].
	{eb.KeyA, gift.KeyA},
	{eb.KeyC, gift.KeyC},
	{eb.KeyV, gift.KeyV},
	{eb.KeyX, gift.KeyX},
	{eb.KeyZ, gift.KeyZ},
}

var trackedButtons = [...]eb.MouseButton{eb.MouseButtonLeft, eb.MouseButtonRight, eb.MouseButtonMiddle}

func newInputBridge(app *gift.App) *inputBridge {
	return &inputBridge{
		app:     app,
		start:   time.Now(),
		density: 1,

		keyPressed:  eb.IsKeyPressed,
		appendChars: eb.AppendInputChars,
		cursor:      eb.CursorPositionF,
		wheel:       eb.Wheel,

		runes:   make([]rune, 0, 16),
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
	b.density = b.app.Density()
	b.app.SetModifiers(b.modifiers())
	b.pollKeys()
	b.pollRunes()
	b.pollMouse()
	b.pollTouches()
}

func (b *inputBridge) pollKeys() {
	mods := b.modifiers()
	for i, k := range trackedKeys {
		now := b.keyPressed(k.eb)
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

// pollRunes forwards the characters the platform produced this tick.
//
// This is the locale dependent half of the keyboard and the only way a
// printable character can reach gift. It is *not* derived from the key table
// above and must never be: [eb.IsKeyPressed] answers about a physical key of a
// US keyboard, so deriving characters from it would type 'q' on a French
// keyboard whose user pressed 'a', and would have no answer at all for an
// umlaut, an accent or anything behind AltGr.
//
// It is also not an IME. Ebitengine has no composition API and the project
// plan, section 14, keeps CJK candidate windows out of scope. What does pass
// through here is everything a dead key or a compose sequence resolved to,
// because by the time the platform reports a character the composition is
// over — that is ordinary localised typing and not an IME.
//
// Two filters are already applied before the buffer is read, both by
// Ebitengine and neither repeated here: its InputState.appendRune drops
// everything that is not unicode.IsPrint, and its GLFW character callback
// "skips the characters that are produced with the modifier combinations the
// platform treats as shortcuts, like Ctrl+= on X11". So control-V delivers no
// 'v' to a text field, and gift needs no modifier test of its own.
func (b *inputBridge) pollRunes() {
	b.runes = b.appendChars(b.runes[:0])
	for _, r := range b.runes {
		b.app.TypeRune(r)
	}
}

func (b *inputBridge) modifiers() gift.Mods {
	var m gift.Mods
	if b.keyPressed(eb.KeyShift) {
		m |= gift.ModShift
	}
	if b.keyPressed(eb.KeyControl) {
		m |= gift.ModControl
	}
	if b.keyPressed(eb.KeyAlt) {
		m |= gift.ModAlt
	}
	if b.keyPressed(eb.KeyMeta) {
		m |= gift.ModMeta
	}
	return m
}

// logical converts a position Ebitengine reported in framebuffer pixels into
// the logical pixels gift's pointer model works in. At density 1 it is the
// identity, and the division is by an integer power of the density, so a
// whole physical coordinate stays whole for the 2x case this exists for.
func (b *inputBridge) logical(x, y float64) geom.Point {
	if b.density == 1 || !(b.density > 0) {
		return geom.Pt(float32(x), float32(y))
	}
	return geom.Pt(float32(x)/b.density, float32(y)/b.density)
}

func (b *inputBridge) pollMouse() {
	x, y := b.cursor()
	pos := b.logical(x, y)
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

	if wx, wy := b.wheel(); wx != 0 || wy != 0 {
		// The wheel is *not* divided. It is a notch count and not a length;
		// see [gift.ScrollConfig.WheelStep], which turns one notch into a
		// distance in logical pixels.
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
		pos := b.logical(x, y)
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
