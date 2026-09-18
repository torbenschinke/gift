package ui_test

import (
	"math"
	"strings"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// barFills returns the filled rounded rectangles of a progress bar: the track
// first and, when there is one, the fill after it.
func barFills(t testing.TB, h *gifttest.Harness) []geom.Rect {
	t.Helper()
	var out []geom.Rect
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRoundRect {
			out = append(out, op.Bounds)
		}
	}
	return out
}

// progressAt builds a determinate bar at f and returns the width of its filled
// part.
func progressAt(t testing.TB, f float64) float32 {
	t.Helper()
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.ProgressBar(f).Key("bar").Frame(200, geom.Unbounded())).Padding(20),
		Size: geom.Sz(300, 100),
	})
	fills := barFills(t, h)
	if len(fills) == 0 {
		t.Fatalf("a progress bar at %v drew nothing at all", f)
	}
	if len(fills) == 1 {
		return 0
	}
	return fills[1].Width()
}

// TestADeterminateProgressBarFillsInProportionToItsFraction is the surface a
// user meets first.
//
// It checks five fractions and not two, and it checks the *ratios* between
// them rather than each one against a constant, so that a bar which filled by
// some other monotonic function of the fraction — the square, say — fails here
// instead of passing on "0 is empty and 1 is full".
func TestADeterminateProgressBarFillsInProportionToItsFraction(t *testing.T) {
	full := progressAt(t, 1)
	if full <= 0 {
		t.Fatal("a bar at 1 has no fill at all")
	}
	if got := progressAt(t, 0); got != 0 {
		t.Fatalf("a bar at 0 has a fill %v wide", got)
	}
	for _, f := range []float64{0.25, 0.5, 0.75} {
		got := progressAt(t, f)
		want := float32(f) * full
		if !nearly(got, want) {
			t.Fatalf("a bar at %v is %v wide; %v of a full bar of %v is %v", f, got, f, full, want)
		}
	}
}

// TestAProgressBarClampsAFractionOutsideItsRange. A download whose declared
// size was a little short reports 1.02, and a bar that painted past its own
// right hand edge would be a cosmetic inaccuracy turned into a drawing bug.
func TestAProgressBarClampsAFractionOutsideItsRange(t *testing.T) {
	full := progressAt(t, 1)
	if got := progressAt(t, 1.4); got != full {
		t.Fatalf("a bar at 1.4 is %v wide, want the full %v", got, full)
	}
}

// TestAProgressBarRejectsANegativeFractionAndSaysWhatToWriteInstead is the
// other half of the asymmetry above, and it is a regression test for a defect
// that shipped in this project's own demo: ui.ProgressBar(-1) was accepted,
// clamped to zero, and drew a bar sitting at 0 % where its author meant an
// indeterminate one. -1 looks like an obvious sentinel and is not one.
//
// The message has to name the modifier, because the caller who writes -1 is by
// definition looking for the indeterminate mode and has not found it.
func TestAProgressBarRejectsANegativeFractionAndSaysWhatToWriteInstead(t *testing.T) {
	for _, v := range []float64{-1, -0.0001, -3} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("ui.ProgressBar(%v) was accepted; a negative fraction is a "+
						"mistake and used to be clamped to a bar at 0 %%", v)
					return
				}
				if msg, _ := r.(string); !strings.Contains(msg, "Indeterminate()") {
					t.Errorf("ui.ProgressBar(%v) panicked with %q, which does not name the "+
						"modifier the caller was reaching for", v, msg)
				}
			}()
			ui.ProgressBar(v)
		}()
	}
}

// TestAProgressBarRejectsAFractionThatIsNotANumber: a NaN reaches the backend
// as a NaN vertex position and empties the window, which is diagnosed three
// layers below the mistake if it is not rejected here.
func TestAProgressBarRejectsAFractionThatIsNotANumber(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ui.ProgressBar(%v) was accepted", v)
				}
			}()
			ui.ProgressBar(v)
		}()
	}
}

// TestAProgressBarIsNotAHitTargetAndLetsTapsThroughToWhatIsBehindIt.
//
// A progress bar is not a control. It takes no input and holds no focus, which
// is also why it is six logical pixels tall and has no [ui.ControlHitTarget]
// floor: there is nothing to hit.
func TestAProgressBarIsNotAHitTargetAndLetsTapsThroughToWhatIsBehindIt(t *testing.T) {
	clicks := 0
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.Button(ui.Text("under"), func() { clicks++ }).Key("under").Frame(200, 100),
			ui.ProgressBar(0.5).Key("bar").Frame(200, geom.Unbounded()),
		),
		Size: geom.Sz(300, 200),
		Font: loadTestFont(t),
	})
	bar := h.Find(gifttest.ByKey("bar")).Bounds()
	h.ClickAt(geom.Pt((bar.Min.X+bar.Max.X)/2, (bar.Min.Y+bar.Max.Y)/2))
	if clicks != 1 {
		t.Fatalf("the button under the progress bar got %d clicks; the bar is swallowing them", clicks)
	}
	h.Tab()
	h.AssertFocus(gifttest.ByKey("under"))
}

