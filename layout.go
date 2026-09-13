package gift

import (
	"fmt"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// Layouter measures a node and places its children.
//
// Layout is single phase, like Flutter: constraints go down, sizes come back
// up and the parent decides the positions. There is no separate arrange pass
// in which children could change their mind.
type Layouter interface {
	// Layout measures the children through ctx and returns the size of this
	// node.
	//
	// It must measure and place every child exactly once. Measuring a child
	// twice is not detected as an error, but it doubles the cost of the
	// subtree; not measuring a child leaves it at its previous size, and not
	// placing it leaves it at its previous position.
	//
	// The returned size should satisfy c. A size that violates the
	// constraints is passed through unchanged, because clamping silently
	// would hide layout bugs.
	Layout(ctx *LayoutContext, c geom.Constraints) geom.Size
}

// LayoutContext gives a [Layouter] access to its children.
//
// Instances are pooled per tree depth and reused across frames; a context is
// valid only for the duration of the [Layouter.Layout] call that received it.
type LayoutContext struct {
	app  *App
	node scene.Handle
	nd   *nodeData
}

// ChildCount returns the number of children of the node being laid out.
func (l *LayoutContext) ChildCount() int { return len(l.nd.children) }

// Measure lays out the given child with the constraints c and returns the
// size the child chose. The index must be in [0, ChildCount).
func (l *LayoutContext) Measure(child int, c geom.Constraints) geom.Size {
	l.check(child)
	sz := l.app.layoutNode(l.nd.children[child], c)
	l.nd.sizes[child] = sz
	return sz
}

// Place positions the given child at the offset at, relative to the origin of
// the node being laid out. Absolute bounds are derived from these relative
// offsets once the whole tree has been measured.
func (l *LayoutContext) Place(child int, at geom.Point) {
	l.check(child)
	l.nd.origins[child] = at
}

// ChildSize returns the size a child chose in a preceding [LayoutContext.Measure].
// It is the zero size if the child has not been measured in this pass.
func (l *LayoutContext) ChildSize(child int) geom.Size {
	l.check(child)
	return l.nd.sizes[child]
}

func (l *LayoutContext) check(child int) {
	if child < 0 || child >= len(l.nd.children) {
		panic(fmt.Sprintf("gift: child index %d out of range, the node has %d children", child, len(l.nd.children)))
	}
}

// layoutNode measures h with the constraints c and returns its size.
func (a *App) layoutNode(h scene.Handle, c geom.Constraints) geom.Size {
	n := a.store.Get(h)
	nd := n.Payload.(*nodeData)
	a.diag.Layouts++

	nd.prepareChildSlots()

	var size geom.Size
	if nd.layouter != nil {
		lc := a.acquireLayout()
		lc.node = h
		lc.nd = nd
		size = nd.layouter.Layout(lc, c)
		a.releaseLayout()
	}

	n = a.store.Get(h)
	n.Size = size
	n.Flags &^= scene.FlagNeedsLayout
	n.Flags |= scene.FlagNeedsPaint
	return size
}

// acquireLayout returns the pooled LayoutContext of the current depth.
func (a *App) acquireLayout() *LayoutContext {
	if a.layoutDepth == len(a.layoutCtxs) {
		a.layoutCtxs = append(a.layoutCtxs, &LayoutContext{app: a})
	}
	lc := a.layoutCtxs[a.layoutDepth]
	a.layoutDepth++
	return lc
}

func (a *App) releaseLayout() { a.layoutDepth-- }

// assignBounds turns the relative child offsets produced by Place into
// absolute bounds. It runs once per layout pass, after the root has been
// measured.
func (a *App) assignBounds(h scene.Handle, origin geom.Point) {
	n := a.store.Get(h)
	nd := n.Payload.(*nodeData)
	n.Bounds = geom.RcSize(origin, n.Size)
	for i, c := range nd.children {
		var off geom.Point
		if i < len(nd.origins) {
			off = nd.origins[i]
		}
		a.assignBounds(c, origin.Add(off))
	}
}
