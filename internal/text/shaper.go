package text

import (
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/torbenschinke/gift/geom"
)

// Request fully describes one text layout problem.
//
// It is the cache key and the shaping input at the same time, which is what
// makes measuring and drawing structurally unable to disagree: there is no
// second set of parameters anywhere that only one of the two paths applies.
type Request struct {
	// Text is the string to lay out.
	//
	// Every mandatory break of UAX 14 starts a new line unconditionally,
	// whether it sits in the middle of the text or at the end of it: "\n",
	// a lone "\r", "\r\n" as a single break, vertical tab, form feed, NEL,
	// U+2028 and U+2029. "a\n" and "a\r" are both two lines, the second of
	// them empty. See [paragraphs] for why that is spelled out here rather
	// than left to the segmenter of the line wrapper.
	Text string
	// Font is the font to shape with. It must not be nil.
	Font *Font
	// Size is the em size in pixels. It must be finite and greater than zero
	// and is quantised to 1/64 pixel, the resolution the shaper works in.
	Size float32
	// MaxWidth is the width limit for line breaking, in pixels. Use
	// [geom.Unbounded] to lay the text out on a single line regardless of its
	// width. A limit of zero is legal and means every word overflows.
	//
	// The limit is a *break* limit, not a clamp: a word that does not fit is
	// not broken, and the resulting line is wider than MaxWidth. See
	// [Paragraph.Overflow].
	MaxWidth float32
}

// Config configures the shaping cache of a [Shaper].
type Config struct {
	// MaxBytes is the memory budget of the cache, counting the retained
	// strings and the backing arrays of the cached paragraphs. Zero selects
	// DefaultMaxBytes. The budget is a budget, not a hard limit on process
	// memory: one entry is always kept, even if it exceeds the budget on its
	// own, because evicting the paragraph a caller just asked for would turn
	// every frame into a miss.
	MaxBytes int
	// MaxAge is the number of [Shaper.Tick] calls an entry may go unused
	// before it is evicted. Zero selects DefaultMaxAge. Ticking once per frame
	// makes this a time budget in frames.
	MaxAge uint32
}

// Defaults for [Config].
const (
	// DefaultMaxBytes is one mebibyte, which holds several thousand short
	// labels or a few dozen wrapped paragraphs.
	//
	// It is the budget of the process wide shaper returned by [Default], so
	// it is a budget for the *whole scene* and not for one widget. A scene
	// that paints more distinct paragraphs than fit in it misses on all of
	// them on every frame; see "The budget is scene wide, and falling off it
	// is a cliff" in the package documentation.
	DefaultMaxBytes = 1 << 20
	// DefaultMaxAge is 600 ticks, ten seconds at sixty frames per second.
	DefaultMaxAge = 600
)

// Stats is a snapshot of the shaping cache counters.
//
// The counters are plain numbers written in the frame path without formatting,
// boxing or locking, as the project plan, section 15, requires. Take a snapshot
// outside the frame path and log that, if anything.
type Stats struct {
	// Hits is the number of [Shaper.Layout] calls answered from the cache.
	Hits uint64
	// Misses is the number of [Shaper.Layout] calls that had to shape.
	Misses uint64
	// Evictions is the number of entries dropped because of the byte budget.
	Evictions uint64
	// AgeEvictions is the number of entries dropped by [Shaper.Tick] because
	// they went unused for longer than Config.MaxAge.
	AgeEvictions uint64
	// ShapedGlyphs is the total number of glyphs produced by misses. It is the
	// honest measure of shaping work; the number of misses alone says nothing
	// about how much text was shaped.
	ShapedGlyphs uint64
	// Entries is the number of paragraphs currently cached.
	Entries int
	// Bytes is the current accounted size of the cache.
	Bytes int
}

// HitRatio returns Hits / (Hits + Misses), or zero if there were no lookups.
func (s Stats) HitRatio() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// cacheKey identifies a layout problem.
//
// Size and width are stored as quantised integers rather than as float32 for
// two reasons: a NaN key would never match itself and would leak an entry per
// lookup, and two sizes that differ by less than the 1/64 pixel the shaper can
// represent produce identical output and should share an entry.
type cacheKey struct {
	text  string
	font  *Font
	size  int32
	width int32
}

// widthUnbounded is the quantised width of an unbounded request. No finite
// width can reach it, so it cannot collide.
const widthUnbounded = math.MaxInt32

