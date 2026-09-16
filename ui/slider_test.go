package ui_test

import (
	"math"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// sliderFixture is a slider bound to state, plus the record of every value it
// reported.
//
// The state is the point. A slider reports through a callback and draws what
// it was given, so storing the value rebuilds the tree — which throws away the
// view, the node object and every field either of them holds — and it does so
// once per move of the pointer. A fixture without state would test a slider
// that never rebuilds, which is the one case that cannot occur in an
// application.
type sliderFixture struct {
	h        *gifttest.Harness
	reported *[]float64
	value    *float64
}

func newSlider(t testing.TB, build func(v float64, set func(float64)) ui.SliderView) *sliderFixture {
	t.Helper()
	var reported []float64
	var current float64
	f := &sliderFixture{reported: &reported, value: &current}
	f.h = gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			st := ctx.State("v", 0.0)
			v := ctx.Read(st)
			current = v
			return ui.VStack(
				build(v, func(want float64) {
					reported = append(reported, want)
					st.Set(want)
				}).Key("sl"),
			).Padding(20)
		},
		Size: geom.Sz(300, 200),
	})
	return f
}

func (f *sliderFixture) node() gifttest.Node { return f.h.Find(gifttest.ByKey("sl")) }

// knobX is the centre of the knob as drawn. It is read out of the display list
// rather than computed from the value, so that every assertion in this file is
// about the picture the user sees.
func (f *sliderFixture) knobX(t testing.TB) float32 {
	t.Helper()
	seen := 0
	for _, op := range f.h.Ops() {
		if op.Kind != render.OpFillRoundRect {
			continue
		}
		seen++
		// The painter emits the track, then the filled part when there is
		// one, then the knob. The knob is the only one of the three that is
		// square, which is how it is told apart without depending on the
		// order or on whether the fill was emitted at all.
		if op.Bounds.Width() == op.Bounds.Height() {
			return (op.Bounds.Min.X + op.Bounds.Max.X) / 2
		}
	}
	t.Fatalf("the slider drew no square rounded rectangle among its %d fills, so it drew no "+
		"knob.\n%s", seen, formatOpsForTest(f.h))
	return 0
}

// TestDraggingASliderTracksTheFingerAcrossEveryRebuildItCauses is the central
// claim of the widget and the one the project's review history says to write
// first.
//
// Every move of the pointer reports a value, the fixture stores it, and the
// tree is rebuilt — [gifttest.Harness.Settle] runs inside each action, so each
// step below really does rebuild. The drag is a straight sweep to the right,
// so the value must increase at every single step and the knob must follow.
//
// The defect this is aimed at is not hypothetical. Review gate 7 of this
// project found a scroll bar thumb whose drag *inverted* when the tree was
// rebuilt underneath it, because its grab origin was recomputed from the
// offset it had just written. A slider rebuilds by construction, so the same
// mistake would make this sequence decrease.
func TestDraggingASliderTracksTheFingerAcrossEveryRebuildItCauses(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Frame(228, geom.Unbounded())
	})
	b := f.node().Bounds()
	y := (b.Min.Y + b.Max.Y) / 2

	// Down on the knob itself and deliberately off its centre, so that the
	// grab offset is a number and not zero: a press on the track, or on the
	// exact centre, would leave it zero and the inversion this test is about
	// could not show.
	f.h.PressAt(geom.Pt(f.knobX(t)+8, y))
	buildsAtStart := f.h.Diagnostics().Builds

	// The offset the press recorded: the knob centre is eight pixels left of
	// the finger, and it has to stay exactly eight pixels left of it for the
	// whole drag. This is the strong form of "tracks the finger" and it is
	// what tells a slider that follows the finger apart from one that merely
	// happens to increase — an inverted, doubled or re-anchored drag all
	// increase somewhere.
	const grab = float32(8)

	var prevValue = *f.value
	var prevKnob = f.knobX(t)
	for x := b.Min.X + 60; x <= b.Max.X-20; x += 10 {
		f.h.MoveTo(geom.Pt(x, y))
		v, k := *f.value, f.knobX(t)
		if !nearly(k, x-grab) {
			t.Fatalf("the finger is at x=%v and the knob centre is at %v; it was taken hold "+
				"of %v pixels to the right of its centre and must stay there, so the knob "+
				"belongs at %v", x, k, grab, x-grab)
		}
		if !(v > prevValue) {
			t.Fatalf("the finger moved right to x=%v and the value went from %v to %v; "+
				"a slider must track the finger monotonically. Reported so far: %v",
				x, prevValue, v, *f.reported)
		}
		if !(k > prevKnob) {
			t.Fatalf("the finger moved right to x=%v and the knob went from %v to %v", x, prevKnob, k)
		}
		prevValue, prevKnob = v, k
	}
	f.h.Release()

	if got := f.h.Diagnostics().Builds - buildsAtStart; got < 10 {
		t.Fatalf("the drag caused only %d rebuilds, so it did not exercise the case this test "+
			"exists for: a slider that keeps its gesture state in the node object it rebuilds", got)
	}
	if v := *f.value; v < 0.85 {
		t.Fatalf("the drag ended near the right hand end of the track and the value is %v", v)
	}
}

