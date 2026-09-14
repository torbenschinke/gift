package layout_test

import (
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"testing"

	"github.com/torbenschinke/gift/internal/layout"
)

// dims is a Dimensions over a plain slice of sizes. A zero pair means the
// dimensions are not known yet.
type dims struct{ w, h []uint32 }

func (d dims) DimensionsAt(i int) (uint32, uint32) { return d.w[i], d.h[i] }

func randomDims(n int, seed uint64, unknownEvery int) dims {
	r := rand.New(rand.NewPCG(seed, 0x5eed))
	d := dims{w: make([]uint32, n), h: make([]uint32, n)}
	for i := range n {
		if unknownEvery > 0 && i%unknownEvery == 0 {
			continue
		}
		d.w[i] = uint32(200 + r.IntN(3000))
		d.h[i] = uint32(200 + r.IntN(3000))
	}
	return d
}

func uniform(n int, w, h uint32) dims {
	d := dims{w: make([]uint32, n), h: make([]uint32, n)}
	for i := range n {
		d.w[i], d.h[i] = w, h
	}
	return d
}

func build(t testing.TB, p layout.GalleryParams, d dims) *layout.Index {
	t.Helper()
	ix := layout.NewIndex()
	ix.SetItems(len(d.w), d)
	ix.Rebuild(p)
	return ix
}

const feps = 1e-9

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// --- masonry ---------------------------------------------------------------

func TestMasonryColumnsAndWidth(t *testing.T) {
	tests := []struct {
		name         string
		width, minW  float64
		gap          float64
		wantCols     int
		wantColWidth float64
	}{
		{"exact fit", 1000, 250, 0, 4, 250},
		{"gaps eat a column", 1000, 240, 8, 4, 244},
		{"narrower than the minimum still has one column", 100, 240, 8, 1, 100},
		{"one column", 300, 240, 8, 1, 300},
		{"twelve columns at 1920", 1920, 152, 8, 12, (1920 - 11*8) / 12.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ix := build(t, layout.GalleryParams{
				Width: tc.width, MinColumnWidth: tc.minW, Gap: tc.gap,
			}, uniform(24, 100, 100))
			if got := ix.Columns(); got != tc.wantCols {
				t.Fatalf("columns = %d, want %d", got, tc.wantCols)
			}
			r, _ := ix.ItemRect(0)
			if !near(r.W, tc.wantColWidth, 1e-9) {
				t.Fatalf("column width = %v, want %v", r.W, tc.wantColWidth)
			}
			// The columns must exactly tile the width.
			last, _ := ix.ItemRect(tc.wantCols - 1)
			if want := tc.width; !near(last.Right(), want, 1e-4) && tc.wantCols > 1 {
				t.Fatalf("last column right edge = %v, want %v", last.Right(), want)
			}
		})
	}
}

func TestMasonryShortestColumn(t *testing.T) {
	// Three columns of width 100, gap 0. Heights follow from the aspects:
	// 1:1 -> 100, 1:2 -> 200, 2:1 -> 50.
	d := dims{
		w: []uint32{100, 100, 200, 100, 100, 100},
		h: []uint32{100, 200, 100, 100, 100, 400},
	}
	ix := build(t, layout.GalleryParams{Width: 300, MinColumnWidth: 100, Gap: 0}, d)
	if ix.Columns() != 3 {
		t.Fatalf("columns = %d", ix.Columns())
	}
	// Column bottoms evolve as (0,0,0) -> (100,0,0) -> (100,200,0) ->
	// (100,200,50) -> (100,200,150) -> (200,200,150) -> (200,200,550).
	want := []layout.DocRect{
		{X: 0, Y: 0, W: 100, H: 100},     // all columns empty, lowest index wins
		{X: 100, Y: 0, W: 100, H: 200},   // c1 is the shortest at 0
		{X: 200, Y: 0, W: 100, H: 50},    // c2 is the shortest at 0
		{X: 200, Y: 50, W: 100, H: 100},  // c2 at 50
		{X: 0, Y: 100, W: 100, H: 100},   // c0 at 100
		{X: 200, Y: 150, W: 100, H: 400}, // c2 at 150, not c0 or c1 at 200
	}
	for i, w := range want {
		got, ok := ix.ItemRect(i)
		if !ok || got != w {
			t.Errorf("item %d = %+v, want %+v", i, got, w)
		}
	}
	// Extent is the tallest column: c2 = 150+400 = 550.
	if got := ix.ContentExtent(); !near(got, 550, feps) {
		t.Fatalf("extent = %v, want 550", got)
	}
}

