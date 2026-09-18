package ui

import (
	"fmt"
	"math"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

var sliderType = gift.RegisterType("ui.Slider")

// Metrics of a slider, in logical pixels. See [toggleTrackWidth] for why none
// of them is a modifier.
const (
	sliderTrackHeight = float32(4)
	sliderKnobSize    = float32(28)
	sliderFocusGap    = float32(3)

	// sliderDefaultWidth is how wide a slider makes itself when nothing
	// bounds it — in an [HStack], where rule 1 of the overflow model measures
	// an inflexible child with an unbounded main axis.
	//
	// A slider has no intrinsic width at all: its width *is* its resolution,
	// and there is no content to derive one from. So it is a constant, for
	// the same reason [defaultFieldWidth] is, and a caller who wants another
	// one writes [SliderView.Frame] or [SliderView.Flex].
	sliderDefaultWidth = float32(200)

	// sliderKeyDivisions is how many presses of an arrow key cross the whole
	// range when no [SliderView.Step] was given.
	//
	// Ten, not a hundred. A keyboard step is a coarse adjustment — the fine
	// one is the pointer, which has as many positions as the control has
	// pixels — and a hundred presses to cross a slider is a control the
	// keyboard cannot practically operate.
	sliderKeyDivisions = 10
)

// Default appearance of a slider; unresolved, so that it follows a later
// [SetTheme]. See button.go for the full argument.
var (
	defaultSliderTrack = ColorControl
	// defaultSliderFill is the part of the track between the minimum and the
	// knob. It is the accent because it is the part that carries the value:
	// the eye reads the *length* of it, not the position of the knob.
	defaultSliderFill = ColorAccent
	// defaultSliderKnob and its hairline are the knob of [ToggleView] again,
	// and for the reason given there: a plain [ColorSurface] disc is a hole
	// in the dark theme without an edge.
	defaultSliderKnob       = ColorSurface
	defaultSliderKnobBorder = Border{Width: 1, Color: ColorSeparator}
	defaultSliderFocusRing  = Border{Width: 2, Color: ColorAccent}
)

// SliderView is a continuous control for a value in a range. It is created by
// [Slider]; the zero value is not useful.
//
//	ui.Slider(vol.Get(), vol.Set).Range(0, 11).Step(0.5).Label("Volume")
//
// # How it is operated
//
// Drag the knob, or tap anywhere on the track and the knob comes to the
// finger and can be dragged on from there without lifting it. Or focus it and
// use the arrow keys, which move by [SliderView.Step] or, when none was set,
// by a tenth of the range; home and end go to the ends.
//
// All four arrows work: right and up increase, left and down decrease. See
// [sliderNode.handleKey] for why up is the increasing direction here and the
// decreasing one on a [SegmentedControlView].
//
// # Dragging across a rebuild
//
// A slider reports every step of the drag through its callback, the
// application stores it, and the tree is rebuilt — so a single drag of a
// finger is a continuous stream of rebuilds, each of which throws this view
// and its node object away and makes new ones. Everything the gesture
// remembers therefore lives in [gift.ControlState], in the retained node
// payload, and specifically the offset between the finger and the centre of
// the knob: it is recorded once, when the knob is taken hold of, and never
// recomputed from the value.
//
// That last clause is the whole of it. Review gate 7 of this project found a
// scroll bar thumb that recomputed its grab from the offset it had just
// written, which inverted the drag whenever the tree was rebuilt underneath
// it. A slider rebuilds by construction, so the same mistake here would not be
// an edge case, it would be the normal behaviour.
//
// The knob also takes the pointer with [gift.EventContext.StealPointer] and
// consumes every [gift.EventPointerMove] while it holds it. Without the second
// half, an enclosing [ScrollView] recognises the same movement as a content
// drag and the slider fights the viewport for one finger.
//
// # The value is the caller's
//
// The control draws the value it was given and reports the one that was asked
// for; nothing moves until the application stores it. A slider bound to a
// value the application clamps differently — or refuses — therefore shows the
// truth rather than a knob the model does not agree with.
//
// # No animation
//
// The knob does not ease towards a new value, and that is deliberate: while it
// is being dragged it must be exactly under the finger, and a control that
// eased during a drag would lag behind it. A programmatic jump is therefore a
// jump, which is the same choice [gift.App.ScrollTo] makes and for a related
// reason.
type SliderView struct {
	base
	value    float64
	min, max float64
	step     float64
	onChange func(float64)
	name     string
	disabled bool

	tint    Color
	hasTint bool
}

// Slider returns a slider showing value and calling onChange with the value
// the user asked for. The default range is 0 to 1.
//
// A nil onChange makes the slider inert: it still draws, still focuses and
// still grabs, but nothing the user does can change the value, because the
// value is the application's. Prefer [SliderView.Disabled] for a control that
// is unavailable.
//
// value is not clamped to the range for drawing: a value outside it is drawn
// at the nearest end, but it is reported back unchanged if the user never
// touches the control. Clamping the caller's number on the way in would mean a
// control that silently disagrees with the model it is showing.
//
// It panics on a value that is not a finite number — a NaN or an infinity,
// which is what a division by a total that turned out to be zero produces. The
// diagnosis is here, during the build, and not three layers down where it
// would arrive as a NaN vertex position and empty the window; [SliderView.Range]
// says the same about the ends of the range. See [checkFraction].
func Slider(value float64, onChange func(float64)) SliderView {
	return SliderView{value: checkFraction("Slider", value), max: 1, onChange: onChange}
}

// ViewType implements gift.View.
func (s SliderView) ViewType() gift.TypeID { return sliderType }

// Build implements gift.View.
func (s SliderView) Build(*gift.BuildContext) gift.Element {
	fill := defaultSliderFill
	if s.hasTint {
		fill = s.tint
	}
	n := &sliderNode{
		fr:         s.frame,
		value:      s.value,
		min:        s.min,
		max:        s.max,
		step:       s.step,
		onChange:   s.onChange,
		track:      ResolveColor(defaultSliderTrack),
		fill:       ResolveColor(fill),
		knob:       ResolveColor(defaultSliderKnob),
		knobBorder: resolveBorder(defaultSliderKnobBorder),
		focusRing:  resolveBorder(defaultSliderFocusRing),
	}
	if s.disabled {
		n.track = ResolveColor(ColorControlDisabled)
		n.fill = ResolveColor(Fade(fill, 0.35))
		n.knobBorder.Color = ResolveColor(Fade(ColorSeparator, disabledHairlineFade))
	}
	return gift.Element{
		Key:        s.key,
		Flex:       s.flex,
		Layouter:   n,
		Painter:    n,
		Label:      s.name,
		Interactor: n,
		Focusable:  !s.disabled,
		Disabled:   s.disabled,
	}
}

// sliderNode is the retained half of a [SliderView]. The grab offset is not
// here; see the type documentation of [SliderView].
type sliderNode struct {
	fr       frameSpec
	value    float64
	min, max float64
	step     float64
	onChange func(float64)

	track, fill, knob Color
	knobBorder        Border
	focusRing         Border
}

// Layout gives the slider the width it is offered, or [sliderDefaultWidth]
// when the axis is unbounded, and the finger sized height of
// [ControlHitTarget].
//
// It never returns less than the knob on either axis, whatever
// [SliderView.Frame] asked for, and reports the difference as an overflow; see
// [controlSize]. A slider framed to a height of zero — which is a thing an
// application writes, and which cmd/example-kitchensink wrote — would
// otherwise have a knob clamped to nothing and would be a track a finger can
// find and cannot move.
func (n *sliderNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	cc := n.fr.apply(c)
	w := sliderDefaultWidth
	if cc.HasBoundedWidth() {
		w = cc.Max.W
	}
	h := ControlHitTarget
	if h < sliderKnobSize {
		h = sliderKnobSize
	}
	return controlSize(ctx, cc, geom.Sz(w, h), geom.Sz(sliderKnobSize, sliderKnobSize))
}

// --- geometry ----------------------------------------------------------------

// sliderGeom is the resolved geometry of one slider, in whatever space the
// bounds it was computed from lived in.
//
// It is a value and is returned by value, exactly like [scrollBarGeom]: the
// painter and the interactor keep none of it between frames, so there is no
// cached rectangle that can be stale after a resize, and a drag that spans a
// rebuild recomputes it from bounds that did not change.
type sliderGeom struct {
	// bounds is the whole live area of the control, which is what the knob is
	// kept inside of; see [sliderGeom.knobRect].
	bounds geom.Rect
	track  geom.Rect
	// x0 and x1 are the leftmost and rightmost positions of the knob centre.
	x0, x1 float32
}

// travel is the distance the knob centre can move. It is zero for a slider
// narrower than its own knob, which every caller has to handle.
func (g sliderGeom) travel() float32 { return g.x1 - g.x0 }

// center is the knob centre at fraction f.
func (g sliderGeom) center(f float32) float32 { return g.x0 + g.travel()*f }

// fraction is the inverse: where along the travel x lies, clamped.
func (g sliderGeom) fraction(x float32) float32 {
	t := g.travel()
	if !(t > 0) {
		return 0
	}
	return clamp01((x - g.x0) / t)
}

// knobRect is the knob at fraction f, centred in the control's own bounds and
// never leaving them.
//
// The clamp is [centeredRect] and it is not decoration. [ControlHitTarget]
// promises that a control "draws its visible parts centred inside" its bounds,
// precisely so that nothing of it is visible where it cannot be touched, and a
// slider given a short [SliderView.Frame] — .Frame(200, 10) is the honest
// case, a bar-thin slider in a dense row — would otherwise paint a 28 pixel
// knob nine pixels above and below a ten pixel hit area. [ToggleView] has gone
// through centeredRect since it was written; this is the same rule applied to
// the one control in the package that was missing it.
//
// Horizontally there is nothing to clamp: x0 and x1 are already inset by the
// knob radius, so the knob touches each end exactly.
func (g sliderGeom) knobRect(f float32) geom.Rect {
	cx := g.center(f)
	r := sliderKnobSize / 2
	col := geom.Rc(cx-r, g.bounds.Min.Y, cx+r, g.bounds.Max.Y)
	return centeredRect(col, sliderKnobSize, sliderKnobSize)
}

// geometry resolves the slider over bounds.
func (n *sliderNode) geometry(b geom.Rect) sliderGeom {
	var g sliderGeom
	g.bounds = b
	cy := (b.Min.Y + b.Max.Y) / 2
	g.track = geom.Rc(b.Min.X, cy-sliderTrackHeight/2, b.Max.X, cy+sliderTrackHeight/2)
	r := sliderKnobSize / 2
	g.x0 = b.Min.X + r
	g.x1 = b.Max.X - r
	if g.x1 < g.x0 {
		// A slider narrower than its own knob. The knob is pinned in the
		// middle and the travel is zero, which every caller of travel()
		// handles; the alternative is a negative travel and a knob that
		// moves backwards.
		mid := (b.Min.X + b.Max.X) / 2
		g.x0, g.x1 = mid, mid
	}
	return g
}

// fraction is where the declared value sits in the range, in [0, 1].
func (n *sliderNode) fraction() float32 {
	span := n.max - n.min
	if !(span > 0) && !(span < 0) {
		// A degenerate range. Every value is the same value, so the knob goes
		// to the start rather than to a NaN.
		return 0
	}
	return clamp01(float32((n.value - n.min) / span))
}

// valueAt turns a fraction back into a value in the range, quantised to
// [SliderView.Step].
//
// The quantisation is done in the *value* space and not in the fraction,
// because a step is a property of the quantity the slider stands for — half a
// decibel, one euro — and quantising the fraction would make the reachable
// values depend on the width of the control in pixels.
func (n *sliderNode) valueAt(f float32) float64 {
	v := n.min + float64(f)*(n.max-n.min)
	return n.quantise(v)
}

// quantise snaps v to the nearest multiple of the step above the minimum and
// clamps it to the range.
func (n *sliderNode) quantise(v float64) float64 {
	if n.step > 0 {
		v = n.min + math.Round((v-n.min)/n.step)*n.step
	}
	lo, hi := n.min, n.max
	if hi < lo {
		lo, hi = hi, lo
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// report calls the callback unless the value is the one already declared.
//
// The comparison is against the declared value and not against the last
// reported one, so a drag that moves inside a single step is silent and does
// not make the application rebuild once per pixel for no change.
func (n *sliderNode) report(v float64) {
	if v == n.value || n.onChange == nil {
		return
	}
	n.onChange(v)
}

// Paint draws the track, the filled part, the knob and the focus ring.
func (n *sliderNode) Paint(ctx *gift.PaintContext) {
	// See [toggleNode.Paint]: in front of the gates, not behind them.
	assertResolved(n.track, "the track of a Slider")
	assertResolved(n.fill, "the filled part of a Slider")
	assertResolved(n.knob, "the knob of a Slider")
	assertResolvedBorder(n.knobBorder, "the knob border of a Slider")
	assertResolvedBorder(n.focusRing, "the focus ring of a Slider")

	g := n.geometry(ctx.Bounds())
	f := n.fraction()
	fillCapsule(ctx, g.track, n.track)
	if cx := g.center(f); cx > g.track.Min.X {
		fillCapsule(ctx, geom.Rc(g.track.Min.X, g.track.Min.Y, cx, g.track.Max.Y), n.fill)
	}

	knob := g.knobRect(f)
	fillCapsule(ctx, knob, n.knob)
	if n.knobBorder.IsVisible() {
		strokeCapsule(ctx, knob, n.knobBorder)
	}

	ia := ctx.Interaction()
	if ia.FocusVisible && !ia.Disabled && n.focusRing.IsVisible() {
		strokeCapsule(ctx, knob.Inset(geom.InsetsAll(-sliderFocusGap)), n.focusRing)
	}
}

// HandleEvent implements gift.Interactor.
//
//   - A press takes the focus and records the grab. A press on the knob keeps
//     the offset between the finger and the knob centre, so the knob does not
//     jump; a press anywhere else on the control brings the knob to the finger
//     and continues as a drag from there.
//
// There is deliberately no [gift.EventContext.StealPointer] here, and the
// omission is the considered one rather than the forgotten one. A steal moves
// a capture that somebody else holds; a slider *is* the node the press hit, so
// gift has already given it the capture and every further event of that
// pointer, including the ones outside its own bounds — which is what
// TestASliderKeepsFollowingAFingerThatLeavesItsBounds shows. The scroll bar
// thumb needs a steal because its press lands on top of a gallery tile that
// took it first; nothing is underneath a slider.
//   - A move while grabbed sets the value and is *consumed*, which is what
//     stops an enclosing scroll view reading the same movement as a swipe.
//   - A release or a cancel ends the grab. A cancel is not an undo: the values
//     reported so far were reported and the application has them. There is no
//     "value at the start of the gesture" to restore to, and inventing one
//     would mean this control keeping a copy of the application's state.
//
// There is no check for the disabled case here, and its absence is deliberate;
// see [disabledIsTheCoresBusiness].
func (n *sliderNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	st := ctx.ControlState()
	switch e.Kind {
	case gift.EventPointerDown:
		ctx.RequestFocus()
		g := n.geometry(ctx.DeviceBounds())
		f := n.fraction()
		if g.knobRect(f).Contains(e.Pos) {
			st.Grab = e.Pos.X - g.center(f)
		} else {
			st.Grab = 0
			n.report(n.valueAt(g.fraction(e.Pos.X)))
		}
		st.Grabbed = true
		ctx.SetControlState(st)
		return true

	case gift.EventPointerMove:
		if !st.Grabbed {
			return false
		}
		g := n.geometry(ctx.DeviceBounds())
		n.report(n.valueAt(g.fraction(e.Pos.X - st.Grab)))
		return true

	case gift.EventPointerUp, gift.EventPointerCancel:
		if !st.Grabbed {
			return false
		}
		st.Grabbed, st.Grab = false, 0
		ctx.SetControlState(st)
		return true

	case gift.EventKeyDown:
		return n.handleKey(e)
	}
	return false
}

// handleKey is the keyboard half. Repeats are honoured: holding an arrow on a
// slider is how a keyboard user crosses a range, which is the opposite of the
// rule a button follows; see [gift.Event.Repeat].
//
// # The vertical arrows, and why they point the other way from a segmented control
//
// Up and right increase the value, down and left decrease it, whatever
// direction the slider is drawn in. A slider stands for a *magnitude*, and
// "up is more" is the convention every platform uses for one — a volume
// slider that went quieter when the up arrow was pressed would be wrong in a
// way no documentation could repair.
//
// [segmentedNode.handleKey] maps the same two keys the other way round, and
// that is deliberate rather than an oversight: a segmented control's value is
// a *position in a list*, not a magnitude, and up moves to the earlier item
// there for the same reason it does in every list, menu and table. The two
// widgets are answering different questions with the same key, and each of
// them uses the answer its own question has.
func (n *sliderNode) handleKey(e gift.Event) bool {
	switch e.Key {
	case gift.KeyLeft, gift.KeyDown:
		n.report(n.quantise(n.value - n.keyStep()))
		return true
	case gift.KeyRight, gift.KeyUp:
		n.report(n.quantise(n.value + n.keyStep()))
		return true
	case gift.KeyHome:
		n.report(n.quantise(n.min))
		return true
	case gift.KeyEnd:
		n.report(n.quantise(n.max))
		return true
	}
	return false
}

// keyStep is how far one press of an arrow moves the value: the declared step,
// or a tenth of the range. See [sliderKeyDivisions].
func (n *sliderNode) keyStep() float64 {
	if n.step > 0 {
		return n.step
	}
	return (n.max - n.min) / sliderKeyDivisions
}

// --- modifiers -------------------------------------------------------------

// Range sets the ends of the value range, replacing the default of 0 to 1.
//
// A descending range — a maximum below the minimum — is legal and reverses the
// control: the left end of the track is the minimum, whatever number that is.
// Two equal ends are legal too and produce a slider that cannot move; that is
// what a range of one value means, and it is a state a bound model can pass
// through while it is loading.
//
// Both ends must be finite. An infinite one turns every fraction into a NaN
// and the window goes empty three layers below the mistake.
func (s SliderView) Range(min, max float64) SliderView {
	s.min = checkFraction("Range", min)
	s.max = checkFraction("Range", max)
	return s
}

// Step quantises the value to multiples of v counted from the minimum, for
// the pointer and for the keyboard alike. A step of zero, the default, makes
// the slider continuous at the resolution of the display.
//
// It is also the keyboard increment; see [sliderKeyDivisions] for what happens
// without one.
func (s SliderView) Step(v float64) SliderView {
	if v < 0 || !(v == v) || v > 1e38 {
		panic(fmt.Sprintf("gift/ui: Slider.Step(%v) must be a finite, non negative number", v))
	}
	s.step = v
	return s
}

// Tint sets the colour of the filled part of the track, replacing
// [ColorAccent]. It may be a semantic colour and is resolved during build.
func (s SliderView) Tint(v Color) SliderView { s.tint, s.hasTint = v, true; return s }

// Label sets the accessible name of the slider: what a person would call the
// quantity. It changes nothing visual and is never drawn; see
// [gift.Element.Label].
func (s SliderView) Label(v string) SliderView { s.name = v; return s }

// Disabled takes the slider out of input. It is skipped by the focus order,
// receives no events and draws in its disabled colours.
func (s SliderView) Disabled(v bool) SliderView { s.disabled = v; return s }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free; see [frameSpec].
//
// A height below the natural one is honoured and shrinks the *drawing*: the
// knob is centred in the bounds and clamped to them, so a slider in a
// deliberately thin row stays entirely inside its own hit area rather than
// spilling nine pixels above and below it. See [sliderGeom.knobRect] and
// [ControlHitTarget].
func (s SliderView) Frame(w, h float32) SliderView { s.setFrame(w, h); return s }

// MinWidth raises the minimum width of the slider; see [frameSpec].
func (s SliderView) MinWidth(v float32) SliderView { s.setMinWidth(v); return s }

// MaxWidth lowers the maximum width of the slider; see [frameSpec].
func (s SliderView) MaxWidth(v float32) SliderView { s.setMaxWidth(v); return s }

// Key sets the reconciliation key of this view among its siblings.
func (s SliderView) Key(v string) SliderView { s.setKey(v); return s }

// Flex makes the slider take a share of the remaining main axis space of its
// parent stack, which is the usual way to put one in a row.
func (s SliderView) Flex(v float32) SliderView { s.setFlex(v); return s }
