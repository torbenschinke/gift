package gifttest

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
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

	// Background is the colour a golden image is rendered onto, behind
	// everything the application draws. The zero value means opaque white.
	//
	// It exists because gift has no concept of a window background: a view
	// that wants one draws it, and a view that does not leaves whatever the
	// platform cleared the screen to. A golden image has to pick something,
	// and picking transparent black would compare the alpha channel of every
	// untouched pixel and render dark text invisible. White is the choice
	// that makes a default styled application legible; an application with a
	// dark design sets this to its own colour.
	Background render.Color
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
	now       time.Duration
	maxFrames int
	bg        render.Color

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

	bg := opts.Background
	if bg == (render.Color{}) {
		bg = render.RGB(255, 255, 255)
	}
	h := &Harness{
		t:         t,
		app:       gift.New(gift.Options{Root: root, Logger: opts.Logger}),
		size:      size,
		maxFrames: maxFrames,
		bg:        bg,
	}
	h.Settle()
	return h
}

// App returns the application under test, for the cases this package has no
// verb for. Using it is not cheating; it is the escape hatch that keeps the
// harness from having to wrap everything gift will ever grow.
func (h *Harness) App() *gift.App { return h.app }

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
