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
	a.clearDeps(sc)
	// Cleared before the call, not after: a state write from inside the build
	// must survive as a pending rebuild instead of being wiped by the build
	// that caused it.
	sc.needsBuild = false

	done := false
	defer func() {
		a.building = prev
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
	nd.obstructs = desc.elem.Obstructs
	if nd.obstructs {
		a.setObstruction(h)
	} else if a.in.soft.obstruct == h {
		a.setObstruction(scene.Handle{})
	}
	nd.xform = desc.elem.Transform
	a.applyScroll(nd, desc.elem.Scroll)
	nd.ia.Disabled = desc.elem.Disabled
	if nd.disabled || nd.interactor == nil {
		nd.ia.Hover, nd.ia.Pressed = false, false
	}
	if a.in.focus == h && !a.focusable(h) {
		a.setFocus(scene.Handle{})
	}

	checkOwnership(nd, desc.elem.Children)
	nd.childViews = desc.elem.Children

	a.reconcileChildren(h, nd, desc.elem.Children, owner)

	n := a.store.Get(h)
	n.Flags &^= scene.FlagNeedsBuild
	a.markNeedsLayout(h)
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