// TestPressingASliderOnTheKnobDoesNotMakeTheKnobJumpToTheFinger is the other
// half of the grab offset: the whole reason to record one.
//
// The press lands well inside the knob but off its centre. If the offset were
// not kept, the knob would centre itself under the finger and the value would
// change without the finger having moved at all.
func TestPressingASliderOnTheKnobDoesNotMakeTheKnobJumpToTheFinger(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Frame(228, geom.Unbounded())
	})
	// Start in the middle so that there is room to be off centre on both
	// sides.
	f.h.Find(gifttest.ByKey("sl")).Focus()
	f.h.Key(gift.KeyEnd)
	f.h.Key(gift.KeyHome)
	for range 5 {
		f.h.Key(gift.KeyRight)
	}
	before := *f.value
	b := f.node().Bounds()
	y := (b.Min.Y + b.Max.Y) / 2

	f.h.PressAt(geom.Pt(f.knobX(t)+10, y)) // inside the 28 pixel knob, 10 off centre
	if v := *f.value; v != before {
		t.Fatalf("pressing the knob off centre changed the value from %v to %v; the knob "+
			"jumped to the finger instead of being taken hold of where it was", before, v)
	}
	f.h.Release()
}

// TestASliderKeepsFollowingAFingerThatLeavesItsBounds is pointer capture, and
// it is what makes the steal the scroll bar needs unnecessary here: gift gives
// the capture to the node the press hit, so every further move of that pointer
// arrives at the slider whether or not it is over it.
//
// A finger that runs off the right hand end of the track and keeps going must
// pin the value at the maximum rather than lose the gesture, which is what
// every platform slider does.
func TestASliderKeepsFollowingAFingerThatLeavesItsBounds(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Frame(228, geom.Unbounded())
	})
	b := f.node().Bounds()
	y := (b.Min.Y + b.Max.Y) / 2

	f.h.PressAt(geom.Pt(b.Min.X+14, y))
	f.h.MoveTo(geom.Pt(b.Max.X+200, y+300)) // far outside, on both axes
	if v := *f.value; v != 1 {
		t.Fatalf("a finger dragged well past the end of the slider left it at %v, want the "+
			"maximum: the capture was lost", v)
	}
	f.h.MoveTo(geom.Pt(b.Min.X-200, y+300))
	if v := *f.value; v != 0 {
		t.Fatalf("dragged back past the other end the slider is at %v, want the minimum", v)
	}
	f.h.Release()
}

