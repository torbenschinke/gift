// Command example-kitchensink is the whole of gift on one screen: the
// navigation of the project plan, section 23, step 9b, the controls of step 9a,
// the list components of step 9c, the text field and on-screen keyboard of
// step 8, the icons of section 21, the image pipeline of section 9 and the
// runtime theme switch of section 20.
//
//	go run ./cmd/example-kitchensink
//	GIFT_METRICS=1 go run -tags giftmetrics ./cmd/example-kitchensink
//	go run ./cmd/example-kitchensink -icons
//
// It is meant to be looked at. Four tabs along the bottom; the first is a
// navigation stack you can drill into and come back from; the second is a
// settings list with every control from step 9a in it; the third is a long
// list next to the number at which a long list stops being a good idea; the
// fourth is a form with a text field, the on-screen keyboard and an alert. The
// button in the top right switches the whole window between light and dark
// without a single colour being passed down anywhere.
//
// # What the -icons flag is for
//
// The texture upload budget of the project plan, section 21, is eight uploads
// per drawn frame, so a screen with more new icons than that fills in over
// several frames. The flag prints one line per tick for the first second and a
// half — the number of drawn frames and the number of icon masks rasterised,
// uploaded and resident — so the claim in section 21 is a measurement in this
// program rather than a sentence in a document.
//
// Measured on the first screen this demo opens on: thirty icon operations
// drawn from twenty-eight distinct masks — two rows end in the same chevron at
// the same size and share one texture. The masks arrive over four or five
// drawn frames, never more than eight in any one of them. Two runs of the real
// program, uploads resident after each drawn frame:
//
//	8, 19, 27, 28
//	5, 11, 19, 27, 28
//
// and the headless test next door, which has no picture thumbnails competing
// for the same budget, measured 8, 16, 24, 28. The first drawn frame is
// therefore visibly incomplete — twenty of the twenty-eight symbols are not
// there yet — and the screen is whole about seventy milliseconds later. On a
// kiosk that is the window opening, which is the one moment where it does not
// matter. TestTheFirstScreenFillsInItsIconsWithinTheUploadBudget in this
// package is that measurement as a test.
//
// The trace also shows the price of the deferral, which section 21 does not
// mention: seventy-eight rasterisations for twenty-eight masks. A mask whose
// upload the budget refuses is not kept, so it is rasterised again on the next
// drawn frame. It is bounded — it only happens while a screen is filling in —
// and it is CPU work on the frames that were already the slowest.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/internal/example"
	"github.com/torbenschinke/gift/internal/example/kitchensink"
	"github.com/torbenschinke/gift/ui"
)

var traceIcons = flag.Bool("icons", false, "print the icon cache counters once per tick for the first second and a half")

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "example-kitchensink:", err)
		os.Exit(1)
	}
}

// run is everything the window needs and nothing the screen decides: the
// views themselves live in [kitchensink], so that cmd/gift-shot and the golden
// tests can render the same screens this window shows.
func run() error {
	if err := example.LoadFont(); err != nil {
		return err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	if kitchensink.Pictures, err = kitchensink.Samples(filepath.Join(base, "gift", "kitchensink")); err != nil {
		return err
	}

	app := gift.New(gift.Options{Root: kitchensink.Screen})
	kitchensink.App = app

	// The image pipeline. ui.Image draws through whatever is installed here
	// and draws its placeholder when nothing is; see ui.SetImagePipeline.
	pipe := asset.NewPipeline(asset.Config{Deliver: app.Post})
	defer func() {
		pipe.Close()
		// The closures Close produced still have to run: each one releases a
		// thumbnail reference, and the frame loop has stopped by now.
		app.DrainPosts()
	}()
	ui.SetImagePipeline(pipe)

	// The kiosk switch. This is the one line that makes a text field ask for
	// gift's own keyboard when it takes the focus; without it the field is an
	// ordinary desktop field. See ui.SetOnScreenKeyboard.
	ui.SetOnScreenKeyboard(nil, true)

	if *traceIcons {
		go kitchensink.TraceIconCache(app)
	}
	return backend.Run(app, backend.Config{
		Title: "gift kitchen sink", Width: 900, Height: 760,
	})
}
