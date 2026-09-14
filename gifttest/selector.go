package gifttest

import (
	"strings"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// Selector is a predicate over the nodes of the retained tree.
//
// Selectors are the reason a test in this package contains no coordinates. A
// test says "the button that says +", the harness works out where that is, and
// a redesign that moves the button by forty pixels changes nothing.
//
// They compose with [Selector.And], [Selector.Or] and [Not], and they carry a
// description that ends up in the failure message, so a compound selector
// explains itself:
//
//	gifttest: Find(type=="ui.Button" && text=="+") matched 0 nodes
type Selector struct {
	// desc is what the selector prints as. It is built up by the combinators
	// so that a failure names the query and not a function pointer.
	desc string
	// match is the predicate. It receives the harness so that it can ask the
	// App about the node; nothing in a selector reaches into gift directly.
	match func(h *Harness, r gift.NodeRef) bool
}

// String returns the human readable form of the selector, as it appears in a
// failure message.
func (s Selector) String() string {
	if s.desc == "" {
		return "<any>"
	}
	return s.desc
}

// ByText matches a node whose label is exactly s.
//
// The label is [gift.Element.Label]: [ui.Text] sets it to its string and
// [ui.ButtonView.Label] sets it explicitly. It is not the text that was
// rasterised — the harness does not read pixels — which means ByText finds a
// label that is drawn in a colour nobody can see, and that is the correct
// behaviour: the test is about what the application says, and an invisible
// label is a different bug with its own assertion.
//
// A button whose label view is a [ui.Text] is found through that text node,
// not through the button node. The actions cope with this; see [Node.Click].
func ByText(s string) Selector {
	return Selector{
		desc: "text==" + quote(s),
		match: func(h *Harness, r gift.NodeRef) bool {
			return h.app.NodeLabel(r) == s
		},
	}
}

// ByTextContains matches a node whose label contains sub.
//
// It exists for labels that are assembled at runtime — "3 items selected" —
// where the interesting part is a fragment. Prefer [ByText]: a substring match
// is the usual way a selector silently starts matching two nodes.
func ByTextContains(sub string) Selector {
	return Selector{
		desc: "text~=" + quote(sub),
		match: func(h *Harness, r gift.NodeRef) bool {
			return strings.Contains(h.app.NodeLabel(r), sub)
		},
	}
}

// ByKey matches a node whose reconciliation key is k.
//
// This is the selector to reach for when a test has to be certain, because a
// key is the one identifier the application controls explicitly and the one
// that survives a change of wording. A key only has to be unique among
// siblings, so it is not automatically unique in the tree; combine it with
// [ByType] or with an ancestor filter when it is not.
func ByKey(k string) Selector {
	return Selector{
		desc: "key==" + quote(k),
		match: func(h *Harness, r gift.NodeRef) bool {
			return h.app.NodeKey(r) == k
		},
	}
}

// ByType matches a node built from the view type registered under name, such
// as "ui.Button" or "ui.Text".
//
// The name is the one passed to [gift.RegisterType]. A name no view type
// registered matches nothing, and the failure message says so with the list of
// names that do exist, because a typo here is otherwise indistinguishable from
// "the view is not on screen".
func ByType(name string) Selector {
	return Selector{
		desc: "type==" + quote(name),
		match: func(h *Harness, r gift.NodeRef) bool {
			return gift.TypeName(h.app.NodeType(r)) == name
		},
	}
}

// Interactive matches a node that takes part in hit testing, that is one whose
// element carried a [gift.Interactor]. A disabled control is interactive by
// this definition: it still swallows clicks.
func Interactive() Selector {
	return Selector{
		desc: "interactive",
		match: func(h *Harness, r gift.NodeRef) bool {
			return h.app.NodeInteractive(r)
		},
	}
}

// Focusable matches a node that could currently take the keyboard focus.
func Focusable() Selector {
	return Selector{
		desc: "focusable",
		match: func(h *Harness, r gift.NodeRef) bool {
			return h.app.NodeFocusable(r)
		},
	}
}

// Enabled matches a node that is not disabled.
func Enabled() Selector {
	return Selector{
		desc: "enabled",
		match: func(h *Harness, r gift.NodeRef) bool {
			return !h.app.NodeInteraction(r).Disabled
		},
	}
}

// Any matches every node in the tree. It is the base of a hand written
// predicate and the argument of [Harness.Dump] when everything is wanted.
func Any() Selector {
	return Selector{desc: "any", match: func(*Harness, gift.NodeRef) bool { return true }}
}

// Where builds a selector from an arbitrary predicate over a [Node].
//
// desc is what it prints as in a failure message and is not optional: a
// message that says `Find(func) matched 2 nodes` helps nobody.
func Where(desc string, pred func(Node) bool) Selector {
	return Selector{
		desc: desc,
		match: func(h *Harness, r gift.NodeRef) bool {
			return pred(Node{h: h, ref: r})
		},
	}
}

// And matches when both selectors match the same node.
func (s Selector) And(o Selector) Selector {
	return Selector{
		desc: "(" + s.String() + " && " + o.String() + ")",
		match: func(h *Harness, r gift.NodeRef) bool {
			return s.match(h, r) && o.match(h, r)
		},
	}
}

// Or matches when either selector matches the node.
func (s Selector) Or(o Selector) Selector {
	return Selector{
		desc: "(" + s.String() + " || " + o.String() + ")",
		match: func(h *Harness, r gift.NodeRef) bool {
			return s.match(h, r) || o.match(h, r)
		},
	}
}

// Not inverts a selector.
func Not(s Selector) Selector {
	return Selector{
		desc: "!" + s.String(),
		match: func(h *Harness, r gift.NodeRef) bool {
			return !s.match(h, r)
		},
	}
}

// Under matches a node that is a descendant of some node matching parent.
//
// It is the disambiguator for a list of identical rows: ByText("Delete")
// matches ten nodes, Under(ByKey("row-3")).And(ByText("Delete")) matches one.
func Under(parent Selector) Selector {
	return Selector{
		desc: "under(" + parent.String() + ")",
		match: func(h *Harness, r gift.NodeRef) bool {
			for p := h.app.NodeParent(r); !p.IsZero(); p = h.app.NodeParent(p) {
				if parent.match(h, p) {
					return true
				}
			}
			return false
		},
	}
}

// --- running a selector -----------------------------------------------------

// Find returns the single node matching s and fails the test when there is not
// exactly one.
//
// Both failure modes print the query, the nodes that did match, and a dump of
// the tree with the matches marked. That is the whole reason this package
// exists in the shape it does: a selector that goes wrong is the most common
// failure in a UI test suite, and "expected 1, got 2" is not a diagnosis.
func (h *Harness) Find(s Selector) Node {
	h.t.Helper()
	got := h.findAll(s)
	switch len(got) {
	case 1:
		return got[0]
	case 0:
		h.t.Fatalf("gifttest: Find(%s) matched no node.\n%s\n%s",
			s, h.nearMisses(s), h.dumpMarked(s))
	default:
		h.t.Fatalf("gifttest: Find(%s) matched %d nodes, want exactly 1.\n\nmatches:\n%s\n%s\n%s",
			s, len(got), formatMatches(got), disambiguationHint, h.dumpMarked(s))
	}
	return Node{}
}

// First returns the first node matching s in document order and fails when
// there is none.
//
// It is the deliberate opposite of [Harness.Find]: use it when several matches
// are expected and the first one is meant, and say so in the test by using
// this name rather than by ignoring an ambiguity.
func (h *Harness) First(s Selector) Node {
	h.t.Helper()
	got := h.findAll(s)
	if len(got) == 0 {
		h.t.Fatalf("gifttest: First(%s) matched no node.\n%s\n%s",
			s, h.nearMisses(s), h.dumpMarked(s))
		return Node{}
	}
	return got[0]
}

// FindAll returns every node matching s, in document order. An empty result is
// not a failure; assert on it with [Harness.AssertCount] or with len.
func (h *Harness) FindAll(s Selector) []Node {
	h.t.Helper()
	return h.findAll(s)
}

// Exists reports whether at least one node matches s, without failing.
func (h *Harness) Exists(s Selector) bool {
	h.t.Helper()
	return len(h.findAll(s)) > 0
}

// At returns the topmost interactive node at the device space point p.
//
// This is the raw escape hatch, and it is deliberately the only one. It uses
// the same hit test the pointer dispatcher uses, so what it returns is what a
// click at p would reach — including "nothing", which fails the test with the
// point and a dump. Reach for it when the thing under test *is* a coordinate:
// overlapping siblings, a clip boundary, a hit area that should not extend
// into a shadow.
func (h *Harness) At(p geom.Point) Node {
	h.t.Helper()
	ref, ok := h.app.HitTest(p)
	if !ok {
		h.t.Fatalf("gifttest: At(%g, %g) hit no interactive node.\n%s", p.X, p.Y, h.Dump())
		return Node{}
	}
	return Node{h: h, ref: ref}
}

// findAll walks the tree in document order and collects the matches.
//
// Document order is the pre order traversal gift uses for the focus order too,
// so "the first match" means the same thing here and there.
//
// # Why the buffer is not shared
//
// It used to be one scratch slice on the Harness, reset at the top of this
// function and returned to the caller. That is only safe as long as a selector
// cannot run a selector, and selectors can: [Under] does it internally, and
// [Where] invites an application's predicate to do it — "the row whose Delete
// button is enabled" is a query inside a predicate. The inner run truncated
// the buffer the outer run was appending to and then appended its own matches
// on top, so an outer query over three nodes came back with four Nodes, two of
// them the inner query's. [Harness.Find] then saw a count that had nothing to
// do with the tree: it reported one match where there were two, and the test
// asserted against an arbitrary node while the selector it was written with
// was ambiguous.
//
// The buffer is therefore per call. That is one allocation per query, in a
// test, where [Harness.FindAll] already allocates a copy and the tree walk
// already copies the children of every node for the same re entrancy reason.
// Correctness over a scratch slice; see [Harness.walkNode].
func (h *Harness) findAll(s Selector) []Node {
	var out []Node
	h.walk(func(r gift.NodeRef, _ int) {
		if s.match(h, r) {
			out = append(out, Node{h: h, ref: r})
		}
	})
	return out
}

// walk visits every node in document order, passing the depth along for the
// benefit of the dump.
func (h *Harness) walk(fn func(r gift.NodeRef, depth int)) {
	root := h.app.Root()
	if root.IsZero() {
		return
	}
	h.walkNode(root, 0, fn)
}

func (h *Harness) walkNode(r gift.NodeRef, depth int, fn func(gift.NodeRef, int)) {
	fn(r, depth)
	// The children are copied into a local slice before recursing because fn
	// may itself run a selector — Under does — and a shared scratch buffer
	// cannot survive re entrancy. A test is not the frame path; correctness
	// wins over the allocation here.
	kids := h.app.NodeChildren(r, nil)
	for _, c := range kids {
		h.walkNode(c, depth+1, fn)
	}
}

const disambiguationHint = "Say which one you mean: give the view a .Key(\"...\") and use ByKey, " +
	"narrow the query with .And(ByType(\"ui.Button\")) or Under(...), " +
	"or use First if any match will do."

func quote(s string) string {
	if len(s) > 40 {
		return "\"" + s[:37] + "...\""
	}
	return "\"" + s + "\""
}
