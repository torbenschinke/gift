package render

import "github.com/torbenschinke/gift/geom"

// OpKind discriminates the drawing operations of an [Op].
//
// New kinds are appended at the end. A backend that does not know a kind must
// skip the operation instead of failing, so that a newer gift version can be
// used with an older backend during development.
type OpKind uint8

const (
	// OpNone is the zero value. It draws nothing and is skipped by backends.
	OpNone OpKind = iota
	// OpFillRect fills Bounds with Color.
	OpFillRect
	// OpFillRoundRect fills Bounds with Color, with corners rounded by
	// CornerRadius.
	OpFillRoundRect
	// OpStrokeRoundRect strokes the outline of Bounds with Color, with
	// corners rounded by CornerRadius and a line width of StrokeWidth.
	// The stroke lies inside Bounds; see the project plan, section 8.
	OpStrokeRoundRect
	// OpGlyphs draws GlyphCount glyphs from the glyph side table of the
	// owning list, starting at index Glyphs, in Color.
	//
	// Bounds is the paint rectangle of the text block. It is not the shape
	// that is filled — the glyph masks are — but it is what a backend culls
	// against and what a reader of a display list dump can make sense of.
	// CornerRadius and StrokeWidth are ignored.
	OpGlyphs
)

// Op is a single drawing operation.
//
// It is plain old data on purpose: no pointers, no slices, no interfaces and
// no maps. Operations live in one flat, reused slice inside a [List], so a
// frame that emits the same operations as the previous one performs no
// allocation at all.
//
// Clip and Xform are indices into the side tables of the owning list rather
// than embedded values, because most operations share the clip and the
// transform of their neighbours and copying a rectangle and a matrix into
// every operation would triple the size of the list. Glyphs and GlyphCount
// follow the same pattern for the same reason, one step further: a text
// operation is a *range* in a third side table, because the number of glyphs
// is not bounded and a variable length payload cannot live in a fixed size
// struct.
//
// Size: 52 bytes before [OpGlyphs] existed, 60 after. The eight bytes are the
// index and the count and buy every operation kind the same flat layout; the
// alternative, reusing CornerRadius and StrokeWidth as an untyped union, would
// have cost nothing and been a float32 quietly holding an array index.
type Op struct {
	// Kind selects how the remaining fields are interpreted.
	Kind OpKind
	// Bounds is the axis aligned target rectangle in the coordinate system
	// selected by Xform.
	Bounds geom.Rect
	// Color is the fill or stroke colour in premultiplied alpha. For
	// [OpGlyphs] it is the text colour, which is where the colour of text
	// comes from: the glyph atlas holds coverage only.
	Color Color
	// CornerRadius is the corner radius for the round rect kinds. It is
	// clamped by the backend to half of the smaller edge.
	CornerRadius float32
	// StrokeWidth is the line width for the stroke kinds.
	StrokeWidth float32
	// Clip is the index of the clip rectangle in the owning list.
	// Index 0 means unclipped; see [List.Clip].
	Clip uint32
	// Xform is the index of the transform in the owning list.
	// Index 0 means identity; see [List.Xform].
	Xform uint32
	// Glyphs is the index of the first glyph of an [OpGlyphs] operation in
	// the glyph side table of the owning list; see [List.Glyphs].
	Glyphs uint32
	// GlyphCount is the number of glyphs of an [OpGlyphs] operation. Zero
	// means the operation draws nothing.
	GlyphCount uint32
}
