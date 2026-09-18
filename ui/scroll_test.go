package ui_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// The scroll scene every test in this file works on: a viewport 200 high over
// twenty rows of 100, so the content is 2000 and the maximum offset is 1800.
// The numbers are round on purpose — a failure that says "offset 700, want
// 600" is a sentence, and one that says "offset 713.4" is an investigation.
const (
	scrollViewportW, scrollViewportH = 300, 200
	scrollRowH                       = 100
	scrollRows                       = 20
	scrollContentH                   = scrollRows * scrollRowH
	scrollMaxOffset                  = scrollContentH - scrollViewportH
)

func scrollRowKey(i int) string { return "row" + strconv.Itoa(i) }

// scrollScene is a plain vertical scroller of coloured rows.
func scrollScene() gift.View {
	rows := make([]gift.View, 0, scrollRows)
	for i := range scrollRows {
		rows = append(rows, ui.Box().
			Frame(scrollViewportW, scrollRowH).
			Background(ui.RGB(uint8(10*i), 40, 60)).
			Key(scrollRowKey(i)))
	}
	return ui.VStack(
		ui.VScroll(rows...).Frame(scrollViewportW, scrollViewportH).Key("scroller"),
	)
}

func scrollHarness(t *testing.T) *gifttest.Harness {
	t.Helper()
	return gifttest.New(t, gifttest.Options{View: scrollScene()})
}

func scroller(h *gifttest.Harness) gifttest.Node { return h.Find(gifttest.ByKey("scroller")) }

// TestScrollIsGeometry is the shape of the scene, asserted once so that every
// other test in the file can quote the numbers.
func TestScrollIsGeometry(t *testing.T) {
	h := scrollHarness(t)
	info := scroller(h).ScrollInfo()
	if info.Axis != gift.ScrollVertical {
		t.Errorf("axis = %v, want vertical", info.Axis)
	}
	if info.ContentExtent != scrollContentH {
		t.Errorf("content extent = %g, want %d", info.ContentExtent, scrollContentH)
	}
	if info.ViewportExtent != scrollViewportH {
		t.Errorf("viewport extent = %g, want %d", info.ViewportExtent, scrollViewportH)
	}
	if info.MaxOffset != scrollMaxOffset {
		t.Errorf("max offset = %g, want %d", info.MaxOffset, scrollMaxOffset)
	}
}

// TestScrollNeitherBuildsNorLayouts is the single most important property of
// this work unit: the project plan, section 6, promises that pure scrolling
// costs a transform patch and nothing else, and the whole performance argument
// of the gallery rests on it. It is asserted on the counters and not on a
// timing.
func TestScrollNeitherBuildsNorLayouts(t *testing.T) {
	h := scrollHarness(t)
	s := scroller(h)

	before := h.Diagnostics()
	for range 10 {
		s.ScrollBy(37)
	}
	after := h.Diagnostics()

	if got := after.Builds - before.Builds; got != 0 {
		t.Errorf("scrolling rebuilt %d scope(s), want 0", got)
	}
	if got := after.Layouts - before.Layouts; got != 0 {
		t.Errorf("scrolling ran %d layouter(s), want 0", got)
	}
	if got := after.Scrolls - before.Scrolls; got != 10 {
		t.Errorf("Diagnostics.Scrolls moved by %d, want 10 — the offset did not actually change", got)
	}
	s.AssertScrollOffset(370)
}

// TestScrollTranslatesInsteadOfMoving is the other half of the same claim: the
// layout rectangle of a row does not move, the device rectangle does.
func TestScrollTranslatesInsteadOfMoving(t *testing.T) {
	h := scrollHarness(t)
	row := h.Find(gifttest.ByKey(scrollRowKey(3)))
	layoutBefore := row.LayoutBounds()

	scroller(h).ScrollBy(250)

	if got := row.LayoutBounds(); got != layoutBefore {
		t.Errorf("the layout bounds moved from %v to %v; a scroll must not relayout", layoutBefore, got)
	}
	// Row 3 starts at document y 300 and the viewport starts at y 0, so at
	// offset 250 its top edge is at device y 50.
	if got := row.Bounds().Min.Y; got != 50 {
		t.Errorf("device top = %g, want 50", got)
	}
}

