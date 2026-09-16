//go:build giftauto

package auto

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// DefaultTimeout bounds every wait this package performs: the wait for a
// posted closure to run, the wait for a drawn frame and the wait for a
// framebuffer.
//
// Two seconds is far longer than a tick at any plausible rate and short
// enough that a driving script fails with a diagnosis instead of hanging in a
// CI job. Override it with GIFT_AUTO_TIMEOUT.
const DefaultTimeout = 2 * time.Second

// pollInterval is how often a wait for frames re-reads the frame counter.
//
// It is a poll and not a condition variable on purpose: the counter is bumped
// from the frame path, and a mutex there would put automation into the one
// code path this project measures in nanoseconds. Half a millisecond is a
// thirtieth of a frame at 60 Hz, so a wait for n frames is late by at most
// that.
const pollInterval = 500 * time.Microsecond

// errNotDrawn is the answer to a screenshot request that no frame arrived for.
var errNotDrawn = errors.New("no frame was drawn")

// errNoApp is the answer before the frame loop handed the application over.
var errNoApp = errors.New("the application has not started yet")

// driver is the half of this package that knows about gift and knows nothing
// about HTTP.
//
// It exists as its own type because the interesting behaviour — marshalling
// work onto the UI goroutine and waiting for it, waiting for drawn frames,
// deciding what a screenshot does when nothing is being drawn — has to be
// testable without a window. A test supplies its own post function and its own
// frame source and drives a [gift.App] by hand; see driver_test.go.
type driver struct {
	// app and post are installed by [driver.attach] when the frame loop hands
	// the application over. Both are read from HTTP goroutines.
	mu   sync.Mutex
	app  *gift.App
	post func(func())

	// drawn counts the frames the backend has drawn since the seam was
	// installed. It is written from the frame path, so it is an atomic and
	// nothing else in the frame path is touched.
	drawn atomic.Uint64

	// want is read once per drawn frame and is the flag that keeps the
	// read-back off every frame that nobody asked for.
	want atomic.Bool

	// shots are the screenshot requests waiting for the next frame, and
	// pending is the burst that is collecting consecutive ones; see
	// [driver.armBurst]. Both are under shotMu, because both are read by the
	// frame path in [driver.Frame].
	shotMu  sync.Mutex
	shots   []chan shot
	pending *burst

	timeout time.Duration

	// pointer is the last position every pointer step defaults to, so that a
	// batch can say "press here" and then "release" without repeating the
	// coordinate. It is only ever touched from a posted closure, that is on
	// the UI goroutine.
	pointer geom.Point
}

// shot is one captured framebuffer, already copied out of the backend's
// reusable buffer.
type shot struct {
	img   *image.RGBA
	count uint64
}

// burst is a request for the next n *consecutive* frames.
//
// # Why one screenshot is not enough to see a transition
//
// A screenshot request is an HTTP round trip: the input is applied on the UI
// goroutine, the answer travels back, the caller asks for pixels, and the
// frame that is captured is whichever one the window happened to draw next.
// Measured against this very application, that was between two and six frames
// after the input — and a transition is eleven frames long at 60 Hz, so a
// single capture cannot say whether the picture moved, stood still, or had
// already finished moving before the camera arrived.
//
// A burst is armed *on the UI goroutine*, as a step of an input batch, so it
// starts collecting in the same update that dispatched the tap. It then takes
// every frame the window draws, in order, with no round trip in between. The
// evidence for "this transition moves" is then a sequence of frames that
// differ from each other, and the evidence for "it ended" is that the last
// few do not — which is exactly the shape of the evidence that identified the
// defect in the first place.
type burst struct {
	n     int
	shots []shot
	done  chan struct{}
}

// newDriver returns a driver that is not attached to an application yet.
func newDriver(timeout time.Duration) *driver {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &driver{timeout: timeout}
}

// attach installs the application and the way onto its UI goroutine.
//
// post is [gift.App.Post] in a real program. It is a parameter so that a test
// can watch what was posted and when it ran.
func (d *driver) attach(app *gift.App, post func(func())) {
	d.mu.Lock()
	d.app, d.post = app, post
	d.mu.Unlock()
}

// target returns the application and the post function, or an error before
// the frame loop started.
func (d *driver) target() (*gift.App, func(func()), error) {
	d.mu.Lock()
	app, post := d.app, d.post
	d.mu.Unlock()
	if app == nil || post == nil {
		return nil, nil, errNoApp
	}
	return app, post, nil
}

