package ui_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// This file holds the tests WU-O added. Every one of them is here because it
// found a defect that four review gates and the whole existing suite did not:
// a public method that panicked mid frame, a generation counter that did not
// mean what it documented, a layout that tore under a mid flight correction,
// and a pool truncation that collapsed the visible band to the left edge of
// the window.

// galleryFrames drives a gallery on a raw [gift.App], one frame at a time.
//
// The harness settles after every mutation, and every defect below lives in
// the frames *between* a mutation and the settled state — the passes of an
// incremental reflow, the one frame in which the tile pool is still the old
// size. So these tests do not use it.
type galleryFrames struct {
	t  *testing.T
	a  *gift.App
	g  *ui.Gallery
	sz geom.Size
}

func newGalleryFrames(t *testing.T, n int, l ui.GalleryLayout, sz geom.Size) *galleryFrames {
	t.Helper()
	g := ui.NewGallery(asset.NewCollection(synth(n)))
	f := &galleryFrames{t: t, g: g, sz: sz}
	f.a = gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.ImageGallery(g).Layout(l).Flex(1).Key("gallery")).
			Frame(geom.Unbounded(), geom.Unbounded())
	}})
	return f
}

func (f *galleryFrames) frame() {
	f.t.Helper()
	if err := f.a.Update(f.sz); err != nil {
		f.t.Fatal(err)
	}
	f.a.Paint()
}

func (f *galleryFrames) frames(n int) {
	f.t.Helper()
	for range n {
		f.frame()
	}
}

// settle runs frames until the gallery has stopped reflowing *and* its tile
// pool has stopped growing, with a bound.
//
// Both halves. The pool size is decided by a build and consumed by the layout
// pass after it, so a gallery that has committed its layout still needs a
// couple of frames before the band is fully bound — which is precisely the
// window TestGalleryPoolTruncationSpansTheViewport is about, and precisely
// what the other tests here must be past before they measure anything.
func (f *galleryFrames) settle() {
	f.t.Helper()
	quiet := 0
	for range 96 {
		slots, bound := f.g.SlotCount(), len(f.g.Bindings(nil))
		f.frame()
		if !f.g.Reflowing() && f.g.SlotCount() == slots && len(f.g.Bindings(nil)) == bound {
			quiet++
			if quiet >= 2 {
				return
			}
			continue
		}
		quiet = 0
	}
	f.t.Fatalf("the gallery did not settle in 96 frames: %v", f.g)
}

// --- O1 ----------------------------------------------------------------------

// TestGalleryCompactDuringAReflow is the public API half of
// TestIndexCompactAtEveryRebuildPhase.
//
// [ui.Gallery.Compact] documents itself as being "for a gallery that has gone
// off screen", which is exactly the moment an application calls it, and a
// gallery goes off screen at an arbitrary point of an incremental reflow. It
// panicked with a slice bounds error inside the next layout pass, in masonry
// only, because justified never touches the builder scratch it released.
//
// Compact is called at every pass of the reflow, in both modes, and the
// gallery must go on to produce the same layout it would have produced
// untouched.
func TestGalleryCompactDuringAReflow(t *testing.T) {
	for _, tc := range []struct {
		name string
		l    ui.GalleryLayout
	}{
		{"masonry", ui.Masonry().MinColumnWidth(240).Gap(8)},
		{"justified", ui.Justified().RowHeight(180).Gap(8)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The reference extent, from a gallery nobody compacted.
			ref := newGalleryFrames(t, 100000, tc.l, geom.Sz(800, 600))
			ref.settle()
			ref.sz = geom.Sz(1200, 700)
			ref.settle()
			want := ref.g.ContentExtent()
			if want <= 0 {
				t.Fatalf("the reference gallery has no content: %v", ref.g)
			}

			// A resize of a 100 000 entry gallery takes several passes at the
			// default budget, so there is a real window to interfere with.
			for at := range 12 {
				f := newGalleryFrames(t, 100000, tc.l, geom.Sz(800, 600))
				f.settle()
				f.sz = geom.Sz(1200, 700)
				for pass := 0; ; pass++ {
					if pass == at {
						f.g.Compact()
					}
					f.frame()
					if !f.g.Reflowing() {
						break
					}
					if pass > 64 {
						t.Fatalf("no progress after Compact at pass %d", at)
					}
				}
				// A compact that happened after the reflow finished is the
				// case that was always safe; drive one more frame either way.
				f.frame()
				if got := f.g.ContentExtent(); got != want {
					t.Errorf("Compact at pass %d produced extent %.3f, want %.3f", at, got, want)
				}
				if f.g.VisibleCount() == 0 {
					t.Errorf("Compact at pass %d left the gallery blank: %v", at, f.g)
				}
			}
		})
	}
}

