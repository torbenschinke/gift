package gift

import (
	"fmt"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
	"github.com/torbenschinke/gift/render"
)

// Painter emits the drawing operations of one node.
type Painter interface {
	// Paint appends drawing operations to the display list of the current
	// frame.
	//
	// It is called once per drawn frame for every visible node. It must not
	// allocate, must not log and must not change any state, because doing so
	// would either break the zero allocation contract of the frame path or
	// build during paint. See the project plan, sections 11 and 15.
	//
	// Children are not drawn automatically. A painter that wants its
	// children on screen calls [PaintContext.PaintChildren] at the point in
	// its own drawing order where they belong, which is what makes
	// background, content and border ordering explicit.
	//
	// A node without a painter is the other case: a nil [Element.Painter]
	// paints all children in order and nothing else.
	Paint(ctx *PaintContext)
}

// PaintContext is the interface of a [Painter] to the display list.
//
// There is exactly one instance per App; it is reused for every node and
// every frame and is valid only for the duration of a [Painter.Paint] call.
type PaintContext struct {
	app  *App
	cur  scene.Handle
	nd   *nodeData
	list *render.List
	// clipDepth counts the clips the current painter pushed, so that an
	// unbalanced painter is caught at its own boundary instead of corrupting
	// the clips of its siblings.
	clipDepth int
	// xform is the index of the active transform in the display list. It is
	// 0, the identity, outside any transformed subtree; a scroll container
	// pushes its translation here on the way into its children. See
	// [App.beginSubtree].
	xform uint32
}

// Bounds returns the absolute rectangle of the node being painted, as
// computed by the last layout pass.
//
// It is in *local* space, the space the operations a painter emits are
// interpreted in: an operation carries the index of the active transform, and
// the backend applies it. A painter that needs the device space rectangle —
// because it is about to push a clip, which is device space by the convention
// of the project plan, section 7 — asks [PaintContext.DeviceBounds].
func (p *PaintContext) Bounds() geom.Rect {
	return p.app.store.Get(p.cur).Bounds
}

// DeviceBounds returns the rectangle of the node being painted mapped through
// the transform that is active for it, which is the space clips live in.
//
// Before scrolling existed every transform was the identity and this was the
// same rectangle as [PaintContext.Bounds]; ui.Text pushed its clip with the
// latter and was right by accident. Under a scroll transform it was wrong by
// the scroll offset — the label's glyphs were clipped against the rectangle
// the label occupied *before* the container scrolled — and the symptom was
// text that faded out in the wrong place.
func (p *PaintContext) DeviceBounds() geom.Rect {
	return p.list.Xform(p.xform).TransformRect(p.app.store.Get(p.cur).Bounds)
}

// Add appends op to the display list.
//
// The clip and transform indices of op are overwritten with the currently
// active ones, so a painter never has to know about the clip stack or about
// the scroll offset of a container above it. [Op.Bounds] therefore stays in
// the node's own local space and the backend applies the transform, which is
// what makes scrolling a matrix change rather than a rewrite of every
// rectangle.
func (p *PaintContext) Add(op render.Op) {
	op.Clip = p.list.CurrentClip()
	op.Xform = p.xform
	p.list.Add(op)
	p.app.diag.PaintedOps++
}

// PushClip intersects r, which is in device space, with the active clip and
// makes the result active for all following operations until the matching
// [PaintContext.PopClip].
//
// Device space, not local space. The project plan, section 7, fixes that
// convention: [render.List] keeps one clip stack and one intersection, and
// intersecting rectangles that live in different spaces produces nonsense. A
// painter that wants to clip to its own node uses [PaintContext.DeviceBounds].
//
// A painter must pop every clip it pushed before it returns. An unbalanced
// stack is a contract violation and panics.
func (p *PaintContext) PushClip(r geom.Rect) {
	p.list.PushClip(r)
	p.clipDepth++
}

// PopClip removes the innermost clip pushed by this painter.
func (p *PaintContext) PopClip() {
	if p.clipDepth == 0 {
		panic("gift: PaintContext.PopClip without a matching PushClip in this painter")
	}
	p.list.PopClip()
	p.clipDepth--
}

