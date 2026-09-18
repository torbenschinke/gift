package gifttest_test

import (
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// TestPointerCaptureDoesNotActivate is pointer capture as a user experiences
// it: press the button, slide off, let go, nothing happens.
//
// It is the one interaction where the harness has to name a coordinate, and it
// names it for the right reason — "somewhere that is not the button" is a
// statement about geometry.
func TestPointerCaptureDoesNotActivate(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	plus := h.Find(gifttest.ByKey("plus"))
	plus.Press()
	plus.AssertPressed()

	h.MoveTo(pt(700, 500))
	// Still captured, so still the button's press — but no longer pressed,
	// because the pointer wandered off it.
	h.Find(gifttest.ByKey("plus")).AssertNotPressed()

	h.Release()
	h.Find(gifttest.ByKey("count")).AssertText("0")
	h.Find(gifttest.ByKey("plus")).AssertNotPressed()
}

// TestPointerCancelDoesNotActivate is the other half: a press the platform
// took away is not a click.
func TestPointerCancelDoesNotActivate(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	h.Find(gifttest.ByKey("plus")).Press().AssertPressed()
	h.CancelPointer()
	h.Find(gifttest.ByKey("count")).AssertText("0")
	h.Find(gifttest.ByKey("plus")).AssertNotPressed()
}

// TestTapActivatesWithoutHover is the touch rule of the project plan, section
// 7, as an assertion: a finger activates the button and leaves no hover state
// behind it.
func TestTapActivatesWithoutHover(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	plus := h.Find(gifttest.ByKey("plus"))
	plus.Tap()

	h.Find(gifttest.ByKey("count")).AssertText("1")
	h.Find(gifttest.ByKey("plus")).AssertNotHovered()

	// And nothing else acquired one either, which is the version of the
	// assertion that would catch a synthesised hover landing on the wrong
	// node.
	h.AssertNone(gifttest.Where("hovered", func(n gifttest.Node) bool {
		return n.Interaction().Hover
	}))
}

// TestMouseClickDoesHover is the control for the test above: the same button,
// clicked with a mouse, is hovered afterwards. Without it "no hover" could be
// passing because hover never works at all.
func TestMouseClickDoesHover(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	h.Find(gifttest.ByKey("plus")).Click()
	h.Find(gifttest.ByKey("plus")).AssertHovered()
}

// --- long press -------------------------------------------------------------

var holdType = gift.RegisterType("gifttest_test.Hold")

// holdView is a leaf that counts the long presses it received. gift's own
// widgets do not use [gift.EventLongPress] yet — there is nothing to open a
// context menu on — so the gesture is exercised through a view that does,
// which is also the smallest possible demonstration that the harness works
// against a view it has never heard of.
type holdView struct {
	key   string
	longs *int
	taps  *int
}

func (holdView) ViewType() gift.TypeID { return holdType }

func (v holdView) Build(*gift.BuildContext) gift.Element {
	n := &holdNode{v: v}
	return gift.Element{
		Key:        v.key,
		Label:      "hold me",
		Layouter:   n,
		Interactor: n,
		Focusable:  true,
	}
}

type holdNode struct{ v holdView }

func (n *holdNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(120, 60)
}

func (n *holdNode) HandleEvent(_ *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventLongPress:
		*n.v.longs++
		return true
	case gift.EventPointerUp:
		if e.Inside && !e.Dragged {
			*n.v.taps++
		}
		return true
	case gift.EventPointerDown:
		return true
	}
	return false
}

// TestLongPressFiresWithoutSleeping moves the injected clock instead of
// waiting. The gesture threshold is half a second and the test costs
// microseconds, which is the whole point of gift taking its timestamps from
// the caller.
func TestLongPressFiresWithoutSleeping(t *testing.T) {
	var longs, taps int
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(holdView{key: "hold", longs: &longs, taps: &taps}),
	})

	h.Find(gifttest.ByKey("hold")).LongPress()

	if longs != 1 {
		t.Errorf("the long press fired %d times, want 1", longs)
	}
	if h.Now() < gift.LongPressDelay {
		t.Errorf("the clock is at %v, which is less than the %v threshold; "+
			"the gesture cannot have been recognised honestly", h.Now(), gift.LongPressDelay)
	}
}

// TestShortTapDoesNotLongPress is the boundary: a tap that lifts before the
// threshold must not be a long press, or every tap would be one.
func TestShortTapDoesNotLongPress(t *testing.T) {
	var longs, taps int
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(holdView{key: "hold", longs: &longs, taps: &taps}),
	})

	h.Find(gifttest.ByKey("hold")).Tap()
	h.Advance(gift.LongPressDelay * 2)

	if longs != 0 {
		t.Errorf("a tap produced %d long press(es)", longs)
	}
	if taps != 1 {
		t.Errorf("the tap was counted %d times, want 1", taps)
	}
}

