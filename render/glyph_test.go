package render

import (
	"testing"
	"unsafe"

	"github.com/torbenschinke/gift/geom"
)

// TestOpIsStillPlainOldData pins the property the whole display list design
// rests on: an Op contains no pointer, so a frame's worth of them is one flat
// array the collector never has to scan. Adding a slice, a map or an interface
// to Op would break it, and this is the assertion that notices.
func TestOpIsStillPlainOldData(t *testing.T) {
	// A struct with no pointers has a zero pointer-bytes prefix, which is what
	// makes it collector free. Go exposes that only indirectly; the cheap,
	// stable proxy is that Op is exactly as large as the sum of its scalar
	// fields with the documented alignment, which a pointer field would
	// change.
	// 52 before OpGlyphs, 60 after, 64 since OpShadow added Blur, 68 since
	// OpImage added Image, 72 since OpMaterial added Material. The growth is
	// deliberate and argued on Op; the point of this assertion is that it is
	// never accidental.
	const want = 72
	if got := unsafe.Sizeof(Op{}); got != want {
		t.Errorf("sizeof(Op) = %d, want %d; if this grew on purpose, update the number and the note on Op", got, want)
	}
	if got := unsafe.Sizeof(Glyph{}); got != 24 {
		t.Errorf("sizeof(Glyph) = %d, want 24", got)
	}
}

// TestGlyphSideTableIndices is the headless half of "a text op reaches the
// display list with the right side table indices": no font, no shaper, no
// backend, just the mechanism.
func TestGlyphSideTableIndices(t *testing.T) {
	var l List
	l.Reset()

	l.Add(Op{Kind: OpFillRect, Bounds: geom.Rc(0, 0, 10, 10), Color: RGB(1, 2, 3)})

	first := l.GlyphsLen()
	if first != 0 {
		t.Fatalf("a fresh list starts with %d glyphs, want 0", first)
	}
	for i := range 3 {
		l.AppendGlyph(Glyph{Font: 7, ID: GlyphID(100 + i), Size: 16, X: float32(i * 10), Y: 20})
	}
	l.Add(Op{
		Kind: OpGlyphs, Bounds: geom.Rc(0, 0, 30, 20), Color: RGB(9, 9, 9),
		Glyphs: first, GlyphCount: l.GlyphsLen() - first,
	})

	second := l.GlyphsLen()
	l.AppendGlyph(Glyph{Font: 7, ID: 200, Size: 16, X: 0, Y: 40})
	l.Add(Op{Kind: OpGlyphs, Glyphs: second, GlyphCount: l.GlyphsLen() - second})

	ops := l.Ops()
	if len(ops) != 3 {
		t.Fatalf("got %d ops, want 3", len(ops))
	}
	if ops[1].Glyphs != 0 || ops[1].GlyphCount != 3 {
		t.Errorf("first text op spans [%d,+%d), want [0,+3)", ops[1].Glyphs, ops[1].GlyphCount)
	}
	if ops[2].Glyphs != 3 || ops[2].GlyphCount != 1 {
		t.Errorf("second text op spans [%d,+%d), want [3,+1)", ops[2].Glyphs, ops[2].GlyphCount)
	}

	gs := l.Glyphs(ops[1].Glyphs, ops[1].GlyphCount)
	if len(gs) != 3 {
		t.Fatalf("got %d glyphs, want 3", len(gs))
	}
	for i, g := range gs {
		if g.ID != GlyphID(100+i) || g.X != float32(i*10) || g.Font != 7 || g.Size != 16 {
			t.Errorf("glyph %d = %+v", i, g)
		}
	}
	if g := l.Glyphs(ops[2].Glyphs, ops[2].GlyphCount); len(g) != 1 || g[0].ID != 200 {
		t.Errorf("second op resolves to %+v", g)
	}

	// An out of range range is nil, not a panic: a backend fed a malformed
	// list must draw nothing rather than take the process down.
	if g := l.Glyphs(3, 99); g != nil {
		t.Errorf("out of range Glyphs returned %v, want nil", g)
	}
}

// TestResetKeepsGlyphCapacity is the steady state property: the glyph side
// table is subject to the same reuse rule as the ops, the clips and the
// transforms, so a frame that draws the same text as the previous one does not
// allocate.
func TestResetKeepsGlyphCapacity(t *testing.T) {
	var l List
	l.Reset()
	for i := range 500 {
		l.AppendGlyph(Glyph{Font: 1, ID: GlyphID(i), Size: 12})
	}
	before := cap(l.glyphs)
	l.Reset()
	if l.GlyphsLen() != 0 {
		t.Fatalf("Reset left %d glyphs behind", l.GlyphsLen())
	}
	if got := cap(l.glyphs); got != before {
		t.Fatalf("Reset dropped the glyph capacity: %d -> %d", before, got)
	}

	fill := func() {
		l.Reset()
		for i := range 500 {
			l.AppendGlyph(Glyph{Font: 1, ID: GlyphID(i), Size: 12})
		}
		l.Add(Op{Kind: OpGlyphs, Glyphs: 0, GlyphCount: 500})
	}
	fill()
	if got := testing.AllocsPerRun(50, fill); got != 0 {
		t.Fatalf("refilling a warm list allocated %v times per run, want 0", got)
	}
}
