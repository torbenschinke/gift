package ebiten

import (
	"image"
	"math"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/internal/text"
	"github.com/worldiety/gift/render"
)

// AtlasConfig configures a [GlyphAtlas]. The zero value selects the defaults
// below and is what [NewRenderer] uses.
type AtlasConfig struct {
	// PageSize is the edge length of one atlas page in pixels. Zero selects
	// [DefaultAtlasPageSize].
	PageSize int
	// MaxPages is the hard upper bound on the number of pages. Zero selects
	// [DefaultAtlasMaxPages]. This is the bounded page count the project
	// plan, section 11, asks for: the atlas may not grow to fill GPU memory
	// just because an application scrolled past a lot of text.
	MaxPages int
	// MaxBytes is the byte budget across all pages. Zero selects
	// PageSize*PageSize*4*MaxPages, that is "as many pages as MaxPages
	// allows". A smaller value evicts before the page count is reached.
	MaxBytes int
	// MaxAge is the number of drawn frames a page may go untouched before
	// [GlyphAtlas.Tick] deallocates it. Zero selects [DefaultAtlasMaxAge]; a
	// negative value disables age eviction.
	MaxAge int
}

// Defaults for [AtlasConfig].
//
// # They were re-measured for the device density and left alone
//
// Rasterising in device pixels — the project plan, section 18 — multiplies the
// area of every mask by the square of the density, so the obvious worry is
// that these numbers stop being enough at 2x. They do not, and
// TestAtlasBudgetAtTwoX is the measurement rather than the opinion: the whole
// printable ASCII range at the five sizes a desktop application uses packs
// into 386 KiB of coverage at 1x, 1.22 MiB at 2x and 2.55 MiB at 3x. Up to and
// including 2x that is one page of four; 3x is the first density that needs a
// second one, and four pages still hold it with room to spare.
//
// Growing the page for high density displays was considered and rejected. A
// page is allocated whole, so a larger default would cost every 1x
// installation — the Raspberry Pi of section 1 — four times the GPU memory in
// order to spare a 2x machine an allocation it is not making.
const (
	// DefaultAtlasPageSize is 1024, which holds roughly four thousand glyphs
	// at a body text size and is comfortably below every GL ES 3.0 minimum
	// for a texture edge.
	DefaultAtlasPageSize = 1024
	// DefaultAtlasMaxPages is four, that is 16 MiB of coverage at the default
	// page size.
	DefaultAtlasMaxPages = 4
	// DefaultAtlasMaxAge is 600 drawn frames, ten seconds at sixty hertz. It
	// is the same number internal/text uses for its shaping cache, on purpose:
	// a page whose glyphs have not been asked for in ten seconds is holding
	// memory for a screen nobody is looking at.
	DefaultAtlasMaxAge = 600
)

// glyphKey identifies one rasterised glyph.
//
// The size is in *device* pixels: it is what [GlyphAtlas.Lookup] computed from
// the glyph's nominal size and the scale of the transform it is drawn under,
// so the same label on a 1x and on a 2x display occupies two entries with two
// bitmaps. That is deliberate and is the point of the project plan,
// section 18; the alternative, one entry stretched by the transform, is the
// blur this key was changed to remove.
//
// The size is a quantised integer and not a float32, for the two reasons the
// shaping cache gives for the same choice: a NaN key would never equal itself
// and would leak an entry per lookup, and two sizes closer together than the
// 1/64 pixel the shaper can represent produce identical output and must share
// an entry.
//
// There is deliberately no subpixel phase in this key. The project plan,
// section 7, rules out subpixel positioning, internal/text rounds glyph
// origins to whole pixels, and a phase field here would silently multiply the
// number of entries by four while claiming to be an optimisation.
type glyphKey struct {
	font render.FontID
	size int32
	id   render.GlyphID
}