// Shaper turns [Request] values into [Paragraph] values and caches the result.
//
// The zero Shaper is not usable; call [NewShaper]. A Shaper is not safe for
// concurrent use; see the package documentation.
type Shaper struct {
	hb      shaping.HarfbuzzShaper
	wrapper shaping.LineWrapper

	cfg Config

	entries []*entry
	index   map[cacheKey]int32
	free    []int32
	head    int32 // most recently used, -1 when empty
	tail    int32 // least recently used, -1 when empty
	bytes   int
	tick    uint64

	stats Stats

	// scratch buffers, reused across misses.
	runes []rune
	// runeBytes[i] is the byte offset of runes[i] inside the current source
	// paragraph, and runeBytes[len(runes)] is its length. It exists so that
	// mapping a glyph cluster back to a byte offset is a lookup and not a walk
	// of the string, which would make a long line quadratic.
	runeBytes []int32
	outs      []shaping.Output
	blank     []bool
	tmpRuns   []runRange
	tmpLines  []lineRange
	paras     []source
}

// entry is one cached paragraph plus its place in the LRU list.
type entry struct {
	key cacheKey
	par Paragraph

	glyphs []Glyph
	runs   []Run
	lines  []Line

	bytes int
	used  uint64
	prev  int32
	next  int32

	// tok is the borrow generation of this entry, shared with every
	// Paragraph handed out from it. It is allocated once and never replaced;
	// see [borrowToken].
	tok *borrowToken
}

// runRange and lineRange hold the layout of a paragraph while it is being
// built, because the final Run and Line values must point into backing arrays
// that are not appended to any more.
type runRange struct {
	start, end int
	advance    float32
}

type lineRange struct {
	runStart, runEnd int
	width            float32
	// advance is the width including the trailing whitespace width excludes;
	// see [Line.Advance].
	advance   float32
	byteStart int
	byteEnd   int
}

// NewShaper returns a Shaper with the given cache configuration.
func NewShaper(cfg Config) *Shaper {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = DefaultMaxAge
	}
	return &Shaper{
		cfg:   cfg,
		index: make(map[cacheKey]int32),
		head:  -1,
		tail:  -1,
	}
}

// Measure returns the extent of the text described by req.
//
// It is defined as Layout(req).Size and is not allowed to be anything else.
// Every width gift lays out with is therefore the width of a glyph run that
// exists, and the backend draws that very run. See the package documentation.
func (s *Shaper) Measure(req Request) geom.Size { return s.Layout(req).Size }

// Layout lays out req and returns the result.
//
// The returned pointer is borrowed from the cache: it stays valid until the
// entry is evicted, which can happen at the next Layout or [Shaper.Tick] call.
// Callers in the frame path read it and drop it; nobody keeps it.
//
// A cache hit performs no allocation. A miss shapes the text and allocates;
// see the package documentation on the allocation boundary.
//
// Layout panics on a malformed request - a nil font, a size that is not
// positive and finite, a negative or NaN width - because those are programming
// errors in the caller and not conditions text can recover from; see the
// project plan, section 15.
func (s *Shaper) Layout(req Request) *Paragraph {
	key := s.validate(req)
	if i, ok := s.index[key]; ok {
		s.stats.Hits++
		s.touch(i)
		return &s.entries[i].par
	}
	s.stats.Misses++
	return s.build(key, req)
}

// Tick advances the age clock of the cache by one and evicts everything that
// has not been used for Config.MaxAge ticks.
//
// Call it once per frame, outside the measurement path. It allocates nothing
// and does no work at all when nothing has aged out.
func (s *Shaper) Tick() {
	s.tick++
	if uint64(s.cfg.MaxAge) > s.tick {
		return
	}
	deadline := s.tick - uint64(s.cfg.MaxAge)
	for s.tail >= 0 && s.entries[s.tail].used <= deadline {
		s.evictTail()
		s.stats.AgeEvictions++
	}
}

// Stats returns a snapshot of the cache counters.
func (s *Shaper) Stats() Stats {
	st := s.stats
	st.Entries = len(s.index)
	st.Bytes = s.bytes
	return st
}

