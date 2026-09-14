package text

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/go-text/typesetting/font"
)

// ErrBadFont is returned by [ParseFont] when the bytes are not a font this
// package can use. Use errors.Is to detect it; the wrapped error carries the
// detail from the parser.
var ErrBadFont = errors.New("gift/internal/text: not a usable font")

// fontSeq hands out the identity of a loaded font. It is the only global state
// in the package and is never read in the frame path.
var fontSeq atomic.Uint64

// FontID identifies a loaded font.
//
// It is unique per [ParseFont] call, not per font file: loading the same bytes
// twice yields two ids, because the two [Font] values also have two separate
// internal glyph caches and must not be mixed. The id exists so that a glyph
// atlas in the backend can key its pages without holding on to a pointer, and
// so that diagnostics can name a font without printing its family.
type FontID uint64

// GlyphID is the index of a glyph within its font. It is the value the font
// itself uses, not a unicode code point, and it is only meaningful together
// with the [Font] of the [Run] it came from.
type GlyphID uint32

// Font is a parsed font file ready for shaping.
//
// A Font is not safe for concurrent use; see the package documentation. It is
// immutable from the outside: everything exported is derived from the file.
type Font struct {
	face *font.Face
	id   FontID
	upem float32

	// ascent, descent and gap are the horizontal font extents in font units,
	// with descent stored positive downwards. They are read once here so that
	// [Font.Metrics] is a multiplication and not a table lookup.
	ascent, descent, gap float32
}

// ParseFont parses a single TrueType or OpenType font from data.
//
// The data is not copied and must not be modified afterwards; the parser reads
// glyph outlines from it lazily. A font collection (.ttc) is rejected rather
// than silently reduced to its first face, because "which face did I get" is
// not a question a caller should have to guess.
//
// Anything that is not a usable font - truncated data, an image, an empty
// slice, a collection - produces an error wrapping [ErrBadFont].
//
// # Why this recovers a panic
//
// The parser of github.com/go-text/typesetting v0.3.5 does not merely return
// an error for every malformed input: a file whose glyf table is cut short
// panics with an index out of range inside tables.ParseGlyf
// (font/opentype/tables/glyphs_glyf_src.go:50, reached from font.NewFont).
// That was observed, not assumed; the test suite of this package feeds it half
// a real font and pins the behaviour.
//
// A corrupt file on disk is outside world input under the project plan,
// section 15, and must become a value, not a process death. Font loading is
// never in the frame path, so the deferred recover costs nothing that matters.
// The panic is turned into an error rather than re-raised because gift must
// not die from a file the application handed it.
func ParseFont(data []byte) (f *Font, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("%w: the font parser panicked on this data: %v", ErrBadFont, r)
		}
	}()
	return parseFont(data)
}

func parseFont(data []byte) (*Font, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty data", ErrBadFont)
	}
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadFont, err)
	}
	if face == nil || face.Font == nil {
		return nil, fmt.Errorf("%w: parser returned no face", ErrBadFont)
	}
	upem := float32(face.Upem())
	if upem <= 0 || !isFinite(upem) {
		return nil, fmt.Errorf("%w: font has an unusable units per em value of %v", ErrBadFont, upem)
	}
	f := &Font{
		face: face,
		id:   FontID(fontSeq.Add(1)),
		upem: upem,
	}
	ext, ok := face.FontHExtents()
	if !ok {
		// A font without horizontal extents cannot be laid out horizontally
		// at all: there would be no ascent to put a baseline at. Rejecting it
		// here is better than inventing metrics that every line box would
		// then be wrong by.
		return nil, fmt.Errorf("%w: font has no horizontal extents", ErrBadFont)
	}
	f.ascent = ext.Ascender
	f.descent = -ext.Descender // stored positive downwards
	f.gap = ext.LineGap
	if !isFinite(f.ascent) || !isFinite(f.descent) || !isFinite(f.gap) {
		return nil, fmt.Errorf("%w: font has non finite horizontal extents", ErrBadFont)
	}
	return f, nil
}

// ID returns the identity of the loaded font.
func (f *Font) ID() FontID { return f.id }

// UnitsPerEm returns the design grid size of the font, typically 1000 or 2048.
// It is exported because a glyph atlas that rasterises outlines needs it.
func (f *Font) UnitsPerEm() float32 { return f.upem }

// Metrics are the vertical metrics of a font at a concrete size, in pixels.
//
// All values are non negative and point away from the baseline: Ascent goes up,
// Descent goes down. Gap is the leading the font asks for between two lines.
//
// The metrics are font wide, not per line. gift uses one font per paragraph, so
// every line of a paragraph has the same box, which is what makes a list of
// baselines an arithmetic progression and lets a caller compute the y of line n
// without walking the lines before it.
type Metrics struct {
	// Ascent is the distance from the baseline to the top of the line box.
	Ascent float32
	// Descent is the distance from the baseline to the bottom of the line box.
	Descent float32
	// Gap is the additional leading between the bottom of one line box and
	// the top of the next.
	Gap float32
	// LineHeight is the integer distance between two consecutive baselines,
	// that is ceil(Ascent + Descent + Gap). It is an integer because the plan,
	// section 7, requires integer baseline positioning: if the step were
	// fractional, the rounding of baseline n would depend on n and identical
	// lines would be rasterised differently.
	LineHeight float32
	// FirstBaseline is the integer distance from the top of a paragraph to
	// the baseline of its first line, that is ceil(Ascent).
	FirstBaseline float32
}

// Metrics returns the vertical metrics of f at size pixels per em.
//
// The size is quantised to 1/64 pixel first, exactly like the shaping input, so
// that the metrics a caller computes and the metrics a [Paragraph] carries can
// never disagree by a rounding step.
func (f *Font) Metrics(size float32) Metrics {
	size = quantum(size)
	s := size / f.upem
	m := Metrics{
		Ascent:  f.ascent * s,
		Descent: f.descent * s,
		Gap:     f.gap * s,
	}
	m.LineHeight = ceil(m.Ascent + m.Descent + m.Gap)
	m.FirstBaseline = ceil(m.Ascent)
	return m
}

// quantum rounds v to the nearest 1/64, the resolution of the 26.6 fixed point
// numbers the shaper works in. Two sizes that quantise to the same value
// produce bit identical shaping results, which is why the cache key uses the
// quantised value; see cacheKey.
func quantum(v float32) float32 {
	return float32(math.Round(float64(v)*64)) / 64
}

func ceil(v float32) float32 { return float32(math.Ceil(float64(v))) }

func isFinite(v float32) bool {
	return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v))
}
