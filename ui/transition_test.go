package ui_test

import (
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// The motion of the three navigation containers, measured.
//
// Every test in this file is a *pair* of frames or better, and that is not a
// stylistic preference: the defect these transitions fix presented as "frame
// 4000 differs from frame 3999 by 1,723,004 pixels and frames 4006, 4012,
// 4018 and 4024 are bit identical to it", so a single frame proves nothing at
// all. What proves a transition is that two frames taken at two known points
// of it differ, and that two frames taken after it has ended do not.
//
// The clock is the harness's, which is the clock [gift.App.BeginInput] is
// given, so every one of these runs in microseconds and none of them can
// flake: a transition is a pure function of that clock; see
// [gift.TransitionSpec].

// markerColor is a colour no theme produces, so that a test can find the one
// rectangle it cares about in a display list of hundreds.
func markerColor(i int) ui.Color {
	return ui.RGBA(uint8(10+i), 0, uint8(250-i), 255)
}

// marker is a full size box in a colour of its own: the thing a transition
// test watches move.
func marker(i int) gift.View {
	return ui.Box().Background(markerColor(i)).Flex(1)
}

// markerRect is where the marker of index i is drawn *on the device*, which
// is the node's own rectangle mapped through the transform stack of the
// display list. A transition is a transform, so reading the operation's raw
// Bounds would report the place the node would be if it were not moving —
// which is exactly the value a broken transition also reports.
func markerRect(t testing.TB, h *gifttest.Harness, i int) (geom.Rect, bool) {
	t.Helper()
	want := ui.ResolveColor(markerColor(i))
	list := h.List()
	for _, op := range h.Ops() {
		if op.Kind != render.OpFillRect && op.Kind != render.OpFillRoundRect {
			continue
		}
		if op.Color != render.Color(want) {
			continue
		}
		return list.Xform(op.Xform).TransformRect(op.Bounds), true
	}
	return geom.Rect{}, false
}

func mustMarkerRect(t testing.TB, h *gifttest.Harness, i int) geom.Rect {
	t.Helper()
	r, ok := markerRect(t, h, i)
	if !ok {
		t.Fatalf("the marker of tab %d is not in the display list at all", i)
	}
	return r
}

// tabsWithMarkers is a two tab application whose screens are two full size
// coloured boxes.
func tabsWithMarkers(t testing.TB, size geom.Size) (*gifttest.Harness, *gift.State[int]) {
	t.Helper()
	var sel *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  size,
		Root: func(ctx *gift.Context) gift.View {
			sel = ctx.State("tab", 0)
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("A", ui.Symbol{}, marker(0)),
				ui.Tab("B", ui.Symbol{}, marker(1)),
			)
		},
	})
	return h, sel
}

