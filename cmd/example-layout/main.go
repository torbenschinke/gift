// Command example-layout is the first gift program to read: the counter from
// the project plan, section 4, with the styling and the keyboard handling that
// make it a real control rather than a click handler.
//
//	go run ./cmd/example-layout
//	GIFT_METRICS=1 go run -tags giftmetrics ./cmd/example-layout
//
// Click the buttons, or tab to one and press space or enter. gift ships no
// font, so loadFont finds one; GIFT_FONT overrides the search.
package main

import (
	"fmt"
	"os"
	"strconv"

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
	app := gift.New(gift.Options{Root: counter})
	if err := backend.Run(app, backend.Config{Title: "gift counter", Width: 480, Height: 280}); err != nil {
		fmt.Fprintln(os.Stderr, "example-layout:", err)
		os.Exit(1)
	}
}

var (
	ink   = ui.RGB(18, 20, 26)
	panel = ui.RGB(30, 34, 44)
	label = ui.RGB(236, 238, 242)
	muted = ui.RGBA(255, 255, 255, 150)
	hair  = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 40)}

	step = ui.ButtonStyle{Background: ui.RGB(52, 58, 72), Border: hair, CornerRadius: 10}
	hot  = ui.ButtonStyle{Background: ui.RGB(70, 78, 98), Border: hair, CornerRadius: 10}
	sunk = ui.ButtonStyle{Background: ui.RGB(36, 40, 52), Border: hair, CornerRadius: 10}
	off  = ui.ButtonStyle{Background: ui.RGBA(255, 255, 255, 14), CornerRadius: 10}
)

// counter is the view function from the project plan, section 4. The state
// write in a button's closure invalidates this scope and nothing else; a hover
// or a press invalidates no scope at all, because interaction state lives in
// the retained node.
func counter(ctx *gift.Context) gift.View {
	count := ctx.State("count", 0)
	n := ctx.Read(count)

	// The ZStack fills the window: a Box is greedy on every bounded axis, and
	// a ZStack bounds both. The panel is centred on top of it.
	return ui.ZStack(ui.Box().Background(ink), ui.VStack(
		ui.Text("Counter").FontSize(24).Foreground(label),
		ui.HStack(
			ui.Text(strconv.Itoa(n)).FontSize(48).Foreground(label),
			ui.Text("clicks").FontSize(14).Foreground(muted),
		).Gap(8).AlignBaseline(),
		ui.HStack(
			ui.Button(ui.Text("-").FontSize(20).Foreground(label), func() {
				count.Set(count.Get() - 1)
			}).Style(step).HoverStyle(hot).PressedStyle(sunk).DisabledStyle(off).
				Frame(56, 44).Disabled(n == 0),
			ui.Button(ui.Text("+").FontSize(20).Foreground(label), func() {
				count.Set(count.Get() + 1)
			}).Style(step).HoverStyle(hot).PressedStyle(sunk).Frame(56, 44),
			ui.Button(ui.Text("Reset").FontSize(15).Foreground(label), func() {
				count.Set(0)
			}).Style(step).HoverStyle(hot).PressedStyle(sunk).DisabledStyle(off).
				PaddingInsets(geom.Insets{Top: 10, Right: 16, Bottom: 10, Left: 16}).
				Disabled(n == 0),
		).Gap(8),
		ui.Text("Click, or tab to a button and press space or enter.").
			FontSize(12).Foreground(muted),
	).Gap(16).Padding(24).Align(geom.Alignment{X: 0.5}).
		Background(panel).CornerRadius(16).Border(hair),
	).Align(geom.Alignment{X: 0.5, Y: 0.5})
}
