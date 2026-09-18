package ui

import (
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// TextEditor is the document a [TextFieldView] edits, together with the state
// of editing it: the caret, the selection anchor, the horizontal scroll of the
// field and the phase of the blink.
//
// It is created by [NewTextEditor] or, inside a component, by [Editor]. The
// zero value is not usable.
//
// # Why the state lives here and not in the view
//
// Because a view does not survive a rebuild and the caret has to. Typing one
// character writes the application's state, the component rebuilds, and gift
// installs a freshly built node — so every field of that node is new. A caret
// kept there would return to zero after each keystroke, and a selection would
// be impossible to make at all.
//
// This is the same answer [Gallery] gives to the same question and it is the
// idiom of this package for a widget with a long lived model: the object
// belongs to the application, the view is a declaration over it. gift's own
// presentation state — hover, press, focus, the scroll offset of a container —
// lives in the retained node instead, because gift owns those; a text document
// is not gift's to own.
//
// # One editor, one field
//
// A TextEditor is a mutable object with one caret. Two fields over one editor
// would be two carets writing one position, so the second mount is rejected
// with a diagnosis rather than supported; see [TextFieldView].
//
// # Offsets are byte offsets
//
// Every position in this API — the caret, both ends of the selection — is a
// byte offset into [TextEditor.Text] and always sits on a rune boundary. Bytes
// and not runes, because that is what the shaping result speaks: a glyph
// carries the byte index of the first byte of its cluster, so a byte offset is
// the one unit in which the caret, the selection and the hit test cannot
// disagree. An offset that falls inside a rune is snapped to the boundary
// below it rather than rejected.
type TextEditor struct {
	// buf is the document. It is a byte slice and not a string for the reason
	// the project plan, section 11, exists: inserting a character into a
	// string builds a new string, so typing a hundred characters into a
	// hundred character field would allocate a hundred times and copy five
	// thousand bytes. Here an insertion is an append into spare capacity plus
	// a copy of the tail, and the capacity is amortised — a field that has
	// held a long line never allocates for a short one again.
	buf []byte
	// str is the cached string form of buf, and strOK says whether it is
	// current.
	//
	// It exists because everything that measures or draws the text needs a
	// string: [text.Request] is keyed by one and the shaping cache keeps it.
	// So exactly one string is built per edit, lazily, and every read inside
	// the frame — layout, paint, the caret arithmetic — takes the cached one
	// and allocates nothing. That is the single allocation per keystroke the
	// performance contract permits, and it is "the text itself growing".
	//
	// It is a real copy and never an unsafe alias of buf. The shaper retains
	// the string as a cache key for as long as the entry lives, and a key
	// that changes under the map would return another field's glyphs.
	str   string
	strOK bool

	// caret is where the next insertion goes; anchor is the other end of the
	// selection. They are equal exactly when nothing is selected, which is
	// why there is no separate "has selection" flag to keep in step.
	caret  int
	anchor int

	// scroll is how far the text is shifted to the left inside the field, in
	// logical pixels, and is never negative. A single line field scrolls
	// itself rather than living in an [HScroll]: the content of a scroll
	// container is its children, and the glyphs of a field are not children
	// of anything.
	scroll float32

	// blinkAt is when the current blink cycle began. Every edit and every
	// caret move resets it, which is what makes the caret solid while
	// somebody is typing instead of flashing out from under the character
	// they just wrote.
	blinkAt time.Duration
	// blinkUntil is when blinking stops and the caret goes solid; see
	// [CaretBlinkWindow].
	blinkUntil time.Duration

	// drag is the state of the pointer gesture that is selecting text, if
	// one is; see [dragState] for why it has three values and not two. It
	// survives a rebuild for the same reason the caret does — a keystroke
	// from a timer in the middle of a drag would otherwise drop the gesture.
	drag dragState
	// dragFrom is where the press that started the gesture landed, in device
	// space, and dragWordLo/dragWordHi the run a double click selected. The
	// first decides whether the gesture is a selection or a scroll, the
	// second two are the fixed end of a word-wise drag.
	dragFrom               geom.Point
	dragWordLo, dragWordHi int
	// clickAt, clickX and clicks recognise the multi click. gift has no
	// double click event — it reports presses and the time they happened, and
	// what counts as a double click is a property of the control — so the
	// field counts them itself; see [DoubleClickInterval].
	clickAt time.Duration
	clickX  float32
	clicks  int

	// owner and ownerPass are the double mount guard; see [TextEditor].
	owner     gift.NodeRef
	ownerPass uint64
	haveOwner bool
}

// NewTextEditor returns an editor holding s, with the caret after the last
// character and nothing selected.
//
// The caret goes to the end rather than to the beginning because that is where
// a field that is shown with a value already in it is meant to be continued,
// and it is what every platform does when such a field takes the focus.
func NewTextEditor(s string) *TextEditor {
	e := &TextEditor{}
	e.SetText(s)
	return e
}

// Editor returns the editor named name inside the component instance ctx
// belongs to, creating it with initial on first use.
//
// It is the short way to give a field a home that outlives a rebuild, and it
// is an ordinary [gift.Context.State] underneath, so the editor is unmounted
// with its component like any other state:
//
//	func form(ctx *gift.Context) gift.View {
//	    name := ui.Editor(ctx, "name", "")
//	    return ui.TextField(name).OnChange(func(s string) { ... })
//	}
//
// An application that already keeps its value in a [gift.State] hands the
// binding to [TextFieldView.Bind] instead and still needs an editor for the
// caret; the two are not alternatives, they answer different questions.
//
// initial is used on the first build only, exactly like the initial value of
// any other state. Changing it later changes nothing; write [TextEditor.SetText]
// or go through a binding.
func Editor(ctx *gift.Context, name, initial string) *TextEditor {
	st := ctx.State(name, (*TextEditor)(nil))
	if e := st.Get(); e != nil {
		return e
	}
	e := NewTextEditor(initial)
	st.Set(e)
	return e
}

// Text returns the current contents.
//
// The string is built once per edit and cached, so calling this in a layouter
// or a painter is free; see [TextEditor].
func (e *TextEditor) Text() string {
	if !e.strOK {
		e.str, e.strOK = string(e.buf), true
	}
	return e.str
}

// Len returns the length of the text in bytes.
func (e *TextEditor) Len() int { return len(e.buf) }

// SetText replaces the whole document, puts the caret at the end and clears
// the selection.
//
// It is the entry point for the application changing the value under the user
// — loading a record, resetting a form — and it is what [TextFieldView.Bind]
// calls when the bound state changed elsewhere.
//
// The horizontal scroll goes back to zero, and that is not tidiness. An offset
// is a position in a document, and this is a different document: a field
// scrolled four hundred pixels into a long value that is then handed a short
// one would show the empty space past the end of the new text and nothing
// else, which is a field that has gone blank for no reason a user can see.
// Zero is also the right place to start reading a value that somebody else
// wrote. The field clamps the offset in its layout as well — see
// textFieldNode.clampScroll — but that is the backstop for a width that
// changed, not the answer here.
func (e *TextEditor) SetText(s string) {
	e.buf = append(e.buf[:0], s...)
	e.str, e.strOK = s, true
	e.caret, e.anchor = len(e.buf), len(e.buf)
	e.scroll = 0
}

// Caret returns the byte offset the next character would be inserted at.
func (e *TextEditor) Caret() int { return e.caret }

// Selection returns the selected byte range as the ordered pair start, end.
// The two are equal when nothing is selected.
func (e *TextEditor) Selection() (int, int) { return e.selection() }

// HasSelection reports whether any text is selected.
func (e *TextEditor) HasSelection() bool { return e.caret != e.anchor }

// SelectedText returns the selected text, which is "" when nothing is
// selected. It allocates, so it belongs in an event handler and not in a
// frame.
func (e *TextEditor) SelectedText() string {
	lo, hi := e.selection()
	if lo == hi {
		return ""
	}
	return string(e.buf[lo:hi])
}

// SetSelection selects the byte range from a to b and puts the caret at b, so
// that a following shifted arrow extends from the end the caller named last.
// Both offsets are clamped into the text and snapped to rune boundaries.
func (e *TextEditor) SetSelection(a, b int) {
	e.anchor = e.snap(a)
	e.caret = e.snap(b)
}

// SetCaret moves the caret to off and clears the selection.
func (e *TextEditor) SetCaret(off int) {
	e.caret = e.snap(off)
	e.anchor = e.caret
}

// SelectAll selects the whole document and leaves the caret at the end.
func (e *TextEditor) SelectAll() { e.anchor, e.caret = 0, len(e.buf) }

// selection returns the caret and the anchor in order.
func (e *TextEditor) selection() (int, int) {
	if e.caret < e.anchor {
		return e.caret, e.anchor
	}
	return e.anchor, e.caret
}

// snap clamps off into the document and moves it down to the nearest rune
// boundary. An offset in the middle of a multi byte character is the one
// malformed position the rest of this type must never see: a caret there would
// split an umlaut into two bytes at the next insertion.
func (e *TextEditor) snap(off int) int {
	if off <= 0 {
		return 0
	}
	if off >= len(e.buf) {
		return len(e.buf)
	}
	for off > 0 && !utf8.RuneStart(e.buf[off]) {
		off--
	}
	return off
}

// --- editing ----------------------------------------------------------------

// Insert replaces the selection with s and leaves the caret after it. It
// reports whether the document changed.
//
// Inserting the empty string with nothing selected is not a change and is not
// reported as one, so an application's OnChange is not called for it.
func (e *TextEditor) Insert(s string) bool {
	lo, hi := e.selection()
	if s == "" && lo == hi {
		return false
	}
	e.replace(lo, hi, s)
	return true
}

// InsertRune is [TextEditor.Insert] for one character, without building a
// string for it.
//
// This is the path every keystroke takes, and it is why it exists separately:
// string(r) allocates, once per character typed, for a value that is used
// immediately and thrown away. The rune is encoded into a stack array instead
// and the only allocation left in a keystroke is the document itself growing.
func (e *TextEditor) InsertRune(r rune) bool {
	var tmp [utf8.UTFMax]byte
	n := utf8.EncodeRune(tmp[:], r)
	lo, hi := e.selection()
	e.replaceBytes(lo, hi, tmp[:n])
	return true
}

// DeleteSelection removes the selected text and reports whether there was
// any.
func (e *TextEditor) DeleteSelection() bool {
	lo, hi := e.selection()
	if lo == hi {
		return false
	}
	e.replaceBytes(lo, hi, nil)
	return true
}

// DeleteBackward removes the selection if there is one, and otherwise the rune
// before the caret. It reports whether anything was removed.
func (e *TextEditor) DeleteBackward() bool {
	if e.DeleteSelection() {
		return true
	}
	if e.caret == 0 {
		return false
	}
	e.replaceBytes(e.prevRune(e.caret), e.caret, nil)
	return true
}

// DeleteForward removes the selection if there is one, and otherwise the rune
// after the caret.
func (e *TextEditor) DeleteForward() bool {
	if e.DeleteSelection() {
		return true
	}
	if e.caret >= len(e.buf) {
		return false
	}
	e.replaceBytes(e.caret, e.nextRune(e.caret), nil)
	return true
}

// replace is replaceBytes for a string operand.
func (e *TextEditor) replace(lo, hi int, s string) {
	e.replaceBytes(lo, hi, []byte(s))
}

// replaceBytes splices s into the byte range lo..hi and puts the caret after
// it.
//
// The splice is one copy of the tail and one copy of the operand, in a slice
// that keeps its capacity across edits. Nothing here allocates unless the
// document outgrows its buffer, which is the growth the performance contract
// allows; see [TextEditor.buf].
func (e *TextEditor) replaceBytes(lo, hi int, s []byte) {
	n := len(s)
	tail := len(e.buf) - hi
	want := lo + n + tail
	if cap(e.buf) < want {
		grown := make([]byte, want, roundUpCapacity(want))
		copy(grown, e.buf[:lo])
		copy(grown[lo:], s)
		copy(grown[lo+n:], e.buf[hi:])
		e.buf = grown
	} else {
		e.buf = e.buf[:want]
		copy(e.buf[lo+n:], e.buf[hi:hi+tail])
		copy(e.buf[lo:], s)
	}
	e.caret, e.anchor = lo+n, lo+n
	e.strOK = false
}

// roundUpCapacity is the growth policy of the document buffer: doubling from a
// small base. It is spelled out rather than left to append, because the splice
// above writes into the middle of the slice and cannot use append's own
// growth.
func roundUpCapacity(n int) int {
	c := 32
	for c < n {
		c *= 2
	}
	return c
}

// --- motion -----------------------------------------------------------------

// MoveCaret puts the caret at off. With extend set the anchor stays where it
// is and the selection grows or shrinks; without it the selection collapses.
func (e *TextEditor) MoveCaret(off int, extend bool) {
	e.caret = e.snap(off)
	if !extend {
		e.anchor = e.caret
	}
}

// MoveLeft and MoveRight move the caret by one rune.
//
// With a selection and without extend they collapse to the near edge of it
// rather than moving one character from the caret, which is what every text
// field does: pressing left with a word selected puts the caret before the
// word, not one character inside it.
func (e *TextEditor) MoveLeft(extend bool) {
	if !extend && e.HasSelection() {
		lo, _ := e.selection()
		e.SetCaret(lo)
		return
	}
	e.MoveCaret(e.prevRune(e.caret), extend)
}

// MoveRight is [TextEditor.MoveLeft] in the other direction.
func (e *TextEditor) MoveRight(extend bool) {
	if !extend && e.HasSelection() {
		_, hi := e.selection()
		e.SetCaret(hi)
		return
	}
	e.MoveCaret(e.nextRune(e.caret), extend)
}

// MoveWordLeft and MoveWordRight move the caret to the near edge of the
// adjacent word; see [runeClass] for what a word is here.
func (e *TextEditor) MoveWordLeft(extend bool) { e.MoveCaret(e.prevWord(e.caret), extend) }

// MoveWordRight is [TextEditor.MoveWordLeft] in the other direction.
func (e *TextEditor) MoveWordRight(extend bool) { e.MoveCaret(e.nextWord(e.caret), extend) }

// MoveHome and MoveEnd move the caret to the ends of the document.
//
// A single line field has no line that is not the whole document, so home and
// end, control-home and control-end, and command-left and command-right all
// mean this. A multi line editor would have to tell them apart; this one would
// be pretending.
func (e *TextEditor) MoveHome(extend bool) { e.MoveCaret(0, extend) }

// MoveEnd is [TextEditor.MoveHome] at the other end.
func (e *TextEditor) MoveEnd(extend bool) { e.MoveCaret(len(e.buf), extend) }

// SelectWordAt selects the run of text around off, where a run is what
// [runeClass] says it is: a word, a stretch of whitespace, or a stretch of
// punctuation.
func (e *TextEditor) SelectWordAt(off int) {
	lo, hi := e.wordAt(e.snap(off))
	e.anchor, e.caret = lo, hi
}

// dragState is what a pointer that is down on the field is doing, and it has
// three values because the middle one is the whole of the answer to "a drag on
// a field inside a scrolling form".
//
// A press alone is not a selection yet. Between the press and the moment the
// pointer has travelled [gift.DragSlop], nobody may claim the gesture: the
// field consumes nothing, so the form above it sees the same moves and can
// still recognise a scroll. At the slop the direction decides, once and for
// good; see [textFieldNode.HandleEvent].
type dragState uint8

const (
	// dragNone: no gesture, or one this field has given up.
	dragNone dragState = iota
	// dragPending: the pointer is down but has not yet moved far enough for
	// anybody to say what the gesture is.
	dragPending
	// dragChar and dragWord: this field owns the gesture and is extending the
	// selection, by character after a single click and by whole runs after a
	// double one.
	dragChar
	dragWord
)

// dragExtendWord is a word-wise drag: the run the double click selected stays
// selected whichever way the pointer goes, and the selection grows to whole
// runs at the moving end.
//
// It is what every platform does with a double click and drag, and the reason
// the fixed end is remembered rather than recomputed is that the anchor of the
// selection moves between the two ends of that run as the pointer crosses it.
func (e *TextEditor) dragExtendWord(off int) {
	lo, hi := e.wordAt(e.snap(off))
	if lo < e.dragWordLo {
		e.anchor, e.caret = e.dragWordHi, lo
		return
	}
	if hi > e.dragWordHi {
		e.anchor, e.caret = e.dragWordLo, hi
		return
	}
	e.anchor, e.caret = e.dragWordLo, e.dragWordHi
}

func (e *TextEditor) prevRune(off int) int {
	if off <= 0 {
		return 0
	}
	_, n := utf8.DecodeLastRune(e.buf[:e.snap(off)])
	return off - n
}

func (e *TextEditor) nextRune(off int) int {
	if off >= len(e.buf) {
		return len(e.buf)
	}
	_, n := utf8.DecodeRune(e.buf[e.snap(off):])
	return off + n
}

// wordAt returns the boundaries of the run around off.
func (e *TextEditor) wordAt(off int) (int, int) {
	if len(e.buf) == 0 {
		return 0, 0
	}
	// A caret at the very end belongs to the run before it; there is nothing
	// after it to classify.
	at := off
	if at >= len(e.buf) {
		at = e.prevRune(len(e.buf))
	}
	r, _ := utf8.DecodeRune(e.buf[at:])
	class := runeClass(r)
	lo := at
	for lo > 0 {
		p := e.prevRune(lo)
		q, _ := utf8.DecodeRune(e.buf[p:])
		if runeClass(q) != class {
			break
		}
		lo = p
	}
	hi := at
	for hi < len(e.buf) {
		q, _ := utf8.DecodeRune(e.buf[hi:])
		if runeClass(q) != class {
			break
		}
		hi = e.nextRune(hi)
	}
	return lo, hi
}

// prevWord is the offset a word-wise left move lands on: skip the whitespace
// immediately before the caret, then the run before that.
func (e *TextEditor) prevWord(off int) int {
	off = e.snap(off)
	for off > 0 {
		p := e.prevRune(off)
		r, _ := utf8.DecodeRune(e.buf[p:])
		if runeClass(r) != classSpace {
			break
		}
		off = p
	}
	if off == 0 {
		return 0
	}
	lo, _ := e.wordAt(e.prevRune(off))
	return lo
}

// nextWord is the offset a word-wise right move lands on: the end of the run
// the caret is in or before, plus any whitespace after it.
func (e *TextEditor) nextWord(off int) int {
	off = e.snap(off)
	for off < len(e.buf) {
		r, _ := utf8.DecodeRune(e.buf[off:])
		if runeClass(r) == classSpace {
			off = e.nextRune(off)
			continue
		}
		_, hi := e.wordAt(off)
		return hi
	}
	return len(e.buf)
}

// runeClass is how this package divides text into words, and it is one rule
// rather than three: a run of letters, digits and underscores is a word, a run
// of whitespace is a word, and a run of anything else is a word.
//
// It is Unicode aware through [unicode.IsLetter] and friends, so an umlaut is
// a letter and "gruen" spelled with one is a single word. It is not UAX 29
// word segmentation: that needs the boundary iterator of typesetting, which
// internal/text does not expose, and the difference shows up in text this
// widget is not for — there is no Thai word breaking here, and a single line
// field is already not the tool for a paragraph of Thai.
//
// The three way classification is what makes a double click on the space
// between two words select the space instead of one of the words, and a double
// click on "..." select all three dots.
type runeKind uint8

const (
	classWord runeKind = iota
	classSpace
	classOther
)

func runeClass(r rune) runeKind {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return classWord
	default:
		return classOther
	}
}
