package ui

import (
	"fmt"
	"math"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var (
	galleryType     = gift.RegisterType("ui.ImageGallery")
	galleryTileType = gift.RegisterType("ui.GalleryTile")
)

// --- the layout declaration --------------------------------------------------

// GalleryLayout is the algorithm and the metrics a gallery arranges its items
// with. Obtain one from [Masonry] or [Justified] and refine it with the
// methods below; the zero value is a masonry layout with default metrics.
//
// It is a plain value and not a view. Switching between the two at runtime is
// therefore an ordinary state change in the application — a different
// GalleryLayout comes out of the next build — and the gallery notices that its
// parameters changed, starts an incremental reflow and discards whatever
// reflow was already in flight. The project plan, section 10, asks for exactly
// that: "Ergebnisse veralteter Layouts werden verworfen."
type GalleryLayout struct {
	mode              layout.GalleryMode
	gap               float64
	minColumnWidth    float64
	targetRowHeight   float64
	provisionalAspect float64
	minAspect         float64
	maxAspect         float64
}

// Masonry returns a layout with fixed column widths and variable item heights,
// placing each item into the currently shortest column.
func Masonry() GalleryLayout { return GalleryLayout{mode: layout.Masonry} }

// Justified returns a layout that fills rows to the full content width, giving
// every item in a row the same height. It is also known as the brick layout.
func Justified() GalleryLayout { return GalleryLayout{mode: layout.Justified} }

// IsMasonry reports whether l is a masonry layout.
func (l GalleryLayout) IsMasonry() bool { return l.mode == layout.Masonry }

// Gap sets the space between two adjacent items, horizontally and vertically.
// A negative value is treated as zero: overlapping tiles have no honest
// reading in a gallery.
func (l GalleryLayout) Gap(v float32) GalleryLayout { l.gap = float64(checkGap(v)); return l }

// MinColumnWidth sets the narrowest acceptable masonry column. The column
// count is the largest one whose columns are still at least this wide. It is
// ignored by a justified layout.
func (l GalleryLayout) MinColumnWidth(v float32) GalleryLayout {
	l.minColumnWidth = float64(v)
	return l
}

// RowHeight sets the row height a justified layout aims for. The actual height
// varies per row, because a row that fills the width exactly cannot also have
// a prescribed height. It is ignored by a masonry layout.
func (l GalleryLayout) RowHeight(v float32) GalleryLayout {
	l.targetRowHeight = float64(v)
	return l
}

// ProvisionalAspect sets the width over height ratio used for an item whose
// dimensions are not known yet. The default is 1.
//
// It is a real layout input and not a fallback nobody sees: the project plan,
// section 10, requires unknown sizes to be laid out provisionally rather than
// waited for, so at a cold start every item uses this value and the document
// extent is entirely made of it.
func (l GalleryLayout) ProvisionalAspect(v float32) GalleryLayout {
	l.provisionalAspect = float64(v)
	return l
}

// AspectClamp bounds every aspect ratio, known or provisional. The defaults
// are 0.2 and 5.
//
// Without it a 1 by 20 000 scan produces a masonry entry taller than any
// viewport, which cannot be scrolled past sensibly, and a justified row of one
// item three pixels high.
func (l GalleryLayout) AspectClamp(lo, hi float32) GalleryLayout {
	l.minAspect, l.maxAspect = float64(lo), float64(hi)
	return l
}

// params turns the declaration into the internal parameter set for a content
// width. Defaulting happens inside internal/layout, on purpose: there is then
// one place that decides what a zero field means.
func (l GalleryLayout) params(width float64) layout.GalleryParams {
	return layout.GalleryParams{
		Mode:              l.mode,
		Width:             width,
		Gap:               l.gap,
		MinColumnWidth:    l.minColumnWidth,
		TargetRowHeight:   l.targetRowHeight,
		ProvisionalAspect: l.provisionalAspect,
		MinAspect:         l.minAspect,
		MaxAspect:         l.maxAspect,
	}
}

// --- tile appearance ---------------------------------------------------------

// TileStyle is the look of one gallery tile.
//
// Every tile is a placeholder in this step of the project plan: step 3 is the
// gallery without I/O and there is no picture to draw yet. What the style
// describes is therefore the *placeholder*, and step 4 will draw the decoded
// thumbnail in its place and keep the rest — the selection border, the cursor
// ring and the provisional marker are properties of the tile and not of the
// picture.
type TileStyle struct {
	// CornerRadius rounds the placeholder and its borders.
	CornerRadius float32

	// Palette is the set of placeholder fills. The entry is chosen by a hash
	// of the stable [asset.ID], never by the slot, so a tile that is recycled
	// changes colour with the item and a tile that scrolls out and back comes
	// back the same colour. That is the visible half of the recycling
	// contract of the project plan, section 13: "keine falschen Bilder nach
	// Tile-Recycling".
	//
	// An empty palette uses one neutral grey.
	Palette []Color

	// Provisional fills a tile whose real dimensions have not arrived yet, so
	// that the difference between a laid out guess and a laid out fact is
	// visible rather than claimed. A transparent value uses the palette.
	Provisional Color

	// Selected is stroked inside a selected tile.
	Selected Border

	// Cursor is stroked inside the tile the keyboard cursor is on. It is
	// separate from Selected because the two are independent: control-arrow
	// moves the cursor without selecting, shift-arrow selects a range the
	// cursor is only one end of.
	Cursor Border
}

var defaultTileStyle = TileStyle{
	CornerRadius: 4,
	Palette: []Color{
		RGB(96, 110, 140), RGB(116, 96, 140), RGB(96, 140, 124),
		RGB(140, 118, 96), RGB(140, 96, 104), RGB(104, 130, 140),
	},
	Provisional: RGBA(255, 255, 255, 26),
	Selected:    Border{Width: 3, Color: RGB(96, 160, 255)},
	Cursor:      Border{Width: 2, Color: RGB(255, 255, 255)},
}

// --- the retained model ------------------------------------------------------

// DefaultGalleryRebuildBudget is how many item placements one incremental
// reflow step performs per layout pass.
//
// A full masonry reflow of n items costs about 2n of these, so the default
// spreads a 100 000 entry reflow over about seven passes. The project plan,
// section 10, permits a reflow to be O(N) but not to happen in one frame:
// "sie werden ausserhalb des Frame-Hotpaths oder inkrementell berechnet".
//
// The *first* build of a layout is not chunked, because there is nothing to
// show until it finishes and seven blank frames are worse than one slow one.
// Every reflow after it is, because the previous layout stays on screen and
// answers queries while the new one is built.
var DefaultGalleryRebuildBudget = 32768

// Gallery is the retained model of a virtualised image gallery: the catalogue,
// the selection, the spatial index and the pool of recycled tiles.
//
// # Why the application owns it and not the view
//
// The project plan, section 10, sketches the gallery as ui.ImageGallery(photos)
// — a view built from a collection. The sketch is an API sketch and this is
// where it had to give: a view value is rebuilt and thrown away on every build,
// and the thing that must survive is a 2.4 MB spatial index over 100 000
// entries and a pool of tiles whose bindings are the whole point. Reconstructing
// either per build would make a rebuild O(N) and would throw away the recycling
// evidence with it.
//
// So the split is explicit. The Gallery is a long lived object the application
// creates once and holds, exactly like the [asset.Collection] it wraps;
// [ImageGallery] is the short lived declaration that says how it should look
// right now. That also puts the durable selection where the project plan,
// section 5, requires it — "beim Collection-Owner, nicht im recycelten Tile" —
// without any machinery to put it there.
//
// # Ownership
//
// A Gallery belongs to the UI executor. It is not safe for concurrent use.
type Gallery struct {
	coll *asset.Collection
	sel  *asset.Selection

	ix *layout.Index
	// indexed is the collection the spatial index was filled from and
	// collVersion its [asset.Collection.Version] at that moment.
	//
	// Both, not just the version. Two different collections start at version
	// one, so a [Gallery.SetCollection] to a fresh catalogue of the same age
	// is invisible to a version comparison — and the failure is not "nothing
	// happens": the index still describes the old catalogue while the new one
	// answers the ID lookups, so a tile is bound to position 173 of a
	// catalogue that has fewer entries than that.
	indexed     *asset.Collection
	collVersion uint64
	// params are the parameters of the committed or in flight layout, and
	// haveParams says whether they mean anything.
	params     layout.GalleryParams
	haveParams bool
	// needReflow is set when something invalidated the layout and a rebuild
	// has not been started for it yet.
	needReflow bool
	// firstLayout is true until one layout has been committed. It is what
	// makes the initial build unchunked; see [DefaultGalleryRebuildBudget].
	firstLayout bool

	// spec, style, overscan, onSelect and onActivate are refreshed from the
	// view on every build. They are the declaration half.
	spec       GalleryLayout
	style      TileStyle
	overscan   float64
	onSelect   func(asset.ID)
	onActivate func(asset.ID)

	// slots is the tile pool. Its length is the number of tile nodes that
	// exist, which is bounded by the viewport and never by the catalogue.
	slots []tileSlot
	// tiles are the retained halves of the tile views, one per slot, created
	// once and reused across builds so that a rebuild of the pool does not
	// allocate a node object per tile.
	tiles []*tileNode
	// want is the reusable visible set buffer. [layout.Index.Visible] is only
	// allocation free because it appends to a caller owned slice, and the
	// project plan, section 10, calls that "ein Vertrag an den Aufrufer" that
	// must be carried all the way into the gallery. This field is where it is
	// carried to.
	want []layout.Visible
	// assign[i] is the slot visible item i was bound to, or -1.
	assign []int32

	// gen is the request generation counter. It increases every time a slot
	// is bound to a different item. It is deliberately *not* the content
	// revision and not the scene node generation; the project plan, section
	// 9, keeps the three apart, and this is the one that will label in flight
	// image requests in step 4 so that a result arriving after a recycle is
	// dropped instead of painted into the wrong tile.
	gen uint64

	// anchor is the split scroll anchor of the project plan, section 10: the
	// stable ID lives here, in the collection's world, and the local offset
	// lives in the layout index's world.
	anchorID    asset.ID
	anchorLocal float64
	haveAnchor  bool
	// restoreAnchor asks the *next committed* layout to put the viewport back
	// on the anchored item, and anchorAtVersion is the layout version at the
	// time of the request.
	//
	// The version is what makes the request survive a chunked reflow. Without
	// it the restore would fire on the first pass of the reflow, against the
	// layout that is still committed and in which nothing has moved, and
	// would then be consumed before the layout that actually moved everything
	// arrived. That is a one line bug with a symptom — "the anchor works for
	// small galleries and not for large ones" — that would take a day to find.
	restoreAnchor   bool
	anchorAtVersion uint64

	// reveal is the item a keyboard move asked to be brought into view, or
	// -1. It is an item position and not an ID because it is consumed in the
	// same or the next layout pass, before anything can move.
	reveal int

	// viewport and desiredSlots are the last measured viewport height and the
	// pool size the last layout pass wanted.
	viewport     float64
	desiredSlots int

	// rebuildBudget is the per pass chunk size; see
	// [DefaultGalleryRebuildBudget].
	rebuildBudget int

	// invalidate marks the gallery node for layout. It is handed over by the
	// first layout pass — see [gift.LayoutContext.Invalidator] — and is what
	// lets a mutation of the model reach the frame loop at all: a correction
	// batch changes no view, writes no state and would otherwise sit in the
	// catalogue until something else happened to cause a layout.
	invalidate func()
}

// Invalidate marks the gallery for another layout pass.
//
// Every mutating method here calls it, so an application normally does not
// have to. It is exported for the case where the application changed something
// the gallery reads but does not own — the contents of the [asset.Selection]
// it shares with another view, say.
func (g *Gallery) Invalidate() {
	if g.invalidate != nil {
		g.invalidate()
	}
}

// NewGallery returns a gallery over c with an empty selection.
//
// It does no layout work: the index is filled and the first layout is computed
// on the first layout pass, when the viewport width is known. c must not be
// nil.
func NewGallery(c *asset.Collection) *Gallery {
	if c == nil {
		panic("gift/ui: NewGallery with a nil collection")
	}
	return &Gallery{
		coll:          c,
		sel:           asset.NewSelection(),
		ix:            layout.NewIndex(),
		firstLayout:   true,
		needReflow:    true,
		reveal:        -1,
		rebuildBudget: DefaultGalleryRebuildBudget,
	}
}

// Collection returns the catalogue this gallery shows.
func (g *Gallery) Collection() *asset.Collection { return g.coll }

// SetCollection replaces the catalogue. The selection is kept and pruned, so a
// reload of the same pictures keeps them selected and a switch to a different
// set does not leave phantom entries behind.
func (g *Gallery) SetCollection(c *asset.Collection) {
	if c == nil {
		panic("gift/ui: Gallery.SetCollection with a nil collection")
	}
	g.coll = c
	g.sel.Prune(c)
	g.Invalidate()
}

// Selection returns the durable selection and keyboard cursor. It is an
// [asset.Selection] and lives with the catalogue, never in a tile; see the
// project plan, section 5.
func (g *Gallery) Selection() *asset.Selection { return g.sel }

// SetSelection replaces the selection object, for an application that shares
// one selection between two views of the same catalogue. A nil argument
// installs a fresh empty selection.
func (g *Gallery) SetSelection(s *asset.Selection) {
	if s == nil {
		s = asset.NewSelection()
	}
	g.sel = s
	g.Invalidate()
}

// ApplyCorrections folds a batch of late arriving dimensions into the
// catalogue and schedules one incremental reflow for the whole batch.
//
// It takes an anchor first, so the reflow keeps the viewport on the picture it
// was on. That is the decision the project plan, section 10, leaves to the
// gallery: "ein Korrekturbuendel verschiebt das bereits uebernommene Layout
// nicht. Wann der Inhalt nachrueckt, ist eine Ankerentscheidung und gehoert
// der Galerie."
//
// It returns the number of entries that actually changed. A batch that changed
// nothing schedules nothing.
func (g *Gallery) ApplyCorrections(cs []asset.Correction) int {
	n := g.coll.ApplyCorrections(cs)
	if n > 0 {
		g.requestAnchorRestore()
		g.Invalidate()
	}
	return n
}

// requestAnchorRestore asks the next committed layout to keep the viewport on
// the anchored entry. It is a no-op when there is no anchor yet, which is the
// case at the very first layout, where "keep the viewport where it was" means
// "at the top" and that is already where it is.
func (g *Gallery) requestAnchorRestore() {
	if !g.haveAnchor {
		return
	}
	g.restoreAnchor = true
	g.anchorAtVersion = g.ix.Version()
}

// Reveal asks for the entry with the given ID to be scrolled into view on the
// next layout pass, and reports whether the entry exists.
func (g *Gallery) Reveal(id asset.ID) bool {
	i, ok := g.coll.Find(id)
	if !ok {
		return false
	}
	g.reveal = i
	g.Invalidate()
	return true
}

// Ready reports whether a layout has been committed and the gallery has
// something to show.
func (g *Gallery) Ready() bool { return g.ix.Ready() }

// Reflowing reports whether an incremental reflow is in flight. While it is,
// the gallery keeps answering from the previous layout.
func (g *Gallery) Reflowing() bool { return g.ix.Rebuilding() }

// LayoutVersion is the version of the committed layout. It increases with
// every completed reflow and is what a consumer compares to decide that
// everything derived from item positions is stale.
func (g *Gallery) LayoutVersion() uint64 { return g.ix.Version() }

// ContentExtent is the document height of the committed layout, in logical
// pixels. It is a float64 and stays one: a 100 000 entry masonry document is
// routinely tens of millions of pixels tall, where float32 cannot represent
// consecutive integers at all.
func (g *Gallery) ContentExtent() float64 { return g.ix.ContentExtent() }

// Columns is the masonry column count of the committed layout, and zero for a
// justified one.
func (g *Gallery) Columns() int { return g.ix.Columns() }

// SlotCount is the number of tile nodes that exist.
//
// This is the number the virtualisation claim is made of: it is a function of
// the viewport and of nothing else, so it is the same for a hundred entries
// and for a hundred thousand. See TestGalleryLiveNodesAreBoundedByTheViewport.
func (g *Gallery) SlotCount() int { return len(g.slots) }

// VisibleCount is the number of entries the last layout pass found inside the
// viewport, before the pool size was taken into account.
func (g *Gallery) VisibleCount() int { return len(g.want) }

// Generation is the current request generation; see [TileBinding.Generation].
func (g *Gallery) Generation() uint64 { return g.gen }

// Anchor returns the stable ID and the local offset the viewport is currently
// pinned to.
//
// This is the split the project plan, section 10, insists on: the layout index
// stores no IDs, because that would cost eight bytes per entry plus an ID to
// position map the collection already has, and the collection stores no
// offsets. The pair is only ever assembled here.
func (g *Gallery) Anchor() (id asset.ID, local float64, ok bool) {
	return g.anchorID, g.anchorLocal, g.haveAnchor
}

// Compact releases the recycled scratch of the layout index. It is for a
// gallery that has gone off screen, not for the scroll path.
func (g *Gallery) Compact() { g.ix.Compact() }

// TileBinding is what one tile currently stands for.
//
// It is the honest answer to "which picture is in this tile", and it is a
// value taken out of the gallery rather than read off a node, because a tile
// node cannot carry it: [gift.Element.Label] and every other per node string
// is written during a build, and a recycled tile is rebound during *layout*.
// A test that asked a node what it shows would be reading the identity of
// whatever item that slot held when the pool was last resized.
type TileBinding struct {
	// Slot is the position in the tile pool. It is storage, not identity, and
	// is here only so that a test can show the two are different.
	Slot int
	// Item is the position in the catalogue.
	Item int
	// ID is the stable identity of the entry.
	ID asset.ID
	// Revision is the content version of the entry at the time of binding;
	// see [asset.Metadata.Revision].
	Revision string
	// Generation is the request generation of this binding. It increases
	// every time the slot is bound to a different item, and never otherwise,
	// so a result labelled with an older generation belongs to a picture this
	// tile no longer shows.
	Generation uint64
	// Provisional reports that the entry is laid out with a guessed aspect
	// ratio because its real dimensions have not arrived.
	Provisional bool
	// DocX, DocY, DocW and DocH are the document rectangle of the tile, in
	// float64 logical pixels. They are not device coordinates and the
	// conversion to float32 happens only after the viewport origin has been
	// subtracted; see the project plan, section 10.
	DocX, DocY, DocW, DocH float64
}

// Bindings appends the current binding of every bound tile to dst and returns
// the extended slice, in slot order.
func (g *Gallery) Bindings(dst []TileBinding) []TileBinding {
	for i := range g.slots {
		s := &g.slots[i]
		if !s.bound {
			continue
		}
		dst = append(dst, s.binding(i))
	}
	return dst
}

// BindingOf returns the tile the entry with the given ID is currently in.
func (g *Gallery) BindingOf(id asset.ID) (TileBinding, bool) {
	for i := range g.slots {
		s := &g.slots[i]
		if s.bound && s.id == id {
			return s.binding(i), true
		}
	}
	return TileBinding{}, false
}

// ItemRect returns the document rectangle of catalogue entry i in the
// committed layout, whether or not it has a tile.
func (g *Gallery) ItemRect(i int) (x, y, w, h float64, ok bool) {
	r, ok := g.ix.ItemRect(i)
	return r.X, r.Y, r.W, r.H, ok
}

// tileSlot is one entry of the tile pool: the binding, not the node.
type tileSlot struct {
	bound       bool
	provisional bool
	item        int
	id          asset.ID
	revision    string
	gen         uint64
	rect        layout.DocRect

	// claimed is per pass scratch of the binding algorithm.
	claimed bool
}

func (s *tileSlot) binding(slot int) TileBinding {
	return TileBinding{
		Slot: slot, Item: s.item, ID: s.id, Revision: s.revision,
		Generation: s.gen, Provisional: s.provisional,
		DocX: s.rect.X, DocY: s.rect.Y, DocW: s.rect.W, DocH: s.rect.H,
	}
}

// --- the view ----------------------------------------------------------------

// GalleryView is the declaration half of a gallery: how the retained [Gallery]
// should look and behave right now. Create one with [ImageGallery].
//
// # It needs a bounded height
//
// Like [ScrollView], and for the same reason: a stack measures an inflexible
// child with an unbounded main axis — rule 1 of the overflow model of the
// project plan, section 7 — and a viewport as tall as its content shows
// everything and scrolls nothing. Give it a [GalleryView.Flex], a
// [GalleryView.Frame] or a [GalleryView.MaxHeight]. A gallery with an
// unbounded height reports a zero viewport, binds no tiles and is blank, which
// is the loud version of the failure.
//
// # What it costs to scroll
//
// More than an ordinary scroller and much less than a rebuild. A virtualising
// container declares [gift.ScrollSpec.Virtual], so an offset change invalidates
// the layout of this one node rather than only its paint: one binary search per
// masonry column, a rebinding of the tiles that changed item, and one
// [gift.LayoutContext.Measure] per tile, of which only the ones whose size
// changed actually run a layouter. No view function runs and
// [gift.Diagnostics.Builds] does not move. The numbers are in
// TestGalleryScrollPathCost.
type GalleryView struct {
	base
	g          *Gallery
	spec       GalleryLayout
	tile       TileStyle
	hasTile    bool
	overscan   float32
	cfg        gift.ScrollConfig
	onSelect   func(asset.ID)
	onActivate func(asset.ID)
	name       string
}

// ImageGallery returns the view of g.
//
// The spelling of the project plan, section 10, is ui.ImageGallery(photos).
// The difference is one indirection and it is deliberate; see [Gallery] for
// why the retained half has to be the application's.
//
//	photos := asset.NewCollection(entries)
//	gallery := ui.NewGallery(photos)   // once, in the application
//
//	ui.ImageGallery(gallery).
//	    Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).
//	    OnSelect(func(id asset.ID) { /* open the detail view */ }).
//	    Flex(1)
func ImageGallery(g *Gallery) GalleryView {
	if g == nil {
		panic("gift/ui: ImageGallery with a nil Gallery; create one with ui.NewGallery")
	}
	return GalleryView{g: g, spec: Masonry()}
}

// ViewType implements gift.View.
func (v GalleryView) ViewType() gift.TypeID { return galleryType }

// Build implements gift.View.
func (v GalleryView) Build(*gift.BuildContext) gift.Element {
	g := v.g
	g.spec = v.spec
	g.style = defaultTileStyle
	if v.hasTile {
		g.style = v.tile
	}
	g.overscan = float64(v.overscan)
	g.onSelect, g.onActivate = v.onSelect, v.onActivate

	n := &galleryNode{g: g, fr: v.frame, st: v.style, pad: v.pad}
	kids := g.resizePool(g.desiredSlots)

	return gift.Element{
		Key:      v.key,
		Flex:     v.flex,
		Label:    v.name,
		Layouter: n,
		Painter:  n,
		Children: kids,
		// The gallery is the keyboard target, not the tiles. A focusable tile
		// would put the focus on a recycled slot, and the focus would then
		// follow the storage instead of the picture — precisely what the
		// project plan, section 10, rules out for selection and navigation.
		Interactor: n,
		Focusable:  true,
		// Always: a viewport that did not clip would paint the whole overscan
		// band over its neighbours.
		Clip: true,
		Scroll: &gift.ScrollSpec{
			Axis:    gift.ScrollVertical,
			Config:  v.cfg,
			Virtual: true,
		},
	}
}

// resizePool brings the tile pool to n slots and returns a fresh children
// slice for it.
//
// The slice is fresh on every build because ownership of a children slice
// passes to gift and reusing one is a contract violation the debug build
// detects; see the project plan, section 4. The *nodes* behind it are not
// fresh: [tileNode] values are created once per slot and kept, so growing the
// pool from 60 to 68 tiles allocates eight of them and not sixty eight.
func (g *Gallery) resizePool(n int) []gift.View {
	if n < 0 {
		n = 0
	}
	for len(g.tiles) < n {
		g.tiles = append(g.tiles, &tileNode{g: g, slot: len(g.tiles)})
	}
	switch {
	case len(g.slots) < n:
		g.slots = append(g.slots, make([]tileSlot, n-len(g.slots))...)
	case len(g.slots) > n:
		// Unbind what is going away, so that a slot which comes back later
		// cannot come back carrying an old identity.
		for i := n; i < len(g.slots); i++ {
			g.slots[i] = tileSlot{}
		}
		g.slots = g.slots[:n]
	}
	g.desiredSlots = n

	kids := make([]gift.View, n)
	for i := range n {
		kids[i] = tileView{n: g.tiles[i]}
	}
	return kids
}

// --- gallery specific modifiers ----------------------------------------------

// Layout selects the arrangement. The default is [Masonry] with default
// metrics.
//
// Changing it between two builds is a runtime layout switch: the gallery takes
// a scroll anchor, starts an incremental reflow and discards any reflow that
// was already running, so a user who toggles the control twice in one second
// pays for one reflow and sees one result.
func (v GalleryView) Layout(l GalleryLayout) GalleryView { v.spec = l; return v }

// Tile sets the appearance of the placeholders. See [TileStyle].
func (v GalleryView) Tile(s TileStyle) GalleryView { v.tile, v.hasTile = s, true; return v }

// Overscan keeps tiles mounted for this many logical pixels above and below
// the viewport.
//
// It is zero by default, and zero is the honest default here: there is no
// image pipeline yet, so a tile costs a node and a rounded rectangle and there
// is nothing for an overscan band to prefetch. Step 4 gives it a job —
// decoding the row that is about to appear — and an application that scrolls
// fast will want a band roughly the height of one row.
//
// It costs tiles, and therefore nodes: the pool grows to cover the viewport
// plus the band.
func (v GalleryView) Overscan(px float32) GalleryView { v.overscan = px; return v }

// OnSelect is called with the stable ID of the entry the user selected, by
// clicking it or by pressing space on the keyboard cursor.
//
// It receives an [asset.ID] and never a tile, a slot or an index, which is the
// API level statement of the rule in the project plan, section 10: selection
// follows stable IDs and the catalogue order, "nicht dem zufaelligen
// Speicherplatz einer Kachel".
func (v GalleryView) OnSelect(fn func(asset.ID)) GalleryView { v.onSelect = fn; return v }

// OnActivate is called when the user activates an entry: a double click, or
// enter on the keyboard cursor. It is what opens a detail view.
func (v GalleryView) OnActivate(fn func(asset.ID)) GalleryView { v.onActivate = fn; return v }

// Label sets the accessible name of the gallery; see [gift.Element.Label].
func (v GalleryView) Label(s string) GalleryView { v.name = s; return v }

// Friction sets the exponential decay rate of a fling, exactly as
// [ScrollView.Friction].
func (v GalleryView) Friction(f float32) GalleryView { v.cfg.Friction = f; return v }

// WheelStep sets how far one unit of wheel delta scrolls, in logical pixels.
func (v GalleryView) WheelStep(f float32) GalleryView { v.cfg.WheelStep = f; return v }

// Config replaces the whole gesture configuration; see [gift.ScrollConfig].
func (v GalleryView) Config(c gift.ScrollConfig) GalleryView { v.cfg = c; return v }

// --- the shared modifier set -------------------------------------------------

// Padding sets the same padding on all four edges. It is inside the viewport,
// so it scrolls with the content, exactly as [ScrollView.Padding].
func (v GalleryView) Padding(f float32) GalleryView { v.setPadding(f); return v }

// PaddingInsets sets the padding per edge.
func (v GalleryView) PaddingInsets(i geom.Insets) GalleryView { v.setPaddingInsets(i); return v }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free — but not for the height, which then has no viewport; see [GalleryView].
func (v GalleryView) Frame(w, h float32) GalleryView { v.setFrame(w, h); return v }

// MinWidth raises the minimum width of the viewport.
func (v GalleryView) MinWidth(f float32) GalleryView { v.setMinWidth(f); return v }

// MinHeight raises the minimum height of the viewport.
func (v GalleryView) MinHeight(f float32) GalleryView { v.setMinHeight(f); return v }

// MaxWidth lowers the maximum width of the viewport.
func (v GalleryView) MaxWidth(f float32) GalleryView { v.setMaxWidth(f); return v }

// MaxHeight lowers the maximum height of the viewport. It is one of the ways
// to give the gallery a bounded height.
func (v GalleryView) MaxHeight(f float32) GalleryView { v.setMaxHeight(f); return v }

// Background fills the viewport behind the tiles. It does not scroll.
func (v GalleryView) Background(c Color) GalleryView { v.setBackground(c); return v }

// Border strokes the inside of the viewport bounds after the tiles were drawn.
func (v GalleryView) Border(b Border) GalleryView { v.setBorder(b); return v }

// Shadow draws a blurred copy of the viewport box behind it.
func (v GalleryView) Shadow(s Shadow) GalleryView { v.setShadow(s); return v }

// CornerRadius rounds the background and the border of the viewport. The tiles
// have their own radius; see [TileStyle].
func (v GalleryView) CornerRadius(f float32) GalleryView { v.setCornerRadius(f); return v }

// Clip is accepted only as Clip(true), which is already what a gallery does;
// Clip(false) panics for the same reason it does on [ScrollView.Clip].
func (v GalleryView) Clip(b bool) GalleryView {
	if !b {
		panic("gift/ui: Clip(false) on an ImageGallery; a viewport that does not clip would paint " +
			"its overscan band and its partially visible tiles over its neighbours")
	}
	return v
}

// Key sets the reconciliation key of this view among its siblings.
func (v GalleryView) Key(s string) GalleryView { v.setKey(s); return v }

// Flex makes the gallery take a share of the remaining main axis space of its
// parent stack. It is the usual way to give it a bounded height.
func (v GalleryView) Flex(f float32) GalleryView { v.setFlex(f); return v }

// --- the tile view -----------------------------------------------------------

// tileView is the declaration of one pool slot. It carries no item: which
// entry a slot stands for is decided during layout, not during build, which is
// exactly what makes scrolling free of builds.
type tileView struct{ n *tileNode }

func (tileView) ViewType() gift.TypeID { return galleryTileType }

func (t tileView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Layouter: t.n,
		Painter:  t.n,
		// A hit target, but not focusable: see [GalleryView.Build]. It claims
		// only the release that is a click; everything else bubbles.
		Interactor: t.n,
		// No key. Unkeyed siblings are matched by position, which is O(1) per
		// child; the keyed path in gift's reconciler searches the previous
		// sibling list per child and is therefore quadratic. A virtualised
		// collection must never take that path, and with a positional pool it
		// cannot: the slot *is* the position and it never moves.
	}
}