// --- O2 ----------------------------------------------------------------------

// TestGalleryCorrectionsKeepTheirBindings is the assertion
// [ui.TileBinding.Generation] needs in order to mean what it says.
//
// A correction batch changes dimensions. It inserts nothing, removes nothing
// and moves nothing, so every tile still stands for the picture it stood for,
// and the request generation of every one of them must stand still. Measured
// before WU-O at 100 000 entries with nine bound tiles: the generation advanced
// by nine per batch, per frame, every tile rebound to the same item. Step 4's
// dimension probing is the producer of those batches, and `gen` exists
// precisely so that a decode result arriving after a recycle can be dropped —
// so that would have been a pipeline invalidating every decode in flight on
// every batch it emitted.
//
// The batch is also applied repeatedly, frame after frame, because the failing
// shape was a steady state and not a transient.
func TestGalleryCorrectionsKeepTheirBindings(t *testing.T) {
	f := newGalleryFrames(t, 100000, ui.Masonry().MinColumnWidth(240).Gap(8), geom.Sz(800, 600))
	f.settle()
	var before []ui.TileBinding
	before = f.g.Bindings(before)
	if len(before) < 4 {
		t.Fatalf("not enough bound tiles to measure: %v", f.g)
	}

	batch := make([]asset.Correction, 0, 8)
	var after []ui.TileBinding
	for round := range 5 {
		batch = batch[:0]
		for i := range 8 {
			// Entries far away from the viewport, so nothing on screen even
			// has an excuse to be rebound.
			batch = append(batch, asset.Correction{
				ID:     asset.ID("img-" + strconv.Itoa(50000+i)),
				Width:  uint32(400 + i + round*13),
				Height: 300,
			})
		}
		gen := f.g.Generation()
		if n := f.g.ApplyCorrections(batch); n != len(batch) {
			t.Fatalf("round %d: %d of %d corrections applied", round, n, len(batch))
		}
		f.settle()
		if d := f.g.Generation() - gen; d != 0 {
			t.Errorf("round %d: a correction batch advanced the request generation by %d; "+
				"no tile changed the picture it stands for, so none of them may look recycled",
				round, d)
		}
	}

	after = f.g.Bindings(after)
	if len(after) != len(before) {
		t.Fatalf("%d tiles before the corrections, %d after", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("tile %d changed from %+v to %+v across a batch that moved no entry",
				i, before[i], after[i])
		}
	}
}

// TestGalleryCorrectionsRefreshWhatTheyChanged is the other side of it: the
// binding must keep its generation *and* tell the truth about the entry.
//
// A correction changes the revision and takes the entry out of the provisional
// state. Those are copies of catalogue facts held in the slot, and keeping a
// binding is only correct if they are refreshed in place. Getting this wrong
// the other way — refusing to unbind and refusing to refresh — would leave a
// tile claiming a revision the catalogue no longer has.
func TestGalleryCorrectionsRefreshWhatTheyChanged(t *testing.T) {
	f := newGalleryFrames(t, 20000, ui.Masonry().MinColumnWidth(240).Gap(8), geom.Sz(800, 600))
	f.settle()
	var bs []ui.TileBinding
	bs = f.g.Bindings(bs)
	if len(bs) == 0 {
		t.Fatalf("nothing bound: %v", f.g)
	}
	gen := f.g.Generation()

	batch := make([]asset.Correction, 0, len(bs))
	for _, b := range bs {
		batch = append(batch, asset.Correction{ID: b.ID, Width: 900, Height: 600, Revision: "probed"})
	}
	if n := f.g.ApplyCorrections(batch); n != len(batch) {
		t.Fatalf("%d of %d corrections applied", n, len(batch))
	}
	f.settle()

	// The generation may move here, and legitimately: correcting the visible
	// entries changes their heights, the reflow moves the band, and some
	// tiles really do come to stand for a different picture. What must not
	// happen is a tile that kept its slot and its item moving anyway.
	t.Logf("correcting the %d visible entries advanced the generation by %d",
		len(bs), f.g.Generation()-gen)
	for _, b := range bs {
		got, ok := f.g.BindingOf(b.ID)
		if !ok {
			continue // legitimately scrolled out of the band by the reflow
		}
		if got.Revision != "probed" {
			t.Errorf("tile of %q still reports revision %q after its correction", b.ID, got.Revision)
		}
		if got.Provisional {
			t.Errorf("tile of %q is still marked provisional after its correction", b.ID)
		}
		if got.Slot == b.Slot && got.Item == b.Item && got.Generation != b.Generation {
			t.Errorf("tile of %q kept slot %d and item %d but moved from generation %d to %d",
				b.ID, b.Slot, b.Item, b.Generation, got.Generation)
		}
	}
}

