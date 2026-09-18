package ui_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// The tests of ui.TabBar, ui.NavigationStack and ui.Modal.
//
// Everything in this file is about one decision and its consequences: an
// inactive tab and a covered screen stay *mounted* and are taken out of the
// frame with gift.Element.Hidden. So the file is organised as the three
// promises that choice makes — the state survives, the frame does not pay for
// it, and the input does not reach it — plus the modal, whose whole content is
// the third of those in a harder form.

// --- fixtures ----------------------------------------------------------------

// navHarness is the options every test here shares. The font and the theme are
// pinned for the reason gifttest.Options documents: without them a test in
// this file depends on which other test in the package ran first.
func navHarness(t *testing.T, view gift.View) *gifttest.Harness {
	t.Helper()
	return gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		View:  view,
	})
}

// rows is a column of n labelled boxes, tall enough to be worth scrolling.
func rows(prefix string, n int) []gift.View {
	out := make([]gift.View, n)
	for i := range n {
		out[i] = ui.Text(prefix+strconv.Itoa(i)).
			Frame(geom.Unbounded(), 40).
			Key(strconv.Itoa(i))
	}
	return out
}

// --- the tab bar: what it shows ----------------------------------------------

// TestTheSelectedTabIsTheOnlyOneOnScreen is the first thing a user sees and is
// therefore the first thing tested: the other tabs are mounted, and they are
// not on the screen.
func TestTheSelectedTabIsTheOnlyOneOnScreen(t *testing.T) {
	h := navHarness(t, ui.TabBar(1, nil,
		ui.Tab("Home", ui.Symbol{}, ui.Text("home body")),
		ui.Tab("Settings", ui.Symbol{}, ui.Text("settings body")),
	))
	h.Find(gifttest.ByText("settings body")).AssertVisible()
	h.Find(gifttest.ByText("home body")).AssertNotVisible()
	// Mounted, not merely absent. The difference is the whole design.
	h.AssertExists(gifttest.ByText("home body"))
}

// TestAHiddenTabEmitsNoDrawingOperations is the paint side of the same
// sentence, measured rather than asserted about one node: gift has no paint
// culling, so the only thing that can keep an inactive tab out of the display
// list is the Hidden flag.
func TestAHiddenTabEmitsNoDrawingOperations(t *testing.T) {
	body := func(tint ui.Color) gift.View {
		return ui.VStack(ui.Box().Frame(100, 100).Background(tint)).Key("body")
	}
	red, blue := ui.RGB(255, 0, 0), ui.RGB(0, 0, 255)
	h := navHarness(t, ui.TabBar(0, nil,
		ui.Tab("A", ui.Symbol{}, body(red)),
		ui.Tab("B", ui.Symbol{}, body(blue)),
	))
	count := func(c ui.Color) int {
		n := 0
		for _, op := range h.Ops() {
			if op.Color == c {
				n++
			}
		}
		return n
	}
	if got := count(red); got != 1 {
		t.Fatalf("the selected tab emitted %d operations in its own colour, want 1", got)
	}
	if got := count(blue); got != 0 {
		t.Fatalf("the hidden tab emitted %d drawing operations. gift paints every mounted "+
			"node every frame unless something stops it, and that something is the only "+
			"reason an inactive tab may stay mounted at all", got)
	}
}

// TestATabItemOffersAtLeastAFingerSizedTarget is the hit target rule of the
// project plan, section 23 step 9, for the one control in this unit a finger
// meets first. It is checked at both densities, because the rule is about
// logical pixels and a density that changed them would be a defect.
func TestATabItemOffersAtLeastAFingerSizedTarget(t *testing.T) {
	for _, density := range []float64{1, 2} {
		t.Run("density "+strconv.FormatFloat(density, 'f', -1, 64), func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{
				Theme:   ui.LightTheme(),
				Font:    loadTestFont(t),
				Size:    geom.Sz(640, 480),
				Density: density,
				View: ui.TabBar(0, nil,
					ui.Tab("A", ui.Symbol{}, ui.Box()),
					ui.Tab("B", ui.Symbol{}, ui.Box()),
					ui.Tab("C", ui.Symbol{}, ui.Box()),
				),
			})
			for _, name := range []string{"A", "B", "C"} {
				b := h.Find(gifttest.ByKey(name).And(gifttest.ByType("ui.Button"))).Bounds()
				if b.Width() < ui.ControlHitTarget || b.Height() < ui.ControlHitTarget {
					t.Fatalf("tab %q is %vx%v logical pixels, below the %v floor",
						name, b.Width(), b.Height(), ui.ControlHitTarget)
				}
				// And the bar is fully live: an item shorter than the bar
				// would leave a dead strip along the bottom bezel.
				if b.Height() != ui.TabBarHeight {
					t.Fatalf("tab %q is %v tall, the bar is %v", name, b.Height(), ui.TabBarHeight)
				}
			}
		})
	}
}

// TestTappingATabReportsTheIndexAndChangesNothingByItself pins the one-way
// data flow: the bar is not stateful, and an application that declines to
// store the new index keeps the tab it had.
func TestTappingATabReportsTheIndexAndChangesNothingByItself(t *testing.T) {
	got := -1
	h := navHarness(t, ui.TabBar(0, func(i int) { got = i },
		ui.Tab("A", ui.Symbol{}, ui.Text("a body")),
		ui.Tab("B", ui.Symbol{}, ui.Text("b body")),
	))
	h.Find(gifttest.ByKey("B").And(gifttest.ByType("ui.Button"))).Tap()
	if got != 1 {
		t.Fatalf("the bar reported tab %d, want 1", got)
	}
	h.Find(gifttest.ByText("a body")).AssertVisible()
	h.Find(gifttest.ByText("b body")).AssertNotVisible()
}

