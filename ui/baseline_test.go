package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// baselineOf returns the absolute y of the first baseline of the glyph run
// with index i in the display list.
//
// It reads the glyph positions rather than the node bounds on purpose: the
// baseline is where the glyphs actually sit, and asserting on anything else
// would pass even if the alignment moved the box and not the text.
func baselineOf(t *testing.T, l *render.List, i int) float32 {
	t.Helper()
	n := 0
	for _, op := range l.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		if n != i {
			n++
			continue
		}
		g := l.Glyphs(op.Glyphs, op.GlyphCount)
		if len(g) == 0 {
			t.Fatalf("glyph run %d is empty", i)
		}
		return g[0].Y
	}
	t.Fatalf("the list has no glyph run with index %d", i)
	return 0
}

// TestAlignBaselineAlignsLabelsOfDifferentSizes is the first real consumer of
// the baseline channel: two labels of different font sizes sit on one line.
//
// Under the default alignment they do not — a leading aligned row puts both
// tops at the same y, so the bigger label's baseline is far below the smaller
// one's. That difference is what the second half of the test pins, so that the
// first half cannot pass by accident.
func TestAlignBaselineAlignsLabelsOfDifferentSizes(t *testing.T) {
	f := loadTestFont(t)
	row := func(v ui.Stack) *render.List {
		return run(t, ui.ZStack(v), geom.Sz(400, 200))
	}
	big := ui.Text("Ag").Font(f).FontSize(40)
	small := ui.Text("Ag").Font(f).FontSize(12)

	aligned := row(ui.HStack(big, small).Gap(8).AlignBaseline())
	b0, b1 := baselineOf(t, aligned, 0), baselineOf(t, aligned, 1)
	if b0 != b1 {
		t.Errorf("baseline aligned labels sit on %v and %v, want one common line", b0, b1)
	}

	dflt := row(ui.HStack(big, small).Gap(8))
	d0, d1 := baselineOf(t, dflt, 0), baselineOf(t, dflt, 1)
	if d0 == d1 {
		t.Fatalf("the default alignment already aligns the baselines at %v; the test proves nothing", d0)
	}
}

// TestAlignBaselineLeavesARectangleAlone: a child that reports no baseline is
// not guessed at. It keeps the ordinary cross axis alignment.
func TestAlignBaselineLeavesARectangleAlone(t *testing.T) {
	f := loadTestFont(t)
	box := probe(20, 60)
	label := ui.Text("Ag").Font(f).FontSize(12)

	withBaseline := run(t, ui.ZStack(ui.HStack(box, label).Gap(8).AlignBaseline()), geom.Sz(400, 200))
	plain := run(t, ui.ZStack(ui.HStack(box, label).Gap(8)), geom.Sz(400, 200))

	// The rectangle is the first fill in both lists.
	var a, b geom.Rect
	for _, op := range withBaseline.Ops() {
		if op.Kind == render.OpFillRect {
			a = op.Bounds
			break
		}
	}
	for _, op := range plain.Ops() {
		if op.Kind == render.OpFillRect {
			b = op.Bounds
			break
		}
	}
	if !approxRect(a, b) {
		t.Fatalf("the rectangle moved from %v to %v; a child with no baseline must keep its ordinary alignment", b, a)
	}
}

// TestAlignBaselineIsRejectedOnAVStack. A baseline positions a child
// vertically, which in a vertical stack is what the stacking decides. The
// package promises never to accept a modifier it then ignores, so it is
// refused at the call site.
func TestAlignBaselineIsRejectedOnAVStack(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("AlignBaseline on a VStack did not panic")
		}
	}()
	_ = ui.VStack(probe(10, 10)).AlignBaseline()
}

// TestDefaultCrossAlignmentIsUnchanged pins that adding the option changed
// nothing for callers who did not ask for it.
func TestDefaultCrossAlignmentIsUnchanged(t *testing.T) {
	v := ui.HStack(probe(20, 20), probe(20, 60)).Align(geom.Alignment{Y: 0.5})
	l := run(t, ui.ZStack(v), geom.Sz(400, 200))
	wantBounds(t, l,
		geom.RcXYWH(0, 20, 20, 20),
		geom.RcXYWH(20, 0, 20, 60),
	)
}

var _ = gift.View(nil)
