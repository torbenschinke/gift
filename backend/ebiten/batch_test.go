package ebiten

import (
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// addText appends a glyph run for s to l and returns the op index.
func addText(t testing.TB, l *render.List, s string, size float32, at geom.Point, col render.Color) {
	t.Helper()
	first := l.GlyphsLen()
	for _, g := range shapeGlyphs(t, s, size) {
		g.X += at.X
		g.Y += at.Y
		l.AppendGlyph(g)
	}
	l.Add(render.Op{
		Kind:   render.OpGlyphs,
		Bounds: geom.Rc(at.X, at.Y-size, at.X+400, at.Y+size),
		Color:  col,
		Glyphs: first, GlyphCount: l.GlyphsLen() - first,
	})
}

func addRect(l *render.List, r geom.Rect, col render.Color) {
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: r, Color: col})
}

func submit(t testing.TB, r *Renderer, c *capture, l *render.List) {
	t.Helper()
	r.BeginFrame(geom.Sz(800, 600))
	r.Submit(l)
	r.EndFrame()
}

// TestShapesOnlySceneIsOneDrawCall is the baseline the text numbers are read
// against, and it is unchanged by this work unit: however many rectangles a
// frame contains, they are one batch.
func TestShapesOnlySceneIsOneDrawCall(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	for i := range 200 {
		f := float32(i)
		addRect(&l, geom.Rc(f, f, f+10, f+10), render.RGB(1, 2, 3))
	}
	submit(t, r, c, &l)
	if c.batches != 1 {
		t.Fatalf("200 rectangles produced %d draw calls, want 1: %v", c.batches, c.mats)
	}
}

// TestTextIsInterleavedNotReordered is the property the project plan,
// section 11, actually asks for: display list order is drawing order, and a
// batch ends where the material changes. Text drawn between two shapes stays
// between them.
//
// The cheap way to get back to one draw call would be to collect the text of a
// frame and draw it last. gift does not, and the visible consequence would be
// a label sliding in front of a panel that was declared after it.
func TestTextIsInterleavedNotReordered(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	addRect(&l, geom.Rc(0, 0, 100, 50), render.RGB(10, 10, 10))
	addText(t, &l, "first", 16, geom.Point{X: 10, Y: 30}, render.RGB(255, 255, 255))
	addRect(&l, geom.Rc(0, 60, 100, 110), render.RGB(20, 20, 20))
	addText(t, &l, "second", 16, geom.Point{X: 10, Y: 90}, render.RGB(255, 255, 255))
	addRect(&l, geom.Rc(0, 120, 100, 170), render.RGB(30, 30, 30))

	submit(t, r, c, &l)

	want := []Material{MaterialShape, MaterialGlyph, MaterialShape, MaterialGlyph, MaterialShape}
	if len(c.mats) != len(want) {
		t.Fatalf("got %d batches %v, want %d %v", len(c.mats), c.mats, len(want), want)
	}
	for i := range want {
		if c.mats[i] != want[i] {
			t.Fatalf("batch sequence %v, want %v", c.mats, want)
		}
	}
	s := r.Stats()
	if s.ShapeBatches != 3 || s.GlyphBatches != 2 {
		t.Errorf("shape batches %d, glyph batches %d; want 3 and 2", s.ShapeBatches, s.GlyphBatches)
	}
	t.Logf("interleaved scene: %d draw calls (%d shape, %d glyph), %d glyph quads",
		s.Batches, s.ShapeBatches, s.GlyphBatches, s.GlyphQuads)
}

// TestAdjacentTextIsOneBatch: consecutive text on the same atlas page does not
// pay per operation. A batch ends where the *material* changes, not where the
// operation does.
func TestAdjacentTextIsOneBatch(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	addRect(&l, geom.Rc(0, 0, 100, 50), render.RGB(10, 10, 10))
	for i := range 8 {
		addText(t, &l, "row", 14, geom.Point{X: 10, Y: float32(60 + 20*i)}, render.RGB(255, 255, 255))
	}
	submit(t, r, c, &l)
	if len(c.mats) != 2 || c.mats[0] != MaterialShape || c.mats[1] != MaterialGlyph {
		t.Fatalf("batches %v, want one shape then one glyph batch", c.mats)
	}
}

