package ebiten

import (
	"log/slog"
	"math"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/internal/text"
	"github.com/worldiety/gift/metrics"
	"github.com/worldiety/gift/render"
)

// Config configures the window and the frame loop of [Run].
type Config struct {
	// Title is the window title.
	Title string

	// Width and Height are the initial window size in logical pixels. Zero
	// or less selects 1280x720.
	Width, Height int

	// Logger is used for lifecycle and error paths only, never in the frame
	// path. A nil Logger means logging is off; this package never falls back
	// to slog.Default. See the project plan, section 15.
	Logger *slog.Logger

	// TPS is the tick rate, that is the number of update callbacks per
	// second. Zero or less selects Ebitengine's default of sixty.
	//
	// It does not set the frame rate. Ebitengine draws as often as the
	// display and the driver allow, and several updates may happen between
	// two drawn frames; the project plan, section 6, is explicit about this.
	TPS int

	// IdleTPS, if greater than zero, lowers the tick rate to this value once
	// IdleFrames consecutive updates have changed nothing.
	//
	// # Why this exists
	//
	// Without change and without animation Ebitengine still ticks and draws
	// sixty times per second, because that is its model; gift has no on
	// demand rendering and the project plan, section 6, rules it out. On a
	// Raspberry Pi that constant load is heat, and heat is thermal
	// throttling, which then shows up as missed frame intervals in a
	// measurement that has nothing to do with the scene. Lowering the tick
	// rate while nothing happens is the sanctioned countermeasure — an
	// application policy, not a second presentation path.
	//
	// It is off by default because it changes what is being measured. A
	// benchmark must state whether it was on.
	IdleTPS int

	// IdleFrames is the number of unchanged updates before the idle tick
	// rate is applied. Zero or less selects sixty, that is about a second.
	IdleFrames int

	// NominalFrameInterval is the interval the display is expected to hold,
	// for example 16667 microseconds at sixty hertz. Zero or less derives it
	// from TPS.
	//
	// # Why this is a field and not simply TPS
	//
	// TPS is the update rate. FrameInterval measures the distance between two
	// draw callbacks, and Ebitengine draws as often as the display and the
	// driver allow, which is a different number — the project plan,
	// section 6, is explicit that several updates may happen between two
	// frames. Deriving the frame threshold from the tick rate is therefore an
	// assumption, and it is only correct when the two happen to coincide,
	// which is the common sixty hertz case and nothing more. Set this field
	// when they do not, for example on a 50 Hz panel.
	NominalFrameInterval time.Duration

	// IntervalTolerance is the slack added to the nominal interval before a
	// frame counts as missed. Zero selects
	// [metrics.DefaultIntervalTolerance].
	//
	// It exists because the obvious thing is wrong. A strict comparison
	// against the bare nominal interval reported 50 % of the frames of a
	// cleanly timed measurement as missed; the project plan, section 13,
	// therefore binds the threshold at 17.17 ms for sixty hertz, that is
	// 16.667 plus 0.5. Run used to construct its default timer with the raw
	// nominal interval, so every consumer that did not bring its own timer
	// silently measured against the retracted threshold.
	IntervalTolerance time.Duration

	// WarmupFrames is the number of leading frame intervals to discard. Zero
	// selects [metrics.DefaultWarmupIntervals]; a negative value keeps all of
	// them.
	//
	// Opening a window costs a first interval of well over a hundred
	// milliseconds, and at the default history size a sixty second run can
	// never evict it again. It is warm-up, not a missed frame, and the
	// project plan, section 13, judges the scene and not the startup.
	WarmupFrames int

	// OnRenderer, if non nil, is called once with the renderer before the
	// window opens. It is how an application reaches [RendererStats].
	//
	// The renderer belongs to the UI executor, so the only place the
	// application may legally read those counters from is OnUpdate.
	OnRenderer func(*Renderer)

	// OnUpdate runs once per update, after input has been dispatched into
	// gift and before gift builds and lays out.
	//
	// # Why it survived WU-H
	//
	// It used to be the only way an application could get anything to happen,
	// and the example abused it as a substitute for input. It is kept, with a
	// narrower job: it is the tick hook. Animations, timers, a benchmark that
	// stops after sixty seconds and a headless driver all need a callback per
	// update that is not an input event, and none of them are served by the
	// event model. What it is no longer is the input path — that is
	// [inputBridge], and an application writes no code for it.
	//
	// Returning [Terminate] stops the loop and makes [Run] return nil, which
	// is how a benchmark run exits after a fixed duration. Any other non nil
	// error stops the loop and is returned by [Run].
	OnUpdate func() error

	// AssetStats supplies the image pipeline counters for the measurement
	// report. It may be nil.
	//
	// It is here rather than filled in automatically because the backend
	// does not own the pipeline and must not import ui to find it: the
	// project plan, section 3, keeps fachliche Controls out of the backend.
	// The application owns the pipeline, so the application hands over the
	// one line that reads it. Without this the decode, disk cache and budget
	// counters are unreachable in a running program, which is exactly the
	// cold versus warm evidence section 12 step 4 names as its deliverable.
	//
	//	cfg.AssetStats = func() metrics.AssetStats {
	//	    return metrics.AssetStatsOf(ui.ImagePipeline().Stats())
	//	}
	AssetStats func() metrics.AssetStats

	// GlassQuality pins the glass material's quality level for the whole run.
	// The zero value is [render.Adaptive], which is the measurement based
	// policy of the project plan, section 8.
	//
	// Pin it for anything that is meant to be compared with anything else.
	// Section 13 does not treat that as advice: an adaptive run changes how
	// much work it does part way through, so its frame time distribution is
	// two distributions with a seam in the middle, and the seam moves with
	// the machine.
	GlassQuality render.GlassQuality

	// NoInput disables the input bridge. It exists for a measurement run that
	// must not be perturbed by a cursor that happens to rest over a button,
	// and for a test harness that dispatches events into [gift.App] itself.
	NoInput bool
}

