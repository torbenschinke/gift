package ui_test

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// The tests share the Roboto that internal/text keeps in its testdata.
//
// It is read from there rather than taken from font/inter on purpose. These
// tests assert measured widths, line breaks and baselines, and every one of
// those numbers belongs to the typeface; internal/text's own tests pin Roboto
// down to its file size and hand-derived metrics, so the two suites have to
// agree on one fixture. A ui test that used Inter would be testing a second
// font for no gain and would have to be re-derived the day Inter is updated.
const testFontPath = "../internal/text/testdata/Roboto-Regular.ttf"

var (
	testFontOnce sync.Once
	testFont     ui.Font
	testFontErr  error
)

func loadTestFont(t testing.TB) ui.Font {
	t.Helper()
	testFontOnce.Do(func() {
		data, err := os.ReadFile(testFontPath)
		if err != nil {
			testFontErr = err
			return
		}
		testFont, testFontErr = ui.LoadFont(data)
	})
	if testFontErr != nil {
		t.Fatalf("load test font: %v", testFontErr)
	}
	return testFont
}

// textOf returns a text view with the shared test font, so that a test never
// depends on whether some other test installed a default.
func textOf(t testing.TB, s string) ui.TextView { return ui.Text(s).Font(loadTestFont(t)) }

// measure runs one update and returns the size the root node reported.
func measure(t testing.TB, v gift.View, viewport geom.Size) (*gift.App, geom.Size) {
	t.Helper()
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View { return v }})
	if err := a.Update(viewport); err != nil {
		t.Fatal(err)
	}
	return a, rootSize(t, a)
}

// rootSize reads the extent of the painted content back out of the display
// list, which is the only public way to observe a node's bounds.
func rootSize(t testing.TB, a *gift.App) geom.Size {
	t.Helper()
	l := a.Paint()
	var min, max geom.Point
	first := true
	for _, op := range l.Ops() {
		if op.Kind == render.OpNone {
			continue
		}
		b := op.Bounds
		if first {
			min, max, first = b.Min, b.Max, false
			continue
		}
		if b.Min.X < min.X {
			min.X = b.Min.X
		}
		if b.Min.Y < min.Y {
			min.Y = b.Min.Y
		}
		if b.Max.X > max.X {
			max.X = b.Max.X
		}
		if b.Max.Y > max.Y {
			max.Y = b.Max.Y
		}
	}
	if first {
		return geom.Size{}
	}
	return geom.Sz(max.X-min.X, max.Y-min.Y)
}

// TestTextSizeIsTheMeasuredText is the load bearing property of the whole text
// stack: what the layout reserves is what internal/text measured, not a
// separately computed approximation. The op bounds in the display list are
// derived from the node bounds, so reading them back proves the number
// travelled all the way through layout.
func TestTextSizeIsTheMeasuredText(t *testing.T) {
	const s = "Measurement drives layout"
	v := textOf(t, s).FontSize(20).Background(ui.RGB(1, 2, 3))
	a, got := measure(t, v, geom.Sz(800, 600))
	_ = a

	if got.W <= 0 || got.H <= 0 {
		t.Fatalf("text measured to %v", got)
	}
	// Shape the same string through the public path a second time and compare.
	// The two must agree exactly: internal/text has one entry point and the
	// layout and the drawing path both go through it.
	ref := ui.MeasureForTest(loadTestFont(t), s, 20, geom.Unbounded())
	if got != ref {
		t.Fatalf("layout reserved %v, internal/text measured %v", got, ref)
	}
	t.Logf("%q at 20 px measured %v", s, got)
}

