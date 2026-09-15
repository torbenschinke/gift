package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// The on-screen keyboard tests of the project plan, section 19.
//
// Every one of them drives the widget through gifttest with real pointer
// events, because everything worth catching here is in the wiring: whether a
// drawn key reaches the field, whether it takes the focus away from it on the
// way, and whether the field it is typing into is still on the screen. None of
// them asserts on an internal signal — the subject is what ends up in the
// document.

// kbFixture is a scrolling form whose last row is a text field, with the
// keyboard overlaid on top of it. The form is deliberately taller than the
// window, so that revealing the field is a real scroll and not a no-op.
type kbFixture struct {
	h      *gifttest.Harness
	ed     *ui.TextEditor
	submit []string
}

const (
	// kbWindow is the test window. It is 500 tall, and the keyboard is 260 of
	// that, so the field has to move a long way to stay visible.
	kbWindowW = float32(560)
	kbWindowH = float32(500)

	// kbHeight is what the metrics of the keyboard come to: two paddings,
	// five rows of keys and four gaps. It is spelled out rather than exported
	// because a change to the metrics should fail the geometry tests, which
	// is where somebody would want to notice it.
	kbHeight = 8 + 8 + 5*44 + 4*6
)

// newKeyboard builds the fixture with the kiosk switch in the given position
// and restores it afterwards.
//
// The switch is process wide, so the restore is not tidiness: without it the
// next test in the file would run with whatever this one left behind. No test
// in this repository calls t.Parallel, which is what makes that safe; see the
// note on process wide state in section 13 of the project plan.
func newKeyboard(t testing.TB, on bool) *kbFixture {
	t.Helper()
	f := &kbFixture{ed: ui.NewTextEditor("")}
	font := loadTestFont(t)

	ui.SetOnScreenKeyboard(nil, on)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })

	rows := make([]gift.View, 0, 15)
	rows = append(rows, ui.Button(ui.Text("done").Font(font), nil).Key("done"))
	for i := range 12 {
		rows = append(rows, ui.Box().Frame(geom.Unbounded(), 40).
			Background(ui.RGB(30, 30, 30)).Key(string(rune('a'+i))))
	}
	rows = append(rows, ui.TextField(f.ed).
		Font(font).
		FontSize(16).
		Frame(200, geom.Unbounded()).
		OnSubmit(func(s string) { f.submit = append(f.submit, s) }).
		Key("field"))
	// Content below the field, and this is not padding for the layout's sake.
	// A reveal can only lift the field by as much as the container can still
	// scroll, and a container can only scroll while there is content left
	// underneath; a field that is the last thing in a form cannot be moved
	// above the keyboard at all. That is the honest limit of the mechanism and
	// it is stated on [ui.SetOnScreenKeyboard].
	rows = append(rows, ui.Box().Frame(geom.Unbounded(), 300).
		Background(ui.RGB(20, 20, 20)).Key("tail"))

	f.h = gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.VScroll(ui.VStack(rows...).Gap(8).Padding(8)).Key("form"),
			ui.OnScreenKeyboard().Font(font).Key("kb"),
		).Align(geom.Bottom).Frame(kbWindowW, kbWindowH),
		Size: geom.Sz(kbWindowW, kbWindowH),
		Font: font,
	})
	return f
}

func (f *kbFixture) field() gifttest.Node { return f.h.Find(gifttest.ByKey("field")) }
func (f *kbFixture) kb() gifttest.Node    { return f.h.Find(gifttest.ByKey("kb")) }

// keyboardIsShowing reads the one observable that means "there is a keyboard":
// the node has a height. A hidden keyboard is still a node — that is the
// cheapest way to say "nothing here" — but it reports a zero size.
func (f *kbFixture) keyboardIsShowing() bool { return f.kb().LayoutBounds().Height() > 0 }

// tap presses and releases the key with the given label, at its centre.
//
// It goes through the harness's raw ClickAt rather than through an aimed
// action, because a key is not a node and there is nothing to aim at; see
// [ui.KeyRectForTest].
func (f *kbFixture) tap(t testing.TB, label string) {
	t.Helper()
	r, ok := ui.KeyRectForTest(label)
	if !ok {
		t.Fatalf("no key labelled %q on the page that is showing", label)
	}
	b := f.kb().Bounds()
	f.h.ClickAt(geom.Pt(b.Min.X+r.Min.X+r.Width()/2, b.Min.Y+r.Min.Y+r.Height()/2))
}

