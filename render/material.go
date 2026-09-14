package render

// Background is what a node fills its shape with.
//
// There are exactly two implementations and there is no route for a third from
// outside this package: a plain [Color], and a [Glass] material. The interface
// exists so that the modifier of the project plan, section 8,
//
//	.Background(ui.Glass().Quality(ui.Adaptive))
//
// can be spelled that way while
//
//	.Background(ui.RGB(18, 20, 26))
//
// keeps working. Go has no overloading, so the alternative was a second
// modifier name, and section 8 names this one.
//
// Boxing a Color into this interface allocates. That is a build time cost and
// the project plan, section 11, exempts build explicitly; nothing in the frame
// path ever holds a Background, because a node stores the resolved [Material]
// instead. See ui's styleSpec.
type Background interface {
	// background is unexported so that the set of implementations is closed.
	// A backend switches on [Material.Kind] and must be able to rely on
	// having seen every case.
	background()
}

func (Color) background() {}
func (Glass) background() {}

// MaterialKind discriminates the materials of a [Material].
//
// A material is a *backdrop dependent* background: something whose appearance
// is a function of what was drawn underneath it. A plain colour is not one,
// which is why [Color] never produces a Material.
type MaterialKind uint8

const (
	// MaterialNone is the zero value and draws nothing. It is what index 0 of
	// the material side table of a [List] holds.
	MaterialNone MaterialKind = iota
	// MaterialGlass is the experimental glass material of the project plan,
	// section 8. See [Glass].
	MaterialGlass
)

// String makes a failing test readable.
func (k MaterialKind) String() string {
	if k == MaterialGlass {
		return "glass"
	}
	return "none"
}

// GlassQuality selects how much work a glass material is allowed to cost.
//
// The project plan, section 8, fixes two levels and one policy. The levels are
// [Reduced] and [Full]; [Adaptive] is not a third level but a rule for
// choosing between the two, and it is deliberately *measurement* based rather
// than device based, because Ebitengine exposes no capability query at all —
// no GL version, no extension list, no memory figure. That was established in
// WU-D and WU-E and section 6 states it outright.
type GlassQuality uint8

const (
	// Adaptive lets the backend choose between [Reduced] and [Full] from a
	// sliding window of frame intervals and the material area on screen, with
	// hysteresis and a minimum dwell time per level. It is the default.
	//
	// A measurement that is meant to be compared against another measurement
	// must not use it. The project plan, section 13, makes pinning a level
	// mandatory for that, and the backend offers exactly that pin.
	Adaptive GlassQuality = iota
	// Reduced is one composite pass over an unblurred copy of the backdrop:
	// tint, a Fresnel-like edge brightening, a specular highlight and a small
	// normal based refraction offset. No blur, no downsample chain.
	Reduced
	// Full is the experimental level: a region copy, a dual-Kawase down and
	// up chain confined to the material region, and a composite pass with
	// tint, refraction, highlight and grain.
	//
	// The project plan, section 8, calls it experimental and makes no claim
	// of pixel parity with Apple's Liquid Glass. Neither does this
	// implementation. Section 13 explicitly allows it to miss its threshold
	// on a Raspberry Pi 4, in which case Reduced is the default and the
	// failure is documented rather than hidden.
	Full
)

// String makes a failing test and a diagnostic line readable.
func (q GlassQuality) String() string {
	switch q {
	case Reduced:
		return "reduced"
	case Full:
		return "full"
	default:
		return "adaptive"
	}
}

// GlassParams are the numbers a glass material is drawn from.
//
// They are plain data with no methods, because this is what travels in the
// material side table of a [List] and what a backend reads. The fluent
// construction surface is [Glass].
type GlassParams struct {
	// Tint is the colour laid over the backdrop, in premultiplied alpha. Its
	// alpha is how milky the glass is: zero is a clear pane and one is an
	// opaque plate that happens to cost a blur.
	Tint Color
	// Blur is the target blur radius in logical pixels, and it is used by
	// [Full] only. [Reduced] has no blur by construction.
	//
	// It is a radius and not a diameter, unlike [Shadow.Blur], because the
	// dual-Kawase chain is parameterised by how far it reaches and not by a
	// Gaussian it never evaluates. The backend turns it into a number of
	// down and up levels; see the backend's glass documentation.
	Blur float32
	// Refraction is how far, in logical pixels, the backdrop is displaced at
	// the very edge of the shape. It falls off to zero towards the middle,
	// which is what makes a pane look like it has thickness.
	Refraction float32
	// Highlight is the strength of the specular streak and of the Fresnel
	// edge brightening, 0 to 1.
	Highlight float32
	// Grain is the strength of the high frequency noise, 0 to 1. It is used
	// by [Full] only: on an unblurred backdrop it reads as dirt rather than
	// as frost.
	Grain float32
	// Level is the requested quality. [Adaptive] means the backend decides.
	Level GlassQuality
}

