package ui

import (
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

// RGBA returns the Color for the given straight alpha 8 bit components. The
// colour channels are premultiplied on construction, never in the frame path.
func RGBA(r, g, b, a uint8) Color { return render.RGBA(r, g, b, a) }

// RGB returns the fully opaque Color for the given 8 bit components.
func RGB(r, g, b uint8) Color { return render.RGB(r, g, b) }

// styleSpec is the fixed set of named style fields of a node. The project
// plan, section 8, defines the drawing order over exactly these names.
type styleSpec struct {
	background Color
	border     Border
	radius     float32
	clip       bool
}

// needsPainter reports whether the node has anything to draw or clip.
//
// When it does not, the view supplies a nil gift.Painter, which makes gift
// paint the children directly and skips the whole painter machinery for the
// node. Allocating a painter that only forwards to its children would cost an
// interface call and a heap object per structural container.
func (s styleSpec) needsPainter() bool {
	return !s.background.IsTransparent() || s.border.IsVisible() || s.clip
}

// frameSpec is the size contract a node imposes on itself.
//
// # Precedence
//
// Frame is applied first and makes the axis tight. Min and Max are applied
// afterwards and therefore win: Frame(200, 100).MaxWidth(50) is 50 wide, not
// 200. That order is the useful one — Max is a clamp, and a clamp that a
// fixed size can escape is not a clamp — and it is the one thing about this
// combination worth remembering.
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
	if f.hasMaxW && f.maxW < out.Max.W {
		out.Max.W = f.maxW
	}
	if f.hasMaxH && f.maxH < out.Max.H {
		out.Max.H = f.maxH
	}
	if f.hasMinW && f.minW > out.Min.W {
		out.Min.W = f.minW
	}
	if f.hasMinH && f.minH > out.Min.H {
		out.Min.H = f.minH
	}
	// Keep the result normalised: a minimum above the maximum would make
	// geom.Constrain return the minimum and silently break the clamp.
	if out.Min.W > out.Max.W {
		out.Min.W = out.Max.W
	}
	if out.Min.H > out.Max.H {
		out.Min.H = out.Max.H
	}
	return out
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

func (b *base) setKey(v string)                { b.key = v }
func (b *base) setFlex(v float32)              { b.flex = v }
func (b *base) setPadding(v float32)           { b.pad = geom.InsetsAll(v) }
func (b *base) setPaddingInsets(v geom.Insets) { b.pad = v }
func (b *base) setAlign(v geom.Alignment)      { b.align = v }
func (b *base) setBackground(v Color)          { b.style.background = v }
func (b *base) setBorder(v Border)             { b.style.border = v }
func (b *base) setCornerRadius(v float32)      { b.style.radius = v }
func (b *base) setClip(v bool)                 { b.style.clip = v }

// setFrame makes both axes tight. An axis given as [geom.Unbounded] is left
// free, which is how a caller fixes one axis only.
func (b *base) setFrame(w, h float32) {
	b.frame.hasW, b.frame.w = isFinite(w), w
	b.frame.hasH, b.frame.h = isFinite(h), h
}

func (b *base) setMinWidth(v float32)  { b.frame.hasMinW, b.frame.minW = true, v }
func (b *base) setMinHeight(v float32) { b.frame.hasMinH, b.frame.minH = true, v }
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
// section 8. It is shared by every styled view type.
func paintStyle(ctx *gift.PaintContext, st styleSpec, pad geom.Insets) {
	b := ctx.Bounds()

	// Step 2 inserts the shadow pass here, before the background. The order
	// is shadow, background, content, border and is a property of the node,
	// not of the order in which the modifiers were called.

	if !st.background.IsTransparent() {
		op := render.Op{Kind: render.OpFillRect, Bounds: b, Color: st.background}
		if st.radius > 0 {
			op.Kind = render.OpFillRoundRect
			op.CornerRadius = st.radius
		}
		ctx.Add(op)
	}

	if st.clip {
		// Clip to the full bounds, not to the padded bounds. Padding is a
		// layout property everywhere else, and clipping it away would cut
		// off content that deliberately overflows into the padding, such as
		// a focus ring, a selection glow or a badge. A separate content clip
		// can be added later if it is ever actually wanted.
		//
		// Honest limitation: this clips to the bounding rectangle, not to
		// the rounded shape. A shape accurate clip needs stencil or shader
		// support and is a backend concern; until the backend offers it, a
		// clipped child may cover the inside of a rounded corner.
		ctx.PushClip(b)
	}
	ctx.PaintChildren()
	if st.clip {
		ctx.PopClip()
	}

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