// TestHitTestingFollowsTheScroll covers the input half of the same transform:
// a node scrolled up by N is hit at its new position, not at its old one, and
// a node scrolled out of the viewport is not hit at all.
func TestHitTestingFollowsTheScroll(t *testing.T) {
	buttons := make([]gift.View, 0, scrollRows)
	for i := range scrollRows {
		buttons = append(buttons, ui.Button(ui.Box().Frame(10, 10), nil).
			Frame(scrollViewportW, scrollRowH).
			Key(scrollRowKey(i)))
	}
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(buttons...).Frame(scrollViewportW, scrollViewportH).Key("scroller"),
	)})

	// Row 1 spans document y 100..200 and is visible at 100..200 initially.
	old := geom.Pt(150, 150)
	row1 := h.Find(gifttest.ByKey(scrollRowKey(1)))
	if got := h.At(old); got.IsZero() || got.Key() != scrollRowKey(1) {
		t.Fatalf("before the scroll, (150,150) should hit row1, got %s", got.Describe())
	}

	scroller(h).ScrollBy(100)

	// It now sits at device 0..100, so the old point belongs to row 2.
	if got := h.At(old); got.IsZero() || got.Key() != scrollRowKey(2) {
		t.Errorf("after scrolling by 100, (150,150) should hit row2, got %s", got.Describe())
	}
	if got := h.At(geom.Pt(150, 50)); got.IsZero() || got.Key() != scrollRowKey(1) {
		t.Errorf("after scrolling by 100, (150,50) should hit row1, got %s", got.Describe())
	}
	_ = row1

	// Row 0 has left the viewport entirely and must not be hit anywhere.
	row0 := h.Find(gifttest.ByKey(scrollRowKey(0)))
	row0.AssertNotVisible()
	for y := float32(0); y < scrollViewportH; y += 10 {
		if got := h.At(geom.Pt(150, y)); !got.IsZero() && got.Key() == scrollRowKey(0) {
			t.Fatalf("row0 is scrolled out but was hit at y=%g", y)
		}
	}
}

// TestScrolledOutContentIsClippedAway asserts on the display list: the rows
// outside the viewport are still emitted — gift does not cull — but every one
// of them carries a clip rectangle that excludes it, so the backend draws
// nothing. That is the contract the renderer's SkippedOutsideClip counter
// exists for.
func TestScrolledOutContentIsClippedAway(t *testing.T) {
	h := scrollHarness(t)
	scroller(h).ScrollBy(500)

	l := h.List()
	viewport := geom.Rc(0, 0, scrollViewportW, scrollViewportH)
	visible, hidden := 0, 0
	for _, op := range l.Ops() {
		if op.Kind != render.OpFillRect {
			continue
		}
		// The operation's bounds are local; the clip is device space. Map
		// the one through the other to ask what a backend would draw.
		dev := l.Xform(op.Xform).TransformRect(op.Bounds)
		if op.Clip == 0 {
			t.Errorf("a row was emitted unclipped: %v", op.Bounds)
			continue
		}
		clip := l.Clip(op.Clip)
		if !clip.Intersect(viewport).IsEmpty() && clip.Min.Y < 0 {
			t.Errorf("the viewport clip %v reaches outside the viewport", clip)
		}
		if dev.Intersect(clip).IsEmpty() {
			hidden++
		} else {
			visible++
		}
	}
	// 500 shows rows 5 and 6 and the top of 7 — three rows intersect the
	// 200 pixel viewport at an offset that is a multiple of 100 plus nothing,
	// so exactly two do.
	if visible != 2 {
		t.Errorf("%d rows survive the clip, want 2", visible)
	}
	if hidden != scrollRows-2 {
		t.Errorf("%d rows are clipped away, want %d", hidden, scrollRows-2)
	}
}

