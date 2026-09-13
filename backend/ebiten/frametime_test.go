package ebiten

import (
	"sync"
	"testing"
	"time"
)

func TestFrameTimerDistribution(t *testing.T) {
	f := NewFrameTimer(10*time.Millisecond, 100)
	for i := 1; i <= 100; i++ {
		f.RecordDraw(time.Duration(i) * time.Millisecond)
	}
	s := f.Snapshot().DrawCPU
	if s.Count != 100 {
		t.Fatalf("Count = %d, want 100", s.Count)
	}
	if s.Min != time.Millisecond || s.Max != 100*time.Millisecond {
		t.Fatalf("Min/Max = %v/%v", s.Min, s.Max)
	}
	if s.Mean != 50500*time.Microsecond {
		t.Fatalf("Mean = %v, want 50.5ms", s.Mean)
	}
	// Nearest rank over 1..100 ms.
	if s.P50 != 50*time.Millisecond || s.P95 != 95*time.Millisecond || s.P99 != 99*time.Millisecond {
		t.Fatalf("P50/P95/P99 = %v/%v/%v", s.P50, s.P95, s.P99)
	}
}

func TestFrameTimerRingOverwrites(t *testing.T) {
	f := NewFrameTimer(time.Second, 4)
	for i := 1; i <= 10; i++ {
		f.RecordUpdate(time.Duration(i) * time.Millisecond)
	}
	s := f.Snapshot().UpdateCPU
	if s.Count != 4 {
		t.Fatalf("Count = %d, want the ring capacity 4", s.Count)
	}
	if s.Min != 7*time.Millisecond || s.Max != 10*time.Millisecond {
		t.Fatalf("window = %v..%v, want the last four samples 7..10ms", s.Min, s.Max)
	}
}

func TestFrameTimerMissedIntervals(t *testing.T) {
	f := NewFrameTimer(16667*time.Microsecond, 100)
	for i := 0; i < 90; i++ {
		f.RecordInterval(16 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		f.RecordInterval(20 * time.Millisecond)
	}
	snap := f.Snapshot()
	if snap.MissedIntervals != 10 {
		t.Fatalf("MissedIntervals = %d, want 10", snap.MissedIntervals)
	}
	if snap.MissedRatio < 0.099 || snap.MissedRatio > 0.101 {
		t.Fatalf("MissedRatio = %v, want 0.1", snap.MissedRatio)
	}
	if snap.TargetInterval != 16667*time.Microsecond {
		t.Fatalf("TargetInterval = %v", snap.TargetInterval)
	}
}

func TestFrameTimerEmpty(t *testing.T) {
	f := NewFrameTimer(0, 0)
	snap := f.Snapshot()
	if snap.DrawCPU.Count != 0 || snap.DrawCPU.P99 != 0 {
		t.Fatalf("empty snapshot = %+v", snap.DrawCPU)
	}
	if snap.TargetInterval != 16667*time.Microsecond {
		t.Fatalf("default target = %v, want 16.667ms", snap.TargetInterval)
	}
	if snap.MissedRatio != 0 {
		t.Fatalf("MissedRatio = %v on an empty window", snap.MissedRatio)
	}
}

func TestFrameTimerSeriesAreSeparate(t *testing.T) {
	f := NewFrameTimer(time.Second, 16)
	f.RecordUpdate(time.Millisecond)
	f.RecordDraw(2 * time.Millisecond)
	f.RecordDraw(2 * time.Millisecond)
	f.RecordInterval(3 * time.Millisecond)
	snap := f.Snapshot()
	if snap.Updates != 1 || snap.Draws != 2 {
		t.Fatalf("Updates/Draws = %d/%d, want 1/2", snap.Updates, snap.Draws)
	}
	if snap.UpdateCPU.Mean != time.Millisecond ||
		snap.DrawCPU.Mean != 2*time.Millisecond ||
		snap.FrameInterval.Mean != 3*time.Millisecond {
		t.Fatal("the three series were conflated")
	}
}

// TestFrameTimerRecordDoesNotAllocate keeps the recorder out of the way of the
// 0 B/op contract of the project plan, section 11.
func TestFrameTimerRecordDoesNotAllocate(t *testing.T) {
	f := NewFrameTimer(time.Second, 256)
	if n := testing.AllocsPerRun(1000, func() {
		f.RecordUpdate(time.Millisecond)
		f.RecordDraw(time.Millisecond)
		f.RecordInterval(time.Millisecond)
	}); n != 0 {
		t.Fatalf("recording allocates %v times, want 0", n)
	}
}

// TestFrameTimerSnapshotIsRaceFree is the reason the recorder has a mutex: the
// measurement harness runs on another goroutine than the frame loop. Run with
// -race for this to mean anything.
func TestFrameTimerSnapshotIsRaceFree(t *testing.T) {
	f := NewFrameTimer(time.Millisecond, 64)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				f.RecordDraw(time.Microsecond)
				f.RecordInterval(2 * time.Microsecond)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = f.Snapshot()
		}
		close(stop)
	}()
	wg.Wait()
}