func (f *kbFixture) tapAll(t testing.TB, labels ...string) {
	t.Helper()
	for _, l := range labels {
		f.tap(t, l)
	}
}

// --- a virtual press is a real press -----------------------------------------

// TestAVirtualKeyPressLeavesTheSameDocumentAsAPhysicalOne is the central
// claim of this widget, and it is checked the only way that is worth
// anything: the same word is entered twice, once by tapping drawn keys and
// once through [gift.App.TypeRune], and the two documents have to agree — in
// their text and in where the caret ended up.
//
// It would pass trivially if the keyboard called TextEditor.Insert directly.
// That is exactly the implementation this test is meant to forbid, so the
// tests below check the things such an implementation would get wrong:
// TestTappingAKeyDoesNotStealTheFocusFromTheField, because a direct call needs
// no focus at all, and TestAKeyTypesIntoWhicheverFieldHasTheFocus, because a
// direct call would need to be told which field.
func TestAVirtualKeyPressLeavesTheSameDocumentAsAPhysicalOne(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tapAll(t, "g", "r", "ü", "n")

	tapped, caret := f.ed.Text(), f.ed.Caret()

	typed := ui.NewTextEditor("")
	g := gifttest.New(t, gifttest.Options{
		View: ui.TextField(typed).Font(loadTestFont(t)).FontSize(16).Key("f"),
		Size: geom.Sz(400, 100),
		Font: loadTestFont(t),
	})
	g.Find(gifttest.ByKey("f")).Focus()
	g.TypeText("grün")

	if tapped != typed.Text() {
		t.Fatalf("the drawn keys produced %q and the physical ones %q", tapped, typed.Text())
	}
	if caret != typed.Caret() {
		t.Fatalf("the caret is at byte %d after tapping and %d after typing", caret, typed.Caret())
	}
	if tapped != "grün" {
		t.Fatalf("both paths produced %q, so they agree and are both wrong", tapped)
	}
}

// TestTheGermanKeysProduceUmlautsAndSharpS is the requirement section 19
// states by name.
//
// The multi byte characters are the point. A keyboard that carried its keys as
// bytes, or that indexed a layout string by byte offset, types "Ã¤" here and
// passes a test that only presses "abc".
func TestTheGermanKeysProduceUmlautsAndSharpS(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tapAll(t, "ä", "ö", "ü", "ß")

	if got := f.ed.Text(); got != "äöüß" {
		t.Fatalf("the document is %q (% x), want %q", got, got, "äöüß")
	}
	if got, want := f.ed.Caret(), len("äöüß"); got != want {
		t.Fatalf("the caret is at byte %d, want %d", got, want)
	}
}

// TestAKeyTypesIntoWhicheverFieldHasTheFocus checks that the keyboard is not
// bound to a field at all.
//
// The keyboard is built once and never told which editor exists. Moving the
// focus with tab and tapping again has to move the text with it; a keyboard
// that had captured a field in a closure would keep filling the first one.
func TestAKeyTypesIntoWhicheverFieldHasTheFocus(t *testing.T) {
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })
	font := loadTestFont(t)
	a, b := ui.NewTextEditor(""), ui.NewTextEditor("")
	field := func(ed *ui.TextEditor, key string) gift.View {
		return ui.TextField(ed).Font(font).FontSize(16).Frame(150, geom.Unbounded()).Key(key)
	}
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.VStack(field(a, "a"), field(b, "b")).Gap(8).Padding(8),
			ui.OnScreenKeyboard().Font(font).Key("kb"),
		).Align(geom.Bottom).Frame(kbWindowW, kbWindowH),
		Size: geom.Sz(kbWindowW, kbWindowH),
		Font: font,
	})
	kb := h.Find(gifttest.ByKey("kb"))
	tap := func(label string) {
		t.Helper()
		r, ok := ui.KeyRectForTest(label)
		if !ok {
			t.Fatalf("no key labelled %q", label)
		}
		o := kb.Bounds().Min
		h.ClickAt(geom.Pt(o.X+r.Min.X+r.Width()/2, o.Y+r.Min.Y+r.Height()/2))
	}

	h.Find(gifttest.ByKey("a")).Focus()
	tap("x")
	h.Tab()
	tap("y")

	if a.Text() != "x" || b.Text() != "y" {
		t.Fatalf("the two fields hold %q and %q, want %q and %q; the keyboard is not following "+
			"the focus", a.Text(), b.Text(), "x", "y")
	}
}

