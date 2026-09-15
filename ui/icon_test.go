package ui_test

import (
	"math"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/icon/outline"
	"github.com/torbenschinke/gift/icon/solid"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// This file is the headless half of icons: the mask cache, the tint, the
// density and the allocation contract. There are no pixels in it; the pixels
// are what internal/icon tests, and what an application sees is a display
// list.

// --- a recording render.Images ------------------------------------------------

// maskImages is a [render.Images] that keeps the pixels it was given, so that
// a test can say what was uploaded and not merely how often.
//
// fakeImages in image_test.go would do for the counting, but the interesting
// question here — "are these two icons the same mask" — needs the bytes.
type maskImages struct {
	recs   []maskTex
	budget int
	spent  int

	uploads, resolves, stales int
}

type maskTex struct {
	gen  uint32
	w, h int
	pix  []byte
	live bool
}

func newMaskImages() *maskImages {
	return &maskImages{recs: make([]maskTex, 1), budget: -1}
}

func (m *maskImages) Resolve(h render.ImageHandle) (render.ImageID, bool) {
	i := int(h.ID)
	if i <= 0 || i >= len(m.recs) || !m.recs[i].live || m.recs[i].gen != h.Gen {
		m.stales++
		return 0, false
	}
	m.resolves++
	return h.ID, true
}

func (m *maskImages) Acquire(px render.Pixels) (render.ImageHandle, bool) {
	if px.IsEmpty() {
		return render.ImageHandle{}, false
	}
	if m.budget >= 0 && m.spent >= m.budget {
		return render.ImageHandle{}, false
	}
	m.spent++
	m.uploads++
	// The contract says the pixels are borrowed for the duration of the call,
	// so a copy is not politeness, it is the protocol; see [render.Pixels].
	m.recs = append(m.recs, maskTex{gen: 1, w: px.W, h: px.H, pix: append([]byte(nil), px.Pix...), live: true})
	return render.ImageHandle{ID: render.ImageID(len(m.recs) - 1), Gen: 1}, true
}

func (m *maskImages) Deallocate(h render.ImageHandle) {
	i := int(h.ID)
	if i > 0 && i < len(m.recs) && m.recs[i].live && m.recs[i].gen == h.Gen {
		m.recs[i].live = false
	}
}

func (m *maskImages) evictAll() {
	for i := range m.recs {
		if m.recs[i].live {
			m.recs[i].live = false
			m.recs[i].gen++
		}
	}
}

// iconHarness builds a harness with a recording image service wired in, which
// is what [gift.PaintContext.Images] hands the icon cache.
func iconHarness(t *testing.T, v gift.View, density float64) (*gifttest.Harness, *maskImages) {
	t.Helper()
	ui.ResetIconService()
	im := newMaskImages()
	h := gifttest.New(t, gifttest.Options{
		View:    v,
		Size:    geom.Sz(200, 100),
		Density: density,
	})
	h.App().SetImages(im)
	h.Frame()
	return h, im
}

// --- the tests ---------------------------------------------------------------

// TestAnIconDrawsOneTintedImageOperation is the shape of the whole feature in
// one assertion: an icon is an [render.OpImage] carrying the foreground
// colour, and it is not a new operation kind, a second shader or a path.
func TestAnIconDrawsOneTintedImageOperation(t *testing.T) {
	want := ui.RGB(200, 40, 40)
	h, im := iconHarness(t, ui.Icon(outline.User).Key("i").Foreground(want), 1)
	var got []render.Op
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			got = append(got, op)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%d image operations, want exactly one (%d uploads)", len(got), im.uploads)
	}
	if got[0].Color != want {
		t.Errorf("the operation carries colour %v, want the foreground %v; the mask is coverage "+
			"and the colour has to come from the operation", got[0].Color, want)
	}
	for _, op := range h.Ops() {
		if op.Kind != render.OpImage && op.Kind != render.OpFillRect && op.Kind != render.OpFillRoundRect {
			t.Errorf("an icon emitted a %v operation; it must not need a new kind", op.Kind)
		}
	}
	b := h.Find(gifttest.ByKey("i")).Bounds()
	if b.Width() != ui.DefaultIconSize || b.Height() != ui.DefaultIconSize {
		t.Errorf("the icon measures %v, want %v square", b, float32(ui.DefaultIconSize))
	}
}

