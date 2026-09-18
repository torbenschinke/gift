package ui_test

import (
	"testing"

	"github.com/worldiety/gift/font/inter"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// The tests of the four interaction defects a human found while driving the
// real kitchen sink binary through gift/auto: a keyboard that could not be put
// away, a scroll bar thumb a finger could not grab, a focus ring every tap
// left behind, and an escape key that reached nobody.

// --- defect 1: the keyboard can be put away ---------------------------------

// TestTheOnScreenKeyboardHasAKeyThatPutsItAway is the affordance a kiosk needs
// and the shipped keyboard did not have. Escape needs a keyboard nobody has
// plugged in, a tap outside needs somewhere to tap that is not covered, and
// the return key of a single line field submits rather than dismisses; the key
// drawn on the keyboard itself is the one route that always exists.
func TestTheOnScreenKeyboardHasAKeyThatPutsItAway(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Tap()
	f.h.Settle()
	if !f.keyboardIsShowing() {
		t.Fatal("tapping the field did not raise the keyboard; the fixture is wrong")
	}

	f.tap(t, "\u2304")
	f.h.Settle()

	if f.keyboardIsShowing() {
		t.Fatalf("the dismiss key left the keyboard up, %v tall", f.kb().LayoutBounds().Height())
	}
	if f.h.App().SoftKeyboardRequested() {
		t.Fatal("the dismiss key left the request standing, so the next build puts the " +
			"keyboard straight back up")
	}
	if _, ok := f.h.Focused(); ok {
		t.Fatal("the dismiss key left the field focused. The request is the focused node's " +
			"and only it ever withdraws one; a field that keeps the focus would never ask " +
			"again and the keyboard could not be brought back by tapping the same field")
	}
}

// TestTheKeyboardCanBeBroughtBackAfterItWasDismissed is the other half of the
// sentence above: dismissing must not be a one way door.
func TestTheKeyboardCanBeBroughtBackAfterItWasDismissed(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Tap()
	f.h.Settle()
	f.tap(t, "\u2304")
	f.h.Settle()

	f.field().Tap()
	f.h.Settle()
	if !f.keyboardIsShowing() {
		t.Fatal("tapping the field again did not bring the keyboard back")
	}
}

// TestATapNextToTheFieldPutsTheKeyboardAway is the second half of defect 1:
// the press lands on the scroll container of the form, which is a hit target
// and is not focusable, and gift now blurs on exactly that; see
// [gift.App.PointerDown].
func TestATapNextToTheFieldPutsTheKeyboardAway(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Tap()
	f.h.Settle()
	if !f.keyboardIsShowing() {
		t.Fatal("tapping the field did not raise the keyboard; the fixture is wrong")
	}

	// The top left corner of the window: inside the form's scroll container,
	// above the keyboard, on nothing focusable.
	f.h.TapAt(geom.Pt(4, 4))
	f.h.Settle()

	if f.keyboardIsShowing() {
		t.Fatal("a tap on the form next to the field left the keyboard up. On a kiosk " +
			"that is a keyboard covering half the screen that nothing can put away")
	}
}

// TestEscapePutsTheOnScreenKeyboardAwayBeforeItPopsAScreen states the
// precedence of the escape key, which is the one thing about this fix that is
// a choice rather than a repair: a dismissible surface on the screen takes the
// dismissal key, and the screen underneath gets the second press.
func TestEscapePutsTheOnScreenKeyboardAwayBeforeItPopsAScreen(t *testing.T) {
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })

	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			depth := ctx.State("depth", 2)
			screens := []ui.ScreenSpec{ui.Screen("Root", ui.Text("root"))}
			if ctx.Read(depth) > 1 {
				screens = append(screens, ui.Screen("Detail", ui.TextField(ed).Key("field")))
			}
			return ui.VStack(
				ui.NavigationStack(func() { depth.Set(1) }, screens...).Flex(1),
				ui.OnScreenKeyboard().Key("kb"),
			)
		},
	})
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	if !h.App().SoftKeyboardRequested() {
		t.Fatal("tapping the field did not ask for the keyboard; the fixture is wrong")
	}

	h.Key(gift.KeyEscape)
	h.Settle()
	if h.App().SoftKeyboardRequested() {
		t.Fatal("escape did not put the keyboard away")
	}
	h.AssertExists(gifttest.ByKey("field"))

	h.Key(gift.KeyEscape)
	h.Settle()
	h.AssertNone(gifttest.ByKey("field"))
}

