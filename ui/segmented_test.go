package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

var segmentLabels = []string{"Day", "Week", "Month", "Year"}

// segmentedApp is a segmented control bound to state, written the way an
// application would write it.
func segmentedApp(t testing.TB, initial int) func(*gift.Context) gift.View {
	font := loadTestFont(t)
	return func(ctx *gift.Context) gift.View {
		sel := ctx.State("tab", initial)
		i := ctx.Read(sel)
		return ui.VStack(
			ui.SegmentedControl(i, segmentLabels, func(want int) { sel.Set(want) }).
				Key("seg").Font(font).Label("Period"),
		).Padding(20)
	}
}

// indicatorOf is the sliding card: the second filled rounded rectangle, which
// the painter emits after the tray and before the labels.
func indicatorOf(t testing.TB, h *gifttest.Harness) geom.Rect {
	t.Helper()
	seen := 0
	for _, op := range h.Ops() {
		if op.Kind != render.OpFillRoundRect {
			continue
		}
		seen++
		if seen == 2 {
			return op.Bounds
		}
	}
	t.Fatalf("the segmented control drew %d filled rounded rectangles, so it drew no "+
		"indicator.\n%s", seen, formatOpsForTest(h))
	return geom.Rect{}
}

// settledIndicator is the indicator after any transition has finished.
func settledIndicator(t testing.TB, h *gifttest.Harness) geom.Rect {
	t.Helper()
	h.Advance(2 * ui.ControlAnimation)
	return indicatorOf(t, h)
}

// TestTappingEachSegmentSelectsThatOneAndNoOther is the surface a user meets
// first, and it is written against all four segments rather than two.
//
// Four matters. The failure this project has seen most often is a test whose
// name promises more than its body checks — "selects the right one of four"
// with a geometry in which two of the four are indistinguishable. So each
// segment is tapped by its own text, the reported index is checked, *and* the
// indicator is checked to be over that segment and over no other: the four
// answers are four different rectangles, in order, none overlapping.
func TestTappingEachSegmentSelectsThatOneAndNoOther(t *testing.T) {
	var got []int
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			i := ctx.Read(sel)
			return ui.VStack(
				ui.SegmentedControl(i, segmentLabels, func(want int) {
					got = append(got, want)
					sel.Set(want)
				}).Key("seg").Font(loadTestFont(t)),
			).Padding(20)
		},
		Size: geom.Sz(400, 120),
	})

	var seen []geom.Rect
	for i, label := range segmentLabels {
		h.TapAt(h.Find(gifttest.ByText(label)).Center())
		if i > 0 && (len(got) == 0 || got[len(got)-1] != i) {
			t.Fatalf("tapping %q reported %v, want the last entry to be %d", label, got, i)
		}
		r := settledIndicator(t, h)
		// The indicator must be over the segment whose text was tapped.
		text := h.Find(gifttest.ByText(label)).Bounds()
		if !r.Contains(geom.Pt((text.Min.X+text.Max.X)/2, (text.Min.Y+text.Max.Y)/2)) {
			t.Fatalf("after tapping %q the indicator is %v, which does not cover the label at %v",
				label, r, text)
		}
		for j, prev := range seen {
			if prev.Overlaps(r) {
				t.Fatalf("the indicator for segment %d is %v and for segment %d it was %v; "+
					"the two overlap, so this test could not tell them apart", i, r, j, prev)
			}
		}
		seen = append(seen, r)
	}
	if len(seen) != 4 {
		t.Fatalf("only %d segments were exercised", len(seen))
	}
	// And they are in left to right order, which is the other thing a
	// degenerate geometry would hide.
	for i := 1; i < len(seen); i++ {
		if !(seen[i].Min.X > seen[i-1].Min.X) {
			t.Fatalf("segment %d is at %v and segment %d at %v; they are not in order",
				i-1, seen[i-1], i, seen[i])
		}
	}
}