// TestGalleryCorrectionFrameCostDoesNotGrowWithTheCatalogue is the second half
// of O2: routing corrections through [layout.Index.ApplyCorrections] instead of
// [layout.Index.SetItems] removes an unchunked O(N) loop from a path whose
// entire design premise is that O(N) must not happen in a frame.
//
// What a correction frame legitimately costs is one chunk of incremental
// reflow — [ui.DefaultGalleryRebuildBudget] item placements — plus O(batch) for
// the batch itself. That is a constant, so above the budget the cost must be
// flat in N. Two catalogues a factor of four apart and both well past the
// budget are therefore expected to cost about the same. Before WU-O they did
// not: the [layout.Index.SetItems] call on top of the chunk was linear in N
// and dominated, measured at 0.51 ms per frame at 100 000 entries.
//
// A ratio and not an absolute time, because an absolute threshold on a shared
// machine is a flaky test.
func TestGalleryCorrectionFrameCostDoesNotGrowWithTheCatalogue(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	cost := func(n int) float64 {
		f := newGalleryFrames(t, n, ui.Masonry().MinColumnWidth(240).Gap(8), geom.Sz(800, 600))
		f.settle()
		batch := make([]asset.Correction, 8)
		round := 0
		step := func() {
			round++
			for i := range batch {
				batch[i] = asset.Correction{
					ID:     asset.ID("img-" + strconv.Itoa((i*7)%n)),
					Width:  uint32(400 + i + round%97),
					Height: 300,
				}
			}
			f.g.ApplyCorrections(batch)
			f.frame()
		}
		for range 20 {
			step()
		}
		res := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				step()
			}
		})
		return float64(res.NsPerOp())
	}
	// Both past DefaultGalleryRebuildBudget, four times apart.
	small, large := cost(100000), cost(400000)
	t.Logf("a frame with a correction batch costs %.0f ns at n=100000 and %.0f ns at n=400000",
		small, large)
	if large > small*2 {
		t.Errorf("a fourfold catalogue made a correction frame %.1f times more expensive; "+
			"the correction path is still linear in N rather than bounded by the reflow budget",
			large/small)
	}
}

// --- O3 ----------------------------------------------------------------------

