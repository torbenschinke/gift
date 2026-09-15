package render

import "github.com/torbenschinke/gift/geom"

// sentinelExtent is the half extent of the unbounded clip rectangle at index
// 0. It is large enough to contain any plausible view space coordinate and
// small enough that intersecting it with a finite rectangle can never produce
// an infinity or a NaN.
const sentinelExtent = 1e30

// List is the display list of a single frame.
//
// A list owns four flat slices: the operations themselves, the clip
// rectangles, the transforms and the glyphs. Operations reference the clips
// and transforms by index, and index 0 of both is a reserved sentinel meaning
// "unclipped" respectively "identity". A text operation references a range of
// the glyph table; see [Op.Glyphs].
//
// A list is reused across frames. [List.Reset] empties it while keeping the
// capacity of all backing arrays, so a steady state frame performs no
// allocation.
//
// A List is not safe for concurrent use. It belongs to the UI executor.
type List struct {
	ops    []Op
	clips  []geom.Rect
	xforms []geom.Affine2D
	glyphs []Glyph
	// materials is the side table of [OpMaterial]. Index 0 is a reserved
	// sentinel meaning "no material", like index 0 of clips and xforms.
	materials []Material
	// clipStack holds indices into clips. Its first element is always 0,
	// the unbounded sentinel, so the stack is never empty.
	clipStack []uint32
}

func (l *List) ensure() {
	if len(l.clips) == 0 {
		l.clips = append(l.clips, geom.Rc(-sentinelExtent, -sentinelExtent, sentinelExtent, sentinelExtent))
	}
	if len(l.xforms) == 0 {
		l.xforms = append(l.xforms, geom.Identity())
	}
	if len(l.materials) == 0 {
		l.materials = append(l.materials, Material{})
	}
	if len(l.clipStack) == 0 {
		l.clipStack = append(l.clipStack, 0)
	}
}

// Reset empties the list and resets the clip stack, keeping the capacity of
// all backing arrays.
//
// Every slice previously returned by [List.Ops] or [List.Glyphs] becomes
// invalid at this point. A consumer that still holds such a slice is reading
// recycled memory.
func (l *List) Reset() {
	l.ops = l.ops[:0]
	l.clips = l.clips[:0]
	l.xforms = l.xforms[:0]
	l.glyphs = l.glyphs[:0]
	l.materials = l.materials[:0]
	l.clipStack = l.clipStack[:0]
	l.ensure()
}

// PushClip intersects r with the currently active clip, appends the result
// and returns its index. The result becomes the active clip until the
// matching [List.PopClip].
//
// Nested clips therefore never widen the visible area, which is what a
// scrolling container inside another scrolling container requires.
func (l *List) PushClip(r geom.Rect) uint32 {
	l.ensure()
	cur := l.clips[l.clipStack[len(l.clipStack)-1]]
	l.clips = append(l.clips, cur.Intersect(r))
	idx := uint32(len(l.clips) - 1)
	l.clipStack = append(l.clipStack, idx)
	return idx
}

// PopClip removes the innermost clip pushed by [List.PushClip]. The clip
// rectangle itself stays in the list, because operations already emitted
// still reference it by index.
//
// Popping the sentinel is a programming error and panics.
func (l *List) PopClip() {
	l.ensure()
	if len(l.clipStack) <= 1 {
		panic("gift/render: List.PopClip without matching PushClip")
	}
	l.clipStack = l.clipStack[:len(l.clipStack)-1]
}

// CurrentClip returns the index of the currently active clip rectangle.
// It is 0 while no clip is pushed.
func (l *List) CurrentClip() uint32 {
	l.ensure()
	return l.clipStack[len(l.clipStack)-1]
}

// PushXform appends the transform m and returns its index. Transforms are not
// deduplicated; the caller is expected to push once and reuse the index for
// all operations that share the transform.
func (l *List) PushXform(m geom.Affine2D) uint32 {
	l.ensure()
	l.xforms = append(l.xforms, m)
	return uint32(len(l.xforms) - 1)
}

// Add appends op to the list. The caller is responsible for setting the Clip
// and Xform indices; see [List.CurrentClip].
func (l *List) Add(op Op) {
	// Debug builds only; see assertResolvedColor. This is the one choke point
	// every drawn colour passes through, which is why the check lives here
	// rather than in each of the dozen places ui emits an operation.
	assertResolvedColor(op.Color, "operation colour")
	l.ops = append(l.ops, op)
}

