// Command example-effects is the effects demonstration of the project plan,
// section 12, step 5: border, shadow and a glass panel over the gallery.
//
//	go run ./cmd/example-effects
//	go run ./cmd/example-effects -quality=full
//	GIFT_METRICS=1 go run -tags giftmetrics ./cmd/example-effects
//
// Scroll the gallery behind the panel with the wheel or by dragging. The three
// buttons in the panel pin the glass quality level, which is what the project
// plan, section 13, requires before two measurements may be compared; the
// label next to them says which level is actually in effect, because under
// Adaptive that is a decision the backend takes from the frame intervals and
// not something the application knows.
//
// # What to look at
//
// The card row along the bottom is border and shadow: the same rounded
// rectangle with a growing blur, an offset and a spread, all of it analytic in
// the shared shape shader with no offscreen image anywhere.
//
// The panel across the top is the material. Scroll underneath it and watch the
// backdrop follow: a glass panel that never moves is still re-blurred every
// frame, because what is behind it moved. The project plan, section 8, says
// that outright — "Beim Scrollen darunter wird er dirty" — and it is the
// reason there is no cache to look for here.
//
// # Honest limits
//
// The material is experimental and the project plan, section 8, says so. It
// makes no claim to be anybody else's glass. On Reduced there is no blur at
// all: the backdrop is sampled unblurred, and what makes it read as a surface
// is the rim brightening, the specular streak and the refraction at the edge.
// Over a busy backdrop that is convincing; over a plain flat colour it is a
// slightly tinted rectangle with bright edges, and no amount of shader will
// change that, because there is nothing behind it to bend.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

var (
	quality = flag.String("quality", "adaptive", "glass quality to pin: adaptive, reduced or full")
	count   = flag.Int("count", 4000, "number of placeholder tiles behind the panel")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "example-effects:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := loadFont(); err != nil {
		return err
	}
	q, err := parseQuality(*quality)
	if err != nil {
		return err
	}

	// No image pipeline on purpose. The tiles draw their placeholder colours,
	// which is a cheap, deterministic, endlessly scrollable backdrop — and
	// the backdrop is what this example is about. example-gallery is where
	// real pictures live.
	gallery = ui.NewGallery(asset.NewCollection(placeholders(*count)))

	app := gift.New(gift.Options{Root: root})
	// A logger, because this is the example where the adaptive policy runs.
	// The project plan, section 15, wants `Warn` for degraded quality and
	// names a fall back to Glass Reduced as the example of it; without a
	// logger gift stays silent, and the one event section 15 names by name
	// would be invisible outside a giftmetrics build. Nothing is logged per
	// frame: the level is a counter and only a *change* is a line.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return backend.Run(app, backend.Config{
		Title: "gift effects", Width: 1280, Height: 800,
		Logger:       log,
		GlassQuality: q,
		OnRenderer:   func(r *backend.Renderer) { renderer = r },
	})
}

// gallery and renderer are package level because a view function is called
// every build and must not construct either of them. The gallery owns an index
// over thousands of entries and the renderer owns the GPU; both outlive any
// number of builds. This is the correction the project plan, section 10,
// records: the long lived object belongs to the application and the view is
// the short lived declaration on top of it.
var (
	gallery  *ui.Gallery
	renderer *backend.Renderer
)

func parseQuality(s string) (render.GlassQuality, error) {
	switch strings.ToLower(s) {
	case "", "adaptive":
		return ui.Adaptive, nil
	case "reduced":
		return ui.Reduced, nil
	case "full":
		return ui.Full, nil
	}
	return ui.Adaptive, fmt.Errorf("unknown -quality %q: want adaptive, reduced or full", s)
}

// placeholders builds the synthetic catalogue. Varying aspect ratios so the
// masonry columns are not all the same shape and the backdrop has structure to
// refract.
func placeholders(n int) []asset.Metadata {
	if n < 1 {
		n = 1
	}
	items := make([]asset.Metadata, n)
	for i := range items {
		items[i] = asset.Metadata{
			ID:     asset.ID(fmt.Sprintf("tile-%06d", i)),
			Width:  uint32(600 + (i*173)%900),
			Height: uint32(500 + (i*97)%700),
		}
	}
	return items
}

var (
	ink   = ui.RGB(14, 16, 22)
	label = ui.RGB(240, 242, 248)
	muted = ui.RGBA(255, 255, 255, 160)
	hair  = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 90)}

	pill    = ui.ButtonStyle{Background: ui.RGBA(255, 255, 255, 28), Border: hair, CornerRadius: 9}
	pillOn  = ui.ButtonStyle{Background: ui.RGBA(120, 170, 255, 170), Border: hair, CornerRadius: 9}
	pillHot = ui.ButtonStyle{Background: ui.RGBA(255, 255, 255, 60), Border: hair, CornerRadius: 9}

	tiles = ui.TileStyle{
		CornerRadius: 8,
		Palette: []ui.Color{
			ui.RGB(196, 84, 64), ui.RGB(72, 132, 196), ui.RGB(96, 172, 108),
			ui.RGB(212, 172, 72), ui.RGB(148, 92, 188), ui.RGB(64, 168, 176),
		},
	}
)

