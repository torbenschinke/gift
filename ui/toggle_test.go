package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// toggleApp is the fixture every test in this file drives: one switch bound to
// one piece of state, written the way an application would write it.
//
// It is a component and not a bare view on purpose. A [ui.Toggle] reports the
// value that was asked for and draws the value it was given, so a fixture
// without state would be a switch that never moves, and half of what is under
// test here — what survives the rebuild that storing the value causes — would
// not happen at all.
func toggleApp(initial bool) func(*gift.Context) gift.View {
	return func(ctx *gift.Context) gift.View {
		on := ctx.State("on", initial)
		v := ctx.Read(on)
		return ui.VStack(
			ui.Toggle(v, func(want bool) { on.Set(want) }).Key("sw").Label("Dark mode"),
		).Padding(20)
	}
}

// TestATapOnASwitchFlipsItAndATapBackFlipsItAgain is the surface a user meets
// first, and it is a touch and not a mouse click: a finger produces no hover,
// so a control that only reacted when hovered would pass a click test and fail
// on the device this project targets.
func TestATapOnASwitchFlipsItAndATapBackFlipsItAgain(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})

	if on := switchIsOn(t, h); on {
		t.Fatal("the switch was born on")
	}
	h.Find(gifttest.ByKey("sw")).Tap()
	if on := switchIsOn(t, h); !on {
		t.Fatal("a tap did not switch it on")
	}
	h.Find(gifttest.ByKey("sw")).Tap()
	if on := switchIsOn(t, h); on {
		t.Fatal("a second tap did not switch it off again")
	}
}

// TestATapNearlyOutsideTheDrawnSwitchStillFlipsIt is the hit target claim of
// [ui.ControlHitTarget] as a test.
//
// The switch is drawn 31 logical pixels high inside a hit area of 44, so there
// are between six and seven pixels above and below the capsule that are live
// and paint nothing. A finger on a Pi touchscreen lands there routinely. The
// tap below is aimed two pixels inside the top edge of the bounds, which is
// nine pixels above anything that is drawn.
//
// It also asserts the other half, that the area really is that big and not
// simply the whole screen: a tap one pixel above the bounds does nothing.
func TestATapNearlyOutsideTheDrawnSwitchStillFlipsIt(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
	b := h.Find(gifttest.ByKey("sw")).Bounds()

	// The drawn capsule, from the display list, so that the claim "nine
	// pixels above anything drawn" is measured and not asserted.
	track := firstFillRoundRect(t, h)
	if track.Min.Y-b.Min.Y < 5 {
		t.Fatalf("the drawn track %v is not meaningfully inset in the hit area %v, so this "+
			"test would pass for a switch with no extra target at all", track, b)
	}

	h.ClickAt(geom.Pt(b.Min.X+b.Width()/2, b.Min.Y+2))
	if !switchIsOn(t, h) {
		t.Fatalf("a tap at the top edge of the hit area %v did not reach the switch; the drawn "+
			"track is %v, so this is the band ControlHitTarget exists for", b, track)
	}

	h.ClickAt(geom.Pt(b.Min.X+b.Width()/2, b.Min.Y-1))
	if !switchIsOn(t, h) {
		t.Fatal("a tap one pixel above the bounds flipped the switch, so the hit area is not " +
			"the bounds and this test proves nothing about their size")
	}
}

// TestAFocusedSwitchRespondsToSpaceAndEnter covers the keyboard half. It tabs
// to the switch rather than focusing it directly, so the focus order is part
// of what is checked.
func TestAFocusedSwitchRespondsToSpaceAndEnter(t *testing.T) {
	for _, k := range []struct {
		name string
		key  gift.Key
	}{
		{"space", gift.KeySpace},
		{"enter", gift.KeyEnter},
	} {
		t.Run(k.name, func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
			h.Tab()
			h.AssertFocus(gifttest.ByKey("sw"))
			h.Key(k.key)
			if !switchIsOn(t, h) {
				t.Fatalf("%s on a focused switch did not flip it", k.name)
			}
			h.Key(k.key)
			if switchIsOn(t, h) {
				t.Fatalf("a second %s did not flip it back", k.name)
			}
		})
	}
}

