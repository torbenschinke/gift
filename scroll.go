package gift

import (
	"math"
	"time"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
)

// ScrollAxis is the single axis a scroll container moves along.
//
// One axis and not two. A two dimensional scroller needs a second offset, a
// second set of bounds and a rule for what a diagonal fling does, and the
// gallery of the project plan, section 10, is a column layout that scrolls one
// way. Two nested containers express the two dimensional case honestly.
type ScrollAxis uint8

const (
	// ScrollNone is the zero value and declares no scrolling at all.
	ScrollNone ScrollAxis = iota
	// ScrollVertical moves the content along y. The offset grows downwards:
	// an offset of 100 shows the content that starts 100 logical pixels
	// below the top of the document.
	ScrollVertical
	// ScrollHorizontal moves the content along x, with the offset growing
	// towards the trailing edge.
	ScrollHorizontal
)

// String returns "none", "vertical" or "horizontal".
func (a ScrollAxis) String() string {
	switch a {
	case ScrollVertical:
		return "vertical"
	case ScrollHorizontal:
		return "horizontal"
	default:
		return "none"
	}
}

// of returns the component of p along the axis.
func (a ScrollAxis) of(p geom.Point) float32 {
	if a == ScrollHorizontal {
		return p.X
	}
	return p.Y
}

// ofSize returns the component of s along the axis.
func (a ScrollAxis) ofSize(s geom.Size) float32 {
	if a == ScrollHorizontal {
		return s.W
	}
	return s.H
}

// Default scroll gesture constants.
//
// They are variables rather than constants so that an application can retune
// the feel globally, and every one of them can also be overridden per
// container through [ScrollConfig]; the project plan asks for adjustable
// values rather than magic numbers in the middle of the gesture code.
//
// # The friction model
//
// A fling decays exponentially: v(t) = v0 * exp(-k*t), where k is
// [DefaultScrollFriction] in reciprocal seconds. Integrating it gives the
// distance travelled in one tick, v0 * (1 - exp(-k*dt)) / k, which is what
// [App.BeginInput] applies — the closed form and not v*dt, so the result does
// not depend on the tick rate. The total distance of an undamped fling is
// v0/k, so a flick at 2000 px/s with k = 4 travels 500 logical pixels and is
// visually over after about 700 ms.
//
// Exponential decay rather than a constant deceleration because it has no
// discontinuity at the end: the velocity approaches zero smoothly and the
// fling is ended by [DefaultScrollStopVelocity] rather than by the curve
// crossing zero, which is what makes a stop look like a stop rather than a
// stall.
var (
	// DefaultScrollFriction is the decay rate k of a fling, in 1/s.
	DefaultScrollFriction float32 = 4
	// DefaultScrollFlingVelocity is how fast the pointer has to be moving on
	// release, in logical pixels per second, before a fling starts at all.
	// Below it the release is a stop.
	DefaultScrollFlingVelocity float32 = 120
	// DefaultScrollStopVelocity is the speed at which a running fling is
	// considered finished, in logical pixels per second.
	DefaultScrollStopVelocity float32 = 20
	// DefaultScrollMaxVelocity caps the fling speed, in logical pixels per
	// second. A velocity estimate over a very short window can be absurd;
	// this is what keeps a jittery sample from launching the content into
	// the far end of the document.
	DefaultScrollMaxVelocity float32 = 8000
	// DefaultScrollWheelStep is how far one unit of [Event.Delta] of an
	// [EventWheel] scrolls, in logical pixels.
	DefaultScrollWheelStep float32 = 48
)

// maxFlingStep bounds the time step of one fling tick.
//
// A stalled frame — a garbage collection, a window drag, a debugger — hands
// [App.BeginInput] a delta of hundreds of milliseconds, and without this bound
// the content would teleport. Clamping is the right answer rather than
// subdividing: the gesture is a physical simulation of something the user let
// go of, not a recording that has to stay in sync.
const maxFlingStep = 100 * time.Millisecond

