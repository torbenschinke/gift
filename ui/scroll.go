package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var (
	vscrollType = gift.RegisterType("ui.VScroll")
	hscrollType = gift.RegisterType("ui.HScroll")
)

// ScrollView is a viewport that clips its children and lets the user move them
// along one axis. It is created by [VScroll] or [HScroll]; the zero value is
// not useful.
//
// # What it is
//
// A scroll view is a stack — the same [VStack] and [HStack] algorithm, with
// the same Gap, Padding and Align — inside a window that is smaller than it.
// The children are measured with the scroll axis unbounded, so a tall column
// stays tall, and the container reports the size its own constraints permit.
// The difference between the two is what there is to scroll.
//
// # What scrolling costs
//
// Nothing but a transform. The offset is presentation state in the retained
// node, exactly like hover and press — the project plan, section 5, requires
// that — so moving it rebuilds nothing and measures nothing: gift translates
// the children on the way into the subtree, for paint and for hit testing
// alike, from one declaration. [gift.Diagnostics.Scrolls] moves while
// [gift.Diagnostics.Builds] and [gift.Diagnostics.Layouts] stand still, and
// that is asserted rather than asserted about.
//
// # It needs a bounded extent
//
// A scroll view has to be told how big its window is, and a stack measures an
// inflexible child with an *unbounded* main axis — rule 1 of the overflow
// model of the project plan, section 7. So a bare VScroll inside a VStack has
// no viewport at all: it is as tall as its content and scrolling does nothing.
// Give it a [ScrollView.Flex] so it takes the leftover space, or a
// [ScrollView.Frame] or [ScrollView.MaxHeight]. The symptom is that scrolling
// does nothing at all; [gift.ScrollInfo] then reports a MaxOffset of zero and
// a ViewportExtent equal to the ContentExtent, which is the pair to look at.
//
// There is deliberately no giftdebug diagnosis for it. "The viewport is as
// large as the content" is also exactly what a scroller with three rows in it
// looks like, and a warning that fires on every correct short list is a
// warning people learn to ignore.
//
// # Input
//
// gift supplies the gesture handling: the wheel over any descendant, a drag
// past [gift.DragSlop] — which takes the press away from a button underneath —
// and a kinetic fling on release. The container is a hit target over its whole
// bounds, so a click on its empty background does not fall through to whatever
// is behind it, which is what every platform scroller does.
type ScrollView struct {
	base
	axis     layout.Axis
	cross    layout.CrossAlign
	gap      float32
	cfg      gift.ScrollConfig
	children []gift.View
}

// VScroll arranges its children from top to bottom and scrolls vertically.
//
// The children slice belongs to gift from this call onwards; the ownership
// rule of [VStack] applies unchanged.
func VScroll(children ...gift.View) ScrollView {
	return ScrollView{axis: layout.Vertical, children: children}
}

// HScroll arranges its children from leading to trailing and scrolls
// horizontally. The ownership rule of [VStack] applies unchanged.
func HScroll(children ...gift.View) ScrollView {
	return ScrollView{axis: layout.Horizontal, children: children}
}

// ViewType implements gift.View.
//
// A vertical and a horizontal scroller are different view types, for the same
// reason [VStack] and [HStack] are: turning one into the other under the same
// key would keep a scroll offset that was measured along the other axis.
func (s ScrollView) ViewType() gift.TypeID {
	if s.axis == layout.Horizontal {
		return hscrollType
	}
	return vscrollType
}

// Build implements gift.View.
func (s ScrollView) Build(*gift.BuildContext) gift.Element {
	n := &node{
		kind: kindScroll,
		spec: layout.StackSpec{
			Axis:       s.axis,
			Gap:        s.gap,
			Padding:    s.pad,
			Alignment:  s.align,
			CrossAlign: s.cross,
		},
		fr: s.frame,
		st: s.style,
	}
	var p gift.Painter
	if s.style.needsPainter() {
		p = n
	}
	axis := gift.ScrollVertical
	if s.axis == layout.Horizontal {
		axis = gift.ScrollHorizontal
	}
	return gift.Element{
		Key:      s.key,
		Flex:     s.flex,
		Layouter: n,
		Painter:  p,
		Children: s.children,
		// Always. A viewport that did not clip would paint its whole content
		// over its neighbours and would be indistinguishable from a stack;
		// see [ScrollView.Clip].
		Clip:   true,
		Scroll: &gift.ScrollSpec{Axis: axis, Config: s.cfg},
	}
}

// --- scrolling modifiers -----------------------------------------------------

// Friction sets the exponential decay rate of a fling in reciprocal seconds,
// replacing [gift.DefaultScrollFriction] for this container.
//
// The velocity of a fling follows v(t) = v0 * exp(-Friction*t), so the total
// distance of an undamped fling is v0/Friction: a flick at 2000 px/s with the
// default of 4 travels 500 logical pixels. A larger value stops sooner. Zero
// or a non finite value selects the default.
func (s ScrollView) Friction(v float32) ScrollView { s.cfg.Friction = v; return s }

