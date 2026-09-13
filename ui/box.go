package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var boxType = gift.RegisterType("ui.Box")

// BoxView is a styled rectangle without children. It is the base primitive of
// gift: everything that is drawn but not text or an image is a Box.
//
// # Size
//
// A Box has no content and therefore no intrinsic size, so it is greedy: it
// takes the whole extent on every axis that is bounded, and collapses to its
// padding on an axis that is not.
//
// That makes the obvious spelling work. A ZStack hands its children loose but
// bounded constraints, so
//
//	ui.ZStack(ui.Box().Background(c), content)
//
// paints the plate behind the content. A stack, however, measures an
// inflexible child with an unbounded main axis, so a Box in a VStack still
// collapses on the main axis: give it a height with [BoxView.Frame] or
// [BoxView.MinHeight], or a share of the leftover space with [BoxView.Flex].
type BoxView struct {
	base
}

// Box returns an unstyled, zero sized rectangle.
func Box() BoxView { return BoxView{} }

// ViewType implements gift.View.
func (b BoxView) ViewType() gift.TypeID { return boxType }

// Build implements gift.View.
//
// A Box has its own tiny layout path rather than reusing the overlay
// algorithm, because its sizing rule differs: it is greedy on bounded axes.
func (b BoxView) Build(*gift.BuildContext) gift.Element {
	return element(b.base, kindBox, 0, layout.Vertical, nil)
}

// Padding sets the same padding on all four edges, replacing any previous
// padding. Since a Box has no children, padding is simply a minimum size.
func (b BoxView) Padding(v float32) BoxView { b.setPadding(v); return b }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (b BoxView) PaddingInsets(v geom.Insets) BoxView { b.setPaddingInsets(v); return b }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free.
//
// Precedence: Frame is applied first, the Min and Max modifiers afterwards,
// so Frame(200, 100).MaxWidth(50) is 50 wide. A clamp that a fixed size can
// escape would not be a clamp.
func (b BoxView) Frame(w, h float32) BoxView { b.setFrame(w, h); return b }

// MinWidth raises the minimum width of the node.
func (b BoxView) MinWidth(v float32) BoxView { b.setMinWidth(v); return b }

// MinHeight raises the minimum height of the node.
func (b BoxView) MinHeight(v float32) BoxView { b.setMinHeight(v); return b }

// MaxWidth lowers the maximum width of the node.
func (b BoxView) MaxWidth(v float32) BoxView { b.setMaxWidth(v); return b }

// MaxHeight lowers the maximum height of the node.
func (b BoxView) MaxHeight(v float32) BoxView { b.setMaxHeight(v); return b }

// Background fills the bounds. Without it, and without a border, a Box draws
// nothing and needs no painter.
func (b BoxView) Background(v Color) BoxView { b.setBackground(v); return b }

// Border strokes the inside of the bounds.
func (b BoxView) Border(v Border) BoxView { b.setBorder(v); return b }

// CornerRadius rounds the background and the border.
func (b BoxView) CornerRadius(v float32) BoxView { b.setCornerRadius(v); return b }

// Clip pushes a clip around the padded bounds. A Box has no children, so this
// only matters once it is used as a container in a later step; it is kept for
// symmetry with the other views.
func (b BoxView) Clip(v bool) BoxView { b.setClip(v); return b }

// Key sets the reconciliation key of this view among its siblings.
func (b BoxView) Key(v string) BoxView { b.setKey(v); return b }

// Flex makes the box take a share of the remaining main axis space of its
// parent stack, proportional to v.
func (b BoxView) Flex(v float32) BoxView { b.setFlex(v); return b }