// TestTheSameIconInTwoColoursComesFromOneMask is the payoff of "coverage, not
// colour", and it is measured on the uploaded bytes rather than on a counter.
//
// Two icons, same symbol, same size, different foregrounds. There must be one
// upload, both operations must name the same image id, and the two operations
// must carry the two different colours — because a test that only counted
// uploads would also pass if the second icon had silently drawn in the first
// one's colour.
func TestTheSameIconInTwoColoursComesFromOneMask(t *testing.T) {
	red, blue := ui.RGB(220, 30, 30), ui.RGB(30, 30, 220)
	h, im := iconHarness(t, ui.HStack(
		ui.Icon(outline.User).Key("a").Foreground(red),
		ui.Icon(outline.User).Key("b").Foreground(blue),
	), 1)

	var ops []render.Op
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			ops = append(ops, op)
		}
	}
	if len(ops) != 2 {
		t.Fatalf("%d image operations, want two", len(ops))
	}
	if im.uploads != 1 {
		t.Errorf("%d uploads for one icon in two colours, want one. The mask holds coverage and "+
			"nothing else, so one entry has to serve every colour", im.uploads)
	}
	if ops[0].Image != ops[1].Image {
		t.Errorf("the two icons name image %d and %d; they must share one", ops[0].Image, ops[1].Image)
	}
	if ops[0].Color != red || ops[1].Color != blue {
		t.Errorf("the two operations carry %v and %v, want %v and %v", ops[0].Color, ops[1].Color, red, blue)
	}
	// And the uploaded bytes are premultiplied white — the tint is a
	// multiply, so anything else in the texture would tint the tint.
	tex := im.recs[int(ops[0].Image)]
	for i := 0; i+3 < len(tex.pix); i += 4 {
		p := tex.pix[i : i+4]
		if p[0] != p[3] || p[1] != p[3] || p[2] != p[3] {
			t.Fatalf("pixel %d of the mask is %v; the mask must be premultiplied white, that is "+
				"all four channels equal to the coverage", i/4, p)
		}
	}
}

// TestTwoDifferentIconsDoNotShareAMask is the other direction, so that
// "everything is one upload" could not pass as the sharing above.
func TestTwoDifferentIconsDoNotShareAMask(t *testing.T) {
	_, im := iconHarness(t, ui.HStack(
		ui.Icon(outline.User).Key("a"),
		ui.Icon(solid.User).Key("b"),
		ui.Icon(outline.Bell).Key("c"),
	), 1)
	if im.uploads != 3 {
		t.Errorf("%d uploads for three different symbols, want three", im.uploads)
	}
}

// TestTwoSizesOfOneIconAreTwoMasks pins the other half of the key. One mask
// stretched to two sizes is the blur the project plan, section 18, exists to
// remove, and it is the cheaper mistake to make.
func TestTwoSizesOfOneIconAreTwoMasks(t *testing.T) {
	_, im := iconHarness(t, ui.HStack(
		ui.Icon(outline.User).Key("a").Size(16),
		ui.Icon(outline.User).Key("b").Size(32),
	), 1)
	if im.uploads != 2 {
		t.Fatalf("%d uploads for one symbol at two sizes, want two", im.uploads)
	}
	if got, want := im.recs[1].w, 16; got != want {
		t.Errorf("the first mask is %d pixels, want %d", got, want)
	}
	if got, want := im.recs[2].w, 32; got != want {
		t.Errorf("the second mask is %d pixels, want %d", got, want)
	}
}

