package gift

import (
	"time"

	"github.com/worldiety/gift/internal/scene"
)

// This file is the third and last of the "keep repainting for a while"
// mechanisms, after the kinetic fling of [App.tickScrolls] and the indicator
// linger of [App.tickIndicators]. It exists because a view that animates
// itself needs two things gift did not offer, and neither of them can be
// faked from the outside:
//
//   - a clock during paint. A painter is handed no time at all, and the only
//     clock in the process that a test can move is the one
//     [App.BeginInput] is given. [PaintContext.Now] is that clock.
//   - a reason for the backend to keep calling. gift redraws the whole
//     visible list every frame, so an animation changes no pixel by itself;
//     what it needs is [App.NeedsPaint] to stay true, because that is what
//     the backend's idle tick policy reads. [EventContext.Animate] is that.
//
// The first consumer is the blinking caret of ui.TextField, and the shape is
// taken from the indicator linger rather than invented: an enrolment with a
// deadline, ticked once in BeginInput, compacted in place, dropped when the
// node is unmounted. The deadline is the part that matters — an animation
// that never expires would hold an idle kiosk at its full tick rate for ever,
// which is the one failure mode a "please keep drawing" API has.

// animation is one node that asked to be repainted until a deadline.
type animation struct {
	node  scene.Handle
	until time.Duration
}

// Animate keeps the receiving node marked for repaint for the next d, on the
// clock [App.BeginInput] is given.
//
// # What it does and does not promise
//
// It does not run anything. There is no interpolation, no easing and no
// callback: gift marks the node for paint on every tick inside the window and
// the node's painter decides what to draw from [PaintContext.Now]. That keeps
// the whole mechanism to one deadline per animating node and keeps the
// arithmetic of an animation in the view that owns it, which is where a
// designer can read it.
//
// The window is a *deadline and not a duration left to run*: calling it again
// with the same d before the first one expires extends the window rather than
// queueing a second one, so a handler may call it on every event without
// counting. A d of zero or less cancels the enrolment, and that is how a view
// that lost the focus stops asking — one repaint still happens, so that the
// frame in which the caret disappears is actually drawn.
//
// # Why a deadline is mandatory
//
// Because the alternative is an idle application that never sleeps. The
// backend drops to Config.IdleTPS when nothing needs painting; an enrolment
// without an end would defeat that for the lifetime of the node, on a kiosk
// that may sit untouched for hours. [ScrollIndicatorLinger] makes the same
// trade for the same reason and states the same cost. A view whose animation
// is genuinely unbounded — a spinner — re-arms from its own handler or
// accepts that it stops; a blinking caret stops, on purpose, because a caret
// that has blinked for a minute has said everything it has to say and the
// user is not looking.
//
// # An enrolment does not outlive its reason
//
// It used to. A node that was animating because it held the focus, and that
// declared itself [Disabled] or was hidden in the very build in which it still
// held it, was never told [EventFocusLost]: [App.setFocus] dropped the
// notification when it ran during a build. The node therefore never called
// Animate(0), and its enrolment stood until the deadline it last asked for.
// Measured with ui.TextField: hiding the tab of a focused field cost 100 of
// 120 sampled ticks — the full ten second window at 60 Hz, drawing no caret —
// where an ordinary blur of the same field cost none.
//
// The notification is now deferred rather than dropped, and delivered at the
// first point in the update where an application handler may run; see
// pending.go. It reaches a disabled node too, through [App.notifyDirect],
// because the node is the only thing in the process that can let go of what it
// is holding. On top of that, [App.stopHiddenWork] ends every enrolment inside
// a subtree that becomes hidden, and [App.animate] refuses a new one for a
// node nobody can see — which is the half that matters for ui.Toggle, whose
// enrolment is re-armed from its *layouter* and would otherwise come straight
// back on the next pass.
//
// The deadline is still mandatory, and for the reason above rather than as a
// backstop: a view that re-arms for ever from its own handler is still allowed
// to, and the deadline is what bounds everything else.
//
// It allocates once per node that has ever animated, and nothing afterwards:
// the enrolment list is a reused slice compacted in place, exactly like
// [inputState.flings] and [inputState.indicators].
func (c *EventContext) Animate(d time.Duration) { c.app.animate(c.cur, d) }

