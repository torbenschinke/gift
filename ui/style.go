package ui

import (
	"fmt"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// Color is the colour type of gift. It is [render.Color]: colours are a
// renderer neutral value type and live below ui in the dependency order, so
// that a backend can reason about them without importing this package.
type Color = render.Color

// Border is a stroke along the inside of the bounds of a view. It is
// [render.Border], re-exported under the spelling of the project plan,
// section 8.
type Border = render.Border

// Shadow is a blurred copy of the shape of a view, drawn behind it. It is
// [render.Shadow], re-exported under the spelling of the project plan,
// section 8:
//
//	.Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)})
//
// It extends the paint bounds and neither the layout size nor the hit area,
// and a parent clip cuts it like it cuts anything else.
type Shadow = render.Shadow

// RGBA returns the Color for the given straight alpha 8 bit components. The
// colour channels are premultiplied on construction, never in the frame path.
func RGBA(r, g, b, a uint8) Color { return render.RGBA(r, g, b, a) }

// RGB returns the fully opaque Color for the given 8 bit components.
func RGB(r, g, b uint8) Color { return render.RGB(r, g, b) }

// styleSpec is the fixed set of named style fields of a node. The project
// plan, section 8, defines the drawing order over exactly these names.
type styleSpec struct {
	background Color
	// material is the resolved backdrop dependent background, or the zero
	// value when the background is a plain colour.
	//
	// The two are mutually exclusive and the last [base.setBackgroundSpec]
	// wins, which is the "Wiederholtes Setzen ersetzt den jeweiligen
	// Style-Wert" of the project plan, section 8, applied to a field that now
	// has two possible types. Keeping them in one field pair rather than
	// letting both be set at once matters: a colour under a glass pane would
	// be a second, invisible way of tinting it, and it would make the
	// backdrop of the glass contain the node's own background.
	material render.Material
	border   Border
	shadow   Shadow
	radius   float32
	clip     bool
}

// needsPainter reports whether the node has anything to draw or clip.
//
// When it does not, the view supplies a nil gift.Painter, which makes gift
// paint the children directly and skips the whole painter machinery for the
// node. Allocating a painter that only forwards to its children would cost an
// interface call and a heap object per structural container.
func (s styleSpec) needsPainter() bool {
	return !s.background.IsTransparent() || s.material.IsVisible() ||
		s.border.IsVisible() || s.shadow.IsVisible() || s.clip
}

// frameSpec is the size contract a node imposes on itself.
//
// # Precedence
//
// The modifiers are fields, not wrappers, so the order in which they are
// called is irrelevant. What matters is the order in which they are applied,
// and that order is fixed:
//
//  1. Frame makes the axis tight.
//  2. MaxWidth and MaxHeight are applied and lower both ends of the range.
//  3. MinWidth and MinHeight are applied and raise both ends of the range.
//
// Two consequences, both deliberate:
//
//   - Max beats Frame. Frame(200, 100).MaxWidth(50) is 50 wide. A clamp that a
//     fixed size can escape is not a clamp.
//   - Min beats everything, including Max. Frame(20, 20).MinWidth(80) is 80
//     wide and MinWidth(80).MaxWidth(40) is 80 wide. This is the rule CSS and
//     Flutter both use, and it is the only one under which a minimum cannot be
//     silently lost: a minimum usually exists because the content below it
//     cannot be rendered any smaller, and clamping it away produces an
//     unreadable node instead of an honest overflow.
//
// The previous implementation contradicted its own documentation here. It
// raised the minimum and then normalised it straight back down to the maximum,
// so Frame(20, 20).MinWidth(80) came out 20 wide and the minimum vanished
// without a trace. Each step below therefore moves both ends of the range, so
// the result is normalised by construction rather than by a repair pass.
type frameSpec struct {
	w, h       float32
	hasW, hasH bool

	minW, minH       float32
	maxW, maxH       float32
	hasMaxW, hasMaxH bool
	hasMinW, hasMinH bool
}

// apply returns the constraints the node passes to its layouter.
func (f frameSpec) apply(c geom.Constraints) geom.Constraints {
	out := c
	if f.hasW {
		out.Min.W, out.Max.W = f.w, f.w
	}
	if f.hasH {
		out.Min.H, out.Max.H = f.h, f.h
	}
	if f.hasMaxW {
		out.Min.W, out.Max.W = lowerTo(out.Min.W, out.Max.W, f.maxW)
	}
	if f.hasMaxH {
		out.Min.H, out.Max.H = lowerTo(out.Min.H, out.Max.H, f.maxH)
	}
	if f.hasMinW {
		out.Min.W, out.Max.W = raiseTo(out.Min.W, out.Max.W, f.minW)
	}
	if f.hasMinH {
		out.Min.H, out.Max.H = raiseTo(out.Min.H, out.Max.H, f.minH)
	}
	return out
}

// lowerTo applies a maximum to a range. It pulls the upper end down to v and
// the lower end with it, so the range stays normalised without a repair pass
// that would silently undo a minimum.
func lowerTo(lo, hi, v float32) (float32, float32) {
	if v < hi {
		hi = v
	}
	if v < lo {
		lo = v
	}
	return lo, hi
}

// raiseTo applies a minimum to a range, pushing the upper end up with it. This
// is where Min beats Max; see the precedence section of [frameSpec].
func raiseTo(lo, hi, v float32) (float32, float32) {
	if v > lo {
		lo = v
	}
	if v > hi {
		hi = v
	}
	return lo, hi
}

// base holds the modifier fields every styled view shares.
//
// It is embedded, and the concrete view types declare one line forwarders that
// return their own type. The public methods have to be per type because
// variant A requires modifiers to return concrete types; the bodies must not
// be, which is what this struct and its setters are for.
type base struct {
	key   string
	flex  float32
	pad   geom.Insets
	align geom.Alignment
	frame frameSpec
	style styleSpec
}

func (b *base) setKey(v string)           { b.key = v }
func (b *base) setFlex(v float32)         { b.flex = checkFlex(v) }
func (b *base) setAlign(v geom.Alignment) { b.align = v }

func (b *base) setPadding(v float32) { b.pad = geom.InsetsAll(checkPadding("Padding", v)) }

func (b *base) setPaddingInsets(v geom.Insets) {
	checkPadding("PaddingInsets.Top", v.Top)
	checkPadding("PaddingInsets.Right", v.Right)
	checkPadding("PaddingInsets.Bottom", v.Bottom)
	checkPadding("PaddingInsets.Left", v.Left)
	b.pad = v
}

// checkGap rejects a gap that is not a finite number.
//
// A negative gap is legal and documented: it overlaps adjacent children, which
// is occasionally what a design calls for and which the stack algorithm
// handles without a special case. A non finite one is not legal in any sense —
// [geom.Unbounded] as a gap produces infinite child origins, infinite bounds
// and NaN vertex positions, and the failure surfaces as an empty window three
// layers below the mistake. Rejecting it at the call site costs one comparison
// during build, which is outside the frame path.
func checkGap(v float32) float32 {
	if !isFinite(v) {
		panic(fmt.Sprintf(
			"gift/ui: Gap(%v) is not a finite number; a negative gap is allowed and overlaps children, "+
				"but an infinite or NaN gap produces infinite origins and NaN vertex positions", v))
	}
	return v
}

// checkPadding rejects padding that is negative or not finite.
//
// Negative padding is rejected rather than defined, and the reason is that it
// has no single sensible meaning here: the constraints handed to the children
// are deflated by the padding and clamped at zero, while the resulting size is
// outset by it, so a negative inset would enlarge the children's room on one
// side of the arithmetic and shrink the container on the other. A modifier
// whose two halves disagree is worse than one that is absent; a caller who
// wants overlap has a negative Gap, and a caller who wants a larger container
// has Frame or MinWidth.
func checkPadding(what string, v float32) float32 {
	if !isFinite(v) || v < 0 {
		panic(fmt.Sprintf(
			"gift/ui: %s(%v) must be a finite, non negative number; "+
				"for deliberately overlapping children use a negative Gap instead", what, v))
	}
	return v
}

// checkShadow rejects a shadow whose numbers are not finite, and a negative
// blur.
//
// The same argument as [checkGap]: an infinite blur produces an infinite quad,
// a NaN one produces NaN vertex positions, and both surface as an empty window
// several layers below the mistake. [render.Shadow.IsVisible] would already
// refuse to draw such a shadow, so this check buys a diagnosis rather than
// correctness — but it buys it at the call site, during build, which is
// outside the frame path.
//
// A negative Spread is legal and useful: it tucks the shadow under its own
// node so that only the offset side shows. A negative Blur is not, because
// there is no shape it could mean.
func checkShadow(v Shadow) Shadow {
	switch {
	case !isFinite(v.Blur) || v.Blur < 0:
		panic(fmt.Sprintf("gift/ui: Shadow.Blur(%v) must be a finite, non negative number", v.Blur))
	case !isFinite(v.Spread):
		panic(fmt.Sprintf("gift/ui: Shadow.Spread(%v) is not a finite number; "+
			"a negative spread is allowed and tucks the shadow under its node", v.Spread))
	case !isFinite(v.OffsetX) || !isFinite(v.OffsetY):
		panic(fmt.Sprintf("gift/ui: Shadow offset (%v, %v) is not finite", v.OffsetX, v.OffsetY))
	}
	return v
}

// checkFlex rejects a flex that is not a finite number. A negative or zero
// flex means inflexible, which is the documented default and not an error.
func checkFlex(v float32) float32 {
	if !isFinite(v) {
		panic(fmt.Sprintf("gift/ui: Flex(%v) is not a finite number", v))
	}
	return v
}
func (b *base) setBackground(v Color) { b.style.background, b.style.material = v, render.Material{} }

// setBackgroundSpec accepts either of the two things a background can be.
//
// The type switch is here, once, rather than in eight view types. A nil
// Background clears the background, which is what ".Background(nil)" ought to
// mean and is better than a panic for a value the compiler cannot reject.
func (b *base) setBackgroundSpec(v Background) {
	switch t := v.(type) {
	case nil:
		b.style.background, b.style.material = Color{}, render.Material{}
	case Color:
		b.setBackground(t)
	case GlassMaterial:
		b.style.background, b.style.material = Color{}, t.Material()
	default:
		// Unreachable: render.Background has an unexported method and
		// exactly two implementations. The panic is here so that adding a
		// third one without coming back here is loud rather than silent.
		panic(fmt.Sprintf("gift/ui: Background of unknown type %T", v))
	}
}

func (b *base) setBorder(v Border)        { b.style.border = v }
func (b *base) setShadow(v Shadow)        { b.style.shadow = checkShadow(v) }
func (b *base) setCornerRadius(v float32) { b.style.radius = v }
func (b *base) setClip(v bool)            { b.style.clip = v }

// setFrame makes both axes tight. An axis given as [geom.Unbounded] is left
// free, which is how a caller fixes one axis only.
func (b *base) setFrame(w, h float32) {
	b.frame.hasW, b.frame.w = isFinite(w), w
	b.frame.hasH, b.frame.h = isFinite(h), h
}

// setMinWidth and setMinHeight ignore a non finite value the same way the Max
// setters do. An infinite minimum would make every size infinite and every
// derived origin a NaN, which is not a layout, it is a corrupted frame.
func (b *base) setMinWidth(v float32)  { b.frame.hasMinW, b.frame.minW = isFinite(v), v }
func (b *base) setMinHeight(v float32) { b.frame.hasMinH, b.frame.minH = isFinite(v), v }
func (b *base) setMaxWidth(v float32)  { b.frame.hasMaxW, b.frame.maxW = isFinite(v), v }
func (b *base) setMaxHeight(v float32) { b.frame.hasMaxH, b.frame.maxH = isFinite(v), v }

func isFinite(v float32) bool {
	// Unbounded is positive infinity and is the documented way to say "no
	// constraint on this axis". NaN is treated the same way rather than
	// propagated into the layout arithmetic.
	return v == v && v <= maxFinite && v >= -maxFinite
}

const maxFinite = 3.4028235e38

// paintStyle draws one node in the fixed order of the project plan,
// section 8. It is shared by every styled view type whose content is its
// children.
//
// The clip is not here. [gift.Element.Clip] is applied by gift itself on the
// way into the subtree — see [gift.PaintContext.PaintChildren] — so that the
// paint clip and the input clip come from one declaration read in one place
// and a widget cannot honour one and forget the other. ui.Button forgot it for
// a whole work unit.
//
// A view whose content is not its children — [TextView] — cannot use this,
// because the content step is different. It calls [paintBackground] and
// [paintBorder] around its own content instead, which is why the two halves
// are separate functions: passing a content callback in would put a closure in
// the paint path, and a closure is an allocation per painted node.
func paintStyle(ctx *gift.PaintContext, st styleSpec) {
	b := ctx.Bounds()
	paintBackground(ctx, st, b)
	ctx.PaintChildren()
	paintBorder(ctx, st, b)
}

// paintBackground emits the shadow and background steps of the drawing order.
//
// The shadow comes first, always, and that is a property of the node and not
// of the order in which the modifiers were called: the full order is shadow,
// background, content, border, and the project plan, section 8, fixes it.
//
// The emitted shadow operation carries the *shape* — the bounds inflated by
// the spread and moved by the offset — and the blur separately, because the
// backend has to grow its geometry by the falloff and would otherwise have to
// reverse that arithmetic. See [render.OpShadow].
func paintBackground(ctx *gift.PaintContext, st styleSpec, b geom.Rect) {
	if sh := st.shadow; sh.IsVisible() {
		ctx.Add(render.Op{
			Kind:         render.OpShadow,
			Bounds:       sh.Shape(b),
			Color:        sh.Color,
			CornerRadius: sh.Radius(st.radius),
			Blur:         sh.Blur,
		})
	}

	if !st.background.IsTransparent() {
		op := render.Op{Kind: render.OpFillRect, Bounds: b, Color: st.background}
		if st.radius > 0 {
			op.Kind = render.OpFillRoundRect
			op.CornerRadius = st.radius
		}
		ctx.Add(op)
	}

	// The material step, and it is deliberately the *same* step as the plain
	// background rather than one after it: the two are mutually exclusive in
	// [styleSpec], so at most one of these two blocks runs.
	//
	// It has to be here, before PaintChildren, and not merely by convention.
	// A material reads whatever is already in the display list — see
	// [render.OpMaterial] — so emitting it after the content would hand the
	// glass a backdrop containing the node's own children, and nothing
	// downstream could tell that from the intended picture. The drawing order
	// of the project plan, section 8, is the correctness condition here and
	// not just a look.
	if st.material.IsVisible() {
		ctx.Add(render.Op{
			Kind:         render.OpMaterial,
			Bounds:       b,
			CornerRadius: st.radius,
			Material:     ctx.AddMaterial(st.material),
		})
	}
}

// paintBorder emits the border step of the drawing order.
//
// A clip, where one is set, surrounds the content and not the border: see
// paintStyle. It clips to the full bounds, not to the padded bounds. Padding
// is a layout property everywhere else, and clipping it away would cut off
// content that deliberately overflows into the padding, such as a focus ring,
// a selection glow or a badge.
//
// Honest limitation: the clip is the bounding rectangle, not the rounded
// shape. A shape accurate clip needs stencil or shader support and is a
// backend concern; until the backend offers it, a clipped child may cover the
// inside of a rounded corner.
func paintBorder(ctx *gift.PaintContext, st styleSpec, b geom.Rect) {
	if st.border.IsVisible() {
		ctx.Add(render.Op{
			Kind:         render.OpStrokeRoundRect,
			Bounds:       b,
			Color:        st.border.Color,
			CornerRadius: st.radius,
			StrokeWidth:  st.border.Width,
		})
	}
}
