//go:build giftdebug

package ui_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// TestASemanticPaletteEntryIsReported is the end to end half of
// TestEveryVisibilityGateIsGuarded, and it is the case the documentation of
// [ui.TileStyle.Palette] has promised all along: "one written here is a defect
// that the giftdebug build reports from render".
//
// It did not. render's check sits in [render.List.Add], the tile painter drops
// a transparent fill before it gets there, and a semantic palette entry is
// transparent — so this exact tree laid out nine tiles, emitted zero
// operations and said nothing, under the tag. The sentence in the
// documentation was false and this test is what keeps it true.
//
// A palette entry is the one colour in this package a caller can leave
// unresolved through the public API, which is why the end to end case is this
// one and not a background. The slice belongs to the caller — the ownership
// rule of the project plan, section 4 — so the library must not resolve it,
// and a diagnosis is the whole of the contract.
func TestASemanticPaletteEntryIsReported(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a gallery with a semantic palette painted without complaint. " +
				"It does not paint the wrong colour, it paints nothing: the tiles are laid " +
				"out, measured and hit testable, and the window is empty")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "TileStyle.Palette") {
			t.Errorf("the panic does not name the field to fix: %v", r)
		}
		if !strings.Contains(msg, "ColorAccent") {
			t.Errorf("the panic does not name the colour: %v", r)
		}
	}()

	g := ui.NewGallery(asset.NewCollection(paletteSynth(9)))
	gifttest.New(t, gifttest.Options{
		Theme: ui.DarkTheme(),
		View: ui.ImageGallery(g).
			Tile(ui.TileStyle{CornerRadius: 4, Palette: []ui.Color{ui.ColorAccent}}).
			Flex(1).Key("gallery"),
		Size: geom.Sz(400, 300),
	})
}

func paletteSynth(n int) []asset.Metadata {
	out := make([]asset.Metadata, n)
	for i := range n {
		out[i] = asset.Metadata{
			ID: asset.ID("img-" + strconv.Itoa(i)), Revision: "r1",
			MIMEType: "image/jpeg", Width: 400, Height: 300,
		}
	}
	return out
}
