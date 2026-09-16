//go:build !giftauto

package ebiten

import "github.com/torbenschinke/gift"

// autoEnabled is false without the giftauto build tag, and it is a constant,
// so every `if autoEnabled` in this package is removed by the compiler
// together with the branch behind it. That is what makes the automation
// interface impossible to link into a production binary: there is no hook
// variable to set, no exported function to call and no branch to take.
const autoEnabled = false

// autoStart does nothing. See automation_giftauto.go for the real one.
func autoStart(*gift.App) {}

// autoWantsFrame does nothing. See automation_giftauto.go.
func autoWantsFrame() bool { return false }

// autoFrame does nothing. See automation_giftauto.go.
func autoFrame(Frame) {}

// Frame is declared in both files so that the signatures above are legal
// without the tag. Nothing constructs one in this build.
type Frame struct {
	// Width and Height are the size of the captured framebuffer in physical
	// pixels.
	Width, Height int
	// Pix is the framebuffer in RGBA order, Width*Height*4 bytes long.
	Pix []byte
	// Count is the number of frames this backend has drawn, this one
	// included.
	Count uint64
}