// ScrollConfig tunes the gesture behaviour of one scroll container. A zero
// field takes the corresponding package default.
type ScrollConfig struct {
	// Friction is the exponential decay rate of a fling in 1/s; see the
	// documentation of [DefaultScrollFriction].
	Friction float32
	// FlingVelocity is the release speed below which no fling starts.
	FlingVelocity float32
	// StopVelocity is the speed at which a fling ends.
	StopVelocity float32
	// MaxVelocity caps the fling speed.
	MaxVelocity float32
	// WheelStep is the distance one unit of wheel delta scrolls.
	WheelStep float32
}

func (c ScrollConfig) withDefaults() ScrollConfig {
	if !(c.Friction > 0) {
		c.Friction = DefaultScrollFriction
	}
	if !(c.FlingVelocity > 0) {
		c.FlingVelocity = DefaultScrollFlingVelocity
	}
	if !(c.StopVelocity > 0) {
		c.StopVelocity = DefaultScrollStopVelocity
	}
	if !(c.MaxVelocity > 0) {
		c.MaxVelocity = DefaultScrollMaxVelocity
	}
	if !(c.WheelStep > 0) {
		c.WheelStep = DefaultScrollWheelStep
	}
	return c
}

// ScrollSpec declares a node to be a scroll viewport. It is [Element.Scroll].
type ScrollSpec struct {
	// Axis is the axis the content moves along. [ScrollNone] declares no
	// viewport at all and is rejected at build time.
	Axis ScrollAxis
	// Config tunes the gesture constants of this container.
	Config ScrollConfig

	// Virtual declares that this container's layouter *produces* its content
	// from the scroll offset instead of merely being translated by it.
	//
	// # What it changes
	//
	// An ordinary scroll container lays its children out once and then moves
	// them with a matrix; changing the offset marks the node for repaint and
	// nothing else, which is the whole claim of the scroll fast path. A
	// virtualising container — the gallery of the project plan, section 10 —
	// has no node for most of its content: it decides on every offset which
	// of its bounded set of tiles stands for which item, and that decision is
	// made in [Layouter.Layout] through [LayoutContext.ScrollOffset]. For
	// such a container an offset change must invalidate *layout*, or the
	// tiles would keep standing for the items they stood for at the offset
	// the last layout saw.
	//
	// So: with Virtual set, [App.setScroll] marks the node for layout rather
	// than only for paint. Nothing is rebuilt either way — no view function
	// runs, [Diagnostics.Builds] does not move — and the layout that follows
	// descends along the marked path only, so it costs the depth of the tree
	// plus this one layouter. That is the honest price of virtualisation and
	// it is measured in TestGalleryScrollPathCost.
	//
	// # Why it is opt in
	//
	// Because relayouting on every wheel notch is exactly what an ordinary
	// scroller must not do, and a default that did it would quietly undo the
	// property ui.ScrollView documents and gifttest asserts. A container that
	// does not read [LayoutContext.ScrollOffset] has nothing to gain from it.
	Virtual bool
}

// velSamples is the size of the velocity ring. Eight samples at sixty hertz
// span 133 ms, which is more than [velWindow] needs.
const velSamples = 8

// velWindow is how far back the release velocity is estimated over.
//
// Short enough that the number describes the flick and not the whole drag —
// a user who drags slowly for a second and then flicks expects the flick —
// and long enough that two consecutive samples of a jittery digitiser do not
// decide it alone.
const velWindow = 100 * time.Millisecond

type velSample struct {
	t time.Duration
	p float32
}