// TestADeterminateProgressBarDoesNotAnimateAndLetsTheApplicationSleep.
//
// This is the difference between the two modes stated as a test, and it is the
// half that protects the Pi: a determinate bar is a pure function of the
// fraction it was built with, so it asks for nothing.
func TestADeterminateProgressBarLetsTheApplicationSleep(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.ProgressBar(0.4).Key("bar"),
		Size: geom.Sz(300, 100),
	})
	h.Advance(ui.ControlAnimation * 4)
	if asksForFrames(h) {
		t.Fatal("a determinate progress bar is asking to be repainted although nothing about " +
			"it changes; it would hold the backend at its full tick rate")
	}
}

// TestAnIndeterminateProgressBarKeepsMovingAndKeepsAskingForFrames.
//
// The three claims are one claim: it re-arms its own enrolment from its
// painter, so it never stops, and that is the documented cost of the mode.
// The pill is sampled at three points of one period and all three must differ,
// which is what fails if the mode is drawn as a static bar.
func TestAnIndeterminateProgressBarKeepsMovingAndKeepsAskingForFrames(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.ProgressBar(0).Indeterminate().Key("bar").Frame(200, geom.Unbounded())).Padding(20),
		Size: geom.Sz(300, 100),
	})

	// A quarter of a period in, so that the pill is inside the bar: at the
	// very start of a period its trailing edge is exactly on the left edge of
	// the bar and there is nothing to draw, which is what "enters from
	// outside" means and is checked separately below.
	h.Advance(ui.ProgressPeriod / 4)

	var seen []geom.Rect
	for range 3 {
		fills := barFills(t, h)
		if len(fills) < 2 {
			t.Fatalf("an indeterminate bar drew %d fills; it drew no pill.\n%s",
				len(fills), formatOpsForTest(h))
		}
		seen = append(seen, fills[1])
		h.Advance(ui.ProgressPeriod / 4)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Fatalf("the pill was at %v and a quarter of a period later it was still at %v; "+
				"the indeterminate bar is not moving", seen[i-1], seen[i])
		}
	}
	if !(seen[1].Min.X > seen[0].Min.X && seen[2].Min.X > seen[1].Min.X) {
		t.Fatalf("the pill went %v, %v, %v; it must travel in one direction",
			seen[0].Min.X, seen[1].Min.X, seen[2].Min.X)
	}

	// Long after any bounded animation in this package would have expired.
	h.Advance(100 * ui.ControlAnimation)
	if !asksForFrames(h) {
		t.Fatal("an indeterminate progress bar stopped asking for frames; it would freeze " +
			"mid travel as soon as the backend dropped to its idle tick rate")
	}
}

// TestAnIndeterminatePillIsCutToTheBarAsItEntersAndLeaves, which is what makes
// it grow in and shrink out rather than appear whole at one edge.
func TestAnIndeterminatePillIsCutToTheBarAsItEntersAndLeaves(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.ProgressBar(0).Indeterminate().Key("bar").Frame(200, geom.Unbounded())).Padding(20),
		Size: geom.Sz(300, 100),
	})
	track := barFills(t, h)[0]
	var narrow bool
	for range 24 {
		fills := barFills(t, h)
		if len(fills) > 1 {
			p := fills[1]
			if p.Min.X < track.Min.X-0.01 || p.Max.X > track.Max.X+0.01 {
				t.Fatalf("the pill %v is outside the bar %v", p, track)
			}
			if p.Width() < track.Width()*0.3 {
				narrow = true
			}
		}
		h.Advance(ui.ProgressPeriod / 12)
	}
	if !narrow {
		t.Fatal("the pill was never cut short by an edge over a whole period, so it is not " +
			"entering and leaving; a clamp that did nothing would pass the bounds check above")
	}
}

