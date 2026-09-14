package ebiten

import (
	"log/slog"
	"math"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/text"
	"github.com/torbenschinke/gift/metrics"
	"github.com/torbenschinke/gift/render"
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
}

// Layout reports the logical screen size. Ebitengine calls it before the first
// Update, which is what makes the viewport known to the first build.
func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	if outsideWidth > 0 && outsideHeight > 0 {
		g.w, g.h = outsideWidth, outsideHeight
	}
	return g.w, g.h
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
	return err
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
	size := geom.Sz(float32(b.Dx()), float32(b.Dy()))

	g.r.SetTarget(screen)
	g.r.BeginFrame(size)
	g.r.Submit(g.app.Paint())
	g.r.EndFrame()

	if metrics.Enabled() {
		g.rec.RecordDraw(time.Since(start))
	}
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
