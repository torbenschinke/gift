package ui_test

import (
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The scene of this file is the one from scroll_test.go, with the indicator
// left at its default. Everything is quoted against the constants there: a
// 300x200 viewport over 2000 of content, so MaxOffset is 1800 and the viewport
// is a tenth of the document.

// barScene is scrollScene with a named bar style, so that a test can state the
// geometry it expects instead of deriving it from the defaults.
var testBar = ui.ScrollBar{Width: 10, MinThumb: 20, Margin: 0, Hold: 500 * time.Millisecond, Fade: 200 * time.Millisecond}

func barHarness(t *testing.T, view gift.View) *gifttest.Harness {
	t.Helper()
	return gifttest.New(t, gifttest.Options{View: view})
}

func barScene() gift.View {
	rows := make([]gift.View, 0, scrollRows)
	for i := range scrollRows {
		rows = append(rows, ui.Box().
			Frame(scrollViewportW, scrollRowH).
			Background(ui.RGB(uint8(10*i), 40, 60)).
			Key(scrollRowKey(i)))
	}
	return ui.VStack(
		ui.VScroll(rows...).
			Frame(scrollViewportW, scrollViewportH).
			ScrollBar(testBar).
			// A fling would keep moving after a drag and would make every
			// offset assertion in this file a race against the friction
			// curve. The gesture is still a real drag; only the kinetic
			// continuation is switched off.
			FlingVelocity(1e9).
			Key("scroller"),
	)
}

// barOps returns the rounded rectangles the indicator emitted, in order: the
// track first and the thumb second, or nothing when the bar is invisible.
//
// It identifies them by shape and position rather than by colour, because the
// colour is the thing a fade changes and a test that matched on it would stop
// finding a half faded bar.
func barOps(h *gifttest.Harness, within geom.Rect) []render.Op {
	var out []render.Op
	for _, op := range h.Ops() {
		if op.Kind != render.OpFillRoundRect {
			continue
		}
		// The bar lives in the trailing ten pixels of the viewport; the rows
		// of the scene are plain rectangles and fill the whole width.
		if op.Bounds.Min.X < within.Max.X-10.5 {
			continue
		}
		out = append(out, op)
	}
	return out
}

// TestScrollBarGrabSurvivesARebuild is the regression test for the defect that
// made the grab presentation state of the *ui node* instead of the scroll
// container.
//
// The sequence is the one an application produces with gift's own asynchronous
// pattern — [gift.App.Post] plus a [gift.State] write — while the user holds
// the thumb. If the grab is dropped by the rebuild, the pointer capture and
// the drag flag survive it regardless, so the next move falls through the
// indicator into gift's scroll handler and is read as a *content* drag: the
// content then moves against the pointer instead of with the thumb. Before the
// fix this sequence ended at offset 405 with Dragging = true, which is the
// same signature TestScrollBarGrabDoesNotBubbleToTheViewport exists to
// prevent; it now ends at 900.
func TestScrollBarGrabSurvivesARebuild(t *testing.T) {
	var tick *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		tick = ctx.State("tick", 0)
		rows := make([]gift.View, 0, scrollRows)
		for i := range scrollRows {
			rows = append(rows, ui.Box().
				Frame(scrollViewportW, scrollRowH).
				Background(ui.RGB(uint8(10*i), 40, 60)).
				Key(scrollRowKey(i)))
		}
		return ui.VStack(
			// Reading the state is what enrols this component in its
			// changes. The scroller below has a fixed frame, so the rebuild
			// moves nothing the assertions depend on.
			ui.Box().Frame(float32(10+5*ctx.Read(tick)), 10).Background(ui.RGB(9, 9, 9)),
			ui.VScroll(rows...).
				Frame(scrollViewportW, scrollViewportH).
				ScrollBar(testBar).
				FlingVelocity(1e9).
				Key("scroller"),
		)
	}
	h := gifttest.New(t, gifttest.Options{Root: root})
	s := scroller(h)

	wakeTo(s, 0)
	thumb := barOps(h, s.Bounds())[1].Bounds
	grab := geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)

	h.PressAt(grab)
	h.MoveTo(geom.Pt(grab.X, grab.Y+45))
	if got := scroller(h).ScrollInfo().Offset; !nearF64(got, 450) {
		t.Fatalf("offset after the first 45 px of thumb travel = %g, want 450", got)
	}

	// The rebuild, in the middle of the gesture.
	tick.Set(1)
	h.Settle()

	h.MoveTo(geom.Pt(grab.X, grab.Y+90))
	info := scroller(h).ScrollInfo()
	if info.Dragging {
		t.Error("the viewport took over the gesture after the rebuild; the grab was lost")
	}
	if !nearF64(info.Offset, 900) {
		t.Errorf("offset after 90 px of thumb travel across a rebuild = %g, want 900; "+
			"405 is the viewport dragging the content against the thumb", info.Offset)
	}
	h.Release()
}

