//go:build giftauto

package auto

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/ui"
)

// This file tests the automation interface without a window, which is the
// point: everything except the read-back of a real framebuffer is ordinary
// code driving an ordinary [gift.App], and the one thing that is not — the
// frame the backend hands over — is a struct of bytes a test can construct.
//
// The fake below is a frame loop: one goroutine that owns the App, updates,
// paints, and offers a framebuffer whose first pixel carries the application's
// state. That last part is what makes the synchronisation guarantee testable
// at all: a screenshot that shows the state from before the click it followed
// is a picture that says so.

var tapType = gift.RegisterType("auto.Tap")

// tapTarget is a leaf that counts activations and fills a fixed rectangle.
type tapTarget struct {
	w, h  float32
	count *int
	key   string
}

func (tapTarget) ViewType() gift.TypeID { return tapType }

func (t tapTarget) Build(*gift.BuildContext) gift.Element {
	n := &tapNode{t: t}
	return gift.Element{Key: t.key, Label: "tap me", Layouter: n, Interactor: n, Focusable: true}
}

type tapNode struct{ t tapTarget }

func (n *tapNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(n.t.w, n.t.h)
}

func (n *tapNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventPointerDown:
		ctx.RequestFocus()
		return true
	case gift.EventPointerUp:
		if e.Inside && !e.Dragged {
			*n.t.count++
		}
		return true
	}
	return false
}

// fakeLoop is a frame loop without a window: the one goroutine that owns the
// App, plus a framebuffer whose first pixel is a number the test controls.
type fakeLoop struct {
	app *gift.App
	d   *driver

	stop chan struct{}
	done chan struct{}

	mu sync.Mutex
	// drawing is whether the loop is calling WantsFrame at all. Setting it to
	// false is a window that is not visible: updates keep happening, frames
	// do not.
	drawing bool
	// pixel is what the framebuffer's first byte carries, read on the loop
	// goroutine after the update.
	pixel func() byte
}

// newFakeLoop mounts root and starts the loop.
func newFakeLoop(t *testing.T, root func(*gift.Context) gift.View, pixel func() byte) *fakeLoop {
	t.Helper()
	l := &fakeLoop{
		app:     gift.New(gift.Options{Root: root}),
		d:       newDriver(time.Second),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		drawing: true,
		pixel:   pixel,
	}
	l.d.attach(l.app, l.app.Post)
	go l.run()
	t.Cleanup(l.close)
	// Two updates before the test starts driving, because a posted closure
	// runs at the *beginning* of an update, before that update builds: input
	// dispatched in the very first update would hit a tree that does not
	// exist yet. That is not an artefact of this fake — the backend polls
	// real input at the same point of the same tick, before gift builds —
	// and a window has been up for many ticks before a human touches it.
	if err := l.d.waitTicks(2); err != nil {
		t.Fatalf("the fake loop did not start: %v", err)
	}
	return l
}

func (l *fakeLoop) run() {
	defer close(l.done)
	var now time.Duration
	for {
		select {
		case <-l.stop:
			return
		default:
		}
		now += 16 * time.Millisecond
		l.app.BeginInput(now)
		if err := l.app.Update(geom.Sz(400, 300)); err != nil {
			return
		}
		l.app.Paint()

		l.mu.Lock()
		drawing := l.drawing
		l.mu.Unlock()
		if drawing && l.d.WantsFrame() {
			pix := make([]byte, 4*4*4)
			if l.pixel != nil {
				pix[0] = l.pixel()
			}
			l.d.Frame(4, 4, pix, l.d.Frames())
		}
		time.Sleep(time.Millisecond)
	}
}

func (l *fakeLoop) setDrawing(v bool) {
	l.mu.Lock()
	l.drawing = v
	l.mu.Unlock()
}

func (l *fakeLoop) close() {
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
	<-l.done
}

// TestDoReturnsOnlyAfterTheClosureHasRunOnTheUIGoroutine is the
// synchronisation guarantee itself: when a request answers, its work has
// happened, not merely been queued.
func TestDoReturnsOnlyAfterTheClosureHasRunOnTheUIGoroutine(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 100, count: &count}
	}, nil)

	for i := range 20 {
		ran := false
		if err := l.d.do(func(*gift.App) { ran = true }); err != nil {
			t.Fatalf("do: %v", err)
		}
		// Read without synchronisation on purpose: if do returned before the
		// closure ran, this is a data race the -race build reports and a
		// false here on any build.
		if !ran {
			t.Fatalf("iteration %d: do returned before the closure ran", i)
		}
	}
}

// TestDoFailsWithADiagnosisWhenTheUIGoroutineIsNotDraining keeps the tool from
// hanging silently when the thing it drives has stopped.
func TestDoFailsWithADiagnosisWhenTheUIGoroutineIsNotDraining(t *testing.T) {
	d := newDriver(50 * time.Millisecond)
	app := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return tapTarget{w: 10, h: 10, count: new(int)}
	}})
	// Posted, never drained: nothing ever calls Update.
	d.attach(app, app.Post)
	err := d.do(func(*gift.App) { t.Error("the closure must not run") })
	if err == nil || !strings.Contains(err.Error(), "did not run a posted closure") {
		t.Fatalf("err = %v, want a diagnosis naming the undrained post", err)
	}
}

