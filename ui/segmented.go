package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var segmentedType = gift.RegisterType("ui.SegmentedControl")

// Metrics of a segmented control, in logical pixels.
const (
	// segmentedInset is the gap between the tray and the sliding indicator,
	// on all four sides. It is what makes the indicator read as a card lying
	// in a groove rather than as a repainted segment.
	segmentedInset = float32(2)
	// segmentedPadH and segmentedPadV are the space around a label inside its
	// own segment, which is what decides the natural size of the control.
	segmentedPadH = float32(12)
	segmentedPadV = float32(7)
	// segmentedRadius is the corner of the tray. The indicator is rounded by
	// the same amount less the inset, so the two curves are concentric.
	segmentedRadius   = float32(9)
	segmentedFocusGap = float32(3)
)

// Default appearance of a segmented control; unresolved, so that it follows a
// later [SetTheme]. See button.go for the full argument.
var (
	// defaultSegmentedTray is the groove behind all the segments.
	defaultSegmentedTray = ColorControl
	// defaultSegmentedIndicator is the card over the selected one.
	//
	// It is [ColorSurface] rather than [ColorAccent], which is the choice the
	// Human Interface Guidelines make and it is worth a sentence: a segmented
	// control is a *view switch*, not a command, and painting the current
	// view's tab in the accent colour makes it compete with the accent used
	// for actions on the screen it selects.
	defaultSegmentedIndicator = ColorSurface
	// defaultSegmentedIndicatorBorder separates the card from the tray in a
	// theme where the two are close in lightness; see [defaultToggleKnob].
	defaultSegmentedIndicatorBorder = Border{Width: 1, Color: ColorSeparator}
	defaultSegmentedFocusRing       = Border{Width: 2, Color: ColorAccent}
)

// SegmentedControlView is a row of mutually exclusive choices with a sliding
// indicator over the chosen one. It is created by [SegmentedControl]; the zero
// value is not useful.
//
//	ui.SegmentedControl(tab.Get(), []string{"Day", "Week", "Month"}, tab.Set)
//
// # What it is for
//
// A small, fixed set of alternatives that are all worth showing at once —
// three or four, occasionally five. It is the Human Interface Guidelines'
// answer to "which of these views am I looking at", and it is the wrong
// control for a long list, for a list that changes at runtime, or for anything
// the user has to scroll to see: every segment is on screen, always, and they
// share the width equally, so a sixth segment makes all six too narrow to read
// rather than making the control scroll.
//
// # How it is operated
//
// Tap a segment, or focus the control and use the left and right arrows, which
// move the selection by one; home and end go to the ends. The whole control is
// one focus stop, not one per segment, because it stands for one value.
//
// Up and down work too and are the vertical spelling of the same step: up is
// the previous segment and down the next, as in any list. That is the
// *opposite* of a [SliderView], where up increases the value; see
// [segmentedNode.handleKey].
//
// The tap is decided where the finger *lifts*, so a finger that comes down on
// the wrong segment can slide onto the right one before lifting; see
// [segmentedNode.HandleEvent] for why that is not merely a nicety on a
// touchscreen.
//
// Arrow keys change the selection immediately rather than moving a cursor that
// a second key press confirms. That is what every platform's segmented control
// does and it is the right trade for a control whose segments switch a view:
// there is nothing destructive to confirm.
//
// # The labels
//
// They are ordinary [TextView] children, which is not an implementation
// detail: it means each segment carries its own string as a
// [gift.Element.Label], so a test finds a segment by its text and does not
// have to know where the control put it. The slice passed to
// [SegmentedControl] is *not* retained — the strings are turned into child
// views during the build and the slice is the caller's again on return.
//
// # The animation
//
// The indicator slides over [ControlAnimation] whenever the selection changes,
// whoever changed it, and then stops: a control at rest enrols in nothing and
// lets the machine sleep. See [gift.LayoutContext.SetControlState] for why
// that is driven from the layouter.
type SegmentedControlView struct {
	base
	selected int
	labels   []string
	onChange func(int)
	name     string
	disabled bool

	font     Font
	fontSize float32
	hasFont  bool
	hasSize  bool
}

// SegmentedControl returns a segmented control showing labels with segment
// selected marked, and calling onChange with the index the user asked for.
//
// It panics on an empty labels slice: a segmented control with nothing to
// choose between has no size, no behaviour and no reading, and a silent empty
// box would be diagnosed as a layout bug somewhere else.
//
// A selected index outside the range draws with no indicator at all and
// reports nothing until the user picks something. That is the honest picture
// of "the model is not one of my choices" — clamping it to an end would show a
// selection the application does not have.
//
// A nil onChange makes the control inert; prefer [SegmentedControlView.Disabled]
// for one that is unavailable.
func SegmentedControl(selected int, labels []string, onChange func(int)) SegmentedControlView {
	if len(labels) == 0 {
		panic("gift/ui: SegmentedControl with no labels")
	}
	return SegmentedControlView{selected: selected, labels: labels, onChange: onChange}
}