// tileNode is the retained half of one pool slot: layouter and painter.
//
// It holds the slot number and nothing else about the item. Everything it
// draws it reads out of [Gallery.slots] and out of the live selection at paint
// time, so there is no copy of an item's identity in the tile that could
// survive a recycle.
type tileNode struct {
	g    *Gallery
	slot int
}

// Layout takes the size the gallery gave it. The gallery measures every tile
// with tight constraints derived from the document rectangle of the item, so
// a tile has nothing to decide.
func (t *tileNode) Layout(_ *gift.LayoutContext, c geom.Constraints) geom.Size {
	return c.Constrain(c.Min)
}

// Paint draws the placeholder of whatever the slot currently stands for.
//
// The selection and the cursor are read from the live [asset.Selection] rather
// than from a flag copied into the slot during layout. That is what makes
// "click to select" a repaint and not a relayout: the interactor writes the
// selection and asks for a repaint, and this reads the new answer.
func (t *tileNode) Paint(ctx *gift.PaintContext) {
	s := &t.g.slots[t.slot]
	if !s.bound {
		// An unbound slot is a zero sized node that draws nothing. It is kept
		// rather than unmounted because unmounting it would put the pool back
		// on the reconciliation path on every scroll.
		return
	}
	st := t.g.style
	b := ctx.Bounds()
	fill := placeholderColor(s.id, st)
	if s.provisional && !st.Provisional.IsTransparent() {
		fill = st.Provisional
	}
	paintBackground(ctx, styleSpec{background: fill, radius: st.CornerRadius}, b)

	sel := t.g.sel
	if sel.Contains(s.id) && st.Selected.IsVisible() {
		paintBorder(ctx, styleSpec{border: st.Selected, radius: st.CornerRadius}, b)
	}
	if sel.Cursor() == s.id && st.Cursor.IsVisible() {
		paintBorder(ctx, styleSpec{border: st.Cursor, radius: st.CornerRadius}, b)
	}
}

