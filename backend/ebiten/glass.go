package ebiten

import (
	"math"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// GlassPolicyConfig configures the measurement based [GlassPolicy]. The zero
// value selects the defaults below.
type GlassPolicyConfig struct {
	// Window is the number of drawn frame intervals the decision is made
	// over. Zero selects [DefaultGlassWindow].
	Window int

	// DownInterval is the median frame interval above which the policy drops
	// from [render.Full] to [render.Reduced], and UpInterval the one below
	// which it climbs back. Zero selects [DefaultGlassDownInterval] and
	// [DefaultGlassUpInterval].
	//
	// The gap between the two is the hysteresis and it is not optional. With
	// a single threshold a scene that sits exactly on it alternates every
	// dwell period, and the alternation is far more noticeable than either
	// level, because the eye is very good at seeing a background change and
	// very bad at seeing how blurred it is.
	DownInterval, UpInterval time.Duration

	// MinDwellFrames is the number of drawn frames a level must be held
	// before it may change again. Zero selects [DefaultGlassDwellFrames].
	MinDwellFrames int

	// MaxAreaFraction is the share of the screen the material regions of one
	// frame may cover before [render.Full] is refused outright. Zero selects
	// [DefaultGlassMaxArea].
	//
	// This is the second, independent signal, and it is the one the project
	// plan, section 13, actually binds: "Glass Full ueber Galerie:
	// Zusatzkosten < 4,0 ms je Frame; Materialflaeche <= 25 % des Screens."
	// The frame interval says "we are already too slow", which is a lagging
	// indicator and needs a dwell period to act on; the area says "this
	// frame is outside the envelope the threshold was stated for", which is
	// known before the frame is drawn and is therefore acted on immediately.
	MaxAreaFraction float64

	// UpAreaFraction is the share of the screen the material area must be
	// *below* before [render.Full] may be restored. Zero selects
	// [DefaultGlassUpArea].
	//
	// It is the hysteresis of the area signal and it is exactly as
	// non-optional as the gap between DownInterval and UpInterval. With one
	// threshold for both directions, a panel whose area oscillates around
	// the limit vetoes Full, resets the dwell, climbs back the moment the
	// dwell expires on whatever the area happened to be in that single
	// frame, and is vetoed again on the next one — measured at 39 level
	// changes in thirty simulated seconds, one every 0.77 s, which is a
	// blurred background switching on and off twice a second.
	//
	// The gap alone is not enough either, because the climb back is decided
	// from a single frame's area. See [GlassPolicyConfig.AreaWindow].
	UpAreaFraction float64

	// AreaWindow is the number of consecutive drawn frames the material area
	// must have stayed at or below UpAreaFraction before [render.Full] may
	// be restored. Zero selects [GlassPolicyConfig.Window], that is the same
	// window the interval median is taken over; a negative value means one
	// frame, which is the flickering behaviour and exists only so that a
	// test can reproduce it.
	AreaWindow int
}

// Defaults for [GlassPolicyConfig].
const (
	// DefaultGlassWindow is 60 drawn frames, about a second at sixty hertz.
	//
	// Short enough that opening a heavy view is reacted to within a second,
	// long enough that a single garbage collection or a window manager
	// hiccup cannot move the median.
	DefaultGlassWindow = 60

	// DefaultGlassDownInterval is 20 ms and DefaultGlassUpInterval 18 ms.
	//
	// The reference is the 16.667 ms nominal interval of the project plan,
	// section 13, plus its 0.5 ms tolerance, which gives 17.17 ms as the
	// point where a frame counts as missed. A *median* above 20 ms means the
	// scene is missing intervals routinely and not occasionally, which is the
	// condition worth reacting to. 18 ms is close enough to nominal that
	// climbing back is a real recovery and not a lull.
	//
	// These are intervals between drawn frames and therefore include vsync.
	// At sixty hertz a healthy scene sits at 16.7 ms and cannot go lower, so
	// the policy cannot distinguish "comfortable" from "just barely" — it can
	// only see the frames that were missed. That is a genuine limit of
	// measuring from the outside and it is why the area budget exists next to
	// it.
	DefaultGlassDownInterval = 20 * time.Millisecond
	DefaultGlassUpInterval   = 18 * time.Millisecond

	// DefaultGlassDwellFrames is 90 drawn frames, one and a half seconds.
	//
	// It is deliberately longer than the window. A level that changed must be
	// held long enough for the window to fill with measurements taken *at
	// that level*, or the policy decides the next time using evidence
	// gathered under the previous one and oscillates with a period of two
	// dwell times. Window plus a half is the shortest safe value.
	DefaultGlassDwellFrames = 90

	// DefaultGlassMaxArea is 0.25, the fraction the project plan,
	// section 13, states the Full threshold under.
	DefaultGlassMaxArea = 0.25

	// DefaultGlassUpArea is 0.20, four fifths of [DefaultGlassMaxArea].
	//
	// The same shape of choice as DefaultGlassUpInterval against
	// DefaultGlassDownInterval: far enough below the veto that a scene which
	// reaches it is genuinely back inside the envelope and not merely
	// jittering around its edge, close enough that a panel which shrank for
	// good gets its blur back. A fifth of the screen against a quarter is
	// twenty percent of headroom, which is five times the plausible frame to
	// frame jitter of a panel whose size is decided by a layout.
	DefaultGlassUpArea = 0.20
)

// GlassPolicy chooses between [render.Reduced] and [render.Full].
//
// # Why it is measurement based and not capability based
//
// Because there is nothing to query. Ebitengine exposes no GL version, no
// extension list and no GPU memory figure; the project plan, section 6, states
// that outright and WU-D and WU-E both ran into it. There is no honest way to
// ask a machine whether it can afford a blur, so the only remaining question
// is whether it is in fact affording one, and that is a frame interval.
//
// # What it decides on
//
// Two signals, deliberately of different character:
//
//   - The median of a sliding window of intervals between drawn frames,
//     with separate down and up thresholds and a minimum dwell time per
//     level. This is the lagging signal: it can only react after the frames
//     have already been missed, and the hysteresis and dwell exist so that it
//     cannot oscillate while reacting.
//   - The material area of the frame as a fraction of the screen. This is
//     the leading signal: the project plan, section 13, states the Full budget
//     only for a material area up to a quarter of the screen, so a frame that
//     exceeds it is outside the envelope and is drawn Reduced immediately,
//     with no dwell at all. Coming back is a different question and is
//     answered with a band of its own — [GlassPolicyConfig.UpAreaFraction] and
//     [GlassPolicyConfig.AreaWindow] — so a panel that hovers around the limit
//     does not flicker. The asymmetry is deliberate: leaving the envelope has
//     to be acted on now, re-entering it has to be believed first.
//
// # Pinning
//
// [GlassPolicy.Pin] fixes a level and [GlassPolicy.Unpin] returns to the
// policy. A pinned level never changes, whatever the measurements say, and
// section 13 makes pinning mandatory for any measurement that is meant to be
// compared with another one.
//
// A GlassPolicy belongs to the UI executor and is not safe for concurrent use.
type GlassPolicy struct {
	cfg GlassPolicyConfig

	// ring is the sliding window of intervals. It is preallocated in
	// [NewGlassPolicy] and never grows, so recording an interval is one
	// store and one increment.
	ring  []time.Duration
	n     int
	head  int
	sorts []time.Duration

	level  render.GlassQuality
	pinned bool
	dwell  int

	// lastArea is the material area fraction of the previous drawn frame.
	// The current frame's area is not known until its material operations
	// have been seen, and deciding halfway through a frame would give two
	// panels of one frame different levels.
	lastArea float64
	area     float64
	// areaStreak is the number of consecutive drawn frames whose material
	// area was at or below [GlassPolicyConfig.UpAreaFraction]. The climb back
	// to Full requires a full window of them, not the single frame the dwell
	// happens to expire on.
	areaStreak int

	// pendingLevel and hasPending carry a level change out of the frame path
	// to whoever logs it; see [GlassPolicy.TakeLevelChange].
	pendingLevel render.GlassQuality
	hasPending   bool

	stats GlassPolicyStats
}

// NewGlassPolicy returns a policy at [render.Full], unpinned.
//
// Full is the optimistic start on purpose: the policy can only observe a cost
// it is paying, so it has to pay it once to find out. A Raspberry Pi 4 that
// cannot afford it drops to Reduced within a window plus a dwell, that is
// about two and a half seconds, and logs nothing — the effective level is a
// counter, as the project plan, section 15, requires.
func NewGlassPolicy(cfg GlassPolicyConfig) *GlassPolicy {
	if cfg.Window <= 0 {
		cfg.Window = DefaultGlassWindow
	}
	if cfg.DownInterval <= 0 {
		cfg.DownInterval = DefaultGlassDownInterval
	}
	if cfg.UpInterval <= 0 {
		cfg.UpInterval = DefaultGlassUpInterval
	}
	if cfg.UpInterval >= cfg.DownInterval {
		// Without a gap there is no hysteresis, and without hysteresis the
		// dwell time is the only thing standing between the application and a
		// background that changes every second and a half. Rejecting it here
		// is better than a comment nobody reads.
		panic("gift/backend/ebiten: GlassPolicyConfig.UpInterval must be below DownInterval; " +
			"the gap between them is the hysteresis")
	}
	if cfg.MinDwellFrames <= 0 {
		cfg.MinDwellFrames = DefaultGlassDwellFrames
	}
	if cfg.MaxAreaFraction <= 0 {
		cfg.MaxAreaFraction = DefaultGlassMaxArea
	}
	if cfg.UpAreaFraction <= 0 {
		cfg.UpAreaFraction = DefaultGlassUpArea
		if cfg.UpAreaFraction >= cfg.MaxAreaFraction {
			// A caller that lowered MaxAreaFraction below the default up
			// threshold gets a proportional one rather than a panic, because
			// it did not ask for the up threshold at all.
			cfg.UpAreaFraction = cfg.MaxAreaFraction * 0.8
		}
	}
	if cfg.UpAreaFraction >= cfg.MaxAreaFraction {
		// The same rule as for the intervals, and for the same reason: with
		// no gap there is no hysteresis, and an area that oscillates around
		// one threshold changes the level every dwell period for ever.
		panic("gift/backend/ebiten: GlassPolicyConfig.UpAreaFraction must be below MaxAreaFraction; " +
			"the gap between them is the hysteresis of the area signal")
	}
	if cfg.AreaWindow == 0 {
		cfg.AreaWindow = cfg.Window
	}
	if cfg.AreaWindow < 1 {
		cfg.AreaWindow = 1
	}
	return &GlassPolicy{
		cfg:   cfg,
		ring:  make([]time.Duration, cfg.Window),
		sorts: make([]time.Duration, cfg.Window),
		level: render.Full,
	}
}

// Pin fixes the effective level. Pin([render.Adaptive]) is [GlassPolicy.Unpin].
func (p *GlassPolicy) Pin(q render.GlassQuality) {
	if q == render.Adaptive {
		p.Unpin()
		return
	}
	p.pinned = true
	if p.level != q {
		p.level = q
		p.stats.Changes++
		// Deliberately *not* recorded as a pending level change: an
		// application that pins a level configured it, and configuration is
		// Info at startup rather than a degradation warning. See
		// [GlassPolicy.TakeLevelChange].
	}
	p.stats.Pinned = true
}

// Unpin returns control to the measurements. The current level is kept as the
// starting point and the dwell timer restarts, so unpinning cannot cause an
// immediate change.
func (p *GlassPolicy) Unpin() {
	p.pinned = false
	p.dwell = 0
	p.stats.Pinned = false
}

// IsPinned reports whether a level is pinned.
func (p *GlassPolicy) IsPinned() bool { return p.pinned }

// Level returns the effective quality level. It is never [render.Adaptive]:
// adaptive is the policy, and this is its answer.
func (p *GlassPolicy) Level() render.GlassQuality { return p.level }

// RecordInterval adds one interval between drawn frames to the window.
//
// Intervals under a millisecond are dropped rather than recorded, for the
// reason the project plan, section 13, gives for the same rule in
// [metrics.FrameTimer]: they are two draw callbacks in a row and not two
// presentations, and counting them would pull the median down and hold the
// policy at Full on a machine that is visibly stuttering.
func (p *GlassPolicy) RecordInterval(d time.Duration) {
	if d < time.Millisecond {
		p.stats.SubFrameIntervals++
		return
	}
	p.ring[p.head] = d
	p.head++
	if p.head == len(p.ring) {
		p.head = 0
	}
	if p.n < len(p.ring) {
		p.n++
	}
}

// AddArea accumulates the device pixel area of one material region of the
// frame in progress.
func (p *GlassPolicy) AddArea(a float64) {
	if a > 0 {
		p.area += a
	}
}

// Median returns the median of the window, or zero while it is empty. Nearest
// rank on the lower middle sample, which for an even count is the smaller of
// the two — the pessimistic choice, so the policy climbs back a little later
// rather than a little sooner.
func (p *GlassPolicy) Median() time.Duration {
	if p.n == 0 {
		return 0
	}
	s := p.sorts[:p.n]
	copy(s, p.ring[:p.n])
	// Insertion sort. The window is sixty elements and nearly sorted in a
	// steady scene, and sort.Slice would box the slice into an interface and
	// allocate — which this function is called from the frame path with.
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
	return s[(len(s)-1)/2]
}

// BeginFrame moves the policy on by one drawn frame and returns the level the
// frame is drawn at.
//
// The level is decided once, here, and every material of the frame uses it.
// Deciding per material would give two panels of one frame different
// appearances, which looks like a bug and is one.
func (p *GlassPolicy) BeginFrame(screenArea float64) render.GlassQuality {
	area := p.area
	p.area = 0
	if screenArea > 0 {
		p.lastArea = area / screenArea
	} else {
		p.lastArea = 0
	}
	p.stats.AreaFraction = p.lastArea
	// The streak of frames spent below the *up* threshold. It is maintained
	// unconditionally, including while pinned and during the dwell, so that
	// the moment the policy is allowed to decide it is deciding on a window
	// of evidence rather than on one frame.
	if p.lastArea <= p.cfg.UpAreaFraction {
		if p.areaStreak < p.cfg.AreaWindow {
			p.areaStreak++
		}
	} else {
		p.areaStreak = 0
	}

	if p.pinned {
		p.stats.EffectiveLevel = p.level
		return p.level
	}
	if p.dwell < p.cfg.MinDwellFrames {
		p.dwell++
	}

	// The area veto is immediate and is not subject to dwell. See
	// [GlassPolicyConfig.MaxAreaFraction].
	if p.level == render.Full && p.lastArea > p.cfg.MaxAreaFraction {
		p.setLevel(render.Reduced)
		p.stats.AreaDowngrades++
		p.stats.EffectiveLevel = p.level
		return p.level
	}
	if p.dwell < p.cfg.MinDwellFrames || p.n < len(p.ring) {
		p.stats.EffectiveLevel = p.level
		return p.level
	}

	med := p.Median()
	p.stats.MedianInterval = med
	switch p.level {
	case render.Full:
		if med > p.cfg.DownInterval {
			p.setLevel(render.Reduced)
			p.stats.Downgrades++
		}
	default:
		// Both halves of the area hysteresis: the area must be well below
		// the veto threshold, and it must have been there for a whole
		// window. Asking only "is it below the limit right now" is what
		// produced a level change every 0.77 s on an area that oscillated by
		// four percent around the limit.
		if med < p.cfg.UpInterval && p.areaStreak >= p.cfg.AreaWindow {
			p.setLevel(render.Full)
			p.stats.Upgrades++
		}
	}
	p.stats.EffectiveLevel = p.level
	return p.level
}

func (p *GlassPolicy) setLevel(q render.GlassQuality) {
	if p.level == q {
		return
	}
	p.level = q
	p.dwell = 0
	p.stats.Changes++
	// Handed out of band, never logged from here: see
	// [GlassPolicy.TakeLevelChange] and the project plan, section 15.
	p.pendingLevel, p.hasPending = q, true
	// The window is deliberately *not* cleared. Clearing it would make the
	// policy blind for a whole window right after a change, and then the
	// first decision after the dwell would be taken on a partly filled
	// window; keeping it means the old measurements age out naturally while
	// the dwell runs. The dwell is longer than the window for exactly this
	// reason; see [DefaultGlassDwellFrames].
}

// GlassPolicyStats are the counters of the policy. The project plan,
// section 8, requires the effective level to be visible in the diagnostics and
// section 15 requires it to be a counter rather than a log line; this is that.
type GlassPolicyStats struct {
	// EffectiveLevel is the level the last drawn frame used.
	EffectiveLevel render.GlassQuality
	// Pinned reports whether the level was fixed by the application.
	Pinned bool
	// Changes is the total number of level changes, Downgrades the subset
	// caused by the frame interval and AreaDowngrades the subset caused by
	// the material area budget. Upgrades is the number of climbs back.
	//
	// Changes is the number a flicker test asserts on: a scene that sits on a
	// threshold must produce a small constant, not one per dwell period. Both
	// signals have a band and a hold: the intervals have DownInterval against
	// UpInterval plus MinDwellFrames, and the area has MaxAreaFraction against
	// UpAreaFraction plus AreaWindow. TestGlassPolicyDoesNotFlicker and
	// TestGlassPolicyDoesNotFlickerOnArea pin one each.
	Changes, Downgrades, AreaDowngrades, Upgrades uint64
	// MedianInterval is the median of the window at the last decision and
	// AreaFraction the material area of the last frame as a share of the
	// screen.
	MedianInterval time.Duration
	AreaFraction   float64
	// SubFrameIntervals is the number of intervals under a millisecond that
	// were discarded. Counted rather than silently dropped, for the reason
	// the project plan, section 13, gives.
	SubFrameIntervals uint64
}

// Stats returns a snapshot of the policy counters.
func (p *GlassPolicy) Stats() GlassPolicyStats { return p.stats }

// TakeLevelChange reports the level the policy last switched to and clears the
// flag, so that every change is reported exactly once and a run that did not
// change reports nothing.
//
// # Why this exists rather than a log call in setLevel
//
// The project plan, section 15, asks for both halves and they are not the same
// half. The effective level is a *counter*, written in the frame path with no
// formatting and no interface boxing — that is [GlassPolicyStats.EffectiveLevel]
// and it stays a counter. Section 15 also asks for `Warn` on degraded quality
// and names a fall back to Glass Reduced as its example, and a level change is
// out of band by definition: it happens a handful of times in a whole run,
// never per frame, and there is nothing to rate limit.
//
// So the change is *recorded* in the frame path as one enum store and one
// bool, and it is formatted and logged where a logger exists; see [Run]. A
// caller that never drains it pays those two stores per level change and
// nothing else, so the 0 B/op contract of the project plan, section 11, is
// untouched.
func (p *GlassPolicy) TakeLevelChange() (render.GlassQuality, bool) {
	if !p.hasPending {
		return p.level, false
	}
	p.hasPending = false
	return p.pendingLevel, true
}

// glassPass names one stage of a material pass chain, for the counters and for
// the pass trace a headless test records.
type glassPass uint8

const (
	// glassPassCopy is the region copy of the backdrop.
	glassPassCopy glassPass = iota
	// glassPassDown and glassPassUp are one level of the dual-Kawase chain.
	glassPassDown
	glassPassUp
	// glassPassComposite is the final pass: tint, refraction, highlight and,
	// at Full, grain.
	glassPassComposite
	// glassPassFallback is the degraded material: a plain tinted rounded
	// rectangle through the shared shape shader, with no backdrop at all.
	glassPassFallback
	// glassPassScene is the scene to screen blit at the end of a frame that
	// contained a material. It is not part of any one material — it is paid
	// once however many there are — which is why it is a stage of its own and
	// not a second copy.
	glassPassScene
)

// String makes a failing test readable.
func (p glassPass) String() string {
	switch p {
	case glassPassCopy:
		return "copy"
	case glassPassDown:
		return "down"
	case glassPassUp:
		return "up"
	case glassPassComposite:
		return "composite"
	case glassPassScene:
		return "scene"
	default:
		return "fallback"
	}
}

// maxBlurLevels caps the dual-Kawase chain.
//
// Four levels take the region to a sixteenth of its linear size, which for a
// 1920 wide panel is 120 pixels across; the project plan, section 8, reckons
// with an eighth. A fifth level costs almost nothing in fill rate and does
// cost a target and a draw call, and beyond that the blur stops looking like
// frosted glass and starts looking like a solid colour.
const maxBlurLevels = 4

// blurLevels is how many down and up passes a device space blur radius asks
// for.
//
// Each level halves the linear resolution, and the four tap kernel at level n
// reaches roughly 2^n pixels of the original. So the level count is the
// logarithm, clamped: radius 4 is one level, 8 is two, 16 is three, 32 and
// above is four.
func blurLevels(radiusDev float32) int {
	if !(radiusDev > 2) {
		return 1
	}
	n := int(math.Round(math.Log2(float64(radiusDev)) - 1))
	if n < 1 {
		return 1
	}
	if n > maxBlurLevels {
		return maxBlurLevels
	}
	return n
}

// packGlassStyle packs the three scalars that share the last vertex attribute.
//
// See glass.kage for the layout and for why the packing exists at all. The
// refraction field is quantised to 63 and the other two to 255, so the largest
// value this can produce is 63*65536 + 255*256 + 255 = 4194303. That is 2^22-1
// and leaves two whole bits of margin below 2^24-1, the last integer a float32
// holds exactly, which is the bound the packing actually needs. The comment
// here used to quote 16777215, describing a scheme with a tighter margin than
// the code has; the arithmetic below is the binding statement.
//
// refraction arrives in *device* pixels, like every other geometric value that
// reaches a shader in this package, because that is what the shader adds to a
// device space sample position. See [Renderer.compositeGlass].
func packGlassStyle(refraction, highlight, grain float32) float32 {
	r := quant(refraction, 63)
	h := quant(highlight*255, 255)
	g := quant(grain*255, 255)
	return float32(r*65536 + h*256 + g)
}

func quant(v float32, hi int) int {
	if !(v > 0) {
		return 0
	}
	n := int(v + 0.5)
	if n > hi {
		return hi
	}
	return n
}

// appendMaterial draws one [render.OpMaterial].
//
// # It is a barrier
//
// The first thing it does is flush, and that is not an optimisation detail but
// the semantics: a material reads what was drawn before it, so everything
// before it has to be on the target before the copy can happen. See
// [render.OpMaterial]. The cost is stated in [RendererStats.GlassOps]: a glass
// panel splits the frame's shape run in two and adds its own passes.
//
// # The chain
//
// Reduced is copy, composite. Full is copy, n down passes, n up passes,
// composite. Both are confined to the material region, both leave the pool
// with nothing leased, and neither reads a pixel back to the CPU.
func (r *Renderer) appendMaterial(l *render.List, op render.Op) {
	m := l.Material(op.Material)
	if m.Kind != render.MaterialGlass {
		// A material index of zero, or a kind this backend does not know.
		// Skipped and counted, like any other unknown; see [render.OpKind].
		r.unknowns++
		return
	}
	g := m.Glass

	b := op.Bounds
	if b.IsEmpty() {
		r.skipEmptyBounds++
		return
	}
	clip := l.Clip(op.Clip)
	if clip.IsEmpty() {
		r.skipEmptyClip++
		return
	}
	xf := l.Xform(op.Xform)
	region := xf.TransformRect(b).Canon()
	// The parent clip applies to the material exactly as it applies to a
	// background fill, which is the project plan, section 8: "Ein
	// Glass-Backdrop wird an seiner Materialform geclippt", and a parent clip
	// is already folded into the clip rectangle by render.List.PushClip.
	vis := region.Intersect(clip)
	// And against the screen. A material entirely off screen draws nothing,
	// and saying so here is what keeps this function and
	// [listHasVisibleMaterial] agreeing about which materials are visible —
	// they have to, or a frame could acquire a scene target for a panel this
	// function then skips, or skip acquiring one for a panel it then draws.
	if r.frameSize.W > 0 && r.frameSize.H > 0 {
		vis = vis.Intersect(geom.Rc(0, 0, r.frameSize.W, r.frameSize.H))
	}
	if r.scene != nil {
		vis = vis.Intersect(geom.Rc(0, 0, float32(r.sceneW), float32(r.sceneH)))
	}
	if vis.IsEmpty() {
		r.skipOutsideClip++
		return
	}

	// The barrier.
	r.flush()

	r.glassOps++
	r.emitted++
	area := float64(vis.Width()) * float64(vis.Height())
	if r.policy != nil {
		r.policy.AddArea(area)
	}

	sx, sy := deviceScale(xf)
	sr := sx
	if sy < sr {
		sr = sy
	}
	halfW, halfH := region.Width()*0.5, region.Height()*0.5
	radius := op.CornerRadius * sr
	if lim := min32(halfW, halfH); radius > lim {
		radius = lim
	}
	if radius < 0 {
		radius = 0
	}

	q := r.frameQuality
	if g.Level != render.Adaptive && !r.policyPinned() {
		// A material may pin its own level, which is what makes the two
		// levels comparable side by side in example-effects and what section
		// 13 needs for a measurement.
		//
		// It may not override a pinned *policy*. A pin is the application
		// saying "this whole run is measured at one level", which section 13
		// makes a precondition of comparing one measurement with another, and
		// a per material request that quietly won left the reported level
		// describing a frame that was drawn at the other one. When both are
		// set the policy wins and the material's own request is ignored.
		q = g.Level
	}

	back, rw, rh := r.leaseBackdrop(region)
	if back == nil {
		r.glassFallbacks++
		r.glassPass(glassPassFallback)
		r.drawGlassFallback(vis, clip, region, halfW, halfH, radius, g)
		return
	}
	if q == render.Full {
		r.blurRegion(back, rw, rh, g.Blur*sr)
		r.glassFullOps++
	} else {
		r.glassReducedOps++
	}

	r.glassPass(glassPassComposite)
	r.compositeGlass(back, vis, region, halfW, halfH, radius, sr, g, q)
	r.targets.Release(back)
}

// leaseBackdrop copies the part of the scene that lies under the material
// region into a pooled target, and returns it with the valid extent.
//
// The target covers the *whole* region, not the visible part of it, and the
// copy is placed at the region's own offset. That is what lets the composite
// pass carry nothing but the half extents: the local position of a fragment
// inside the target is its position inside the region, so the distance field
// and the backdrop sample share one coordinate system and no origin offset has
// to travel in a vertex attribute there is no room for.
//
// A region larger than the scene is refused rather than clamped, and the
// caller degrades. Clamping would move the region origin, and the rounded
// corners of the shape would then be drawn at the edge of the screen instead
// of off it.
func (r *Renderer) leaseBackdrop(region geom.Rect) (*eb.Image, int, int) {
	if r.scene == nil || r.targets == nil {
		return nil, 0, 0
	}
	rw := int(math.Ceil(float64(region.Width())))
	rh := int(math.Ceil(float64(region.Height())))
	if rw <= 0 || rh <= 0 || rw > r.sceneW || rh > r.sceneH {
		return nil, 0, 0
	}
	t := r.targets.Acquire(rw, rh)
	if t == nil {
		return nil, 0, 0
	}

	src := region.Intersect(geom.Rc(0, 0, float32(r.sceneW), float32(r.sceneH)))
	if src.IsEmpty() {
		r.targets.Release(t)
		return nil, 0, 0
	}
	// The region may hang off an edge of the screen, in which case part of
	// the target has no backdrop at all and holds whatever the previous
	// tenant left. Clearing costs a full target fill, so it happens only in
	// that case; the common one is a panel entirely on screen, where the copy
	// covers every pixel the composite will sample.
	if src != region && r.drawFn == nil {
		t.Clear()
	}
	dstX := src.Min.X - region.Min.X
	dstY := src.Min.Y - region.Min.Y
	// Recorded before the draw rather than after it, so that a trace hook sees
	// the stage the draw belongs to and not the previous one.
	r.glassPass(glassPassCopy)
	r.blitCopy(t, r.scene, src, dstX, dstY)
	return t, rw, rh
}

// blurRegion runs the dual-Kawase chain in place: the result ends up back in
// src, at the region's own resolution, so the composite pass does not have to
// know whether it is looking at a blurred backdrop or a raw one.
func (r *Renderer) blurRegion(src *eb.Image, w, h int, radiusDev float32) {
	levels := blurLevels(radiusDev)
	var chain [maxBlurLevels]*eb.Image
	var sizes [maxBlurLevels][2]int

	cw, ch := w, h
	cur := src
	n := 0
	for i := range levels {
		dw, dh := max(1, cw/2), max(1, ch/2)
		t := r.targets.Acquire(dw, dh)
		if t == nil {
			// The budget refused a level. The chain stops where it is, which
			// is a blur of the levels that did fit — degraded, visible in
			// [TargetStats.Rejected], and never a missing panel.
			break
		}
		r.glassPass(glassPassDown)
		r.drawKawase(r.downShader, t, cur, dw, dh, cw, ch)
		chain[i], sizes[i] = t, [2]int{dw, dh}
		cur, cw, ch = t, dw, dh
		n = i + 1
	}
	// Back up. The last level up writes into src itself, which is both the
	// region resolution the composite wants and one fewer target.
	for i := n - 1; i >= 0; i-- {
		dst := src
		dw, dh := w, h
		if i > 0 {
			dst = chain[i-1]
			dw, dh = sizes[i-1][0], sizes[i-1][1]
		}
		r.glassPass(glassPassUp)
		r.drawKawase(r.upShader, dst, chain[i], dw, dh, sizes[i][0], sizes[i][1])
	}
	for i := range n {
		r.targets.Release(chain[i])
	}
}

// compositeGlass draws the final pass into the current target.
//
// The quad is the visible rectangle, so a clipped panel pays fragments only
// for what is on screen; the source coordinates are the position inside the
// region, which is also the position inside the backdrop target.
func (r *Renderer) compositeGlass(back *eb.Image, vis, region geom.Rect,
	halfW, halfH, radius, scale float32, g render.GlassParams, q render.GlassQuality) {
	if r.dst == nil && r.drawFn == nil {
		return
	}
	grain := g.Grain
	if q != render.Full {
		// No grain on an unblurred backdrop. It reads as dirt on the screen
		// rather than as frost in the glass, which is a different and worse
		// artefact than having no grain at all.
		grain = 0
	}
	// Refraction is a length in logical pixels and the shader consumes it in
	// device pixels, exactly like the corner radius and the blur radius two
	// callers up. It was packed raw, which was latent only because nothing in
	// gift emits a scale transform yet — the same class of defect WU-E fixed
	// for the shape shader, where a local-unit pad cut the outer half of every
	// antialiased edge under a shrink.
	packed := packGlassStyle(g.Refraction*scale, g.Highlight, grain)

	r.passVerts = r.passVerts[:0]
	r.passIdx = r.passIdx[:0]
	corners := [4][2]float32{
		{vis.Min.X, vis.Min.Y}, {vis.Max.X, vis.Min.Y},
		{vis.Max.X, vis.Max.Y}, {vis.Min.X, vis.Max.Y},
	}
	for _, c := range corners {
		r.passVerts = append(r.passVerts, eb.Vertex{
			DstX: c[0], DstY: c[1],
			SrcX: c[0] - region.Min.X, SrcY: c[1] - region.Min.Y,
			ColorR: g.Tint.R, ColorG: g.Tint.G, ColorB: g.Tint.B, ColorA: g.Tint.A,
			Custom0: halfW, Custom1: halfH, Custom2: radius, Custom3: packed,
		})
	}
	r.passIdx = append(r.passIdx, 0, 1, 2, 0, 2, 3)

	if r.drawFn != nil {
		r.drawFn(MaterialGlass, r.passVerts, r.passIdx)
		r.countGlassBatch()
		return
	}
	r.glassOpts.Images[0] = back
	r.dst.DrawTrianglesShader32(r.passVerts, r.passIdx, r.glassShader, &r.glassOpts)
	r.glassOpts.Images[0] = nil
	r.countGlassBatch()
}

// drawGlassFallback draws the material as a plain tinted rounded rectangle
// through the shared shape shader.
//
// This is what happens when there is no backdrop to sample: no scene target
// because the frame was already being drawn to the screen when the first
// material appeared, a region larger than the screen, or a target pool that
// refused the lease. It is also what every headless test sees, which is
// deliberate — the display list, the clipping and the accounting are then
// exercised without a graphics context, exactly as project plan section 12,
// criterion 4 requires of everything else.
//
// It is a degradation and it is counted as one — see
// [RendererStats.GlassFallbacks] — and it is also *visible* as one, which used
// not to be true. A pane whose tint is fully transparent is a clear pane: with
// a backdrop it is glass, and without one it is nothing at all. Returning
// early for that case meant a default-tinted panel degraded to a ghost while a
// clear one degraded to a hole in the layout, with the only evidence behind
// the giftmetrics build tag. So a transparent tint falls back to
// [render.DefaultGlassTint], which is the barely-there cool white that says
// "there is a pane here and it is not working" without inventing a colour the
// application never asked for.
func (r *Renderer) drawGlassFallback(vis, clip, region geom.Rect, halfW, halfH, radius float32, g render.GlassParams) {
	tint := g.Tint
	if tint.IsTransparent() {
		tint = render.DefaultGlassTint()
	}
	r.material(MaterialShape, nil)
	sh := shapeParams{
		color:   tint,
		halfW:   halfW,
		halfH:   halfH,
		radius:  radius,
		originX: region.Min.X,
		originY: region.Min.Y,
		scaleX:  1,
		scaleY:  1,
	}
	base := uint32(len(r.verts))
	r.verts = append(r.verts,
		vertex(vis.Min.X, vis.Min.Y, vis.Min.X-region.Min.X, vis.Min.Y-region.Min.Y, sh),
		vertex(vis.Max.X, vis.Min.Y, vis.Max.X-region.Min.X, vis.Min.Y-region.Min.Y, sh),
		vertex(vis.Max.X, vis.Max.Y, vis.Max.X-region.Min.X, vis.Max.Y-region.Min.Y, sh),
		vertex(vis.Min.X, vis.Max.Y, vis.Min.X-region.Min.X, vis.Max.Y-region.Min.Y, sh),
	)
	r.idx = append(r.idx, base, base+1, base+2, base, base+2, base+3)
}

// blitCopy copies srcRect of src into dst with the destination replaced
// rather than blended. It is the region copy of a backdrop; the scene to
// screen blit uses [Renderer.blitOver] instead.
//
// DrawTriangles and not DrawImage, and that is about allocation rather than
// taste: drawing a sub-rectangle with DrawImage needs an [eb.Image.SubImage],
// which allocates an image header every call and therefore once per glass
// panel per frame. Four vertices in a buffer this package already owns cost
// nothing.
func (r *Renderer) blitCopy(dst, src *eb.Image, srcRect geom.Rect, dstX, dstY float32) {
	r.blit(dst, src, srcRect, dstX, dstY, &r.copyOpts)
}

// blitOver composites srcRect of src onto dst with source over. See
// [Renderer.SetTarget] for why the scene blit must not replace.
func (r *Renderer) blitOver(dst, src *eb.Image, srcRect geom.Rect, dstX, dstY float32) {
	r.blit(dst, src, srcRect, dstX, dstY, &r.blitOpts)
}

func (r *Renderer) blit(dst, src *eb.Image, srcRect geom.Rect, dstX, dstY float32, opts *eb.DrawTrianglesOptions) {
	if dst == nil || src == nil {
		return
	}
	w, h := srcRect.Width(), srcRect.Height()
	r.passVerts = r.passVerts[:0]
	r.passIdx = r.passIdx[:0]
	quad := [4][4]float32{
		{dstX, dstY, srcRect.Min.X, srcRect.Min.Y},
		{dstX + w, dstY, srcRect.Max.X, srcRect.Min.Y},
		{dstX + w, dstY + h, srcRect.Max.X, srcRect.Max.Y},
		{dstX, dstY + h, srcRect.Min.X, srcRect.Max.Y},
	}
	for _, v := range quad {
		r.passVerts = append(r.passVerts, eb.Vertex{
			DstX: v[0], DstY: v[1], SrcX: v[2], SrcY: v[3],
			ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1,
		})
	}
	r.passIdx = append(r.passIdx, 0, 1, 2, 0, 2, 3)
	if r.drawFn != nil {
		// Headless. The geometry and the accounting are exercised, the GPU
		// command is not; see [Renderer.drawFn].
		r.drawFn(MaterialGlass, r.passVerts, r.passIdx)
	} else {
		dst.DrawTriangles32(r.passVerts, r.passIdx, src, opts)
	}
	r.countGlassBatch()
}

// drawKawase runs one blur pass: the whole of dst's valid area, sampling the
// whole of src's valid area.
func (r *Renderer) drawKawase(sh *eb.Shader, dst, src *eb.Image, dw, dh, sw, sh2 int) {
	if dst == nil || src == nil || sh == nil {
		return
	}
	fdw, fdh := float32(dw), float32(dh)
	fsw, fsh := float32(sw), float32(sh2)
	r.passVerts = r.passVerts[:0]
	r.passIdx = r.passIdx[:0]
	quad := [4][4]float32{
		{0, 0, 0, 0},
		{fdw, 0, fsw, 0},
		{fdw, fdh, fsw, fsh},
		{0, fdh, 0, fsh},
	}
	for _, v := range quad {
		r.passVerts = append(r.passVerts, eb.Vertex{
			DstX: v[0], DstY: v[1], SrcX: v[2], SrcY: v[3],
			ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1,
			// The tap offset is one source pixel, which is the dual filter's
			// own parameter: the reach comes from the number of levels and
			// not from stretching the kernel, because a stretched kernel at
			// four taps is a visible cross.
			Custom0: 1, Custom1: fsw, Custom2: fsh,
		})
	}
	r.passIdx = append(r.passIdx, 0, 1, 2, 0, 2, 3)
	if r.drawFn != nil {
		r.drawFn(MaterialGlass, r.passVerts, r.passIdx)
	} else {
		dst.DrawTrianglesShader32(r.passVerts, r.passIdx, sh, r.blurOptsFor(src))
	}
	r.countGlassBatch()
}

func (r *Renderer) blurOptsFor(src *eb.Image) *eb.DrawTrianglesShaderOptions {
	r.blurOpts.Images[0] = src
	return &r.blurOpts
}

// glassPass records one stage, for the counters and for the pass trace a test
// installs.
func (r *Renderer) glassPass(p glassPass) {
	r.glassPasses++
	if r.passFn != nil {
		r.passFn(p)
	}
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// countGlassBatch records one draw call issued by a material pass. These do
// not go through [Renderer.flush] — a pass is issued immediately, because it
// has its own shader and its own target — so the counters are bumped here
// instead.
func (r *Renderer) countGlassBatch() {
	r.batches++
	r.frameBatches++
	r.glassBatches++
}

// policyPinned reports whether the application fixed the quality level for the
// whole run. See [Renderer.appendMaterial] for why a material's own level
// request yields to it.
func (r *Renderer) policyPinned() bool { return r.policy != nil && r.policy.IsPinned() }