// TestTextPaddingAddsToTheMeasuredSize checks that padding is layout and not
// decoration: the node grows by it and the glyphs move inside it.
func TestTextPaddingAddsToTheMeasuredSize(t *testing.T) {
	const s = "padded"
	bare := ui.MeasureForTest(loadTestFont(t), s, 16, geom.Unbounded())
	_, got := measure(t, textOf(t, s).FontSize(16).Padding(7).Background(ui.RGB(1, 2, 3)), geom.Sz(800, 600))
	want := geom.Sz(bare.W+14, bare.H+14)
	if got != want {
		t.Fatalf("padded text measured %v, want %v", got, want)
	}
}

// TestTextWrapsAtTheBoundedWidth is the second half of the measurement
// contract: a bounded width produces the expected number of visual lines, and
// the node grows in height rather than in width.
func TestTextWrapsAtTheBoundedWidth(t *testing.T) {
	const s = "one two three four five six seven eight nine ten"
	f := loadTestFont(t)

	single := ui.MeasureForTest(f, s, 16, geom.Unbounded())
	lineH := ui.LineHeightForTest(f, 16)
	if single.H > lineH*1.5 {
		t.Fatalf("the unwrapped string is already %v tall, the fixture is wrong", single.H)
	}

	for _, width := range []float32{single.W/2 + 1, single.W/3 + 1, single.W/4 + 1} {
		wrapped := ui.MeasureForTest(f, s, 16, width)
		lines := ui.LineCountForTest(f, s, 16, width)
		if lines < 2 {
			t.Fatalf("width %v did not wrap at all", width)
		}
		if wrapped.W > width {
			t.Errorf("width %v produced a %v wide result; no word is that long", width, wrapped.W)
		}

		// And the same number of lines reaches the layout.
		_, got := measure(t,
			textOf(t, s).FontSize(16).MaxWidth(width).Background(ui.RGB(1, 2, 3)),
			geom.Sz(2000, 600))
		if got.H != wrapped.H {
			t.Errorf("width %v: layout reserved height %v, measurement says %v", width, got.H, wrapped.H)
		}
		t.Logf("width %6.1f -> %d lines, %v", width, lines, wrapped)
	}
}

// TestUnbreakableWordOverflowsIntoDiagnostics is the overflow model of the
// project plan, section 7, applied to text: a word that cannot fit is not
// broken, not hyphenated and not truncated. It keeps its honest width, the
// node reports the width it was allowed, and the difference is a number in
// gift.Diagnostics like every other overflow.
func TestUnbreakableWordOverflowsIntoDiagnostics(t *testing.T) {
	long := strings.Repeat("W", 40)
	f := loadTestFont(t)
	honest := ui.MeasureForTest(f, long, 16, geom.Unbounded())
	limit := honest.W / 4

	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(textOf(t, long).FontSize(16).MaxWidth(limit).Background(ui.RGB(1, 2, 3)))
	}})
	if err := a.Update(geom.Sz(800, 600)); err != nil {
		t.Fatal(err)
	}
	d := a.Diagnostics()
	if d.OverflowNodes == 0 {
		t.Fatal("an unbreakable word wider than its limit reported no overflow at all")
	}
	want := honest.W - limit
	if d.OverflowExtent < want*0.9 {
		t.Fatalf("overflow extent %v, want about %v", d.OverflowExtent, want)
	}
	t.Logf("word %v wide in a %v limit: %d overflowing nodes, %v px total",
		honest.W, limit, d.OverflowNodes, d.OverflowExtent)

	// The same string with room to breathe overflows nothing. Without this
	// half the test above would pass on a node that always reports overflow.
	b := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(textOf(t, long).FontSize(16).Background(ui.RGB(1, 2, 3)))
	}})
	if err := b.Update(geom.Sz(2000, 600)); err != nil {
		t.Fatal(err)
	}
	if n := b.Diagnostics().OverflowNodes; n != 0 {
		t.Fatalf("the same word with room to spare reported %d overflowing nodes", n)
	}
}

