package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// What a hidden subtree costs, measured.
//
// The claim under test is one sentence of [ui.TabBarView]: an inactive tab
// "costs nothing per frame". Review gate 13 showed that the paint side of
// gift.Element.Hidden — which is all the flag used to be — is not enough for
// it, because three mechanisms in this project keep the application awake from
// *outside* the painter: an EventContext.Animate window, a kinetic fling and a
// scroll indicator linger. Two of the three are re-armed from a *layouter*,
// and a hidden subtree is still laid out.
//
// Every test here is a count of frames that asked to be drawn, and every one
// of them carries its control case in the same function. A measurement of zero
// is worthless without the matching measurement of "and here it is not zero",
// because the cheapest way to make a frame counter read zero is to break the
// fixture.

// afterTheTransition lets the movement a tab switch now performs finish, so
// that a measurement of "what does a hidden subtree cost per frame" is not
// really a measurement of "what does a transition cost while it runs".
//
// The two are different questions and both have a right answer. A transition
// costs a full paint of the outgoing subtree for [ui.ControlAnimation] — see
// [gift.TransitionSpec], which is where the bargain is written down — and a
// hidden subtree costs nothing per frame once it is over. Every test in this
// file is about the second number, and
// TestTheTransitionOfATabSwitchEndsAndTheApplicationGoesIdle next door is
// about the first one and about the fact that it is bounded at all.
func afterTheTransition(h *gifttest.Harness) {
	h.Advance(ui.ControlAnimation + 32*time.Millisecond)
}

// --- the caret, which is the expensive one -----------------------------------

// TestHidingATabWithAFocusedFieldStopsTheCaretBlink is the reviewer's
// measurement, kept.
//
// ui.TextField enrols itself for repaints for [ui.CaretBlinkWindow] — ten
// seconds, the longest deadline in the project by two orders of magnitude —
// and cancels the enrolment from its gift.EventFocusLost handler. gift used to
// *drop* that event when the focus changed during a build, which is exactly
// what hiding the field's tab does, so the enrolment stood for its whole
// window: 100 of 120 sampled ticks at 60 Hz, drawing no caret, on a kiosk that
// is supposed to be asleep.
//
// The two control measurements are the test. "The focused field is busy" says
// the enrolment exists at all; "an ordinary blur costs nothing" says the
// handler works when it is called, so the only thing the third measurement can
// be about is whether it was called.
func TestHidingATabWithAFocusedFieldStopsTheCaretBlink(t *testing.T) {
	h, _ := tabApp(t)
	tapTab(h, "Form")
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertFocused()

	if busy := busyTicks(h, 20); busy != 20 {
		t.Fatalf("a focused text field asked for %d of 20 frames. The caret is not blinking, "+
			"so there is no enrolment here to leak and nothing below measures anything", busy)
	}

	// Control: an ordinary blur, with the field's own handler running in the
	// ordinary way. This is the number the hidden case has to match.
	h.App().MoveFocus(true)
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertNotFocused()
	if busy := busyTicks(h, 120); busy != 0 {
		t.Fatalf("an ordinary blur left the application asking for %d of 120 frames; "+
			"ui.TextField's own EventFocusLost handler is broken and this test cannot "+
			"tell anything apart", busy)
	}

	// And now the case. Enter switches tabs, which is the only way to hide
	// the field while it still holds the focus: tapping the bar would move
	// the focus to the tab button first and would exercise the control case
	// above a second time.
	h.Find(gifttest.ByKey("field")).Tap()
	h.Settle()
	h.Find(gifttest.ByKey("field")).AssertFocused()
	h.Key(gift.KeyEnter)
	h.Settle()
	h.AssertNoFocus()
	h.AssertExists(gifttest.ByKey("field"))
	afterTheTransition(h)

	if busy := busyTicks(h, 120); busy != 0 {
		t.Fatalf("hiding the tab of a focused text field left the application asking for "+
			"%d of 120 frames — ten seconds of full rate wakeups at 60 Hz, drawing no "+
			"caret. The field was never told it lost the focus, so it never called "+
			"Animate(0). An ordinary blur of the same field costs 0", busy)
	}
}