// placeholderColor picks a palette entry from the stable ID.
//
// From the ID and not from the slot, the item position or a counter. A colour
// derived from the slot would stay put while the picture under it changed,
// which is the exact appearance of the bug the project plan, section 13, names:
// "keine falschen Bilder nach Tile-Recycling".
//
// FNV-1a over the ID bytes, open coded so that paint does not call into
// hash/fnv and does not allocate.
func placeholderColor(id asset.ID, st TileStyle) Color {
	if len(st.Palette) == 0 {
		return RGB(110, 114, 124)
	}
	var h uint32 = 2166136261
	for i := range len(id) {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return st.Palette[int(h%uint32(len(st.Palette)))]
}

// --- the gallery node --------------------------------------------------------

// galleryNode is the retained half of a [GalleryView]: the layouter that
// virtualises, the painter of the viewport box and the interactor that owns
// the keyboard.
type galleryNode struct {
	g   *Gallery
	fr  frameSpec
	st  styleSpec
	pad geom.Insets
}

// Layout is the whole of the virtualisation.
//
// The order of the steps matters and is:
//
//  1. Follow the catalogue. A structural change refills the index and unbinds
//     every tile, because every item position is then suspect.
//  2. Follow the parameters. A resize or a layout switch changes them and
//     starts an incremental reflow; an anchor is taken first.
//  3. Advance the reflow by a bounded chunk, and ask for another pass if it is
//     not finished. Queries keep answering from the previous layout meanwhile.
//  4. Report the content extent, so that the scroll offset can be clamped.
//  5. Restore the anchor or honour a reveal, both of which move the offset.
//  6. Report the extent again, now with the origin, so that the transform gift
//     pushes is exactly zero and the float64 document coordinates survive to
//     the last possible moment.
//  7. Query the visible interval, bind the tiles, measure and place them.
//  8. Take the anchor for the next reflow.
func (n *galleryNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	g := n.g
	g.invalidate = ctx.Invalidator()
	cc := n.fr.apply(c)

	// A gallery is greedy on both bounded axes, like a Box: it is a viewport
	// and a viewport is whatever room it was given. An unbounded axis falls
	// back to the minimum, which for the scroll axis means no viewport at all;
	// see [GalleryView].
	w, h := cc.Max.W, cc.Max.H
	if !cc.HasBoundedWidth() {
		w = cc.Min.W
	}
	if !cc.HasBoundedHeight() {
		h = cc.Min.H
	}
	size := cc.Constrain(geom.Sz(w, h))
	g.viewport = float64(size.H - n.pad.Vertical())

	contentW := float64(size.W - n.pad.Horizontal())
	g.syncCollection()
	g.syncParams(contentW)
	g.stepReflow(ctx)

	extent := g.ix.ContentExtent()
	if extent > 0 {
		extent += float64(n.pad.Vertical())
	}
	// First call: establish the extent so that the offset is clamped against
	// something real before anything reads or writes it.
	ctx.ReportScrollContent(extent, 0)
	g.applyPendingOffset(ctx, n.pad)
	off := ctx.ScrollOffset()
	// Second call: now that the offset is final, declare it to be the document
	// coordinate of local zero. gift then pushes a translation of origin-off,
	// which is exactly zero, so nothing about a 30 000 000 pixel document ever
	// reaches a float32. The project plan, section 10, requires the conversion
	// to happen after the subtraction; here the subtraction is the identity
	// and the tiles are placed at small local coordinates directly.
	ctx.ReportScrollContent(extent, off)

	g.bindAndPlace(ctx, n.pad, off)
	g.takeAnchor(off, n.pad)

	// A gallery cannot overflow: the scroll axis is a document and the cross
	// axis is what the columns were computed from. Reporting zero keeps
	// gifttest.Harness.AssertNoOverflow usable in a scene that contains one.
	ctx.ReportOverflow(geom.Size{})
	return size
}

// syncCollection refills the spatial index when the catalogue changed.
//
// Every tile is unbound, unconditionally. A cheaper reaction — keeping the
// bindings whose ID still resolves to the same position — would be correct
// most of the time and wrong exactly when an entry was inserted before them,
// which is the case that produces a tile showing the neighbour of what it
// should show. The pool is bounded by the viewport, so the unconditional
// version costs a few dozen struct writes.
func (g *Gallery) syncCollection() {
	if g.indexed == g.coll && g.collVersion == g.coll.Version() {
		return
	}
	g.indexed, g.collVersion = g.coll, g.coll.Version()
	g.ix.SetItems(g.coll.Len(), g.coll)
	for i := range g.slots {
		g.slots[i] = tileSlot{}
	}
	g.needReflow = true
	g.requestAnchorRestore()
}

// syncParams notices a resize or a layout switch.
func (g *Gallery) syncParams(width float64) {
	p := g.spec.params(width)
	if g.haveParams && p == g.params {
		return
	}
	g.params, g.haveParams = p, true
	g.needReflow = true
	g.requestAnchorRestore()
}

// stepReflow starts and advances the incremental reflow.
func (g *Gallery) stepReflow(ctx *gift.LayoutContext) {
	if g.needReflow {
		g.needReflow = false
		// Any reflow already in flight is discarded here, inside
		// BeginRebuild. That is the mechanism the project plan, section 10,
		// asks for: twenty intermediate widths during a window drag start
		// twenty reflows and commit one.
		g.ix.BeginRebuild(g.params)
	}
	if !g.ix.Rebuilding() {
		return
	}
	budget := g.rebuildBudget
	if g.firstLayout {
		// Nothing is on screen yet, so chunking would buy blank frames
		// instead of responsiveness.
		budget = math.MaxInt32
	}
	if g.ix.Step(budget) {
		g.firstLayout = false
		return
	}
	// Not finished. Ask for another pass rather than another frame's worth of
	// nothing; the previous layout keeps answering in the meantime.
	ctx.RequestLayout()
}

// applyPendingOffset moves the viewport for an anchor restore or a reveal.
func (g *Gallery) applyPendingOffset(ctx *gift.LayoutContext, pad geom.Insets) {
	if !g.ix.Ready() {
		return
	}
	if g.restoreAnchor && g.ix.Version() != g.anchorAtVersion {
		g.restoreAnchor = false
		if i, ok := g.coll.Find(g.anchorID); ok {
			if r, ok := g.ix.ItemRect(i); ok {
				// Clamped again against the *new* height, so the anchored
				// item intersects the viewport whatever the reflow did to it.
				local := min(g.anchorLocal, max(r.H-1, 0))
				ctx.SetScrollOffset(r.Y + local - float64(pad.Top))
			}
		}
	}
	if g.reveal >= 0 {
		item := g.reveal
		g.reveal = -1
		if r, ok := g.ix.ItemRect(item); ok {
			off := ctx.ScrollOffset()
			top := r.Y - float64(pad.Top)
			bottom := r.Y + r.H - g.viewport
			switch {
			case top < off:
				ctx.SetScrollOffset(top)
			case bottom > off:
				ctx.SetScrollOffset(bottom)
			}
		}
	}
}

// takeAnchor records which entry the top of the viewport is on.
//
// It is taken *after* the tiles were placed, that is from the layout that is
// on screen, so that the next reflow restores the picture the user is actually
// looking at. The entry chosen is the first visible one in catalogue order,
// which for masonry is not the topmost one on screen — it is the one with the
// smallest index among those intersecting the viewport, and that is the stable
// choice: the topmost one changes column when the width changes.
func (g *Gallery) takeAnchor(off float64, pad geom.Insets) {
	top := off - float64(pad.Top)
	best, bestY, bestH := -1, 0.0, 0.0
	for i := range g.want {
		v := g.want[i]
		if best >= 0 && v.Item >= best {
			continue
		}
		best, bestY, bestH = v.Item, v.Rect.Y, v.Rect.H
	}
	if best < 0 || best >= g.coll.Len() {
		return
	}
	g.anchorID = g.coll.ID(best)
	// Clamped into the item. An unclamped offset is faithful and useless: the
	// anchored tile usually starts above the viewport, and after a reflow that
	// made it shorter — a narrower masonry column, a switch to justified — the
	// same distance puts it entirely off screen, which is precisely the thing
	// the anchor exists to prevent. Clamping turns "this many pixels into the
	// document from the top of that item" into "this many pixels into that
	// item", which is the same number whenever the item did not change size
	// and is still inside it when it did.
	g.anchorLocal = min(max(top-bestY, 0), max(bestH-1, 0))
	g.haveAnchor = true
}

// bindAndPlace is the frame path.
//
// # Cost
//
// One [layout.Index.Visible] query, which is O(c*log(n/c) + k) for c masonry
// columns, plus O(k*p) for the binding, where k is the number of visible items
// and p the pool size. Both k and p are bounded by the viewport. The quadratic
// looking term is a linear scan of a pool of a few dozen slots — about two
// thousand integer comparisons at 1920x1080 — and it buys the property that a
// tile whose item is still visible is *not* rebound, so a one pixel scroll
// re-measures nothing.
//
// Nothing here allocates: want and assign are reused buffers and Visible
// appends to the first of them, which is the caller side of the contract the
// project plan, section 10, states.
func (g *Gallery) bindAndPlace(ctx *gift.LayoutContext, pad geom.Insets, off float64) {
	k := ctx.ChildCount()
	g.want = g.want[:0]
	if g.ix.Ready() {
		top := off - float64(pad.Top) - g.overscan
		bottom := off - float64(pad.Top) + g.viewport + g.overscan
		g.want = g.ix.Visible(top, bottom, g.want)
	}

	// Size the pool for the next build. Growing has headroom so that a slow
	// scroll does not rebuild every few frames; shrinking needs the pool to be
	// more than twice as large as necessary, so the two cannot oscillate.
	need := len(g.want)
	target := len(g.slots)
	switch {
	case need > len(g.slots):
		target = need + need/8 + 4
	case need*2+8 < len(g.slots):
		target = need + need/8 + 4
	}
	if target != g.desiredSlots {
		g.desiredSlots = target
		// Only a build can change the number of children. This frame is laid
		// out with the pool it has, which for a growing viewport means a
		// briefly under filled band — the placeholder behaviour of the
		// project plan, section 10, and not a wrong picture.
		ctx.RequestBuild()
	}
	if need > k {
		g.want = g.want[:k]
	}

	g.assign = grow32i(g.assign, len(g.want))
	for i := range g.slots {
		g.slots[i].claimed = false
	}
	// Keep every tile whose item is still visible, so that a scroll rebinds
	// only the tiles that actually changed item.
	for i := range g.want {
		g.assign[i] = -1
		for si := range g.slots {
			s := &g.slots[si]
			if s.bound && !s.claimed && s.item == g.want[i].Item {
				s.claimed = true
				g.assign[i] = int32(si)
				break
			}
		}
	}
	// Everything else goes into a slot nobody kept.
	free := 0
	for i := range g.want {
		if g.assign[i] >= 0 {
			continue
		}
		for free < len(g.slots) && g.slots[free].claimed {
			free++
		}
		if free >= len(g.slots) {
			break
		}
		g.slots[free].claimed = true
		g.assign[i] = int32(free)
		g.bind(free, g.want[i])
	}

	// Place. A tile keeps its identity and only moves; a slot nobody claimed
	// is unbound and collapsed to nothing.
	for si := range g.slots {
		if !g.slots[si].claimed {
			g.unbind(si)
		}
	}
	for i := range g.want {
		si := int(g.assign[i])
		if si < 0 {
			continue
		}
		s := &g.slots[si]
		s.rect = g.want[i].Rect
		sz := geom.Sz(float32(s.rect.W), float32(s.rect.H))
		ctx.Measure(si, geom.Tight(sz))
		// The one place a document coordinate becomes a float32, and it does
		// so after the viewport origin has been subtracted. See the top of
		// internal/layout/gallery.go.
		ctx.Place(si, geom.Pt(
			float32(s.rect.X)+pad.Left,
			float32(s.rect.Y-off)+pad.Top,
		))
	}
	for si := range g.slots {
		if g.slots[si].bound {
			continue
		}
		ctx.Measure(si, geom.Tight(geom.Size{}))
		ctx.Place(si, geom.Point{})
	}
}

// bind attaches a slot to an item and issues a new request generation.
//
// The generation moves here and nowhere else, which is what makes it mean
// "this tile stands for a different picture than it did". The project plan,
// section 9, keeps it apart from the content revision — which is copied
// alongside it and describes the bytes — and from the scene node generation,
// which describes the storage and is gift's business.
func (g *Gallery) bind(slot int, v layout.Visible) {
	if v.Item < 0 || v.Item >= g.coll.Len() {
		// Belt and braces. The index and the catalogue are kept in step by
		// syncCollection, and a mismatch here would mean that failed; binding
		// nothing is a blank tile, while indexing past the end is a panic in
		// the middle of a frame.
		return
	}
	s := &g.slots[slot]
	g.gen++
	*s = tileSlot{
		bound:       true,
		claimed:     true,
		item:        v.Item,
		id:          g.coll.ID(v.Item),
		revision:    g.coll.Revision(v.Item),
		provisional: g.ix.Provisional(v.Item),
		gen:         g.gen,
		rect:        v.Rect,
	}
}

// unbind clears a slot completely. Assigning the zero value rather than
// clearing a flag is the point: there is then nothing left of the previous
// item for the next binding to inherit.
func (g *Gallery) unbind(slot int) { g.slots[slot] = tileSlot{} }

func grow32i(s []int32, n int) []int32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]int32, n)
}