func TestMasonryGapsAndExtent(t *testing.T) {
	const n = 200
	p := layout.GalleryParams{Width: 1000, MinColumnWidth: 240, Gap: 8}
	ix := build(t, p, randomDims(n, 1, 0))
	cols := ix.Columns()

	// Reconstruct the columns from the rectangles and check the stacking.
	bottoms := make([]float64, cols)
	counts := make([]int, cols)
	var tallest float64
	for i := range n {
		r, _ := ix.ItemRect(i)
		c := int(math.Round(r.X / (r.W + p.Gap)))
		if c < 0 || c >= cols {
			t.Fatalf("item %d x=%v is not on a column", i, r.X)
		}
		want := bottoms[c]
		if counts[c] > 0 {
			want += p.Gap
		}
		if !near(r.Y, want, 1e-9) {
			t.Fatalf("item %d y = %v, want %v (column %d)", i, r.Y, want, c)
		}
		bottoms[c] = r.Bottom()
		counts[c]++
		if r.Bottom() > tallest {
			tallest = r.Bottom()
		}
	}
	if got := ix.ContentExtent(); !near(got, tallest, 1e-9) {
		t.Fatalf("extent = %v, want the tallest column %v", got, tallest)
	}
	// Every column must have been used, otherwise "shortest column" is broken.
	for c, k := range counts {
		if k == 0 {
			t.Fatalf("column %d is empty", c)
		}
	}
}

// --- justified -------------------------------------------------------------

func TestJustifiedRowsFillTheWidth(t *testing.T) {
	const n = 500
	p := layout.GalleryParams{
		Mode: layout.Justified, Width: 1200, Gap: 6, TargetRowHeight: 200,
	}
	ix := build(t, p, randomDims(n, 7, 0))

	rows := 0
	i := 0
	for i < n {
		r, _ := ix.ItemRect(i)
		// Collect the row: every following item with the same y.
		j := i
		for j < n {
			rj, _ := ix.ItemRect(j)
			if rj.Y != r.Y {
				break
			}
			if !near(rj.H, r.H, 1e-6) {
				t.Fatalf("item %d height %v differs from row height %v", j, rj.H, r.H)
			}
			j++
		}
		last, _ := ix.ItemRect(j - 1)
		isLastRow := j == n
		// Tolerance: x and w are stored as float32, so the right edge of
		// a row carries a few rounding steps of a value near 1200, which
		// is about 1e-4 pixels.
		if !isLastRow && !near(last.Right(), p.Width, 1e-3) {
			t.Fatalf("row starting at %d ends at %v, want the full width %v", i, last.Right(), p.Width)
		}
		if isLastRow {
			// Documented last row rule: target height, left aligned, not
			// stretched.
			if !near(r.H, p.TargetRowHeight, 1e-9) && last.Right() < p.Width-1e-6 {
				t.Fatalf("underfull last row height = %v, want the target %v", r.H, p.TargetRowHeight)
			}
			if !near(r.X, 0, 1e-9) {
				t.Fatalf("last row starts at x=%v, want left aligned", r.X)
			}
		}
		rows++
		i = j
	}
	if rows < 2 {
		t.Fatalf("only %d rows, the test proves nothing", rows)
	}
	if got := ix.Rows(); got != rows {
		t.Fatalf("Rows() = %d, reconstructed %d", got, rows)
	}
}

func TestJustifiedRowHeightTolerance(t *testing.T) {
	const n = 20000
	p := layout.GalleryParams{
		Mode: layout.Justified, Width: 1920, Gap: 8, TargetRowHeight: 240,
	}
	ix := build(t, p, randomDims(n, 11, 0))
	lo, hi, ok := ix.RowHeightBounds()
	if !ok {
		t.Fatal("no rows")
	}
	loRel := lo / p.TargetRowHeight
	hiRel := hi / p.TargetRowHeight
	t.Logf("row height over %d rows: min %.1f (%.2fx target) max %.1f (%.2fx target)",
		ix.Rows(), lo, loRel, hi, hiRel)
	// Stated tolerance, with the "closest to target" break rule and aspects
	// in [0.07, 15] clamped to [0.2, 5]: within a factor of two either way.
	if loRel < 0.5 || hiRel > 2.0 {
		t.Fatalf("row heights [%v, %v] leave the stated [0.5x, 2x] tolerance around %v", lo, hi, p.TargetRowHeight)
	}
}

