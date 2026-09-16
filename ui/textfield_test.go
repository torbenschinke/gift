package ui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The text field tests. Every one of them drives the widget through
// gifttest — real events, the real dispatcher, the real focus — because the
// defects worth catching here live in the wiring between gift and the widget
// and not inside either.
//
// The one exception is the editor model, which is exercised directly in
// textedit_test.go: word boundaries and rune arithmetic have nothing to do
// with events and a harness would only make them slower to read.

// fieldFixture is one field alone in the window, with the shared test font.
type fieldFixture struct {
	h      *gifttest.Harness
	ed     *ui.TextEditor
	change []string
}

func newField(t *testing.T, initial string, decorate func(ui.TextFieldView) ui.TextFieldView) *fieldFixture {
	t.Helper()
	f := &fieldFixture{ed: ui.NewTextEditor(initial)}
	v := ui.TextField(f.ed).
		Font(loadTestFont(t)).
		FontSize(16).
		Frame(200, geom.Unbounded()).
		OnChange(func(s string) { f.change = append(f.change, s) }).
		Key("field")
	if decorate != nil {
		v = decorate(v)
	}
	f.h = gifttest.New(t, gifttest.Options{
		// A Box under the field, so that a click on the background has
		// somewhere to land that is not the field.
		View: ui.ZStack(ui.Box(), ui.VStack(v).Padding(20)),
		Size: geom.Sz(400, 200),
		Font: loadTestFont(t),
	})
	return f
}

func (f *fieldFixture) node() gifttest.Node { return f.h.Find(gifttest.ByKey("field")) }

// clickAtOffset clicks where the caret for the byte offset off would be, plus
// two pixels, so the midpoint rule of offsetAt lands on off itself.
//
// The x it computes comes from measuring the prefix with the same font the
// field uses, so the test never hard codes a pixel.
func (f *fieldFixture) clickAtOffset(t *testing.T, off int) {
	t.Helper()
	b := f.node().Bounds()
	x := b.Min.X + fieldPadLeft + prefixWidth(t, f.ed.Text()[:off])
	f.h.ClickAt(geom.Pt(x+1, b.Min.Y+b.Height()/2))
}

// fieldPadLeft is the default left padding of a field. It is spelled out here
// because the test has to convert between text coordinates and window
// coordinates, and there is no public accessor for it; a change to the default
// will fail the click tests, which is the correct place to notice it.
const fieldPadLeft = 8

func prefixWidth(t *testing.T, s string) float32 {
	t.Helper()
	if s == "" {
		return 0
	}
	return ui.MeasureForTest(loadTestFont(t), s, 16, geom.Unbounded()).W
}

// caretOp returns the caret rectangle of the current frame, if one was drawn.
// The caret is the only one pixel wide filled rectangle a field emits.
func caretOp(h *gifttest.Harness) (render.Op, bool) {
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRect && op.Bounds.Width() == 1 {
			return op, true
		}
	}
	return render.Op{}, false
}

// selectionOp returns the selection highlight of the current frame, if one was
// drawn: a filled rectangle wider than the caret.
func selectionOp(h *gifttest.Harness) (render.Op, bool) {
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRect && op.Bounds.Width() > 1 {
			return op, true
		}
	}
	return render.Op{}, false
}

// --- typing -------------------------------------------------------------------

// TestTypingInsertsTheCharactersTheKeyboardProduced types a string containing
// an umlaut and checks the document, the caret and the change callback.
//
// The umlaut is the point of the test and not decoration. It is two bytes, so
// a caret counted in runes, a buffer spliced by rune index or a backspace that
// removed one byte would all pass a test typing "abc" and fail here.
func TestTypingInsertsTheCharactersTheKeyboardProduced(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.TypeText("grün")

	if got := f.ed.Text(); got != "grün" {
		t.Fatalf("the document is %q, want %q", got, "grün")
	}
	if got, want := f.ed.Caret(), len("grün"); got != want {
		t.Fatalf("the caret is at byte %d, want %d — one past the last byte of the umlaut", got, want)
	}
	if got := f.change; len(got) != 4 || got[3] != "grün" {
		t.Fatalf("OnChange saw %q, want one call per character ending in %q", got, "grün")
	}
}

// TestBackspaceRemovesAWholeUmlautAndNotOneByteOfIt is the deletion half of
// the test above, and it is separate because the failure it catches is
// different: a backspace that moves one byte leaves an invalid UTF-8 sequence
// in the buffer, which the shaper then renders as a replacement character.
func TestBackspaceRemovesAWholeUmlautAndNotOneByteOfIt(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.TypeText("aö")
	f.h.Key(gift.KeyBackspace)

	if got := f.ed.Text(); got != "a" {
		t.Fatalf("after backspace the document is %q (% x), want %q", got, got, "a")
	}
}

// TestTypingReplacesTheSelection is the rule every other editing operation
// depends on: an insertion with a selection is a replacement.
func TestTypingReplacesTheSelection(t *testing.T) {
	f := newField(t, "hello world", nil)
	f.node().Focus()
	f.ed.SetSelection(0, 5)
	f.h.TypeText("bye")

	if got := f.ed.Text(); got != "bye world" {
		t.Fatalf("the document is %q, want %q", got, "bye world")
	}
	if f.ed.HasSelection() {
		t.Fatal("the selection survived the replacement")
	}
}

// --- pointer ------------------------------------------------------------------

// TestClickPlacesTheCaretAtBothEndsAndInTheMiddle drives the whole
// x-coordinate to byte offset mapping through real clicks.
//
// Clicking far to the left and far to the right of the text is the part that
// catches an off by one at the boundaries; the middle click is the part that
// catches a mapping that is merely monotonic.
func TestClickPlacesTheCaretAtBothEndsAndInTheMiddle(t *testing.T) {
	// No space in the string, because [prefixWidth] measures a shaped line and
	// a shaped line does not count its trailing whitespace — measuring
	// "hello " would give the width of "hello". The caret *does* sit after
	// that space, which is a different property with a test of its own; see
	// TestTheCaretSitsAfterATrailingSpace.
	const s = "helloworld"
	f := newField(t, s, nil)
	b := f.node().Bounds()
	y := b.Min.Y + b.Height()/2

	f.h.ClickAt(geom.Pt(b.Min.X+1, y))
	if got := f.ed.Caret(); got != 0 {
		t.Fatalf("a click on the left edge put the caret at %d, want 0", got)
	}

	// Far to the right of the text but still inside the field.
	f.h.Advance(DoubleClickGap)
	f.h.ClickAt(geom.Pt(b.Max.X-2, y))
	if got := f.ed.Caret(); got != len(s) {
		t.Fatalf("a click past the end of the text put the caret at %d, want %d", got, len(s))
	}

	f.h.Advance(DoubleClickGap)
	f.clickAtOffset(t, 5) // before the 'w' of "world"
	if got := f.ed.Caret(); got != 5 {
		t.Fatalf("a click before the 'w' put the caret at %d, want 5 (%q|%q)",
			got, s[:5], s[5:])
	}
}

// TestTheCaretSitsAfterATrailingSpace is the widget half of the one addition
// this work unit made to internal/text.
//
// A shaped line reports a Width that excludes its trailing whitespace, on
// purpose: a label ending in a space must not be wider than the text it shows.
// A caret at the end of "hi " is not a measurement, though — it has to be
// after the space, or typing a space appears to do nothing at all and the
// next character jumps a full space width to the right. [text.Line.Advance] is
// the number that says where, and this test is what fails without it.
func TestTheCaretSitsAfterATrailingSpace(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.TypeText("hi")
	before, ok := caretOp(f.h)
	if !ok {
		t.Fatal("no caret after typing")
	}
	f.h.TypeText(" ")
	after, ok := caretOp(f.h)
	if !ok {
		t.Fatal("no caret after typing a space")
	}
	if !(after.Bounds.Min.X > before.Bounds.Min.X) {
		t.Fatalf("the caret is at x=%v after the space and was at x=%v before it; "+
			"typing a space moved nothing", after.Bounds.Min.X, before.Bounds.Min.X)
	}
}

// DoubleClickGap is longer than [ui.DoubleClickInterval], so two clicks in a
// test are two clicks and not a double click. It is named rather than written
// inline because a test that forgets it fails with "the caret is at 0, want 6"
// and nothing pointing at the cause.
const DoubleClickGap = ui.DoubleClickInterval + 50*time.Millisecond