// TestAnIconAtTwoXIsRasterisedAtTwoX is the density contract and the whole
// point of the previous work unit, applied to icons: the mask is produced at
// the number of device pixels it will occupy, not produced once and magnified.
//
// The size of the mask is the direct evidence and is checked first. The
// "finer, not bigger" half — that the extra pixels carry more detail rather
// than repeating the ones below them — is measured in
// TestTwoXIsAFinerRasterAndNotAMagnification below, in the same way
// gifttest's TestTwoXIsMeasurablySharper measures it for glyphs.
func TestAnIconAtTwoXIsRasterisedAtTwoX(t *testing.T) {
	for _, tc := range []struct {
		density float64
		want    int
	}{{1, 24}, {2, 48}, {3, 72}} {
		_, im := iconHarness(t, ui.Icon(outline.User).Key("i").Size(24), tc.density)
		if im.uploads != 1 {
			t.Fatalf("density %v: %d uploads, want one", tc.density, im.uploads)
		}
		if got := im.recs[1].w; got != tc.want {
			t.Errorf("at density %v a 24 pixel icon uploaded a %d pixel mask, want %d. A mask "+
				"that stays 24 is one the GPU has to magnify, which is the blur section 18 of "+
				"the project plan removes", tc.density, got, tc.want)
		}
	}
}

// TestTwoXIsAFinerRasterAndNotAMagnification turns "sharper" into a number,
// which the size of the mask cannot do on its own: magnifying a 24 pixel mask
// to 48 pixels would also produce a 48 pixel mask.
//
// The measure is the one gifttest uses for glyphs: the share of *partially
// covered* pixels among the pixels that carry any coverage. An icon edge is
// antialiased, so a pixel on the outline of a stroke is neither empty nor
// full. Magnification turns each such pixel into a block of four and leaves
// the share unchanged. A finer raster does not: the ink area grows with the
// square of the density while the outline grows only with its length, so the
// share must fall by about half.
//
// The assertion is deliberately loose — a fall of at least a quarter — for the
// same reason the glyph one is: the exact factor depends on the shapes. A
// halving is what it should be; a quarter is what it must be for the claim to
// hold at all.
func TestTwoXIsAFinerRasterAndNotAMagnification(t *testing.T) {
	share := func(density float64) (float64, int, int) {
		_, im := iconHarness(t, ui.Icon(outline.User).Key("i").Size(24), density)
		if im.uploads != 1 {
			t.Fatalf("density %v: %d uploads", density, im.uploads)
		}
		tex := im.recs[1]
		ink, partial := 0, 0
		for i := 3; i < len(tex.pix); i += 4 {
			switch c := tex.pix[i]; {
			case c == 0:
			case c == 255:
				ink++
			default:
				ink++
				partial++
			}
		}
		return float64(partial) / float64(ink), ink, tex.w
	}
	r1, ink1, w1 := share(1)
	r2, ink2, w2 := share(2)
	t.Logf("1x: %d pixel mask, %d inked, %.1f %% of them partial", w1, ink1, 100*r1)
	t.Logf("2x: %d pixel mask, %d inked, %.1f %% of them partial", w2, ink2, 100*r2)

	if ink1 == 0 || ink2 == 0 {
		t.Fatal("one of the two masks has no ink at all")
	}
	if got := float64(ink2) / float64(ink1); got < 3.0 || got > 5.0 {
		t.Errorf("the 2x mask has %.2f times the ink of the 1x one, want about 4", got)
	}
	if r2 > 0.75*r1 {
		t.Errorf("the partial coverage share is %.3f at 1x and %.3f at 2x. A mask rasterised at "+
			"2x must have markedly fewer edge pixels per unit of ink; a magnified one would "+
			"have the same share", r1, r2)
	}
}

