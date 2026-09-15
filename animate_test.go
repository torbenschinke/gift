package gift_test

import (
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var animType = gift.RegisterType("test.Animated")

// animated is a leaf that asks to be repainted for a window when it is
// pressed, and records the clock its painter was given.
type animated struct {
	window time.Duration
	seen   *[]time.Duration
}

func (animated) ViewType() gift.TypeID { return animType }

func (a animated) Build(*gift.BuildContext) gift.Element {
	n := &animatedNode{a: a}
	return gift.Element{Layouter: n, Painter: n, Interactor: n, Focusable: true}
}

type animatedNode struct{ a animated }

func (n *animatedNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(50, 50)
}

func (n *animatedNode) Paint(ctx *gift.PaintContext) {
	if n.a.seen != nil {
		*n.a.seen = append(*n.a.seen, ctx.Now())
	}
}

func (n *animatedNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventPointerDown:
		ctx.RequestFocus()
		ctx.Animate(n.a.window)
		return true
	case gift.EventKeyDown:
		// Escape cancels, which is the shape a text field uses when it loses
		// the focus.
		if e.Key == gift.KeyEscape {
			ctx.Animate(0)
			return true
		}
	}
	return false
}

// TestAnimateKeepsTheApplicationPaintingUntilItsDeadline is the contract of
// [gift.EventContext.Animate] in both directions: frames keep being asked for
// while the window is open, and the application is allowed to go to sleep the
// moment it closes.
//
// The second half is the one worth having. An animation API without a deadline
// is indistinguishable from one with a deadline until somebody leaves the
// window open overnight, which is exactly the kiosk this framework is for.
func TestAnimateKeepsTheApplicationPaintingUntilItsDeadline(t *testing.T) {
	const window = 100 * time.Millisecond
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return animated{window: window}
	}})
	now := time.Duration(0)
	frame := func() {
		a.BeginInput(now)
		if err := a.Update(geom.Sz(200, 200)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	frame()
	if a.NeedsPaint() {
		t.Fatal("the application asks for frames before anything animates")
	}

	a.BeginInput(now)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(10, 10))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(10, 10))
	frame()

	for range 5 {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		if !a.NeedsPaint() {
			t.Fatalf("at %v, %v into a %v window, the application stopped asking for frames",
				now, now, window)
		}
		a.Paint()
	}

	now += window
	a.BeginInput(now)
	a.Paint()
	// One more tick: the expiring one still repaints, so the frame in which
	// the animation is over is drawn.
	now += 16 * time.Millisecond
	a.BeginInput(now)
	if a.NeedsPaint() {
		t.Fatal("the application still asks for frames after the animation window closed")
	}
}

// TestAnimateZeroCancelsAndStillRepaintsOnce. The repaint is not a detail: it
// is the frame in which whatever was animating disappears.
func TestAnimateZeroCancelsAndStillRepaintsOnce(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return animated{window: time.Hour}
	}})
	now := time.Duration(0)
	a.BeginInput(now)
	_ = a.Update(geom.Sz(200, 200))
	a.Paint()

	a.BeginInput(now)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(10, 10))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(10, 10))
	a.Paint()

	now += 16 * time.Millisecond
	a.BeginInput(now)
	if !a.NeedsPaint() {
		t.Fatal("the hour long window is not keeping the application awake")
	}
	a.Paint()

	a.BeginInput(now)
	a.KeyDown(gift.KeyEscape, 0)
	if !a.NeedsPaint() {
		t.Fatal("cancelling did not ask for the one frame that draws the cancellation")
	}
	a.Paint()

	now += 16 * time.Millisecond
	a.BeginInput(now)
	if a.NeedsPaint() {
		t.Fatal("the application still asks for frames after Animate(0)")
	}
}

// TestThePainterClockIsTheInjectedOne. A painter that read the wall clock
// would make every animation untestable and two painters in one frame disagree
// about what now is.
func TestThePainterClockIsTheInjectedOne(t *testing.T) {
	var seen []time.Duration
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return animated{window: time.Second, seen: &seen}
	}})
	for _, at := range []time.Duration{0, 5 * time.Second, 1234 * time.Millisecond} {
		a.BeginInput(at)
		if err := a.Update(geom.Sz(200, 200)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	want := []time.Duration{0, 5 * time.Second, 1234 * time.Millisecond}
	if len(seen) != len(want) {
		t.Fatalf("the painter ran %d times, want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("frame %d was painted with PaintContext.Now() = %v, want %v",
				i, seen[i], want[i])
		}
	}
}

// TestAnimationsOfAnUnmountedNodeAreForgotten. A node that disappears while it
// is animating would otherwise keep an application awake for the rest of its
// window with nothing on screen to show for it.
func TestAnimationsOfAnUnmountedNodeAreForgotten(t *testing.T) {
	show := true
	var state *gift.State[bool]
	a := gift.New(gift.Options{Root: func(ctx *gift.Context) gift.View {
		state = ctx.State("show", show)
		if ctx.Read(state) {
			return animated{window: time.Hour}
		}
		return frameView{}
	}})
	now := time.Duration(0)
	a.BeginInput(now)
	_ = a.Update(geom.Sz(200, 200))
	a.Paint()

	a.BeginInput(now)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(10, 10))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(10, 10))
	a.Paint()

	state.Set(false)
	now += 16 * time.Millisecond
	a.BeginInput(now)
	_ = a.Update(geom.Sz(200, 200))
	a.Paint()

	now += 16 * time.Millisecond
	a.BeginInput(now)
	if a.NeedsPaint() {
		t.Fatal("the application still asks for frames for a node that was unmounted")
	}
}
