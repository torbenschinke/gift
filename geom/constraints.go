package geom

// Constraints is the size range a parent imposes on a child during layout.
//
// Min is the smallest acceptable size, Max the largest. Either component of Max
// may be [Unbounded], which means the child may become arbitrarily large along
// that axis, for example inside a scrollable viewport. Min is expected to be
// finite and non negative.
//
// A Constraints value is normalised when Min is component wise less than or
// equal to Max and no component is negative; see IsNormalized. Layout code may
// assume normalised constraints, and the operations below preserve that property
// for normalised inputs.
type Constraints struct {
	Min, Max Size
}

// Tight returns Constraints that admit exactly the size s.
func Tight(s Size) Constraints {
	return Constraints{Min: s, Max: s}
}

// Loose returns Constraints with a zero minimum and s as maximum.
func Loose(s Size) Constraints {
	return Constraints{Min: Size{}, Max: s}
}

// Unconstrained returns Constraints with a zero minimum and an Unbounded maximum
// on both axes.
func Unconstrained() Constraints {
	return Constraints{Min: Size{}, Max: Size{W: Unbounded, H: Unbounded}}
}

// Constrain returns s clamped into the range described by c.
//
// An Unbounded maximum leaves the corresponding component untouched rather than
// producing a NaN, because the clamp is done with ordinary comparisons.
func (c Constraints) Constrain(s Size) Size {
	return Size{
		W: clamp(s.W, c.Min.W, c.Max.W),
		H: clamp(s.H, c.Min.H, c.Max.H),
	}
}

// ConstrainWidth returns w clamped into the horizontal range of c.
func (c Constraints) ConstrainWidth(w float32) float32 {
	return clamp(w, c.Min.W, c.Max.W)
}

// ConstrainHeight returns h clamped into the vertical range of c.
func (c Constraints) ConstrainHeight(h float32) float32 {
	return clamp(h, c.Min.H, c.Max.H)
}

// Loosen returns c with its minimum dropped to zero and its maximum unchanged.
func (c Constraints) Loosen() Constraints {
	return Constraints{Min: Size{}, Max: c.Max}
}

// Tighten returns Constraints that admit exactly c.Constrain(s). This is the
// usual way to turn a measured size back into a tight contract for the arrange
// pass.
func (c Constraints) Tighten(s Size) Constraints {
	t := c.Constrain(s)
	return Constraints{Min: t, Max: t}
}

// Deflate returns c reduced by the insets i, as required when a padding modifier
// forwards constraints to its child.
//
// Both Min and Max lose the inset extent and are clamped at zero, so the result
// never contains a negative size. An Unbounded maximum stays Unbounded, because
// infinity minus a finite inset is still infinity.
func (c Constraints) Deflate(i Insets) Constraints {
	return Constraints{
		Min: c.Min.Inset(i),
		Max: c.Max.Inset(i),
	}
}

// HasBoundedWidth reports whether the horizontal maximum is finite.
func (c Constraints) HasBoundedWidth() bool {
	return isFinite(c.Max.W)
}

// HasBoundedHeight reports whether the vertical maximum is finite.
func (c Constraints) HasBoundedHeight() bool {
	return isFinite(c.Max.H)
}

// IsSatisfiedBy reports whether s lies within the range described by c,
// inclusive on both ends.
func (c Constraints) IsSatisfiedBy(s Size) bool {
	return s.W >= c.Min.W && s.W <= c.Max.W &&
		s.H >= c.Min.H && s.H <= c.Max.H
}

// IsNormalized reports whether c is well formed: no component is negative or
// NaN, Min is finite, and Min is component wise less than or equal to Max.
func (c Constraints) IsNormalized() bool {
	return c.Min.W >= 0 && c.Min.H >= 0 &&
		c.Max.W >= 0 && c.Max.H >= 0 &&
		c.Min.IsFinite() &&
		c.Min.W <= c.Max.W && c.Min.H <= c.Max.H
}