func TestJustifiedExtentAndGaps(t *testing.T) {
	const n = 300
	p := layout.GalleryParams{Mode: layout.Justified, Width: 900, Gap: 10, TargetRowHeight: 150}
	ix := build(t, p, randomDims(n, 3, 0))
	var prev *layout.DocRect
	var maxBottom float64
	for i := range n {
		r, _ := ix.ItemRect(i)
		if prev != nil && r.Y != prev.Y {
			if !near(r.Y, prev.Bottom()+p.Gap, 1e-9) {
				t.Fatalf("row at item %d starts at %v, want %v", i, r.Y, prev.Bottom()+p.Gap)
			}
		} else if prev != nil {
			if !near(r.X, prev.Right()+p.Gap, 1e-3) {
				t.Fatalf("item %d starts at x=%v, want %v", i, r.X, prev.Right()+p.Gap)
			}
		}
		rc := r
		prev = &rc
		maxBottom = math.Max(maxBottom, r.Bottom())
	}
	if !near(ix.ContentExtent(), maxBottom, 1e-9) {
		t.Fatalf("extent = %v, want %v", ix.ContentExtent(), maxBottom)
	}
}

// --- the index against a brute force reference -----------------------------

// bruteVisible is the reference implementation: a linear scan over every item.
func bruteVisible(ix *layout.Index, top, bottom float64, dst []layout.Visible) []layout.Visible {
	if !(bottom > top) {
		return dst
	}
	for i := range ix.Len() {
		r, _ := ix.ItemRect(i)
		if r.Y < bottom && r.Bottom() > top {
			dst = append(dst, layout.Visible{Item: i, Rect: r})
		}
	}
	return dst
}

func sortVis(v []layout.Visible) { sort.Slice(v, func(a, b int) bool { return v[a].Item < v[b].Item }) }

func compareVisible(t *testing.T, ix *layout.Index, top, bottom float64, got, want []layout.Visible) {
	t.Helper()
	sortVis(got)
	sortVis(want)
	if len(got) != len(want) {
		t.Fatalf("viewport [%v,%v): index reported %d items, brute force %d", top, bottom, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("viewport [%v,%v): item %d: index %+v, brute force %+v", top, bottom, i, got[i], want[i])
		}
	}
}

func TestIndexAgainstBruteForce(t *testing.T) {
	modes := []struct {
		name string
		p    layout.GalleryParams
	}{
		{"masonry", layout.GalleryParams{Width: 1920, MinColumnWidth: 240, Gap: 8}},
		{"justified", layout.GalleryParams{Mode: layout.Justified, Width: 1920, Gap: 8, TargetRowHeight: 240}},
	}
	const n = 20000
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			d := randomDims(n, 42, 17)
			ix := build(t, m.p, d)
			extent := ix.ContentExtent()
			const vh = 1080.0

			var got, want []layout.Visible
			check := func(top float64) {
				got = ix.Visible(top, top+vh, got[:0])
				want = bruteVisible(ix, top, top+vh, want[:0])
				compareVisible(t, ix, top, top+vh, got, want)
			}

			// A sweep across the whole document.
			for top := -2 * vh; top < extent+2*vh; top += vh / 3 {
				check(top)
			}
			// Random jumps.
			r := rand.New(rand.NewPCG(9, 9))
			for range 2000 {
				check(r.Float64() * extent)
			}
			// Reversals: walk forward then backward in small steps.
			base := extent / 2
			for k := range 200 {
				check(base + float64(k)*37)
			}
			for k := 200; k >= 0; k-- {
				check(base + float64(k)*37)
			}
			// A viewport larger than the content.
			got = ix.Visible(-1e6, extent+1e6, got[:0])
			if len(got) != n {
				t.Fatalf("oversized viewport reported %d items, want all %d", len(got), n)
			}
			// Degenerate intervals.
			if len(ix.Visible(100, 100, got[:0])) != 0 {
				t.Fatal("empty interval reported items")
			}
			if len(ix.Visible(500, 100, got[:0])) != 0 {
				t.Fatal("inverted interval reported items")
			}

			// Resize, the second half of the test the project plan,
			// section 13, names.
			for _, w := range []float64{640, 1000, 1920, 3840, 137} {
				p := m.p
				p.Width = w
				ix.Rebuild(p)
				e := ix.ContentExtent()
				for range 500 {
					top := r.Float64() * e
					got = ix.Visible(top, top+vh, got[:0])
					want = bruteVisible(ix, top, top+vh, want[:0])
					compareVisible(t, ix, top, top+vh, got, want)
				}
			}
		})
	}
}