// PaintChildren paints all children of the current node, in order.
//
// When the node declared [Element.Clip] the children are painted under that
// clip; see [PaintContext.paintKids].
func (p *PaintContext) PaintChildren() {
	p.paintKids(p.nd.children)
}

// PaintChild paints the child with the given index. The index must be in
// [0, ChildCount).
func (p *PaintContext) PaintChild(i int) {
	if i < 0 || i >= len(p.nd.children) {
		panic("gift: PaintContext.PaintChild index out of range")
	}
	p.paintKids(p.nd.children[i : i+1])
}

// paintKids paints a run of children of the current node under the clip the
// node declared, if any.
//
// # Why the clip is here and not in the widget
//
// [Element.Clip] used to mean two different things in two different places.
// gift read it for hit testing, and a widget was separately expected to call
// [PaintContext.PushClip] around its children. ui.Stack and ui.Text did;
// ui.Button did not, so a 400x400 label inside a 50x50 button with Clip(true)
// painted over the whole window while its own documentation promised the
// opposite, and the input half agreed with the documentation rather than with
// the pixels. Two readers of one declaration, and nothing making them agree.
//
// Now there is one reader. The flag is applied here, on the way into the
// subtree, which is the one route every container's children take:
// [PaintContext.PaintChildren] and [PaintContext.PaintChild] are the only ways
// a painter can descend, and a node with no painter at all goes through
// [App.paintNode], which applies the same clip for the same reason. A widget
// cannot forget it, because there is nothing left for a widget to do.
//
// # What stays outside
//
// The clip surrounds the children and not the node's own drawing. That is the
// order the project plan, section 8, fixes and ui_test pins: a background is
// exactly the bounds and a border lies inside them, so clipping either would
// be a no-op, but a *shadow* deliberately extends past the bounds and clipping
// it to them would delete it. A node's own ops are therefore emitted under
// whatever clip its parent established, and only the subtree is confined.
//
// # Leaves
//
// A view whose content is not its children — [ui.Text] draws glyphs — has no
// children to route and applies the clip around its own content itself. There
// is no shared path to put that on, because "content" for a leaf is whatever
// that leaf draws, and gift cannot tell it apart from the background.
//
// # Cost
//
// One bool test and one nil test per container per frame, and one clip stack
// entry plus one transform for the containers that asked for them. No
// allocation, so the frame path contract of the project plan, section 11, is
// unaffected.
func (p *PaintContext) paintKids(kids []scene.Handle) {
	prev, clipped := p.app.beginSubtree(p.cur)
	for _, c := range kids {
		p.app.paintNode(c)
	}
	p.app.endSubtree(prev, clipped)
}

// beginSubtree applies everything a node imposes on the way into its subtree:
// its [Element.Clip] and, for a scroll container, the translation of its
// scroll offset. endSubtree undoes it.
//
// # One reader, two callers, no drift
//
// There are exactly two routes into a subtree — [PaintContext.paintKids] for a
// node that has a painter and the nil painter branch of [App.paintNode] for
// one that does not — and both go through here. [App.hitNode] does the same
// two things in the same order for input. That is the single reader rule
// [Element.Clip] already followed, extended to the scroll offset, and it is
// why a scrolled node cannot be drawn in one place and hit in another.
//
// # The clip is device space, and that is a bug fix
//
// The rectangle pushed here is the node's bounds mapped through the transform
// that is active for the node itself. It used to be the raw bounds, which was
// indistinguishable from the right answer for as long as every transform was
// the identity — and nothing ever set one, so nothing ever noticed. The first
// scroll container inside another one would have clipped its content against
// the rectangle it occupied before the outer container scrolled. The input
// half in [App.hitNode] already composed the transform, so the two halves
// would have disagreed, which is exactly the failure the project plan,
// section 7, forbids.
func (a *App) beginSubtree(h scene.Handle) (prev uint32, clipped bool) {
	n := a.store.Get(h)
	nd := &n.Payload
	prev = a.pctx.xform
	if nd.clip {
		a.list.PushClip(a.list.Xform(prev).TransformRect(n.Bounds))
		clipped = true
	}
	if s := nd.scroll; s != nil {
		a.pctx.xform = a.list.PushXform(s.xform().Mul(a.list.Xform(prev)))
	}
	return prev, clipped
}