// --- the synchronisation guarantee -------------------------------------------

// do runs fn on the UI goroutine and returns only after fn has actually run.
//
// This is the whole synchronisation contract of this package. An HTTP handler
// runs on its own goroutine and may touch nothing in gift; [gift.App.Post] is
// the one thread-safe way in, and a posted closure runs at the beginning of
// the next [gift.App.Update]. Waiting for it turns "the input will be applied
// at some point" into "the input has been applied", which is what makes a
// shell script driving this deterministic: when the request that pressed a
// button has answered, the press has been dispatched, the handlers it fired
// have run and the rebuild they caused is in the tree.
//
// It fails rather than blocking for ever when the UI goroutine is not
// draining posts, because a hung driver with no message is the single least
// useful failure mode a debugging tool can have.
func (d *driver) do(fn func(app *gift.App)) error {
	app, post, err := d.target()
	if err != nil {
		return err
	}
	done := make(chan struct{})
	post(func() {
		defer close(done)
		fn(app)
	})
	select {
	case <-done:
		return nil
	case <-time.After(d.timeout):
		return fmt.Errorf("the UI goroutine did not run a posted closure within %v; "+
			"the frame loop is not updating (drawn frames: %d)", d.timeout, d.drawn.Load())
	}
}

// query runs fn on the UI goroutine and hands back what it produced.
//
// It is [driver.do] with a value, and it is the shape every read-only endpoint
// uses: everything in gift except Post and Diagnostics belongs to the UI
// goroutine, so a handler builds its answer inside the closure and serialises
// it outside.
func query[T any](d *driver, fn func(app *gift.App) T) (T, error) {
	var out T
	err := d.do(func(app *gift.App) { out = fn(app) })
	return out, err
}

// --- the frame side ----------------------------------------------------------

// Start is the [ebiten.Automation] half: the frame loop hands the application
// over before the window opens.
func (d *driver) Start(app *gift.App) { d.attach(app, app.Post) }

// WantsFrame is called once per drawn frame, from the frame path.
//
// It counts the frame — which is what makes "wait for two frames" and the
// not-drawn diagnosis possible — and reports whether anybody is waiting for
// pixels. It is one atomic add and one atomic load; the read-back itself only
// happens when somebody asked.
func (d *driver) WantsFrame() bool {
	d.drawn.Add(1)
	return d.want.Load()
}

// Frame delivers the framebuffer of the frame that was just drawn to every
// waiting screenshot request.
//
// The pixels are copied here, synchronously, because the buffer belongs to the
// frame loop and is overwritten by the next capture. One copy serves every
// waiter, since nothing mutates it afterwards.
func (d *driver) Frame(w, h int, pix []byte, count uint64) {
	d.shotMu.Lock()
	waiting := d.shots
	d.shots = nil
	b := d.pending
	d.want.Store(b != nil)
	d.shotMu.Unlock()
	if len(waiting) == 0 && b == nil {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, pix)
	s := shot{img: img, count: count}
	for _, ch := range waiting {
		ch <- s
	}
	if b == nil {
		return
	}
	// The burst keeps its own copy per frame: the whole point is to hold a
	// sequence, and the buffer above belongs to the frame loop.
	b.shots = append(b.shots, s)
	if len(b.shots) < b.n {
		return
	}
	d.shotMu.Lock()
	if d.pending == b {
		d.pending = nil
		d.want.Store(len(d.shots) > 0)
	}
	d.shotMu.Unlock()
	close(b.done)
}

// armBurst starts collecting the next n frames and returns the collector.
//
// It is called from a posted closure, that is on the UI goroutine at the start
// of an update, which is what makes the first captured frame the first one
// drawn after the input of that update. A second burst replaces the first:
// there is one camera.
func (d *driver) armBurst(n int) *burst {
	b := &burst{n: n, done: make(chan struct{})}
	d.shotMu.Lock()
	d.pending = b
	d.want.Store(true)
	d.shotMu.Unlock()
	return b
}

// waitBurst blocks until the burst is full, or fails with the same diagnosis
// a screenshot gives when the window is not being drawn.
func (d *driver) waitBurst(b *burst) ([]shot, error) {
	if b == nil {
		return nil, nil
	}
	select {
	case <-b.done:
		return b.shots, nil
	case <-time.After(d.timeout):
		d.shotMu.Lock()
		if d.pending == b {
			d.pending = nil
			d.want.Store(len(d.shots) > 0)
		}
		got := len(b.shots)
		d.shotMu.Unlock()
		return b.shots, d.notDrawnError(fmt.Sprintf(
			"waited for %d consecutive frames and got %d", b.n, got))
	}
}

