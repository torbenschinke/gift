//go:build giftauto

package ebiten

import (
	"sync"

	"github.com/worldiety/gift"
)

// This file is the whole of the automation seam on the backend side, and it
// exists only under the giftauto build tag. Without the tag the counterpart in
// automation_off.go compiles the seam away to a constant false.
//
// It deliberately knows nothing about HTTP, JSON or the driver. It offers two
// things the frame loop alone can offer — the [gift.App] before the window
// opens, and the *real* framebuffer inside Draw — and hands them to whoever
// registered. The gift/auto package is that whoever.

// Frame is one captured framebuffer: the pixels the window actually drew.
//
// It is a plain struct rather than an *ebiten.Image so that a consumer of this
// seam neither imports Ebitengine nor is able to keep a reference to a screen
// image beyond the Draw call it was valid in. Pix is owned by the receiver.
type Frame struct {
	// Width and Height are the size of the captured framebuffer in physical
	// pixels. That is the logical window size times the device density; see
	// [game.LayoutF].
	Width, Height int
	// Pix is the framebuffer in RGBA order, Width*Height*4 bytes long.
	Pix []byte
	// Count is the number of frames this backend has drawn, this one
	// included.
	Count uint64
}

// Automation is the interface the giftauto seam calls into.
//
// The three methods are called from the frame loop goroutine, which is the UI
// executor, and in this order: Start once before the window opens, then
// WantsFrame at the end of every drawn frame, and Frame only when the
// preceding WantsFrame said yes.
type Automation interface {
	// Start is called once with the application, on the UI goroutine, before
	// the window opens and before the first update.
	Start(app *gift.App)
	// WantsFrame reports whether the next Frame call should happen. It is
	// called once per drawn frame and must be cheap, because reading back a
	// framebuffer is not: an implementation that always says yes turns every
	// frame into a GPU stall.
	WantsFrame() bool
	// Frame delivers the framebuffer of the frame that has just been drawn.
	Frame(f Frame)
}

// autoEnabled is true in this build. See automation_off.go.
const autoEnabled = true

var (
	autoMu sync.Mutex
	auto   Automation
)

// SetAutomation installs the automation seam. It exists only in a build with
// the giftauto tag, so a production binary cannot call it even by accident.
//
// It is safe to call from any goroutine and before [Run]; the gift/auto
// package calls it from an init function. Passing nil removes the seam.
func SetAutomation(a Automation) {
	autoMu.Lock()
	auto = a
	autoMu.Unlock()
}

// installedAutomation returns the seam, or nil.
func installedAutomation() Automation {
	autoMu.Lock()
	a := auto
	autoMu.Unlock()
	return a
}

// autoStart hands the application to the seam before the window opens.
func autoStart(app *gift.App) {
	if a := installedAutomation(); a != nil {
		a.Start(app)
	}
}

// autoWantsFrame asks the seam whether this drawn frame has to be read back.
func autoWantsFrame() bool {
	a := installedAutomation()
	return a != nil && a.WantsFrame()
}

// autoFrame delivers the captured framebuffer to the seam.
func autoFrame(f Frame) {
	if a := installedAutomation(); a != nil {
		a.Frame(f)
	}
}