// TestHidingATabTellsTheFieldInItThatItLostTheFocus is the same defect seen
// from the other end, and it is here because the frame count above would also
// go to zero if somebody "fixed" it by cancelling the enrolment in the core
// and leaving the handler uncalled.
//
// ui.TextField's EventFocusLost arm does three things, and the enrolment is
// only one of them. This asserts a second: a selection drag in progress is
// ended. A field hidden mid-drag that kept dragNone unset would resume
// selecting from the old anchor the next time a move reached it.
func TestHidingATabTellsTheFieldInItThatItLostTheFocus(t *testing.T) {
	ed := ui.NewTextEditor("abcdefghij")
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("Form", ui.Symbol{}, ui.TextField(ed).
					OnSubmit(func(string) { sel.Set(1) }).
					Frame(300, geom.Unbounded()).
					Key("field")),
				ui.Tab("Other", ui.Symbol{}, ui.Text("nothing")),
			)
		},
	})
	field := h.Find(gifttest.ByKey("field"))
	b := field.Bounds()
	y := b.Min.Y + b.Height()/2

	// Press inside the text and drag a few characters: the field takes the
	// capture and is selecting.
	h.PressAt(geom.Pt(b.Min.X+4, y))
	for i := 1; i <= 6; i++ {
		h.MoveTo(geom.Pt(b.Min.X+4+float32(i)*6, y))
	}
	h.Settle()
	if !ed.HasSelection() {
		t.Fatal("the drag selected nothing; the fixture is not exercising a selection drag")
	}

	// Hide the tab with the finger still down, without releasing.
	h.Key(gift.KeyEnter)
	h.Settle()
	h.AssertNoFocus()

	// Bring the tab back and move the mouse across the field with no button
	// held. If the field still believed it was dragging, this would extend
	// the selection from the old anchor.
	h.ReleaseAt(geom.Pt(b.Min.X+40, y))
	h.Settle()
	before := selectionOf(ed)
	h.MoveTo(geom.Pt(b.Min.X+120, y))
	h.Settle()
	if got := selectionOf(ed); got != before {
		t.Fatalf("a mouse move with no button held changed the selection from %v to %v. "+
			"The field's drag mode survived its tab being hidden, which is the second of "+
			"the three things its EventFocusLost arm does", before, got)
	}
}

// quiesce drives frames until nothing asks for another one, so that a
// measurement below starts from a resting application rather than from the
// tail of the control case above it. The bound is a failure and not a return:
// an application that never settles makes every count in this file
// meaningless, and saying so is better than measuring the wrong thing.
func quiesce(t *testing.T, h *gifttest.Harness) {
	t.Helper()
	for range 1200 {
		if busyTicks(h, 1) == 0 {
			return
		}
	}
	t.Fatal("the application never stopped asking for frames; nothing here can be measured")
}

// selectionOf is the selection as a comparable pair.
func selectionOf(ed *ui.TextEditor) [2]int {
	a, b := ed.Selection()
	return [2]int{a, b}
}

// --- the two controls that animate from their layouter -----------------------

