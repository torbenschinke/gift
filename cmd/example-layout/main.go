// Command example-layout is the first gift program to read: a small dashboard
// of nested [ui.VStack], [ui.HStack] and [ui.ZStack], a [ui.Spacer], the style
// modifiers, a [gift.Component] with a state scope of its own and [gift.Memo]
// for the rows that do not need rebuilding.
//
//	go run ./cmd/example-layout
//
// There are no flags and no measurement code here. To measure a gift program,
// rebuild it with the metrics build tag and set one environment variable; the
// program itself stays as it is. See the metrics package.
//
//	GIFT_METRICS=1 go run -tags giftmetrics ./cmd/example-layout
//
// There is no text and no button in this picture, because [ui.Text] and
// [ui.Button] do not exist yet. Everything visible is a coloured box, and the
// timer at the bottom of this file stands in for the input that will drive the
// state once there is any.
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
	app := gift.New(gift.Options{Root: dashboard})
	if err := backend.Run(app, backend.Config{Title: "gift example-layout", OnUpdate: tick}); err != nil {
		fmt.Fprintln(os.Stderr, "example-layout:", err)
		os.Exit(1)
	}
}

const rows = 6

var (
	ink    = ui.RGB(18, 20, 26)
	panel  = ui.RGB(30, 34, 44)
	plain  = ui.RGB(38, 43, 56)
	hot    = ui.RGB(64, 96, 150)
	accent = ui.RGB(220, 120, 60)
	hair   = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 40)}
	keys   = [rows]string{"r0", "r1", "r2", "r3", "r4", "r5"}

	// selected and pulse are captured during the build of the scope that owns
	// each of them. The state still belongs to its scope; tick runs on the UI
	// executor, which is where writes are allowed.
	selected, pulse *gift.State[int]
	frame           int
)

// dashboard is the root view. It owns the selected row, so a tick rebuilds
// this function and only this function; the rows are memoised, so the two
// whose props changed are rebuilt and the other four are not.
func dashboard(ctx *gift.Context) gift.View {
	selected = ctx.State("selected", 0)
	sel := ctx.Read(selected)

	body := make([]gift.View, rows)
	for i := range body {
		body[i] = gift.Memo(keys[i], rowProps{Index: i, Hot: i == sel}, buildRow)
	}

	return ui.VStack(
		header(),
		ui.VStack(body...).
			Gap(6).Padding(12).Background(panel).CornerRadius(14).Border(hair).Flex(1),
	).
		Gap(12).Padding(16).Background(ink)
}

// header puts a title block and a stateful badge on an accent plate. The
// [ui.Spacer] takes the width that is left, which puts the badge on the right.
func header() gift.View {
	return ui.ZStack(
		ui.HStack(
			ui.VStack(
				ui.Box().Frame(160, 14).Background(ui.RGB(250, 250, 250)).CornerRadius(7),
				ui.Box().Frame(110, 8).Background(ui.RGBA(255, 255, 255, 160)).CornerRadius(4),
			).Gap(6),
			ui.Spacer(),
			gift.Component("badge", badge),
		).Padding(14).Align(geom.Alignment{Y: 0.5}),
	).
		Background(accent).CornerRadius(10).Border(hair).Frame(geom.Unbounded(), 72)
}

// badge is a component, so it has a state scope of its own. Writing pulse
// rebuilds this instance and nothing above it.
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
	return ui.HStack(pips...).
		Gap(6).Padding(10).Background(ui.RGBA(0, 0, 0, 90)).CornerRadius(12).Border(hair)
}

// rowProps is the comparable input of a memoised row: no slice, no map, no
// closure, or it could not be compared.
type rowProps struct {
	Index int
	Hot   bool
}

// buildRow is a plain function of its props with no state of its own, which is
// what makes memoising it sound.
func buildRow(_ *gift.Context, p rowProps) gift.View {
	back := plain
	if p.Hot {
		back = hot
	}
	return ui.HStack(
		ui.Box().Frame(26, 26).Background(accent).CornerRadius(13).Border(hair),
		ui.Box().Frame(float32(60+30*p.Index), 10).Background(ui.RGBA(255, 255, 255, 140)).CornerRadius(5),
		ui.Spacer(),
		ui.Box().Frame(44, 16).Background(ui.RGBA(0, 0, 0, 60)).CornerRadius(8),
	).
		Gap(10).Padding(8).Align(geom.Alignment{Y: 0.5}).
		Background(back).CornerRadius(8).Border(hair)
}

// tick moves the selection once a second and the badge every two.
func tick() error {
	frame++
	if selected != nil && frame%60 == 0 {
		selected.Set(frame / 60 % rows)
	}
	if pulse != nil && frame%120 == 0 {
		pulse.Set(pulse.Get() + 1)
	}
	return nil
}
