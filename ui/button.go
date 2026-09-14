package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var buttonType = gift.RegisterType("ui.Button")

// ButtonStyle is the appearance of a button's own box in one interaction
// state. It is the [Border] and [Color] set a [BoxView] would take, named as
// one value so that a state can be described in one place.
//
// # Replacement, not merging
//
// A state style, when set, replaces the whole box style for that state. It is
// not merged field by field with the normal style, and the reason is that
// merging needs a "was this field set" bit per field: the zero [Color] is
// transparent and the zero [Border] is invisible, so an unset field and a
// deliberately cleared one look identical. A caller who wants a pressed
// background and the normal border writes both, which is three fields, once.
// The alternative is a struct of pointers or a parallel set of has-flags, for
// a struct this small.
type ButtonStyle struct {
	// Background fills the button's bounds.
	Background Color
	// Border strokes the inside of the bounds.
	Border Border
	// CornerRadius rounds both.
	CornerRadius float32
}

// Default button appearance. gift has no theme — the project plan, section 14,
// excludes one from the MVP — so these are the values that make an unstyled
// button look like a button instead of like nothing, and nothing more. Any
// application with a design overrides them.
var (
	defaultButtonStyle = ButtonStyle{
		Background:   RGB(232, 234, 238),
		Border:       Border{Width: 1, Color: RGBA(0, 0, 0, 40)},
		CornerRadius: 6,
	}
	defaultButtonHover = ButtonStyle{
		Background:   RGB(244, 246, 250),
		Border:       Border{Width: 1, Color: RGBA(0, 0, 0, 60)},
		CornerRadius: 6,
	}
	defaultButtonPressed = ButtonStyle{
		Background:   RGB(200, 204, 212),
		Border:       Border{Width: 1, Color: RGBA(0, 0, 0, 80)},
		CornerRadius: 6,
	}
	defaultButtonDisabled = ButtonStyle{
		Background:   RGBA(0, 0, 0, 20),
		Border:       Border{Width: 1, Color: RGBA(0, 0, 0, 20)},
		CornerRadius: 6,
	}
	defaultButtonFocusRing = Border{Width: 2, Color: RGB(64, 128, 240)}
	defaultButtonPadding   = geom.Insets{Top: 6, Right: 12, Bottom: 6, Left: 12}
)

// ButtonView is a pressable control around an arbitrary label view. It is
// created by [Button]; the zero value is not useful.
//
// # What it is
//
// The project plan, section 7, says a button is "Pointer-Capture,
// Pressed/Hover/Disabled, Tastaturfokus und Aktivierung per Space/Enter, nicht
// nur einen Maus-Klickhandler", and that is the list this type implements. A
// press captures the pointer, so releasing somewhere else does not activate;
// a touch taps without ever producing a hover; tab reaches the button and
// space or enter fires it; a disabled button is skipped by the focus order and
// ignores every one of those paths.
//
// # Styling
//
// Hover, pressed and disabled are presentation state that lives in the
// retained node, not in the view — see [gift.Interaction] — so a hover does
// not rebuild anything. The consequence for the API is that the four looks
// have to be declared up front rather than chosen by the application per
// frame: [ButtonView.HoverStyle], [ButtonView.PressedStyle] and
// [ButtonView.DisabledStyle] carry them, and the ordinary [ButtonView.Background],
// [ButtonView.Border] and [ButtonView.CornerRadius] modifiers describe the
// normal state like on any other view.
//
// # The label is the caller's
//
// A ButtonStyle covers the button's box and not its label. The label is an
// arbitrary [gift.View] and modifiers do not survive the View boundary — the
// project plan, section 4, "Styling endet an der View-Grenze" — so a button
// cannot recolour the text a caller handed it. A design that needs the label
// to change colour when pressed needs the pressed state at the call site, and
// that is a rebuild. This is a real limitation of variant A and is stated
// rather than papered over.
type ButtonView struct {
	base
	label  gift.View
	action func()
	// name is the accessible name; see [ButtonView.Label].
	name string

	hover, pressed, disabledStyle          ButtonStyle
	hasHover, hasPressed, hasDisabledStyle bool
	focusRing                              Border
	hasFocusRing                           bool
	disabled                               bool
	hasPadding                             bool
	hasStyle                               bool
}