// scrollState is the retained presentation state of one scroll container.
//
// It lives in the node payload, next to [Interaction], and for the same
// reason: the project plan, section 5, puts the scroll offset in local
// presentation state that must not force a rebuild. Nothing here is ever read
// by a view function, and writing the offset marks the node for repaint and
// nothing else — in particular not for layout, which is the property the whole
// gallery performance argument rests on.
//
// # Document coordinates
//
// off, content and origin are float64. The project plan, section 10, requires
// that very large document positions are converted to float32 only after the
// viewport origin has been subtracted, and float32 carries 24 bits of
// mantissa: above 16.7 million a float32 cannot represent consecutive integers
// at all, and a 100 000 item gallery reaches tens of millions of logical
// pixels. Keeping the offset in float64 is what makes "scroll by one pixel at
// document position 30 000 000" a movement rather than a rounding error.
//
// origin is the document coordinate that local coordinate zero of the children
// stands for. For ordinary content — a column of real nodes — it is zero and
// the pushed translation is -off, which is exact as long as the content itself
// fits in float32. A *virtualising* layouter reports an origin near the
// current offset and places the visible items relative to it, so the
// translation it produces is a small number and the float32 conversion happens
// after the subtraction, exactly as the plan requires. See
// [LayoutContext.ReportScrollContent].
type scrollState struct {
	axis ScrollAxis
	cfg  ScrollConfig

	// virtual is [ScrollSpec.Virtual]: an offset change invalidates the
	// layout of this node instead of only its paint.
	virtual bool

	// off is the document coordinate shown at the leading edge of the
	// viewport, clamped to [0, maxOffset].
	off float64
	// content is the document extent along the axis and origin the document
	// coordinate of local zero; both come from the layouter.
	content float64
	origin  float64
	// viewport is the extent of the viewport along the axis, taken from the
	// size the node reported in the last layout.
	viewport float32

	// vel is the current fling velocity in document units per second, and
	// flinging says whether a fling is running. lastTick is the clock value
	// the fling was last advanced to.
	vel      float32
	flinging bool
	lastTick time.Duration

	// dragging says whether this container is currently driving a pointer
	// drag, which is also what says it has taken the press.
	dragging bool

	samples [velSamples]velSample
	nsam    int
}

// maxOffset is the largest legal offset.
//
// When the content is smaller than the viewport it is zero, so the offset is
// pinned at zero and the content sits at the leading edge of the viewport. It
// is deliberately not centred and not stretched: a container whose content
// shrinks below the viewport and then grows again would otherwise jump twice.
func (s *scrollState) maxOffset() float64 {
	m := s.content - float64(s.viewport)
	if !(m > 0) {
		return 0
	}
	return m
}

func (s *scrollState) clamp(v float64) float64 {
	if !(v > 0) {
		// Also catches NaN, which must never enter a transform.
		return 0
	}
	if m := s.maxOffset(); v > m {
		return m
	}
	return v
}

// canMove reports whether moving by d would change the offset at all. It is
// the predicate behind the overscroll chaining rule; see [scrollHandler].
func (s *scrollState) canMove(d float64) bool {
	return s.clamp(s.off+d) != s.off
}

// xform is the translation this container applies to its children.
//
// The float32 conversion happens once, here, and on the *difference* between
// the offset and the content origin. See the type documentation.
func (s *scrollState) xform() geom.Affine2D {
	d := float32(s.origin - s.off)
	if s.axis == ScrollHorizontal {
		return geom.Translate(geom.Pt(d, 0))
	}
	return geom.Translate(geom.Pt(0, d))
}

func (s *scrollState) resetTrack() { s.nsam = 0 }

func (s *scrollState) track(t time.Duration, p float32) {
	if s.nsam == len(s.samples) {
		copy(s.samples[:], s.samples[1:])
		s.nsam--
	}
	s.samples[s.nsam] = velSample{t: t, p: p}
	s.nsam++
}

// velocity estimates the pointer speed in logical pixels per second over the
// last [velWindow] of samples, or zero when there is not enough to go on.
func (s *scrollState) velocity() float32 {
	if s.nsam < 2 {
		return 0
	}
	last := s.samples[s.nsam-1]
	first := last
	for i := s.nsam - 2; i >= 0; i-- {
		if last.t-s.samples[i].t > velWindow {
			break
		}
		first = s.samples[i]
	}
	dt := (last.t - first.t).Seconds()
	if dt <= 0 {
		return 0
	}
	return float32(float64(last.p-first.p) / dt)
}