// TestTappingTheAlreadySelectedSegmentReportsNothing. A control that reported
// a change that did not happen would make every application rebuild on every
// tap of the current tab.
func TestTappingTheAlreadySelectedSegmentReportsNothing(t *testing.T) {
	reports := 0
	h := gifttest.New(t, gifttest.Options{
		View: ui.SegmentedControl(1, segmentLabels, func(int) { reports++ }).
			Key("seg").Font(loadTestFont(t)),
		Size: geom.Sz(400, 120),
	})
	h.TapAt(h.Find(gifttest.ByText("Week")).Center())
	if reports != 0 {
		t.Fatalf("tapping the selected segment reported %d changes", reports)
	}
	h.TapAt(h.Find(gifttest.ByText("Year")).Center())
	if reports != 1 {
		t.Fatalf("tapping another segment reported %d changes, want one", reports)
	}
}

// TestAFocusedSegmentedControlWalksWithTheArrowKeysAndStopsAtTheEnds.
func TestAFocusedSegmentedControlWalksWithTheArrowKeysAndStopsAtTheEnds(t *testing.T) {
	var got []int
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			i := ctx.Read(sel)
			return ui.SegmentedControl(i, segmentLabels, func(want int) {
				got = append(got, want)
				sel.Set(want)
			}).Key("seg").Font(loadTestFont(t))
		},
		Size: geom.Sz(400, 120),
	})
	h.Tab()
	h.AssertFocus(gifttest.ByKey("seg"))

	h.Key(gift.KeyRight)
	h.Key(gift.KeyRight)
	if last(got) != 2 {
		t.Fatalf("two presses of right from segment 0 reported %v, want it to end at 2", got)
	}
	h.Key(gift.KeyLeft)
	if last(got) != 1 {
		t.Fatalf("left reported %v, want it to end at 1", got)
	}
	h.Key(gift.KeyEnd)
	if last(got) != 3 {
		t.Fatalf("end reported %v, want the last segment", got)
	}
	n := len(got)
	h.Key(gift.KeyRight)
	if len(got) != n {
		t.Fatalf("right at the last segment reported %v; the selection must stop rather "+
			"than wrap, or a held key would cycle for ever", got)
	}
	h.Key(gift.KeyHome)
	if last(got) != 0 {
		t.Fatalf("home reported %v, want the first segment", got)
	}
}

// TestTheIndicatorSlidesBetweenSegmentsAndThenStops is the animation and its
// end, in one test, because the two halves are one promise.
func TestTheIndicatorSlidesBetweenSegmentsAndThenStops(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: segmentedApp(t, 0), Size: geom.Sz(400, 120)})
	first := indicatorOf(t, h)

	h.TapAt(h.Find(gifttest.ByText("Month")).Center())
	h.Advance(ui.ControlAnimation / 2)
	mid := indicatorOf(t, h)
	last := settledIndicator(t, h)

	if !(last.Min.X > first.Min.X) {
		t.Fatalf("selecting a segment to the right moved the indicator from %v to %v", first, last)
	}
	if !(mid.Min.X > first.Min.X && mid.Min.X < last.Min.X) {
		t.Fatalf("halfway through the transition the indicator was at %v, which is not "+
			"strictly between %v and %v: it jumped rather than slid", mid, first, last)
	}
	if !nearly(mid.Width(), first.Width()) {
		t.Fatalf("the indicator changed width from %v to %v while travelling; the segments "+
			"are equal and so is the card", first.Width(), mid.Width())
	}
	if asksForFrames(h) {
		t.Fatal("the segmented control is still asking for frames after its transition ended")
	}
}

