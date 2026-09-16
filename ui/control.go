package ui

import (
	"fmt"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// This file holds what [ToggleView], [SliderView], [SegmentedControlView] and
// [ProgressBarView] share: the size of a finger, the length of a transition,
// and the two pieces of arithmetic that turn a retained [gift.ControlState]
// into the number a painter draws.

// ControlHitTarget is the smallest edge, in logical pixels, that any control
// in this package offers a finger.
//
// # Why the hit area is not the drawn size
//
// The target of this project is a 1920x1080 touchscreen on a Raspberry Pi.
// A switch drawn 31 logical pixels high is the right *look* — it is what every
// current phone platform draws — and it is a bad *target*: 31 pixels is about
// five millimetres on that panel, and the contact patch of a fingertip is
// closer to nine. So every control in this file lays itself out at least this
// tall and draws its visible parts centred inside that, which means the row of
// pixels above and below the switch is live even though nothing is painted
// there.
//
// Forty-four is the number Apple's Human Interface Guidelines have used for
// fifteen years and Material Design rounds to forty-eight; the difference is
// below the measurement error of a fingertip and the smaller one wastes less
// vertical space in a form.
//
// It is a floor and not a size. A control given a [ToggleView.Frame] larger
// than this keeps the larger bounds, and the whole of those bounds is live.
const ControlHitTarget = float32(44)

// ControlAnimation is how long a control takes to move from one state to the
// other: the travel of a switch knob, the slide of a segmented indicator.
//
// It is a package constant and not a modifier for the reason
// [CaretBlinkInterval] is one: how long a control takes to answer is a
// property of the product, not a decision an individual call site should make
// differently from the control next to it. A design that wants no animation at
// all has nothing to set here — and would not want one control to be the
// exception.
//
// Just under a fifth of a second is the usual figure for a small state
// transition. Below about 100 ms a transition reads as a jump and stops
// carrying information; above about 300 ms it is something the user waits for.
const ControlAnimation = 180 * time.Millisecond

// controlPhase is the interpolated position of a control right now, in [0, 1]
// for a switch and in [0, n-1] for a segmented control.
//
// It is a pure function of the retained state and the clock, which is what
// makes it safe to call from a painter: the painter reads, the layouter and
// the interactor write. Once the window has elapsed it returns the target
// exactly, so a control that has finished animating draws the same pixels for
// ever and the enrolment that was keeping it awake expires; see
// [gift.EventContext.Animate].
func controlPhase(st gift.ControlState, now time.Duration) float32 {
	if !st.Armed {
		return st.Target
	}
	d := now - st.Start
	if d <= 0 {
		return st.From
	}
	if d >= ControlAnimation {
		return st.Target
	}
	return st.From + (st.Target-st.From)*easeInOut(float32(d)/float32(ControlAnimation))
}

// easeInOut is smoothstep: zero slope at both ends, steepest in the middle.
//
// It is the cheapest curve that does not start and stop abruptly, it needs no
// table and no transcendental function, and it is exact at both ends — which
// matters here, because [controlPhase] relies on the value at 1 being 1 and
// not 0.9999 for a control that has come to rest.
func easeInOut(t float32) float32 { return t * t * (3 - 2*t) }

// retarget points the control's animation at v and reports the state to store.
//
// It starts from wherever the control is at this instant rather than from the
// previous target, so interrupting a transition halfway — flicking a switch
// twice — turns round from the middle instead of jumping to the end first.
//
// The first call on a given node adopts v without animating. A control that is
// built switched on must be drawn switched on, not seen to switch itself on
// while the window opens; that is what [gift.ControlState.Armed] is for.
func retarget(st gift.ControlState, v float32, now time.Duration) (gift.ControlState, bool) {
	if !st.Armed {
		st.Armed, st.From, st.Target, st.Start = true, v, v, now
		return st, false
	}
	if st.Target == v {
		return st, false
	}
	st.From = controlPhase(st, now)
	st.Target = v
	st.Start = now
	return st, true
}

// syncControl is the whole of what a control's layouter does about animation:
// notice that the value the last build declared differs from the one being
// animated towards, and start a transition.
//
// The layout pass is the right place and the only available one; see
// [gift.LayoutContext.SetControlState] for why a build cannot do it and a
// paint must not.
func syncControl(ctx *gift.LayoutContext, v float32) {
	st, animate := retarget(ctx.ControlState(), v, ctx.Now())
	ctx.SetControlState(st)
	if animate {
		ctx.Animate(ControlAnimation)
	}
}

// lerpColor mixes two premultiplied colours. Premultiplied is what makes this
// a straight interpolation per channel rather than a conversion; see
// [render.Color].
//
// Both arguments must already be resolved. An unresolved one has a red channel
// of -1 and would mix a role into a colour, which is the failure
// [assertResolved] exists for — so the callers resolve during build and this
// function is never handed a semantic colour.
func lerpColor(a, b Color, t float32) Color {
	// Exact at both ends, and not as an optimisation: without this a switch
	// that is fully on is drawn in the accent plus a rounding error, so a
	// test that compares the emitted colour with the theme's own value fails
	// by one unit in the last place and everybody blames the theme.
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return Color{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A + (b.A-a.A)*t,
	}
}

// fillCapsule emits one fully rounded rectangle, which is the shape every
// control in this file is built out of.
//
// The radius is half the shorter edge, so a wide rectangle is a capsule and a
// square one is a circle. That removes a corner radius knob from four widgets
// at once, for the reason [ScrollBar] gives for not having one: a decoration
// with a knob that has exactly one sensible setting is a knob that lets the
// widgets that share it drift apart.
func fillCapsule(ctx *gift.PaintContext, r geom.Rect, c Color) {
	ctx.Add(render.Op{
		Kind:         render.OpFillRoundRect,
		Bounds:       r,
		Color:        c,
		CornerRadius: capsuleRadius(r),
	})
}

// strokeCapsule is [fillCapsule] for an outline.
func strokeCapsule(ctx *gift.PaintContext, r geom.Rect, b Border) {
	ctx.Add(render.Op{
		Kind:         render.OpStrokeRoundRect,
		Bounds:       r,
		Color:        b.Color,
		CornerRadius: capsuleRadius(r),
		StrokeWidth:  b.Width,
	})
}

func capsuleRadius(r geom.Rect) float32 {
	e := r.Width()
	if h := r.Height(); h < e {
		e = h
	}
	return e / 2
}

// fillRounded emits a rounded rectangle with an explicit radius, for the one
// shape in this file that is not a capsule: the tray and the sliding indicator
// of a [SegmentedControlView].
func fillRounded(ctx *gift.PaintContext, r geom.Rect, radius float32, c Color) {
	ctx.Add(render.Op{
		Kind:         render.OpFillRoundRect,
		Bounds:       r,
		Color:        c,
		CornerRadius: radius,
	})
}

// centeredRect returns a rectangle of size w by h in the middle of outer,
// clamped so that it never leaves outer on either axis.
//
// The clamp is what makes a control that was given a [ToggleView.Frame]
// smaller than its natural size shrink rather than paint outside its own hit
// area, which would be a control that is visible where it cannot be touched.
func centeredRect(outer geom.Rect, w, h float32) geom.Rect {
	if w > outer.Width() {
		w = outer.Width()
	}
	if h > outer.Height() {
		h = outer.Height()
	}
	x := outer.Min.X + (outer.Width()-w)/2
	y := outer.Min.Y + (outer.Height()-h)/2
	return geom.Rc(x, y, x+w, y+h)
}

// clamp01 is the clamp every fraction in this file goes through. It maps NaN
// to zero rather than propagating it, because a NaN fraction reaches the
// backend as a NaN vertex position and empties the window.
func clamp01(v float32) float32 {
	if !(v > 0) { // false for NaN
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// checkFraction rejects a value that cannot be a position on a control.
//
// It runs during build, outside the frame path, for the reason [checkPadding]
// does: an infinite or NaN fraction turns into NaN vertex positions three
// layers below the mistake, and the symptom is an empty window rather than a
// misplaced knob.
func checkFraction(what string, v float64) float64 {
	if v != v || v > 1e38 || v < -1e38 {
		panic(fmt.Sprintf("gift/ui: %s(%v) must be a finite number", what, v))
	}
	return v
}

// strokeOp is [strokeCapsule] with an explicit radius, for the shapes in this
// file that are not capsules.
func strokeOp(r geom.Rect, radius float32, b Border) render.Op {
	return render.Op{
		Kind:         render.OpStrokeRoundRect,
		Bounds:       r,
		Color:        b.Color,
		CornerRadius: radius,
		StrokeWidth:  b.Width,
	}
}

// disabledIsTheCoresBusiness records why no HandleEvent in this file begins
// with a check for the disabled state, and why the one in button.go that does
// is not a model to copy.
//
// gift does not deliver an event to a disabled node at all: App.deliver in gift's input.go
// skips a node whose payload carries Disabled and walks on to its parent, and
// the payload flag is written from the element in the same pass that builds
// the widget, so there is no window in which a widget is disabled and still
// addressed. A guard at the top of a handler is therefore not defence in
// depth, it is a branch no test can enter — and this project's standard is
// that a claim the code makes must have a test that fails when the claim is
// inverted. A guard that cannot fail anything is prose pretending to be code.
//
// Worse, it reads as if the widget looked after the disabled case, and it is
// exactly that reading which hid the defect this comment was written for: a
// control disabled while the finger is still down never receives the
// EventPointerUp that would end its drag, so it cannot clear its own grab, and
// a stale [gift.ControlState.Grabbed] turns the next bare hover into a value
// change. The cleanup belongs where the disabled state is applied, in
// App.applyElement in gift's reconcile.go, and that is where it now is.
//
// It is a constant so that the three handlers can point at one explanation
// instead of repeating it, and so that deleting the explanation breaks the
// build rather than leaving three dangling references.
const disabledIsTheCoresBusiness = "gift does not deliver events to a disabled node"
