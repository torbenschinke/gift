package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var (
	vstackType = gift.RegisterType("ui.VStack")
	hstackType = gift.RegisterType("ui.HStack")
)

// Stack is a vertical or horizontal stack of children. It is created by
// [VStack] or [HStack]; the zero value is not useful.
//
// A vertical and a horizontal stack are different view types as far as
// reconciliation is concerned, so changing VStack into HStack under the same
// key remounts the subtree instead of silently turning the layout by ninety
// degrees while keeping the state below it.
type Stack struct {
	base
	axis     layout.Axis
	gap      float32
	children []gift.View
}

// VStack arranges its children from top to bottom.
//
// The children slice belongs to gift from this call onwards. VStack(a, b, c)
// allocates a fresh slice and is always safe; VStack(items...) hands over the
// caller's slice without copying it. See the project plan, section 4.
func VStack(children ...gift.View) Stack {
	return Stack{axis: layout.Vertical, children: children}
}

// HStack arranges its children from leading to trailing. The ownership rule
// of [VStack] applies unchanged.
func HStack(children ...gift.View) Stack {
	return Stack{axis: layout.Horizontal, children: children}
}

// ViewType implements gift.View.
func (s Stack) ViewType() gift.TypeID {
	if s.axis == layout.Horizontal {
		return hstackType
	}
	return vstackType
}

// Build implements gift.View.
func (s Stack) Build(*gift.BuildContext) gift.Element {
	return element(s.base, kindStack, s.gap, s.axis, s.children)
}

// Gap sets the space inserted between two adjacent children. It is never
// added before the first or after the last child.
func (s Stack) Gap(v float32) Stack { s.gap = v; return s }

// Padding sets the same padding on all four edges, replacing any previous
// padding.
func (s Stack) Padding(v float32) Stack { s.setPadding(v); return s }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (s Stack) PaddingInsets(v geom.Insets) Stack { s.setPaddingInsets(v); return s }

// Align sets the cross axis alignment of the children. A vertical stack reads
// the X component, a horizontal stack the Y component.
func (s Stack) Align(v geom.Alignment) Stack { s.setAlign(v); return s }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free.
//
// Precedence: Frame is applied first, the Min and Max modifiers afterwards,
// so Frame(200, 100).MaxWidth(50) is 50 wide. A clamp that a fixed size can
// escape would not be a clamp.
func (s Stack) Frame(w, h float32) Stack { s.setFrame(w, h); return s }

// MinWidth raises the minimum width of the node.
func (s Stack) MinWidth(v float32) Stack { s.setMinWidth(v); return s }

// MinHeight raises the minimum height of the node.
func (s Stack) MinHeight(v float32) Stack { s.setMinHeight(v); return s }

// MaxWidth lowers the maximum width of the node.
func (s Stack) MaxWidth(v float32) Stack { s.setMaxWidth(v); return s }

// MaxHeight lowers the maximum height of the node.
func (s Stack) MaxHeight(v float32) Stack { s.setMaxHeight(v); return s }

// Background fills the bounds behind the children. A fully transparent colour
// means no background at all, and a node without background, border and clip
// needs no painter.
func (s Stack) Background(v Color) Stack { s.setBackground(v); return s }

// Border strokes the inside of the bounds after the children were drawn. It
// does not change the layout.
func (s Stack) Border(v Border) Stack { s.setBorder(v); return s }

// CornerRadius rounds the background and the border.
func (s Stack) CornerRadius(v float32) Stack { s.setCornerRadius(v); return s }

// Clip confines the children to the padded bounds. The clip is rectangular
// even with a corner radius; see paintStyle.
func (s Stack) Clip(v bool) Stack { s.setClip(v); return s }

// Key sets the reconciliation key of this view among its siblings.
func (s Stack) Key(v string) Stack { s.setKey(v); return s }

// Flex makes the stack take a share of the remaining main axis space of its
// parent stack, proportional to v.
func (s Stack) Flex(v float32) Stack { s.setFlex(v); return s }
