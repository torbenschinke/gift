//go:build race

package ui_test

// raceEnabled says whether this binary was built with the race detector.
//
// The allocation contract of the project plan, section 11, is a statement
// about the shipped frame path. A race build is not that path: the detector
// rewrites every memory access, and sync.Pool in particular stops handing back
// the buffer it was given — which is how the giftdebug UI executor check
// borrows its stack scratch. Measuring allocations there would be measuring
// the instrumentation.
const raceEnabled = true
