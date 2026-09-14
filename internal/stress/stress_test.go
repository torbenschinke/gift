package stress

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// rowRects returns the bounds of the row plates of one painted frame.
//
// A row plate is the only rounded fill in this scene that carries RowBg or
// RowHot, so it can be recognised from the display list without the test
// knowing anything about the tree.
func rowRects(l *render.List) []geom.Rect {
	var out []geom.Rect
	for _, op := range l.Ops() {
		if op.Kind != render.OpFillRoundRect {
			continue
		}
		if op.Color != RowBg && op.Color != RowHot {
			continue
		}
		out = append(out, op.Bounds)
	}
	return out
}

func paintScene(t *testing.T, rows, cells int, size geom.Size) (*gift.App, *render.List) {
	t.Helper()
	sc := New(rows, cells)
	a := gift.New(gift.Options{Root: sc.Root})
	if err := a.Update(size); err != nil {
		t.Fatal(err)
	}
	return a, a.Paint()
}

// TestRowsNeitherCollapseNorOverlap is the acceptance check of WU-C2, run
// headless so that it is part of the ordinary test suite rather than something
// somebody has to eyeball in a window.
//
// The defect it pins: a stack handed later children a shrinking remainder as
// their main axis maximum. At forty rows of twelve cells thirty of the forty
// rows reported a height of zero while their own fixed size cells kept their
// real extents, so the scene drew ten rows and a hundred and twenty pixel pile
// of unclipped debris. Forty rows of forty do not fit into this window and are
// not supposed to — but they must be forty honest rows in order, not ten.
func TestRowsNeitherCollapseNorOverlap(t *testing.T) {
	const rows, cells = 40, 12
	_, l := paintScene(t, rows, cells, geom.Sz(1280, 720))

	rects := rowRects(l)
	if len(rects) != rows {
		t.Fatalf("painted %d row plates, want %d; a collapsed row is still a row and must still be drawn",
			len(rects), rows)
	}

	var prev geom.Rect
	for i, r := range rects {
		if h := r.Height(); h <= 0 {
			t.Fatalf("row %d collapsed to a height of %v", i, h)
		}
		if i > 0 {
			if r.Min.Y < prev.Max.Y {
				t.Fatalf("row %d starts at y=%v, above the bottom of row %d at y=%v: the rows overlap",
					i, r.Min.Y, i-1, prev.Max.Y)
			}
			// Every row has the same content, so every row has the same
			// height. A row whose height depends on its index is the
			// signature of the starvation defect.
			if r.Height() != prev.Height() {
				t.Fatalf("row %d is %v high and row %d is %v; a child's size must not depend on its siblings",
					i, r.Height(), i-1, prev.Height())
			}
		}
		prev = r
	}
}

// TestNoEmptyBoundsInTheScene is the renderer side of the same statement. A
// skipped empty bounds op means a node reported a zero extent, and nothing in
// this scene is supposed to.
func TestNoEmptyBoundsInTheScene(t *testing.T) {
	_, l := paintScene(t, 40, 12, geom.Sz(1280, 720))
	for i, op := range l.Ops() {
		if op.Kind == render.OpNone {
			continue
		}
		if op.Bounds.IsEmpty() {
			t.Fatalf("ops[%d] has empty bounds %v: some node collapsed", i, op.Bounds)
		}
	}
}

// TestOverflowIsReportedAndOnlyWhereItIsReal: forty rows do not fit into a
// 720 pixel window, and that is now a number instead of a silent collapse. In
// a window tall enough for them, nothing overflows at all.
func TestOverflowIsReportedAndOnlyWhereItIsReal(t *testing.T) {
	a, _ := paintScene(t, 40, 12, geom.Sz(1280, 720))
	d := a.Diagnostics()
	if d.OverflowNodes != 1 {
		t.Fatalf("OverflowNodes = %d, want exactly the row panel", d.OverflowNodes)
	}
	if d.OverflowExtent <= 0 {
		t.Fatalf("OverflowExtent = %v, want a positive overhang", d.OverflowExtent)
	}

	tall, _ := paintScene(t, 40, 12, geom.Sz(1280, 2400))
	if d := tall.Diagnostics(); d.OverflowNodes != 0 || d.OverflowExtent != 0 {
		t.Fatalf("in a window that fits: OverflowNodes = %d, OverflowExtent = %v, want zero",
			d.OverflowNodes, d.OverflowExtent)
	}
}

// TestHeaderPlateSpelling backs the comment on Scene.header: since WU-D a Box
// is greedy on every bounded axis, so the spelling an older comment said
// "draws nothing at all" in fact paints the plate.
func TestHeaderPlateSpelling(t *testing.T) {
	plate := ui.RGB(1, 2, 3)
	v := ui.ZStack(ui.Box().Background(plate), ui.Box().Frame(10, 10).Background(ui.RGB(9, 9, 9))).
		Frame(200, 60)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View { return v }})
	if err := a.Update(geom.Sz(400, 400)); err != nil {
		t.Fatal(err)
	}
	ops := a.Paint().Ops()
	if len(ops) != 2 {
		t.Fatalf("painted %d ops, want 2", len(ops))
	}
	if ops[0].Color != plate {
		t.Fatalf("ops[0] is not the plate: %+v", ops[0])
	}
	if got, want := ops[0].Bounds, geom.RcXYWH(0, 0, 200, 60); got != want {
		t.Fatalf("the plate is %v, want the whole ZStack %v", got, want)
	}
}

