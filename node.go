package gift

import (
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// nodeData is the gift owned payload of a scene node.
//
// It is allocated once per mounted node and reused for the whole lifetime of
// that node, including all of its child side tables. That is what keeps
// layout and paint of an unchanged tree allocation free.
type nodeData struct {
	// view is the view description the node was last built from. It is kept
	// for diagnostics and for the ownership check.
	view View
	// scope is non nil exactly for component nodes.
	scope *scope

	layouter Layouter
	painter  Painter

	// children mirrors the scene sibling list as an indexable slice, because
	// layout and paint address children by index.
	children []scene.Handle
	// next is the scratch buffer for the child list under construction. It
	// is swapped with children at the end of a reconciliation, so neither
	// slice is ever reallocated in the steady state.
	next []scene.Handle
	// used marks which of the previous children have been claimed during the
	// current reconciliation.
	used []bool

	// childViews is the children slice handed over by the last build. gift
	// owns it; it is kept so that the debug build can detect a caller that
	// keeps mutating it.
	childViews []View

	// sizes and origins are the per child layout results.
	sizes   []geom.Size
	origins []geom.Point

	own ownershipGuard
}

// prepareChildSlots makes sure the per child layout tables match the current
// number of children. It reuses the existing backing arrays whenever they are
// large enough.
func (nd *nodeData) prepareChildSlots() {
	n := len(nd.children)
	if cap(nd.sizes) < n {
		nd.sizes = make([]geom.Size, n)
		nd.origins = make([]geom.Point, n)
	} else {
		nd.sizes = nd.sizes[:n]
		nd.origins = nd.origins[:n]
	}
	// The tables are not cleared: a child that is not measured keeps its
	// previous size, which is documented behaviour of Layouter.Layout.
}