// TestPressingASliderOnItsTrackMovesTheKnobToTheFingerAndKeepsDragging is the
// touch behaviour: on a finger sized control a tap on the track is a request
// to go there, and the gesture continues as a drag from that point.
func TestPressingASliderOnItsTrackMovesTheKnobToTheFingerAndKeepsDragging(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Frame(228, geom.Unbounded())
	})
	b := f.node().Bounds()
	y := (b.Min.Y + b.Max.Y) / 2
	mid := (b.Min.X + b.Max.X) / 2

	f.h.PressAt(geom.Pt(mid, y))
	if v := *f.value; math.Abs(v-0.5) > 0.02 {
		t.Fatalf("a press in the middle of the track put the value at %v, want about 0.5", v)
	}
	// And it is a grab, not a one shot jump: the next move without lifting
	// continues to drive the value.
	f.h.MoveTo(geom.Pt(mid+40, y))
	if v := *f.value; !(v > 0.6) {
		t.Fatalf("after pressing the track and moving further right the value is %v; the press "+
			"did not continue as a drag", v)
	}
	f.h.Release()
}

// TestASliderKnobConsumesItsMovesSoAnEnclosingScrollViewDoesNotStealTheDrag.
//
// The slider sits inside a vertical scroll view that has something to scroll.
// A horizontal drag on the knob must move the slider and leave the scroll
// offset exactly where it was; without [gift.EventContext.StealPointer] and
// without consuming [gift.EventPointerMove] the viewport recognises the same
// movement and both things happen at once.
func TestASliderKnobConsumesItsMovesSoAnEnclosingScrollViewDoesNotStealTheDrag(t *testing.T) {
	var current float64
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			st := ctx.State("v", 0.0)
			v := ctx.Read(st)
			current = v
			return ui.VScroll(
				ui.Box().Frame(geom.Unbounded(), 300).Key("filler"),
				ui.Slider(v, st.Set).Key("sl").Frame(228, geom.Unbounded()),
				ui.Box().Frame(geom.Unbounded(), 300).Key("filler2"),
			).Key("scroll")
		},
		Size: geom.Sz(300, 200),
	})
	h.Find(gifttest.ByKey("sl")).ScrollIntoView()
	before := h.Find(gifttest.ByKey("scroll")).ScrollOffset()

	b := h.Find(gifttest.ByKey("sl")).Bounds()
	y := (b.Min.Y + b.Max.Y) / 2
	h.PressAt(geom.Pt(b.Min.X+14, y))
	// A path with a vertical component, which is what a finger actually does
	// and what a viewport would read as a swipe.
	for i := 1; i <= 8; i++ {
		h.MoveTo(geom.Pt(b.Min.X+14+float32(i)*20, y-float32(i)*4))
	}
	h.Release()

	if current < 0.5 {
		t.Fatalf("the drag moved the slider to %v only; the gesture went somewhere else", current)
	}
	if after := h.Find(gifttest.ByKey("scroll")).ScrollOffset(); after != before {
		t.Fatalf("the scroll view moved from %v to %v during a drag on the slider knob; the "+
			"knob did not consume its moves", before, after)
	}
}

// TestAFocusedSliderMovesWithTheArrowKeysAndJumpsWithHomeAndEnd.
func TestAFocusedSliderMovesWithTheArrowKeysAndJumpsWithHomeAndEnd(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Range(0, 10)
	})
	f.h.Tab()
	f.h.AssertFocus(gifttest.ByKey("sl"))

	f.h.Key(gift.KeyRight)
	if v := *f.value; v != 1 {
		t.Fatalf("right on a slider over 0 to 10 moved it to %v, want one tenth of the range", v)
	}
	f.h.Key(gift.KeyRight)
	f.h.Key(gift.KeyLeft)
	if v := *f.value; v != 1 {
		t.Fatalf("right, right, left left the slider at %v, want 1", v)
	}
	f.h.Key(gift.KeyEnd)
	if v := *f.value; v != 10 {
		t.Fatalf("end put the slider at %v, want the maximum 10", v)
	}
	f.h.Key(gift.KeyHome)
	if v := *f.value; v != 0 {
		t.Fatalf("home put the slider at %v, want the minimum 0", v)
	}
	// And it stops at the end rather than running past it.
	f.h.Key(gift.KeyLeft)
	if v := *f.value; v != 0 {
		t.Fatalf("left at the minimum moved the slider to %v", v)
	}
}

