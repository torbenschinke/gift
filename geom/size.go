package geom

// Size is a width and a height in view space.
//
// Sizes are conventionally non negative, but the type does not enforce that.
// Operations that can produce a negative extent clamp at zero and say so in
// their documentation.
type Size struct {
	W, H float32
}

// Sz returns the Size with width w and height h.
func Sz(w, h float32) Size {
	return Size{W: w, H: h}
}

// Add returns the component wise sum of s and t.
func (s Size) Add(t Size) Size {
	return Size{W: s.W + t.W, H: s.H + t.H}
}

// Sub returns the component wise difference s minus t. The result is not
// clamped; use Inset if a non negative result is required.
func (s Size) Sub(t Size) Size {
	return Size{W: s.W - t.W, H: s.H - t.H}
}

// Inset returns s shrunk by the insets i. Each component is clamped at zero, so
// the result never has a negative extent. An Unbounded component stays
// Unbounded.
func (s Size) Inset(i Insets) Size {
	return Size{
		W: maxf(0, s.W-i.Horizontal()),
		H: maxf(0, s.H-i.Vertical()),
	}
}

// Outset returns s grown by the insets i. An Unbounded component stays
// Unbounded.
func (s Size) Outset(i Insets) Size {
	return Size{
		W: s.W + i.Horizontal(),
		H: s.H + i.Vertical(),
	}
}

// IsZero reports whether both components are exactly zero.
func (s Size) IsZero() bool {
	return s.W == 0 && s.H == 0
}

// IsFinite reports whether both components are neither infinite nor NaN. A Size
// containing Unbounded is therefore not finite.
func (s Size) IsFinite() bool {
	return isFinite(s.W) && isFinite(s.H)
}
