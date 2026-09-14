package text

import "github.com/torbenschinke/gift/geom"

// Glyph is one positioned glyph of a shaped run.
//
// The position is relative to the origin of the [Line] the glyph belongs to:
// X grows to the right from the left edge of the line, Y grows downwards from
// the baseline of the line. A renderer draws the glyph at
//
//	lineOriginX + g.X, line.Baseline + g.Y
//
// with no further arithmetic and, crucially, without shaping anything again.
//
// X and Y are whole pixels. The project plan, section 7, rules out subpixel
// positioning, and the rounding happens here rather than in the backend so
// that measurement sees the same numbers the rasteriser will: a width that is
// computed from unrounded pen positions and drawn from rounded ones is the
// classic off by a pixel at the end of a long line.
type Glyph struct {
	// ID is the glyph index inside the font of the owning [Run].
	ID GlyphID
	// X is the horizontal position of the glyph origin, relative to the left
	// edge of the line.
	X float32
	// Y is the vertical position of the glyph origin, relative to the
	// baseline, positive downwards. It is non zero only when the font moves a
	// glyph off the baseline, for instance for a mark attachment.
	Y float32
	// Advance is the horizontal advance the shaper assigned to this glyph,
	// including any kerning adjustment. It is not rounded and is therefore
	// not, in general, the difference between two consecutive X values.
	Advance float32
	// Cluster is the index of the first byte in Request.Text that this glyph
	// belongs to. Several glyphs can share a cluster (one rune decomposed) and
	// one glyph can cover several runes (a ligature). The values are non
	// decreasing within a run, because this package never reorders anything;
	// see the package documentation on bidi.
	Cluster int32
}

// Run is a sequence of glyphs from a single font.
//
// There is exactly one run per non empty line today, because a [Request] names
// exactly one font. The type exists anyway so that the addition of a font
// fallback chain, which the project plan, section 7, names as the most likely
// first extension, changes which runs are produced and not what a consumer has
// to look at.
type Run struct {
	// Font is the font the glyphs of this run are indexed in.
	Font *Font
	// Size is the size in pixels the run was shaped at.
	Size float32
	// Glyphs are the positioned glyphs, in logical order.
	Glyphs []Glyph
	// Advance is the sum of the glyph advances of the run, without the
	// trailing whitespace that line breaking trimmed away.
	Advance float32
}

// Line is one visual line of a [Paragraph].
type Line struct {
	// Baseline is the distance from the top of the paragraph to the baseline
	// of this line, in whole pixels. Baselines are an arithmetic progression:
	// line n sits at Metrics.FirstBaseline + n*Metrics.LineHeight.
	Baseline float32
	// Width is the honest advance width of the line, excluding trimmed
	// trailing whitespace. It may exceed the width limit of the request; see
	// [Paragraph.Overflow].
	Width float32
	// Runs are the glyph runs of this line, in logical order. A line produced
	// by an empty paragraph, for example between two consecutive newlines, has
	// no runs but still has a baseline and a height.
	Runs []Run
	// Start and End are the byte offsets into Request.Text that this line
	// covers, including the newline that ended it, if any.
	Start, End int
}

// Paragraph is the result of laying out one [Request]: the complete, positioned
// text.
//
// It is borrowed from the shaping cache and is only valid until the owning
// entry is evicted; see the package documentation.
type Paragraph struct {
	// Size is the extent of the text. Width is the widest line, height is the
	// baseline of the last line plus its descent, both in whole pixels for the
	// height and honest, unrounded pixels for the width.
	//
	// Size is not measured independently of the glyphs: it is computed from
	// exactly the lines below. That is the guarantee the package documentation
	// makes, and the test suite asserts it by recomputing the extent from the
	// glyph runs.
	Size geom.Size

	// Overflow is how far Size.W exceeds the width limit of the request,
	// clamped at zero, and is zero for an unbounded request.
	//
	// A single word that is longer than the limit is *not* broken, hyphenated,
	// scaled or truncated: it keeps its honest width, the line reports that
	// width, and the excess appears here. This is the text form of the
	// overflow model of the project plan, section 7: content that does not fit
	// stays visible and the amount by which it does not fit is a number
	// somebody can read, rather than a silent collapse or a silent ellipsis.
	// A caller who wants the excess cut off clips it, explicitly, in the
	// layer that owns the clip stack.
	Overflow float32

	// Metrics are the vertical font metrics the lines were laid out with.
	Metrics Metrics

	// Lines are the visual lines, top to bottom. A request with an empty text
	// produces exactly one empty line, because an empty label still occupies
	// one line box and a caller that lays out a form must not have the row
	// height jump when the string becomes empty.
	Lines []Line

	// tok and gen identify which incarnation of a cache entry this paragraph
	// is a borrow of, so that reading it after the entry was evicted is a
	// panic instead of a plausible wrong number. Both are nil and zero in a
	// release build and in a Paragraph nobody borrowed; see [Paragraph.checkBorrow].
	tok *borrowToken
	gen uint64
}

// LineCount returns the number of visual lines.
func (p *Paragraph) LineCount() int {
	p.checkBorrow("LineCount")
	return len(p.Lines)
}

// GlyphCount returns the total number of glyphs over all lines and runs. It is
// intended for diagnostics and for sizing a backend side vertex buffer.
func (p *Paragraph) GlyphCount() int {
	p.checkBorrow("GlyphCount")
	n := 0
	for i := range p.Lines {
		for j := range p.Lines[i].Runs {
			n += len(p.Lines[i].Runs[j].Glyphs)
		}
	}
	return n
}

// IsOverflowing reports whether the text is wider than the width limit it was
// laid out against.
func (p *Paragraph) IsOverflowing() bool {
	p.checkBorrow("IsOverflowing")
	return p.Overflow > 0
}