// TestContentSmallerThanViewportPinsToTheStart covers the documented answer to
// "what if there is nothing to scroll": the offset is pinned at zero, the
// content sits at the leading edge rather than being centred or stretched, and
// the container consumes no wheel event so that an outer one still can.
func TestContentSmallerThanViewportPinsToTheStart(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(
			ui.Box().Frame(scrollViewportW, 50).Background(ui.RGB(1, 2, 3)).Key("only"),
		).Frame(scrollViewportW, scrollViewportH).Key("scroller"),
	)})
	s := scroller(h)
	info := s.ScrollInfo()
	if info.MaxOffset != 0 {
		t.Errorf("max offset = %g, want 0 for content smaller than the viewport", info.MaxOffset)
	}
	if got := h.Find(gifttest.ByKey("only")).Bounds().Min.Y; got != 0 {
		t.Errorf("the content sits at y=%g, want 0: it is pinned to the leading edge", got)
	}
	// Nothing moves, in either direction.
	s.ScrollBy(500).AssertScrollOffset(0)
	s.ScrollBy(-500).AssertScrollOffset(0)
	h.Wheel(geom.Pt(150, 100), geom.Pt(0, -10))
	s.AssertScrollOffset(0)
}

// TestScrollingIsNotOverflow is the distinction between "the content is taller
// than the viewport", which is what a scroller is for, and an overflow, which
// is a container lying about its size. Only the second is reported.
func TestScrollingIsNotOverflow(t *testing.T) {
	h := scrollHarness(t)
	h.AssertNoOverflow()
	if got := h.Diagnostics().OverflowNodes; got != 0 {
		t.Fatalf("a scroller with ten times its own height of content reports %d overflowing node(s), "+
			"want 0: the content is reachable by scrolling", got)
	}

	// The cross axis is a different matter: nothing can bring a child that is
	// wider than a vertical viewport into view, so that is an honest overflow.
	wide := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(
			ui.Box().Frame(scrollViewportW*2, 50).Background(ui.RGB(1, 2, 3)),
		).Frame(scrollViewportW, scrollViewportH).Key("scroller"),
	)})
	if got := wide.Diagnostics().OverflowNodes; got == 0 {
		t.Error("a child twice as wide as a vertical viewport is not reported as an overflow, but it is one: " +
			"there is no horizontal offset that could bring it into view")
	}
}

// --- input -------------------------------------------------------------------

// TestWheelScrollsTheNearestScroller is the plain case, with the pointer over
// a leaf deep inside the container rather than over the container itself.
func TestWheelScrollsTheNearestScroller(t *testing.T) {
	h := scrollHarness(t)
	s := scroller(h)
	// Negative Y is the wheel pulled towards the user, which moves forward
	// through the document.
	h.Wheel(geom.Pt(150, 100), geom.Pt(0, -2))
	s.AssertScrollOffset(2 * float64(gift.DefaultScrollWheelStep))
	h.Wheel(geom.Pt(150, 100), geom.Pt(0, 2))
	s.AssertScrollOffset(0)
}

// TestWheelChainsToTheOuterScrollerAtTheLimit is the overscroll rule: the
// nearest scrollable ancestor consumes the event while it can still move, and
// the moment it cannot the event bubbles to the next one out. The rule is per
// direction, which the second half asserts.
func TestWheelChainsToTheOuterScrollerAtTheLimit(t *testing.T) {
	inner := make([]gift.View, 0, 4)
	for i := range 4 {
		inner = append(inner, ui.Box().Frame(200, 100).
			Background(ui.RGB(uint8(20*i), 0, 0)).Key("inner"+strconv.Itoa(i)))
	}
	outerRows := []gift.View{
		ui.VScroll(inner...).Frame(200, 200).Key("inner-scroller"),
		ui.Box().Frame(200, 600).Background(ui.RGB(0, 90, 0)).Key("tail"),
	}
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(outerRows...).Frame(200, 300).Key("outer-scroller"),
	)})
	in := h.Find(gifttest.ByKey("inner-scroller"))
	out := h.Find(gifttest.ByKey("outer-scroller"))

	// Inner content 400, viewport 200, so max offset 200. Outer content 800,
	// viewport 300, so max offset 500.
	at := geom.Pt(100, 100) // inside the inner viewport

	h.Wheel(at, geom.Pt(0, -10)) // far more than the inner can take
	in.AssertScrollOffset(200)
	out.AssertScrollOffset(0)
	if in.ScrollInfo().MaxOffset != 200 {
		t.Fatalf("the inner scroller has max offset %g, the test assumes 200", in.ScrollInfo().MaxOffset)
	}

	// The inner one is now at its limit downwards, so the next notch chains.
	h.Wheel(at, geom.Pt(0, -1))
	in.AssertScrollOffset(200)
	out.AssertScrollOffset(float64(gift.DefaultScrollWheelStep))

	// The rule is per direction: the inner one is not at its limit upwards
	// and takes the event back.
	h.Wheel(at, geom.Pt(0, 1))
	in.AssertScrollOffset(200 - float64(gift.DefaultScrollWheelStep))
	out.AssertScrollOffset(float64(gift.DefaultScrollWheelStep))
}

