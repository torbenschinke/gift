package ui

import (
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/internal/layout"
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
// inflexible child with an unbounded main axis — rule 1 of the overflow model
// of the project plan, section 7 — so a Box in a VStack fills the width and
// collapses on the main axis: give it a height with [BoxView.Frame] or
// [BoxView.MinHeight], or a share of the leftover space with [BoxView.Flex].
//
// This paragraph was true of the documentation before it was true of the code.
// Until WU-C2 a stack bounded the main axis of its children by whatever was
// left, so a Box did not collapse at all: it ate the whole remaining extent
// and every sibling after it was measured against nothing. TestBoxGreedySizing
// covers all four containers, so the sentence is now checked rather than
// asserted.
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
	return element(b.base, kindBox, 0, layout.Vertical, layout.CrossAlignPosition, nil)
}

// Padding sets the same padding on all four edges, replacing any previous
// padding. Since a Box has no children, padding is simply a minimum size. It
// must be finite and non negative; see [Stack.Padding].
func (b BoxView) Padding(v float32) BoxView { b.setPadding(v); return b }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (b BoxView) PaddingInsets(v geom.Insets) BoxView { b.setPaddingInsets(v); return b }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free.
//
// Precedence: Frame is applied first, then Max, then Min, and the call order
// of the modifiers does not matter. Frame(200, 100).MaxWidth(50) is 50 wide
// and Frame(20, 20).MinWidth(80) is 80 wide; see [frameSpec].
func (b BoxView) Frame(w, h float32) BoxView { b.setFrame(w, h); return b }

// MinWidth raises the minimum width of the node. It also raises the maximum
// if that is lower: a minimum wins over a Frame and over a MaxWidth. See
// [frameSpec] for the full precedence rule.
func (b BoxView) MinWidth(v float32) BoxView { b.setMinWidth(v); return b }

// MinHeight raises the minimum height of the node, and the maximum with it if
// that is lower; see [frameSpec].
func (b BoxView) MinHeight(v float32) BoxView { b.setMinHeight(v); return b }

// MaxWidth lowers the maximum width of the node, and the minimum with it if
// that is higher. A MinWidth applied on top of it still wins; see [frameSpec].
func (b BoxView) MaxWidth(v float32) BoxView { b.setMaxWidth(v); return b }

// MaxHeight lowers the maximum height of the node, and the minimum with it if
// that is higher; see [frameSpec].
func (b BoxView) MaxHeight(v float32) BoxView { b.setMaxHeight(v); return b }

// Background fills the bounds. Without it, and without a border, a Box draws
// nothing and needs no painter.
func (b BoxView) Background(v Background) BoxView { b.setBackgroundSpec(v); return b }

// Border strokes the inside of the bounds.
func (b BoxView) Border(v Border) BoxView { b.setBorder(v); return b }

// Shadow draws a blurred copy of the background shape behind the view.
//
// It extends the paint bounds but not the layout size and not the hit area, so
// a shadow never moves a sibling and never makes a gap clickable; the project
// plan, section 8, fixes that. A parent clip cuts it.
func (b BoxView) Shadow(v Shadow) BoxView { b.setShadow(v); return b }

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
