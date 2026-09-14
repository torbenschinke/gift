package gift

// This file is the read only window onto the retained tree.
//
// # Why it exists and why it is this small
//
// [App.HitTest] answers "what is at this point", which is the question input
// asks. Everything outside the frame path — a diagnostic dump, an automated
// test, later an accessibility bridge — asks the other question: "where is the
// node that says Save". Answering it needs a traversal, and a traversal needs
// a root, a child list and a way to tell one node from another.
//
// The methods below are that and nothing more. They hand out [NodeRef] values,
// which carry no pointer into the store and cannot be forged, and they return
// zero values for a stale reference instead of panicking, because a reference
// obtained before a rebuild is a perfectly ordinary thing for a caller to
// still hold. None of them is called from the frame path.

// Root returns the reference to the root node, which is the component instance
// mounted from [Options.Root].
//
// It is the entry point of every traversal. The tree it heads is the retained
// tree, not the view tree: a component appears as one node of type
// "gift.Component" with the view it returned as its only child.
func (a *App) Root() NodeRef {
	if a.root == nil || !a.store.Valid(a.root.node) {
		return NodeRef{}
	}
	return NodeRef{a.root.node}
}

// NodeParent returns the parent of r, or the zero [NodeRef] for the root and
// for a stale reference.
func (a *App) NodeParent(r NodeRef) NodeRef {
	if !a.store.Valid(r.h) {
		return NodeRef{}
	}
	p := a.store.Get(r.h).Parent
	if !a.store.Valid(p) {
		return NodeRef{}
	}
	return NodeRef{p}
}

// NodeChildren appends the children of r to dst, in order, and returns the
// extended slice.
//
// It appends rather than allocating so that a traversal can reuse one buffer,
// which is what keeps a repeated tree walk — a test that runs a selector after
// every frame, say — from generating garbage. Pass nil for a fresh slice.
func (a *App) NodeChildren(r NodeRef, dst []NodeRef) []NodeRef {
	if !a.store.Valid(r.h) {
		return dst
	}
	for _, c := range a.data(r.h).children {
		dst = append(dst, NodeRef{c})
	}
	return dst
}

// NodeType returns the [TypeID] of the view the node was built from, or the
// zero TypeID for a stale reference. Pass it to [TypeName] for the name the
// view type registered under, such as "ui.Button".
func (a *App) NodeType(r NodeRef) TypeID {
	if !a.store.Valid(r.h) {
		return 0
	}
	return TypeID(a.store.Get(r.h).TypeID)
}

// NodeLabel returns [Element.Label] of the node, or the empty string for a
// node that declared none and for a stale reference.
//
// This is the string a test's ByText selector matches against and the string
// an accessibility bridge would read out. It is not the text that was drawn:
// a node that draws its label as glyphs sets both from the same value, but
// nothing enforces that, and nothing can — a view may legitimately say more
// than it shows.
func (a *App) NodeLabel(r NodeRef) string {
	if !a.store.Valid(r.h) {
		return ""
	}
	return a.data(r.h).label
}

// NodeInteractive reports whether the node takes part in hit testing, that is
// whether its element carried an [Interactor].
//
// A disabled node is still interactive by this definition: it is a hit target
// that swallows clicks, which is exactly what [Element.Disabled] promises. Ask
// [App.NodeInteraction] for the Disabled bit.
func (a *App) NodeInteractive(r NodeRef) bool {
	if !a.store.Valid(r.h) {
		return false
	}
	return a.data(r.h).interactor != nil
}

// NodeFocusable reports whether the node may currently hold the keyboard
// focus, that is whether it would be visited by [App.MoveFocus].
func (a *App) NodeFocusable(r NodeRef) bool {
	if !a.store.Valid(r.h) {
		return false
	}
	return a.focusable(r.h)
}

// NodeValid reports whether r still refers to a live node. A reference goes
// stale when its node is unmounted, and the slot may then be reused by an
// unrelated node — the generation in the handle is what keeps the two apart.
func (a *App) NodeValid(r NodeRef) bool { return a.store.Valid(r.h) }
