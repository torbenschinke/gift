package gift_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var controlTestType = gift.RegisterType("test.Control")

// controlView is the smallest possible consumer of [gift.ControlState]: a leaf
// that writes the state on a press and reports what it read back.
type controlView struct {
	key string
	// write is what the press stores.
	write gift.ControlState
	// read receives whatever the node had when the event arrived.
	read *gift.ControlState
	// moveWrites are written one per [gift.EventPointerMove], in order, and
	// the last one is repeated once the slice is exhausted. It is how a test
	// can write the *same* state twice without a press in between.
	moveWrites *[]gift.ControlState
	// reportMove receives the state the node held when a move arrived. It is
	// the hover path, which is where an inherited grab does its damage.
	reportMove *gift.ControlState
}

func (controlView) ViewType() gift.TypeID { return controlTestType }

func (v controlView) Build(*gift.BuildContext) gift.Element {
	n := &controlTestNode{v: v}
	return gift.Element{
		Key:        v.key,
		Layouter:   n,
		Interactor: n,
		Focusable:  true,
	}
}

type controlTestNode struct{ v controlView }

func (n *controlTestNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(100, 100)
}

func (n *controlTestNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventPointerDown:
		if n.v.read != nil {
			*n.v.read = ctx.ControlState()
		}
		ctx.SetControlState(n.v.write)
		return true
	case gift.EventPointerUp:
		if n.v.read != nil {
			*n.v.read = ctx.ControlState()
		}
		return true
	case gift.EventPointerMove:
		if n.v.reportMove != nil {
			*n.v.reportMove = ctx.ControlState()
			return true
		}
		if w := n.v.moveWrites; w != nil && len(*w) > 0 {
			ctx.SetControlState((*w)[0])
			if len(*w) > 1 {
				*w = (*w)[1:]
			}
			return true
		}
	}
	return false
}

// TestControlStateSurvivesARebuildOfTheViewThatDeclaredIt is the property the
// whole type exists for.
//
// A control that reports its value through a callback rebuilds on every step
// of its own gesture, which replaces the view *and* the node object. Anything
// the gesture remembers in either of them is therefore gone between two moves
// of one finger. This test rebuilds the root between the press and the release
// and asserts that what the press wrote is still there.
func TestControlStateSurvivesARebuildOfTheViewThatDeclaredIt(t *testing.T) {
	var got gift.ControlState
	builds := 0
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		builds++
		return controlView{key: "c", write: gift.ControlState{Grabbed: true, Grab: 7.5}, read: &got}
	}})
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))

	// The rebuild, which is what a slider's own onChange causes.
	a.Invalidate()
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}
	if builds < 2 {
		t.Fatalf("the tree was built %d times; this test did not rebuild anything", builds)
	}

	a.BeginInput(0)
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))
	if !got.Grabbed || got.Grab != 7.5 {
		t.Fatalf("after a rebuild the control state is %+v, want the grab the press wrote. "+
			"A gesture whose origin is lost mid drag either jumps or inverts", got)
	}
}