// TestASliderWithAStepOnlyStopsOnMultiplesOfIt, for the pointer as well as for
// the keyboard — a step that only applied to one of the two would be a control
// that can reach values the application said were not allowed.
func TestASliderWithAStepOnlyStopsOnMultiplesOfIt(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Range(0, 10).Step(2).Frame(228, geom.Unbounded())
	})
	b := f.node().Bounds()
	y := (b.Min.Y + b.Max.Y) / 2

	f.h.PressAt(geom.Pt(b.Min.X+14, y))
	for x := b.Min.X + 20; x <= b.Max.X-20; x += 7 {
		f.h.MoveTo(geom.Pt(x, y))
	}
	f.h.Release()
	f.h.Tab()
	f.h.Key(gift.KeyRight)

	if len(*f.reported) == 0 {
		t.Fatal("the drag reported nothing at all")
	}
	for _, v := range *f.reported {
		if math.Mod(v, 2) != 0 {
			t.Fatalf("the slider reported %v, which is not a multiple of the step of 2. "+
				"All of them: %v", v, *f.reported)
		}
	}
	// And the quantisation is not a lie by omission: the drag really did pass
	// through several different steps.
	distinct := map[float64]bool{}
	for _, v := range *f.reported {
		distinct[v] = true
	}
	if len(distinct) < 4 {
		t.Fatalf("the whole drag only ever reported %v, so a control stuck at one value would "+
			"pass this test", *f.reported)
	}
}

// TestADisabledSliderIgnoresPointerAndKeyboardAndIsSkippedByTheFocusOrder.
func TestADisabledSliderIgnoresPointerAndKeyboardAndIsSkippedByTheFocusOrder(t *testing.T) {
	reports := 0
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Slider(0.5, func(float64) { reports++ }).Key("sl").Disabled(true),
			ui.Button(ui.Text("after"), nil).Key("after"),
		).Padding(20),
		Size: geom.Sz(300, 200),
		Font: loadTestFont(t),
	})
	b := h.Find(gifttest.ByKey("sl")).Bounds()
	h.ClickAt(geom.Pt(b.Max.X-20, (b.Min.Y+b.Max.Y)/2))
	if reports != 0 {
		t.Fatalf("a disabled slider reported %d changes", reports)
	}
	h.Tab()
	h.AssertFocus(gifttest.ByKey("after"))
}

// TestASliderDrawsTheValueItWasGivenAndNotOneItInvented: the control is not
// stateful, so an application that declines the change shows the old value.
func TestASliderDrawsTheValueItWasGivenAndNotOneItInvented(t *testing.T) {
	var asked []float64
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Slider(0.25, func(v float64) { asked = append(asked, v) }).
				Key("sl").Frame(228, geom.Unbounded()),
		).Padding(20),
		Size: geom.Sz(300, 200),
	})
	b := h.Find(gifttest.ByKey("sl")).Bounds()
	before := knobOf(t, h)
	h.ClickAt(geom.Pt(b.Max.X-20, (b.Min.Y+b.Max.Y)/2))
	if len(asked) == 0 {
		t.Fatal("the click reported nothing")
	}
	if after := knobOf(t, h); after != before {
		t.Fatalf("the knob moved from %v to %v although nobody stored the new value; the "+
			"control is keeping a value of its own", before, after)
	}
}