// TestWheelOnTheWrongAxisChains covers the other half of the rule: a
// horizontal wheel over a vertical scroller has no component along its axis,
// so it is not consumed and reaches whatever else wants it.
func TestWheelOnTheWrongAxisChains(t *testing.T) {
	cols := make([]gift.View, 0, 4)
	for i := range 4 {
		cols = append(cols, ui.Box().Frame(200, 200).Background(ui.RGB(0, uint8(20*i), 0)))
	}
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.HScroll(
			ui.VScroll(
				ui.Box().Frame(200, 600).Background(ui.RGB(1, 2, 3)),
			).Frame(200, 200).Key("vertical"),
			cols[0], cols[1], cols[2],
		).Frame(400, 200).Key("horizontal"),
	)})
	v := h.Find(gifttest.ByKey("vertical"))
	hz := h.Find(gifttest.ByKey("horizontal"))

	h.Wheel(geom.Pt(100, 100), geom.Pt(-1, 0))
	v.AssertScrollOffset(0)
	hz.AssertScrollOffset(float64(gift.DefaultScrollWheelStep))

	h.Wheel(geom.Pt(100, 100), geom.Pt(0, -1))
	v.AssertScrollOffset(float64(gift.DefaultScrollWheelStep))
}

// buttonScroller is the scene for the drag-versus-tap tests: a scroller full
// of buttons, so that every press lands on something that would very much like
// to activate.
func buttonScroller(t *testing.T, clicks *int) *gifttest.Harness {
	t.Helper()
	rows := make([]gift.View, 0, scrollRows)
	for i := range scrollRows {
		rows = append(rows, ui.Button(ui.Box().Frame(10, 10), func() { *clicks++ }).
			Frame(scrollViewportW, scrollRowH).
			Key(scrollRowKey(i)))
	}
	return gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(rows...).Frame(scrollViewportW, scrollViewportH).Key("scroller"),
	)})
}

// TestDragTakesThePressAwayFromTheButton is the classic mobile interaction: a
// finger comes down on a button, turns into a scroll, and the button must not
// activate and must not stay pressed.
func TestDragTakesThePressAwayFromTheButton(t *testing.T) {
	clicks := 0
	h := buttonScroller(t, &clicks)
	s := scroller(h)
	row := h.Find(gifttest.ByKey(scrollRowKey(0)))

	// A swipe of 80 pixels, which is ten times the drag slop.
	row.Swipe(geom.Pt(0, -80))

	if clicks != 0 {
		t.Errorf("the button activated %d time(s); a drag past the slop must take the press away", clicks)
	}
	row.AssertNotPressed()
	if got := s.ScrollOffset(); got <= 0 {
		t.Errorf("the container did not scroll (offset %g); the drag went nowhere", got)
	}
}

// TestTapInsideTheScrollerStillActivates is the other side of the same coin.
// A gesture that never leaves the slop is a tap and must reach the button.
func TestTapInsideTheScrollerStillActivates(t *testing.T) {
	clicks := 0
	h := buttonScroller(t, &clicks)
	h.Find(gifttest.ByKey(scrollRowKey(0))).Tap()
	if clicks != 1 {
		t.Errorf("the button activated %d time(s), want 1: a tap inside a scroller is still a tap", clicks)
	}
	scroller(h).AssertScrollOffset(0)
}

