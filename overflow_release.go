//go:build !giftdebug

package gift

import (
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// diagnoseOverflow does nothing in a release build.
//
// Naming the node that overflowed means formatting a string, and the frame
// path of a release build formats nothing; see the project plan, section 15.
// The empty body is inlined away, so not one instruction is left behind — the
// counters in [Diagnostics] are what a release build offers, and
// -tags giftdebug is how you find out which node they refer to.
func diagnoseOverflow(*App, scene.Handle, *nodeData, geom.Size) {}
