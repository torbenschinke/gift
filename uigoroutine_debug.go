//go:build giftdebug

package gift

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// uiGuard remembers which goroutine owns the App.
//
// It only exists in a build with the giftdebug tag. In a release build the
// type is an empty struct and the assertion below compiles to nothing, which
// is what keeps an event handler that writes state on the zero allocation
// side of the frame path contract; see the project plan, section 11.
type uiGuard struct {
	id  atomic.Uint64
	set atomic.Bool
}

// capture records the calling goroutine as the UI executor. It is called once,
// from [New], and costs one stack parse for the whole lifetime of the App.
func (g *uiGuard) capture() {
	g.id.Store(goid())
	g.set.Store(true)
}

// assertUIGoroutine rejects state access from outside the UI executor.
//
// The check is a goroutine id comparison, nothing more: the id of the UI
// executor is captured once in [New] and every access compares against the
// cached value. There is no stack parsing on an accepted access.
//
// Known limits, stated honestly:
//
//   - The id is read with runtime.Stack, which is the only portable way to
//     obtain it. That costs roughly a microsecond and therefore happens
//     exactly once per App plus once per access that is about to be rejected.
//   - Goroutine ids are reused by the runtime, so a new goroutine can in
//     principle inherit the id of the UI executor after it has exited. That
//     is a theoretical false negative, never a false positive.
//   - The check is absent from a release build. It diagnoses a programming
//     error during development; the race detector is the tool that finds the
//     same class of bug in a build without the tag.
func (a *App) assertUIGoroutine(what string) {
	if !a.ui.set.Load() {
		return
	}
	owner := a.ui.id.Load()
	if id := goid(); id != owner {
		panic(fmt.Sprintf(
			"gift: %s called from goroutine %d, but the UI executor is goroutine %d; "+
				"post the result to the UI executor instead of touching state directly",
			what, id, owner))
	}
}

var goroutinePrefix = []byte("goroutine ")

// goid returns the id of the calling goroutine.
//
// There is no supported API for this. Parsing the stack header is the
// portable way and costs roughly a microsecond, which is why it is called
// once when the UI executor is captured and once per rejected state access,
// never on an accepted one.
func goid() uint64 {
	var buf [48]byte
	n := runtime.Stack(buf[:], false)
	b := buf[:n]
	if len(b) < len(goroutinePrefix) {
		return 0
	}
	b = b[len(goroutinePrefix):]
	var id uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}