// TestAFreshlyMountedControlDoesNotInheritTheGestureOfTheOneBeforeIt.
//
// The scene store hands a freed slot back with its payload untouched, which is
// what makes an unmount/remount cycle allocation free. A control's gesture
// state is not refreshed from the element — a rebuild replaces the view and
// not the node, which is the whole reason the state lives there — so it has to
// be dropped when the node is unmounted. Without that, a new control is born
// [gift.ControlState.Grabbed] holding a grab offset it never took, and moves
// on the very next pointer move without a press.
//
// # Why this takes three updates and an assertion about the slot
//
// [App.reconcileChildren] mounts every new child before it unmounts the
// leftovers, so replacing key "a" with key "b" in *one* update finds the free
// list empty and gives "b" a brand new slot. A test written that way asserts
// only that a virgin slot is zero, which it is whatever the code under test
// does. So the control is first replaced by something that is not a control at
// all — which frees the slot — and only the update after that mounts the new
// control, which does land in the freed slot. The test says so explicitly:
// if the slot indices ever stop matching, the recycled path was not entered
// and this test is again guarding nothing.
func TestAFreshlyMountedControlDoesNotInheritTheGestureOfTheOneBeforeIt(t *testing.T) {
	var got gift.ControlState
	phase := 0
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		switch phase {
		case 0:
			return controlView{key: "a", write: gift.ControlState{Grabbed: true, Grab: 11}}
		case 1:
			return fillerView{key: "f"}
		default:
			return controlView{key: "b", read: &got}
		}
	}})
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}
	slotA, ok := gift.NodeSlotForTest(a, "a")
	if !ok {
		t.Fatal("the control was not mounted")
	}

	// A press that grabs, and a release the widget under test deliberately
	// does not clean up after: the node keeps Grabbed and Grab for its life.
	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))
	a.BeginInput(0)
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))

	// Replace the control with something of a different type. The filler is
	// mounted into a fresh slot and only then is the control unmounted, so
	// this is the update that puts the control's slot on the free list.
	phase = 1
	a.Invalidate()
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}

	// And now a control again. It is mounted before the filler is unmounted,
	// so it takes the slot the free list is holding: the first control's.
	phase = 2
	a.Invalidate()
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}
	slotB, ok := gift.NodeSlotForTest(a, "b")
	if !ok {
		t.Fatal("the second control was not mounted")
	}
	if slotA != slotB {
		t.Fatalf("the new control landed in slot %d and the old one used slot %d, so the "+
			"recycled payload was never involved and this test proves nothing. Fix the "+
			"sequence above, not this assertion", slotB, slotA)
	}

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))

	if got != (gift.ControlState{}) {
		t.Fatalf("a freshly mounted control was born holding %+v, which belonged to the node "+
			"that used the slot before it", got)
	}
}

// TestAControlThatIsUnmountedWhileGrabbedDoesNotDragOnARecycledSlot is the
// same property seen from the pointer, which is where it hurts: the inherited
// grab is not an odd value in a struct, it is a control that moves without
// anybody having pressed it.
func TestAControlThatIsUnmountedWhileGrabbedDoesNotDragOnARecycledSlot(t *testing.T) {
	var moved gift.ControlState
	phase := 0
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		switch phase {
		case 0:
			return controlView{key: "a", write: gift.ControlState{Grabbed: true, Grab: 11}}
		case 1:
			return fillerView{key: "f"}
		default:
			return controlView{key: "b", reportMove: &moved}
		}
	}})
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}
	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))
	a.BeginInput(0)
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))

	for _, p := range []int{1, 2} {
		phase = p
		a.Invalidate()
		if err := a.Update(geom.Sz(200, 200)); err != nil {
			t.Fatal(err)
		}
	}

	// A bare hover over the new control: no button is down and no press ever
	// reached this node.
	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(60, 60))
	if moved.Grabbed {
		t.Fatalf("a hover over a control that was never pressed found it holding %+v, so it "+
			"would have dragged its value to the cursor", moved)
	}
}

// fillerView is a leaf of a different type, used to occupy the tree for one
// update so that a control's slot reaches the free list; see
// TestAFreshlyMountedControlDoesNotInheritTheGestureOfTheOneBeforeIt.
type fillerView struct{ key string }

var fillerTestType = gift.RegisterType("test.Filler")

func (fillerView) ViewType() gift.TypeID { return fillerTestType }

func (v fillerView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: v.key, Layouter: fillerNode{}}
}

type fillerNode struct{}

func (fillerNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size { return geom.Sz(10, 10) }

// TestWritingTheSameControlStateAgainRepaintsNothing. The write is on the
// pointer path of every control in package ui — a slider writes on every move
// of the finger — so a redundant one must not mark the tree dirty. Without the
// comparison the node would be repainted on every move that changed nothing.
func TestWritingTheSameControlStateAgainRepaintsNothing(t *testing.T) {
	writes := []gift.ControlState{{Grabbed: true, Grab: 1}, {Grabbed: true, Grab: 2}}
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return controlView{key: "c", moveWrites: &writes}
	}})
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}
	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(50, 50))
	a.Paint()

	// A move that writes a *different* state. It must repaint, or the test
	// below would pass for a node that never repaints at all.
	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(51, 50))
	if !a.NeedsPaint() {
		t.Fatal("a control that changed its state was not marked for repaint")
	}
	a.Paint()

	// The same state again. The slice is exhausted, so the node writes the
	// value it already holds.
	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(52, 50))
	if a.NeedsPaint() {
		t.Fatal("writing the identical control state marked the node for repaint")
	}
}