// TestAScreenshotShowsAFrameDrawnAfterThePrecedingInput is the ordering
// promise a driving script depends on: press, then screenshot, and the picture
// contains the press.
func TestAScreenshotShowsAFrameDrawnAfterThePrecedingInput(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 100, count: &count}
	}, func() byte { return byte(count) })

	for i := 1; i <= 10; i++ {
		if _, err := l.d.runBatch(Batch{Steps: []Step{{Op: "tap", X: f32(10), Y: f32(10)}}}); err != nil {
			t.Fatalf("tap %d: %v", i, err)
		}
		s, err := l.d.screenshot()
		if err != nil {
			t.Fatalf("screenshot %d: %v", i, err)
		}
		if got := s.img.Pix[0]; got != byte(i) {
			t.Fatalf("after tap %d the screenshot shows state %d; the frame predates the input", i, got)
		}
	}
}

// TestAScreenshotOfAWindowThatIsNotBeingDrawnFailsWithTheReasonAndNotWithSilence
// is the answer to the trap this interface exists to avoid: Ebitengine does
// not call Draw for an invisible window, and a driver must be able to tell
// that from a frozen application.
func TestAScreenshotOfAWindowThatIsNotBeingDrawnFailsWithTheReasonAndNotWithSilence(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 100, count: &count}
	}, nil)
	l.d.timeout = 100 * time.Millisecond
	l.setDrawing(false)

	before := l.app.Diagnostics().Updates
	_, err := l.d.screenshot()
	if err == nil {
		t.Fatal("a screenshot succeeded although no frame was drawn")
	}
	if !isNotDrawn(err) {
		t.Fatalf("err = %v, want the not-drawn sentinel", err)
	}
	for _, want := range []string{"not visible", "drawn frames:", "updates:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the diagnosis %q does not mention %q", err.Error(), want)
		}
	}
	// The other half of the diagnosis: updates kept happening, which is what
	// makes this case distinguishable from a wedged application.
	if after := l.app.Diagnostics().Updates; after <= before {
		t.Fatalf("updates did not advance while the window was not drawn: %d then %d", before, after)
	}

	// And it recovers: a window brought back to the front answers again.
	l.setDrawing(true)
	if _, err := l.d.screenshot(); err != nil {
		t.Fatalf("after drawing resumed: %v", err)
	}
}

// TestWaitFramesWaitsForDrawnFramesAndWaitTicksForUpdates keeps the two clocks
// apart, which is what makes "wait two frames" mean something on a machine
// that updates far more often than it draws.
func TestWaitFramesWaitsForDrawnFramesAndWaitTicksForUpdates(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 10, h: 10, count: &count}
	}, nil)

	before := l.d.Frames()
	if err := l.d.waitFrames(3); err != nil {
		t.Fatalf("waitFrames: %v", err)
	}
	if got := l.d.Frames(); got < before+3 {
		t.Fatalf("waitFrames(3) returned at %d, started at %d", got, before)
	}

	updates := l.app.Diagnostics().Updates
	if err := l.d.waitTicks(3); err != nil {
		t.Fatalf("waitTicks: %v", err)
	}
	if got := l.app.Diagnostics().Updates; got < updates+3 {
		t.Fatalf("waitTicks(3) returned at %d, started at %d", got, updates)
	}

	// With nothing being drawn, a frame wait fails and a tick wait does not.
	l.d.timeout = 100 * time.Millisecond
	l.setDrawing(false)
	if err := l.d.waitFrames(2); !isNotDrawn(err) {
		t.Fatalf("waitFrames with no draws = %v, want the not-drawn sentinel", err)
	}
	if err := l.d.waitTicks(2); err != nil {
		t.Fatalf("waitTicks with no draws = %v, want success: updates are unaffected by visibility", err)
	}
}

// TestAPointerStepWithoutCoordinatesUsesThePositionOfThePreviousOne is what
// lets a batch say "press here, move there, release" without repeating a
// coordinate, which is the spelling the documentation promises.
func TestAPointerStepWithoutCoordinatesUsesThePositionOfThePreviousOne(t *testing.T) {
	var count int
	// The target is pushed away from the origin on purpose: a release that
	// forgot where the press was would land at (0, 0), and a target that
	// contained the origin would swallow the mistake.
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return ui.HStack(ui.Box().Frame(80, 80), tapTarget{w: 100, h: 100, count: &count})
	}, nil)

	_, err := l.d.runBatch(Batch{Steps: []Step{
		{Op: "pointerDown", X: f32(120), Y: f32(20)},
		{Op: "waitMs", Ms: 5},
		{Op: "waitFrames", Frames: 1},
		{Op: "pointerUp"},
	}})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	got, err := query(l.d, func(app *gift.App) int { return count })
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != 1 {
		t.Fatalf("activations = %d, want 1; the release did not land where the press did", got)
	}
}