// Material is one entry of the material side table of a [List].
//
// It is POD, like [Op] itself, and lives in a side table for the reason stated
// on Op: an operation is a fixed size struct in a flat reused slice, and a
// material is twenty-eight bytes of parameters that only one operation kind in
// a hundred carries.
type Material struct {
	// Kind selects how Glass is interpreted. [MaterialNone] draws nothing.
	Kind MaterialKind
	// Glass are the parameters of a [MaterialGlass].
	Glass GlassParams
}

// IsVisible reports whether the material would draw anything.
func (m Material) IsVisible() bool { return m.Kind != MaterialNone }

// Glass is the experimental glass material of the project plan, section 8,
// under the fluent spelling that section uses:
//
//	.Background(ui.Glass().Quality(ui.Adaptive))
//
// The fields are unexported and the setters return a new value, unlike
// [Border] and [Shadow], which are struct literals. The reason is that a
// glass material has six parameters of which an application usually sets one,
// and a literal with five zero fields would not mean "the defaults" — it would
// mean an invisible, blur-free, tintless pane. [NewGlass] carries defaults
// that look like glass; a literal Glass{} does too, because the accessors
// substitute them.
type Glass struct {
	p      GlassParams
	filled bool
}

// Defaults of a [Glass], applied by [Glass.Params] when a setter did not run.
//
// They are a starting point that looks like frosted glass over a dark
// application, not a claim about anybody else's material.
const (
	DefaultGlassBlur       float32 = 16
	DefaultGlassRefraction float32 = 6
	DefaultGlassHighlight  float32 = 0.5
	DefaultGlassGrain      float32 = 0.06
)

// DefaultGlassTint is a barely-there cool white, alpha 0.12.
func DefaultGlassTint() Color { return RGBA(255, 255, 255, 30) }

// NewGlass returns a glass material with the default parameters and
// [Adaptive] quality. ui.Glass is the spelling an application uses.
func NewGlass() Glass {
	return Glass{p: GlassParams{
		Tint:       DefaultGlassTint(),
		Blur:       DefaultGlassBlur,
		Refraction: DefaultGlassRefraction,
		Highlight:  DefaultGlassHighlight,
		Grain:      DefaultGlassGrain,
	}, filled: true}
}

// Params returns the parameters, substituting the defaults for a zero value.
//
// This is what makes Glass{} and NewGlass() the same material, which in turn
// is what lets ui store a Glass in a struct field without a "was it set" flag
// next to it.
func (g Glass) Params() GlassParams {
	if !g.filled {
		d := NewGlass()
		d.p.Level = g.p.Level
		return d.p
	}
	return g.p
}

// Material returns the display list entry for this material.
func (g Glass) Material() Material {
	return Material{Kind: MaterialGlass, Glass: g.Params()}
}

// Quality requests a quality level. See [GlassQuality].
func (g Glass) Quality(q GlassQuality) Glass {
	p := g.Params()
	p.Level = q
	return Glass{p: p, filled: true}
}

// Tint sets the colour laid over the backdrop, in premultiplied alpha.
func (g Glass) Tint(c Color) Glass {
	p := g.Params()
	p.Tint = c
	return Glass{p: p, filled: true}
}

// Blur sets the blur radius in logical pixels. It affects [Full] only.
func (g Glass) Blur(v float32) Glass {
	p := g.Params()
	p.Blur = clampGlass(v, 0, 64)
	return Glass{p: p, filled: true}
}

// Refraction sets the edge displacement in logical pixels.
func (g Glass) Refraction(v float32) Glass {
	p := g.Params()
	p.Refraction = clampGlass(v, 0, 63)
	return Glass{p: p, filled: true}
}

// Highlight sets the strength of the specular streak and the Fresnel edge,
// 0 to 1.
func (g Glass) Highlight(v float32) Glass {
	p := g.Params()
	p.Highlight = clampGlass(v, 0, 1)
	return Glass{p: p, filled: true}
}

// Grain sets the strength of the noise, 0 to 1. It affects [Full] only.
func (g Glass) Grain(v float32) Glass {
	p := g.Params()
	p.Grain = clampGlass(v, 0, 1)
	return Glass{p: p, filled: true}
}

// clampGlass keeps a parameter finite and inside the range the backend can
// encode. The clamp is here rather than in the backend because the encoding is
// lossy — see the backend's packing of the style parameters into one vertex
// attribute — and a value that silently wrapped would be a bright streak in a
// corner with no explanation anywhere.
func clampGlass(v, lo, hi float32) float32 {
	if v != v {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