// TestTappingAKeyDoesNotStealTheFocusFromTheField is the classic defect of
// every on-screen keyboard: the first tap blurs the field, the caret vanishes,
// the character arrives nowhere, and the second tap does nothing either.
//
// It is checked after several taps and not just one, because a keyboard that
// took the focus and gave it back would pass a single tap.
func TestTappingAKeyDoesNotStealTheFocusFromTheField(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tapAll(t, "a", "b", "c")

	f.h.AssertFocus(gifttest.ByKey("field"))
	f.field().AssertFocused()
	if got := f.ed.Text(); got != "abc" {
		t.Fatalf("the document is %q; the field lost the focus and the characters went nowhere", got)
	}
	if got := f.ed.Caret(); got != 3 {
		t.Fatalf("the caret is at byte %d, want 3; it was reset on the way", got)
	}
	if _, ok := caretOp(f.h); !ok {
		t.Fatal("no caret is drawn after tapping; the field is not showing itself as focused")
	}
}

// TestThePressedKeyIsTheOneUnderTheFinger pins the hit test. A keyboard whose
// rectangles were off by a row would still type — the wrong letter.
func TestThePressedKeyIsTheOneUnderTheFinger(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	// One key from each of the five rows, by label, top to bottom.
	f.tapAll(t, "5", "z", "j", "v", ",")
	if got := f.ed.Text(); got != "5zjv," {
		t.Fatalf("the document is %q, want %q; the rectangles do not line up with the rows",
			got, "5zjv,")
	}
}

// --- shift and the symbol page ------------------------------------------------

// TestShiftCapitalisesExactlyTheNextCharacter covers both halves of the
// one-shot rule: shift applies to the character after it, and it does not
// apply to the one after that.
func TestShiftCapitalisesExactlyTheNextCharacter(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tap(t, "\u21e7")
	f.tap(t, "A")
	f.tap(t, "b")

	if got := f.ed.Text(); got != "Ab" {
		t.Fatalf("the document is %q, want %q", got, "Ab")
	}
}

// TestShiftChangesTheLabelsAndNotTheGeometry is the property the whole node
// design rests on: the pages have the same shape, so a shift is a repaint and
// never a relayout. If that stops holding, a press stops landing on the key it
// was drawn under, because the rectangles are computed only in Layout.
func TestShiftChangesTheLabelsAndNotTheGeometry(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()

	lower, ok := ui.KeyRectForTest("a")
	if !ok {
		t.Fatal("no lower case a before shift")
	}
	if _, ok := ui.KeyRectForTest("A"); ok {
		t.Fatal("an upper case A is on the page before shift was pressed")
	}

	f.tap(t, "\u21e7")

	upper, ok := ui.KeyRectForTest("A")
	if !ok {
		t.Fatal("no upper case A after shift; the labels did not change")
	}
	if upper != lower {
		t.Fatalf("the A key is at %v and the a key was at %v; shift moved the geometry", upper, lower)
	}
}

// TestTheSymbolSwitchReplacesTheLettersAndComesBack checks the layout switch
// in both directions, and that a character from the symbol page really is
// inserted rather than the letter that used to be in that position.
func TestTheSymbolSwitchReplacesTheLettersAndComesBack(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()

	f.tap(t, "?123")
	if _, ok := ui.KeyRectForTest("q"); ok {
		t.Fatal("the letter q is still on the page after switching to symbols")
	}
	f.tap(t, "@")
	f.tap(t, "#")

	f.tap(t, "ABC")
	if _, ok := ui.KeyRectForTest("@"); ok {
		t.Fatal("the symbol @ is still on the page after switching back to letters")
	}
	f.tap(t, "q")

	if got := f.ed.Text(); got != "@#q" {
		t.Fatalf("the document is %q, want %q", got, "@#q")
	}
}

// --- the command keys ---------------------------------------------------------

// TestBackspaceAndEnterTravelAsKeyPressesAndNotAsCharacters is what separates
// the two input channels of [gift.Key].
//
// Backspace has to delete rather than insert a glyph nobody can see, and enter
// has to reach [ui.TextFieldView.OnSubmit] rather than put a line break into a
// single line field.
func TestBackspaceAndEnterTravelAsKeyPressesAndNotAsCharacters(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tapAll(t, "a", "ß")
	f.tap(t, "\u232b")

	if got := f.ed.Text(); got != "a" {
		t.Fatalf("after backspace the document is %q (% x), want %q; a backspace that removed "+
			"one byte leaves broken UTF-8 behind", got, got, "a")
	}

	f.tap(t, "\u21b5")
	if len(f.submit) != 1 || f.submit[0] != "a" {
		t.Fatalf("OnSubmit saw %q, want one call with %q", f.submit, "a")
	}
	if got := f.ed.Text(); got != "a" {
		t.Fatalf("enter changed the document to %q; it inserted a line break into a single "+
			"line field", got)
	}
}

