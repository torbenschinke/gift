// Command example-layout is the deliverable of step 1 of the project plan,
// section 12: a non trivial static stack scene with stable identity, drawn by
// the Ebitengine backend.
//
// It exists to be measured, not to be pretty. The scene is deterministic and
// reproducible: the same flags produce the same tree, the same display list
// and the same number of nodes on every run, so two measurements are
// comparable.
//
//	go run ./cmd/example-layout
//	go run ./cmd/example-layout -rows 40 -cells 12 -duration 60s -measure
//
// # What it demonstrates
//
//   - Nested VStack, HStack and ZStack with spacers, backgrounds, borders,
//     corner radii and clipping, over several hundred retained nodes.
//   - Stable identity: a [gift.Component] with state of its own, driven by the
//     space bar, and [gift.Memo] rows that are skipped when their props did
//     not change. The build counters make both observable.
//   - The frame model of the project plan, section 6: build and layout in
//     Ebitengine's Update, the display list in its Draw, counted separately.
//
// # Measurement output
//
// With -measure the program prints one JSON object per line on stdout: one
// every -measure-interval and one on exit. CPU times, the draw callback and
// the wall clock frame interval are three separate objects, because the
// project plan, sections 11 and 13, forbids mixing them. None of them is a
// GPU time.
//
// # The idle policy
//
// -idle-tps lowers the tick rate once nothing has changed for a while. It
// exists because Ebitengine keeps ticking and drawing sixty times a second
// even for a motionless scene, and on a Raspberry Pi that constant load turns
// into heat and then into thermal throttling, which contaminates every
// measurement taken afterwards. The project plan, section 6, names this as the
// sanctioned countermeasure and rules out on demand rendering.
//
// It is off by default because it changes what is being measured, and note
// that this scene moves its highlight once a second, so the policy only
// engages if the tick is disabled or the idle threshold is raised above sixty
// ticks.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"runtime"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/torbenschinke/gift"
	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

func main() {
	var (
		rows      = flag.Int("rows", 12, "number of rows in the body; the main complexity knob")
		cells     = flag.Int("cells", 14, "number of cells per row")
		duration  = flag.Duration("duration", 0, "exit after this long; zero runs until the window is closed")
		fps       = flag.Int("fps", 60, "target tick rate and the interval frame times are measured against")
		tolerance = flag.Duration("interval-tolerance", 500*time.Microsecond,
			"slack added to the nominal interval before a frame counts as missed; "+
				"a display is never exactly at its nominal rate, and a zero tolerance flags every frame")
		measure = flag.Bool("measure", true, "print a machine readable measurement line on exit")
		every   = flag.Duration("measure-interval", 0, "also print a measurement line this often; zero prints only on exit")
		idleTPS = flag.Int("idle-tps", 0, "tick rate to fall back to while nothing changes, against Pi thermal throttling; "+
			"zero, the default, disables the idle policy because it changes what is being measured")
		width   = flag.Int("width", 1280, "window width in logical pixels")
		height  = flag.Int("height", 720, "window height in logical pixels")
		verbose = flag.Bool("v", false, "log lifecycle events to stderr")
	)
	flag.Parse()

	if *rows < 1 {
		*rows = 1
	}
	if *cells < 1 {
		*cells = 1
	}

	var log *slog.Logger
	if *verbose {
		// gift never logs to slog.Default; the logger is passed in
		// explicitly or logging is off. See the project plan, section 15.
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	sc := &scene{rows: *rows, cells: *cells}
	app := gift.New(gift.Options{Logger: log, Root: sc.root})

	target := targetInterval(*fps)
	frames := backend.NewFrameTimer(target+*tolerance, backend.DefaultFrameHistory)

	rep := &reporter{
		app:     app,
		frames:  frames,
		scene:   sc,
		every:   *every,
		enable:  *measure,
		nominal: target,
		enc:     json.NewEncoder(os.Stdout),
	}

	cfg := backend.Config{
		Title:      "gift example-layout",
		Width:      *width,
		Height:     *height,
		Logger:     log,
		TPS:        *fps,
		IdleTPS:    *idleTPS,
		Frames:     frames,
		OnRenderer: func(r *backend.Renderer) { rep.renderer = r },
		OnUpdate: func() error {
			sc.tick()
			if inpututil.IsKeyJustPressed(eb.KeySpace) {
				sc.bump()
			}
			if inpututil.IsKeyJustPressed(eb.KeyEscape) {
				return backend.Terminate
			}
			rep.periodic()
			if *duration > 0 && time.Since(rep.started()) >= *duration {
				return backend.Terminate
			}
			return nil
		},
	}

	err := backend.Run(app, cfg)
	rep.final()
	if err != nil {
		fmt.Fprintln(os.Stderr, "example-layout:", err)
		os.Exit(1)
	}
}

// --- the scene --------------------------------------------------------------

// targetInterval is the frame interval a tick rate is measured against,
// rounded up to ten microseconds.
//
// The rounding is not cosmetic. A sixtieth of a second is 16.6666 ms, an
// actual sixty hertz frame lands a few hundred nanoseconds above that, and
// comparing against the exact quotient would report almost every perfectly
// timed frame as missed. The project plan, section 13, states the threshold as
// 16.67 ms, which is exactly what this produces for sixty.
func targetInterval(fps int) time.Duration {
	if fps <= 0 {
		fps = 60
	}
	const step = float64(10 * time.Microsecond)
	return time.Duration(math.Ceil(float64(time.Second)/float64(fps)/step) * step)
}

// scene owns the parameters and the handles to the state the input path
// drives.
//
// The state pointers are captured during the build of the component that owns
// them. That is deliberate and not a back door: the state still belongs to its
// scope, and writing it from the update callback happens on the UI executor,
// which is where writes are allowed.
type scene struct {
	rows, cells int

	frame     int
	highlight *gift.State[int]
	presses   *gift.State[int]
}

// tick advances the highlighted row once a second.
//
// Only two rows change their props, so only two memoised rows are rebuilt. The
// build counter in the measurement output is how you check that.
func (s *scene) tick() {
	s.frame++
	if s.highlight == nil {
		return
	}
	s.highlight.Set((s.frame / 60) % s.rows)
}

// bump increments the counter of the badge component.
//
// The badge has its own scope, so this rebuilds one component and nothing
// else, not the root and not the rows.
func (s *scene) bump() {
	if s.presses == nil {
		return
	}
	s.presses.Set(s.presses.Get() + 1)
}

var (
	bg      = ui.RGB(18, 20, 26)
	panelBg = ui.RGB(30, 34, 44)
	rowBg   = ui.RGB(38, 43, 56)
	rowHot  = ui.RGB(64, 96, 150)
	line    = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 40)}
	accent  = ui.RGB(220, 120, 60)
)

