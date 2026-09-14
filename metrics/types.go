package metrics

import (
	"time"
)

// DefaultFrameHistory is the number of samples a [FrameTimer] keeps per
// series. At sixty frames per second it covers a little over a minute, which
// is the length of the scroll scenarios in the project plan, section 13.
const DefaultFrameHistory = 4096

// DefaultWarmupIntervals is the number of leading frame intervals a timer
// discards by default. At sixty hertz it is the first second, which comfortably
// covers the 123 to 148 ms startup interval that every real run shows.
const DefaultWarmupIntervals = 60

// DefaultMinInterval is the shortest distance between two draw callbacks that
// is still counted as a frame. Anything at or below it is a pair of back to
// back callbacks, not two presentations.
const DefaultMinInterval = time.Millisecond

// DefaultIntervalTolerance is the slack added to the nominal frame interval
// before an interval counts as missed.
//
// The project plan, section 13, fixes it at 0.5 ms and records why: a strict
// comparison against 16.67 ms reported 50 % of the frames of a well timed
// measurement as missed, because no display runs at exactly its nominal rate.
// 16.667 + 0.5 = 17.167 ms is the binding threshold at sixty hertz.
const DefaultIntervalTolerance = 500 * time.Microsecond

// DefaultNominalInterval is the interval a sixty hertz display is expected to
// hold.
const DefaultNominalInterval = 16667 * time.Microsecond

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
// All three series are wall clock measurements taken on the CPU, around the
// backend's callbacks. None of them is a GPU time measurement. Issuing a
// draw call returns as soon as the command is queued; the driver executes it
// later and the backend presents the result later still. The project plan,
// section 11, says so explicitly, and the field names here keep the three
// apart rather than adding them up into a single misleading "frame time".
type FrameTimes struct {
	// UpdateCPU is the time spent inside the update callback: input,
	// build, reconciliation and layout. It is recorded per backend
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
	// as the backend's loop exposes them.
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

// FrameTimerOptions configures a [FrameTimer] in full. It is the constructor to
// use when the defaults are not good enough, in particular when the display is
// not at sixty hertz.
type FrameTimerOptions struct {
	// Nominal is the interval the display is expected to hold. Zero or less
	// selects [DefaultNominalInterval], that is sixty hertz.
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

// RendererStats are the renderer side numbers of one report.
//
// It is a plain struct and not an interface on purpose. This package must not
// import a backend — that would be a cycle, since the backend calls in here —
// so the backend converts its own counters into this shape. The skip reasons
// are separate fields for the reason the backend keeps them separate: one
// conflated number answered "the application asked for something invisible"
// and "a container collapsed and its content vanished" with the same integer,
// and a real layout defect survived a whole work unit behind it.
type RendererStats struct {
	// Frames is the number of completed frames.
	Frames uint64
	// DrawCalls is the number of draw calls issued. It is one per run of
	// same-material operations in display list order, not one per frame: text
	// is a textured quad and a shape is not, and the project plan, section
	// 11, forbids reordering transparent content to merge them.
	DrawCalls uint64
	// ShapeDrawCalls, GlyphDrawCalls and ImageDrawCalls split DrawCalls by
	// material. An image is one call per run of operations sampling the same
	// texture; the backend's documentation explains why that is not the same
	// as the number of draws the GPU performs.
	ShapeDrawCalls, GlyphDrawCalls, ImageDrawCalls uint64
	// GlyphQuads is the number of glyph quads emitted and ImageOps the
	// number of image operations that produced geometry.
	GlyphQuads, ImageOps uint64
	// ShadowOps is the number of shadow operations that produced geometry
	// and ShadowSharpOps the subset of them with no blur.
	//
	// There is no cache ratio next to them because there is no cache: gift
	// evaluates the Gaussian analytically in the shared shape shader, so a
	// shadow costs fill rate and nothing else. The project plan, section 8,
	// proposed a cached shape mask instead; see the backend's package
	// documentation for why that was not built.
	ShadowOps, ShadowSharpOps uint64
	// GlassOps is the number of material regions drawn, split by the level
	// each was actually drawn at, plus the ones that degraded to a plain tint
	// because no backdrop could be obtained.
	GlassOps, GlassReducedOps, GlassFullOps, GlassFallbacks uint64
	// GlassPasses is the number of material pass stages executed and
	// GlassDrawCalls the draw calls they issued, including the scene to
	// screen blit. GlassDrawCalls is part of DrawCalls.
	GlassPasses, GlassDrawCalls uint64
	// GlassLevel is the effective quality level of the last drawn frame,
	// "reduced" or "full", and GlassPinned whether the application fixed it.
	//
	// The project plan, section 8, requires the effective level to be visible
	// in the diagnostics, and section 13 requires a measurement that is to be
	// compared with another one to pin it — so a report that does not say
	// which level it measured is not a comparable measurement. Both fields
	// are here so that the report can say so.
	GlassLevel  string
	GlassPinned bool
	// GlassLevelChanges is how often the adaptive policy switched over the
	// whole run. In a pinned run it is zero or one; in an adaptive run a
	// large number is the flicker the hysteresis exists to prevent.
	GlassLevelChanges uint64
	// Targets are the intermediate render target counters.
	Targets TargetStats
	// Ops is the number of operations that produced geometry.
	Ops uint64

	SkippedNone        uint64
	SkippedTransparent uint64
	// SkippedEmptyBounds is the one to watch: an operation whose own bounds
	// are empty is a node that reported a zero extent.
	SkippedEmptyBounds uint64
	SkippedEmptyClip   uint64
	SkippedOutsideClip uint64
	SkippedZeroStroke  uint64
	SkippedEmptyText   uint64
	// SkippedNoImage counts image operations whose texture was not resident.
	SkippedNoImage uint64
	UnknownKinds   uint64

	// Atlas are the glyph atlas counters.
	Atlas AtlasStats
	// Textures are the image texture cache counters.
	Textures TextureStats
}

// TextureStats are the GPU image residency numbers of one report.
//
// It is a plain struct for the same reason [AtlasStats] is: this package must
// not import a backend, so the backend converts.
type TextureStats struct {
	// Uploads and UploadedBytes are what reached the GPU.
	Uploads, UploadedBytes uint64
	// Deferred is the number of uploads postponed by the per *drawn* frame
	// upload budget, and Rejected the number refused because no room could be
	// made. The first is the budget doing its job; the second says the
	// residency budget is too small for the scene.
	Deferred, Rejected uint64
	// Evictions, AgeEvictions and ExplicitReleases are why textures were
	// released, and Deallocations the total number of explicit deallocations,
	// which must equal their sum. The project plan, section 11, asks for the
	// explicit release rather than a finaliser, and this is the number that
	// says it happened.
	Evictions, AgeEvictions, ExplicitReleases, Deallocations uint64
	// Stale is the number of handles resolved after their texture was gone.
	Stale uint64
	// Textures and Bytes are the current residency and PeakBytes the high
	// water mark. Bytes is *logical* pixel bytes: the project plan,
	// section 11, is explicit that padding, fragmentation, atlas growth,
	// intermediate targets and staging need more than this.
	Textures  int
	Bytes     int64
	PeakBytes int64
}

// AtlasStats are the glyph atlas numbers of one report.
//
// It is a plain struct for the same reason [RendererStats] is: metrics must
// not import a backend, so the backend converts its own counters into this
// shape.
type AtlasStats struct {
	// Hits and Misses are the glyph lookup outcomes.
	Hits, Misses uint64
	// Rasterised is the number of outlines turned into coverage.
	Rasterised uint64
	// UploadedBytes is the total coverage uploaded.
	UploadedBytes uint64
	// PageEvictions and GlyphEvictions are what the budget cost.
	PageEvictions, GlyphEvictions uint64
	// Rejected counts glyphs that did not fit anywhere. Non zero means text
	// is missing from the screen.
	Rejected uint64
	// Pages, Glyphs and Bytes are the current occupancy.
	Pages, Glyphs, Bytes int
}

// Skipped is the total number of skipped operations.
func (s RendererStats) Skipped() uint64 {
	return s.SkippedNone + s.SkippedTransparent + s.SkippedEmptyBounds +
		s.SkippedEmptyClip + s.SkippedOutsideClip + s.SkippedZeroStroke +
		s.SkippedEmptyText + s.SkippedNoImage
}

// Accounted is Ops + Skipped + UnknownKinds and must equal the number of
// operations submitted.
func (s RendererStats) Accounted() uint64 { return s.Ops + s.Skipped() + s.UnknownKinds }

// ShaperStats are the text shaping cache numbers of one report.
//
// As of WU-G it is wired up: ui.Text measures through the process wide shaper
// of internal/text, and the Ebitengine backend sets [Options.Shaper] to a
// snapshot of it. Present is false only when the backend was configured
// without one.
type ShaperStats struct {
	// Present says whether a shaper supplied these numbers at all.
	Present bool
	// Hits and Misses are the shaping cache outcomes. The project plan,
	// section 11, makes the distinction binding: layout is allocation free
	// for text it has already seen, and a miss allocates about five
	// kilobytes inside harfbuzz.
	Hits, Misses uint64
	// Evictions is the number of entries dropped by the byte budget, and
	// AgeEvictions the ones dropped for going unused.
	Evictions, AgeEvictions uint64
	// ShapedGlyphs is the number of glyphs produced by misses, which is the
	// honest measure of shaping work: the miss count alone says nothing about
	// how much text was shaped.
	ShapedGlyphs uint64
	// Entries is the number of cached paragraphs and Bytes the accounted size
	// of the cache.
	Entries int
	Bytes   uint64
}

// Options configures a [Recorder].
//
// Everything in here is supplied by the backend, which knows the display and
// owns the renderer. Whether a report is produced at all, how often and
// where it goes is not in here: that is read from the environment, because a
// library must not take command line flags away from its host program. See
// the package documentation.
// CoreStats is the runtime half of a report. It mirrors gift.Diagnostics
// field for field; the consumer converts, because this package deliberately
// imports nothing from the framework it measures.
type CoreStats struct {
	Frames         uint64
	Builds         uint64
	Layouts        uint64
	PaintedNodes   uint64
	PaintedOps     uint64
	LiveNodes      uint64
	LiveScopes     uint64
	OverflowNodes  uint64
	OverflowExtent float32
	Scrolls        uint64
	HitTests       uint64
	InputEvents    uint64
}

type Options struct {
	// Timer configures the frame timer.
	Timer FrameTimerOptions

	// Core supplies the runtime counters. It may be nil.
	//
	// It is a function returning a plain struct, like every other source
	// here, so that this package imports nothing from the framework it
	// measures. The consumer owns the App and converts; see
	// backend/ebiten.Run, which is the only caller that has one.
	Core func() CoreStats

	// Renderer supplies the renderer counters. It may be nil.
	//
	// It is a function and not a struct because the counters are read at
	// report time, out of band, and the backend is the only thing that
	// knows how to reach them safely.
	Renderer func() RendererStats

	// Shaper supplies the text shaping counters. It may be nil.
	Shaper func() ShaperStats

	// Asset supplies the image pipeline counters. It may be nil.
	//
	// It is a function for the same reason Renderer is: the numbers are
	// read at report time, out of band, and the application is the only
	// thing that knows which pipeline it built. See [AssetStats].
	Asset func() AssetStats
}

// AssetStats are the image pipeline numbers of one report.
//
// It is a plain struct for the same reason [RendererStats] and [ShaperStats]
// are: this package imports nothing from the framework it measures — not gift,
// not asset, not the backend — so no dependency ever points from a measured
// package to the measuring one. The *consumer* converts
// [asset.Pipeline.Stats] into this shape, which is the same seam the backend
// uses for its own counters:
//
//	pipe := asset.NewPipeline(asset.Config{Deliver: app.Post, ...})
//	opts.Asset = func() metrics.AssetStats {
//	    s := pipe.Stats()
//	    return metrics.AssetStats{Present: true, Requests: s.Requests, ...}
//	}
type AssetStats struct {
	// Present says whether a pipeline supplied these numbers at all.
	Present bool
	// Requests, Deduplicated and Promotions describe the demand: how much
	// was asked for, how much of it was already in flight, and how often a
	// speculative prefetch turned out to be needed after all.
	Requests, Deduplicated, Promotions uint64
	// Dropped, Cancelled and ReadyDropped are the saturation outcomes. A non
	// zero Dropped means the request queue was full; the project plan,
	// section 13, requires a budget violation to be visible rather than
	// silently absorbed.
	Dropped, Cancelled, ReadyDropped uint64
	// Completed, Failed and BackoffRefused are the delivered outcomes, and
	// Quarantined the number of sources taken out of service until their
	// revision changes. A Quarantined that climbs while Failed does not is a
	// catalogue with dead entries in it, which is a different problem from a
	// pipeline that is failing.
	Completed, Failed, BackoffRefused, Quarantined uint64
	// NotModified is the number of conditional fetches a server answered
	// with 304 — the measure of whether the freshness policy of the project
	// plan, section 9, is buying anything.
	NotModified uint64
	// Decodes, MemoryHits and DiskHits are where the pixels came from. This
	// is the cold versus warm distinction of the project plan, section 13.
	Decodes, MemoryHits, DiskHits uint64
	// DecodedPixels and ScaledPixels are the work volumes. A decode count
	// says nothing about how large the pictures were.
	DecodedPixels, ScaledPixels uint64
	// InputBytes, DecodeBytes and PixelBytes are the current occupancies of
	// the three byte budgets, and the Peak fields their high water marks.
	// The project plan, section 9, gives every stage a byte budget, and a
	// budget that is not reported cannot be checked.
	InputBytes, InputPeak, InputLimit    int64
	DecodeBytes, DecodePeak, DecodeLimit int64
	PixelBytes, PixelPeak, PixelLimit    int64
	// InputWaits, DecodeWaits and PixelWaits are how often a worker had to
	// wait for bytes of each budget.
	//
	// They are here because WU-R found a reservation bug that made HTTP
	// effectively single threaded, and no counter in this report showed it:
	// the peak looked healthy and the throughput did not. A budget that is
	// reported only by its occupancy hides contention, and contention is the
	// thing a budget is tuned against.
	InputWaits, DecodeWaits, PixelWaits uint64
	// CacheEntries is the CPU pixel cache occupancy and DiskBytes the
	// persistent one, against DiskBudget.
	CacheEntries          int
	DiskBytes, DiskBudget int64
}

// TargetStats are the intermediate render target counters of the backend.
//
// These are the render targets of the glass material of the project plan,
// section 8, plus the one screen sized scene target a material forces. They
// are the only images gift allocates that are neither a glyph atlas page nor a
// picture, so they get their own block in the report rather than being folded
// into the texture numbers.
type TargetStats struct {
	// Leases is the number of targets handed out and Reuses the subset served
	// from the pool without allocating. In a steady scene with glass on
	// screen the two grow together; a rising Allocations means something
	// changes size every frame.
	Leases, Reuses uint64
	// Allocations and Deallocations are the explicit create and release
	// calls. A gap that only grows is a leak.
	Allocations, Deallocations uint64
	// Evictions is the number released under budget pressure and
	// AgeEvictions the subset released for going unused.
	Evictions, AgeEvictions uint64
	// Rejected is the number of leases the budget refused, which means glass
	// degraded on screen. The project plan, section 13, forbids hiding that.
	Rejected uint64
	// Targets and Bytes are the current residency and PeakBytes the high
	// water mark, in logical pixel bytes with the same warning as
	// [TextureStats.Bytes].
	Targets   int
	Bytes     int64
	PeakBytes int64
}
