package gift

import (
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// NodeRef refers to one node of the retained tree.
//
// It is an opaque, comparable value: an application may hold it, compare it
// and ask the App about it, but it cannot construct one and it cannot reach
// into the node. The zero NodeRef refers to nothing.
//
// It exists so that the few methods which have to name a node —
// [App.HitTest], [App.Focus] — do not expose the handle type of an internal
// package in the public API.
type NodeRef struct{ h scene.Handle }

// IsZero reports whether r refers to no node.
func (r NodeRef) IsZero() bool { return r.h.IsZero() }

// HitTest returns the topmost interactive node at the device space point p,
// and whether there is one.
//
// It is exported for tests, for diagnostics and for an application that wants
// to answer the question itself; the dispatcher uses the same walk for every
// pointer event.
func (a *App) HitTest(p geom.Point) (NodeRef, bool) {
	a.assertUIGoroutine("HitTest")
	h := a.hitTest(p)
	return NodeRef{h}, !h.IsZero()
}

// NodeKey returns the reconciliation key of the referenced node, or the empty
// string for an unkeyed or stale reference. It is meant for diagnostics.
func (a *App) NodeKey(r NodeRef) string {
	if !a.store.Valid(r.h) {
		return ""
	}
	return a.store.Get(r.h).Key
}

// NodeBounds returns the absolute rectangle of the referenced node as of the
// last layout pass, or the zero Rect for a stale reference.
func (a *App) NodeBounds(r NodeRef) geom.Rect {
	if !a.store.Valid(r.h) {
		return geom.Rect{}
	}
	return a.store.Get(r.h).Bounds
}

// NodeInteraction returns the hover, press, focus and disabled state of the
// referenced node. It is how a test asserts that a button looks pressed
// without going through the display list.
func (a *App) NodeInteraction(r NodeRef) Interaction {
	if !a.store.Valid(r.h) {
		return Interaction{}
	}
	return a.data(r.h).ia
}

// hitTest walks the retained tree front to back and returns the first
// interactive node that contains p.
//
// # Why the retained tree and not the display list
//
// The display list is flat, is only produced during Paint and carries no
// identity: an operation knows its rectangle and its colour, not which node
// emitted it. Hit testing has to answer "which node", it has to run in
// Update, and it has to work for a node that draws nothing at all — an
// invisible touch target is a normal thing to want. So it walks the tree.
//
// # Coordinates
//
// The project plan, section 7, requires input, clipping and hit testing to use
// the same coordinate transformations, and fixes the convention: clips are
// device space and transforms map bounds into device space. This walk does
// exactly that. It carries the composed local-to-device transform down, maps
// each node's bounds through it, and intersects the clip of a node that
// declares [Element.Clip] into an inherited device space rectangle. The
// painter composes the same two things from the same two fields; see
// [App.paintNode].
//
// Since WU-L there is a real producer: a scroll container translates its
// children, and this walk composes exactly what [App.beginSubtree] pushes into
// the display list. The composition was written here rather than added later because the alternative is a
// second traversal that drifts from the first one, which is the failure the
// plan names.
//
// # Order
//
// Children are visited in reverse and before the node itself, which inverts
// the drawing order of background, children, border: the last child is drawn
// on top and therefore wins the hit, and a container that draws a background
// behind its children only wins where no child did. Overlapping [ui.ZStack]
// children resolve by the same rule.
//
// # Cost
//
// One recursion, no allocation, no closures. The project plan, section 11,
// puts input processing inside the zero allocation contract.
func (a *App) hitTest(p geom.Point) scene.Handle {
	a.diag.HitTests++
	if a.root == nil || !a.store.Valid(a.root.node) {
		return scene.Handle{}
	}
	return a.hitNode(a.root.node, p, unboundedClip, geom.Identity(), 0)
}

// unboundedClip is the device space rectangle that contains every plausible
// coordinate. It mirrors the sentinel clip of [render.List], for the same
// reason: a stack that starts with "no clip" needs a value, and an infinity
// would turn every intersection into a NaN.
var unboundedClip = geom.Rc(-1e30, -1e30, 1e30, 1e30)

func (a *App) hitNode(h scene.Handle, p geom.Point, clip geom.Rect, m geom.Affine2D, depth int) scene.Handle {
	if depth > scene.MaxDepth {
		return scene.Handle{}
	}
	n := a.store.Get(h)
	nd := &n.Payload
	// [Element.Hidden]. The input half of the check in [App.paintNode], in
	// the same place and for the same reason: a subtree nobody can see must
	// not answer a tap either.
	if nd.hidden {
		return scene.Handle{}
	}

	if nd.xform != nil {
		m = nd.xform.Mul(m)
	}
	bounds := m.TransformRect(n.Bounds)
	if nd.clip {
		clip = clip.Intersect(bounds)
		// An empty clip means nothing below this node can be hit. Returning
		// here is not just an optimisation: without it a child that overflows
		// its clipped parent would still answer, which is precisely the
		// "clipped ancestor" case.
		if clip.IsEmpty() || !clip.Contains(p) {
			return scene.Handle{}
		}
	}

	for i := len(nd.children) - 1; i >= 0; i-- {
		if hit := a.hitNode(nd.children[i], p, clip, a.childXform(nd, m), depth+1); !hit.IsZero() {
			return hit
		}
	}

	if nd.interactor == nil {
		// Transparent to input. See [Interactor] for why this is the
		// permissive default and not the nil painter trap.
		return scene.Handle{}
	}
	if bounds.Contains(p) && clip.Contains(p) {
		return h
	}
	return scene.Handle{}
}

// childXform returns the local to device transform the children of nd see.
//
// It is m plus the translation of a scroll offset, and it is the input half of
// [App.beginSubtree]. The two functions must agree; they are two lines each
// and they sit in two files because one walks the tree for paint and the other
// for input, which is the one duplication the project plan, section 7,
// accepts — provided both read the same declaration, which they do.
func (a *App) childXform(nd *nodeData, m geom.Affine2D) geom.Affine2D {
	if s := nd.scroll; s != nil {
		return s.xform().Mul(m)
	}
	return m
}

// hits reports whether the device space point p lands on the node h, taking
// the clips and transforms of its ancestors into account.
//
// It is the question pointer capture asks on every move and on the release:
// "is the pointer still over the node that took the press". It is not the same
// question as [App.hitTest] — a node that is covered by a sibling still
// answers true here, because a captured press belongs to the node that took
// it, not to whatever is now on top.
func (a *App) hits(h scene.Handle, p geom.Point) bool {
	if !a.store.Valid(h) {
		return false
	}
	n := a.store.Get(h)
	if n.Payload.interactor == nil || a.hiddenAbove(h) {
		return false
	}
	clip, m, ok := a.deviceSpace(h)
	if !ok {
		return false
	}
	return m.TransformRect(n.Bounds).Contains(p) && clip.Contains(p)
}

// hiddenAbove reports whether h or any node above it declared
// [Element.Hidden].
//
// [App.hitNode] does not need it — it descends and stops at the hidden node —
// but the two questions that start from a node and look upwards do: whether a
// captured pointer is still on its node, and whether a node is on the screen
// at all. It walks the parent chain and allocates nothing.
func (a *App) hiddenAbove(h scene.Handle) bool {
	for depth := 0; a.store.Valid(h); depth++ {
		if a.data(h).hidden {
			return true
		}
		if depth > scene.MaxDepth {
			return false
		}
		h = a.store.Get(h).Parent
	}
	return false
}

// deviceSpace returns the inherited clip and transform of h, by walking up to
// the root and composing on the way back down.
//
// It walks up rather than down because a capture test knows the node and not
// the path. The recursion is bounded by [scene.MaxDepth] and allocates
// nothing.
func (a *App) deviceSpace(h scene.Handle) (geom.Rect, geom.Affine2D, bool) {
	return a.deviceSpaceAt(h, 0)
}

func (a *App) deviceSpaceAt(h scene.Handle, depth int) (geom.Rect, geom.Affine2D, bool) {
	if depth > scene.MaxDepth || !a.store.Valid(h) {
		return unboundedClip, geom.Identity(), false
	}
	n := a.store.Get(h)
	clip, m := unboundedClip, geom.Identity()
	if !n.Parent.IsZero() {
		var ok bool
		clip, m, ok = a.deviceSpaceAt(n.Parent, depth+1)
		if !ok {
			return clip, m, false
		}
		// The parent's scroll offset applies to its children, which is what
		// this node is. Leaving it out was the same omission as the raw clip
		// rectangle in the painter: correct for as long as no transform
		// existed, silently wrong the moment one did.
		m = a.childXform(a.data(n.Parent), m)
	}
	nd := &n.Payload
	if nd.xform != nil {
		m = nd.xform.Mul(m)
	}
	if nd.clip {
		clip = clip.Intersect(m.TransformRect(n.Bounds))
	}
	return clip, m, true
}