// Terminate is the error that stops the frame loop without making [Run] fail.
// It is Ebitengine's termination sentinel under a name that does not require
// the application to import Ebitengine.
var Terminate = eb.Termination

// Run opens a window and drives app until it is closed.
//
// # Frame model
//
// This is the place where the project plan, section 6, becomes code. The two
// halves of a gift frame are wired to the two Ebitengine callbacks and to
// nothing else:
//
//   - [gift.App.Update] — build, reconciliation and layout — runs in
//     Ebitengine's Update, which may run several times before a frame is
//     drawn and, under load, not at all between two frames.
//   - [gift.App.Paint] — the display list — runs in Ebitengine's Draw, once
//     per drawn frame, and the whole visible list is redrawn every time.
//     There is no partial screen repaint and no dirty rectangle.
//
// The core rejects the two crossings with a panic, and this function does not
// work around that: anything budgeted per drawn frame is counted in Draw, and
// anything that has to happen per tick is counted in Update.
//
// Run must be called from the goroutine that created app, because that
// goroutine is the UI executor, and Ebitengine calls back on the goroutine
// that called it.
func Run(app *gift.App, cfg Config) error {
	if app == nil {
		panic("gift/backend/ebiten: Run with a nil App")
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 {
		w = 1280
	}
	if h <= 0 {
		h = 720
	}
	tps := cfg.TPS
	if tps <= 0 {
		tps = eb.DefaultTPS
	}
	idleAfter := cfg.IdleFrames
	if idleAfter <= 0 {
		idleAfter = 60
	}

	r, err := NewRenderer()
	if err != nil {
		return err
	}
	// The one route from a painter in ui to a texture in this package. gift
	// carries the service and never uses it; see [gift.App.SetImages].
	app.SetImages(r.Images())
	r.PinGlassQuality(cfg.GlassQuality)
	if cfg.OnRenderer != nil {
		cfg.OnRenderer(r)
	}

	// Measurement. Enabled is a compile time constant false without the
	// giftmetrics tag, so in an ordinary build this whole block, the closure
	// and every recording call below is dead code that the compiler removes.
	// With the tag it still does nothing unless GIFT_METRICS says otherwise.
	var rec *metrics.Recorder
	if metrics.Enabled() {
		nominal := cfg.NominalFrameInterval
		if nominal <= 0 {
			nominal = nominalFor(tps)
		}
		rec = metrics.Start(metrics.Options{
			Timer: metrics.FrameTimerOptions{
				Nominal:   nominal,
				Tolerance: cfg.IntervalTolerance,
				Capacity:  metrics.DefaultFrameHistory,
				Warmup:    cfg.WarmupFrames,
			},
			Core: func() metrics.CoreStats { return coreMetrics(app) },
			Renderer: func() metrics.RendererStats {
				return rendererMetrics(r.Stats(), r.Atlas().Stats(), r.Textures().Stats(),
					r.Targets().Stats(), r.GlassPolicy().Stats())
			},
			// The shaper is the process wide one of internal/text, which is
			// what ui.Text measures through. The backend may not import ui —
			// the project plan, section 3 — so this is the only place both
			// sides can meet.
			Shaper: func() metrics.ShaperStats { return shaperMetrics(text.Default().Stats()) },
			Asset:  cfg.AssetStats,
		})
		defer func() { _ = rec.Close() }()
	}

	g := &game{
		app:       app,
		input:     newInputBridge(app),
		r:         r,
		rec:       rec,
		log:       cfg.Logger,
		onUpdate:  cfg.OnUpdate,
		activeTPS: tps,
		busyTPS:   tps,
		idleTPS:   cfg.IdleTPS,
		idleAfter: idleAfter,
		w:         w,
		h:         h,
	}
	if cfg.NoInput {
		g.input = nil
	}

	// Before the window opens and on this goroutine, which is the UI
	// executor: the seam has to hold the App before the first update can
	// deliver anything to it. Compiled away without the giftauto tag.
	if autoEnabled {
		autoStart(app)
	}

	eb.SetWindowTitle(cfg.Title)
	eb.SetWindowSize(w, h)
	eb.SetWindowResizingMode(eb.WindowResizingModeEnabled)
	eb.SetTPS(tps)

	if cfg.Logger != nil {
		cfg.Logger.Info("gift/backend/ebiten: starting",
			slog.String("title", cfg.Title),
			slog.Int("width", w), slog.Int("height", h),
			slog.Int("tps", tps), slog.Int("idle_tps", cfg.IdleTPS))
	}

	err = eb.RunGame(g)
	if cfg.Logger != nil {
		cfg.Logger.Info("gift/backend/ebiten: stopped",
			slog.Uint64("frames", g.r.Stats().Frames))
	}
	return err
}

// nominalFor is the nominal frame interval a tick rate implies, rounded up to
// a microsecond: 16.667 ms for sixty, which is the number the project plan,
// section 13, states and the one the tolerance of 0.5 ms is added to. The bare
// quotient is 16.666666 ms, and printing a threshold of 17.166666 ms next to a
// plan that binds 17.167 ms invites the reader to wonder which of the two the
// tool actually used.
//
// It is only the nominal value. What an interval is compared against is this
// plus [Config.IntervalTolerance]; both and their sum are in every report.
func nominalFor(tps int) time.Duration {
	if tps <= 0 {
		tps = 60
	}
	const step = float64(time.Microsecond)
	return time.Duration(math.Ceil(float64(time.Second)/float64(tps)/step) * step)
}

// game is the Ebitengine side of the frame loop.
type game struct {
	app *gift.App
	r   *Renderer
	rec *metrics.Recorder
	log *slog.Logger

	onUpdate func() error
	input    *inputBridge

	activeTPS, busyTPS, idleTPS, idleAfter int
	idleCount                              int

	w, h     int
	lastDraw time.Time

	// shot is the read-back buffer of [game.capture]. It stays nil, and the
	// field itself is unreachable, without the giftauto build tag.
	shot []byte

	// scale reads the device scale factor of the monitor. It is a field for
	// the same reason [inputBridge.keyPressed] and [inputBridge.appendChars]
	// are: the platform reading is the one thing a test cannot perform, and
	// with it behind a function value everything around it becomes ordinary
	// arithmetic that a test binary can check.
	//
	// It is not merely a convenience here. Calling [eb.Monitor] from a test
	// binary on darwin does not return nil, as [monitorScale] assumed for a
	// while: it traps inside GLFW's platform initialisation, because the
	// pinned documentation says Monitor "must be called on the main thread
	// before ebiten.RunGame" and a test binary is neither. So without the
	// seam, [game.LayoutF] would have no test at all rather than a skipped
	// one.
	//
	// [Run] leaves it nil and [game.monitorScale] falls back to the real
	// reading, so the production path has no indirection to pay for.
	scale func() float64
}

// monitorScale is the density reading LayoutF uses: the seam if a test
// installed one, the real monitor otherwise.
func (g *game) monitorScale() float64 {
	if g.scale != nil {
		return g.scale()
	}
	return monitorScale()
}

// Layout reports the logical screen size. Ebitengine calls it before the first
// Update, which is what makes the viewport known to the first build.
//
// It exists only to satisfy [eb.Game]. Ebitengine never calls it, because this
// type also implements [eb.LayoutFer] and the pinned documentation of
// Game.Layout says so itself: "If the game implements the interface LayoutFer,
// Layout is never called and LayoutF is called instead." Deleting it is not an
// option — the interface requires it — so it answers the same thing LayoutF
// does, in whole numbers, rather than being a second and divergent policy.
func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	w, h := g.LayoutF(float64(outsideWidth), float64(outsideHeight))
	return int(w), int(h)
}