// ViewType implements gift.View.
func (s SegmentedControlView) ViewType() gift.TypeID { return segmentedType }

// Build implements gift.View.
func (s SegmentedControlView) Build(*gift.BuildContext) gift.Element {
	n := &segmentedNode{
		fr:        s.frame,
		selected:  s.selected,
		count:     len(s.labels),
		onChange:  s.onChange,
		tray:      ResolveColor(defaultSegmentedTray),
		indicator: ResolveColor(defaultSegmentedIndicator),
		border:    resolveBorder(defaultSegmentedIndicatorBorder),
		focusRing: resolveBorder(defaultSegmentedFocusRing),
	}
	if s.disabled {
		n.tray = ResolveColor(ColorControlDisabled)
		n.border.Color = ResolveColor(Fade(ColorSeparator, disabledHairlineFade))
	}
	// A fresh slice per build, handed to gift; the caller's slice of strings
	// is only read. See [gift.Element.Children] for the ownership rule.
	kids := make([]gift.View, len(s.labels))
	for i, label := range s.labels {
		t := Text(label).Align(AlignCenter)
		if s.hasFont {
			t = t.Font(s.font)
		}
		if s.hasSize {
			t = t.FontSize(s.fontSize)
		}
		t = t.Foreground(s.labelColor(i))
		kids[i] = t
	}
	return gift.Element{
		Key:        s.key,
		Flex:       s.flex,
		Layouter:   n,
		Painter:    n,
		Children:   kids,
		Label:      s.name,
		Interactor: n,
		Focusable:  !s.disabled,
		Disabled:   s.disabled,
	}
}

// labelColor is the foreground of segment i.
//
// The selected one is [ColorLabel] and the others [ColorSecondaryLabel], which
// is the only difference between them that survives at a glance: the indicator
// card is a quiet surface and on a light theme it is barely a shade away from
// the tray, so the *text weight* is what carries the selection when the
// control is read from half a metre away.
//
// It is a build time decision, which is safe here and is not a rebuild trap:
// the selection is a parameter of the view and every change of it is a
// rebuild anyway. Hover and press are not consulted, because those live in the
// retained node and would need a per segment interaction state gift does not
// have.
func (s SegmentedControlView) labelColor(i int) Color {
	switch {
	case s.disabled:
		return Fade(ColorSecondaryLabel, 0.5)
	case i == s.selected:
		return ColorLabel
	default:
		return ColorSecondaryLabel
	}
}

// segmentedNode is the retained half of a [SegmentedControlView]. The
// indicator's position lives in [gift.ControlState] so that it survives the
// rebuild that every selection change causes.
type segmentedNode struct {
	fr       frameSpec
	selected int
	count    int
	onChange func(int)

	tray, indicator Color
	border          Border
	focusRing       Border

	// sizes is the scratch buffer of measured child sizes.
	//
	// It is reused across the layout passes of one build — a resize, a
	// relayout forced by a parent, a second pass under different constraints
	// — and not across builds: [SegmentedControlView.Build] allocates a fresh
	// segmentedNode every time, so a change of selection reallocates this
	// slice along with the node, the children slice and the label views. That
	// is one small allocation on a path that is already several, and it is
	// not on the frame path of a control at rest, which is what matters on
	// the Pi; a buffer that outlived the node would have to live in the
	// retained payload, which is the thing this project spends deliberately
	// rather than by default.
	sizes []geom.Size
}

// target is the phase the indicator should come to rest at: the selected index
// as a number, or the phase it already has when there is no valid selection.
func (n *segmentedNode) hasSelection() bool { return n.selected >= 0 && n.selected < n.count }