// Paint draws the viewport box. The tiles are ordinary children and are drawn
// by gift under this node's clip and scroll transform.
func (n *galleryNode) Paint(ctx *gift.PaintContext) {
	b := ctx.Bounds()
	paintBackground(ctx, n.st, b)
	ctx.PaintChildren()
	paintBorder(ctx, n.st, b)
}

// --- input -------------------------------------------------------------------

// HandleEvent owns the keyboard and the selection and delegates the gesture.
//
// The delegation is the whole reason [gift.ScrollInteractor] exists: gift
// installs its scroll handler only on a container that brought none of its
// own, and a gallery has to bring one because it is also the keyboard target.
// Reimplementing wheel, drag and fling here would be a second copy of a
// gesture that is already right.
//
// # Which events arrive here
//
// A tile is a hit target too, but it only claims the release that turns into a
// selection. Everything else it declines, so a press, a drag and a wheel over
// a tile bubble up to this node — see [gift.Interactor] — and are handled
// exactly as if the tile were not there.
func (n *galleryNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	g := n.g
	switch e.Kind {
	case gift.EventPointerDown:
		// Take the focus so the arrow keys go somewhere, but do not consume
		// the event: gift's handler still wants it to stop a running fling,
		// and an outer scroller wants to hear about it too.
		ctx.RequestFocus()
	case gift.EventKeyDown:
		if g.handleKey(ctx, e) {
			return true
		}
	}
	return gift.ScrollInteractor().HandleEvent(ctx, e)
}