// Button returns a button showing label and calling action when it is
// activated.
//
// This is the spelling of the project plan, section 4: a view and a closure.
// The label is an ordinary view, so it may be text, a box, a stack of both, or
// anything else; ownership of it passes to gift like any other child.
//
// A nil action is legal and makes the button inert while still pressable and
// focusable, which is what a control whose command is temporarily unavailable
// should look like only if it is also disabled — prefer [ButtonView.Disabled]
// for that, because it is the one that tells the user.
func Button(label gift.View, action func()) ButtonView {
	if label == nil {
		panic("gift/ui: Button with a nil label view")
	}
	return ButtonView{label: label, action: action}
}

// ViewType implements gift.View.
func (b ButtonView) ViewType() gift.TypeID { return buttonType }

// Build implements gift.View.
func (b ButtonView) Build(*gift.BuildContext) gift.Element {
	n := &buttonNode{
		fr:       b.frame,
		align:    geom.Alignment{X: 0.5, Y: 0.5},
		action:   b.action,
		disabled: b.disabled,
	}
	if b.align != (geom.Alignment{}) {
		n.align = b.align
	}
	n.pad = defaultButtonPadding
	if b.hasPadding {
		n.pad = b.pad
	}
	n.normal = defaultButtonStyle
	if b.hasStyle {
		n.normal = ButtonStyle{
			Background:   b.style.background,
			Border:       b.style.border,
			CornerRadius: b.style.radius,
		}
	}
	n.hover, n.pressed, n.disabledStyle = n.normal, n.normal, n.normal
	switch {
	case b.hasHover:
		n.hover = b.hover
	case !b.hasStyle:
		n.hover = defaultButtonHover
	}
	switch {
	case b.hasPressed:
		n.pressed = b.pressed
	case !b.hasStyle:
		n.pressed = defaultButtonPressed
	}
	switch {
	case b.hasDisabledStyle:
		n.disabledStyle = b.disabledStyle
	case !b.hasStyle:
		n.disabledStyle = defaultButtonDisabled
	}
	n.shadow = b.style.shadow
	n.focusRing = defaultButtonFocusRing
	if b.hasFocusRing {
		n.focusRing = b.focusRing
	}
	n.kids[0] = b.label

	return gift.Element{
		Key:      b.key,
		Flex:     b.flex,
		Layouter: n,
		Painter:  n,
		Children: n.kids[:],
		// The accessible name of the command, if the caller gave one. A
		// button whose label is a [TextView] needs none: the text node
		// underneath carries the same string already. One whose label is an
		// icon has nothing that could, which is what [ButtonView.Label] is
		// for. See [gift.Element.Label].
		Label: b.name,
		// Input. A button is the reason [gift.Interactor] exists: it opts in
		// explicitly, it is focusable unless disabled, and a disabled button
		// still blocks clicks instead of letting them fall through to
		// whatever is behind it.
		Interactor: n,
		Focusable:  !b.disabled,
		Disabled:   b.disabled,
		Clip:       b.style.clip,
	}
}

// buttonNode is the retained half of a [ButtonView]: layouter, painter and
// interactor in one object, allocated once per build like every other
// container node in this package.
//
// It holds no hover, pressed or focus flags of its own. Those live in the
// retained node and are read through [gift.PaintContext.Interaction], which is
// what makes them survive a rebuild and, much more importantly, not cause one.
type buttonNode struct {
	fr    frameSpec
	pad   geom.Insets
	align geom.Alignment

	normal, hover, pressed, disabledStyle ButtonStyle
	focusRing                             Border
	// shadow is the one part of the look that is not per state.
	//
	// [ButtonStyle] deliberately does not carry one. A shadow that changed
	// with hover would make the button jump under the pointer, which is an
	// animation and not a state style, and the project plan, section 14,
	// excludes animation curves from the MVP. A caller who wants a pressed
	// button to sit lower writes two views, or waits for animation.
	shadow Shadow

	action   func()
	disabled bool

	// kids is the one element children slice. It is a array inside this
	// struct rather than a slice literal, so a build allocates the node and
	// nothing else.
	kids [1]gift.View

	items   [1]layout.Item
	origins [1]geom.Point
	ctx     *gift.LayoutContext
}

// MeasureChild implements layout.Measurer.
func (n *buttonNode) MeasureChild(i int, c geom.Constraints) geom.Size {
	return n.ctx.Measure(i, c)
}