// LayoutF reports the size of the offscreen gift draws into, in physical
// pixels.
//
// # What Ebitengine means by these numbers
//
// Quoted from the pinned module, ebiten/v2@v2.10.1, run.go:
//
//	LayoutF accepts a native outside size in device-independent pixels and
//	returns the game's logical screen size in pixels. The logical size is
//	used for 1) the screen size given at Draw and 2) calculation of the
//	scale from the screen to the final screen size. For 1), the actual
//	screen size is the logical size rounded up.
//
// Two things follow, and they are the whole of the project plan, section 18,
// on this side. The outside size is in *device-independent* units, so it is
// gift's logical viewport and is handed to [gift.App.Update] unchanged. The
// returned size is what the screen image at Draw is, and the ratio between
// the two is the scale Ebitengine applies on the way to the framebuffer.
// Returning the outside size unchanged — which is what this did until WU-W —
// therefore asks Ebitengine to take a 1x picture and filter it up to a 2x
// panel, which is the blur this work unit is about. Returning the outside
// size times the density makes that scale exactly one, and every pixel gift
// writes is a pixel of the display.
//
// # The density
//
// It comes from ebiten.Monitor().DeviceScaleFactor() and is rounded to an
// integer by [gift.App.SetDensity], which is the single rounding point of the
// project plan, section 18. It is read here, on every call, rather than once
// at start-up, because Ebitengine documents that Layout "is called almost
// every frame" and because a window dragged from a Retina panel to an
// external one changes the factor without any other notification. The cost is
// one method call and a float compare per frame.
//
// # What a fractional factor produces
//
// A monitor reporting 1.5 is rounded to 2, so this returns twice the outside
// size while Ebitengine's final screen is 1.5 times it. Ebitengine then
// *downsamples* by three quarters instead of upsampling by one half. That is
// supersampling: sharper than the 1x frame it replaces, softer than a true
// 1.5 would be, and it costs the fragments of a 2x frame. The plan excludes
// fractional scaling; this is what "round it and document the result" looks
// like in pixels.
func (g *game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	if outsideWidth > 0 && outsideHeight > 0 {
		g.w, g.h = int(outsideWidth), int(outsideHeight)
	}
	d := float64(g.app.SetDensity(g.monitorScale()))
	return float64(g.w) * d, float64(g.h) * d
}

