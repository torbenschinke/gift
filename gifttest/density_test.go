package gifttest_test

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// densityLabel is the scene of both golden tests below: one line of text on a
// flat background, large enough to read and small enough that a reviewer can
// compare two PNGs side by side.
//
// It is deliberately the same declaration at both densities. The point of the
// project plan, section 18, is that an application says nothing about the
// display it is on: the density changes the pixels, never the program.
func densityLabel() gifttest.Options {
	return gifttest.Options{
		// The plate is part of the scene and not part of the harness: the
		// harness contributes no pixel to any image in this module, so the
		// view under test paints its own background exactly as a window
		// would. See [ui.Window].
		View: ui.ZStack(
			ui.Box().Background(ui.RGB(20, 24, 34)),
			ui.VStack(
				ui.Text("Density").Key("label").FontSize(24).Foreground(ui.RGB(240, 242, 246)),
			).Padding(12),
		),
		Size: geom.Sz(160, 60),
	}
}

// TestDensityRoundsAFractionalFactor pins the rounding of the project plan,
// section 18, where an application would meet it: a harness — or a monitor —
// reporting 1.5 gets 2, and the frame is 320 by 120 physical pixels for a 160
// by 60 logical viewport. There is no 1.5 anywhere in the output.
//
// It is a headless test. The rounding is arithmetic and needs no GPU, and the
// viewport it does not change is readable without one.
func TestDensityRoundsAFractionalFactor(t *testing.T) {
	opts := densityLabel()
	opts.Density = 1.5
	h := gifttest.New(t, opts)

	if got := h.Density(); got != 2 {
		t.Errorf("Density() = %v for a raw factor of 1.5, want 2", got)
	}
	// The viewport is unchanged, and so is everything a test can select. A
	// density that moved the layout would make a fractional factor a
	// fractional *layout*, which is exactly what section 14 excludes.
	if h.Size() != geom.Sz(160, 60) {
		t.Errorf("viewport %v at density 1.5, want the logical 160x60", h.Size())
	}
	b := h.Find(gifttest.ByKey("label")).Bounds()
	one := gifttest.New(t, densityLabel()).Find(gifttest.ByKey("label")).Bounds()
	if b != one {
		t.Errorf("the label is at %v at density 1.5 and at %v at density 1; bounds are logical", b, one)
	}
}

// TestGlyphGoldenAtTwoX is the golden the project plan, section 18, asks for:
// the same declaration as the 1x glyph golden next door, rendered at density 2.
//
// # What differs from the 1x golden beyond mere size
//
// Both, obviously, in size: testdata/density-2x.png is 320 by 120 where the 1x
// one is 160 by 60, because the golden is the *physical* framebuffer. That on
// its own would be true of the blurry upscale this work unit removes, so the
// test does not stop there.
//
// The difference that matters is the coverage. At 1x the word is drawn from 24
// pixel glyph masks; at 2x it is drawn from 48 pixel masks that were
// rasterised from the same outlines, not from the 24 pixel masks magnified. A
// magnification — which is what Ebitengine's filter did to the whole frame —
// preserves the ratio between the pixels on the edge of a stroke and the
// pixels inside it, because both areas grow by the same factor. A finer raster
// does not: the ink area grows with the square of the density while the edge
// grows only with its length, so the *share* of partially covered pixels
// falls. TestTwoXIsMeasurablySharper below states that as a number.
//
// What has not changed is the geometry: the text sits at twice the coordinate,
// occupies twice the extent and is the same shape. A reviewer comparing the
// two files should see the same picture, twice as large, with smoother stems
// and visibly rounder bowls — not a bolder or a thinner word.
func TestGlyphGoldenAtTwoX(t *testing.T) {
	opts := densityLabel()
	opts.Density = 2
	h := gifttest.New(t, opts)
	h.Find(gifttest.ByKey("label")).AssertDrawsGlyphs()
	h.AssertGolden("density-2x")
}

// TestGlyphGoldenAtOneX is the companion, and it is the regression the target
// platform of the project plan, section 1, needs: a Raspberry Pi renders at
// density 1 and must get exactly the frame it always got.
func TestGlyphGoldenAtOneX(t *testing.T) {
	h := gifttest.New(t, densityLabel())
	h.Find(gifttest.ByKey("label")).AssertDrawsGlyphs()
	h.AssertGolden("density-1x")
}

