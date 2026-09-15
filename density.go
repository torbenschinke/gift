package gift

import (
	"math"

	"github.com/torbenschinke/gift/geom"
)

// MaxDensity is the largest device density gift accepts.
//
// Four is two steps beyond the 2x displays this feature exists for and one
// beyond anything a desktop currently reports. It is a sanity bound and not a
// promise: the cost of a density is quadratic in pixels and in glyph atlas
// area, so a factor arriving from a misconfigured compositor should be
// refused at the boundary rather than turn into a framebuffer nobody can
// afford.
const MaxDensity = 4

// RoundDensity turns a raw device scale factor into the integer density gift
// works in. It is the *one* place the rounding of the project plan,
// section 18, happens.
//
// # Why there is a rounding at all
//
// The project plan, section 14, excludes fractional DPI scaling and
// section 18 repeats the exclusion: integer factors are promised, a
// fractional one is rounded and the result is documented rather than dressed
// up as accuracy gift does not have. A 1.5 factor therefore becomes 2, and
// what the user sees is a frame rendered at twice the logical size and then
// resampled by Ebitengine to three halves of it. That is supersampling: it is
// slightly soft, it costs more fragments than 1.5 would, and it is
// unambiguously sharper than the 1x frame Ebitengine used to upscale. A
// factor of 1.4 becomes 1 and the output is what it has always been.
//
// # Why it is a single function and not a conversion at each use
//
// Because a density that is 2 in the glyph atlas, 1.5 in the image ladder and
// 2 in the root transform is three different pictures of one display. Every
// consumer — [App.SetDensity], the Ebitengine backend, gifttest — calls this
// and nothing else rounds.
//
// A non-finite or non-positive factor is 1, and anything above [MaxDensity]
// is clamped to it.
func RoundDensity(factor float64) float32 {
	if math.IsNaN(factor) || factor <= 1 {
		return 1
	}
	if factor >= MaxDensity {
		return MaxDensity
	}
	return float32(math.Round(factor))
}

// SetDensity sets the device density, that is how many physical pixels one
// logical pixel of the layout covers along each axis.
//
// factor is the raw value of the platform — on Ebitengine
// ebiten.Monitor().DeviceScaleFactor() — and is rounded by [RoundDensity]
// here, so that a caller never has to know the rounding rule. It reports the
// density that is now in force.
//
// # What changes and what does not
//
// Nothing about layout. The application keeps declaring sizes, paddings and
// font sizes in logical pixels, a [Layouter] keeps measuring in them, and
// [App.Update] keeps being handed the viewport in them. What changes is the
// *presentation*: [App.Paint] pushes a scale of the density at the root of
// the display list, so every operation reaches the backend with a transform
// that maps its local rectangle onto physical pixels. The backend already
// bakes radius, stroke width, the antialiasing pad, the shadow sigma and the
// glass refraction out of that transform, so shapes become sharp rather than
// larger.
//
// Input and hit testing are deliberately *not* scaled. Pointer positions and
// [App.HitTest] stay in logical pixels, which is the space the bounds a test
// or an application reads are in. The backend divides the physical cursor
// position by the density once, at the boundary, and everything above it —
// drag slop, fling velocity, scroll offsets — keeps one unit system. The
// alternative, carrying the density into the input path as well, would have
// made a drag of a hundred physical pixels scroll twice as far on a 2x
// display than on a 1x one.
//
// A density of 1 is the identity in every one of these places: the root
// transform is not pushed at all, so a 1x frame produces exactly the display
// list it produced before densities existed.
func (a *App) SetDensity(factor float64) float32 {
	a.assertUIGoroutine("SetDensity")
	d := RoundDensity(factor)
	if d == a.density {
		return d
	}
	a.density = d
	// A density change resizes every glyph mask and every thumbnail rung, and
	// both are chosen in layout. Nothing about the *logical* layout changed,
	// so this is not a resize; it is the same layout asked to pick different
	// resources, and a full pass is the honest way to get one.
	a.Invalidate()
	return d
}

// Density returns the device density in force, which is always an integer of
// at least one. See [App.SetDensity].
func (a *App) Density() float32 { return a.density }

// densityXform is the root transform of the display list, or the identity at
// density 1.
func (a *App) densityXform() geom.Affine2D {
	if a.density == 1 {
		return geom.Identity()
	}
	return geom.Scale(a.density, a.density)
}