// TestTheSpaceKeyInsertsASpace. Space is the one key that is both a position
// and a character, and the keyboard sends both, so this also covers the case
// where the two are sent in the wrong order or one of them is dropped.
func TestTheSpaceKeyInsertsASpace(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tapAll(t, "a", "space", "b")
	if got := f.ed.Text(); got != "a b" {
		t.Fatalf("the document is %q, want %q", got, "a b")
	}
}

// TestHoldingBackspaceRepeatsThroughGiftsOwnKeyRepeat checks that the drawn
// key arms the same repeat a physical one does, which is the whole reason a
// command key sends a press and a release instead of a single synthetic
// press.
//
// It presses, moves the clock past the repeat delay and several intervals, and
// then releases. A keyboard that sent press and release together would delete
// exactly one character here.
func TestHoldingBackspaceRepeatsThroughGiftsOwnKeyRepeat(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.h.TypeText("abcdef")

	r, ok := ui.KeyRectForTest("\u232b")
	if !ok {
		t.Fatal("no backspace key")
	}
	b := f.kb().Bounds()
	p := geom.Pt(b.Min.X+r.Min.X+r.Width()/2, b.Min.Y+r.Min.Y+r.Height()/2)

	f.h.PressAt(p)
	after1 := f.ed.Text()
	f.h.Advance(gift.KeyRepeatDelay + 3*gift.KeyRepeatInterval)
	held := f.ed.Text()
	f.h.ReleaseAt(p)
	f.h.Advance(gift.KeyRepeatDelay + 3*gift.KeyRepeatInterval)
	released := f.ed.Text()

	if after1 != "abcde" {
		t.Fatalf("the press alone left %q, want %q; a key types on the press", after1, "abcde")
	}
	if len(held) >= len(after1) {
		t.Fatalf("holding backspace left %q, the same as the single press; the repeat was "+
			"never armed", held)
	}
	if released != held {
		t.Fatalf("the document went on shrinking from %q to %q after the finger was lifted; "+
			"the release did not disarm the repeat", held, released)
	}
}

// --- appearing and disappearing -----------------------------------------------

// TestTheKeyboardAppearsOnFocusOnlyWithTheKioskSwitchOn is the switch of
// section 19, in both positions.
//
// The off case is the one that matters for a desktop: a field that takes the
// focus on a developer's machine must not be covered by a keyboard nobody
// asked for.
func TestTheKeyboardAppearsOnFocusOnlyWithTheKioskSwitchOn(t *testing.T) {
	t.Run("switch on", func(t *testing.T) {
		f := newKeyboard(t, true)
		if f.keyboardIsShowing() {
			t.Fatal("the keyboard is on the screen before anything took the focus")
		}
		f.field().Focus()
		if !f.keyboardIsShowing() {
			t.Fatal("the field has the focus and the switch is on, but there is no keyboard")
		}
		if got := f.kb().LayoutBounds().Height(); got != kbHeight {
			t.Fatalf("the keyboard is %v tall, want %v", got, float32(kbHeight))
		}
	})

	t.Run("switch off", func(t *testing.T) {
		f := newKeyboard(t, false)
		f.field().Focus()
		if f.keyboardIsShowing() {
			t.Fatalf("the kiosk switch is off and the keyboard appeared anyway, %v tall",
				f.kb().LayoutBounds().Height())
		}
		f.h.TypeText("x")
		if got := f.ed.Text(); got != "x" {
			t.Fatalf("with the switch off the field holds %q; the physical keyboard was broken "+
				"by a feature that is not even on", got)
		}
	})
}

// TestTheKeyboardDisappearsWhenTheFieldLosesTheFocus. The blur is a click on
// the form above the keyboard, which is how it happens in a kiosk.
func TestTheKeyboardDisappearsWhenTheFieldLosesTheFocus(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	if !f.keyboardIsShowing() {
		t.Fatal("no keyboard to dismiss")
	}
	// The focus is moved to a button rather than cleared by a click on the
	// background: the form is a scroll container, and a scroll container is a
	// hit target, so a click on it does not clear the focus.
	f.h.Find(gifttest.ByKey("done")).Focus()

	f.h.AssertFocus(gifttest.ByKey("done"))
	if f.keyboardIsShowing() {
		t.Fatal("the field lost the focus and the keyboard stayed on the screen")
	}
}

