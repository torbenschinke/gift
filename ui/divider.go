package ui

import (
	"fmt"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

var dividerType = gift.RegisterType("ui.Divider")

// DividerThickness is the height of a hairline, in logical pixels.
//
// One logical pixel, not one *device* pixel. The layout of this project is
// declared in logical units at every density — the project plan, section 18,
// is explicit that the density changes the pixels and never the program — so a
// divider is one unit tall whether it ends up two device pixels wide on a 2x
// panel or one on the Raspberry Pi. What the density does change is where the
// band is allowed to start; see [DividerView].
const DividerThickness = float32(1)

// DividerView is a horizontal hairline. It is created by [Divider]; the zero
// value is not useful.
//
// # Why this is a node of its own and not a one pixel Box
//
// Because a hairline is the one shape in a user interface where half a pixel
// of rounding is visible, and `ui.Box().Frame(geom.Unbounded(), 1)` has no way
// to avoid it. A box emits its bounds, the backend multiplies them by the
// density transform, and a band whose device top lands on 41.5 is rasterised
// across two rows of pixels at half coverage each. The result is a grey line
// where a black one was asked for, and — this is the part that makes it worth
// a type — it is grey *for some rows of a list and not others*, because the
// fractional part depends on the sum of the heights above it and on the
// current scroll offset. A list then has hairlines of two different weights on
// the screen at once.
//
// So this view snaps. In its painter, where the device rectangle is actually
// known, the band is moved to the nearest whole device pixel and given a whole
// number of device pixels of thickness, at least one. The *layout* is
// untouched: the node is [DividerView.Thickness] logical pixels tall at every
// density, it occupies exactly that much room in the stack above it, and no
// sibling moves. Only the painted band is quantised, by at most half a device
// pixel.
//
// The snap survives a scroll transform, because it is computed from
// [gift.PaintContext.DeviceBounds] rather than from the local bounds: a list
// resting at a fling-produced offset of 10.25 logical pixels on a 2x panel has
// a device top of x.5, and that is precisely the case a local-space rounding
// would get wrong.
//
// # It fills the width it is offered
//
// Like a [BoxView] it is greedy on a bounded axis and collapses on an
// unbounded one, so a Divider in a [VStack] spans the stack and a Divider in
// an [HStack] — which measures its children with an unbounded main axis — has
// no width at all. This view is horizontal; there is no vertical rule, because
// nothing in this package has asked for one.
type DividerView struct {
	base
	thickness float32
	color     Color
	hasColor  bool
	lead      float32
	trail     float32
}

// Divider returns a hairline of [DividerThickness] in [ColorSeparator].
func Divider() DividerView { return DividerView{thickness: DividerThickness} }

// ViewType implements gift.View.
func (v DividerView) ViewType() gift.TypeID { return dividerType }

// Build implements gift.View.
func (v DividerView) Build(*gift.BuildContext) gift.Element {
	c := ColorSeparator
	if v.hasColor {
		c = v.color
	}
	n := &dividerNode{
		thickness: v.thickness,
		color:     ResolveColor(c),
		lead:      v.lead,
		trail:     v.trail,
	}
	return gift.Element{Key: v.key, Flex: v.flex, Layouter: n, Painter: n}
}

// Thickness sets the height of the line in logical pixels. It must be finite
// and positive.
func (v DividerView) Thickness(f float32) DividerView {
	if !isFinite(f) || f <= 0 {
		panic(fmt.Sprintf("gift/ui: Divider.Thickness(%v) must be a finite, positive number", f))
	}
	v.thickness = f
	return v
}

// Color sets the colour of the line, replacing [ColorSeparator]. It may be a
// semantic colour and is resolved during build.
func (v DividerView) Color(c Color) DividerView { v.color, v.hasColor = c, true; return v }

// Inset shortens the line at its leading and trailing ends, in logical pixels.
//
// This is what makes a separator line up with the text of a row rather than
// with the edge of the panel, which is the convention every grouped list on
// every platform follows. Both must be finite and non negative; an inset
// larger than the width draws nothing rather than a line running backwards.
func (v DividerView) Inset(leading, trailing float32) DividerView {
	v.lead = checkPadding("Divider.Inset leading", leading)
	v.trail = checkPadding("Divider.Inset trailing", trailing)
	return v
}

// Key sets the reconciliation key of this view among its siblings.
func (v DividerView) Key(s string) DividerView { v.setKey(s); return v }

// Flex makes the divider take a share of the remaining main axis space of its
// parent stack. A hairline in a vertical stack wants none; this exists so that
// a Divider can be the flexible filler of a horizontal one, where it has no
// height of its own to speak of.
func (v DividerView) Flex(f float32) DividerView { v.setFlex(f); return v }

// dividerNode is the retained half of a [DividerView].
//
// It carries the density it was laid out at, because the painter needs it and
// a [gift.PaintContext] deliberately does not offer one — a painter is not
// supposed to make decisions from the display, and this one does not either:
// it uses the factor only to convert its own snap back into local units.
// Layout always precedes paint for a node that is drawn, and a density change
// invalidates everything (see [gift.App.SetDensity]), so the value is never
// stale.
type dividerNode struct {
	thickness   float32
	color       Color
	lead, trail float32
	density     float32
}

// Layout implements gift.Layouter. The height is the logical thickness at
// every density; see [DividerThickness].
func (n *dividerNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.density = ctx.Density()
	w := c.Min.W
	if c.HasBoundedWidth() {
		w = c.Max.W
	}
	return c.Constrain(geom.Sz(w, n.thickness))
}

// Paint implements gift.Painter.
//
// There is deliberately no transparency gate here, which is why this function
// is absent from the inventory of TestTheGateInventoryIsComplete. A divider
// has exactly one colour and emitting a fully transparent fill costs one
// operation the backend discards; a gate would buy that operation back and
// would put this painter into the class of functions where an unresolved
// colour turns into an invisible widget instead of a diagnosis. The
// assertion is still here, because "resolved" is a contract of this package
// and not a consequence of the gate.
//
// The argument is quantitative and it does not generalise. [listNode.Paint]
// used to cite this one and emits a fill *per separator*, which is 999 of them
// in a thousand row list on every frame; it has a gate of its own and says so.
// One divider is one operation, and that is the whole of the premise here.
func (n *dividerNode) Paint(ctx *gift.PaintContext) {
	assertResolved(n.color, "the colour of a Divider")
	b := ctx.Bounds()
	x0, x1 := b.Min.X+n.lead, b.Max.X-n.trail
	if !(x1 > x0) {
		return
	}
	top, th := snapHairline(b.Min.Y, n.thickness, b.Min.Y, ctx.DeviceBounds().Min.Y, n.density)
	ctx.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(x0, top, x1, top+th), Color: n.color})
}

// snapHairline maps a horizontal band onto whole device pixels and returns it
// in the local space it came from.
//
// top and thickness are in the local space of the node being painted; nodeTop
// is that node's own local top edge and nodeDeviceTop the same edge after the
// active transform, which is the density scale composed with whatever scroll
// translations are above it. Both transforms are uniform scales plus a
// translation, so one factor — the density — converts a device length back
// into a local one, and the offset cancels.
//
// The resulting band has whole number device edges and is at least one device
// pixel thick. A thickness that rounds to zero would be the one outcome worse
// than a blurry hairline: no hairline. That is not hypothetical at a
// [DividerView.Thickness] below half a device pixel, which is a legal thing to
// ask for on a 1x panel.
func snapHairline(top, thickness, nodeTop, nodeDeviceTop, d float32) (float32, float32) {
	if !(d > 0) || !isFinite(d) {
		return top, thickness
	}
	devTop := nodeDeviceTop + (top-nodeTop)*d
	sTop := roundf(devTop)
	sThick := roundf(thickness * d)
	if sThick < 1 {
		sThick = 1
	}
	return top + (sTop-devTop)/d, sThick / d
}