// --- waiting ------------------------------------------------------------------

// Frames returns the number of frames drawn since the interface started.
func (d *driver) Frames() uint64 { return d.drawn.Load() }

// waitFrames blocks until n further frames have been drawn.
//
// It is the "wait two frames" of an input batch, and it is a different verb
// from "wait 30 ms": a frame is the unit an animation, a fling step and a
// repaint are counted in, and on a machine that is drawing at 12 Hz a
// millisecond wait would not span one.
func (d *driver) waitFrames(n uint64) error {
	if n == 0 {
		return nil
	}
	target := d.drawn.Load() + n
	deadline := time.Now().Add(d.timeout)
	for d.drawn.Load() < target {
		if time.Now().After(deadline) {
			return d.notDrawnError(fmt.Sprintf("waited for %d more drawn frame(s)", n))
		}
		time.Sleep(pollInterval)
	}
	return nil
}

// waitTicks blocks until n further updates have happened.
//
// Updates and frames are different clocks and this package exposes both,
// because they are exactly the two numbers that tell "the application is
// wedged" apart from "the window is not visible". See [waitFrames] and
// [driver.notDrawnError].
func (d *driver) waitTicks(n uint64) error {
	if n == 0 {
		return nil
	}
	app, _, err := d.target()
	if err != nil {
		return err
	}
	target := app.Diagnostics().Updates + n
	deadline := time.Now().Add(d.timeout)
	for app.Diagnostics().Updates < target {
		if time.Now().After(deadline) {
			return fmt.Errorf("the application did not run %d more update(s) within %v; "+
				"the frame loop is stopped", n, d.timeout)
		}
		time.Sleep(pollInterval)
	}
	return nil
}

// screenshot returns the next frame the window draws.
//
// # What it does when nothing is being drawn, and why
//
// Ebitengine does not call Draw for a window that is not visible. A program
// launched into the background, minimised or on another workspace genuinely
// draws zero frames while its updates keep running at the tick rate. The three
// possible answers are to block for ever, to force a draw, or to fail with a
// diagnosis. Forcing a draw is not available — there is no such call, and
// rendering a second offscreen image would answer with pixels the window never
// showed, which is the reconstruction this package exists to avoid. Blocking
// for ever turns a hidden window into a hung script.
//
// So it waits, briefly, and then fails with the two counters that identify the
// case: updates advancing while frames stand still is an invisible window and
// not a broken application. Both numbers are in the error and next to every
// successful screenshot, so the two cases can never be confused for each
// other.
func (d *driver) screenshot() (shot, error) {
	if _, _, err := d.target(); err != nil {
		return shot{}, err
	}
	ch := make(chan shot, 1)
	d.shotMu.Lock()
	d.shots = append(d.shots, ch)
	d.want.Store(true)
	d.shotMu.Unlock()

	select {
	case s := <-ch:
		return s, nil
	case <-time.After(d.timeout):
		d.cancelShot(ch)
		return shot{}, d.notDrawnError("waited for a framebuffer")
	}
}

// cancelShot removes a request that timed out, so that a window which becomes
// visible later does not deliver a frame into an abandoned channel.
func (d *driver) cancelShot(ch chan shot) {
	d.shotMu.Lock()
	defer d.shotMu.Unlock()
	for i, c := range d.shots {
		if c == ch {
			d.shots = append(d.shots[:i], d.shots[i+1:]...)
			break
		}
	}
	if len(d.shots) == 0 {
		d.want.Store(false)
	}
}

// notDrawnError is the diagnosis a caller gets instead of pixels. It names the
// one cause that is not a defect.
func (d *driver) notDrawnError(what string) error {
	var updates uint64
	if app, _, err := d.target(); err == nil {
		updates = app.Diagnostics().Updates
	}
	return fmt.Errorf("%w: %s for %v; drawn frames: %d, updates: %d. "+
		"Ebitengine does not call Draw for a window that is not visible, so a program "+
		"in the background, minimised or on another workspace draws nothing while its "+
		"updates keep running. Updates climbing while frames stand still is that case "+
		"and not a frozen application; bring the window to the front",
		errNotDrawn, what, d.timeout, d.drawn.Load(), updates)
}
