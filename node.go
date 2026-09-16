package gift

import (
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// nodeData is the gift owned payload of a scene node.
//
// It is stored *inline* in the node, not behind a pointer: a tree of N nodes
// costs N node structs inside the store's blocks and no separate heap object
// per node. The scene store never clears it on free, which is what makes an
// unmount/remount cycle allocation free — the child side tables keep their
// backing arrays and the recycled slot hands them straight back. See the
// documentation of the scene package.
//
// The flip side is that everything the garbage collector must not retain has
// to be released explicitly, which is what [App.destroyScopes] does.
type nodeData struct {
	// view is the view description the node was last built from. It is kept
	// for diagnostics and for the ownership check.
	view View
	// scope is non nil exactly for component nodes.
	scope *scope

	layouter Layouter
	painter  Painter

	// label is [Element.Label]: what a person would call this node. It is
	// written once per build and read by nothing in the frame path; see the
	// documentation of that field.
	label string

	// interactor, focusable, disabled and clip are the input half of the
	// element; see [Element]. They are plain fields in the payload, so the
	// hit test reads them without an interface dispatch and the dispatcher
	// writes hover and press without touching the view.
	interactor Interactor
	focusable  bool
	disabled   bool
	clip       bool

	// obstructs is [Element.Obstructs]. It is read by nothing in the frame
	// path; the node it names is remembered in [softInputState] instead, and
	// this field only says whether this node is that one.
	obstructs bool

	// xform is [Element.Transform]. It is a pointer because the common case
	// is the identity and a nil check is cheaper than comparing six floats.
	xform *geom.Affine2D

	// scroll is the retained presentation state of a scroll container, nil
	// for every other node. Like [Interaction] it lives here and not in the
	// view, because the project plan, section 5, requires the scroll offset
	// to survive a rebuild and, much more importantly, not to cause one.
	//
	// It translates the *children* of this node rather than the node itself,
	// which is what lets one node be both the clipping viewport and the
	// source of the content transform; see [App.beginSubtree].
	scroll *scrollState

	// control is the retained gesture and animation state of a control; see
	// [ControlState]. It is a value and not a pointer, because it is six
	// words and a pointer would be an allocation per control node for the
	// sake of saving them on every node that is not one.
	//
	// It is *not* refreshed from the element, exactly like ia below: a
	// rebuild replaces the view and not the node, and the whole reason this
	// field exists is that a control rebuilds itself on every step of its own
	// gesture. It is dropped when the node is unmounted, like every other
	// payload field that outlives a build; see [nodeData.release].
	control ControlState

	// ia is the interaction state gift owns for this node. It lives here and
	// not in the view because that is what makes hover and press survive a
	// rebuild and, more importantly, what makes them not cause one; see
	// [Interaction].
	ia Interaction

	// flex is [Element.Flex] of the last build. It is plain old data stored
	// inline, so that a stack can read the flexibility of a child without an
	// interface dispatch and without allocating.
	flex float32

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

	// lastC are the constraints this node was last measured with, and
	// haveLastC says whether they mean anything yet.
	//
	// This is the cache that makes partial relayout possible: a node that is
	// not marked dirty and is asked for the same constraints again returns
	// its previous size without running its layouter and therefore without
	// touching its subtree. See [App.layoutNode].
	lastC     geom.Constraints
	haveLastC bool

	// overflow is how far this node's content exceeded the size it reported,
	// per axis, as of the last time its layouter ran. It is kept per node and
	// not recomputed per frame because a clean node is not measured again;
	// see [App.setOverflow].
	overflow geom.Size

	// baseline is the distance from the top of this node to the baseline of
	// its content, and hasBaseline says whether the node reported one. See
	// [LayoutContext.ReportBaseline].
	baseline    float32
	hasBaseline bool

	// invalidate is the cached closure handed out by
	// [LayoutContext.Invalidator]. It is nil until a layouter asks for one.
	invalidate func()

	own ownershipGuard
}

// release drops everything the garbage collector must not retain, and
// truncates the reusable side tables to zero length while keeping their
// capacity. It runs when a node is unmounted.
//
// Truncating rather than niling the slices is the point of the exercise: the
// slot goes back on the free list with its buffers intact, so the next mount
// into that slot does not allocate.
func (nd *nodeData) release() {
	nd.view = nil
	nd.scope = nil
	nd.layouter = nil
	nd.painter = nil
	nd.label = ""
	nd.interactor = nil
	nd.focusable = false
	nd.disabled = false
	nd.clip = false
	nd.xform = nil
	nd.scroll = nil
	// The store hands a freed slot back with its payload untouched, which is
	// what makes an unmount/remount cycle allocation free, so a field that is
	// not refreshed from the element has to be dropped here or the next node
	// in this slot inherits it. A control's gesture state is exactly such a
	// field: without this a fresh control could be born Grabbed, holding the
	// grab offset of the drag the node before it never finished, and would
	// then move on the next bare pointer move without a press.
	nd.control = ControlState{}
	nd.ia = Interaction{}
	nd.flex = 0
	nd.childViews = nil
	nd.children = nd.children[:0]
	nd.next = nd.next[:0]
	nd.used = nd.used[:0]
	nd.sizes = nd.sizes[:0]
	nd.origins = nd.origins[:0]
	nd.lastC = geom.Constraints{}
	nd.haveLastC = false
	// The aggregate counters were already adjusted by App.clearOverflow;
	// this only keeps the recycled slot from carrying a stale value.
	nd.overflow = geom.Size{}
	nd.baseline, nd.hasBaseline = 0, false
	// The closure captures a handle that now belongs to a different node.
	// Keeping it would make a stale model invalidate a stranger.
	nd.invalidate = nil
	nd.releaseOwnership()
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