// TestDragBelowTheSlopDoesNotScroll pins the threshold itself: a drag shorter
// than [gift.DragSlop] is not a scroll either.
func TestDragBelowTheSlopDoesNotScroll(t *testing.T) {
	clicks := 0
	h := buttonScroller(t, &clicks)
	h.Find(gifttest.ByKey(scrollRowKey(0))).Swipe(geom.Pt(0, -4))
	scroller(h).AssertScrollOffset(0)
}

// --- kinetic scrolling -------------------------------------------------------

// TestFlingDecaysAndStops drives a kinetic scroll entirely from the injected
// clock: no sleep, no wall time, and the same numbers on every machine.
func TestFlingDecaysAndStops(t *testing.T) {
	h := scrollHarness(t)
	s := scroller(h)

	// 160 pixels in 80 ms is 2000 px/s upwards, which with the default
	// friction of 4 has 500 pixels of travel left in it.
	h.Find(gifttest.ByKey(scrollRowKey(0))).Fling(geom.Pt(0, -160), 80*time.Millisecond)

	info := s.ScrollInfo()
	if !info.Flinging {
		t.Fatalf("the release did not start a fling: %+v", info)
	}
	if info.Velocity <= 0 {
		t.Fatalf("the fling velocity is %g, want a positive value: a finger moving up scrolls forward",
			info.Velocity)
	}

	afterDrag := info.Offset
	prev := afterDrag
	// Let it run. Each Advance is one tick of the friction curve.
	for range 200 {
		h.Advance(16 * time.Millisecond)
		cur := s.ScrollInfo()
		if cur.Offset < prev {
			t.Fatalf("the fling reversed: %g then %g", prev, cur.Offset)
		}
		prev = cur.Offset
		if !cur.Flinging {
			break
		}
	}
	final := s.ScrollInfo()
	if final.Flinging {
		t.Fatalf("the fling never stopped; it is at %g after 3.2 s", final.Offset)
	}
	if final.Velocity != 0 {
		t.Errorf("a stopped fling has velocity %g, want 0", final.Velocity)
	}
	if travelled := final.Offset - afterDrag; travelled < 300 || travelled > 600 {
		t.Errorf("the fling travelled %g pixels; with v0=2000 and friction 4 the closed form says "+
			"about 500, and anything far off means the integration is wrong", travelled)
	}
}

// TestFlingIsDeterministic runs the same gesture twice and demands the same
// numbers, which is the property the injected clock exists for.
func TestFlingIsDeterministic(t *testing.T) {
	run := func() float64 {
		h := scrollHarness(t)
		h.Find(gifttest.ByKey(scrollRowKey(0))).Fling(geom.Pt(0, -160), 80*time.Millisecond)
		for range 200 {
			h.Advance(16 * time.Millisecond)
			if !scroller(h).ScrollInfo().Flinging {
				break
			}
		}
		return scroller(h).ScrollOffset()
	}
	a, b := run(), run()
	if a != b {
		t.Errorf("two identical flings ended at %g and %g", a, b)
	}
}

// TestFlingStopsAtTheBound covers the other exit of the loop: a fling that
// reaches the end of the document stops there rather than running the curve
// out against a clamp.
func TestFlingStopsAtTheBound(t *testing.T) {
	h := scrollHarness(t)
	s := scroller(h)
	s.ScrollTo(scrollMaxOffset - 20)

	h.Find(gifttest.ByKey(scrollRowKey(scrollRows-1))).Fling(geom.Pt(0, -160), 80*time.Millisecond)
	for range 200 {
		h.Advance(16 * time.Millisecond)
		if !s.ScrollInfo().Flinging {
			break
		}
	}
	info := s.ScrollInfo()
	if info.Flinging {
		t.Error("the fling is still running at the end of the document")
	}
	if info.Offset != info.MaxOffset {
		t.Errorf("offset %g, want the maximum %g", info.Offset, info.MaxOffset)
	}
}