// Layout centres the label inside the padded bounds.
//
// It is the overlay algorithm with one child, which is the honest description
// of a button: the label decides the size, the padding grows it, and a
// [ButtonView.Frame] overrides both and overflows if it has to.
func (n *buttonNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.ctx = ctx
	cc := n.fr.apply(c)
	res := layout.Overlay(n.pad, n.align, cc, ctx.ChildCount(), n, n.items[:], n.origins[:])
	for i := range ctx.ChildCount() {
		ctx.Place(i, n.origins[i])
	}
	ctx.ReportOverflow(res.Overflow)
	// A button next to another button lines up on the baseline of its label,
	// which is the first real consumer of the channel gift.LayoutContext has
	// carried since WU-G. A label that reports no baseline — a button whose
	// content is an icon — passes none on, rather than offering its own edge
	// as a substitute.
	if ctx.ChildCount() > 0 {
		if bl, ok := ctx.ChildBaseline(0); ok {
			ctx.ReportBaseline(n.origins[0].Y + bl)
		}
	}
	n.ctx = nil
	return res.Size
}

// Paint draws the box in the style of the current interaction state, then the
// label, then the focus ring.
//
// The state comes from the retained node and not from a field of this object,
// so a hover is a repaint and nothing else: no view function runs and
// [gift.Diagnostics.Builds] does not move.
func (n *buttonNode) Paint(ctx *gift.PaintContext) {
	ia := ctx.Interaction()
	st := n.styleFor(ia)
	b := ctx.Bounds()
	paintBackground(ctx, st, b)
	ctx.PaintChildren()
	paintBorder(ctx, st, b)
	if ia.Focused && !ia.Disabled && n.focusRing.IsVisible() {
		paintBorder(ctx, styleSpec{border: n.focusRing, radius: st.radius}, b)
	}
}

// styleFor picks the box style of one interaction state. The precedence is
// disabled, pressed, hover, normal: a disabled control cannot be pressed, and
// a pressed one is pressed whether or not the pointer is also hovering.
func (n *buttonNode) styleFor(ia gift.Interaction) styleSpec {
	var st styleSpec
	switch {
	case ia.Disabled:
		st = styleOf(n.disabledStyle)
	case ia.Pressed:
		st = styleOf(n.pressed)
	case ia.Hover:
		st = styleOf(n.hover)
	default:
		st = styleOf(n.normal)
	}
	// The shadow is state independent; see [buttonNode.shadow].
	st.shadow = n.shadow
	return st
}

func styleOf(s ButtonStyle) styleSpec {
	return styleSpec{background: s.Background, border: s.Border, radius: s.CornerRadius}
}

// HandleEvent implements gift.Interactor.
//
// It is the whole interaction contract of a button in one place:
//
//   - A press takes the keyboard focus and, through gift's pointer capture,
//     every further event of that pointer. gift maintains Pressed; this
//     method does not have to.
//   - A release activates only when it happened inside the button and the
//     pointer did not wander off in between. A release outside still arrives
//     here — that is the point of capture — and is declined.
//   - A cancel never activates. A gesture the platform took over is not a
//     click.
//   - Space and enter activate when the button has the focus, on key down,
//     which is where every desktop toolkit puts the activation of a button.
//
// A disabled button never sees any of this: gift does not deliver events to a
// disabled node at all. The check below is belt and braces for a node that was
// disabled between the press and the release.
func (n *buttonNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if n.disabled {
		return false
	}
	switch e.Kind {
	case gift.EventPointerDown:
		ctx.RequestFocus()
		return true
	case gift.EventPointerUp:
		if e.Inside && !e.Dragged {
			n.activate()
		}
		return true
	case gift.EventKeyDown:
		if e.Key == gift.KeySpace || e.Key == gift.KeyEnter {
			n.activate()
			return true
		}
	}
	return false
}

func (n *buttonNode) activate() {
	if n.action != nil {
		n.action()
	}
}

// --- modifiers -------------------------------------------------------------

// Style replaces the normal state box style in one call. It is the same thing
// as Background, Border and CornerRadius together and exists so that the four
// states can be written symmetrically.
func (b ButtonView) Style(v ButtonStyle) ButtonView {
	b.setBackground(v.Background)
	b.setBorder(v.Border)
	b.setCornerRadius(v.CornerRadius)
	b.hasStyle = true
	return b
}

// HoverStyle sets the look while a mouse is over the button. A touch never
// triggers it; see [gift.PointerKind].
func (b ButtonView) HoverStyle(v ButtonStyle) ButtonView { b.hover, b.hasHover = v, true; return b }

// PressedStyle sets the look while a pointer this button captured is down and
// inside it.
func (b ButtonView) PressedStyle(v ButtonStyle) ButtonView {
	b.pressed, b.hasPressed = v, true
	return b
}

