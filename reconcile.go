package gift

import (
	"fmt"

	"github.com/torbenschinke/gift/internal/scene"
)

// childDesc is what the reconciler knows about a child view before it decides
// whether to update an existing node or to mount a new one.
//
// For an ordinary view the element is already built at this point. Build must
// be side effect free precisely because of that: it happens once, before the
// matching, and the result is then applied to whichever node wins.
type childDesc struct {
	view      View
	key       string
	typeID    TypeID
	elem      Element
	component compSource
}

func (a *App) describe(v View) childDesc {
	if v == nil {
		panic("gift: a nil View in a children list")
	}
	if cv, ok := v.(compSource); ok {
		return childDesc{view: v, key: cv.compKey(), typeID: componentTypeID, component: cv}
	}
	elem := v.Build(&a.bctx)
	return childDesc{view: v, key: elem.Key, typeID: v.ViewType(), elem: elem}
}

// buildScope rebuilds one component instance and reconciles its subtree.
//
// The saved building scope is restored with a defer, and the dirty mark is put
// back when the build did not finish. Panics are the normal way gift reports a
// contract violation — a type change under a state key, a slice ownership
// violation — and the project plan, section 15, expects the application to be
// able to see and report them. If a panic left a.building pointing at a dead
// scope, every later [Context.Read] would register its dependency there, and
// since needsBuild had already been cleared the scope would never be rebuilt
// again. A recovered panic has to leave a consistent App behind.
func (a *App) buildScope(sc *scope) {
	if !sc.alive {
		return
	}
	prev := a.building
	a.building = sc
	// The inherited answer to "is any of this on screen", recomputed from the
	// tree because this scope may be the root of its own build: a memoised
	// component three levels inside an inactive tab is rebuilt on its own,
	// with no enclosing applyElement to have carried the flag down. See
	// [App.reconcileHidden].
	prevHidden := a.reconcileHidden
	a.reconcileHidden = a.hiddenAbove(sc.node)
	a.clearDeps(sc)
	// Cleared before the call, not after: a state write from inside the build
	// must survive as a pending rebuild instead of being wiped by the build
	// that caused it.
	sc.needsBuild = false

	done := false
	defer func() {
		a.building = prev
		a.reconcileHidden = prevHidden
		if !done {
			// Abnormal exit. Leave the scope dirty so that the next update
			// retries it instead of treating a half applied build as current.
			a.markNeedsBuild(sc)
		}
	}()

	v := sc.cell.build(&sc.ctx)
	a.diag.Builds++

	if v == nil {
		panic(fmt.Sprintf("gift: component %q returned a nil View", sc.path))
	}
	sc.one[0] = v
	a.reconcileChildren(sc.node, a.data(sc.node), sc.one[:], sc)

	n := a.store.Get(sc.node)
	n.Flags &^= scene.FlagNeedsBuild
	a.markNeedsLayout(sc.node)
	done = true
}

// mountChild allocates a node for desc under parent, fills it in and links it
// into the child list of parent.
//
// The link is made last, after the whole subtree below h exists. That is what
// keeps a panic inside a user component from corrupting the tree: an abnormal
// exit leaves the child list of parent exactly as it was, so the next update
// sees the same previous children and does not mount a second copy. The price
// is that the slots of the half built subtree are leaked until the App is
// dropped, which is the right trade for a diagnosis that the application is
// expected to report and then restart from.
func (a *App) mountChild(parent scene.Handle, desc childDesc, owner *scope) scene.Handle {
	h := a.store.Alloc()
	n := a.store.Get(h)
	n.Key = desc.key
	n.TypeID = uint32(desc.typeID)
	nd := &n.Payload

	if desc.component != nil {
		sc := &scope{
			app:    a,
			key:    desc.key,
			node:   h,
			parent: owner,
			alive:  true,
			cell:   desc.component.newCell(),
		}
		sc.path = scopePath(owner, desc.key)
		sc.ctx = Context{app: a, scope: sc}
		nd.scope = sc
		nd.layouter = passthrough{}
		nd.painter = passthrough{}
		a.liveScopes++

		mounted := false
		defer func() {
			if !mounted {
				// The build of the brand new instance did not finish. It
				// never became part of the tree, so it must not stay in the
				// dirty queue: the next update would rebuild an instance that
				// nothing refers to, on top of the fresh one the parent
				// mounts in its place.
				sc.alive = false
				a.liveScopes--
			}
		}()
		a.buildScope(sc)
		a.store.AppendChild(parent, h)
		mounted = true
		return h
	}

	a.applyElement(h, nd, desc, owner)
	a.store.AppendChild(parent, h)
	return h
}

