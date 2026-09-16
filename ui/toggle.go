package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var toggleType = gift.RegisterType("ui.Toggle")

// Metrics of a switch, in logical pixels.
//
// The proportions are the ones every current phone platform draws, because
// they are the ones a user recognises as a switch rather than as a pill with
// a dot in it: the track is a capsule a little under twice as wide as it is
// tall, and the knob is the track height less a small inset on every side.
//
// None of them is a modifier. A switch that is a different size in two places
// in one application is a design mistake, and a control whose look can be
// resized but whose hit area is fixed at [ControlHitTarget] would invite
// exactly that. A caller who needs a larger target uses [ToggleView.Frame],
// which grows the live area and leaves the drawing where it is.
const (
	toggleTrackWidth  = float32(51)
	toggleTrackHeight = float32(31)
	toggleKnobInset   = float32(2)
	toggleFocusGap    = float32(3)
)

// Default appearance of a switch, in the semantic colours of the project plan,
// section 20, and unresolved so that they follow a later [SetTheme]; see
// button.go for the full argument.
var (
	// defaultToggleOff is the track of a switch that is off. It is the same
	// face every other control at rest uses, which is what makes an off
	// switch read as "a control that is not doing anything" rather than as a
	// second accent.
	defaultToggleOff = ColorControl
	// defaultToggleOn is the track of a switch that is on. The accent is the
	// role whose whole job is "this is the thing that is active".
	defaultToggleOn = ColorAccent
	// defaultToggleKnob is the knob in both states.
	//
	// It is [ColorSurface] — the colour of something raised above the
	// background — and not a literal white, which is what it would have to be
	// to look right in the light theme only. Under the dark theme the surface
	// is a dark slate and sits on a track that is a lighter slate, so the knob
	// would be a hole rather than a cap; the hairline below is what stops
	// that, and it is why the knob has one at all.
	defaultToggleKnob = ColorSurface
	// defaultToggleKnobBorder separates the knob from a track of a similar
	// lightness. See defaultToggleKnob.
	defaultToggleKnobBorder = Border{Width: 1, Color: ColorSeparator}
	// defaultToggleFocusRing is the ring drawn around the track while the
	// switch holds the keyboard focus, outside it by toggleFocusGap so that
	// it reads as a ring and not as a thicker track.
	defaultToggleFocusRing = Border{Width: 2, Color: ColorAccent}
)

// ToggleView is a switch: a two state control that applies its change
// immediately rather than on an "apply" button. It is created by [Toggle]; the
// zero value is not useful.
//
//	ui.Toggle(dark.Get(), func(on bool) { dark.Set(on) }).Label("Dark mode")
//
// # What it is
//
// It is the control of the Human Interface Guidelines for a setting that takes
// effect at once, and it is deliberately not a check box: a check box states a
// value that some later command will act on, a switch *is* the action. gift has
// no check box yet, and when it arrives it will be a different widget with a
// different shape rather than a modifier on this one.
//
// # How it is operated
//
// Tap it, or focus it and press space or enter. Both go through the same
// activation, so a test that drives the keyboard proves the same code path a
// finger does.
//
// A press captures the pointer, like [ButtonView], so a finger that comes down
// on the switch and travels away releases somewhere else and changes nothing.
// A release that is outside, or that gift has classified as a drag, is
// declined. This is the one place where a switch differs from the platform
// switches it is modelled on: it cannot be *dragged* from one side to the
// other. That is a deliberate omission and not an oversight — see the report
// of the work unit that added it — and it costs a gesture that a tap
// substitutes for completely.
//
// Note that [SegmentedControlView] deliberately does *not* decline a dragged
// release, and the difference is known rather than accidental: a segmented
// control's recovery gesture is to slide onto the right segment before
// lifting, which is a drag by definition. The price this widget pays for the
// stricter rule is that [gift.DragSlop] is only eight logical pixels — about
// 1.3 millimetres on a 1920x1080 panel — so a finger that rolls while it lifts
// loses the tap. That is worth revisiting; it has not been changed here
// because relaxing it also makes a swipe that begins and ends on the switch
// flip it, which is a decision about the gesture vocabulary and not a defect
// fix.
//
// # The animation
//
// The knob travels over [ControlAnimation] whenever the value changes, whoever
// changed it: by tap, by key, or by the application assigning the state from a
// timer. That last case is the reason the animation is driven from the
// layouter and not from the event handler; see
// [gift.LayoutContext.SetControlState].
//
// The transition is bounded and stops. A switch that has finished moving
// enrols in nothing, draws the same pixels every frame and lets the backend
// drop to its idle tick rate, which on a kiosk is the difference between a
// machine that sleeps and one that does not.
//
// # The label is not here
//
// A switch has no text of its own. The Human Interface Guidelines put the
// label to the left of the switch in a form row, which in gift is an
// [HStack] the caller writes — and a label inside the control would be a
// second, worse way to write that row. [ToggleView.Label] sets the accessible
// name only and draws nothing; see [gift.Element.Label].
type ToggleView struct {
	base
	on       bool
	onChange func(bool)
	name     string
	disabled bool

	tint    Color
	hasTint bool
}

