package asset

// Selection is the durable selection and keyboard cursor of a catalogue.
//
// It lives here, next to the collection, because the project plan, section 5,
// says where it must not live: "Dauerhafte Galerieauswahl lebt beim
// Collection-Owner, nicht im recycelten Tile." A tile is a recycled slot that
// stands for a different entry every few frames; anything stored in it is
// wrong the moment it is reused, and a selection stored in it would follow the
// slot instead of the picture.
//
// Everything in it is expressed in [ID] and never in a position, for the same
// reason a correction is: a sort or a reset moves positions and leaves IDs
// alone. Selecting the fifth picture and then sorting by date must keep that
// picture selected, not whatever is fifth afterwards.
//
// # Ownership
//
// A Selection belongs to the UI executor and is not safe for concurrent use.
// The zero value is not usable; obtain one from [NewSelection].
type Selection struct {
	sel    map[ID]struct{}
	cursor ID
	// anchor is the fixed end of a range extension, that is where the last
	// plain click or cursor move put it.
	anchor  ID
	version uint64
}

// NewSelection returns an empty selection.
func NewSelection() *Selection {
	return &Selection{sel: make(map[ID]struct{}), version: 1}
}

// Version increases whenever the selection or the cursor changed.
//
// A view compares it to decide whether it has to redraw; it is the same
// mechanism as [Collection.StructureVersion] and exists for the same reason,
// which is
// that polling a map for changes every frame is not a comparison.
func (s *Selection) Version() uint64 { return s.version }

// Len is the number of selected entries.
func (s *Selection) Len() int { return len(s.sel) }

// Contains reports whether id is selected. It is the per tile question and is
// a map lookup, which is what makes it affordable once per visible tile per
// frame.
func (s *Selection) Contains(id ID) bool {
	_, ok := s.sel[id]
	return ok
}

// Set selects or deselects id and reports whether anything changed. The empty
// ID is ignored.
func (s *Selection) Set(id ID, on bool) bool {
	if id == "" {
		return false
	}
	_, had := s.sel[id]
	switch {
	case on && !had:
		s.sel[id] = struct{}{}
	case !on && had:
		delete(s.sel, id)
	default:
		return false
	}
	s.version++
	return true
}

// Toggle flips the state of id and reports the new state.
func (s *Selection) Toggle(id ID) bool {
	on := !s.Contains(id)
	s.Set(id, on)
	return on
}

// Clear deselects everything, leaving the cursor where it is, and reports
// whether anything changed.
//
// The cursor survives on purpose: clearing a selection with a plain click is
// immediately followed by selecting what was clicked, and moving the cursor
// twice in one gesture would make a range extension from it mean two different
// things depending on the order of the calls.
func (s *Selection) Clear() bool {
	if len(s.sel) == 0 {
		return false
	}
	clear(s.sel)
	s.version++
	return true
}

// Only makes id the entire selection and puts the cursor and the range anchor
// on it. It is what a plain click does.
func (s *Selection) Only(id ID) {
	changed := s.Clear()
	if s.Set(id, true) {
		changed = true
	}
	if s.cursor != id || s.anchor != id {
		s.cursor, s.anchor = id, id
		changed = true
	}
	if changed && s.version == 0 {
		s.version++
	}
}

// Cursor is the entry the keyboard focus of the gallery is on, or the empty ID
// when there is none.
//
// It is separate from the selection because it has to be: shift-arrow extends
// a selection from a moving cursor, and control-arrow moves the cursor without
// selecting at all. One value cannot express both.
func (s *Selection) Cursor() ID { return s.cursor }

// SetCursor moves the cursor and, unless keepAnchor, the range anchor with it.
// It reports whether anything changed.
func (s *Selection) SetCursor(id ID, keepAnchor bool) bool {
	changed := s.cursor != id
	s.cursor = id
	if !keepAnchor && s.anchor != id {
		s.anchor = id
		changed = true
	}
	if changed {
		s.version++
	}
	return changed
}

// Anchor is the fixed end of a range extension; see [Selection.SelectRange].
func (s *Selection) Anchor() ID { return s.anchor }

// SelectRange replaces the selection with the entries from the anchor to id,
// inclusive, in the order c defines.
//
// The range is resolved through the collection and not through anything the
// view knows, which is the point: the project plan, section 10, requires
// keyboard navigation to follow "der logischen Collection-Reihenfolge, nicht
// dem zufaelligen Speicherplatz einer Kachel", and in a masonry layout the two
// are emphatically not the same — item 5 and item 6 are usually in different
// columns and hundreds of pixels apart.
//
// An anchor that is no longer in the collection falls back to id alone, which
// is the only answer that is not a guess.
func (s *Selection) SelectRange(c *Collection, id ID) {
	from, okFrom := c.Find(s.anchor)
	to, okTo := c.Find(id)
	if !okTo {
		return
	}
	if !okFrom {
		from = to
	}
	if from > to {
		from, to = to, from
	}
	s.Clear()
	for i := from; i <= to; i++ {
		s.sel[c.ID(i)] = struct{}{}
	}
	s.cursor = id
	s.version++
}

// IDs appends every selected ID to dst and returns the extended slice.
//
// The order is unspecified — it is a map — so a caller that needs catalogue
// order sorts by [Collection.Find]. It is not sorted here because the callers
// that want an order want different ones and the ones that do not want any are
// the common case.
func (s *Selection) IDs(dst []ID) []ID {
	for id := range s.sel {
		dst = append(dst, id)
	}
	return dst
}

// Prune drops every selected ID and every cursor that c no longer contains,
// and reports whether anything was dropped.
//
// It is the tidy-up after a reset. It is not automatic, because a selection
// that survives a temporary reload of the same catalogue is usually what the
// user expects and gift cannot tell the two cases apart.
func (s *Selection) Prune(c *Collection) bool {
	changed := false
	for id := range s.sel {
		if _, ok := c.Find(id); !ok {
			delete(s.sel, id)
			changed = true
		}
	}
	if _, ok := c.Find(s.cursor); !ok && s.cursor != "" {
		s.cursor = ""
		changed = true
	}
	if _, ok := c.Find(s.anchor); !ok && s.anchor != "" {
		s.anchor = ""
		changed = true
	}
	if changed {
		s.version++
	}
	return changed
}
