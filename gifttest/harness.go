package gifttest

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// DefaultSize is the viewport a [Harness] uses when [Options.Size] is zero.
// It is a plain desktop window, large enough that a counter or a form is not
// squeezed into an overflow nobody meant to test.
var DefaultSize = geom.Sz(800, 600)

// DefaultMaxFrames bounds [Harness.Settle].
//
// Sixty four is far more than any honest settling needs — a state write
// settles in two frames, a chain of three dependent writes in four — and far
// less than a test runner's patience. The number exists so that a view which
// invalidates itself from its own build fails with a diagnosis rather than
// spinning until the test times out with nothing to show for it.
const DefaultMaxFrames = 64

// Options configures a [Harness].
type Options struct {
	// Root is the root component function, exactly as [gift.Options.Root].
	Root func(*gift.Context) gift.View

	// View is a convenience for a test that has no state of its own: the
	// given view is returned from a root component that is rebuilt only when
	// gift asks. Exactly one of Root and View must be set.
	View gift.View

	// Size is the viewport. Zero means [DefaultSize].
	Size geom.Size

	// Logger is passed straight to gift. Nil means no logging, which is what
	// a test normally wants; pass slog.New(slog.NewTextHandler(os.Stderr,
	// nil)) to see the lifecycle.
	Logger *slog.Logger

	// MaxFrames bounds [Harness.Settle]. Zero means [DefaultMaxFrames].
	MaxFrames int

	// Density is the device density the application under test runs at, as a
	// raw platform factor. Zero means 1, which is what every test that does
	// not care wants and is bit-identical to a harness that never heard of
	// densities.
	//
	// It is the raw factor and not the rounded one on purpose: the rounding
	// of the project plan, section 18, is [gift.RoundDensity] and it happens
	// in [gift.App.SetDensity], so a test that passes 1.5 exercises the same
	// rounding a 1.5x monitor would get rather than a second implementation
	// of it. [Harness.Density] reports what came out.
	//
	// The viewport, every bound a selector reports and every coordinate an
	// action uses stay in logical pixels at any density. What changes is the
	// size of the image [Harness.AssertGolden] compares — it is the viewport
	// times the density, in physical pixels — and the resolution of the
	// glyph masks and thumbnails inside it.
	Density float64

	// There is deliberately no Background field here any more.
	//
	// There used to be one, and it defaulted to opaque white. It meant that
	// every golden image this package produced was a picture of the
	// application *plus one rectangle the application never painted* — and
	// that is the one thing a golden must never contain, because it is
	// precisely the thing a golden cannot report. Four demo screens shipped
	// with thirty to forty per cent of their pixels at {0, 0, 0, 0} while
	// twelve goldens of those very screens were green.
	//
	// The harness now clears to transparent black, which is exactly what
	// Ebitengine hands a real window at the top of every Draw; see the
	// project plan, section 6. A view that wants a background paints one, and
	// [ui.Window] is how an application says so in one line. A test whose
	// subject is a component rather than a window wraps it the same way:
	//
	//	gifttest.New(t, gifttest.Options{View: ui.Window(card), Theme: ui.DarkTheme()})
	//
	// [Harness.AssertOpaque] is the assertion that the obligation was met.

	// Theme is the colour theme the views under test resolve their semantic
	// colours against, for the duration of the test.
	//
	// It exists for exactly the reason [Options.Font] does. The theme is
	// process wide — see [ui.SetTheme] and the project plan, section 20 — so
	// a golden image of an application that uses [ui.ColorSurface] depends on
	// which theme some other test in the process happened to leave installed.
	// Setting it here makes that dependency a line in the test rather than an
	// ordering accident.
	//
	// The zero Theme means "leave the installed theme alone", so an existing
	// test keeps behaving exactly as it did. A test that wants the default
	// explicitly writes ui.LightTheme().
	Theme ui.Theme

	// Font is the font every [ui.Text] under test is measured and drawn with,
	// unless a view names its own with ui.TextView.Font.
	//
	// Set it in any test whose result depends on glyph shapes: every golden
	// image, every assertion on a width, every layout that wraps. Without it
	// the harness uses whatever [ui.SetDefaultFont] was last given anywhere in
	// the process — which is to say, a golden that passes today because
	// another file in the same package happened to install Roboto first, and
	// fails the day that file is deleted or reordered. That hazard is the
	// reason this field exists.
	//
	// A consumer outside this module gets a deterministic golden by embedding
	// its own typeface, or by importing one of the bundled ones:
	//
	//	import _ "github.com/worldiety/gift/font/inter"
	//
	// The TestMain is not optional for a golden: without it
	// AssertGolden panics from inside Ebitengine, because pixels
	// can only be read back on the main loop. See [Main].
	//	func TestMain(m *testing.M) { gifttest.Main(m) }
	//
	//	h := gifttest.New(t, gifttest.Options{
	//		View: view,
	//		Font: ui.MustFont(ui.FontQuery{Family: inter.Family}),
	//	})
	//
	// The zero Font means "leave the process default alone", so an existing
	// test keeps behaving exactly as it did.
	//
	// # What it does under the hood, and why that is stated here
	//
	// gift's default font is process wide — the project plan, section 13,
	// explains why it is not per App — so the harness installs Font as that
	// default for the duration of the test and restores the previous value
	// through TB.Cleanup. The *test* touches no global state and that is the
	// point; the process still has exactly one, and the rule against t.Parallel
	// in the package documentation applies to this field with particular
	// force. Two parallel tests with two different fonts would take turns
	// clobbering one variable.
	//
	// A test that also changes the default font itself has to do so with
	// t.Cleanup and not with defer. Go runs every deferred function before the
	// first cleanup, so a defer would restore its font before the harness
	// restores the one it found, and the harness would have the last word.
	Font ui.Font
}

