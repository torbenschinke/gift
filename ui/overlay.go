package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var zstackType = gift.RegisterType("ui.ZStack")

// Overlay stacks its children on top of each other in Z order, sizes itself
// to the largest one and aligns every child inside that box. It is created by
// [ZStack].
type Overlay struct {
	base
	children []gift.View
}

// ZStack draws its children on top of each other, first child at the bottom.
// The ownership rule of [VStack] applies unchanged.
func ZStack(children ...gift.View) Overlay {
	return Overlay{children: children}
}

// ViewType implements gift.View.
func (o Overlay) ViewType() gift.TypeID { return zstackType }

// Build implements gift.View.
func (o Overlay) Build(*gift.BuildContext) gift.Element {
	return element(o.base, kindOverlay, 0, layout.Vertical, o.children)
}

// Padding sets the same padding on all four edges, replacing any previous
// padding.
func (o Overlay) Padding(v float32) Overlay { o.setPadding(v); return o }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (o Overlay) PaddingInsets(v geom.Insets) Overlay { o.setPaddingInsets(v); return o }

// Align sets how the children are placed inside the box. Unlike in a [Stack],
// both components are used, because neither axis is a stacking axis.
func (o Overlay) Align(v geom.Alignment) Overlay { o.setAlign(v); return o }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free.
//
// Precedence: Frame is applied first, the Min and Max modifiers afterwards,
// so Frame(200, 100).MaxWidth(50) is 50 wide. A clamp that a fixed size can
// escape would not be a clamp.
func (o Overlay) Frame(w, h float32) Overlay { o.setFrame(w, h); return o }

// MinWidth raises the minimum width of the node.
func (o Overlay) MinWidth(v float32) Overlay { o.setMinWidth(v); return o }

// MinHeight raises the minimum height of the node.
func (o Overlay) MinHeight(v float32) Overlay { o.setMinHeight(v); return o }

// MaxWidth lowers the maximum width of the node.
func (o Overlay) MaxWidth(v float32) Overlay { o.setMaxWidth(v); return o }

// MaxHeight lowers the maximum height of the node.
func (o Overlay) MaxHeight(v float32) Overlay { o.setMaxHeight(v); return o }

// Background fills the bounds behind the children.
func (o Overlay) Background(v Color) Overlay { o.setBackground(v); return o }

// Border strokes the inside of the bounds after the children were drawn.
func (o Overlay) Border(v Border) Overlay { o.setBorder(v); return o }

// CornerRadius rounds the background and the border.
func (o Overlay) CornerRadius(v float32) Overlay { o.setCornerRadius(v); return o }

// Clip confines the children to the padded bounds.
func (o Overlay) Clip(v bool) Overlay { o.setClip(v); return o }

// Key sets the reconciliation key of this view among its siblings.
func (o Overlay) Key(v string) Overlay { o.setKey(v); return o }

// Flex makes the overlay take a share of the remaining main axis space of its
// parent stack, proportional to v.
func (o Overlay) Flex(v float32) Overlay { o.setFlex(v); return o }
