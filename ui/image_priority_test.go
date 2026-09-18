package ui_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// The picture half of "a hidden subtree costs nothing", which is not about
// frames at all.
//
// [ui.ImageView] schedules its request to the asset pipeline from its
// *layouter*, and a hidden subtree is still laid out. [ui.TabBarView] builds
// every tab before the first frame. So a kiosk with pictures on four tabs used
// to put all of them into the queue at asset.Visible priority, competing with
// the tab the user is looking at for what on a Pi 4 is one core of decode
// budget. Nothing was wrong with the pixels; the tab the user was on simply
// filled in last.

// loggedSource is a picture whose bytes are handed over only when the test says
// so, and which records the order in which the pipeline opened it.
type loggedSource struct {
	src   asset.Source
	name  string
	gate  chan struct{}
	order *openLog
}

// openLog is the shared record of which source was opened when.
type openLog struct {
	mu   sync.Mutex
	seen []string
}

func (l *openLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, name)
}

func (l *openLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seen...)
}

// indexOf is the position of name in the log, or -1.
func (l *openLog) indexOf(name string) int {
	for i, s := range l.snapshot() {
		if s == name {
			return i
		}
	}
	return -1
}

func (g *loggedSource) Metadata() asset.Metadata { return g.src.Metadata() }

func (g *loggedSource) Open(ctx context.Context) (io.ReadCloser, error) {
	if g.gate != nil {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	g.order.add(g.name)
	return g.src.Open(ctx)
}

// TestPicturesOnAHiddenTabWaitForTheOnesOnScreen is MAJOR 5's image half.
//
// The pipeline runs one worker here, so the order it opens sources in is the
// order it decided to do the work in, and that is exactly what a priority is
// for. The first request is a gate: it occupies the single worker while the
// two tab requests queue up behind it, because a queue with nothing in it
// cannot express a preference and a test that let the worker take each request
// as it arrived would only be measuring the order they were made in.
//
// The hidden tab is deliberately the one whose picture is requested *first* —
// it is tab zero and layers are laid out in order — so a failure to prioritise
// shows up as the hidden picture being decoded before the visible one.
func TestPicturesOnAHiddenTabWaitForTheOnesOnScreen(t *testing.T) {
	_, items, srcs := writePictures(t, 3)
	log := &openLog{}
	gate := make(chan struct{})

	gated := func(i int, name string, g chan struct{}) *loggedSource {
		return &loggedSource{src: srcs[items[i].ID], name: name, gate: g, order: log}
	}
	blocker := gated(0, "gate", gate)
	hiddenPic := gated(1, "hidden", nil)
	visiblePic := gated(2, "visible", nil)

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{
		Deliver: del.deliver,
		Sizes:   []int{64},
		Workers: 1,
	})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(func() {
		open()
		pipe.Close()
		del.drain()
	})
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	t.Cleanup(ui.ResetImageService)

	gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  geom.Sz(640, 480),
		Root: func(ctx *gift.Context) gift.View {
			sel := ctx.State("tab", 1)
			return ui.VStack(
				ui.Image(blocker).Size(64).Frame(64, 64).Key("gate"),
				ui.TabBar(ctx.Read(sel), sel.Set,
					ui.Tab("Hidden", ui.Symbol{}, ui.Image(hiddenPic).Size(64).Frame(64, 64)),
					ui.Tab("Visible", ui.Symbol{}, ui.Image(visiblePic).Size(64).Frame(64, 64)),
				).Flex(1),
			)
		},
	})

	// The worker is inside blocker.Open by now, and both tab requests are in
	// the queue behind it. Let it go and wait for the other two to arrive.
	waitFor(t, func() bool { return log.indexOf("gate") < 0 }, "the gate picture was never opened")
	open()
	waitFor(t, func() bool {
		return log.indexOf("hidden") >= 0 && log.indexOf("visible") >= 0
	}, "the two tab pictures were never both opened")

	hi, vi := log.indexOf("hidden"), log.indexOf("visible")
	if hi < vi {
		t.Fatalf("the pipeline decoded the picture on the hidden tab before the one the "+
			"user is looking at: %v. Every tab is built and laid out, so every picture "+
			"on every tab is requested; the one nobody can see has to be asked for at "+
			"asset.Prefetch and not at asset.Visible", log.snapshot())
	}
}

// waitFor polls cond until it holds or the test gives up. The pipeline is a
// worker pool and the test goroutine is not in it, so there is nothing to
// synchronise on except the effect.
func waitFor(t *testing.T, cond func() bool, complaint string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s within ten seconds", complaint)
		}
		time.Sleep(time.Millisecond)
	}
}