// monitorScale is the raw device scale factor of the monitor the window is
// on, or 1 when there is no monitor.
//
// # When it may be called
//
// The pinned documentation, ebiten/v2@v2.10.1 monitor.go, is explicit on two
// points and both matter here: [eb.Monitor] "returns nil before the main loop
// starts", and it "must be called on the main thread before
// ebiten.RunGame". This function is only ever reached from [game.LayoutF],
// which Ebitengine calls from inside its own loop on its own main thread, so
// production use satisfies both.
//
// The nil check is therefore for the window between process start and the
// first frame, not, as this comment claimed until WU-AA, for "the headless
// case a test binary runs in". A test binary does not get nil: on darwin the
// call traps inside glfw.platformInit, which is the documented rule being
// enforced rather than a bug. Nothing in a test may call this, and nothing
// does — [game.scale] is the seam that makes that possible.
func monitorScale() float64 {
	m := eb.Monitor()
	if m == nil {
		return 1
	}
	return m.DeviceScaleFactor()
}

// Update runs the application tick and gift's build and layout.
//
// Nothing here paints. Calling [gift.App.Paint] from an update is a contract
// violation that the core rejects with a panic, and it would also be wrong
// arithmetic: several updates may precede one drawn frame.
func (g *game) Update() error {
	var start time.Time
	if metrics.Enabled() {
		start = time.Now()
	}

	// Input first, and inside Update. Every handler an event fires runs here,
	// so a state write it makes is picked up by the g.app.Update below, in
	// this same tick. The project plan, section 6, allows build and layout
	// only in Update, and the UI executor assertion assumes exactly this.
	if g.input != nil {
		g.input.poll()
	}

	if g.onUpdate != nil {
		if err := g.onUpdate(); err != nil {
			return err
		}
	}

	// One age tick of the shaping cache per update. It is deliberately not in
	// Draw: the cache is CPU work driven by layout, layout runs in Update,
	// and the atlas — which is GPU memory budgeted per drawn frame — is
	// ticked in EndFrame instead. See the project plan, section 6.
	text.Default().Tick()

	err := g.app.Update(geom.Sz(float32(g.w), float32(g.h)))

	// The idle policy reads the paint flag, which the update sets when a
	// build or a layout actually changed something. Reading it here, before
	// the draw clears it, is the only place where it says "this tick did
	// work".
	if g.idleTPS > 0 {
		g.applyIdlePolicy(g.app.NeedsPaint())
	}

	if metrics.Enabled() {
		g.rec.RecordUpdate(time.Since(start))
		g.rec.Tick()
	}
	g.logGlassLevel()
	return err
}

