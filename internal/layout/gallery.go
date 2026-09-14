package layout

import (
	"fmt"
	"math"
	"sort"
)

// This file implements the spatial index of the virtualised gallery described
// in the project plan, section 10. It is pure arithmetic: it knows nothing
// about views, nodes, assets or scrolling, and the ui layer drives it from a
// gift.Layouter.
//
// # Document coordinates are float64
//
// Every position and extent this file produces is float64. A 100 000 entry
// masonry document is routinely 30 000 000 pixels tall, and float32 has a
// resolution of 2 pixels up there: an item boundary would not be
// representable, two neighbouring items would report the same top, and a
// binary search over those tops would stop being a total order. The project
// plan, section 10, therefore requires the conversion to float32 to happen
// only after the viewport origin has been subtracted, and that subtraction is
// the caller's job, not this package's. There is deliberately no float32
// document coordinate anywhere in this API.
//
// Item *extents* are stored as float32 because an item is a few hundred pixels
// tall no matter where the document it sits in ends, and 4 bytes per entry
// times 100 000 entries is a number the plan's memory budget cares about. The
// running sums that produce the tops are float64 throughout, so the error does
// not accumulate: only the individual height is rounded, and it is rounded
// once, at build time, not per frame.

// GalleryMode selects the gallery layout algorithm.
type GalleryMode uint8

const (
	// Masonry places each item into the currently shortest column. Columns
	// have a fixed width, items keep their aspect ratio and therefore vary
	// in height, and the columns end at different bottoms.
	Masonry GalleryMode = iota

	// Justified fills rows to the full content width. Every item in a row
	// shares the row height, widths vary with the aspect ratio, and the row
	// height is chosen per row so that the row fills the width exactly.
	// Also known as the brick layout.
	Justified
)

// Defaults applied by [GalleryParams.normalise] for a zero or nonsensical
// field. They are not exported as knobs; they are the documented behaviour of
// a zero value.
const (
	defaultMinColumnWidth   = 160.0
	defaultTargetRowHeight  = 200.0
	defaultProvisionalRatio = 1.0
	defaultMinAspect        = 0.2
	defaultMaxAspect        = 5.0
)

// GalleryParams is the complete input of a gallery layout apart from the item
// dimensions themselves. Two indexes built with equal params and equal
// dimensions are byte for byte identical; there is no hidden state.
type GalleryParams struct {
	// Mode selects the algorithm.
	Mode GalleryMode

	// Width is the content width available to the gallery, in logical
	// pixels, already net of any padding the caller applies. It must be
	// positive; a non positive width yields an empty layout rather than a
	// panic, because a viewport of width zero happens for one frame during
	// window setup.
	Width float64

	// Gap is the space between two adjacent items, horizontally and
	// vertically. Negative values are clamped to zero: a gallery is not a
	// stack and there is no honest reading of overlapping tiles.
	Gap float64

	// MinColumnWidth is the smallest acceptable masonry column width. The
	// column count is the largest count whose columns are still at least
	// this wide. Ignored by [Justified].
	MinColumnWidth float64

	// TargetRowHeight is the row height [Justified] aims for. The actual
	// row height varies, because a row that fills the width exactly cannot
	// also have a prescribed height; see [Index.RowHeightBounds].
	// Ignored by [Masonry].
	TargetRowHeight float64

	// ProvisionalAspect is the width over height ratio used for an item
	// whose dimensions are not known yet. The project plan, section 10,
	// requires such items to be laid out provisionally rather than skipped
	// or blocked on. Defaults to 1.
	ProvisionalAspect float64

	// MinAspect and MaxAspect clamp every aspect ratio, known or
	// provisional. Without a clamp a 1x20000 scan would produce a masonry
	// column entry 20 000 pixels tall that is taller than the viewport and
	// can never be scrolled past sensibly, and a justified row of one item
	// with a row height of 3 pixels. Default to 0.2 and 5.
	MinAspect, MaxAspect float64
}

