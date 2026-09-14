// Command example-layout is the first gift program to read: nested stacks, the
// style modifiers, [ui.Text], a [gift.Component] with its own state scope and
// [gift.Memo] for the rows that do not need rebuilding.
//
//	go run ./cmd/example-layout
//	GIFT_METRICS=1 go run -tags giftmetrics ./cmd/example-layout
//
// gift ships no font, so loadFont finds one; GIFT_FONT overrides the search.
// There is no button yet — that is [ui.Button] and it does not exist — so tick
// stands in for the input that will eventually drive the state.
package main

import (
	"fmt"
	"os"

	"github.com/torbenschinke/gift"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

func main() {
	if err := loadFont(); err != nil {
		fmt.Fprintln(os.Stderr, "example-layout:", err)
		os.Exit(1)
	}
	app := gift.New(gift.Options{Root: dashboard})
	if err := backend.Run(app, backend.Config{Title: "gift example-layout", OnUpdate: tick}); err != nil {
		fmt.Fprintln(os.Stderr, "example-layout:", err)
		os.Exit(1)
	}
}

var (
	ink, panel = ui.RGB(18, 20, 26), ui.RGB(30, 34, 44)
	plain, hot = ui.RGB(38, 43, 56), ui.RGB(64, 96, 150)
	accent     = ui.RGB(220, 120, 60)
	label      = ui.RGB(236, 238, 242)
	muted      = ui.RGBA(255, 255, 255, 150)
	hair       = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 40)}
	items      = [][2]string{{"Library", "12 480"}, {"Recents", "96"}, {"Favourites", "7"}, {"Shared", "1 204"}, {"Imports", "0"}, {"Trash", "38"}}
	sel, pulse *gift.State[int]
	frame      int
)

// dashboard owns the selected row, so a tick rebuilds it and nothing above it;
// the rows are memoised, so only the two whose props changed rebuild.
func dashboard(ctx *gift.Context) gift.View {
	sel = ctx.State("selected", 0)
	body := make([]gift.View, len(items))
	for i := range body {
		body[i] = gift.Memo(items[i][0], rowProps{i, i == ctx.Read(sel)}, buildRow)
	}
	return ui.VStack(
		// The Spacer takes the width that is left, so the badge goes right.
		ui.HStack(
			ui.VStack(
				ui.Text("Photo Library").FontSize(24).Foreground(label),
				ui.Text("14 812 items, 96 GB").FontSize(13).Foreground(muted),
			).Gap(4),
			ui.Spacer(),
			gift.Component("badge", badge),
		).Padding(16).Align(geom.Alignment{Y: 0.5}).
			Background(accent).CornerRadius(10).Border(hair),
		ui.VStack(body...).
			Gap(6).Padding(12).Background(panel).CornerRadius(14).Border(hair).Flex(1),
	).Gap(12).Padding(16).Background(ink)
}

// badge is a component, so writing pulse rebuilds this instance alone.
func badge(ctx *gift.Context) gift.View {
	pulse = ctx.State("pulse", 0)
	lit := ctx.Read(pulse) % 9
	pips := make([]gift.View, 8)
	for i := range pips {
		c := ui.RGBA(0, 0, 0, 70)
		if i < lit {
			c = ui.RGB(255, 255, 255)
		}
		pips[i] = ui.Box().Frame(10, 10).Background(c).CornerRadius(5)
	}
	return ui.HStack(pips...).Gap(6).Padding(10).
		Background(ui.RGBA(0, 0, 0, 90)).CornerRadius(12).Border(hair)
}

// rowProps is the comparable input of a memoised row.
type rowProps struct {
	Index int
	Hot   bool
}

func buildRow(_ *gift.Context, p rowProps) gift.View {
	back := plain
	if p.Hot {
		back = hot
	}
	return ui.HStack(
		ui.Box().Frame(26, 26).Background(accent).CornerRadius(13).Border(hair),
		ui.Text(items[p.Index][0]).FontSize(15).Foreground(label),
		ui.Spacer(),
		ui.Text(items[p.Index][1]).FontSize(13).Foreground(muted),
	).Gap(10).Padding(8).Align(geom.Alignment{Y: 0.5}).
		Background(back).CornerRadius(8).Border(hair)
}

// tick moves the selection once a second and the badge every two. It runs on
// the UI executor, which is where state writes are allowed.
func tick() error {
	frame++
	if sel != nil && frame%60 == 0 {
		sel.Set(frame / 60 % len(items))
	}
	if pulse != nil && frame%120 == 0 {
		pulse.Set(pulse.Get() + 1)
	}
	return nil
}
