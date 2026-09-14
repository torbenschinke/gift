package asset

import "fmt"

// ID is the stable identity of one catalogue entry.
//
// Stable means: the same picture keeps the same ID across restarts, across a
// re-scan of the directory it lives in and across a change of its content. It
// is what selection, keyboard navigation and the scroll anchor are expressed
// in, because the project plan, section 10, requires them to follow "stabilen
// IDs und der logischen Collection-Reihenfolge, nicht dem zufaelligen
// Speicherplatz einer Kachel".
//
// A string and not an integer: the IDs of the step 4 sources are a cleaned
// absolute path and a normalised URL, which is what makes them survive a
// restart at all. An integer would have to be mapped to one of those
// somewhere, and that somewhere would be a second source of truth.
//
// The empty ID is the zero value and means "no entry".
type ID string

// Metadata is what is known about one entry without opening it.
//
// It is the struct of the project plan, section 9, unchanged. Every field
// except [Metadata.ID] may be empty or zero, and zero means "not known yet",
// never "known to be zero": the point of the type is that it is answerable
// without blocking.
type Metadata struct {
	// ID is the stable identity. It must not be empty.
	ID ID

	// Revision is the content version of this entry: an ETag, a
	// Last-Modified, a hash, or an mtime and size. An empty Revision means
	// "not validated yet" and explicitly not "immutable"; see the project
	// plan, section 9.
	Revision string

	// Width and Height are the pixel dimensions of the *oriented* picture,
	// or zero when they are not known yet. An entry with zero dimensions is
	// laid out with a provisional aspect ratio and corrected later; see
	// [Collection.ApplyCorrections].
	Width, Height uint32

	// MIMEType is the media type, or empty when it is not known yet.
	MIMEType string
}

// HasDimensions reports whether both dimensions are known.
func (m Metadata) HasDimensions() bool { return m.Width != 0 && m.Height != 0 }

// Correction is a late arriving fact about one entry, addressed by its stable
// ID rather than by its position.
//
// By ID, because a correction is produced by something that started work an
// unknown number of frames ago and positions move in between. The project plan,
// section 10, requires corrections to be applied in *batches*: correcting one
// entry moves every entry after it in the layout, so applying them one at a
// time would be one full reflow per probed picture.
type Correction struct {
	// ID names the entry. A correction for an unknown ID is ignored.
	ID ID
	// Width and Height are the real oriented dimensions. Zero leaves the
	// current value alone, so a correction that only carries a revision does
	// not erase dimensions that were already known.
	Width, Height uint32
	// Revision, when not empty, replaces the content version of the entry.
	Revision string
}

// Collection is an ordered catalogue of entries with stable IDs.
//
// It is the model a gallery is a view of: ordered, versioned, answerable
// without I/O, and holding neither a view nor a piece of state per entry — the
// project plan, section 10, is explicit that "es braucht keinen State und
// keinen View pro Katalogeintrag".
//
// # Memory
//
// One [Metadata] per entry, which is 56 bytes on a 64 bit platform plus the
// bytes of the two strings, and one map entry per entry once [Collection.Find]
// has been called. At 100 000 entries that is about 5.6 MB of structs and, for
// short IDs, a further 5 MB or so of index. Compare the 24.4 GiB of pixels the
// project plan, section 10, computes for the same catalogue: the catalogue is
// not the thing that has to be bounded.
//
// # Ownership
//
// A Collection belongs to the UI executor. It is not safe for concurrent use
// and does not pretend to be.
type Collection struct {
	items []Metadata

	// byID is built lazily, on the first [Collection.Find], and dropped
	// whenever the order changes. Most of what a gallery does — scrolling,
	// laying out, painting — addresses entries by position, so a catalogue
	// that is only ever scrolled never pays for the map.
	byID map[ID]int

	version     uint64
	metaVersion uint64
}

// NewCollection returns a collection over items.
//
// Ownership of the slice passes to the collection, exactly as it does for a
// children slice; see the project plan, section 4. The caller must neither
// modify it nor reuse it afterwards. Passing nil is legal and yields an empty
// collection.
//
// A duplicate or empty ID is a programming error and panics. It is rejected
// here rather than tolerated because every later symptom of it is remote: a
// selection that jumps to the wrong picture, a scroll anchor that lands in the
// wrong place, a correction applied to the wrong entry. Validation is O(n) and
// happens once, outside the frame path.
func NewCollection(items []Metadata) *Collection {
	c := &Collection{items: items, version: 1, metaVersion: 1}
	c.validate()
	return c
}