func TestPrecisionAtLargeDocumentPositions(t *testing.T) {
	// One column, tall items: 100 000 items times ~600 pixels is far past
	// 30 000 000 pixels, which is where float32 has a resolution of 2 px.
	const n = 100000
	p := layout.GalleryParams{Width: 600, MinColumnWidth: 600, Gap: 4}
	ix := build(t, p, randomDims(n, 5, 0))
	if ix.Columns() != 1 {
		t.Fatalf("columns = %d, want 1", ix.Columns())
	}
	if ix.ContentExtent() < 30e6 {
		t.Fatalf("extent %v is below 30 000 000, the test proves nothing", ix.ContentExtent())
	}
	// Every neighbouring pair must still be strictly ordered up there, which
	// is what a binary search needs and what float32 tops would lose.
	for i := 1; i < n; i++ {
		a, _ := ix.ItemRect(i - 1)
		b, _ := ix.ItemRect(i)
		if !(b.Y > a.Y) {
			t.Fatalf("items %d and %d have tops %v and %v: not strictly increasing", i-1, i, a.Y, b.Y)
		}
		if !near(b.Y, a.Bottom()+p.Gap, 1e-6) {
			t.Fatalf("item %d top %v, want %v", i, b.Y, a.Bottom()+p.Gap)
		}
	}
	var got, want []layout.Visible
	r := rand.New(rand.NewPCG(77, 77))
	for range 2000 {
		top := 30e6 + r.Float64()*(ix.ContentExtent()-30e6)
		got = ix.Visible(top, top+1080, got[:0])
		want = bruteVisible(ix, top, top+1080, want[:0])
		compareVisible(t, ix, top, top+1080, got, want)
		if len(got) == 0 {
			t.Fatalf("no items visible at %v inside the content", top)
		}
	}
}

// --- incremental rebuild ---------------------------------------------------

func TestIncrementalRebuildMatchesFromScratch(t *testing.T) {
	const n = 5000
	d := randomDims(n, 21, 13)
	for _, mode := range []layout.GalleryMode{layout.Masonry, layout.Justified} {
		p := layout.GalleryParams{
			Mode: mode, Width: 1000, MinColumnWidth: 200, Gap: 7, TargetRowHeight: 180,
		}
		ref := build(t, p, d)

		inc := layout.NewIndex()
		inc.SetItems(n, d)
		inc.BeginRebuild(p)
		steps := 0
		for !inc.Step(64) {
			steps++
			if steps > 10000 {
				t.Fatal("rebuild does not terminate")
			}
		}
		if steps < 10 {
			t.Fatalf("only %d chunks, the budget is not being honoured", steps)
		}
		if inc.ContentExtent() != ref.ContentExtent() {
			t.Fatalf("mode %d: extent %v, want %v", mode, inc.ContentExtent(), ref.ContentExtent())
		}
		for i := range n {
			a, _ := inc.ItemRect(i)
			b, _ := ref.ItemRect(i)
			if a != b {
				t.Fatalf("mode %d item %d: incremental %+v, from scratch %+v", mode, i, a, b)
			}
		}
	}
}

func TestQueryDuringRebuildSeesTheCommittedLayout(t *testing.T) {
	const n = 4000
	d := randomDims(n, 31, 0)
	p := layout.GalleryParams{Width: 1000, MinColumnWidth: 200, Gap: 8}
	ix := layout.NewIndex()
	ix.SetItems(n, d)

	// Before the first commit a query is empty and Ready is false.
	if ix.Ready() {
		t.Fatal("Ready before the first commit")
	}
	ix.BeginRebuild(p)
	ix.Step(10)
	if got := ix.Visible(0, 1000, nil); len(got) != 0 {
		t.Fatalf("query before the first commit returned %d items", len(got))
	}
	for !ix.Step(1 << 20) {
	}
	if !ix.Ready() || ix.Version() != 1 {
		t.Fatalf("Ready=%v version=%d after the first commit", ix.Ready(), ix.Version())
	}

	before := ix.Visible(0, 1000, nil)
	extent := ix.ContentExtent()

	// A second rebuild with a different width, stepped partially: queries
	// must still answer from the committed layout, unchanged.
	p2 := p
	p2.Width = 400
	if v := ix.BeginRebuild(p2); v != 2 {
		t.Fatalf("BeginRebuild announced version %d, want 2", v)
	}
	ix.Step(100)
	if !ix.Rebuilding() {
		t.Fatal("rebuild finished in one chunk, the test proves nothing")
	}
	if ix.Version() != 1 {
		t.Fatalf("version moved to %d mid rebuild", ix.Version())
	}
	if ix.ContentExtent() != extent {
		t.Fatal("extent changed mid rebuild")
	}
	mid := ix.Visible(0, 1000, nil)
	compareVisible(t, ix, 0, 1000, mid, before)

	for !ix.Step(1 << 20) {
	}
	if ix.Version() != 2 || ix.ContentWidth() != 400 {
		t.Fatalf("after commit: version %d width %v", ix.Version(), ix.ContentWidth())
	}
}

