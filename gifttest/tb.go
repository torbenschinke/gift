package gifttest

// TB is the part of [testing.TB] this package uses.
//
// # Why not testing.TB itself
//
// Because testing.TB cannot be implemented outside the testing package — it
// has an unexported method, deliberately — and this package has to be able to
// test its own failures. "A selector that matches two nodes fails with a
// message naming both" is an assertion about a failure message, and the only
// way to make it is to hand the harness a recorder instead of a *testing.T.
//
// The cost is one interface; *testing.T and *testing.B satisfy it as they are,
// so no caller ever names it.
//
// # The contract of a substitute
//
// Fatalf must not return. The harness relies on it the way every test does:
// after a Fatalf the code that follows assumes the failure stopped it. A
// recorder implements that with a panic and a recover at the boundary, which
// is what testing itself does with runtime.Goexit.
type TB interface {
	// Helper marks the calling function as a test helper, so that a failure
	// is reported at the caller's line.
	Helper()
	// Errorf reports a failure and continues.
	Errorf(format string, args ...any)
	// Fatalf reports a failure and must not return.
	Fatalf(format string, args ...any)
	// Skipf skips the test and must not return.
	Skipf(format string, args ...any)
	// Log writes to the test log.
	Log(args ...any)
	// Name is the name of the running test, used in messages.
	Name() string
}