// --- the built in interactor -------------------------------------------------

// scrollHandler is the [Interactor] gift installs on every node that declared
// an [Element.Scroll] and brought no interactor of its own.
//
// It is a stateless empty struct: everything it touches lives in the node
// payload, so there is exactly one instance for the whole process and
// declaring a scroll container costs no allocation on the input side.
//
// # Overscroll chaining
//
// A wheel event is delivered to the node under the pointer and bubbles
// upwards; see [App.PointerWheel]. The rule this handler implements is:
//
//   - A container consumes the event when the requested movement would change
//     its offset at all, and it then consumes the *whole* event even if it can
//     only move part of the way. Splitting a wheel notch between two nested
//     scrollers looks like a stuck scrollbar and is what browsers deliberately
//     avoid.
//   - A container that is already at the limit in the requested direction, or
//     whose axis the event has no component along, does not consume it. The
//     event bubbles to the next scrollable ancestor, which is the behaviour a
//     reader expects at the end of a nested list.
//   - The rule is per direction and evaluated per event. A container at the
//     bottom still consumes a scroll upwards.
//
// Drag follows the same rule at the moment the drag is claimed, and not
// afterwards: once a container owns the gesture it keeps it until the release,
// because handing a finger over to another scroller half way through a stroke
// is worse than stopping at the end.
type scrollHandler struct{}

func (scrollHandler) HandleEvent(ctx *EventContext, e Event) bool {
	s := ctx.nd.scroll
	if s == nil {
		return false
	}
	a, h := ctx.app, ctx.cur

	switch e.Kind {
	case EventWheel:
		d := s.axis.of(e.Delta)
		if d == 0 {
			return false
		}
		// Positive delta is the wheel pushed away from the user, which moves
		// towards the beginning of the document; see [Event.Delta].
		step := -float64(d) * float64(s.cfg.WheelStep)
		if !s.canMove(step) {
			return false
		}
		a.stopFling(s)
		a.setScroll(h, s, s.off+step)
		return true

	case EventPointerDown:
		// A press stops a running fling and nothing else. It is deliberately
		// not consumed: a press belongs to whatever is underneath until the
		// pointer has travelled further than [DragSlop], which is what makes
		// a tap on a button inside a scroller a tap.
		if s.flinging {
			a.stopFling(s)
			return true
		}
		return false

	case EventPointerMove:
		if !e.Dragged {
			// Still inside the slop. The gesture may yet become a tap.
			return false
		}
		d := s.axis.of(e.Delta)
		if !s.dragging {
			if d == 0 || !s.canMove(-float64(d)) {
				// Nothing to give: let an outer scroller take the drag.
				return false
			}
			// Take the press away from whoever holds it. A drag that started
			// on a button inside the scroller must not leave that button
			// pressed and must not activate it on release.
			ctx.StealPointer()
			s.dragging = true
			s.resetTrack()
		}
		s.track(e.Time, s.axis.of(e.Pos))
		a.setScroll(h, s, s.off-float64(d))
		return true

	case EventPointerUp:
		if !s.dragging {
			return false
		}
		s.dragging = false
		s.track(e.Time, s.axis.of(e.Pos))
		// The content moves against the finger, so the fling velocity is the
		// negated pointer velocity.
		v := -s.velocity()
		s.resetTrack()
		if absf(v) >= s.cfg.FlingVelocity {
			a.startFling(h, s, v)
		}
		return true

	case EventPointerCancel:
		s.dragging = false
		s.resetTrack()
		return true
	}
	return false
}

// --- offsets -----------------------------------------------------------------