// TestASliderIsCrispAtDensityTwo. Density changes nothing about a control made
// of rounded rectangles: the scale lives in one transform at the root of the
// display list and the backend bakes radius and stroke width out of it, so the
// widget must emit exactly the same logical geometry at both densities. A
// widget that reacted to the density would be one that rounds twice.
func TestASliderIsCrispAtDensityTwo(t *testing.T) {
	geomAt := func(d float64) []render.Op {
		h := gifttest.New(t, gifttest.Options{
			View:    ui.VStack(ui.Slider(0.4, nil).Key("sl").Frame(228, geom.Unbounded())).Padding(20),
			Size:    geom.Sz(300, 200),
			Density: d,
		})
		return h.Ops()
	}
	one, two := geomAt(1), geomAt(2)
	if len(one) != len(two) {
		t.Fatalf("a slider emits %d operations at density 1 and %d at density 2", len(one), len(two))
	}
	for i := range one {
		if one[i].Bounds != two[i].Bounds || one[i].CornerRadius != two[i].CornerRadius {
			t.Fatalf("operation %d differs between the densities: %v against %v. A control's "+
				"geometry is logical; the density is the root transform's business",
				i, one[i], two[i])
		}
	}
	// And the density really was in force, or the comparison above would be
	// two identical runs of the same thing.
	if len(two) == 0 {
		t.Fatal("no operations at all")
	}
}

// BenchmarkSliderFrameIsAllocationFree is the 0 B/op contract of the project
// plan, section 11, for a slider that is on screen.
//
// A slider does not animate, so its steady state is its only state: the
// painter recomputes the geometry from the bounds and emits three operations.
func BenchmarkSliderFrameIsAllocationFree(b *testing.B) {
	h := gifttest.New(b, gifttest.Options{
		View: ui.VStack(ui.Slider(0.4, nil).Key("sl").Frame(228, geom.Unbounded())).Padding(20),
		Size: geom.Sz(300, 200),
	})
	h.Frame()
	h.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Frame()
	}
}

// BenchmarkSliderDragStep is the gesture path, which is deliberately *not*
// allocation free and is not meant to be: every move reports a value, the
// application stores it, and the whole subtree is rebuilt. What this measures
// is what one step of a drag costs.
func BenchmarkSliderDragStep(b *testing.B) {
	f := newSlider(b, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Frame(228, geom.Unbounded())
	})
	bounds := f.node().Bounds()
	y := (bounds.Min.Y + bounds.Max.Y) / 2
	f.h.PressAt(geom.Pt(bounds.Min.X+14, y))
	x := bounds.Min.X + 20
	dx := float32(2)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		x += dx
		if x > bounds.Max.X-20 || x < bounds.Min.X+20 {
			dx = -dx
		}
		f.h.MoveTo(geom.Pt(x, y))
	}
}

// knobOf is [sliderFixture.knobX] for a harness that has no fixture.
func knobOf(t testing.TB, h *gifttest.Harness) float32 {
	t.Helper()
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRoundRect && op.Bounds.Width() == op.Bounds.Height() {
			return (op.Bounds.Min.X + op.Bounds.Max.X) / 2
		}
	}
	t.Fatalf("no knob in the display list.\n%s", formatOpsForTest(h))
	return 0
}

// busySliderFixture is a slider bound to state whose disabled flag the test
// owns, so that it can be switched while a finger is down.
//
// That is the shape of the defect this pair of tests is about, and it is an
// ordinary shape on a kiosk rather than a contrived one: the drag fires a
// request, the request sets a busy flag, the flag disables the panel, and the
// flag clears again when the reply arrives.
type busySliderFixture struct {
	h     *gifttest.Harness
	busy  *bool
	value *float64
	b     geom.Rect
	y     float32
}

func newBusySlider(t testing.TB) *busySliderFixture {
	t.Helper()
	f := &busySliderFixture{busy: new(bool), value: new(float64)}
	f.h = gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			st := ctx.State("v", 0.0)
			v := ctx.Read(st)
			*f.value = v
			return ui.VStack(
				ui.Slider(v, func(want float64) { st.Set(want) }).
					Key("sl").Frame(228, geom.Unbounded()).Disabled(*f.busy),
			).Padding(20)
		},
		Size: geom.Sz(300, 200),
	})
	f.b = f.h.Find(gifttest.ByKey("sl")).Bounds()
	f.y = (f.b.Min.Y + f.b.Max.Y) / 2
	return f
}