// TestTwoXIsMeasurablySharper turns "it looks sharper" into a number, which is
// what a golden cannot do: a golden only says that something changed.
//
// The measure is the share of *partially covered* pixels among the pixels that
// carry any ink at all. A glyph edge is antialiased, so a pixel on the outline
// of a stem is neither the background nor the full foreground colour. If a 2x
// frame were the 1x frame magnified, every such pixel would become a block of
// four and the share would be unchanged. If the glyphs were rasterised at 2x,
// the ink area quadruples while the outline only doubles, so the share must
// fall by about half.
//
// The assertion is deliberately loose — a fall of at least a quarter — because
// the exact factor depends on the shapes of the letters, on the rasteriser's
// gamma and on how many strokes are thin enough to be all edge. A halving is
// what it should be; a quarter is what it must be for the claim "rasterised
// finer, not magnified" to hold at all.
func TestTwoXIsMeasurablySharper(t *testing.T) {
	// Behind a golden call, so that a build without giftgpu skips here with
	// the same explanation every other pixel assertion gives instead of
	// failing in Image.
	h2 := gifttest.New(t, withDensity(densityLabel(), 2))
	h2.AssertGolden("density-2x")

	h1 := gifttest.New(t, densityLabel())
	r1, ink1 := edgeShare(h1.Image())
	r2, ink2 := edgeShare(h2.Image())
	t.Logf("1x: %d inked pixels, %.1f %% of them partial", ink1, 100*r1)
	t.Logf("2x: %d inked pixels, %.1f %% of them partial", ink2, 100*r2)

	if ink1 == 0 || ink2 == 0 {
		t.Fatal("one of the two frames drew no text at all")
	}
	if got, want := float64(ink2)/float64(ink1), 4.0; got < 0.75*want || got > 1.25*want {
		t.Errorf("the 2x frame has %.2f times the ink of the 1x one, want about %v", got, want)
	}
	if r2 > 0.75*r1 {
		t.Errorf("partial coverage share is %.3f at 1x and %.3f at 2x; "+
			"a frame rasterised at 2x must have markedly fewer edge pixels per unit of ink, "+
			"a magnified one would have the same share", r1, r2)
	}
}

func withDensity(o gifttest.Options, d float64) gifttest.Options {
	o.Density = d
	return o
}

// edgeShare returns the fraction of inked pixels that are partially covered,
// and the number of inked pixels.
//
// "Inked" means the pixel differs from the background of [densityLabel] at
// all; "partial" means it differs from the background and from the foreground
// by more than a small tolerance in the green channel, which is the channel
// with the largest contrast between the two colours of this scene.
func edgeShare(img image.Image) (float64, int) {
	if img == nil {
		return 0, 0
	}
	const (
		bg = 24  // ui.RGB(20, 24, 34), green channel
		fg = 242 // ui.RGB(240, 242, 246), green channel
		// Eight units of slack at each end, so that the driver-dependent last
		// unit or two of blending that GoldenTolerance exists for cannot turn
		// a flat pixel into an "edge" one.
		slack = 8
	)
	b := img.Bounds()
	ink, partial := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, g, _, _ := img.At(x, y).RGBA()
			v := int(g >> 8)
			if v <= bg+slack {
				continue
			}
			ink++
			if v < fg-slack {
				partial++
			}
		}
	}
	if ink == 0 {
		return 0, 0
	}
	return float64(partial) / float64(ink), ink
}