// --- the tab bar: what it keeps ----------------------------------------------

// tabApp is a real two tab application with state on both sides: a scrolling
// list in the first tab and a text field in the second.
//
// The field's OnSubmit switches to the first tab, which is not decoration: it
// is the only way to change tabs *without* first moving the focus somewhere
// else, and "the focus is inside the tab at the moment it is hidden" is
// exactly the case that has to be tested. Tapping the bar would move the focus
// to the tab button before anything was hidden and would prove nothing.
func tabApp(t *testing.T) (*gifttest.Harness, *ui.TextEditor) {
	t.Helper()
	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("List", ui.Symbol{}, ui.VScroll(rows("row ", 40)...).Key("list")),
				ui.Tab("Form", ui.Symbol{},
					ui.TextField(ed).OnSubmit(func(string) { sel.Set(0) }).Key("field")),
			)
		},
	})
	return h, ed
}

// tapTab presses the bar item with the given title.
func tapTab(h *gifttest.Harness, title string) {
	h.Find(gifttest.ByKey(title).And(gifttest.ByType("ui.Button"))).Tap()
	h.Settle()
}

// TestAnInactiveTabKeepsItsScrollOffset is the payoff of keeping a tab
// mounted, and it is asserted on the observable thing rather than on the flag:
// scroll a list, leave, come back, and the list is where it was left.
//
// Unmounting the tab would reset it, because the offset is presentation state
// in the node payload and the node would be gone; see the project plan,
// section 5.
func TestAnInactiveTabKeepsItsScrollOffset(t *testing.T) {
	h, _ := tabApp(t)
	list := h.Find(gifttest.ByKey("list"))
	list.ScrollBy(300)
	h.Settle()
	want := list.ScrollOffset()
	if want < 200 {
		t.Fatalf("the list only scrolled to %v; the fixture is not tall enough to test this", want)
	}

	tapTab(h, "Form")
	tapTab(h, "List")

	h.Find(gifttest.ByKey("list")).AssertScrollOffset(want)
}

// TestAnInactiveTabKeepsTheTextInItsField is the same promise for the state a
// kiosk user would be angriest about losing.
//
// It is the weaker of the two and it is worth saying why, rather than letting
// it look like the stronger one. The text of a [ui.TextField] lives in a
// [ui.TextEditor] the *application* owns and hands to the view, so it survives
// an unmount as well and this test would pass under the unmounting design too.
// It is here because the brief of this work unit names it and because it is
// what a user would check; [TestAnInactiveTabKeepsItsScrollOffset] above is
// the one that discriminates, because a scroll offset is presentation state in
// the node payload and dies with the node.
func TestAnInactiveTabKeepsTheTextInItsField(t *testing.T) {
	h, ed := tabApp(t)
	tapTab(h, "Form")
	h.Find(gifttest.ByKey("field")).Tap()
	h.TypeText("half typed")
	h.Settle()

	tapTab(h, "List")
	tapTab(h, "Form")

	if got := ed.Text(); got != "half typed" {
		t.Fatalf("the field says %q after a round trip through the other tab, want %q",
			got, "half typed")
	}
}

// --- the tab bar: what it costs ----------------------------------------------

// TestAnIndeterminateProgressBarInAHiddenTabLetsTheDeviceSleep is the test the
// whole mounted-versus-unmounted argument turns on.
//
// ui.ProgressBar documents that an indeterminate bar holds the backend at full
// tick rate for as long as it is *mounted*, and it does so by re-arming its
// own repaint enrolment from its painter. Keeping an inactive tab mounted
// would therefore pin a kiosk at 60 Hz for ever — unless the hidden subtree is
// genuinely not painted, in which case the painter never runs, the enrolment
// expires, and the tree goes quiet.
//
// This asserts the quiet. Invert ui.Element.Hidden in App.paintNode and it
// fails, which is the point: it is a test of the mechanism and not of the
// prose.
func TestAnIndeterminateProgressBarInAHiddenTabLetsTheDeviceSleep(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 1)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("Quiet", ui.Symbol{}, ui.Text("nothing happens here")),
				ui.Tab("Busy", ui.Symbol{}, ui.ProgressBar(0).Indeterminate().Key("bar")),
			)
		},
	})

	// The frames are driven by hand rather than through Harness.Advance,
	// because the question is the value of NeedsPaint *between* the input
	// phase and the paint: Paint clears the flag as its last act, so a test
	// that asked afterwards would read false for an application that never
	// stops drawing.
	a := h.App()
	now := h.Now()
	tick := func() bool {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		if err := a.Update(h.Size()); err != nil {
			t.Fatal(err)
		}
		busy := a.NeedsPaint()
		a.Paint()
		return busy
	}

	// While the busy tab is on screen the application never sleeps. This is
	// the documented behaviour of ui.ProgressBar and is asserted here so that
	// the negative below cannot pass by the bar having quietly stopped
	// working.
	for i := range 20 {
		if !tick() {
			t.Fatalf("the visible indeterminate bar stopped asking to be repainted after %d frames", i)
		}
	}

	h.Find(gifttest.ByKey("Quiet").And(gifttest.ByType("ui.Button"))).Tap()
	h.Settle()
	now = h.Now()
	// The bar is still mounted. If it were not, this test would be about
	// unmounting and would prove nothing about the flag.
	h.AssertExists(gifttest.ByKey("bar"))

	idle := false
	for range 120 {
		if !tick() {
			idle = true
			break
		}
	}
	if !idle {
		t.Fatal("a hidden indeterminate progress bar is still holding the device awake. " +
			"It re-arms its repaint enrolment from its own painter, so this can only " +
			"happen if the hidden subtree is being painted after all")
	}
}