// glyphEntry is one packed glyph.
//
// It is plain old data in a flat slice, addressed by index, for the same
// reason internal/scene and the shaping cache are: a lookup plus a use stamp
// is a handful of integer writes with nothing for the collector to scan, and
// that is what keeps the atlas hit path allocation free.
type glyphEntry struct {
	// page is the index of the page this glyph lives on, or -1 while the
	// entry is free.
	page int32
	// x, y, w, h are the rectangle inside the page, in pixels.
	x, y, w, h int32
	// left and top are [text.GlyphMask.Left] and Top: the offset from the
	// glyph origin to the top left of the bitmap.
	left, top int32
	// inked is false for a glyph with no coverage, a space for instance. Such
	// an entry is cached too, because the alternative is rasterising every
	// space of every line on every frame in order to rediscover that it is
	// empty.
	inked bool
	// rejected marks an entry that stands for a glyph the packer could not
	// find room for. It is cached for exactly the same reason a blank is: the
	// alternative is rasterising the outline again on every frame in order to
	// rediscover that it still does not fit. See [GlyphAtlas.insert].
	rejected bool
}

// isPageless reports whether the entry occupies no page, which is true of a
// blank and of a rejection.
func (e glyphEntry) isPageless() bool { return e.page < 0 }

// atlasPage is one GPU texture and the shelf packer that fills it.
type atlasPage struct {
	img *eb.Image
	// penX is the left edge of the free space on the current shelf, shelfY
	// the top of that shelf and shelfH its height.
	penX, shelfY, shelfH int32
	// open says whether a shelf has been opened on this page yet. See
	// [atlasPage.fit].
	open bool
	// used is the frame number this page was last read from.
	used uint64
	// entries is the number of live glyphs on the page. It is what makes a
	// page eviction's cost visible in the counters.
	entries int
}

// GlyphAtlas packs rasterised glyph coverage into a bounded set of
// [eb.Image] pages.
//
// # Packing
//
// Shelf packing: glyphs are placed left to right on a horizontal shelf whose
// height is the height of the first glyph placed on it, and a glyph that does
// not fit the remaining width opens a new shelf below.
//
// It was chosen over skyline or maximal-rectangles packing for three reasons.
// Text is the one workload shelf packing is nearly optimal for, because every
// glyph of one font at one size has almost the same height and a shelf
// therefore fills to within a pixel or two. An insert is O(1) over three
// integers rather than a walk of a free rectangle list, and the miss path
// already pays for a rasterisation, so a packer that is denser but slower buys
// nothing measurable. And the project plan, section 11, says explicitly that a
// bespoke atlas engine is not to be built ahead of a measurement that asks for
// one.
//
// The price is stated rather than hidden: a shelf packer cannot reclaim a hole
// in the middle of a page. That is why eviction works on whole pages; see
// below.
//
// # Eviction
//
// Two triggers, both of which the project plan, section 7, names:
//
//   - Bytes and page count. When a glyph needs room, every page is full and
//     the page budget is exhausted, the least recently used page is evicted.
//   - Use age. [GlyphAtlas.Tick] evicts every page that has not been read
//     from for [AtlasConfig.MaxAge] drawn frames.
//
// Evicting means dropping every entry on the page and calling
// [eb.Image.Deallocate] on its image, which is the explicit release the
// project plan, section 11, requires instead of waiting for a finaliser.
//
// A page that was read from during the frame in progress is never evicted:
// vertices referencing it may already be queued. If every page is in that
// state and none can be freed, the glyph is not added and
// [AtlasStats.Rejected] counts it. That is a bounded, visible failure instead
// of an unbounded texture allocation.
//
// # Colour
//
// The atlas stores coverage, never colour. A mask is written with all four
// channels set to the coverage value, which is premultiplied white, so
// multiplying it by the premultiplied vertex colour of the operation yields a
// correctly premultiplied result for any colour. One entry therefore serves
// every colour the same glyph is ever drawn in.
type GlyphAtlas struct {
	cfg AtlasConfig

	pages   []*atlasPage
	index   map[glyphKey]int32
	entries []glyphEntry
	free    []int32

	// frame is the number of drawn frames, used as the page age stamp.
	frame uint64
	bytes int
	// pageless is the number of live entries that occupy no page: blanks and
	// cached rejections. It is tracked so that an eviction can drop them
	// without a second scan of the index.
	pageless int

	rast *text.Rasterizer
	mask text.GlyphMask
	// staging is the reused RGBA upload buffer.
	staging []byte

	stats AtlasStats

	// onDeallocate, if non nil, is called with a page image immediately
	// before it is deallocated. It exists so that a test can assert that the
	// explicit release of the project plan, section 11, actually happens
	// rather than merely being claimed in a comment.
	onDeallocate func(*eb.Image)
}