// TestDensityOneIsBitIdenticalToTheCommittedGoldens is the no-regression proof
// the project plan, section 1, requires of this work unit: the target platform
// is a Raspberry Pi at density 1, and density 1 must be the same pixels at the
// same cost as before.
//
// It is stricter than [gifttest.Harness.AssertGolden] on purpose. A golden
// comparison allows [gifttest.GoldenTolerance] per channel so that it survives
// a change of GPU, and a change that shifted every glyph by a fraction of a
// unit of coverage would pass it. These goldens were committed before the
// density existed, so exact equality against them — every byte of every
// channel — is the statement that the 1x frame did not move at all.
//
// # Why it refuses to run under GIFT_UPDATE_GOLDEN
//
// Because otherwise the guarantee is a circle, and it was one until WU-AA.
// This test used to call [gifttest.Harness.AssertGolden] and then read
// testdata/glyphs.png back. Under GIFT_UPDATE_GOLDEN that first call does not
// compare anything: it *writes* the file from the frame this very test just
// rendered, and the read below then compared the frame against itself.
// Verified at the time by corrupting every green channel of the committed
// golden by 0x20 — the test passed with the variable set and failed without
// it, which is exactly backwards for a test whose whole job is to notice that
// the committed pixels moved.
//
// Reading the file before the assertion would not be enough either, because
// TestGlyphGolden in material_test.go rewrites the same golden and nothing
// fixes the order in which two tests of one package run. So the rule is the
// blunt one: a run that is rewriting goldens is a run that verifies nothing —
// compareGolden says so in its own message — and this test says so by
// failing. The way to regenerate goldens is therefore two commands, an update
// run and a verifying run, and the verifying one is the one that decides.
//
// What remains true, and is now actually true, is the sentence this doc used
// to make without earning it: it compares the *committed* file and nothing
// derived from it, so it cannot be satisfied by rewriting the golden.
func TestDensityOneIsBitIdenticalToTheCommittedGoldens(t *testing.T) {
	if gifttest.Update() {
		t.Fatalf("GIFT_UPDATE_GOLDEN is set. This test compares the committed " +
			"testdata/glyphs.png against a freshly rendered frame, and a run that " +
			"rewrites goldens has nothing left to compare against: it would be " +
			"comparing the frame with itself.\n" +
			"Regenerate the goldens, then run again without the variable; that second " +
			"run is the one that decides whether density 1 still produces the pixels " +
			"the Raspberry Pi target has always had.")
	}
	// Read the committed bytes first, before this process has rendered
	// anything at all. Defence in depth behind the check above: whatever else
	// this binary does later, `want` is what git has.
	want := readPNG(t, "testdata/glyphs.png")

	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.Box().Background(ui.RGB(20, 24, 34)),
			ui.VStack(
				ui.Text("Glyphs").Key("label").FontSize(24).Foreground(ui.RGB(240, 242, 246)),
			).Padding(12),
		),
		Size: geom.Sz(160, 60),
	})
	// The skip gate for a build without giftgpu, and a real assertion rather
	// than a golden call: [gifttest.Harness.AssertPixel] never writes a file,
	// which is the property this test needs from whatever it uses to decide
	// that pixels are available. The pixel is a corner of the flat background
	// the scene declares.
	h.AssertPixel(geom.Pt(0, 0), ui.RGB(20, 24, 34))

	got := h.Image()
	if got == nil {
		t.Fatal("no framebuffer")
	}
	if want.Bounds() != got.Bounds() {
		t.Fatalf("the frame is %v, the committed golden %v", got.Bounds(), want.Bounds())
	}
	diff := 0
	worst := 0
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, ga := got.At(x, y).RGBA()
			d := max(chanDiff(wr, gr), chanDiff(wg, gg), chanDiff(wb, gb), chanDiff(wa, ga))
			if d != 0 {
				diff++
				worst = max(worst, d)
			}
		}
	}
	if diff != 0 {
		t.Errorf("%d of %d pixels differ from the golden committed before densities existed, "+
			"worst channel difference %d. At density 1 the output must not change at all; "+
			"a golden that has to be regenerated here is a defect, not a new golden.",
			diff, b.Dx()*b.Dy(), worst)
	}
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("reading the committed golden: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return img
}

// chanDiff is the absolute difference of two eight bit channel values taken
// from the sixteen bit values image.Image reports.
func chanDiff(a, b uint32) int {
	d := int(a>>8) - int(b>>8)
	if d < 0 {
		return -d
	}
	return d
}

