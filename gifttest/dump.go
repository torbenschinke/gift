package gifttest

import (
	"fmt"
	"strings"

	"github.com/worldiety/gift"
)

// maxDumpLines bounds a tree dump.
//
// A failure message that is longer than a screen is a failure message nobody
// reads. A counter is fifteen nodes and a form is a hundred; a gallery is
// thousands, and printing all of them would bury the three lines that matter.
const maxDumpLines = 120

// Dump returns the retained tree as indented text: type, key, label, bounds
// and the input flags of every node.
//
// It is the thing to print when a test fails for a reason the assertion cannot
// name. Every selector failure prints it automatically.
func (h *Harness) Dump() string { return h.dumpMarked(Selector{}) }

// dumpMarked renders the tree with every node matching mark prefixed by "=>".
//
// The marking is what turns a dump from a wall of text into a diagnosis: in
// the ambiguous case the two offending nodes are visible at a glance, together
// with the ancestors that tell them apart and therefore with the answer to
// "how do I narrow this".
func (h *Harness) dumpMarked(mark Selector) string {
	return h.dumpWith(func(r gift.NodeRef) string {
		if mark.match != nil && mark.match(h, r) {
			return "=> "
		}
		return "   "
	})
}

// dumpMarkedRefs renders the tree with the node an action aimed at marked
// "want" and the node the hit test actually reached marked "got".
//
// Two different marks rather than one, because the whole diagnosis of a
// covered control is "these two are different and here is where they sit
// relative to each other".
func (h *Harness) dumpMarkedRefs(want, got gift.NodeRef) string {
	return h.dumpWith(func(r gift.NodeRef) string {
		switch r {
		case want:
			return "want=> "
		case got:
			return "got => "
		default:
			return "       "
		}
	})
}

// dumpWith renders the tree, asking prefix for the marker of every line.
func (h *Harness) dumpWith(prefix func(gift.NodeRef) string) string {
	var b strings.Builder
	b.WriteString("the tree was:\n")
	lines := 0
	truncated := false
	h.walk(func(r gift.NodeRef, depth int) {
		if lines >= maxDumpLines {
			truncated = true
			return
		}
		lines++
		b.WriteString(prefix(r))
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString(h.nodeLine(r))
		b.WriteByte('\n')
	})
	if truncated {
		fmt.Fprintf(&b, "   ... truncated after %d nodes\n", maxDumpLines)
	}
	return b.String()
}

// nodeLine is one line of the dump.
func (h *Harness) nodeLine(r gift.NodeRef) string {
	var b strings.Builder
	b.WriteString(gift.TypeName(h.app.NodeType(r)))
	if k := h.app.NodeKey(r); k != "" {
		b.WriteString(" key=")
		b.WriteString(quote(k))
	}
	if l := h.app.NodeLabel(r); l != "" {
		b.WriteString(" text=")
		b.WriteString(quote(l))
	}
	b.WriteString(" bounds=")
	b.WriteString(rectString(h.app.NodeBounds(r)))
	if h.app.NodeInteractive(r) {
		b.WriteString(" interactive")
	}
	ia := h.app.NodeInteraction(r)
	for _, f := range [...]struct {
		on   bool
		name string
	}{
		{ia.Disabled, "disabled"},
		{ia.Focused, "focused"},
		{ia.Hover, "hover"},
		{ia.Pressed, "pressed"},
	} {
		if f.on {
			b.WriteByte(' ')
			b.WriteString(f.name)
		}
	}
	return b.String()
}

// formatMatches lists the nodes a selector matched, numbered, one per line.
func formatMatches(got []Node) string {
	if len(got) == 0 {
		return "  <none>\n"
	}
	var b strings.Builder
	for i, n := range got {
		fmt.Fprintf(&b, "  [%d] %s\n", i, n.describe())
	}
	return b.String()
}

// nearMisses explains a selector that matched nothing by showing what the tree
// does contain along the axis the selector asked about.
//
// This is the difference between "matched 0 nodes" and a diagnosis. A test
// looking for text "Save" gets the list of labels that do exist, where the
// typo, the trailing space or the fact that the button says "SAVE" is
// immediately visible. A test looking for a type that nobody registered gets
// the list of registered type names, which is the only way to tell a typo from
// an absent view.
func (h *Harness) nearMisses(s Selector) string {
	var b strings.Builder
	switch {
	case strings.HasPrefix(s.desc, "text=="), strings.HasPrefix(s.desc, "text~="):
		labels := h.collect(func(r gift.NodeRef) string { return h.app.NodeLabel(r) })
		if len(labels) == 0 {
			return "No node in the tree carries a label at all. " +
				"ByText matches gift.Element.Label; ui.Text sets it, and a custom view has to set it too.\n"
		}
		b.WriteString("labels that do exist:\n")
		for _, l := range labels {
			fmt.Fprintf(&b, "  %s\n", quote(l))
		}
	case strings.HasPrefix(s.desc, "key=="):
		keys := h.collect(func(r gift.NodeRef) string { return h.app.NodeKey(r) })
		if len(keys) == 0 {
			return "No node in the tree has a key. Set one with .Key(\"...\") on the view.\n"
		}
		b.WriteString("keys that do exist:\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s\n", quote(k))
		}
	case strings.HasPrefix(s.desc, "type=="):
		want := strings.Trim(strings.TrimPrefix(s.desc, "type=="), "\"")
		known := gift.RegisteredTypeNames(nil)
		if !contains(known, want) {
			fmt.Fprintf(&b, "No view type is registered under %q. Registered names are:\n  %s\n",
				want, strings.Join(known, ", "))
			break
		}
		b.WriteString("types present in the tree:\n  ")
		b.WriteString(strings.Join(h.collect(func(r gift.NodeRef) string {
			return gift.TypeName(h.app.NodeType(r))
		}), ", "))
		b.WriteByte('\n')
	}
	return b.String()
}

// collect gathers the distinct non empty values of fn over the tree, in
// document order.
func (h *Harness) collect(fn func(gift.NodeRef) string) []string {
	var out []string
	h.walk(func(r gift.NodeRef, _ int) {
		v := fn(r)
		if v == "" || contains(out, v) {
			return
		}
		out = append(out, v)
	})
	return out
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
