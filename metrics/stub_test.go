//go:build !giftmetrics

package metrics

import (
	"testing"
	"time"
)

// TestStubIsOff states the contract of the untagged build in one line.
func TestStubIsOff(t *testing.T) {
	if Enabled() {
		t.Fatal("Enabled() is true in a build without the giftmetrics tag")
	}
	if r := Start(Options{}); r != nil {
		t.Fatalf("Start returned %p, want nil: the stub must not retain anything", r)
	}
}

// TestStubAllocatesNothing is the property the build tag exists for. A whole
// frame's worth of calls on a nil recorder must not touch the heap once.
func TestStubAllocatesNothing(t *testing.T) {
	rec := Start(Options{})
	if n := testing.AllocsPerRun(1000, func() {
		rec.RecordUpdate(time.Millisecond)
		rec.RecordDraw(time.Millisecond)
		rec.RecordInterval(16 * time.Millisecond)
		rec.Tick()
	}); n != 0 {
		t.Fatalf("the stub allocates %v times per frame, want 0", n)
	}
}

// TestStubTolerates makes explicit that the whole API is callable and that
// none of it observes anything.
func TestStubTolerates(t *testing.T) {
	rec := Start(Options{})
	rec.RecordDraw(time.Second)
	if snap := rec.Snapshot(); snap != (FrameTimes{}) {
		t.Fatalf("the stub recorded something: %+v", snap)
	}
	if rec.Timer() != nil {
		t.Fatal("the stub handed out a timer")
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
}

// BenchmarkStubFrame is the evidence that an application built without the tag
// pays nothing.
//
// It is the exact shape the backend uses: the calls are guarded by Enabled,
// which is the constant false here, so the branch — including the two calls to
// time.Now that would time the callback — is dead code the compiler removes.
// Expect a time per operation at or below the resolution of the benchmark
// timer and 0 B/op. The unguarded variant below is what a careless caller
// would write; it costs the two time.Now calls and nothing else.
func BenchmarkStubFrame(b *testing.B) {
	rec := Start(Options{})
	b.ReportAllocs()
	for b.Loop() {
		if Enabled() {
			start := time.Now()
			rec.RecordUpdate(time.Since(start))
			start = time.Now()
			rec.RecordDraw(time.Since(start))
			rec.Tick()
		}
	}
}

// BenchmarkStubUnguarded records through the nil recorder without the Enabled
// guard. It must still be allocation free: the methods have empty bodies.
func BenchmarkStubUnguarded(b *testing.B) {
	rec := Start(Options{})
	b.ReportAllocs()
	for b.Loop() {
		rec.RecordUpdate(time.Millisecond)
		rec.RecordDraw(time.Millisecond)
		rec.RecordInterval(16 * time.Millisecond)
		rec.Tick()
	}
}
