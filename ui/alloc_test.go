package ui_test

import (
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/ui"
)

// tree builds a representative layout of roughly 200 nodes out of nothing but
// public ui views: twenty rows of nine boxes with a spacer in between, a
// styled and clipped frame around the whole thing.
//
//	1 outer VStack + 20 HStacks + 20*(9 boxes + 1 spacer) = 221 nodes,
//	plus the root component node.
//
// Twenty one of those nodes carry a shadow, which is the section 13 scenario
// count and is here rather than in a test of its own so that every allocation
// contract in this file covers the shadow path automatically. There is nothing
// to warm up: the blur is evaluated in the shape shader, so a shadow has no
// cache that could allocate on a miss.
func tree(*gift.Context) gift.View {
	rows := make([]gift.View, 0, 20)
	for r := range 20 {
		cols := make([]gift.View, 0, 10)
		for c := range 9 {
			b := ui.Box().
				Frame(float32(10+c), 12).
				Background(ui.RGB(uint8(c*20), 40, 60)).
				Key(strconv.Itoa(c))
			if c == 4 {
				b = b.CornerRadius(3).
					Border(ui.Border{Width: 1, Color: ui.RGB(255, 255, 255)}).
					Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)})
			}
			cols = append(cols, b)
			if c == 4 {
				cols = append(cols, ui.Spacer().Key("gap"))
			}
		}
		rows = append(rows, ui.HStack(cols...).Gap(2).Align(geom.Center).Key(strconv.Itoa(r)))
	}
	return ui.VStack(rows...).
		Gap(4).
		Padding(8).
		Background(ui.RGBA(0, 0, 0, 40)).
		Border(ui.Border{Width: 1, Color: ui.RGB(80, 80, 80)}).
		CornerRadius(6).
		Shadow(ui.Shadow{Blur: 24, OffsetY: 8, Color: ui.RGBA(0, 0, 0, 90)}).
		Clip(true)
}

// flexTree is the same shape as [tree], but every leaf is flexible instead of
// fixed size.
//
// It exists because of the overflow model of the project plan, section 7: a
// stack measures an inflexible child with an unbounded main axis, so a fixed
// size box sees the very same constraints no matter how wide the window is and
// is legitimately answered from the layout cache. That is the caching working,
// not a defect — but it means [tree] cannot be used to exercise a relayout that
// reaches the leaves. A flexible child's main extent is derived from the free
// space, so here a resize really does invalidate all 221 nodes.
func flexTree(*gift.Context) gift.View {
	rows := make([]gift.View, 0, 20)
	for r := range 20 {
		cols := make([]gift.View, 0, 10)
		for c := range 9 {
			b := ui.Box().
				Flex(float32(1 + c%3)).
				MinHeight(12).
				Background(ui.RGB(uint8(c*20), 40, 60)).
				Key(strconv.Itoa(c))
			if c == 4 {
				b = b.CornerRadius(3).
					Border(ui.Border{Width: 1, Color: ui.RGB(255, 255, 255)}).
					Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)})
			}
			cols = append(cols, b)
			if c == 4 {
				cols = append(cols, ui.Spacer().Key("gap"))
			}
		}
		rows = append(rows, ui.HStack(cols...).Gap(2).Align(geom.Center).Key(strconv.Itoa(r)))
	}
	return ui.VStack(rows...).
		Gap(4).
		Padding(8).
		Background(ui.RGBA(0, 0, 0, 40)).
		Clip(true)
}

// TestFramePathIsAllocationFree is go/no-go criterion 3 of the project plan,
// section 12, measured through the public ui API: after warmup, neither an
// idle frame nor a full relayout may allocate.
func TestFramePathIsAllocationFree(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		a := gift.New(gift.Options{Root: tree})
		step := func() {
			if err := a.Update(geom.Sz(800, 600)); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
		for range 16 {
			step()
		}
		if n := a.Diagnostics().LiveNodes; n < 200 {
			t.Fatalf("the test tree has only %d nodes, the measurement would be meaningless", n)
		}
		if got := testing.AllocsPerRun(200, step); got != 0 {
			t.Fatalf("idle frame path allocated %v times per run, want 0", got)
		}
	})

	t.Run("full relayout", func(t *testing.T) {
		a := gift.New(gift.Options{Root: flexTree})
		w := float32(800)
		step := func() {
			if w == 800 {
				w = 801
			} else {
				w = 800
			}
			if err := a.Update(geom.Sz(w, 600)); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
		for range 16 {
			step()
		}

		before := a.Diagnostics()
		step()
		after := a.Diagnostics()
		if got := after.Layouts - before.Layouts; got < 200 {
			t.Fatalf("only %d nodes were measured, this is not a full relayout", got)
		}
		if after.Builds != before.Builds {
			t.Fatalf("a resize rebuilt %d scopes", after.Builds-before.Builds)
		}
		if got := testing.AllocsPerRun(200, step); got != 0 {
			t.Fatalf("full relayout plus paint allocated %v times per run, want 0", got)
		}
	})
}