// Layout divides the width into equal columns and centres each label in one.
//
// Equal columns, not columns sized to their labels. A control whose segments
// change width when the selection changes is one whose segments move under the
// finger that is about to tap them, and equal shares are what the Human
// Interface Guidelines specify for this control anyway.
func (n *segmentedNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	if n.hasSelection() {
		syncControl(ctx, float32(n.selected))
	}
	cc := n.fr.apply(c)
	k := ctx.ChildCount()
	if cap(n.sizes) < k {
		n.sizes = make([]geom.Size, k)
	}
	n.sizes = n.sizes[:k]

	// The width a column may offer a label. When the control is unbounded
	// there is no share to compute, so the labels are measured free and the
	// widest one decides.
	colMax := geom.Unbounded()
	if cc.HasBoundedWidth() && k > 0 {
		colMax = cc.Max.W/float32(k) - 2*segmentedPadH
		if colMax < 0 {
			colMax = 0
		}
	}
	var widest, tallest float32
	for i := range k {
		n.sizes[i] = ctx.Measure(i, geom.Constraints{Max: geom.Sz(colMax, geom.Unbounded())})
		if n.sizes[i].W > widest {
			widest = n.sizes[i].W
		}
		if n.sizes[i].H > tallest {
			tallest = n.sizes[i].H
		}
	}

	w := float32(k) * (widest + 2*segmentedPadH)
	if cc.HasBoundedWidth() {
		w = cc.Max.W
	}
	h := tallest + 2*segmentedPadV
	if h < ControlHitTarget {
		h = ControlHitTarget
	}
	// Never smaller than the tray plus its labels; see [controlSize]. A
	// segmented control squeezed to nothing is the same defect a zero height
	// slider was: a tray with no card and no text in it, which is not a
	// control at all.
	size := controlSize(ctx, cc, geom.Sz(w, h),
		geom.Sz(float32(max(k, 1))*2*segmentedInset, tallest+2*segmentedInset))

	col := size.W / float32(max(k, 1))
	for i := range k {
		x := col*float32(i) + (col-n.sizes[i].W)/2
		ctx.Place(i, geom.Pt(x, (size.H-n.sizes[i].H)/2))
	}
	return size
}

// columns is the width of one segment over bounds.
func (n *segmentedNode) column(b geom.Rect) float32 {
	if n.count <= 0 {
		return 0
	}
	return b.Width() / float32(n.count)
}

// indicatorRect is the card at phase p, where p is a segment index that may be
// between two of them while the indicator is travelling.
func (n *segmentedNode) indicatorRect(b geom.Rect, p float32) geom.Rect {
	inner := b.Inset(geom.InsetsAll(segmentedInset))
	col := n.column(b)
	x := inner.Min.X + col*p
	return geom.Rc(x, inner.Min.Y, x+col-2*segmentedInset, inner.Max.Y)
}

// segmentAt is the index of the segment under x, or -1 when x is outside.
func (n *segmentedNode) segmentAt(b geom.Rect, x float32) int {
	col := n.column(b)
	if !(col > 0) || x < b.Min.X || x >= b.Max.X {
		return -1
	}
	i := int((x - b.Min.X) / col)
	if i >= n.count {
		i = n.count - 1
	}
	return i
}

// Paint draws the tray, the sliding indicator, the labels and the focus ring.
//
// The indicator goes under the labels, so a card that has not finished
// travelling passes behind the text of the segment it is leaving rather than
// over it.
func (n *segmentedNode) Paint(ctx *gift.PaintContext) {
	// See [toggleNode.Paint]: in front of the gates, not behind them.
	assertResolved(n.tray, "the tray of a SegmentedControl")
	assertResolved(n.indicator, "the indicator of a SegmentedControl")
	assertResolvedBorder(n.border, "the indicator border of a SegmentedControl")
	assertResolvedBorder(n.focusRing, "the focus ring of a SegmentedControl")

	b := ctx.Bounds()
	fillRounded(ctx, b, segmentedRadius, n.tray)

	if n.hasSelection() {
		p := controlPhase(ctx.ControlState(), ctx.Now())
		r := n.indicatorRect(b, p)
		radius := segmentedRadius - segmentedInset
		fillRounded(ctx, r, radius, n.indicator)
		if n.border.IsVisible() {
			ctx.Add(strokeOp(r, radius, n.border))
		}
	}

	ctx.PaintChildren()

	ia := ctx.Interaction()
	if ia.Focused && !ia.Disabled && n.focusRing.IsVisible() {
		g := geom.InsetsAll(-segmentedFocusGap)
		ctx.Add(strokeOp(b.Inset(g), segmentedRadius+segmentedFocusGap, n.focusRing))
	}
}