// TestASwitchReleasedOutsideItselfDoesNotFlip is pointer capture. A press that
// travels off the control and is released elsewhere is not an activation, and
// the release still arrives here — that is what capture is for.
func TestASwitchReleasedOutsideItselfDoesNotFlip(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
	sw := h.Find(gifttest.ByKey("sw"))
	sw.Press()
	h.ReleaseAt(geom.Pt(280, 180))
	if switchIsOn(t, h) {
		t.Fatal("a release outside the switch flipped it")
	}
}

// TestACancelledPressOnASwitchDoesNotFlip: a gesture the platform took over is
// not a tap.
func TestACancelledPressOnASwitchDoesNotFlip(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
	h.Find(gifttest.ByKey("sw")).Press()
	h.CancelPointer()
	if switchIsOn(t, h) {
		t.Fatal("a cancelled press flipped the switch")
	}
}

// TestADisabledSwitchIgnoresEveryInputAndIsSkippedByTheFocusOrder.
func TestADisabledSwitchIgnoresEveryInputAndIsSkippedByTheFocusOrder(t *testing.T) {
	flips := 0
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Toggle(false, func(bool) { flips++ }).Key("sw").Disabled(true),
			ui.Button(ui.Text("after"), nil).Key("after"),
		).Padding(20),
		Size: geom.Sz(300, 200),
		Font: loadTestFont(t),
	})
	h.Find(gifttest.ByKey("sw")).AssertDisabled()
	h.Find(gifttest.ByKey("sw")).Tap()
	if flips != 0 {
		t.Fatalf("a disabled switch reported %d changes", flips)
	}
	h.Tab()
	h.AssertFocus(gifttest.ByKey("after"))
}

// TestTheKnobOfASwitchTravelsFromOneSideToTheOther is the animation, measured
// rather than described.
//
// It reads the knob's position out of the display list at three points: before
// the flip, halfway through [ui.ControlAnimation], and after it. The middle
// sample has to be strictly between the two ends — that is what "animated"
// means, and it is the assertion that fails if the knob jumps instead.
func TestTheKnobOfASwitchTravelsFromOneSideToTheOther(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})

	off := knobCenterX(t, h)
	h.Find(gifttest.ByKey("sw")).Tap()
	// The tap settles the tree, which leaves the clock where it was, so this
	// is the first frame of the transition.
	start := knobCenterX(t, h)
	h.Advance(ui.ControlAnimation / 2)
	mid := knobCenterX(t, h)
	h.Advance(ui.ControlAnimation)
	on := knobCenterX(t, h)

	if !(on > off) {
		t.Fatalf("the knob ended at %v having started at %v; switching on must move it right", on, off)
	}
	if !nearly(start, off) {
		t.Fatalf("the knob was already at %v in the frame the flip happened, having been at %v: "+
			"the transition started at the wrong place", start, off)
	}
	if !(mid > off && mid < on) {
		t.Fatalf("halfway through the transition the knob was at %v, which is not strictly "+
			"between %v and %v: the knob jumped rather than travelled", mid, off, on)
	}
}

// TestASwitchThatHasFinishedAnimatingLetsTheApplicationSleep is the other half
// of the animation contract, and it is the one that matters on a kiosk: an
// enrolment that never expires holds the backend at its full tick rate for the
// life of the node.
//
// It asserts on [gift.App.NeedsPaint], which is what the backend's idle tick
// policy reads.
func TestASwitchThatHasFinishedAnimatingLetsTheApplicationSleep(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
	h.Frame()
	if asksForFrames(h) {
		t.Fatal("an untouched switch is already asking for frames")
	}

	h.Find(gifttest.ByKey("sw")).Tap()
	h.Advance(ui.ControlAnimation / 2)
	if !asksForFrames(h) {
		t.Fatal("a switch in the middle of its transition is not asking to be repainted, so " +
			"the transition would be drawn at the idle tick rate")
	}

	h.Advance(2 * ui.ControlAnimation)
	if asksForFrames(h) {
		t.Fatal("a switch that finished animating is still asking for frames; an animation " +
			"without an end keeps the machine awake for ever")
	}
}