// TestAToggleFlippedWhileHiddenDoesNotAnimate is the finding that showed the
// paint side of the flag is not the whole of it.
//
// ui.Toggle animates its knob by calling gift.EventContext.Animate from
// syncControl, which runs in its *layouter*. A hidden subtree is still laid
// out — deliberately; see gift.Element.Hidden — so a state write into an
// inactive tab used to enrol the toggle afresh on every pass and hold the
// device at full rate for the whole control animation. Measured before: 12 of
// 60 frames. ui.SegmentedControl is the second caller of syncControl and gets
// the same treatment; the subtest covers both.
func TestAControlChangedWhileHiddenDoesNotAnimate(t *testing.T) {
	for _, tc := range []struct {
		name string
		// view builds the control for a given state value.
		view func(on *gift.State[bool]) gift.View
	}{
		{"a toggle", func(on *gift.State[bool]) gift.View {
			return ui.Toggle(on.Get(), on.Set).Key("control")
		}},
		{"a segmented control", func(on *gift.State[bool]) gift.View {
			sel := 0
			if on.Get() {
				sel = 1
			}
			return ui.SegmentedControl(sel, []string{"A", "B"}, func(int) {}).Key("control")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var on *gift.State[bool]
			var sel *gift.State[int]
			h := gifttest.New(t, gifttest.Options{
				Theme: ui.LightTheme(),
				Font:  loadTestFont(t),
				Size:  geom.Sz(640, 480),
				Root: func(ctx *gift.Context) gift.View {
					sel = ctx.State("tab", 0)
					on = ctx.State("on", false)
					ctx.Read(on)
					return ui.TabBar(ctx.Read(sel), sel.Set,
						ui.Tab("A", ui.Symbol{}, tc.view(on)),
						ui.Tab("B", ui.Symbol{}, ui.Text("nothing")),
					)
				},
			})

			// Control: the same change while the tab is on screen animates,
			// which is what the widget is for and what makes the zero below
			// mean something.
			on.Set(true)
			if busy := busyTicks(h, 60); busy == 0 {
				t.Fatal("the visible control did not animate at all; the fixture cannot " +
					"show the difference it is about to assert")
			}
			on.Set(false)
			h.Settle()
			quiesce(t, h)

			// And now hidden. A background task flipping a control in a tab
			// nobody is looking at.
			sel.Set(1)
			h.Settle()
			afterTheTransition(h)
			on.Set(true)
			// One frame, not zero, and the one is not the animation. It is
			// not true that any state write costs a frame: a write nobody
			// reads costs zero, measured. This one is read by the control in
			// the hidden tab, and a hidden subtree is still built and still
			// laid out — deliberately, see [gift.Element.Hidden] — and a
			// relayout marks a repaint. So the one frame is the rebuild, and
			// it is the cost the godoc of TabBarView names as the one a
			// hidden tab does not avoid.
			//
			// What is forbidden is the *sequence* of frames an animation
			// produces — twelve of sixty, measured, for both of these
			// controls before the fix. If this ever reads two, the question
			// is whether a second mechanism started re-arming, not whether
			// the tolerance should be raised.
			if busy := busyTicks(h, 60); busy > 1 {
				t.Fatalf("changing a control inside a hidden tab asked for %d of 60 frames. "+
					"Its animation is re-armed from its layouter and a hidden subtree is "+
					"still laid out, so nothing about not painting it stops this", busy)
			}
			// The control is still mounted, which is what distinguishes this
			// from a test about unmounting.
			h.AssertExists(gifttest.ByKey("control"))
		})
	}
}

// --- the fling ---------------------------------------------------------------

// TestHidingATabStopsAFlingInItAndLeavesTheOffsetAlone is the promise of
// [ui.NavigationStackView] that a popped screen comes back "with the list
// still where the user left it".
//
// It was false by a thousand pixels. A fling in flight when its layer was
// hidden kept ticking, because App.tickScrolls runs from the input phase and
// knows nothing about paint: measured at 140 of 600 frames, 2.24 seconds, and
// an offset that travelled from 400 to 1395 document units. So the list came
// back somewhere the user had never been, and the device stayed awake to get
// it there.
func TestHidingATabStopsAFlingInItAndLeavesTheOffsetAlone(t *testing.T) {
	var sel *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel = ctx.State("tab", 0)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("List", ui.Symbol{}, ui.VScroll(rows("row ", 60)...).Key("list")),
				ui.Tab("Other", ui.Symbol{}, ui.Text("nothing")),
			)
		},
	})
	// 160 pixels in 80 ms is 2000 px/s, which the default friction leaves
	// several hundred units of travel in; the same figures as
	// TestFlingDecaysAndStops, so this is a fling the scroll tests recognise.
	throw := func() {
		h.Find(gifttest.ByKey("0")).Fling(geom.Pt(0, -160), 80*time.Millisecond)
	}
	list := func() gifttest.Node { return h.Find(gifttest.ByKey("list")) }

	// Control: an undisturbed fling keeps the application awake and travels.
	throw()
	if info := list().ScrollInfo(); !info.Flinging {
		t.Fatalf("the throw started no fling: %+v", info)
	}
	start := list().ScrollOffset()
	if busy := busyTicks(h, 60); busy == 0 {
		t.Fatal("a running fling asked for no frames at all; nothing here measures anything")
	}
	if list().ScrollOffset() <= start {
		t.Fatal("the fling moved nothing; nothing here measures anything")
	}
	quiesce(t, h)
	list().ScrollBy(-list().ScrollOffset())
	h.Settle()

	// And now the case: throw, then hide the tab out from under it. The
	// selection is written directly rather than by tapping the bar, because a
	// tap would settle several frames of fling before anything was hidden.
	throw()
	if info := list().ScrollInfo(); !info.Flinging {
		t.Fatalf("the second throw started no fling: %+v", info)
	}
	sel.Set(1)
	h.Settle()
	afterTheTransition(h)
	atHide := list().ScrollOffset()
	if info := list().ScrollInfo(); info.Flinging {
		t.Fatalf("the list in the hidden tab is still flinging at %g units per second. "+
			"App.tickScrolls runs from the input phase and knows nothing about paint, so "+
			"not drawing the tab does not stop it", info.Velocity)
	}

	if busy := busyTicks(h, 600); busy != 0 {
		t.Fatalf("a fling inside a hidden tab asked for %d of 600 frames", busy)
	}
	if got := list().ScrollOffset(); got != atHide {
		t.Fatalf("the list in the hidden tab drifted from %v to %v after it was hidden. "+
			"ui.NavigationStackView promises a screen comes back where the user left it, "+
			"and this is a screen that scrolled itself %v units while nobody was looking",
			atHide, got, got-atHide)
	}

	// It comes back where it was left, which is the sentence in the godoc.
	sel.Set(0)
	h.Settle()
	list().AssertScrollOffset(atHide)
}

