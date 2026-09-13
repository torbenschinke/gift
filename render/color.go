package render

// Color is a colour in premultiplied alpha with components in the range
// [0, 1].
//
// Premultiplied means that R, G and B have already been scaled by A, so a
// half transparent pure red is {0.5, 0, 0, 0.5} and not {1, 0, 0, 0.5}.
// This is the convention Ebitengine expects for pixel uploads and for vertex
// colours, so the conversion happens once at construction time and never in
// the frame path; see the project plan, section 8.
//
// The zero Color is fully transparent.
type Color struct {
	// R, G and B are the premultiplied colour channels.
	R, G, B float32
	// A is the alpha channel, 0 fully transparent and 1 fully opaque.
	A float32
}

// RGBA returns the Color for the given straight alpha 8 bit components.
// The colour channels are premultiplied by the alpha channel, so callers pass
// the intuitive values: RGBA(255, 0, 0, 128) is a half transparent red.
func RGBA(r, g, b, a uint8) Color {
	af := float32(a) / 255
	return Color{
		R: float32(r) / 255 * af,
		G: float32(g) / 255 * af,
		B: float32(b) / 255 * af,
		A: af,
	}
}

// RGB returns the fully opaque Color for the given 8 bit components.
func RGB(r, g, b uint8) Color {
	return RGBA(r, g, b, 255)
}

// IsOpaque reports whether the colour is fully opaque. Opaque fills may be
// drawn without blending.
func (c Color) IsOpaque() bool { return c.A >= 1 }

// IsTransparent reports whether the colour is fully transparent. Fully
// transparent operations may be skipped entirely.
func (c Color) IsTransparent() bool { return c.A <= 0 }
