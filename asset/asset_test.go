package asset_test

import (
	"strconv"
	"testing"

	"github.com/torbenschinke/gift/asset"
)

func entries(n int) []asset.Metadata {
	out := make([]asset.Metadata, n)
	for i := range n {
		out[i] = asset.Metadata{
			ID:       asset.ID("e" + strconv.Itoa(i)),
			Revision: "r0",
			Width:    uint32(100 + i),
			Height:   100,
		}
	}
	return out
}

func TestCollectionBasics(t *testing.T) {
	c := asset.NewCollection(entries(10))
	if c.Len() != 10 {
		t.Fatalf("Len = %d", c.Len())
	}
	if c.ID(3) != "e3" {
		t.Errorf("ID(3) = %q", c.ID(3))
	}
	if i, ok := c.Find("e7"); !ok || i != 7 {
		t.Errorf("Find(e7) = %d, %v", i, ok)
	}
	if _, ok := c.Find("nope"); ok {
		t.Error("Find found an entry that is not there")
	}
	if w, h := c.DimensionsAt(3); w != 103 || h != 100 {
		t.Errorf("DimensionsAt(3) = %d, %d", w, h)
	}
	// Out of range is answered, not punished: the layout index reads this in
	// a loop whose bound it owns.
	if w, h := c.DimensionsAt(999); w != 0 || h != 0 {
		t.Errorf("DimensionsAt(999) = %d, %d, want zeroes", w, h)
	}
	if c.StructureVersion() == 0 || c.MetadataVersion() == 0 {
		t.Error("a fresh collection has version 0; 0 must mean something else")
	}
}

func TestCollectionRejectsAmbiguousIdentity(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("an empty ID was accepted")
			}
		}()
		asset.NewCollection([]asset.Metadata{{ID: "a"}, {}})
	})
	t.Run("duplicate", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("a duplicate ID was accepted")
			}
		}()
		asset.NewCollection([]asset.Metadata{{ID: "a"}, {ID: "a"}})
	})
}

// TestCollectionCorrectionsAreVersionedAndAddressedByID pins the two
// properties the gallery depends on: a batch that changes nothing costs
// nothing downstream, and a correction for an entry that has gone is ignored
// rather than misapplied.
func TestCollectionCorrectionsAreVersionedAndAddressedByID(t *testing.T) {
	c := asset.NewCollection([]asset.Metadata{
		{ID: "a", Revision: "r0"},
		{ID: "b", Revision: "r0", Width: 10, Height: 10},
	})
	v0, m0 := c.StructureVersion(), c.MetadataVersion()

	if n := c.ApplyCorrections(nil); n != 0 || c.MetadataVersion() != m0 {
		t.Errorf("an empty batch changed %d entries and moved the metadata version to %d",
			n, c.MetadataVersion())
	}
	if n := c.ApplyCorrections([]asset.Correction{{ID: "gone", Width: 1, Height: 1}}); n != 0 {
		t.Errorf("a correction for an unknown ID changed %d entries", n)
	}
	if c.MetadataVersion() != m0 {
		t.Error("a batch that changed nothing moved the metadata version")
	}

	n := c.ApplyCorrections([]asset.Correction{
		{ID: "a", Width: 400, Height: 300, Revision: "r1"},
		{ID: "b", Revision: "r1"}, // revision only: dimensions survive
	})
	if n != 2 {
		t.Fatalf("%d of 2 corrections applied", n)
	}
	if c.MetadataVersion() == m0 {
		t.Error("a real correction did not move the metadata version")
	}
	// And it did not move the *structure* version, which is the whole of the
	// WU-O split: a correction inserts, removes and moves nothing, so
	// everything keyed by position — a layout index, a tile binding — is
	// still valid and must not be thrown away. See
	// [asset.Collection.StructureVersion].
	if c.StructureVersion() != v0 {
		t.Errorf("a correction moved the structure version from %d to %d; a consumer that "+
			"keys by position would unbind everything for a change that moved nothing",
			v0, c.StructureVersion())
	}
	if m := c.At(0); m.Width != 400 || m.Height != 300 || m.Revision != "r1" {
		t.Errorf("entry a is %+v", m)
	}
	if m := c.At(1); m.Width != 10 || m.Height != 10 || m.Revision != "r1" {
		t.Errorf("a revision only correction erased the dimensions of b: %+v", m)
	}
	// Applying the same batch again is a no-op.
	v1 := c.MetadataVersion()
	if n := c.ApplyCorrections([]asset.Correction{{ID: "a", Width: 400, Height: 300, Revision: "r1"}}); n != 0 {
		t.Errorf("re-applying a correction changed %d entries", n)
	}
	if c.MetadataVersion() != v1 {
		t.Error("re-applying a correction moved the metadata version")
	}
}