// TestSceneIsSeveralHundredNodes guards the reason this fixture exists. The
// project plan, section 12, criterion 2, wants a non trivial scene; a scene
// that quietly shrank to a dozen nodes would still pass every test above and
// would measure nothing.
func TestSceneIsSeveralHundredNodes(t *testing.T) {
	a, _ := paintScene(t, 12, 14, geom.Sz(1280, 720))
	if n := a.Diagnostics().LiveNodes; n < 300 {
		t.Fatalf("LiveNodes = %d at the default parameters, want several hundred", n)
	}
}

// BenchmarkPaint is the frame path without a build: the display list of an
// unchanged tree. The project plan, section 12, criterion 3, wants 0 B/op
// here after warm-up.
func BenchmarkPaint(b *testing.B) {
	sc := New(12, 14)
	a := gift.New(gift.Options{Root: sc.Root})
	if err := a.Update(geom.Sz(1280, 720)); err != nil {
		b.Fatal(err)
	}
	a.Paint()
	b.ReportAllocs()
	for b.Loop() {
		a.Paint()
	}
}

// BenchmarkUpdatePaint includes the update, which rebuilds only what the tick
// invalidated. It is the one that shows what the memoised rows buy.
func BenchmarkUpdatePaint(b *testing.B) {
	sc := New(12, 14)
	a := gift.New(gift.Options{Root: sc.Root})
	size := geom.Sz(1280, 720)
	if err := a.Update(size); err != nil {
		b.Fatal(err)
	}
	a.Paint()
	b.ReportAllocs()
	for b.Loop() {
		sc.Tick()
		if err := a.Update(size); err != nil {
			b.Fatal(err)
		}
		a.Paint()
	}
}

// TestSceneContainsTextAndShadows is the guard against the defect this scene
// actually had.
//
// It contained neither. A run reported glyph_draw_calls 0, shaped_glyphs 0,
// atlas hits and misses 0 and shadow_ops 0, so every frame time and every
// allocation number quoted for step 2 of the project plan was a number about
// step 1 — the text stack, the atlas and the shadows, which are most of what
// step 2 added, were never executed by the one scene that measures anything.
// A fixture that measures the wrong thing is worse than no fixture, because it
// produces numbers.
func TestSceneContainsTextAndShadows(t *testing.T) {
	const rows, cells = 12, 14
	_, l := paintScene(t, rows, cells, geom.Sz(1280, 720))

	var glyphRuns, glyphs, shadows int
	for _, op := range l.Ops() {
		switch op.Kind {
		case render.OpGlyphs:
			glyphRuns++
			glyphs += int(op.GlyphCount)
		case render.OpShadow:
			shadows++
		}
	}
	t.Logf("%d glyph runs, %d glyphs, %d shadow ops in a %dx%d scene", glyphRuns, glyphs, shadows, rows, cells)

	// Two labels per row, plus the header's two, the badge's one and the
	// footer's two. The bound is deliberately loose: the assertion is "there
	// is real text in here", not "the design is exactly this".
	if want := 2 * rows; glyphRuns < want {
		t.Errorf("the scene paints %d glyph runs, want at least %d; the text stack is not being "+
			"exercised and the measurement is about step 1 of the project plan", glyphRuns, want)
	}
	if glyphs < 200 {
		t.Errorf("the scene paints %d glyphs; that is not a text workload", glyphs)
	}
	// One per row, one on the panel, one on the header, one on the badge.
	if want := rows + 1; shadows < want {
		t.Errorf("the scene paints %d shadow operations, want at least %d; the analytic shadow "+
			"path is not being exercised", shadows, want)
	}
}

// TestSceneIsDeterministic. Two Scenes with the same parameters must produce
// the same display list, or two measurements are not comparable and the
// package documentation's promise is false. Text is the part most likely to
// break it, which is why this test arrives with the text.
func TestSceneIsDeterministic(t *testing.T) {
	size := geom.Sz(1280, 720)
	_, a := paintScene(t, 12, 14, size)
	first := append([]render.Op(nil), a.Ops()...)
	_, b := paintScene(t, 12, 14, size)
	second := b.Ops()

	if len(first) != len(second) {
		t.Fatalf("two identical scenes painted %d and %d operations", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("operation %d differs between two identical scenes:\n  %+v\n  %+v",
				i, first[i], second[i])
		}
	}
}

// TestRowTitlesAreDistinctAndStable. The shaping cache is keyed on the string,
// so a scene whose rows all said the same thing would measure one cache entry
// and forty hits — which is a fine thing to measure, and not the thing this
// scene claims to measure.
func TestRowTitlesAreDistinctAndStable(t *testing.T) {
	s := New(40, 4)
	seen := map[string]bool{}
	for _, v := range s.titles {
		seen[v] = true
	}
	if len(seen) < 10 {
		t.Errorf("40 rows carry only %d distinct titles; the shaping cache would see almost no "+
			"misses and the scene would not exercise it", len(seen))
	}
	again := New(40, 4)
	for i := range s.titles {
		if s.titles[i] != again.titles[i] || s.labels[i] != again.labels[i] {
			t.Fatalf("row %d text differs between two constructions of the same scene", i)
		}
	}
}