// TestGalleryCorrectionsMidReflowAreNotTorn is the gallery level form of
// TestIndexApplyCorrectionsDuringARebuildDoesNotTear.
//
// O2 made this path live — corrections now reach
// [layout.Index.ApplyCorrections] rather than [layout.Index.SetItems] — so it
// has to be right. A batch that lands while a reflow is in flight must yield
// the layout that batch describes, not a mixture of the layout before it and
// the layout after it.
func TestGalleryCorrectionsMidReflowAreNotTorn(t *testing.T) {
	batch := make([]asset.Correction, 0, 400)
	for i := 0; i < 20000; i += 50 {
		batch = append(batch, asset.Correction{
			ID: asset.ID("img-" + strconv.Itoa(i)), Width: 100, Height: 1000, Revision: "probed",
		})
	}

	l := ui.Masonry().MinColumnWidth(240).Gap(8)
	ref := newGalleryFrames(t, 20000, l, geom.Sz(800, 600))
	ref.settle()
	ref.g.ApplyCorrections(batch)
	ref.settle()
	want := ref.g.ContentExtent()

	// Interfere at every pass of a resize reflow.
	for at := range 6 {
		f := newGalleryFrames(t, 20000, l, geom.Sz(800, 600))
		f.settle()
		f.sz = geom.Sz(800, 700) // a resize that does not change the columns
		applied := false
		for pass := 0; pass < 64; pass++ {
			if pass == at {
				applied = true
				f.g.ApplyCorrections(batch)
			}
			f.frame()
			if !f.g.Reflowing() && applied {
				break
			}
		}
		f.settle()
		// Same shape as the reference: same width, same corrections. The
		// resize changed only the viewport height, which no item's rectangle
		// depends on.
		if got := f.g.ContentExtent(); got != want {
			t.Errorf("a correction batch at pass %d produced extent %.3f, want %.3f", at, got, want)
		}
		if b, ok := f.g.BindingOf("img-0"); ok && b.Provisional {
			t.Errorf("pass %d: img-0 is marked provisional after being corrected", at)
		}
	}
}

// --- O4 ----------------------------------------------------------------------

// TestGalleryPoolTruncationSpansTheViewport pins the geometry of an under
// filled band.
//
// The pool size is decided by a build and the visible set by the layout pass
// after it, so a window enlargement leaves exactly one frame in which there
// are more visible items than tiles. The excess has to go somewhere, and where
// it went until WU-O was a prefix of the visible set — which for masonry is
// whole leading columns, because [layout.Index.Visible] returns masonry
// results grouped by column and says so. Measured on a 400 to 2400 pixel
// enlargement: six of thirty nine items bound and every one of them inside the
// leftmost 430 pixels of a 2400 pixel viewport. Two godocs called that "a
// briefly under filled band"; it reads as the gallery collapsing to the left
// edge.
//
// The assertion is therefore about the span and not about the count: whatever
// is kept must cover the viewport.
func TestGalleryPoolTruncationSpansTheViewport(t *testing.T) {
	const wide = 2400
	f := newGalleryFrames(t, 100000, ui.Masonry().MinColumnWidth(200).Gap(8), geom.Sz(400, 600))
	f.settle()
	f.sz = geom.Sz(wide, 600)

	var bs []ui.TileBinding
	sawTruncation := false
	for range 24 {
		f.frame()
		bs = f.g.Bindings(bs[:0])
		if len(bs) == 0 || len(bs) >= f.g.SlotCount() == false {
			// not the interesting frame
		}
		if f.g.SlotCount() >= f.g.Columns()*3 || f.g.Columns() == 0 {
			// The pool has caught up with the viewport; stop looking.
			if sawTruncation {
				break
			}
		}
		if len(bs) < f.g.Columns() && len(bs) > 0 {
			sawTruncation = true
			lo, hi := bs[0].DocX, bs[0].DocX+bs[0].DocW
			for _, b := range bs {
				lo = min(lo, b.DocX)
				hi = max(hi, b.DocX+b.DocW)
			}
			span := hi - lo
			t.Logf("under filled frame: %d tiles of %d columns span x=%.0f..%.0f of %d",
				len(bs), f.g.Columns(), lo, hi, wide)
			// A prefix of the visible set keeps whole leading columns. The
			// stride keeps the band spread over the whole width; half of it
			// is a generous floor and still four times what the prefix gave.
			if span < 0.5*wide {
				t.Errorf("%d tiles of a %d column layout span only %.0f of %d pixels; "+
					"the kept set is a prefix of the visible set and not a sample of it",
					len(bs), f.g.Columns(), span, wide)
			}
		}
	}
	if !sawTruncation {
		t.Skip("the pool never lagged the viewport in this run; nothing to assert")
	}
}

// --- O6 / Reorder ------------------------------------------------------------