// TestASwitchBornOnDoesNotAnimateItselfOn is what [gift.ControlState.Armed]
// is for. A settings screen with six switches already on must not open with
// six knobs sliding into place.
func TestASwitchBornOnDoesNotAnimateItselfOn(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(true), Size: geom.Sz(300, 200)})
	track := firstFillRoundRect(t, h)
	x := knobCenterX(t, h)
	if !(x > (track.Min.X+track.Max.X)/2) {
		t.Fatalf("the knob of a switch built on is at %v, left of the middle of its track %v: "+
			"it is animating into place instead of being drawn there", x, track)
	}
}

// TestFlippingASwitchTwiceTurnsTheKnobRoundFromTheMiddle: an interrupted
// transition starts from where the knob is, not from where it was going.
func TestFlippingASwitchTwiceTurnsTheKnobRoundFromTheMiddle(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
	off := knobCenterX(t, h)

	h.Find(gifttest.ByKey("sw")).Tap()
	h.Advance(ui.ControlAnimation / 2)
	mid := knobCenterX(t, h)

	h.Find(gifttest.ByKey("sw")).Tap() // back to off, from the middle
	back := knobCenterX(t, h)
	if !nearly(back, mid) {
		t.Fatalf("the knob was at %v and the second flip put it at %v; an interrupted "+
			"transition must turn round from where the knob is", mid, back)
	}
	h.Advance(2 * ui.ControlAnimation)
	if end := knobCenterX(t, h); !nearly(end, off) {
		t.Fatalf("the knob came to rest at %v, want the off position %v", end, off)
	}
}

// TestASwitchIsDrawnInSemanticColoursThatFollowTheTheme: the track of a switch
// that is on is the accent of the theme in force, in both themes, which is the
// property a literal RGB would break.
func TestASwitchIsDrawnInSemanticColoursThatFollowTheTheme(t *testing.T) {
	for _, tc := range []struct {
		name  string
		theme ui.Theme
	}{
		{"light", ui.LightTheme()},
		{"dark", ui.DarkTheme()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{
				View:  ui.Toggle(true, nil).Key("sw"),
				Size:  geom.Sz(120, 80),
				Theme: tc.theme,
			})
			want := tc.theme.Color(ui.ColorAccent)
			track := firstFillRoundRect(t, h)
			var found bool
			for _, op := range h.Ops() {
				if op.Kind == render.OpFillRoundRect && op.Bounds == track && op.Color == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("the track of a switch that is on is not %v under the %s theme.\n%s",
					want, tc.name, formatOpsForTest(h))
			}
		})
	}
}

// BenchmarkToggleFrameIsAllocationFree is the 0 B/op contract of the project
// plan, section 11, for the steady state frame path of a switch.
//
// The loop draws a switch that is *animating*, which is the expensive state
// and the only one in which the painter does arithmetic: the eased phase, the
// colour mix and the knob rectangle. It gets there by flipping the switch and
// then moving the clock halfway through the transition and leaving it there,
// so every frame of the loop is the same instant in the middle of the travel
// and none of them falls off the end of it.
func BenchmarkToggleFrameIsAllocationFree(b *testing.B) {
	h := gifttest.New(b, gifttest.Options{Root: toggleApp(false), Size: geom.Sz(300, 200)})
	h.Find(gifttest.ByKey("sw")).Tap()
	h.Advance(ui.ControlAnimation / 2)
	h.Frame()
	h.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Frame()
	}
}