// TestASegmentedControlWithAnOutOfRangeSelectionDrawsNoIndicator: the honest
// picture of a model that is not one of the choices.
func TestASegmentedControlWithAnOutOfRangeSelectionDrawsNoIndicator(t *testing.T) {
	withSel := gifttest.New(t, gifttest.Options{
		View: ui.SegmentedControl(1, segmentLabels, nil).Key("seg").Font(loadTestFont(t)),
		Size: geom.Sz(400, 120),
	})
	none := gifttest.New(t, gifttest.Options{
		View: ui.SegmentedControl(-1, segmentLabels, nil).Key("seg").Font(loadTestFont(t)),
		Size: geom.Sz(400, 120),
	})
	countFills := func(h *gifttest.Harness) int {
		n := 0
		for _, op := range h.Ops() {
			if op.Kind == render.OpFillRoundRect {
				n++
			}
		}
		return n
	}
	a, b := countFills(withSel), countFills(none)
	if !(a > b) {
		t.Fatalf("a control with a selection emits %d filled rounded rectangles and one "+
			"without emits %d; the indicator is not being left out", a, b)
	}
}

// TestASegmentedControlSharesItsWidthEqually. Segments that resize with the
// selection move under the finger that is about to tap them.
func TestASegmentedControlSharesItsWidthEqually(t *testing.T) {
	centres := func(selected int) []float32 {
		h := gifttest.New(t, gifttest.Options{
			View: ui.SegmentedControl(selected, segmentLabels, nil).
				Key("seg").Font(loadTestFont(t)).Frame(400, geom.Unbounded()),
			Size: geom.Sz(500, 120),
		})
		var out []float32
		for _, l := range segmentLabels {
			r := h.Find(gifttest.ByText(l)).Bounds()
			out = append(out, (r.Min.X+r.Max.X)/2)
		}
		return out
	}
	a, b := centres(0), centres(3)
	c := a
	for i := range a {
		if !nearly(a[i], b[i]) {
			t.Fatalf("label %d is centred at x=%v when segment 0 is selected and at %v when "+
				"segment 3 is; the segments move with the selection", i, a[i], b[i])
		}
	}
	// And the columns really are equal: the label *centres* are equally
	// spaced. The origins are not, because the four words are not the same
	// width, and a test that compared those would be asserting the metrics of
	// the test font instead of the layout of the control.
	for i := 2; i < len(c); i++ {
		if !nearly(c[i]-c[i-1], c[1]-c[0]) {
			t.Fatalf("the label centres are %v, which are not equally spaced", c)
		}
	}
}

// TestASegmentedControlIsOneFocusStopAndNotFourIsTheWholePointOfTheWidget.
func TestASegmentedControlIsOneFocusStopAndNotFour(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.SegmentedControl(0, segmentLabels, nil).Key("seg").Font(loadTestFont(t)),
			ui.Button(ui.Text("after"), nil).Key("after"),
		).Padding(20),
		Size: geom.Sz(400, 200),
		Font: loadTestFont(t),
	})
	h.Tab()
	h.AssertFocus(gifttest.ByKey("seg"))
	h.Tab()
	h.AssertFocus(gifttest.ByKey("after"))
}

// TestSegmentedControlPanicsWithNoLabels: an empty one has no size, no
// behaviour and no reading, and a silent empty box is diagnosed somewhere else.
func TestSegmentedControlPanicsWithNoLabels(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ui.SegmentedControl accepted an empty slice of labels")
		}
	}()
	ui.SegmentedControl(0, nil, nil)
}

// TestASegmentedControlReadsItsLabelsAtBuildAndNotAfterwards is the ownership
// rule of the project plan, section 4, in both directions.
//
// The slice belongs to the caller. The control turns it into child views
// during the build and keeps none of it, so mutating it afterwards changes
// nothing that is already on screen — and the next build picks the new strings
// up, which is what makes the first half a snapshot rather than a copy nobody
// refreshes.
func TestASegmentedControlReadsItsLabelsAtBuildAndNotAfterwards(t *testing.T) {
	labels := []string{"One", "Two"}
	rebuilds := 0
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			rebuilds++
			return ui.SegmentedControl(0, labels, nil).Key("seg").Font(loadTestFont(t))
		},
		Size: geom.Sz(400, 120),
	})
	labels[1] = "Changed"
	h.Frame()
	h.AssertExists(gifttest.ByText("Two"))
	h.AssertNone(gifttest.ByText("Changed"))

	before := rebuilds
	h.App().Invalidate()
	h.Settle()
	if rebuilds == before {
		t.Fatal("the invalidation did not rebuild anything, so the second half of this test " +
			"asserts nothing")
	}
	h.AssertExists(gifttest.ByText("Changed"))
	h.AssertNone(gifttest.ByText("Two"))
}