// TestScrollBarStaysAwakeUnderThePointerAndFadesWhenItLeaves covers the hover
// path, which had no test at all, and the snap it used to end in.
//
// A pointer resting on the track emits no events, so the idle counter kept
// growing underneath a bar that the hover held at full opacity. The bar
// therefore looked normal right up to the moment the pointer left, and then
// disappeared in a single frame because it was already long past the fade. The
// fix is that entering *and leaving* the track count as activity.
func TestScrollBarStaysAwakeUnderThePointerAndFadesWhenItLeaves(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 400)
	track := barOps(h, b)[0].Bounds
	onTrack := geom.Pt(track.Min.X+track.Width()/2, track.Min.Y+track.Height()/2)
	offTrack := geom.Pt(b.Min.X+20, b.Min.Y+20)

	// Let it fade out completely first, so that the hover is the only thing
	// that can bring it back.
	h.Advance(testBar.Hold + 2*testBar.Fade)
	if ops := barOps(h, b); len(ops) != 0 {
		t.Fatalf("the bar is still drawn after the fade: %d op(s)", len(ops))
	}

	h.MoveTo(onTrack)
	ops := barOps(h, b)
	if len(ops) != 2 {
		t.Fatalf("hovering the track drew %d op(s), want 2: a faded bar has to wake up or it "+
			"is an invisible click target", len(ops))
	}
	if ops[1].Color.A <= 0 {
		t.Errorf("the hovered thumb was emitted transparent (alpha %g)", ops[1].Color.A)
	}

	// Resting on it for far longer than hold and fade together changes
	// nothing: the pointer is on the bar and the bar is a target.
	h.Advance(4 * (testBar.Hold + testBar.Fade))
	if ops := barOps(h, b); len(ops) != 2 {
		t.Fatalf("the bar faded out while the pointer was resting on it: %d op(s)", len(ops))
	}

	// Leaving is where it used to snap: the frame right after the move had a
	// huge IdleFor and therefore zero opacity.
	h.MoveTo(offTrack)
	ops = barOps(h, b)
	if len(ops) != 2 {
		t.Fatalf("the bar vanished in the frame the pointer left the track: %d op(s), want 2 "+
			"(the hold has not even started yet)", len(ops))
	}
	full := ops[1].Color.A

	h.Advance(testBar.Hold + testBar.Fade/2)
	mid := barOps(h, b)
	if len(mid) != 2 {
		t.Fatalf("half way through the fade after a hover the bar emitted %d op(s), want 2", len(mid))
	}
	if !(mid[1].Color.A > 0 && mid[1].Color.A < full) {
		t.Errorf("thumb alpha half way through the post hover fade = %g, want strictly between 0 and %g",
			mid[1].Color.A, full)
	}

	h.Advance(testBar.Fade)
	if ops := barOps(h, b); len(ops) != 0 {
		t.Errorf("the bar never finished fading after the hover: %d op(s)", len(ops))
	}
}

// TestDefaultScrollBarFitsInsideTheIndicatorLinger pins the one invariant that
// connects the style in this package to the repaint policy in the core.
//
// [gift.ScrollIndicatorLinger] is what keeps frames coming after the content
// stopped; a bar whose hold and fade together outlast it gets its last fade
// frames at the idle tick rate and snaps away instead of fading. Every other
// visibility test in this file uses testBar, so nothing else would notice a
// change to the shipped default — and the defect this replaces was exactly
// that: a Hold raised to 600 ms under a comment that still said 500.
func TestDefaultScrollBarFitsInsideTheIndicatorLinger(t *testing.T) {
	const margin = 100 * time.Millisecond
	total := ui.DefaultScrollBar.Hold + ui.DefaultScrollBar.Fade
	if total+margin > gift.ScrollIndicatorLinger {
		t.Errorf("DefaultScrollBar.Hold(%v) + Fade(%v) = %v, which leaves less than %v of the "+
			"%v repaint window; the last frames of the fade would be drawn at the idle tick "+
			"rate and the bar would snap away. Lower the hold or raise gift.ScrollIndicatorLinger.",
			ui.DefaultScrollBar.Hold, ui.DefaultScrollBar.Fade, total, margin, gift.ScrollIndicatorLinger)
	}
}