// applyElement writes the freshly built element onto an existing node and
// reconciles its children.
func (a *App) applyElement(h scene.Handle, nd *nodeData, desc childDesc, owner *scope) {
	nd.view = desc.view
	nd.layouter = desc.elem.Layouter
	nd.painter = desc.elem.Painter
	nd.label = desc.elem.Label
	nd.flex = desc.elem.Flex
	// The input declaration is refreshed from the new element, but the
	// interaction state is not: hover, press and focus belong to the node and
	// must survive a rebuild that happened for an unrelated reason. What does
	// follow the element is Disabled, because a button that was disabled
	// while the mouse rested on it must not keep the hover look.
	nd.interactor = desc.elem.Interactor
	nd.focusable = desc.elem.Focusable && desc.elem.Interactor != nil
	nd.disabled = desc.elem.Disabled
	nd.clip = desc.elem.Clip
	// Before the obstruction below, because that one asks whether this node
	// is visible and the answer is written here.
	a.applyHidden(h, nd, desc.elem.Hidden)
	a.applyFocusTrap(h, nd, desc.elem.FocusTrap)
	a.applyKeyFallback(nd, desc.elem.KeyFallback)
	nd.preservesFocus = desc.elem.PreservesFocus
	nd.obstructs = desc.elem.Obstructs
	// "The last one built wins" is what [Element.Obstructs] promises, and the
	// last one built is not necessarily one anybody can see: a
	// ui.OnScreenKeyboard inside every tab of a ui.TabBar would otherwise
	// register the *inactive* tab's keyboard, and [App.unobstructed] would
	// then reveal a focused field around a rectangle that is not on the
	// screen. So the rule is the last *unhidden* one.
	hiddenHere := nd.hidden || a.reconcileHidden
	if nd.obstructs && !hiddenHere {
		a.setObstruction(h)
	} else if a.in.soft.obstruct == h {
		a.setObstruction(scene.Handle{})
	}
	nd.xform = desc.elem.Transform
	a.applyScroll(nd, desc.elem.Scroll)
	nd.ia.Disabled = desc.elem.Disabled
	if nd.disabled || nd.interactor == nil {
		nd.ia.Hover, nd.ia.Pressed = false, false
		// And the gesture, for the same reason with a sharper edge. A
		// disabled node is not delivered to at all — see [App.deliver] — so
		// the EventPointerUp that would have ended a drag in progress never
		// arrives, and a control disabled while the finger is still down
		// would stay [ControlState.Grabbed] for the rest of its life. The
		// next bare hover over it then reaches a handler that believes it is
		// being dragged and rewrites the application's value from the cursor
		// position. The widget cannot clean that up itself, precisely
		// because the core has stopped talking to it, so the core does it.
		//
		// The trigger is ordinary rather than exotic: a drag fires a request,
		// the request sets a busy flag, the flag disables the panel, and the
		// finger is still down.
		//
		// Only the gesture is dropped. The animation fields are left alone,
		// because a control disabled halfway through a transition should
		// finish it rather than jump, and clearing Armed would make it
		// animate up from zero when it is enabled again.
		nd.control.Grabbed, nd.control.Grab = false, 0
	}
	if a.in.focus == h && !a.focusable(h) {
		a.setFocus(scene.Handle{})
	}

	checkOwnership(nd, desc.elem.Children)
	nd.childViews = desc.elem.Children

	// The children are reconciled before they are linked into the tree, so
	// [App.hiddenAbove] cannot answer for them yet; this is the answer carried
	// down by hand. See [App.reconcileHidden].
	prevHidden := a.reconcileHidden
	a.reconcileHidden = hiddenHere
	a.reconcileChildren(h, nd, desc.elem.Children, owner)
	a.reconcileHidden = prevHidden

	n := a.store.Get(h)
	n.Flags &^= scene.FlagNeedsBuild
	a.markNeedsLayout(h)
}

