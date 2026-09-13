package scene

import (
	"strings"
	"testing"
)

// payload is a stand-in for the module root's node data: it holds a slice
// whose backing array must survive a free/reuse cycle.
type payload struct {
	tag  string
	kids []int
}

func newTestStore(capacity int) *Store[payload] { return NewStore[payload](capacity) }

func children(s *Store[payload], h Handle) []Handle {
	var out []Handle
	for c := s.Get(h).FirstChild; !c.IsZero(); c = s.Get(c).NextSibling {
		out = append(out, c)
	}
	return out
}

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("want a panic mentioning %q", want)
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want it to mention %q", r, want)
		}
	}()
	fn()
}

func TestZeroHandleIsInvalid(t *testing.T) {
	s := newTestStore(4)
	var h Handle
	if !h.IsZero() {
		t.Fatal("zero handle must report IsZero")
	}
	if s.Valid(h) {
		t.Fatal("zero handle must not be valid")
	}
	if a := s.Alloc(); a.IsZero() {
		t.Fatal("Alloc must never return the zero handle")
	}
}

// TestZeroStoreIsUsable pins the documented behaviour of the zero value. The
// branch in Alloc that creates the reserved slot 0 used to be dead code,
// because the only constructor always produced it; either the zero Store works
// or the branch has to go.
func TestZeroStoreIsUsable(t *testing.T) {
	var s Store[payload]
	if s.Len() != 0 {
		t.Fatalf("Len = %d", s.Len())
	}
	root := s.Alloc()
	if root.IsZero() || !s.Valid(root) {
		t.Fatal("Alloc on the zero Store must return a live handle")
	}
	if root.Index() == 0 {
		t.Fatal("slot 0 must stay reserved even on a zero Store")
	}
	c := s.Alloc()
	s.AppendChild(root, c)
	if got := children(&s, root); len(got) != 1 || got[0] != c {
		t.Fatalf("children = %v", got)
	}
}

func TestAppendChildOrder(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	var want []Handle
	for range 5 {
		c := s.Alloc()
		s.AppendChild(root, c)
		want = append(want, c)
	}
	got := children(s, root)
	if len(got) != len(want) {
		t.Fatalf("got %d children, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("child %d = %v, want %v", i, got[i], want[i])
		}
		if s.Get(got[i]).Parent != root {
			t.Fatalf("child %d has wrong parent", i)
		}
	}
	if s.ChildCount(root) != 5 {
		t.Fatalf("ChildCount = %d, want 5", s.ChildCount(root))
	}
	if s.Len() != 6 {
		t.Fatalf("Len = %d, want 6", s.Len())
	}
}

func TestFreeReleasesSubtree(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a := s.Alloc()
	b := s.Alloc()
	s.AppendChild(root, a)
	s.AppendChild(root, b)
	var grand []Handle
	for range 3 {
		g := s.Alloc()
		s.AppendChild(a, g)
		grand = append(grand, g)
	}
	if s.Len() != 6 {
		t.Fatalf("Len = %d", s.Len())
	}

	s.Free(a)

	if s.Valid(a) {
		t.Fatal("freed node still valid")
	}
	for i, g := range grand {
		if s.Valid(g) {
			t.Fatalf("grandchild %d still valid", i)
		}
	}
	if s.Len() != 2 {
		t.Fatalf("Len after free = %d, want 2", s.Len())
	}
	got := children(s, root)
	if len(got) != 1 || got[0] != b {
		t.Fatalf("sibling list not repaired: %v", got)
	}
	// The last child cache must be repaired as well.
	c := s.Alloc()
	s.AppendChild(root, c)
	got = children(s, root)
	if len(got) != 2 || got[0] != b || got[1] != c {
		t.Fatalf("append after free broken: %v", got)
	}
}

func TestFreeFirstChild(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a, b := s.Alloc(), s.Alloc()
	s.AppendChild(root, a)
	s.AppendChild(root, b)
	s.Free(a)
	if got := children(s, root); len(got) != 1 || got[0] != b {
		t.Fatalf("children = %v", got)
	}
	if s.Get(root).FirstChild != b {
		t.Fatal("FirstChild not updated")
	}
}

