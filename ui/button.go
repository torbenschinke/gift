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
// # A state style replaces, but the theme still shows through
//
// A state style, when set, replaces the whole box style for that state: it is
// not merged field by field with the normal style. That part is unchanged and
// the reason section 8 of the project plan gives for it still holds — merging
// two *caller supplied* styles would need a "was this field set" bit per
// field, and an exported struct a caller fills with a composite literal has no
// bit a literal could set.
//
// What did change, in the work unit that implemented section 20, is what an
// unset colour means. The zero [Color] in this struct is not "transparent", it
// is **"the theme decides"**:
//
//	ui.Button(label, act).PressedStyle(ui.ButtonStyle{CornerRadius: 10})
//
// gets the themed pressed face and no border, rather than an invisible
// button. A caller who genuinely wants nothing drawn writes [ColorClear],
// which is transparency by name. The same rule already applied to the numbers
// of [ScrollBar], where a zero Width means "the default" and not "no bar".
//
// [ButtonStyle.Border] follows it one level down: a Border with a width and an
// unset colour is stroked in [ColorSeparator], while a Border with no width is
// no border at all. Geometry is the call site's, the hue is the theme's. Note
// that this rule is local to ButtonStyle — [ButtonView.Border] and every other
// .Border modifier leave a colourless border invisible, because there the
// value is the caller's and not a field with a default under it.
//
// # The struct is read in two places, and they differ in one way
//
// [ButtonView.HoverStyle], [ButtonView.PressedStyle] and
// [ButtonView.DisabledStyle] take a *replacement*: every field of the value
// counts, an unset Border means no border, and an unset CornerRadius means a
// square corner. That is the whole-style replacement of section 8 and the
// reason the paragraph above exists.
//
// [ButtonView.Style] does not replace anything — it feeds the ordinary field
// by field style of the view, the same one [ButtonView.Background] and friends
// feed, which has a "was this set" bit per field. There an unset field means
// "the caller did not write it", so Style{CornerRadius: 11} keeps the themed
// face, the themed hairline *and* the themed hover and pressed faces, exactly
// like .CornerRadius(11) does. The two spellings used to disagree; see
// [ButtonView.Style].
type ButtonStyle struct {
	// Background fills the button's bounds. The zero value means the themed
	// face of the state this style belongs to; use [ColorClear] for none.
	Background Color
	// Border strokes the inside of the bounds. A width without a colour is
	// stroked in [ColorSeparator].
	Border Border
	// CornerRadius rounds both. The zero value is a square corner and not a
	// themed default: a radius is a shape, not a colour, and section 20 is
	// about colours.
	CornerRadius float32
}

// withThemedDefaults applies the unset rule of [ButtonStyle]: an unset
// background becomes the semantic face of the state this style belongs to, and
// an unset border colour becomes [ColorSeparator]. The result still carries
// semantic colours.
func (s ButtonStyle) withThemedDefaults(face Color) ButtonStyle {
	if s.Background == (Color{}) {
		s.Background = face
	}
	if s.Border.Width > 0 && s.Border.Color == (Color{}) {
		s.Border.Color = ColorSeparator
	}
	return s
}

// resolved applies the unset rule and then turns every colour into a literal
// one. It runs in [ButtonView.Build] and nowhere else, so a button node holds
// plain colours and the frame path never consults a theme.
func (s ButtonStyle) resolved(face Color) ButtonStyle {
	s = s.withThemedDefaults(face)
	s.Background = ResolveColor(s.Background)
	s.Border = resolveBorder(s.Border)
	return s
}

// Default button appearance, in the semantic colours of the project plan,
// section 20.
//
// These are package variables holding *unresolved* colours, which is what
// makes them follow a later [SetTheme] rather than freezing whichever theme
// was installed when this package was initialised. They are resolved once per
// build, in [ButtonView.Build].
//
// They are unexported, and they stay unexported now that a theme exists. An
// exported defaultButtonStyle would be a second process wide lever next to
// [SetTheme] — one that covers one widget, does not know about light and dark,
// and would silently win over the theme for anybody who assigned to it. The
// way to restyle every button in an application is a theme:
//
//	ui.SetTheme(app, ui.LightTheme().With(ui.ColorControl, ui.RGB(226, 232, 240)))
//
// The radius and the padding are not themed. They are metrics, and section 20
// introduces semantic *colours*; a metric scale is a separate decision that
// would want the density work of section 18 next to it.
var (
	defaultButtonBorder    = Border{Width: 1, Color: ColorSeparator}
	defaultButtonFocusRing = Border{Width: 2, Color: ColorAccent}
	defaultButtonRadius    = float32(6)
	defaultButtonPadding   = geom.Insets{Top: 6, Right: 12, Bottom: 6, Left: 12}
)

