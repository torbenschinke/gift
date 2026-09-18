package gift

import "github.com/worldiety/gift/internal/scene"

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
	// Moving the focus is the keyboard's own gesture, so the focus it
	// installs is visible; see [Interaction.FocusVisible].
	a.setFocusFrom(next, true)
	return true
}

// FocusVisible reports whether the current focus should be shown as a ring,
// which is the case when it was installed by the keyboard rather than by a
// pointer; see [Interaction.FocusVisible].
//
// It answers false when nothing is focused, because a ring nobody owns cannot
// be visible.
func (a *App) FocusVisible() bool {
	return a.store.Valid(a.in.focus) && a.in.focusVisible
}

// setFocus moves the focus to h, emitting the lost and gained pair.
//
// A node that is not focusable — not interactive, disabled, or simply not
// declared focusable — cannot take the focus; passing one is how a click on a
// non focusable target clears the focus rather than parking it somewhere
// invisible. [App.PointerDown] relies on exactly that sentence: it hands this
// method whatever the press hit, and a press that landed on a plain container
// therefore blurs instead of leaving a focus, and an on-screen keyboard,
// behind on a node the user has just pointed away from.
//
// Whether the focus it installs is *visible* — drawn as a ring — is decided by
// where the call came from; see [App.setFocusFrom] and
// [Interaction.FocusVisible]. This entry point takes the answer from the
// phase gift is in, which is what makes a press hide the ring and a key show
// it without every caller having to say so.
func (a *App) setFocus(h scene.Handle) { a.setFocusFrom(h, a.in.keyPhase) }

// setFocusFrom moves the focus to h and states explicitly whether the new
// focus is keyboard focus that has to be shown.
func (a *App) setFocusFrom(h scene.Handle, visible bool) {
	if a.store.Valid(h) && !a.focusable(h) {
		h = scene.Handle{}
	}
	if h == a.in.focus {
		// The same node, possibly with a different provenance: a keyboard
		// user who tabs onto a node and then touches it must lose the ring,
		// and the reverse must gain it. Nothing else about the focus changes,
		// so no lost/gained pair is owed — only a repaint.
		if a.in.focusVisible != visible {
			a.in.focusVisible = visible
			// Nothing is focused: there is no node to repaint and no ring to
			// take away, and the only reason to write the field at all is
			// that a stale true would outlive the node it described.
			if a.store.Valid(h) {
				a.data(h).ia.FocusVisible = visible
				a.markNeedsPaint(h)
			}
		}
		return
	}
	a.in.focusVisible = visible
	prev := a.in.focus
	a.in.focus = h
	// A key held down was being repeated into the node that is losing the
	// focus; see [App.cancelKeyRepeat].
	a.cancelKeyRepeat()
	// Notification during a build is deferred, not dropped. No application
	// handler may run in the middle of a reconciliation — it would write
	// state and reshape the tree under the reconciler — but a node that lost
	// the focus has real work to do about it, and the one that does the most
	// is ui.TextField: it cancels a ten second caret enrolment, ends a
	// selection drag and withdraws the on-screen keyboard. So the event is
	// queued and delivered at the first safe point in the same update; see
	// pending.go and [App.settleNotices].
	notify := a.building == nil
	if a.store.Valid(prev) {
		pd := a.data(prev)
		pd.ia.Focused, pd.ia.FocusVisible = false, false
		a.markNeedsPaint(prev)
		if notify {
			a.deliver(prev, Event{Kind: EventFocusLost, Time: a.in.now, Pointer: MousePointer}, true)
		} else {
			a.deferNotice(prev, noticeFocusLost, MousePointer)
		}
	}
	if a.store.Valid(h) {
		// The focus came back to a node that is still waiting to be told it
		// lost it. Nothing was lost, so nothing is owed.
		a.dropNotice(h, noticeFocusLost)
		hd := a.data(h)
		hd.ia.Focused, hd.ia.FocusVisible = true, visible
		a.markNeedsPaint(h)
		if notify {
			a.deliver(h, Event{Kind: EventFocusGained, Time: a.in.now, Pointer: MousePointer}, true)
		}
	}
}

// focusable reports whether h may hold the keyboard focus.
//
// It deliberately does *not* ask whether h is inside a subtree that declared
// [Element.Hidden], although that would be a true thing to say about a node
// nobody can see. The two routes into [App.setFocus] are already closed on
// that side — [App.hitNode] does not return a hidden node to a press, and
// [App.appendFocusable] does not put one in the tab order — and the focus that
// is already *on* a node when its layer is hidden is taken away where the
// hiding happens, in [App.applyHidden]. A third check here would be a guard no
// test can enter, which this project treats as prose pretending to be code;
// see ui.disabledIsTheCoresBusiness for the argument in full.
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
	order = a.appendFocusable(order, a.focusRoot(), 0)
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
	// [Element.Hidden]. A tab that is not on screen is not in the tab order,
	// which is the third of the three things that flag turns off; the other
	// two are in [App.paintNode] and [App.hitNode].
	if nd.hidden {
		return dst
	}
	if nd.focusable && !nd.disabled && nd.interactor != nil {
		dst = append(dst, h)
	}
	for _, c := range nd.children {
		dst = a.appendFocusable(dst, c, depth+1)
	}
	return dst
}

// focusRoot is the node the focus order is enumerated from: the last node in
// document order that declared [Element.FocusTrap], or the root of the tree
// when nothing did.
//
// "Last in pre order" is the whole rule, and it gives the two answers a modal
// stack needs without a second concept. An alert presented over a sheet is a
// later sibling, so it wins. A trap nested inside another trap comes after its
// own parent, so the inner one wins. A hidden subtree is skipped, so a trap
// inside an inactive tab is not a trap at all.
//
// It is one extra pre order walk per focus change. Tab is pressed at human
// speed; the walk allocates nothing and is skipped entirely — the recursion
// does not even start — when no node in the process ever declared a trap,
// because [App.traps] is then zero.
func (a *App) focusRoot() scene.Handle {
	if a.traps == 0 {
		return a.root.node
	}
	found := scene.Handle{}
	a.findTrap(a.root.node, 0, &found)
	if found.IsZero() {
		return a.root.node
	}
	return found
}

func (a *App) findTrap(h scene.Handle, depth int, out *scene.Handle) {
	if depth > scene.MaxDepth || !a.store.Valid(h) {
		return
	}
	nd := a.data(h)
	if nd.hidden {
		return
	}
	if nd.focusTrap {
		*out = h
	}
	for _, c := range nd.children {
		a.findTrap(c, depth+1, out)
	}
}