// TestFreeMiddleChild exercises the relink branch of detach, which neither of
// the tests above reaches: they only ever free the first or the last child.
func TestFreeMiddleChild(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a, b, c := s.Alloc(), s.Alloc(), s.Alloc()
	for _, h := range []Handle{a, b, c} {
		s.AppendChild(root, h)
	}

	s.Free(b)

	if s.Valid(b) {
		t.Fatal("freed node still valid")
	}
	got := children(s, root)
	if len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("children after freeing the middle one = %v, want [a c]", got)
	}
	if s.Get(a).NextSibling != c {
		t.Fatal("the predecessor was not relinked to the successor")
	}
	if s.ChildCount(root) != 2 {
		t.Fatalf("ChildCount = %d, want 2", s.ChildCount(root))
	}
	// The last child cache must still point at c, not at the freed node.
	d := s.Alloc()
	s.AppendChild(root, d)
	if got := children(s, root); len(got) != 3 || got[2] != d {
		t.Fatalf("append after freeing the middle child = %v", got)
	}
}

func TestUseAfterFreePanics(t *testing.T) {
	s := newTestStore(4)
	h := s.Alloc()
	s.Free(h)
	mustPanic(t, "use after free", func() { s.Get(h) })
}

func TestFreelistReuseAndGeneration(t *testing.T) {
	s := newTestStore(4)
	h := s.Alloc()
	idx := h.Index()
	s.Free(h)
	h2 := s.Alloc()
	if h2.Index() != idx {
		t.Fatalf("slot not reused: %d vs %d", h2.Index(), idx)
	}
	if h2 == h {
		t.Fatal("recycled handle must differ from the stale one")
	}
	if s.Valid(h) {
		t.Fatal("stale handle must stay invalid after the slot is reused")
	}
	if !s.Valid(h2) {
		t.Fatal("fresh handle must be valid")
	}
}