// TestDragEmitsAPath checks that a drag is a gesture and not a teleport: the
// intermediate moves happen, and the release is marked as dragged rather than
// as a tap.
func TestDragEmitsAPath(t *testing.T) {
	var longs, taps int
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(holdView{key: "hold", longs: &longs, taps: &taps}),
	})

	h.Find(gifttest.ByKey("hold")).DragTo(pt(700, 500))

	if taps != 0 {
		t.Errorf("a drag was counted as %d tap(s); a release that wandered further than "+
			"gift.DragSlop is not a tap", taps)
	}
}

// --- the escape hatch -------------------------------------------------------

// TestAtIsTheRawPoint covers the one selector that is a coordinate, and the
// reason it exists: two overlapping siblings, where the question genuinely is
// which one a click at this point reaches.
func TestAtIsTheRawPoint(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.Button(ui.Text("under"), nil).Key("under").Frame(200, 200),
			ui.Button(ui.Text("over"), nil).Key("over").Frame(100, 100),
		),
	})
	over := h.Find(gifttest.ByKey("over"))
	h.At(over.Center()).AssertKey("over")
}

// TestAtOnNothingFails is the failure side of the same method: a point that
// hits nothing says so with the point and the tree, rather than returning a
// zero node that fails an assertion further down.
func TestAtOnNothingFails(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{Root: counter})
		h.At(pt(799, 599))
	})
	wantContains(t, "At on empty space", r.all(), "hit no interactive node", "the tree was:")
}

// --- typing -----------------------------------------------------------------

// fieldView is the stand in for the text field gift does not have yet. It
// collects the characters it receives and handles the two editing keys, which
// is the smallest thing that can show the harness driving both channels.
//
// It is deliberately written here and not in gift: this work unit is the input
// foundation only, and a ui.TextField is a later one. That the harness can
// drive a view it has never heard of is the point of the package.
type fieldView struct {
	key  string
	text *string
}

var fieldType = gift.RegisterType("gifttest.test.Field")

func (fieldView) ViewType() gift.TypeID { return fieldType }

func (v fieldView) Build(*gift.BuildContext) gift.Element {
	n := &fieldNode{v: v}
	return gift.Element{Key: v.key, Label: "field", Layouter: n, Interactor: n, Focusable: true}
}

type fieldNode struct {
	v        fieldView
	repeats  int
	presses  int
	lastMods gift.Mods
}

func (n *fieldNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(200, 40)
}

func (n *fieldNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventRune:
		*n.v.text += string(e.Rune)
		return true
	case gift.EventKeyDown:
		n.presses++
		n.lastMods = e.Mods
		if e.Repeat {
			n.repeats++
		}
		if e.Key == gift.KeyBackspace {
			r := []rune(*n.v.text)
			if len(r) > 0 {
				*n.v.text = string(r[:len(r)-1])
			}
			return true
		}
		return false
	case gift.EventPointerDown:
		ctx.RequestFocus()
		return true
	}
	return false
}

// TestTypeTextReachesTheFocusedView is the harness method this package refused
// to implement until the rune channel existed. It would catch a TypeText that
// types into nothing, into the wrong node, or that mangles non ASCII text.
func TestTypeTextReachesTheFocusedView(t *testing.T) {
	var got string
	h := gifttest.New(t, gifttest.Options{View: ui.ZStack(fieldView{key: "f", text: &got})})
	h.Find(gifttest.ByKey("f")).Focus()

	const want = "Grüße, Ærø"
	h.TypeText(want)
	if got != want {
		t.Fatalf("the view received %q, want %q", got, want)
	}
	for _, r := range got {
		t.Logf("received %q = U+%04X", r, r)
	}
}

// TestTypedCharactersAreNotKeyPresses pins the boundary from the harness's
// side: TypeText drives the character channel and Key drives the physical one,
// and a harness that implemented one in terms of the other would let a test
// pass that a real keyboard fails.
func TestTypedCharactersAreNotKeyPresses(t *testing.T) {
	var got string
	h := gifttest.New(t, gifttest.Options{View: ui.ZStack(fieldView{key: "f", text: &got})})
	h.Find(gifttest.ByKey("f")).Focus()

	h.TypeText("ab")
	h.Key(gift.KeyBackspace)
	if got != "a" {
		t.Fatalf("after typing \"ab\" and pressing backspace the text is %q, want %q", got, "a")
	}

	// The reverse: the space *character* is not the space *bar*. A button
	// activates on the latter and must not on the former.
	var activations int
	h2 := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(ui.Button(ui.Text("ok"), func() { activations++ }).Key("b").Frame(80, 40)),
	})
	h2.Find(gifttest.ByKey("b")).Focus()
	h2.TypeText(" ")
	if activations != 0 {
		t.Fatalf("typing a space activated the button %d time(s)", activations)
	}
	h2.Key(gift.KeySpace)
	if activations != 1 {
		t.Fatalf("the space bar activated the button %d time(s), want 1", activations)
	}
}