// TestATabSwitchSlidesTheTwoScreensAcrossTheWindowInsteadOfCuttingToTheNewOne
// is the defect this work started from, stated as three positions.
//
// Before: the whole window changed in one frame and the four frames after it
// were bit identical. Now the arriving tab is off the trailing edge when the
// switch begins, somewhere in the middle of the window a third of the way
// through, and exactly in place at the end — and the tab it replaces is the
// mirror image of that, so that the two edges meet on every frame and no gap
// opens between them.
func TestATabSwitchSlidesTheTwoScreensAcrossTheWindowInsteadOfCuttingToTheNewOne(t *testing.T) {
	h, sel := tabsWithMarkers(t, geom.Sz(400, 300))
	atRest := mustMarkerRect(t, h, 0)

	sel.Set(1)
	h.Settle()

	start := mustMarkerRect(t, h, 1)
	if start.Min.X < atRest.Max.X-1 {
		t.Fatalf("the arriving tab starts at x=%v; it should still be off the trailing edge "+
			"of a window that ends at x=%v, or the switch is a cut", start.Min.X, atRest.Max.X)
	}

	h.Advance(60 * time.Millisecond)
	middle := mustMarkerRect(t, h, 1)
	if !(middle.Min.X < start.Min.X && middle.Min.X > atRest.Min.X) {
		t.Fatalf("a third of the way through the transition the arriving tab is at x=%v, "+
			"which is not between its parked place x=%v and its final place x=%v",
			middle.Min.X, start.Min.X, atRest.Min.X)
	}

	// The two move in lockstep: the outgoing tab's trailing edge is the
	// incoming one's leading edge on every frame, so there is never a strip
	// of window with nothing on it.
	leaving := mustMarkerRect(t, h, 0)
	if d := leaving.Max.X - middle.Min.X; d < -0.5 || d > 0.5 {
		t.Errorf("mid transition the outgoing tab ends at x=%v and the incoming one starts "+
			"at x=%v; a gap of %v opens between them", leaving.Max.X, middle.Min.X, d)
	}

	h.Advance(ui.ControlAnimation)
	if got := mustMarkerRect(t, h, 1); got != atRest {
		t.Errorf("after the transition the arrived tab is at %v, want exactly the place the "+
			"tab it replaced occupied, %v", got, atRest)
	}
	if _, ok := markerRect(t, h, 0); ok {
		t.Error("the tab that was switched away from is still being painted after its " +
			"transition ended; a hidden tab must cost nothing per frame")
	}
}

// TestTheTransitionOfATabSwitchEndsAndTheApplicationGoesIdle is the property
// every animation in this project has to have, applied to the newest one: the
// enrolment is bounded, the picture stops changing and the device is allowed
// to sleep.
//
// It measures three things that are easy to confuse. That frames are asked for
// *during* the window — otherwise the transition is invisible whatever the
// arithmetic says. That the number of them is the window and not more. And
// that [gift.Diagnostics.Animating], which is the only thing an outside
// observer can read, agrees with both.
func TestTheTransitionOfATabSwitchEndsAndTheApplicationGoesIdle(t *testing.T) {
	h, sel := tabsWithMarkers(t, geom.Sz(400, 300))
	if h.Diagnostics().Animating {
		t.Fatal("the application is animating before anything happened; nothing here " +
			"measures anything")
	}

	sel.Set(1)
	h.Settle()
	if !h.Diagnostics().Animating {
		t.Fatal("nothing is animating in the frame after a tab switch, so /diag would " +
			"report a still picture while the screen is supposed to be moving")
	}

	const step = 16 * time.Millisecond
	busy := busyTicks(h, 60)
	// 180 ms at 16 ms a tick is between eleven and twelve ticks, plus the one
	// frame every enrolment draws as it expires; see [gift.App.tickAnimations].
	if busy == 0 {
		t.Fatal("a tab switch asked for no frames at all")
	}
	if want := int(ui.ControlAnimation/step) + 2; busy > want {
		t.Errorf("a tab switch asked for %d of 60 frames; %v at %v a tick is at most %d, "+
			"so something is re-arming instead of expiring", busy, ui.ControlAnimation, step, want)
	}
	if h.Diagnostics().Animating {
		t.Error("the application is still animating a second after a tab switch; the " +
			"transition never terminated")
	}
	if busy := busyTicks(h, 60); busy != 0 {
		t.Errorf("a settled application asked for %d of 60 frames after a tab switch", busy)
	}
}

