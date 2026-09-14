package ui_test

import (
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// --- virtualisation ----------------------------------------------------------

// TestGalleryLiveNodesAreBoundedByTheViewport is the central claim of the
// project plan, section 10: "Es braucht keinen State und keinen View pro
// Katalogeintrag."
//
// It is asserted as a number that does not move: the live node count of a
// hundred entry gallery and of a hundred thousand entry gallery are the same,
// and both are a few dozen. A gallery that mounted a node per entry would
// report 100 017 here, and would have taken a second to do it.
func TestGalleryLiveNodesAreBoundedByTheViewport(t *testing.T) {
	type result struct {
		nodes  uint64
		scopes uint64
		slots  int
	}
	got := map[int]result{}
	for _, n := range []int{100, 10000, 100000} {
		g := ui.NewGallery(asset.NewCollection(synth(n)))
		h := gifttest.New(t, gifttest.Options{
			View: ui.VStack(ui.ImageGallery(g).
				Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).
				Flex(1).Key("gallery")),
			Size: geom.Sz(800, 600),
		})
		// Scroll around, so that the number is a steady state and not the
		// count of a gallery that never had to recycle anything.
		node := h.Find(gifttest.ByKey("gallery"))
		for _, off := range []float64{0, 5000, 1e6, 250, 1e9} {
			node.ScrollTo(off)
		}
		d := h.Diagnostics()
		got[n] = result{nodes: d.LiveNodes, scopes: d.LiveScopes, slots: g.SlotCount()}
		t.Logf("n=%d live nodes=%d scopes=%d slots=%d visible=%d",
			n, d.LiveNodes, d.LiveScopes, g.SlotCount(), g.VisibleCount())
	}
	base := got[100]
	for n, r := range got {
		if r.nodes != base.nodes {
			t.Errorf("n=%d has %d live nodes, n=100 has %d; the node count must not depend on the catalogue size",
				n, r.nodes, base.nodes)
		}
		if r.scopes != base.scopes {
			t.Errorf("n=%d has %d live scopes, n=100 has %d", n, r.scopes, base.scopes)
		}
		// A 600 pixel viewport of three columns holds about nine tiles. Fifty
		// is generous headroom and still two thousand times less than 100 000.
		if r.slots > 50 {
			t.Errorf("n=%d has a pool of %d slots for a 800x600 viewport; that is not a bound", n, r.slots)
		}
	}
}

// TestGalleryScrollPathCost measures what scrolling one viewport costs, and
// reports it rather than only asserting on it.
//
// The claim of the project plan, section 12, step 3, is "der reine Scrollpfad
// ist unabhaengig von N abgesehen von der Indexsuche". The numbers that show
// it are: zero builds, a layout count that is the marked path plus the tiles
// whose size changed, and identical counts at 100 and at 100 000 entries.
func TestGalleryScrollPathCost(t *testing.T) {
	type cost struct{ builds, layouts, frames uint64 }
	got := map[int]cost{}
	pools := map[int]int{}
	for _, n := range []int{100, 100000} {
		g := ui.NewGallery(asset.NewCollection(synth(n)))
		h := gifttest.New(t, gifttest.Options{
			View: ui.VStack(ui.ImageGallery(g).
				Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).
				Flex(1).Key("gallery")),
			Size: geom.Sz(800, 600),
		})
		node := h.Find(gifttest.ByKey("gallery"))
		// Warm up: let the pool stop growing before measuring.
		for range 6 {
			node.ScrollBy(600)
		}
		before := h.Diagnostics()
		node.ScrollBy(600)
		after := h.Diagnostics()
		got[n] = cost{
			builds:  after.Builds - before.Builds,
			layouts: after.Layouts - before.Layouts,
			frames:  after.Frames - before.Frames,
		}
		pools[n] = g.SlotCount()
		t.Logf("n=%d: one viewport scroll cost %d builds, %d layouts over %d frames (pool %d, visible %d)",
			n, got[n].builds, got[n].layouts, got[n].frames, g.SlotCount(), g.VisibleCount())
	}
	for n, c := range got {
		if c.builds != 0 {
			t.Errorf("n=%d: scrolling one viewport rebuilt %d scopes; the scroll path must not build", n, c.builds)
		}
		// An absolute bound, not only an N-independent one. A gallery running
		// ten thousand layouters per scroll would satisfy "the same number at
		// both sizes" and would still be unusable, and this test asserted
		// exactly that and no more until WU-O. The bound is the one
		// [gift.ScrollSpec.Virtual] documents: one layouter per tile in the
		// pool, plus the container, plus the depth of the marked path. Eight
		// is generous headroom over the two-deep tree here.
		limit := uint64(pools[n] + 8)
		if c.layouts > limit {
			t.Errorf("n=%d: scrolling one viewport ran %d layouters for a pool of %d; the bound "+
				"is one per tile plus the marked path, that is %d", n, c.layouts, pools[n], limit)
		}
	}
	if got[100].layouts != got[100000].layouts {
		t.Errorf("one viewport costs %d layouts at n=100 and %d at n=100000; the scroll path depends on N",
			got[100].layouts, got[100000].layouts)
	}
}