// TestTypeTextRefusesNonPrintable pins the one thing the harness still will
// not fake. Characters gift can never receive from a platform must not be
// typeable, or a test would exercise input no keyboard produces.
func TestTypeTextRefusesNonPrintable(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		var got string
		h := gifttest.New(tb, gifttest.Options{View: ui.ZStack(fieldView{key: "f", text: &got})})
		h.Find(gifttest.ByKey("f")).Focus()
		h.TypeText("a\bb")
	})
	wantContains(t, "TypeText with a backspace character", r.all(),
		"printable", "gift.KeyBackspace", "Harness.Key")
}

// TestTypeTextWithoutFocusTypesIntoNothing is the honest failure mode: a test
// that forgot to focus gets an empty field rather than text that appeared in
// the last view that happened to exist.
func TestTypeTextWithoutFocusTypesIntoNothing(t *testing.T) {
	var got string
	h := gifttest.New(t, gifttest.Options{View: ui.ZStack(fieldView{key: "f", text: &got})})
	h.TypeText("hello")
	if got != "" {
		t.Fatalf("text arrived at an unfocused view: %q", got)
	}
	if n := h.Diagnostics().RunesTyped; n != 5 {
		t.Fatalf("RunesTyped is %d, want 5; the characters did reach gift, they had nowhere to go", n)
	}
}

// TestHeldKeyRepeatsOnTheInjectedClock is key repeat as an application test
// sees it, with no sleeping anywhere.
//
// It would catch a repeat that never starts, one that starts immediately, and
// one that keeps going after the release — the last being the defect that
// makes a device unusable rather than merely wrong.
func TestHeldKeyRepeatsOnTheInjectedClock(t *testing.T) {
	var got string
	h := gifttest.New(t, gifttest.Options{View: ui.ZStack(fieldView{key: "f", text: &got})})
	h.Find(gifttest.ByKey("f")).Focus()
	h.TypeText("abcdef")

	// The press itself deletes once, as a press does. Everything after this
	// point is the repeat and nothing else.
	h.KeyDown(gift.KeyBackspace)
	afterPress := got
	if afterPress != "abcde" {
		t.Fatalf("the press deleted %d characters, want 1; the text is %q", 6-len([]rune(got)), got)
	}

	// Nothing may repeat before the delay.
	h.AdvanceTicks(4, time.Second/60)
	if got != afterPress {
		t.Fatalf("a held key repeated after %v, before the %v delay; the text is %q",
			4*time.Second/60, gift.KeyRepeatDelay, got)
	}

	// Past the delay, at a frame cadence, the repeats arrive.
	h.AdvanceTicks(30, time.Second/60)
	if got == afterPress {
		t.Fatalf("holding backspace for %v past the delay deleted nothing", 30*time.Second/60)
	}
	afterRelease := got
	h.KeyUp(gift.KeyBackspace)
	h.AdvanceTicks(60, time.Second/60)
	if got != afterRelease {
		t.Fatalf("the repeat continued after the release: %q became %q", afterRelease, got)
	}
}

// TestFocusChangeStopsTheRepeat is the rule that keeps a held backspace from
// following the focus into the next field and eating it.
func TestFocusChangeStopsTheRepeat(t *testing.T) {
	var first, second string
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		fieldView{key: "a", text: &first},
		fieldView{key: "b", text: &second},
	)})
	h.Find(gifttest.ByKey("a")).Focus()
	h.TypeText("aaaa")
	h.Find(gifttest.ByKey("b")).Focus()
	h.TypeText("bbbb")

	// Hold backspace in the second field, then move the focus away while it
	// is still down.
	h.Find(gifttest.ByKey("b")).Focus()
	h.KeyDown(gift.KeyBackspace)
	h.AdvanceTicks(40, time.Second/60)
	h.Find(gifttest.ByKey("a")).Focus()
	before := first
	h.AdvanceTicks(60, time.Second/60)
	if first != before {
		t.Fatalf("the held key kept deleting after the focus moved: %q became %q", before, first)
	}
}

// TestTapAtLeavesTheMouseCursorWhereItWas is the clause the documentation of
// [gifttest.Harness.TapAt] now carries, and it is worth pinning because it is
// the one way TapAt differs from [gifttest.Harness.ClickAt] other than the
// pointer kind.
//
// A finger that has lifted is nowhere, so a tap must not move the cursor the
// mouse verbs track. The observable consequence: a mouse press taken before
// the tap is still remembered at its own point, and [gifttest.Harness.Release]
// — which releases "wherever the mouse currently is" — still lands on the
// button and activates it. If TapAt moved the cursor, that release would
// happen somewhere else and the button would not count.
func TestTapAtLeavesTheMouseCursorWhereItWas(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	plus := h.Find(gifttest.ByKey("plus"))
	plus.Press()

	// A tap somewhere far away, on the other pointer.
	h.TapAt(pt(700, 500))

	h.Release()
	h.Find(gifttest.ByKey("count")).AssertText("1")
}