// TestDraggingSelectsTheTextBetweenTheTwoPositions presses inside the text and
// releases further along, then checks what is selected — and that the
// selection highlight is actually drawn, because a selection nobody can see is
// the same defect as no selection.
func TestDraggingSelectsTheTextBetweenTheTwoPositions(t *testing.T) {
	const s = "hello world"
	f := newField(t, s, nil)
	b := f.node().Bounds()
	y := b.Min.Y + b.Height()/2
	x := func(off int) float32 { return b.Min.X + fieldPadLeft + prefixWidth(t, s[:off]) + 1 }

	f.h.PressAt(geom.Pt(x(0), y))
	f.h.MoveTo(geom.Pt(x(5), y))
	f.h.ReleaseAt(geom.Pt(x(5), y))

	if got := f.ed.SelectedText(); got != "hello" {
		t.Fatalf("the drag selected %q, want %q", got, "hello")
	}
	if _, ok := selectionOp(f.h); !ok {
		t.Fatalf("nothing was drawn for the selection.\n%s", f.h.Dump())
	}
}

// TestDoubleClickSelectsAWordAndTripleClickSelectsEverything.
func TestDoubleClickSelectsAWordAndTripleClickSelectsEverything(t *testing.T) {
	const s = "hello wide world"
	f := newField(t, s, nil)
	f.node().Focus()

	f.clickAtOffset(t, 7) // inside "wide"
	f.clickAtOffset(t, 7)
	if got := f.ed.SelectedText(); got != "wide" {
		t.Fatalf("the double click selected %q, want %q", got, "wide")
	}

	f.clickAtOffset(t, 7)
	if got := f.ed.SelectedText(); got != s {
		t.Fatalf("the triple click selected %q, want the whole text", got)
	}
}

// TestTwoSlowClicksAreNotADoubleClick is the other direction, and it is the
// test that would have caught a double click recogniser with no clock in it:
// without the interval check every second click anywhere selects a word.
func TestTwoSlowClicksAreNotADoubleClick(t *testing.T) {
	f := newField(t, "hello wide world", nil)
	f.clickAtOffset(t, 7)
	f.h.Advance(DoubleClickGap)
	f.clickAtOffset(t, 7)
	if f.ed.HasSelection() {
		t.Fatalf("two clicks %v apart selected %q; they are two separate clicks",
			DoubleClickGap, f.ed.SelectedText())
	}
}

// --- keyboard -----------------------------------------------------------------

// TestEveryCaretMovementKey walks the caret with each key in turn and checks
// the offset after every step, so that a key that does nothing is a failure
// and not an unnoticed gap.
func TestEveryCaretMovementKey(t *testing.T) {
	const s = "hello wide world"
	f := newField(t, s, nil)
	f.node().Focus()
	f.ed.SetCaret(0)

	steps := []struct {
		what string
		key  gift.Key
		mods []gift.Mods
		want int
	}{
		{"right", gift.KeyRight, nil, 1},
		{"right again", gift.KeyRight, nil, 2},
		{"left", gift.KeyLeft, nil, 1},
		{"end", gift.KeyEnd, nil, len(s)},
		{"home", gift.KeyHome, nil, 0},
		{"word right", gift.KeyRight, []gift.Mods{gift.WordModifier}, 5},
		{"word right again", gift.KeyRight, []gift.Mods{gift.WordModifier}, 10},
		{"word left", gift.KeyLeft, []gift.Mods{gift.WordModifier}, 6},
		{"down is the end of the one line", gift.KeyDown, nil, len(s)},
		{"up is the start of it", gift.KeyUp, nil, 0},
	}
	// The shortcut chords are deliberately not in this table. On a keyboard
	// where [gift.ShortcutModifier] and [gift.WordModifier] are the same key —
	// every platform except macOS — a row for each of them is two rows
	// asserting two different answers to one key press, and the table could
	// only be green on the machine the modifiers happened to differ on. They
	// have their own test, which sets both conventions explicitly; see
	// TestBothKeyboardConventionsForWordAndLineMotion.
	for _, st := range steps {
		f.h.Key(st.key, st.mods...)
		if got := f.ed.Caret(); got != st.want {
			t.Fatalf("%s put the caret at %d, want %d", st.what, got, st.want)
		}
		if f.ed.HasSelection() {
			t.Fatalf("%s created a selection without shift being held", st.what)
		}
	}
}

// TestShiftExtendsTheSelectionAndAnUnshiftedArrowCollapsesIt.
func TestShiftExtendsTheSelectionAndAnUnshiftedArrowCollapsesIt(t *testing.T) {
	f := newField(t, "hello world", nil)
	f.node().Focus()
	f.ed.SetCaret(0)

	f.h.Key(gift.KeyRight, gift.ModShift)
	f.h.Key(gift.KeyRight, gift.ModShift)
	if got := f.ed.SelectedText(); got != "he" {
		t.Fatalf("two shifted rights selected %q, want %q", got, "he")
	}
	f.h.Key(gift.KeyRight, gift.ModShift, gift.WordModifier)
	if got := f.ed.SelectedText(); got != "hello" {
		t.Fatalf("a shifted word-right selected %q, want %q", got, "hello")
	}
	f.h.Key(gift.KeyLeft)
	if f.ed.HasSelection() {
		t.Fatalf("an unshifted left kept the selection %q", f.ed.SelectedText())
	}
	if got := f.ed.Caret(); got != 0 {
		t.Fatalf("the collapse put the caret at %d, want 0 — the near edge of the selection", got)
	}
}

// TestDeleteAndBackspaceRemoveInBothDirections, including the selection case
// where both of them delete the selection and nothing else.
func TestDeleteAndBackspaceRemoveInBothDirections(t *testing.T) {
	f := newField(t, "abcd", nil)
	f.node().Focus()
	f.ed.SetCaret(2)

	f.h.Key(gift.KeyBackspace)
	if got := f.ed.Text(); got != "acd" {
		t.Fatalf("backspace produced %q, want %q", got, "acd")
	}
	f.h.Key(gift.KeyDelete)
	if got := f.ed.Text(); got != "ad" {
		t.Fatalf("delete produced %q, want %q", got, "ad")
	}
	f.ed.SelectAll()
	f.h.Key(gift.KeyDelete)
	if got := f.ed.Text(); got != "" {
		t.Fatalf("delete with everything selected produced %q, want the empty document", got)
	}
	// The one that must not crash and must not report a change.
	before := len(f.change)
	f.h.Key(gift.KeyBackspace)
	if len(f.change) != before {
		t.Fatalf("backspace in an empty field reported a change: %q", f.change[before:])
	}
}

// TestHoldingBackspaceDeletesRepeatedly is the key repeat test, and it uses
// gift's own repeat rather than sending several presses: the field must react
// to a synthetic press with [gift.Event.Repeat] set exactly as to a real one.
//
// The clock is stepped at the frame interval and not in one jump, because gift
// bounds the number of synthetic presses per tick on purpose; a single large
// Advance would deliver the bound and the test would be measuring
// maxRepeatsPerTick instead of the repeat.
func TestHoldingBackspaceDeletesRepeatedly(t *testing.T) {
	f := newField(t, "abcdefgh", nil)
	f.node().Focus()

	f.h.KeyDown(gift.KeyBackspace)
	if got := f.ed.Text(); got != "abcdefg" {
		t.Fatalf("the press itself deleted nothing: %q", got)
	}
	// Past the repeat delay and then a second of frames, which is far more
	// than enough repeats to empty the rest.
	f.h.AdvanceTicks(40, 16*time.Millisecond)
	f.h.KeyUp(gift.KeyBackspace)
	if got := f.ed.Text(); got != "" {
		t.Fatalf("holding backspace for 640 ms left %q; the repeat is not reaching the field", got)
	}

	// And it stops: nothing is deleted after the release.
	f.h.TypeText("xy")
	f.h.AdvanceTicks(40, 16*time.Millisecond)
	if got := f.ed.Text(); got != "xy" {
		t.Fatalf("after the release the document is %q, want %q; the repeat did not stop", got, "xy")
	}
}

// TestEnterSubmitsAndDoesNotInsertAnything.
func TestEnterSubmitsAndDoesNotInsertAnything(t *testing.T) {
	var submitted []string
	f := newField(t, "query", func(v ui.TextFieldView) ui.TextFieldView {
		return v.OnSubmit(func(s string) { submitted = append(submitted, s) })
	})
	f.node().Focus()
	f.h.Key(gift.KeyEnter)

	if len(submitted) != 1 || submitted[0] != "query" {
		t.Fatalf("OnSubmit saw %q, want one call with %q", submitted, "query")
	}
	if got := f.ed.Text(); got != "query" {
		t.Fatalf("enter changed the document to %q", got)
	}
}

// --- the clipboard -------------------------------------------------------------

