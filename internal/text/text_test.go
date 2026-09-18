package text

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/worldiety/gift/geom"
)

// The tests use Roboto-Regular.ttf from testdata. It is there because it
// carries the OpenType layout tables the shaping tests need; see
// testdata/LICENSE.md for provenance and licence.
const (
	robotoUPEM     = 2048
	robotoAscent   = 1900 // font units, from the hhea/OS2 derived font extents
	robotoDescent  = 500  // font units, positive downwards
	robotoLineGap  = 0
	robotoFileSize = 168260
)

func loadRoboto(t testing.TB) *Font {
	t.Helper()
	data, err := os.ReadFile("testdata/Roboto-Regular.ttf")
	if err != nil {
		t.Fatalf("read font: %v", err)
	}
	if len(data) != robotoFileSize {
		t.Fatalf("unexpected font file size %d, the hand derived expectations in this file refer to a specific build of Roboto", len(data))
	}
	f, err := ParseFont(data)
	if err != nil {
		t.Fatalf("parse font: %v", err)
	}
	return f
}

func newTestShaper() *Shaper { return NewShaper(Config{}) }

func TestParseFontRejectsNonFonts(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"text", []byte("this is not a font, it is a sentence")},
		{"png header", []byte("\x89PNG\r\n\x1a\n0000000000000000")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := ParseFont(tc.data)
			if err == nil {
				t.Fatalf("ParseFont accepted %q", tc.name)
			}
			if !errors.Is(err, ErrBadFont) {
				t.Errorf("error %v does not wrap ErrBadFont", err)
			}
			if f != nil {
				t.Errorf("got a font back together with an error")
			}
		})
	}
}

func TestParseFontTruncated(t *testing.T) {
	data, err := os.ReadFile("testdata/Roboto-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFont(data[:len(data)/2]); err == nil {
		t.Fatal("a truncated font was accepted")
	} else if !errors.Is(err, ErrBadFont) {
		t.Errorf("error %v does not wrap ErrBadFont", err)
	}
}

func TestFontIDsAreDistinct(t *testing.T) {
	a, b := loadRoboto(t), loadRoboto(t)
	if a.ID() == b.ID() {
		t.Fatalf("two loads share the id %d; they have separate glyph caches and must be distinguishable", a.ID())
	}
}

// TestMetricsAreHandDerived pins the vertical metrics against numbers computed
// from the font tables by hand, not against whatever the implementation
// currently returns.
func TestMetricsAreHandDerived(t *testing.T) {
	f := loadRoboto(t)
	if f.UnitsPerEm() != robotoUPEM {
		t.Fatalf("upem = %v, want %d", f.UnitsPerEm(), robotoUPEM)
	}
	for _, size := range []float32{16, 32, 13.5} {
		want := Metrics{
			Ascent:  robotoAscent * size / robotoUPEM,
			Descent: robotoDescent * size / robotoUPEM,
			Gap:     robotoLineGap * size / robotoUPEM,
		}
		want.LineHeight = float32(math.Ceil(float64(want.Ascent + want.Descent + want.Gap)))
		want.FirstBaseline = float32(math.Ceil(float64(want.Ascent)))
		if got := f.Metrics(size); got != want {
			t.Errorf("Metrics(%v) = %+v, want %+v", size, got, want)
		}
	}
	// Spot check the arithmetic itself, so that a mistake in the formula above
	// cannot hide behind the same mistake in the implementation.
	m := f.Metrics(32)
	if m.Ascent != 29.6875 || m.Descent != 7.8125 || m.LineHeight != 38 || m.FirstBaseline != 30 {
		t.Errorf("Metrics(32) = %+v, want ascent 29.6875, descent 7.8125, line height 38, first baseline 30", m)
	}
}