// handleKey moves the cursor and the selection in *catalogue* order.
//
// The project plan, section 10, is explicit that navigation follows "stabilen
// IDs und der logischen Collection-Reihenfolge, nicht dem zufaelligen
// Speicherplatz einer Kachel". So left and right are one entry, and up and
// down are one line's worth of entries — the column count for masonry, the
// length of the current row for justified — and never "the tile that happens
// to be above this one". In a masonry layout those are different answers,
// because the item above a tile on screen is usually several entries away.
//
// The modifiers follow the platform convention: plain replaces the selection,
// shift extends it from the range anchor, control or meta moves the cursor
// alone.
func (g *Gallery) handleKey(ctx *gift.EventContext, e gift.Event) bool {
	n := g.coll.Len()
	if n == 0 {
		return false
	}
	cur, has := g.coll.Find(g.sel.Cursor())
	if !has {
		cur = 0
	}
	target := cur
	switch e.Key {
	case gift.KeyLeft:
		target = cur - 1
	case gift.KeyRight:
		target = cur + 1
	case gift.KeyUp:
		target = cur - g.stride(cur)
	case gift.KeyDown:
		target = cur + g.stride(cur)
	case gift.KeyPageUp:
		target = cur - g.page()
	case gift.KeyPageDown:
		target = cur + g.page()
	case gift.KeyHome:
		target = 0
	case gift.KeyEnd:
		target = n - 1
	case gift.KeySpace:
		if has {
			id := g.coll.ID(cur)
			if g.sel.Toggle(id) {
				g.fire(g.onSelect, id)
			}
			ctx.Repaint()
		}
		return true
	case gift.KeyEnter:
		if has {
			g.fire(g.onActivate, g.coll.ID(cur))
		}
		return true
	default:
		return false
	}
	if !has {
		// No cursor yet: the first arrow key puts it on the first entry
		// rather than moving from a position nobody chose.
		target = 0
	}
	target = min(max(target, 0), n-1)
	id := g.coll.ID(target)
	switch {
	case e.Mods.Has(gift.ModShift):
		g.sel.SelectRange(g.coll, id)
	case e.Mods.Has(gift.ModControl) || e.Mods.Has(gift.ModMeta):
		g.sel.SetCursor(id, false)
	default:
		g.sel.Only(id)
		g.fire(g.onSelect, id)
	}
	g.reveal = target
	// The cursor is a layout input, because bringing it into view moves the
	// offset and the offset decides which tiles exist.
	ctx.RequestLayout()
	return true
}