// TestMaxLinesReportsWhatItDropped pins the one modifier that deliberately
// removes content: the height shrinks and the removed height shows up as
// overflow, so nothing disappears without a number.
func TestMaxLinesReportsWhatItDropped(t *testing.T) {
	const s = "one two three four five six seven eight nine ten eleven twelve"
	f := loadTestFont(t)
	full := ui.MeasureForTest(f, s, 16, 120)
	if ui.LineCountForTest(f, s, 16, 120) < 4 {
		t.Skip("the fixture does not wrap enough on this font build")
	}

	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(textOf(t, s).FontSize(16).MaxWidth(120).MaxLines(2).Background(ui.RGB(1, 2, 3)))
	}})
	if err := a.Update(geom.Sz(800, 600)); err != nil {
		t.Fatal(err)
	}
	l := a.Paint()
	var h float32
	for _, op := range l.Ops() {
		if op.Kind == render.OpFillRect {
			h = op.Bounds.Height()
		}
	}
	if h >= full.H {
		t.Fatalf("MaxLines(2) reserved %v, the unlimited text is %v", h, full.H)
	}
	if d := a.Diagnostics(); d.OverflowNodes == 0 {
		t.Fatal("MaxLines dropped lines without reporting overflow")
	}
	t.Logf("MaxLines(2): %v tall instead of %v, overflow %v",
		h, full.H, a.Diagnostics().OverflowExtent)
}

// TestTextOpReachesTheDisplayList is the integration half of the side table
// test in render: real shaping, real glyph ids, and the indices still line up.
func TestTextOpReachesTheDisplayList(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(
			ui.Box().Frame(10, 10).Background(ui.RGB(1, 1, 1)),
			textOf(t, "Hi").FontSize(24).Foreground(ui.RGB(255, 0, 0)),
			ui.Box().Frame(10, 10).Background(ui.RGB(2, 2, 2)),
		)
	}})
	if err := a.Update(geom.Sz(400, 300)); err != nil {
		t.Fatal(err)
	}
	l := a.Paint()

	var kinds []render.OpKind
	var textOp render.Op
	found := false
	for _, op := range l.Ops() {
		kinds = append(kinds, op.Kind)
		if op.Kind == render.OpGlyphs {
			textOp, found = op, true
		}
	}
	if !found {
		t.Fatalf("no glyph op in the display list; kinds were %v", kinds)
	}
	// Display list order is drawing order, and the text sits between the two
	// boxes exactly where the view tree put it.
	want := []render.OpKind{render.OpFillRect, render.OpGlyphs, render.OpFillRect}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("op kinds %v, want %v", kinds, want)
	}

	gs := l.Glyphs(textOp.Glyphs, textOp.GlyphCount)
	if len(gs) != 2 {
		t.Fatalf("%q produced %d glyphs, want 2", "Hi", len(gs))
	}
	for i, g := range gs {
		if g.Font == 0 {
			t.Errorf("glyph %d carries font id 0", i)
		}
		if g.Size != 24 {
			t.Errorf("glyph %d carries size %v, want 24", i, g.Size)
		}
		if g.X != float32(int(g.X)) || g.Y != float32(int(g.Y)) {
			t.Errorf("glyph %d sits at a fractional position (%v, %v); "+
				"the project plan, section 7, rules out subpixel positioning", i, g.X, g.Y)
		}
	}
	if !(gs[1].X > gs[0].X) {
		t.Errorf("the second glyph is not to the right of the first: %v, %v", gs[0].X, gs[1].X)
	}
	if gs[0].Y != gs[1].Y {
		t.Errorf("two glyphs of one line sit on different baselines: %v, %v", gs[0].Y, gs[1].Y)
	}
	if textOp.Color != ui.RGB(255, 0, 0) {
		t.Errorf("the text op carries colour %v, want the foreground", textOp.Color)
	}
}

