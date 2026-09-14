package ebiten

import (
	"os"
	"sync"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/internal/text"
	"github.com/torbenschinke/gift/render"
)

// The atlas tests shape real text with the Roboto that internal/text keeps in
// its testdata, because a glyph id without the font it came from is not a test
// of anything. gift ships no font; see ui.SetDefaultFont.
const atlasFontPath = "../../internal/text/testdata/Roboto-Regular.ttf"

var (
	atlasFontOnce sync.Once
	atlasFont     *text.Font
	atlasFontErr  error
)

func testFont(t testing.TB) *text.Font {
	t.Helper()
	atlasFontOnce.Do(func() {
		data, err := os.ReadFile(atlasFontPath)
		if err != nil {
			atlasFontErr = err
			return
		}
		atlasFont, atlasFontErr = text.ParseFont(data)
	})
	if atlasFontErr != nil {
		t.Fatalf("load test font: %v", atlasFontErr)
	}
	return atlasFont
}

// shapeGlyphs turns a string into the display list glyphs a painter would
// emit, which is exactly what the backend consumes.
func shapeGlyphs(t testing.TB, s string, size float32) []render.Glyph {
	t.Helper()
	f := testFont(t)
	p := text.NewShaper(text.Config{}).Layout(text.Request{Text: s, Font: f, Size: size, MaxWidth: 1e6})
	var out []render.Glyph
	for li := range p.Lines {
		ln := &p.Lines[li]
		for ri := range ln.Runs {
			run := &ln.Runs[ri]
			for gi := range run.Glyphs {
				g := &run.Glyphs[gi]
				out = append(out, render.Glyph{
					Font: render.FontID(run.Font.ID()),
					ID:   render.GlyphID(g.ID),
					Size: run.Size,
					X:    g.X, Y: ln.Baseline + g.Y,
				})
			}
		}
	}
	return out
}

// TestAtlasCachesAndSeparatesSizes: the same glyph asked for twice is one
// entry and a hit, and the same glyph at two sizes is two entries — because
// the size is part of the key and a 12 px 'A' is not a scaled 24 px 'A'.
func TestAtlasCachesAndSeparatesSizes(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{})
	small := shapeGlyphs(t, "A", 12)
	large := shapeGlyphs(t, "A", 24)
	if len(small) != 1 || len(large) != 1 {
		t.Fatalf("expected one glyph each, got %d and %d", len(small), len(large))
	}
	if small[0].ID != large[0].ID {
		t.Fatalf("the two sizes shaped to different glyph ids, the fixture is wrong")
	}

	i1, ok := a.Lookup(small[0])
	if !ok {
		t.Fatal("first lookup failed")
	}
	i2, ok := a.Lookup(small[0])
	if !ok || i1 != i2 {
		t.Fatalf("the second lookup of the same glyph produced entry %d, want %d", i2, i1)
	}
	i3, ok := a.Lookup(large[0])
	if !ok {
		t.Fatal("large lookup failed")
	}
	if i3 == i1 {
		t.Fatal("the same glyph at 12 px and at 24 px shares one atlas entry")
	}

	e1, e3 := a.Entry(i1), a.Entry(i3)
	if !(e3.w > e1.w && e3.h > e1.h) {
		t.Errorf("the 24 px entry (%dx%d) is not larger than the 12 px one (%dx%d)", e3.w, e3.h, e1.w, e1.h)
	}

	s := a.Stats()
	if s.Hits != 1 || s.Misses != 2 || s.Rasterised != 2 {
		t.Errorf("counters: hits=%d misses=%d rasterised=%d, want 1/2/2", s.Hits, s.Misses, s.Rasterised)
	}
	if s.Glyphs != 2 {
		t.Errorf("the atlas holds %d glyphs, want 2", s.Glyphs)
	}
	t.Logf("12 px %dx%d, 24 px %dx%d, %d bytes in %d page(s)", e1.w, e1.h, e3.w, e3.h, s.Bytes, s.Pages)
}

// TestAtlasBlankGlyphIsCachedNotRasterised: a space has no ink, and
// rediscovering that once per frame would be the most expensive way to draw
// nothing.
func TestAtlasBlankGlyphIsCachedNotRasterised(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{})
	gs := shapeGlyphs(t, "a b", 16)
	for range 5 {
		for _, g := range gs {
			if _, ok := a.Lookup(g); !ok {
				t.Fatalf("lookup of %+v failed", g)
			}
		}
	}
	s := a.Stats()
	if s.Blanks == 0 {
		t.Fatal("the space was never recorded as blank")
	}
	if s.Rasterised != 2 {
		t.Errorf("rasterised %d outlines, want 2 (the space has none)", s.Rasterised)
	}
	if s.Misses != 3 {
		t.Errorf("%d misses over five identical frames, want 3", s.Misses)
	}
}