// defaultStyle is the themed look of one interaction state. face is the
// semantic colour of that state: [ColorControl], [ColorControlHover],
// [ColorControlPressed] or [ColorControlDisabled].
//
// # One separator for hover and pressed, a weaker one for disabled
//
// The old literals had a per state alpha ladder — 40, 60, 80 and 20 — and
// three quarters of it is deliberately gone. A hairline stepping by twenty
// units of alpha is below the threshold at which anybody reads it as feedback,
// while the face, which steps by twelve to thirty two units of colour, is what
// actually says "hovered". One separator is also the only version of that
// which can be themed without three more tokens.
//
// Disabled is the exception, and collapsing it into the same separator was a
// mistake worth naming: it took the hairline from alpha 20 to alpha 40, which
// made a disabled button's outline *twice as strong* as before and exactly as
// strong as an enabled one. The argument above does not cover this case,
// because it is about feedback and disabled is not feedback — it is an
// affordance, and the affordance is that there is less of the control. The
// face alone cannot carry it: [ColorControlDisabled] is a twenty-alpha wash in
// both themes, so a crisp full strength outline around a nearly absent face
// reads as an enabled button someone forgot to fill in.
//
// So the disabled hairline is [ColorSeparator] at half, which is the alpha 20
// it had before this work unit, expressed as a derivation of the one token
// instead of as a fourth one. That is what [Fade] is for and it costs no new
// role; see [disabledHairlineFade].
func defaultStyle(face Color) ButtonStyle {
	return ButtonStyle{
		Background:   face,
		Border:       defaultButtonBorder,
		CornerRadius: defaultButtonRadius,
	}
}