// TestSwitchingTabsSettlesAndDrawsNothingForEver is the "every animation ends"
// rule of the project plan, section 23 step 9, applied to navigation.
//
// A tab change in this implementation is not animated at all — see the report
// of this work unit — so what there is to assert is the stronger version: a
// tab change reaches a fixed point, in one frame, and then nothing asks to be
// drawn again.
func TestSwitchingTabsSettlesAndDrawsNothingForEver(t *testing.T) {
	h, _ := tabApp(t)
	tapTab(h, "Form")
	for range 30 {
		h.Advance(16 * time.Millisecond)
	}
	if h.App().NeedsPaint() {
		t.Fatal("something is still animating half a second after a tab change")
	}
}

// --- the tab bar: input ------------------------------------------------------

// TestAHiddenTabIsNotInTheTabOrder walks the whole focus order and refuses to
// find anything in the tab that is not on screen. A focus ring drawn where
// nothing is painted is the visible half of the defect; the invisible half is
// that every key press then disappears.
func TestAHiddenTabIsNotInTheTabOrder(t *testing.T) {
	h, _ := tabApp(t)
	// The field lives in the second tab, which is not the selected one.
	field := h.Find(gifttest.ByKey("field"))
	for i := range 12 {
		h.App().MoveFocus(true)
		h.Settle()
		f, ok := h.App().Focus()
		if !ok {
			// Not a cosmetic assertion. A hidden node that is still *in* the
			// order but refuses the focus when it is offered one makes the
			// traversal stall on it: every press picks the same node, the
			// focus is cleared, and the next press starts from nothing and
			// picks it again. Tab then does nothing at all, for ever.
			t.Fatalf("tab press %d left nothing focused; the focus order contains a node "+
				"that will not accept the focus, so the traversal is stuck on it", i+1)
		}
		if f == field.Ref() {
			t.Fatal("tab reached a text field inside a hidden tab")
		}
	}
}

// TestSwitchingTabsTakesTheFocusOutOfTheTabThatLeaves is the other half: the
// focus may already be inside the tab when it is hidden, and nothing unmounts
// to clean up after it.
func TestSwitchingTabsTakesTheFocusOutOfTheTabThatLeaves(t *testing.T) {
	h, _ := tabApp(t)
	tapTab(h, "Form")
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertFocused()

	// Enter, which the fixture wires to "go back to the first tab". The
	// field still holds the focus at the instant its layer is hidden.
	h.Key(gift.KeyEnter)
	h.Settle()
	h.AssertNoFocus()
	// And the field it left is still there, which is what distinguishes this
	// from the unmounting design.
	h.AssertExists(gifttest.ByKey("field"))
}

// TestSwitchingTabsUnderAHeldKeyStopsTheRepeat covers the one piece of input
// state that outlives a single event: gift synthesises key repeats into the
// focused node, and the focus has just been taken away from one.
func TestSwitchingTabsUnderAHeldKeyStopsTheRepeat(t *testing.T) {
	h, ed := tabApp(t)
	tapTab(h, "Form")
	h.Find(gifttest.ByKey("field")).Tap()
	h.TypeText("abcdef")
	h.Settle()

	// Hold backspace. The repeat is armed and several deletions happen.
	h.KeyDown(gift.KeyBackspace)
	h.Advance(gift.KeyRepeatDelay + 3*gift.KeyRepeatInterval)
	afterHold := ed.Text()
	if afterHold == "abcdef" {
		t.Fatal("holding backspace deleted nothing; the fixture is not exercising the repeat")
	}

	// Switch tabs with the key still physically down, without releasing it
	// and without touching the bar, which would move the focus first.
	tapTab(h, "List")
	h.Advance(10 * gift.KeyRepeatInterval)
	h.Settle()
	if got := ed.Text(); got != afterHold {
		t.Fatalf("the repeat kept deleting into a tab that is not on screen: %q became %q",
			afterHold, got)
	}
}

// TestSwitchingTabsTakesTheOnScreenKeyboardAway is the kiosk case named in the
// brief of this work unit. The keyboard is a process wide declaration made by
// the focused field; the field's tab is hidden without the field ever being
// told, so nothing would withdraw it.
func TestSwitchingTabsTakesTheOnScreenKeyboardAway(t *testing.T) {
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })

	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			// The keyboard sits *next to* the application rather than over
			// it, which is the arrangement ui.KeyboardView documents as the
			// right one when the thing behind it is not a scroll container.
			// It also keeps the tab bar reachable, which a keyboard laid
			// over the bottom of the window would not.
			return ui.VStack(
				ui.TabBar(ctx.Read(sel), sel.Set,
					ui.Tab("Form", ui.Symbol{}, ui.TextField(ed).
						// Enter switches tabs, so that the field still holds
						// the focus at the instant its layer is hidden.
						// Tapping the bar would move the focus to the tab
						// button first, the field would be told it lost the
						// focus in the ordinary way, and it would withdraw
						// the keyboard itself — which is a different code
						// path and not the one under test.
						OnSubmit(func(string) { sel.Set(1) }).
						Key("field")),
					ui.Tab("Other", ui.Symbol{}, ui.Text("nothing")),
				).Flex(1),
				ui.OnScreenKeyboard().Key("kb"),
			)
		},
	})
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	if !h.App().SoftKeyboardRequested() {
		t.Fatal("tapping the field did not ask for the on-screen keyboard; the fixture is wrong")
	}
	if b := h.Find(gifttest.ByKey("kb")).Bounds(); b.Height() == 0 {
		t.Fatal("the keyboard is not on screen; the fixture is wrong")
	}

	h.Key(gift.KeyEnter)
	h.Settle()
	if h.App().SoftKeyboardRequested() {
		t.Fatal("the on-screen keyboard is still up after the field's tab was hidden. " +
			"Nothing is focused, so every key it draws delivers a rune that is dropped, " +
			"and there is no interaction left that would take it away")
	}
	if b := h.Find(gifttest.ByKey("kb")).Bounds(); b.Height() != 0 {
		t.Fatalf("the keyboard still occupies %v", b)
	}
}