// Harness is one application under test: the [gift.App], the injected clock
// and the viewport.
//
// It belongs to the goroutine that created it, like the App inside it. Every
// failure it reports goes through the [TB] it was given, with
// t.Helper already called, so a failure points at the line of the test rather
// than at a line of this package.
type Harness struct {
	t   TB
	app *gift.App

	size      geom.Size
	density   float32
	now       time.Duration
	maxFrames int

	// list is the display list of the most recent paint. It is borrowed from
	// the App and is valid until the next frame, which is why every accessor
	// that hands data out of it copies; see [Harness.Ops].
	list *render.List

	// renderer is the backend renderer of the golden path, created on first
	// use and kept. It is typed as any because this file has no build tag and
	// must not name a backend; only golden_gpu.go, which does, casts it.
	//
	// Kept and not created per call, because a renderer owns the GPU image
	// residency: a test that renders two frames of a gallery would otherwise
	// upload every thumbnail twice and would never see a texture survive a
	// frame, which is half of what an image golden is checking.
	renderer any

	// mouse is where the harness last put the mouse pointer, so that
	// Release can happen where Press left it without the test restating the
	// coordinate.
	mouse geom.Point
}

// New mounts the view and brings it to a settled, painted steady state.
//
// The returned harness is ready for assertions: layout has run, a first frame
// has been painted, and the display list reflects it.
func New(t TB, opts Options) *Harness {
	t.Helper()
	switch {
	case opts.Root == nil && opts.View == nil:
		t.Fatalf("gifttest.New: set either Options.Root or Options.View")
	case opts.Root != nil && opts.View != nil:
		t.Fatalf("gifttest.New: set Options.Root or Options.View, not both")
	}
	root := opts.Root
	if root == nil {
		v := opts.View
		root = func(*gift.Context) gift.View { return v }
	}
	size := opts.Size
	if size.IsZero() {
		size = DefaultSize
	}
	maxFrames := opts.MaxFrames
	if maxFrames <= 0 {
		maxFrames = DefaultMaxFrames
	}

	// Before the App exists, because the first Settle below already builds and
	// therefore already resolves both. See [Options.Theme] and [Options.Font].
	if opts.Theme != (ui.Theme{}) {
		prev := ui.CurrentTheme()
		ui.SetTheme(nil, opts.Theme)
		t.Cleanup(func() { ui.SetTheme(nil, prev) })
	}
	if !opts.Font.IsZero() {
		prev := ui.DefaultFont()
		ui.SetDefaultFont(opts.Font)
		t.Cleanup(func() { ui.SetDefaultFont(prev) })
	}
	h := &Harness{
		t:         t,
		app:       gift.New(gift.Options{Root: root, Logger: opts.Logger}),
		size:      size,
		maxFrames: maxFrames,
	}
	// Before the first Settle, so that the first build and the first layout
	// already see the density and no test has to settle twice to get the
	// thumbnail rung it asked for. A zero Density is 1, and setting 1 on an
	// App that is already at 1 changes nothing and invalidates nothing.
	h.density = h.app.SetDensity(densityOr1(opts.Density))
	h.Settle()
	return h
}

// App returns the application under test, for the cases this package has no
// verb for. Using it is not cheating; it is the escape hatch that keeps the
// harness from having to wrap everything gift will ever grow.
func (h *Harness) App() *gift.App { return h.app }

// densityOr1 maps the zero value of [Options.Density] onto 1.
func densityOr1(f float64) float64 {
	if f == 0 {
		return 1
	}
	return f
}

// Density returns the device density in force, after the rounding of
// [gift.RoundDensity]. A harness built with Density 1.5 reports 2.
func (h *Harness) Density() float32 { return h.density }

// deviceSize is the viewport in physical pixels, which is the size of the
// image a golden compares. It rounds up, like Ebitengine does when it turns
// the float screen size of LayoutF into an image.
func (h *Harness) deviceSize() (int, int) {
	return int(math.Ceil(float64(h.size.W) * float64(h.density))),
		int(math.Ceil(float64(h.size.H) * float64(h.density)))
}

// Size returns the current viewport.
func (h *Harness) Size() geom.Size { return h.size }

// Now returns the value of the injected clock, as a duration since an
// arbitrary origin. It starts at zero and moves only through
// [Harness.Advance].
func (h *Harness) Now() time.Duration { return h.now }

