//go:build !giftdebug

package gift

// uiGuard is empty unless the giftdebug build tag is set.
//
// The release version of the UI executor check exists so that the call sites
// in [State.Get], [State.Set] and [Binding.Get] stay identical in both builds.
// Neither the type nor the methods leave a single instruction behind: an empty
// struct occupies no space and an empty method is inlined away.
//
// This matters because those call sites are the event handler path, and an
// event handler is inside the zero allocation contract of the frame path; see
// the project plan, section 11. Obtaining a goroutine id requires parsing the
// runtime stack header, which allocates and costs microseconds, so it cannot
// be on that path even once per access.
type uiGuard struct{}

// capture does nothing in a release build.
func (*uiGuard) capture() {}

// assertUIGoroutine does nothing in a release build. Build with
// -tags giftdebug to have state access from outside the UI executor rejected
// with a panic. Without the tag, the race detector is the tool for that class
// of bug.
func (*App) assertUIGoroutine(string) {}