// --- the navigation stack ----------------------------------------------------

// stackApp is a navigation stack whose screen list lives where it belongs: in
// the application's own state.
func stackApp(t *testing.T, extra ...ui.ScreenSpec) (*gifttest.Harness, *ui.TextEditor) {
	t.Helper()
	ed := ui.NewTextEditor("")
	var pushFromField func()
	root := ui.Screen("Root", ui.VStack(
		// Submitting the field pushes a screen. It is the only way to push
		// without first moving the focus to whatever was tapped, which is
		// what TestPushingAScreenTakesTheFocusOffTheFieldUnderneath needs.
		ui.TextField(ed).OnSubmit(func(string) { pushFromField() }).Key("field"),
		ui.Text("root body").Key("root-body"),
	))
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			depth := ctx.State("depth", 1)
			pushFromField = func() { depth.Set(depth.Get() + 1) }
			screens := []ui.ScreenSpec{root}
			for i := range ctx.Read(depth) - 1 {
				if i < len(extra) {
					screens = append(screens, extra[i])
					continue
				}
				screens = append(screens, ui.Screen("Detail "+strconv.Itoa(i),
					ui.Text("detail body "+strconv.Itoa(i)).Key("detail-"+strconv.Itoa(i))))
			}
			return ui.VStack(
				ui.NavigationStack(func() { depth.Set(depth.Get() - 1) }, screens...).Flex(1),
				ui.Button(ui.Text("push"), func() { depth.Set(depth.Get() + 1) }).Key("push"),
			)
		},
	})
	return h, ed
}

// TestPushingAScreenShowsItOverTheOneBefore is the surface a user sees first.
func TestPushingAScreenShowsItOverTheOneBefore(t *testing.T) {
	h, _ := stackApp(t)
	h.AssertNone(gifttest.ByKey("back"))
	h.Find(gifttest.ByKey("root-body")).AssertVisible()

	h.Find(gifttest.ByKey("push")).Tap()
	h.Settle()

	h.Find(gifttest.ByKey("detail-0")).AssertVisible()
	h.Find(gifttest.ByKey("root-body")).AssertNotVisible()
	// The back affordance names where it goes, which is the whole reason it
	// carries the previous screen's title.
	h.Find(gifttest.ByKey("back")).AssertText("Back")
	h.AssertExists(gifttest.ByText("Root"))
}

// TestACoveredScreenIsNeitherPaintedNorTouchable is the navigation stack's
// version of the tab bar's two hidden-subtree promises, and the second half is
// the one that would otherwise be a silent defect: a button on the screen
// underneath is still exactly where it was, and a tap that lands on its pixels
// must not reach it.
func TestACoveredScreenIsNeitherPaintedNorTouchable(t *testing.T) {
	hit := 0
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			depth := ctx.State("depth", 1)
			screens := []ui.ScreenSpec{
				ui.Screen("Root", ui.VStack(
					ui.Button(ui.Text("underneath"), func() { hit++ }).Key("under"),
					ui.Spacer(),
				)),
			}
			if ctx.Read(depth) > 1 {
				screens = append(screens, ui.Screen("Detail",
					// Deliberately not covering the whole screen, so that the
					// tap below lands on bare layer and not on a button of
					// the pushed screen.
					ui.VStack(ui.Spacer(), ui.Text("detail").Key("detail"))))
			}
			return ui.VStack(
				ui.NavigationStack(nil, screens...).Flex(1),
				ui.Button(ui.Text("push"), func() { depth.Set(2) }).Key("push"),
			)
		},
	})
	under := h.Find(gifttest.ByKey("under"))
	at := center(under.Bounds())
	h.TapAt(at)
	h.Settle()
	if hit != 1 {
		t.Fatalf("the button under test was not reachable before the push: %d hits", hit)
	}

	h.Find(gifttest.ByKey("push")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("under")).AssertNotVisible()

	// The hit test, asked directly. This is a separate assertion from the tap
	// below and not a restatement of it: a covered button that still answers
	// the hit test would take the press, show itself pressed and only decline
	// to *activate*, which is a control lighting up under a screen that is
	// covering it.
	if got := h.At(at); !got.IsZero() && got.Ref() == under.Ref() {
		t.Fatal("the hit test returned a button inside a covered screen")
	}
	h.TapAt(at)
	h.Settle()
	if hit != 1 {
		t.Fatalf("a tap on the pixels of a covered screen reached it: %d hits", hit)
	}
}

// TestAButtonWhoseScreenIsHiddenUnderTheFingerDoesNotFire is the capture side
// of the same rule, and it is the case a hit test alone cannot cover: the
// press already happened while the screen was on top, so the button holds the
// pointer and will be told about the release wherever it lands.
//
// It is an ordinary kiosk sequence: a finger goes down on a button, a
// background task or a timer navigates away, the finger comes up.
func TestAButtonWhoseScreenIsHiddenUnderTheFingerDoesNotFire(t *testing.T) {
	hit := 0
	var push func()
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			depth := ctx.State("depth", 1)
			push = func() { depth.Set(2) }
			screens := []ui.ScreenSpec{
				ui.Screen("Root", ui.VStack(
					ui.Button(ui.Text("underneath"), func() { hit++ }).Key("under"),
					ui.Spacer(),
				)),
			}
			if ctx.Read(depth) > 1 {
				screens = append(screens, ui.Screen("Detail", ui.Text("detail")))
			}
			return ui.NavigationStack(nil, screens...)
		},
	})
	at := center(h.Find(gifttest.ByKey("under")).Bounds())
	h.PressAt(at)
	h.Find(gifttest.ByKey("under")).AssertPressed()

	// Navigate with the finger still down.
	push()
	h.Settle()

	h.ReleaseAt(at)
	h.Settle()
	if hit != 0 {
		t.Fatalf("a button on a screen that was covered while the finger was down still "+
			"fired: %d activations", hit)
	}
}