// TestFlingNeitherBuildsNorLayouts is the scroll invariant again, this time
// for the frames a kinetic animation produces.
func TestFlingNeitherBuildsNorLayouts(t *testing.T) {
	h := scrollHarness(t)
	h.Find(gifttest.ByKey(scrollRowKey(0))).Fling(geom.Pt(0, -160), 80*time.Millisecond)

	before := h.Diagnostics()
	ticks := 0
	for range 200 {
		h.Advance(16 * time.Millisecond)
		ticks++
		if !scroller(h).ScrollInfo().Flinging {
			break
		}
	}
	after := h.Diagnostics()
	if ticks < 10 {
		t.Fatalf("the fling only ran for %d ticks, the measurement is meaningless", ticks)
	}
	if got := after.Builds - before.Builds; got != 0 {
		t.Errorf("%d frames of kinetic scrolling rebuilt %d scope(s), want 0", ticks, got)
	}
	if got := after.Layouts - before.Layouts; got != 0 {
		t.Errorf("%d frames of kinetic scrolling ran %d layouter(s), want 0", ticks, got)
	}
}

// TestFlingKeepsTheFrameLoopAwake is the interaction with the idle policy of
// the project plan, section 6: a fling is the one thing that changes without
// input, so it has to keep NeedsPaint true or the backend would drop to
// IdleTPS in the middle of the animation.
func TestFlingKeepsTheFrameLoopAwake(t *testing.T) {
	h := scrollHarness(t)
	h.Find(gifttest.ByKey(scrollRowKey(0))).Fling(geom.Pt(0, -160), 80*time.Millisecond)

	// Driven through the App rather than through Harness.Advance, because
	// Advance paints and painting is what clears the flag. The question here
	// is what the *backend* sees between its Update and its Draw.
	app := h.App()
	now := h.Now()
	awake, ticks := 0, 0
	for range 200 {
		now += 16 * time.Millisecond
		app.BeginInput(now)
		if app.NeedsPaint() {
			awake++
		}
		if err := app.Update(h.Size()); err != nil {
			t.Fatal(err)
		}
		app.Paint()
		ticks++
		if !scroller(h).ScrollInfo().Flinging {
			break
		}
	}
	if ticks < 10 {
		t.Fatalf("the fling only ran for %d ticks", ticks)
	}
	if awake < ticks-1 {
		t.Errorf("only %d of %d kinetic ticks set NeedsPaint; the backend's idle policy "+
			"would throttle the animation", awake, ticks)
	}
}

// TestPressStopsAFling is the behaviour every list has: putting a finger down
// on moving content catches it.
func TestPressStopsAFling(t *testing.T) {
	h := scrollHarness(t)
	s := scroller(h)
	h.Find(gifttest.ByKey(scrollRowKey(0))).Fling(geom.Pt(0, -160), 80*time.Millisecond)
	if !s.ScrollInfo().Flinging {
		t.Fatal("no fling to stop")
	}
	h.PressAt(geom.Pt(150, 100))
	if s.ScrollInfo().Flinging {
		t.Error("the press did not stop the fling")
	}
	h.Release()
}

// --- scroll into view --------------------------------------------------------

// TestScrollIntoViewMovesTheMinimum covers the three cases of the reveal
// arithmetic: already visible, below the fold and above it.
func TestScrollIntoViewMovesTheMinimum(t *testing.T) {
	h := scrollHarness(t)
	s := scroller(h)

	h.Find(gifttest.ByKey(scrollRowKey(0))).ScrollIntoView()
	s.AssertScrollOffset(0)

	// Row 5 spans 500..600; bringing its bottom edge to the bottom of the
	// 200 high viewport is offset 400.
	h.Find(gifttest.ByKey(scrollRowKey(5))).ScrollIntoView().AssertVisible()
	s.AssertScrollOffset(400)

	// Row 2 spans 200..300 and is now above the viewport; its top edge goes
	// to the top, which is offset 200.
	h.Find(gifttest.ByKey(scrollRowKey(2))).ScrollIntoView().AssertVisible()
	s.AssertScrollOffset(200)
}