// TestRealisticSceneDrawCalls reports the number the brief asks for, on a
// layout that looks like something: a background, a header plate with a title,
// and twelve rows each of which is a plate, a label and a value.
//
// It asserts the shape of the answer, not a magic constant: one batch per run
// of same-material operations, which is 2*rows + a few.
func TestRealisticSceneDrawCalls(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()

	const rows = 12
	addRect(&l, geom.Rc(0, 0, 800, 600), render.RGB(18, 20, 26))
	addRect(&l, geom.Rc(12, 12, 788, 84), render.RGB(220, 120, 60))
	addText(t, &l, "Library", 24, geom.Point{X: 28, Y: 56}, render.RGB(255, 255, 255))
	for i := range rows {
		y := float32(100 + 38*i)
		addRect(&l, geom.Rc(12, y, 788, y+32), render.RGB(38, 43, 56))
		addText(t, &l, "Recents", 14, geom.Point{X: 24, Y: y + 22}, render.RGB(240, 240, 240))
		addText(t, &l, "1204", 12, geom.Point{X: 700, Y: y + 22}, render.RGB(180, 180, 180))
	}
	submit(t, r, c, &l)

	s := r.Stats()
	// Runs: [bg+header], [title], then rows times [plate], [label+value].
	wantShape := uint64(1 + rows)
	wantGlyph := uint64(1 + rows)
	if s.ShapeBatches != wantShape || s.GlyphBatches != wantGlyph {
		t.Fatalf("draw calls: %d shape + %d glyph, want %d + %d (sequence %v)",
			s.ShapeBatches, s.GlyphBatches, wantShape, wantGlyph, c.mats)
	}
	t.Logf("realistic scene, %d rows: %d draw calls total (%d shape, %d glyph), %d ops, %d glyph quads",
		rows, s.Batches, s.ShapeBatches, s.GlyphBatches, s.Ops, s.GlyphQuads)

	// Same scene without any text, for the comparison the brief asks for.
	r2, c2 := newHeadlessRenderer(t)
	var l2 render.List
	l2.Reset()
	addRect(&l2, geom.Rc(0, 0, 800, 600), render.RGB(18, 20, 26))
	addRect(&l2, geom.Rc(12, 12, 788, 84), render.RGB(220, 120, 60))
	for i := range rows {
		y := float32(100 + 38*i)
		addRect(&l2, geom.Rc(12, y, 788, y+32), render.RGB(38, 43, 56))
	}
	submit(t, r2, c2, &l2)
	if c2.batches != 1 {
		t.Fatalf("the shapes only variant took %d draw calls", c2.batches)
	}
	t.Logf("same scene without text: %d draw call", c2.batches)
}

// TestGlyphAccountingIsTotal keeps the invariant of [RendererStats.Accounted]
// true for the new operation kind: every op lands in exactly one counter.
func TestGlyphAccountingIsTotal(t *testing.T) {
	r, c := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	addRect(&l, geom.Rc(0, 0, 10, 10), render.RGB(1, 2, 3))
	addText(t, &l, "visible", 16, geom.Point{X: 0, Y: 20}, render.RGB(255, 255, 255))
	// Transparent text.
	addText(t, &l, "invisible", 16, geom.Point{X: 0, Y: 40}, render.Color{})
	// A run of nothing but spaces: no ink anywhere.
	addText(t, &l, "   ", 16, geom.Point{X: 0, Y: 60}, render.RGB(255, 255, 255))
	// An empty range.
	l.Add(render.Op{Kind: render.OpGlyphs, Bounds: geom.Rc(0, 0, 1, 1), Color: render.RGB(255, 255, 255)})
	// A run entirely outside its clip.
	cl := l.PushClip(geom.Rc(0, 0, 5, 5))
	first := l.GlyphsLen()
	for _, g := range shapeGlyphs(t, "away", 16) {
		g.X += 4000
		g.Y += 4000
		l.AppendGlyph(g)
	}
	l.Add(render.Op{
		Kind: render.OpGlyphs, Bounds: geom.Rc(4000, 4000, 4400, 4020),
		Color: render.RGB(255, 255, 255), Clip: cl,
		Glyphs: first, GlyphCount: l.GlyphsLen() - first,
	})
	l.PopClip()

	submit(t, r, c, &l)

	s := r.Stats()
	if got, want := s.Accounted(), uint64(l.Len()); got != want {
		t.Fatalf("accounted %d operations, the list has %d", got, want)
	}
	if s.SkippedTransparent != 1 {
		t.Errorf("transparent text counted %d times, want 1", s.SkippedTransparent)
	}
	if s.SkippedEmptyText != 3 {
		t.Errorf("empty text counted %d times, want 3 (spaces, empty range, fully clipped)", s.SkippedEmptyText)
	}
}