// HandleEvent implements gift.Interactor.
//
// # The segment is decided at the release, at the position of the release
//
// Not at the press, and not only for a gesture gift did not classify as a
// drag. A finger that comes down on the wrong segment can slide onto the right
// one and lift there, and that is the whole point: [gift.DragSlop] is eight
// logical pixels, which on the 1920x1080 panel this project targets is about
// 1.3 millimetres, so a finger that merely *rolls* while it lifts is already a
// drag by gift's reckoning. A control that declined a drag would therefore
// throw away ordinary taps, and the recovery gesture documented above would
// select nothing at all.
//
// A release outside the control still selects nothing: the finger left, and
// [gift.Event.Inside] is how it says so.
//
// # Why this does not fight an enclosing scroll view
//
// Because a scroll container does not wait for the release. It recognises the
// gesture on the *move*, calls [gift.EventContext.StealPointer], and the steal
// sends this node an EventPointerCancel and redirects every later event of
// that pointer — including the release — to the scroller. So a vertical swipe
// that begins on a segmented control inside a [ScrollView] scrolls and selects
// nothing, and it is the cancel and not a drag flag that says so.
//
// The case this does give up is a segmented control inside a *horizontally*
// scrolling view, where sliding along the control is the scroller's gesture
// and the control only gets a cancel. That is the same ambiguity every
// platform has and it is resolved the same way: the container wins.
//
// There is deliberately no flag recording that a press was seen, either. The
// release is delivered to whatever holds the pointer capture, and this node
// holds it exactly when the press landed on it; a node that lost the capture
// to a steal is sent a cancel and hears nothing more about that pointer. A
// flag would only distinguish a release bubbling up from an interactive
// descendant, and a segmented control has none — its children are the labels.
// That is the same exposure [ButtonView] has had since it was written, and
// inventing a branch for it here would be a branch no test can enter; see
// [disabledIsTheCoresBusiness] for why this package does not do that.
//
// There is no check for the disabled case here either, and its absence is
// deliberate for the same reason.
func (n *segmentedNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventPointerDown:
		ctx.RequestFocus()
		return true
	case gift.EventPointerUp:
		if e.Inside {
			if i := n.segmentAt(ctx.DeviceBounds(), e.Pos.X); i >= 0 {
				n.report(i)
			}
		}
		return true
	case gift.EventKeyDown:
		return n.handleKey(e)
	}
	return false
}

// handleKey moves the selection. A repeat is honoured, so holding an arrow
// walks along the segments and stops at the end instead of wrapping: wrapping
// would make a held key cycle for ever, which is the one behaviour a four
// element control must not have.
//
// # The vertical arrows
//
// Up and left go to the previous segment, down and right to the next. That is
// the axis convention of a list cursor, because that is what the selection of
// a segmented control is: a position among ordered items, where up means
// earlier in every list, menu and table anyone has used.
//
// It is the opposite mapping from [sliderNode.handleKey], where up increases
// the value, and the difference is deliberate; that function's documentation
// carries the argument for both.
func (n *segmentedNode) handleKey(e gift.Event) bool {
	switch e.Key {
	case gift.KeyLeft, gift.KeyUp:
		n.report(n.clampIndex(n.selected - 1))
		return true
	case gift.KeyRight, gift.KeyDown:
		n.report(n.clampIndex(n.selected + 1))
		return true
	case gift.KeyHome:
		n.report(0)
		return true
	case gift.KeyEnd:
		n.report(n.count - 1)
		return true
	}
	return false
}

func (n *segmentedNode) clampIndex(i int) int {
	if i < 0 {
		return 0
	}
	if i >= n.count {
		return n.count - 1
	}
	return i
}

// report calls the callback unless the segment is the one already selected.
func (n *segmentedNode) report(i int) {
	if i == n.selected || n.onChange == nil {
		return
	}
	n.onChange(i)
}

// --- modifiers -------------------------------------------------------------

// Font sets the typeface of every label, replacing the default font.
func (s SegmentedControlView) Font(v Font) SegmentedControlView {
	s.font, s.hasFont = v, true
	return s
}

// FontSize sets the point size of every label.
func (s SegmentedControlView) FontSize(v float32) SegmentedControlView {
	s.fontSize, s.hasSize = v, true
	return s
}

// Label sets the accessible name of the control as a whole — what the choice
// is about, as opposed to the choices, which are the segments' own labels. It
// changes nothing visual; see [gift.Element.Label].
func (s SegmentedControlView) Label(v string) SegmentedControlView { s.name = v; return s }

// Disabled takes the control out of input. It is skipped by the focus order,
// receives no events and draws in its disabled colours.
func (s SegmentedControlView) Disabled(v bool) SegmentedControlView { s.disabled = v; return s }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free; see [frameSpec].
func (s SegmentedControlView) Frame(w, h float32) SegmentedControlView {
	s.setFrame(w, h)
	return s
}

// MinWidth raises the minimum width of the control; see [frameSpec].
func (s SegmentedControlView) MinWidth(v float32) SegmentedControlView {
	s.setMinWidth(v)
	return s
}

// MaxWidth lowers the maximum width of the control; see [frameSpec].
func (s SegmentedControlView) MaxWidth(v float32) SegmentedControlView {
	s.setMaxWidth(v)
	return s
}

// Key sets the reconciliation key of this view among its siblings.
func (s SegmentedControlView) Key(v string) SegmentedControlView { s.setKey(v); return s }

// Flex makes the control take a share of the remaining main axis space of its
// parent stack.
func (s SegmentedControlView) Flex(v float32) SegmentedControlView { s.setFlex(v); return s }
