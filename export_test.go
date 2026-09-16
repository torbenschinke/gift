package gift

import "github.com/torbenschinke/gift/internal/scene"

// This file exposes a sliver of the pointer state machine to the external test
// package. It is a _test.go file, so nothing here is part of the API.

// SetMouseDraggedForTest forces the drag flag of the mouse pointer.
//
// It exists so that a test can put the dispatcher into the state the stuck
// scroll defect used to produce — a mouse that carries [Event.Dragged] with no
// button held — without depending on the defect still being there. That is the
// only way to test the second half of the fix independently of the first: once
// [pointer.endGesture] clears the flag, no sequence of public calls reaches
// that state again, and a handler that trusted [Event.Dragged] alone would
// have no failing test left to keep it honest.
func SetMouseDraggedForTest(a *App, v bool) {
	p := &a.in.pointers[0]
	p.active, p.id, p.kind = true, MousePointer, PointerMouse
	p.dragged = v
}

// MaxRepeatsPerTickForTest is [maxRepeatsPerTick], the bound on the number of
// synthetic key repeats one tick may deliver after a stall.
//
// It is exported to the test package so that the test of the bound asserts
// against the constant rather than against a number retyped next to it. The
// test used to allow up to eight where the constant is four, which means a cap
// silently doubled — by a merge, or by somebody "fixing" a sluggish repeat —
// would have kept the test green. A test of a constant that does not name the
// constant only pins the order of magnitude.
const MaxRepeatsPerTickForTest = maxRepeatsPerTick

// NodeSlotForTest returns the storage slot index of the mounted node carrying
// the given reconciliation key, and whether one was found.
//
// It exists so that a test about payload recycling can say which slot it is
// talking about. The scene store hands a freed slot straight back on the next
// mount, and a test that only checks "the new node is clean" passes just as
// happily when the new node landed in a brand new slot and the recycled path
// was never entered — which is how the first version of
// TestAFreshlyMountedControlDoesNotInheritTheGestureOfTheOneBeforeIt managed
// to guard nothing at all.
func NodeSlotForTest(a *App, key string) (uint32, bool) {
	var walk func(h scene.Handle) (uint32, bool)
	walk = func(h scene.Handle) (uint32, bool) {
		if a.store.Get(h).Key == key {
			return h.Index(), true
		}
		for _, c := range a.data(h).children {
			if idx, ok := walk(c); ok {
				return idx, true
			}
		}
		return 0, false
	}
	return walk(a.root.node)
}