// TestGlyphQuadsAreClippedGeometrically checks that a clip halves a glyph
// rather than dropping or keeping it whole: the texture coordinates have to
// follow the cut, or a clipped glyph would be a squashed one.
func TestGlyphQuadsAreClippedGeometrically(t *testing.T) {
	full := glyphVertsUnderClip(t, geom.Rc(-1e6, -1e6, 1e6, 1e6))
	if len(full) == 0 {
		t.Fatal("the unclipped run produced no vertices")
	}
	half := glyphVertsUnderClip(t, geom.Rc(0, 0, 20, 1e6))
	if len(half) == 0 || len(half) >= len(full) {
		t.Fatalf("clipping to 20 px produced %d vertices, unclipped is %d", len(half), len(full))
	}
	// Every surviving vertex is inside the clip, and its texture coordinate is
	// inside the atlas rectangle it came from.
	for _, v := range half {
		if v.DstX < -0.001 || v.DstX > 20.001 {
			t.Fatalf("a clipped vertex sits at x=%v, outside the clip", v.DstX)
		}
		if v.SrcX < 0 || v.SrcY < 0 {
			t.Fatalf("a clipped vertex has a negative texture coordinate (%v, %v)", v.SrcX, v.SrcY)
		}
	}
}

func glyphVertsUnderClip(t testing.TB, clip geom.Rect) []eb.Vertex {
	t.Helper()
	r, _ := newHeadlessRenderer(t)
	var out []eb.Vertex
	r.drawFn = func(m Material, v []eb.Vertex, _ []uint32) {
		if m == MaterialGlyph {
			out = append(out, v...)
		}
	}
	var l render.List
	l.Reset()
	c := l.PushClip(clip)
	first := l.GlyphsLen()
	for _, g := range shapeGlyphs(t, "clipped", 16) {
		g.Y += 16
		l.AppendGlyph(g)
	}
	l.Add(render.Op{
		Kind: render.OpGlyphs, Bounds: geom.Rc(0, 0, 400, 20),
		Color: render.RGB(255, 255, 255), Clip: c,
		Glyphs: first, GlyphCount: l.GlyphsLen() - first,
	})
	l.PopClip()
	r.BeginFrame(geom.Sz(800, 600))
	r.Submit(&l)
	r.EndFrame()
	return out
}

// TestGlyphSubmitIsAllocationFree is the backend half of the frame path
// contract of the project plan, section 11: a warm frame that draws text
// allocates nothing, atlas lookup included.
func TestGlyphSubmitIsAllocationFree(t *testing.T) {
	r, _ := newHeadlessRenderer(t)
	var l render.List
	l.Reset()
	addRect(&l, geom.Rc(0, 0, 800, 600), render.RGB(18, 20, 26))
	for i := range 12 {
		y := float32(100 + 38*i)
		addRect(&l, geom.Rc(12, y, 788, y+32), render.RGB(38, 43, 56))
		addText(t, &l, "The quick brown fox", 14, geom.Point{X: 24, Y: y + 22}, render.RGB(240, 240, 240))
	}
	frame := func() {
		r.BeginFrame(geom.Sz(800, 600))
		r.Submit(&l)
		r.EndFrame()
	}
	for range 10 {
		frame()
	}
	if got := testing.AllocsPerRun(100, frame); got != 0 {
		t.Fatalf("a warm mixed frame allocated %v times per run, want 0", got)
	}
	t.Logf("warm mixed frame: %d draw calls, %d glyph quads, atlas %+v",
		r.Stats().Batches/r.Stats().Frames, r.Stats().GlyphQuads/r.Stats().Frames, r.Atlas().Stats())
}

// BenchmarkMixedSubmit reports what translating a warm mixed frame costs in
// the backend, atlas lookups included.
func BenchmarkMixedSubmit(b *testing.B) {
	r, _ := newHeadlessRenderer(b)
	var l render.List
	l.Reset()
	addRect(&l, geom.Rc(0, 0, 800, 600), render.RGB(18, 20, 26))
	for i := range 12 {
		y := float32(100 + 38*i)
		addRect(&l, geom.Rc(12, y, 788, y+32), render.RGB(38, 43, 56))
		addText(b, &l, "The quick brown fox", 14, geom.Point{X: 24, Y: y + 22}, render.RGB(240, 240, 240))
	}
	for range 8 {
		r.BeginFrame(geom.Sz(800, 600))
		r.Submit(&l)
		r.EndFrame()
	}
	b.ReportAllocs()
	for b.Loop() {
		r.BeginFrame(geom.Sz(800, 600))
		r.Submit(&l)
		r.EndFrame()
	}
}
