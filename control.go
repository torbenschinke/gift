package gift

import "time"

// ControlState is the retained presentation state of one control: the gesture
// it is in the middle of, and the animation it is in the middle of.
//
// # Why it lives here and not in the view or in the node object
//
// It is the same argument [ScrollIndicatorState] makes, applied to a widget
// that rebuilds far more often than a scroll container does. A control that
// reports its value through a callback causes a rebuild on every step of the
// gesture *by construction*: dragging the knob of a ui.Slider is a continuous
// stream of rebuilds, and a rebuild constructs a fresh layouter, painter and
// interactor object and installs it. Anything the gesture remembers in that
// object is therefore gone between two moves of the same finger.
//
// The consequence of losing it is worse than losing the gesture. The pointer
// capture and the [Event.Dragged] flag belong to the dispatcher and survive,
// so the next move still arrives — at a handler whose grab offset has been
// reset to zero. The knob then jumps to the finger, and if the offset is
// instead recomputed from the *current* value the drag inverts, which is the
// defect review gate 7 of this project found in the scroll bar thumb. So the
// grab offset is recorded once, at the press, in the one place that outlives
// a build.
//
// # Why the fields are this generic
//
// Because the alternative is a payload field per widget in gift's own node
// data, and gift must not grow a field every time package ui gains a control.
// They are named for what a control does with them rather than for one
// widget: a grab is a pointer gesture in progress, and a phase is a number
// being interpolated towards a target. That is deliberately the same trade
// [ScrollIndicatorState] makes, which carries Grabbed and Grab for exactly one
// caller.
//
// The zero value is the state of a control that has never been touched: no
// grab, no animation, and [ControlState.Armed] false so that the first value a
// control sees is adopted without animating towards it from zero.
type ControlState struct {
	// Grabbed reports whether this control has taken hold of a pointer and is
	// dragging something.
	Grabbed bool

	// Grab is the offset, along the control's own axis and in the device
	// space of [Event.Pos], between the point the gesture started at and the
	// leading edge of the thing being dragged.
	//
	// It is written once, when the grab is taken, and read on every move. It
	// is never recomputed from the control's value; see the type
	// documentation for what happens when it is.
	Grab float32

	// Target is the value the control is animating towards, normally in
	// [0, 1]. It is the value the last build declared.
	Target float32

	// From is the value the running animation started at, and Start the
	// timestamp it started at, on the clock [App.BeginInput] is given.
	From  float32
	Start time.Duration

	// Armed reports whether Target has ever been written.
	//
	// It separates "the control is at zero" from "the control has not been
	// built yet", and without it every control that is born switched on would
	// animate from off to on in the first frames of the application.
	Armed bool
}

// ControlState returns the control state of the node receiving the event.
func (c *EventContext) ControlState() ControlState { return c.nd.control }

// SetControlState writes the control state of the node receiving the event and
// marks it for repaint when it changed.
//
// It never rebuilds and never relayouts: this is presentation state, and the
// project plan, section 5, requires presentation state not to force a build.
func (c *EventContext) SetControlState(v ControlState) {
	if v == c.nd.control {
		return
	}
	c.nd.control = v
	c.app.markNeedsPaint(c.cur)
}

// ControlState returns the control state of the node being painted.
func (p *PaintContext) ControlState() ControlState { return p.nd.control }

// ControlState returns the control state of the node being laid out.
func (l *LayoutContext) ControlState() ControlState { return l.nd.control }