// --- the pointer -------------------------------------------------------------

// TestHidingATabUnderADraggingFingerDoesNotGoOnMovingTheSlider is the input
// half of the same class, and it is the worst of the findings that are not the
// blocker, because the value is *committed*.
//
// gift delivers every move to the node that captured the pointer, without a
// hit test — that is what a capture is. Hiding the subtree took nothing away
// from the pointer state, so a ui.Slider whose tab was hidden mid-drag went on
// reading the finger and writing the application's value: measured 0.500 to
// 0.971, committed on release.
func TestHidingATabUnderADraggingFingerDoesNotGoOnMovingTheSlider(t *testing.T) {
	var value float64 = 0.5
	var sel *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel = ctx.State("tab", 0)
			v := ctx.State("v", 0.5)
			value = ctx.Read(v)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("A", ui.Symbol{}, ui.Slider(v.Get(), v.Set).
					Frame(400, geom.Unbounded()).Key("slider")),
				ui.Tab("B", ui.Symbol{}, ui.Text("nothing")),
			)
		},
	})
	b := h.Find(gifttest.ByKey("slider")).Bounds()
	y := b.Min.Y + b.Height()/2

	// Control: a drag that is not interrupted moves the value, so a value
	// that did not move below is a value the fix held onto and not a slider
	// that never worked.
	h.PressAt(geom.Pt(b.Min.X+b.Width()*0.5, y))
	h.MoveTo(geom.Pt(b.Min.X+b.Width()*0.9, y))
	h.Settle()
	if value <= 0.6 {
		t.Fatalf("an ordinary drag left the value at %v; the fixture is not dragging the "+
			"slider at all", value)
	}
	h.ReleaseAt(geom.Pt(b.Min.X+b.Width()*0.9, y))
	h.Settle()

	// Back to the middle and start again, this time hiding the tab with the
	// finger still down.
	h.Find(gifttest.ByKey("slider")).Tap()
	h.PressAt(geom.Pt(b.Min.X+b.Width()*0.5, y))
	h.MoveTo(geom.Pt(b.Min.X+b.Width()*0.55, y))
	h.Settle()
	atHide := value

	sel.Set(1)
	h.Settle()
	h.AssertExists(gifttest.ByKey("slider"))

	// The finger keeps travelling, all the way to the far end. Nothing is
	// under it: the slider is in a tab that is not on the screen.
	for i := 6; i <= 10; i++ {
		h.MoveTo(geom.Pt(b.Min.X+b.Width()*float32(i)/10, y))
	}
	h.ReleaseAt(geom.Pt(b.Min.X+b.Width()*0.99, y))
	h.Settle()

	if value != atHide {
		t.Fatalf("a finger dragging over a hidden tab moved the application's value from "+
			"%v to %v and committed it on release. A captured pointer is delivered to "+
			"without a hit test, so hiding the subtree has to take the capture away as "+
			"well", atHide, value)
	}
}