// TestAnEvictedIconMaskIsRebuiltAndNotDrawnWrong is the handle generation half
// of the [render.Images] contract, which is the reason this cache is thirty
// lines rather than an eviction policy of its own: the backend evicts, the
// handle goes stale, and the next frame notices and re-uploads.
func TestAnEvictedIconMaskIsRebuiltAndNotDrawnWrong(t *testing.T) {
	h, im := iconHarness(t, ui.Icon(outline.User).Key("i"), 1)
	if im.uploads != 1 {
		t.Fatalf("%d uploads on the first frame", im.uploads)
	}
	h.Frame()
	if im.uploads != 1 {
		t.Errorf("a second frame uploaded again (%d total); the cache is not holding", im.uploads)
	}
	im.evictAll()
	h.Frame()
	if im.uploads != 2 {
		t.Errorf("%d uploads after the backend evicted everything, want two: the stale handle "+
			"has to be noticed and the mask rebuilt", im.uploads)
	}
	if im.stales == 0 {
		t.Error("no handle was ever reported stale, so the generation check did not run")
	}
	drawn := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			drawn++
		}
	}
	if drawn != 1 {
		t.Errorf("%d image operations after the eviction, want one", drawn)
	}
}

// TestAnUploadRefusedByTheBudgetIsRetried is the "not an error" half of
// [render.Images.Acquire]: a refusal draws nothing this frame and asks again,
// and it must not be remembered as "this icon has no ink".
func TestAnUploadRefusedByTheBudgetIsRetried(t *testing.T) {
	ui.ResetIconService()
	im := newMaskImages()
	im.budget = 0
	h := gifttest.New(t, gifttest.Options{
		View: ui.Icon(outline.User).Key("i"),
		Size: geom.Sz(200, 100),
	})
	h.App().SetImages(im)
	h.Frame()
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			t.Fatal("an image operation although every upload was refused")
		}
	}
	im.budget = -1
	h.Frame()
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d image operations on the frame after the budget opened up, want one. A "+
			"refused upload that latched would leave the icon invisible for good", n)
	}
}

// TestAZeroSymbolDrawsNothingAndDoesNotPanic is the missing-icon case, which a
// table lookup produces on any ordinary day.
func TestAZeroSymbolDrawsNothingAndDoesNotPanic(t *testing.T) {
	h, im := iconHarness(t, ui.Icon(ui.Symbol{}).Key("i"), 1)
	if im.uploads != 0 {
		t.Errorf("%d uploads for the zero Symbol", im.uploads)
	}
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			t.Error("the zero Symbol drew an image")
		}
	}
	// It still measures and is still selectable, so a layout does not shift
	// when an icon is missing.
	if b := h.Find(gifttest.ByKey("i")).Bounds(); b.Width() != ui.DefaultIconSize {
		t.Errorf("the zero Symbol measures %v, want its declared size", b)
	}
}