// BenchmarkSegmentedControlFrameIsAllocationFree is the 0 B/op contract for a
// segmented control whose indicator is in the middle of a transition, which is
// the expensive state.
func BenchmarkSegmentedControlFrameIsAllocationFree(b *testing.B) {
	h := gifttest.New(b, gifttest.Options{Root: segmentedApp(b, 0), Size: geom.Sz(400, 120)})
	h.TapAt(h.Find(gifttest.ByText("Month")).Center())
	h.Advance(ui.ControlAnimation / 2)
	h.Frame()
	h.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Frame()
	}
}

func last(xs []int) int {
	if len(xs) == 0 {
		return -1
	}
	return xs[len(xs)-1]
}

// TestAFingerThatSlidesOntoAnotherSegmentBeforeLiftingSelectsTheOneItLiftedOn
// is the gesture the type documentation of [ui.SegmentedControlView] promises,
// and it used to be impossible.
//
// [gift.DragSlop] is eight logical pixels. Segments on this control are about
// ninety wide, so sliding from one to the next is always a drag by gift's
// reckoning — and a handler that declined a drag therefore selected nothing at
// all for the very gesture the prose advertised. On the 1920x1080 panel this
// project targets eight pixels is about 1.3 millimetres, so the same handler
// also lost an ordinary tap whenever the finger rolled while lifting.
func TestAFingerThatSlidesOntoAnotherSegmentBeforeLiftingSelectsTheOneItLiftedOn(t *testing.T) {
	var got []int
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.VStack(
				ui.SegmentedControl(ctx.Read(sel), segmentLabels, func(want int) {
					got = append(got, want)
					sel.Set(want)
				}).Key("seg").Font(loadTestFont(t)),
			).Padding(20)
		},
		Size: geom.Sz(400, 120),
	})
	wrong := h.Find(gifttest.ByText("Day")).Center()
	right := h.Find(gifttest.ByText("Month")).Center()
	if !(right.X-wrong.X > gift.DragSlop) {
		t.Fatalf("the two segments are only %v apart, which is inside the drag slop of %v, so "+
			"this test would pass without the behaviour it is about",
			right.X-wrong.X, gift.DragSlop)
	}

	h.PressAt(wrong)
	h.MoveTo(geom.Pt((wrong.X+right.X)/2, wrong.Y))
	h.MoveTo(right)
	h.ReleaseAt(right)

	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("a finger that came down on %q and lifted on %q reported %v, want the index "+
			"of the segment it lifted on", "Day", "Month", got)
	}
}

// TestAFingerThatLeavesASegmentedControlBeforeLiftingSelectsNothing is the
// other half: the release position decides, and a release that is not on the
// control is not a choice.
func TestAFingerThatLeavesASegmentedControlBeforeLiftingSelectsNothing(t *testing.T) {
	var got []int
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.VStack(
				ui.SegmentedControl(ctx.Read(sel), segmentLabels, func(want int) {
					got = append(got, want)
					sel.Set(want)
				}).Key("seg").Font(loadTestFont(t)),
			).Padding(20)
		},
		Size: geom.Sz(400, 120),
	})
	b := h.Find(gifttest.ByKey("seg")).Bounds()
	h.PressAt(h.Find(gifttest.ByText("Month")).Center())
	h.ReleaseAt(geom.Pt(b.Max.X+30, b.Max.Y+30))
	if len(got) != 0 {
		t.Fatalf("a finger that lifted off the control reported %v", got)
	}
}

