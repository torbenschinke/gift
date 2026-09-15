package ui_test

import (
	"strings"
	"testing"

	"github.com/torbenschinke/gift/ui"
)

// The editor model, tested directly. Word boundaries, rune arithmetic and the
// replacement rule have nothing to do with events, and a harness around them
// would only make the failures harder to read; the widget half is in
// textfield_test.go and goes through real input.

// TestTheEditorSplicesUTF8AndNeverBetweenTheBytesOfARune. Every offset this
// type hands out or accepts has to sit on a rune boundary, whatever it is
// given.
func TestTheEditorSplicesUTF8AndNeverBetweenTheBytesOfARune(t *testing.T) {
	e := ui.NewTextEditor("mäßig")
	if got, want := e.Caret(), len("mäßig"); got != want {
		t.Fatalf("a new editor puts the caret at %d, want the end at %d", got, want)
	}

	// Offset 2 is the second byte of the 'ä'. Landing there and then typing
	// would produce invalid UTF-8.
	e.SetCaret(2)
	if got := e.Caret(); got != 1 {
		t.Fatalf("a caret set inside a rune stayed at %d, want it snapped back to 1", got)
	}
	e.InsertRune('x')
	if got := e.Text(); got != "mxäßig" {
		t.Fatalf("the insertion produced %q (% x)", got, got)
	}

	e.SetCaret(len("mxäßig"))
	for range 3 {
		e.DeleteBackward()
	}
	if got := e.Text(); got != "mxä" {
		t.Fatalf("three backspaces left %q, want %q — each one removes a whole rune", got, "mxä")
	}
}

// TestWordSelectionTreatsWordsSpacesAndPunctuationAsThreeKindsOfRun is the
// rule behind the double click and behind the word-wise arrows.
func TestWordSelectionTreatsWordsSpacesAndPunctuationAsThreeKindsOfRun(t *testing.T) {
	const s = "grün, weiß  und_rot!!"
	for _, tc := range []struct {
		at   int
		want string
	}{
		{0, "grün"},
		{3, "grün"},        // inside the umlaut word
		{len("grün"), ","}, // punctuation is its own run, and so is the space after it
		{len("grün,"), " "},
		{len("grün, "), "weiß"},
		{len("grün, weiß"), "  "},
		{len("grün, weiß  "), "und_rot"},
		{len(s) - 1, "!!"},
		{len(s), "!!"}, // a caret at the very end belongs to the run before it
	} {
		e := ui.NewTextEditor(s)
		e.SelectWordAt(tc.at)
		if got := e.SelectedText(); got != tc.want {
			t.Errorf("a double click at byte %d selected %q, want %q", tc.at, got, tc.want)
		}
	}
}

// TestWordMotionSkipsTheSpacesBetweenWords, which is the difference between
// "move to the next boundary" and "move to the next word" — the first one
// stops twice between two words and feels broken.
func TestWordMotionSkipsTheSpacesBetweenWords(t *testing.T) {
	const s = "one two  three"
	e := ui.NewTextEditor(s)
	e.SetCaret(0)

	want := []int{3, 7, len(s)}
	for i, w := range want {
		e.MoveWordRight(false)
		if got := e.Caret(); got != w {
			t.Fatalf("word-right %d put the caret at %d (%q|%q), want %d",
				i+1, got, s[:got], s[got:], w)
		}
	}
	for _, w := range []int{9, 4, 0} {
		e.MoveWordLeft(false)
		if got := e.Caret(); got != w {
			t.Fatalf("word-left put the caret at %d (%q|%q), want %d", got, s[:got], s[got:], w)
		}
	}
}

// TestAnInsertReplacesTheSelectionAndAnEmptyInsertIsNotAChange.
func TestAnInsertReplacesTheSelectionAndAnEmptyInsertIsNotAChange(t *testing.T) {
	e := ui.NewTextEditor("hello world")
	e.SetSelection(6, 11)
	if !e.Insert("there") {
		t.Fatal("Insert reported no change")
	}
	if got := e.Text(); got != "hello there" {
		t.Fatalf("the document is %q", got)
	}
	if e.HasSelection() || e.Caret() != len("hello there") {
		t.Fatalf("after the replacement the caret is at %d and the selection is %q",
			e.Caret(), e.SelectedText())
	}
	if e.Insert("") {
		t.Fatal("inserting nothing with nothing selected reported a change")
	}
}

// TestTheDocumentBufferStopsAllocatingOnceItIsWarm is the editing half of the
// performance contract: typing grows the text and nothing else.
//
// One allocation per keystroke is expected and is the string the shaping cache
// is keyed by; see [ui.TextEditor]. The buffer itself must not be part of it
// once the capacity is there, which is what makes the second half of this test
// — typing into a buffer that has already been that long — the interesting
// one.
func TestTheDocumentBufferStopsAllocatingOnceItIsWarm(t *testing.T) {
	e := ui.NewTextEditor(strings.Repeat("x", 512))
	e.SetText("")
	// The buffer keeps its capacity across SetText, so these 512 insertions
	// must not grow it. The one allocation left is the cached string, and it
	// is only built when somebody reads the text — which nothing here does.
	if got := testing.AllocsPerRun(20, func() {
		for range 64 {
			e.InsertRune('a')
		}
		for range 64 {
			e.DeleteBackward()
		}
	}); got != 0 {
		t.Fatalf("128 edits in a warm buffer allocated %v times, want 0", got)
	}
}

// TestTheDefaultClipboardIsInProcessAndRoundTrips. The seam has a working
// implementation before the platform one exists, which is what makes the field
// testable today.
func TestTheDefaultClipboardIsInProcessAndRoundTrips(t *testing.T) {
	c := ui.CurrentClipboard()
	if c == nil {
		t.Fatal("CurrentClipboard returned nil; it must never make a caller check")
	}
	mem := &ui.MemoryClipboard{}
	if _, ok := mem.Text(); ok {
		t.Fatal("a fresh clipboard reports that it holds text")
	}
	ui.SetClipboard(mem)
	t.Cleanup(func() { ui.SetClipboard(nil) })
	if got := ui.CurrentClipboard(); got != ui.Clipboard(mem) {
		t.Fatalf("SetClipboard did not install the implementation: %T", got)
	}
	mem.SetText("round trip")
	if got, ok := ui.CurrentClipboard().Text(); !ok || got != "round trip" {
		t.Fatalf("the clipboard returned %q, %v", got, ok)
	}
	ui.SetClipboard(nil)
	if ui.CurrentClipboard() == ui.Clipboard(mem) {
		t.Fatal("SetClipboard(nil) did not restore the default")
	}
}