// TestAnIconIsSquareAndCentredInANonSquareFrame pins the one layout decision
// that is not forced: a mask is square, so a frame that is not square
// letterboxes rather than distorting the artwork.
func TestAnIconIsSquareAndCentredInANonSquareFrame(t *testing.T) {
	h, _ := iconHarness(t, ui.Icon(outline.User).Key("i").Size(20).Frame(80, 40), 1)
	var op render.Op
	for _, o := range h.Ops() {
		if o.Kind == render.OpImage {
			op = o
		}
	}
	if op.Kind != render.OpImage {
		t.Fatal("no image operation")
	}
	w, hh := op.Bounds.Width(), op.Bounds.Height()
	if math.Abs(float64(w-hh)) > 0.01 {
		t.Errorf("the icon is drawn into %v, which is %v by %v and not square", op.Bounds, w, hh)
	}
	b := h.Find(gifttest.ByKey("i")).Bounds()
	cx, cy := (b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2
	ox, oy := (op.Bounds.Min.X+op.Bounds.Max.X)/2, (op.Bounds.Min.Y+op.Bounds.Max.Y)/2
	if math.Abs(float64(cx-ox)) > 0.01 || math.Abs(float64(cy-oy)) > 0.01 {
		t.Errorf("the icon is centred on (%v,%v) and its frame on (%v,%v)", ox, oy, cx, cy)
	}
}

// TestAnIconFollowsTheTheme is the semantic colour contract of the project
// plan, section 20, applied to icons: the default foreground is a role, so a
// theme switch moves it without the call site saying anything.
func TestAnIconFollowsTheTheme(t *testing.T) {
	prev := ui.CurrentTheme()
	defer ui.SetTheme(nil, prev)

	colourOf := func() ui.Color {
		h, _ := iconHarness(t, ui.Icon(outline.User).Key("i"), 1)
		for _, op := range h.Ops() {
			if op.Kind == render.OpImage {
				return op.Color
			}
		}
		t.Fatal("no image operation")
		return ui.Color{}
	}
	ui.SetTheme(nil, ui.LightTheme())
	light := colourOf()
	ui.SetTheme(nil, ui.DarkTheme())
	dark := colourOf()
	if light == dark {
		t.Errorf("an icon is %v under both themes; its default foreground is not semantic", light)
	}
	if ui.IsSemantic(light) || ui.IsSemantic(dark) {
		t.Errorf("an unresolved semantic colour reached the display list: %v / %v", light, dark)
	}
}

// BenchmarkIconFramePathIsAllocationFree is the 0 B/op contract of the project
// plan, section 11, for an icon that is drawn on every frame.
//
// The harness is warmed first — the first frame rasterises and uploads, which
// is exactly the work the contract exempts — and the loop then measures the
// steady state: one map lookup, one handle resolve and one operation.
//
// It focuses the whole frame and not one node, which for a scene of a single
// stack of icons is the same thing plus the stack itself, and that is the
// honest scope: the claim is "a screen of icons costs nothing per frame", not
// "one function costs nothing".
func BenchmarkIconFramePathIsAllocationFree(b *testing.B) {
	ui.ResetIconService()
	im := newMaskImages()
	h := gifttest.New(b, gifttest.Options{
		View: ui.HStack(
			ui.Icon(outline.User).Size(20),
			ui.Icon(outline.Bell).Size(20),
			ui.Icon(solid.Star).Size(16).Foreground(ui.RGB(240, 180, 40)),
			ui.Icon(solid.BadgeCheck).Size(24),
		).Gap(8),
		Size: geom.Sz(400, 60),
	})
	h.App().SetImages(im)
	h.Frame()
	h.Frame()
	if im.uploads != 4 {
		b.Fatalf("%d uploads after two warm up frames, want four: the benchmark would be "+
			"measuring the rasteriser", im.uploads)
	}
	before := im.uploads
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Frame()
	}
	b.StopTimer()
	if im.uploads != before {
		b.Fatalf("%d uploads during the loop; the cache missed and this measured the miss path",
			im.uploads-before)
	}
}