func (p GalleryParams) normalise() GalleryParams {
	if !(p.Width > 0) {
		p.Width = 0
	}
	if !(p.Gap > 0) {
		p.Gap = 0
	}
	if !(p.MinColumnWidth > 0) {
		p.MinColumnWidth = defaultMinColumnWidth
	}
	if !(p.TargetRowHeight > 0) {
		p.TargetRowHeight = defaultTargetRowHeight
	}
	if !(p.ProvisionalAspect > 0) {
		p.ProvisionalAspect = defaultProvisionalRatio
	}
	if !(p.MinAspect > 0) {
		p.MinAspect = defaultMinAspect
	}
	if !(p.MaxAspect > 0) || p.MaxAspect < p.MinAspect {
		p.MaxAspect = math.Max(defaultMaxAspect, p.MinAspect)
	}
	return p
}

// DocRect is a rectangle in document space. All four components are float64;
// see the note at the top of this file.
type DocRect struct {
	X, Y, W, H float64
}

// Bottom returns Y+H.
func (r DocRect) Bottom() float64 { return r.Y + r.H }

// Right returns X+W.
func (r DocRect) Right() float64 { return r.X + r.W }

// Visible is one item reported by [Index.Visible].
type Visible struct {
	// Item is the index into the item sequence the index was built from.
	Item int
	// Rect is the document rectangle of that item.
	Rect DocRect
}

// Dimensions supplies the intrinsic pixel size of each item.
//
// The shape mirrors asset.Metadata from the project plan, section 9, which
// carries Width and Height as uint32 and leaves them at zero when they are not
// known yet. Taking the raw integers rather than a precomputed aspect ratio
// means the caller never has to invent a value for the unknown case, and the
// single place that decides what "unknown" looks like — the provisional ratio
// and the clamp — is here, where the layout is, and not spread over every
// collection implementation.
type Dimensions interface {
	// DimensionsAt returns the oriented pixel size of item i. A zero width
	// or height means the size is not known yet and the item is laid out
	// with [GalleryParams.ProvisionalAspect].
	DimensionsAt(i int) (w, h uint32)
}

// Correction is a late arriving real size for one item, as produced by the
// asset pipeline once it has probed the file.
type Correction struct {
	Item int
	W, H uint32
}

// Anchor pins a scroll position to an item instead of to a document
// coordinate, which is what keeps the viewport still across a rebuild that
// moves every item — a width change, a sort, or a batch of corrections.
//
// The project plan, section 10, phrases this as "stable image ID plus local
// offset". The index does not know about asset IDs, so it stores the item
// position; the caller holds the ID. After a sort the item positions change
// and the caller must re-resolve its ID to the new position with
// [Anchor.Rebind] before calling [Index.Resolve]. After a resize or a
// correction batch the positions do not change and the anchor can be resolved
// unchanged.
type Anchor struct {
	// Item is the item the anchor is pinned to.
	Item int
	// Local is the document offset of the anchored point relative to the
	// top of that item. It is typically the distance from the top of the
	// item to the top edge of the viewport, and is therefore usually
	// negative or small.
	Local float64
}

// Rebind returns a copy of a pinned to a different item position, keeping the
// local offset. It is what a caller uses after a sort has moved the item its
// stable ID refers to.
func (a Anchor) Rebind(item int) Anchor { a.Item = item; return a }

// Index is the spatial index of a gallery layout.
//
// # Lifecycle
//
//	ix := layout.NewIndex()
//	ix.SetItems(n, dims)              // O(n), once per collection change
//	ix.BeginRebuild(params)           // O(1)
//	for !ix.Step(2000) {              // bounded chunks, off the hot path
//	}
//	ix.Visible(top, bottom, buf[:0])  // O(log n + k), zero allocations
//
// A query always reads the last *committed* layout. While a rebuild is in
// flight the index therefore keeps answering from the previous layout: a
// consistent, slightly stale answer rather than a half built one. Before the
// first commit [Index.Ready] is false and a query returns nothing.
//
// Index is not safe for concurrent use. It belongs to the UI executor, like
// every other piece of layout state.
type Index struct {
	// aspect is the raw width over height ratio of every item, or zero when
	// the dimensions are not known yet. It is stored raw, not clamped and
	// not defaulted, so that changing [GalleryParams.ProvisionalAspect] or
	// the clamp is a pure rebuild and never has to go back to the source.
	// The clamping happens once per item per build, in [Index.aspectOf].
	aspect []float32
	n      int

	cur   *galleryState // committed, nil until the first commit
	pend  *galleryState // under construction, nil when idle
	spare *galleryState // recycled backing arrays

	b       builder
	version uint64
}

