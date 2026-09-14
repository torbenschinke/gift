//go:build giftmetrics

package metrics

import (
	"math"
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
	f := raw(time.Second, 4)
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

// raw returns a timer that keeps every interval, so that a test can do exact
// arithmetic without the warm-up and sub frame filters in the way. Those two
// have tests of their own below.
func raw(target time.Duration, capacity int) *FrameTimer {
	return NewFrameTimerWith(FrameTimerOptions{
		Nominal: target, Tolerance: -1, Capacity: capacity, Warmup: -1, MinInterval: -1,
	})
}

func TestFrameTimerMissedIntervals(t *testing.T) {
	f := raw(16667*time.Microsecond, 100)
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
	if snap.DrawCPU.Count != 0 || snap.DrawCPU.P99 != 0 || snap.DrawCPU.P999 != 0 {
		t.Fatalf("empty snapshot = %+v", snap.DrawCPU)
	}
	// 16.667 ms nominal plus the 0.5 ms tolerance the project plan,
	// section 13, makes binding.
	if snap.NominalInterval != 16667*time.Microsecond {
		t.Fatalf("default nominal = %v, want 16.667ms", snap.NominalInterval)
	}
	if snap.Tolerance != 500*time.Microsecond {
		t.Fatalf("default tolerance = %v, want 0.5ms", snap.Tolerance)
	}
	if snap.TargetInterval != 17167*time.Microsecond {
		t.Fatalf("default target = %v, want 17.167ms", snap.TargetInterval)
	}
	if snap.NominalInterval+snap.Tolerance != snap.TargetInterval {
		t.Fatal("nominal + tolerance must equal the target, or the printed numbers do not add up")
	}
	if snap.MissedRatio != 0 {
		t.Fatalf("MissedRatio = %v on an empty window", snap.MissedRatio)
	}
}

func TestFrameTimerSeriesAreSeparate(t *testing.T) {
	f := raw(time.Second, 16)
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
	f := raw(time.Second, 256)
	if n := testing.AllocsPerRun(1000, func() {
		f.RecordUpdate(time.Millisecond)
		f.RecordDraw(time.Millisecond)
		f.RecordInterval(time.Millisecond)
	}); n != 0 {
		t.Fatalf("recording allocates %v times, want 0", n)
	}
}

// TestFrameTimerConcurrentSnapshotHarness drives a recorder and a snapshotter
// on two goroutines at once. It is the reason the recorder has a mutex: the
// measurement harness runs on another goroutine than the frame loop.
//
// It was called TestFrameTimerSnapshotIsRaceFree and asserted nothing at all,
// so without -race it passed trivially while claiming to have proved something.
// The name now says what it is — a harness for the race detector — and it does
// assert that the two goroutines actually overlapped, because a harness that
// finished before the other side started would be just as empty.
func TestFrameTimerConcurrentSnapshotHarness(t *testing.T) {
	f := raw(time.Millisecond, 64)
	var wg sync.WaitGroup
	var last FrameTimes
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
			last = f.Snapshot()
		}
		close(stop)
	}()
	wg.Wait()

	// The snapshotter saw the recorder at work. Without this the test would
	// still pass if the recording goroutine never got scheduled, which is
	// exactly the kind of silent nothing it used to be.
	if last.Draws == 0 || last.FrameInterval.Count == 0 {
		t.Fatalf("the snapshotter never observed a recorded sample: %+v", last)
	}
	final := f.Snapshot()
	if final.Draws < last.Draws {
		t.Fatalf("Draws went backwards: %d then %d", last.Draws, final.Draws)
	}
}

// TestPercentileIsNearestRank is the regression test for the round-rank bug.
//
// The old implementation used int(n*p+0.5)-1. A probe over n in 1..300 and
// p in {0.50, 0.95, 0.99} found 282 divergences from the definition, always
// one sample low. The n values below are ones where round and ceil differ, so
// this test fails against the old code; n=100, which the existing distribution
// test uses, is not one of them, which is why that test never caught it.
func TestPercentileIsNearestRank(t *testing.T) {
	for _, n := range []int{3, 7, 10, 11, 13, 21, 37, 99, 101, 150, 250, 1000} {
		s := make([]time.Duration, n)
		for i := range s {
			s[i] = time.Duration(i+1) * time.Millisecond
		}
		for _, p := range []float64{0.50, 0.95, 0.99, 0.999} {
			// Nearest rank: the ceil(n*p)-th smallest sample, one based.
			rank := int(math.Ceil(float64(n) * p))
			if rank < 1 {
				rank = 1
			}
			if rank > n {
				rank = n
			}
			want := time.Duration(rank) * time.Millisecond
			if got := percentile(s, p); got != want {
				t.Errorf("n=%d p=%v: percentile = %v, want the rank %d sample %v", n, p, got, rank, want)
			}
		}
	}
}