// setScroll writes a clamped offset and marks the node for repaint.
//
// This is the whole cost of a scroll: two comparisons, a float64 store and the
// repaint mark. Nothing is built, nothing is measured and no view function
// runs, which is what [Diagnostics.Builds] and [Diagnostics.Layouts] are
// asserted on.
//
// A container that declared [ScrollSpec.Virtual] is marked for layout instead,
// because for it the content *is* a function of the offset. Still no build.
func (a *App) setScroll(h scene.Handle, s *scrollState, v float64) bool {
	v = s.clamp(v)
	if v == s.off {
		return false
	}
	s.off = v
	a.diag.Scrolls++
	if s.virtual {
		a.markNeedsLayout(h)
		return true
	}
	a.markNeedsPaint(h)
	return true
}

// startFling begins a kinetic scroll at v document units per second.
func (a *App) startFling(h scene.Handle, s *scrollState, v float32) {
	if v > s.cfg.MaxVelocity {
		v = s.cfg.MaxVelocity
	}
	if v < -s.cfg.MaxVelocity {
		v = -s.cfg.MaxVelocity
	}
	if !s.canMove(float64(v)) {
		// Already at the bound the fling points at.
		return
	}
	s.vel, s.flinging, s.lastTick = v, true, a.in.now
	for _, existing := range a.in.flings {
		if existing == h {
			return
		}
	}
	a.in.flings = append(a.in.flings, h)
	a.markNeedsPaint(h)
}

func (a *App) stopFling(s *scrollState) {
	s.flinging, s.vel = false, 0
}

// tickScrolls advances every running fling to now. It is called once per input
// phase from [App.BeginInput], so the whole animation is driven by the
// injected clock and never by wall time.
//
// # Idle policy
//
// Every tick that actually moves the content calls [App.markNeedsPaint], so
// [App.NeedsPaint] stays true for as long as a fling runs and the backend's
// idle tick policy — see the project plan, section 6 — keeps the full update
// rate. When the fling ends the flag stops being set, the backend counts its
// idle frames again and drops back to [Config.IdleTPS]. No separate "is
// animating" channel is needed, and adding one would be a second source of
// truth for the same question.
//
// The list of running flings is a reused slice in the input state, compacted
// in place, so a fling costs no allocation per frame.
func (a *App) tickScrolls(now time.Duration) {
	if len(a.in.flings) == 0 {
		return
	}
	out := a.in.flings[:0]
	for _, h := range a.in.flings {
		if !a.store.Valid(h) {
			continue
		}
		s := a.data(h).scroll
		if s == nil || !s.flinging {
			continue
		}
		if a.stepFling(h, s, now) {
			out = append(out, h)
		}
	}
	a.in.flings = out
}

// stepFling advances one fling and reports whether it is still running.
func (a *App) stepFling(h scene.Handle, s *scrollState, now time.Duration) bool {
	step := now - s.lastTick
	if step <= 0 {
		return true
	}
	if step > maxFlingStep {
		step = maxFlingStep
	}
	s.lastTick = now

	dt := float32(step.Seconds())
	k := s.cfg.Friction
	decay := float32(math.Exp(float64(-k * dt)))
	// The closed form of the integral of v0*exp(-k t) over [0, dt]. Using
	// v*dt instead would make the travelled distance depend on the tick rate,
	// which would be a different fling on a Pi at 30 Hz than on a desktop at
	// 60 Hz.
	dist := float64(s.vel) * float64(1-decay) / float64(k)
	s.vel *= decay

	moved := a.setScroll(h, s, s.off+dist)
	if !moved || absf(s.vel) < s.cfg.StopVelocity {
		// Either the bound was reached or the curve has run out. A fling that
		// hits the end stops there rather than bouncing; overscroll bounce is
		// an animation curve and the project plan, section 14, excludes those
		// from the MVP.
		s.flinging, s.vel = false, 0
		return false
	}
	return true
}

// --- the public surface ------------------------------------------------------