// applyHidden writes [Element.Hidden] onto the node and takes the input state
// that a hidden node must not keep away from it.
//
// Hiding is not unmounting, so [App.forgetNode] does not run and nothing else
// would clean up after it. Four things have to go, and none of them is on the
// node that carries the flag — that node is a ui.layer, which has no
// interactor at all. Every one of them is on a *descendant*, and every one is
// reachable without walking the subtree, because the dispatcher already holds
// a handle to exactly the node concerned:
//
//   - the keyboard focus, [inputState.focus]. Left alone, the caret blinks
//     where nothing is drawn and every keystroke disappears into it.
//   - the pointer capture, [pointer.capture]. This is the one with teeth: a
//     captured pointer is delivered to unconditionally, so a ui.Slider whose
//     tab is hidden mid-drag goes on writing the application's value from a
//     finger moving over a screen the control is not on, and commits it on
//     release. Measured before this: 0.500 to 0.971.
//   - the hover, [pointer.over], and
//   - the press look, which lives on the captured node.
//
// Each of the three nodes is told, so that a widget holding a gesture of its
// own — a slider's grab, a text field's selection drag — can let go of it. The
// telling is deferred to the end of the build, because no application handler
// may run inside a reconciliation; see pending.go.
//
// Nothing here is O(subtree). The focus question is answered by walking *up*
// from the focused node, and the pointer questions by walking up from two
// handles, so the whole of it is O(depth) with a constant of three.
func (a *App) applyHidden(h scene.Handle, nd *nodeData, hidden bool) {
	was := nd.hidden
	nd.hidden = hidden
	if !hidden || was {
		return
	}
	if a.isAncestor(h, a.in.focus) {
		a.setFocus(scene.Handle{})
	}
	if a.isAncestor(h, a.in.soft.obstruct) {
		// The keyboard went with the tab. A reveal aimed at the rectangle it
		// used to occupy would push a focused field up around nothing; see
		// [App.unobstructed]. The rebuild of the subtree usually clears this
		// on its own — see [App.applyElement] — but a memoised component is
		// not re-applied, so the record is dropped here as well.
		a.setObstruction(scene.Handle{})
	}
	// The three repaint enrolments, stopped rather than left to expire. Every
	// one of them exists to keep drawing something, and a hidden subtree
	// draws nothing; see [App.stopHiddenWork] for the measurements and
	// [TabBarView] for what they are worth on a Pi.
	a.stopHiddenWork(h)
	for i := range a.in.pointers {
		p := &a.in.pointers[i]
		if !p.active {
			continue
		}
		if a.store.Valid(p.over) && a.isAncestor(h, p.over) {
			gone := p.over
			p.over = scene.Handle{}
			a.setHover(gone, false)
			a.deferNotice(gone, noticePointerLeave, p.id)
		}
		if a.store.Valid(p.capture) && a.isAncestor(h, p.capture) {
			gone := p.capture
			// The capture goes first. Everything downstream of a hidden
			// subtree — the move, the release, the long press — is keyed on
			// this handle, and clearing it is what actually stops the drag;
			// the notification below is so that the widget can stop
			// believing it is being dragged.
			p.capture = scene.Handle{}
			p.insideCap = false
			a.setPressed(gone, false)
			a.deferNotice(gone, noticePointerCancel, p.id)
		}
	}
}

// applyFocusTrap writes [Element.FocusTrap] onto the node and, on the build
// that installs one, pulls the focus out of everything the trap excludes.
//
// Clearing rather than moving. The first tab press inside the trap then lands
// on its first focusable node, which is the alert's first button, and no
// application handler runs during a reconciliation to get it there.
func (a *App) applyFocusTrap(h scene.Handle, nd *nodeData, trap bool) {
	was := nd.focusTrap
	nd.focusTrap = trap
	switch {
	case trap && !was:
		a.traps++
	case !trap && was:
		a.traps--
		checkTrapCount(a)
	default:
		return
	}
	if !trap {
		return
	}
	if a.store.Valid(a.in.focus) && !a.isAncestor(h, a.in.focus) {
		a.setFocus(scene.Handle{})
	}
}

