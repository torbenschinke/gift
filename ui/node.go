package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

// nodeKind selects the layout algorithm of a [node].
type nodeKind uint8

const (
	kindStack nodeKind = iota
	kindOverlay
	kindBox
	kindScroll
)

// node is the retained half of every container view: it is both the
// gift.Layouter and, when the view has something to draw, the gift.Painter of
// the node.
//
// One object serves both roles on purpose. A build allocates exactly one of
// these per container, and the scratch buffers it carries are what makes
// layout allocation free afterwards: the node lives for as long as the
// retained scene node does, so a frame that only relayouts reuses the buffers
// it grew on the first frame.
//
// The alternative — keeping the scratch inside gift's node payload — would
// require gift to know about stack layout, which the project plan, section 3,
// puts in internal/layout instead.
type node struct {
	kind nodeKind
	spec layout.StackSpec
	fr   frameSpec
	st   styleSpec

	items   []layout.Item
	origins []geom.Point

	// ctx is the layout context of the call in progress. It exists so that
	// the node can implement layout.Measurer through a pointer receiver:
	// boxing a wrapper value into the Measurer interface on every layout
	// would allocate once per container per frame.
	ctx *gift.LayoutContext
}

// MeasureChild implements layout.Measurer.
func (n *node) MeasureChild(i int, c geom.Constraints) geom.Size {
	return n.ctx.Measure(i, c)
}

// ChildBaseline implements layout.Baseliner. It forwards the baseline a child
// reported during this pass, or false when the child reported none — which is
// the answer for anything that is not text, and the one case the algorithm
// must not guess at.
func (n *node) ChildBaseline(i int) (float32, bool) { return n.ctx.ChildBaseline(i) }

// Layout measures the children through the shared algorithms in
// internal/layout and places them.
//
// The constraints the children see are the incoming ones as modified by the
// frame modifiers, and the size reported upwards is the one the frame asks
// for. A Frame that does not fit the incoming constraints therefore wins and
// overflows, which gift passes through unchanged rather than clamping
// silently; see [gift.Layouter].
func (n *node) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	k := ctx.ChildCount()
	n.ensure(k)
	n.ctx = ctx
	cc := n.fr.apply(c)

	var res layout.Result
	switch n.kind {
	case kindStack:
		for i := range k {
			n.items[i].Flex = ctx.ChildFlex(i)
		}
		res = layout.Stack(n.spec, cc, k, n, n.items, n.origins)
	case kindScroll:
		res = n.layoutScroll(ctx, cc, k)
	case kindBox:
		// A Box has no content, so it is greedy: it takes the whole extent
		// on every axis that is bounded, and collapses to its padding on an
		// axis that is not.
		//
		// This is what makes the obvious spelling work. A ZStack hands its
		// children loose but bounded constraints, so
		//
		//	ui.ZStack(ui.Box().Background(c), content)
		//
		// paints the plate behind the content instead of producing a zero
		// sized, invisible box. The minimum-size rule that was here before
		// silently dropped such backgrounds, which cost real debugging time
		// during WU-D.
		//
		// A stack measures an inflexible child with an unbounded main axis —
		// see the project plan, section 7, "Overflow-Modell", rule 1 — so a
		// Box in a VStack collapses on the main axis and fills the cross
		// axis. Give it a height with Frame or MinHeight, or a share of the
		// leftover space with Flex.
		pad := n.spec.Padding
		size := geom.Sz(pad.Horizontal(), pad.Vertical())
		if cc.HasBoundedWidth() {
			size.W = cc.Max.W
		}
		if cc.HasBoundedHeight() {
			size.H = cc.Max.H
		}
		// A Box has no children, so it cannot overflow: it never asks for
		// more than the constraints offer.
		res.Size = cc.Constrain(size)
	default:
		res = layout.Overlay(n.spec.Padding, n.spec.Alignment, cc, k, n, n.items, n.origins)
	}

	for i := range k {
		ctx.Place(i, n.origins[i])
	}
	// Overflow is reported, never hidden. The children above keep the origins
	// the algorithm gave them even when they do not fit; the project plan,
	// section 7, "Overflow-Modell", rules out clipping them silently, and
	// rule 3 requires the excess to be visible in gift.Diagnostics.
	ctx.ReportOverflow(res.Overflow)
	// Cleared without a defer: a closure would be the only allocation in
	// this function, and a layouter that panics has already put the App into
	// the state the application is expected to report and restart from.
	n.ctx = nil
	return res.Size
}