// center is the middle of a rectangle. gifttest has one of these and does not
// export it.
func center(r geom.Rect) geom.Point {
	return geom.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

// TestATextFieldKeepsItsTextWhileAScreenIsPushedOverIt is the state retention
// promise on the stack side, in the spelling the brief of this work unit asks
// for. The caveat of [TestAnInactiveTabKeepsTheTextInItsField] applies: the
// discriminating one is [TestACoveredScreenKeepsItsScrollOffset] below.
func TestATextFieldKeepsItsTextWhileAScreenIsPushedOverIt(t *testing.T) {
	h, ed := stackApp(t)
	h.Find(gifttest.ByKey("field")).Tap()
	h.TypeText("unsaved")
	h.Settle()

	h.Find(gifttest.ByKey("push")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("back")).Tap()
	h.Settle()

	if got := ed.Text(); got != "unsaved" {
		t.Fatalf("the field says %q after a push and a pop, want %q", got, "unsaved")
	}
}

// TestACoveredScreenKeepsItsScrollOffset is the state retention promise of the
// navigation stack in the currency that actually proves it: a scroll offset is
// presentation state in the gift node payload, so it dies with the node and
// survives exactly when the screen stays mounted.
func TestACoveredScreenKeepsItsScrollOffset(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			depth := ctx.State("depth", 1)
			screens := []ui.ScreenSpec{
				ui.Screen("Root", ui.VScroll(rows("row ", 40)...).Key("list")),
			}
			if ctx.Read(depth) > 1 {
				screens = append(screens, ui.Screen("Detail", ui.Text("detail")))
			}
			return ui.VStack(
				ui.NavigationStack(func() { depth.Set(1) }, screens...).Flex(1),
				ui.Button(ui.Text("push"), func() { depth.Set(2) }).Key("push"),
			)
		},
	})
	list := h.Find(gifttest.ByKey("list"))
	list.ScrollBy(300)
	h.Settle()
	want := list.ScrollOffset()
	if want < 200 {
		t.Fatalf("the list only scrolled to %v; the fixture is not tall enough", want)
	}

	h.Find(gifttest.ByKey("push")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("back")).Tap()
	h.Settle()

	h.Find(gifttest.ByKey("list")).AssertScrollOffset(want)
}

// TestPushingAScreenTakesTheFocusOffTheFieldUnderneath is the focus half. The
// covered screen is not unmounted, so gift's own unmount cleanup never runs.
func TestPushingAScreenTakesTheFocusOffTheFieldUnderneath(t *testing.T) {
	h, _ := stackApp(t)
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertFocused()

	h.Key(gift.KeyEnter)
	h.Settle()
	h.AssertExists(gifttest.ByKey("detail-0"))
	h.Find(gifttest.ByKey("field")).AssertNotFocused()
}

// TestTheBackButtonAsksTheApplicationToPopAndDoesNotPopByItself is the
// one-way data flow again, and it is the property that makes "this screen
// refuses to be left" expressible.
func TestTheBackButtonAsksTheApplicationToPopAndDoesNotPopByItself(t *testing.T) {
	asked := 0
	h := navHarness(t, ui.NavigationStack(func() { asked++ },
		ui.Screen("Root", ui.Text("root body")),
		ui.Screen("Detail", ui.Text("detail body")),
	))
	h.Find(gifttest.ByKey("back")).Tap()
	if asked != 1 {
		t.Fatalf("the back button asked %d times, want 1", asked)
	}
	// The view was built with two screens and still has two, because the
	// application under test declined to shorten its list.
	h.Find(gifttest.ByText("detail body")).AssertVisible()
}

// TestPoppingTheLastScreenLeavesTheRootInPlace is the boundary the brief
// names. There are two ways to ask for a pop and both have to stop.
func TestPoppingTheLastScreenLeavesTheRootInPlace(t *testing.T) {
	asked := 0
	h := navHarness(t, ui.VStack(
		ui.NavigationStack(func() { asked++ },
			ui.Screen("Root", ui.Button(ui.Text("on root"), nil).Key("target")),
		).Flex(1),
	))
	// There is no affordance at all on the root screen.
	h.AssertNone(gifttest.ByKey("back"))

	// And the key that would pop does nothing. The focus has to be inside the
	// stack for the key to arrive there, which is the documented limitation
	// of the escape path and is set up here on purpose.
	h.Find(gifttest.ByKey("target")).Tap()
	h.Settle()
	h.Key(gift.KeyEscape)
	h.Settle()
	if asked != 0 {
		t.Fatalf("escape on the root screen asked to pop %d times, want 0", asked)
	}
	h.Find(gifttest.ByText("on root")).AssertVisible()
}

// TestEscapeInsideAPushedScreenAsksToPopIt is the positive of the case above,
// so that the boundary test cannot pass by escape doing nothing anywhere.
func TestEscapeInsideAPushedScreenAsksToPopIt(t *testing.T) {
	asked := 0
	h := navHarness(t, ui.NavigationStack(func() { asked++ },
		ui.Screen("Root", ui.Text("root body")),
		ui.Screen("Detail", ui.Button(ui.Text("on detail"), nil).Key("target")),
	))
	h.Find(gifttest.ByKey("target")).Tap()
	h.Settle()
	h.Key(gift.KeyEscape)
	h.Settle()
	if asked != 1 {
		t.Fatalf("escape inside a pushed screen asked to pop %d times, want 1", asked)
	}
}

