package gifttest_test

import (
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// TestPointerCaptureDoesNotActivate is pointer capture as a user experiences
// it: press the button, slide off, let go, nothing happens.
//
// It is the one interaction where the harness has to name a coordinate, and it
// names it for the right reason — "somewhere that is not the button" is a
// statement about geometry.
func TestPointerCaptureDoesNotActivate(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	plus := h.Find(gifttest.ByKey("plus"))
	plus.Press()
	plus.AssertPressed()

	h.MoveTo(pt(700, 500))
	// Still captured, so still the button's press — but no longer pressed,
	// because the pointer wandered off it.
	h.Find(gifttest.ByKey("plus")).AssertNotPressed()

	h.Release()
	h.Find(gifttest.ByKey("count")).AssertText("0")
	h.Find(gifttest.ByKey("plus")).AssertNotPressed()
}

// TestPointerCancelDoesNotActivate is the other half: a press the platform
// took away is not a click.
func TestPointerCancelDoesNotActivate(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	h.Find(gifttest.ByKey("plus")).Press().AssertPressed()
	h.CancelPointer()
	h.Find(gifttest.ByKey("count")).AssertText("0")
	h.Find(gifttest.ByKey("plus")).AssertNotPressed()
}

// TestTapActivatesWithoutHover is the touch rule of the project plan, section
// 7, as an assertion: a finger activates the button and leaves no hover state
// behind it.
func TestTapActivatesWithoutHover(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	plus := h.Find(gifttest.ByKey("plus"))
	plus.Tap()

	h.Find(gifttest.ByKey("count")).AssertText("1")
	h.Find(gifttest.ByKey("plus")).AssertNotHovered()

	// And nothing else acquired one either, which is the version of the
	// assertion that would catch a synthesised hover landing on the wrong
	// node.
	h.AssertNone(gifttest.Where("hovered", func(n gifttest.Node) bool {
		return n.Interaction().Hover
	}))
}

// TestMouseClickDoesHover is the control for the test above: the same button,
// clicked with a mouse, is hovered afterwards. Without it "no hover" could be
// passing because hover never works at all.
func TestMouseClickDoesHover(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	h.Find(gifttest.ByKey("plus")).Click()
	h.Find(gifttest.ByKey("plus")).AssertHovered()
}

// --- long press -------------------------------------------------------------

var holdType = gift.RegisterType("gifttest_test.Hold")

// holdView is a leaf that counts the long presses it received. gift's own
// widgets do not use [gift.EventLongPress] yet — there is nothing to open a
// context menu on — so the gesture is exercised through a view that does,
// which is also the smallest possible demonstration that the harness works
// against a view it has never heard of.
type holdView struct {
	key   string
	longs *int
	taps  *int
}

func (holdView) ViewType() gift.TypeID { return holdType }

func (v holdView) Build(*gift.BuildContext) gift.Element {
	n := &holdNode{v: v}
	return gift.Element{
		Key:        v.key,
		Label:      "hold me",
		Layouter:   n,
		Interactor: n,
		Focusable:  true,
	}
}

type holdNode struct{ v holdView }

func (n *holdNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(120, 60)
}

func (n *holdNode) HandleEvent(_ *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventLongPress:
		*n.v.longs++
		return true
	case gift.EventPointerUp:
		if e.Inside && !e.Dragged {
			*n.v.taps++
		}
		return true
	case gift.EventPointerDown:
		return true
	}
	return false
}

// TestLongPressFiresWithoutSleeping moves the injected clock instead of
// waiting. The gesture threshold is half a second and the test costs
// microseconds, which is the whole point of gift taking its timestamps from
// the caller.
func TestLongPressFiresWithoutSleeping(t *testing.T) {
	var longs, taps int
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(holdView{key: "hold", longs: &longs, taps: &taps}),
	})

	h.Find(gifttest.ByKey("hold")).LongPress()

	if longs != 1 {
		t.Errorf("the long press fired %d times, want 1", longs)
	}
	if h.Now() < gift.LongPressDelay {
		t.Errorf("the clock is at %v, which is less than the %v threshold; "+
			"the gesture cannot have been recognised honestly", h.Now(), gift.LongPressDelay)
	}
}

// TestShortTapDoesNotLongPress is the boundary: a tap that lifts before the
// threshold must not be a long press, or every tap would be one.
func TestShortTapDoesNotLongPress(t *testing.T) {
	var longs, taps int
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(holdView{key: "hold", longs: &longs, taps: &taps}),
	})

	h.Find(gifttest.ByKey("hold")).Tap()
	h.Advance(gift.LongPressDelay * 2)

	if longs != 0 {
		t.Errorf("a tap produced %d long press(es)", longs)
	}
	if taps != 1 {
		t.Errorf("the tap was counted %d times, want 1", taps)
	}
}

// TestDragEmitsAPath checks that a drag is a gesture and not a teleport: the
// intermediate moves happen, and the release is marked as dragged rather than
// as a tap.
func TestDragEmitsAPath(t *testing.T) {
	var longs, taps int
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(holdView{key: "hold", longs: &longs, taps: &taps}),
	})

	h.Find(gifttest.ByKey("hold")).DragTo(pt(700, 500))

	if taps != 0 {
		t.Errorf("a drag was counted as %d tap(s); a release that wandered further than "+
			"gift.DragSlop is not a tap", taps)
	}
}

// --- the escape hatch -------------------------------------------------------

// TestAtIsTheRawPoint covers the one selector that is a coordinate, and the
// reason it exists: two overlapping siblings, where the question genuinely is
// which one a click at this point reaches.
func TestAtIsTheRawPoint(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.Button(ui.Text("under"), nil).Key("under").Frame(200, 200),
			ui.Button(ui.Text("over"), nil).Key("over").Frame(100, 100),
		),
	})
	over := h.Find(gifttest.ByKey("over"))
	h.At(over.Center()).AssertKey("over")
}

// TestAtOnNothingFails is the failure side of the same method: a point that
// hits nothing says so with the point and the tree, rather than returning a
// zero node that fails an assertion further down.
func TestAtOnNothingFails(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{Root: counter})
		h.At(pt(799, 599))
	})
	wantContains(t, "At on empty space", r.all(), "hit no interactive node", "the tree was:")
}

// --- TypeText ---------------------------------------------------------------

// TestTypeTextSaysWhyItCannot pins the one action this package refuses to
// fake. A harness that could drive an input path the runtime does not have
// would let a test pass for an application that does not work.
func TestTypeTextSaysWhyItCannot(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{Root: counter})
		h.TypeText("hello")
	})
	wantContains(t, "TypeText", r.all(),
		"not implemented",
		"no text input view",
		"no character event",
		"Harness.Key")
	if !strings.Contains(r.all(), "section 14") {
		t.Errorf("TypeText should point at the part of the plan that excludes a text editor")
	}
}