// TestCollectionStructuralChangesMoveBothVersions is the other side of
// TestCollectionCorrectionsAreVersionedAndAddressedByID: a reset and a reorder
// move entries, so they invalidate everything keyed by position *and*
// everything keyed by content, and both numbers have to say so.
func TestCollectionStructuralChangesMoveBothVersions(t *testing.T) {
	c := asset.NewCollection([]asset.Metadata{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	for _, tc := range []struct {
		name string
		do   func()
	}{
		{"reorder", func() { c.Reorder([]int{2, 0, 1}) }},
		{"reset", func() { c.Reset([]asset.Metadata{{ID: "x"}}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sv, mv := c.StructureVersion(), c.MetadataVersion()
			tc.do()
			if c.StructureVersion() == sv {
				t.Errorf("a %s did not move the structure version", tc.name)
			}
			if c.MetadataVersion() == mv {
				t.Errorf("a %s did not move the metadata version", tc.name)
			}
		})
	}
}

func TestCollectionReorderMovesPositionsNotIdentities(t *testing.T) {
	c := asset.NewCollection(entries(4))
	c.Reorder([]int{3, 2, 1, 0})
	if c.ID(0) != "e3" || c.ID(3) != "e0" {
		t.Errorf("after reversing, the order is %q..%q", c.ID(0), c.ID(3))
	}
	if i, ok := c.Find("e0"); !ok || i != 3 {
		t.Errorf("Find(e0) after the reorder = %d, %v; the index was not rebuilt", i, ok)
	}
	defer func() {
		if recover() == nil {
			t.Error("a non permutation was accepted")
		}
	}()
	c.Reorder([]int{0, 0, 1, 2})
}

// --- selection ---------------------------------------------------------------

func TestSelectionIsExpressedInIDs(t *testing.T) {
	c := asset.NewCollection(entries(10))
	s := asset.NewSelection()

	s.Only("e5")
	if !s.Contains("e5") || s.Len() != 1 || s.Cursor() != "e5" {
		t.Fatalf("Only did not select and put the cursor on e5: %d selected, cursor %q", s.Len(), s.Cursor())
	}
	// A reorder moves positions; the selection is unmoved because it never
	// held one.
	c.Reorder([]int{9, 8, 7, 6, 5, 4, 3, 2, 1, 0})
	if !s.Contains("e5") {
		t.Error("the selection did not survive a reorder")
	}
	if i, _ := c.Find("e5"); i != 4 {
		t.Errorf("e5 is now at %d, want 4", i)
	}
}

func TestSelectionRangeFollowsCollectionOrder(t *testing.T) {
	c := asset.NewCollection(entries(10))
	s := asset.NewSelection()
	s.Only("e2")
	s.SelectRange(c, "e5")
	if s.Len() != 4 {
		t.Fatalf("e2..e5 is %d entries, want 4", s.Len())
	}
	for i := 2; i <= 5; i++ {
		if !s.Contains(asset.ID("e" + strconv.Itoa(i))) {
			t.Errorf("e%d is missing from the range", i)
		}
	}
	// Backwards from the same anchor.
	s.SelectRange(c, "e0")
	if s.Len() != 3 || !s.Contains("e0") || !s.Contains("e2") || s.Contains("e3") {
		t.Errorf("a backwards range from e2 to e0 is %d entries", s.Len())
	}
}

func TestSelectionCursorAndAnchorAreSeparate(t *testing.T) {
	s := asset.NewSelection()
	s.Only("e1")
	if s.Anchor() != "e1" {
		t.Fatalf("anchor %q", s.Anchor())
	}
	s.SetCursor("e4", true) // keep the anchor: this is a shift-move
	if s.Cursor() != "e4" || s.Anchor() != "e1" {
		t.Errorf("cursor %q anchor %q, want e4 and e1", s.Cursor(), s.Anchor())
	}
	s.SetCursor("e6", false)
	if s.Anchor() != "e6" {
		t.Errorf("a plain move left the anchor at %q", s.Anchor())
	}
}

func TestSelectionVersionMovesOnlyOnChange(t *testing.T) {
	s := asset.NewSelection()
	v := s.Version()
	if s.Set("x", false) || s.Version() != v {
		t.Error("deselecting something that was not selected moved the version")
	}
	if !s.Set("x", true) || s.Version() == v {
		t.Error("selecting did not move the version")
	}
	v = s.Version()
	if s.Set("x", true) || s.Version() != v {
		t.Error("selecting the same entry twice moved the version")
	}
	if s.Set("", true) {
		t.Error("the empty ID was selected")
	}
}

func TestSelectionPruneDropsWhatIsGone(t *testing.T) {
	c := asset.NewCollection(entries(4))
	s := asset.NewSelection()
	s.Only("e2")
	s.Set("e3", true)
	c.Reset(entries(3)) // e3 is gone
	if !s.Prune(c) {
		t.Error("Prune reported no change although e3 is gone")
	}
	if s.Contains("e3") || !s.Contains("e2") {
		t.Errorf("after Prune: e3 %v, e2 %v", s.Contains("e3"), s.Contains("e2"))
	}
	if s.Prune(c) {
		t.Error("a second Prune reported a change")
	}
}

func BenchmarkCollectionFind(b *testing.B) {
	c := asset.NewCollection(entries(100000))
	c.Find("e0") // build the index outside the loop
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		c.Find(asset.ID("e" + strconv.Itoa(i%100000)))
	}
}