// TestTextAlignMovesTheLines checks the one modifier that only matters when
// the node is wider than its text.
func TestTextAlignMovesTheLines(t *testing.T) {
	firstX := func(a ui.TextAlign) float32 {
		app := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
			return ui.VStack(textOf(t, "ab").FontSize(16).Align(a).Frame(400, geom.Unbounded()))
		}})
		if err := app.Update(geom.Sz(600, 200)); err != nil {
			t.Fatal(err)
		}
		l := app.Paint()
		for _, op := range l.Ops() {
			if op.Kind == render.OpGlyphs {
				return l.Glyphs(op.Glyphs, op.GlyphCount)[0].X
			}
		}
		t.Fatal("no glyph op")
		return 0
	}
	lead, centre, trail := firstX(ui.AlignLeading), firstX(ui.AlignCenter), firstX(ui.AlignTrailing)
	t.Logf("leading=%v center=%v trailing=%v", lead, centre, trail)
	if !(lead < centre && centre < trail) {
		t.Fatalf("alignment did not order the lines: %v, %v, %v", lead, centre, trail)
	}
}

// TestTextWithoutAFontPanicsWithAnExplanation is the answer to "what happens
// when no font is available". Not a blank screen, and not a log line that
// vanishes because the application configured no logger.
func TestTextWithoutAFontPanicsWithAnExplanation(t *testing.T) {
	saved := ui.DefaultFont()
	ui.SetDefaultFont(ui.Font{})
	defer ui.SetDefaultFont(saved)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("building a Text without a font did not panic")
		}
		msg, _ := r.(string)
		for _, want := range []string{"needs a font", "SetDefaultFont", "LoadFont", "font/inter", "MustFont"} {
			if !strings.Contains(msg, want) {
				t.Errorf("the diagnosis does not mention %q:\n%s", want, msg)
			}
		}
		t.Logf("diagnosis:\n%s", msg)
	}()

	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.Text("no font here"))
	}})
	_ = a.Update(geom.Sz(100, 100))
}

// TestDefaultFontIsUsedWhenTheViewNamesNone checks the other half: an
// application that installed a default never has to repeat it.
func TestDefaultFontIsUsedWhenTheViewNamesNone(t *testing.T) {
	saved := ui.DefaultFont()
	ui.SetDefaultFont(loadTestFont(t))
	defer ui.SetDefaultFont(saved)

	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.Text("default").FontSize(18))
	}})
	if err := a.Update(geom.Sz(400, 200)); err != nil {
		t.Fatal(err)
	}
	l := a.Paint()
	for _, op := range l.Ops() {
		if op.Kind == render.OpGlyphs && op.GlyphCount > 0 {
			return
		}
	}
	t.Fatal("the default font produced no glyphs")
}