// TestAProgressBarIsDrawnInSemanticColoursThatFollowTheTheme.
func TestAProgressBarIsDrawnInSemanticColoursThatFollowTheTheme(t *testing.T) {
	for _, tc := range []struct {
		name  string
		theme ui.Theme
	}{
		{"light", ui.LightTheme()},
		{"dark", ui.DarkTheme()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{
				View:  ui.ProgressBar(0.5).Key("bar"),
				Size:  geom.Sz(300, 100),
				Theme: tc.theme,
			})
			want := tc.theme.Color(ui.ColorAccent)
			found := false
			for _, op := range h.Ops() {
				if op.Kind == render.OpFillRoundRect && op.Color == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("the fill of a progress bar is not the %s theme's accent %v.\n%s",
					tc.name, want, formatOpsForTest(h))
			}
		})
	}
}

// BenchmarkProgressBarFrameIsAllocationFree is the 0 B/op contract for the
// determinate bar.
func BenchmarkProgressBarFrameIsAllocationFree(b *testing.B) {
	h := gifttest.New(b, gifttest.Options{
		View: ui.ProgressBar(0.4).Key("bar"),
		Size: geom.Sz(300, 100),
	})
	h.Frame()
	h.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Frame()
	}
}

// BenchmarkIndeterminateProgressBarFrameIsAllocationFree is the same for the
// mode that runs on every frame, which is the one whose per frame cost is
// actually paid sixty times a second.
func BenchmarkIndeterminateProgressBarFrameIsAllocationFree(b *testing.B) {
	h := gifttest.New(b, gifttest.Options{
		View: ui.ProgressBar(0).Indeterminate().Key("bar"),
		Size: geom.Sz(300, 100),
	})
	h.Frame()
	h.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Frame()
	}
}

// TestAnIndeterminateBarScrolledOutOfSightStillCostsAFullFrameRate is the
// unpleasant half of the cost paragraph of [ui.ProgressBarView], and it is
// here because that paragraph used to claim the opposite.
//
// gift has no paint culling: [gift.App.Paint] descends into every mounted node
// whether or not any of its pixels are inside a viewport. So a bar that has
// been scrolled out of the scroll view it lives in keeps being painted, keeps
// re-arming its own enrolment and keeps a kiosk at sixty frames a second while
// showing nothing. Documentation that told an application to scroll the bar
// away would be telling it to do nothing at all.
func TestAnIndeterminateBarScrolledOutOfSightStillCostsAFullFrameRate(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VScroll(
			ui.ProgressBar(0).Indeterminate().Key("bar").Frame(200, geom.Unbounded()),
			ui.Box().Frame(geom.Unbounded(), 2000).Key("filler"),
		).Key("scroll"),
		Size: geom.Sz(300, 100),
	})
	h.Find(gifttest.ByKey("filler")).ScrollIntoView()
	if h.Find(gifttest.ByKey("scroll")).ScrollOffset() == 0 {
		t.Fatal("nothing scrolled, so the bar is still in the viewport and this test would " +
			"pass for a gift that did cull")
	}
	bar := h.Find(gifttest.ByKey("bar")).Bounds()
	view := h.Find(gifttest.ByKey("scroll")).Bounds()
	if bar.Overlaps(view) {
		t.Fatalf("the bar is at %v and the viewport at %v; they still overlap", bar, view)
	}

	h.Advance(100 * ui.ControlAnimation)
	if !asksForFrames(h) {
		t.Fatal("an indeterminate bar that was scrolled out of sight stopped asking for " +
			"frames. If gift has grown paint culling then this is good news and the cost " +
			"paragraph of ProgressBarView, which says the opposite in as many words, has to " +
			"be rewritten with it")
	}
}

// TestRemovingAnIndeterminateBarFromTheTreeIsWhatStopsIt is the other side of
// the same sentence: unmounting the view is the only thing that works.
func TestRemovingAnIndeterminateBarFromTheTreeIsWhatStopsIt(t *testing.T) {
	show := true
	h := gifttest.New(t, gifttest.Options{
		Root: func(*gift.Context) gift.View {
			if show {
				return ui.VStack(ui.ProgressBar(0).Indeterminate().Key("bar")).Padding(20)
			}
			return ui.VStack(ui.Box().Key("nothing")).Padding(20)
		},
		Size: geom.Sz(300, 100),
	})
	h.Advance(ui.ProgressPeriod)
	if !asksForFrames(h) {
		t.Fatal("the bar was not asking for frames to begin with")
	}

	show = false
	h.App().Invalidate()
	h.Settle()
	h.Advance(100 * ui.ControlAnimation)
	if asksForFrames(h) {
		t.Fatal("an indeterminate bar that was removed from the tree is still asking for " +
			"frames; its enrolment outlived the node")
	}
}