// NewGlyphAtlas returns an empty atlas. It allocates no GPU memory until the
// first glyph is added.
func NewGlyphAtlas(cfg AtlasConfig) *GlyphAtlas {
	if cfg.PageSize <= 0 {
		cfg.PageSize = DefaultAtlasPageSize
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = DefaultAtlasMaxPages
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = cfg.PageSize * cfg.PageSize * 4 * cfg.MaxPages
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = DefaultAtlasMaxAge
	}
	return &GlyphAtlas{
		cfg:   cfg,
		index: make(map[glyphKey]int32, 512),
		rast:  text.NewRasterizer(),
	}
}

// glyphPad is the gap kept between two packed glyphs, in pixels.
//
// The sampler is nearest and the mapping from atlas pixel to screen pixel is
// one to one, so no neighbour can bleed in through filtering. The gap is kept
// anyway because it costs a fraction of a per cent of the page and it is the
// difference between "this is correct" and "this is correct as long as nobody
// ever adds a scale factor".
const glyphPad = 1

// Lookup returns the entry index for a glyph, rasterising and packing it if it
// is not cached yet, and reports whether the glyph could be resolved at all.
//
// # The size in the key is a device size
//
// scale is how many device pixels one unit of the glyph's coordinate space
// covers — the device density of the project plan, section 18, composed with
// whatever transform the text sits under — and the size that enters the key,
// the rasteriser and the packed bitmap is [render.Glyph.Size] times it. A 16
// point label therefore occupies a 16 pixel mask on a 1x display and a 32
// pixel mask on a 2x one, and the two are separate entries under separate
// keys rather than one entry drawn at two sizes.
//
// That is what makes text crisp instead of merely larger, and it is the
// reason the glyph material may keep [eb.FilterNearest]: the mask is
// rasterised for the exact number of pixels it is drawn into, so the sampler
// never interpolates. See [Renderer.appendGlyphs] for the other half, which
// transforms the glyph origin and leaves the extent alone.
//
// A scale of 1 reproduces the previous behaviour exactly, down to the key:
// Size*1 is Size and [quantize] is the same function on the same number.
//
// The hit path is a map lookup on a comparable struct key plus one integer
// write to the page use stamp, and allocates nothing. The multiplication is
// one float32 multiply per glyph, hoisted out of the loop by the caller for
// the scale itself.
func (a *GlyphAtlas) Lookup(g render.Glyph, scale float32) (int32, bool) {
	if !(g.Size > 0) {
		return 0, false
	}
	if !(scale > 0) || !(scale < math.MaxInt32) {
		// A singular or non-finite transform. The op will collapse anyway;
		// rasterising at the nominal size merely keeps the key finite.
		scale = 1
	}
	size := g.Size * scale
	k := glyphKey{font: g.Font, size: quantize(size), id: g.ID}
	if i, ok := a.index[k]; ok {
		a.stats.Hits++
		e := &a.entries[i]
		if e.rejected {
			// Known not to fit. The caller draws nothing, exactly as before,
			// but the outline is not rasterised again; see insert.
			a.stats.Rejected++
			return 0, false
		}
		if e.page >= 0 {
			a.pages[e.page].used = a.frame
		}
		return i, true
	}
	a.stats.Misses++
	// Resolving the font id happens here and not on the hit path on purpose:
	// it takes a read lock in internal/text, and the hit path must stay a
	// plain map lookup. See [text.Lookup].
	f := text.Lookup(text.FontID(g.Font))
	if f == nil {
		a.stats.Rejected++
		return 0, false
	}
	return a.insert(k, f, size, g.ID)
}

// Entry returns the packed rectangle and bearing of an entry obtained from
// [GlyphAtlas.Lookup].
func (a *GlyphAtlas) Entry(i int32) glyphEntry { return a.entries[i] }

