package text

import (
	"testing"

	"github.com/worldiety/gift/geom"
)

// TestLineAdvanceIncludesTrailingWhitespaceThatWidthExcludes is the unit test
// of the one field this package grew for the text field of ui.
//
// Both numbers are needed and they are genuinely different numbers. Width is
// what a label reserves, and it must not grow when the string happens to end
// in a space. Advance is where the line ends, and a caret placed at the end of
// "hi " has to sit after the space or typing a space looks like it did
// nothing.
//
// The test covers one trailing space and two, because the two are trimmed by
// different code: the line wrapper of typesetting zeroes exactly one final
// whitespace glyph and reports what it took away, and this package zeroes the
// rest itself. A sum taken on either side of one of those two steps is short
// by one space, and that is the defect this test exists for.
func TestLineAdvanceIncludesTrailingWhitespaceThatWidthExcludes(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	lay := func(text string) Line {
		return s.Layout(Request{Text: text, Font: f, Size: 16, MaxWidth: geom.Unbounded()}).Lines[0]
	}

	bare := lay("hi")
	if bare.Advance != bare.Width {
		t.Fatalf("a line with no trailing space has Width %v and Advance %v; they must agree",
			bare.Width, bare.Advance)
	}
	// The space of this font at this size, measured through the one entry
	// point rather than assumed.
	space := lay("hi x").Runs[0].Glyphs[2].Advance
	if !(space > 0) {
		t.Fatalf("the fixture is wrong: the space measured %v", space)
	}

	for n, want := range map[int]float32{1: space, 2: 2 * space} {
		ln := lay("hi" + spaces(n))
		if ln.Width != bare.Width {
			t.Errorf("%d trailing space(s) changed the Width from %v to %v; a label that ends "+
				"in a space must not be wider than the text it shows", n, bare.Width, ln.Width)
		}
		if got := ln.Advance - bare.Advance; !closeEnough(got, want) {
			t.Errorf("%d trailing space(s) added %v to the Advance, want %v; a caret at the end "+
				"of the line would land %v short", n, got, want, want-got)
		}
	}
}

func spaces(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}

func closeEnough(a, b float32) bool {
	d := a - b
	// A quarter of a pixel: the advances are quantised to 1/64 and the
	// comparison must not depend on the order the sums were taken in.
	return d < 0.25 && d > -0.25
}
