package geom

// Insets is a per edge padding or margin in view space.
//
// The fields are ordered Top, Right, Bottom, Left, following the clockwise CSS
// convention. Values are conventionally non negative; negative values are
// allowed and simply grow instead of shrink.
type Insets struct {
	Top, Right, Bottom, Left float32
}

// InsetsAll returns Insets with the same value v on all four edges.
func InsetsAll(v float32) Insets {
	return Insets{Top: v, Right: v, Bottom: v, Left: v}
}

// InsetsSymmetric returns Insets with vertical applied to the top and bottom
// edge and horizontal applied to the left and right edge.
func InsetsSymmetric(vertical, horizontal float32) Insets {
	return Insets{Top: vertical, Right: horizontal, Bottom: vertical, Left: horizontal}
}

// Horizontal returns Left plus Right, the total width consumed by i.
func (i Insets) Horizontal() float32 {
	return i.Left + i.Right
}

// Vertical returns Top plus Bottom, the total height consumed by i.
func (i Insets) Vertical() float32 {
	return i.Top + i.Bottom
}

// Add returns the component wise sum of i and j.
func (i Insets) Add(j Insets) Insets {
	return Insets{
		Top:    i.Top + j.Top,
		Right:  i.Right + j.Right,
		Bottom: i.Bottom + j.Bottom,
		Left:   i.Left + j.Left,
	}
}

// IsZero reports whether all four edges are exactly zero.
func (i Insets) IsZero() bool {
	return i.Top == 0 && i.Right == 0 && i.Bottom == 0 && i.Left == 0
}