// logGlassLevel reports an adaptive quality change through the configured
// logger, once per change.
//
// # Why this is not a violation of the no-logging-in-the-frame-path rule
//
// The project plan, section 15, asks for two things and they are different
// things. "Zaehler statt Logzeilen" governs what the frame path writes per
// frame, and the effective glass level obeys it: it is
// [GlassPolicyStats.EffectiveLevel], a plain enum field. But the same section
// also asks for "`Warn` fuer degradierte Qualitaet, etwa Rueckfall auf Glass
// Reduced", and a fall back is not a per frame quantity — it happens a handful
// of times in a whole run. Only counting it meant the one event section 15
// names by name was invisible outside a giftmetrics build.
//
// The cost when nothing changed is a nil check and a bool load; no attribute
// is constructed, so the 0 B/op benchmark of the project plan, section 11, is
// unaffected. It runs after gift's build and layout, not between them.
func (g *game) logGlassLevel() {
	if g.log == nil {
		return
	}
	p := g.r.GlassPolicy()
	if p == nil {
		return
	}
	q, ok := p.TakeLevelChange()
	if !ok {
		return
	}
	s := p.Stats()
	if q == render.Full {
		g.log.Info("gift/backend/ebiten: glass quality restored",
			slog.String("level", q.String()),
			slog.Float64("material_area_fraction", s.AreaFraction),
			slog.Uint64("changes", s.Changes))
		return
	}
	// Warn, because this is degraded quality and section 15 names exactly
	// this case as its example of it.
	g.log.Warn("gift/backend/ebiten: glass quality degraded",
		slog.String("level", q.String()),
		slog.Bool("pinned", s.Pinned),
		slog.Duration("median_frame_interval", s.MedianInterval),
		slog.Float64("material_area_fraction", s.AreaFraction),
		slog.Uint64("changes", s.Changes))
}