// ScrollIntoView adjusts every scroll container above the receiving node so
// that the node is visible, and reports whether anything moved.
//
// It is [App.ScrollIntoView] for the node whose handler is running, and it
// exists because an interactor has no [NodeRef] of itself and no application
// handle: a text field that takes the focus deep inside a scrolling form has
// to be able to say "show me" without the application wiring a reference
// through. The movement is a jump, not an animation; see [App.ScrollTo].
func (c *EventContext) ScrollIntoView() bool { return c.app.ScrollIntoView(NodeRef{c.cur}) }

// Now returns the timestamp of the input phase this frame was drawn after, on
// the clock [App.BeginInput] is given.
//
// It is the only clock a painter may read. Reading the wall clock instead
// would make every animation untestable — the project plan, section 13,
// requires deterministic time and gifttest moves this one — and would make two
// painters in one frame disagree about what "now" is.
//
// A painter that uses it is almost certainly also a caller of
// [EventContext.Animate]; without that, the value only changes when something
// else in the application causes a frame.
func (p *PaintContext) Now() time.Duration { return p.app.in.now }

// animate is the body of [EventContext.Animate].
func (a *App) animate(h scene.Handle, d time.Duration) {
	if !a.store.Valid(h) {
		return
	}
	// The cancelling call is honoured first and unconditionally. It is how a
	// node that lost the focus stops asking, and the node it comes from is
	// very often one that has just been hidden — that is the whole point of
	// delivering [EventFocusLost] to it; see pending.go. Refusing the cancel
	// because the node is hidden would leave the enrolment standing for
	// exactly the case the refusal below exists to prevent.
	if d <= 0 {
		a.markNeedsPaint(h)
		a.stopAnimating(h)
		return
	}
	// A node nobody can see does not get to keep the device awake, and this
	// is not merely the mirror of [App.stopHiddenWork] — it is the half that
	// a layouter needs. ui.Toggle and ui.SegmentedControl call Animate from
	// their *layouter*, and a hidden subtree is still laid out: without this,
	// a state write into an inactive tab would enrol the control afresh on
	// every layout pass, and stopping the enrolment at the moment of hiding
	// would achieve nothing.
	if a.hiddenAbove(h) {
		return
	}
	// One repaint in every case, including the cancelling one: the frame that
	// ends an animation has to be drawn, or the last state the animation was
	// in stays on the screen until something unrelated happens.
	a.markNeedsPaint(h)
	until := a.in.now + d
	for i := range a.in.anims {
		if a.in.anims[i].node == h {
			a.in.anims[i].until = until
			return
		}
	}
	a.in.anims = append(a.in.anims, animation{node: h, until: until})
}

func (a *App) stopAnimating(h scene.Handle) {
	for i, an := range a.in.anims {
		if an.node == h {
			a.in.anims = append(a.in.anims[:i], a.in.anims[i+1:]...)
			return
		}
	}
}

// tickAnimations marks every animating node for repaint and drops the ones
// whose window has closed.
//
// It runs in [App.BeginInput] next to [App.tickIndicators], allocates nothing
// and does no work at all when nothing animates: the length check is the first
// statement and the empty list is the steady state of every application that
// has no caret on screen.
func (a *App) tickAnimations(now time.Duration) {
	if len(a.in.anims) == 0 {
		return
	}
	out := a.in.anims[:0]
	for _, an := range a.in.anims {
		if !a.store.Valid(an.node) {
			continue
		}
		// The expiring tick is marked as well, so the first frame in which
		// the animation is over is drawn once. After that the node is gone
		// from the list and the application is allowed to go to sleep.
		a.markNeedsPaint(an.node)
		if now < an.until {
			out = append(out, an)
		}
	}
	a.in.anims = out
}