// TestScrollIntoViewThroughNestedScrollers checks that both containers are
// adjusted, innermost first.
func TestScrollIntoViewThroughNestedScrollers(t *testing.T) {
	inner := make([]gift.View, 0, 6)
	for i := range 6 {
		inner = append(inner, ui.Box().Frame(200, 100).
			Background(ui.RGB(uint8(20*i), 0, 0)).Key("inner"+strconv.Itoa(i)))
	}
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(
			ui.Box().Frame(200, 400).Background(ui.RGB(0, 0, 90)).Key("spacer"),
			ui.VScroll(inner...).Frame(200, 200).Key("inner-scroller"),
		).Frame(200, 300).Key("outer-scroller"),
	)})
	target := h.Find(gifttest.ByKey("inner5"))
	target.AssertNotVisible()
	target.ScrollIntoView().AssertVisible()

	if got := h.Find(gifttest.ByKey("inner-scroller")).ScrollInfo().Offset; got != 400 {
		t.Errorf("the inner scroller is at %g, want 400", got)
	}
	if got := h.Find(gifttest.ByKey("outer-scroller")).ScrollInfo().Offset; got != 300 {
		t.Errorf("the outer scroller is at %g, want 300", got)
	}
}

// TestScrollIntoViewOfSomethingTallerThanTheViewport aligns the beginning of
// the node with the beginning of the viewport, because that is where its
// content starts.
func TestScrollIntoViewOfSomethingTallerThanTheViewport(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(
			ui.Box().Frame(200, 100).Background(ui.RGB(1, 1, 1)).Key("first"),
			ui.Box().Frame(200, 500).Background(ui.RGB(2, 2, 2)).Key("tall"),
		).Frame(200, 200).Key("scroller"),
	)})
	h.Find(gifttest.ByKey("tall")).ScrollIntoView()
	scroller(h).AssertScrollOffset(100)
}

// --- the shape of the view ----------------------------------------------------

// TestScrollViewRejectsClipFalse keeps the promise that no view accepts a
// modifier it would then ignore.
func TestScrollViewRejectsClipFalse(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ScrollView.Clip(false) did not panic")
		}
	}()
	ui.VScroll().Clip(false)
}

// TestScrollOffsetSurvivesARebuild is the section 5 requirement stated as a
// test: the offset belongs to the node, so rebuilding the component around it
// must not send the reader back to the top.
func TestScrollOffsetSurvivesARebuild(t *testing.T) {
	var label *gift.State[float32]
	root := func(ctx *gift.Context) gift.View {
		label = ctx.State("label", float32(10))
		rows := make([]gift.View, 0, scrollRows)
		for i := range scrollRows {
			rows = append(rows, ui.Box().
				Frame(scrollViewportW, scrollRowH).
				Background(ui.RGB(uint8(10*i), 40, 60)).
				Key(scrollRowKey(i)))
		}
		return ui.VStack(
			ui.Box().Frame(ctx.Read(label), 10).Background(ui.RGB(9, 9, 9)),
			ui.VScroll(rows...).Frame(scrollViewportW, scrollViewportH).Key("scroller"),
		)
	}
	h := gifttest.New(t, gifttest.Options{Root: root})
	scroller(h).ScrollTo(750)

	label.Set(20)
	h.Settle()

	scroller(h).AssertScrollOffset(750)
}