// galleryState is one complete layout. Everything in it is derived from the
// aspects and the params.
//
// # Per entry memory
//
// Masonry keeps x (4), y (8), h (4) and order (4) per item, so 20 bytes, plus
// one int32 per column for the segment starts and, while a build is running,
// one int32 of scratch per item. Justified keeps x (4), y (8), h (4) and w (4)
// per item, so 20 bytes, plus 12 bytes per row. On top of that [Index] keeps
// the input aspect, 4 bytes per item, and the known bitset, one eighth of a
// byte. The measured total is reported by TestGalleryMemoryPerEntry.
type galleryState struct {
	params GalleryParams
	n      int

	// cols and colW are masonry only.
	cols int
	colW float64

	x []float32 // document x. Fits float32: it is bounded by the width.
	y []float64 // document top.
	h []float32 // item height.
	w []float32 // item width, justified only; masonry uses colW.

	// order holds, for masonry, the item indices of column c in
	// order[segStart[c]:segStart[c+1]], increasing in y. For justified it
	// is nil and item indices within a row are contiguous.
	order    []int32
	segStart []int32 // len = segments+1

	// segY and segH are justified only: the top and height of each row.
	segY []float64
	segH []float32

	// col is build scratch: the column each item landed in. It is kept
	// between rebuilds for reuse and released by [Index.Compact].
	col []int32

	extent float64
	minRow float64
	maxRow float64
}

func (s *galleryState) segments() int { return len(s.segStart) - 1 }

// builder is the progress of an in flight rebuild.
type builder struct {
	active bool
	phase  uint8
	pos    int

	// y is the top of the next justified row, carried across chunks.
	y float64

	// masonry running column bottoms, and per column counts for the
	// grouping phase.
	bottom []float64
	count  []int32
	cursor []int32
}

const (
	phasePlace uint8 = iota
	phaseGroup
	phaseDone
)

// NewIndex returns an empty index. It is ready for [Index.SetItems]; until a
// rebuild has been committed every query reports nothing.
func NewIndex() *Index { return &Index{} }

// SetItems replaces the item sequence and reads every dimension once.
//
// It is O(n) and it cancels any rebuild in flight, because a rebuild of a
// sequence that no longer exists cannot produce a usable result. The committed
// layout is kept until the next rebuild commits, so queries keep working, but
// they describe the old sequence; a caller that changed the item count must
// start and finish a rebuild before trusting item indices again.
//
// dims is read exactly len(n) times and is not retained.
func (ix *Index) SetItems(n int, dims Dimensions) {
	if n < 0 {
		panic(fmt.Sprintf("gift/internal/layout: Index.SetItems with negative count %d", n))
	}
	ix.cancel()
	ix.n = n
	ix.aspect = grow32(ix.aspect, n)
	if dims == nil && n > 0 {
		panic("gift/internal/layout: Index.SetItems with a nil Dimensions and a non empty item count")
	}
	for i := range n {
		ix.aspect[i] = rawAspect(dims.DimensionsAt(i))
	}
}

// ApplyCorrections folds a batch of real dimensions into the item sequence and
// reports how many of them actually changed something.
//
// The project plan, section 10, requires corrections to be *batched*, and this
// is why: correcting one item moves every item after it, so applying
// corrections one at a time would be one O(n) rebuild per probed image. The
// caller collects them for a frame — or for as long as it likes — applies them
// in one call, and starts a single rebuild.
//
// ApplyCorrections does not touch the committed layout. The positions in it
// are still the provisional ones until a rebuild has been started and
// committed; that is deliberate, because the moment to move the content is a
// decision about the scroll anchor, and that decision belongs to the caller.
func (ix *Index) ApplyCorrections(cs []Correction) int {
	changed := 0
	for _, c := range cs {
		if c.Item < 0 || c.Item >= ix.n {
			continue
		}
		a := rawAspect(c.W, c.H)
		if a == ix.aspect[c.Item] {
			continue
		}
		ix.aspect[c.Item] = a
		changed++
	}
	return changed
}

