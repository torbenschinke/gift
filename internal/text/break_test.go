package text

import (
	"math"
	"testing"

	"github.com/torbenschinke/gift/geom"
)

// width is the measured width of one string on a single unbounded line. The
// line breaking expectations below are derived from it: "this word fits into
// the limit, the next one does not".
func width(t testing.TB, s *Shaper, f *Font, size float32, text string) float32 {
	t.Helper()
	return s.Measure(Request{Text: text, Font: f, Size: size, MaxWidth: geom.Unbounded()}).W
}

func TestBreakAtWordBoundaries(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const size = 16
	const text = "hello brave world"

	// Hand derived: a limit that holds "hello brave" but not "hello brave
	// world" must produce exactly the break after "brave".
	limit := width(t, s, f, size, "hello brave") + 1
	if limit >= width(t, s, f, size, text) {
		t.Fatalf("the test premise is wrong: %v already holds the whole string", limit)
	}
	req := Request{Text: text, Font: f, Size: size, MaxWidth: limit}
	p := s.Layout(req)
	if got, want := lineTexts(req, p), []string{"hello brave ", "world"}; !equalStrings(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if p.IsOverflowing() {
		t.Errorf("overflow %v, but every line fits", p.Overflow)
	}
	// Trailing whitespace of a broken line does not count towards its width.
	if got, want := p.Lines[0].Width, width(t, s, f, size, "hello brave"); !nearly(got, want) {
		t.Errorf("first line width %v, want %v, the trailing space must not count", got, want)
	}
}

func TestBreakOneWordPerLine(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const size = 16
	const text = "alpha beta gamma"
	limit := width(t, s, f, size, "gamma") + 1 // the widest single word, nothing more
	req := Request{Text: text, Font: f, Size: size, MaxWidth: limit}
	p := s.Layout(req)
	if got, want := lineTexts(req, p), []string{"alpha ", "beta ", "gamma"}; !equalStrings(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if n := p.LineCount(); n != 3 {
		t.Fatalf("line count %d", n)
	}
}

// TestWordLongerThanTheLimitOverflows pins the decision the package makes for
// the text version of the overflow model of the project plan, section 7: a word
// that cannot fit is neither broken nor truncated, it overflows and says so.
func TestWordLongerThanTheLimitOverflows(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const size = 16
	const word = "Donaudampfschifffahrtsgesellschaft"
	honest := width(t, s, f, size, word)

	const limit = 20
	req := Request{Text: word, Font: f, Size: size, MaxWidth: limit}
	p := s.Layout(req)

	if n := p.LineCount(); n != 1 {
		t.Fatalf("line count %d, want 1: the word must not be broken in the middle", n)
	}
	if !nearly(p.Size.W, honest) {
		t.Errorf("Size.W = %v, want the honest word width %v: no truncation, no scaling", p.Size.W, honest)
	}
	if got, want := p.Overflow, honest-limit; !nearly(got, want) {
		t.Errorf("Overflow = %v, want %v", got, want)
	}
	if !p.IsOverflowing() {
		t.Error("IsOverflowing is false although the line is wider than the limit")
	}
	if g := p.Lines[0].Runs[0].Glyphs; len(g) != len([]rune(word)) {
		// Roboto has no ligature in this word, so one glyph per rune is the
		// expectation; what matters is that nothing was dropped.
		t.Errorf("%d glyphs for %d runes; nothing may be silently truncated", len(g), len([]rune(word)))
	}

	// A word that does not fit still breaks *between* words: only the
	// oversized word itself overflows.
	req2 := Request{Text: "hi " + word + " ho", Font: f, Size: size, MaxWidth: limit}
	p2 := s.Layout(req2)
	if got, want := lineTexts(req2, p2), []string{"hi ", word + " ", "ho"}; !equalStrings(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestZeroWidthLimit(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	req := Request{Text: "a b", Font: f, Size: 16, MaxWidth: 0}
	p := s.Layout(req)
	if got, want := lineTexts(req, p), []string{"a ", "b"}; !equalStrings(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if !p.IsOverflowing() {
		t.Error("a limit of zero must overflow, not collapse")
	}
}

func TestExplicitNewlines(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const size = 16
	m := f.Metrics(size)

	req := Request{Text: "a\n\nb\r\nc", Font: f, Size: size, MaxWidth: geom.Unbounded()}
	p := s.Layout(req)
	if got, want := lineTexts(req, p), []string{"a", "", "b", "c"}; !equalStrings(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	for i, ln := range p.Lines {
		want := m.FirstBaseline + float32(i)*m.LineHeight
		if ln.Baseline != want {
			t.Errorf("line %d baseline %v, want %v", i, ln.Baseline, want)
		}
	}
	if got, want := p.Lines[1].Width, float32(0); got != want {
		t.Errorf("the empty line has width %v", got)
	}
	if len(p.Lines[1].Runs) != 0 {
		t.Errorf("the empty line has %d runs", len(p.Lines[1].Runs))
	}
	// Hand derived: four lines at size 16, ascent 14.84 -> first baseline 15,
	// line height ceil(14.84+3.91) = 19, descent 3.91 -> ceil 4.
	if got, want := p.Size.H, float32(15+3*19+4); got != want {
		t.Errorf("height %v, want %v", got, want)
	}

	// A trailing newline opens a new, empty line. Anything else would make
	// "a\n" and "a" the same height, and a text editor style caret would have
	// nowhere to sit.
	req2 := Request{Text: "a\n", Font: f, Size: size, MaxWidth: geom.Unbounded()}
	if n := s.Layout(req2).LineCount(); n != 2 {
		t.Errorf("\"a\\n\" produced %d lines, want 2", n)
	}
	// Every mandatory break of UAX 14 does the same thing, in the middle of
	// the text and at the end of it.
	//
	// This is the rule that used to hold only in the middle. The split was on
	// "\n" alone and the rest was left to the segmenter inside the line
	// wrapper, which breaks a text but does not open a line box after the last
	// break — so "a\n" was two lines and "a\r" was one, and the documentation
	// claimed they were the same.
	for _, tc := range []struct {
		name string
		text string
		want int
	}{
		{"lone CR in the middle", "a\rb", 2},
		{"trailing CR", "a\r", 2},
		{"trailing CRLF", "a\r\n", 2},
		{"CRLF is one break", "a\r\nb", 2},
		{"trailing vertical tab", "a\v", 2},
		{"trailing form feed", "a\f", 2},
		{"trailing NEL", "a\u0085", 2},
		{"trailing line separator", "a\u2028", 2},
		{"trailing paragraph separator", "a\u2029", 2},
		{"vertical tab in the middle", "a\vb", 2},
		{"NEL in the middle", "a\u0085b", 2},
		{"two trailing breaks", "a\r\r", 3},
		{"mixed", "a\rb\nc\u2028d", 4},
		{"no break at all", "abc", 1},
		{"a lone break", "\r", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Request{Text: tc.text, Font: f, Size: size, MaxWidth: geom.Unbounded()}
			if n := s.Layout(r).LineCount(); n != tc.want {
				t.Errorf("%q produced %d lines, want %d", tc.text, n, tc.want)
			}
		})
	}
}

// TestMandatoryBreakByteRangesStayInOrder: the byte ranges a Line reports
// refer to Request.Text and not to a substring of it, so splitting on more
// break characters must not shift them.
//
// The ranges exclude the break that ended the line — that is existing
// behaviour and is not what this work unit changed — so the assertion is that
// they are ordered, non overlapping and inside the text, not that they tile
// it.
func TestMandatoryBreakByteRangesStayInOrder(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const src = "alpha\rbeta\r\ngamma\u2028delta\n"
	p := s.Layout(Request{Text: src, Font: f, Size: 16, MaxWidth: geom.Unbounded()})

	if p.LineCount() != 5 {
		t.Fatalf("%q produced %d lines, want 5", src, p.LineCount())
	}
	prev := 0
	for i, ln := range p.Lines {
		if ln.Start < prev || ln.End < ln.Start || ln.End > len(src) {
			t.Fatalf("line %d has range [%d,%d) after %d, over a %d byte text",
				i, ln.Start, ln.End, prev, len(src))
		}
		prev = ln.End
	}
	want := []string{"alpha", "beta", "gamma", "delta", ""}
	for i, ln := range p.Lines {
		if got := src[ln.Start:ln.End]; got != want[i] {
			t.Errorf("line %d covers %q, want %q", i, got, want[i])
		}
	}
}

func TestEmptyString(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	m := f.Metrics(16)
	p := s.Layout(Request{Text: "", Font: f, Size: 16, MaxWidth: 100})
	if p.LineCount() != 1 {
		t.Fatalf("empty text produced %d lines, want exactly one empty line box", p.LineCount())
	}
	if p.Size.W != 0 {
		t.Errorf("width %v, want 0", p.Size.W)
	}
	if want := m.FirstBaseline + float32(math.Ceil(float64(m.Descent))); p.Size.H != want {
		t.Errorf("height %v, want one line box %v", p.Size.H, want)
	}
	if p.GlyphCount() != 0 {
		t.Errorf("%d glyphs for the empty string", p.GlyphCount())
	}
}

func TestLeadingAndTrailingWhitespace(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const size = 16
	space := width(t, s, f, size, "x x") - 2*width(t, s, f, size, "x")

	// Leading whitespace is content: it indents the line and counts.
	lead := s.Measure(Request{Text: "  x", Font: f, Size: size, MaxWidth: geom.Unbounded()}).W
	if want := 2*space + width(t, s, f, size, "x"); !nearly(lead, want) {
		t.Errorf("leading whitespace: width %v, want %v", lead, want)
	}
	// Trailing whitespace is not: it would make every label that ends in a
	// space wider than the text it shows.
	trail := s.Measure(Request{Text: "x  ", Font: f, Size: size, MaxWidth: geom.Unbounded()}).W
	if want := width(t, s, f, size, "x"); !nearly(trail, want) {
		t.Errorf("trailing whitespace: width %v, want %v", trail, want)
	}
	// A string of nothing but whitespace is one empty looking line, not zero
	// lines and not an error.
	p := s.Layout(Request{Text: "   ", Font: f, Size: size, MaxWidth: geom.Unbounded()})
	if p.LineCount() != 1 || p.Size.W != 0 {
		t.Errorf("whitespace only: %d lines, width %v; want 1 line of width 0", p.LineCount(), p.Size.W)
	}
}

// TestRTLIsNotSilentlyReordered pins an item from the out of scope list of the
// project plan, section 7. Bidi is not implemented; this test exists so that a
// future reader cannot mistake "untested" for "supported". Hebrew input comes
// out in logical order, which is visually wrong, and that is the documented
// state of affairs.
func TestRTLIsNotSilentlyReordered(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	const hebrew = "\u05d0\u05d1\u05d2" // alef bet gimel
	p := s.Layout(Request{Text: hebrew, Font: f, Size: 32, MaxWidth: geom.Unbounded()})
	if p.LineCount() != 1 {
		t.Fatalf("line count %d", p.LineCount())
	}
	gs := p.Lines[0].Runs[0].Glyphs
	if len(gs) != 3 {
		t.Fatalf("%d glyphs for three runes", len(gs))
	}
	for i := 1; i < len(gs); i++ {
		if gs[i].Cluster < gs[i-1].Cluster {
			t.Fatalf("glyph clusters are not in logical order: %v after %v; this package does not implement bidi and must not pretend to",
				gs[i].Cluster, gs[i-1].Cluster)
		}
		if gs[i].X < gs[i-1].X {
			t.Fatalf("glyphs are laid out right to left; this package is LTR only")
		}
	}
	if gs[0].Cluster != 0 || gs[2].Cluster != 4 {
		t.Errorf("clusters %v/%v, want the first rune at byte 0 and the third at byte 4", gs[0].Cluster, gs[2].Cluster)
	}
}

func TestLayoutRejectsMalformedRequests(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	for _, tc := range []struct {
		name string
		req  Request
	}{
		{"nil font", Request{Text: "x", Size: 16}},
		{"zero size", Request{Text: "x", Font: f, Size: 0}},
		{"negative size", Request{Text: "x", Font: f, Size: -3}},
		{"NaN size", Request{Text: "x", Font: f, Size: float32(math.NaN())}},
		{"negative width", Request{Text: "x", Font: f, Size: 16, MaxWidth: -1}},
		{"NaN width", Request{Text: "x", Font: f, Size: 16, MaxWidth: float32(math.NaN())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Layout accepted a %s request", tc.name)
				}
			}()
			s.Layout(tc.req)
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
