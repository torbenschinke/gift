//go:build giftdebug

package gift

import (
	"fmt"
	"runtime"
	"sync"
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
//     obtain it. That costs roughly a microsecond, and it happens on *every*
//     access — accepted or not — because there is nothing to compare against
//     otherwise. The comment here used to claim the opposite, and the code
//     never did what it said.
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

// goidBufs holds the scratch buffers of [goid].
//
// runtime.Stack takes a []byte and the compiler cannot prove it does not keep
// it, so a local array escapes and every call allocates 48 bytes. That went
// unnoticed for as long as nothing in the frame path called the guard: the
// state accessors are the only other caller and the allocation benchmarks that
// cover them skip the giftdebug build for this very reason. The input entry
// points call it too, and WU-L put a wheel event and a fling tick in the frame
// path, so the allocation became a contract violation — "the allocation
// contract must hold in every tag combination".
//
// A pool rather than a per App buffer because the whole purpose of the check
// is to catch a *foreign* goroutine, so the buffer cannot belong to the App.
// A pooled Get and Put allocate nothing after the first use per processor.
var goidBufs = sync.Pool{New: func() any { return new([64]byte) }}

// goid returns the id of the calling goroutine.
//
// There is no supported API for this. Parsing the stack header is the portable
// way and costs roughly a microsecond per call.
func goid() uint64 {
	buf := goidBufs.Get().(*[64]byte)
	defer goidBufs.Put(buf)
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