// TestCopyCutAndPasteUseTheInstalledClipboard exercises all three against the
// in-process clipboard, including the two cases the seam gets wrong most
// easily: a paste replaces the selection, and a copy with nothing selected
// leaves the clipboard alone rather than emptying it.
func TestCopyCutAndPasteUseTheInstalledClipboard(t *testing.T) {
	cb := &ui.MemoryClipboard{}
	ui.SetClipboard(cb)
	t.Cleanup(func() { ui.SetClipboard(nil) })

	f := newField(t, "hello world", nil)
	f.node().Focus()

	f.ed.SetSelection(0, 5)
	f.h.Key(gift.KeyC, gift.ShortcutModifier)
	if got, ok := cb.Text(); !ok || got != "hello" {
		t.Fatalf("copy put %q, %v on the clipboard, want %q, true", got, ok, "hello")
	}
	if got := f.ed.Text(); got != "hello world" {
		t.Fatalf("copy changed the document to %q", got)
	}

	f.ed.SetSelection(6, 11)
	f.h.Key(gift.KeyX, gift.ShortcutModifier)
	if got, _ := cb.Text(); got != "world" {
		t.Fatalf("cut put %q on the clipboard, want %q", got, "world")
	}
	if got := f.ed.Text(); got != "hello " {
		t.Fatalf("cut left %q, want %q", got, "hello ")
	}

	// Paste at the caret, which cut left at the end.
	f.h.Key(gift.KeyV, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "hello world" {
		t.Fatalf("paste produced %q, want %q", got, "hello world")
	}

	// Paste over a selection replaces it.
	f.ed.SetSelection(0, 5)
	f.h.Key(gift.KeyV, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "world world" {
		t.Fatalf("paste over a selection produced %q, want %q", got, "world world")
	}

	// A copy with an empty selection must not wipe what is on the clipboard.
	f.ed.SetCaret(0)
	f.h.Key(gift.KeyC, gift.ShortcutModifier)
	if got, ok := cb.Text(); !ok || got != "world" {
		t.Fatalf("a copy with nothing selected changed the clipboard to %q, %v", got, ok)
	}
}

// checkedClipboard is a [ui.CheckedClipboard] whose write can be made to fail,
// which is the only way to reach the branch below: [ui.MemoryClipboard]
// deliberately implements the two method interface and never fails.
type checkedClipboard struct {
	ui.MemoryClipboard
	err   error
	wrote int
}

func (c *checkedClipboard) SetTextErr(s string) error {
	c.wrote++
	if c.err != nil {
		return c.err
	}
	c.MemoryClipboard.SetText(s)
	return nil
}

func (c *checkedClipboard) SetText(s string) { _ = c.SetTextErr(s) }

// TestACutWhoseCopyFailedDoesNotDeleteTheText is data loss, not tidiness. The
// field has no undo, so text that a cut removed after a failed copy is in no
// clipboard, in no document and in no history — and a dead X11 selection owner
// or a display connection that went away with the session is an ordinary
// Tuesday on a kiosk, not a corner case.
//
// The two directions are one test on purpose: "a failing clipboard does not
// delete" passes trivially if the cut is broken for everybody, so the working
// clipboard is checked with the same field and the same keystroke.
func TestACutWhoseCopyFailedDoesNotDeleteTheText(t *testing.T) {
	cb := &checkedClipboard{}
	ui.SetClipboard(cb)
	t.Cleanup(func() { ui.SetClipboard(nil) })

	f := newField(t, "hello world", nil)
	f.node().Focus()

	// Direction one: the clipboard works, so the cut cuts.
	f.ed.SetSelection(0, 6)
	f.h.Key(gift.KeyX, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "world" {
		t.Fatalf("a cut through a working clipboard left %q, want %q", got, "world")
	}
	if got, ok := cb.Text(); !ok || got != "hello " {
		t.Fatalf("the working cut put %q, %v on the clipboard", got, ok)
	}

	// Direction two: the clipboard refuses, so the document keeps its text.
	cb.err = errors.New("the selection owner never answered")
	f.ed.SetSelection(0, 5)
	f.h.Key(gift.KeyX, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "world" {
		t.Fatalf("a cut whose copy failed changed the document to %q; the text is now nowhere", got)
	}
	if !f.ed.HasSelection() {
		t.Error("the failed cut dropped the selection, so a second Ctrl+X would do nothing either")
	}
	if cb.wrote != 2 {
		t.Errorf("the clipboard was written %d times, want two; the failing cut has to have tried", cb.wrote)
	}
}

// TestACutThroughAPlainClipboardStillCuts is the fallback half: an
// implementation of the two method [ui.Clipboard] cannot report a failure, so
// a cut through one must behave exactly as it did before the checked interface
// existed. [ui.MemoryClipboard] is such an implementation and is the default,
// so this is also the path every other test in this file takes.
func TestACutThroughAPlainClipboardStillCuts(t *testing.T) {
	cb := &ui.MemoryClipboard{}
	if _, ok := any(cb).(ui.CheckedClipboard); ok {
		t.Fatal("MemoryClipboard implements CheckedClipboard, so the fallback path is no longer covered anywhere")
	}
	ui.SetClipboard(cb)
	t.Cleanup(func() { ui.SetClipboard(nil) })

	f := newField(t, "hello world", nil)
	f.node().Focus()
	f.ed.SetSelection(0, 6)
	f.h.Key(gift.KeyX, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "world" {
		t.Fatalf("a cut through a plain clipboard left %q, want %q", got, "world")
	}
}

// TestSelectAllSelectsTheWholeDocument.
func TestSelectAllSelectsTheWholeDocument(t *testing.T) {
	f := newField(t, "hello world", nil)
	f.node().Focus()
	f.h.Key(gift.KeyA, gift.ShortcutModifier)
	if got := f.ed.SelectedText(); got != "hello world" {
		t.Fatalf("select all selected %q", got)
	}
}

// TestAnUnmodifiedShortcutLetterIsJustALetter guards the boundary between the
// two input channels. The letters of the shortcuts are physical keys and they
// must do nothing on their own; the characters they produce arrive on the rune
// channel and must be inserted.
func TestAnUnmodifiedShortcutLetterIsJustALetter(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.Key(gift.KeyA)
	if f.ed.Text() != "" || f.ed.HasSelection() {
		t.Fatalf("an unmodified A key changed the field: %q, selection %q",
			f.ed.Text(), f.ed.SelectedText())
	}
	f.h.TypeText("a")
	if got := f.ed.Text(); got != "a" {
		t.Fatalf("the character 'a' produced %q", got)
	}
}

// TestAMultiLinePasteKeepsOnlyTheFirstLine. A line break in a single line
// field would produce a second line in the shaping result, and the field draws
// exactly one.
func TestAMultiLinePasteKeepsOnlyTheFirstLine(t *testing.T) {
	cb := &ui.MemoryClipboard{}
	cb.SetText("first\nsecond\tthird")
	ui.SetClipboard(cb)
	t.Cleanup(func() { ui.SetClipboard(nil) })

	f := newField(t, "", nil)
	f.node().Focus()
	f.h.Key(gift.KeyV, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "first" {
		t.Fatalf("the paste produced %q, want %q", got, "first")
	}
}

// --- focus, disabled and the scroller ------------------------------------------

// TestTabReachesTheFieldAndTheFocusRingIsDrawn. The ring is checked in the
// display list rather than trusted, because a focus ring that is declared and
// not painted is precisely the kind of thing a focus test usually misses.
func TestTabReachesTheFieldAndTheFocusRingIsDrawn(t *testing.T) {
	f := newField(t, "x", nil)
	strokesBefore := countStrokes(f.h)

	f.h.Tab()
	got, ok := f.h.Focused()
	if !ok || got.Key() != "field" {
		t.Fatalf("tab did not reach the field.\n%s", f.h.Dump())
	}
	if after := countStrokes(f.h); after != strokesBefore+1 {
		t.Fatalf("the focused field emits %d stroked rectangles and the unfocused one %d; "+
			"the focus ring is not being drawn", after, strokesBefore)
	}
}

func countStrokes(h *gifttest.Harness) int {
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpStrokeRoundRect {
			n++
		}
	}
	return n
}

// TestBlurKeepsTheValueAndTheCaretDisappears pins the decision this widget
// made about commit semantics: there is none. Every edit is published the
// moment it happens, so losing the focus changes nothing about the value and
// only stops the caret.
func TestBlurKeepsTheValueAndTheCaretDisappears(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.TypeText("kept")
	if _, ok := caretOp(f.h); !ok {
		t.Fatal("no caret while the field has the focus")
	}
	changes := len(f.change)

	// A click on the background, which is how a field loses the focus in a
	// real window.
	f.h.ClickAt(geom.Pt(5, 190))
	if _, ok := f.h.Focused(); ok {
		t.Fatal("the click on the background did not clear the focus")
	}
	if got := f.ed.Text(); got != "kept" {
		t.Fatalf("the value after blur is %q, want %q", got, "kept")
	}
	if len(f.change) != changes {
		t.Fatalf("blur reported %d further changes; the value is committed per keystroke, "+
			"not on blur", len(f.change)-changes)
	}
	if _, ok := caretOp(f.h); ok {
		t.Fatal("the caret is still drawn after the field lost the focus")
	}
}

// TestADisabledFieldTakesNoFocusNoTypingAndDrawsNoCaret.
func TestADisabledFieldTakesNoFocusNoTypingAndDrawsNoCaret(t *testing.T) {
	f := newField(t, "locked", func(v ui.TextFieldView) ui.TextFieldView { return v.Disabled(true) })

	f.h.Tab()
	if n, ok := f.h.Focused(); ok && n.Key() == "field" {
		t.Fatal("tab focused a disabled field")
	}
	f.node().Click()
	if n, ok := f.h.Focused(); ok && n.Key() == "field" {
		t.Fatal("a click focused a disabled field")
	}
	if _, ok := caretOp(f.h); ok {
		t.Fatal("a disabled field drew a caret")
	}
	// Typing goes to whatever has the focus, which is nothing; the document
	// must be untouched either way.
	f.h.TypeText("nope")
	if got := f.ed.Text(); got != "locked" {
		t.Fatalf("typing into a disabled field produced %q", got)
	}
}

// TestADragInsideAScrollerSelectsTextInsteadOfScrollingIt is the trap named in
// the work order: a scroll container steals the press of a drag that crosses
// its slop, which is right for a button and fatal for a caret drag. The field
// consumes the moves so the container never sees the gesture.
//
// The test asserts both halves: the text is selected *and* the container did
// not move. Asserting only the first would pass for a field that selects while
// the form scrolls out from under it.
func TestADragInsideAScrollerSelectsTextInsteadOfScrollingIt(t *testing.T) {
	const s = "hello world"
	ed := ui.NewTextEditor(s)
	rows := make([]gift.View, 0, 12)
	rows = append(rows, ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
		Frame(200, geom.Unbounded()).Key("field"))
	for i := range 11 {
		rows = append(rows, ui.Box().Frame(200, 40).Background(ui.RGB(30, 30, 30)).Key(string(rune('a'+i))))
	}
	h := gifttest.New(t, gifttest.Options{
		View: ui.VScroll(rows...).Gap(4).Frame(300, 200).Key("scroller"),
		Size: geom.Sz(400, 300),
		Font: loadTestFont(t),
	})
	scroller := h.Find(gifttest.ByKey("scroller"))
	info, ok := h.App().ScrollInfo(scroller.Ref())
	if !ok || !(info.MaxOffset > 0) {
		t.Fatalf("the fixture does not scroll: %+v", info)
	}

	b := h.Find(gifttest.ByKey("field")).Bounds()
	y := b.Min.Y + b.Height()/2
	x := func(off int) float32 { return b.Min.X + fieldPadLeft + prefixWidth(t, s[:off]) + 1 }

	h.PressAt(geom.Pt(x(0), y))
	// Well past gift.DragSlop, and *upwards* as well as sideways. Upwards is
	// the direction that matters: the viewport is at offset zero, so a
	// downward drag is a direction the container cannot move in and it would
	// decline the gesture for a reason that has nothing to do with this
	// widget. Dragging up is the movement it would gladly take.
	for i := 1; i <= 8; i++ {
		h.MoveTo(geom.Pt(x(0)+(x(5)-x(0))*float32(i)/8, y-float32(i)*3))
	}
	h.ReleaseAt(geom.Pt(x(5), y-24))

	if got := ed.SelectedText(); got != "hello" {
		t.Fatalf("the drag selected %q, want %q", got, "hello")
	}
	after, _ := h.App().ScrollInfo(scroller.Ref())
	if after.Offset != 0 {
		t.Fatalf("the scroll container moved to %v during a caret drag; it stole the gesture",
			after.Offset)
	}
}

// --- the caret and the idle policy ----------------------------------------------

// TestTheCaretBlinksWhileFocused. Half a cycle after the last keystroke the
// caret is gone, and a cycle after it is back.
func TestTheCaretBlinksWhileFocused(t *testing.T) {
	f := newField(t, "x", nil)
	f.node().Focus()
	if _, ok := caretOp(f.h); !ok {
		t.Fatal("the caret is invisible in the first frame after taking the focus")
	}
	f.h.Advance(ui.CaretBlinkInterval)
	if _, ok := caretOp(f.h); ok {
		t.Fatalf("the caret is still drawn %v after the focus arrived; it does not blink",
			ui.CaretBlinkInterval)
	}
	f.h.Advance(ui.CaretBlinkInterval)
	if _, ok := caretOp(f.h); !ok {
		t.Fatal("the caret did not come back after a full blink cycle")
	}
}

// TestTypingRestartsTheBlinkSoTheCaretIsSolidWhileWriting. Without this a user
// typing steadily sees the caret flash out from under the character they are
// writing, which is the one moment it must not.
func TestTypingRestartsTheBlinkSoTheCaretIsSolidWhileWriting(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.Advance(ui.CaretBlinkInterval) // the caret is now in its dark half
	if _, ok := caretOp(f.h); ok {
		t.Fatal("the fixture is wrong: the caret should be dark here")
	}
	f.h.TypeText("a")
	if _, ok := caretOp(f.h); !ok {
		t.Fatal("typing did not restart the blink; the caret stayed dark under the new character")
	}
}

// TestTheApplicationSettlesAfterTheBlinkWindowAndAfterBlur is the idle policy
// half of the blink, and it is the test that keeps a caret from holding a
// kiosk at full tick rate all night.
//
// Two ways out, both checked: the blink window expires, or the field loses the
// focus. In both cases [gift.App.NeedsPaint] has to go false and stay false,
// and the caret has to be *visible* in the expired case rather than frozen in
// whichever half it happened to be in.
func TestTheApplicationSettlesAfterTheBlinkWindowAndAfterBlur(t *testing.T) {
	t.Run("the window expires", func(t *testing.T) {
		f := newField(t, "x", nil)
		f.node().Focus()
		f.h.Advance(ui.CaretBlinkWindow + time.Second)
		if asksForFrames(f.h) {
			t.Fatal("the application still asks for frames after the blink window closed")
		}
		if _, ok := caretOp(f.h); !ok {
			t.Fatal("the caret is gone after the blink window; it should go solid, not dark")
		}
	})

	t.Run("the focus is lost", func(t *testing.T) {
		f := newField(t, "x", nil)
		f.node().Focus()
		f.h.ClickAt(geom.Pt(5, 190))
		if asksForFrames(f.h) {
			t.Fatal("the application still asks for frames after the field lost the focus")
		}
	})
}

// asksForFrames opens the next input phase and reports whether anything in the
// application asked to be painted again in it.
//
// The phase has to be opened and the answer taken *before* the frame is
// painted, because [gift.App.Paint] clears the flag: a check after a full
// frame reads false for an application that is repainting sixty times a
// second, which is the opposite of what this asks.
func asksForFrames(h *gifttest.Harness) bool {
	h.Frame()
	h.App().BeginInput(h.Now() + 16*time.Millisecond)
	return h.App().NeedsPaint()
}

// TestTheTextScrollsToKeepTheCaretVisible. The field is 200 pixels wide and
// the text is far wider, so the caret can only be on screen if the text moved.
func TestTheTextScrollsToKeepTheCaretVisible(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 8)
	f := newField(t, long, nil)
	b := f.node().Bounds()
	f.node().Focus()

	f.h.Key(gift.KeyEnd)
	c, ok := caretOp(f.h)
	if !ok {
		t.Fatal("no caret after End")
	}
	if c.Bounds.Min.X < b.Min.X || c.Bounds.Max.X > b.Max.X {
		t.Fatalf("the caret is at x=%v, outside the field %v..%v; the text did not scroll",
			c.Bounds.Min.X, b.Min.X, b.Max.X)
	}
	if !glyphsStartLeftOf(f.h, b.Min.X) {
		t.Fatal("the caret is in view but the text was not shifted left; the field is drawing " +
			"the beginning of a string that cannot fit")
	}

	f.h.Key(gift.KeyHome)
	c, ok = caretOp(f.h)
	if !ok {
		t.Fatal("no caret after Home")
	}
	if got, want := c.Bounds.Min.X, b.Min.X+fieldPadLeft; got != want {
		t.Fatalf("after Home the caret is at %v, want %v: the text must have scrolled back", got, want)
	}
}

// glyphsStartLeftOf reports whether any glyph of the frame sits left of x,
// which is what a text scrolled out to the left looks like.
func glyphsStartLeftOf(h *gifttest.Harness, x float32) bool {
	l := h.List()
	for _, op := range h.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		for _, g := range l.Glyphs(op.Glyphs, op.GlyphCount) {
			if g.X < x {
				return true
			}
		}
	}
	return false
}

// --- binding --------------------------------------------------------------------

// TestABoundFieldPublishesEditsAndAdoptsExternalChanges drives both directions
// of [ui.TextFieldView.Bind] through a real component with real state.
func TestABoundFieldPublishesEditsAndAdoptsExternalChanges(t *testing.T) {
	var value *gift.State[string]
	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			value = ctx.State("value", "")
			// Read it, so the component really does depend on the state and a
			// missing rebuild would show up as a stale label.
			return ui.VStack(
				ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
					Frame(200, geom.Unbounded()).Bind(value.Binding()).Key("field"),
				ui.Text("["+ctx.Read(value)+"]").Font(loadTestFont(t)).Key("echo"),
			).Padding(10)
		},
		Size: geom.Sz(400, 200),
		Font: loadTestFont(t),
	})

	h.Find(gifttest.ByKey("field")).Focus()
	h.TypeText("hi")
	if got := value.Get(); got != "hi" {
		t.Fatalf("the bound state is %q after typing, want %q", got, "hi")
	}
	if got := h.Find(gifttest.ByKey("echo")).Text(); got != "[hi]" {
		t.Fatalf("the label that reads the state shows %q; the edit did not cause a rebuild", got)
	}

	// The other direction: something else writes the state.
	value.Set("elsewhere")
	h.Settle()
	if got := ed.Text(); got != "elsewhere" {
		t.Fatalf("the field shows %q after the state was written from outside, want %q",
			got, "elsewhere")
	}
	if got := ed.Caret(); got != len("elsewhere") {
		t.Fatalf("the caret is at %d after adopting a new value, want the end at %d",
			got, len("elsewhere"))
	}
	// And the field still works afterwards.
	h.TypeText("!")
	if got := value.Get(); got != "elsewhere!" {
		t.Fatalf("typing after an external change produced %q", got)
	}

	// The long to short transition, which is the one this test used to miss
	// and the one the persistence example of the project plan, step 10, takes:
	// a value is typed until the field scrolls, and a record loaded from
	// somewhere else replaces it with a short one. A field that kept the old
	// offset showed the empty space past the end of the new text — measured at
	// the time: twenty seven glyphs inside the bounds before, none after, and
	// only a click repaired it.
	field := h.Find(gifttest.ByKey("field"))
	field.Focus()
	h.TypeText(strings.Repeat("abcdefgh", 11))
	b := field.Bounds()
	if !glyphsStartLeftOf(h, b.Min.X) {
		t.Fatal("the fixture is wrong: the field did not scroll")
	}
	value.Set("hi")
	h.Settle()
	if got := ed.Text(); got != "hi" {
		t.Fatalf("the field shows %q after a short value was written from outside", got)
	}
	if got := glyphsInside(h, b); got != len("hi") {
		t.Fatalf("%d of the %d glyphs of %q are inside the field %v; the horizontal offset of "+
			"the long value survived the short one and the field is blank.\n%s",
			got, len("hi"), "hi", b, h.Dump())
	}
}

