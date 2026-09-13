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

	var size geom.Size
	switch n.kind {
	case kindStack:
		for i := range k {
			n.items[i].Flex = ctx.ChildFlex(i)
		}
		size = layout.Stack(n.spec, cc, k, n, n.items, n.origins)
	default:
		size = layout.Overlay(n.spec.Padding, n.spec.Alignment, cc, k, n, n.items, n.origins)
	}

	for i := range k {
		ctx.Place(i, n.origins[i])
	}
	// Cleared without a defer: a closure would be the only allocation in
	// this function, and a layouter that panics has already put the App into
	// the state the application is expected to report and restart from.
	n.ctx = nil
	return size
}

// Paint draws the node in the fixed order of the project plan, section 8.
// It is installed as the painter only when there is something to draw; see
// [styleSpec.needsPainter].
func (n *node) Paint(ctx *gift.PaintContext) {
	paintStyle(ctx, n.st, n.spec.Padding)
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

// element builds the gift.Element of a container view.
func element(b base, kind nodeKind, gap float32, axis layout.Axis, children []gift.View) gift.Element {
	n := &node{
		kind: kind,
		spec: layout.StackSpec{Axis: axis, Gap: gap, Padding: b.pad, Alignment: b.align},
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
	}
}
