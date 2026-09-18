package gifttest

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// GoldenTolerance is the per channel difference, in 8 bit units, that a golden
// comparison accepts.
//
// # Why four and not zero
//
// Not zero, because the comparison has to survive a change of GPU. Everything
// in gift's output that could jitter is already pinned: glyphs are rasterised
// on the CPU by gift's own rasteriser, glyph positions are rounded to whole
// pixels (the project plan, section 7, rules out subpixel positioning), and
// layout produces integral coordinates for anything a test would draw. What is
// left is float32 arithmetic in the shape shader's coverage term and in the
// blend, and those differ between drivers in the last unit or two. Four gives
// that a factor of two of headroom.
//
// # Why four and not thirty two
//
// Because the tolerance must not be able to hide a shape in the wrong place. A
// misplaced or mis-sized shape changes whole pixels from background to
// foreground, and the contrast between a background and the thing drawn on it
// is, in any image worth looking at, far more than four units in at least one
// channel. A one pixel shift of a light-on-dark edge produces differences in
// the hundreds. There is deliberately no "allow N% of pixels to differ"
// budget: such a budget is exactly the mechanism by which a shifted edge —
// which touches only the pixels along one line — gets waved through.
//
// TestToleranceCannotMaskAMisplacedShape pins this: a shape moved by a single
// pixel fails the comparison, and the test states the margin by which it does.
const GoldenTolerance = 4

// clearFor converts a [render.Color] into the premultiplied eight bit colour
// an image comparison works in. [render.Color] is already premultiplied and
// already in the zero to one range, so this is a scale and nothing else.
func clearFor(c render.Color) color.RGBA {
	to8 := func(v float32) uint8 {
		switch {
		case v <= 0:
			return 0
		case v >= 1:
			return 255
		default:
			return uint8(v*255 + 0.5)
		}
	}
	return color.RGBA{to8(c.R), to8(c.G), to8(c.B), to8(c.A)}
}

// osExit is os.Exit, named so that Main reads the same in both build modes.
var osExit = os.Exit

// goldenDir is where golden images live, relative to the package under test.
// It is testdata by convention and by the go tool's rule that testdata is not
// a package.
const goldenDir = "testdata"

// updateEnv names the environment variable that rewrites goldens.
//
// An environment variable and not only a flag: a test binary run by `go test
// ./...` shares its flag set with every other package, and a -update flag
// registered by a library collides the moment two libraries have the same
// idea. [Update] reads both, so a project that wants the flag can register one
// and pass it in.
const updateEnv = "GIFT_UPDATE_GOLDEN"

// requireEnv makes a golden assertion fail rather than skip when pixels are
// not available.
//
// Without the giftgpu tag there is no graphics context and the comparison
// cannot run. Skipping is right for a developer running `go test ./...`, and
// wrong for a CI job that believes it is checking rendering. Set
// GIFT_REQUIRE_GOLDEN=1 in that job and a skipped golden becomes a failure.
const requireEnv = "GIFT_REQUIRE_GOLDEN"

// Update reports whether goldens are being rewritten, that is whether
// GIFT_UPDATE_GOLDEN is set to something other than "0" or "".
//
// When it is true, [Harness.AssertGolden] writes the image it produced,
// reports the file it wrote through t.Log *and* marks the test as having
// rewritten a golden, so that a run which updated files is never mistaken for
// a run which verified them.
func Update() bool {
	v := os.Getenv(updateEnv)
	return v != "" && v != "0" && v != "false"
}

func requireGolden() bool {
	v := os.Getenv(requireEnv)
	return v != "" && v != "0" && v != "false"
}

// goldenPaths returns the expected, actual and diff file names for a golden.
func goldenPaths(name string) (want, got, diff string) {
	base := sanitise(name)
	return filepath.Join(goldenDir, base+".png"),
		filepath.Join(goldenDir, base+".actual.png"),
		filepath.Join(goldenDir, base+".diff.png")
}

// sanitise turns a test name into a file name. Subtests contain slashes and
// spaces, and a golden called "Counter/at zero.png" would be a directory.
func sanitise(name string) string {
	r := strings.NewReplacer("/", "_", " ", "_", string(filepath.Separator), "_")
	return r.Replace(name)
}

