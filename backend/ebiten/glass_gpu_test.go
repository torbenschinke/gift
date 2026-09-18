//go:build giftgpu

// Pixel level evidence for the glass material of the project plan, section 8.
// These need a real graphics context, so they are behind giftgpu like every
// other test in this package that reads a pixel back.
//
//	go test -tags giftgpu ./backend/ebiten/
package ebiten

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// drawGlassScene renders a deterministic backdrop and one glass panel over it,
// through the whole real path: a scene target, a region copy, the pass chain
// and the composite, then the scene to screen blit.
//
// The destination is an ordinary offscreen and not the screen, which is the
// only way this can be done at all: Ebitengine's internal/atlas panics with "a
// screen image cannot be created as a source" if the screen is used as a draw
// source, which is the whole reason the scene target exists. An offscreen
// destination exercises the same code, because the renderer never looks at
// what kind of image it was handed.
func drawGlassScene(t *testing.T, w, h int, q render.GlassQuality, g render.Glass, region geom.Rect) *eb.Image {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	r.PinGlassQuality(q)
	dst := eb.NewImage(w, h)

	var l render.List
	l.Reset()
	// A deterministic backdrop: a dark plate and a bright bar across the
	// middle, so that a blur has something to smear and an unblurred sample
	// has a hard edge to keep.
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, float32(w), float32(h)),
		Color: render.RGB(20, 24, 34)})
	l.Add(render.Op{Kind: render.OpFillRect,
		Bounds: geom.Rc(0, float32(h)/2-8, float32(w), float32(h)/2+8),
		Color:  render.RGB(240, 90, 40)})
	addGlass(&l, region, 16, g)

	r.SetTarget(dst)
	r.BeginFrame(geom.Sz(float32(w), float32(h)))
	r.Submit(&l)
	r.EndFrame()

	if s := r.Stats(); s.GlassFallbacks != 0 {
		t.Fatalf("the material degraded to the fallback: %d fallbacks, targets %+v",
			s.GlassFallbacks, r.Targets().Stats())
	}
	return dst
}

func rgbaAt(img *eb.Image, x, y int) color.RGBA { return img.At(x, y).(color.RGBA) }

// TestGlassReducedRenders is the "does it actually draw" half.
//
// It checks three separate claims rather than one image: the panel covers its
// region, the backdrop is visible through it, and the rim is brighter than the
// middle. The third is what distinguishes a glass material from a translucent
// rectangle, and it is the one the project plan, section 8, calls
// "Fresnel-artige Kantenaufhellung".
func TestGlassReducedRenders(t *testing.T) {
	const w, h = 128, 128
	region := geom.Rc(24, 24, 104, 104)
	g := render.NewGlass().Quality(render.Reduced).
		Tint(render.RGBA(255, 255, 255, 30)).Highlight(1).Refraction(6)
	dst := drawGlassScene(t, w, h, render.Reduced, g, region)

	// Outside the panel the backdrop is untouched, which also proves the
	// scene to screen blit happened at all.
	if got := rgbaAt(dst, 4, 4); got.R != 20 || got.G != 24 || got.B != 34 {
		t.Errorf("outside the panel the pixel is %v, want the backdrop (20,24,34)", got)
	}
	if got := rgbaAt(dst, 4, h/2); got.R < 200 {
		t.Errorf("the backdrop bar outside the panel is %v, want the bright bar", got)
	}

	// Inside, the bar shows through: the panel is not opaque.
	inBar := rgbaAt(dst, 64, h/2)
	inPlate := rgbaAt(dst, 64, 40)
	if int(inBar.R)-int(inPlate.R) < 80 {
		t.Errorf("inside the panel the bar is %v and the plate %v; the backdrop is not showing through",
			inBar, inPlate)
	}
	// Reduced does not blur, so the bar keeps a hard edge under the panel.
	// Two pixels above its top edge must still be plate-like.
	if got := rgbaAt(dst, 64, h/2-12); int(got.R) > int(inPlate.R)+30 {
		t.Errorf("the bar has bled %v pixels above its edge inside a Reduced panel; Reduced has no blur", got)
	}

	// The rim is brighter than the middle. Sampled on the plate, away from
	// the bar, so the comparison is between two points of the same backdrop.
	rim := rgbaAt(dst, 64, 27)
	mid := rgbaAt(dst, 64, 45)
	if int(rim.R) <= int(mid.R) {
		t.Errorf("the rim is %v and the middle %v; the edge is not brighter", rim, mid)
	}
}