// root is the root component. It owns the highlighted row index, so a tick
// rebuilds it — which is exactly the situation [gift.Memo] exists for.
func (s *scene) root(ctx *gift.Context) gift.View {
	s.highlight = ctx.State("highlight", 0)
	hot := ctx.Read(s.highlight)

	body := make([]gift.View, 0, s.rows)
	for i := 0; i < s.rows; i++ {
		// The props are an explicit comparable value, so a row whose index
		// and highlight state did not change is not rebuilt at all.
		body = append(body, gift.Memo("row", rowProps{
			Index: i,
			Cells: s.cells,
			Hot:   i == hot,
		}, buildRow))
	}

	return ui.VStack(
		s.header(),
		ui.VStack(body...).
			Gap(4).
			Padding(12).
			Background(panelBg).
			CornerRadius(14).
			Border(line).
			Clip(true).
			Flex(1),
		s.footer(),
	).
		Gap(12).
		Padding(16).
		Background(bg)
}

// header is a ZStack: a bar with a title block on the left, a spacer and a
// stateful badge on the right, on an accent coloured plate.
//
// The plate is the ZStack's own Background and not a child Box. A Box without
// a frame sizes itself to the minimum of its constraints, which inside a
// ZStack is zero, so the obvious spelling ZStack(Box().Background(c), content)
// draws nothing at all. See [ui.BoxView].
func (s *scene) header() gift.View {
	return ui.ZStack(
		ui.HStack(
			ui.VStack(
				ui.Box().Frame(160, 14).Background(ui.RGB(250, 250, 250)).CornerRadius(7),
				ui.Box().Frame(110, 8).Background(ui.RGBA(255, 255, 255, 160)).CornerRadius(4),
			).Gap(6),
			ui.Spacer(),
			gift.Component("badge", s.badge),
		).Padding(14).Align(geom.Alignment{Y: 0.5}),
	).
		Background(accent).
		CornerRadius(10).
		Border(line).
		Frame(geom.Unbounded(), 72)
}