// root is the whole application: a gallery, a glass panel over it, and a row
// of border and shadow cards.
//
// The panel is a sibling of the gallery in a ZStack and not a child of it.
// That is the arrangement the project plan, section 8, asks for — "ein
// Glass-Panel ueber scrollender Galerie. Kein Blur fuer jede einzelne Kachel"
// — and it is also the only one that is affordable: one material region of a
// few hundred thousand pixels, however many tiles happen to be under it.
func root(ctx *gift.Context) gift.View {
	pinned := ctx.State("pinned", ui.Adaptive)

	return ui.ZStack(
		ui.ImageGallery(gallery).
			Layout(ui.Masonry().MinColumnWidth(200).Gap(8)).
			Tile(tiles).
			Overscan(200).
			Padding(8).
			Background(ink),
		// The overlay column: the panel at the top, the cards at the bottom,
		// a Spacer between them. A ZStack gives its children bounded
		// constraints on both axes, so this column fills the window and its
		// Spacer does the placing.
		ui.VStack(
			panel(ctx, pinned),
			ui.Spacer(),
			cards(),
		).PaddingInsets(geom.Insets{Top: 18, Right: 18, Bottom: 18, Left: 18}),
	)
}

// panel is the glass material, spelled the way the project plan, section 8,
// spells it.
func panel(ctx *gift.Context, pinned *gift.State[ui.GlassQuality]) ui.Stack {
	want := ctx.Read(pinned)
	effective := "reduced"
	if renderer != nil {
		effective = renderer.GlassLevel().String()
	}
	status := "effective: " + effective
	if want == ui.Adaptive {
		status += " · chosen by measurement"
	} else {
		status += " · pinned"
	}

	return ui.VStack(
		ui.HStack(
			ui.Text("Library").FontSize(24).Foreground(label),
			ui.Spacer(),
			levelButton("Adaptive", ui.Adaptive, want, pinned),
			levelButton("Reduced", ui.Reduced, want, pinned),
			levelButton("Full", ui.Full, want, pinned),
		).Gap(8).Align(geom.Alignment{Y: 0.5}),
		ui.Text(status).FontSize(13).Foreground(muted),
	).
		Gap(12).
		Padding(20).
		Background(ui.Glass().Quality(ui.Adaptive)).
		Border(ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 90)}).
		CornerRadius(18).
		Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)})
}

// levelButton pins one quality level.
//
// Pinning is a property of the renderer and not of the view tree, because the
// adaptive policy lives where the frame intervals are measured. So the button
// writes the application's state, which redraws the label, and tells the
// renderer, which changes the pixels.
func levelButton(name string, level, want ui.GlassQuality, pinned *gift.State[ui.GlassQuality]) ui.ButtonView {
	style := pill
	if level == want {
		style = pillOn
	}
	return ui.Button(ui.Text(name).FontSize(13).Foreground(label), func() {
		pinned.Set(level)
		if renderer != nil {
			renderer.PinGlassQuality(level)
		}
	}).
		Style(style).HoverStyle(pillHot).PressedStyle(pillOn).
		PaddingInsets(geom.Insets{Top: 7, Right: 14, Bottom: 7, Left: 14})
}

// cards is the border and shadow half: the same rounded rectangle four times,
// with the shadow parameters of the project plan, section 8, varied one at a
// time so that each one is attributable.
func cards() ui.Stack {
	return ui.HStack(
		card("Border", ui.Shadow{}),
		card("Blur 8", ui.Shadow{Blur: 8, Color: ui.RGBA(0, 0, 0, 140)}),
		card("Blur 24, Y+8", ui.Shadow{Blur: 24, OffsetY: 8, Color: ui.RGBA(0, 0, 0, 160)}),
		card("Spread -6", ui.Shadow{Blur: 20, OffsetY: 10, Spread: -6, Color: ui.RGBA(0, 0, 0, 180)}),
		ui.Spacer(),
	).Gap(22)
}

func card(name string, sh ui.Shadow) ui.Stack {
	return ui.VStack(
		ui.Text(name).FontSize(13).Foreground(label),
	).
		Frame(170, 78).
		Align(geom.Alignment{X: 0.5, Y: 0.5}).
		Background(ui.RGBA(30, 34, 46, 235)).
		Border(hair).
		CornerRadius(14).
		Shadow(sh)
}
