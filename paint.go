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
	// xform is the index of the active transform. It is 0 until gift gains
	// scrolling and transform nodes.
	xform uint32
}

// Bounds returns the absolute rectangle of the node being painted, as
// computed by the last layout pass.
func (p *PaintContext) Bounds() geom.Rect {
	return p.app.store.Get(p.cur).Bounds
}

// Add appends op to the display list.
//
// The clip and transform indices of op are overwritten with the currently
// active ones, so a painter never has to know about the clip stack. gift does
// not push transforms yet, so the transform index is always 0, the identity.
func (p *PaintContext) Add(op render.Op) {
	op.Clip = p.list.CurrentClip()
	op.Xform = p.xform
	p.list.Add(op)
	p.app.diag.PaintedOps++
}

// PushClip intersects r with the active clip and makes the result active for
// all following operations until the matching [PaintContext.PopClip].
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
func (p *PaintContext) PaintChildren() {
	for _, c := range p.nd.children {
		p.app.paintNode(c)
	}
}

// PaintChild paints the child with the given index. The index must be in
// [0, ChildCount).
func (p *PaintContext) PaintChild(i int) {
	if i < 0 || i >= len(p.nd.children) {
		panic("gift: PaintContext.PaintChild index out of range")
	}
	p.app.paintNode(p.nd.children[i])
}

// ChildCount returns the number of children of the node being painted.
func (p *PaintContext) ChildCount() int { return len(p.nd.children) }

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

	if nd.painter == nil {
		a.paintDepth++
		for _, c := range nd.children {
			a.paintNode(c)
		}
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