// Toggle returns a switch showing on and calling onChange with the value the
// user asked for.
//
// The control is *not* stateful: it draws the value it was given and reports
// the one that was asked for, and nothing happens until the application
// stores it and rebuilds. That is the one-way data flow of the project plan,
// section 5, and it is what makes a switch that refuses to move — a setting
// the application declined to change — expressible instead of impossible.
//
// A nil onChange makes the switch inert while still focusable and still
// animating if the value changes underneath it. Prefer [ToggleView.Disabled]
// for a control that is unavailable, because that is the one the user can see.
func Toggle(on bool, onChange func(bool)) ToggleView {
	return ToggleView{on: on, onChange: onChange}
}

// ViewType implements gift.View.
func (t ToggleView) ViewType() gift.TypeID { return toggleType }

// Build implements gift.View.
func (t ToggleView) Build(*gift.BuildContext) gift.Element {
	on := defaultToggleOn
	if t.hasTint {
		on = t.tint
	}
	n := &toggleNode{
		fr:         t.frame,
		on:         t.on,
		onChange:   t.onChange,
		off:        ResolveColor(defaultToggleOff),
		onColor:    ResolveColor(on),
		knob:       ResolveColor(defaultToggleKnob),
		knobBorder: resolveBorder(defaultToggleKnobBorder),
		focusRing:  resolveBorder(defaultToggleFocusRing),
	}
	if t.disabled {
		// Less of the control is the affordance, exactly as for a disabled
		// button; see [defaultStyle]. The off track becomes the disabled
		// face and the on track keeps its hue at a third of its presence, so
		// that a disabled switch still says which way it is pointing — which
		// a uniform grey would not.
		n.off = ResolveColor(ColorControlDisabled)
		n.onColor = ResolveColor(Fade(on, 0.35))
		n.knobBorder.Color = ResolveColor(Fade(ColorSeparator, disabledHairlineFade))
	}
	return gift.Element{
		Key:        t.key,
		Flex:       t.flex,
		Layouter:   n,
		Painter:    n,
		Label:      t.name,
		Interactor: n,
		Focusable:  !t.disabled,
		Disabled:   t.disabled,
	}
}

// toggleNode is the retained half of a [ToggleView].
//
// It holds no phase of its own. The knob position lives in
// [gift.ControlState], in the node payload, because this object is replaced on
// every build and the value changing is exactly what causes a build.
type toggleNode struct {
	fr       frameSpec
	on       bool
	onChange func(bool)

	off, onColor, knob Color
	knobBorder         Border
	focusRing          Border
}

// target is the phase the switch should come to rest at.
func (n *toggleNode) target() float32 {
	if n.on {
		return 1
	}
	return 0
}

// Layout gives the switch the size of its track, raised to the finger sized
// floor of [ControlHitTarget] on both axes, and starts the knob moving if the
// value changed since the last build.
func (n *toggleNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	syncControl(ctx, n.target())
	cc := n.fr.apply(c)
	w, h := toggleTrackWidth, toggleTrackHeight
	if h < ControlHitTarget {
		h = ControlHitTarget
	}
	if w < ControlHitTarget {
		w = ControlHitTarget
	}
	// Never smaller than the track it draws; see [controlSize]. A switch
	// clamped into a zero sized frame draws neither track nor knob, which is
	// the failure mode [SliderView] shipped with.
	return controlSize(ctx, cc, geom.Sz(w, h),
		geom.Sz(toggleTrackWidth, toggleTrackHeight))
}

