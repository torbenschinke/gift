// Command example-gallery is the virtualised image gallery of the project
// plan, section 10, with a hundred thousand synthetic entries and no image
// pipeline yet.
//
//	go run ./cmd/example-gallery
//	go run ./cmd/example-gallery -n 1000000
//	GIFT_METRICS=1 go run -tags giftmetrics ./cmd/example-gallery
//
// Scroll with the wheel or by dragging; a fling keeps going. Click a tile to
// select it, shift-click or control-click to extend, and use the arrow keys,
// page keys, home and end once the gallery has the focus. The two buttons
// switch between masonry and justified at runtime.
//
// Every tile is a placeholder, which is the point of this step: the colours
// come from a hash of the stable asset ID, so a tile that is recycled changes
// colour with the picture it now stands for and a tile that scrolls away and
// comes back looks the same. Step 4 draws thumbnails in their place.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// count is the only flag. It earns its place: the entire claim of this example
// is that the catalogue size does not reach the frame path, and a reader who
// cannot change the number cannot check that claim.
var count = flag.Int("n", 100000, "number of synthetic catalogue entries")

func main() {
	flag.Parse()
	if err := loadFont(); err != nil {
		fmt.Fprintln(os.Stderr, "example-gallery:", err)
		os.Exit(1)
	}
	gallery := ui.NewGallery(asset.NewCollection(photos(*count)))
	app := gift.New(gift.Options{Root: func(ctx *gift.Context) gift.View {
		return browser(ctx, gallery)
	}})
	if err := backend.Run(app, backend.Config{
		Title: "gift gallery", Width: 1280, Height: 800,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "example-gallery:", err)
		os.Exit(1)
	}
}

// photos invents n catalogue entries. They are ordinary asset.Metadata: an ID,
// a revision and the oriented pixel size, which is all a gallery needs.
//
// Every seventh entry has no dimensions. Those are laid out with the
// provisional aspect ratio and drawn in the provisional colour, so the state a
// cold catalogue is really in is visible instead of hypothetical.
func photos(n int) []asset.Metadata {
	out := make([]asset.Metadata, n)
	for i := range n {
		m := asset.Metadata{ID: asset.ID("photo-" + strconv.Itoa(i)), Revision: "v1"}
		if i%7 != 0 {
			m.Width = uint32(600 + (i*173)%1400)
			m.Height = uint32(500 + (i*97)%900)
			m.MIMEType = "image/jpeg"
		}
		out[i] = m
	}
	return out
}

var (
	ink    = ui.RGB(18, 20, 26)
	chrome = ui.RGB(28, 31, 40)
	label  = ui.RGB(236, 238, 242)
	muted  = ui.RGBA(255, 255, 255, 150)
	hair   = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 40)}

	tab    = ui.ButtonStyle{Background: ui.RGB(48, 53, 66), Border: hair, CornerRadius: 8}
	tabOn  = ui.ButtonStyle{Background: ui.RGB(78, 118, 190), Border: hair, CornerRadius: 8}
	tabHot = ui.ButtonStyle{Background: ui.RGB(64, 71, 88), Border: hair, CornerRadius: 8}

	tiles = ui.TileStyle{
		CornerRadius: 6,
		Palette: []ui.Color{
			ui.RGB(196, 84, 74), ui.RGB(74, 142, 196), ui.RGB(96, 176, 116),
			ui.RGB(206, 166, 74), ui.RGB(150, 106, 190), ui.RGB(88, 172, 172),
		},
		Provisional: ui.RGBA(255, 255, 255, 22),
		Selected:    ui.Border{Width: 3, Color: ui.RGB(255, 255, 255)},
		Cursor:      ui.Border{Width: 2, Color: ui.RGBA(255, 255, 255, 140)},
	}
)

// browser is the whole application: a toolbar and the gallery.
//
// Note what is *not* here. There is no view per entry, no state per entry and
// no list of tiles: the catalogue is a value and the gallery is one view over
// it. Scrolling this function's output does not call this function.
func browser(ctx *gift.Context, g *ui.Gallery) gift.View {
	justified := ctx.State("justified", false)
	// picked exists only so the toolbar can say something about the
	// selection. The selection itself lives in the gallery's asset.Selection,
	// which is where it survives recycling; see the project plan, section 5.
	picked := ctx.State("picked", asset.ID(""))

	arrangement := ui.Masonry().MinColumnWidth(220).Gap(10)
	if ctx.Read(justified) {
		arrangement = ui.Justified().RowHeight(200).Gap(10)
	}

	return ui.VStack(
		toolbar(ctx, g, justified, ctx.Read(picked)),
		ui.ImageGallery(g).
			Layout(arrangement).
			Tile(tiles).
			Padding(10).
			Background(ink).
			OnSelect(func(id asset.ID) { picked.Set(id) }).
			OnActivate(func(id asset.ID) { picked.Set("opening " + id) }).
			Flex(1),
	).Background(ink)
}

func toolbar(ctx *gift.Context, g *ui.Gallery, justified *gift.State[bool], picked asset.ID) gift.View {
	on := ctx.Read(justified)
	status := strconv.Itoa(g.Collection().Len()) + " photos"
	if n := g.Selection().Len(); n > 0 {
		status += " · " + strconv.Itoa(n) + " selected · " + string(picked)
	}
	return ui.HStack(
		ui.Text("Library").FontSize(20).Foreground(label),
		ui.Text(status).FontSize(13).Foreground(muted),
		ui.Spacer(),
		modeButton("Masonry", !on, func() { justified.Set(false) }),
		modeButton("Justified", on, func() { justified.Set(true) }),
	).Gap(12).PaddingInsets(geom.Insets{Top: 10, Right: 14, Bottom: 10, Left: 14}).
		Align(geom.Alignment{Y: 0.5}).
		Background(chrome)
}

func modeButton(name string, active bool, set func()) ui.ButtonView {
	style := tab
	if active {
		style = tabOn
	}
	return ui.Button(ui.Text(name).FontSize(13).Foreground(label), set).
		Style(style).HoverStyle(tabHot).PressedStyle(tabOn).
		PaddingInsets(geom.Insets{Top: 7, Right: 14, Bottom: 7, Left: 14})
}
