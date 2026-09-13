package geom

// Alignment places a child inside a larger rectangle.
//
// X and Y are relative factors in the range [0, 1]: 0 means leading, that is
// left or top, 0.5 means centred and 1 means trailing, that is right or bottom.
// Values outside that range are not rejected and simply extrapolate.
//
// The naming uses leading and trailing rather than left and right so that it
// stays meaningful if writing direction support is added later. In the current
// LTR only MVP leading is left.
type Alignment struct {
	X, Y float32
}

// The nine standard alignments.
//
// They must be treated as read only; they are variables only because Go has no
// composite constants.
var (
	// TopLeading aligns to the top left corner.
	TopLeading = Alignment{X: 0, Y: 0}
	// Top aligns to the top edge, horizontally centred.
	Top = Alignment{X: 0.5, Y: 0}
	// TopTrailing aligns to the top right corner.
	TopTrailing = Alignment{X: 1, Y: 0}
	// Leading aligns to the left edge, vertically centred.
	Leading = Alignment{X: 0, Y: 0.5}
	// Center aligns to the centre on both axes.
	Center = Alignment{X: 0.5, Y: 0.5}
	// Trailing aligns to the right edge, vertically centred.
	Trailing = Alignment{X: 1, Y: 0.5}
	// BottomLeading aligns to the bottom left corner.
	BottomLeading = Alignment{X: 0, Y: 1}
	// Bottom aligns to the bottom edge, horizontally centred.
	Bottom = Alignment{X: 0.5, Y: 1}
	// BottomTrailing aligns to the bottom right corner.
	BottomTrailing = Alignment{X: 1, Y: 1}
)

// Position returns the rectangle of size child placed inside within according to
// a.
//
// The child keeps its size; only its origin is chosen. If the child is larger
// than within, the free space is negative and the child overflows symmetrically
// to how it would be inset, which is the expected behaviour for an unclipped
// overflowing child.
func (a Alignment) Position(child Size, within Rect) Rect {
	freeW := within.Width() - child.W
	freeH := within.Height() - child.H
	origin := Point{
		X: within.Min.X + freeW*a.X,
		Y: within.Min.Y + freeH*a.Y,
	}
	return RcSize(origin, child)
}