// TestScrollDoesNotStickToTheBareCursor is the end to end version of the
// defect the client reported: "the mouse-up is not detected, so scrolling
// sticks to the mouse cursor until you click again".
//
// It is deliberately written the way the failing report was: with the *mouse*,
// and with a move after the release. The two verbs the harness already had for
// dragging a scroller — Swipe and Fling — use the touch path, whose slot is
// zeroed on release and which therefore cannot reproduce this, which is why
// the defect survived a whole work unit of scroll tests.
//
// The two guards inside gift have their own tests in scroll_stuck_test.go of
// the root package. This one is the user's sentence.
func TestScrollDoesNotStickToTheBareCursor(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()
	from := geom.Pt(b.Min.X+20, b.Min.Y+160)

	h.PressAt(from)
	h.MoveTo(geom.Pt(from.X, from.Y-60))
	h.ReleaseAt(geom.Pt(from.X, from.Y-60))

	settled := s.ScrollInfo()
	if settled.Flinging {
		t.Fatalf("the scene is supposed to suppress flings, but one started at %g px/s", settled.Velocity)
	}
	if settled.Offset != 60 {
		t.Fatalf("offset after the drag = %g, want 60", settled.Offset)
	}

	// The move that used to move the content: no button is held.
	h.MoveTo(geom.Pt(from.X, from.Y-160))
	if got := s.ScrollInfo(); got.Offset != settled.Offset || got.Dragging {
		t.Errorf("a button-less move took the offset from %g to %g (dragging = %v); "+
			"the scroll is following the bare cursor",
			settled.Offset, got.Offset, got.Dragging)
	}
	// And again, further, because one unchanged frame could be an accident.
	h.MoveTo(geom.Pt(from.X, from.Y+40))
	if got := s.ScrollInfo(); got.Offset != settled.Offset {
		t.Errorf("a second button-less move took the offset to %g, want %g", got.Offset, settled.Offset)
	}
}

// TestScrollBarIsVisibleAfterScrollingAndFadesWhenIdle is the client's other
// complaint: there was no bar at all.
//
// The three moments are the whole visibility rule: nothing before the first
// movement, a bar immediately after one, and nothing again once the hold and
// the fade have passed.
func TestScrollBarIsVisibleAfterScrollingAndFadesWhenIdle(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	if ops := barOps(h, b); len(ops) != 0 {
		t.Errorf("the bar is drawn before anything ever scrolled: %d op(s)", len(ops))
	}

	s.ScrollBy(400)
	ops := barOps(h, b)
	if len(ops) != 2 {
		t.Fatalf("after a scroll the bar emitted %d op(s), want 2 (track and thumb)", len(ops))
	}
	if ops[0].Color.A <= 0 || ops[1].Color.A <= 0 {
		t.Errorf("the bar was emitted fully transparent: track alpha %g, thumb alpha %g",
			ops[0].Color.A, ops[1].Color.A)
	}

	// Half way through the fade it is still there and dimmer.
	full := ops[1].Color.A
	h.Advance(testBar.Hold + testBar.Fade/2)
	mid := barOps(h, b)
	if len(mid) != 2 {
		t.Fatalf("half way through the fade the bar emitted %d op(s), want 2", len(mid))
	}
	if !(mid[1].Color.A > 0 && mid[1].Color.A < full) {
		t.Errorf("thumb alpha half way through the fade = %g, want strictly between 0 and %g",
			mid[1].Color.A, full)
	}

	h.Advance(testBar.Fade)
	if ops := barOps(h, b); len(ops) != 0 {
		t.Errorf("the bar is still drawn %v after the last movement: %d op(s)",
			testBar.Hold+testBar.Fade+testBar.Fade/2, len(ops))
	}
}

// TestScrollBarThumbShowsTheOffset pins the geometry: the thumb is as long a
// share of the track as the viewport is of the document, and it sits at the
// matching share of the leftover travel.
func TestScrollBarThumbShowsTheOffset(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	// Viewport 200 of content 2000, so a tenth of a 200 pixel track: 20.
	// Travel is therefore 180.
	const wantThumb = float32(scrollViewportH) * scrollViewportH / scrollContentH
	const wantTravel = scrollViewportH - wantThumb

	for _, off := range []float64{0, scrollMaxOffset / 2, scrollMaxOffset} {
		wakeTo(s, off)
		ops := barOps(h, b)
		if len(ops) != 2 {
			t.Fatalf("offset %g: %d bar op(s), want 2", off, len(ops))
		}
		thumb := ops[1].Bounds
		if got := thumb.Height(); !near(got, wantThumb) {
			t.Errorf("offset %g: thumb height = %g, want %g", off, got, wantThumb)
		}
		want := b.Min.Y + wantTravel*float32(off/scrollMaxOffset)
		if got := thumb.Min.Y; !near(got, want) {
			t.Errorf("offset %g: thumb top = %g, want %g", off, got, want)
		}
	}
}