// --- the modal ---------------------------------------------------------------

// modalApp is a screen with a button on it and an alert that can be opened and
// closed.
func modalApp(t *testing.T, hits *int) *gifttest.Harness {
	t.Helper()
	ed := ui.NewTextEditor("")
	return gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			open := ctx.State("open", false)
			content := ui.VStack(
				// Submitting the field opens the alert, so that the field
				// still holds the focus at the instant the trap appears.
				// Tapping the "open" button would move the focus to that
				// button first and the field would already have lost it.
				ui.TextField(ed).OnSubmit(func(string) { open.Set(true) }).Key("field"),
				ui.Button(ui.Text("underneath"), func() { *hits++ }).Key("under"),
				ui.Spacer(),
				ui.Button(ui.Text("open"), func() { open.Set(true) }).Key("open"),
			)
			var modal gift.View
			if ctx.Read(open) {
				modal = ui.Alert("Careful", "Really?",
					ui.AlertCancel("Cancel", func() { open.Set(false) }),
				)
			}
			return ui.Modal(content, modal).Key("host")
		},
	})
}

// TestATapBesideAnOpenAlertDoesNotReachWhatIsUnderneath is the input trapping
// requirement, set up so that it cannot pass by accident: the button under
// test is nowhere near the alert's own pixels, so a modal that only covered
// its card would let the tap straight through.
func TestATapBesideAnOpenAlertDoesNotReachWhatIsUnderneath(t *testing.T) {
	hits := 0
	h := modalApp(t, &hits)
	under := h.Find(gifttest.ByKey("under"))
	at := center(under.Bounds())
	h.TapAt(at)
	h.Settle()
	if hits != 1 {
		t.Fatalf("the button was not reachable before the alert opened: %d hits", hits)
	}

	h.Find(gifttest.ByKey("open")).Tap()
	h.Settle()
	// The alert really is somewhere else. Without this the test would be
	// about a modal covering its own button, which is the easy half.
	card := h.Find(gifttest.ByType("ui.Alert")).Bounds()
	if card.Contains(at) {
		t.Fatalf("the alert at %v covers the button at %v; this test would prove nothing",
			card, at)
	}
	// And the button is still exactly where it was, and still painted.
	h.Find(gifttest.ByKey("under")).AssertVisible()

	h.TapAt(at)
	h.Settle()
	if hits != 1 {
		t.Fatalf("a tap beside the alert reached the button underneath: %d hits", hits)
	}
}

// TestTabDoesNotLeaveAnOpenAlert is the keyboard half of the same sentence. A
// scrim stops a finger and does nothing at all about the focus order.
func TestTabDoesNotLeaveAnOpenAlert(t *testing.T) {
	hits := 0
	h := modalApp(t, &hits)
	h.Find(gifttest.ByKey("open")).Tap()
	h.Settle()

	inside := map[gift.NodeRef]bool{}
	for _, n := range h.FindAll(gifttest.Under(gifttest.ByType("ui.Alert")).And(gifttest.Focusable())) {
		inside[n.Ref()] = true
	}
	if len(inside) == 0 {
		t.Fatal("the alert has no focusable node in it; this test would prove nothing")
	}
	for range 12 {
		h.App().MoveFocus(true)
		h.Settle()
		f, ok := h.App().Focus()
		if !ok {
			continue
		}
		if !inside[f] {
			t.Fatalf("tab left the alert and landed on %q",
				h.At(center(h.App().NodeDeviceBounds(f))).Text())
		}
	}
}

// TestOpeningAnAlertTakesTheFocusOffTheFieldBehindIt: the trap excludes the
// field, so the field must not keep the focus it had when the alert opened.
func TestOpeningAnAlertTakesTheFocusOffTheFieldBehindIt(t *testing.T) {
	hits := 0
	h := modalApp(t, &hits)
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertFocused()

	h.Key(gift.KeyEnter)
	h.Settle()
	h.AssertExists(gifttest.ByType("ui.Alert"))
	h.Find(gifttest.ByKey("field")).AssertNotFocused()
	// And the focus is nowhere rather than somewhere else in the content: the
	// trap clears it, so the first tab press lands on the alert's own button.
	h.AssertNoFocus()
}

// TestAClosedModalCostsNothing pins the shape of the nil case: no scrim node,
// nothing extra between the host and the content.
func TestAClosedModalCostsNothing(t *testing.T) {
	h := navHarness(t, ui.Modal(ui.Text("content"), nil).Key("host"))
	h.AssertNone(gifttest.ByType("ui.scrim"))
	h.AssertNone(gifttest.ByType("ui.layer"))
	h.Find(gifttest.ByText("content")).AssertVisible()
}

// TestTheContentUnderAModalIsStillPainted is the cost of the modal stated as a
// test, so that nobody later "optimises" it into the hidden treatment the tab
// bar uses. A scrim over an invisible application is a grey rectangle.
func TestTheContentUnderAModalIsStillPainted(t *testing.T) {
	h := navHarness(t, ui.Modal(
		ui.Box().Frame(40, 40).Background(ui.RGB(1, 2, 3)).Key("mark"),
		ui.Alert("Careful", "Really?"),
	))
	h.Find(gifttest.ByKey("mark")).AssertVisible()
	n := 0
	for _, op := range h.Ops() {
		if op.Color == ui.RGB(1, 2, 3) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the content under the modal emitted %d operations, want 1", n)
	}
}