// SetControlState writes the control state of the node being laid out.
//
// # Why a layouter may write it and a painter may not
//
// Because the layout pass is the one moment at which a control can notice
// that a *build* changed its value. A build produces a fresh view and a fresh
// node object, and neither of them can read what the previous value was — that
// is the whole reason this state exists — so the comparison has to happen
// somewhere that sees both the new declaration and the retained state.
// [App.applyElement] marks every rebuilt node for layout, so the layouter runs
// exactly when a value may have changed, and it runs before the frame in which
// the new value would be drawn.
//
// A painter must not, and the prohibition is not a convention: [Painter.Paint]
// is documented as free of side effects, it runs with the display list open,
// and a painter that mutated state would make the picture depend on the order
// in which nodes happen to be visited. A painter reads this value and
// interpolates from it; see [PaintContext.Now].
//
// It marks nothing dirty. A layouter that starts an animation says so with
// [LayoutContext.Animate], which is the call that keeps the frames coming.
func (l *LayoutContext) SetControlState(v ControlState) { l.nd.control = v }

// Animate keeps the node being laid out marked for repaint for the next d, on
// the clock [App.BeginInput] is given. It is [EventContext.Animate] for a
// layouter, and every word of that documentation applies — in particular that
// d is a deadline and that an animation without one never lets the machine
// sleep again.
//
// A layouter needs it because a value may change without any event at all: an
// application that sets a toggle from a timer, from [App.Post] or from a
// network reply rebuilds the control, and the control has to animate towards
// the new value just as it does when a finger moved it. The layout pass is
// where that change becomes visible to the retained side; see
// [LayoutContext.SetControlState].
func (l *LayoutContext) Animate(d time.Duration) { l.app.animate(l.node, d) }

// Animate keeps the node being painted marked for repaint for the next d.
//
// # This is the unbounded case, and it is the only one
//
// [EventContext.Animate] says that an animation whose end is not known "re-arms
// from its own handler or accepts that it stops". A determinate control has a
// handler: a toggle animates because something toggled it. An *indeterminate*
// progress indicator has none — nothing happens to it at all, which is the
// entire message it exists to convey — so the only place it can re-arm from is
// the frame it just drew.
//
// The cost is exactly what the deadline elsewhere exists to prevent: a node
// that calls this on every frame holds the backend at its full tick rate for
// as long as it is on screen. That is acceptable only because it is what the
// widget *means*. An indeterminate indicator that is left on screen after the
// work finished is an application defect with a measurable price, and
// ui.ProgressBar says so in as many words.
//
// It is a deadline like every other enrolment, so a node that stops calling it
// stops being drawn at the full rate one window later, and a node that is
// unmounted is dropped by [App.tickAnimations] on the next tick.
//
// # How a call from inside a paint can possibly work
//
// It cannot work the way the other Animate methods do, and the difference is
// load bearing rather than incidental. Like them it calls [App.markNeedsPaint]
// on the node — but this one runs *during* [App.Paint], and the last thing
// App.Paint does before returning the display list is set a.needsPaint back to
// false. So the dirty mark a painter makes is thrown away without exception,
// every single time.
//
// What survives is the enrolment: the node and its deadline are appended to
// a.in.anims, and the next [App.BeginInput] runs App.tickAnimations, which
// marks every node whose deadline has not passed. That is the whole mechanism,
// and it is why d matters here even though this method is called again on the
// very next frame — a painter that stops calling it goes quiet once the last
// window it asked for has elapsed, not immediately.
//
// The consequence for a caller is that this never repaints the frame it is
// called from, only the ones after it, and that it is useless outside a
// backend that calls BeginInput on a tick.
//
// There is no paint culling in gift: [App.Paint] descends into every mounted
// node, so "while it is being drawn" and "while it is mounted" are the same
// window. A node that is scrolled out of the viewport keeps painting and keeps
// re-arming; see ui.ProgressBarView.
func (p *PaintContext) Animate(d time.Duration) { p.app.animate(p.cur, d) }

// Now returns the timestamp of the input phase this layout pass belongs to,
// on the clock [App.BeginInput] is given. It is [PaintContext.Now] for a
// layouter.
//
// A layouter that reads it is one that starts animations; see
// [LayoutContext.SetControlState]. Nothing else in a layout may depend on the
// clock, because a layout that changes with time and is not driven by an
// enrolment would be a size that moves under a tree nobody asked to re-measure.
func (l *LayoutContext) Now() time.Duration { return l.app.in.now }