// TestAGeneratedIconThatNeedsAHoleStillHasOne is the end to end guard on the
// even-odd work, and it is deliberately made on the *shipped* blob rather than
// on the analysis that produced it.
//
// internal/iconsvg tests the reorientation; this tests that the generator
// actually applied it before writing icons.bin. solid/badge-check.svg is one
// of the 45 corpus paths whose even-odd fill differs from its nonzero fill:
// the check mark is punched out of the badge, and both contours wind the same
// way, so a nonzero fill of the untouched geometry is a solid blob.
//
// The measure is enclosed emptiness — a pixel with no coverage that has ink to
// its left, right, above and below. A blob has none. It is checked against
// solid.Star in the same breath, which is a shape with no hole in it, so that
// "this metric finds holes everywhere" could not pass as a result.
func TestAGeneratedIconThatNeedsAHoleStillHasOne(t *testing.T) {
	enclosed := func(sym ui.Symbol) (int, int) {
		_, im := iconHarness(t, ui.Icon(sym).Size(64), 1)
		if im.uploads != 1 {
			t.Fatalf("%d uploads", im.uploads)
		}
		tex := im.recs[1]
		w := tex.w
		cov := func(x, y int) int { return int(tex.pix[(y*w+x)*4+3]) }
		holes, ink := 0, 0
		for y := range w {
			for x := range w {
				if cov(x, y) > 0 {
					ink++
					continue
				}
				left, right, up, down := false, false, false, false
				for i := range x {
					left = left || cov(i, y) > 0
				}
				for i := x + 1; i < w; i++ {
					right = right || cov(i, y) > 0
				}
				for i := range y {
					up = up || cov(x, i) > 0
				}
				for i := y + 1; i < w; i++ {
					down = down || cov(x, i) > 0
				}
				if left && right && up && down {
					holes++
				}
			}
		}
		return holes, ink
	}

	holes, ink := enclosed(solid.BadgeCheck)
	if ink == 0 {
		t.Fatal("solid.BadgeCheck has no ink at all")
	}
	// The check mark is about two units wide across a 24 unit icon, so at 64
	// pixels the hole is a band a few hundred pixels in area, of which 111
	// are enclosed on all four sides — the ends of the mark are not, because
	// they reach towards the edge of the badge. Sixty is comfortably clear of
	// noise and comfortably below the real figure; the alternative, a hole
	// that filled in, scores zero.
	if holes < 60 {
		t.Errorf("solid.BadgeCheck has %d enclosed empty pixels out of %d inked. Its check mark "+
			"is knocked out of the badge by an even-odd fill rule, and the generator has to "+
			"reorient the contours so that the nonzero rasteriser reproduces it; %d means the "+
			"hole filled in", holes, ink, holes)
	}
	t.Logf("solid.BadgeCheck: %d enclosed empty pixels, %d inked", holes, ink)

	// The control: a shape with no hole in it must score zero, so the measure
	// above is measuring holes and not merely concavity.
	if h, _ := enclosed(solid.Star); h != 0 {
		t.Errorf("solid.Star reports %d enclosed empty pixels; it has no hole, so this metric "+
			"is finding something else and the assertion above proves nothing", h)
	}
}

// TestIconCacheStatsReportWhatHappened checks the counters, which are how an
// application sees whether the cache is doing its job. They are plain numbers
// written in the frame path and read out of band, like every other counter in
// gift; see the project plan, section 15.
func TestIconCacheStatsReportWhatHappened(t *testing.T) {
	h, _ := iconHarness(t, ui.HStack(
		ui.Icon(outline.User).Size(20),
		ui.Icon(outline.User).Size(20).Foreground(ui.ColorAccent),
		ui.Icon(solid.Star).Size(20),
	), 1)
	s := ui.IconCacheStats()
	if s.Entries != 2 || s.Rasterised != 2 || s.Uploads != 2 {
		t.Errorf("%+v after one frame with two distinct symbols, want two of each", s)
	}
	h.Frame()
	if after := ui.IconCacheStats(); after.Rasterised != s.Rasterised {
		t.Errorf("a second frame rasterised again (%d, was %d); the counter is the one number "+
			"that says the cache is being defeated", after.Rasterised, s.Rasterised)
	}
}

// TestNewSymbolRejectsNonsense pins the boundary of the one exported
// constructor the generated packages use.
func TestNewSymbolRejectsNonsense(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		box  float32
	}{
		{"no data", nil, 24},
		{"a zero viewBox", []byte{0}, 0},
		{"a negative viewBox", []byte{0}, -1},
	} {
		if s := ui.NewSymbol(tc.data, tc.box); !s.IsZero() {
			t.Errorf("%s produced a non zero Symbol", tc.name)
		}
	}
	if ui.NewSymbol([]byte{0, 0x80, 0x80, 0}, 24).IsZero() {
		t.Error("a well formed empty icon came back as the zero Symbol")
	}
}