// badge is a component with a state scope of its own. The space bar writes
// that state, which rebuilds this component instance and nothing above it.
func (s *scene) badge(ctx *gift.Context) gift.View {
	s.presses = ctx.State("presses", 0)
	n := ctx.Read(s.presses)

	pips := make([]gift.View, 0, 8)
	for i := 0; i < 8; i++ {
		c := ui.RGBA(0, 0, 0, 70)
		if i < n%9 {
			c = ui.RGB(255, 255, 255)
		}
		pips = append(pips, ui.Box().Frame(10, 10).Background(c).CornerRadius(5))
	}
	return ui.HStack(pips...).
		Gap(6).
		Padding(10).
		Background(ui.RGBA(0, 0, 0, 90)).
		CornerRadius(12).
		Border(line)
}

func (s *scene) footer() gift.View {
	return ui.HStack(
		ui.Box().Frame(90, 10).Background(ui.RGBA(255, 255, 255, 120)).CornerRadius(5),
		ui.Spacer(),
		ui.Box().Frame(40, 10).Background(ui.RGBA(255, 255, 255, 60)).CornerRadius(5),
		ui.Box().Frame(40, 10).Background(ui.RGBA(255, 255, 255, 60)).CornerRadius(5),
	).Gap(8).Padding(8).Align(geom.Alignment{Y: 0.5}).Frame(geom.Unbounded(), 34)
}

// rowProps is the comparable input of a memoised row. Everything the row
// reads is in here, and nothing in here is a slice, a map or a closure.
type rowProps struct {
	Index int
	Cells int
	Hot   bool
}

// buildRow builds one row. It is a plain function of its props and has no
// state, which is what makes the memoisation sound.
func buildRow(_ *gift.Context, p rowProps) gift.View {
	back := rowBg
	if p.Hot {
		back = rowHot
	}

	cells := make([]gift.View, 0, p.Cells+2)
	cells = append(cells, ui.Box().Frame(26, 26).
		Background(tint(p.Index, 0)).
		CornerRadius(13).
		Border(line))

	for c := 0; c < p.Cells; c++ {
		// A cell is a ZStack, so the scene really does exercise all three
		// stack kinds and an overlapping draw order. Both children carry a
		// frame here because the overlap is the point; an unframed Box would
		// fill the ZStack and hide the one behind it.
		cells = append(cells, ui.ZStack(
			ui.Box().
				Frame(float32(8+(p.Index*7+c*13)%26), 8).
				Background(tint(p.Index, c+1)).
				CornerRadius(4),
			ui.Box().
				Frame(6, 6).
				Background(ui.RGBA(255, 255, 255, 200)).
				CornerRadius(3),
		).
			Align(geom.Alignment{X: 0.5, Y: 0.5}).
			Background(ui.RGBA(0, 0, 0, 60)).
			CornerRadius(6).
			Frame(38, 22).
			Clip(true))
	}
	cells = append(cells, ui.Spacer())

	return ui.HStack(cells...).
		Gap(6).
		Padding(6).
		Align(geom.Alignment{Y: 0.5}).
		Background(back).
		CornerRadius(8).
		Border(line)
}

// tint is a deterministic colour ramp. No randomness anywhere in the scene:
// two runs with the same flags must produce the same pixels.
func tint(row, col int) ui.Color {
	h := (row*37 + col*61) % 180
	return ui.RGB(uint8(60+h), uint8(90+(h/2)), uint8(200-h/2))
}

// --- measurement -------------------------------------------------------------

// reporter prints the machine readable measurement lines the project plan,
// section 13, asks for.
//
// Everything it reads is a snapshot taken out of band: [gift.Diagnostics] is
// synchronised, [backend.FrameTimer.Snapshot] is synchronised, and
// [backend.Renderer.Stats] is read from the update callback, which runs on the
// UI executor that wrote it. Nothing here runs inside the frame path.
type reporter struct {
	app      *gift.App
	frames   *backend.FrameTimer
	renderer *backend.Renderer
	scene    *scene

	every   time.Duration
	enable  bool
	nominal time.Duration
	enc     *json.Encoder

	start time.Time
	last  time.Time
	done  bool
}

func (r *reporter) started() time.Time {
	if r.start.IsZero() {
		r.start = time.Now()
		r.last = r.start
	}
	return r.start
}

func (r *reporter) periodic() {
	r.started()
	if !r.enable || r.every <= 0 {
		return
	}
	if time.Since(r.last) < r.every {
		return
	}
	r.last = time.Now()
	r.emit("periodic")
}

func (r *reporter) final() {
	if !r.enable || r.done {
		return
	}
	r.done = true
	r.emit("final")
}