// glyphsInside counts the glyphs of the frame whose origin is inside b. The
// vertical test is what keeps a label underneath the field out of the count.
func glyphsInside(h *gifttest.Harness, b geom.Rect) int {
	n := 0
	l := h.List()
	for _, op := range h.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		for _, g := range l.Glyphs(op.Glyphs, op.GlyphCount) {
			if g.X >= b.Min.X && g.X <= b.Max.X && g.Y >= b.Min.Y && g.Y <= b.Max.Y {
				n++
			}
		}
	}
	return n
}

// --- allocations -----------------------------------------------------------------

// TestTheFrameOfAFocusedFieldIsAllocationFree is the performance contract of
// the project plan, section 11, for this widget: a blinking caret must not
// allocate per frame.
//
// It is the frame path without a build, which is what that section covers:
// BeginInput, Update with nothing dirty, and Paint. The blink is what makes
// this interesting — the caret's visibility changes during the measurement, so
// both branches of it are exercised.
func TestTheFrameOfAFocusedFieldIsAllocationFree(t *testing.T) {
	ed := ui.NewTextEditor("hello world")
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
			Frame(200, geom.Unbounded()).Key("field")).Padding(10)
	}})
	now := time.Duration(0)
	if err := a.Update(geom.Sz(400, 200)); err != nil {
		t.Fatal(err)
	}
	a.Paint()
	a.BeginInput(now)
	if !a.MoveFocus(true) {
		t.Fatal("nothing took the focus")
	}
	step := func() {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		if err := a.Update(geom.Sz(400, 200)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 32 {
		step()
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("a frame with a blinking caret allocated %v times per run, want 0", got)
	}
}

// BenchmarkTextFieldFrame is the number a measurement run quotes for the
// widget: one frame of a focused field with a caret in it.
func BenchmarkTextFieldFrame(b *testing.B) {
	ed := ui.NewTextEditor("hello world")
	f := benchFont(b)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.TextField(ed).Font(f).FontSize(16).
			Frame(200, geom.Unbounded()).Key("field")).Padding(10)
	}})
	now := time.Duration(0)
	_ = a.Update(geom.Sz(400, 200))
	a.Paint()
	a.BeginInput(now)
	a.MoveFocus(true)
	step := func() {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		_ = a.Update(geom.Sz(400, 200))
		a.Paint()
	}
	for range 32 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