// Page returns the image an entry lives on.
func (a *GlyphAtlas) Page(i int32) *eb.Image { return a.pages[a.entries[i].page].img }

// insert rasterises and packs a glyph that missed.
func (a *GlyphAtlas) insert(k glyphKey, f *text.Font, size float32, id render.GlyphID) (int32, bool) {
	inked := a.rast.Glyph(f, text.GlyphID(id), size, &a.mask)
	if !inked {
		// A blank glyph still gets an entry, on no page at all. Rasterising
		// every space of every line on every frame to rediscover that it is
		// empty would be the single most expensive way to draw nothing.
		i := a.alloc()
		a.entries[i] = glyphEntry{page: -1}
		a.index[k] = i
		a.pageless++
		a.stats.Blanks++
		return i, true
	}

	w, h := int32(a.mask.Width), int32(a.mask.Height)
	p, px, py, ok := a.place(w, h)
	if !ok {
		// The rejection is cached, keyed like a blank and on no page at all.
		//
		// Without this the atlas rasterised the outline, threw it away and
		// recorded nothing, so the next frame rasterised it again — for ever,
		// for every glyph that did not fit. A full atlas measured 350
		// discarded outlines per frame, with no symptom other than a counter
		// climbing at sixty times the rate anybody would expect. The work was
		// invisible because it produced no pixels and no allocation, only CPU
		// time.
		//
		// A cached rejection is dropped when a page is evicted — see
		// [GlyphAtlas.evictPage] — so the glyph is retried once per
		// eviction cycle instead of once per frame. The honest limitation: a
		// scene that is over budget and completely static never evicts
		// anything, so those glyphs stay missing until something else asks
		// for room. That is the same outcome as before, minus the wasted
		// rasterisation, and [AtlasStats.RejectedRasterised] is the number
		// that says it is happening.
		i := a.alloc()
		a.entries[i] = glyphEntry{page: -1, rejected: true}
		a.index[k] = i
		a.pageless++
		a.stats.Rejected++
		a.stats.RejectedRasterised++
		return 0, false
	}

	a.upload(a.pages[p], px, py, w, h)

	i := a.alloc()
	a.entries[i] = glyphEntry{
		page: p, x: px, y: py, w: w, h: h,
		left: int32(a.mask.Left), top: int32(a.mask.Top),
		inked: true,
	}
	a.index[k] = i
	a.pages[p].entries++
	a.pages[p].used = a.frame
	a.stats.Rasterised++
	a.stats.UploadedBytes += uint64(w) * uint64(h) * 4
	return i, true
}

// place finds room for a w by h bitmap, opening shelves, adding pages and
// evicting pages as needed. It returns the page index and the position.
func (a *GlyphAtlas) place(w, h int32) (page int32, x, y int32, ok bool) {
	side := int32(a.cfg.PageSize)
	if w+2*glyphPad > side || h+2*glyphPad > side {
		// A single glyph larger than a whole page. Refusing it is the only
		// bounded answer; growing the page size for one glyph would make the
		// budget depend on the largest font size the application ever used.
		return 0, 0, 0, false
	}
	for {
		for i, p := range a.pages {
			if x, y, ok := p.fit(w, h, side); ok {
				return int32(i), x, y, true
			}
		}
		if a.addPage() {
			continue
		}
		if !a.evictLRU() {
			return 0, 0, 0, false
		}
	}
}

// fit places a w by h bitmap on this page if it fits, advancing the shelf.
//
// open says whether a shelf has been opened at all. Without it the zero value
// shelfH == 0 made the first-fit test fail for every glyph, so the first shelf
// opened at y = glyphPad and the topmost row of every page was never used.
// Cosmetic — one row in a thousand and twenty four — but it is a row.
func (p *atlasPage) fit(w, h, side int32) (x, y int32, ok bool) {
	if p.open && p.penX+w+glyphPad <= side && h <= p.shelfH {
		x, y = p.penX, p.shelfY
		p.penX += w + glyphPad
		return x, y, true
	}
	// Open a new shelf below the current one. The first one starts at row
	// zero, not below an imaginary shelf of height zero.
	ny := int32(0)
	if p.open {
		ny = p.shelfY + p.shelfH + glyphPad
	}
	if ny+h+glyphPad > side {
		return 0, 0, false
	}
	p.shelfY, p.shelfH, p.penX, p.open = ny, h, w+glyphPad, true
	return 0, ny, true
}

