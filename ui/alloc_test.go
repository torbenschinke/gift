package ui_test

import (
	"strconv"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// tree builds a representative layout of roughly 200 nodes out of nothing but
// public ui views: twenty rows of nine boxes with a spacer in between, a
// styled and clipped frame around the whole thing.
//
//	1 outer VStack + 20 HStacks + 20*(9 boxes + 1 spacer) = 221 nodes,
//	plus the root component node.
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
				b = b.CornerRadius(3).Border(ui.Border{Width: 1, Color: ui.RGB(255, 255, 255)})
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
		a := gift.New(gift.Options{Root: tree})
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
