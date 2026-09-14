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
	// It must be a pure function of its constraints, its children and the
	// view it came from. gift caches the size a node returned for a given
	// set of constraints and returns it again, without calling Layout, as
	// long as neither the node nor anything below it was invalidated; see
	// [App.layoutNode].
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

	// measured records that at least one child has been measured in the
	// call this context is serving. It is the guard of
	// [LayoutContext.AnchorScroll], which may only run before the children
	// of the pass exist.
	measured bool
}

// Node returns a reference to the node being laid out.
//
// It exists for a layouter that keeps retained state *outside* gift — the
// gallery of the project plan, section 10 keeps a spatial index and a tile
// pool in an application owned object — and therefore has to be able to tell
// which node it is currently running for. Such an object is shared by
// construction, and two mounted views over one of them would silently
// interleave their writes into the same pool. Comparing this reference across
// a pass is how that is turned into a diagnosis instead of a symptom.
//
// The reference is stable for as long as the node is mounted and becomes
// stale when it is unmounted; it is comparable with ==.
func (l *LayoutContext) Node() NodeRef { return NodeRef{l.node} }

// Pass returns the ordinal of the layout pass this call belongs to.
//
// It increases once per [App.Update] that lays anything out, and it is
// constant for every layouter that runs inside that pass. Together with
// [LayoutContext.Node] it is what lets a layouter with external retained state
// distinguish "I am being laid out again, next frame" from "a second node is
// laying me out in the same frame". It is not a frame counter and not a time
// source, and it is deliberately not exposed as either.
func (l *LayoutContext) Pass() uint64 { return l.app.layoutPass }

// ChildCount returns the number of children of the node being laid out.
func (l *LayoutContext) ChildCount() int { return len(l.nd.children) }