// TestGlassFullBlursTheBackdrop is the one property that distinguishes Full
// from Reduced, and it is checked as a *gradient* rather than as a picture:
// under a blurred panel the hard edge of the bar has become a ramp.
func TestGlassFullBlursTheBackdrop(t *testing.T) {
	const w, h = 128, 128
	region := geom.Rc(16, 16, 112, 112)
	g := render.NewGlass().Quality(render.Full).Blur(16).
		Tint(render.RGBA(255, 255, 255, 20)).Highlight(0.4).Refraction(0).Grain(0)
	full := drawGlassScene(t, w, h, render.Full, g, region)
	reduced := drawGlassScene(t, w, h, render.Reduced,
		g.Quality(render.Reduced), region)

	// Twelve pixels above the bar's edge: still plate under Reduced, already
	// tinted by the bar under Full.
	y := h/2 - 12
	rf := int(rgbaAt(full, 64, y).R)
	rr := int(rgbaAt(reduced, 64, y).R)
	if rf <= rr+10 {
		t.Errorf("at y=%d Full is %d and Reduced %d; the blur did not spread the bar", y, rf, rr)
	}
	// And in the middle of the bar the blur has *darkened* it, because it is
	// averaging in the plate on both sides.
	bf := int(rgbaAt(full, 64, h/2).R)
	br := int(rgbaAt(reduced, 64, h/2).R)
	if bf >= br {
		t.Errorf("in the middle of the bar Full is %d and Reduced %d; a blur must average it down", bf, br)
	}
}

// TestGlassIsPremultiplied is the colour convention check of the project plan,
// section 8, applied to the one new shader that composes rather than fills.
//
// A premultiplied result over an opaque backdrop is opaque and has no channel
// above its own alpha. The failure a straight-alpha mistake produces is a
// panel that looks right over black and washed out over anything else, which
// is exactly the kind of bug a golden image taken over one background never
// catches.
func TestGlassIsPremultiplied(t *testing.T) {
	const w, h = 64, 64
	region := geom.Rc(8, 8, 56, 56)
	for _, q := range []render.GlassQuality{render.Reduced, render.Full} {
		g := render.NewGlass().Quality(q).Tint(render.RGBA(60, 120, 255, 120))
		dst := drawGlassScene(t, w, h, q, g, region)
		for _, p := range [][2]int{{32, 32}, {20, 20}, {44, 44}, {32, 12}} {
			c := rgbaAt(dst, p[0], p[1])
			if c.A != 255 {
				t.Errorf("%v at %v has alpha %d; over an opaque backdrop the result must be opaque",
					q, p, c.A)
			}
			if c.R > c.A || c.G > c.A || c.B > c.A {
				t.Errorf("%v at %v is %v, a channel above its own alpha: not premultiplied", q, p, c)
			}
		}
	}
}

