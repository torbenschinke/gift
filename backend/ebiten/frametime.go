package ebiten

import (
	"math"
	"slices"
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
	// P50, P95, P99 and P999 are the nearest rank percentiles of the window.
	//
	// Nearest rank means ceil(n*p), not round(n*p). The difference is not
	// cosmetic: with the round variant that stood here before, a probe over
	// n in 1..300 and p in {0.50, 0.95, 0.99} disagreed with the definition
	// in 282 of 900 cases, always one sample low. Being systematically
	// optimistic at the tail is the single worst property a tool can have
	// when it is the thing deciding a p99 gate.
	//
	// P999 exists because the project plan, section 13, judges the cold cache
	// scenario on "p99,9 < 33 ms". A window of 4096 samples resolves it to
	// the fourth largest sample, which is coarse but is what the plan asks
	// for; below about a thousand samples it degenerates into the maximum and
	// should be read as such.
	P50, P95, P99, P999 time.Duration
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

	// NominalInterval is the interval the display is expected to hold, for
	// example 16.667 ms at sixty hertz. Tolerance is the slack added to it.
	// TargetInterval is their sum and is the value samples are actually
	// compared against.
	//
	// All three are carried in every snapshot because the project plan,
	// section 13, requires it: a strict comparison against the bare nominal
	// interval reported 50 % of the frames of a cleanly timed measurement as
	// missed, so the binding threshold is 17.17 ms, and a number that is not
	// printed next to its threshold cannot be checked by anybody.
	NominalInterval time.Duration
	Tolerance       time.Duration
	TargetInterval  time.Duration

	// MissedIntervals is the number of FrameInterval samples in the window
	// that exceeded TargetInterval.
	MissedIntervals int
	// MissedRatio is MissedIntervals divided by FrameInterval.Count, or zero
	// when there are no samples.
	MissedRatio float64

	// Warmup is the number of leading intervals that are discarded, and
	// WarmupDropped is how many of them have been seen so far.
	//
	// Opening a window is not a frame. Real runs show a first interval of
	// 123 to 148 ms while the driver, the swap chain and the shader upload
	// settle. At 4096 samples the ring covers about 68 seconds, so a sixty
	// second scenario can never evict that outlier: it sits in the window for
	// the whole measurement, owns the maximum, and pushes the tail
	// percentiles and the missed ratio away from what the scene actually did.
	Warmup        int
	WarmupDropped uint64

	// MinInterval is the shortest distance between two draw callbacks that is
	// still treated as a frame, and SubFrameIntervals counts the ones that
	// were not.
	//
	// Ebitengine sometimes issues two Draw callbacks back to back, microseconds
	// apart. Those are not two presentations, and counting them as frames
	// deflates every percentile and dilutes the missed ratio with samples that
	// no display ever showed. They are discarded and counted separately rather
	// than silently averaged in; discarding without saying so would be the
	// same kind of quiet optimism as the round-rank percentile.
	MinInterval       time.Duration
	SubFrameIntervals uint64

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

// DefaultWarmupIntervals is the number of leading frame intervals a timer
// discards by default. At sixty hertz it is the first second, which comfortably
// covers the 123 to 148 ms startup interval that every real run shows.
const DefaultWarmupIntervals = 60

// DefaultMinInterval is the shortest distance between two draw callbacks that
// is still counted as a frame. Anything at or below it is a pair of back to
// back callbacks, not two presentations.
const DefaultMinInterval = time.Millisecond

// FrameTimerOptions configures a [FrameTimer] in full. It is the constructor to
// use when the defaults are not good enough, in particular when the display is
// not at sixty hertz.
type FrameTimerOptions struct {
	// Nominal is the interval the display is expected to hold. Zero or less
	// selects 16667 microseconds, that is sixty hertz.
	Nominal time.Duration
	// Tolerance is the slack added to Nominal before an interval counts as
	// missed. Zero selects [DefaultIntervalTolerance], the 0.5 ms the project
	// plan, section 13, makes binding. A negative value means no tolerance at
	// all; expect a strict comparison to report well timed frames as missed,
	// which is the measurement the plan retracted.
	Tolerance time.Duration
	// Capacity is the number of samples kept per series. Zero or less selects
	// [DefaultFrameHistory].
	Capacity int
	// Warmup is the number of leading frame intervals to discard. Zero or
	// less selects [DefaultWarmupIntervals]; pass a negative value to keep
	// every sample, which is what a test that wants exact arithmetic does.
	Warmup int
	// MinInterval is the shortest interval still treated as a frame. Zero
	// selects [DefaultMinInterval]; a negative value keeps everything.
	MinInterval time.Duration
}

// NewFrameTimerWith returns a timer configured by o.
func NewFrameTimerWith(o FrameTimerOptions) *FrameTimer {
	if o.Nominal <= 0 {
		o.Nominal = 16667 * time.Microsecond
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
// The godoc here used to name 16667 microseconds as "the threshold of the
// project plan, section 13". That threshold has been retracted: a strict
// comparison against the bare nominal interval reported half the frames of a
// cleanly timed measurement as missed. Use [NewFrameTimerWith] to state the
// nominal interval and the tolerance separately, which is what a display that
// is not at sixty hertz needs.
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

// DefaultIntervalTolerance is the slack added to the nominal frame interval
// before an interval counts as missed.
//
// The project plan, section 13, fixes it at 0.5 ms and records why: a strict
// comparison against 16.67 ms reported 50 % of the frames of a well timed
// measurement as missed, because no display runs at exactly its nominal rate.
// 16.667 + 0.5 = 17.167 ms is the binding threshold at sixty hertz.
const DefaultIntervalTolerance = 500 * time.Microsecond

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