func (s *Shaper) validate(req Request) cacheKey {
	if req.Font == nil {
		panic("gift/internal/text: Request.Font is nil; a request must name the font it is shaped with")
	}
	if !(req.Size > 0) || !isFinite(req.Size) {
		panic(fmt.Sprintf("gift/internal/text: Request.Size is %v; it must be finite and greater than zero", req.Size))
	}
	width := int32(widthUnbounded)
	switch {
	case math.IsNaN(float64(req.MaxWidth)) || req.MaxWidth < 0:
		panic(fmt.Sprintf("gift/internal/text: Request.MaxWidth is %v; use geom.Unbounded() for no limit, not a negative or NaN value", req.MaxWidth))
	case isFinite(req.MaxWidth):
		w := int64(math.Round(float64(req.MaxWidth) * 64))
		if w >= widthUnbounded {
			// A limit this large cannot be reached by any text we can shape;
			// treating it as unbounded keeps the sentinel unique.
			w = widthUnbounded - 1
		}
		width = int32(w)
	}
	return cacheKey{
		text:  req.Text,
		font:  req.Font,
		size:  int32(math.Round(float64(req.Size) * 64)),
		width: width,
	}
}

// --- shaping ---------------------------------------------------------------

// build shapes a request that missed the cache and installs the result.
func (s *Shaper) build(key cacheKey, req Request) *Paragraph {
	i := s.alloc()
	e := s.entries[i]
	e.key = key
	e.glyphs = e.glyphs[:0]
	e.runs = e.runs[:0]
	e.lines = e.lines[:0]
	s.tmpRuns = s.tmpRuns[:0]
	s.tmpLines = s.tmpLines[:0]

	size := quantum(req.Size)
	metrics := req.Font.Metrics(size)
	bounded := isFinite(req.MaxWidth)
	maxWidth := req.MaxWidth
	if !bounded {
		maxWidth = 0
	}

	// Pass one: shape every source paragraph and record the visual lines as
	// index ranges into e.glyphs. Nothing may point into e.glyphs yet, because
	// appending to it can move the backing array.
	s.paras = paragraphs(s.paras[:0], req.Text)
	for _, para := range s.paras {
		s.shapeParagraph(e, req.Font, size, para, bounded, maxWidth)
	}

	// Pass two: materialise the runs, then the lines. Runs first and lines
	// second, because a Line points into the run array and that array must be
	// final before it is sliced.
	for _, r := range s.tmpRuns {
		e.runs = append(e.runs, Run{
			Font:    req.Font,
			Size:    size,
			Glyphs:  e.glyphs[r.start:r.end:r.end],
			Advance: r.advance,
		})
	}
	var widest float32
	for n, l := range s.tmpLines {
		if l.width > widest {
			widest = l.width
		}
		e.lines = append(e.lines, Line{
			Baseline: metrics.FirstBaseline + float32(n)*metrics.LineHeight,
			Width:    l.width,
			Advance:  l.advance,
			Runs:     e.runs[l.runStart:l.runEnd:l.runEnd],
			Start:    l.byteStart,
			End:      l.byteEnd,
		})
	}

	last := e.lines[len(e.lines)-1]
	e.par = Paragraph{
		Size:     geom.Sz(widest, last.Baseline+ceil(metrics.Descent)),
		Metrics:  metrics,
		Lines:    e.lines,
		Overflow: overflowOf(widest, req.MaxWidth),
	}
	if borrowChecks {
		// Stamp this incarnation of the entry. evictTail bumps the shared
		// counter, so a borrow that outlives its eviction disagrees with it.
		if e.tok == nil {
			e.tok = &borrowToken{}
		}
		e.par.tok, e.par.gen = e.tok, e.tok.gen
	}

	s.stats.ShapedGlyphs += uint64(len(e.glyphs))
	s.account(i)
	s.index[key] = i
	s.pushFront(i)
	s.evictToBudget(i)
	return &s.entries[i].par
}