// TestGlassTintDirection. A blue tint must move the result towards blue, and a
// higher tint alpha must move it further. Two assertions, because the first
// alone passes for a shader that ignores the alpha.
func TestGlassTintDirection(t *testing.T) {
	const w, h = 64, 64
	region := geom.Rc(8, 8, 56, 56)
	light := drawGlassScene(t, w, h, render.Reduced,
		render.NewGlass().Quality(render.Reduced).Tint(render.RGBA(40, 90, 255, 40)).Highlight(0), region)
	heavy := drawGlassScene(t, w, h, render.Reduced,
		render.NewGlass().Quality(render.Reduced).Tint(render.RGBA(40, 90, 255, 200)).Highlight(0), region)

	l, hv := rgbaAt(light, 32, 24), rgbaAt(heavy, 32, 24)
	if int(hv.B) <= int(l.B) {
		t.Errorf("a heavier blue tint gave %v against %v; the alpha did not reach the composite", hv, l)
	}
	if int(hv.B)-int(hv.R) <= int(l.B)-int(l.R) {
		t.Errorf("the heavier tint is not bluer: %v against %v", hv, l)
	}
}

// TestGlassGoldenImages are the "that has changed" images of the project plan,
// section 13. They say nothing about whether the material is right — the
// assertions above do that — and they run only under giftgpu.
func TestGlassGoldenImages(t *testing.T) {
	const w, h = 128, 128
	region := geom.Rc(20, 20, 108, 84)
	for _, tc := range []struct {
		name string
		q    render.GlassQuality
	}{
		{"glass-reduced", render.Reduced},
		{"glass-full", render.Full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := render.NewGlass().Quality(tc.q).Blur(16).
				Tint(render.RGBA(255, 255, 255, 34)).
				Refraction(6).Highlight(0.6).Grain(0.08)
			dst := drawGlassScene(t, w, h, tc.q, g, region)
			compareGolden(t, tc.name, dst)
		})
	}
}

// --- golden images ----------------------------------------------------------
//
// A local comparison rather than gifttest's, and not out of duplication for
// its own sake: gifttest's tagged half imports this package, so this package
// cannot import gifttest without a cycle. The rules are the same ones the
// project plan, section 13, fixes — per channel tolerance of four, no budget
// for outlying pixels, actual and diff written on failure and removed on
// success.

// glassGoldenTolerance is four steps per channel, the number section 13 binds.
// Ebitengine's own ReadPixels documentation warns of "very slight differences
// between some machines", and four is sized for that and for nothing else.
const glassGoldenTolerance = 4

func compareGolden(t *testing.T, name string, img *eb.Image) {
	t.Helper()
	b := img.Bounds()
	got := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			got.Set(x, y, img.At(x, y))
		}
	}

	dir := filepath.Join("testdata")
	want := filepath.Join(dir, name+".png")
	gotPath := filepath.Join(dir, name+".got.png")
	diffPath := filepath.Join(dir, name+".diff.png")

	if os.Getenv("GIFT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, want, got)
		_ = os.Remove(gotPath)
		_ = os.Remove(diffPath)
		t.Logf("GOLDEN REWRITTEN: %s (GIFT_UPDATE_GOLDEN is set; this run verified nothing)", want)
		return
	}

	f, err := os.Open(want)
	if err != nil {
		writePNG(t, gotPath, got)
		t.Fatalf("golden %s is missing (%v); this run's image is at %s. "+
			"Set GIFT_UPDATE_GOLDEN=1 to create it, after looking at it.", want, err, gotPath)
	}
	ref, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("decoding %s: %v", want, err)
	}
	if ref.Bounds() != b {
		t.Fatalf("golden %s is %v, this run is %v", want, ref.Bounds(), b)
	}

	diff := image.NewRGBA(b)
	bad := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			gr, gg, gb, ga := got.At(x, y).RGBA()
			wr, wg, wb, wa := ref.At(x, y).RGBA()
			off := chanOff(gr, wr) || chanOff(gg, wg) || chanOff(gb, wb) || chanOff(ga, wa)
			if off {
				bad++
				diff.Set(x, y, color.RGBA{255, 0, 255, 255})
				continue
			}
			c := color.RGBA{uint8(wr >> 8 / 3), uint8(wg >> 8 / 3), uint8(wb >> 8 / 3), 255}
			diff.Set(x, y, c)
		}
	}
	if bad > 0 {
		writePNG(t, gotPath, got)
		writePNG(t, diffPath, diff)
		t.Errorf("golden %s differs in %d pixels (tolerance %d per channel, no outlier budget).\n"+
			"actual: %s\ndiff:   %s", want, bad, glassGoldenTolerance, gotPath, diffPath)
		return
	}
	_ = os.Remove(gotPath)
	_ = os.Remove(diffPath)
}

