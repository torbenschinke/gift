package render

// FontID is the identity of a loaded font, as the text stack hands it over.
//
// It is an opaque number here on purpose. render must stay renderer neutral
// and must not know what a font is made of; the project plan, section 3, puts
// shaping in internal/text and atlas management in the backend, and a type
// that carried a face or a byte slice across this boundary would erase that
// line. What travels is a number that both sides agree identifies the same
// font, and nothing else.
//
// Zero is not a valid font. internal/text hands out ids starting at one.
type FontID uint64

// GlyphID is the index of a glyph inside its font. It is only meaningful
// together with the [FontID] of the same [Glyph].
type GlyphID uint32

// Glyph is one positioned glyph in the glyph side table of a [List].
//
// # What travels and what the backend looks up
//
// The atlas key the text stack defines is the triple (font, size, glyph id),
// and all three are in this struct. The backend therefore forms the key by
// reading three fields and does exactly one lookup per glyph: it never
// reshapes, never re-measures and never has to walk back to a run or a line to
// find out which font it is looking at.
//
// The obvious alternative — a second side table of runs carrying font and
// size once, with the glyphs holding only an id and a position — would save
// eight bytes per glyph. It was rejected because it buys that with an
// indirection in the innermost loop of the frame path and with a second
// lifetime to document, and because the eight bytes are bounded by what is on
// screen: three thousand visible glyphs cost 72 KiB in a slice that is reused
// for the life of the process, not per frame.
//
// # Positions
//
// X and Y are the glyph origin — the pen position on the baseline — in the
// same coordinate space as the [Op.Bounds] of the operation that references
// this glyph, that is before the transform named by [Op.Xform] is applied.
// They are whole numbers: the project plan, section 7, rules out subpixel
// positioning, and internal/text already rounds. The offset from the origin to
// the top left of the rasterised mask is a property of the font and the size,
// so it lives in the atlas and not here; putting it in the display list would
// mean the same number travelled once per glyph instance instead of once per
// atlas entry.
//
// The colour is not here either. Glyph coverage is a single channel mask, the
// colour comes from [Op.Color], and one op therefore draws a whole run in one
// colour without the atlas ever holding a coloured pixel.
type Glyph struct {
	// Font is the font this glyph is indexed in.
	Font FontID
	// ID is the glyph index inside Font.
	ID GlyphID
	// Size is the em size in pixels the glyph was shaped at and must be
	// rasterised at. It is part of the atlas key: the same glyph at two sizes
	// is two entries.
	Size float32
	// X and Y are the glyph origin on the baseline, in the coordinate space
	// of the owning operation. Both are whole numbers.
	X, Y float32
}