// ScrollInfo is a snapshot of the state of one scroll container.
type ScrollInfo struct {
	// Axis is the axis the container scrolls along.
	Axis ScrollAxis
	// Offset is the document coordinate at the leading edge of the viewport.
	Offset float64
	// MaxOffset is the largest legal offset, that is the content extent
	// minus the viewport extent, or zero when the content is smaller.
	MaxOffset float64
	// ContentExtent is the document extent along the axis, as reported by
	// the layouter.
	ContentExtent float64
	// ContentOrigin is the document coordinate of local coordinate zero of
	// the children; see [LayoutContext.ReportScrollContent].
	ContentOrigin float64
	// ViewportExtent is the extent of the container along the axis.
	ViewportExtent float32
	// Velocity is the current fling velocity in document units per second,
	// and Flinging says whether a fling is running.
	Velocity float32
	Flinging bool
	// Dragging says whether a pointer drag is currently driving this
	// container.
	Dragging bool
	// Virtual mirrors [ScrollSpec.Virtual].
	Virtual bool
}

// ScrollInfo returns the state of the scroll container r, and false when r is
// stale or is not a scroll container.
func (a *App) ScrollInfo(r NodeRef) (ScrollInfo, bool) {
	s := a.scrollOf(r.h)
	if s == nil {
		return ScrollInfo{}, false
	}
	return ScrollInfo{
		Axis:           s.axis,
		Offset:         s.off,
		MaxOffset:      s.maxOffset(),
		ContentExtent:  s.content,
		ContentOrigin:  s.origin,
		ViewportExtent: s.viewport,
		Velocity:       s.vel,
		Flinging:       s.flinging,
		Dragging:       s.dragging,
		Virtual:        s.virtual,
	}, true
}

// IsScrollable reports whether r is a scroll container.
func (a *App) IsScrollable(r NodeRef) bool { return a.scrollOf(r.h) != nil }

// ScrollTo moves the scroll container r to the document offset off, clamped to
// its bounds, and reports whether the offset changed.
//
// It is a jump, not an animation, and so is [App.ScrollIntoView]. Two reasons,
// and neither is laziness. An animated scroll is presentation state that
// changes every frame, so every caller — including every test — would have to
// pump frames until it settled before asserting anything, and the natural
// mistake is to assert too early and get a flaky test. And gift has no
// animation curves at all; the project plan, section 14, excludes them from
// the MVP, so the only honest curve available is the fling this package
// already implements for a gesture. An application that wants an animated jump
// today drives [App.ScrollTo] from its own tick and owns the curve, which is
// one loop and no new concept.
//
// Any running fling is cancelled: a programmatic jump and a fling disagreeing
// about where the content should be is the one thing worse than either.
func (a *App) ScrollTo(r NodeRef, off float64) bool {
	a.assertUIGoroutine("ScrollTo")
	s := a.scrollOf(r.h)
	if s == nil {
		return false
	}
	a.stopFling(s)
	return a.setScroll(r.h, s, off)
}

// ScrollBy moves the scroll container r by d document units and reports
// whether the offset changed.
func (a *App) ScrollBy(r NodeRef, d float64) bool {
	a.assertUIGoroutine("ScrollBy")
	s := a.scrollOf(r.h)
	if s == nil {
		return false
	}
	a.stopFling(s)
	return a.setScroll(r.h, s, s.off+d)
}

// ScrollIntoView adjusts every scroll container above r so that r is inside
// the visible part of each of them, and reports whether anything moved.
//
// It moves as little as possible: a node that is already visible is left
// alone, a node above the viewport is brought to the leading edge and a node
// below it to the trailing edge. A node larger than the viewport is aligned
// with the leading edge, because that is where its beginning is.
//
// The containers are adjusted from the innermost outwards, which is the order
// in which the work is independent: moving an inner container changes where r
// sits inside an outer one, while moving an outer container moves the inner
// viewport and r together.
//
// It is a jump; see [App.ScrollTo] for why.
func (a *App) ScrollIntoView(r NodeRef) bool {
	a.assertUIGoroutine("ScrollIntoView")
	if !a.store.Valid(r.h) {
		return false
	}
	moved := false
	h := a.store.Get(r.h).Parent
	for depth := 0; !h.IsZero() && a.store.Valid(h); depth++ {
		if depth > scene.MaxDepth {
			break
		}
		n := a.store.Get(h)
		if s := n.Payload.scroll; s != nil {
			if a.revealIn(h, s, r.h) {
				moved = true
			}
		}
		h = n.Parent
	}
	return moved
}

