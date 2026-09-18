//go:build giftdebug

package gift

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/internal/scene"
)

// diagnoseOverflow reports a node whose content stopped fitting, or stopped
// overflowing again.
//
// The counters in [Diagnostics] say how much overflow there is; they do not
// say where. This says where, and it exists only in a build with the giftdebug
// tag because naming a node means formatting a string, which is an allocation
// and has no business in the frame path of a release build. See the release
// counterpart in overflow_release.go, which compiles to nothing.
func diagnoseOverflow(a *App, h scene.Handle, nd *nodeData, over geom.Size) {
	var zero geom.Size
	if over == zero {
		a.overflowLog("gift: node %s no longer overflows", nodeLabel(a, h, nd))
		return
	}
	a.overflowLog("gift: node %s overflows its parent by %.3gx%.3g logical pixels; "+
		"the children keep their honest sizes and positions and are not clipped, "+
		"see the project plan, section 7, Overflow-Modell",
		nodeLabel(a, h, nd), over.W, over.H)
}

// nodeLabel names a node as well as gift can: registered view type, key or
// scope path, and slot index.
//
// Deliberately without the node's size. The layouter has not returned yet when
// this runs, so Node.Size still holds the size of the previous pass, and a
// diagnosis that prints a stale number next to a fresh one is worse than one
// that prints neither.
func nodeLabel(a *App, h scene.Handle, nd *nodeData) string {
	n := a.store.Get(h)
	name := TypeName(TypeID(n.TypeID))
	switch {
	case n.Key != "":
		return fmt.Sprintf("%s{key:%s index:%d}", name, n.Key, h.Index())
	case nd.scope != nil:
		return fmt.Sprintf("%s{scope:%s index:%d}", name, nd.scope.path, h.Index())
	default:
		return fmt.Sprintf("%s{index:%d}", name, h.Index())
	}
}

// overflowLog writes the diagnosis to the application's logger, or, when the
// application did not supply one, to standard error.
//
// gift never falls back to slog.Default; the project plan, section 15, is
// explicit about that, and standard error is not slog.Default. Falling back to
// nothing at all was the alternative, and a build whose only purpose is to
// diagnose must not stay silent because the application forgot a logger.
func (a *App) overflowLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if a.log != nil {
		a.log.Warn(msg, slog.String("giftdebug", "overflow"))
		return
	}
	fmt.Fprintln(os.Stderr, "giftdebug:", msg)
}
