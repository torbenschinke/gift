//go:build giftgpu

package gifttest_test

import (
	"image"
	"strconv"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// A scrolled scene, checked in pixels.
//
// The headless assertions in scroll_test.go say the transform is right and
// that the clipped rows carry a clip that excludes them. That is a statement
// about the display list, and the display list is gift's side of the contract.
// This is the other side: the backend really does apply the transform and
// really does drop the geometry outside the clip, so a row that has been
// scrolled away is genuinely absent from the framebuffer rather than merely
// marked as such.
//
// Distinct, saturated, widely separated colours are used on purpose. The
// question is "is this row's colour on the screen at all", and a search for an
// exact colour is only meaningful if no two rows and no blend of two rows can
// produce it.

const (
	gpuRows     = 8
	gpuRowH     = 40
	gpuViewW    = 120
	gpuViewH    = 120
	gpuWindowH  = 160
	gpuRowsSeen = gpuViewH / gpuRowH
)

// gpuRowColor is the colour of row i: a pure red ramp in steps of 30, so
// neighbouring rows differ by more than any blend could bridge and none of
// them is the white background.
func gpuRowColor(i int) ui.Color { return ui.RGB(uint8(30+i*30), 0, 0) }

func gpuScrollScene() gift.View {
	rows := make([]gift.View, 0, gpuRows)
	for i := range gpuRows {
		rows = append(rows, ui.Box().
			Frame(gpuViewW, gpuRowH).
			Background(gpuRowColor(i)).
			Key("row"+strconv.Itoa(i)))
	}
	return ui.VStack(
		ui.VScroll(rows...).Frame(gpuViewW, gpuViewH).Key("scroller"),
	)
}

// countRed returns how many pixels of img have the exact red value r with no
// green and no blue, within the golden tolerance.
func countRed(img image.Image, want ui.Color) int {
	to8 := func(v float32) int {
		switch {
		case v <= 0:
			return 0
		case v >= 1:
			return 255
		default:
			return int(v*255 + 0.5)
		}
	}
	wr, wg, wb := to8(want.R), to8(want.G), to8(want.B)
	near := func(a, b int) bool {
		d := a - b
		return d <= gifttest.GoldenTolerance && d >= -gifttest.GoldenTolerance
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if near(int(r>>8), wr) && near(int(g>>8), wg) && near(int(bl>>8), wb) {
				n++
			}
		}
	}
	return n
}

func TestScrolledSceneRendersCorrectPixels(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: gpuScrollScene(),
		Size: geom.Sz(gpuViewW, gpuWindowH),
	})

	// At rest the first three rows are on screen — three because the viewport
	// is 120 and a row is 40 — and everything after them is not.
	img := h.Image()
	for i := range gpuRows {
		got := countRed(img, gpuRowColor(i))
		want := gpuViewW * gpuRowH
		if i >= gpuRowsSeen {
			want = 0
		}
		if got != want {
			t.Errorf("at rest, row %d covers %d pixels, want %d", i, got, want)
		}
	}

	// Scroll by exactly two rows. Rows 2, 3 and 4 are now on screen and rows
	// 0 and 1 must be gone from the framebuffer entirely — not merely
	// overdrawn, since nothing is drawn on top of them.
	h.Find(gifttest.ByKey("scroller")).ScrollTo(2 * gpuRowH)

	img = h.Image()
	for i := range gpuRows {
		got := countRed(img, gpuRowColor(i))
		want := 0
		if i >= 2 && i < 2+gpuRowsSeen {
			want = gpuViewW * gpuRowH
		}
		if got != want {
			t.Errorf("after scrolling two rows, row %d covers %d pixels, want %d", i, got, want)
		}
	}

	// And the top left pixel of the viewport is row 2, which is the whole
	// claim of the transform stated as one sample.
	r, _, _, _ := img.At(2, 2).RGBA()
	if want := uint32(30 + 2*30); absInt(int(r>>8)-int(want)) > gifttest.GoldenTolerance {
		t.Errorf("the pixel at the top of the viewport has red %d, want %d: the content was not "+
			"translated by the scroll offset", r>>8, want)
	}

	// A partial scroll: half a row, so the boundary really moves and the clip
	// really cuts a row in half.
	h.Find(gifttest.ByKey("scroller")).ScrollTo(2*gpuRowH + gpuRowH/2)
	img = h.Image()
	if got, want := countRed(img, gpuRowColor(2)), gpuViewW*gpuRowH/2; got != want {
		t.Errorf("after scrolling two and a half rows, row 2 covers %d pixels, want %d", got, want)
	}
	if got := countRed(img, gpuRowColor(1)); got != 0 {
		t.Errorf("row 1 is two and a half rows above the viewport and still covers %d pixels", got)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
