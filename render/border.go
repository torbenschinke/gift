package render

// Border is a stroke drawn along the inside of the bounds of a node.
//
// It lives in render and not in ui because it is a renderer neutral style
// value, like [Color]: ui re-exports it under the spelling the project plan,
// section 8, uses. Keeping the value types on this side of the dependency
// arrow means a backend can reason about a border without importing ui.
//
// The stroke lies inside the bounds and does not change the layout; see the
// project plan, section 8. A Border with a Width of zero or a fully
// transparent Color draws nothing.
//
// Shadow is deliberately absent. It belongs to step 2 of the project plan,
// section 12, and a struct that exists but is ignored is worse than one that
// does not compile yet.
type Border struct {
	// Width is the stroke width in logical pixels.
	Width float32
	// Color is the stroke colour in premultiplied alpha.
	Color Color
}

// IsVisible reports whether the border would draw anything.
func (b Border) IsVisible() bool { return b.Width > 0 && !b.Color.IsTransparent() }