// TestATabTappedTwiceInQuickSuccessionTurnsRoundFromWhereItIsRatherThanJumping
// is the kiosk clause: the user is in a hurry, taps again halfway through, and
// the picture must not jump to the end of the first movement before starting
// the second one.
func TestATabTappedTwiceInQuickSuccessionTurnsRoundFromWhereItIsRatherThanJumping(t *testing.T) {
	h, sel := tabsWithMarkers(t, geom.Sz(400, 300))
	atRest := mustMarkerRect(t, h, 0)

	sel.Set(1)
	h.Settle()
	h.Advance(60 * time.Millisecond)
	half := mustMarkerRect(t, h, 0)
	if half.Min.X >= atRest.Min.X {
		t.Fatalf("the outgoing tab has not moved (x=%v); nothing here measures anything",
			half.Min.X)
	}

	// Back again, mid flight.
	sel.Set(0)
	h.Settle()
	back := mustMarkerRect(t, h, 0)
	if d := back.Min.X - half.Min.X; d < -1 || d > 1 {
		t.Errorf("the frame in which the user changed their mind moved the screen from "+
			"x=%v to x=%v; a reversal has to start where the screen is, not at either end",
			half.Min.X, back.Min.X)
	}
	h.Advance(ui.ControlAnimation + 32*time.Millisecond)
	if got := mustMarkerRect(t, h, 0); got != atRest {
		t.Errorf("after the reversal the tab came to rest at %v, want %v", got, atRest)
	}
}

// TestAScreenPushedOntoTheNavigationStackArrivesFromTheTrailingEdge is the
// push half of a navigation transition, and it also pins the parallax: the
// screen underneath moves the other way and stays on the screen throughout,
// because a screen that vanished the instant it was covered would leave a
// strip of nothing behind the arriving one.
func TestAScreenPushedOntoTheNavigationStackArrivesFromTheTrailingEdge(t *testing.T) {
	var depth *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(400, 300),
		Root: func(ctx *gift.Context) gift.View {
			depth = ctx.State("depth", 0)
			screens := []ui.ScreenSpec{ui.Screen("Root", marker(0))}
			if ctx.Read(depth) >= 1 {
				screens = append(screens, ui.Screen("Pushed", marker(1)))
			}
			return ui.NavigationStack(func() { depth.Set(0) }, screens...)
		},
	})
	root := mustMarkerRect(t, h, 0)

	depth.Set(1)
	h.Settle()
	arriving := mustMarkerRect(t, h, 1)
	if arriving.Min.X < root.Max.X-1 {
		t.Fatalf("the pushed screen starts at x=%v, inside a window that ends at x=%v; "+
			"a push that starts on the screen is a cut", arriving.Min.X, root.Max.X)
	}
	if _, ok := markerRect(t, h, 0); !ok {
		t.Fatal("the covered screen stopped being painted the instant it was covered, so " +
			"the arriving screen slides in over a hole")
	}

	h.Advance(60 * time.Millisecond)
	mid := mustMarkerRect(t, h, 1)
	if mid.Min.X >= arriving.Min.X {
		t.Errorf("the pushed screen is at x=%v after 60 ms, having started at x=%v; it is "+
			"not moving", mid.Min.X, arriving.Min.X)
	}
	covered := mustMarkerRect(t, h, 0)
	if covered.Min.X >= root.Min.X {
		t.Errorf("the covered screen is at x=%v and started at x=%v; it should be sliding "+
			"the other way", covered.Min.X, root.Min.X)
	}

	h.Advance(ui.ControlAnimation)
	if got := mustMarkerRect(t, h, 1); got != root {
		t.Errorf("the pushed screen came to rest at %v, want the place the root screen "+
			"occupied, %v", got, root)
	}
}