// revealIn is one container's share of [App.ScrollIntoView].
func (a *App) revealIn(vh scene.Handle, s *scrollState, target scene.Handle) bool {
	_, tm, ok := a.deviceSpace(target)
	if !ok {
		return false
	}
	_, vm, ok := a.deviceSpace(vh)
	if !ok {
		return false
	}
	t := tm.TransformRect(a.store.Get(target).Bounds)
	v := vm.TransformRect(a.store.Get(vh).Bounds)
	if t.IsEmpty() || v.IsEmpty() {
		return false
	}

	var lo, hi, vlo, vhi float32
	if s.axis == ScrollHorizontal {
		lo, hi, vlo, vhi = t.Min.X, t.Max.X, v.Min.X, v.Max.X
	} else {
		lo, hi, vlo, vhi = t.Min.Y, t.Max.Y, v.Min.Y, v.Max.Y
	}

	// Raising the offset moves the content towards the leading edge, so the
	// device coordinates of the target fall by the same amount.
	var d float32
	switch {
	case lo < vlo:
		d = lo - vlo
	case hi > vhi:
		d = hi - vhi
		// Never push the leading edge of the target back out of the viewport,
		// which is what would happen for a target taller than the viewport.
		if m := lo - vlo; d > m {
			d = m
		}
		if d < 0 {
			d = 0
		}
	default:
		return false
	}
	if d == 0 {
		return false
	}
	a.stopFling(s)
	return a.setScroll(vh, s, s.off+float64(d))
}

func (a *App) scrollOf(h scene.Handle) *scrollState {
	if !a.store.Valid(h) {
		return nil
	}
	return a.data(h).scroll
}

// --- the layout side ---------------------------------------------------------

// ReportScrollContent tells gift how far the content of this scroll container
// reaches and where local coordinate zero sits in the document.
//
// It may only be called by the layouter of a node whose [Element] declared a
// [ScrollSpec]; anything else is a contract violation and panics.
//
// extent is the document extent along the scroll axis, which for ordinary
// content is simply the measured extent of the children. origin is the
// document coordinate that local coordinate zero of the children corresponds
// to, and for ordinary content it is zero.
//
// # Why origin exists before anything uses it
//
// The project plan, section 10, requires that very large document positions
// are converted to float32 only after the viewport origin has been subtracted.
// A virtualising layouter — the masonry gallery — will place its visible items
// at local coordinates measured from a document origin it picks near the
// current offset, report that origin here, and gift then pushes a translation
// of origin - offset, which is a small number. The float32 conversion happens
// on the difference and nowhere else; see [scrollState].
//
// The offset itself is always float64, so a caller that reports origin zero
// for a 30 000 000 pixel document still gets exact offsets, exact bounds and
// exact scroll-into-view arithmetic; only the translation it hands the GPU is
// then as coarse as float32 is at that magnitude, which is unavoidable when
// the content really is a single node that tall.
//
// The offset is re-clamped here, so content that shrank under a resize pulls
// the viewport back rather than leaving it looking at nothing.
func (l *LayoutContext) ReportScrollContent(extent, origin float64) {
	s := l.nd.scroll
	if s == nil {
		panic("gift: LayoutContext.ReportScrollContent on a node whose Element did not declare a ScrollSpec")
	}
	if !(extent > 0) {
		extent = 0
	}
	s.content, s.origin = extent, origin
	s.off = s.clamp(s.off)
}

