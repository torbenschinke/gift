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
	view        View
	key         string
	typeID      TypeID
	elem        Element
	isComponent bool
	component   componentView
}

func (a *App) describe(v View) childDesc {
	if v == nil {
		panic("gift: a nil View in a children list")
	}
	if cv, ok := v.(componentView); ok {
		return childDesc{view: v, key: cv.key, typeID: componentTypeID, isComponent: true, component: cv}
	}
	elem := v.Build(&a.bctx)
	return childDesc{view: v, key: elem.Key, typeID: v.ViewType(), elem: elem}
}

// buildScope rebuilds one component instance and reconciles its subtree.
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
	v := sc.fn(&sc.ctx)
	a.building = prev
	a.diag.Builds++

	if v == nil {
		panic(fmt.Sprintf("gift: component %q returned a nil View", sc.path))
	}
	sc.one[0] = v
	nd := a.data(sc.node)
	a.reconcileChildren(sc.node, nd, sc.one[:], sc)

	n := a.store.Get(sc.node)
	n.Flags &^= scene.FlagNeedsBuild
	n.Flags |= scene.FlagNeedsLayout | scene.FlagNeedsPaint
}

// mountChild allocates a node for desc under parent and fills it in.
func (a *App) mountChild(parent scene.Handle, desc childDesc, owner *scope) scene.Handle {
	h := a.store.Alloc()
	nd := &nodeData{}
	n := a.store.Get(h)
	n.Key = desc.key
	n.TypeID = uint32(desc.typeID)
	n.Payload = nd
	a.store.AppendChild(parent, h)

	if desc.isComponent {
		sc := &scope{
			app:    a,
			key:    desc.component.key,
			fn:     desc.component.fn,
			node:   h,
			parent: owner,
			alive:  true,
		}
		sc.path = scopePath(owner, desc.component.key)
		sc.ctx = Context{app: a, scope: sc}
		nd.scope = sc
		nd.layouter = passthrough{}
		nd.painter = passthrough{}
		a.liveScopes++
		a.buildScope(sc)
		return h
	}

	a.applyElement(h, nd, desc, owner)
	return h
}

// applyElement writes the freshly built element onto an existing node and
// reconciles its children.
func (a *App) applyElement(h scene.Handle, nd *nodeData, desc childDesc, owner *scope) {
	nd.view = desc.view
	nd.layouter = desc.elem.Layouter
	nd.painter = desc.elem.Painter

	checkOwnership(nd, desc.elem.Children)
	nd.childViews = desc.elem.Children

	a.reconcileChildren(h, nd, desc.elem.Children, owner)

	n := a.store.Get(h)
	n.Flags &^= scene.FlagNeedsBuild
	n.Flags |= scene.FlagNeedsLayout | scene.FlagNeedsPaint
}

// updateChild applies desc to the existing node h, which the matcher has
// decided is the same element as before.
func (a *App) updateChild(h scene.Handle, desc childDesc, owner *scope) {
	nd := a.data(h)
	if desc.isComponent {
		sc := nd.scope
		sc.fn = desc.component.fn
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
	}

	for j, oh := range old {
		if !nd.used[j] {
			a.unmount(oh)
		}
	}

	nd.next = old[:0]
	nd.children = next
	a.store.SetChildren(h, nd.children)
}

// unmount releases the subtree at h together with every scope and every state
// inside it.
func (a *App) unmount(h scene.Handle) {
	a.destroyScopes(h)
	a.store.Free(h)
}

func (a *App) destroyScopes(h scene.Handle) {
	nd := a.data(h)
	for _, c := range nd.children {
		a.destroyScopes(c)
	}
	if sc := nd.scope; sc != nil {
		a.clearDeps(sc)
		sc.alive = false
		sc.states = nil
		sc.fn = nil
		nd.scope = nil
		a.liveScopes--
	}
	nd.view = nil
	nd.childViews = nil
	nd.layouter = nil
	nd.painter = nil
}

func (a *App) data(h scene.Handle) *nodeData {
	return a.store.Get(h).Payload.(*nodeData)
}

func scopePath(parent *scope, key string) string {
	if parent == nil {
		return "/" + key
	}
	return parent.path + "/" + key
}