// TestTheKeyboardIsNotBuiltWhenTheSwitchIsOff is the "costs nothing" half of
// the switch, stated as an assertion rather than as a benchmark: with the
// switch off, focusing a field must not even cause a rebuild, because the
// field does not publish the request that would trigger one.
func TestTheKeyboardIsNotBuiltWhenTheSwitchIsOff(t *testing.T) {
	f := newKeyboard(t, false)
	f.h.Settle()
	before := f.h.Diagnostics().Builds
	f.field().Focus()
	if got := f.h.Diagnostics().Builds - before; got != 0 {
		t.Fatalf("focusing a field with the kiosk switch off rebuilt %d scope(s); the request "+
			"is supposed to be gated before it reaches gift", got)
	}
}

// --- keyboard avoidance -------------------------------------------------------

// TestTheFocusedFieldIsNotBehindTheKeyboard is the avoidance requirement of
// section 19.
//
// The field is the last row of a form that is twice as tall as the window, so
// before the keyboard appears it is somewhere below the fold; focusing it
// reveals it, and the keyboard then lands on top of the place it was revealed
// to. Only a second reveal, against the reduced viewport, can put it back.
func TestTheFocusedFieldIsNotBehindTheKeyboard(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()

	field, kb := f.field().Bounds(), f.kb().Bounds()
	if !f.keyboardIsShowing() {
		t.Fatal("no keyboard, so there is nothing this test can show")
	}
	if field.Max.Y > kb.Min.Y {
		t.Fatalf("the field ends at y=%v and the keyboard starts at y=%v, so %v pixels of the "+
			"field are behind it", field.Max.Y, kb.Min.Y, field.Max.Y-kb.Min.Y)
	}
	if field.Min.Y < 0 {
		t.Fatalf("the field starts at y=%v, above the top of the window; the reveal overshot",
			field.Min.Y)
	}
}

// TestTheFieldStaysTypeableAfterTheKeyboardCoveredIt is the same property
// asserted through the feature instead of through the geometry: whatever the
// numbers say, a character tapped on the keyboard has to land in a field the
// user can see.
func TestTheFieldStaysTypeableAfterTheKeyboardCoveredIt(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	f.tapAll(t, "o", "k")

	if got := f.ed.Text(); got != "ok" {
		t.Fatalf("the document is %q, want %q", got, "ok")
	}
	f.field().AssertVisible()
	if _, ok := caretOp(f.h); !ok {
		t.Fatal("no caret is drawn, so the field is not showing itself as focused")
	}
	// The field's own bounds and not the caret's. The caret is emitted inside
	// the scroll container, so its rectangle is in the content space the
	// container translates, while the field's device bounds are in the space
	// the keyboard's are; comparing the two directly would be comparing
	// coordinates from two different spaces and would pass or fail for
	// reasons that have nothing to do with this test.
	if got, kb := f.field().Bounds(), f.kb().Bounds(); got.Max.Y > kb.Min.Y {
		t.Fatalf("the field ends at y=%v, below the top of the keyboard at y=%v",
			got.Max.Y, kb.Min.Y)
	}
}

// TestAFormThatFitsIsNotScrolledByTheKeyboard is the other side of the
// avoidance: a field that is already clear of the keyboard must not be moved.
//
// A reveal that scrolled unconditionally would jerk every form on every focus
// change, which is the kind of thing that only shows up on the hardware.
func TestAFormThatFitsIsNotScrolledByTheKeyboard(t *testing.T) {
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })
	font := loadTestFont(t)
	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.VScroll(ui.VStack(
				ui.TextField(ed).Font(font).FontSize(16).Frame(200, geom.Unbounded()).Key("field"),
				ui.Box().Frame(geom.Unbounded(), 40).Background(ui.RGB(30, 30, 30)),
			).Gap(8).Padding(8)).Key("form"),
			ui.OnScreenKeyboard().Font(font).Key("kb"),
		).Align(geom.Bottom).Frame(kbWindowW, kbWindowH),
		Size: geom.Sz(kbWindowW, kbWindowH),
		Font: font,
	})
	before := h.Find(gifttest.ByKey("field")).Bounds()
	h.Find(gifttest.ByKey("field")).Focus()
	after := h.Find(gifttest.ByKey("field")).Bounds()

	if before != after {
		t.Fatalf("the field moved from %v to %v although it was never behind the keyboard",
			before, after)
	}
}

