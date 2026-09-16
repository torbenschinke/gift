package gift

import (
	"time"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// This file is the fourth "keep repainting for a while" mechanism, after the
// kinetic fling, the indicator linger and [EventContext.Animate], and it is
// the only one a *view* cannot express with the other three.
//
// The problem it solves is stated in one sentence: gift's navigation
// containers switch screens by flipping [Element.Hidden], the flag takes
// effect in the frame it is written, and a flag has no middle. A tab switch
// was therefore a hard cut of the whole window in a single frame — measured
// on the kitchen sink demo at 1,160,664 changed pixels between two
// consecutive frames, with the four frames after it bit-identical.
//
// Three ways of animating that were considered and rejected:
//
//   - the view interpolates and rebuilds every frame. It is what a naive
//     toolkit does and it is out of the question here: a rebuild per frame
//     allocates, and the project plan, section 11, puts the frame path under
//     a zero allocation contract that benchmarks assert.
//   - the view writes [Element.Transform] per frame. Same problem one level
//     down — Transform is a build time declaration behind a pointer, so a
//     value that changes every frame is a rebuild and an allocation per frame,
//     and the field documents exactly that as the reason scrolling does not
//     use it.
//   - a second animation system with callbacks and a scheduler. Rejected
//     because [ControlState] already establishes the shape this project uses
//     for "a number moving towards a target": retained presentation state on
//     the node, a pure function of that state and the clock, and no rebuild.
//
// So a transition is retained presentation state, exactly like a scroll
// offset and a control's phase: it is written when a *build* changes
// [Element.Hidden], it is read as a pure function of [App.BeginInput]'s clock
// by the paint side, and the repaint enrolment that keeps the frames coming
// is one entry in the same list [EventContext.Animate] uses.

// TransitionSpec declares where a node sits while it is hidden, so that
// [Element.Hidden] becomes a movement instead of a cut.
//
// The zero value is the whole of the previous behaviour: a node with no
// transition appears and disappears in one frame.
//
//	// a tab to the right of the selected one waits off the trailing edge
//	e.Transition = gift.TransitionSpec{
//		Parked:   geom.Pt(1, 0),
//		Duration: 180 * time.Millisecond,
//	}
//
// # The model: one number, and where the node is at each end of it
//
// A node carrying a spec has a *phase*: 0 while it is shown, 1 while it is
// hidden, and everything in between while it is moving from one to the other.
// The phase is a pure function of the retained state and the clock — the same
// design as [ControlState] and for the same reasons — so the painter reads it,
// nothing writes it per frame, and two nodes moving at once are exactly in
// step because they read one clock.
//
// Parked is where the node is at phase 1, as a fraction of *its own size*:
// {X: 1} is one full width towards the trailing edge, {X: -0.12} is an eighth
// of a width towards the leading edge, {Y: 1} is one full height down. A
// fraction and not a length, because the thing that moves is a screen and the
// distance that reads as "off the screen" is the size of the screen.
//
// Duration is how long the movement takes. Zero means no transition at all,
// whatever Parked says: a spec is opt-in on both fields.
//
// # What it costs while it runs, and what it costs afterwards
//
// While a transition runs, the node is painted *even though it is hidden*.
// That is the point — a screen that is sliding out has to be on the screen —
// and it is a real, bounded cost: for Duration, an outgoing tab costs a full
// paint of its subtree per frame. When the phase reaches 1 the node stops
// being painted and the guarantee [Element.Hidden] makes is back in force
// unchanged: a hidden tab costs one field read per frame and nothing else.
// Nothing here weakens that guarantee, it only postpones it by Duration.
//
// The repaint enrolment is bounded by Duration and is dropped by
// [App.tickAnimations] like every other one, so a transition always ends and
// the application always goes idle afterwards.
//
// It allocates once per node that has ever transitioned — the retained state
// is behind a pointer so that the nodes that never transition, which is
// almost all of them, carry eight bytes — and nothing per frame.
//
// # Input does not move with the picture, and that is deliberate
//
// [Element.Transform] is documented as shared between the two halves of a
// frame so that input and output cannot disagree about where a node is. This
// is the one transform that is not: the hit test ignores the phase entirely.
// A node that is hidden answers no tap from the first frame of its exit even
// though it is still drawn, and a node that is appearing answers taps at its
// *final* position from the first frame of its entrance even though it is
// still drawn short of it.
//
// The alternative was considered and is worse for the target of this project.
// A kiosk user who taps during a transition is aiming at the screen that is
// arriving, and hit testing the moving picture would mean the target under
// their finger is a different one three frames later — a lottery whose odds
// change during the gesture. The cost of this choice is a window of at most
// Duration in which a tap near the leading edge lands on a control that has
// not visibly arrived there yet. At 180 ms that is measurable and, on a
// touchscreen, preferable to delaying the input or to aiming at a moving
// target. A transition must never make the machine feel slower than the cut
// it replaced.
//
// # Interrupting it
//
// # Two ends and a memory
//
// Parked is where the node goes when it is hidden. Entry is where it comes
// *from* when it is mounted already visible into an application that is
// already on the screen, which is what a navigation push is; the zero Entry
// means "the same place as Parked", which is what every other caller wants.
//
// The two are not the same for a navigation stack and that is the whole
// reason Entry exists: a screen that is pushed arrives from the trailing edge
// and a screen that is *covered* waits a little way towards the leading one,
// so the place a screen came from and the place it will wait are on opposite
// sides of the window. The node remembers which place it is actually parked
// at, so a screen that is uncovered again comes back from the side it went
// to rather than from the side a freshly pushed screen would arrive from.
//
// # Interrupting it
//
// A build that flips [Element.Hidden] back while a transition is running
// turns the node round from wherever it is at that instant rather than
// jumping to the end first; that is [retargetTransition], and it is the rule
// [retarget] already applies to a control whose switch is flicked twice.
type TransitionSpec struct {
	// Parked is the offset of the node while it is hidden, as a fraction of
	// its own size.
	Parked geom.Point

	// Entry is where the node comes from when it is mounted visible into an
	// application that has already drawn a frame. The zero value means
	// Parked.
	Entry geom.Point

	// Return is where the node comes from when it is *shown again* after
	// having been hidden. The zero value means "from where it went", which is
	// the honest answer and is what a tab bar wants.
	//
	// It exists for the one case where the honest answer is unusable: a
	// navigation stack parks a covered screen a full width away, because the
	// screen that covers it has to be able to slide over it without the two
	// overlapping, and a screen that came back from a full width away would
	// leave the whole window empty for the first frame of a pop — the screen
	// that *was* on top is unmounted by then and cannot fill it. A node at
	// phase 1 is not painted at all, so moving where it waits while it is
	// there is invisible by construction.
	Return geom.Point

	// Duration is how long the movement from one end to the other takes.
	// Zero disables the transition.
	Duration time.Duration
}

// IsZero reports whether the spec asks for no movement at all.
func (s TransitionSpec) IsZero() bool { return s.Duration <= 0 }

// transitionState is the retained half of a [TransitionSpec].
//
// It is the same four fields [ControlState] carries for the same job, and it
// is a separate struct rather than a second user of that one because a
// ui.layer is not a control and a control that also transitioned would have
// two animations fighting over one set of fields.
type transitionState struct {
	spec TransitionSpec

	// park is the place this node is actually waiting at, as a fraction of
	// its own size. It is [TransitionSpec.Parked] as of the build that sent
	// the node away, or [TransitionSpec.Entry] for a node that arrived, and
	// it is deliberately *not* re-read from the spec on every build: a
	// screen that is being uncovered has to come back from the side it went
	// to, and by then its spec describes the side a newly pushed screen
	// would arrive from. See [TransitionSpec].
	park geom.Point

	// from is the phase the running movement started at and start is when it
	// started, on the clock [App.BeginInput] is given.
	from  float32
	start time.Duration
	// target is the phase the node is moving towards: 1 while hidden, 0
	// while shown.
	target float32
	// armed reports whether the node has ever been told which end it is at.
	// Without it a node built hidden would animate itself out of view in the
	// first frames of the application, which is the mistake
	// [ControlState.Armed] exists to prevent one level down.
	armed bool
}

// phase returns where the node is right now: 0 shown, 1 hidden.
//
// It is a pure function of the retained state and the clock, which is what
// makes it safe to call from a painter and from a diagnostic at the same
// time, and what makes two screens that are moving past each other agree on
// the frame boundary exactly.
func (t *transitionState) phase(now time.Duration) float32 {
	if t == nil || !t.armed {
		return 0
	}
	d := now - t.start
	switch {
	case d <= 0:
		return t.from
	case t.spec.Duration <= 0 || d >= t.spec.Duration:
		return t.target
	}
	return t.from + (t.target-t.from)*easeInOut(float32(d)/float32(t.spec.Duration))
}

// easeInOut is smoothstep, the same curve and the same argument as
// ui.easeInOut: zero slope at both ends, no table, and exact at both ends so
// that a transition that has finished is at its target and not at 0.9999 of
// it.
//
// It is duplicated rather than shared because package ui imports gift and not
// the other way round, and a four token function is a cheaper duplication
// than an exported one that would then be part of the public surface for
// ever.
func easeInOut(t float32) float32 { return t * t * (3 - 2*t) }

// transPhase is the phase of the node right now, for a node that may have no
// transition at all.
func (nd *nodeData) transPhase(now time.Duration) float32 {
	if nd.trans == nil {
		if nd.hidden {
			return 1
		}
		return 0
	}
	return nd.trans.phase(now)
}

// paintedDespiteHidden reports whether a hidden node still has to be drawn
// because it is on its way out.
func (nd *nodeData) paintedDespiteHidden(now time.Duration) bool {
	return nd.trans != nil && nd.trans.phase(now) < 1
}

// transXform is the translation the current phase puts on the node, or the
// identity when it is at rest. bounds is the node's own rectangle, which is
// what the parked offset is a fraction of.
func (nd *nodeData) transXform(now time.Duration, bounds geom.Rect) (geom.Affine2D, bool) {
	if nd.trans == nil {
		return geom.Identity(), false
	}
	p := nd.trans.phase(now)
	if p <= 0 {
		return geom.Identity(), false
	}
	off := geom.Pt(
		nd.trans.park.X*bounds.Width()*p,
		nd.trans.park.Y*bounds.Height()*p,
	)
	if off.X == 0 && off.Y == 0 {
		return geom.Identity(), false
	}
	return geom.Translate(off), true
}

// applyTransition writes the declared spec onto the node and starts, retargets
// or adopts the movement the build implies.
//
// It is called from [App.applyElement] with the value [Element.Hidden] is
// about to be given, *before* [App.applyHidden] runs, because the enrolment
// has to be taken out after [App.stopHiddenWork] has cleared the subtree's
// other enrolments and not before — see the ordering note there.
//
// mounting says that this node is new. A node that is born hidden adopts
// phase 1 without moving, and a node that is born visible adopts 0 — except
// when the application is already on screen, where a layer appearing in an
// existing tree is a navigation push and has to be seen to arrive. "Already on
// screen" is one drawn frame: at start-up every tab and every screen of the
// demo mounts at once, and an application whose whole window slid in from the
// right on the first frame would be a toy.
func (a *App) applyTransition(h scene.Handle, nd *nodeData, spec TransitionSpec, hidden, mounting bool) {
	if spec.IsZero() {
		// Not merely "do nothing": a node whose spec is taken away while it
		// is moving must stop moving, or it keeps a stale phase for ever and
		// is drawn parked while it believes it is shown.
		nd.trans = nil
		return
	}
	if nd.trans == nil {
		nd.trans = &transitionState{}
	}
	t := nd.trans
	t.spec = spec

	target := float32(0)
	if hidden {
		target = 1
	}
	if !t.armed {
		t.armed = true
		t.target, t.from, t.start = target, target, a.in.now
		t.park = spec.Parked
		if !mounting || hidden || a.diag.Frames == 0 {
			return
		}
		if spec.Entry != (geom.Point{}) {
			t.park = spec.Entry
		}
		// The push. The node is new, visible, and something was already on
		// the screen before it, so it arrives from its parked position
		// rather than simply being there.
		t.from = 1
		t.start = a.in.now
		a.enrolTransition(h, spec.Duration)
		return
	}
	if t.target == target {
		return
	}
	// Turn round from wherever it is, rather than from the end it was aiming
	// at; see [retarget] for the same rule on a control.
	t.from = t.phase(a.in.now)
	if target == 0 && t.from >= 1 && spec.Return != (geom.Point{}) {
		// Coming back from a place nobody could see it in. See
		// [TransitionSpec.Return]; the phase test is what keeps an
		// *interrupted* exit turning round from where it visibly is.
		t.park = spec.Return
	}
	if target == 1 {
		// It is going away, so this build decides where to. A node on its
		// way back keeps the place it was sent to, which is the other half
		// of the rule; see [transitionState.park].
		t.park = spec.Parked
	}
	t.target = target
	t.start = a.in.now
	a.enrolTransition(h, spec.Duration)
}

// enrolTransition keeps the frames coming for the length of a transition.
//
// It is [App.animate] without the two things that would defeat it: it does not
// refuse a node that is hidden — a node sliding *out* is hidden by
// definition, and refusing it is what would make the exit invisible — and it
// is called after [App.stopHiddenWork], which has just removed every
// enrolment inside the subtree including, potentially, an earlier one of
// these.
//
// Everything else is shared: the entry lives in the same list, is ticked by
// the same [App.tickAnimations], is bounded by the same deadline and is
// dropped when the node is unmounted. There is no second mechanism.
func (a *App) enrolTransition(h scene.Handle, d time.Duration) {
	if d <= 0 || !a.store.Valid(h) {
		return
	}
	a.markNeedsPaint(h)
	until := a.in.now + d
	for i := range a.in.anims {
		if a.in.anims[i].node == h {
			if a.in.anims[i].until < until {
				a.in.anims[i].until = until
			}
			return
		}
	}
	a.in.anims = append(a.in.anims, animation{node: h, until: until})
}

// TransitionPhase returns how far the subtree being painted is through a
// transition: 0 at rest, 1 fully parked.
//
// It is the phase of the nearest ancestor that is transitioning, this node
// included, and it is how a painter draws something that has to *fade* rather
// than move — a modal's scrim, which cannot slide with its dialog because it
// covers the window. A painter that only has to move needs nothing from here;
// the movement is applied to its whole subtree by [App.paintNode].
//
// It is zero in every application that declares no transition, which is the
// steady state, and reading it costs one field load.
func (p *PaintContext) TransitionPhase() float32 { return p.app.transPhase }