// TestAVerticalDragThatBeginsOnASegmentedControlScrollsAndSelectsNothing is
// the case that pays for dropping the drag flag from the release.
//
// A drag flag is not what keeps a scroll gesture from selecting; the scroll
// container is. It recognises the swipe on the *move*, steals the pointer, and
// the steal sends the control an EventPointerCancel and redirects the release
// to the scroller — so the control never sees a release at all. The viewport
// must move and nothing must be selected.
func TestAVerticalDragThatBeginsOnASegmentedControlScrollsAndSelectsNothing(t *testing.T) {
	var got []int
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.VScroll(
				ui.Box().Frame(geom.Unbounded(), 200).Key("above"),
				ui.SegmentedControl(ctx.Read(sel), segmentLabels, func(want int) {
					got = append(got, want)
					sel.Set(want)
				}).Key("seg").Font(loadTestFont(t)),
				ui.Box().Frame(geom.Unbounded(), 400).Key("below"),
			).Key("scroll")
		},
		Size: geom.Sz(400, 200),
	})
	h.Find(gifttest.ByKey("seg")).ScrollIntoView()
	before := h.Find(gifttest.ByKey("scroll")).ScrollOffset()

	start := h.Find(gifttest.ByText("Month")).Center()
	h.PressAt(start)
	for i := 1; i <= 6; i++ {
		h.MoveTo(geom.Pt(start.X, start.Y-float32(i)*12))
	}
	// And back down, so that the finger lifts *on* the control again: the
	// content has scrolled with it, so this point is over a segment. Without
	// the steal this release would therefore be an ordinary selecting one,
	// which is what makes the assertion below load bearing rather than an
	// accident of where the finger happened to end up.
	h.MoveTo(geom.Pt(start.X, start.Y-6))
	h.ReleaseAt(geom.Pt(start.X, start.Y-6))

	if len(got) != 0 {
		t.Fatalf("a vertical swipe that began on the control selected %v", got)
	}
	if after := h.Find(gifttest.ByKey("scroll")).ScrollOffset(); after == before {
		t.Fatalf("the scroll view did not move (%v) during a vertical swipe that began on a "+
			"segmented control, so this test did not exercise the scroll case at all", before)
	}
}

// TestTheVerticalArrowsWalkASegmentedControlTheWayAnOrdinaryListDoes is the
// other half of the axis pair; see
// TestTheVerticalArrowsMoveASliderTheWayUpMeansMore in slider_test.go.
//
// Up is the previous segment and down the next, which is the opposite of the
// slider's mapping and is meant to be: a selection is a position among ordered
// items and up means earlier in every list, menu and table, while a slider's
// value is a magnitude and up means more.
func TestTheVerticalArrowsWalkASegmentedControlTheWayAnOrdinaryListDoes(t *testing.T) {
	var got []int
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 0)
			return ui.SegmentedControl(ctx.Read(sel), segmentLabels, func(want int) {
				got = append(got, want)
				sel.Set(want)
			}).Key("seg").Font(loadTestFont(t))
		},
		Size: geom.Sz(400, 120),
	})
	h.Tab()
	h.AssertFocus(gifttest.ByKey("seg"))

	h.Key(gift.KeyDown)
	h.Key(gift.KeyDown)
	if last(got) != 2 {
		t.Fatalf("two presses of down from segment 0 reported %v; down must be the next "+
			"segment, exactly as right is", got)
	}
	h.Key(gift.KeyUp)
	if last(got) != 1 {
		t.Fatalf("up reported %v; up must be the previous segment", got)
	}
	h.Key(gift.KeyHome)
	n := len(got)
	h.Key(gift.KeyUp)
	if len(got) != n {
		t.Fatalf("up at the first segment reported %v; it must stop rather than wrap", got)
	}
}
