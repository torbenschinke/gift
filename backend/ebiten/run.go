package ebiten

import (
	"log/slog"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
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

	// Frames receives the frame timings. If nil, [Run] creates one; use
	// [Run]'s Config to pass your own when the measurement has to be read
	// from outside.
	Frames *FrameTimer

	// OnRenderer, if non nil, is called once with the renderer before the
	// window opens. It is how an application reaches [RendererStats].
	//
	// The renderer belongs to the UI executor, so the only place the
	// application may legally read those counters from is OnUpdate.
	OnRenderer func(*Renderer)

	// OnUpdate runs at the beginning of every update, before gift builds and
	// lays out. It is where an application reads input and drives timers.
	//
	// Returning [Terminate] stops the loop and makes [Run] return nil, which
	// is how a benchmark run exits after a fixed duration. Any other non nil
	// error stops the loop and is returned by [Run].
	OnUpdate func() error
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
	ft := cfg.Frames
	if ft == nil {
		ft = NewFrameTimer(time.Second/time.Duration(tps), DefaultFrameHistory)
	}

	r, err := NewRenderer()
	if err != nil {
		return err
	}
	if cfg.OnRenderer != nil {
		cfg.OnRenderer(r)
	}

	g := &game{
		app:       app,
		r:         r,
		ft:        ft,
		log:       cfg.Logger,
		onUpdate:  cfg.OnUpdate,
		activeTPS: tps,
		busyTPS:   tps,
		idleTPS:   cfg.IdleTPS,
		idleAfter: idleAfter,
		w:         w,
		h:         h,
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

// game is the Ebitengine side of the frame loop.
type game struct {
	app *gift.App
	r   *Renderer
	ft  *FrameTimer
	log *slog.Logger

	onUpdate func() error

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
	start := time.Now()

	if g.onUpdate != nil {
		if err := g.onUpdate(); err != nil {
			return err
		}
	}

	err := g.app.Update(geom.Sz(float32(g.w), float32(g.h)))

	// The idle policy reads the paint flag, which the update sets when a
	// build or a layout actually changed something. Reading it here, before
	// the draw clears it, is the only place where it says "this tick did
	// work".
	if g.idleTPS > 0 {
		g.applyIdlePolicy(g.app.NeedsPaint())
	}

	g.ft.RecordUpdate(time.Since(start))
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
	start := time.Now()
	if !g.lastDraw.IsZero() {
		g.ft.RecordInterval(start.Sub(g.lastDraw))
	}
	g.lastDraw = start

	b := screen.Bounds()
	size := geom.Sz(float32(b.Dx()), float32(b.Dy()))

	g.r.SetTarget(screen)
	g.r.BeginFrame(size)
	g.r.Submit(g.app.Paint())
	g.r.EndFrame()

	g.ft.RecordDraw(time.Since(start))
}