// BenchmarkTextFieldKeystroke is the editing path: insert a character and
// delete it again, with everything a keystroke drags behind it.
//
// It is *not* allocation free and is not meant to be. Every keystroke builds
// one new string for the document, because the shaping cache is keyed by one
// and keeps it; two keystrokes, two strings, and that is the whole of what
// this loop allocates.
//
// What it deliberately does *not* measure is the shaping itself. The two
// strings it alternates between are both in the cache after the warmup, so
// every frame here is a cache hit; a genuinely new string pays a miss of
// roughly five kilobytes in harfbuzz, which the project plan, section 11,
// excludes from the contract by name and which no benchmark of this widget
// could do anything about. What this number is for is noticing the day a
// keystroke starts allocating ten times instead of twice.
func BenchmarkTextFieldKeystroke(b *testing.B) {
	ed := ui.NewTextEditor("hello world")
	f := benchFont(b)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.TextField(ed).Font(f).FontSize(16).
			Frame(200, geom.Unbounded()).Key("field")).Padding(10)
	}})
	now := time.Duration(0)
	_ = a.Update(geom.Sz(400, 200))
	a.Paint()
	a.BeginInput(now)
	a.MoveFocus(true)
	step := func() {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		a.TypeRune('x')
		a.KeyDown(gift.KeyBackspace, 0)
		a.KeyUp(gift.KeyBackspace, 0)
		_ = a.Update(geom.Sz(400, 200))
		a.Paint()
	}
	for range 32 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

func benchFont(b *testing.B) ui.Font {
	b.Helper()
	return loadTestFont(b)
}

// --- the editor's lifetime -------------------------------------------------------

// TestTheEditorOfAComponentSurvivesTheRebuildsItsOwnTypingCauses is the
// property the whole design of [ui.TextEditor] exists for.
//
// Every keystroke writes application state and rebuilds the component, which
// builds a new view and a new node. If the caret lived in either of those, it
// would be back at zero after every character and this test would end with
// "ihtere" or "hi" rather than "there".
func TestTheEditorOfAComponentSurvivesTheRebuildsItsOwnTypingCauses(t *testing.T) {
	var seen string
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			ed := ui.Editor(ctx, "name", "hi ")
			count := ctx.State("count", 0)
			return ui.VStack(
				ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
					Frame(200, geom.Unbounded()).Key("field").
					OnChange(func(s string) {
						seen = s
						count.Set(count.Get() + 1)
					}),
				ui.Text("edits: "+string(rune('0'+ctx.Read(count)))).Font(loadTestFont(t)),
			).Padding(10)
		},
		Size: geom.Sz(400, 200),
		Font: loadTestFont(t),
	})
	h.Find(gifttest.ByKey("field")).Focus()
	h.TypeText("there")
	if seen != "hi there" {
		t.Fatalf("after typing into a rebuilding component the value is %q, want %q", seen, "hi there")
	}
}

// TestTwoFieldsOverOneEditorAreRejected. The symptom without the check is two
// carets writing one position, in one frame, with nothing to see but text
// jumping between two places.
func TestTwoFieldsOverOneEditorAreRejected(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if r == nil || !strings.Contains(msg, "two TextField views over one ui.TextEditor") {
			t.Fatalf("two fields over one editor were accepted (%v)", r)
		}
	}()
	ed := ui.NewTextEditor("x")
	f := func() ui.TextFieldView {
		return ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).Frame(200, geom.Unbounded())
	}
	gifttest.New(t, gifttest.Options{
		View: ui.VStack(f().Key("a"), f().Key("b")),
		Size: geom.Sz(400, 200),
		Font: loadTestFont(t),
	})
}