// TestAnUnknownOpIsRejectedByNameSoATypoIsNotSilence.
func TestAnUnknownOpIsRejectedByNameSoATypoIsNotSilence(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 10, h: 10, count: &count}
	}, nil)
	_, err := l.d.runBatch(Batch{Steps: []Step{{Op: "tap", X: f32(1), Y: f32(1)}, {Op: "clcik"}}})
	if err == nil || !strings.Contains(err.Error(), `step 1 (clcik): unknown op "clcik"`) {
		t.Fatalf("err = %v, want the index and the name of the bad step", err)
	}
}

// f32 is the address of a float literal, which is what an optional coordinate
// needs.
func f32(v float32) *float32 { return &v }

// TestAPointerStepIsATouchUnlessItAsksForTheMouse pins the default that keeps
// a synthesised gesture from being overtaken by the backend's per-tick cursor
// poll, and the observable difference between the two kinds: only a mouse
// sets hover.
func TestAPointerStepIsATouchUnlessItAsksForTheMouse(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 100, count: &count, key: "hit"}
	}, nil)

	hover := func() bool {
		v, err := query(l.d, func(app *gift.App) bool {
			n := flatten(buildTree(app, maxTreeDepth).Root, Filter{Key: "hit"}, nil)
			return len(n) == 1 && n[0].Hover
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	if _, err := l.d.runBatch(Batch{Steps: []Step{{Op: "tap", X: f32(10), Y: f32(10)}}}); err != nil {
		t.Fatal(err)
	}
	if hover() {
		t.Fatal("a touch tap left the node hovered; a finger has no hover")
	}
	if _, err := l.d.runBatch(Batch{Steps: []Step{{Op: "click", X: f32(10), Y: f32(10)}}}); err != nil {
		t.Fatal(err)
	}
	if !hover() {
		t.Fatal("a mouse click left no hover; the click op is not using the mouse pointer")
	}
	got, err := query(l.d, func(*gift.App) int { return count })
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("activations = %d, want 2: both the tap and the click must activate", got)
	}
}

// TestACaptureStepCollectsConsecutiveFramesAndSaysWhatChangedBetweenThem is the
// answer to the limitation this package's author reported: a screenshot is
// always *after* the input and is not always the *first* frame after it —
// measured between two and six frames late — so a movement shorter than that
// jitter cannot be told apart from no movement at all.
//
// A capture step is armed on the UI goroutine and takes every frame the window
// draws from that update onwards, in order. The evidence for motion is then a
// sequence: frames that differ from one another, and then frames that do not.
func TestACaptureStepCollectsConsecutiveFramesAndSaysWhatChangedBetweenThem(t *testing.T) {
	var count int
	// The framebuffer of this fake carries a number in its first pixel, so
	// "the picture changed" is a byte a test can arrange: here it changes for
	// three frames after the tap and then stands still, which is the shape of
	// a transition that ends.
	frames := 0
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 100, count: &count, key: "target"}
	}, func() byte {
		if count == 0 {
			return 0
		}
		frames++
		if frames > 3 {
			return 99
		}
		return byte(frames)
	})

	res, err := l.d.runBatch(Batch{Steps: []Step{
		{Op: "capture", Frames: 6},
		{Op: "tap", X: f32(10), Y: f32(10)},
	}})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(res.Captured) != 6 {
		t.Fatalf("the capture collected %d frames, want 6", len(res.Captured))
	}
	if res.Captured[0].Changed != -1 {
		t.Errorf("the first frame of a burst reports %d changed pixels; it has nothing to "+
			"be compared with and must say so", res.Captured[0].Changed)
	}
	moved, still := 0, 0
	for _, c := range res.Captured[1:] {
		if c.Changed > 0 {
			moved++
		} else if c.Changed == 0 {
			still++
		}
	}
	if moved == 0 {
		t.Errorf("no frame of the burst differs from the one before it, although the "+
			"fixture changes its framebuffer three times: %+v", res.Captured)
	}
	if still == 0 {
		t.Errorf("no frame of the burst equals the one before it, so the burst cannot show "+
			"that a movement ended: %+v", res.Captured)
	}
	for i := 1; i < len(res.Captured); i++ {
		if res.Captured[i].Frame <= res.Captured[i-1].Frame {
			t.Fatalf("the captured frames are not consecutive: %+v", res.Captured)
		}
	}
}

// TestACaptureStepCanCarryThePixelsOfEveryFrameItTook. The hashes are enough
// to prove that something moved; a person who has to look at *what* moved
// needs the pictures, and asking for them one round trip at a time is exactly
// the thing a burst exists to avoid.
func TestACaptureStepCanCarryThePixelsOfEveryFrameItTook(t *testing.T) {
	var count int
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 100, count: &count}
	}, nil)
	res, err := l.d.runBatch(Batch{Steps: []Step{{Op: "capture", Frames: 2, PNG: true}}})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	for i, c := range res.Captured {
		if c.PNG == "" {
			t.Errorf("frame %d of a burst that asked for pixels carries none", i)
		}
		if c.SHA == "" {
			t.Errorf("frame %d of a burst carries no hash", i)
		}
	}
}