// TestScrollBarThumbDragScrolls is the second half of the client's request:
// the bar has to be grabbable, and the content has to follow proportionally.
//
// One pixel of thumb travel is MaxOffset/travel = 1800/180 = ten document
// pixels here, and that ratio is what the assertion states.
func TestScrollBarThumbDragScrolls(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	// Wake the bar, and give the thumb somewhere to move from.
	wakeTo(s, 0)
	ops := barOps(h, b)
	if len(ops) != 2 {
		t.Fatalf("no bar to grab: %d op(s)", len(ops))
	}
	thumb := ops[1].Bounds
	grab := geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)

	h.PressAt(grab)
	if got := s.ScrollInfo().Offset; got != 0 {
		t.Errorf("the press alone moved the offset to %g; a thumb grab must not jump", got)
	}
	h.MoveTo(geom.Pt(grab.X, grab.Y+45))
	if got := s.ScrollInfo().Offset; !nearF64(got, 450) {
		t.Errorf("offset after 45 px of thumb travel = %g, want 450", got)
	}
	h.MoveTo(geom.Pt(grab.X, grab.Y+180))
	if got := s.ScrollInfo().Offset; !nearF64(got, scrollMaxOffset) {
		t.Errorf("offset at the end of the travel = %g, want %d", got, scrollMaxOffset)
	}
	// Past the end: clamped, not overshooting and not wrapping.
	h.MoveTo(geom.Pt(grab.X, grab.Y+900))
	if got := s.ScrollInfo().Offset; !nearF64(got, scrollMaxOffset) {
		t.Errorf("offset past the end of the travel = %g, want %d", got, scrollMaxOffset)
	}
	h.Release()

	// And the release really ends it: the bare cursor moves nothing.
	after := s.ScrollInfo().Offset
	h.MoveTo(geom.Pt(grab.X, grab.Y))
	if got := s.ScrollInfo().Offset; got != after {
		t.Errorf("a button-less move after a thumb drag took the offset from %g to %g", after, got)
	}
}

// TestScrollBarGrabDoesNotBubbleToTheViewport is the trap named in the project
// plan, section 23, step 6: "Der Griff muss EventPointerMove konsumieren, sonst
// nimmt ihm der Viewport den Zug wieder ab."
//
// Both readings of the failure are asserted, because they are different
// symptoms of the same missing return. If the move bubbled, gift's scroll
// handler would recognise a drag past DragSlop, call StealPointer and set its
// own dragging flag — so the container would report Dragging — and it would
// then move the content *against* the pointer, on top of the thumb's own
// movement in the same direction. The offset assertion is the sharper of the
// two: a doubled or reversed movement is a number, not a mood.
func TestScrollBarGrabDoesNotBubbleToTheViewport(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	thumb := barOps(h, b)[1].Bounds
	grab := geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)

	h.PressAt(grab)
	// Well past DragSlop, so a viewport that saw this move would certainly
	// treat it as a drag.
	h.MoveTo(geom.Pt(grab.X, grab.Y+45))

	info := s.ScrollInfo()
	if info.Dragging {
		t.Error("the viewport is dragging during a thumb grab; the move bubbled past the indicator")
	}
	if !nearF64(info.Offset, 450) {
		t.Errorf("offset = %g, want 450; a viewport that also acted on the move would subtract "+
			"its own 45 pixels and land on 405", info.Offset)
	}
	h.Release()
}

// TestScrollBarTrackClickPagesTowardsTheClick pins the documented choice for a
// click that lands on the track and not on the thumb: one viewport towards the
// click, once, rather than a jump to the clicked position.
func TestScrollBarTrackClickPagesTowardsTheClick(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	track := barOps(h, b)[0].Bounds
	below := geom.Pt(track.Min.X+track.Width()/2, track.Max.Y-2)

	h.ClickAt(below)
	if got := s.ScrollInfo().Offset; !nearF64(got, scrollViewportH) {
		t.Errorf("offset after a click below the thumb = %g, want %d (one viewport)", got, scrollViewportH)
	}
	h.ClickAt(below)
	if got := s.ScrollInfo().Offset; !nearF64(got, 2*scrollViewportH) {
		t.Errorf("offset after a second click = %g, want %d", got, 2*scrollViewportH)
	}

	above := geom.Pt(track.Min.X+track.Width()/2, track.Min.Y+2)
	h.ClickAt(above)
	if got := s.ScrollInfo().Offset; !nearF64(got, scrollViewportH) {
		t.Errorf("offset after a click above the thumb = %g, want %d", got, scrollViewportH)
	}
}

