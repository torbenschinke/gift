package gift

import "github.com/torbenschinke/gift/internal/scene"

// Focus returns the node that currently holds the keyboard focus, and whether
// anything does.
func (a *App) Focus() (NodeRef, bool) {
	h := a.in.focus
	if !a.store.Valid(h) {
		return NodeRef{}, false
	}
	return NodeRef{h}, true
}

// MoveFocus moves the keyboard focus to the next focusable node in document
// order, or to the previous one when forward is false, and reports whether the
// focus changed.
//
// # Order
//
// Document order is the pre order traversal of the retained tree: a node comes
// before its children, and a child before its later siblings. That is the
// logical tree the application wrote, not the order things happen to be drawn
// in and not the geometry on screen. The project plan, section 10, makes the
// same choice for the gallery — "Selektion und Tastaturnavigation folgen
// stabilen IDs und der logischen Collection-Reihenfolge" — and there is no
// reason for the general case to disagree with it.
//
// The traversal wraps around, and a node that declined to be focusable, or is
// disabled, is skipped rather than focused and immediately passed over.
func (a *App) MoveFocus(forward bool) bool {
	a.assertUIGoroutine("MoveFocus")
	next := a.focusNeighbour(a.in.focus, forward)
	if next.IsZero() || next == a.in.focus {
		return false
	}
	a.setFocus(next)
	return true
}

// setFocus moves the focus to h, emitting the lost and gained pair.
//
// A node that is not focusable — not interactive, disabled, or simply not
// declared focusable — cannot take the focus; passing one is how a click on a
// non focusable target clears the focus rather than parking it somewhere
// invisible.
func (a *App) setFocus(h scene.Handle) {
	if a.store.Valid(h) && !a.focusable(h) {
		h = scene.Handle{}
	}
	if h == a.in.focus {
		return
	}
	prev := a.in.focus
	a.in.focus = h
	// A key held down was being repeated into the node that is losing the
	// focus; see [App.cancelKeyRepeat].
	a.cancelKeyRepeat()
	// Notification is suppressed during a build. The one path that gets here
	// mid build is a node that just declared itself disabled while holding the
	// focus, and running an application event handler in the middle of
	// reconciliation would let it write state and reshape the tree underneath
	// the reconciler. The state itself is still updated, so nothing draws a
	// focus ring it no longer owns; only the courtesy notification is dropped.
	notify := a.building == nil
	if a.store.Valid(prev) {
		a.data(prev).ia.Focused = false
		a.markNeedsPaint(prev)
		if notify {
			a.deliver(prev, Event{Kind: EventFocusLost, Time: a.in.now, Pointer: MousePointer}, true)
		}
	}
	if a.store.Valid(h) {
		a.data(h).ia.Focused = true
		a.markNeedsPaint(h)
		if notify {
			a.deliver(h, Event{Kind: EventFocusGained, Time: a.in.now, Pointer: MousePointer}, true)
		}
	}
}

// focusable reports whether h may hold the keyboard focus.
func (a *App) focusable(h scene.Handle) bool {
	nd := a.data(h)
	return nd.focusable && !nd.disabled && nd.interactor != nil
}

// focusNeighbour returns the focusable node after, or before, from in
// document order, wrapping around.
//
// It flattens the tree into a reusable slice first. A linked successor walk
// over the first child / next sibling representation would need no buffer, but
// it needs the parent chain to step out of a subtree, and doing that for the
// *previous* neighbour means walking to the deepest last descendant of the
// preceding sibling — two mirror image traversals with their own edge cases.
// One flatten, reused across calls and therefore allocation free after the
// first focus change, is the smaller thing to get right. Tab is pressed at
// human speed, not per frame.
func (a *App) focusNeighbour(from scene.Handle, forward bool) scene.Handle {
	// Reachable from the exported [App.MoveFocus], which an application may
	// call before the first Update has built a root — a keyboard shortcut
	// wired up at start up does it. There is no focus order without a tree,
	// and saying so is better than a nil dereference inside the runtime.
	if a.root == nil {
		return scene.Handle{}
	}
	order := a.in.focusScan[:0]
	order = a.appendFocusable(order, a.root.node, 0)
	a.in.focusScan = order
	if len(order) == 0 {
		return scene.Handle{}
	}

	idx := -1
	for i, h := range order {
		if h == from {
			idx = i
			break
		}
	}
	switch {
	case idx < 0 && forward:
		return order[0]
	case idx < 0:
		return order[len(order)-1]
	case forward:
		return order[(idx+1)%len(order)]
	default:
		return order[(idx-1+len(order))%len(order)]
	}
}

func (a *App) appendFocusable(dst []scene.Handle, h scene.Handle, depth int) []scene.Handle {
	if depth > scene.MaxDepth || !a.store.Valid(h) {
		return dst
	}
	nd := a.data(h)
	if nd.focusable && !nd.disabled && nd.interactor != nil {
		dst = append(dst, h)
	}
	for _, c := range nd.children {
		dst = a.appendFocusable(dst, c, depth+1)
	}
	return dst
}