// --- recycling ---------------------------------------------------------------

// TestGalleryTileCarriesNothingOfItsPreviousItem is the test the project plan,
// section 13, names: "Keine falschen Bilder nach Tile-/Slot-Recycling und
// Quellenrevisionen."
//
// There are no pictures yet, so the equivalent is the identity: after a jump
// that recycles every tile, no slot may still report the ID, the revision or
// the request generation of what it held before.
func TestGalleryTileCarriesNothingOfItsPreviousItem(t *testing.T) {
	entries := synth(20000)
	// Give the two items under test distinguishable revisions, because a
	// revision that is equal everywhere cannot show that it was replaced.
	entries[5].Revision = "rev-five"
	entries[5000].Revision = "rev-five-thousand"
	g := ui.NewGallery(asset.NewCollection(entries))
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).
			Flex(1).Key("gallery")),
		Size: geom.Sz(800, 600),
	})
	node := h.Find(gifttest.ByKey("gallery"))

	// Put item 5 on screen and find the slot it landed in.
	revealItem(t, h, g, node, 5)
	before, ok := g.BindingOf("img-5")
	if !ok {
		t.Fatalf("item 5 has no tile after revealing it: %v", g)
	}
	if before.Revision != "rev-five" {
		t.Errorf("tile of item 5 carries revision %q, want %q", before.Revision, "rev-five")
	}
	slot := before.Slot

	// Jump far away, then to item 5000, and look at the same slot.
	revealItem(t, h, g, node, 5000)
	after, ok := g.BindingOf("img-5000")
	if !ok {
		t.Fatalf("item 5000 has no tile after revealing it: %v", g)
	}

	// The interesting case only exists if the slot really was reused.
	var reused ui.TileBinding
	var bs []ui.TileBinding
	for _, b := range g.Bindings(bs) {
		if b.Slot == slot {
			reused = b
		}
	}
	if reused.ID == "" {
		t.Fatalf("slot %d is unbound after the jump; the pool did not recycle", slot)
	}
	if reused.ID == before.ID {
		t.Fatalf("slot %d still holds %q after jumping 5000 entries away", slot, reused.ID)
	}
	if reused.Revision == before.Revision {
		t.Errorf("slot %d kept the revision %q of its previous item", slot, reused.Revision)
	}
	if reused.Generation <= before.Generation {
		t.Errorf("slot %d has request generation %d, which is not newer than the %d it had "+
			"as item %d; a stale request could not be told apart from a current one",
			slot, reused.Generation, before.Generation, before.Item)
	}
	// And the identity is the one the catalogue says, not the one the slot
	// used to have.
	want := asset.ID("img-" + strconv.Itoa(reused.Item))
	if reused.ID != want {
		t.Errorf("slot %d shows ID %q for item %d, want %q", slot, reused.ID, reused.Item, want)
	}
	if after.ID != "img-5000" || after.Revision != "rev-five-thousand" {
		t.Errorf("item 5000 is in slot %d as %q/%q", after.Slot, after.ID, after.Revision)
	}
}