// addPage appends a page if the page count and the byte budget allow it.
func (a *GlyphAtlas) addPage() bool {
	side := a.cfg.PageSize
	cost := side * side * 4
	if len(a.pages) >= a.cfg.MaxPages || a.bytes+cost > a.cfg.MaxBytes {
		return false
	}
	a.pages = append(a.pages, &atlasPage{img: eb.NewImage(side, side)})
	a.bytes += cost
	a.stats.PagesCreated++
	return true
}

// evictLRU deallocates the least recently used page that was not read from in
// the frame in progress, and reports whether it found one.
func (a *GlyphAtlas) evictLRU() bool {
	best, bestUsed := -1, uint64(math.MaxUint64)
	for i, p := range a.pages {
		if p.used == a.frame {
			continue
		}
		if p.used < bestUsed {
			best, bestUsed = i, p.used
		}
	}
	if best < 0 {
		return false
	}
	a.evictPage(int32(best))
	return true
}

// evictPage drops every glyph on a page and releases its image.
//
// The image is deallocated rather than reused, because [eb.Image.Deallocate]
// is terminal. A fresh image is created when the page is next needed, which is
// the explicit release the project plan, section 11, asks for; the alternative
// — keeping the texture and merely forgetting its contents — would mean the
// atlas never gives memory back at all.
func (a *GlyphAtlas) evictPage(idx int32) {
	p := a.pages[idx]
	dropped := 0
	for k, i := range a.index {
		e := a.entries[i]
		// Entries on the evicted page go, and so do the pageless ones: a
		// cached rejection has to be retried now that there is room, and a
		// blank that is never dropped makes Stats().Glyphs drift upwards for
		// the life of the process while claiming to be the current occupancy.
		// Both are cheap to rediscover and neither holds a byte of GPU
		// memory.
		if e.page != idx && !e.isPageless() {
			continue
		}
		delete(a.index, k)
		a.entries[i] = glyphEntry{page: -1}
		a.free = append(a.free, i)
		if e.isPageless() {
			a.pageless--
			continue
		}
		dropped++
	}
	if a.onDeallocate != nil {
		a.onDeallocate(p.img)
	}
	p.img.Deallocate()
	a.bytes -= a.cfg.PageSize * a.cfg.PageSize * 4
	a.pages = append(a.pages[:idx], a.pages[idx+1:]...)
	// Every entry above the removed page shifted down by one.
	for i := range a.entries {
		if a.entries[i].page > idx {
			a.entries[i].page--
		}
	}
	a.stats.PageEvictions++
	a.stats.GlyphEvictions += uint64(dropped)
}

// Tick advances the frame clock and evicts pages that have gone unread for
// longer than [AtlasConfig.MaxAge]. The backend calls it once per drawn frame,
// after the last draw call of that frame has been issued.
func (a *GlyphAtlas) Tick() {
	a.frame++
	if a.cfg.MaxAge < 0 {
		return
	}
	age := uint64(a.cfg.MaxAge)
	if a.frame <= age {
		return
	}
	deadline := a.frame - age
	for i := len(a.pages) - 1; i >= 0; i-- {
		if a.pages[i].used < deadline {
			a.evictPage(int32(i))
			a.stats.AgeEvictions++
		}
	}
}

// alloc returns a free entry slot, recycling one if there is any.
func (a *GlyphAtlas) alloc() int32 {
	if n := len(a.free); n > 0 {
		i := a.free[n-1]
		a.free = a.free[:n-1]
		return i
	}
	a.entries = append(a.entries, glyphEntry{page: -1})
	return int32(len(a.entries) - 1)
}