// TestThePlaceholderIsShownOnlyWhileTheFieldIsEmpty, and is the accessible
// name of the field so that a test and a screen reader can find it by the
// words next to it.
func TestThePlaceholderIsShownOnlyWhileTheFieldIsEmpty(t *testing.T) {
	f := newField(t, "", func(v ui.TextFieldView) ui.TextFieldView {
		return v.Placeholder("Your name")
	})
	if !f.h.Exists(gifttest.ByText("Your name")) {
		t.Fatalf("the placeholder is not the accessible name of the field.\n%s", f.h.Dump())
	}
	empty := countGlyphs(f.h)
	if empty == 0 {
		t.Fatal("an empty field with a placeholder drew no glyphs")
	}

	f.node().Focus()
	f.h.TypeText("a")
	if got := countGlyphs(f.h); got != 1 {
		t.Fatalf("a field holding one character drew %d glyphs, want 1; the placeholder is "+
			"still being drawn under the text", got)
	}
}

func countGlyphs(h *gifttest.Harness) int {
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpGlyphs {
			n += int(op.GlyphCount)
		}
	}
	return n
}

// TestTabbingToAFieldBelowTheFoldScrollsItIntoView. A field that takes the
// focus off screen is a focus ring nobody can see and a caret nobody can find;
// this is the [gift.App.ScrollIntoView] the project plan, section 19, names for
// exactly this.
func TestTabbingToAFieldBelowTheFoldScrollsItIntoView(t *testing.T) {
	ed := ui.NewTextEditor("")
	rows := make([]gift.View, 0, 13)
	for i := range 12 {
		rows = append(rows, ui.Box().Frame(200, 40).Background(ui.RGB(30, 30, 30)).Key(string(rune('a'+i))))
	}
	rows = append(rows, ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
		Frame(200, geom.Unbounded()).Key("field"))
	h := gifttest.New(t, gifttest.Options{
		View: ui.VScroll(rows...).Gap(4).Frame(300, 200).Key("scroller"),
		Size: geom.Sz(400, 300),
		Font: loadTestFont(t),
	})
	scroller := h.Find(gifttest.ByKey("scroller"))
	if info, _ := h.App().ScrollInfo(scroller.Ref()); info.Offset != 0 {
		t.Fatalf("the fixture starts scrolled to %v", info.Offset)
	}
	viewport := scroller.Bounds()
	if b := h.Find(gifttest.ByKey("field")).Bounds(); b.Min.Y < viewport.Max.Y {
		t.Fatalf("the fixture is wrong: the field at %v is already inside the viewport %v", b, viewport)
	}

	h.Tab()
	if n, ok := h.Focused(); !ok || n.Key() != "field" {
		t.Fatalf("tab did not reach the field.\n%s", h.Dump())
	}
	b := h.Find(gifttest.ByKey("field")).Bounds()
	if b.Max.Y > viewport.Max.Y || b.Min.Y < viewport.Min.Y {
		t.Fatalf("the focused field is at %v, outside the viewport %v; it was not scrolled into view",
			b, viewport)
	}
}

// TestBothKeyboardConventionsForWordAndLineMotion drives the caret with each
// of the two conventions [gift.WordModifier] documents, by assigning the two
// package variables rather than by trusting the ones this machine booted with.
//
// It is the test that was missing, and the defect it would have caught was
// real: the field tested the shortcut modifier first, so on X11 and on Windows
// — where both variables are control, and X11 is the platform of the project
// plan, section 1 — control-left and control-right meant home and end and
// [ui.TextEditor.MoveWordLeft] could not be reached from the keyboard at all.
// Nobody develops on that platform, and both variables are ordinary package
// variables, so a test can be on it in one line.
func TestBothKeyboardConventionsForWordAndLineMotion(t *testing.T) {
	const s = "hello wide world"
	for _, tc := range []struct {
		name           string
		shortcut, word gift.Mods
		// wantShortcutRight is where control-right or command-right lands. The
		// two conventions differ in exactly this.
		wantShortcutRight int
	}{
		{"macOS: command is the line, option is the word", gift.ModMeta, gift.ModAlt, len(s)},
		{"X11 and Windows: control is both, and the word wins", gift.ModControl, gift.ModControl, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer withModifiers(tc.shortcut, tc.word)()

			f := newField(t, s, nil)
			f.node().Focus()

			f.ed.SetCaret(0)
			f.h.Key(gift.KeyRight, gift.WordModifier)
			if got := f.ed.Caret(); got != 5 {
				t.Fatalf("the word modifier and right put the caret at %d, want 5 — the end of "+
					"%q. The word motion is unreachable from this keyboard.", got, "hello")
			}

			f.ed.SetCaret(len(s))
			f.h.Key(gift.KeyLeft, gift.WordModifier)
			if got := f.ed.Caret(); got != 11 {
				t.Fatalf("the word modifier and left put the caret at %d, want 11 — the start "+
					"of %q. The word motion is unreachable from this keyboard.", got, "world")
			}

			f.ed.SetCaret(0)
			f.h.Key(gift.KeyRight, gift.ShortcutModifier)
			if got := f.ed.Caret(); got != tc.wantShortcutRight {
				t.Fatalf("the shortcut modifier and right put the caret at %d, want %d",
					got, tc.wantShortcutRight)
			}

			wantShortcutLeft := 11 // the start of "world", the previous word
			if tc.wantShortcutRight == len(s) {
				wantShortcutLeft = 0 // the start of the line
			}
			f.ed.SetCaret(len(s))
			f.h.Key(gift.KeyLeft, gift.ShortcutModifier)
			if got := f.ed.Caret(); got != wantShortcutLeft {
				t.Fatalf("the shortcut modifier and left put the caret at %d, want %d",
					got, wantShortcutLeft)
			}

			// Home and end are the chord-free way to the ends and mean the
			// same thing under both conventions. On the colliding keyboard
			// they are the *only* way, which is what the convention is.
			f.h.Key(gift.KeyEnd)
			if got := f.ed.Caret(); got != len(s) {
				t.Fatalf("end put the caret at %d, want %d", got, len(s))
			}
			f.h.Key(gift.KeyHome)
			if got := f.ed.Caret(); got != 0 {
				t.Fatalf("home put the caret at %d, want 0", got)
			}
		})
	}
}

// withModifiers installs a keyboard convention for the duration of a test and
// returns the restore function.
func withModifiers(shortcut, word gift.Mods) func() {
	prevS, prevW := gift.ShortcutModifier, gift.WordModifier
	gift.ShortcutModifier, gift.WordModifier = shortcut, word
	return func() { gift.ShortcutModifier, gift.WordModifier = prevS, prevW }
}

// TestShiftClickExtendsTheSelectionFromTheCaret is the pointer half of
// shift-arrow and the way a selection longer than the field is made: click at
// one end, shift-click at the other.
func TestShiftClickExtendsTheSelectionFromTheCaret(t *testing.T) {
	const s = "helloworldagain"
	f := newField(t, s, nil)
	f.node().Focus()
	f.clickAtOffset(t, 0)
	if f.ed.HasSelection() {
		t.Fatalf("the first click selected %q", f.ed.SelectedText())
	}

	f.h.Advance(DoubleClickGap)
	f.h.SetModifiers(gift.ModShift)
	f.clickAtOffset(t, 5)
	f.h.SetModifiers()
	if got := f.ed.SelectedText(); got != "hello" {
		t.Fatalf("the shift-click selected %q, want %q; it started a new selection instead of "+
			"extending the old one", got, "hello")
	}
}

// TestARuneArrivingWithTheShortcutModifierIsNotTyped is the other half of
// TestAnUnmodifiedShortcutLetterIsJustALetter. A platform that does not filter
// the character out of control-V would otherwise paste *and* insert a "v".
func TestARuneArrivingWithTheShortcutModifierIsNotTyped(t *testing.T) {
	f := newField(t, "", nil)
	f.node().Focus()
	f.h.SetModifiers(gift.ShortcutModifier)
	f.h.TypeRune('v')
	f.h.SetModifiers()
	if got := f.ed.Text(); got != "" {
		t.Fatalf("a character delivered with the shortcut modifier held was inserted: %q", got)
	}
	// And without it, the same character is an ordinary one.
	f.h.TypeRune('v')
	if got := f.ed.Text(); got != "v" {
		t.Fatalf("the same character without the modifier produced %q", got)
	}
}

// TestLabelOverridesThePlaceholderAsTheAccessibleName. The placeholder is the
// name a person would use for a field that has one, and [ui.TextFieldView.Label]
// is for the field whose visible text says something else — or nothing.
func TestLabelOverridesThePlaceholderAsTheAccessibleName(t *testing.T) {
	f := newField(t, "", func(v ui.TextFieldView) ui.TextFieldView {
		return v.Placeholder("dd.mm.yyyy").Label("Date of birth")
	})
	if !f.h.Exists(gifttest.ByText("Date of birth")) {
		t.Fatalf("Label is not the accessible name of the field.\n%s", f.h.Dump())
	}
	if f.h.Exists(gifttest.ByText("dd.mm.yyyy")) {
		t.Fatalf("the placeholder is still the accessible name, so Label did not override it.\n%s",
			f.h.Dump())
	}
	// It is a name and not a caption: nothing of it is drawn, and the
	// placeholder is still the text the user sees.
	if got := countGlyphs(f.h); got != len("dd.mm.yyyy") {
		t.Fatalf("the empty field drew %d glyphs, want the %d of the placeholder; Label is "+
			"never drawn", got, len("dd.mm.yyyy"))
	}
}