// TestGalleryTileKeepsItsItemWhenNothingMoved is the other half of the
// recycling contract: a tile whose item is still visible must not be rebound.
// Without it, "no wrong pictures" could be satisfied by rebinding everything
// every frame, which would throw away every cached decode in step 4.
func TestGalleryTileKeepsItsItemWhenNothingMoved(t *testing.T) {
	h, g := galleryFixture(t, 10000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	node.ScrollTo(4000)
	var before []ui.TileBinding
	before = g.Bindings(before)
	gen := g.Generation()

	node.ScrollBy(1) // one pixel: nothing enters or leaves
	if g.Generation() != gen {
		t.Errorf("a one pixel scroll rebound %d tiles", g.Generation()-gen)
	}
	var after []ui.TileBinding
	after = g.Bindings(after)
	if len(after) != len(before) {
		t.Fatalf("%d tiles before, %d after a one pixel scroll", len(before), len(after))
	}
	for i := range before {
		if before[i].Slot != after[i].Slot || before[i].ID != after[i].ID {
			t.Errorf("tile %d moved from slot %d/%q to slot %d/%q",
				i, before[i].Slot, before[i].ID, after[i].Slot, after[i].ID)
		}
		// The actual claim, which the clause this replaces never made. It
		// read `before[i].DocY == after[i].DocY && after[i].DocY == 0`, which
		// is dead: the test scrolls to 4000 first, so no tile is at document
		// y zero and the second conjunct is never true. Nothing asserted that
		// the document position was unchanged — and "nothing moved" is
		// exactly what the test is named after. A one pixel scroll moves the
		// viewport, not the document, so every tile must report the same
		// document rectangle it did before.
		if before[i].DocY != after[i].DocY || before[i].DocX != after[i].DocX ||
			before[i].DocW != after[i].DocW || before[i].DocH != after[i].DocH {
			t.Errorf("tile %d of %q moved in the document from (%.3f,%.3f %.3fx%.3f) to "+
				"(%.3f,%.3f %.3fx%.3f) across a one pixel scroll; the viewport moved, not the content",
				i, before[i].ID,
				before[i].DocX, before[i].DocY, before[i].DocW, before[i].DocH,
				after[i].DocX, after[i].DocY, after[i].DocW, after[i].DocH)
		}
		if before[i].DocH <= 0 {
			t.Errorf("tile %d of %q has an empty document rectangle", i, before[i].ID)
		}
		if before[i].Generation != after[i].Generation {
			t.Errorf("tile %d of %q moved from generation %d to %d without changing item",
				i, before[i].ID, before[i].Generation, after[i].Generation)
		}
	}
}

// --- the scroll anchor -------------------------------------------------------

// TestGalleryAnchorSurvivesReflow covers the three events the project plan,
// section 10, requires the anchor to survive: a resize, a layout switch and a
// batch of corrections.
//
// The assertion is the same in all three: the entry the viewport was anchored
// to before the reflow still has a tile after it. That is the honest form of
// "the viewport stayed on the same picture"; asserting on the offset would be
// asserting on arithmetic that legitimately produces a different number when
// every item has moved.
func TestGalleryAnchorSurvivesReflow(t *testing.T) {
	t.Run("resize", func(t *testing.T) {
		h, g := galleryFixture(t, 30000, ui.Masonry().MinColumnWidth(240).Gap(8))
		id := scrollAndAnchor(t, h, g, 90000)
		h.Resize(geom.Sz(1200, 700))
		assertAnchored(t, h, g, id, "a resize from 800x600 to 1200x700")
	})

	t.Run("layout switch", func(t *testing.T) {
		// The layout comes from state, because switching it at runtime is an
		// ordinary rebuild with a different declaration — which is exactly
		// how an application does it.
		g := ui.NewGallery(asset.NewCollection(synth(30000)))
		var mode *gift.State[bool]
		h := gifttest.New(t, gifttest.Options{
			Root: func(ctx *gift.Context) gift.View {
				mode = ctx.State("justified", false)
				l := ui.Masonry().MinColumnWidth(240).Gap(8)
				if ctx.Read(mode) {
					l = ui.Justified().RowHeight(180).Gap(8)
				}
				return ui.VStack(ui.ImageGallery(g).Layout(l).Flex(1).Key("gallery"))
			},
			Size: geom.Sz(800, 600),
		})
		id := scrollAndAnchor(t, h, g, 90000)
		mode.Set(true)
		h.Settle()
		if g.Columns() != 0 {
			t.Fatalf("the layout did not switch: still %d masonry columns", g.Columns())
		}
		assertAnchored(t, h, g, id, "a switch from masonry to justified")
	})

	t.Run("corrections", func(t *testing.T) {
		h, g := galleryFixture(t, 30000, ui.Masonry().MinColumnWidth(240).Gap(8))
		id := scrollAndAnchor(t, h, g, 90000)
		extentBefore := g.ContentExtent()

		// Every eleventh entry has no dimensions; give them all real ones in
		// one batch, which is what the project plan, section 10, means by
		// "Korrekturen werden gebuendelt".
		var batch []asset.Correction
		for i := 0; i < g.Collection().Len(); i += 11 {
			batch = append(batch, asset.Correction{
				ID:       asset.ID("img-" + strconv.Itoa(i)),
				Width:    1600,
				Height:   500,
				Revision: "r2",
			})
		}
		if n := g.ApplyCorrections(batch); n != len(batch) {
			t.Fatalf("%d of %d corrections applied", n, len(batch))
		}
		h.Settle()
		if g.ContentExtent() == extentBefore {
			t.Fatalf("the correction batch did not reflow: extent is still %.0f", extentBefore)
		}
		assertAnchored(t, h, g, id, "a batch of 2728 corrections")
		// The corrected entries are no longer provisional.
		if b, ok := g.BindingOf("img-11"); ok && b.Provisional {
			t.Errorf("img-11 is still laid out provisionally after its correction")
		}
	})
}

// TestGalleryProvisionalDimensions checks the state a cold gallery is in: a
// catalogue with no dimensions at all is laid out anyway, is marked as
// provisional, and the marking goes away when the facts arrive.
func TestGalleryProvisionalDimensions(t *testing.T) {
	items := make([]asset.Metadata, 4000)
	for i := range items {
		items[i] = asset.Metadata{ID: asset.ID("p" + strconv.Itoa(i))}
	}
	g := ui.NewGallery(asset.NewCollection(items))
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(240).Gap(8).ProvisionalAspect(1.5)).
			Flex(1).Key("gallery")),
		Size: geom.Sz(800, 600),
	})
	if !g.Ready() || g.VisibleCount() == 0 {
		t.Fatalf("a catalogue with no known dimensions produced no layout: %v", g)
	}
	var bs []ui.TileBinding
	for _, b := range g.Bindings(bs) {
		if !b.Provisional {
			t.Errorf("tile of %q is not marked provisional although nothing is known about it", b.ID)
		}
		// Aspect 1.5 on a 261 pixel column is a 174 pixel tile.
		if got := b.DocW / b.DocH; got < 1.4 || got > 1.6 {
			t.Errorf("tile of %q has aspect %.2f, want the provisional 1.5", b.ID, got)
		}
	}

	id := scrollAndAnchor(t, h, g, 3000)
	var batch []asset.Correction
	for i := range 4000 {
		batch = append(batch, asset.Correction{
			ID: asset.ID("p" + strconv.Itoa(i)), Width: 600, Height: 900, Revision: "probed",
		})
	}
	g.ApplyCorrections(batch)
	h.Settle()
	assertAnchored(t, h, g, id, "a full correction batch over a provisional catalogue")
	for _, b := range g.Bindings(bs[:0]) {
		if b.Provisional {
			t.Errorf("tile of %q is still provisional after its correction", b.ID)
		}
		if b.Revision != "probed" {
			t.Errorf("tile of %q carries revision %q, want %q", b.ID, b.Revision, "probed")
		}
	}
}