// Provisional reports whether item i is still laid out with the provisional
// aspect ratio because its real dimensions have not arrived yet.
func (ix *Index) Provisional(i int) bool {
	if i < 0 || i >= ix.n {
		return false
	}
	return ix.aspect[i] == 0
}

// BeginRebuild starts a new layout with p and returns the version the result
// will carry once it commits.
//
// Any rebuild already in flight is discarded here, and that is the mechanism
// the project plan, section 10, calls for when it says results of stale
// layouts are thrown away: a resize that produces twenty intermediate widths
// runs twenty BeginRebuild calls and commits one layout, the last one.
//
// BeginRebuild itself is O(1) apart from sizing the output arrays. The work
// happens in [Index.Step].
func (ix *Index) BeginRebuild(p GalleryParams) uint64 {
	ix.cancel()
	p = p.normalise()
	st := ix.take()
	st.params = p
	st.n = ix.n
	st.extent = 0
	st.minRow, st.maxRow = math.Inf(1), math.Inf(-1)
	st.x = grow32(st.x, ix.n)
	st.y = grow64(st.y, ix.n)
	st.h = grow32(st.h, ix.n)
	st.segStart = st.segStart[:0]
	st.segY = st.segY[:0]
	st.segH = st.segH[:0]
	st.order = st.order[:0]
	st.w = st.w[:0]

	if p.Mode == Masonry {
		st.cols, st.colW = columnsFor(p)
		st.col = grow32i(st.col, ix.n)
		ix.b.bottom = grow64(ix.b.bottom, st.cols)
		ix.b.count = grow32i(ix.b.count, st.cols)
		for c := range st.cols {
			ix.b.bottom[c] = 0
			ix.b.count[c] = 0
		}
	} else {
		st.cols, st.colW = 0, 0
		st.w = grow32(st.w, ix.n)
		st.segStart = append(st.segStart, 0)
	}

	ix.pend = st
	ix.b.active = true
	ix.b.phase = phasePlace
	ix.b.pos = 0
	ix.b.y = 0
	return ix.version + 1
}

// Step advances an in flight rebuild by at most budget items and reports
// whether the rebuild is finished. A finished rebuild has already been
// committed and is visible to queries; there is no separate commit call,
// because there is no useful state between "done" and "taken over".
//
// Step with no rebuild in flight returns true and does nothing. A budget below
// one is treated as one, so a caller cannot deadlock itself by passing zero.
//
// The budget is item placements, not time. Masonry runs two phases over the
// items — placing and grouping by column — so a full rebuild of n items costs
// about 2n budget.
func (ix *Index) Step(budget int) bool {
	if !ix.b.active {
		return true
	}
	if budget < 1 {
		budget = 1
	}
	st := ix.pend
	for budget > 0 && ix.b.phase != phaseDone {
		switch {
		case st.params.Mode == Masonry && ix.b.phase == phasePlace:
			budget -= ix.placeMasonry(st, budget)
		case st.params.Mode == Masonry:
			budget -= ix.groupMasonry(st, budget)
		default:
			budget -= ix.placeJustified(st, budget)
		}
	}
	if ix.b.phase != phaseDone {
		return false
	}
	ix.commit()
	return true
}

// Rebuild is BeginRebuild followed by running the whole build to completion.
// It is the convenient form for tests and for a caller that has decided the
// item count is small enough to do in one go; the chunked form exists because
// the project plan, section 10, forbids an O(n) rebuild on the frame hot path.
func (ix *Index) Rebuild(p GalleryParams) {
	ix.BeginRebuild(p)
	for !ix.Step(1 << 30) {
	}
}

// Rebuilding reports whether a rebuild is in flight.
func (ix *Index) Rebuilding() bool { return ix.b.active }

// Version is the version of the committed layout. It starts at zero, which is
// also the version of "nothing committed yet", and increases by one with every
// commit. A caller that caches anything derived from item positions compares
// this number and throws its cache away when it changed.
func (ix *Index) Version() uint64 { return ix.version }

// Ready reports whether a layout has been committed and queries are
// meaningful.
func (ix *Index) Ready() bool { return ix.cur != nil }