// Ops returns the operations of the list in emission order.
//
// The slice is borrowed, not owned. It stays valid only until the producer of
// the list calls [List.Reset], which normally happens at the start of the
// next frame. A consumer that keeps the slice beyond that point reads
// recycled memory and will observe operations of a different frame. Copy what
// you need to keep.
func (l *List) Ops() []Op { return l.ops }

// Clip returns the clip rectangle with index i. Index 0 is the unbounded
// sentinel, which contains every plausible view space coordinate.
func (l *List) Clip(i uint32) geom.Rect {
	l.ensure()
	return l.clips[i]
}

// Xform returns the transform with index i. Index 0 is the identity.
func (l *List) Xform(i uint32) geom.Affine2D {
	l.ensure()
	return l.xforms[i]
}

// AddMaterial appends m to the material side table and returns its index.
//
// Materials are not deduplicated. A frame with two glass panels of identical
// parameters therefore holds two entries, which costs fifty-six bytes of a
// reused slice and saves a comparison in the paint path that would have run
// for every styled node in the scene in order to find the one in a hundred
// that has a material at all.
//
// A material with [MaterialNone] returns index 0 without appending, so a
// painter may call this unconditionally.
func (l *List) AddMaterial(m Material) uint32 {
	l.ensure()
	// The same debug build check [List.Add] performs, for the colour that
	// does not travel on an Op. A material goes into a side table, so it
	// reaches the backend without passing Add at all, and a glass pane tinted
	// with an unresolved gift/ui semantic colour got all the way to the GPU
	// and painted saturated cyan before this line existed.
	assertResolvedColor(m.Glass.Tint, "material tint")
	if !m.IsVisible() {
		return 0
	}
	l.materials = append(l.materials, m)
	return uint32(len(l.materials) - 1)
}

// Material returns the material with index i. Index 0 is the sentinel, whose
// Kind is [MaterialNone].
//
// An out of range index returns the sentinel rather than panicking, under the
// same rule as [List.Glyphs]: a backend reading a malformed list draws nothing
// instead of taking the process down.
func (l *List) Material(i uint32) Material {
	l.ensure()
	if int(i) >= len(l.materials) {
		return Material{}
	}
	return l.materials[i]
}

// MaterialsLen returns the number of entries in the material side table,
// including the sentinel at index 0.
func (l *List) MaterialsLen() int {
	l.ensure()
	return len(l.materials)
}

// Len returns the number of operations in the list.
func (l *List) Len() int { return len(l.ops) }

// AppendGlyph appends one positioned glyph to the glyph side table.
//
// The usual sequence is: remember [List.GlyphsLen], append the glyphs of a
// run, then emit one [OpGlyphs] whose Glyphs is the remembered index and whose
// GlyphCount is the difference. Appending one at a time rather than handing
// over a slice is deliberate — the producer of glyphs is a painter reading a
// borrowed *text.Paragraph, and a slice parameter would force it to build an
// intermediate buffer it does not otherwise need.
//
// # Lifetime
//
// This is a copy, and it is the whole point. The shaping result a painter
// reads from is borrowed from the shaping cache and stays valid only until the
// next Layout or Tick call; the display list, by the same rule that already
// governs ops, clips and transforms, stays valid until the next [List.Reset].
// Copying the glyph here, during the frame that shaped it, is what keeps those
// two lifetimes from having to be reconciled at all.
func (l *List) AppendGlyph(g Glyph) { l.glyphs = append(l.glyphs, g) }

// GlyphsLen returns the number of glyphs currently in the side table. It is
// the index the next [List.AppendGlyph] will write to.
func (l *List) GlyphsLen() uint32 { return uint32(len(l.glyphs)) }

// Glyphs returns the count glyphs starting at first, which is normally
// [Op.Glyphs] and [Op.GlyphCount] of an [OpGlyphs] operation.
//
// The slice is borrowed under the same rule as [List.Ops]: it is valid until
// the producer calls [List.Reset]. An out of range request returns nil rather
// than panicking, so that a backend reading a malformed list draws nothing
// instead of taking the process down.
func (l *List) Glyphs(first, count uint32) []Glyph {
	end := uint64(first) + uint64(count)
	if end > uint64(len(l.glyphs)) {
		return nil
	}
	return l.glyphs[first:end:end]
}