func TestStaleRebuildIsDiscarded(t *testing.T) {
	const n = 3000
	d := randomDims(n, 4, 0)
	base := layout.GalleryParams{Width: 1000, MinColumnWidth: 200, Gap: 8}
	ix := layout.NewIndex()
	ix.SetItems(n, d)
	ix.Rebuild(base)

	// A resize drag: many widths, each one cancelling the last.
	widths := []float64{980, 940, 900, 860, 820, 777}
	for _, w := range widths {
		p := base
		p.Width = w
		ix.BeginRebuild(p)
		ix.Step(50) // deliberately unfinished
	}
	last := base
	last.Width = widths[len(widths)-1]
	for !ix.Step(1 << 20) {
	}
	// Exactly one commit happened on top of the first.
	if ix.Version() != 2 {
		t.Fatalf("version = %d, want 2: a cancelled rebuild must not commit", ix.Version())
	}
	ref := build(t, last, d)
	if ix.ContentExtent() != ref.ContentExtent() {
		t.Fatalf("extent %v, want %v", ix.ContentExtent(), ref.ContentExtent())
	}
	for i := range n {
		a, _ := ix.ItemRect(i)
		b, _ := ref.ItemRect(i)
		if a != b {
			t.Fatalf("item %d: %+v, want %+v", i, a, b)
		}
	}
}

// --- corrections and anchors ----------------------------------------------

func TestCorrectionsMoveLaterItemsAndPreserveTheAnchor(t *testing.T) {
	const n = 2000
	d := randomDims(n, 8, 0)
	// The first half has unknown dimensions and is laid out provisionally.
	for i := range n / 2 {
		d.w[i], d.h[i] = 0, 0
	}
	p := layout.GalleryParams{Width: 1000, MinColumnWidth: 200, Gap: 8, ProvisionalAspect: 1.5}
	ix := layout.NewIndex()
	ix.SetItems(n, d)
	ix.Rebuild(p)

	if !ix.Provisional(0) || ix.Provisional(n-1) {
		t.Fatal("Provisional does not track the known dimensions")
	}
	// A provisional item has the provisional aspect.
	r0, _ := ix.ItemRect(0)
	if !near(r0.W/r0.H, 1.5, 1e-5) {
		t.Fatalf("provisional aspect = %v, want 1.5", r0.W/r0.H)
	}

	// Anchor the viewport to an item late in the document.
	const anchored = 1500
	ar, _ := ix.ItemRect(anchored)
	viewTop := ar.Y + 13.5
	a, ok := ix.Anchor(anchored, viewTop)
	if !ok || !near(a.Local, 13.5, 1e-9) {
		t.Fatalf("anchor = %+v", a)
	}

	// A batch of corrections for the provisional half: all of them get very
	// tall, which must push everything after them down.
	batch := make([]layout.Correction, 0, n/2)
	for i := range n / 2 {
		batch = append(batch, layout.Correction{Item: i, W: 400, H: 1600})
	}
	if got := ix.ApplyCorrections(batch); got != n/2 {
		t.Fatalf("ApplyCorrections reported %d changes, want %d", got, n/2)
	}
	// Corrections alone must not move anything: the layout is committed.
	if r, _ := ix.ItemRect(anchored); r != ar {
		t.Fatal("ApplyCorrections moved a committed item")
	}
	ix.Rebuild(p)

	if ix.Provisional(0) {
		t.Fatal("a corrected item is still provisional")
	}
	r0b, _ := ix.ItemRect(0)
	if !near(r0b.W/r0b.H, 0.25, 1e-5) {
		t.Fatalf("corrected aspect = %v, want 0.25", r0b.W/r0b.H)
	}
	nr, _ := ix.ItemRect(anchored)
	if !(nr.Y > ar.Y+1000) {
		t.Fatalf("item %d moved from %v to %v: taller predecessors did not push it down", anchored, ar.Y, nr.Y)
	}

	// The anchor resolves to the same item and the same local offset.
	newTop, ok := ix.Resolve(a)
	if !ok {
		t.Fatal("anchor no longer resolves")
	}
	if !near(newTop-nr.Y, 13.5, 1e-9) {
		t.Fatalf("local offset after the rebuild = %v, want 13.5", newTop-nr.Y)
	}
	// And the anchored item is still the one at the viewport top.
	vis := ix.Visible(newTop, newTop+1080, nil)
	found := false
	for _, v := range vis {
		if v.Item == anchored {
			found = true
		}
	}
	if !found {
		t.Fatal("the anchored item is not visible at the resolved offset")
	}
}

