package ui

import "github.com/worldiety/gift/render"

// Background is what a view fills its shape with: a [Color] or a material.
// It is [render.Background]; see there for why this is an interface.
type Background = render.Background

// GlassMaterial is the experimental glass material of the project plan,
// section 8. It is [render.Glass], re-exported here the way [Border] and
// [Shadow] are.
type GlassMaterial = render.Glass

// GlassQuality selects how much work a glass material may cost. The values are
// [Adaptive], [Reduced] and [Full].
type GlassQuality = render.GlassQuality

// The quality levels of the project plan, section 8.
//
// [Adaptive] is the default and is not a third level: it is a measurement
// based rule for choosing between the other two, because Ebitengine exposes no
// capability query. A measurement that is to be compared with another must pin
// a level; the project plan, section 13, requires it.
const (
	Adaptive = render.Adaptive
	Reduced  = render.Reduced
	Full     = render.Full
)

// Glass returns the experimental glass material with the default parameters,
// in the spelling of the project plan, section 8:
//
//	ui.VStack(
//	    ui.Text("Library").FontSize(24),
//	    ui.Button(ui.Text("Import"), importPhotos),
//	).
//	    Gap(12).
//	    Padding(20).
//	    Background(ui.Glass().Quality(ui.Adaptive)).
//	    Border(ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 90)}).
//	    CornerRadius(18).
//	    Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)})
//
// # What it is and is not
//
// It is a backdrop dependent background. The backdrop is everything that was
// drawn before the node and nothing else — not the node's own content, not its
// children, not a later sibling — and it is clipped to the node's shape, that
// is to its bounds and [CornerRadius]. Scrolling underneath it makes it dirty,
// every frame, so a glass frame that never moves is still re-blurred whenever
// what is behind it moves. There is no reusable cache and section 8 says so.
//
// It is not a reimplementation of anybody else's material. The project plan,
// section 8, states plainly that there is no claim to pixel parity with
// Apple's Liquid Glass and that the [Full] level is experimental, and neither
// this function nor the shader behind it claims otherwise.
//
// # Cost
//
// [Reduced] is one region copy and one composite pass over the material
// region. [Full] adds a dual-Kawase down and up chain, also confined to the
// region. Neither is free and neither touches the whole screen; see the
// backend's glass documentation for the measured numbers and for the one place
// where the implementation had to depart from section 8.
func Glass() GlassMaterial { return render.NewGlass() }
