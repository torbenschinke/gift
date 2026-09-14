//go:build giftmetrics

package metrics

import (
	"math"
	"slices"
	"sync"
	"time"
)

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
	mu       sync.Mutex
	update   ring
	draw     ring
	interval ring
	scratch  []time.Duration

	nominal   time.Duration
	tolerance time.Duration
	target    time.Duration

	warmup      int
	warmupSeen  uint64
	minInterval time.Duration
	subFrames   uint64

	updates, draws uint64
}

// NewFrameTimerWith returns a timer configured by o.
func NewFrameTimerWith(o FrameTimerOptions) *FrameTimer {
	if o.Nominal <= 0 {
		o.Nominal = DefaultNominalInterval
	}
	switch {
	case o.Tolerance < 0:
		o.Tolerance = 0
	case o.Tolerance == 0:
		o.Tolerance = DefaultIntervalTolerance
	}
	if o.Capacity <= 0 {
		o.Capacity = DefaultFrameHistory
	}
	switch {
	case o.Warmup < 0:
		o.Warmup = 0
	case o.Warmup == 0:
		o.Warmup = DefaultWarmupIntervals
	}
	switch {
	case o.MinInterval < 0:
		o.MinInterval = 0
	case o.MinInterval == 0:
		o.MinInterval = DefaultMinInterval
	}
	return &FrameTimer{
		update:      newRing(o.Capacity),
		draw:        newRing(o.Capacity),
		interval:    newRing(o.Capacity),
		scratch:     make([]time.Duration, 0, o.Capacity),
		nominal:     o.Nominal,
		tolerance:   o.Tolerance,
		target:      o.Nominal + o.Tolerance,
		warmup:      o.Warmup,
		minInterval: o.MinInterval,
	}
}

// NewFrameTimer returns a timer that keeps capacity samples per series and
// treats a frame interval above target as missed.
//
// target is the complete threshold, tolerance included; the timer does not add
// anything to it. A capacity of zero or less selects [DefaultFrameHistory]. A
// target of zero or less selects 17167 microseconds, which is the 16.667 ms of
// a sixty hertz display plus the 0.5 ms tolerance that the project plan,
// section 13, makes binding.
//
// Warm-up exclusion and the sub frame filter are on with their defaults; see
// [FrameTimerOptions] to turn them off.
func NewFrameTimer(target time.Duration, capacity int) *FrameTimer {
	o := FrameTimerOptions{Capacity: capacity}
	if target > 0 {
		// The caller stated a finished threshold. Present it as nominal with
		// no tolerance, so that the three numbers in the snapshot still add
		// up and nothing is invented on the caller's behalf.
		o.Nominal, o.Tolerance = target, -1
	}
	return NewFrameTimerWith(o)
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
//
// Two classes of sample are counted but not recorded: the first Warmup
// intervals, which contain window creation and shader upload, and any interval
// at or below MinInterval, which is two draw callbacks back to back rather
// than two presentations. Both counts are in the snapshot; see [FrameTimes].
func (f *FrameTimer) RecordInterval(d time.Duration) {
	f.mu.Lock()
	switch {
	case f.warmupSeen < uint64(f.warmup):
		f.warmupSeen++
	case d <= f.minInterval:
		f.subFrames++
	default:
		f.interval.add(d)
	}
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
		NominalInterval:   f.nominal,
		Tolerance:         f.tolerance,
		TargetInterval:    f.target,
		Warmup:            f.warmup,
		WarmupDropped:     f.warmupSeen,
		MinInterval:       f.minInterval,
		SubFrameIntervals: f.subFrames,
		Updates:           f.updates,
		Draws:             f.draws,
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
	slices.Sort(s)
	return Stats{
		Count: len(s),
		Min:   s[0],
		Mean:  sum / time.Duration(len(s)),
		P50:   percentile(s, 0.50),
		P95:   percentile(s, 0.95),
		P99:   percentile(s, 0.99),
		P999:  percentile(s, 0.999),
		Max:   s[len(s)-1],
	}
}

// percentile returns the nearest rank percentile of the sorted slice s.
//
// Nearest rank is ceil(n*p), and the implementation says so. The previous
// version rounded — int(n*p+0.5) — which is a different definition and is the
// smaller of the two whenever the fractional part is below a half, that is
// roughly a third of all n for the percentiles this tool reports. It was
// therefore biased towards reporting a better tail than the samples support.
func percentile(s []time.Duration, p float64) time.Duration {
	if len(s) == 0 {
		return 0
	}
	i := int(math.Ceil(float64(len(s))*p)) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}