// Paint draws the node in the fixed order of the project plan, section 8.
// It is installed as the painter only when there is something to draw; see
// [styleSpec.needsPainter].
func (n *node) Paint(ctx *gift.PaintContext) {
	paintStyle(ctx, n.st)
}

// ensure sizes the scratch buffers to k children, reusing the backing arrays
// whenever they are large enough. A frame in which the child count did not
// change never reallocates.
func (n *node) ensure(k int) {
	if cap(n.items) < k {
		n.items = make([]layout.Item, k)
		n.origins = make([]geom.Point, k)
		return
	}
	n.items = n.items[:k]
	n.origins = n.origins[:k]
}

// layoutScroll is the stack algorithm with the scroll axis unbounded.
//
// # Why the axis is measured unbounded
//
// A scroll container exists precisely so that its content may be larger than
// it is. Measuring the content against the viewport extent would make a tall
// column report the viewport height and there would be nothing to scroll; so
// the scroll axis is [geom.Unbounded] and the cross axis keeps the viewport's
// bound. That is also rule 1 of the overflow model of the project plan,
// section 7, applied to the one container where it is not merely a safety
// property but the whole point.
//
// # Why this is not an overflow
//
// Content taller than the viewport is the normal, intended state here, and
// [gift.Diagnostics.OverflowNodes] must stay zero for it — otherwise the one
// number that says "some container in this scene is lying about its size"
// would be noisy in exactly the scenes it is needed for, and
// [gifttest.Harness.AssertNoOverflow] would be useless in a gallery.
//
// The distinction is not "how much bigger" but *whether the content is
// reachable*. Rule 3 of the overflow model is about content that keeps honest
// positions nobody can ever see; here the content keeps honest positions and
// the user reaches all of them by scrolling. Mechanically it falls out of the
// measurement: the scroll axis is unbounded, and an unbounded axis cannot
// overflow because nothing was exceeded. The excess is reported through
// [gift.LayoutContext.ReportScrollContent] instead, where it is a content
// extent rather than a complaint.
//
// The *cross* axis is a different matter and is reported as an ordinary
// overflow. A child wider than a vertical scroller really is cut off and
// really is unreachable, because there is no horizontal offset to move it
// into view. That is the honest line between the two.
func (n *node) layoutScroll(ctx *gift.LayoutContext, cc geom.Constraints, k int) layout.Result {
	for i := range k {
		n.items[i].Flex = ctx.ChildFlex(i)
	}
	free := cc
	if n.spec.Axis == layout.Horizontal {
		free.Min.W, free.Max.W = 0, geom.Unbounded()
	} else {
		free.Min.H, free.Max.H = 0, geom.Unbounded()
	}
	res := layout.Stack(n.spec, free, k, n, n.items, n.origins)

	// The content extent is what the children came to; the viewport is what
	// the incoming constraints allow. gift derives the viewport from the size
	// returned below, so only the content has to be reported.
	content := res.Size
	ctx.ReportScrollContent(float64(mainExtent(n.spec.Axis, content)), 0)
	res.Size = cc.Constrain(content)
	return res
}

// mainExtent returns the component of s along ax.
func mainExtent(ax layout.Axis, s geom.Size) float32 {
	if ax == layout.Horizontal {
		return s.W
	}
	return s.H
}

// element builds the gift.Element of a container view.
func element(b base, kind nodeKind, gap float32, axis layout.Axis, cross layout.CrossAlign, children []gift.View) gift.Element {
	n := &node{
		kind: kind,
		spec: layout.StackSpec{Axis: axis, Gap: gap, Padding: b.pad, Alignment: b.align, CrossAlign: cross},
		fr:   b.frame,
		st:   b.style,
	}
	var p gift.Painter
	if b.style.needsPainter() {
		p = n
	}
	return gift.Element{
		Key:      b.key,
		Flex:     b.flex,
		Layouter: n,
		Painter:  p,
		Children: children,
		// The paint clip and the input clip come from this one field. The
		// painter pushes the full bounds (see paintStyle) and gift clips hit
		// testing to the same rectangle, so a clipped child cannot be
		// invisible and clickable at the same time.
		Clip: b.style.clip,
	}
}