// stride is how many catalogue entries one line down is.
func (g *Gallery) stride(item int) int {
	if c := g.ix.Columns(); c > 0 {
		return c
	}
	if start, end, ok := g.ix.RowOf(item); ok && end > start {
		return end - start
	}
	return 1
}

// page is how many entries one viewport is worth, taken from the last visible
// set. It is a measurement and not an estimate, which matters because row
// lengths vary wildly in a justified layout.
func (g *Gallery) page() int {
	if len(g.want) > 0 {
		return len(g.want)
	}
	return 1
}

func (g *Gallery) fire(fn func(asset.ID), id asset.ID) {
	if fn != nil {
		fn(id)
	}
}

// tileHit is the selection half of the pointer path, called by the tile the
// release landed on.
func (g *Gallery) tileHit(ctx *gift.EventContext, slot int, e gift.Event) bool {
	s := &g.slots[slot]
	if !s.bound {
		return false
	}
	id := s.id
	switch {
	case e.Mods.Has(gift.ModShift):
		g.sel.SelectRange(g.coll, id)
	case e.Mods.Has(gift.ModControl) || e.Mods.Has(gift.ModMeta):
		g.sel.Toggle(id)
		g.sel.SetCursor(id, false)
	default:
		g.sel.Only(id)
	}
	g.fire(g.onSelect, id)
	// A repaint and not a relayout: nothing moved. The tile reads the
	// selection live, so the new border is on screen in the next frame
	// without a single view function running.
	ctx.Repaint()
	return true
}

// HandleEvent claims the release that is a click on this tile and nothing
// else.
//
// Declining everything else is what makes a drag that starts on a tile scroll
// the gallery: the move events bubble to the gallery node, gift's scroll
// handler takes the press away through StealPointer, and this tile never hears
// about the release. Claiming the press instead would have made every drag
// start with a selection.
func (t *tileNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if e.Kind != gift.EventPointerUp || !e.Inside || e.Dragged {
		return false
	}
	return t.g.tileHit(ctx, t.slot, e)
}

// String makes a gallery readable in a test failure.
func (g *Gallery) String() string {
	return fmt.Sprintf("ui.Gallery{entries:%d slots:%d visible:%d layout:v%d extent:%.0f}",
		g.coll.Len(), len(g.slots), len(g.want), g.ix.Version(), g.ix.ContentExtent())
}