// statsMillis is a duration distribution in milliseconds. Milliseconds because
// the thresholds of the project plan, section 13, are stated in them.
type statsMillis struct {
	Count int     `json:"count"`
	Min   float64 `json:"min_ms"`
	Mean  float64 `json:"mean_ms"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
	Max   float64 `json:"max_ms"`
}

func ms(s backend.Stats) statsMillis {
	f := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	return statsMillis{
		Count: s.Count,
		Min:   f(s.Min), Mean: f(s.Mean), P50: f(s.P50),
		P95: f(s.P95), P99: f(s.P99), Max: f(s.Max),
	}
}

// measurement is one line of output.
//
// The three timing series are separate fields on purpose: CPU time in the
// update callback, CPU time in the draw callback and the wall clock distance
// between drawn frames measure three different things, and the project plan,
// sections 11 and 13, forbids adding them up. None of them observes the GPU.
type measurement struct {
	Kind      string  `json:"kind"`
	ElapsedS  float64 `json:"elapsed_s"`
	Rows      int     `json:"rows"`
	Cells     int     `json:"cells"`
	LiveNodes uint64  `json:"live_nodes"`

	Gift struct {
		Frames       uint64 `json:"paints"`
		Builds       uint64 `json:"builds"`
		Layouts      uint64 `json:"layouts"`
		PaintedNodes uint64 `json:"painted_nodes"`
		PaintedOps   uint64 `json:"painted_ops"`
		LiveScopes   uint64 `json:"live_scopes"`
	} `json:"gift"`

	Renderer struct {
		Frames       uint64 `json:"frames"`
		DrawCalls    uint64 `json:"draw_calls"`
		Ops          uint64 `json:"ops"`
		Skipped      uint64 `json:"skipped_ops"`
		UnknownKinds uint64 `json:"unknown_kinds"`
	} `json:"renderer"`

	UpdateCPU     statsMillis `json:"update_cpu"`
	DrawCPU       statsMillis `json:"draw_cpu"`
	FrameInterval statsMillis `json:"frame_interval"`

	// NominalIntervalMs is the interval the target frame rate implies.
	// MissedThresholdMs is the value a frame interval is actually compared
	// against: the nominal interval plus the configured tolerance. The two
	// differ because no display runs at exactly its nominal rate, and
	// comparing against the bare quotient reports every well timed frame as
	// missed.
	NominalIntervalMs float64 `json:"nominal_interval_ms"`
	MissedThresholdMs float64 `json:"missed_threshold_ms"`
	MissedIntervals   int     `json:"missed_intervals"`
	MissedRatio       float64 `json:"missed_ratio"`
	Updates           uint64  `json:"updates"`
	Draws             uint64  `json:"draws"`

	Mem struct {
		HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
		TotalAllocled  uint64 `json:"total_alloc_bytes"`
		Mallocs        uint64 `json:"mallocs"`
		NumGC          uint32 `json:"num_gc"`
	} `json:"mem"`
}

func (r *reporter) emit(kind string) {
	d := r.app.Diagnostics()
	ft := r.frames.Snapshot()

	var m measurement
	m.Kind = kind
	m.ElapsedS = time.Since(r.started()).Seconds()
	m.Rows = r.scene.rows
	m.Cells = r.scene.cells
	m.LiveNodes = d.LiveNodes

	m.Gift.Frames = d.Frames
	m.Gift.Builds = d.Builds
	m.Gift.Layouts = d.Layouts
	m.Gift.PaintedNodes = d.PaintedNodes
	m.Gift.PaintedOps = d.PaintedOps
	m.Gift.LiveScopes = d.LiveScopes

	if r.renderer != nil {
		rs := r.renderer.Stats()
		m.Renderer.Frames = rs.Frames
		m.Renderer.DrawCalls = rs.Batches
		m.Renderer.Ops = rs.Ops
		m.Renderer.Skipped = rs.Skipped
		m.Renderer.UnknownKinds = rs.UnknownKinds
	}

	m.UpdateCPU = ms(ft.UpdateCPU)
	m.DrawCPU = ms(ft.DrawCPU)
	m.FrameInterval = ms(ft.FrameInterval)
	m.NominalIntervalMs = float64(r.nominal) / float64(time.Millisecond)
	m.MissedThresholdMs = float64(ft.TargetInterval) / float64(time.Millisecond)
	m.MissedIntervals = ft.MissedIntervals
	m.MissedRatio = ft.MissedRatio
	m.Updates = ft.Updates
	m.Draws = ft.Draws

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.Mem.HeapAllocBytes = mem.HeapAlloc
	m.Mem.TotalAllocled = mem.TotalAlloc
	m.Mem.Mallocs = mem.Mallocs
	m.Mem.NumGC = mem.NumGC

	_ = r.enc.Encode(&m)
}