// applyIdlePolicy raises or lowers the tick rate. See [Config.IdleTPS].
func (g *game) applyIdlePolicy(changed bool) {
	if changed {
		g.idleCount = 0
		if g.activeTPS != g.busyTPS {
			g.activeTPS = g.busyTPS
			eb.SetTPS(g.busyTPS)
		}
		return
	}
	if g.idleCount < g.idleAfter {
		g.idleCount++
		return
	}
	if g.activeTPS != g.idleTPS {
		g.activeTPS = g.idleTPS
		eb.SetTPS(g.idleTPS)
	}
}

// Draw produces and submits the display list of one frame.
//
// Everything the project plan, section 11, budgets per drawn frame is counted
// here and not in Update, because Update runs at a different rate.
func (g *game) Draw(screen *eb.Image) {
	var start time.Time
	if metrics.Enabled() {
		start = time.Now()
		if !g.lastDraw.IsZero() {
			g.rec.RecordInterval(start.Sub(g.lastDraw))
		}
		g.lastDraw = start
	}

	b := screen.Bounds()
	// Physical pixels, because [game.LayoutF] asked for a device sized
	// screen. That is the space the renderer works in — clips, the glass
	// region and the scene target are all measured against it — and it is
	// the density times the viewport [game.Update] hands to gift.
	size := geom.Sz(float32(b.Dx()), float32(b.Dy()))

	g.r.SetTarget(screen)
	g.r.BeginFrame(size)
	g.r.Submit(g.app.Paint())
	g.r.EndFrame()

	// The automation seam of the project plan's debugging tooling, and the
	// only place a screenshot of the *real* framebuffer can be taken: this is
	// the image the window presents. autoEnabled is a compile time constant
	// false without the giftauto build tag, so without it neither the branch
	// nor the read-back buffer exists. See automation_giftauto.go.
	if autoEnabled && autoWantsFrame() {
		autoFrame(g.capture(screen))
	}

	if metrics.Enabled() {
		g.rec.RecordDraw(time.Since(start))
	}
}

// capture reads the framebuffer back into the reusable buffer and returns it
// as a [Frame].
//
// The buffer is kept between captures because a 1920x1080 read-back is 8 MiB
// and a driver script takes screenshots in a loop. It is handed out to the
// seam, which copies what it needs before returning; the seam is called
// synchronously from Draw for exactly that reason.
func (g *game) capture(screen *eb.Image) Frame {
	b := screen.Bounds()
	w, h := b.Dx(), b.Dy()
	if n := w * h * 4; cap(g.shot) < n {
		g.shot = make([]byte, n)
	} else {
		g.shot = g.shot[:n]
	}
	screen.ReadPixels(g.shot)
	return Frame{Width: w, Height: h, Pix: g.shot, Count: g.r.Stats().Frames}
}