// TestTheScrimDismissesOnlyWhenTheApplicationAsksForIt covers both halves of
// ModalView.OnDismiss: the tap is swallowed either way, and it only *means*
// something when a handler was given.
func TestTheScrimDismissesOnlyWhenTheApplicationAsksForIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire bool
		want int
	}{
		{name: "without a handler", wire: false, want: 0},
		{name: "with a handler", wire: true, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := 0
			m := ui.Modal(
				ui.Box().Frame(40, 40).Background(ui.RGB(9, 9, 9)),
				ui.Alert("Careful", "Really?"),
			)
			if tc.wire {
				m = m.OnDismiss(func() { got++ })
			}
			h := navHarness(t, m)
			scrim := h.Find(gifttest.ByType("ui.scrim"))
			card := h.Find(gifttest.ByType("ui.Alert")).Bounds()
			_ = scrim
			// A corner of the window, which the card does not reach.
			at := geom.Pt(10, 10)
			if card.Contains(at) {
				t.Fatalf("the card at %v covers the corner; pick another point", card)
			}
			// A press that travels away and lifts somewhere else is not a
			// tap and must not dismiss, whichever way the handler is wired.
			// It is the ordinary "I changed my mind" recovery and it is the
			// same rule ui.Button applies; see ButtonView.HandleEvent.
			_ = scrim
			dragFrom(h, at, center(card))
			h.Settle()
			if got != 0 {
				t.Fatalf("a press that was dragged onto the card dismissed the modal")
			}

			h.TapAt(at)
			h.Settle()
			if got != tc.want {
				t.Fatalf("the scrim reported %d dismissals, want %d", got, tc.want)
			}
		})
	}
}

// --- geometry ----------------------------------------------------------------

// TestATabsScreenFillsTheTabArea is the layout promise of the layer node. It
// is not cosmetic: a screen that shrink-wrapped its content would leave the
// window showing through around a tab's background, and a scroll container
// inside it would be as wide as its longest row.
func TestATabsScreenFillsTheTabArea(t *testing.T) {
	h := navHarness(t, ui.TabBar(0, nil,
		ui.Tab("One", ui.Symbol{}, ui.VStack(ui.Text("short")).Key("screen")),
		ui.Tab("Two", ui.Symbol{}, ui.Text("other")),
	))
	area := h.Find(gifttest.ByKey("content").And(gifttest.ByType("ui.ZStack"))).LayoutBounds()
	screen := h.Find(gifttest.ByKey("screen")).LayoutBounds()
	if screen.Width() != area.Width() || screen.Height() != area.Height() {
		t.Fatalf("the screen is %vx%v inside a tab area of %vx%v",
			screen.Width(), screen.Height(), area.Width(), area.Height())
	}
	// And the area is the window minus the bar, so nothing above got it
	// wrong either.
	if got, want := area.Height(), h.Size().H-ui.TabBarHeight-1; got != want {
		t.Fatalf("the tab area is %v tall, want %v (the window less the bar and its hairline)",
			got, want)
	}
}

// TestTheNavigationLayoutIsTheSameAtEveryDensity is the rule of the project
// plan, section 18: layout and pointer input stay logical at any density, and
// the scaling lives in one transform at the root of the display list. A
// container that did any arithmetic with the density would round twice.
func TestTheNavigationLayoutIsTheSameAtEveryDensity(t *testing.T) {
	view := func() gift.View {
		return ui.Modal(
			ui.TabBar(0, nil,
				ui.Tab("One", ui.Symbol{}, ui.NavigationStack(nil,
					ui.Screen("Root", ui.Text("root")),
					ui.Screen("Detail", ui.Text("detail")),
				)),
				ui.Tab("Two", ui.Symbol{}, ui.Text("two")),
			),
			ui.Alert("Careful", "Really?", ui.AlertAction("OK", nil)),
		)
	}
	bounds := func(density float64) map[string]geom.Rect {
		h := gifttest.New(t, gifttest.Options{
			Theme:   ui.LightTheme(),
			Font:    loadTestFont(t),
			Size:    geom.Sz(640, 480),
			Density: density,
			View:    view(),
		})
		out := map[string]geom.Rect{}
		for _, key := range []string{"bar", "content", "back", "modal"} {
			out[key] = h.First(gifttest.ByKey(key)).LayoutBounds()
		}
		out["alert"] = h.Find(gifttest.ByType("ui.Alert")).LayoutBounds()
		out["scrim"] = h.Find(gifttest.ByType("ui.scrim")).LayoutBounds()
		return out
	}
	one, two := bounds(1), bounds(2)
	for key, want := range one {
		if want == (geom.Rect{}) {
			t.Fatalf("%q has no bounds at density 1; the fixture does not contain it", key)
		}
		if got := two[key]; got != want {
			t.Fatalf("%q is %v at density 2 and %v at density 1; a logical rectangle must "+
				"not move with the density", key, got, want)
		}
	}
	// And the density really was in force, which is what stops this test from
	// passing because Options.Density was ignored.
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t),
		Size: geom.Sz(640, 480), Density: 2, View: view(),
	})
	if h.Density() != 2 {
		t.Fatalf("the harness ran at density %v", h.Density())
	}
}

