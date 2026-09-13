package scene

import (
	"strings"
	"testing"
)

func children(s *Store, h Handle) []Handle {
	var out []Handle
	for c := s.Get(h).FirstChild; !c.IsZero(); c = s.Get(c).NextSibling {
		out = append(out, c)
	}
	return out
}

func TestZeroHandleIsInvalid(t *testing.T) {
	s := NewStore(4)
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

func TestAppendChildOrder(t *testing.T) {
	s := NewStore(8)
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
	if s.Len() != 6 {
		t.Fatalf("Len = %d, want 6", s.Len())
	}
}

func TestFreeReleasesSubtree(t *testing.T) {
	s := NewStore(8)
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
	s := NewStore(8)
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

func TestUseAfterFreePanics(t *testing.T) {
	s := NewStore(4)
	h := s.Alloc()
	s.Free(h)

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "use after free") {
			t.Fatalf("panic = %v, want a use after free diagnosis", r)
		}
	}()
	s.Get(h)
}

func TestFreelistReuseAndGeneration(t *testing.T) {
	s := NewStore(4)
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
	s := NewStore(64)
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

func TestPayloadIsCleared(t *testing.T) {
	s := NewStore(4)
	h := s.Alloc()
	s.Get(h).Payload = new(int)
	s.Get(h).Key = "k"
	idx := h.Index()
	s.Free(h)
	h2 := s.Alloc()
	if h2.Index() != idx {
		t.Skip("slot not reused, nothing to assert")
	}
	if n := s.Get(h2); n.Payload != nil || n.Key != "" {
		t.Fatalf("recycled node not cleared: %+v", n)
	}
}

func TestDoubleFreePanics(t *testing.T) {
	s := NewStore(4)
	h := s.Alloc()
	s.Free(h)
	defer func() {
		if recover() == nil {
			t.Fatal("want panic on double free")
		}
	}()
	s.Free(h)
}