func (a *App) endSubtree(prev uint32, clipped bool) {
	a.pctx.xform = prev
	if clipped {
		a.list.PopClip()
	}
}

// ChildCount returns the number of children of the node being painted.
func (p *PaintContext) ChildCount() int { return len(p.nd.children) }

// Interaction returns the hover, press, focus and disabled state gift
// maintains for the node being painted.
//
// This is how a control draws itself differently per state without a rebuild.
// The dispatcher wrote these bools during Update and marked the node for
// repaint; nothing was built and nothing was measured. See [Interaction].
func (p *PaintContext) Interaction() Interaction { return p.nd.ia }

// GlyphsLen returns the current length of the glyph side table of the display
// list, which is the index the next [PaintContext.AppendGlyph] writes to.
func (p *PaintContext) GlyphsLen() uint32 { return p.list.GlyphsLen() }

// AppendGlyph appends one positioned glyph to the display list.
//
// A text painter remembers [PaintContext.GlyphsLen], appends the glyphs of its
// run and then emits a single [render.OpGlyphs] spanning them. The glyph is
// copied into the list here and now, which is what confines the borrowed
// shaping result to the frame that produced it; see [render.List.AppendGlyph].
func (p *PaintContext) AppendGlyph(g render.Glyph) { p.list.AppendGlyph(g) }

// paintNode paints h and, through its painter, its subtree.
//
// A node without a painter paints its children and nothing else; see
// [Element.Painter]. This branch neither touches the shared PaintContext nor
// installs a defer, because it can neither draw nor clip: it is the fast path
// of every purely structural container.
//
// The saved context of the parent is restored with a defer. A painter that
// panics, which is how gift reports a contract violation such as a state write
// during Draw, must not leave the shared PaintContext pointing at a node that
// is no longer being painted: the application may recover from that panic and
// the next frame has to find the App intact. An open coded defer allocates
// nothing, so the frame path contract is unaffected. The depth counter needs
// no such care because [App.Paint] resets it at the start of every frame.
func (a *App) paintNode(h scene.Handle) {
	n := a.store.Get(h)
	nd := &n.Payload
	if a.paintDepth > scene.MaxDepth {
		panic(fmt.Sprintf(
			"gift: paint recursion deeper than %d levels; a painter is descending into a cycle or an unbounded tree",
			scene.MaxDepth))
	}

	// The transform of a node applies to the node and its whole subtree, in
	// both halves of the frame: this pushes it into the display list and
	// [App.hitNode] composes the very same matrix. One declaration, two
	// readers, no second traversal to drift from the first.
	prevXform := a.pctx.xform
	if nd.xform != nil {
		a.pctx.xform = a.list.PushXform(nd.xform.Mul(a.list.Xform(prevXform)))
		defer func() { a.pctx.xform = prevXform }()
	}

	if nd.painter == nil {
		a.paintDepth++
		// A node with no painter still honours its own [Element.Clip] and its
		// own scroll offset: both are properties of the node, not of the fact
		// that somebody installed a painter on it. See [App.beginSubtree].
		prev, clipped := a.beginSubtree(h)
		for _, c := range nd.children {
			a.paintNode(c)
		}
		a.endSubtree(prev, clipped)
		a.paintDepth--
		// Deliberately not counted. PaintedNodes means "a painter ran", and
		// this branch is the one where none did; counting it made the
		// counter equal to the number of visited nodes and hid the nil
		// painter fast path from every measurement.
		return
	}

	p := &a.pctx
	prevHandle, prevData, prevDepth := p.cur, p.nd, p.clipDepth
	p.cur, p.nd, p.clipDepth = h, nd, 0
	a.paintDepth++
	defer func() {
		p.cur, p.nd, p.clipDepth = prevHandle, prevData, prevDepth
		a.paintDepth--
	}()

	nd.painter.Paint(p)

	if p.clipDepth != 0 {
		panic("gift: painter returned with an unbalanced clip stack")
	}
	a.diag.PaintedNodes++
}