// Len is the number of items in the committed layout, which is not necessarily
// the number passed to the last [Index.SetItems].
func (ix *Index) Len() int {
	if ix.cur == nil {
		return 0
	}
	return ix.cur.n
}

// ContentExtent is the document height of the committed layout, gaps between
// items included and the trailing gap excluded.
func (ix *Index) ContentExtent() float64 {
	if ix.cur == nil {
		return 0
	}
	return ix.cur.extent
}

// ContentWidth is the width the committed layout was built for.
func (ix *Index) ContentWidth() float64 {
	if ix.cur == nil {
		return 0
	}
	return ix.cur.params.Width
}

// Columns is the masonry column count of the committed layout, and zero for a
// justified one.
func (ix *Index) Columns() int {
	if ix.cur == nil {
		return 0
	}
	return ix.cur.cols
}

// Rows is the justified row count of the committed layout, and zero for a
// masonry one.
func (ix *Index) Rows() int {
	if ix.cur == nil || ix.cur.params.Mode != Justified {
		return 0
	}
	return ix.cur.segments()
}

// RowHeightBounds returns the smallest and largest row height in a committed
// justified layout. It exists so that the tolerance around
// [GalleryParams.TargetRowHeight] is a measured number rather than a claim.
func (ix *Index) RowHeightBounds() (lo, hi float64, ok bool) {
	if ix.cur == nil || ix.cur.params.Mode != Justified || ix.cur.segments() == 0 {
		return 0, 0, false
	}
	return ix.cur.minRow, ix.cur.maxRow, true
}

// ItemRect returns the document rectangle of item i in the committed layout.
// It is O(1) and is what the scroll anchor and scroll-to-item are built on.
func (ix *Index) ItemRect(i int) (DocRect, bool) {
	st := ix.cur
	if st == nil || i < 0 || i >= st.n {
		return DocRect{}, false
	}
	return st.rect(i), true
}

func (s *galleryState) rect(i int) DocRect {
	w := s.colW
	if s.params.Mode == Justified {
		w = float64(s.w[i])
	}
	return DocRect{X: float64(s.x[i]), Y: s.y[i], W: w, H: float64(s.h[i])}
}

// Visible appends every item of the committed layout that intersects the
// document interval [top, bottom) to dst and returns the extended slice.
//
// Pass dst[:0] of a slice the caller keeps across frames and Visible allocates
// nothing; that is the form the allocation contract of the project plan,
// section 11, is measured in.
//
// # Cost
//
// O(log n + k), or precisely O(c*log(n/c) + k) for masonry with c columns and
// O(log r + k) for justified with r rows. c is derived from the viewport width
// and the minimum column width and is a small constant — twelve at 1920 pixels
// with a 152 pixel minimum — so it does not grow with n.
//
// Masonry runs one binary search per column over that column's
// items, whose tops and bottoms are both monotonic because a column is a
// stack; justified runs one binary search over the rows. Neither walks the
// items outside the interval, which is the property the project plan,
// section 10, demands when it says the scroll path does not search all 100 000
// entries.
//
// # Order
//
// The items come out grouped by column for masonry — column 0 top to bottom,
// then column 1, and so on — and in item order for justified. Masonry is
// deliberately not resorted into item order: the sort would be O(k log k) per
// frame for a property nobody needs, because the consumer keys its mounted
// nodes by item index anyway. A caller that needs document order sorts it
// itself and pays for it knowingly.
//
// An empty or inverted interval yields nothing. An interval larger than the
// content yields every item.
func (ix *Index) Visible(top, bottom float64, dst []Visible) []Visible {
	st := ix.cur
	if st == nil || st.n == 0 || !(bottom > top) {
		return dst
	}
	if st.params.Mode == Justified {
		return st.visibleRows(top, bottom, dst)
	}
	return st.visibleColumns(top, bottom, dst)
}