// TestHScrollBarLivesOnTheBottomEdge is the horizontal axis, which exists and
// therefore has to work: the track is along the bottom, and a drag along x
// moves the offset the same way.
func TestHScrollBarLivesOnTheBottomEdge(t *testing.T) {
	cols := make([]gift.View, 0, 20)
	for i := range 20 {
		cols = append(cols, ui.Box().Frame(100, 200).Background(ui.RGB(uint8(10*i), 40, 60)))
	}
	h := barHarness(t, ui.VStack(
		ui.HScroll(cols...).Frame(300, 200).ScrollBar(testBar).FlingVelocity(1e9).Key("scroller"),
	))
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	// The shared barOps looks along the trailing *vertical* strip, so this
	// axis collects its own ops: the bottom ten pixels, full width.
	bottomOps := func() []render.Op {
		var ops []render.Op
		for _, op := range h.Ops() {
			if op.Kind == render.OpFillRoundRect && op.Bounds.Min.Y >= b.Max.Y-10.5 {
				ops = append(ops, op)
			}
		}
		return ops
	}
	ops := bottomOps()
	if len(ops) != 2 {
		t.Fatalf("%d bar op(s) along the bottom edge, want 2", len(ops))
	}
	track, thumb := ops[0].Bounds, ops[1].Bounds
	if !near(track.Height(), testBar.Width) {
		t.Errorf("track height = %g, want %g: a horizontal bar is thick across its axis", track.Height(), testBar.Width)
	}
	// 300 of 2000 content over a 300 track: 45 long, 255 of travel, and the
	// maximum offset is 1700.
	if !near(thumb.Width(), 45) {
		t.Errorf("thumb width = %g, want 45", thumb.Width())
	}
	if !near(thumb.Min.X, b.Min.X) {
		t.Errorf("thumb left at offset 0 = %g, want %g", thumb.Min.X, b.Min.X)
	}

	// The forward mapping, which the drag below is only the inverse of: at
	// half the maximum offset the thumb sits at half of its 255 of travel. A
	// thumb off by a constant along x satisfies every other assertion here.
	wakeTo(s, 850)
	mid := bottomOps()
	if len(mid) != 2 {
		t.Fatalf("%d bar op(s) at half the travel, want 2", len(mid))
	}
	if want := b.Min.X + 127.5; !near(mid[1].Bounds.Min.X, want) {
		t.Errorf("thumb left at offset 850 = %g, want %g", mid[1].Bounds.Min.X, want)
	}

	wakeTo(s, 0)
	thumb = bottomOps()[1].Bounds
	if !near(thumb.Min.X, b.Min.X) {
		t.Fatalf("the scene did not return to offset 0: thumb left = %g", thumb.Min.X)
	}

	grab := geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)
	h.PressAt(grab)
	h.MoveTo(geom.Pt(grab.X+51, grab.Y))
	if got := s.ScrollInfo().Offset; !nearF64(got, 51.0/255.0*1700) {
		t.Errorf("offset after 51 px of thumb travel = %g, want %g", got, 51.0/255.0*1700)
	}
	h.Release()
}

// TestScrollBarCanBeTurnedOff is the other half of the contract the package
// documentation states: a modifier a view accepts is a modifier it honours.
func TestScrollBarCanBeTurnedOff(t *testing.T) {
	rows := make([]gift.View, 0, scrollRows)
	for i := range scrollRows {
		rows = append(rows, ui.Box().Frame(scrollViewportW, scrollRowH).Background(ui.RGB(uint8(10*i), 40, 60)))
	}
	h := barHarness(t, ui.VStack(
		ui.VScroll(rows...).
			Frame(scrollViewportW, scrollViewportH).
			ScrollBar(ui.ScrollBar{Hidden: true}).
			Key("scroller"),
	))
	s := scroller(h)
	s.ScrollBy(400)
	if ops := barOps(h, s.Bounds()); len(ops) != 0 {
		t.Errorf("a hidden bar emitted %d op(s)", len(ops))
	}
}

// TestScrollBarIsAbsentWhenThereIsNothingToScroll: the content fits, so there
// is no position to indicate and no bar, however much the user scrolls at it.
func TestScrollBarIsAbsentWhenThereIsNothingToScroll(t *testing.T) {
	h := barHarness(t, ui.VStack(
		ui.VScroll(ui.Box().Frame(300, 50).Background(ui.RGB(20, 20, 20))).
			Frame(300, 200).ScrollBar(testBar).Key("scroller"),
	))
	s := scroller(h)
	h.Wheel(s.Center(), geom.Pt(0, -3))
	if ops := barOps(h, s.Bounds()); len(ops) != 0 {
		t.Errorf("a viewport larger than its content drew %d bar op(s)", len(ops))
	}
}