// TestFramePathIsAllocationFreeWithDebugLogger is the ui side of the last row
// of the build checks in the project plan, section 13: "Allokationsbenchmark
// des Frame-Pfads mit aktivem Debug-Level-Logger: 0 B/op".
//
// It existed only in the root package, which does not prove anything about the
// ui layer: ui is where the overflow diagnosis, the style painters and the
// stack layouters live, and any of them could reach for the logger. An slog
// handler at debug level accepts everything, so a single stray log call in the
// frame path shows up here as an allocation.
func TestFramePathIsAllocationFreeWithDebugLogger(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, tc := range []struct {
		name string
		root func(*gift.Context) gift.View
	}{
		{"fits", tree},
		{"overflowing", overflowingTree},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := gift.New(gift.Options{Logger: log, Root: tc.root})
			w := float32(800)
			step := func() {
				if w == 800 {
					w = 801
				} else {
					w = 800
				}
				if err := a.Update(geom.Sz(w, 600)); err != nil {
					t.Fatal(err)
				}
				a.Paint()
			}
			for range 16 {
				step()
			}
			if got := testing.AllocsPerRun(200, step); got != 0 {
				t.Fatalf("frame path with an active debug logger allocated %v times per run, want 0", got)
			}
		})
	}
}

// overflowingTree is deliberately too tall for the viewport the tests use. The
// overflow accounting runs on every layout of it, so it must not allocate
// either — including in a giftdebug build, where the diagnosis is emitted once
// on the transition and never again while the state is unchanged.
func overflowingTree(*gift.Context) gift.View {
	rows := make([]gift.View, 0, 40)
	for r := range 40 {
		rows = append(rows, ui.Box().
			Frame(100, 40).
			Background(ui.RGB(uint8(r*5), 40, 60)).
			Key(strconv.Itoa(r)))
	}
	return ui.VStack(rows...).Gap(8).Frame(200, 300)
}