// TestAtlasEvictsDeallocatesAndReRasterises is the eviction contract of the
// project plan, sections 7 and 11, in one test: pressure evicts, the page is
// explicitly deallocated rather than left to a finaliser, and a glyph that was
// dropped comes back byte for byte identical.
func TestAtlasEvictsDeallocatesAndReRasterises(t *testing.T) {
	// One tiny page: every few glyphs force an eviction.
	a := NewGlyphAtlas(AtlasConfig{PageSize: 64, MaxPages: 1})
	var deallocated []*eb.Image
	a.onDeallocate = func(img *eb.Image) { deallocated = append(deallocated, img) }

	probe := shapeGlyphs(t, "A", 20)[0]
	i0, ok := a.Lookup(probe)
	if !ok {
		t.Fatal("the probe glyph did not fit an empty page")
	}
	before := a.Entry(i0)
	page0 := a.Page(i0)

	// Fill the page with other glyphs until something is evicted. The frame
	// has to advance, because a page read from during the frame in progress is
	// never evicted — vertices referencing it may already be queued.
	const alphabet = "BCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	for size := 20; a.Stats().PageEvictions == 0 && size < 40; size += 2 {
		a.Tick()
		for _, g := range shapeGlyphs(t, alphabet, float32(size)) {
			a.Lookup(g)
			if a.Stats().PageEvictions > 0 {
				break
			}
		}
	}

	s := a.Stats()
	if s.PageEvictions == 0 {
		t.Fatal("a 64x64 single page atlas never evicted; the budget is not being enforced")
	}
	if s.GlyphEvictions == 0 {
		t.Fatal("a page was evicted but no glyph entries went with it")
	}
	if len(deallocated) == 0 {
		t.Fatal("a page was evicted without Deallocate; the project plan, section 11, requires the explicit release")
	}
	if deallocated[0] != page0 {
		t.Errorf("the deallocated image is not the page that was evicted")
	}
	if s.Pages > 1 {
		t.Errorf("MaxPages is 1 but the atlas holds %d pages", s.Pages)
	}
	t.Logf("%d page evictions, %d glyph evictions, %d deallocated images",
		s.PageEvictions, s.GlyphEvictions, len(deallocated))

	// The probe is gone. Asking again re-rasterises it, and the result must be
	// the same bitmap in a possibly different place.
	a.Tick()
	i1, ok := a.Lookup(probe)
	if !ok {
		t.Fatal("the probe glyph could not be re-added after eviction")
	}
	after := a.Entry(i1)
	if after.w != before.w || after.h != before.h || after.left != before.left || after.top != before.top {
		t.Fatalf("the re-rasterised glyph differs: %+v, was %+v", after, before)
	}
	if !after.inked {
		t.Fatal("the re-rasterised glyph has no ink")
	}
}

// TestAtlasAgeEviction covers the second eviction trigger: a page nobody has
// read from for MaxAge drawn frames goes away even when there is no pressure
// at all.
func TestAtlasAgeEviction(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{MaxAge: 3})
	deallocs := 0
	a.onDeallocate = func(*eb.Image) { deallocs++ }

	for _, g := range shapeGlyphs(t, "age", 16) {
		a.Lookup(g)
	}
	if a.Stats().Pages == 0 {
		t.Fatal("no page was created")
	}
	for range 10 {
		a.Tick()
	}
	s := a.Stats()
	if s.AgeEvictions == 0 || s.Pages != 0 {
		t.Fatalf("after ten idle frames at MaxAge=3: %d age evictions, %d pages left", s.AgeEvictions, s.Pages)
	}
	if deallocs == 0 {
		t.Fatal("an aged out page was dropped without Deallocate")
	}
}

// TestAtlasHitPathIsAllocationFree is the atlas half of the zero allocation
// contract of the project plan, section 11. The lookup is on the frame path,
// once per glyph, so it is the single hottest thing in this package.
func TestAtlasHitPathIsAllocationFree(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{})
	gs := shapeGlyphs(t, "The quick brown fox jumps over the lazy dog", 16)
	if len(gs) < 30 {
		t.Fatalf("the fixture produced only %d glyphs", len(gs))
	}
	warm := func() {
		for i := range gs {
			if _, ok := a.Lookup(gs[i]); !ok {
				t.Fatalf("lookup of glyph %d failed", i)
			}
		}
	}
	warm()
	warm()
	if got := testing.AllocsPerRun(200, warm); got != 0 {
		t.Fatalf("the warm atlas lookup path allocated %v times per run over %d glyphs, want 0", got, len(gs))
	}
	if m := a.Stats().Misses; m != uint64(countDistinct(gs)) {
		t.Errorf("%d misses for %d distinct glyphs", m, countDistinct(gs))
	}
}

func countDistinct(gs []render.Glyph) int {
	seen := map[glyphKey]bool{}
	for _, g := range gs {
		seen[glyphKey{font: g.Font, size: quantize(g.Size), id: g.ID}] = true
	}
	return len(seen)
}

// TestAtlasRejectsAGlyphLargerThanAPage checks the bounded failure: a refusal
// with a counter, not an unbounded texture allocation.
func TestAtlasRejectsAGlyphLargerThanAPage(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{PageSize: 16, MaxPages: 2})
	g := shapeGlyphs(t, "M", 200)[0]
	if _, ok := a.Lookup(g); ok {
		t.Fatal("a 200 px glyph was packed into a 16 px page")
	}
	if a.Stats().Rejected == 0 {
		t.Fatal("the refusal was not counted; missing text would be invisible in the diagnostics")
	}
}

// TestAtlasKeyHasNoSubpixelPhase pins the project plan, section 7: two glyph
// instances that differ only in where they sit on screen share one entry. A
// key that had picked up a fractional phase would quadruple the atlas for
// nothing.
func TestAtlasKeyHasNoSubpixelPhase(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{})
	g := shapeGlyphs(t, "x", 17)[0]
	i0, _ := a.Lookup(g)
	g.X += 0.5
	g.Y -= 0.25
	i1, _ := a.Lookup(g)
	if i0 != i1 {
		t.Fatalf("moving a glyph by half a pixel produced a second atlas entry (%d vs %d)", i0, i1)
	}
	if a.Stats().Rasterised != 1 {
		t.Fatalf("rasterised %d times for one glyph", a.Stats().Rasterised)
	}
}