func (s *galleryState) visibleColumns(top, bottom float64, dst []Visible) []Visible {
	for c := range s.cols {
		col := s.order[s.segStart[c]:s.segStart[c+1]]
		m := len(col)
		// The first entry whose bottom lies past top. Both the tops and
		// the bottoms increase monotonically within a column, because a
		// column is a stack, so this is a proper lower bound and the scan
		// below can stop at the first entry past bottom.
		k := sort.Search(m, func(j int) bool {
			i := col[j]
			return s.y[i]+float64(s.h[i]) > top
		})
		for ; k < m; k++ {
			i := int(col[k])
			if s.y[i] >= bottom {
				break
			}
			dst = append(dst, Visible{Item: i, Rect: s.rect(i)})
		}
	}
	return dst
}

func (s *galleryState) visibleRows(top, bottom float64, dst []Visible) []Visible {
	rows := s.segments()
	r := sort.Search(rows, func(j int) bool {
		return s.segY[j]+float64(s.segH[j]) > top
	})
	for ; r < rows; r++ {
		if s.segY[r] >= bottom {
			break
		}
		for i := int(s.segStart[r]); i < int(s.segStart[r+1]); i++ {
			dst = append(dst, Visible{Item: i, Rect: s.rect(i)})
		}
	}
	return dst
}

// Anchor pins doc — a document coordinate, typically the top edge of the
// viewport — to item i, so that the same visual position can be recovered
// after a rebuild has moved everything.
//
// The recipe for the caller is: take the first item of the current visible
// set, remember its stable asset ID next to this anchor, rebuild, re-resolve
// the ID to an item position if the order changed, then [Index.Resolve].
func (ix *Index) Anchor(i int, doc float64) (Anchor, bool) {
	r, ok := ix.ItemRect(i)
	if !ok {
		return Anchor{}, false
	}
	return Anchor{Item: i, Local: doc - r.Y}, true
}

// Resolve turns an anchor back into a document coordinate against the
// committed layout. It fails when the anchored item does not exist in that
// layout, which is exactly the case where the caller has to fall back to a
// coordinate of its own choosing — the collection dropped the image.
func (ix *Index) Resolve(a Anchor) (float64, bool) {
	r, ok := ix.ItemRect(a.Item)
	if !ok {
		return 0, false
	}
	return r.Y + a.Local, true
}

// RowOf returns the half open item range of the justified row item i belongs
// to. It reports false for a masonry layout, for an unready index and for an
// item outside the committed layout.
//
// It exists for keyboard navigation. The project plan, section 10, requires
// the cursor to follow "der logischen Collection-Reihenfolge", and in a
// justified layout the step that moves one line down is the length of the
// current row, which varies per row and is only known here. A masonry layout
// needs no equivalent, because its vertical step is the column count and that
// is [Index.Columns].
//
// It is one binary search over the row starts, so O(log r).
func (ix *Index) RowOf(i int) (start, end int, ok bool) {
	st := ix.cur
	if st == nil || st.params.Mode != Justified || i < 0 || i >= st.n {
		return 0, 0, false
	}
	rows := st.segments()
	// segStart[r+1] is the exclusive end of row r, so the row of i is the
	// first r whose end is greater than i.
	r := sort.Search(rows, func(j int) bool { return int(st.segStart[j+1]) > i })
	if r >= rows {
		return 0, 0, false
	}
	return int(st.segStart[r]), int(st.segStart[r+1]), true
}

// Compact releases the recycled backing arrays of the index, trading the cost
// of the next rebuild for about half the resident bytes. It is for a gallery
// that has gone off screen, not for the scroll path.
func (ix *Index) Compact() {
	ix.spare = nil
	ix.b.bottom, ix.b.count, ix.b.cursor = nil, nil, nil
	if ix.cur != nil && !ix.b.active {
		ix.cur.col = nil
	}
}

// --- build ------------------------------------------------------------------