// TestTextFramePathIsAllocationFree extends the go/no-go criterion of the
// project plan, section 12, to text: a warm frame that lays out and draws a
// label must not allocate. Layout is allocation free for text it has already
// seen — the precise wording of the project plan, section 11 — and paint reads
// the same cache entry.
func TestTextFramePathIsAllocationFree(t *testing.T) {
	f := loadTestFont(t)
	root := func(*gift.Context) gift.View {
		rows := make([]gift.View, 0, 12)
		for i := range 12 {
			rows = append(rows, ui.HStack(
				ui.Box().Frame(16, 16).Background(ui.RGB(30, 30, 30)),
				ui.Text(labels[i%len(labels)]).Font(f).FontSize(14).Foreground(ui.RGB(240, 240, 240)),
				ui.Spacer(),
				ui.Text(values[i%len(values)]).Font(f).FontSize(12).Foreground(ui.RGB(180, 180, 180)),
			).Gap(8).Padding(4).Background(ui.RGB(40, 44, 56)))
		}
		return ui.VStack(rows...).Gap(6).Padding(12).Background(ui.RGB(18, 20, 26))
	}

	a := gift.New(gift.Options{Root: root})
	step := func() {
		if err := a.Update(geom.Sz(800, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("a warm text frame allocated %v times per run, want 0", got)
	}

	// And with a relayout, which re-measures every label from the cache.
	w := float32(800)
	relayout := func() {
		if w == 800 {
			w = 801
		} else {
			w = 800
		}
		if err := a.Update(geom.Sz(w, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		relayout()
	}
	if got := testing.AllocsPerRun(200, relayout); got != 0 {
		t.Fatalf("a warm text relayout allocated %v times per run, want 0", got)
	}
}

var (
	labels = []string{"Library", "Recents", "Favourites", "Shared", "Trash", "Imports"}
	values = []string{"12", "340", "7", "1 204", "0", "88"}
)

// BenchmarkTextFrame reports what a warm text bearing frame costs. The
// allocation number is the contract; the time is observed, not asserted.
func BenchmarkTextFrame(b *testing.B) {
	f := loadTestFont(b)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		rows := make([]gift.View, 0, 12)
		for i := range 12 {
			rows = append(rows, ui.HStack(
				ui.Box().Frame(16, 16).Background(ui.RGB(30, 30, 30)),
				ui.Text(labels[i%len(labels)]).Font(f).FontSize(14).Foreground(ui.RGB(240, 240, 240)),
				ui.Spacer(),
				ui.Text(values[i%len(values)]).Font(f).FontSize(12).Foreground(ui.RGB(180, 180, 180)),
			).Gap(8).Padding(4).Background(ui.RGB(40, 44, 56)))
		}
		return ui.VStack(rows...).Gap(6).Padding(12).Background(ui.RGB(18, 20, 26))
	}})
	for range 8 {
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
}

// BenchmarkTextFrameAtTwoX is the same warm frame at density 2, and it is here
// for one reason: the zero allocation contract of the project plan,
// section 11, is a contract about the frame path and not about the display it
// happens to be on. The density adds one transform at the root of the display
// list, which is one entry in a slice that is reused across frames, and it
// changes no other arithmetic in Update or Paint.
//
// If this number ever differs from BenchmarkTextFrame's, something in the
// density path is allocating per frame.
func BenchmarkTextFrameAtTwoX(b *testing.B) {
	f := loadTestFont(b)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		rows := make([]gift.View, 0, 12)
		for i := range 12 {
			rows = append(rows, ui.HStack(
				ui.Box().Frame(16, 16).Background(ui.RGB(30, 30, 30)),
				ui.Text(labels[i%len(labels)]).Font(f).FontSize(14).Foreground(ui.RGB(240, 240, 240)),
				ui.Spacer(),
				ui.Text(values[i%len(values)]).Font(f).FontSize(12).Foreground(ui.RGB(180, 180, 180)),
			).Gap(8).Padding(4).Background(ui.RGB(40, 44, 56)))
		}
		return ui.VStack(rows...).Gap(6).Padding(12).Background(ui.RGB(18, 20, 26))
	}})
	a.SetDensity(2)
	for range 8 {
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
}

// TestFramePathAllocatesNothingAtAnyDensity states the same thing as an
// assertion rather than as a reported number, at the three densities gift
// promises. A density is a scale at the root of the display list and nothing
// else in the frame path; the project plan, section 11, does not exempt it.
func TestFramePathAllocatesNothingAtAnyDensity(t *testing.T) {
	f := loadTestFont(t)
	for _, density := range []float64{1, 2, 3} {
		a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
			return ui.VStack(
				ui.Text("Density").Font(f).FontSize(16).Foreground(ui.RGB(240, 240, 240)),
				ui.Text("contract").Font(f).FontSize(12).Foreground(ui.RGB(180, 180, 180)),
			).Gap(6).Padding(12).Background(ui.RGB(18, 20, 26))
		}})
		a.SetDensity(density)
		step := func() {
			_ = a.Update(geom.Sz(400, 300))
			a.Paint()
		}
		for range 16 {
			step()
		}
		if got := testing.AllocsPerRun(200, step); got != 0 {
			t.Errorf("density %v: the warm frame path allocated %v times per run, want 0", density, got)
		}
	}
}