// shapeParagraph shapes one source paragraph, breaks it into visual lines and
// appends those lines to the temporary ranges of s.
//
// A source paragraph is the text between two explicit newlines; an empty one
// still produces one visual line, so that "a\n\nb" is three lines and not two.
func (s *Shaper) shapeParagraph(e *entry, f *Font, size float32, para source, bounded bool, maxWidth float32) {
	if para.text == "" {
		s.tmpLines = append(s.tmpLines, lineRange{
			runStart:  len(s.tmpRuns),
			runEnd:    len(s.tmpRuns),
			byteStart: para.start,
			byteEnd:   para.end,
		})
		return
	}

	s.runes = s.runes[:0]
	s.runeBytes = s.runeBytes[:0]
	for i, r := range para.text {
		s.runes = append(s.runes, r)
		s.runeBytes = append(s.runeBytes, int32(i))
	}
	s.runeBytes = append(s.runeBytes, int32(len(para.text)))

	out := s.hb.Shape(shaping.Input{
		Text:      s.runes,
		RunStart:  0,
		RunEnd:    len(s.runes),
		Direction: di.DirectionLTR,
		Face:      f.face,
		Size:      fixed.Int26_6(math.Round(float64(size) * 64)),
		Script:    scriptOf(s.runes),
		Language:  language.DefaultLanguage(),
	})
	s.outs = append(s.outs[:0], out)

	// The unbounded case runs through the line wrapper as well, with a limit
	// no text can reach. Giving it its own short cut would mean two code
	// paths that can drift apart - the trailing whitespace of a line is
	// trimmed by the wrapper, so a short cut would make an unbounded
	// measurement wider than a measurement against a very large finite width,
	// for the same string. One path, one answer.
	s.wrapper.Prepare(shaping.WrapConfig{
		Direction: di.DirectionLTR,
		// Never: a word that is too long is never broken in the middle. It
		// overflows instead, honestly and visibly; see Paragraph.Overflow.
		BreakPolicy: shaping.Never,
	}, s.runes, shaping.NewSliceIterator(s.outs))

	limit := fixed.Int26_6(math.MaxInt32)
	if bounded {
		limit = fixed.Int26_6(math.Round(float64(maxWidth) * 64))
	}
	runeStart, emitted := 0, 0
	for {
		line, done := s.wrapper.WrapNextLineF(limit)
		if len(line.Line) > 0 || emitted == 0 {
			s.emitLine(e, para, line.Line, s.byteAt(runeStart), s.byteAt(line.NextLine),
				f26(line.TrimmedTrailingWhitespace))
			emitted++
		}
		runeStart = line.NextLine
		if done {
			return
		}
	}
}

// byteAt returns the byte offset of rune index i inside the current source
// paragraph.
func (s *Shaper) byteAt(i int) int {
	switch {
	case i <= 0:
		return 0
	case i >= len(s.runeBytes):
		return int(s.runeBytes[len(s.runeBytes)-1])
	default:
		return int(s.runeBytes[i])
	}
}

// emitLine copies the glyphs of one visual line into the entry and records the
// run and line ranges.
// trimmed is the advance the line wrapper zeroed on the final whitespace
// glyph before this function ever saw it; it is part of [Line.Advance] and of
// nothing else. The wrapper reports it and does not undo it, so it has to be
// carried in rather than recovered here — the glyph's own advance is already
// zero by then.
func (s *Shaper) emitLine(e *entry, para source, outs []shaping.Output, byteStart, byteEnd int, trimmed float32) {
	runStart := len(s.tmpRuns)
	lineStart := len(e.glyphs)
	s.blank = s.blank[:0]
	var pen float32
	for i := range outs {
		o := &outs[i]
		glyphStart := len(e.glyphs)
		x := pen
		for j := range o.Glyphs {
			g := &o.Glyphs[j]
			adv := f26(g.Advance)
			e.glyphs = append(e.glyphs, Glyph{
				ID:      GlyphID(g.GlyphID),
				X:       round(x + f26(g.XOffset)),
				Y:       round(-f26(g.YOffset)),
				Advance: adv,
				Cluster: int32(para.start + s.byteAt(g.TextIndex())),
			})
			// A glyph with no ink is a space as far as line measurement is
			// concerned; the shaper reports its extent, we do not have to
			// guess from the rune.
			s.blank = append(s.blank, g.Width == 0)
			x += adv
		}
		s.tmpRuns = append(s.tmpRuns, runRange{start: glyphStart, end: len(e.glyphs)})
		pen = x
	}

	// The advance including the trailing whitespace, taken before that
	// whitespace is zeroed below, because afterwards it cannot be recovered
	// from the glyphs. See [Line.Advance] for who needs it.
	advance := trimmed
	for _, g := range e.glyphs[lineStart:] {
		advance += g.Advance
	}

	// Trailing whitespace does not contribute to the width of a line. It stays
	// in the glyph list, because a caller mapping a byte offset to a position
	// still needs it, but its advance is zeroed, so that the width of a line is
	// always the sum of the advances of its glyphs and a label that happens to
	// end in a space is not wider than the text it shows.
	//
	// This is done here rather than left to the line wrapper because the
	// wrapper of typesetting v0.3.5 zeroes exactly one final whitespace glyph
	// (shaping/wrapping.go, postProcessLine), so "x  " would keep one of its
	// two spaces. Trimming one space but not two is not a rule anybody can
	// explain to a user.
	for j := len(e.glyphs) - 1; j >= lineStart && s.blank[j-lineStart]; j-- {
		e.glyphs[j].Advance = 0
	}

	var width float32
	for i := runStart; i < len(s.tmpRuns); i++ {
		r := &s.tmpRuns[i]
		var adv float32
		for _, g := range e.glyphs[r.start:r.end] {
			adv += g.Advance
		}
		r.advance = adv
		width += adv
	}

	s.tmpLines = append(s.tmpLines, lineRange{
		runStart:  runStart,
		runEnd:    len(s.tmpRuns),
		width:     width,
		advance:   advance,
		byteStart: para.start + byteStart,
		byteEnd:   para.start + byteEnd,
	})
}

