package gift

// View is a short lived description of a piece of user interface.
//
// A view is a value, not an object with identity. It is created during a
// build, consumed by the reconciler and then dropped. Durable identity lives
// in the scope and in the retained node tree, never in a copy of a view
// struct. Views are never evaluated in the frame hot path; see the project
// plan, sections 4 and 6.
type View interface {
	// ViewType returns the process wide stable ID of the concrete view type.
	//
	// Two views with different ViewType under the same key force a remount
	// instead of an update, which discards the node and the state below it.
	ViewType() TypeID

	// Build describes the element this view stands for. It is called during
	// reconciliation and never during paint.
	//
	// The returned Element is not retained beyond the call, except for the
	// children slice it references; see [Element.Children]. Build must be
	// free of side effects: the reconciler may call it before it has decided
	// whether the resulting element updates an existing node or mounts a new
	// one.
	Build(*BuildContext) Element
}

// Element is the description of one retained node.
//
// It is produced by [View.Build] and applied to a node by the reconciler.
type Element struct {
	// Key is the optional stable identity of this element among its
	// siblings. Siblings with keys are matched by key, siblings without keys
	// are matched by position. A key only has to be unique among siblings,
	// not globally.
	Key string

	// Layouter measures this node and places its children. A nil Layouter
	// means the node has zero size and its children are neither measured nor
	// placed.
	Layouter Layouter

	// Painter emits the drawing operations of this node.
	//
	// A nil Painter draws nothing of its own and paints all children in
	// order. That is the fast path for a purely structural container: a
	// stack without background, border or clip has nothing to draw, and
	// making it invisible together with its whole subtree would be a trap
	// whose only symptom is a blank screen.
	//
	// A non nil Painter is fully responsible for its subtree: children are
	// drawn only where it calls [PaintContext.PaintChildren] or
	// [PaintContext.PaintChild], which is what makes the background,
	// content and border ordering explicit.
	Painter Painter

	// Children are the child views, in order.
	//
	// Ownership passes to gift on return. The caller must neither modify nor
	// reuse the slice afterwards. Whoever hands over a reused buffer must
	// copy first. A violation is detected in builds with the giftdebug tag;
	// see the project plan, section 4.
	Children []View

	// Flex is the flexibility of this element along the main axis of an
	// enclosing stack. Zero, the default, means inflexible: the element is
	// measured against the space that is left. A positive value means the
	// element takes a share of the remaining space proportional to it.
	//
	// It is a plain field rather than an interface or a type assertion in
	// the layout path on purpose: a stack recognises a [ui.Spacer] by
	// reading one float from the retained node, see
	// [LayoutContext.ChildFlex]. A layouter that is not a stack ignores it.
	Flex float32
}

// BuildContext is passed to [View.Build].
//
// It is owned and reused by the App and is valid only for the duration of the
// call. Keeping it is a bug. It currently exposes no methods; it exists so
// that build time services such as theming or text metrics can be added later
// without changing the view contract.
type BuildContext struct {
	app *App
}
