// Command gift-stress puts the stress scene of internal/stress into a window.
//
// It is a measurement harness, not an example. Read cmd/example-layout to
// learn gift; read this only to reproduce a number. The scene is the several
// hundred node stack scene the project plan, section 12, "Go/No-Go vor
// Schritt 2", criterion 2, asks to be held at sixty frames per second on a
// Raspberry Pi 4.
//
//	go run -tags giftmetrics ./cmd/gift-stress -rows 40 -cells 12 -duration 60s
//
// with GIFT_METRICS=1 in the environment. The measurement itself belongs to
// the metrics package and is documented there; this command has no reporting
// code of its own, and without the build tag it takes no measurement at all.
//
// Flags are acceptable here in a way they are not in a library and not in an
// example: this program exists to be parameterised.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/torbenschinke/gift"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/internal/stress"
)

func main() {
	var (
		rows     = flag.Int("rows", 12, "number of rows in the body; the main complexity knob")
		cells    = flag.Int("cells", 14, "number of cells per row")
		duration = flag.Duration("duration", 0, "exit after this long; zero runs until the window is closed")
		width    = flag.Int("width", 1280, "window width in logical pixels")
		height   = flag.Int("height", 720, "window height in logical pixels")
		idleTPS  = flag.Int("idle-tps", 0, "tick rate to fall back to while nothing changes, against Pi thermal "+
			"throttling; zero, the default, disables the idle policy because it changes what is being measured")
	)
	flag.Parse()

	sc := stress.New(*rows, *cells)
	app := gift.New(gift.Options{Root: sc.Root})
	deadline := time.Now().Add(*duration)

	err := backend.Run(app, backend.Config{
		Title:   "gift stress",
		Width:   *width,
		Height:  *height,
		IdleTPS: *idleTPS,
		OnUpdate: func() error {
			sc.Tick()
			if inpututil.IsKeyJustPressed(eb.KeySpace) {
				sc.Bump()
			}
			if inpututil.IsKeyJustPressed(eb.KeyEscape) {
				return backend.Terminate
			}
			if *duration > 0 && time.Now().After(deadline) {
				return backend.Terminate
			}
			return nil
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gift-stress:", err)
		os.Exit(1)
	}
}
