package ebiten

import (
	"sort"
	"sync"
	"time"
)

// DefaultFrameHistory is the number of samples a [FrameTimer] keeps per
// series. At sixty frames per second it covers a little over a minute, which
// is the length of the scroll scenarios in the project plan, section 13.
const DefaultFrameHistory = 4096

// Stats is the distribution of one series of durations.
//
// Count is the number of samples the distribution was computed from, which is
// at most the capacity of the underlying ring buffer. A series that never
// received a sample has a Count of zero and zero valued durations.
type Stats struct {
	// Count is the number of samples in the window.
	Count int
	// Min is the smallest sample in the window.
	Min time.Duration
	// Mean is the arithmetic mean of the window.
	Mean time.Duration
	// P50, P95 and P99 are the nearest rank percentiles of the window.
	P50, P95, P99 time.Duration
	// Max is the largest sample in the window.
	Max time.Duration
}

// FrameTimes is a snapshot of the three series a [FrameTimer] records.
//
// # What is and is not measured
//
// All three series are wall clock measurements taken on the CPU, around
// Ebitengine's callbacks. None of them is a GPU time measurement. Issuing a
// draw call returns as soon as the command is queued; the driver executes it
// later and Ebitengine presents the result later still. The project plan,
// section 11, says so explicitly, and the field names here keep the three
// apart rather than adding them up into a single misleading "frame time".
type FrameTimes struct {
	// UpdateCPU is the time spent inside the update callback: input,
	// build, reconciliation and layout. It is recorded per Ebitengine
	// update, of which several may happen between two drawn frames.
	UpdateCPU Stats

	// DrawCPU is the time spent inside the draw callback: producing the
	// display list and translating it into draw calls. It ends when the last
	// command has been queued, not when the GPU has executed it and not when
	// the frame has been presented.
	DrawCPU Stats

	// FrameInterval is the wall clock distance between the entry of two
	// consecutive draw callbacks. This is the series that answers "did we
	// hold sixty frames per second", because it contains everything the
	// other two do not: GPU execution, the swap and the vsync wait, as far
	// as Ebitengine's loop exposes them.
	FrameInterval Stats

	// TargetInterval is the interval FrameInterval samples are compared
	// against.
	TargetInterval time.Duration
	// MissedIntervals is the number of FrameInterval samples in the window
	// that exceeded TargetInterval.
	MissedIntervals int
	// MissedRatio is MissedIntervals divided by FrameInterval.Count, or zero
	// when there are no samples.
	MissedRatio float64

	// Updates and Draws are the total numbers of callbacks since the timer
	// was created. Unlike the series above they are not windowed, so
	// Updates/Draws is the honest ratio of the two, which is the number the
	// project plan, section 6, warns about.
	Updates, Draws uint64
}

// ring is a fixed capacity ring buffer of durations. It never allocates after
// construction: recording overwrites the oldest sample in place.
type ring struct {
	buf  []time.Duration
	head int
	n    int
}

func newRing(capacity int) ring {
	return ring{buf: make([]time.Duration, capacity)}
}

func (r *ring) add(d time.Duration) {
	r.buf[r.head] = d
	r.head++
	if r.head == len(r.buf) {
		r.head = 0
	}
	if r.n < len(r.buf) {
		r.n++
	}
}

// appendTo copies the samples into dst, which must have enough capacity. It is
// called from the snapshot path only.
func (r *ring) appendTo(dst []time.Duration) []time.Duration {
	if r.n == 0 {
		return dst[:0]
	}
	start := r.head - r.n
	if start < 0 {
		start += len(r.buf)
	}
	dst = dst[:0]
	for i := 0; i < r.n; i++ {
		dst = append(dst, r.buf[(start+i)%len(r.buf)])
	}
	return dst
}

