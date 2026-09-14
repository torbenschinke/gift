package ui_test

import (
	"strconv"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// synth builds n synthetic catalogue entries, deterministically.
//
// The project plan, section 12, step 3, asks for "100.000 synthetische
// Metadatensaetze". Deterministic, because every assertion below about a
// position, an extent or an anchor is only reproducible if the aspect ratios
// are; and with a few unknown dimensions mixed in, because the provisional
// path of section 10 is not an edge case but the state a cold gallery starts
// in.
func synth(n int) []asset.Metadata {
	out := make([]asset.Metadata, n)
	for i := range n {
		m := asset.Metadata{
			ID:       asset.ID("img-" + strconv.Itoa(i)),
			Revision: "r1",
			MIMEType: "image/jpeg",
		}
		// Every eleventh entry has no dimensions yet.
		if i%11 != 0 {
			w := 400 + (i*37)%800
			h := 300 + (i*53)%700
			m.Width, m.Height = uint32(w), uint32(h)
		}
		out[i] = m
	}
	return out
}

// galleryFixture mounts a gallery of n entries in an 800x600 window and
// returns the harness and the retained model.
func galleryFixture(t *testing.T, n int, l ui.GalleryLayout) (*gifttest.Harness, *ui.Gallery) {
	t.Helper()
	g := ui.NewGallery(asset.NewCollection(synth(n)))
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.ImageGallery(g).Layout(l).Flex(1).Key("gallery"),
		),
		Size: geom.Sz(800, 600),
	})
	return h, g
}

func TestGalleryMountsAndShowsTiles(t *testing.T) {
	h, g := galleryFixture(t, 1000, ui.Masonry().MinColumnWidth(240).Gap(8))
	if !g.Ready() {
		t.Fatalf("gallery has no committed layout after settling\n%s", h.Dump())
	}
	if g.Columns() != 3 {
		t.Errorf("columns = %d, want 3 for 800 px and a 240 px minimum", g.Columns())
	}
	if g.VisibleCount() == 0 {
		t.Fatalf("nothing visible: %v\n%s", g, h.Dump())
	}
	if g.SlotCount() < g.VisibleCount() {
		t.Errorf("pool of %d slots is smaller than the %d visible items",
			g.SlotCount(), g.VisibleCount())
	}
	var bs []ui.TileBinding
	bs = g.Bindings(bs)
	if len(bs) != g.VisibleCount() {
		t.Errorf("%d bound tiles for %d visible items", len(bs), g.VisibleCount())
	}
	for _, b := range bs {
		if want := asset.ID("img-" + strconv.Itoa(b.Item)); b.ID != want {
			t.Errorf("slot %d: ID %q for item %d, want %q", b.Slot, b.ID, b.Item, want)
		}
	}
	if ext := g.ContentExtent(); ext < 10000 {
		t.Errorf("content extent %.0f looks too small for 1000 items", ext)
	}
}

func TestGalleryScrolls(t *testing.T) {
	h, g := galleryFixture(t, 5000, ui.Masonry().MinColumnWidth(240).Gap(8))
	n := h.Find(gifttest.ByKey("gallery"))
	first := firstItem(g)
	n.ScrollTo(20000)
	h.Settle()
	if got := firstItem(g); got <= first {
		t.Errorf("after scrolling to 20000 the first visible item is %d, was %d", got, first)
	}
	// And back.
	n.ScrollTo(0)
	h.Settle()
	if got := firstItem(g); got != first {
		t.Errorf("after scrolling back the first visible item is %d, want %d", got, first)
	}
}

func firstItem(g *ui.Gallery) int {
	best := -1
	var bs []ui.TileBinding
	for _, b := range g.Bindings(bs) {
		if best < 0 || b.Item < best {
			best = b.Item
		}
	}
	return best
}

func TestGalleryJustifiedFillsRows(t *testing.T) {
	_, g := galleryFixture(t, 2000, ui.Justified().RowHeight(160).Gap(6))
	if g.Columns() != 0 {
		t.Errorf("a justified layout reports %d columns, want 0", g.Columns())
	}
	var bs []ui.TileBinding
	bs = g.Bindings(bs)
	if len(bs) == 0 {
		t.Fatalf("nothing visible: %v", g)
	}
	for _, b := range bs {
		if b.DocW <= 0 || b.DocH <= 0 {
			t.Errorf("item %d has an empty rect %v", b.Item, b)
		}
	}
}

func TestGalleryDiagnosticsAreQuietWhenIdle(t *testing.T) {
	h, _ := galleryFixture(t, 10000, ui.Masonry())
	before := h.Diagnostics()
	h.Frame()
	after := h.Diagnostics()
	if after.Builds != before.Builds {
		t.Errorf("an idle frame rebuilt %d scopes", after.Builds-before.Builds)
	}
	if after.Layouts != before.Layouts {
		t.Errorf("an idle frame ran %d layouters", after.Layouts-before.Layouts)
	}
}

var _ = gift.ScrollInteractor

// TestGalleryDegenerateCases covers the shapes that are not wrong but are
// easy to crash on: an empty catalogue, a catalogue that becomes empty, and a
// gallery with no bounded height.
func TestGalleryDegenerateCases(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		g := ui.NewGallery(asset.NewCollection(nil))
		h := gifttest.New(t, gifttest.Options{
			View: ui.VStack(ui.ImageGallery(g).Flex(1).Key("gallery")),
			Size: geom.Sz(800, 600),
		})
		if g.VisibleCount() != 0 || g.ContentExtent() != 0 {
			t.Errorf("an empty catalogue reports %d visible and an extent of %g",
				g.VisibleCount(), g.ContentExtent())
		}
		h.Find(gifttest.ByKey("gallery")).ScrollTo(1000).AssertAtScrollStart()
		h.AssertNoOverflow()

		// And it fills up without a remount.
		g.SetCollection(asset.NewCollection(synth(500)))
		h.Settle()
		if g.VisibleCount() == 0 {
			t.Errorf("the gallery stayed empty after the catalogue was filled: %v", g)
		}
	})

	t.Run("emptied", func(t *testing.T) {
		g := ui.NewGallery(asset.NewCollection(synth(500)))
		h := gifttest.New(t, gifttest.Options{
			View: ui.VStack(ui.ImageGallery(g).Flex(1).Key("gallery")),
			Size: geom.Sz(800, 600),
		})
		h.Find(gifttest.ByKey("gallery")).ScrollTo(5000)
		g.SetCollection(asset.NewCollection(nil))
		h.Settle()
		if g.VisibleCount() != 0 {
			t.Errorf("%d tiles survive an emptied catalogue", g.VisibleCount())
		}
		if len(g.Bindings(nil)) != 0 {
			t.Error("a tile is still bound to an entry that no longer exists")
		}
		h.Find(gifttest.ByKey("gallery")).AssertAtScrollStart()
	})

	t.Run("unbounded height", func(t *testing.T) {
		// A gallery in a stack with no Flex, Frame or MaxHeight: the stack
		// measures it with an unbounded main axis, so it has no viewport. It
		// must be blank and quiet, not a crash and not an infinite reflow.
		g := ui.NewGallery(asset.NewCollection(synth(500)))
		h := gifttest.New(t, gifttest.Options{
			View: ui.VStack(ui.ImageGallery(g).Key("gallery")),
			Size: geom.Sz(800, 600),
		})
		if g.VisibleCount() != 0 {
			t.Errorf("a gallery with no viewport shows %d tiles", g.VisibleCount())
		}
		h.AssertNoOverflow()
	})
}