// TestHidingATabClearsTheHoverAndPressOfTheNodeThatHasThem is MAJOR 4 of gate
// 13: the cleanup existed and was performed on the wrong node.
//
// The node that declares gift.Element.Hidden is a ui.layer, which has no
// interactor and therefore never holds a hover or a press. The node that holds
// them is a descendant — the button the mouse is resting on — and it was left
// lit. So the assertions here are on the *button*, deliberately, because that
// is the node the defect was about.
func TestHidingATabClearsTheHoverAndPressOfTheNodeThatHasThem(t *testing.T) {
	var sel *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel = ctx.State("tab", 0)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("A", ui.Symbol{}, ui.Button(ui.Text("press me"), func() {}).Key("btn")),
				ui.Tab("B", ui.Symbol{}, ui.Text("nothing")),
			)
		},
	})
	btn := h.Find(gifttest.ByKey("btn"))
	bb := btn.Bounds()
	c := geom.Pt(bb.Min.X+bb.Width()/2, bb.Min.Y+bb.Height()/2)
	h.MoveTo(c)
	h.PressAt(c)
	h.Settle()
	if ia := btn.Interaction(); !ia.Hover || !ia.Pressed {
		t.Fatalf("the button reads %+v with the mouse down on it; the fixture never lit it", ia)
	}

	sel.Set(1)
	h.Settle()

	if ia := h.Find(gifttest.ByKey("btn")).Interaction(); ia.Hover || ia.Pressed {
		t.Fatalf("the button inside the hidden tab still reads %+v. The cleanup runs on the "+
			"layer that carries the flag, and a layer has no interactor: the node that "+
			"holds the hover and the press is always a descendant", ia)
	}
}

// --- what a hidden subtree must not be allowed to drive ----------------------

// TestScrollIntoViewDeclinesANodeNobodyCanSee is MINOR 9: two questions about
// the same node in the same frame used to get two answers.
//
// gift.App.NodeVisibleBounds says "not on screen" about a node in a hidden tab
// and gift.App.ScrollIntoView used to say "done" — and it did not merely say
// it, it scrolled the container, destroying the offset the inactive tab was
// keeping. That offset is the entire reason the tab stayed mounted.
func TestScrollIntoViewDeclinesANodeNobodyCanSee(t *testing.T) {
	h, _ := tabApp(t)
	list := h.Find(gifttest.ByKey("list"))
	list.ScrollBy(300)
	h.Settle()
	want := list.ScrollOffset()

	// Control: while the tab is on screen, revealing a row far down does
	// something.
	last := h.Find(gifttest.ByKey("39"))
	if !h.App().ScrollIntoView(last.Ref()) {
		t.Fatal("revealing a row far down the visible list did nothing; the fixture " +
			"cannot show the difference")
	}
	h.Settle()
	list.ScrollBy(-list.ScrollOffset() + want)
	h.Settle()

	tapTab(h, "Form")
	target := h.Find(gifttest.ByKey("39"))
	if _, visible := h.App().NodeVisibleBounds(target.Ref()); visible {
		t.Fatal("the row in the hidden tab reports itself visible; the premise is gone")
	}
	if h.App().ScrollIntoView(target.Ref()) {
		t.Fatal("ScrollIntoView scrolled a container inside a hidden tab. " +
			"NodeVisibleBounds says the same node is not on screen, and the offset it " +
			"just moved is the one the tab is being kept mounted to preserve")
	}
	if got := h.Find(gifttest.ByKey("list")).ScrollOffset(); got != want {
		t.Fatalf("the hidden list moved from %v to %v", want, got)
	}
}

// --- the modal on an unbounded axis ------------------------------------------