// DisabledStyle sets the look while the button is disabled.
func (b ButtonView) DisabledStyle(v ButtonStyle) ButtonView {
	b.disabledStyle, b.hasDisabledStyle = v, true
	return b
}

// FocusRing sets the border drawn on top of the button while it holds the
// keyboard focus. A zero width border removes it.
//
// It is one border for every state rather than a field of [ButtonStyle],
// because focus is orthogonal to hover and press: a focused, hovered button is
// hovered *and* focused, and folding the ring into the state styles would make
// that combination impossible to express without writing the ring four times.
func (b ButtonView) FocusRing(v Border) ButtonView { b.focusRing, b.hasFocusRing = v, true; return b }

// Label sets the accessible name of the button, the string a person would use
// to refer to the command.
//
// It changes nothing visual and is never drawn. A button whose label view is a
// [TextView] does not need it — the text node underneath already carries the
// same string, and a test or an accessibility bridge finds the button through
// it. An icon button carries no such string anywhere, and this is where it
// goes. See [gift.Element.Label].
func (b ButtonView) Label(v string) ButtonView { b.name = v; return b }

// Disabled takes the button out of input. It is skipped by the focus order,
// receives no events and draws in its disabled style. It still occupies its
// place in the layout and still blocks clicks from reaching what is behind it.
func (b ButtonView) Disabled(v bool) ButtonView { b.disabled = v; return b }

// Padding sets the same padding on all four edges around the label, replacing
// any previous padding and the default of 6 by 12.
func (b ButtonView) Padding(v float32) ButtonView { b.setPadding(v); b.hasPadding = true; return b }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (b ButtonView) PaddingInsets(v geom.Insets) ButtonView {
	b.setPaddingInsets(v)
	b.hasPadding = true
	return b
}

// Align sets where the label sits inside the padded bounds. The default is
// centred on both axes.
func (b ButtonView) Align(v geom.Alignment) ButtonView { b.setAlign(v); return b }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free. Precedence: Frame, then Max, then Min; see [frameSpec].
func (b ButtonView) Frame(w, h float32) ButtonView { b.setFrame(w, h); return b }

// MinWidth raises the minimum width of the button, and the maximum with it if
// that is lower; see [frameSpec].
func (b ButtonView) MinWidth(v float32) ButtonView { b.setMinWidth(v); return b }

// MinHeight raises the minimum height of the button, and the maximum with it
// if that is lower; see [frameSpec].
func (b ButtonView) MinHeight(v float32) ButtonView { b.setMinHeight(v); return b }

// MaxWidth lowers the maximum width of the button, and the minimum with it if
// that is higher; see [frameSpec].
func (b ButtonView) MaxWidth(v float32) ButtonView { b.setMaxWidth(v); return b }

// MaxHeight lowers the maximum height of the button, and the minimum with it
// if that is higher; see [frameSpec].
func (b ButtonView) MaxHeight(v float32) ButtonView { b.setMaxHeight(v); return b }

// Background fills the bounds in the normal state. Use [ButtonView.HoverStyle]
// and friends for the other states.
func (b ButtonView) Background(v Color) ButtonView { b.setBackground(v); b.hasStyle = true; return b }

// Border strokes the inside of the bounds in the normal state.
func (b ButtonView) Border(v Border) ButtonView { b.setBorder(v); b.hasStyle = true; return b }

// Shadow draws a blurred copy of the button's box behind it.
//
// Unlike Background, Border and CornerRadius it is not part of [ButtonStyle]
// and does not change with hover, press or disabled; see [buttonNode.shadow].
// It extends the paint bounds but not the layout size and not the hit area.
func (b ButtonView) Shadow(v Shadow) ButtonView { b.setShadow(v); return b }

// CornerRadius rounds the background and the border in every state that does
// not override it.
func (b ButtonView) CornerRadius(v float32) ButtonView {
	b.setCornerRadius(v)
	b.hasStyle = true
	return b
}

// Clip confines the label to the bounds, for paint and for hit testing alike.
func (b ButtonView) Clip(v bool) ButtonView { b.setClip(v); return b }

// Key sets the reconciliation key of this view among its siblings.
func (b ButtonView) Key(v string) ButtonView { b.setKey(v); return b }

// Flex makes the button take a share of the remaining main axis space of its
// parent stack, proportional to v.
func (b ButtonView) Flex(v float32) ButtonView { b.setFlex(v); return b }