// --- jumps -------------------------------------------------------------------

// TestGalleryJumps covers the fourth row of the target hardware table in the
// project plan, section 13: "Schnelle Spruenge und Richtungswechsel".
//
// Every jump is checked against the index itself rather than against a
// remembered answer: whatever is bound must be inside the viewport interval,
// and everything inside that interval must be bound. That is the invariant;
// the individual offsets are just ways of reaching interesting states.
func TestGalleryJumps(t *testing.T) {
	h, g := galleryFixture(t, 100000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	info := node.ScrollInfo()
	maxOff := info.MaxOffset
	if maxOff < 1e6 {
		t.Fatalf("a 100 000 entry gallery has a max offset of only %.0f", maxOff)
	}

	offsets := []float64{0, maxOff, maxOff * 2, -1000, maxOff / 2, 0, maxOff - 1, 17.5}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 40 {
		offsets = append(offsets, rng.Float64()*maxOff*1.2-maxOff*0.1)
	}
	for i, off := range offsets {
		node.ScrollTo(off)
		checkViewportConsistent(t, h, g, node, i, off)
	}

	// Jump to the end and past it: the offset clamps and the last entry is on
	// screen.
	node.ScrollTo(maxOff * 10)
	node.AssertAtScrollEnd()
	if _, ok := g.BindingOf(asset.ID("img-" + strconv.Itoa(g.Collection().Len()-1))); !ok {
		// The very last entry need not be in the last column, but something
		// from the final hundred must be.
		last := 0
		var bs []ui.TileBinding
		for _, b := range g.Bindings(bs) {
			if b.Item > last {
				last = b.Item
			}
		}
		if last < g.Collection().Len()-200 {
			t.Errorf("at the end of the document the highest visible item is %d of %d",
				last, g.Collection().Len())
		}
	}

	// And a reversal straight back to the top.
	node.ScrollTo(0)
	node.AssertAtScrollStart()
	if b, ok := g.BindingOf("img-0"); !ok || b.DocY != 0 {
		t.Errorf("after returning to the top, img-0 is %v/%v", b, ok)
	}
}

// checkViewportConsistent asserts the bound set is exactly the visible set.
func checkViewportConsistent(t *testing.T, h *gifttest.Harness, g *ui.Gallery, node gifttest.Node, step int, asked float64) {
	t.Helper()
	off := node.ScrollOffset()
	vh := float64(node.ScrollInfo().ViewportExtent)
	var bs []ui.TileBinding
	bs = g.Bindings(bs)
	if len(bs) != g.VisibleCount() {
		t.Fatalf("step %d (asked %.0f, at %.0f): %d tiles for %d visible items\n%v",
			step, asked, off, len(bs), g.VisibleCount(), g)
	}
	for _, b := range bs {
		if b.DocY+b.DocH <= off || b.DocY >= off+vh {
			t.Errorf("step %d (at %.0f, viewport %.0f): tile of %q is at y=%.1f..%.1f, outside the viewport",
				step, off, vh, b.ID, b.DocY, b.DocY+b.DocH)
		}
		x, y, w, hh, ok := g.ItemRect(b.Item)
		if !ok || x != b.DocX || y != b.DocY || w != b.DocW || hh != b.DocH {
			t.Errorf("step %d: tile of %q says %v, the index says %v %v %v %v (%v)",
				step, b.ID, b, x, y, w, hh, ok)
		}
	}
	_ = h
}

// --- selection and the keyboard ----------------------------------------------

// TestGallerySelectionSurvivesScrollingOutOfView is the project plan,
// section 5: durable selection lives with the collection owner. A selection
// kept in a tile would be gone the moment the tile was recycled, which is
// about four hundred pixels of scrolling.
func TestGallerySelectionSurvivesScrollingOutOfView(t *testing.T) {
	h, g := galleryFixture(t, 50000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))

	g.Selection().Only("img-3")
	if !g.Selection().Contains("img-3") {
		t.Fatal("img-3 is not selected right after selecting it")
	}
	node.ScrollTo(400000)
	h.Settle()
	if _, ok := g.BindingOf("img-3"); ok {
		t.Fatal("img-3 still has a tile after scrolling 400 000 pixels away")
	}
	if !g.Selection().Contains("img-3") {
		t.Error("the selection was lost when the tile was recycled")
	}
	node.ScrollTo(0)
	h.Settle()
	if _, ok := g.BindingOf("img-3"); !ok {
		t.Fatal("img-3 has no tile after scrolling back")
	}
	if !g.Selection().Contains("img-3") {
		t.Error("img-3 is no longer selected after scrolling back to it")
	}
	if g.Selection().Len() != 1 {
		t.Errorf("%d entries are selected, want 1", g.Selection().Len())
	}
}