// TestHidingAMemoisedScreenTakesTheFocusOutOfIt is the case that separates
// "the focus is taken away when the layer is hidden" from "the focus is taken
// away because the focused node happened to be rebuilt in the same pass".
//
// A screen inside a [gift.Memo] whose props did not change is not rebuilt at
// all — that is the rebuild boundary of the project plan, section 6 — so
// nothing in the subtree runs and nothing in it can notice. The only place
// left that can act is the build of the layer above it, which is what
// gift.Element.Hidden makes happen.
func TestHidingAMemoisedScreenTakesTheFocusOutOfIt(t *testing.T) {
	ed := ui.NewTextEditor("")
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("Form", ui.Symbol{},
					gift.Memo("form", 0, func(*gift.Context, int) gift.View {
						return ui.TextField(ed).
							OnSubmit(func(string) { sel.Set(1) }).
							Key("field")
					})),
				ui.Tab("Other", ui.Symbol{}, ui.Text("nothing")),
			)
		},
	})
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertFocused()

	before := h.Diagnostics().Builds
	h.Key(gift.KeyEnter)
	h.Settle()

	h.AssertNoFocus()
	// And the memo really did hold: if the field had been rebuilt, this test
	// would be the one above with extra steps.
	if got := h.Diagnostics().Builds - before; got > 1 {
		t.Fatalf("the tab switch ran %d builds; the memoised screen was rebuilt after all "+
			"and this test proves nothing about the layer", got)
	}
}

// TestADragOnTheScrimDoesNotScrollWhatEnclosesTheModal is the one thing
// ui.scrim's own HandleEvent does that its bounds do not already do.
//
// gift bubbles an unhandled event to the *ancestors* of the node it hit, and
// the content beneath a modal is a sibling and not an ancestor — so for the
// ordinary composition the hit test is the whole of the trapping and the
// return value of the handler changes nothing at all. It starts to matter the
// moment a scroll container is an ancestor of the modal host, which is the
// only shape this can be tested in and therefore the shape it is tested in.
// It is contrived and it is legal, and the alternative to testing it here is
// not testing it.
//
// The frame is load bearing and not decoration: a modal host measured with an
// unbounded axis has no viewport to fill, so its scrim would be zero sized and
// the test would pass for the wrong reason. See [ui.layer]'s layouter.
func TestADragOnTheScrimDoesNotScrollWhatEnclosesTheModal(t *testing.T) {
	h := navHarness(t, ui.VScroll(
		ui.ZStack(ui.Modal(
			ui.VStack(rows("row ", 6)...),
			ui.Alert("Careful", "Really?", ui.AlertAction("OK", nil)),
		)).Frame(640, 480),
		ui.Box().Frame(geom.Unbounded(), 1000).Background(ui.RGB(5, 5, 5)),
	).Key("outer"))
	outer := h.Find(gifttest.ByKey("outer"))
	outer.AssertScrollOffset(0)
	if b := h.Find(gifttest.ByType("ui.scrim")).Bounds(); b.Height() < 400 {
		t.Fatalf("the scrim is %v; the fixture did not give the modal a viewport", b)
	}

	// A wheel over the scrim, well away from the card.
	at := geom.Pt(20, 20)
	if h.Find(gifttest.ByType("ui.Alert")).Bounds().Contains(at) {
		t.Fatal("the card covers the corner; pick another point")
	}
	h.Wheel(at, geom.Pt(0, -240))
	h.Settle()
	if got := outer.ScrollOffset(); got != 0 {
		t.Fatalf("a wheel over the scrim scrolled the page around the modal to %v", got)
	}

	// And a drag, which is the finger version of the same gesture.
	h.Find(gifttest.ByType("ui.scrim")).Swipe(geom.Pt(0, -200))
	h.Settle()
	if got := outer.ScrollOffset(); got != 0 {
		t.Fatalf("a drag on the scrim scrolled the page around the modal to %v", got)
	}
}

// TestPoppingAScreenTakesTheOnScreenKeyboardAway is the unmount version of
// [TestSwitchingTabsTakesTheOnScreenKeyboardAway], and it is a different code
// path: the field is not hidden, it is destroyed, so the cleanup cannot be in
// the build that hid a layer. It is the one in gift's own unmount.
//
// The pop is driven from a posted closure and not from a key or a tap, which
// is what keeps the unmount path the subject: every input that could pop this
// stack moves the focus off the field first, and the cleanup would then be the
// blur's rather than the unmount's. Escape used to be the trigger here and is
// not available any more — with a keyboard up it dismisses the keyboard, see
// [TestEscapePutsTheOnScreenKeyboardAwayBeforeItPopsAScreen].
func TestPoppingAScreenTakesTheOnScreenKeyboardAway(t *testing.T) {
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, false) })

	ed := ui.NewTextEditor("")
	var pop func()
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
			pop = func() { depth.Set(1) }
			return ui.VStack(
				ui.NavigationStack(func() { depth.Set(1) }, screens...).Flex(1),
				ui.OnScreenKeyboard().Key("kb"),
			)
		},
	})
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	if !h.App().SoftKeyboardRequested() {
		t.Fatal("tapping the field did not ask for the on-screen keyboard; the fixture is wrong")
	}

	pop()
	h.Settle()
	h.AssertNone(gifttest.ByKey("field"))
	if h.App().SoftKeyboardRequested() {
		t.Fatal("the on-screen keyboard is still up after the field that asked for it was " +
			"unmounted. Nothing is focused, so every key it draws delivers a rune that is " +
			"dropped on the floor")
	}
	if b := h.Find(gifttest.ByKey("kb")).Bounds(); b.Height() != 0 {
		t.Fatalf("the keyboard still occupies %v", b)
	}
}

// dragFrom presses at from, walks the mouse to to in steps that cross
// [gift.DragSlop] gradually, and releases there. gifttest can drag from a node
// and not from a coordinate, and the coordinate is the subject here.
func dragFrom(h *gifttest.Harness, from, to geom.Point) {
	h.PressAt(from)
	const steps = 8
	for i := 1; i <= steps; i++ {
		f := float32(i) / steps
		h.MoveTo(geom.Pt(from.X+(to.X-from.X)*f, from.Y+(to.Y-from.Y)*f))
	}
	h.ReleaseAt(to)
}