// disabledHairlineFade is how much of [ColorSeparator] the library's own
// hairline keeps around a disabled button. See [defaultStyle].
//
// It applies only when the library supplied the hairline. A caller who wrote a
// border, with .Border or through a declared [ButtonView.DisabledStyle], gets
// theirs at full strength: it is a value they passed in, and the unset rule of
// [ButtonStyle] does not reach into a value somebody named.
const disabledHairlineFade = 0.5

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
	// The normal state, field by field. This is what [styleSpec.set] is for:
	// a caller who set only a corner radius keeps the themed face and the
	// themed hairline and gets their radius, instead of the transparent
	// background and invisible border the zero style used to hand them.
	st := b.style.resolved()
	n.normal = defaultStyle(ColorControl).resolved(ColorControl)
	if b.style.isSet(bitBackground) {
		n.normal.Background = st.background
	}
	if b.style.isSet(bitBorder) {
		n.normal.Border = st.border
	}
	if b.style.isSet(bitRadius) {
		n.normal.CornerRadius = st.radius
	}
	n.hover = b.stateStyle(b.hasHover, b.hover, ColorControlHover, n.normal)
	n.pressed = b.stateStyle(b.hasPressed, b.pressed, ColorControlPressed, n.normal)
	n.disabledStyle = b.stateStyle(b.hasDisabledStyle, b.disabledStyle, ColorControlDisabled, n.normal)
	if !b.hasDisabledStyle && !b.style.isSet(bitBorder) {
		// The library's own hairline, weakened for the one state where less
		// of the control *is* the affordance; see [defaultStyle]. Only when
		// the library supplied it — a border the caller named is theirs.
		n.disabledStyle.Border.Color = ResolveColor(Fade(ColorSeparator, disabledHairlineFade))
	}
	n.shadow = st.shadow
	n.focusRing = resolveBorder(defaultButtonFocusRing)
	if b.hasFocusRing {
		n.focusRing = resolveBorder(b.focusRing)
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

// stateStyle picks the look of one non normal interaction state.
//
// Three cases, and the middle one is the rule the old code expressed with a
// single hasStyle flag:
//
//   - The caller declared the state. It is used as written; it was already
//     resolved by the modifier that took it.
//   - The caller gave the button a background of its own. The state then
//     keeps that background, because a control painted in an application's
//     own colour must not light up in gift's stock hover grey when the
//     pointer crosses it.
//   - Otherwise the state takes the themed face for it, on top of whatever
//     border and radius the normal state ended up with. This is the part that
//     is new: a caller who set only a radius, or only a border, used to lose
//     hover, press and disabled feedback entirely.
func (b ButtonView) stateStyle(declared bool, s ButtonStyle, face Color, normal ButtonStyle) ButtonStyle {
	if declared {
		return s.resolved(face)
	}
	if !b.style.isSet(bitBackground) {
		normal.Background = ResolveColor(face)
	}
	return normal
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
	// animation and not a state style. The project plan, section 14, used to
	// exclude animation curves and no longer does, but an *uninterpolated*
	// jump between two shadows is not the thing that exclusion was about: it
	// would be a flicker whether or not gift can animate. A caller who wants
	// a pressed button to sit lower writes two views, or waits for the
	// animation primitives.
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
	// In front of the focus ring gate below and not behind it; see
	// [assertResolved]. An unresolved colour is transparent, so the gate
	// would drop the ring and a button that never shows where the keyboard
	// is would be the only symptom.
	assertResolvedBorder(n.focusRing, "the focus ring of a Button")

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
// disabled node at all, and the payload flag is written from the element in
// the same pass that builds this node, so there is no window between the press
// and the release in which it could be reached either. The check below is
// therefore unreachable and is kept only because it predates that guarantee;
// the controls in toggle.go, slider.go and segmented.go deliberately do not
// have one, and [disabledIsTheCoresBusiness] says why.
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

// Style sets the normal state box style in one call. It exists so that the
// four states can be written symmetrically.
//
// It sets exactly the fields the caller wrote and leaves the others alone,
// which is what makes it the same thing as [ButtonView.Background],
// [ButtonView.Border] and [ButtonView.CornerRadius] together — the sentence
// this method's documentation has always claimed. It did not hold: the old
// implementation synthesised a themed background and then recorded it as
// though the caller had named one, so
//
//	ui.Button(l, a).Style(ui.ButtonStyle{CornerRadius: 11})
//
// lost its hover and pressed faces while the spelling three characters away,
// .CornerRadius(11), kept them. See [ButtonView.stateStyle] for why that bit
// decides the question.
//
// The one thing the aggregate spelling cannot say is "no corner radius at
// all", because a zero CornerRadius here is indistinguishable from an unset
// one. Write .CornerRadius(0) for that. That is the price of a struct without
// per field presence, and it is the same price [ButtonStyle] pays everywhere
// else; see its documentation.
func (b ButtonView) Style(v ButtonStyle) ButtonView {
	if v.Background != (Color{}) {
		b.setBackground(v.Background)
	}
	if v.Border.Width > 0 {
		// The border half of the unset rule: geometry is the call site's,
		// the hue is the theme's. See [ButtonStyle].
		if v.Border.Color == (Color{}) {
			v.Border.Color = ColorSeparator
		}
		b.setBorder(v.Border)
	}
	if v.CornerRadius != 0 {
		b.setCornerRadius(v.CornerRadius)
	}
	return b
}

// HoverStyle sets the look while a mouse is over the button. A touch never
// triggers it; see [gift.PointerKind].
func (b ButtonView) HoverStyle(v ButtonStyle) ButtonView {
	b.hover, b.hasHover = v, true
	return b
}

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
func (b ButtonView) FocusRing(v Border) ButtonView {
	b.focusRing, b.hasFocusRing = v, true
	return b
}

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
func (b ButtonView) Background(v Background) ButtonView {
	b.setBackgroundSpec(v)
	return b
}

// Border strokes the inside of the bounds in the normal state.
func (b ButtonView) Border(v Border) ButtonView { b.setBorder(v); return b }

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
	return b
}

// Clip confines the label to the bounds, for paint and for hit testing alike.
//
// Both halves come from the one [gift.Element.Clip] this sets; gift applies it
// on the way into the subtree. It used to apply to hit testing only, so a
// label larger than the button painted over everything around it while
// claiming in this very sentence that it did not.
func (b ButtonView) Clip(v bool) ButtonView { b.setClip(v); return b }

// Key sets the reconciliation key of this view among its siblings.
func (b ButtonView) Key(v string) ButtonView { b.setKey(v); return b }

// Flex makes the button take a share of the remaining main axis space of its
// parent stack, proportional to v.
func (b ButtonView) Flex(v float32) ButtonView { b.setFlex(v); return b }