func (ix *Index) placeMasonry(st *galleryState, budget int) int {
	p := st.params
	n := st.n
	end := min(ix.b.pos+budget, n)
	done := end - ix.b.pos
	for i := ix.b.pos; i < end; i++ {
		c := shortest(ix.b.bottom[:st.cols])
		y := ix.b.bottom[c]
		if ix.b.count[c] > 0 {
			y += p.Gap
		}
		// The height is rounded to float32 *before* it is added to the
		// running bottom, so that the stored top of the next item in the
		// column is exactly the stored bottom of this one plus the gap.
		// Accumulating the unrounded value instead leaves a sub micron
		// mismatch between the document tiling and the rectangles the
		// caller actually gets, and makes an incremental build differ
		// from a from scratch one in the last bits.
		h := float64(float32(st.colW / ix.aspectOf(i, p)))
		st.col[i] = int32(c)
		st.x[i] = float32(float64(c) * (st.colW + p.Gap))
		st.y[i] = y
		st.h[i] = float32(h)
		ix.b.bottom[c] = y + h
		ix.b.count[c]++
	}
	ix.b.pos = end
	if end == n {
		for c := range st.cols {
			if ix.b.bottom[c] > st.extent {
				st.extent = ix.b.bottom[c]
			}
		}
		ix.b.phase = phaseGroup
		ix.b.pos = 0
		// Segment starts are the prefix sums of the per column counts.
		st.segStart = grow32i(st.segStart, st.cols+1)
		st.order = grow32i(st.order, n)
		ix.b.cursor = grow32i(ix.b.cursor, st.cols)
		var acc int32
		for c := range st.cols {
			st.segStart[c] = acc
			ix.b.cursor[c] = acc
			acc += ix.b.count[c]
		}
		st.segStart[st.cols] = acc
		if n == 0 {
			ix.b.phase = phaseDone
		}
	}
	return done
}

// groupMasonry fills the per column item lists. Walking the items in index
// order keeps each column's list sorted by y, because a column is filled from
// the top and an item placed later always lands below one placed earlier.
func (ix *Index) groupMasonry(st *galleryState, budget int) int {
	end := min(ix.b.pos+budget, st.n)
	done := end - ix.b.pos
	for i := ix.b.pos; i < end; i++ {
		c := st.col[i]
		st.order[ix.b.cursor[c]] = int32(i)
		ix.b.cursor[c]++
	}
	ix.b.pos = end
	if end == st.n {
		ix.b.phase = phaseDone
	}
	return done
}

// placeJustified fills whole rows. The budget is honoured at row boundaries,
// so a single call may overshoot it by at most the length of one row.
func (ix *Index) placeJustified(st *galleryState, budget int) int {
	p := st.params
	n := st.n
	start := ix.b.pos
	y := ix.b.y
	for ix.b.pos < n && ix.b.pos-start < budget {
		i := ix.b.pos
		j, rowH := ix.fillRow(st, i)
		// See placeMasonry: extents are rounded once, here, and every
		// running sum uses the rounded value.
		rowH = float64(float32(rowH))

		x := 0.0
		for k := i; k < j; k++ {
			w := float64(float32(rowH * ix.aspectOf(k, p)))
			st.x[k] = float32(x)
			st.y[k] = y
			st.h[k] = float32(rowH)
			st.w[k] = float32(w)
			x = float64(float32(x + w + p.Gap))
		}
		st.segY = append(st.segY, y)
		st.segH = append(st.segH, float32(rowH))
		st.segStart = append(st.segStart, int32(j))
		if rowH < st.minRow {
			st.minRow = rowH
		}
		if rowH > st.maxRow {
			st.maxRow = rowH
		}
		st.extent = y + rowH
		y += rowH + p.Gap
		ix.b.pos = j
	}
	ix.b.y = y
	if ix.b.pos >= n {
		ix.b.phase = phaseDone
	}
	return ix.b.pos - start
}