// TestAModalOnAnUnboundedAxisIsRefusedLoudly is the blocker of gate 13.
//
// A ui.Modal inside a ui.VScroll produced a scrim of zero size, because the
// layer did not stretch an axis the parent left unbounded — while the alert
// above it sized and painted exactly as usual. The dialog was on screen, it
// looked right, and a tap went straight through it to the destructive button
// underneath. Nothing reported it: the size equals the constraint on an
// unbounded axis, so the overflow is zero; there is no visual difference; and
// there was no diagnostic.
//
// The answer is a panic, because [ui.ModalView] promises without qualification
// that the scrim swallows every pointer event, and a confirmation dialog on a
// kiosk that silently becomes decoration is the worst defect this project has
// a name for.
func TestAModalOnAnUnboundedAxisIsRefusedLoudly(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a modal measured with an unbounded height was accepted. Its scrim is " +
				"zero sized, the alert above it draws normally, and every tap goes " +
				"through the dialog")
		}
		msg, _ := r.(string)
		for _, want := range []string{"unbounded", "Flex", "Frame", "scrim"} {
			if !strings.Contains(msg, want) {
				t.Errorf("the panic does not mention %q; it has to tell the author what "+
					"the composition is and what to do about it:\n%s", want, msg)
			}
		}
	}()
	gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		View: ui.VScroll(ui.Modal(
			ui.VStack(rows("row ", 6)...),
			ui.Alert("Delete everything?", "This cannot be undone.",
				ui.AlertDestructive("Delete all", func() {})),
		)),
	})
}

// TestAModalWithAViewportSwallowsATapFarFromTheCard is the positive control of
// the test above, and it is the sentence [ui.ModalView] actually promises: not
// "the card is not tappable through" but "a tap anywhere at all does not reach
// what is underneath".
func TestAModalWithAViewportSwallowsATapFarFromTheCard(t *testing.T) {
	fired := 0
	h := navHarness(t, ui.Modal(
		ui.Button(ui.Text("Delete all"), func() { fired++ }).
			Frame(geom.Unbounded(), geom.Unbounded()).Key("danger"),
		ui.Alert("Delete everything?", "This cannot be undone.",
			ui.AlertDestructive("Delete all", func() {})),
	))
	card := h.Find(gifttest.ByType("ui.Alert")).Bounds()
	at := geom.Pt(8, 8)
	if card.Contains(at) {
		t.Fatal("the card covers the corner; pick another point")
	}
	h.TapAt(at)
	h.Settle()
	if fired != 0 {
		t.Fatalf("a tap %v away from the alert fired the button underneath %d times", at, fired)
	}
	if b := h.Find(gifttest.ByType("ui.scrim")).Bounds(); b.Width() < 600 || b.Height() < 400 {
		t.Fatalf("the scrim is %v, which is not the window; it is blocking by luck", b)
	}
}

// --- a name for the numbers --------------------------------------------------

// TestTheHiddenTabCostSummaryIsWhatTheGodocSays is not a behaviour test; it is
// the sentence of [ui.TabBarView] about the *remaining* cost, kept honest.
//
// The godoc says a state write inside a hidden tab still costs a build and a
// layout and no paint. That is the one cost the flag does not remove, it is
// stated rather than implied, and this is what would notice if a later change
// made it silently untrue in either direction — by skipping the layout, which
// would move the work to the frame the tab becomes visible on, or by painting
// after all.
func TestTheHiddenTabCostSummaryIsWhatTheGodocSays(t *testing.T) {
	var sel, n *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel = ctx.State("tab", 1)
			n = ctx.State("n", 1)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("Busy", ui.Symbol{},
					ui.VStack(rows("row ", ctx.Read(n))...).Key("list")),
				ui.Tab("Quiet", ui.Symbol{}, ui.Text("nothing")),
			)
		},
	})
	before := h.Diagnostics()
	n.Set(6)
	h.Settle()
	after := h.Diagnostics()

	if after.Builds == before.Builds {
		t.Fatal("a state write inside a hidden tab rebuilt nothing; the tab is not mounted")
	}
	if got := h.Find(gifttest.ByKey("list")).Bounds().Height(); got < 6*40 {
		t.Fatalf("the hidden list measured %v tall after growing to six rows; it was not "+
			"laid out, so the work has been moved to the frame it becomes visible on",
			got)
	}
	// And no paint: the hidden tab contributes nothing to the display list,
	// which is what the first promise of the flag is.
	h.Find(gifttest.ByKey("list")).AssertNotVisible()
}