// compareGolden is the whole comparison, shared by every backend.
//
// It is in the untagged file on purpose: the rule about what counts as equal
// is the part a reviewer argues about, and it must not be invisible in a
// default build. Only the production of the actual image needs a GPU.
func compareGolden(name string, got image.Image) (ok bool, message string, wrote []string) {
	wantPath, gotPath, diffPath := goldenPaths(name)

	if Update() {
		if err := writePNG(wantPath, got); err != nil {
			return false, err.Error(), nil
		}
		_ = os.Remove(gotPath)
		_ = os.Remove(diffPath)
		return true, fmt.Sprintf("GOLDEN REWRITTEN: %s (%s is set; this run verified nothing)",
			wantPath, updateEnv), []string{wantPath}
	}

	want, err := readPNG(wantPath)
	if err != nil {
		_ = writePNG(gotPath, got)
		return false, fmt.Sprintf(
			"no golden image: %v\nthe image this run produced was written to %s\n"+
				"review it and, if it is right, create the golden with %s=1 go test -tags giftgpu ./...",
			err, gotPath, updateEnv), []string{gotPath}
	}

	res := diffImages(want, got)
	if res.ok {
		_ = os.Remove(gotPath)
		_ = os.Remove(diffPath)
		return true, "", nil
	}

	_ = writePNG(gotPath, got)
	_ = writePNG(diffPath, res.diff)
	return false, fmt.Sprintf(
			"%s\n  expected: %s\n  actual:   %s\n  diff:     %s\n"+
				"If the change is intended, rewrite the golden with %s=1 go test -tags giftgpu ./...",
			res.summary, wantPath, gotPath, diffPath, updateEnv),
		[]string{gotPath, diffPath}
}

// diffResult is the outcome of a pixel comparison.
type diffResult struct {
	ok      bool
	summary string
	diff    image.Image
}

// diffImages compares two images channel by channel.
//
// The diff image it produces is not a subtraction: a subtraction of two nearly
// identical images is nearly black and shows nothing on a screen. It is the
// expected image desaturated and darkened, with every offending pixel painted
// solid magenta, so that the *shape* of the difference is visible — which is
// the thing that tells "the text moved" from "the colour changed" apart at a
// glance.
func diffImages(want, got image.Image) diffResult {
	wb, gb := want.Bounds(), got.Bounds()
	if wb.Dx() != gb.Dx() || wb.Dy() != gb.Dy() {
		return diffResult{
			summary: fmt.Sprintf("size mismatch: golden is %dx%d, this run produced %dx%d",
				wb.Dx(), wb.Dy(), gb.Dx(), gb.Dy()),
			diff: got,
		}
	}

	diff := image.NewRGBA(image.Rect(0, 0, wb.Dx(), wb.Dy()))
	var bad int
	var maxDelta int
	var firstX, firstY int = -1, -1
	for y := 0; y < wb.Dy(); y++ {
		for x := 0; x < wb.Dx(); x++ {
			w := rgba8(want.At(wb.Min.X+x, wb.Min.Y+y))
			g := rgba8(got.At(gb.Min.X+x, gb.Min.Y+y))
			d := maxChannelDelta(w, g)
			if d > maxDelta {
				maxDelta = d
			}
			switch {
			case d > GoldenTolerance:
				bad++
				if firstX < 0 {
					firstX, firstY = x, y
				}
				diff.SetRGBA(x, y, color.RGBA{255, 0, 255, 255})
			default:
				// The expected image, dimmed, as context for the magenta.
				diff.SetRGBA(x, y, color.RGBA{
					R: uint8(int(w.R) / 3),
					G: uint8(int(w.G) / 3),
					B: uint8(int(w.B) / 3),
					A: 255,
				})
			}
		}
	}
	if bad == 0 {
		return diffResult{ok: true, diff: diff}
	}
	total := wb.Dx() * wb.Dy()
	return diffResult{
		summary: fmt.Sprintf(
			"%d of %d pixels (%.3f%%) differ by more than the tolerance of %d per channel; "+
				"the largest difference anywhere is %d, first at (%d,%d)",
			bad, total, 100*float64(bad)/float64(total), GoldenTolerance, maxDelta, firstX, firstY),
		diff: diff,
	}
}