// ScrollOffset returns the current offset of this scroll container, for a
// layouter that virtualises its content and therefore has to know which part
// of the document it is being asked to produce.
//
// It panics for a node that is not a scroll container, for the same reason
// [LayoutContext.ReportScrollContent] does: reading zero from a node that has
// no offset would make a virtualising layouter silently produce the top of the
// document forever.
func (l *LayoutContext) ScrollOffset() float64 {
	s := l.nd.scroll
	if s == nil {
		panic("gift: LayoutContext.ScrollOffset on a node whose Element did not declare a ScrollSpec")
	}
	return s.off
}

// SetScrollOffset moves this scroll container to off, clamped to the content
// reported so far, and reports whether the offset changed.
//
// It is the counterpart of [LayoutContext.ScrollOffset] and exists for exactly
// one caller: the scroll anchor of the project plan, section 10. A reflow —
// a resize, a layout switch, a batch of corrections — moves every item in the
// document, and keeping the viewport on the same picture means computing a new
// offset from the new position of that picture. That computation needs the new
// layout, so it happens inside the layouter, and its result has to go
// somewhere.
//
// It writes the offset and nothing else. In particular it does not invalidate
// layout, because it is called *from* a layout pass and the caller is about to
// use the new offset in the same pass. Call
// [LayoutContext.ReportScrollContent] first, or the clamp has nothing to clamp
// against.
//
// Any running fling is cancelled, for the same reason [App.ScrollTo] cancels
// one: a reflow that moves the content and a fling that also moves it would
// fight over the same number.
func (l *LayoutContext) SetScrollOffset(off float64) bool {
	s := l.nd.scroll
	if s == nil {
		panic("gift: LayoutContext.SetScrollOffset on a node whose Element did not declare a ScrollSpec")
	}
	off = s.clamp(off)
	if off == s.off {
		return false
	}
	l.app.stopFling(s)
	s.off = off
	l.app.diag.Scrolls++
	return true
}

// ScrollInteractor returns the [Interactor] gift installs on a scroll
// container that brought none of its own: wheel, drag past [DragSlop] and the
// kinetic fling, with the overscroll chaining rule described on it.
//
// It exists because the two are otherwise exclusive. A scroll container that
// declares an Interactor — because it also wants the keyboard, which a gallery
// does — replaces gift's, and would then have to reimplement the gesture. The
// intended shape is delegation:
//
//	func (n *myNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
//	    if e.Kind == gift.EventKeyDown { ... }
//	    return gift.ScrollInteractor().HandleEvent(ctx, e)
//	}
//
// The returned value is a shared, stateless singleton — everything it touches
// lives in the node payload — so calling this in the event path allocates
// nothing.
func ScrollInteractor() Interactor { return scrollHandler{} }

// applyScroll installs or removes the scroll state of a node during a build.
//
// The state object survives a rebuild, which is the whole point: the offset is
// presentation state of the node and a rebuild of the component around it must
// not send the user back to the top of the list. It is replaced only when the
// axis changes, because an offset measured along y means nothing along x.
func (a *App) applyScroll(nd *nodeData, spec *ScrollSpec) {
	if spec == nil {
		nd.scroll = nil
		return
	}
	if spec.Axis == ScrollNone {
		panic("gift: Element.Scroll with ScrollNone; a scroll container needs an axis")
	}
	s := nd.scroll
	if s == nil || s.axis != spec.Axis {
		s = &scrollState{}
		nd.scroll = s
	}
	s.axis = spec.Axis
	s.cfg = spec.Config.withDefaults()
	s.virtual = spec.Virtual
	if nd.interactor == nil {
		// A scroll container has to be a hit target, or the wheel would never
		// reach it and a drag on its background would go nowhere. gift
		// supplies the interactor so that a view only has to declare the
		// spec; a view that brings its own keeps it and is then responsible
		// for the gesture itself.
		nd.interactor = scrollHandler{}
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