// TestShapesAreBakedInDevicePixels checks the claim the project plan,
// section 18, makes about the shader side: "Die Shaderseite ist darauf
// vorbereitet: deviceScale backt Radius, Strichbreite, AA-Rand, Schattensigma
// und Refraktion bereits aus dem Transform." That was a reading of the code
// and not a measurement, because until this work unit the root transform was
// the identity and deviceScale returned one for every operation gift produced.
//
// The measurement: draw a rounded, stroked card with a shadow at density 1 and
// at density 2, average each two by two block of the 2x frame down to one
// pixel, and compare the result with the 1x frame. If the radius, the stroke
// width, the antialiasing pad and the shadow sigma are baked out of the
// transform, the two are the same picture and the difference is the resampling
// alone. If any of them were left in logical units, the 2x frame would have a
// corner of half the radius, a stroke of half the width or a shadow of half
// the falloff, and the downsampled image would differ at exactly those places
// by far more than a resampling error.
//
// The tolerance is 40 per channel rather than [gifttest.GoldenTolerance], and
// the number is measured rather than chosen: a box filter is not the
// resampling the rasteriser did. An antialiased edge drawn at 2x and averaged
// down is close to, but not equal to, the same edge drawn at 1x, and a
// shadow's falloff is a curve, so the mean of four samples of it is not its
// value at the centre. The observed worst difference over the whole frame is
// 34, in the shadow gradient along the left edge, and 24 of 9600 pixels exceed
// 24. A radius, a stroke width or a sigma left in logical units would halve a
// feature and show differences in the hundreds along the whole outline of the
// card, so the margin between "resampling" and "not baked" is an order of
// magnitude and this threshold sits inside it.
func TestShapesAreBakedInDevicePixels(t *testing.T) {
	scene := func() gifttest.Options {
		return gifttest.Options{
			View: ui.ZStack(
				ui.Box().Background(ui.RGB(40, 44, 56)),
				ui.Box().Key("card").Frame(72, 40).
					Background(ui.RGB(240, 242, 246)).
					CornerRadius(10).
					Border(ui.Border{Width: 3, Color: ui.RGB(30, 90, 200)}).
					Shadow(ui.Shadow{Blur: 12, OffsetY: 3, Color: ui.RGBA(0, 0, 0, 140)}),
			).Frame(120, 80),
			Size: geom.Sz(120, 80),
		}
	}
	h2 := gifttest.New(t, withDensity(scene(), 2))
	// Skips without giftgpu, with the usual explanation.
	h2.AssertGolden("density-shapes-2x")

	one := gifttest.New(t, scene()).Image()
	two := h2.Image()
	if one == nil || two == nil {
		t.Fatal("no framebuffer")
	}
	b := one.Bounds()
	if two.Bounds().Dx() != 2*b.Dx() || two.Bounds().Dy() != 2*b.Dy() {
		t.Fatalf("the 2x frame is %v, want twice %v", two.Bounds(), b)
	}

	const tolerance = 40
	worst, at, bad := 0, image.Point{}, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wr, wg, wb, _ := one.At(x, y).RGBA()
			gr, gg, gb := boxAverage(two, 2*x, 2*y)
			d := max(chanDiff(wr, gr), chanDiff(wg, gg), chanDiff(wb, gb))
			if d > tolerance {
				bad++
			}
			if d > worst {
				worst, at = d, image.Pt(x, y)
			}
		}
	}
	t.Logf("downsampled 2x versus 1x: worst channel difference %d at %v, %d of %d pixels over %d",
		worst, at, bad, b.Dx()*b.Dy(), tolerance)
	if bad != 0 {
		t.Errorf("%d pixels differ by more than %d after downsampling the 2x frame; "+
			"a geometric quantity is not being baked out of the density transform "+
			"(worst %d at %v)", bad, tolerance, worst, at)
	}
}

// boxAverage is the mean of the two by two block of img whose top left corner
// is (x, y), in sixteen bit channel values.
func boxAverage(img image.Image, x, y int) (r, g, bl uint32) {
	var sr, sg, sb uint32
	for dy := range 2 {
		for dx := range 2 {
			cr, cg, cb, _ := img.At(x+dx, y+dy).RGBA()
			sr, sg, sb = sr+cr, sg+cg, sb+cb
		}
	}
	return sr / 4, sg / 4, sb / 4
}