// rendererMetrics converts the backend's own counters into the plain struct
// the metrics package defines.
//
// The conversion sits here and not there on purpose: metrics must not import
// a backend, or the backend could not call into it. See [metrics.RendererStats].
func rendererMetrics(s RendererStats, a AtlasStats, t TextureStats,
	g TargetStats, p GlassPolicyStats) metrics.RendererStats {
	return metrics.RendererStats{
		GlassOps:          s.GlassOps,
		GlassReducedOps:   s.GlassReducedOps,
		GlassFullOps:      s.GlassFullOps,
		GlassFallbacks:    s.GlassFallbacks,
		GlassPasses:       s.GlassPasses,
		GlassDrawCalls:    s.GlassDrawCalls,
		GlassLevel:        s.GlassLevel.String(),
		GlassPinned:       s.GlassPinned,
		GlassLevelChanges: p.Changes,
		Targets: metrics.TargetStats{
			Leases: g.Leases, Reuses: g.Reuses,
			Allocations: g.Allocations, Deallocations: g.Deallocations,
			Evictions: g.Evictions, AgeEvictions: g.AgeEvictions,
			Rejected: g.Rejected,
			Targets:  g.Targets, Bytes: g.Bytes, PeakBytes: g.PeakBytes,
		},
		Frames:             s.Frames,
		DrawCalls:          s.Batches,
		Ops:                s.Ops,
		SkippedNone:        s.SkippedNone,
		SkippedTransparent: s.SkippedTransparent,
		SkippedEmptyBounds: s.SkippedEmptyBounds,
		SkippedEmptyClip:   s.SkippedEmptyClip,
		SkippedOutsideClip: s.SkippedOutsideClip,
		SkippedZeroStroke:  s.SkippedZeroStroke,
		SkippedEmptyText:   s.SkippedEmptyText,
		SkippedNoImage:     s.SkippedNoImage,
		UnknownKinds:       s.UnknownKinds,
		ShapeDrawCalls:     s.ShapeBatches,
		GlyphDrawCalls:     s.GlyphBatches,
		ImageDrawCalls:     s.ImageBatches,
		GlyphQuads:         s.GlyphQuads,
		ImageOps:           s.ImageOps,
		ShadowOps:          s.ShadowOps,
		ShadowSharpOps:     s.ShadowSharpOps,
		Atlas: metrics.AtlasStats{
			Hits: a.Hits, Misses: a.Misses,
			Rasterised: a.Rasterised, UploadedBytes: a.UploadedBytes,
			PageEvictions: a.PageEvictions, GlyphEvictions: a.GlyphEvictions,
			Rejected: a.Rejected,
			Pages:    a.Pages, Glyphs: a.Glyphs, Bytes: a.Bytes,
		},
		Textures: metrics.TextureStats{
			Uploads: t.Uploads, UploadedBytes: t.UploadedBytes,
			Deferred: t.Deferred, Rejected: t.Rejected,
			Evictions: t.Evictions, AgeEvictions: t.AgeEvictions,
			ExplicitReleases: t.ExplicitReleases, Deallocations: t.Deallocations,
			Stale:    t.Stale,
			Textures: t.Textures, Bytes: t.Bytes, PeakBytes: t.PeakBytes,
		},
	}
}

// shaperMetrics converts the shaping cache counters of internal/text into the
// plain struct the metrics package defines, for the same reason
// [rendererMetrics] exists: metrics must not import what calls into it.
func shaperMetrics(s text.Stats) metrics.ShaperStats {
	return metrics.ShaperStats{
		Present:      true,
		Hits:         s.Hits,
		Misses:       s.Misses,
		Evictions:    s.Evictions,
		AgeEvictions: s.AgeEvictions,
		ShapedGlyphs: s.ShapedGlyphs,
		Entries:      s.Entries,
		Bytes:        uint64(s.Bytes),
	}
}

// coreMetrics converts gift's own counters into the plain struct the metrics
// package declares. The conversion lives here, and not there, because
// metrics deliberately imports nothing from the framework it measures; the
// backend is the one place that holds both.
func coreMetrics(app *gift.App) metrics.CoreStats {
	d := app.Diagnostics()
	return metrics.CoreStats{
		Frames:         d.Frames,
		Builds:         d.Builds,
		Layouts:        d.Layouts,
		PaintedNodes:   d.PaintedNodes,
		PaintedOps:     d.PaintedOps,
		LiveNodes:      d.LiveNodes,
		LiveScopes:     d.LiveScopes,
		OverflowNodes:  d.OverflowNodes,
		OverflowExtent: d.OverflowExtent,
		Scrolls:        d.Scrolls,
		HitTests:       d.HitTests,
		InputEvents:    d.InputEvents,
	}
}