// TestPercentileNeverUnderreports is the property the bug violated: the
// nearest rank percentile is never below the round rank one, and for the
// classic counterexample it is strictly above.
func TestPercentileNeverUnderreports(t *testing.T) {
	roundRank := func(s []time.Duration, p float64) time.Duration {
		i := int(float64(len(s))*p+0.5) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	strictlyBetter := 0
	for n := 1; n <= 300; n++ {
		s := make([]time.Duration, n)
		for i := range s {
			s[i] = time.Duration(i+1) * time.Millisecond
		}
		for _, p := range []float64{0.50, 0.95, 0.99} {
			got, old := percentile(s, p), roundRank(s, p)
			if got < old {
				t.Fatalf("n=%d p=%v: nearest rank %v is below round rank %v", n, p, got, old)
			}
			if got > old {
				strictlyBetter++
			}
		}
	}
	if strictlyBetter == 0 {
		t.Fatal("the two definitions never diverged, the probe is broken")
	}
	t.Logf("nearest rank differs from the old round rank in %d of 900 cases", strictlyBetter)
}

// TestFrameTimerWarmupExclusion covers the startup outlier. A window of 4096
// samples covers about 68 seconds at sixty hertz, so a 60 s scenario can never
// evict a 148 ms first interval on its own.
func TestFrameTimerWarmupExclusion(t *testing.T) {
	f := NewFrameTimerWith(FrameTimerOptions{
		Nominal: 16667 * time.Microsecond, Tolerance: 500 * time.Microsecond,
		Capacity: 100, Warmup: 2, MinInterval: -1,
	})
	f.RecordInterval(148 * time.Millisecond) // window creation
	f.RecordInterval(30 * time.Millisecond)  // shader upload
	for range 10 {
		f.RecordInterval(16 * time.Millisecond)
	}
	snap := f.Snapshot()
	if snap.WarmupDropped != 2 {
		t.Fatalf("WarmupDropped = %d, want 2", snap.WarmupDropped)
	}
	if snap.FrameInterval.Count != 10 {
		t.Fatalf("Count = %d, want the 10 steady state samples", snap.FrameInterval.Count)
	}
	if snap.FrameInterval.Max != 16*time.Millisecond {
		t.Fatalf("Max = %v, want 16ms; the startup outlier is still in the window", snap.FrameInterval.Max)
	}
	if snap.MissedIntervals != 0 {
		t.Fatalf("MissedIntervals = %d, want 0", snap.MissedIntervals)
	}
}

// TestFrameTimerSubFrameIntervals covers the other contaminant: two Draw
// callbacks microseconds apart are not two presentations.
func TestFrameTimerSubFrameIntervals(t *testing.T) {
	f := NewFrameTimerWith(FrameTimerOptions{Capacity: 100, Warmup: -1})
	if f.Snapshot().TargetInterval != 17167*time.Microsecond {
		t.Fatalf("target = %v, want the default 17.167ms", f.Snapshot().TargetInterval)
	}
	for range 8 {
		f.RecordInterval(16 * time.Millisecond)
	}
	for range 4 {
		f.RecordInterval(400 * time.Microsecond)
	}
	snap := f.Snapshot()
	if snap.SubFrameIntervals != 4 {
		t.Fatalf("SubFrameIntervals = %d, want 4", snap.SubFrameIntervals)
	}
	if snap.FrameInterval.Count != 8 {
		t.Fatalf("Count = %d, want 8", snap.FrameInterval.Count)
	}
	if snap.FrameInterval.Min != 16*time.Millisecond {
		t.Fatalf("Min = %v, want 16ms; a sub frame interval was averaged in", snap.FrameInterval.Min)
	}
	if snap.MinInterval != DefaultMinInterval {
		t.Fatalf("MinInterval = %v", snap.MinInterval)
	}
}

// TestFrameTimerToleranceIsApplied is the arithmetic of the project plan,
// section 13: 16.667 ms nominal plus 0.5 ms tolerance is 17.167 ms, and an
// interval of 17 ms is therefore not a missed frame.
func TestFrameTimerToleranceIsApplied(t *testing.T) {
	f := NewFrameTimerWith(FrameTimerOptions{Capacity: 100, Warmup: -1, MinInterval: -1})
	for range 50 {
		f.RecordInterval(17 * time.Millisecond)
	}
	for range 50 {
		f.RecordInterval(18 * time.Millisecond)
	}
	snap := f.Snapshot()
	if snap.MissedIntervals != 50 {
		t.Fatalf("MissedIntervals = %d, want 50; 17ms is inside the 17.167ms threshold", snap.MissedIntervals)
	}
}