// wakeTo puts the container at off and makes sure the move really happened, so
// that the indicator is at full opacity when the assertion looks at it. A
// ScrollTo to the offset the container is already at changes nothing, and an
// indicator that fades on idleness is right to stay hidden for it.
func wakeTo(s gifttest.Node, off float64) {
	other := 0.0
	if off == 0 {
		other = 1
	}
	s.ScrollTo(other)
	s.ScrollTo(off)
}

func near(a, b float32) bool {
	d := a - b
	return d > -0.01 && d < 0.01
}

func nearF64(a, b float64) bool {
	d := a - b
	return d > -0.01 && d < 0.01
}

// --- the gallery ---------------------------------------------------------------

// The gallery is the view the client is actually using, and it is the harder
// of the two: it is a virtualising container whose children are a positional
// tile pool, every one of which is itself a hit target that turns a release
// into a selection. A thumb grab that let the press through would scroll the
// document *and* open a picture.

func galleryBarHarness(t *testing.T, sel *[]asset.ID) (*gifttest.Harness, gifttest.Node) {
	t.Helper()
	g := ui.NewGallery(asset.NewCollection(synth(5000)))
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).
			ScrollBar(testBar).
			Config(gift.ScrollConfig{FlingVelocity: 1e9}).
			OnSelect(func(id asset.ID) { *sel = append(*sel, id) }).
			Flex(1).Key("gallery")),
		Size: geom.Sz(800, 600),
	})
	return h, h.Find(gifttest.ByKey("gallery"))
}

// TestGalleryScrollBarIsVisibleAndGrabbable is the client's complaint, in the
// view the client uses.
func TestGalleryScrollBarIsVisibleAndGrabbable(t *testing.T) {
	var sel []asset.ID
	h, g := galleryBarHarness(t, &sel)
	b := g.Bounds()

	wakeTo(g, 0)
	ops := barOps(h, b)
	if len(ops) != 2 {
		t.Fatalf("the gallery drew %d bar op(s), want 2", len(ops))
	}
	track, thumb := ops[0].Bounds, ops[1].Bounds
	if got := thumb.Height(); got != testBar.MinThumb {
		t.Errorf("thumb height = %g, want the minimum %g: five thousand masonry tiles "+
			"are a document tall enough that the proportional thumb is sub pixel",
			got, testBar.MinThumb)
	}

	info := g.ScrollInfo()
	grab := geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)
	h.PressAt(grab)
	h.MoveTo(geom.Pt(grab.X, grab.Y+100))

	travel := track.Height() - thumb.Height()
	want := info.MaxOffset * float64(100/travel)
	if got := g.ScrollInfo().Offset; !nearF64(got, want) {
		t.Errorf("offset after 100 px of thumb travel = %g, want %g", got, want)
	}
	if g.ScrollInfo().Dragging {
		t.Error("the gallery viewport is dragging during a thumb grab")
	}
	h.Release()

	// The press landed on a tile, and the tile is what turns a release into a
	// selection. The steal is what stops it.
	if len(sel) != 0 {
		t.Errorf("a thumb drag selected %d entr(y/ies): %v", len(sel), sel)
	}
}

// TestGalleryScrollBarThumbGrabStealsThePressFromTheTile is the same steal,
// stated as the property rather than as a side effect: the tile under the
// thumb must lose its pressed look the moment the indicator takes the gesture.
func TestGalleryScrollBarThumbGrabStealsThePressFromTheTile(t *testing.T) {
	var sel []asset.ID
	h, g := galleryBarHarness(t, &sel)
	wakeTo(g, 0)
	thumb := barOps(h, g.Bounds())[1].Bounds
	grab := geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)

	h.PressAt(grab)
	// A release right where the press happened, with no movement at all, is
	// the sequence that would be a click if the press had reached the tile.
	h.ReleaseAt(grab)
	if len(sel) != 0 {
		t.Errorf("a press and release on the thumb selected %v", sel)
	}
}

// --- the allocation contract ---------------------------------------------------

