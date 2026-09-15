package gift

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
