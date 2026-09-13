package geom

import "math"

// unbounded is the backing value of [Unbounded]. Go cannot express an infinite
// floating point constant, so the sentinel has to live in a variable. The
// variable is unexported so that nothing outside this package can assign to it:
// a single stray write would break every constraint calculation in the process.
var unbounded = float32(math.Inf(1))

// Unbounded returns the sentinel used for a constraint maximum that imposes no
// upper limit. It is positive infinity.
//
// Arithmetic on Unbounded is well defined for every operation in this package:
// subtracting a finite inset from Unbounded stays Unbounded, and clamping a
// finite value against Unbounded returns the value unchanged.
//
// This is a function rather than a variable because the sentinel is the anchor
// of the whole constraints system and must be immutable. The body is a single
// load and is inlined; the call costs nothing.
func Unbounded() float32 { return unbounded }

// clamp returns v limited to the inclusive range [lo, hi].
//
// It is written with plain comparisons rather than math.Min and math.Max so that
// it stays in float32, stays inlinable, and yields v unchanged when hi is
// Unbounded instead of producing a NaN.
func clamp(v, lo, hi float32) float32 {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// minf returns the smaller of a and b.
func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// maxf returns the larger of a and b.
func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// isFinite reports whether v is neither infinite nor NaN.
func isFinite(v float32) bool {
	return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v))
}