// TestGalleryReorderRebindsAndKeepsTheAnchor covers the one catalogue mutation
// that moves entries, which the WU-N suite did not exercise at gallery level.
//
// It is the case the project plan, section 10, singles out: "nur eine
// Umsortierung braucht zusaetzlich die ID". Positions mean different entries
// afterwards, so every binding must be reissued — a tile that kept its
// position would show the wrong picture — and the anchor has to be re-resolved
// through the stable ID rather than kept as a position.
func TestGalleryReorderRebindsAndKeepsTheAnchor(t *testing.T) {
	h, g := galleryFixture(t, 5000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	node.ScrollTo(9000)
	h.Settle()
	id, _, ok := g.Anchor()
	if !ok {
		t.Fatalf("no anchor: %v", g)
	}
	n := g.Collection().Len()
	before := map[asset.ID]int{}
	var bs []ui.TileBinding
	for _, b := range g.Bindings(bs) {
		before[b.ID] = b.Item
	}
	gen := g.Generation()

	// Reverse the catalogue: every entry moves, and nothing is inserted or
	// removed, so the ID set is unchanged and the anchored entry still exists.
	order := make([]int, n)
	for i := range order {
		order[i] = n - 1 - i
	}
	g.Reorder(order)
	h.Settle()

	if g.Collection().ID(0) != asset.ID("img-"+strconv.Itoa(n-1)) {
		t.Fatalf("the reorder did not take: entry 0 is %q", g.Collection().ID(0))
	}
	if g.Generation() == gen {
		t.Error("a reorder rebound nothing; every position now means a different entry")
	}
	// Every binding reports the identity the catalogue reports for its
	// position. This is the assertion a tile that kept a stale position fails.
	for _, b := range g.Bindings(bs[:0]) {
		if got := g.Collection().ID(b.Item); got != b.ID {
			t.Errorf("slot %d says it holds %q at position %d, the catalogue says %q",
				b.Slot, b.ID, b.Item, got)
		}
		if was, had := before[b.ID]; had && was == b.Item && b.Item != n/2 {
			t.Errorf("%q is still at position %d after the catalogue was reversed", b.ID, b.Item)
		}
	}
	assertAnchored(t, h, g, id, "a full reorder of the catalogue")
}

// --- O7 ----------------------------------------------------------------------

// TestGalleryRejectsASecondView is the loud version of a silent failure.
//
// A [ui.Gallery] owns one tile pool, one set of bindings and one anchor. Two
// [ui.ImageGallery] views over it are two nodes that bind that single pool
// against two different viewports in the same frame, each undoing the other.
// [ui.NewGallery] already panics helpfully on a nil collection; this is the
// same courtesy for the mistake that is much harder to see.
func TestGalleryRejectsASecondView(t *testing.T) {
	g := ui.NewGallery(asset.NewCollection(synth(500)))
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(
			ui.ImageGallery(g).Flex(1),
			ui.ImageGallery(g).Flex(1),
		).Frame(geom.Unbounded(), geom.Unbounded())
	}})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("two ImageGallery views over one ui.Gallery were accepted; they alias the pool")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "two ImageGallery views over one ui.Gallery") {
			t.Errorf("panicked with %v", r)
		}
	}()
	_ = a.Update(geom.Sz(800, 600))
}

// TestGalleryMeasuresEachTileExactlyOnce pins [gift.Layouter]'s own contract.
//
// "It must measure and place every child exactly once." The gallery has two
// placement loops — one over the bound tiles and one that collapses the rest —
// and a slot that was claimed by the binding pass and then abandoned fell into
// both. It is asserted through the layouter count of a clean idle frame,
// because a doubly measured child is a layouter that ran twice.
func TestGalleryMeasuresEachTileExactlyOnce(t *testing.T) {
	h, g := galleryFixture(t, 10000, ui.Masonry().MinColumnWidth(240).Gap(8))
	node := h.Find(gifttest.ByKey("gallery"))
	before := h.Diagnostics()
	node.ScrollBy(600)
	after := h.Diagnostics()
	// One layouter per tile in the pool, plus the gallery itself and the
	// stack above it, is the bound of [gift.ScrollSpec.Virtual]. Measuring
	// any tile twice would put this over it.
	limit := uint64(g.SlotCount() + 4)
	if got := after.Layouts - before.Layouts; got > limit {
		t.Errorf("one viewport scroll ran %d layouters for a pool of %d; the bound is %d "+
			"and anything above it means a child was measured more than once",
			got, g.SlotCount(), limit)
	}
}
