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
	cross    layout.CrossAlign
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
	return element(s.base, kindStack, s.gap, s.axis, s.cross, s.children)
}

// Gap sets the space inserted between two adjacent children. It is never
// added before the first or after the last child, so n children have n-1 gaps.
//
// A negative gap is legal and overlaps adjacent children by that much; the
// gaps are counted into the content extent either way, so an overlapping stack
// is smaller, not larger. A gap that is not a finite number panics: it would
// produce infinite child origins and NaN vertex positions far away from the
// call that caused it.
func (s Stack) Gap(v float32) Stack { s.gap = checkGap(v); return s }

// Padding sets the same padding on all four edges, replacing any previous
// padding. It must be finite and non negative; anything else panics, see
// checkPadding.
func (s Stack) Padding(v float32) Stack { s.setPadding(v); return s }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (s Stack) PaddingInsets(v geom.Insets) Stack { s.setPaddingInsets(v); return s }

// Align sets the cross axis alignment of the children. A vertical stack reads
// the X component, a horizontal stack the Y component.
//
// It has no effect on a child that takes part in baseline alignment; see
// [Stack.AlignBaseline].
func (s Stack) Align(v geom.Alignment) Stack {
	s.setAlign(v)
	s.cross = layout.CrossAlignPosition
	return s
}

// AlignBaseline lines the children up on their first text baseline instead of
// on an edge or the centre. It replaces any previous [Stack.Align].
//
// Two labels of different font sizes placed next to each other sit on a common
// line, which is what "next to each other" means for text and what centring
// only approximates. A child that reports no baseline — a [BoxView], an
// [Overlay], anything that is not text — is not guessed at: it keeps the
// ordinary alignment inside the same band. Guessing a baseline for a rectangle
// would misalign every row it appeared in, and there is no value that would be
// right.
//
// # Only on a horizontal stack
//
// A baseline is a horizontal line, so aligning on it positions a child
// vertically. In a vertical stack the vertical axis is the stacking axis and
// is already decided, so there is nothing left for a baseline to say. Calling
// this on a [VStack] panics at the call site rather than silently doing
// nothing: the package documentation promises that a view never accepts a
// modifier it then ignores, and this is the cheapest place to keep that
// promise — the axis is known at construction time.
func (s Stack) AlignBaseline() Stack {
	if s.axis != layout.Horizontal {
		panic("gift/ui: AlignBaseline on a VStack; a baseline positions a child vertically, " +
			"which in a vertical stack is what the stacking itself already decides. Use it on an HStack.")
	}
	s.cross = layout.CrossAlignBaseline
	return s
}

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free.
//
// Precedence: Frame is applied first, then Max, then Min, and the call order
// of the modifiers does not matter. Frame(200, 100).MaxWidth(50) is 50 wide
// and Frame(20, 20).MinWidth(80) is 80 wide; see [frameSpec].
func (s Stack) Frame(w, h float32) Stack { s.setFrame(w, h); return s }

// MinWidth raises the minimum width of the node. It also raises the maximum
// if that is lower: a minimum wins over a Frame and over a MaxWidth. See
// [frameSpec] for the full precedence rule.
func (s Stack) MinWidth(v float32) Stack { s.setMinWidth(v); return s }

// MinHeight raises the minimum height of the node, and the maximum with it if
// that is lower; see [frameSpec].
func (s Stack) MinHeight(v float32) Stack { s.setMinHeight(v); return s }

// MaxWidth lowers the maximum width of the node, and the minimum with it if
// that is higher. A MinWidth applied on top of it still wins; see [frameSpec].
func (s Stack) MaxWidth(v float32) Stack { s.setMaxWidth(v); return s }

// MaxHeight lowers the maximum height of the node, and the minimum with it if
// that is higher; see [frameSpec].
func (s Stack) MaxHeight(v float32) Stack { s.setMaxHeight(v); return s }

// Background fills the bounds behind the children. A fully transparent colour
// means no background at all, and a node without background, border and clip
// needs no painter.
func (s Stack) Background(v Color) Stack { s.setBackground(v); return s }

// Border strokes the inside of the bounds after the children were drawn. It
// does not change the layout.
func (s Stack) Border(v Border) Stack { s.setBorder(v); return s }

// Shadow draws a blurred copy of the background shape behind the view.
//
// It extends the paint bounds but not the layout size and not the hit area, so
// a shadow never moves a sibling and never makes a gap clickable; the project
// plan, section 8, fixes that. A parent clip cuts it.
func (s Stack) Shadow(v Shadow) Stack { s.setShadow(v); return s }

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