// TestTheDismissKeyIsOnBothPagesOfTheKeyboard: the symbol page is reachable in
// one tap from the letter page and a user who went there must not be stranded.
func TestTheDismissKeyIsOnBothPagesOfTheKeyboard(t *testing.T) {
	f := newKeyboard(t, true)
	f.field().Tap()
	f.h.Settle()

	f.tap(t, "?123")
	if _, ok := ui.KeyRectForTest("@"); !ok {
		t.Fatal("the symbol page is not showing; the fixture is wrong")
	}
	if _, ok := ui.KeyRectForTest("\u2304"); !ok {
		t.Fatal("no dismiss key on the symbol page")
	}
	f.tap(t, "\u2304")
	f.h.Settle()
	if f.keyboardIsShowing() {
		t.Fatal("the dismiss key of the symbol page did not put the keyboard away")
	}
}

// --- defect 3: the focus ring is the keyboard's ------------------------------

// ringFixture is a button alone in a window, with a box behind it to click on.
type ringFixture struct {
	h *gifttest.Harness
}

func newRing(t *testing.T) *ringFixture {
	t.Helper()
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(ui.Box(), ui.VStack(
			ui.Button(ui.Text("Press").Font(loadTestFont(t)), func() {}).Key("b"),
		).Padding(20)),
		Size:  geom.Sz(300, 160),
		Font:  loadTestFont(t),
		Theme: ui.LightTheme(),
	})
	return &ringFixture{h: h}
}

// rings counts the accent coloured stroked rectangles of the frame, which is
// what a focus ring is and what nothing else in this fixture draws.
func (f *ringFixture) rings() int {
	accent := ui.CurrentTheme().Color(ui.ColorAccent)
	n := 0
	for _, op := range f.h.Ops() {
		if op.Kind == render.OpStrokeRoundRect && op.Color == render.Color(accent) {
			n++
		}
	}
	return n
}

// TestATapOnAButtonLeavesNoFocusRing is defect 3: on a touchscreen every
// control takes the focus on [gift.EventPointerDown], so a ring tied to the
// focus alone is a ring on everything the user has ever touched.
func TestATapOnAButtonLeavesNoFocusRing(t *testing.T) {
	f := newRing(t)
	f.h.Find(gifttest.ByKey("b")).Tap()
	f.h.Settle()

	if _, ok := f.h.Focused(); !ok {
		t.Fatal("the tap did not take the focus. It must: the space bar has to reach the " +
			"button that was last touched")
	}
	if got := f.rings(); got != 0 {
		t.Fatalf("a tap left %d focus ring(s) on the button", got)
	}
}

// TestTabOnAButtonDrawsTheFocusRing is the half that must keep working: the
// framework stays fully keyboard operable, and a keyboard user has to be able
// to see where they are.
func TestTabOnAButtonDrawsTheFocusRing(t *testing.T) {
	f := newRing(t)
	f.h.Tab()
	f.h.Settle()

	if _, ok := f.h.Focused(); !ok {
		t.Fatal("tab did not reach the button; the fixture is wrong")
	}
	if got := f.rings(); got != 1 {
		t.Fatalf("the button keyboard focus is on draws %d accent rings, want 1", got)
	}
}

// TestTouchingAButtonTheKeyboardHadReachedTakesItsRingAway is the transition
// the painter cannot see on its own: the focus does not move, only where it
// came from changes.
func TestTouchingAButtonTheKeyboardHadReachedTakesItsRingAway(t *testing.T) {
	f := newRing(t)
	f.h.Tab()
	f.h.Settle()
	if got := f.rings(); got != 1 {
		t.Fatalf("tab drew %d rings, want 1; the fixture is wrong", got)
	}

	f.h.Find(gifttest.ByKey("b")).Tap()
	f.h.Settle()
	if got := f.rings(); got != 0 {
		t.Fatalf("touching the button the keyboard had reached left %d ring(s)", got)
	}
}

// TestTheFocusRingIsNotTheButtonsBorder is the second judgement of defect 3:
// the ring and the pressed state meant different things and differed only by a
// fill, so the ring is a second contour inside the box rather than the same
// two pixels in the same place. See [ui.focusRingGap].
func TestTheFocusRingIsNotTheButtonsBorder(t *testing.T) {
	f := newRing(t)
	f.h.Tab()
	f.h.Settle()

	b := f.h.Find(gifttest.ByKey("b")).Bounds()
	accent := ui.CurrentTheme().Color(ui.ColorAccent)
	for _, op := range f.h.Ops() {
		if op.Kind != render.OpStrokeRoundRect || op.Color != render.Color(accent) {
			continue
		}
		if op.Bounds == b {
			t.Fatalf("the focus ring is stroked on the button's own bounds %v, which is "+
				"where its border is; a focused control is then indistinguishable from "+
				"one with an accent border and from a pressed one", b)
		}
		if !b.Contains(op.Bounds.Min) || !b.Contains(op.Bounds.Max) {
			t.Fatalf("the focus ring at %v leaves the button's bounds %v, where the first "+
				"clip of a list, a tab bar or a scroll container cuts it off", op.Bounds, b)
		}
		return
	}
	t.Fatal("no focus ring at all after tab")
}