// TestScrollBarFramePathIsAllocationFree extends the contract of the project
// plan, section 11, to the indicator.
//
// A decoration that is drawn on every frame of every scroll is on the frame
// path by definition, and one that allocates there is a defect rather than a
// cost: at sixty hertz it is a garbage collection in the middle of the one
// gesture the whole performance argument is about. Two paths are measured,
// because they are different code — painting a visible bar, and driving the
// content from a held thumb.
func TestScrollBarFramePathIsAllocationFree(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector rewrites every access and defeats the pooling the giftdebug " +
			"goroutine check relies on; allocation counts in a race build measure the instrumentation")
	}
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View { return barScene() }})
	size := geom.Sz(800, 600)
	if err := a.Update(size); err != nil {
		t.Fatal(err)
	}
	var sc gift.NodeRef
	var walk func(gift.NodeRef)
	walk = func(r gift.NodeRef) {
		if a.IsScrollable(r) && sc.IsZero() {
			sc = r
		}
		for _, c := range a.NodeChildren(r, nil) {
			walk(c)
		}
	}
	walk(a.Root())
	if sc.IsZero() {
		t.Fatal("no scroll container")
	}
	b := a.NodeBounds(sc)

	now := time.Duration(0)
	a.BeginInput(now)
	// A bar nobody has woken is not a target, which is the documented rule and
	// is why this scrolls before it grabs.
	a.ScrollBy(sc, 100)
	if err := a.Update(size); err != nil {
		t.Fatal(err)
	}
	// The thumb of this scene is 20 long on a 200 track and sits at 10 for an
	// offset of 100; grabbing its middle and moving inside the track keeps
	// every frame a real drag.
	grab := geom.Pt(b.Max.X-5, b.Min.Y+20)
	a.BeginInput(now)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, grab)
	if err := a.Update(size); err != nil {
		t.Fatal(err)
	}

	y := float32(20)
	dy := float32(1)
	step := func() {
		now += 16 * time.Millisecond
		y += dy
		if y > 170 || y < 10 {
			dy = -dy
		}
		a.BeginInput(now)
		a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(grab.X, b.Min.Y+y))
		if err := a.Update(size); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 32 {
		step()
	}

	before, _ := a.ScrollInfo(sc)
	step()
	after, _ := a.ScrollInfo(sc)
	if after.Offset == before.Offset {
		t.Fatalf("the thumb drag moved nothing (offset %g); the measurement would be meaningless", after.Offset)
	}
	if !barIsPainted(a) {
		t.Fatal("no bar in the display list; the measurement would not cover the painter")
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("a frame with a held scroll bar allocated %v times per run, want 0", got)
	}
}

// barIsPainted reports whether the last display list contains the two rounded
// rectangles of an indicator in the trailing strip of the window.
func barIsPainted(a *gift.App) bool {
	n := 0
	for _, op := range a.Paint().Ops() {
		if op.Kind == render.OpFillRoundRect {
			n++
		}
	}
	return n >= 2
}

// --- the finger on the thumb ---------------------------------------------------

// The tests of defect 2: on a touchscreen there is no hover, so a bar that
// stops being a target when it fades is a bar that can only be grabbed within
// [ScrollBar.Hold] plus [ScrollBar.Fade] of the content last moving. See
// [ui.scrollBarTouchSlop] and the note on a faded bar above scrollBarState.press.

// thumbCentre returns the centre of the thumb while it is still visible, so
// that a test can aim at it after it has faded away.
func thumbCentre(h *gifttest.Harness, s gifttest.Node) geom.Point {
	ops := barOps(h, s.Bounds())
	thumb := ops[1].Bounds
	return geom.Pt(thumb.Min.X+thumb.Width()/2, thumb.Min.Y+thumb.Height()/2)
}

// TestAFadedScrollBarThumbCanStillBeGrabbedByAFinger is the defect itself. The
// content is scrolled, the bar is left to fade out completely, and then the
// thumb is dragged with a touch — no move first, because a finger has none.
func TestAFadedScrollBarThumbCanStillBeGrabbedByAFinger(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)

	wakeTo(s, 0)
	grab := thumbCentre(h, s)

	h.Advance(testBar.Hold + 2*testBar.Fade)
	if ops := barOps(h, s.Bounds()); len(ops) != 0 {
		t.Fatalf("the bar is still drawn after the fade: %d op(s); the fixture is wrong", len(ops))
	}

	h.TouchDownAt(grab)
	h.TouchMoveTo(geom.Pt(grab.X, grab.Y+45))

	// 45 px of thumb travel over a travel of 180 against a MaxOffset of 1800.
	if got := scroller(h).ScrollInfo().Offset; !nearF64(got, 450) {
		t.Fatalf("offset after grabbing the faded thumb and dragging 45 px = %g, want 450. "+
			"A negative or small value means the press fell through to the content, which "+
			"moves the document the other way and by the distance of the finger", got)
	}
	if ops := barOps(h, s.Bounds()); len(ops) != 2 {
		t.Fatalf("the grabbed bar drew %d op(s), want 2: taking the thumb has to wake the "+
			"bar, or the user is dragging something they cannot see", len(ops))
	}
	h.TouchUpAt(geom.Pt(grab.X, grab.Y+45))
}