// TestAPoppedNavigationScreenComesBackFromTheSideItWentTo is the pop half, and
// it is the one with a documented limitation attached: the screen being popped
// is unmounted by the application's own state change and cannot be animated
// out, so what moves is the screen that is revealed. It has to come back from
// the *leading* edge, where it went when it was covered, and not from the
// trailing one, where a freshly pushed screen arrives from.
//
// That distinction is the whole reason [gift.TransitionSpec] has both a Parked
// and an Entry: without the memory of where a node actually went, a pop would
// be animated exactly like a push and would read as going forwards.
func TestAPoppedNavigationScreenComesBackFromTheSideItWentTo(t *testing.T) {
	var depth *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(400, 300),
		Root: func(ctx *gift.Context) gift.View {
			depth = ctx.State("depth", 0)
			screens := []ui.ScreenSpec{ui.Screen("Root", marker(0))}
			if ctx.Read(depth) >= 1 {
				screens = append(screens, ui.Screen("Pushed", marker(1)))
			}
			return ui.NavigationStack(func() { depth.Set(0) }, screens...)
		},
	})
	root := mustMarkerRect(t, h, 0)
	depth.Set(1)
	h.Settle()
	h.Advance(ui.ControlAnimation + 32*time.Millisecond)

	depth.Set(0)
	h.Settle()
	back := mustMarkerRect(t, h, 0)
	if back.Min.X >= root.Min.X {
		t.Fatalf("the revealed screen starts the pop at x=%v, where it will finish (x=%v); "+
			"a pop with nothing moving is a cut", back.Min.X, root.Min.X)
	}
	// And it has to come back from *near* its place rather than from the full
	// width it was parked at, because the screen that was covering it has
	// already been unmounted: a revealed screen that started a whole width
	// away would leave the window empty for the first frame of every pop.
	// That is what [gift.TransitionSpec.Return] is for.
	if back.Min.X < root.Min.X-root.Width()/4 {
		t.Errorf("the revealed screen starts the pop at x=%v, more than a quarter of a "+
			"window short of x=%v; for that frame the window is empty", back.Min.X, root.Min.X)
	}
	h.Advance(60 * time.Millisecond)
	mid := mustMarkerRect(t, h, 0)
	if !(mid.Min.X > back.Min.X && mid.Min.X < root.Min.X) {
		t.Errorf("the revealed screen is at x=%v, which is not between where it came from "+
			"(x=%v) and where it belongs (x=%v)", mid.Min.X, back.Min.X, root.Min.X)
	}
	h.Advance(ui.ControlAnimation)
	if got := mustMarkerRect(t, h, 0); got != root {
		t.Errorf("the revealed screen came to rest at %v, want %v", got, root)
	}
}

// scrimAlpha is the alpha of the one full window wash in the display list.
//
// No wash at all is zero and not an error: a fully transparent scrim is not
// emitted, because an operation nobody can see is an operation the backend
// should not be handed, and "the window is not washed" is what both spellings
// mean on the screen.
func scrimAlpha(t testing.TB, h *gifttest.Harness) float32 {
	t.Helper()
	size := h.Size()
	for _, op := range h.Ops() {
		if op.Kind != render.OpFillRect {
			continue
		}
		if op.Color == render.Color(ui.ResolveColor(markerColor(0))) ||
			op.Color == render.Color(ui.ResolveColor(markerColor(1))) {
			// A marker, not the wash. The two are told apart by colour and
			// not by size, because the content behind a modal is full window
			// too and answering with its alpha would make every assertion
			// here pass for the wrong reason.
			continue
		}
		b := h.List().Xform(op.Xform).TransformRect(op.Bounds)
		if b.Width() >= size.W-1 && b.Height() >= size.H-1 {
			return op.Color.A
		}
	}
	return 0
}

