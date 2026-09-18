package render

import "github.com/worldiety/gift/geom"

// Shadow is a blurred copy of the shape of a node, drawn behind it.
//
// It lives in render and not in ui for the same reason [Border] and [Color]
// do: it is a renderer neutral style value, and ui re-exports it under the
// spelling the project plan, section 8, uses. A backend can therefore reason
// about a shadow without importing ui.
//
// # Semantics
//
// The project plan, section 8, fixes them: a shadow extends the paint bounds
// but neither the layout size nor the hit area, and a parent clip applies to
// it like it applies to everything else. The drawing order of a node is
// shadow, background, content, border.
//
// # What Blur means
//
// Blur is the CSS spelling, not the standard deviation: the Gaussian that is
// convolved with the shape has a sigma of Blur/2, which is the convention
// every design tool and every CSS engine uses. A Blur of 16 therefore means
// sigma 8, and the visible falloff reaches about 24 pixels — see [Shadow.Sigma]
// and [Shadow.Extent].
//
// The zero Shadow draws nothing.
type Shadow struct {
	// Blur is the blur diameter in logical pixels; see the note above. Zero
	// is a hard edged shadow, which is a legitimate thing to ask for when
	// Spread or an offset is set.
	Blur float32
	// Spread inflates the shape before it is blurred. A negative Spread
	// shrinks it, which is how a shadow is tucked under its own node so that
	// only the offset side of it shows.
	Spread float32
	// OffsetX and OffsetY move the shadow relative to the node, positive
	// right and down.
	OffsetX, OffsetY float32
	// Color is the shadow colour in premultiplied alpha. It is the colour at
	// full coverage; the blur scales the whole premultiplied vector, so a
	// partially covered pixel stays premultiplied.
	Color Color
}

// IsVisible reports whether the shadow would draw anything.
//
// A shadow with a non finite parameter is not visible rather than an error:
// the alternative is an infinite quad and a window full of NaN, and the
// modifier that accepts the value already rejects the obvious mistakes at
// build time.
func (s Shadow) IsVisible() bool {
	if s.Color.IsTransparent() {
		return false
	}
	return shadowFinite(s.Blur) && shadowFinite(s.Spread) &&
		shadowFinite(s.OffsetX) && shadowFinite(s.OffsetY)
}

// Sigma is the standard deviation of the Gaussian, that is Blur/2, clamped at
// zero. A Sigma of zero means the shape is drawn with a hard edge.
func (s Shadow) Sigma() float32 {
	if !(s.Blur > 0) {
		return 0
	}
	return s.Blur * 0.5
}

// ShadowSigmas is how many standard deviations of the Gaussian are actually
// drawn.
//
// Three: the residual coverage beyond 3 sigma is 0.13 %, which is below one
// step of an eight bit alpha channel for any shadow less than about three
// quarters opaque, and the geometry is the thing being paid for in fill rate.
// A fourth sigma would grow the quad by a third for a difference nothing can
// display.
const ShadowSigmas = 3

// Extent is how far the drawn shadow reaches beyond its own shape, in logical
// pixels. It is [ShadowSigmas] times [Shadow.Sigma].
func (s Shadow) Extent() float32 { return ShadowSigmas * s.Sigma() }

// Shape returns the rectangle the shadow is the blurred image of: the bounds
// of the node, inflated by Spread and moved by the offset.
//
// This is the rectangle that goes into [Op.Bounds]; the blur is carried
// separately in [Op.Blur] so that a backend can grow the geometry itself. It
// is deliberately not the paint rectangle; see [Shadow.PaintBounds].
func (s Shadow) Shape(b geom.Rect) geom.Rect {
	return geom.Rc(
		b.Min.X-s.Spread+s.OffsetX, b.Min.Y-s.Spread+s.OffsetY,
		b.Max.X+s.Spread+s.OffsetX, b.Max.Y+s.Spread+s.OffsetY)
}

// Radius returns the corner radius of the shadow shape for a node whose own
// corner radius is r.
//
// Spread inflates the shape, so it inflates the corner with it; a negative
// Spread can drive it to zero but not below, because a rounded box with a
// negative radius is not a shape.
func (s Shadow) Radius(r float32) float32 {
	v := r + s.Spread
	if !(v > 0) {
		return 0
	}
	return v
}

// There is deliberately no Shadow.PaintBounds.
//
// There was one, with no caller outside its own test, beside an independent
// inline reimplementation of the same formula in [Op.PaintBounds] and a third
// in the Ebitengine backend's quad padding — the review-gate-4 pattern of a
// tidy exported formulation next to the code that actually runs. The "Shadow
// erweitert die Paint-Bounds" of the project plan, section 8, is expressed
// once, in [Op.PaintBounds], which is the form every consumer has: the paint
// bounds are a property of an emitted operation, whose Bounds is already the
// shape. [Shadow.Sigma] and [Shadow.Extent] are the shared arithmetic and both
// are now called from there and from the backend.

// shadowFinite is the local copy of the finiteness test. render has no shared
// one and a shadow is the only thing in the package that needs it.
func shadowFinite(v float32) bool {
	return v == v && v <= maxFiniteFloat32 && v >= -maxFiniteFloat32
}

const maxFiniteFloat32 = 3.4028235e38