// TestGlassAtTwoX exercises the material path under a root density transform,
// which is the one part of the project plan, section 18, that could not be
// checked by reading: glass has its own scale handling — the blur radius, the
// corner radius and the refraction are each multiplied by the scale of the
// transform in backend/ebiten/glass.go — and none of it had ever run with a
// scale other than one, because until this work unit gift emitted no scale.
//
// # What is compared, and what deliberately is not
//
// A *blur profile*, not the whole frame. The scene has a bright bar behind the
// panel, so a column through the panel crosses the smeared edge of that bar,
// and the shape of that smear is the blur radius made visible. The column is
// read at 1x and at the corresponding device row of the 2x frame: if the blur
// radius is a logical length scaled into device pixels, the two profiles are
// the same curve, and if it were left in logical units the 2x smear would be
// half as wide and the middle of the profile would be off by tens.
//
// Comparing the two *frames* pixel by pixel was tried and rejected, and the
// reason is a genuine property rather than a tolerance that would not settle.
// Refraction displaces the backdrop, and at 2x it does so on a grid twice as
// fine, so the hard edge of the bar inside the panel lands half a logical
// pixel further down than the 1x rasterisation could express. One row of the
// frame then differs at full contrast. That is the 2x frame being *more*
// correct, which is the whole point of the work unit, and a test that failed
// on it would be pinning the coarser answer.
func TestGlassAtTwoX(t *testing.T) {
	scene := func() gifttest.Options {
		return gifttest.Options{
			View: glassScene(ui.Full), Size: geom.Sz(120, 80),
		}
	}
	// The golden is of the *Reduced* level and the blur profile below is of
	// the Full one. They are separated because they answer different
	// questions — a golden says "this changed", a profile says "the radius
	// scaled" — and not, as this comment claimed until WU-AA, because the
	// Full level was not reproducible. It is; see the note on the threshold
	// below for what the irreproducibility actually was and where it was
	// fixed.
	red := gifttest.New(t, withDensity(gifttest.Options{
		View: glassScene(ui.Reduced), Size: geom.Sz(120, 80),
	}, 2))
	red.Find(gifttest.ByKey("panel")).AssertMaterial(render.MaterialGlass)
	red.AssertGolden("density-glass-2x")

	h2 := gifttest.New(t, withDensity(scene(), 2))
	one := gifttest.New(t, scene()).Image()
	two := h2.Image()
	if one == nil || two == nil {
		t.Fatal("no framebuffer")
	}

	// The column is the middle of the panel and the rows span the blurred
	// edge of the bar, from well inside the smear to just short of the
	// panel's own bottom edge.
	const col = 44
	worst, at := 0, 0
	for y := 20; y < 47; y++ {
		r1, g1, b1, _ := one.At(col, y).RGBA()
		r2, g2, b2, _ := two.At(2*col, 2*y).RGBA()
		d := max(chanDiff(r1, r2), chanDiff(g1, g2), chanDiff(b1, b2))
		if d > worst {
			worst, at = d, y
		}
	}
	t.Logf("blur profile down column %d: worst channel difference %d at y=%d", col, worst, at)
	// Twelve, because the two blurs are computed from backdrops of different
	// resolutions and the finer one is a slightly better blur, not the same
	// one. A blur radius left in logical units halves the width of this
	// gradient and shows differences of forty and more in its middle.
	//
	// The margin used to absorb a defect this test misattributed. The note
	// here said the Full level "depends on what ran before it" and blamed the
	// blur chain sampling outside the pooled backdrop target, with a larger
	// region at 2x reaching further into the stale area "which is why 1x has
	// not shown it". Every part of that was wrong.
	//
	// It was not density dependent — measured at 3590 of 9600 differing
	// pixels between two consecutive *1x* Full frames — and it was not the
	// blur chain, whose taps are clamped to the valid area in both Kawase
	// shaders. It was the grain of the composite, which hashed dstPos, the
	// position in the destination image. That position carries the atlas
	// origin of the pooled scene target, and that origin depends on the
	// allocation history of the process. Two identical display lists
	// therefore got two different noise fields, up to six units per channel
	// apart — above [gifttest.GoldenTolerance], which made the committed
	// glass-full golden pass only by the accident of test ordering.
	//
	// Fixed in glass.kage by anchoring the grain to the position inside the
	// material region; TestGlassFullDoesNotDependOnAllocationHistory in
	// material_test.go is the test that fails if it comes back. So twelve is
	// now the blur margin and nothing else.
	if worst > 12 {
		t.Errorf("the blurred edge differs by %d at y=%d between the densities; "+
			"the blur radius is not being scaled out of the density transform", worst, at)
	}
}