// source is one explicit paragraph of the request text together with its byte
// range, so that the reported line ranges refer to Request.Text and not to a
// substring of it.
type source struct {
	text       string
	start, end int
}

// paragraphs splits text at every mandatory break, appending to dst.
//
// dst is a reused scratch buffer of the shaper.
//
// # Which breaks, and why all of them
//
// The mandatory break characters of UAX 14 class BK, plus the carriage return
// and the line feed of classes CR and LF: "\n", "\r", "\r\n" as one break,
// vertical tab, form feed, NEL (U+0085), LINE SEPARATOR (U+2028) and
// PARAGRAPH SEPARATOR (U+2029).
//
// This used to split on "\n" alone and the documentation of [Request.Text]
// claimed the rest were handled by the segmenter inside the line wrapper. They
// were, in the middle of a text, and not at the end of one: "a\n" produced two
// lines and "a\r" produced one, because a trailing break only becomes a second
// line box if somebody creates it, and the wrapper does not. A rule that holds
// everywhere except at the end of the string is not a rule anybody can
// remember, so the split is done here for all of them and the wrapper never
// sees a mandatory break character at all.
//
// The byte ranges reported on a [Line] still refer to Request.Text and still
// include the break that ended the line, exactly as before.
func paragraphs(dst []source, text string) []source {
	start := 0
	for i := 0; i < len(text); {
		n := breakLen(text, i)
		if n == 0 {
			i += runeLen(text, i)
			continue
		}
		dst = append(dst, source{text: text[start:i], start: start, end: i + n})
		i += n
		start = i
	}
	return append(dst, source{text: text[start:], start: start, end: len(text)})
}

// breakLen returns the length in bytes of the mandatory break starting at i,
// or zero when there is none there.
func breakLen(s string, i int) int {
	switch s[i] {
	case '\n', '\v', '\f':
		return 1
	case '\r':
		// CRLF is one break, not two empty lines.
		if i+1 < len(s) && s[i+1] == '\n' {
			return 2
		}
		return 1
	case 0xC2:
		// U+0085 NEL.
		if i+1 < len(s) && s[i+1] == 0x85 {
			return 2
		}
	case 0xE2:
		// U+2028 LINE SEPARATOR and U+2029 PARAGRAPH SEPARATOR.
		if i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9) {
			return 3
		}
	}
	return 0
}

// runeLen is the length of the UTF-8 sequence starting at i, and at least one
// so that invalid input cannot stall the scan.
func runeLen(s string, i int) int {
	_, n := utf8.DecodeRuneInString(s[i:])
	if n < 1 {
		return 1
	}
	return n
}

// scriptOf returns the script to shape with: the first rune that has one.
//
// gift shapes a paragraph as a single run with a single script, which is the
// honest consequence of having neither bidi nor a fallback chain. Mixed script
// text is shaped under the script of its first strong rune.
func scriptOf(runes []rune) language.Script {
	for _, r := range runes {
		switch sc := language.LookupScript(r); sc {
		case language.Common, language.Inherited, language.Unknown:
			continue
		default:
			return sc
		}
	}
	return language.Latin
}

func overflowOf(width, limit float32) float32 {
	if !isFinite(limit) {
		return 0
	}
	if d := width - limit; d > 0 {
		return d
	}
	return 0
}

// f26 converts a 26.6 fixed point number to pixels.
func f26(v fixed.Int26_6) float32 { return float32(v) / 64 }

func round(v float32) float32 { return float32(math.Round(float64(v))) }