// TestTheAdvertisedHeightIsTheHeightItTakes. [ui.OnScreenKeyboardHeight] is
// what an application uses to leave room under its last field, so a value that
// disagreed with the layout would be worse than no value at all.
func TestTheAdvertisedHeightIsTheHeightItTakes(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	if got, want := f.kb().LayoutBounds().Height(), ui.OnScreenKeyboardHeight(); got != want {
		t.Fatalf("the keyboard is %v tall and advertises %v", got, want)
	}
}

// TestTheKeyboardInAColumnShrinksTheFormInsteadOfCoveringIt is the composition
// [ui.KeyboardView] documents as the exact one, and it is tested because it is
// recommended.
//
// The field is the last row of the form and there is nothing below it, which
// is precisely the case the overlay arrangement cannot fix. Here it has to
// work, because the viewport really is smaller while the keyboard is up.
func TestTheKeyboardInAColumnShrinksTheFormInsteadOfCoveringIt(t *testing.T) {
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })
	font := loadTestFont(t)
	ed := ui.NewTextEditor("")

	rows := make([]gift.View, 0, 13)
	for i := range 12 {
		rows = append(rows, ui.Box().Frame(geom.Unbounded(), 40).
			Background(ui.RGB(30, 30, 30)).Key(string(rune('a'+i))))
	}
	rows = append(rows, ui.TextField(ed).Font(font).FontSize(16).
		Frame(200, geom.Unbounded()).Key("field"))

	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.VScroll(ui.VStack(rows...).Gap(8).Padding(8)).Key("form").Flex(1),
			ui.OnScreenKeyboard().Font(font).Key("kb"),
		).Frame(kbWindowW, kbWindowH),
		Size: geom.Sz(kbWindowW, kbWindowH),
		Font: font,
	})
	h.Find(gifttest.ByKey("field")).Focus()

	field := h.Find(gifttest.ByKey("field")).Bounds()
	kb := h.Find(gifttest.ByKey("kb")).Bounds()
	if kb.Height() != ui.OnScreenKeyboardHeight() {
		t.Fatalf("the keyboard is %v tall in a column, want %v", kb.Height(), ui.OnScreenKeyboardHeight())
	}
	if field.Max.Y > kb.Min.Y {
		t.Fatalf("the field ends at y=%v and the keyboard starts at y=%v, although the keyboard "+
			"took the space away from the form rather than covering it",
			field.Max.Y, kb.Min.Y)
	}
}

// --- cost ---------------------------------------------------------------------

// TestAShownKeyboardAllocatesNothingPerFrame is the frame path contract of
// section 11 applied to the widget with the most cells in the package.
//
// Fifty keys shaped and drawn every frame is where a per frame allocation
// would hide: an upper cased label, a slice of rectangles, a formatted string.
// The warmup is what fills the shaping cache; a miss allocates in harfbuzz and
// section 11 excludes that by name, so the measurement has to be of the warm
// state.
func TestAShownKeyboardAllocatesNothingPerFrame(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Focus()
	if !f.keyboardIsShowing() {
		t.Fatal("no keyboard, the measurement would be meaningless")
	}
	step := func() {
		f.h.Frame()
	}
	for range 32 {
		step()
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("a frame with the keyboard on screen allocated %v times per run, want 0", got)
	}
}

// TestAHiddenKeyboardAllocatesNothingPerFrame is the cheaper half, and it is
// separate because it is a different claim: a keyboard that is not showing
// must cost nothing at all, not merely nothing extra.
func TestAHiddenKeyboardAllocatesNothingPerFrame(t *testing.T) {
	f := newKeyboard(t, true)
	if f.keyboardIsShowing() {
		t.Fatal("the keyboard is showing without a focused field")
	}
	step := func() { f.h.Frame() }
	for range 32 {
		step()
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("a frame with the keyboard hidden allocated %v times per run, want 0", got)
	}
}

// BenchmarkOnScreenKeyboardFrame measures a whole frame — input phase, update
// and paint — with the keyboard on screen, next to the same frame without it.
// The difference is what the widget costs per frame.
func BenchmarkOnScreenKeyboardFrame(b *testing.B) {
	for _, tc := range []struct {
		name    string
		focused bool
	}{
		{"shown", true},
		{"hidden", false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			f := newKeyboard(b, true)
			if tc.focused {
				f.field().Focus()
			}
			for range 32 {
				f.h.Frame()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				f.h.Frame()
			}
		})
	}
}