// TestAPasteKeepsOnlyPrintableCharactersOfTheFirstLine is the tab half of
// [singleLine], and the fixture puts the tab *before* the first line break on
// purpose: a tab after it is removed by the line cut and proves nothing about
// the control character filter.
func TestAPasteKeepsOnlyPrintableCharactersOfTheFirstLine(t *testing.T) {
	cb := &ui.MemoryClipboard{}
	cb.SetText("first\tline\nsecond")
	ui.SetClipboard(cb)
	t.Cleanup(func() { ui.SetClipboard(nil) })

	f := newField(t, "", nil)
	f.node().Focus()
	f.h.Key(gift.KeyV, gift.ShortcutModifier)
	if got := f.ed.Text(); got != "firstline" {
		t.Fatalf("the paste produced %q, want %q: the tab before the line break has to go, "+
			"because a field where tab means \"next field\" cannot show it and cannot delete "+
			"it by pressing tab again", got, "firstline")
	}
}

// --- the scroll offset -----------------------------------------------------------

// fieldPadRight is the default right padding, the mirror of [fieldPadLeft].
const fieldPadRight = 8

// TestTheCaretAtTheEndOfALongTextIsFullyInsideTheField is the trailing slack
// of revealCaret, which is a caret width and not zero.
//
// Without it the caret at the end of a text at least as wide as the field sits
// exactly on the inner right edge and is drawn *outside* it, where the clip
// takes it: the user types and sees no caret at all. The existing scroll test
// compares against the outer bounds, which the caret is inside either way.
func TestTheCaretAtTheEndOfALongTextIsFullyInsideTheField(t *testing.T) {
	f := newField(t, strings.Repeat("abcdefghij", 8), nil)
	b := f.node().Bounds()
	f.node().Focus()
	f.h.Key(gift.KeyEnd)

	c, ok := caretOp(f.h)
	if !ok {
		t.Fatal("no caret after End")
	}
	if right := b.Max.X - fieldPadRight; c.Bounds.Max.X > right {
		t.Fatalf("the caret ends at x=%v and the text area ends at %v, so the last column of "+
			"it is clipped away", c.Bounds.Max.X, right)
	}
}

// TestDeletingBackToAShortTextScrollsTheFieldBackToTheStart is the
// scroll-past-the-end clamp.
//
// A field scrolled four hundred pixels into a long value keeps that offset as
// the value shrinks — every branch of revealCaret is satisfied, because the
// caret stays at the right hand edge all the way down — until the text is
// shorter than the field and the whole of it has been scrolled off to the
// left. The clamp is what pulls it back, and without it the field is blank
// with a caret in it.
func TestDeletingBackToAShortTextScrollsTheFieldBackToTheStart(t *testing.T) {
	f := newField(t, "", nil)
	b := f.node().Bounds()
	f.node().Focus()
	f.h.TypeText(strings.Repeat("abcdefgh", 11))
	if !glyphsStartLeftOf(f.h, b.Min.X) {
		t.Fatal("the fixture is wrong: the field is not scrolled")
	}
	for range 80 {
		f.h.Key(gift.KeyBackspace)
	}
	if got := f.ed.Text(); got != "abcdefgh" {
		t.Fatalf("the fixture is wrong: %q is left", got)
	}

	if glyphsStartLeftOf(f.h, b.Min.X+fieldPadLeft) {
		t.Fatalf("the text still starts left of the field after it became shorter than the "+
			"field; the offset was not clamped back.\n%s", f.h.Dump())
	}
	if got, want := firstGlyphX(f.h), b.Min.X+fieldPadLeft; absf32(got-want) > 0.5 {
		t.Fatalf("the text starts at x=%v, want %v — flush with the left padding", got, want)
	}
}

// firstGlyphX is the x of the leftmost glyph of the frame.
func firstGlyphX(h *gifttest.Harness) float32 {
	l := h.List()
	x := float32(0)
	first := true
	for _, op := range h.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		for _, g := range l.Glyphs(op.Glyphs, op.GlyphCount) {
			if first || g.X < x {
				x, first = g.X, false
			}
		}
	}
	return x
}

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestAnExternalValueIsShownFromItsBeginning is the half of the offset reset
// that the clamp in the layout cannot do.
//
// Replacing a long value with another long one leaves an offset that is legal
// — it is inside the new text, so nothing clamps it — and meaningless: the
// field would open a freshly loaded record somewhere in the middle of it, at
// whatever position the *previous* value had been scrolled to, with the caret
// off screen at the end. A new document has no position worth keeping, so
// [ui.TextEditor.SetText] sets it to zero.
func TestAnExternalValueIsShownFromItsBeginning(t *testing.T) {
	f := newField(t, "", nil)
	b := f.node().Bounds()
	f.node().Focus()
	f.h.TypeText(strings.Repeat("abcdefgh", 11))
	if !glyphsStartLeftOf(f.h, b.Min.X) {
		t.Fatal("the fixture is wrong: the field did not scroll")
	}

	f.ed.SetText(strings.Repeat("zyxwvuts", 11))
	f.h.Settle()
	if got, want := firstGlyphX(f.h), b.Min.X+fieldPadLeft; absf32(got-want) > 0.5 {
		t.Fatalf("after a new value was written the text starts at x=%v, want %v: the field "+
			"is showing the middle of a record it has only just been given", got, want)
	}
}

// TestAWiderWindowDoesNotLeaveTheFieldScrolledPastItsText is the clamp in the
// layout, and it is the path no event handler passes through.
//
// The field here is as wide as the window. Typing to the end scrolls it, and
// then the window grows: the text now fits, but the offset that was legal a
// moment ago is not, and nothing the user did can correct it because no event
// happened — the window changed size. Layout is the only place that knows both
// the new width and the text, which is why the clamp is called there too.
func TestAWiderWindowDoesNotLeaveTheFieldScrolledPastItsText(t *testing.T) {
	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).Flex(1).Key("field")).
			Padding(10),
		Size: geom.Sz(200, 200),
		Font: loadTestFont(t),
	})
	h.Find(gifttest.ByKey("field")).Focus()
	h.TypeText(strings.Repeat("abcdefgh", 6))
	if !glyphsStartLeftOf(h, h.Find(gifttest.ByKey("field")).Bounds().Min.X) {
		t.Fatal("the fixture is wrong: the narrow field did not scroll")
	}

	h.Resize(geom.Sz(1200, 200))
	b := h.Find(gifttest.ByKey("field")).Bounds()
	if got, want := firstGlyphX(h), b.Min.X+fieldPadLeft; absf32(got-want) > 0.5 {
		t.Fatalf("after the window grew the text starts at x=%v, want %v: the field is wide "+
			"enough for all of it and is still scrolled off to the left", got, want)
	}
}

// --- the unbounded width --------------------------------------------------------

// TestAFieldInARowTakesTheDefaultWidthAndKeepsItWhileTyping covers the branch
// every other test in this file avoids by writing Frame: a field in an
// [ui.HStack], which is the idiomatic form row and which rule 1 of the overflow
// model measures with an unbounded main axis.
//
// The second half is the property the constant exists for, and it is the one
// worth pinning: the width does not follow the text. A field that measured its
// content would grow under the typist and jump back when they erased, moving
// every sibling in the row with it.
func TestAFieldInARowTakesTheDefaultWidthAndKeepsItWhileTyping(t *testing.T) {
	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		View: ui.HStack(
			ui.Text("Name").Font(loadTestFont(t)).FontSize(16),
			ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).Key("field"),
		).Gap(8).Padding(10),
		Size: geom.Sz(800, 200),
		Font: loadTestFont(t),
	})
	field := func() geom.Rect { return h.Find(gifttest.ByKey("field")).Bounds() }

	empty := field()
	if got := empty.Width(); got != ui.DefaultFieldWidth {
		t.Fatalf("a field with nothing bounding it is %v wide, want the default %v",
			got, ui.DefaultFieldWidth)
	}

	h.Find(gifttest.ByKey("field")).Focus()
	h.TypeText(strings.Repeat("wide enough to overflow twice over ", 3))
	if got := field(); got != empty {
		t.Fatalf("the field is at %v after typing and was at %v; its size followed its text",
			got, empty)
	}
	// And the caret is still in it, which is the part the horizontal scroll
	// does instead of growing.
	c, ok := caretOp(h)
	if !ok {
		t.Fatal("no caret in the field after typing")
	}
	if c.Bounds.Min.X < empty.Min.X || c.Bounds.Max.X > empty.Max.X {
		t.Fatalf("the caret is at %v, outside the field %v", c.Bounds, empty)
	}
}