// fillRow greedily grows a row from item i and returns the exclusive end, the
// sum of the aspect ratios and the row height.
//
// # Break rule
//
// Items are added until the height needed to fill the width drops to or below
// the target. At that point two rows fill the width exactly: the one with the
// last item and the one without it. The one whose height is closer to the
// target wins. Picking the first candidate unconditionally is the usual
// implementation and it is biased short — it always lands below the target,
// never above — which makes every row of wide images noticeably thinner than
// asked for.
//
// # Last row
//
// A trailing row that never reaches the width is laid out at exactly the
// target height and left aligned. It is not stretched: stretching three
// holiday photos across 1920 pixels blows them up to 600 pixels tall next to
// 200 pixel neighbours, which looks like a bug and is the single most
// complained about behaviour of this layout family. It is also not
// distributed or centred, because the reading order of the gallery is left to
// right and a centred last row breaks the column alignment of the item above
// it.
func (ix *Index) fillRow(st *galleryState, i int) (end int, rowH float64) {
	p := st.params
	n := st.n
	var sum float64
	for j := i; j < n; j++ {
		prev := sum
		sum += ix.aspectOf(j, p)
		m := j - i + 1
		avail := p.Width - p.Gap*float64(m-1)
		if avail <= 0 {
			// Degenerate: the gaps alone exceed the width. One item per
			// row at the target height is the only non-negative answer.
			return i + 1, p.TargetRowHeight
		}
		h := avail / sum
		if h > p.TargetRowHeight {
			continue
		}
		if m > 1 {
			prevAvail := p.Width - p.Gap*float64(m-2)
			prevH := prevAvail / prev
			if prevH-p.TargetRowHeight < p.TargetRowHeight-h {
				return j, prevH
			}
		}
		return j + 1, h
	}
	return n, p.TargetRowHeight
}

func (ix *Index) commit() {
	if ix.cur != nil {
		ix.spare = ix.cur
	}
	ix.cur = ix.pend
	ix.pend = nil
	ix.b.active = false
	ix.version++
}

func (ix *Index) cancel() {
	if ix.pend != nil {
		ix.spare = ix.pend
		ix.pend = nil
	}
	ix.b.active = false
	ix.b.phase = phaseDone
	ix.b.pos = 0
}

func (ix *Index) take() *galleryState {
	if ix.spare != nil {
		st := ix.spare
		ix.spare = nil
		return st
	}
	return &galleryState{}
}

// aspectOf is the effective aspect ratio of item i under p: the raw ratio, or
// the provisional one when the dimensions are unknown, clamped either way.
//
// It is computed per build rather than stored so that a change of the
// provisional ratio or of the clamp is an ordinary rebuild. A stored effective
// ratio would be lossy in exactly the case that matters: clamping a 1x20000
// scan to 0.2 and later widening the clamp could not recover the real number
// without going back to the collection.
func (ix *Index) aspectOf(i int, p GalleryParams) float64 {
	a := float64(ix.aspect[i])
	if !(a > 0) {
		a = p.ProvisionalAspect
	}
	if a < p.MinAspect {
		return p.MinAspect
	}
	if a > p.MaxAspect {
		return p.MaxAspect
	}
	return a
}

// rawAspect turns a reported pixel size into the stored ratio. Zero means "not
// known yet", which is exactly what asset.Metadata leaves behind before
// probing; see the project plan, section 9.
func rawAspect(w, h uint32) float32 {
	if w == 0 || h == 0 {
		return 0
	}
	return float32(float64(w) / float64(h))
}

// columnsFor derives the masonry column count and width. The count is the
// largest one whose columns are still at least MinColumnWidth wide, and there
// is always at least one column even when the viewport is narrower than the
// minimum: a gallery that reports no columns would report no content and the
// scroll container would show an empty document instead of a too wide one.
func columnsFor(p GalleryParams) (cols int, colW float64) {
	if p.Width <= 0 {
		return 1, 0
	}
	cols = int((p.Width + p.Gap) / (p.MinColumnWidth + p.Gap))
	if cols < 1 {
		cols = 1
	}
	colW = (p.Width - p.Gap*float64(cols-1)) / float64(cols)
	if colW <= 0 {
		return 1, p.Width
	}
	return cols, colW
}

// shortest returns the index of the smallest bottom, ties going to the lowest
// index so that the layout is deterministic. A linear scan beats a heap here:
// a 1920 pixel viewport with a 160 pixel minimum has twelve columns, and
// twelve float64 compares are a handful of nanoseconds while a heap costs a
// sift plus an indirection per item.
func shortest(bottoms []float64) int {
	best, bv := 0, bottoms[0]
	for c := 1; c < len(bottoms); c++ {
		if bottoms[c] < bv {
			best, bv = c, bottoms[c]
		}
	}
	return best
}

// --- small slice and bitset helpers -----------------------------------------

func grow32(s []float32, n int) []float32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float32, n)
}

func grow64(s []float64, n int) []float64 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float64, n)
}

func grow32i(s []int32, n int) []int32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]int32, n)
}
