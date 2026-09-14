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
//
// Every child is measured with the same loose but bounded constraints, so a
// greedy child such as an unframed [Box] fills the whole box on both axes.
// A child's Flex is ignored; see [Overlay.Flex] for why.
func ZStack(children ...gift.View) Overlay {
	return Overlay{children: children}
}

// ViewType implements gift.View.
func (o Overlay) ViewType() gift.TypeID { return zstackType }

// Build implements gift.View.
func (o Overlay) Build(*gift.BuildContext) gift.Element {
	return element(o.base, kindOverlay, 0, layout.Vertical, layout.CrossAlignPosition, o.children)
}

// Padding sets the same padding on all four edges, replacing any previous
// padding. It must be finite and non negative; see [Stack.Padding].
func (o Overlay) Padding(v float32) Overlay { o.setPadding(v); return o }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (o Overlay) PaddingInsets(v geom.Insets) Overlay { o.setPaddingInsets(v); return o }

// Align sets how the children are placed inside the box. Unlike in a [Stack],
// both components are used, because neither axis is a stacking axis.
func (o Overlay) Align(v geom.Alignment) Overlay { o.setAlign(v); return o }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free.
//
// Precedence: Frame is applied first, then Max, then Min, and the call order
// of the modifiers does not matter. Frame(200, 100).MaxWidth(50) is 50 wide
// and Frame(20, 20).MinWidth(80) is 80 wide; see [frameSpec].
func (o Overlay) Frame(w, h float32) Overlay { o.setFrame(w, h); return o }

// MinWidth raises the minimum width of the node. It also raises the maximum
// if that is lower: a minimum wins over a Frame and over a MaxWidth. See
// [frameSpec] for the full precedence rule.
func (o Overlay) MinWidth(v float32) Overlay { o.setMinWidth(v); return o }

// MinHeight raises the minimum height of the node, and the maximum with it if
// that is lower; see [frameSpec].
func (o Overlay) MinHeight(v float32) Overlay { o.setMinHeight(v); return o }

// MaxWidth lowers the maximum width of the node, and the minimum with it if
// that is higher. A MinWidth applied on top of it still wins; see [frameSpec].
func (o Overlay) MaxWidth(v float32) Overlay { o.setMaxWidth(v); return o }

// MaxHeight lowers the maximum height of the node, and the minimum with it if
// that is higher; see [frameSpec].
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
//
// # A ZStack does not honour the Flex of its own children
//
// This is the one documented exception to the rule in the package
// documentation that a view never accepts a modifier it then ignores. Flex on
// this type is honoured — by the parent stack, which is whose space is being
// divided. Flex on a child of this ZStack is not: a Z stack has no main axis,
// so there is no remainder and no direction to divide it along, and the
// question "how do I make this child fill the box" already has an answer that
// does not need Flex. Every child is measured with the full, bounded
// constraints of the box, so a greedy child such as an unframed [Box] fills it
// on both axes.
//
// The alternative would have been to make Flex mean "fill" inside a ZStack,
// which is a second, incompatible meaning for the same word and would then
// have to answer what Flex(2) is supposed to be. It is documented instead.
func (o Overlay) Flex(v float32) Overlay { o.setFlex(v); return o }