func TestAnchorRebindSurvivesASort(t *testing.T) {
	const n = 500
	d := randomDims(n, 6, 0)
	p := layout.GalleryParams{Width: 800, MinColumnWidth: 200, Gap: 4}
	ix := layout.NewIndex()
	ix.SetItems(n, d)
	ix.Rebuild(p)

	const item = 300
	r, _ := ix.ItemRect(item)
	a, _ := ix.Anchor(item, r.Y+7)

	// A sort: reverse the sequence. The caller re-resolves its stable ID to
	// the new position and rebinds.
	rev := dims{w: make([]uint32, n), h: make([]uint32, n)}
	for i := range n {
		rev.w[i], rev.h[i] = d.w[n-1-i], d.h[n-1-i]
	}
	ix.SetItems(n, rev)
	ix.Rebuild(p)

	a = a.Rebind(n - 1 - item)
	doc, ok := ix.Resolve(a)
	if !ok {
		t.Fatal("rebound anchor does not resolve")
	}
	nr, _ := ix.ItemRect(n - 1 - item)
	if !near(doc-nr.Y, 7, 1e-9) {
		t.Fatalf("local offset = %v, want 7", doc-nr.Y)
	}
}

func TestUnknownDimensionsUseTheProvisionalAspect(t *testing.T) {
	d := dims{w: make([]uint32, 8), h: make([]uint32, 8)}
	ix := layout.NewIndex()
	ix.SetItems(8, d)
	ix.Rebuild(layout.GalleryParams{
		Mode: layout.Justified, Width: 1000, Gap: 0, TargetRowHeight: 100, ProvisionalAspect: 2,
	})
	for i := range 8 {
		r, _ := ix.ItemRect(i)
		if !near(r.W/r.H, 2, 1e-5) {
			t.Fatalf("item %d aspect = %v, want 2", i, r.W/r.H)
		}
	}
}

func TestAspectClamp(t *testing.T) {
	d := dims{w: []uint32{1, 20000}, h: []uint32{20000, 1}}
	ix := layout.NewIndex()
	ix.SetItems(2, d)
	ix.Rebuild(layout.GalleryParams{Width: 400, MinColumnWidth: 400, Gap: 0, MinAspect: 0.5, MaxAspect: 2})
	a, _ := ix.ItemRect(0)
	b, _ := ix.ItemRect(1)
	if !near(a.W/a.H, 0.5, 1e-5) || !near(b.W/b.H, 2, 1e-5) {
		t.Fatalf("clamped aspects = %v and %v, want 0.5 and 2", a.W/a.H, b.W/b.H)
	}
	// Widening the clamp must recover the real ratios, which is only
	// possible because the raw ratio is what is stored.
	ix.Rebuild(layout.GalleryParams{Width: 400, MinColumnWidth: 400, Gap: 0, MinAspect: 1e-5, MaxAspect: 1e5})
	a, _ = ix.ItemRect(0)
	if !near(a.W/a.H, 1.0/20000, 1e-9) {
		t.Fatalf("widened clamp gives %v, want %v", a.W/a.H, 1.0/20000)
	}
}

func TestEmptyAndDegenerate(t *testing.T) {
	ix := layout.NewIndex()
	ix.SetItems(0, dims{})
	ix.Rebuild(layout.GalleryParams{Width: 1000, MinColumnWidth: 200})
	if ix.Len() != 0 || ix.ContentExtent() != 0 {
		t.Fatalf("empty index: len %d extent %v", ix.Len(), ix.ContentExtent())
	}
	if got := ix.Visible(-1e9, 1e9, nil); len(got) != 0 {
		t.Fatalf("empty index reported %d items", len(got))
	}
	if _, ok := ix.ItemRect(0); ok {
		t.Fatal("ItemRect out of range succeeded")
	}
	// A zero width viewport for one frame during window setup.
	ix.SetItems(10, uniform(10, 100, 100))
	ix.Rebuild(layout.GalleryParams{Width: 0, MinColumnWidth: 200})
	if ix.Columns() != 1 {
		t.Fatalf("zero width: columns = %d, want 1", ix.Columns())
	}
	// Justified with a gap wider than the viewport.
	ix.Rebuild(layout.GalleryParams{Mode: layout.Justified, Width: 10, Gap: 50, TargetRowHeight: 20})
	if ix.Rows() != 10 {
		t.Fatalf("degenerate justified: %d rows, want one per item", ix.Rows())
	}
}

// --- allocation and memory -------------------------------------------------