// applyKeyFallback writes [Element.KeyFallback] onto the node and maintains
// the counter that lets [App.keyFallbackNode] skip its tree walk in every
// application that never declares one.
//
// Unlike [App.applyFocusTrap] it has no side effect on the focus: a node that
// starts receiving unfocused keys does not take anything away from anybody,
// because the flag only matters while nothing is focused at all.
func (a *App) applyKeyFallback(nd *nodeData, fallback bool) {
	switch {
	case fallback && !nd.keyFallback:
		a.keyFallbacks++
	case !fallback && nd.keyFallback:
		a.keyFallbacks--
		checkTrapCount(a)
	}
	nd.keyFallback = fallback
}

// stopHiddenWork ends every repaint enrolment inside the subtree of h.
//
// This is the second half of "a hidden subtree costs nothing per frame", and
// the first half — not painting it — turned out not to be enough. Three
// mechanisms in this project keep [App.NeedsPaint] true from outside the
// painter, so hiding the node does not stop them:
//
//   - [EventContext.Animate]. ui.Toggle and ui.SegmentedControl re-arm theirs
//     from their *layouter*, and layout does not skip a hidden node: a toggle
//     flipped by a background task while its tab was off screen held the
//     device at full rate for 12 of 60 frames, and a caret enrolment survived
//     for its whole ten second window.
//   - the kinetic fling of [App.tickScrolls]. Measured: a fling in flight
//     when its tab was hidden ran for 140 of 600 frames, 2.24 seconds, and
//     moved the offset from 400 to 1395 document units — so the screen the
//     user came back to was a thousand pixels past where they left it, which
//     is the promise [ui.NavigationStackView] makes and this is what makes it
//     true.
//   - the scroll indicator linger of [App.tickIndicators], which is a fade
//     nobody can see.
//
// All three are short reused slices in the input state, so this is O(enrolled)
// with a walk up the tree per entry, and it runs only on the build in which a
// subtree becomes hidden. An application with nothing animating pays three
// length checks.
//
// What it deliberately does not do is *remember* the enrolments so that they
// could be resumed when the subtree comes back. A fling that is resumed a
// minute later is a screen that scrolls by itself the moment the user returns
// to it, and an animation is a transition between two states that have both
// already been decided: the toggle is drawn in its new position when the tab
// is shown again, without the slide, which is exactly what a tab switch shows
// for every other kind of change inside it.
func (a *App) stopHiddenWork(h scene.Handle) {
	if len(a.in.anims) > 0 {
		out := a.in.anims[:0]
		for _, an := range a.in.anims {
			if !a.isAncestor(h, an.node) {
				out = append(out, an)
			}
		}
		a.in.anims = out
	}
	if len(a.in.flings) > 0 {
		out := a.in.flings[:0]
		for _, f := range a.in.flings {
			if !a.isAncestor(h, f) {
				out = append(out, f)
				continue
			}
			// The velocity has to be cleared as well, not just the tick
			// enrolment: [scrollState.flinging] is what a later
			// [App.startFling] and the gesture code read, and a container
			// that came back holding a velocity would carry on where it left
			// off the next time anything ticked it.
			if s := a.data(f).scroll; s != nil {
				a.stopFling(s)
			}
		}
		a.in.flings = out
	}
	if len(a.in.indicators) > 0 {
		out := a.in.indicators[:0]
		for _, ind := range a.in.indicators {
			if !a.isAncestor(h, ind) {
				out = append(out, ind)
			}
		}
		a.in.indicators = out
	}
}

// isAncestor reports whether anc is h or an ancestor of h.
func (a *App) isAncestor(anc, h scene.Handle) bool {
	for depth := 0; a.store.Valid(h); depth++ {
		if h == anc {
			return true
		}
		if depth > scene.MaxDepth {
			return false
		}
		h = a.store.Get(h).Parent
	}
	return false
}

// updateChild applies desc to the existing node h, which the matcher has
// decided is the same element as before.
func (a *App) updateChild(h scene.Handle, desc childDesc, owner *scope) {
	nd := a.data(h)
	if desc.component != nil {
		sc := nd.scope
		changed := sc.cell.update(desc.view)
		if !changed && !sc.needsBuild {
			// A memoised component whose props are unchanged and whose own
			// state did not fire. Its subtree is still current, so neither
			// its view function nor anything below it runs. This is the
			// rebuild boundary the project plan, section 6, asks for.
			return
		}
		a.buildScope(sc)
		return
	}
	a.applyElement(h, nd, desc, owner)
}