// TestMeasureIsTheExtentOfTheReturnedRun asserts the property the whole package
// is built around: the size a caller measures is the extent of the glyphs the
// same call returns. It recomputes the extent from the runs instead of trusting
// the field.
func TestMeasureIsTheExtentOfTheReturnedRun(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	for _, req := range []Request{
		{Text: "Hello", Font: f, Size: 16, MaxWidth: geom.Unbounded()},
		{Text: "Hello, world", Font: f, Size: 24, MaxWidth: 60},
		{Text: "one\ntwo\nthree", Font: f, Size: 16, MaxWidth: geom.Unbounded()},
		{Text: "the quick brown fox jumps over the lazy dog", Font: f, Size: 13, MaxWidth: 100},
		{Text: "", Font: f, Size: 16, MaxWidth: 100},
		{Text: "   ", Font: f, Size: 16, MaxWidth: 100},
	} {
		p := s.Layout(req)
		if got := s.Measure(req); got != p.Size {
			t.Errorf("%q: Measure = %v, Layout.Size = %v", req.Text, got, p.Size)
		}

		var widest float32
		for _, ln := range p.Lines {
			var sum float32
			for _, r := range ln.Runs {
				sum += r.Advance
			}
			if !nearly(sum, ln.Width) {
				t.Errorf("%q: line width %v but the run advances sum to %v", req.Text, ln.Width, sum)
			}
			if ln.Width > widest {
				widest = ln.Width
			}
		}
		if !nearly(widest, p.Size.W) {
			t.Errorf("%q: Size.W = %v but the widest line is %v", req.Text, p.Size.W, widest)
		}

		last := p.Lines[len(p.Lines)-1]
		wantH := last.Baseline + float32(math.Ceil(float64(p.Metrics.Descent)))
		if p.Size.H != wantH {
			t.Errorf("%q: Size.H = %v, want last baseline %v plus descent", req.Text, p.Size.H, wantH)
		}
	}
}

// TestShapingIsNotPerRuneAdvances would fail if shaping were replaced by adding
// up per rune advances: it checks a kerning pair and a ligature, both of which
// only exist because the font's GPOS and GSUB tables were applied.
func TestShapingIsNotPerRuneAdvances(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	width := func(text string) float32 {
		return s.Measure(Request{Text: text, Font: f, Size: 64, MaxWidth: geom.Unbounded()}).W
	}
	glyphs := func(text string) []Glyph {
		p := s.Layout(Request{Text: text, Font: f, Size: 64, MaxWidth: geom.Unbounded()})
		return p.Lines[0].Runs[0].Glyphs
	}

	t.Run("kerning", func(t *testing.T) {
		naive := width("A") + width("V")
		shaped := width("AV")
		if !(shaped < naive) {
			t.Fatalf("AV shaped to %v but A+V is %v; the kerning pair was not applied", shaped, naive)
		}
		// The pair is worth a visible amount, not a rounding step.
		if naive-shaped < 1 {
			t.Errorf("kerning of AV is only %v pixels at size 64", naive-shaped)
		}
		// A pair the font does not kern must stay additive, otherwise the
		// test above would also pass for an implementation that subtracts a
		// constant from every pair.
		if got, want := width("AA"), 2*width("A"); !nearly(got, want) {
			t.Errorf("AA = %v, want %v; unkerned pairs must be additive", got, want)
		}
	})

	t.Run("ligature", func(t *testing.T) {
		f, i, fi := glyphs("f"), glyphs("i"), glyphs("fi")
		if len(fi) != 1 {
			t.Fatalf("fi produced %d glyphs, want the single liga glyph", len(fi))
		}
		if fi[0].ID == f[0].ID || fi[0].ID == i[0].ID {
			t.Errorf("the fi glyph id %d is one of the component ids %d/%d", fi[0].ID, f[0].ID, i[0].ID)
		}
		if fi[0].Cluster != 0 {
			t.Errorf("the ligature cluster is %d, want 0: it covers both input bytes", fi[0].Cluster)
		}
		if ffi := glyphs("ffi"); len(ffi) != 1 {
			t.Errorf("ffi produced %d glyphs, want the single ffi liga glyph", len(ffi))
		}
	})
}

func TestGlyphPositionsAreIntegerAndMonotone(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	p := s.Layout(Request{Text: "Wave a flag, VAVAVA.", Font: f, Size: 17, MaxWidth: geom.Unbounded()})
	var prev float32 = -1
	for _, g := range p.Lines[0].Runs[0].Glyphs {
		if g.X != float32(math.Trunc(float64(g.X))) || g.Y != float32(math.Trunc(float64(g.Y))) {
			t.Fatalf("glyph %d sits at a fractional position %v/%v; the plan rules out subpixel positioning", g.ID, g.X, g.Y)
		}
		if g.X < prev {
			t.Fatalf("glyph positions are not monotone: %v after %v", g.X, prev)
		}
		prev = g.X
	}
}

func nearly(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func lineTexts(req Request, p *Paragraph) []string {
	out := make([]string, 0, len(p.Lines))
	for _, ln := range p.Lines {
		out = append(out, strings.TrimRight(req.Text[ln.Start:ln.End], "\r\n"))
	}
	return out
}