// TestAModalFadesItsScrimInAndRisesIntoPlaceInsteadOfAppearingWhole is the
// modal half of the defect: the scrim and the dialog used to arrive complete
// in a single frame, 1,723,004 pixels at once.
//
// The scrim cannot slide — it covers the window, and a covering rectangle that
// moves stops covering — so it is the one part of a modal that fades, and the
// dialog rises from under its own footprint. Both are driven by the phase of
// one transition, so this test reads both from the same frames.
func TestAModalFadesItsScrimInAndRisesIntoPlaceInsteadOfAppearingWhole(t *testing.T) {
	var open *gift.State[bool]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(400, 300),
		Root: func(ctx *gift.Context) gift.View {
			open = ctx.State("open", false)
			return ui.Modal(marker(0), ui.Box().Background(markerColor(1)).Frame(120, 80)).
				Presented(ctx.Read(open))
		},
	})
	if a := scrimAlpha(t, h); a > 0 {
		t.Fatalf("a closed modal drew a wash of alpha %v over the window", a)
	}

	open.Set(true)
	h.Settle()
	first := scrimAlpha(t, h)
	firstDialog := mustMarkerRect(t, h, 1)
	if first != 0 {
		t.Errorf("the first frame of an opening modal drew its scrim at alpha %v; it has "+
			"to start at nothing or the fade is a cut", first)
	}

	h.Advance(90 * time.Millisecond)
	mid := scrimAlpha(t, h)
	midDialog := mustMarkerRect(t, h, 1)
	if !(mid > first) {
		t.Errorf("halfway through the opening the scrim is at alpha %v, having started at "+
			"%v; it is not fading in", mid, first)
	}
	if midDialog.Min.Y >= firstDialog.Min.Y {
		t.Errorf("the dialog is at y=%v having started at y=%v; it is not rising",
			midDialog.Min.Y, firstDialog.Min.Y)
	}

	h.Advance(ui.ControlAnimation)
	full := scrimAlpha(t, h)
	if !(full > mid) {
		t.Errorf("the opened modal's scrim settled at alpha %v, which is not more than the "+
			"%v it was passing through", full, mid)
	}
	if h.Diagnostics().Animating {
		t.Error("the application is still animating after the modal finished opening")
	}
}

// TestADismissedModalIsStillDrawnWhileItLeavesAndIsGoneAfterwards is the half
// the nil-modal spelling cannot do at all, and the reason
// [ui.ModalView.Presented] exists: an unmounted dialog has no node and nothing
// to paint, so a modal that closes by becoming nil closes in one frame.
func TestADismissedModalIsStillDrawnWhileItLeavesAndIsGoneAfterwards(t *testing.T) {
	var open *gift.State[bool]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(400, 300),
		Root: func(ctx *gift.Context) gift.View {
			open = ctx.State("open", true)
			return ui.Modal(marker(0), ui.Box().Background(markerColor(1)).Frame(120, 80)).
				Presented(ctx.Read(open))
		},
	})
	h.Advance(ui.ControlAnimation + 32*time.Millisecond)
	opened := mustMarkerRect(t, h, 1)
	openAlpha := scrimAlpha(t, h)

	open.Set(false)
	h.Settle()
	h.Advance(60 * time.Millisecond)
	leaving, ok := markerRect(t, h, 1)
	if !ok {
		t.Fatal("the dismissed dialog was gone from the display list 60 ms into its exit; " +
			"the dismissal is a cut")
	}
	if leaving.Min.Y <= opened.Min.Y {
		t.Errorf("the leaving dialog is at y=%v, having been at y=%v; it is not sinking",
			leaving.Min.Y, opened.Min.Y)
	}
	if a := scrimAlpha(t, h); !(a < openAlpha) {
		t.Errorf("the scrim of a leaving modal is at alpha %v, the same as the %v it had "+
			"while the modal was up; it is not fading out", a, openAlpha)
	}

	h.Advance(ui.ControlAnimation)
	if _, ok := markerRect(t, h, 1); ok {
		t.Error("the dismissed dialog is still being painted a transition after it left")
	}
	if a := scrimAlpha(t, h); a > 0 {
		t.Errorf("the scrim is still washing the window at alpha %v after the modal left", a)
	}
	if h.Diagnostics().Animating {
		t.Error("the application never went idle after the modal was dismissed")
	}
}