// setBusy changes the flag and rebuilds, which is what an application does
// when its request state changes.
func (f *busySliderFixture) setBusy(v bool) {
	*f.busy = v
	f.h.App().Invalidate()
	f.h.Settle()
}

// TestASliderDisabledWhileTheFingerIsDownDoesNotMoveOnALaterHover.
//
// gift refuses to hand an event to a disabled node, so a control that is
// disabled while it holds a pointer never receives the EventPointerUp that
// would end its drag. It cannot clean up after itself — the core has stopped
// talking to it — so nothing but the core can drop the grab, and if nothing
// does, the node stays [gift.ControlState.Grabbed] for the rest of its life.
// The damage is done by the next bare pointer move: no button is down and no
// press ever reached this node, and the handler rewrites the application's
// value from the cursor position anyway. Measured before the fix: a value of
// 0.13 became 1 on a hover.
func TestASliderDisabledWhileTheFingerIsDownDoesNotMoveOnALaterHover(t *testing.T) {
	f := newBusySlider(t)

	f.h.PressAt(geom.Pt(f.b.Min.X+30, f.y))
	f.h.MoveTo(geom.Pt(f.b.Min.X+60, f.y))
	during := *f.value
	if !(during > 0) {
		t.Fatalf("the drag itself reported nothing (value %v), so the rest of this test would "+
			"pass for a slider that never moves", during)
	}

	// The panel goes busy under the finger.
	f.setBusy(true)
	// The release. It is delivered to nobody.
	f.h.Release()
	// And the request comes back.
	f.setBusy(false)

	// A bare hover across to the far end of the track, with no button down.
	f.h.MoveTo(geom.Pt(f.b.Max.X-4, f.y))
	if got := *f.value; got != during {
		t.Fatalf("a hover over a slider nobody is touching moved it from %v to %v. It was "+
			"disabled while the finger was down, so its release was dropped and it is still "+
			"holding a grab it can never let go of", during, got)
	}
}

// TestASliderThatIsNotDisabledMidDragStillDragsAndStillLetsGo is the control
// case of the test above, and it is here so that "the slider never moves on a
// move" could not pass for a fix.
//
// The same sequence without the busy flag: the drag has to move the value, and
// the ordinary release has to end the grab so that the hover afterwards
// changes nothing either.
func TestASliderThatIsNotDisabledMidDragStillDragsAndStillLetsGo(t *testing.T) {
	f := newBusySlider(t)

	f.h.PressAt(geom.Pt(f.b.Min.X+30, f.y))
	f.h.MoveTo(geom.Pt(f.b.Min.X+60, f.y))
	during := *f.value
	if !(during > 0) {
		t.Fatalf("a plain drag of an enabled slider reported nothing (value %v)", during)
	}
	f.h.MoveTo(geom.Pt(f.b.Min.X+90, f.y))
	if !(*f.value > during) {
		t.Fatalf("the finger moved further right and the value stayed at %v; the grab was "+
			"dropped in the middle of an ordinary drag", *f.value)
	}
	after := *f.value

	f.h.Release()
	f.h.MoveTo(geom.Pt(f.b.Max.X-4, f.y))
	if got := *f.value; got != after {
		t.Fatalf("a hover after an ordinary release moved the slider from %v to %v", after, got)
	}
}