// Paint draws the track, the knob and, when the switch has the focus, the ring.
//
// The knob is drawn from [gift.PaintContext.ControlState] and the clock, so
// every frame of the transition is a repaint and not a rebuild: no view
// function runs while a switch is moving and [gift.Diagnostics.Builds] does
// not move.
func (n *toggleNode) Paint(ctx *gift.PaintContext) {
	// Before anything is read from the context, because these are the guards
	// of the visibility gates further down and of the colour mix in between;
	// see [assertResolved]. An unresolved colour is transparent, so a switch
	// built from one would lay out, hit test and draw nothing at all.
	assertResolved(n.off, "the off track of a Toggle")
	assertResolved(n.onColor, "the on track of a Toggle")
	assertResolved(n.knob, "the knob of a Toggle")
	assertResolvedBorder(n.knobBorder, "the knob border of a Toggle")
	assertResolvedBorder(n.focusRing, "the focus ring of a Toggle")

	p := clamp01(controlPhase(ctx.ControlState(), ctx.Now()))
	track := n.trackRect(ctx.Bounds())
	fillCapsule(ctx, track, lerpColor(n.off, n.onColor, p))

	knob := n.knobRect(track, p)
	fillCapsule(ctx, knob, n.knob)
	if n.knobBorder.IsVisible() {
		strokeCapsule(ctx, knob, n.knobBorder)
	}

	ia := ctx.Interaction()
	if ia.Focused && !ia.Disabled && n.focusRing.IsVisible() {
		g := geom.InsetsAll(-toggleFocusGap)
		strokeCapsule(ctx, track.Inset(g), n.focusRing)
	}
}

// trackRect is the drawn capsule inside the hit area; see [ControlHitTarget].
func (n *toggleNode) trackRect(b geom.Rect) geom.Rect {
	return centeredRect(b, toggleTrackWidth, toggleTrackHeight)
}

// knobRect is the circle at phase p, where 0 is hard left and 1 hard right.
func (n *toggleNode) knobRect(track geom.Rect, p float32) geom.Rect {
	d := track.Height() - 2*toggleKnobInset
	travel := track.Width() - 2*toggleKnobInset - d
	x := track.Min.X + toggleKnobInset + travel*p
	y := track.Min.Y + toggleKnobInset
	return geom.Rc(x, y, x+d, y+d)
}

// HandleEvent implements gift.Interactor. It is [buttonNode.HandleEvent] with
// one difference: the activation carries the value asked for rather than being
// a bare command, and it is computed from the *declared* value, so two taps in
// one frame cannot both report the same new value.
//
// There is no check for the disabled case here, and its absence is deliberate;
// see [disabledIsTheCoresBusiness].
func (n *toggleNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
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
			// Not on a repeat. gift does not currently synthesise repeats
			// for space or enter — see repeatsWhenHeld in input.go — so this
			// branch is defensive rather than load bearing, and there is
			// deliberately no test claiming otherwise. It is here because the
			// repeat set is gift's to change and a switch that flipped thirty
			// times a second would land on whichever state the release
			// happened to fall in; see [gift.Event.Repeat].
			if !e.Repeat {
				n.activate()
			}
			return true
		}
	}
	return false
}

func (n *toggleNode) activate() {
	if n.onChange != nil {
		n.onChange(!n.on)
	}
}

// --- modifiers -------------------------------------------------------------

// Tint sets the colour of the track while the switch is on, replacing
// [ColorAccent]. It may be a semantic colour and is resolved during build.
//
// There is deliberately no modifier for the off track or for the knob. Those
// two are what makes a switch recognisable as one, and an application that
// wants a different palette for every control in it wants a [Theme].
func (t ToggleView) Tint(v Color) ToggleView { t.tint, t.hasTint = v, true; return t }

// Label sets the accessible name of the switch: what a person would call the
// setting. It changes nothing visual and is never drawn.
//
// A switch needs one far more than a button does. A button usually has a text
// label view underneath it that carries the same string; a switch has no text
// anywhere, so without this there is nothing in the tree that says which
// setting it is. See [gift.Element.Label].
func (t ToggleView) Label(v string) ToggleView { t.name = v; return t }

// Disabled takes the switch out of input. It is skipped by the focus order,
// receives no events and draws in its disabled colours. It still occupies its
// place in the layout and still blocks taps from reaching what is behind it.
func (t ToggleView) Disabled(v bool) ToggleView { t.disabled = v; return t }

// Frame fixes both axes of the *hit area*, not of the drawing: the track keeps
// its size and stays centred. Pass [geom.Unbounded] for an axis that should
// stay free. See [ControlHitTarget].
func (t ToggleView) Frame(w, h float32) ToggleView { t.setFrame(w, h); return t }

// MinWidth raises the minimum width of the hit area; see [frameSpec].
func (t ToggleView) MinWidth(v float32) ToggleView { t.setMinWidth(v); return t }

// MinHeight raises the minimum height of the hit area; see [frameSpec].
func (t ToggleView) MinHeight(v float32) ToggleView { t.setMinHeight(v); return t }

// Key sets the reconciliation key of this view among its siblings.
func (t ToggleView) Key(v string) ToggleView { t.setKey(v); return t }

// Flex makes the switch take a share of the remaining main axis space of its
// parent stack. The drawing stays the size it is and stays centred.
func (t ToggleView) Flex(v float32) ToggleView { t.setFlex(v); return t }