// Resize changes the viewport and settles. It is how a layout is tested at two
// widths without building two applications.
func (h *Harness) Resize(s geom.Size) {
	h.t.Helper()
	h.size = s
	h.Settle()
}

// Frame advances exactly one frame: input phase, update, paint.
//
// Use it when the *number* of frames is the thing under test. Everything else
// should use [Harness.Settle], which is what the actions do.
func (h *Harness) Frame() {
	h.t.Helper()
	h.app.BeginInput(h.now)
	if err := h.app.Update(h.size); err != nil {
		h.t.Fatalf("gifttest: App.Update: %v", err)
	}
	h.list = h.app.Paint()
}

// Settle pumps frames until the application stops changing, and fails if it
// never does.
//
// "Stops changing" means one full frame in which no component was rebuilt and
// no layouter ran — the two counters [gift.Diagnostics] keeps for exactly this
// purpose. Paint is not part of the condition, because gift repaints the whole
// visible list every frame by design; see the project plan, section 6.
//
// # Why the bound is a feature
//
// A view whose build writes the state its build reads is an infinite rebuild
// loop. In a window it presents as a program that is busy but never wrong; in
// a test without this bound it presents as a test binary that hangs and is
// killed after ten minutes with no output. Here it presents as a failure that
// names the frame count and the number of builds that happened in the last
// frame, which is enough to find it.
func (h *Harness) Settle() {
	h.t.Helper()
	for i := 0; i < h.maxFrames; i++ {
		before := h.app.Diagnostics()
		h.Frame()
		after := h.app.Diagnostics()
		if after.Builds == before.Builds && after.Layouts == before.Layouts {
			return
		}
	}
	last := h.app.Diagnostics()
	h.Frame()
	now := h.app.Diagnostics()
	h.t.Fatalf("gifttest: the application did not settle within %d frames.\n"+
		"The last frame still rebuilt %d scope(s) and ran %d layouter(s), so something "+
		"invalidates itself every frame — the usual cause is a component whose build "+
		"writes a state its build reads, or a view that allocates a new value where a "+
		"comparable one was expected.\n"+
		"Total so far: %d builds, %d layouts, %d frames.\n%s",
		h.maxFrames,
		now.Builds-last.Builds, now.Layouts-last.Layouts,
		now.Builds, now.Layouts, now.Frames,
		h.Dump())
}

// Advance moves the injected clock forward by d and settles.
//
// This is the whole of gift's time model as a test sees it: [gift.App.BeginInput]
// takes the timestamp, so moving the clock and opening an input phase is what
// makes a long press fire. No goroutine sleeps and no wall clock is read, so a
// 500 ms gesture costs microseconds and cannot flake under load.
//
// A negative duration panics rather than moving a monotonic clock backwards.
func (h *Harness) Advance(d time.Duration) {
	h.t.Helper()
	if d < 0 {
		panic(fmt.Sprintf("gifttest: Advance(%v): the clock is monotonic", d))
	}
	h.now += d
	h.app.BeginInput(h.now)
	h.Settle()
}

// Diagnostics returns the counter snapshot of the application, for the tests
// whose subject is the frame path itself — "this hover rebuilt nothing" is an
// assertion about [gift.Diagnostics.Builds].
func (h *Harness) Diagnostics() gift.Diagnostics { return h.app.Diagnostics() }

// Ops returns a copy of the display list of the most recent frame.
//
// It is a copy on purpose. The list gift hands out is borrowed and is reset at
// the start of the next frame; a test that keeps the borrowed slice and then
// clicks something reads recycled memory, and the resulting failure would be
// blamed on the wrong thing. The copy costs one allocation per call, in a
// test, which is the correct place to spend it.
func (h *Harness) Ops() []render.Op {
	h.t.Helper()
	if h.list == nil {
		h.t.Fatalf("gifttest: Ops before the first frame")
		return nil
	}
	src := h.list.Ops()
	out := make([]render.Op, len(src))
	copy(out, src)
	return out
}

// List returns the borrowed display list of the most recent frame, for the
// assertions that need the clip and transform side tables — [render.List.Clip]
// and [render.List.Glyphs] are only meaningful against the list they came
// from.
//
// It is valid until the next frame. Anything that has to outlive that must be
// copied; see [Harness.Ops].
func (h *Harness) List() *render.List {
	h.t.Helper()
	if h.list == nil {
		h.t.Fatalf("gifttest: List before the first frame")
	}
	return h.list
}

// --- input plumbing ---------------------------------------------------------

// beginInput opens an input phase at the current clock value. Every action
// calls it before dispatching, because gift requires it once per tick and
// because it is where time based gestures fire.
func (h *Harness) beginInput() { h.app.BeginInput(h.now) }

// center is the point an action aims at when it was given a node: the middle
// of its bounds. A test therefore never writes a coordinate, which is the
// entire point of the selectors.
func center(r geom.Rect) geom.Point {
	return geom.Pt(r.Min.X+r.Width()/2, r.Min.Y+r.Height()/2)
}