// TestATapDuringATabTransitionReachesTheArrivingScreenAndNotTheOneLeaving is
// the kiosk clause of [gift.TransitionSpec], and the one place where this
// project deliberately lets input and paint disagree.
//
// A user who taps during a transition is aiming at the screen that is
// arriving, so the arriving screen answers at its *final* position from the
// first frame and the leaving one answers nowhere at all. The alternative —
// hit testing the moving picture — makes the target under a finger a different
// control three frames later.
func TestATapDuringATabTransitionReachesTheArrivingScreenAndNotTheOneLeaving(t *testing.T) {
	var sel *gift.State[int]
	taps := [2]int{}
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(400, 300),
		Root: func(ctx *gift.Context) gift.View {
			sel = ctx.State("tab", 0)
			btn := func(i int) gift.View {
				return ui.Button(ui.Text("tab"+string(rune('A'+i))), func() { taps[i]++ }).
					Key("btn" + string(rune('A'+i))).Flex(1)
			}
			return ui.TabBar(ctx.Read(sel), sel.Set,
				ui.Tab("A", ui.Symbol{}, btn(0)),
				ui.Tab("B", ui.Symbol{}, btn(1)),
			)
		},
	})
	at := h.Find(gifttest.ByKey("btnA")).Center()

	sel.Set(1)
	h.Settle()
	// One frame into the switch: the outgoing screen still fills the window
	// and the arriving one is not visibly there yet.
	h.ClickAt(at)
	if taps[0] != 0 {
		t.Errorf("a tap during the transition reached the screen that is leaving %d time(s); "+
			"a screen on its way out must answer nothing", taps[0])
	}
	if taps[1] != 1 {
		t.Errorf("a tap during the transition reached the arriving screen %d time(s), want 1; "+
			"a transition must never delay or swallow input", taps[1])
	}
}

// TestATransitionAllocatesNothingPerFrame is the frame path contract of the
// project plan, section 11, applied to the new mechanism. A transition is a
// pure function of the clock read during paint; if this ever allocates,
// something is building or boxing per frame.
func TestATransitionAllocatesNothingPerFrame(t *testing.T) {
	h, sel := tabsWithMarkers(t, geom.Sz(400, 300))
	sel.Set(1)
	h.Settle()
	// Inside the window, so every measured frame is a moving one.
	if avg := testing.AllocsPerRun(50, func() {
		h.App().BeginInput(h.Now())
		if err := h.App().Update(h.Size()); err != nil {
			t.Fatal(err)
		}
		h.App().Paint()
	}); avg != 0 {
		t.Errorf("a frame during a transition allocated %v times; the frame path is under a "+
			"zero allocation contract", avg)
	}
}

// TestAPushedScreenNeverOverlapsTheScreenItCovers is the geometric property
// that stands in for an opaque background, and the reason a covered screen
// parks a full width away rather than an eighth of one.
//
// A screen in this package draws no background: its cards do. Two screens that
// overlap are therefore two screens legible through one another, which is what
// an eighth-width parallax produced and what a person would report as "the
// push is a smudge". Moving both screens by the same distance in opposite
// directions makes their edges meet on every frame instead: no overlap and, as
// TestAScreenPushedOntoTheNavigationStackArrivesFromTheTrailingEdge shows on
// the other side of the same arithmetic, no gap either.
func TestAPushedScreenNeverOverlapsTheScreenItCovers(t *testing.T) {
	var depth *gift.State[int]
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(400, 300),
		Root: func(ctx *gift.Context) gift.View {
			depth = ctx.State("depth", 0)
			screens := []ui.ScreenSpec{ui.Screen("Root", marker(0))}
			if ctx.Read(depth) >= 1 {
				screens = append(screens, ui.Screen("Pushed", marker(1)))
			}
			return ui.NavigationStack(func() { depth.Set(0) }, screens...)
		},
	})
	depth.Set(1)
	h.Settle()
	for _, at := range []time.Duration{0, 30, 60, 90, 120, 150} {
		if at > 0 {
			h.Advance(30 * time.Millisecond)
		}
		covered := mustMarkerRect(t, h, 0)
		arriving := mustMarkerRect(t, h, 1)
		if d := arriving.Min.X - covered.Max.X; d < -0.5 || d > 0.5 {
			t.Fatalf("%v into the push the covered screen ends at x=%v and the arriving one "+
				"starts at x=%v: they %s by %v", at, covered.Max.X, arriving.Min.X,
				map[bool]string{true: "overlap", false: "leave a gap"}[d < 0], d)
		}
	}
}