func maxChannelDelta(a, b color.RGBA) int {
	d := absInt(int(a.R) - int(b.R))
	if v := absInt(int(a.G) - int(b.G)); v > d {
		d = v
	}
	if v := absInt(int(a.B) - int(b.B)); v > d {
		d = v
	}
	if v := absInt(int(a.A) - int(b.A)); v > d {
		d = v
	}
	return d
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func rgba8(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func readPNG(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(data))
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// AssertPixel fails unless the pixel at p is want, within [GoldenTolerance].
//
// # Why there is an image assertion next to the goldens
//
// A golden answers "did this change" and needs a file to compare against; it
// cannot answer "is the background still behind the panel", because a golden
// recorded while it was not would happily pin the defect. This is the
// assertion for a single stated fact about the framebuffer, and it is the one
// that covers the scene blit: a frame containing a material must not alter
// pixels the frame did not touch.
//
// The colour is a [render.Color], premultiplied like everything else in gift,
// so a test names the colour its view painted rather than converting one.
//
// Without the giftgpu tag there are no pixels. It then skips, or fails when
// GIFT_REQUIRE_GOLDEN is set, exactly like [Harness.AssertGolden].
func (h *Harness) AssertPixel(p geom.Point, want render.Color) {
	h.t.Helper()
	img, ok := h.framebuffer("AssertPixel")
	if !ok {
		return
	}
	x, y := int(p.X), int(p.Y)
	b := img.Bounds()
	if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
		h.t.Errorf("gifttest: AssertPixel at (%g, %g) is outside the %dx%d viewport",
			p.X, p.Y, b.Dx(), b.Dy())
		return
	}
	got := rgba8(img.At(x, y))
	exp := clearFor(want)
	if maxChannelDelta(got, exp) <= GoldenTolerance {
		return
	}
	h.t.Errorf("gifttest: the pixel at (%g, %g) is {%d %d %d %d}, want {%d %d %d %d} "+
		"within %d per channel",
		p.X, p.Y, got.R, got.G, got.B, got.A, exp.R, exp.G, exp.B, exp.A, GoldenTolerance)
}

// AssertOpaque fails unless every pixel of the framebuffer is fully opaque.
//
// # What it is for
//
// It is the gate on the one obligation gift puts on an application and
// nothing in gift can meet for it: the window background. gift paints exactly
// what the view tree says, Ebitengine hands a real window a transparent black
// screen at the top of every Draw — the project plan, section 6 — and an
// application that paints no background therefore ships a window with holes in
// it. On a desktop that is a composited hole; on a kiosk it is whatever was in
// the framebuffer before. [ui.Window] is the one-line answer, and this is the
// assertion that it was written.
//
// It is deliberately an alpha test and not a comparison against a colour. The
// defect is an *absent* pixel, not a wrong one, and no golden image can report
// it: a golden of a scene with holes in it is a perfectly stable golden. This
// module shipped twelve of those.
//
// The failure names the first offending pixel in scan order and how many there
// are in total, because one is a rounding artefact at an edge and half a
// million is a missing background.
//
// Without the giftgpu tag there are no pixels. It then skips, or fails when
// GIFT_REQUIRE_GOLDEN is set, exactly like [Harness.AssertGolden].
func (h *Harness) AssertOpaque() {
	h.t.Helper()
	img, ok := h.framebuffer("AssertOpaque")
	if !ok {
		return
	}
	b := img.Bounds()
	holes := 0
	firstX, firstY := 0, 0
	var firstPx color.RGBA
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a >= 0xffff {
				continue
			}
			if holes == 0 {
				firstX, firstY, firstPx = x-b.Min.X, y-b.Min.Y, rgba8(img.At(x, y))
			}
			holes++
		}
	}
	if holes == 0 {
		return
	}
	h.t.Errorf("gifttest: %d of %d pixels are not fully opaque; the first is (%d, %d) "+
		"at {%d %d %d %d}.\nA gift application paints its own window background: "+
		"wrap the root view in ui.Window.",
		holes, b.Dx()*b.Dy(), firstX, firstY,
		firstPx.R, firstPx.G, firstPx.B, firstPx.A)
}
