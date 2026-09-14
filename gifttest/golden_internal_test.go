package gifttest

import (
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
)

// These tests are about the comparison rule, not about rendering, so they run
// in every build: the rule is the part of a golden test that decides whether a
// regression is caught, and it must not be invisible without a GPU.

// fillRect paints r into a white image of the given size, in the given colour.
func synthetic(w, h int, rect image.Rectangle, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func shift(img *image.RGBA, by int) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			src := x - by
			if src < 0 || src >= img.Bounds().Dx() {
				out.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
				continue
			}
			out.SetRGBA(x, y, img.RGBAAt(src, y))
		}
	}
	return out
}

// nudge adds d to every channel of every pixel, which is what a driver with
// slightly different rounding does.
func nudge(img *image.RGBA, d int) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	clamp := func(v int) uint8 {
		switch {
		case v < 0:
			return 0
		case v > 255:
			return 255
		default:
			return uint8(v)
		}
	}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := img.RGBAAt(x, y)
			out.SetRGBA(x, y, color.RGBA{
				clamp(int(c.R) - d), clamp(int(c.G) - d), clamp(int(c.B) - d), c.A,
			})
		}
	}
	return out
}

var goldenShape = image.Rect(20, 20, 80, 60)

// TestIdenticalImagesMatch is the floor.
func TestIdenticalImagesMatch(t *testing.T) {
	img := synthetic(100, 80, goldenShape, color.RGBA{0, 0, 0, 255})
	if res := diffImages(img, img); !res.ok {
		t.Fatalf("an image does not equal itself: %s", res.summary)
	}
}

// TestToleranceAbsorbsDriverRounding is what the tolerance is for: the same
// scene rendered by a driver that rounds differently in the last unit or two.
func TestToleranceAbsorbsDriverRounding(t *testing.T) {
	want := synthetic(100, 80, goldenShape, color.RGBA{40, 90, 200, 255})
	for d := 1; d <= GoldenTolerance; d++ {
		if res := diffImages(want, nudge(want, d)); !res.ok {
			t.Errorf("a uniform difference of %d, within the tolerance of %d, was rejected: %s",
				d, GoldenTolerance, res.summary)
		}
	}
	if res := diffImages(want, nudge(want, GoldenTolerance+1)); res.ok {
		t.Errorf("a uniform difference of %d, one past the tolerance, was accepted",
			GoldenTolerance+1)
	}
}

// TestToleranceCannotMaskAMisplacedShape is the assertion that matters.
//
// The whole risk of a tolerance is that it turns into a licence: pick a
// generous number, add a "and up to 1% of pixels may differ" budget, and a
// rectangle drawn in the wrong place sails through — because a shifted edge
// touches only the pixels along one line and is precisely the sort of thing a
// percentage budget forgives.
//
// This package therefore has no pixel budget at all, and the test states the
// margin: a shape displaced by a single pixel differs by the full contrast
// between the shape and its background, which is two orders of magnitude past
// the tolerance.
func TestToleranceCannotMaskAMisplacedShape(t *testing.T) {
	want := synthetic(100, 80, goldenShape, color.RGBA{0, 0, 0, 255})
	got := shift(want, 1)

	res := diffImages(want, got)
	if res.ok {
		t.Fatalf("a shape displaced by one pixel was accepted; the tolerance is masking a layout error")
	}
	if !strings.Contains(res.summary, "largest difference anywhere is 255") {
		t.Errorf("the failure should state the size of the difference, it said: %s", res.summary)
	}
	// Two columns of the shape's 40 rows change, so eighty pixels differ by
	// the full range. The point of naming the number is that it is nowhere
	// near a rounding difference.
	if !strings.Contains(res.summary, "80 of 8000 pixels") {
		t.Errorf("unexpected extent of the difference: %s", res.summary)
	}
	t.Logf("one pixel of displacement reads as: %s", res.summary)
}

// TestSizeMismatchIsAFailureNotAPanic covers the case where the viewport of
// the test changed since the golden was made.
func TestSizeMismatchIsAFailureNotAPanic(t *testing.T) {
	a := synthetic(100, 80, goldenShape, color.RGBA{0, 0, 0, 255})
	b := synthetic(120, 80, goldenShape, color.RGBA{0, 0, 0, 255})
	res := diffImages(a, b)
	if res.ok || !strings.Contains(res.summary, "size mismatch") {
		t.Fatalf("want a size mismatch failure, got ok=%v %q", res.ok, res.summary)
	}
}

// TestUpdateIsOffByDefault guards the one thing that must never be accidental:
// a CI job that rewrites its own expectations.
func TestUpdateIsOffByDefault(t *testing.T) {
	t.Setenv(updateEnv, "")
	if Update() {
		t.Fatal("goldens are being rewritten with no environment variable set")
	}
	t.Setenv(updateEnv, "0")
	if Update() {
		t.Fatalf("%s=0 must not rewrite goldens", updateEnv)
	}
	t.Setenv(updateEnv, "1")
	if !Update() {
		t.Fatalf("%s=1 must rewrite goldens", updateEnv)
	}
}

// TestRewritingSaysSo pins the other half: a run that rewrote a golden
// announces it, so that a green CI log which verified nothing is visibly
// different from one that did.
func TestRewritingSaysSo(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	t.Setenv(updateEnv, "1")
	img := synthetic(8, 8, image.Rect(2, 2, 6, 6), color.RGBA{1, 2, 3, 255})
	ok, msg, wrote := compareGolden("sample", img)
	if !ok {
		t.Fatalf("rewriting should succeed: %s", msg)
	}
	if !strings.Contains(msg, "GOLDEN REWRITTEN") || !strings.Contains(msg, "verified nothing") {
		t.Errorf("a rewrite must announce itself, it said: %q", msg)
	}
	if len(wrote) != 1 {
		t.Fatalf("wrote %v, want exactly the golden", wrote)
	}
	if _, err := os.Stat(wrote[0]); err != nil {
		t.Errorf("the golden was not written: %v", err)
	}
}

// TestMismatchWritesTheEvidence covers what a failing golden leaves behind.
// A message that says "the pixels differ" and nothing else is unusable; the
// actual image and a visual diff next to the expected one is what makes the
// failure reviewable.
func TestMismatchWritesTheEvidence(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	t.Setenv(updateEnv, "")

	want := synthetic(100, 80, goldenShape, color.RGBA{0, 0, 0, 255})
	wantPath, gotPath, diffPath := goldenPaths("moved")
	if err := writePNG(wantPath, want); err != nil {
		t.Fatal(err)
	}

	ok, msg, wrote := compareGolden("moved", shift(want, 1))
	if ok {
		t.Fatal("a one pixel shift compared equal")
	}
	for _, p := range []string{wantPath, gotPath, diffPath} {
		if !strings.Contains(msg, p) {
			t.Errorf("the failure message does not name %s:\n%s", p, msg)
		}
	}
	if len(wrote) != 2 {
		t.Errorf("wrote %v, want the actual image and the diff", wrote)
	}
	for _, p := range wrote {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was not written: %v", p, err)
		}
	}
	if !strings.Contains(msg, updateEnv) {
		t.Errorf("the failure should say how to rewrite the golden deliberately:\n%s", msg)
	}

	// And a run that then matches cleans the evidence up again, so a stale
	// .actual.png cannot outlive the failure that produced it.
	if ok, _, _ := compareGolden("moved", want); !ok {
		t.Fatal("the golden does not match itself")
	}
	if _, err := os.Stat(gotPath); err == nil {
		t.Errorf("%s survived a passing run", gotPath)
	}
}