func TestVisibleDoesNotAllocate(t *testing.T) {
	const n = 100000
	for _, mode := range []layout.GalleryMode{layout.Masonry, layout.Justified} {
		ix := build(t, layout.GalleryParams{
			Mode: mode, Width: 1920, MinColumnWidth: 240, Gap: 8, TargetRowHeight: 240,
		}, randomDims(n, 12, 0))
		buf := make([]layout.Visible, 0, 512)
		extent := ix.ContentExtent()
		r := rand.New(rand.NewPCG(1, 2))
		// Warm up so that buf has grown to its working size.
		for range 100 {
			buf = ix.Visible(r.Float64()*extent, r.Float64()*extent+1080, buf[:0])
		}
		got := testing.AllocsPerRun(1000, func() {
			top := r.Float64() * extent
			buf = ix.Visible(top, top+1080, buf[:0])
		})
		if got != 0 {
			t.Fatalf("mode %d: Visible allocates %v times per call", mode, got)
		}
		if _, ok := ix.ItemRect(n / 2); !ok {
			t.Fatal("ItemRect failed")
		}
		if got := testing.AllocsPerRun(1000, func() {
			_, _ = ix.ItemRect(n / 2)
		}); got != 0 {
			t.Fatalf("ItemRect allocates %v times per call", got)
		}
	}
}

// TestGalleryMemoryPerEntry reports the resident bytes per entry of a
// committed 100 000 item index. It is the number the project plan, section 10,
// compares against its 32 bytes per entry example.
func TestGalleryMemoryPerEntry(t *testing.T) {
	const n = 100000
	d := randomDims(n, 99, 0)
	for _, tc := range []struct {
		name string
		p    layout.GalleryParams
	}{
		{"masonry", layout.GalleryParams{Width: 1920, MinColumnWidth: 240, Gap: 8}},
		{"justified", layout.GalleryParams{Mode: layout.Justified, Width: 1920, Gap: 8, TargetRowHeight: 240}},
	} {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		ix := layout.NewIndex()
		ix.SetItems(n, d)
		ix.Rebuild(tc.p)
		ix.Compact()
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(ix)
		bytes := float64(after.HeapAlloc) - float64(before.HeapAlloc)
		t.Logf("%s: %.0f bytes total, %.1f bytes per entry at n=%d (HeapAlloc delta after Compact and GC)",
			tc.name, bytes, bytes/n, n)
		if bytes/n > 32 {
			t.Fatalf("%s: %.1f bytes per entry exceeds the plan's 32 byte example", tc.name, bytes/n)
		}
	}
}

// --- benchmarks ------------------------------------------------------------

func benchVisible(b *testing.B, mode layout.GalleryMode, n int) {
	ix := layout.NewIndex()
	d := randomDims(n, 2, 0)
	ix.SetItems(n, d)
	ix.Rebuild(layout.GalleryParams{
		Mode: mode, Width: 1920, MinColumnWidth: 240, Gap: 8, TargetRowHeight: 240,
	})
	extent := ix.ContentExtent()
	buf := make([]layout.Visible, 0, 1024)
	r := rand.New(rand.NewPCG(3, 4))
	tops := make([]float64, 1024)
	for i := range tops {
		tops[i] = r.Float64() * extent
	}
	for range 64 {
		buf = ix.Visible(tops[0], tops[0]+1080, buf[:0])
	}
	b.ReportAllocs()
	b.ResetTimer()
	total, iters := 0, 0
	for i := 0; b.Loop(); i++ {
		top := tops[i&1023]
		buf = ix.Visible(top, top+1080, buf[:0])
		total += len(buf)
		iters++
	}
	// k is reported because the query is O(log n + k) and a comparison
	// across n is only honest when k is comparable. At n=1000 a 1080 pixel
	// viewport covers a large share of a short document, so k is not the
	// same as at n=100000; see the report.
	b.ReportMetric(float64(total)/float64(iters), "items/op")
}

// benchBruteVisible is the linear scan the index replaces. It is here so that
// "the query cost does not grow with n" is a comparison against something
// that does.
func benchBruteVisible(b *testing.B, n int) {
	ix := layout.NewIndex()
	ix.SetItems(n, randomDims(n, 2, 0))
	ix.Rebuild(layout.GalleryParams{Width: 1920, MinColumnWidth: 240, Gap: 8})
	extent := ix.ContentExtent()
	buf := make([]layout.Visible, 0, 1024)
	r := rand.New(rand.NewPCG(3, 4))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		top := r.Float64() * extent
		buf = bruteVisible(ix, top, top+1080, buf[:0])
	}
}

func BenchmarkBruteVisibleMasonry1k(b *testing.B)   { benchBruteVisible(b, 1000) }
func BenchmarkBruteVisibleMasonry10k(b *testing.B)  { benchBruteVisible(b, 10000) }
func BenchmarkBruteVisibleMasonry100k(b *testing.B) { benchBruteVisible(b, 100000) }