// upload writes the coverage mask into the page.
//
// The mask is a single channel and the target is RGBA, so the coverage is
// written into all four channels: that is premultiplied white, and
// premultiplied white times a premultiplied colour is that colour at that
// coverage. Writing it only into alpha would work with a dedicated shader and
// break under the plain textured pipeline, which is the one this backend uses
// for glyphs; writing a colour into the atlas is what the brief and the
// project plan, section 7, rule out and nothing here does it.
func (a *GlyphAtlas) upload(p *atlasPage, x, y, w, h int32) {
	n := int(w) * int(h) * 4
	if cap(a.staging) < n {
		a.staging = make([]byte, n)
	}
	buf := a.staging[:n]
	for i, c := range a.mask.Pix[:int(w)*int(h)] {
		j := i * 4
		buf[j], buf[j+1], buf[j+2], buf[j+3] = c, c, c, c
	}
	sub := p.img.SubImage(image.Rect(int(x), int(y), int(x+w), int(y+h))).(*eb.Image)
	sub.WritePixels(buf)
}

// AtlasStats are the glyph atlas counters. Like every other counter in gift
// they are plain numbers written in the frame path and read out of band; see
// the project plan, section 15.
type AtlasStats struct {
	// Hits and Misses are the outcomes of [GlyphAtlas.Lookup]. The hit ratio
	// is the number that says whether the atlas is doing its job: a steady
	// scene should sit at one after the first frame.
	Hits, Misses uint64
	// Rasterised is the number of glyph outlines turned into coverage, that
	// is the number of misses that produced ink.
	Rasterised uint64
	// Blanks is the number of cached entries with no coverage, spaces mostly.
	// They are cached precisely so that they do not show up in Rasterised
	// once per frame.
	Blanks uint64
	// UploadedBytes is the total coverage uploaded to the GPU.
	UploadedBytes uint64
	// PagesCreated and PageEvictions are the page lifecycle. AgeEvictions is
	// the subset of evictions caused by [GlyphAtlas.Tick] rather than by
	// pressure, which is the distinction between "the budget is too small"
	// and "this text is no longer on screen".
	PagesCreated, PageEvictions, AgeEvictions uint64
	// GlyphEvictions is the number of entries dropped with their pages.
	GlyphEvictions uint64
	// Rejected counts lookups that produced no glyph: an outline larger than
	// a whole page, a frame that filled every page it had, or a repeat of
	// either. A non zero value means text is missing from the screen and the
	// budget needs raising. It climbs once per lookup, so in a steady scene
	// it climbs once per frame per missing glyph.
	Rejected uint64
	// RejectedRasterised is the subset of Rejected that actually cost a
	// rasterisation, that is the number of *distinct* glyphs the packer has
	// turned away since the last eviction.
	//
	// This is the number that says whether the atlas is wasting CPU or merely
	// reporting a budget problem. It used to be equal to Rejected, because a
	// turned away glyph was rasterised and discarded on every frame for ever
	// — 350 outlines per frame in a measured probe, invisible because they
	// produced neither pixels nor allocations. A rejection is now cached like
	// a blank, so RejectedRasterised should stay near the number of distinct
	// missing glyphs while Rejected keeps climbing; if the two rise together,
	// something is dropping the cached rejections every frame.
	RejectedRasterised uint64
	// Pages, Glyphs and Bytes are the current occupancy. Glyphs counts index
	// entries, which includes the pageless ones — blanks and cached
	// rejections — and those are dropped with the next page eviction, so the
	// number is the current occupancy rather than a high water mark.
	Pages, Glyphs int
	Bytes         int
}

// Stats returns a snapshot of the atlas counters.
func (a *GlyphAtlas) Stats() AtlasStats {
	s := a.stats
	s.Pages = len(a.pages)
	s.Glyphs = len(a.index)
	s.Bytes = a.bytes
	return s
}

// quantize rounds a size to 1/64 pixel, exactly as internal/text does, so that
// the atlas key and the shaping cache key can never disagree about whether two
// sizes are the same size.
func quantize(v float32) int32 {
	q := math.Round(float64(v) * 64)
	if q < math.MinInt32 || q > math.MaxInt32 || math.IsNaN(q) {
		return 0
	}
	return int32(q)
}