// TestGalleryKeyboardFollowsCatalogueOrder is the other half of the same rule
// in the project plan, section 10: navigation follows the catalogue order and
// not the arrangement of the tiles.
//
// The distinction is measurable in masonry, which is why the test is written
// there: the tile visually above item 7 is item 4 only by coincidence of the
// column heights, whereas one line up in catalogue order is always three
// entries in a three column layout.
func TestGalleryKeyboardFollowsCatalogueOrder(t *testing.T) {
	h, g := galleryFixture(t, 20000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	// Focus by tabbing, not by clicking: a click at the centre of the gallery
	// lands on a tile, and gifttest rightly refuses to pretend otherwise.
	node.Focus()
	h.AssertFocus(gifttest.ByKey("gallery"))
	sel := g.Selection()

	h.Key(gift.KeyHome)
	if got := sel.Cursor(); got != "img-0" {
		t.Fatalf("Home put the cursor on %q, want img-0", got)
	}
	if g.Columns() != 3 {
		t.Fatalf("this test assumes three columns, got %d", g.Columns())
	}

	h.Key(gift.KeyRight)
	h.Key(gift.KeyRight)
	if got := sel.Cursor(); got != "img-2" {
		t.Errorf("two rights from img-0 land on %q, want img-2", got)
	}
	h.Key(gift.KeyDown)
	if got := sel.Cursor(); got != "img-5" {
		t.Errorf("one down from img-2 lands on %q, want img-5 (one line of three)", got)
	}
	h.Key(gift.KeyUp)
	if got := sel.Cursor(); got != "img-2" {
		t.Errorf("one up from img-5 lands on %q, want img-2", got)
	}
	// A plain move replaces the selection.
	if sel.Len() != 1 || !sel.Contains("img-2") {
		t.Errorf("after plain arrow keys the selection is %d entries, want only img-2", sel.Len())
	}
	// Shift extends from the anchor, in catalogue order.
	h.Key(gift.KeyDown, gift.ModShift)
	if sel.Len() != 4 {
		t.Errorf("shift-down from img-2 selected %d entries, want 4 (img-2..img-5)", sel.Len())
	}
	for i := 2; i <= 5; i++ {
		if !sel.Contains(asset.ID("img-" + strconv.Itoa(i))) {
			t.Errorf("img-%d is not in the shift extended range", i)
		}
	}

	// End goes to the last entry and scrolls it into view, which is the
	// placeholder path of the project plan, section 10: a jump shows tiles
	// immediately rather than waiting for anything.
	h.Key(gift.KeyEnd)
	last := asset.ID("img-" + strconv.Itoa(g.Collection().Len()-1))
	if got := sel.Cursor(); got != last {
		t.Fatalf("End put the cursor on %q, want %q", got, last)
	}
	if _, ok := g.BindingOf(last); !ok {
		t.Errorf("End did not bring the last entry into view: %v", g)
	}
	if off := node.ScrollOffset(); off < 1e6 {
		t.Errorf("End left the viewport at %.0f", off)
	}
}

// TestGalleryClickSelects checks the pointer half, and that it costs a repaint
// and not a rebuild.
func TestGalleryClickSelects(t *testing.T) {
	h, g := galleryFixture(t, 5000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	var picked []asset.ID
	_ = picked

	var bs []ui.TileBinding
	bs = g.Bindings(bs)
	if len(bs) < 2 {
		t.Fatalf("not enough tiles to click: %v", g)
	}
	// Click in the middle of the first tile's document rectangle, converted
	// to device space by hand: the gallery sits at the origin of an 800x600
	// window and its content origin is its scroll offset, which is zero.
	target := bs[0]
	p := geom.Pt(float32(target.DocX+target.DocW/2), float32(target.DocY+target.DocH/2))

	before := h.Diagnostics()
	h.ClickAt(p)
	after := h.Diagnostics()

	if !g.Selection().Contains(target.ID) {
		t.Errorf("clicking the tile of %q at %v did not select it (selection: %d)\n%s",
			target.ID, p, g.Selection().Len(), h.Dump())
	}
	if after.Builds != before.Builds {
		t.Errorf("a click rebuilt %d scopes; selection is presentation state read live by the tile",
			after.Builds-before.Builds)
	}
	_ = node
}

// --- helpers -----------------------------------------------------------------

// revealItem scrolls until the given catalogue entry has a tile.
func revealItem(t *testing.T, h *gifttest.Harness, g *ui.Gallery, node gifttest.Node, item int) {
	t.Helper()
	_, y, _, _, ok := g.ItemRect(item)
	if !ok {
		t.Fatalf("item %d has no rectangle: %v", item, g)
	}
	node.ScrollTo(y)
	h.Settle()
	if _, ok := g.BindingOf(g.Collection().ID(item)); !ok {
		t.Fatalf("item %d has no tile after scrolling to %.0f: %v\n%s", item, y, g, h.Dump())
	}
}

// scrollAndAnchor scrolls to off, settles and returns the ID the gallery
// anchored itself to.
func scrollAndAnchor(t *testing.T, h *gifttest.Harness, g *ui.Gallery, off float64) asset.ID {
	t.Helper()
	h.Find(gifttest.ByKey("gallery")).ScrollTo(off)
	h.Settle()
	id, _, ok := g.Anchor()
	if !ok {
		t.Fatalf("no anchor after scrolling to %.0f: %v", off, g)
	}
	return id
}

// assertAnchored fails unless the entry the viewport was pinned to is back
// under the top edge of the viewport.
//
// # Why this asserts a position and not merely a tile
//
// Because "it still has a tile" is barely stronger than "it is somewhere on
// screen": with an [ui.GalleryView.Overscan] of zero a tile exists exactly for
// the items intersecting the viewport, so the old form of this helper passed
// for an anchored entry that had slid most of a viewport away. Measured on the
// masonry to justified switch, which is the reflow that moves the most: the
// anchored item ended up 175 pixels from where it had been, on a 600 pixel
// viewport, and the test was green.
//
// # The tolerance, and where it comes from
//
// It is not a number chosen to make the test pass. [ui.Gallery.takeAnchor]
// clamps the local offset into the anchored item — "this many pixels into that
// item" rather than "this many pixels into the document from its top" — and
// the restore clamps it again against the item's new height. The documented
// consequence is therefore exact: the top edge of the viewport lands somewhere
// inside the anchored item. So that is what is asserted, with the two
// legitimate exceptions where the document clamp overrides the anchor — the
// very top and the very end of the document, which cannot be scrolled past.
func assertAnchored(t *testing.T, h *gifttest.Harness, g *ui.Gallery, id asset.ID, what string) {
	t.Helper()
	h.Settle()
	node := h.Find(gifttest.ByKey("gallery"))
	off := node.ScrollOffset()
	i, found := g.Collection().Find(id)
	if !found {
		t.Fatalf("after %s the anchored ID %q is no longer in the catalogue", what, id)
	}
	_, y, _, hh, ok := g.ItemRect(i)
	if !ok {
		t.Fatalf("after %s the anchored entry %q has no rectangle: %v", what, id, g)
	}
	if _, ok := g.BindingOf(id); !ok {
		t.Errorf("after %s the anchored entry %q (item %d, y=%.1f..%.1f) is no longer on screen; "+
			"the viewport is at %.1f\n%v", what, id, i, y, y+hh, off, g)
		return
	}
	// The clamp of takeAnchor: the viewport top is inside the anchored item.
	lo, hi := y, y+max(hh-1, 0)
	info := node.ScrollInfo()
	const eps = 0.5
	atTop := off <= eps
	atEnd := off >= info.MaxOffset-eps
	if off < lo-eps || off > hi+eps {
		if atTop || atEnd {
			// The document clamp wins over the anchor at both ends, and has
			// to: there is nothing above zero and nothing below MaxOffset.
			return
		}
		t.Errorf("after %s the viewport is at %.1f but the anchored entry %q (item %d) covers "+
			"%.1f..%.1f; the anchor is supposed to leave the top edge inside that item, and it "+
			"drifted %.1f pixels out of it on a %.0f pixel viewport\n%v",
			what, off, id, i, lo, hi, max(lo-off, off-hi), float64(info.ViewportExtent), g)
	}
}

// TestGalleryTilesAreUnkeyed pins the reconciliation bound.
//
// gift matches keyed siblings by searching the previous child list per child,
// which is quadratic in the number of siblings; its own documentation says
// "that is the reason virtualised collections do not go through this path".
// The gallery stays off it by giving its tiles no key at all: unkeyed
// siblings are matched by position, which is one comparison per child, and the
// tile pool is positional by construction because the slot *is* the position.
//
// The bound is therefore the pool size, which
// TestGalleryLiveNodesAreBoundedByTheViewport shows is a function of the
// viewport. At 800x600 it is fourteen. The catalogue size never enters it, and
// the only rebuild that touches the child list at all is the one that resizes
// the pool.
func TestGalleryTilesAreUnkeyed(t *testing.T) {
	h, g := galleryFixture(t, 100000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	kids := node.Children()
	if len(kids) != g.SlotCount() {
		t.Fatalf("%d child nodes for %d slots", len(kids), g.SlotCount())
	}
	if len(kids) > 50 {
		t.Errorf("the gallery has %d children; the keyed reconciliation path is quadratic "+
			"and this is the number that bounds it", len(kids))
	}
	for i, k := range kids {
		if key := k.Key(); key != "" {
			t.Errorf("tile %d has the key %q; a keyed sibling list puts the gallery on gift's "+
				"quadratic reconciliation path", i, key)
		}
	}
}

// TestGalleryDiscardsStaleReflows is the project plan, section 10:
// "Ergebnisse veralteter Layouts werden verworfen."
//
// Twenty intermediate widths — a window drag — must commit one layout, not
// twenty, and the one that is committed must be the one for the final width.
func TestGalleryDiscardsStaleReflows(t *testing.T) {
	// Driven frame by frame on a raw App, because the point is what happens
	// *during* a window drag and the harness settles after every resize.
	g := ui.NewGallery(asset.NewCollection(synth(100000)))
	w := float32(800)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).Flex(1)).
			Frame(geom.Unbounded(), geom.Unbounded())
	}})
	frame := func() {
		if err := a.Update(geom.Sz(w, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 8 {
		frame()
	}
	start := g.LayoutVersion()

	// Twenty widths, one frame each: a window drag. A 100 000 entry reflow
	// takes several passes at the default budget, so none of these twenty can
	// finish before the next one replaces it.
	for i := range 20 {
		w = float32(800 + i)
		frame()
	}
	if mid := g.LayoutVersion() - start; mid != 0 {
		t.Errorf("%d layouts were committed during the drag; a reflow that is superseded "+
			"before it finishes must be discarded, not completed", mid)
	}
	if !g.Reflowing() {
		t.Fatal("no reflow is in flight after the drag")
	}

	// Now let the last one finish.
	for range 32 {
		frame()
		if !g.Reflowing() {
			break
		}
	}
	if committed := g.LayoutVersion() - start; committed != 1 {
		t.Errorf("twenty widths committed %d layouts, want exactly 1", committed)
	}
	// And it is the layout for the final width, 819.
	var bs []ui.TileBinding
	bs = g.Bindings(bs)
	if len(bs) == 0 {
		t.Fatal("nothing visible after the drag")
	}
	want := (819.0 - 2*8) / 3
	if got := bs[0].DocW; got < want-0.5 || got > want+0.5 {
		t.Errorf("a tile is %.2f wide, want %.2f: the committed layout is not the one for the final width",
			got, want)
	}
}