// FrameTimer records the frame timings of one window.
//
// # Why a mutex
//
// The recording side is the UI executor and the snapshot side is whoever wants
// to print a measurement, which is frequently a different goroutine. This is
// the same problem [gift.Diagnostics] solves, and it is solved the same way
// and for the same reason: an uncontended mutex costs tens of nanoseconds
// three times per frame and allocates nothing, whereas an atomically swapped
// double buffer is not actually race free, because nothing orders a reader
// against the writer that overwrites the buffer it is reading two frames
// later.
//
// Recording allocates nothing and logs nothing, as required by the project
// plan, section 15: the frame path writes numbers, never log records.
type FrameTimer struct {
	mu             sync.Mutex
	update         ring
	draw           ring
	interval       ring
	scratch        []time.Duration
	target         time.Duration
	updates, draws uint64
}

// NewFrameTimer returns a timer that keeps capacity samples per series and
// compares frame intervals against target.
//
// A capacity of zero or less selects [DefaultFrameHistory]. A target of zero
// or less selects 16667 microseconds, the interval of a sixty hertz display
// and the threshold of the project plan, section 13.
func NewFrameTimer(target time.Duration, capacity int) *FrameTimer {
	if capacity <= 0 {
		capacity = DefaultFrameHistory
	}
	if target <= 0 {
		target = 16667 * time.Microsecond
	}
	return &FrameTimer{
		update:   newRing(capacity),
		draw:     newRing(capacity),
		interval: newRing(capacity),
		scratch:  make([]time.Duration, 0, capacity),
		target:   target,
	}
}

// RecordUpdate records the CPU time of one update callback.
func (f *FrameTimer) RecordUpdate(d time.Duration) {
	f.mu.Lock()
	f.update.add(d)
	f.updates++
	f.mu.Unlock()
}

// RecordDraw records the CPU time of one draw callback. See [FrameTimes] on
// why this is not a GPU time.
func (f *FrameTimer) RecordDraw(d time.Duration) {
	f.mu.Lock()
	f.draw.add(d)
	f.draws++
	f.mu.Unlock()
}

// RecordInterval records the wall clock distance to the previous drawn frame.
func (f *FrameTimer) RecordInterval(d time.Duration) {
	f.mu.Lock()
	f.interval.add(d)
	f.mu.Unlock()
}

// Snapshot computes the distributions of the current window.
//
// It sorts a copy of each ring and is therefore O(n log n) in the history
// size. It is meant to be called out of band — on exit, or at most once per
// second — never from inside the frame callbacks.
func (f *FrameTimer) Snapshot() FrameTimes {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := FrameTimes{
		TargetInterval: f.target,
		Updates:        f.updates,
		Draws:          f.draws,
	}
	out.UpdateCPU = f.statsLocked(&f.update)
	out.DrawCPU = f.statsLocked(&f.draw)
	out.FrameInterval = f.statsLocked(&f.interval)

	f.scratch = f.interval.appendTo(f.scratch)
	for _, d := range f.scratch {
		if d > f.target {
			out.MissedIntervals++
		}
	}
	if n := out.FrameInterval.Count; n > 0 {
		out.MissedRatio = float64(out.MissedIntervals) / float64(n)
	}
	return out
}

// statsLocked computes the distribution of r. The caller holds the mutex.
func (f *FrameTimer) statsLocked(r *ring) Stats {
	f.scratch = r.appendTo(f.scratch)
	s := f.scratch
	if len(s) == 0 {
		return Stats{}
	}
	var sum time.Duration
	for _, d := range s {
		sum += d
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return Stats{
		Count: len(s),
		Min:   s[0],
		Mean:  sum / time.Duration(len(s)),
		P50:   percentile(s, 0.50),
		P95:   percentile(s, 0.95),
		P99:   percentile(s, 0.99),
		Max:   s[len(s)-1],
	}
}

// percentile returns the nearest rank percentile of the sorted slice s.
func percentile(s []time.Duration, p float64) time.Duration {
	if len(s) == 0 {
		return 0
	}
	i := int(float64(len(s))*p+0.5) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}
