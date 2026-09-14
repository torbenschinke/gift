//go:build giftgpu

package ui_test

import (
	"strconv"
	"testing"

	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// TestGalleryRendersOnGPU is the pixel half of the gallery.
//
// The headless tests say which tile stands for which entry and where the
// layout put it. That is gift's side of the contract. This is the other side:
// the tiles really reach the framebuffer, the scroll transform really moves
// them, and a deterministic scroll position really produces a deterministic
// picture.
//
// The scene is deliberately small and deliberately synthetic. A golden image
// of a 100 000 entry gallery would be a golden image of nine rectangles
// whichever catalogue it came from, and the nine rectangles are the thing that
// can regress.

// goldenGallery is a 60 entry catalogue with fixed aspect ratios, so the
// layout is the same number on every machine.
func goldenGallery() *ui.Gallery {
	items := make([]asset.Metadata, 60)
	for i := range items {
		// Four repeating shapes, so the masonry columns end at different
		// heights and the picture is not a grid.
		w, h := [4][2]uint32{{4, 3}, {3, 4}, {1, 1}, {16, 9}}[i%4][0], [4][2]uint32{{4, 3}, {3, 4}, {1, 1}, {16, 9}}[i%4][1]
		items[i] = asset.Metadata{
			ID:     asset.ID("g" + strconv.Itoa(i)),
			Width:  w * 100,
			Height: h * 100,
		}
	}
	g := ui.NewGallery(asset.NewCollection(items))
	g.Selection().Only("g7")
	return g
}

func TestGalleryRendersOnGPU(t *testing.T) {
	g := goldenGallery()
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.ImageGallery(g).
				Layout(ui.Masonry().MinColumnWidth(90).Gap(6)).
				Tile(ui.TileStyle{
					CornerRadius: 6,
					Palette: []ui.Color{
						ui.RGB(210, 70, 60), ui.RGB(60, 150, 210),
						ui.RGB(90, 190, 110), ui.RGB(220, 180, 70),
					},
					Selected: ui.Border{Width: 3, Color: ui.RGB(20, 20, 20)},
				}).
				Padding(8).
				Background(ui.RGB(245, 246, 248)).
				Flex(1).Key("gallery"),
		).Frame(320, 240),
		Size:       geom.Sz(320, 240),
		Background: ui.RGB(255, 255, 255),
	})

	// Something was drawn at all, before any pixel comparison: a golden that
	// passes because both sides are blank is the classic failure.
	if g.VisibleCount() < 6 {
		t.Fatalf("only %d tiles visible, this would be an empty golden: %v", g.VisibleCount(), g)
	}
	h.AssertGolden("gallery-top")

	// A deterministic scroll position. 260 is a number with no round meaning
	// in the layout, which is the point: it lands in the middle of tiles in
	// all three columns.
	h.Find(gifttest.ByKey("gallery")).ScrollTo(260)
	h.AssertGolden("gallery-scrolled-260")
}