// Measure lays out the given child with the constraints c and returns the
// size the child chose. The index must be in [0, ChildCount).
//
// A clean child that is asked for the constraints it already has answers from
// its cache without running its own layouter, so measuring a subtree that did
// not change costs one comparison.
func (l *LayoutContext) Measure(child int, c geom.Constraints) geom.Size {
	l.check(child)
	l.measured = true
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

// ChildFlex returns [Element.Flex] of the given child, zero for an
// inflexible one. The index must be in [0, ChildCount).
//
// This is how a stack tells a spacer from an ordinary child: one float read
// from the retained node, no interface dispatch and no type assertion in the
// layout path.
func (l *LayoutContext) ChildFlex(child int) float32 {
	l.check(child)
	return l.app.data(l.nd.children[child]).flex
}

func (l *LayoutContext) check(child int) {
	if child < 0 || child >= len(l.nd.children) {
		panic(fmt.Sprintf("gift: child index %d out of range, the node has %d children", child, len(l.nd.children)))
	}
}

// ReportBaseline records the distance from the top of this node to the
// baseline its content sits on, in logical pixels.
//
// # This is a seam, and it is honest about being one
//
// The project plan, section 7, makes baselines part of the shared layout
// contract, and this is the channel they travel through: a text node reports
// the first baseline of its paragraph, and a container that wants to align
// siblings on it reads [LayoutContext.ChildBaseline].
//
// As of WU-G exactly one thing writes it, [ui.Text], and nothing reads it: no
// stack aligns on baselines yet, because cross sibling baseline alignment
// needs a second measuring pass over the row — measure every child, take the
// largest baseline, then place each child shifted by the difference — and that
// is a change to the stack algorithm in internal/layout, not to a text view.
// Adding the channel now and the algorithm later is the cheap order; inventing
// the algorithm now for a toolkit whose only baseline bearing view is a label
// would be building against a single caller.
//
// A layouter that never calls it reports no baseline, which is the right
// answer for a box: a rectangle has no baseline, and a container that guesses
// one — the bottom edge, say — would silently misalign every row it touched.
func (l *LayoutContext) ReportBaseline(v float32) {
	l.nd.baseline, l.nd.hasBaseline = v, true
}

// ChildBaseline returns the baseline the given child reported during this
// layout pass, and whether it reported one at all. See
// [LayoutContext.ReportBaseline].
func (l *LayoutContext) ChildBaseline(child int) (float32, bool) {
	l.check(child)
	nd := l.app.data(l.nd.children[child])
	return nd.baseline, nd.hasBaseline
}

// RequestLayout asks for another layout pass over this node in the next
// update, without rebuilding anything.
//
// It exists for a layouter whose work is *incremental*: the gallery of the
// project plan, section 10, may not rebuild a 100 000 entry masonry index in
// one frame, so it advances the rebuild by a bounded chunk per pass and asks
// for the next pass from here. Section 10 requires exactly that — "Initiales
// Layout, Sortierung und Spaltenwechsel duerfen O(N) Arbeit benoetigen; sie
// werden ausserhalb des Frame-Hotpaths oder inkrementell berechnet".
//
// A layouter that calls this unconditionally never lets the application idle
// and fails [gifttest.Harness.Settle] with a diagnosis, which is the intended
// outcome: "I am still working" must be a statement with an end.
//
// # Why the mark is deferred
//
// Marking the node immediately would be undone three lines later:
// [App.layoutNode] clears [scene.FlagNeedsLayout] on the node as soon as its
// layouter returns, and the ancestors it would have marked are in the middle
// of being laid out and clear theirs on the way back up. The request is
// therefore queued and applied after the whole pass, which is the only point
// at which "again, next update" can be expressed at all.
func (l *LayoutContext) RequestLayout() { l.app.queueLayout(l.node) }

// queueLayout records a node that asked for another layout pass. The queue is
// a reused slice and the scan is linear over a handful of virtualising
// containers, so an incremental reflow costs no allocation per pass.
func (a *App) queueLayout(h scene.Handle) {
	for _, q := range a.pendingLayout {
		if q == h {
			return
		}
	}
	a.pendingLayout = append(a.pendingLayout, h)
}

// flushPendingLayout applies the requests collected during the pass. It runs
// after the pass, so the marks survive into the next update.
func (a *App) flushPendingLayout() {
	if len(a.pendingLayout) == 0 {
		return
	}
	for _, h := range a.pendingLayout {
		a.markNeedsLayout(h)
	}
	a.pendingLayout = a.pendingLayout[:0]
}

// Invalidator returns a function that marks this node for layout from outside
// the frame, and nil for a node that is no longer part of the tree.
//
// # Why a closure and not a NodeRef
//
// Because the caller is a view's retained object, not the application. A
// virtualising container holds a model the application mutates directly — a
// catalogue correction arrives from a worker, a keyboard cursor is moved by a
// command — and that model has to say "my layout is stale" without having been
// handed the App, the node reference and the knowledge of how the two combine.
// Handing it one function is the smallest interface that does the job, and it
// keeps the App out of a package that must not import the frame loop.
//
// The closure is created once per node and cached in the node, so calling this
// on every layout pass allocates only on the first one. It is safe to call
// after the node has been unmounted: it then does nothing, because
// [App.markNeedsLayout] stops at an invalid handle. It must be called from the
// UI executor like everything else.
func (l *LayoutContext) Invalidator() func() {
	if l.nd.invalidate == nil {
		a, h := l.app, l.node
		l.nd.invalidate = func() { a.markNeedsLayout(h) }
	}
	return l.nd.invalidate
}

// RequestBuild asks gift to rebuild the component instance that produced this
// node, in the next update.
//
// It is the one thing a layouter cannot do for itself: change the *number* of
// its children. A virtualising container sizes its tile pool from the
// viewport, and the viewport is only known once the constraints have arrived,
// which is after the build that had to declare the tiles. So it lays out what
// it has, asks for a rebuild here, and has the right pool one update later.
//
// The rebuild happens in the next [App.Update], never inside this one: build
// during layout would mean a view function running while the tree it produces
// is being measured. The frame this is called in is therefore laid out with
// the old child count, which for a tile pool means one frame with fewer
// children than there is content to show.
//
// What that frame looks like is the caller's problem and a real one. The
// obvious implementation — take the first n of the content — is wrong whenever
// the content is not produced in screen order, and a virtualised gallery's is
// not: WU-O measured a masonry gallery whose entire visible band collapsed
// into the leftmost 430 pixels of a 2400 pixel viewport for exactly this one
// frame, because the first n of a column grouped list is the first few
// columns. Thin the content across the pool instead; see ui's keepStride.
//
// A layouter that calls this on every pass is an infinite rebuild loop and is
// caught by [gifttest.Harness.Settle]. Guard it with a comparison.
func (l *LayoutContext) RequestBuild() {
	for h, depth := l.node, 0; !h.IsZero() && l.app.store.Valid(h); depth++ {
		if depth > scene.MaxDepth {
			return
		}
		n := l.app.store.Get(h)
		if sc := n.Payload.scope; sc != nil {
			l.app.markNeedsBuild(sc)
			return
		}
		h = n.Parent
	}
}

// layoutNode measures h with the constraints c and returns its size.
//
// This is where the "Layout: kein Measure/Arrange" row of the project plan,
// section 6, is actually implemented. The node is skipped entirely when both
// of the following hold:
//
//   - it is not marked [scene.FlagNeedsLayout], which means neither it nor
//     anything below it was rebuilt or otherwise invalidated, because
//     [App.markNeedsLayout] propagates the mark all the way to the root; and
//   - the incoming constraints are identical to the ones it was last
//     measured with, so its answer cannot have changed either.
//
// Both halves are needed. The flag alone would miss a parent that changed
// size and now offers its children different constraints; the constraints
// alone would miss a child whose own state changed under unchanged
// constraints. The cache per node is the price of getting this right, and the
// alternative — always relayouting from the root — is exactly the defect this
// replaces.
func (a *App) layoutNode(h scene.Handle, c geom.Constraints) geom.Size {
	n := a.store.Get(h)
	nd := &n.Payload

	if n.Flags&scene.FlagNeedsLayout == 0 && nd.haveLastC && nd.lastC == c {
		return n.Size
	}

	a.diag.Layouts++
	nd.prepareChildSlots()

	var size geom.Size
	if nd.layouter != nil {
		size = a.runLayouter(h, nd, c)
	}

	nd.lastC = c
	nd.haveLastC = true
	n.Size = size
	// A scroll container's viewport is whatever size it ended up with along
	// its own axis, so gift takes it from here rather than making every
	// scrolling layouter report it. Re-clamping the offset is part of the
	// same step: a viewport that grew may have made the current offset
	// illegal, and the next paint would otherwise translate the content past
	// its end.
	if s := nd.scroll; s != nil {
		s.viewport = s.axis.ofSize(size)
		s.off = s.clamp(s.off)
	}
	n.Flags &^= scene.FlagNeedsLayout
	n.Flags |= scene.FlagNeedsPaint
	return size
}

// runLayouter calls the layouter of nd with a pooled context.
//
// The release of the pooled context is deferred: a layouter that panics, which
// is how gift reports a contract violation, must not leave the pool depth
// permanently raised. An open coded defer costs a nanosecond and allocates
// nothing, so the frame path contract is unaffected.
func (a *App) runLayouter(h scene.Handle, nd *nodeData, c geom.Constraints) geom.Size {
	lc := a.acquireLayout()
	defer a.releaseLayout()
	lc.node = h
	lc.nd = nd
	lc.measured = false
	return nd.layouter.Layout(lc, c)
}

// acquireLayout returns the pooled LayoutContext of the current depth.
func (a *App) acquireLayout() *LayoutContext {
	if a.layoutDepth > scene.MaxDepth {
		panic(fmt.Sprintf(
			"gift: layout recursion deeper than %d levels; a component function or a layouter is building an unbounded tree",
			scene.MaxDepth))
	}
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
func (a *App) assignBounds(h scene.Handle, origin geom.Point, depth int) {
	if depth > scene.MaxDepth {
		panic(fmt.Sprintf(
			"gift: tree deeper than %d levels while assigning bounds; a component function is building an unbounded tree",
			scene.MaxDepth))
	}
	n := a.store.Get(h)
	nd := &n.Payload
	n.Bounds = geom.RcSize(origin, n.Size)
	for i, c := range nd.children {
		var off geom.Point
		if i < len(nd.origins) {
			off = nd.origins[i]
		}
		a.assignBounds(c, origin.Add(off), depth+1)
	}
}
