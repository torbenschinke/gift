package gift_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/torbenschinke/gift"
)

// TestFramePathIsAllocationFree is go/no-go criterion 3 of the project plan,
// section 12: after warmup, an update that finds nothing dirty plus a paint
// over an unchanged tree of roughly 200 nodes must not allocate at all.
func TestFramePathIsAllocationFree(t *testing.T) {
	a := gift.New(gift.Options{Root: wideTree})
	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if n := a.Diagnostics().LiveNodes; n < 180 {
		t.Fatalf("the test tree has only %d nodes, the benchmark would be meaningless", n)
	}

	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("frame path allocated %v times per run, want 0", got)
	}
}

// TestFramePathIsAllocationFreeWithDebugLogger repeats the contract with an
// active debug level logger attached, as required by the project plan,
// section 13. Nothing in the frame path may touch that logger.
func TestFramePathIsAllocationFreeWithDebugLogger(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := gift.New(gift.Options{Logger: log, Root: wideTree})
	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("frame path with an active logger allocated %v times per run, want 0", got)
	}
}

func BenchmarkFramePath(b *testing.B) {
	a := gift.New(gift.Options{Root: wideTree})
	for range 4 {
		_ = a.Update(viewport())
		a.Paint()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = a.Update(viewport())
		a.Paint()
	}
}

func BenchmarkRootBuild(b *testing.B) {
	a := gift.New(gift.Options{Root: wideTree})
	_ = a.Update(viewport())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.Invalidate()
		_ = a.Update(viewport())
	}
}