func chanOff(a, b uint32) bool {
	d := int(a>>8) - int(b>>8)
	if d < 0 {
		d = -d
	}
	return d > glassGoldenTolerance
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// BenchmarkGlassOverGallery is the measurement of the project plan,
// section 13: "Glass Reduced ueber Galerie: Zusatzkosten < 1,0 ms je Frame"
// and "Glass Full ueber Galerie: Zusatzkosten < 4,0 ms je Frame;
// Materialflaeche <= 25 % des Screens".
//
// # What is measured
//
// A 1920x1080 frame of a few hundred rectangles, with and without a 1920x200
// glass panel over it — 18.5 % of the screen, inside section 13's envelope.
// The reported cost is the difference, which is what "Zusatzkosten" means.
//
// # Why a pixel is read back
//
// Because CPU time at the draw call is not GPU time; the project plan,
// section 11, says so about uploads and it is just as true here. Ebitengine
// queues commands and returns, so timing the submit path measures the queueing
// and nothing else. Reading one pixel at the end of each iteration forces the
// queue to drain, which turns the wall clock into something that includes the
// GPU. It is a measurement harness and runs in no frame path; section 11's
// "kein GPU-Readback im Framepfad" is about the renderer, and the renderer
// still contains no ReadPixels at all.
//
// # What it is not
//
// It is not a Raspberry Pi 4. Section 13's reference is a Pi 4 at 1920x1080,
// and this benchmark runs wherever it is run. A pass here says the
// implementation is not accidentally quadratic; it does not discharge the
// acceptance threshold, and the number on the reference machine has to be
// taken there.
func BenchmarkGlassOverGallery(b *testing.B) {
	const w, h = 1920, 1080
	for _, tc := range []struct {
		name  string
		glass bool
		level render.GlassQuality
	}{
		{name: "baseline"},
		{name: "reduced", glass: true, level: render.Reduced},
		{name: "full", glass: true, level: render.Full},
	} {
		b.Run(tc.name, func(b *testing.B) {
			r, err := NewRenderer()
			if err != nil {
				b.Fatal(err)
			}
			r.PinGlassQuality(tc.level)
			dst := eb.NewImage(w, h)

			var l render.List
			l.Reset()
			l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, w, h),
				Color: render.RGB(18, 20, 26)})
			// A gallery's worth of tiles: sixty rounded rectangles.
			for i := range 60 {
				x := float32(i%10)*192 + 6
				y := float32(i/10)*180 + 6
				l.Add(render.Op{Kind: render.OpFillRoundRect,
					Bounds:       geom.Rc(x, y, x+180, y+168),
					CornerRadius: 8,
					Color:        render.RGB(uint8(40+i*3), uint8(60+i*2), 120)})
			}
			if tc.glass {
				addGlass(&l, geom.Rc(0, 0, w, 200), 18,
					render.NewGlass().Quality(tc.level).Blur(16).
						Tint(render.RGBA(255, 255, 255, 30)).Refraction(6).Highlight(0.5).Grain(0.06))
			}

			// Warm up: shader upload, target allocation, the first blit.
			for range 20 {
				r.SetTarget(dst)
				r.BeginFrame(geom.Sz(w, h))
				r.Submit(&l)
				r.EndFrame()
			}
			_ = dst.At(0, 0)

			b.ResetTimer()
			for b.Loop() {
				r.SetTarget(dst)
				r.BeginFrame(geom.Sz(w, h))
				r.Submit(&l)
				r.EndFrame()
				// Drains the command queue; see the comment above.
				_ = dst.At(w-1, h-1)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/1e6, "ms/frame")
		})
	}
}