// --- defect 4: escape with nothing focused ----------------------------------

// TestEscapePopsANavigationStackWithNothingFocused is defect 4. The demo told
// the user to press escape and nothing happened, because keys go to the
// focused node and a touch panel focuses nothing.
func TestEscapePopsANavigationStackWithNothingFocused(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			depth := ctx.State("depth", 2)
			screens := []ui.ScreenSpec{ui.Screen("Root", ui.Text("root").Key("root"))}
			if ctx.Read(depth) > 1 {
				screens = append(screens, ui.Screen("Deeper", ui.Text("deep").Key("deep")))
			}
			return ui.NavigationStack(func() { depth.Set(1) }, screens...)
		},
	})
	h.AssertNoFocus()
	h.AssertExists(gifttest.ByKey("deep"))

	h.Key(gift.KeyEscape)
	h.Settle()

	h.AssertNone(gifttest.ByKey("deep"))
	h.AssertExists(gifttest.ByKey("root"))
}

// TestEscapeWithNothingFocusedStopsAtTheRootScreen: the fallback delivers the
// key, it does not change what the handler does with it. Popping the root is
// impossible and stays impossible.
func TestEscapeWithNothingFocusedStopsAtTheRootScreen(t *testing.T) {
	pops := 0
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		View: ui.NavigationStack(func() { pops++ },
			ui.Screen("Root", ui.Text("root").Key("root"))),
	})
	h.AssertNoFocus()

	h.Key(gift.KeyEscape)
	h.Settle()

	if pops != 0 {
		t.Fatalf("escape popped the root screen %d time(s)", pops)
	}
	h.AssertExists(gifttest.ByKey("root"))
}

// TestTabStillMovesTheFocusPastAnUnfocusedNavigationStack guards the one thing
// the fallback could plausibly break: the tab key has to reach
// [gift.App.MoveFocus] while nothing is focused, or a keyboard user can never
// get started. The stack's interactor answers only for escape, which is what
// makes that work.
func TestTabStillMovesTheFocusPastAnUnfocusedNavigationStack(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		View: ui.NavigationStack(nil,
			ui.Screen("Root", ui.Button(ui.Text("go"), func() {}).Key("go"))),
	})
	h.AssertNoFocus()

	h.Tab()
	h.Settle()

	got, ok := h.Focused()
	if !ok || got.Key() != "go" {
		t.Fatalf("tab reached %v, want the button; the key fallback swallowed it.\n%s",
			got, h.Dump())
	}
}

// --- what the two states look like ------------------------------------------

// focusRingScene is a button and a text field side by side, with a box behind
// them for a tap to land on.
func focusRingScene(t *testing.T) *gifttest.Harness {
	t.Helper()
	ed := ui.NewTextEditor("name")
	return gifttest.New(t, gifttest.Options{
		View: ui.Window(ui.VStack(
			ui.Button(ui.Text("Save"), func() {}).Key("save"),
			ui.TextField(ed).Frame(180, geom.Unbounded()).Key("field"),
		).Gap(12).Padding(16)),
		Size:  geom.Sz(260, 140),
		Theme: ui.LightTheme(),
		Font:  ui.MustFont(ui.FontQuery{Family: inter.Family}),
	})
}

// TestTheFocusRingLooksTheWayItLooks is the picture half of defect 3, and it
// is two pictures because the whole point is that the two states differ: a
// control the keyboard reached carries a ring inside its border, and one a
// finger tapped carries none.
//
// A single golden would have passed throughout the defect. The reporter's
// evidence was that four states looked pixel-identical, which is a statement
// about a *pair* of frames and not about either one.
func TestTheFocusRingLooksTheWayItLooks(t *testing.T) {
	h := focusRingScene(t)
	h.Tab()
	h.Settle()
	h.Warm()
	h.AssertGolden("focus-ring-from-the-keyboard")

	h2 := focusRingScene(t)
	h2.Find(gifttest.ByKey("save")).Tap()
	h2.Settle()
	h2.Warm()
	h2.AssertGolden("focus-ring-from-a-tap")
}