func TestAllocFreeCycleIsAllocationFree(t *testing.T) {
	s := newTestStore(64)
	cycle := func() {
		root := s.Alloc()
		for range 16 {
			c := s.Alloc()
			s.AppendChild(root, c)
			for range 2 {
				g := s.Alloc()
				s.AppendChild(c, g)
			}
		}
		s.Free(root)
	}
	for range 8 {
		cycle()
	}
	if got := testing.AllocsPerRun(100, cycle); got != 0 {
		t.Fatalf("alloc/free cycle allocated %v times per run, want 0", got)
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
}

// TestFreeKeepsThePayloadAndClearsTheRest is the contract that makes an
// unmount/remount cycle allocation free: the node fields are reset, but the
// payload is handed back to the slot as it was, so the slices it owns keep
// their backing arrays.
func TestFreeKeepsThePayloadAndClearsTheRest(t *testing.T) {
	s := newTestStore(4)
	h := s.Alloc()
	n := s.Get(h)
	n.Key = "k"
	n.TypeID = 7
	n.Payload.tag = "mine"
	n.Payload.kids = append(n.Payload.kids, 1, 2, 3)
	wantCap := cap(n.Payload.kids)
	idx := h.Index()

	s.Free(h)

	h2 := s.Alloc()
	if h2.Index() != idx {
		t.Fatalf("slot %d was not reused, got %d; the free list regressed", idx, h2.Index())
	}
	n2 := s.Get(h2)
	if n2.Key != "" || n2.TypeID != 0 || !n2.Parent.IsZero() || n2.Flags&FlagMounted == 0 {
		t.Fatalf("recycled node not reset: %+v", struct {
			Key    string
			TypeID uint32
		}{n2.Key, n2.TypeID})
	}
	if n2.Payload.tag != "mine" {
		t.Fatalf("payload was cleared on free: tag = %q, want it preserved", n2.Payload.tag)
	}
	if got := cap(n2.Payload.kids); got != wantCap {
		t.Fatalf("payload slice capacity = %d, want %d preserved across the cycle", got, wantCap)
	}
}

func TestDoubleFreePanics(t *testing.T) {
	s := newTestStore(4)
	h := s.Alloc()
	s.Free(h)
	mustPanic(t, "use after free", func() { s.Free(h) })
}

// --- SetChildren ------------------------------------------------------------

func TestSetChildrenReorders(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a, b, c := s.Alloc(), s.Alloc(), s.Alloc()
	for _, h := range []Handle{a, b, c} {
		s.AppendChild(root, h)
	}

	s.SetChildren(root, []Handle{c, a, b})

	got := children(s, root)
	if len(got) != 3 || got[0] != c || got[1] != a || got[2] != b {
		t.Fatalf("children = %v, want [c a b]", got)
	}
	if s.Get(root).FirstChild != c {
		t.Fatal("FirstChild not updated")
	}
	if !s.Get(b).NextSibling.IsZero() {
		t.Fatal("the new last child must terminate the chain")
	}
	// The lastChild cache must follow the reorder, otherwise an append lands
	// after the wrong node.
	d := s.Alloc()
	s.AppendChild(root, d)
	got = children(s, root)
	if len(got) != 4 || got[3] != d {
		t.Fatalf("append after reorder = %v, want d last", got)
	}
	if s.ChildCount(root) != 4 {
		t.Fatalf("ChildCount = %d, want 4", s.ChildCount(root))
	}
}

func TestSetChildrenWithEmptyKids(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()

	// A node with no children accepts an empty list.
	s.SetChildren(root, nil)
	if !s.Get(root).FirstChild.IsZero() {
		t.Fatal("FirstChild must stay zero")
	}

	a := s.Alloc()
	s.AppendChild(root, a)
	s.FreeChild(root, a)
	s.SetChildren(root, nil)
	if !s.Get(root).FirstChild.IsZero() {
		t.Fatal("FirstChild must be cleared after the only child was freed")
	}
	// The lastChild cache must be cleared too, otherwise the next append
	// links to a dead node.
	b := s.Alloc()
	s.AppendChild(root, b)
	if got := children(s, root); len(got) != 1 || got[0] != b {
		t.Fatalf("children = %v, want [b]", got)
	}
}

// TestSetChildrenRejectsADroppedChild pins the leak this check exists for: a
// child that is omitted from kids keeps its Parent, becomes unreachable from
// the child list and is therefore never freed by anyone.
func TestSetChildrenRejectsADroppedChild(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a, b := s.Alloc(), s.Alloc()
	s.AppendChild(root, a)
	s.AppendChild(root, b)

	mustPanic(t, "every current child must be listed", func() {
		s.SetChildren(root, []Handle{a})
	})
}

func TestSetChildrenRejectsAForeignNode(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a := s.Alloc()
	s.AppendChild(root, a)
	stranger := s.Alloc()

	mustPanic(t, "is not a child of", func() {
		s.SetChildren(root, []Handle{stranger})
	})
}

// TestFreeChildLeavesTheChainToSetChildren documents the deliberate
// asymmetry: FreeChild skips the sibling walk, so the chain is stale until
// SetChildren rewrites it, and the child count is what makes that safe.
func TestFreeChildLeavesTheChainToSetChildren(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	a, b, c := s.Alloc(), s.Alloc(), s.Alloc()
	for _, h := range []Handle{a, b, c} {
		s.AppendChild(root, h)
	}

	s.FreeChild(root, b)
	if s.Valid(b) {
		t.Fatal("FreeChild must release the node")
	}
	if s.ChildCount(root) != 2 {
		t.Fatalf("ChildCount = %d, want 2", s.ChildCount(root))
	}

	s.SetChildren(root, []Handle{a, c})
	got := children(s, root)
	if len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("children = %v, want [a c]", got)
	}
}

func TestFreeChildRejectsAForeignNode(t *testing.T) {
	s := newTestStore(8)
	root := s.Alloc()
	stranger := s.Alloc()
	mustPanic(t, "is not a child of", func() { s.FreeChild(root, stranger) })
}

// TestDepthCapPanics makes sure a runaway recursion is reported instead of
// overflowing the goroutine stack.
func TestDepthCapPanics(t *testing.T) {
	s := newTestStore(MaxDepth + 8)
	root := s.Alloc()
	cur := root
	for range MaxDepth + 4 {
		c := s.Alloc()
		s.AppendChild(cur, c)
		cur = c
	}
	mustPanic(t, "deeper than", func() { s.Free(root) })
}