// WheelStep sets how far one unit of wheel delta scrolls, in logical pixels,
// replacing [gift.DefaultScrollWheelStep].
func (s ScrollView) WheelStep(v float32) ScrollView { s.cfg.WheelStep = v; return s }

// FlingVelocity sets the release speed, in logical pixels per second, below
// which a drag ends in a stop rather than a fling.
func (s ScrollView) FlingVelocity(v float32) ScrollView { s.cfg.FlingVelocity = v; return s }

// Config replaces the whole gesture configuration in one call. A zero field
// takes the corresponding package default; see [gift.ScrollConfig].
func (s ScrollView) Config(v gift.ScrollConfig) ScrollView { s.cfg = v; return s }

// --- stack modifiers ---------------------------------------------------------

// Gap sets the space inserted between two adjacent children, exactly as
// [Stack.Gap].
func (s ScrollView) Gap(v float32) ScrollView { s.gap = checkGap(v); return s }

// Align sets the cross axis alignment of the children, exactly as
// [Stack.Align].
func (s ScrollView) Align(v geom.Alignment) ScrollView {
	s.setAlign(v)
	s.cross = layout.CrossAlignPosition
	return s
}

// --- the shared modifier set -------------------------------------------------

// Padding sets the same padding on all four edges, replacing any previous
// padding.
//
// It is inside the viewport: the padding scrolls with the content, so the
// leading padding is visible at offset zero and gone once the user has
// scrolled past it. That is what the stack algorithm below does with it and
// the honest description of a padded scroller.
func (s ScrollView) Padding(v float32) ScrollView { s.setPadding(v); return s }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (s ScrollView) PaddingInsets(v geom.Insets) ScrollView { s.setPaddingInsets(v); return s }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free — but not for the scroll axis, which then has no viewport; see
// [ScrollView].
//
// Precedence: Frame is applied first, then Max, then Min; see [frameSpec].
func (s ScrollView) Frame(w, h float32) ScrollView { s.setFrame(w, h); return s }

// MinWidth raises the minimum width of the viewport, and the maximum with it
// if that is lower; see [frameSpec].
func (s ScrollView) MinWidth(v float32) ScrollView { s.setMinWidth(v); return s }

// MinHeight raises the minimum height of the viewport, and the maximum with it
// if that is lower; see [frameSpec].
func (s ScrollView) MinHeight(v float32) ScrollView { s.setMinHeight(v); return s }

// MaxWidth lowers the maximum width of the viewport, and the minimum with it
// if that is higher; see [frameSpec]. On an [HScroll] it is the usual way to
// give the viewport a bounded extent.
func (s ScrollView) MaxWidth(v float32) ScrollView { s.setMaxWidth(v); return s }

// MaxHeight lowers the maximum height of the viewport, and the minimum with it
// if that is higher; see [frameSpec]. On a [VScroll] it is the usual way to
// give the viewport a bounded extent.
func (s ScrollView) MaxHeight(v float32) ScrollView { s.setMaxHeight(v); return s }

// Background fills the viewport behind the content. It does not scroll: it is
// drawn by the container, which does not move.
func (s ScrollView) Background(v Background) ScrollView { s.setBackgroundSpec(v); return s }

// Border strokes the inside of the viewport bounds after the content was
// drawn, so the content scrolls underneath it.
func (s ScrollView) Border(v Border) ScrollView { s.setBorder(v); return s }

// Shadow draws a blurred copy of the viewport's box behind it. It extends the
// paint bounds but not the layout size and not the hit area.
func (s ScrollView) Shadow(v Shadow) ScrollView { s.setShadow(v); return s }

// CornerRadius rounds the background and the border. The content clip stays
// rectangular; see paintBorder for the honest limitation.
func (s ScrollView) CornerRadius(v float32) ScrollView { s.setCornerRadius(v); return s }

// Clip is accepted only as Clip(true), which is already what a scroll view
// does; Clip(false) panics.
//
// The modifier exists because every view with bounds carries the shared set —
// the reflection test in this package insists on it — and because a caller who
// writes Clip(false) has a real misunderstanding that is better answered at
// the call site than by silence. A viewport that did not clip would paint its
// entire content over its neighbours, its hit area would extend over them too,
// and it would be a [VStack] with extra steps. The package documentation
// promises that a view never accepts a modifier it then ignores; this is how
// that promise is kept here, the same way [Stack.AlignBaseline] keeps it.
func (s ScrollView) Clip(v bool) ScrollView {
	if !v {
		panic("gift/ui: Clip(false) on a ScrollView; a viewport that does not clip would paint " +
			"its whole content over its neighbours and would simply be a stack. Use VStack or HStack instead.")
	}
	return s
}

// Key sets the reconciliation key of this view among its siblings.
func (s ScrollView) Key(v string) ScrollView { s.setKey(v); return s }

// Flex makes the viewport take a share of the remaining main axis space of its
// parent stack, proportional to v. It is the usual way to give a scroll view a
// bounded extent; see [ScrollView].
func (s ScrollView) Flex(v float32) ScrollView { s.setFlex(v); return s }
