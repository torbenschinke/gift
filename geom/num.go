package geom

import "math"

// Unbounded is the sentinel used for a constraint maximum that imposes no upper
// limit. It is positive infinity.
//
// Arithmetic on Unbounded is well defined for every operation in this package:
// subtracting a finite inset from Unbounded stays Unbounded, and clamping a
// finite value against Unbounded returns the value unchanged.
//
// Note that Go has no way to express an infinite floating point constant, so
// this is a package level variable rather than a constant. It must be treated as
// read only; assigning to it breaks every constraint calculation in the process.
var Unbounded = float32(math.Inf(1))

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
