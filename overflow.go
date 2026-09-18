package gift

import (
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/internal/scene"
)

// ReportOverflow records that this node's content did not fit into the size it
// reported, by over logical pixels per axis.
//
// # Why this exists
//
// The project plan, section 7, "Overflow-Modell", settles the question "the
// content does not fit" as follows: the children keep their honest sizes and
// positions, the container reports the size its constraints permit, nothing is
// clipped automatically, and the excess is carried as a number rather than
// swallowed. This method is where the number leaves the layout algorithm.
//
// A layouter calls it at most once per [Layouter.Layout] call. Passing the
// zero size is how a container that used to overflow says it no longer does;
// a layouter that never calls it is treated as never overflowing.
//
// # Cost
//
// It writes two floats and, when the value changed, adjusts two counters. It
// allocates nothing and takes no lock, so it is inside the zero allocation
// contract of the frame path.
func (l *LayoutContext) ReportOverflow(over geom.Size) {
	l.app.setOverflow(l.node, l.nd, over)
}

// setOverflow stores the overflow of one node and keeps the aggregate
// counters in [Diagnostics] in step with it.
//
// The counters are maintained incrementally rather than recomputed per frame,
// because a clean node is skipped entirely by [App.layoutNode] and would
// otherwise silently drop out of a per pass tally. What the counters therefore
// describe is the state of the retained tree, not the set of nodes that
// happened to be measured last: a row that overflowed and was not re-measured
// still overflows.
func (a *App) setOverflow(h scene.Handle, nd *nodeData, over geom.Size) {
	over = geom.Sz(sane(over.W), sane(over.H))
	if over == nd.overflow {
		return
	}
	a.diag.OverflowExtent += (over.W + over.H) - (nd.overflow.W + nd.overflow.H)
	var zero geom.Size
	switch {
	case nd.overflow == zero:
		a.diag.OverflowNodes++
	case over == zero:
		a.diag.OverflowNodes--
	}
	nd.overflow = over
	// Only on a change, so a scene that overflows permanently produces one
	// diagnosis and not one per frame.
	diagnoseOverflow(a, h, nd, over)
}

// clearOverflow removes an unmounted node from the aggregate counters. It runs
// before the payload is released.
func (a *App) clearOverflow(nd *nodeData) {
	var zero geom.Size
	if nd.overflow == zero {
		return
	}
	a.diag.OverflowExtent -= nd.overflow.W + nd.overflow.H
	a.diag.OverflowNodes--
	nd.overflow = zero
}

// sane maps a negative, infinite or NaN overflow to zero. An overflow is a
// measured difference, and an infinity travelling into a diagnostic counter
// would poison every number printed afterwards.
func sane(v float32) float32 {
	if !(v > 0) || v > maxFiniteExtent {
		return 0
	}
	return v
}

const maxFiniteExtent = 3.4028235e38