// --- the gesture in a scrolling form ---------------------------------------------

// TestAVerticalDragOnAFieldScrollsTheFormAroundIt is the other half of
// TestADragInsideAScrollerSelectsTextInsteadOfScrollingIt and the cost that
// test does not measure.
//
// A field that consumed every move from the press onwards made a form
// unscrollable wherever a finger landed on one, which on the kiosk of the
// project plan, section 19 — where a touch arrives as a mouse under X11 — is
// most of the rows of most of the forms. Measured before the fix: a swipe over
// the field moved the form zero of a possible 313 pixels.
func TestAVerticalDragOnAFieldScrollsTheFormAroundIt(t *testing.T) {
	h, ed, scroller := scrollingForm(t)
	b := h.Find(gifttest.ByKey("field")).Bounds()
	x := b.Min.X + 40
	y := b.Min.Y + b.Height()/2

	h.PressAt(geom.Pt(x, y))
	for i := 1; i <= 8; i++ {
		h.MoveTo(geom.Pt(x, y-float32(i)*10))
	}
	h.ReleaseAt(geom.Pt(x, y-80))

	after, _ := h.App().ScrollInfo(scroller.Ref())
	if !(after.Offset > 0) {
		t.Fatalf("a vertical swipe over the field scrolled the form by %v; a form whose rows "+
			"are fields would not be scrollable at all", after.Offset)
	}
	if ed.HasSelection() {
		t.Fatalf("the vertical swipe also selected %q; the gesture went to both", ed.SelectedText())
	}
}

// scrollingForm is one field above eleven boxes in a VScroll that has room to
// move. It is the fixture of the two gesture tests.
func scrollingForm(t *testing.T) (*gifttest.Harness, *ui.TextEditor, gifttest.Node) {
	t.Helper()
	ed := ui.NewTextEditor("hello world")
	rows := make([]gift.View, 0, 12)
	rows = append(rows, ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
		Frame(200, geom.Unbounded()).Key("field"))
	for i := range 11 {
		rows = append(rows, ui.Box().Frame(200, 40).Background(ui.RGB(30, 30, 30)).Key(string(rune('a'+i))))
	}
	h := gifttest.New(t, gifttest.Options{
		View: ui.VScroll(rows...).Gap(4).Frame(300, 200).Key("scroller"),
		Size: geom.Sz(400, 300),
		Font: loadTestFont(t),
	})
	scroller := h.Find(gifttest.ByKey("scroller"))
	info, ok := h.App().ScrollInfo(scroller.Ref())
	if !ok || !(info.MaxOffset > 0) {
		t.Fatalf("the fixture does not scroll: %+v", info)
	}
	return h, ed, scroller
}

// TestADoubleClickAndDragExtendsBySelectingWholeWords. Every platform does
// this, and the version that armed the drag only on a single click did two
// wrong things at once: the selection did not grow by words, and the moves
// were not consumed, so the same gesture scrolled the container underneath.
func TestADoubleClickAndDragExtendsBySelectingWholeWords(t *testing.T) {
	const s = "alpha bravo charlie"
	f := newField(t, s, nil)
	b := f.node().Bounds()
	y := b.Min.Y + b.Height()/2
	x := func(off int) float32 { return b.Min.X + fieldPadLeft + prefixWidth(t, s[:off]) + 1 }

	// Two presses at the same place, the second one held.
	f.h.ClickAt(geom.Pt(x(2), y))
	f.h.PressAt(geom.Pt(x(2), y))
	if got := f.ed.SelectedText(); got != "alpha" {
		t.Fatalf("the double click selected %q, want %q", got, "alpha")
	}
	// Into the middle of "charlie", well past the slop and horizontally.
	f.h.MoveTo(geom.Pt(x(9), y))
	f.h.MoveTo(geom.Pt(x(15), y))
	f.h.ReleaseAt(geom.Pt(x(15), y))

	if got := f.ed.SelectedText(); got != s {
		t.Fatalf("the double click and drag selected %q, want the whole of %q — a word drag "+
			"takes whole words at the moving end", got, s)
	}
}

// --- the blink window as a number ------------------------------------------------

// TestTheBlinkWindowIsAShortNumberOfSeconds is the assertion
// TestTheApplicationSettlesAfterTheBlinkWindowAndAfterBlur cannot make, because
// that test advances the clock by [ui.CaretBlinkWindow] itself and therefore
// can only fail on the mechanism.
//
// The value is the point. The window exists so that an untouched kiosk stops
// waking at the frame rate, and a window of a minute — or of a year, which is
// what "make it not blink out" looks like as a patch — would keep the
// mechanism working and lose the whole reason for it. So the bounds are named
// here: long enough that the blink has said "this field has the focus", short
// enough that the screen settles while the coffee is made.
func TestTheBlinkWindowIsAShortNumberOfSeconds(t *testing.T) {
	if ui.CaretBlinkWindow < 4*ui.CaretBlinkInterval {
		t.Fatalf("the blink window is %v, less than four blinks of %v; the caret goes solid "+
			"before it has said anything", ui.CaretBlinkWindow, ui.CaretBlinkInterval)
	}
	if ui.CaretBlinkWindow > 30*time.Second {
		t.Fatalf("the blink window is %v; that is half a minute of full rate frames after "+
			"every keystroke on a kiosk that is meant to idle", ui.CaretBlinkWindow)
	}
}

// TestDisablingAFocusedFieldStopsTheCaretAtOnce used to be
// TestDisablingAFocusedFieldLeaksAtMostOneBlinkWindow, and the rename is the
// finding.
//
// A field that is disabled while it holds the focus was never told
// [gift.EventFocusLost]: gift suppressed the notification when the focus
// changed during a build, because an application handler that reshaped the
// tree under the reconciler is the worse of the two failures. So the field
// never cancelled its animation and the application kept asking for frames
// until the deadline it last asked for — ten seconds of wakeups drawing no
// caret, because a disabled field draws none. That was written down, bounded
// and accepted.
//
// It is not accepted any more. The notification is deferred to the end of the
// build instead of dropped, and it reaches a disabled node, because the node
// is the only thing in the process that can let go of what it is holding; see
// gift's pending.go. So the bound is zero and this test asserts zero.
//
// The control case is the point of the first loop: a focused field that is
// left alone asks for frames on every tick, so a failure to measure anything
// cannot be mistaken for a fix.
func TestDisablingAFocusedFieldStopsTheCaretAtOnce(t *testing.T) {
	ed := ui.NewTextEditor("x")
	var off *gift.State[bool]
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			off = ctx.State("off", false)
			return ui.VStack(ui.TextField(ed).Font(loadTestFont(t)).FontSize(16).
				Frame(200, geom.Unbounded()).Disabled(ctx.Read(off)).Key("field")).Padding(10)
		},
		Size: geom.Sz(400, 200),
		Font: loadTestFont(t),
	})
	h.Find(gifttest.ByKey("field")).Focus()
	if busy := busyTicks(h, 20); busy != 20 {
		t.Fatalf("a focused field asked for %d of 20 frames; the caret is not enrolled at "+
			"all and the measurement below would prove nothing", busy)
	}

	off.Set(true)
	h.Settle()
	if _, ok := caretOp(h); ok {
		t.Fatal("a disabled field is still drawing a caret")
	}
	// Ten seconds at 60 Hz is 600 ticks; 120 is two seconds, which is two
	// orders of magnitude more than the zero this must be and short enough to
	// run. A regression would show up as 120.
	if busy := busyTicks(h, 120); busy != 0 {
		t.Fatalf("the application asked for %d of 120 frames after a focused field was "+
			"disabled. The field never heard gift.EventFocusLost, so it never called "+
			"Animate(0), and the kiosk stays awake for the whole %v blink window with "+
			"nothing on screen to show for it", busy, ui.CaretBlinkWindow)
	}
}

// busyTicks drives n input-plus-paint cycles by hand and counts the ones in
// which something still wanted to be drawn.
//
// It is by hand rather than through Harness.Advance because the question is
// the value of NeedsPaint *between* the input phase and the paint: Paint
// clears the flag as its last act, so asking afterwards reads false for an
// application that never stops drawing.
//
// The harness clock is moved to where the loop left it at the end. Without
// that, two calls in one test would both start from the same instant, the
// second would replay the frames of the first at timestamps that had already
// passed, and anything with a deadline would never expire — which presents as
// a test that hangs rather than as a measurement that is wrong.
func busyTicks(h *gifttest.Harness, n int) int {
	const step = 16 * time.Millisecond
	a := h.App()
	now := h.Now()
	busy := 0
	for range n {
		now += step
		a.BeginInput(now)
		if err := a.Update(h.Size()); err != nil {
			panic(err)
		}
		if a.NeedsPaint() {
			busy++
		}
		a.Paint()
	}
	h.Advance(time.Duration(n) * step)
	return busy
}