func BenchmarkUITreeIdle(b *testing.B) {
	a := gift.New(gift.Options{Root: tree})
	for range 8 {
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
}

func BenchmarkUITreeRelayout(b *testing.B) {
	a := gift.New(gift.Options{Root: tree})
	w := float32(800)
	for range 8 {
		_ = a.Update(geom.Sz(w, 600))
		a.Paint()
	}
	b.ReportAllocs()
	for b.Loop() {
		if w == 800 {
			w = 801
		} else {
			w = 800
		}
		_ = a.Update(geom.Sz(w, 600))
		a.Paint()
	}
}

// BenchmarkUITreeBuild reports what building the same tree costs. Build is
// explicitly outside the zero allocation contract of the project plan,
// section 11; the number is observed, not asserted.
func BenchmarkUITreeBuild(b *testing.B) {
	a := gift.New(gift.Options{Root: tree})
	_ = a.Update(geom.Sz(800, 600))
	b.ReportAllocs()
	for b.Loop() {
		a.Invalidate()
		_ = a.Update(geom.Sz(800, 600))
	}
}

// scrollTree is a scroller of two hundred rows over a 600 pixel viewport, so
// that a scroll frame paints a realistic number of nodes and the clipped ones
// still travel through the display list.
func scrollTree(*gift.Context) gift.View {
	rows := make([]gift.View, 0, 200)
	for i := range 200 {
		rows = append(rows, ui.HStack(
			ui.Box().Frame(60, 20).Background(ui.RGB(uint8(i), 40, 60)).Key("a"),
			ui.Box().Frame(60, 20).Background(ui.RGB(40, uint8(i), 60)).Key("b"),
		).Gap(4).Key(strconv.Itoa(i)))
	}
	return ui.VStack(
		ui.VScroll(rows...).Gap(4).Frame(400, 600).Key("scroller"),
	)
}

// findScrollNode returns the reference of the one scroll container in the
// tree.
func findScrollNode(t *testing.T, a *gift.App) gift.NodeRef {
	t.Helper()
	var out gift.NodeRef
	var walk func(gift.NodeRef)
	walk = func(r gift.NodeRef) {
		if a.IsScrollable(r) && out.IsZero() {
			out = r
		}
		for _, c := range a.NodeChildren(r, nil) {
			walk(c)
		}
	}
	walk(a.Root())
	if out.IsZero() {
		t.Fatal("the tree has no scroll container")
	}
	return out
}

// TestScrollFramePathIsAllocationFree is the allocation contract of the
// project plan, section 11, for the one path it names explicitly: "reine
// Scroll-/Transform-Updates".
//
// Two variants, because they are different code. A wheel or a programmatic
// scroll is input plus a transform patch plus a paint. A kinetic frame is the
// same plus one exp and one clamp per tick, driven from the injected clock,
// and it is the one that runs sixty times a second while the user does
// nothing — so it is the one that would show up as a garbage collection in the
// middle of a fling.
func TestScrollFramePathIsAllocationFree(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector rewrites every access and defeats the pooling the giftdebug " +
			"goroutine check relies on; allocation counts in a race build measure the instrumentation")
	}
	t.Run("pure scroll", func(t *testing.T) {
		a := gift.New(gift.Options{Root: scrollTree})
		if err := a.Update(geom.Sz(800, 600)); err != nil {
			t.Fatal(err)
		}
		sc := findScrollNode(t, a)

		now := time.Duration(0)
		down := true
		step := func() {
			now += 16 * time.Millisecond
			a.BeginInput(now)
			// A wheel notch through the real dispatch path, not ScrollBy:
			// hit testing and event bubbling are inside the contract too.
			d := float32(-1)
			if !down {
				d = 1
			}
			a.PointerWheel(geom.Pt(200, 300), geom.Pt(0, d))
			if err := a.Update(geom.Sz(800, 600)); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
		for range 32 {
			step()
			down = !down
		}

		before := a.Diagnostics()
		step()
		after := a.Diagnostics()
		if after.Scrolls == before.Scrolls {
			t.Fatal("the scroll did not move; the measurement would be meaningless")
		}
		if after.Builds != before.Builds || after.Layouts != before.Layouts {
			t.Fatalf("a scroll frame rebuilt %d scope(s) and ran %d layouter(s), want none",
				after.Builds-before.Builds, after.Layouts-before.Layouts)
		}

		if got := testing.AllocsPerRun(200, func() {
			step()
			down = !down
		}); got != 0 {
			t.Fatalf("a pure scroll frame allocated %v times per run, want 0", got)
		}
		_ = sc
	})

	t.Run("kinetic", func(t *testing.T) {
		a := gift.New(gift.Options{Root: scrollTree})
		if err := a.Update(geom.Sz(800, 600)); err != nil {
			t.Fatal(err)
		}
		sc := findScrollNode(t, a)

		now := time.Duration(0)
		// A finger drag fast enough to fling, replayed whenever the previous
		// fling has run out, so that every measured frame is a kinetic tick.
		fling := func() {
			a.BeginInput(now)
			a.ScrollTo(sc, 4000)
			p := geom.Pt(200, 500)
			a.PointerDown(1, gift.PointerTouch, p)
			for i := 1; i <= 8; i++ {
				now += 8 * time.Millisecond
				a.BeginInput(now)
				a.PointerMove(1, gift.PointerTouch, geom.Pt(200, 500-float32(i)*20))
			}
			now += 8 * time.Millisecond
			a.BeginInput(now)
			a.PointerUp(1, gift.PointerTouch, geom.Pt(200, 340))
		}
		fling()
		if info, _ := a.ScrollInfo(sc); !info.Flinging {
			t.Fatalf("the drag did not start a fling: %+v", info)
		}

		step := func() {
			if info, _ := a.ScrollInfo(sc); !info.Flinging {
				fling()
			}
			now += 16 * time.Millisecond
			a.BeginInput(now)
			if err := a.Update(geom.Sz(800, 600)); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
		for range 32 {
			step()
		}
		if got := testing.AllocsPerRun(200, step); got != 0 {
			t.Fatalf("a kinetic scroll frame allocated %v times per run, want 0", got)
		}
	})
}