func (c *Collection) validate() {
	seen := make(map[ID]int, len(c.items))
	for i, m := range c.items {
		if m.ID == "" {
			panic(fmt.Sprintf("gift/asset: Collection entry %d has an empty ID; "+
				"every entry needs a stable identity, because selection, keyboard navigation "+
				"and the scroll anchor are expressed in IDs and not in positions", i))
		}
		if j, dup := seen[m.ID]; dup {
			panic(fmt.Sprintf("gift/asset: Collection entries %d and %d share the ID %q; "+
				"IDs identify entries and a duplicate makes selection and the scroll anchor "+
				"point at whichever of the two was found first", j, i, m.ID))
		}
		seen[m.ID] = i
	}
	// The map built here is exactly the index Find needs, so it is kept
	// rather than thrown away and rebuilt on the first lookup.
	c.byID = seen
}

// Len is the number of entries.
func (c *Collection) Len() int { return len(c.items) }

// At returns the metadata of entry i. The index must be in [0, Len).
func (c *Collection) At(i int) Metadata {
	c.check(i)
	return c.items[i]
}

// ID returns the stable identity of entry i, which is the cheap half of
// [Collection.At] and the one the frame path uses.
func (c *Collection) ID(i int) ID {
	c.check(i)
	return c.items[i].ID
}

// Revision returns the content version of entry i; see [Metadata.Revision].
func (c *Collection) Revision(i int) string {
	c.check(i)
	return c.items[i].Revision
}

// DimensionsAt returns the oriented pixel size of entry i, or zeroes when it
// is not known yet.
//
// The signature is the one internal/layout's gallery index reads its input
// through, so a collection can be handed to it directly. The interface is
// satisfied structurally and this package deliberately does not import that
// one: asset sits below ui in the dependency order of the project plan,
// section 3, and a catalogue has no business knowing what a masonry column is.
//
// An out of range index returns zeroes rather than panicking, because the
// layout index reads it in a loop whose bound it owns and a panic there would
// be a worse diagnosis than a provisional tile.
func (c *Collection) DimensionsAt(i int) (w, h uint32) {
	if i < 0 || i >= len(c.items) {
		return 0, 0
	}
	m := c.items[i]
	return m.Width, m.Height
}

// Find returns the position of the entry with the given ID.
//
// This is the half of the split scroll anchor the project plan, section 10,
// assigns to the collection: "der lokale Offset lebt im Layoutindex, die
// stabile Bild-ID in der Collection". The layout index deliberately carries no
// IDs, so resolving one back to a position is asked of here.
//
// The first call builds an index over the whole catalogue and is O(n); every
// call afterwards is a map lookup. A structural change drops the index and the
// next call rebuilds it.
func (c *Collection) Find(id ID) (int, bool) {
	if id == "" {
		return 0, false
	}
	if c.byID == nil {
		c.byID = make(map[ID]int, len(c.items))
		for i, m := range c.items {
			c.byID[m.ID] = i
		}
	}
	i, ok := c.byID[id]
	return i, ok
}

// StructureVersion is the version of the *shape* of the catalogue: which
// entries exist and in which order.
//
// It starts at one and increases on a reset and on a reorder — the two
// mutations that make a position mean a different entry — and on nothing else.
// A consumer that caches anything keyed by *position* compares this number and
// throws that cache away when it moved. The project plan, section 10, calls
// this "Strukturupdates werden versioniert publiziert".
//
// # Why this is two counters and not one
//
// Until WU-O it was one, defended on the grounds that a correction "changes
// the layout of every entry after it, so the consumer has to react either
// way". The consumer has to *reflow* either way. It does not have to
// *unbind*, and those are not the same reaction: a tile binding is keyed by
// position, a correction moves no position, and the generation counter on
// [ui.TileBinding] promises to move only when a slot comes to stand for a
// different picture. With one counter the gallery could not tell the two
// apart, so it took the safe reaction — unbind everything — and the promise
// became false: a correction batch advanced the generation of every bound
// tile, every frame, while every tile kept its item. In step 4 that is a
// pipeline invalidating every decode it has in flight on every batch it
// emits, which is a loop that throws away the cache it just filled.
//
// So there are two numbers. A consumer that keys by position watches this
// one; a consumer that also mirrors dimensions or revisions watches
// [Collection.MetadataVersion] as well and reacts to it more cheaply.
func (c *Collection) StructureVersion() uint64 { return c.version }