func BenchmarkVisibleMasonry1k(b *testing.B)   { benchVisible(b, layout.Masonry, 1000) }
func BenchmarkVisibleMasonry10k(b *testing.B)  { benchVisible(b, layout.Masonry, 10000) }
func BenchmarkVisibleMasonry100k(b *testing.B) { benchVisible(b, layout.Masonry, 100000) }

func BenchmarkVisibleJustified1k(b *testing.B)   { benchVisible(b, layout.Justified, 1000) }
func BenchmarkVisibleJustified10k(b *testing.B)  { benchVisible(b, layout.Justified, 10000) }
func BenchmarkVisibleJustified100k(b *testing.B) { benchVisible(b, layout.Justified, 100000) }

func benchBuild(b *testing.B, mode layout.GalleryMode, n int) {
	d := randomDims(n, 2, 0)
	ix := layout.NewIndex()
	ix.SetItems(n, d)
	p := layout.GalleryParams{
		Mode: mode, Width: 1920, MinColumnWidth: 240, Gap: 8, TargetRowHeight: 240,
	}
	ix.Rebuild(p)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		p.Width = 1900 + float64(i%40)
		ix.Rebuild(p)
	}
}

func BenchmarkBuildMasonry100k(b *testing.B)   { benchBuild(b, layout.Masonry, 100000) }
func BenchmarkBuildJustified100k(b *testing.B) { benchBuild(b, layout.Justified, 100000) }

func BenchmarkApplyCorrections1k(b *testing.B) {
	const n = 100000
	d := randomDims(n, 2, 0)
	ix := layout.NewIndex()
	ix.SetItems(n, d)
	batch := make([]layout.Correction, 1000)
	for i := range batch {
		batch[i] = layout.Correction{Item: i * 97 % n, W: uint32(300 + i%700), H: 400}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		for j := range batch {
			batch[j].H = uint32(400 + (i+j)%13)
		}
		ix.ApplyCorrections(batch)
	}
}

// BenchmarkColdBuildMasonry100k measures a rebuild that cannot reuse anything,
// which is the honest cost of the first layout of a collection. The warm
// benchmarks above reuse the recycled arrays and are the steady state cost of
// a resize.
func BenchmarkColdBuildMasonry100k(b *testing.B) {
	const n = 100000
	d := randomDims(n, 2, 0)
	p := layout.GalleryParams{Width: 1920, MinColumnWidth: 240, Gap: 8}
	b.ReportAllocs()
	for b.Loop() {
		ix := layout.NewIndex()
		ix.SetItems(n, d)
		ix.Rebuild(p)
	}
}

// TestRowOf checks the justified row lookup keyboard navigation is built on,
// against a brute force scan of the same layout.
func TestRowOf(t *testing.T) {
	ix := layout.NewIndex()
	ix.SetItems(500, dimsFunc(func(i int) (uint32, uint32) {
		return uint32(100 + i%7*40), 100
	}))
	ix.Rebuild(layout.GalleryParams{Mode: layout.Justified, Width: 900, Gap: 6, TargetRowHeight: 120})

	if _, _, ok := ix.RowOf(-1); ok {
		t.Error("RowOf(-1) reported a row")
	}
	if _, _, ok := ix.RowOf(500); ok {
		t.Error("RowOf(500) reported a row")
	}
	for i := range 500 {
		start, end, ok := ix.RowOf(i)
		if !ok {
			t.Fatalf("item %d has no row", i)
		}
		if i < start || i >= end {
			t.Fatalf("item %d is reported in the row [%d,%d)", i, start, end)
		}
		// Every item of the reported row shares its top, and the items just
		// outside it do not. That is the definition of a row and is checked
		// against the rectangles rather than against the internal arrays.
		top, _ := ix.ItemRect(i)
		for j := start; j < end; j++ {
			r, _ := ix.ItemRect(j)
			if r.Y != top.Y {
				t.Fatalf("item %d of row [%d,%d) is at y=%g, item %d at y=%g",
					j, start, end, r.Y, i, top.Y)
			}
		}
		if start > 0 {
			r, _ := ix.ItemRect(start - 1)
			if r.Y == top.Y {
				t.Fatalf("item %d is at the same y as row [%d,%d)", start-1, start, end)
			}
		}
	}

	// A masonry index has no rows; its vertical step is the column count.
	ix.Rebuild(layout.GalleryParams{Mode: layout.Masonry, Width: 900, MinColumnWidth: 200})
	if _, _, ok := ix.RowOf(0); ok {
		t.Error("a masonry index reported a justified row")
	}
}

// dimsFunc adapts a function to the Dimensions interface.
type dimsFunc func(i int) (uint32, uint32)

func (f dimsFunc) DimensionsAt(i int) (uint32, uint32) { return f(i) }