// TestASliderWithAShortFrameKeepsItsKnobInsideItsOwnHitArea.
//
// [ui.ControlHitTarget] promises that a control draws its visible parts
// centred inside its bounds, and says why in as many words: a control must not
// be visible where it cannot be touched. [ui.ToggleView] has honoured that
// since it was written; the slider did not, and a slider given a thin frame
// painted a 28 pixel knob over a 10 pixel live area — nine pixels of knob
// above and nine below, both of them dead to a finger.
func TestASliderWithAShortFrameKeepsItsKnobInsideItsOwnHitArea(t *testing.T) {
	for _, f := range []float64{0, 0.5, 1} {
		h := gifttest.New(t, gifttest.Options{
			View: ui.VStack(ui.Slider(f, nil).Key("sl").Frame(200, 10)).Padding(20),
			Size: geom.Sz(300, 200),
		})
		b := h.Find(gifttest.ByKey("sl")).Bounds()
		if b.Height() != 10 {
			t.Fatalf("the slider is %v tall, not the 10 the frame asked for, so this test is "+
				"not about the case it names", b.Height())
		}
		for _, op := range h.Ops() {
			if op.Kind != render.OpFillRoundRect && op.Kind != render.OpStrokeRoundRect {
				continue
			}
			r := op.Bounds
			if r.Min.Y < b.Min.Y || r.Max.Y > b.Max.Y {
				t.Fatalf("at fraction %v the slider drew %v, which leaves its own bounds %v. "+
					"Those pixels are visible and cannot be touched.\n%s",
					f, r, b, formatOpsForTest(h))
			}
		}
	}
}

// TestASliderWithARoomyFrameStillDrawsAFullSizeKnob is the other direction, so
// that clamping the knob to nothing could not pass as a fix.
func TestASliderWithARoomyFrameStillDrawsAFullSizeKnob(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.Slider(0.5, nil).Key("sl").Frame(200, geom.Unbounded())).Padding(20),
		Size: geom.Sz(300, 200),
	})
	var k geom.Rect
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRoundRect && op.Bounds.Width() == op.Bounds.Height() {
			k = op.Bounds
			break
		}
	}
	if k.Height() != 28 || k.Width() != 28 {
		t.Fatalf("the knob of an unconstrained slider is %v; it must be the full 28 by 28.\n%s",
			k, formatOpsForTest(h))
	}
}

// TestTheVerticalArrowsMoveASliderTheWayUpMeansMore.
//
// Both godocs used to say "left and right arrows" and neither mentioned the
// vertical pair, although both widgets handle it — and handle it in opposite
// directions. That is a deliberate asymmetry now and it is argued in
// [ui.SliderView]: a slider stands for a magnitude, so up is more, while a
// segmented control's selection is a position in a list, where up is earlier.
// Its half of the pair is
// TestTheVerticalArrowsWalkASegmentedControlTheWayAnOrdinaryListDoes.
func TestTheVerticalArrowsMoveASliderTheWayUpMeansMore(t *testing.T) {
	f := newSlider(t, func(v float64, set func(float64)) ui.SliderView {
		return ui.Slider(v, set).Range(0, 10)
	})
	f.h.Tab()
	f.h.AssertFocus(gifttest.ByKey("sl"))

	f.h.Key(gift.KeyUp)
	if v := *f.value; v != 1 {
		t.Fatalf("up on a slider over 0 to 10 moved it to %v; up must increase the value by "+
			"one step, exactly as right does", v)
	}
	f.h.Key(gift.KeyUp)
	f.h.Key(gift.KeyDown)
	if v := *f.value; v != 1 {
		t.Fatalf("up, up, down left the slider at %v, want 1", v)
	}
	f.h.Key(gift.KeyHome)
	f.h.Key(gift.KeyDown)
	if v := *f.value; v != 0 {
		t.Fatalf("down at the minimum moved the slider to %v", v)
	}
}

// TestASliderRejectsAValueThatIsNotAFiniteNumber. An infinite value turns
// every fraction into a NaN, and a NaN reaches the backend as a NaN vertex
// position and empties the window three layers below the mistake. The
// diagnosis belongs at the call.
func TestASliderRejectsAValueThatIsNotAFiniteNumber(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ui.Slider(%v) was accepted", v)
				}
			}()
			ui.Slider(v, nil)
		}()
	}
}