// MetadataVersion is the version of the *contents* of the entries: their
// dimensions and their revisions.
//
// It starts at one and increases whenever any field of any entry changed,
// which includes a reset and a reorder — those replace the metadata too — and
// a batch of corrections that actually corrected something. A batch of no-ops
// does not move it, so it costs nothing downstream.
//
// The pair to watch is therefore both numbers, and the distinction they draw
// is: [Collection.StructureVersion] moved means "positions are suspect",
// this one alone moved means "the same entries are still in the same places
// and some of them got bigger". See [Collection.StructureVersion] for why the
// distinction is load bearing.
func (c *Collection) MetadataVersion() uint64 { return c.metaVersion }

// Reset replaces the whole catalogue and bumps both versions. Ownership of
// items passes to the collection; see [NewCollection].
func (c *Collection) Reset(items []Metadata) {
	c.items = items
	c.validate()
	c.version++
	c.metaVersion++
}

// ApplyCorrections folds a batch of late arriving facts into the catalogue and
// reports how many entries actually changed.
//
// The version moves only when something changed, so a batch that turns out to
// be all no-ops costs nothing downstream. Corrections for unknown IDs are
// ignored rather than rejected: a worker that started before a reset will
// deliver results about entries that no longer exist, and that is normal
// operation and not an error. See the project plan, section 9, "Abgebrochene
// Arbeit darf die aktuelle Ansicht nicht ueberschreiben".
//
// It moves [Collection.MetadataVersion] and deliberately *not*
// [Collection.StructureVersion]: nothing was inserted, removed or moved, so
// every position still means the entry it meant before, and a consumer that
// binds by position may keep its bindings. See [Collection.StructureVersion]
// for what went wrong while these were one number.
//
// Applying corrections does not move any layout by itself. When the content
// catches up is a decision about the scroll anchor and belongs to the view;
// see [internal/layout.Index.ApplyCorrections] for the same rule one level
// down.
func (c *Collection) ApplyCorrections(cs []Correction) int {
	changed := 0
	for _, corr := range cs {
		i, ok := c.Find(corr.ID)
		if !ok {
			continue
		}
		m := &c.items[i]
		before := *m
		if corr.Width != 0 && corr.Height != 0 {
			m.Width, m.Height = corr.Width, corr.Height
		}
		if corr.Revision != "" {
			m.Revision = corr.Revision
		}
		if *m != before {
			changed++
		}
	}
	if changed > 0 {
		c.metaVersion++
	}
	return changed
}

// Reorder applies a new order given as positions into the current one: after
// the call, entry i is the entry that used to be at order[i].
//
// It is the one mutation that moves entries, and it is why an anchor has to be
// re-resolved through [Collection.Find] afterwards rather than kept as a
// position. order must be a permutation of [0, Len); anything else panics,
// because a partial permutation would silently drop or duplicate entries.
func (c *Collection) Reorder(order []int) {
	n := len(c.items)
	if len(order) != n {
		panic(fmt.Sprintf("gift/asset: Collection.Reorder with %d positions for %d entries",
			len(order), n))
	}
	out := make([]Metadata, n)
	seen := make([]bool, n)
	for i, from := range order {
		if from < 0 || from >= n || seen[from] {
			panic(fmt.Sprintf("gift/asset: Collection.Reorder: position %d appears at index %d "+
				"and is out of range or repeated; order must be a permutation", from, i))
		}
		seen[from] = true
		out[i] = c.items[from]
	}
	c.items = out
	c.byID = nil
	c.version++
	c.metaVersion++
}

func (c *Collection) check(i int) {
	if i < 0 || i >= len(c.items) {
		panic(fmt.Sprintf("gift/asset: entry index %d out of range, the collection has %d entries",
			i, len(c.items)))
	}
}
