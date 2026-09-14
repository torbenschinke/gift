package gift

import "github.com/torbenschinke/gift/geom"

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

	// Label is the human readable text this node stands for.
	//
	// It is a *semantic* field, not a visual one: nothing in the layout or
	// the paint path reads it, and setting it changes no pixel. A view sets
	// it to whatever a person would call the node — [ui.Text] sets it to its
	// string, a [ui.Button] with an icon sets it to the name of the command
	// — so that something outside the frame path can find a node by what it
	// says rather than by where it happens to sit.
	//
	// Today the only consumer is the test harness in package gifttest, whose
	// ByText selector is the whole reason coordinate free UI tests are
	// possible. It is deliberately declared here on Element rather than as a
	// private field of one ui view, because the next two consumers are known
	// in advance and want exactly the same string: a button that wants an
	// accessible name distinct from its label view, and the accessibility
	// bridge the project plan, section 14, excludes from the MVP but does not
	// rule out forever.
	//
	// # Cost
	//
	// A string header, copied from the element onto the node during a build
	// and never touched again. A build already copies eight other fields out
	// of the same struct; this is a ninth, and it allocates nothing — the
	// bytes belong to the view, which owns them for as long as the node does.
	// Update, layout and paint never read it. See the allocation contract of
	// the project plan, section 11, and TestLabelCostsNothingPerFrame.
	Label string

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

	// Interactor makes this node a hit target for input. A nil Interactor,
	// the default, makes the node transparent to input without affecting its
	// children; see [Interactor] for why that polarity is the safe one.
	Interactor Interactor

	// Focusable declares that this node may hold the keyboard focus and
	// therefore takes part in the tab order. It is only honoured together
	// with a non nil Interactor: focus exists to deliver key events, and a
	// node that cannot receive them would be a black hole in the tab order.
	Focusable bool

	// Disabled takes the node out of input without taking it out of the
	// layout. A disabled node is still a hit target — so a click on it does
	// not fall through to whatever is behind it — but receives no events,
	// acquires no hover or press state and is skipped by the focus order.
	Disabled bool

	// Clip confines this node's subtree to its bounds, for painting and for
	// input alike.
	//
	// It is one flag with one reader on each side, and both readers use this
	// node's bounds: [App.hitNode] intersects them into the inherited input
	// clip, and [PaintContext.PaintChildren] pushes them onto the display
	// list's clip stack on the way into the subtree. A widget therefore
	// cannot clip input without clipping paint or the other way round, which
	// is exactly what ui.Button used to do — Clip(true) confined its hit area
	// and let a 400x400 label paint over the window.
	//
	// The clip surrounds the subtree and not the node's own drawing, so a
	// shadow still extends past the bounds and a background still covers
	// them; see [PaintContext.paintKids]. A leaf whose content is not its
	// children has to apply the clip to that content itself, and ui.Text
	// does.
	//
	// Honest limitation: the clip is the bounding rectangle and not the
	// rounded shape, on both sides. A shape accurate clip needs stencil or
	// shader support and is a backend concern.
	Clip bool

	// Transform maps this node and its subtree into the space of its parent.
	// A nil Transform, the default, is the identity.
	//
	// It is shared between the two halves of a frame: [App.paintNode] pushes
	// it into the display list and [App.hitNode] composes the same matrix, so
	// input and output cannot disagree about where a node is. The project
	// plan, section 7, requires exactly that.
	//
	// The pointer is retained for as long as the node lives and must not be
	// modified afterwards; build a new one instead.
	//
	// It is a *build time* declaration, which is why scrolling does not use
	// it: a scroll offset changes without a rebuild, and gift would then have
	// to mutate a value the view owns. See [Element.Scroll].
	Transform *geom.Affine2D

	// Scroll declares this node to be a scroll viewport along one axis.
	//
	// A viewport clips its subtree to its own bounds and translates its
	// children by the scroll offset, which gift keeps in the retained node as
	// presentation state. Both halves are read exactly once, on the way into
	// the subtree, by [App.beginSubtree] for paint and by [App.hitNode] for
	// input — the same single reader rule [Element.Clip] follows, and for the
	// same reason.
	//
	// The translation deliberately applies to the children and not to the
	// node itself: the viewport has to stay where the layout put it, because
	// that is the rectangle it clips against. A node that also carries a
	// Transform composes normally, with its own transform applied first.
	//
	// gift installs its own [Interactor] on a scroll node that declares none,
	// which is what makes wheel, drag and kinetic scrolling work without a
	// view writing any gesture code. A nil Scroll, the default, is an
	// ordinary node.
	Scroll *ScrollSpec
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