// --- helpers ----------------------------------------------------------------

// switchIsOn reads the state back through the picture rather than through the
// fixture's variable, so that a test which passes is a test in which the
// *drawn* switch moved.
//
// It moves the clock past [ui.ControlAnimation] first, because the knob is
// animated: in the frame in which a tap is handled the knob has not moved yet,
// and a helper that read the picture at that instant would report the old
// value for every control in this file.
func switchIsOn(t testing.TB, h *gifttest.Harness) bool {
	t.Helper()
	h.Advance(2 * ui.ControlAnimation)
	track := firstFillRoundRect(t, h)
	return knobCenterX(t, h) > (track.Min.X+track.Max.X)/2
}

// firstFillRoundRect is the track: the first filled rounded rectangle the
// switch emits, which its painter draws before anything else.
func firstFillRoundRect(t testing.TB, h *gifttest.Harness) geom.Rect {
	t.Helper()
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRoundRect {
			return op.Bounds
		}
	}
	t.Fatalf("the switch drew no filled rounded rectangle at all.\n%s", formatOpsForTest(h))
	return geom.Rect{}
}

// knobCenterX is the centre of the knob: the second filled rounded rectangle,
// which is the only other fill a switch emits.
func knobCenterX(t testing.TB, h *gifttest.Harness) float32 {
	t.Helper()
	seen := 0
	for _, op := range h.Ops() {
		if op.Kind != render.OpFillRoundRect {
			continue
		}
		seen++
		if seen == 2 {
			return (op.Bounds.Min.X + op.Bounds.Max.X) / 2
		}
	}
	t.Fatalf("the switch drew %d filled rounded rectangles, want at least the track and the "+
		"knob.\n%s", seen, formatOpsForTest(h))
	return 0
}

func nearly(a, b float32) bool {
	d := a - b
	return d < 0.01 && d > -0.01
}

// formatOpsForTest renders the display list for a failure message. The
// failures in this file are about geometry, so the rectangles have to be in
// the message or the reader has to run the test again with a print in it.
func formatOpsForTest(h *gifttest.Harness) string {
	var b strings.Builder
	for i, op := range h.Ops() {
		fmt.Fprintf(&b, "  %d: kind=%d bounds=%v color=%v radius=%v\n",
			i, op.Kind, op.Bounds, op.Color, op.CornerRadius)
	}
	return b.String()
}

// TestTintReplacesTheAccentOfEveryControlThatHasOne is the one modifier the
// four controls share beyond the minimal set, checked in one place so that a
// control which grows a .Tint that does nothing is caught.
//
// It asserts both directions: the accent is gone and the tint is there. Only
// the first half would pass for a control that painted nothing.
func TestTintReplacesTheAccentOfEveryControlThatHasOne(t *testing.T) {
	tint := ui.RGB(200, 30, 90)
	accent := ui.LightTheme().Color(ui.ColorAccent)
	for _, tc := range []struct {
		name string
		view gift.View
	}{
		{"toggle", ui.Toggle(true, nil).Tint(tint).Key("v")},
		{"slider", ui.Slider(0.7, nil).Tint(tint).Key("v")},
		{"progress bar", ui.ProgressBar(0.7).Tint(tint).Key("v")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{
				View: tc.view, Size: geom.Sz(300, 100), Theme: ui.LightTheme(),
			})
			var sawTint, sawAccent bool
			for _, op := range h.Ops() {
				switch op.Color {
				case tint:
					sawTint = true
				case accent:
					sawAccent = true
				}
			}
			if !sawTint {
				t.Fatalf("the tint %v is nowhere in the picture.\n%s", tint, formatOpsForTest(h))
			}
			if sawAccent {
				t.Fatalf("the themed accent %v is still painted next to the tint.\n%s",
					accent, formatOpsForTest(h))
			}
		})
	}
}
