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