// TestAContentDragNearTheTrailingEdgeStillScrollsTheContent is the cost of the
// rule above, held to the smallest it can be: the thumb plus
// [ui.scrollBarTouchSlop] is a target, and everything else near the edge is
// still the content.
func TestAContentDragNearTheTrailingEdgeStillScrollsTheContent(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	thumb := barOps(h, b)[1].Bounds
	h.Advance(testBar.Hold + 2*testBar.Fade)

	// Two pixels inside the trailing edge, and well below the thumb: this is
	// the track, which a faded bar does not own.
	from := geom.Pt(b.Max.X-2, thumb.Max.Y+40)
	if from.Y > b.Max.Y-10 {
		t.Fatalf("the fixture's thumb reaches to %v, too close to the bottom edge %v to "+
			"drag below it", thumb.Max.Y, b.Max.Y)
	}
	h.TouchDownAt(from)
	h.TouchMoveTo(geom.Pt(from.X, from.Y-60))

	// A content drag moves the document *with* the finger: the finger went up
	// by 60, so the content went down by 60.
	if got := scroller(h).ScrollInfo().Offset; !nearF64(got, 60) {
		t.Fatalf("offset after a 60 px content drag two pixels from the trailing edge = %g, "+
			"want 60. A much larger value means the invisible bar stole the drag and paged "+
			"or jumped the document", got)
	}
	h.TouchUpAt(geom.Pt(from.X, from.Y-60))
}

// TestAFadedScrollBarDoesNotPageOnATrackPress is the same boundary from the
// other side, stated as the rule rather than as an offset: an invisible track
// is not a page control. A tap on it is content, and this is what keeps the
// region a content drag loses down to the thumb.
func TestAFadedScrollBarDoesNotPageOnATrackPress(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	thumb := barOps(h, b)[1].Bounds
	h.Advance(testBar.Hold + 2*testBar.Fade)

	// On the track, below the thumb, where a visible bar would page by one
	// viewport extent.
	h.TapAt(geom.Pt(b.Max.X-5, thumb.Max.Y+40))

	if got := scroller(h).ScrollInfo().Offset; got != 0 {
		t.Fatalf("a tap on a faded track moved the document to %g; a bar nobody can see "+
			"must not page, or every tap near the trailing edge jumps a screen", got)
	}
}

// TestAVisibleScrollBarStillPagesOnATrackPress is the behaviour the rule above
// must not have taken away from the mouse.
func TestAVisibleScrollBarStillPagesOnATrackPress(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	thumb := barOps(h, b)[1].Bounds
	h.ClickAt(geom.Pt(b.Max.X-5, thumb.Max.Y+40))

	if got := scroller(h).ScrollInfo().Offset; !nearF64(got, float64(scrollViewportH)) {
		t.Fatalf("a click on the visible track moved the document to %g, want one viewport "+
			"extent, %d", got, scrollViewportH)
	}
}

// TestAPressBesideTheThumbWithinTheTouchSlopGrabsIt pins what
// [ui.scrollBarTouchSlop] is for: the drawn thumb is a mouse target, and a
// finger aiming at an eight pixel wide strip at the edge of the screen misses
// it. The press here is fifteen pixels in from the trailing edge, which is
// beside the thumb and not on it.
func TestAPressBesideTheThumbWithinTheTouchSlopGrabsIt(t *testing.T) {
	h := barHarness(t, barScene())
	s := scroller(h)
	b := s.Bounds()

	wakeTo(s, 0)
	thumb := barOps(h, b)[1].Bounds
	beside := geom.Pt(b.Max.X-15, thumb.Min.Y+thumb.Height()/2)
	if thumb.Contains(beside) {
		t.Fatalf("the aiming point %v is on the thumb %v; the fixture is wrong", beside, thumb)
	}

	h.TouchDownAt(beside)
	h.TouchMoveTo(geom.Pt(beside.X, beside.Y+45))

	if got := scroller(h).ScrollInfo().Offset; !nearF64(got, 450) {
		t.Fatalf("offset after grabbing beside the thumb and dragging 45 px = %g, want 450. "+
			"A thumb only grabbable on its drawn eight pixels is a thumb a fingertip misses", got)
	}
	h.TouchUpAt(geom.Pt(beside.X, beside.Y+45))
}