// reconcileChildren matches views against the existing children of h, updates
// the survivors, mounts the new ones, unmounts the rest and finally brings the
// sibling order in line with the view order.
//
// Keyed children are matched by key and type across the whole previous child
// list, which is what makes a reorder keep its state. Unkeyed children are
// matched by position. The keyed search is linear per child and therefore
// quadratic for a long keyed list; that is acceptable while lists are short
// and is the reason virtualised collections do not go through this path.
func (a *App) reconcileChildren(h scene.Handle, nd *nodeData, views []View, owner *scope) {
	old := nd.children

	if cap(nd.used) < len(old) {
		nd.used = make([]bool, len(old))
	} else {
		nd.used = nd.used[:len(old)]
		for i := range nd.used {
			nd.used[i] = false
		}
	}
	next := nd.next[:0]

	for i, v := range views {
		desc := a.describe(v)
		match := -1
		if desc.key != "" {
			for j, oh := range old {
				if nd.used[j] {
					continue
				}
				on := a.store.Get(oh)
				if on.Key == desc.key && on.TypeID == uint32(desc.typeID) {
					match = j
					break
				}
			}
		} else if i < len(old) && !nd.used[i] {
			on := a.store.Get(old[i])
			if on.Key == "" && on.TypeID == uint32(desc.typeID) {
				match = i
			}
		}

		if match >= 0 {
			nd.used[match] = true
			a.updateChild(old[match], desc, owner)
			next = append(next, old[match])
			continue
		}
		next = append(next, a.mountChild(h, desc, owner))
		// mountChild may have grown nd.children indirectly? It cannot: the
		// payload pointer is stable because scene blocks are never
		// reallocated. That stability is the reason nd may be held here.
	}

	for j, oh := range old {
		if !nd.used[j] {
			a.unmount(h, oh)
		}
	}

	nd.next = old[:0]
	nd.children = next
	// SetChildren rewrites the whole sibling chain from next, which is why
	// unmount above does not pay for an O(n) sibling walk per removed child.
	a.store.SetChildren(h, nd.children)
	checkDuplicateKeys(a, h, nd.children)
}

// unmount releases the subtree at child together with every scope and every
// state inside it.
//
// The sibling list of parent is left dangling on purpose: the caller is in the
// middle of a reconciliation and calls SetChildren with the survivors
// immediately afterwards, so repairing the chain here would be undone one line
// later. See [scene.Store.FreeChild].
func (a *App) unmount(parent, child scene.Handle) {
	a.destroyScopes(child, 0)
	a.store.FreeChild(parent, child)
}

// destroyScopes tears down every scope in the subtree at h and releases the
// node payloads, so that the freed slots go back on the free list holding
// nothing but their reusable buffers.
func (a *App) destroyScopes(h scene.Handle, depth int) {
	if depth > scene.MaxDepth {
		panic(fmt.Sprintf(
			"gift: tree deeper than %d levels while unmounting; a component function is building an unbounded tree",
			scene.MaxDepth))
	}
	nd := a.data(h)
	for _, c := range nd.children {
		a.destroyScopes(c, depth+1)
	}
	a.forgetNode(h)
	if sc := nd.scope; sc != nil {
		a.clearDeps(sc)
		sc.alive = false
		sc.states = nil
		sc.cell = nil
		sc.one[0] = nil
		a.liveScopes--
	}
	if nd.focusTrap {
		// The counter [App.focusRoot] consults. release() clears the flag but
		// cannot maintain the count, because a node payload has no App.
		a.traps--
		checkTrapCount(a)
	}
	if nd.keyFallback {
		// The counter [App.keyFallbackNode] consults, maintained for the same
		// reason and in the same two places as a.traps above.
		a.keyFallbacks--
		checkTrapCount(a)
	}
	a.clearOverflow(nd)
	nd.release()
}

func (a *App) data(h scene.Handle) *nodeData {
	return &a.store.Get(h).Payload
}

func scopePath(parent *scope, key string) string {
	if parent == nil {
		return "/" + key
	}
	return parent.path + "/" + key
}