// TestNestedViewportClipsAreDeviceSpace is the regression test for the first
// bug the transform path produced once it had a real consumer.
//
// gift pushed the clip of a node as its *layout* rectangle while the hit test
// composed the transform first. For as long as every transform was the
// identity the two were the same number and nothing noticed. Under an outer
// scroll they are not: an inner viewport whose layout rectangle sits at
// document y 400 is on screen at device y 100 when the outer container is at
// offset 300, and the old code clipped its content to 400..600 — which does
// not intersect the outer clip at all, so the inner list simply vanished while
// remaining perfectly clickable.
func TestNestedViewportClipsAreDeviceSpace(t *testing.T) {
	inner := make([]gift.View, 0, 6)
	for i := range 6 {
		inner = append(inner, ui.Box().Frame(200, 100).
			Background(ui.RGB(0, 0, uint8(40+20*i))).Key("inner"+strconv.Itoa(i)))
	}
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(
			ui.Box().Frame(200, 400).Background(ui.RGB(90, 0, 0)).Key("spacer"),
			ui.VScroll(inner...).Frame(200, 200).Key("inner-scroller"),
		).Frame(200, 300).Key("outer-scroller"),
	)})
	h.Find(gifttest.ByKey("outer-scroller")).ScrollTo(300)

	innerView := h.Find(gifttest.ByKey("inner-scroller"))
	if got := innerView.Bounds().Min.Y; got != 100 {
		t.Fatalf("the inner viewport is at device y %g, want 100", got)
	}

	// The first inner row is at device 100..200 and must be drawn there,
	// under a clip that contains it.
	row := h.Find(gifttest.ByKey("inner0"))
	row.AssertVisible()
	if got, _ := row.VisibleBounds(); got != geom.Rc(0, 100, 200, 200) {
		t.Errorf("the first inner row is visible at %v, want (0,100)-(200,200)", got)
	}

	l := h.List()
	found := false
	for _, op := range l.Ops() {
		if op.Kind != render.OpFillRect {
			continue
		}
		dev := l.Xform(op.Xform).TransformRect(op.Bounds)
		if dev != geom.Rc(0, 100, 200, 200) {
			continue
		}
		clip := l.Clip(op.Clip)
		if clip.Intersect(dev).IsEmpty() {
			t.Fatalf("the first inner row is drawn at %v but clipped to %v, which excludes it: "+
				"the clip is being pushed in layout space instead of device space", dev, clip)
		}
		found = true
	}
	if !found {
		t.Error("the first inner row was not emitted at its device position")
	}
}

// TestTextInsideAScrollerIsClippedAtItsDevicePosition is the second bug of the
// same family. ui.Text draws glyphs rather than children, so it pushes its own
// clip, and it pushed the layout rectangle. Inside a scroller the label's
// glyphs were then clipped against the place the label used to be, which for
// an offset larger than the label's height removed every glyph.
func TestTextInsideAScrollerIsClippedAtItsDevicePosition(t *testing.T) {
	rows := make([]gift.View, 0, 10)
	for i := range 10 {
		rows = append(rows, textOf(t, "row "+strconv.Itoa(i)).
			Clip(true).Frame(200, 40).Key(scrollRowKey(i)))
	}
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(rows...).Frame(200, 80).Key("scroller"),
	)})
	h.Find(gifttest.ByKey("scroller")).ScrollTo(200)

	// Rows 5 and 6 are on screen now. Their glyphs must survive the clip.
	for _, key := range []string{scrollRowKey(5), scrollRowKey(6)} {
		row := h.Find(gifttest.ByKey(key))
		row.AssertVisible()
		if n := visibleGlyphs(h, row); n == 0 {
			t.Errorf("%s is on screen but every one of its glyphs was clipped away; "+
				"ui.Text is pushing its clip in layout space", key)
		}
	}
	// And a row far outside really is clipped away.
	if n := visibleGlyphs(h, h.Find(gifttest.ByKey(scrollRowKey(0)))); n != 0 {
		t.Errorf("row0 is two hundred pixels above the viewport and still has %d visible glyphs", n)
	}
}

// visibleGlyphs counts the glyphs of the frame that lie inside the node's
// device bounds *and* survive the clip their operation carries.
func visibleGlyphs(h *gifttest.Harness, n gifttest.Node) int {
	l := h.List()
	b := n.Bounds()
	count := 0
	for _, op := range l.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		m := l.Xform(op.Xform)
		clip := l.Clip(op.Clip)
		for _, g := range l.Glyphs(op.Glyphs, op.GlyphCount) {
			p := m.Apply(geom.Pt(g.X, g.Y))
			if b.Contains(p) && clip.Contains(p) {
				count++
			}
		}
	}
	return count
}
