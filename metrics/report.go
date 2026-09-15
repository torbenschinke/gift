//go:build giftmetrics

package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
)

// Recorder is one measurement session: the frame timer, the sources of the
// counters and the writer the reports go to.
//
// A nil *Recorder is valid and does nothing. [Start] returns nil when
// measurement is switched off, so a caller never has to test for it.
//
// The record methods are called from the frame callbacks and allocate
// nothing. [Recorder.Tick] and [Recorder.Close] format and write, and are
// therefore out of band by construction: Tick does nothing at all until the
// configured interval has elapsed.
type Recorder struct {
	timer *FrameTimer
	opt   Options

	enc    *json.Encoder
	closer io.Closer

	every time.Duration
	start time.Time
	last  time.Time
	done  bool
}

// Start begins a measurement session, or returns nil if measurement is off.
//
// Off means either that the binary was built without the giftmetrics tag or
// that GIFT_METRICS is not set to a true value. Every method of the returned
// value tolerates a nil receiver, so the caller does not branch.
func Start(o Options) *Recorder {
	if !Enabled() {
		return nil
	}
	r := &Recorder{
		timer: NewFrameTimerWith(o.Timer),
		opt:   o,
		every: env.interval,
		start: time.Now(),
	}
	r.last = r.start

	var w io.Writer = os.Stdout
	if env.out != "" {
		f, err := os.Create(env.out)
		if err != nil {
			// Falling back to stdout keeps the measurement rather than
			// losing it, and says so, which is the trade the package
			// documentation promises for malformed configuration.
			fmt.Fprintf(os.Stderr, "gift/metrics: cannot write GIFT_METRICS_OUT=%q, using stdout: %v\n", env.out, err)
		} else {
			w, r.closer = f, f
		}
	}
	r.enc = json.NewEncoder(w)
	return r
}

// Timer returns the frame timer of the session, or nil.
func (r *Recorder) Timer() *FrameTimer {
	if r == nil {
		return nil
	}
	return r.timer
}

// RecordUpdate records the CPU time of one update callback.
func (r *Recorder) RecordUpdate(d time.Duration) {
	if r == nil {
		return
	}
	r.timer.RecordUpdate(d)
}

// RecordDraw records the CPU time of one draw callback. It is not a GPU time;
// see [FrameTimes].
func (r *Recorder) RecordDraw(d time.Duration) {
	if r == nil {
		return
	}
	r.timer.RecordDraw(d)
}

// RecordInterval records the wall clock distance to the previous drawn frame.
func (r *Recorder) RecordInterval(d time.Duration) {
	if r == nil {
		return
	}
	r.timer.RecordInterval(d)
}

// Snapshot returns the current distributions, or the zero value.
func (r *Recorder) Snapshot() FrameTimes {
	if r == nil {
		return FrameTimes{}
	}
	return r.timer.Snapshot()
}

// Tick emits a periodic report if GIFT_METRICS_INTERVAL has elapsed.
//
// It is meant to be called once per update. Without an interval configured it
// is a comparison against zero and returns.
func (r *Recorder) Tick() {
	if r == nil || r.every <= 0 {
		return
	}
	now := time.Now()
	if now.Sub(r.last) < r.every {
		return
	}
	r.last = now
	r.emit("periodic")
}

// Close emits the final report and releases the output file. It is safe to
// call more than once.
func (r *Recorder) Close() error {
	if r == nil || r.done {
		return nil
	}
	r.done = true
	r.emit("final")
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// statsMillis is a duration distribution in milliseconds. Milliseconds because
// the thresholds of the project plan, section 13, are stated in them.
type statsMillis struct {
	Count int     `json:"count"`
	Min   float64 `json:"min_ms"`
	Mean  float64 `json:"mean_ms"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
	P999  float64 `json:"p999_ms"`
	Max   float64 `json:"max_ms"`
}

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func ms(s Stats) statsMillis {
	return statsMillis{
		Count: s.Count,
		Min:   millis(s.Min), Mean: millis(s.Mean), P50: millis(s.P50),
		P95: millis(s.P95), P99: millis(s.P99), P999: millis(s.P999), Max: millis(s.Max),
	}
}

// giftCounters is the frame path side of one report.
type giftCounters struct {
	Frames       uint64 `json:"paints"`
	Builds       uint64 `json:"builds"`
	Layouts      uint64 `json:"layouts"`
	PaintedNodes uint64 `json:"painted_nodes"`
	PaintedOps   uint64 `json:"painted_ops"`
	LiveNodes    uint64 `json:"live_nodes"`
	LiveScopes   uint64 `json:"live_scopes"`

	// OverflowNodes and OverflowExtent are the overflow model of the project
	// plan, section 7, made measurable. A healthy scene reports zero; a non
	// zero value is a container that does not fit what it contains.
	OverflowNodes  uint64  `json:"overflow_nodes"`
	OverflowExtent float32 `json:"overflow_extent_px"`
}

// rendererCounters carries the skip counters one per reason; see
// [RendererStats].
type rendererCounters struct {
	Frames             uint64 `json:"frames"`
	DrawCalls          uint64 `json:"draw_calls"`
	Ops                uint64 `json:"ops"`
	Skipped            uint64 `json:"skipped_ops"`
	SkippedNone        uint64 `json:"skipped_none"`
	SkippedTransparent uint64 `json:"skipped_transparent"`
	SkippedEmptyBounds uint64 `json:"skipped_empty_bounds"`
	SkippedEmptyClip   uint64 `json:"skipped_empty_clip"`
	SkippedOutsideClip uint64 `json:"skipped_outside_clip"`
	SkippedZeroStroke  uint64 `json:"skipped_zero_stroke"`
	SkippedEmptyText   uint64 `json:"skipped_empty_text"`
	SkippedNoImage     uint64 `json:"skipped_no_image"`
	UnknownKinds       uint64 `json:"unknown_kinds"`
	// Accounted is Ops + Skipped + UnknownKinds and must equal painted_ops.
	Accounted uint64 `json:"accounted_ops"`

	// ShapeDrawCalls and GlyphDrawCalls split draw_calls by material, and
	// GlyphQuads says how much of the frame was text. A frame is one draw
	// call per run of same-material operations in display list order; see
	// [RendererStats.DrawCalls].
	ShapeDrawCalls uint64 `json:"shape_draw_calls"`
	GlyphDrawCalls uint64 `json:"glyph_draw_calls"`
	ImageDrawCalls uint64 `json:"image_draw_calls"`
	GlyphQuads     uint64 `json:"glyph_quads"`
	ImageOps       uint64 `json:"image_ops"`
	// ShadowOps and ShadowSharpOps are the shadow counters. There is no
	// shadow cache and therefore no hit ratio; see [RendererStats].
	ShadowOps      uint64 `json:"shadow_ops"`
	ShadowSharpOps uint64 `json:"shadow_sharp_ops"`

	// Glass is omitted entirely on a run with no material on screen. A block
	// reporting `"level":"full"` and seven zeroes for an application that
	// has never drawn a pane says something about the policy's internal
	// state and nothing about the run, and a reader comparing two reports
	// has to know that to discount it. No glass, no glass block.
	Glass    *glassCounters  `json:"glass,omitempty"`
	Atlas    atlasCounters   `json:"atlas"`
	Textures textureCounters `json:"textures"`
	Targets  targetCounters  `json:"targets"`
}

// glassCounters are the material numbers of the project plan, section 8.
//
// level and pinned are the two fields that decide whether this report may be
// compared with another one at all. Section 13 makes a fixed quality level a
// precondition of a comparable measurement, so a report from an adaptive run
// says so instead of quietly averaging two different amounts of work.
type glassCounters struct {
	Ops        uint64 `json:"ops"`
	ReducedOps uint64 `json:"reduced_ops"`
	FullOps    uint64 `json:"full_ops"`
	// Fallbacks is the number of materials drawn as a plain tint because no
	// backdrop could be obtained. Non zero means glass degraded on screen.
	Fallbacks uint64 `json:"fallbacks"`
	Passes    uint64 `json:"passes"`
	DrawCalls uint64 `json:"draw_calls"`
	Level     string `json:"level"`
	Pinned    bool   `json:"pinned"`
	// LevelChanges over the whole run. In an adaptive run a large number is
	// the flicker the hysteresis exists to prevent.
	LevelChanges uint64 `json:"level_changes"`
}

// glassOf returns the glass block of a report, or nil when the run drew no
// material at all. See [report.Renderer].
func glassOf(rs RendererStats) *glassCounters {
	if rs.GlassOps == 0 && rs.GlassPasses == 0 && rs.GlassFallbacks == 0 && !rs.GlassPinned {
		return nil
	}
	return &glassCounters{
		Ops: rs.GlassOps, ReducedOps: rs.GlassReducedOps,
		FullOps: rs.GlassFullOps, Fallbacks: rs.GlassFallbacks,
		Passes: rs.GlassPasses, DrawCalls: rs.GlassDrawCalls,
		Level: rs.GlassLevel, Pinned: rs.GlassPinned,
		LevelChanges: rs.GlassLevelChanges,
	}
}

// targetCounters are the intermediate render target numbers; see
// [TargetStats]. Bytes is logical pixel bytes, with the same warning as the
// texture numbers carry.
type targetCounters struct {
	Leases        uint64 `json:"leases"`
	Reuses        uint64 `json:"reuses"`
	Allocations   uint64 `json:"allocations"`
	Deallocations uint64 `json:"deallocations"`
	Evictions     uint64 `json:"evictions"`
	AgeEvictions  uint64 `json:"age_evictions"`
	Rejected      uint64 `json:"rejected"`
	Targets       int    `json:"targets"`
	Bytes         int64  `json:"bytes"`
	PeakBytes     int64  `json:"peak_bytes"`
}

// textureCounters are the GPU image residency numbers; see [TextureStats].
type textureCounters struct {
	Uploads       uint64 `json:"uploads"`
	UploadedBytes uint64 `json:"uploaded_bytes"`
	// Deferred is the budget doing its job, Rejected is the budget being too
	// small. They are not the same number and are never added up.
	Deferred         uint64 `json:"deferred"`
	Rejected         uint64 `json:"rejected"`
	Evictions        uint64 `json:"evictions"`
	AgeEvictions     uint64 `json:"age_evictions"`
	ExplicitReleases uint64 `json:"explicit_releases"`
	Deallocations    uint64 `json:"deallocations"`
	Stale            uint64 `json:"stale_handles"`
	Textures         int    `json:"textures"`
	// Bytes is logical pixel bytes and not device memory; see
	// [TextureStats.Bytes].
	Bytes     int64 `json:"logical_bytes"`
	PeakBytes int64 `json:"peak_logical_bytes"`
}

// atlasCounters are the glyph atlas numbers; see [AtlasStats].
type atlasCounters struct {
	Hits           uint64 `json:"hits"`
	Misses         uint64 `json:"misses"`
	Rasterised     uint64 `json:"rasterised"`
	UploadedBytes  uint64 `json:"uploaded_bytes"`
	PageEvictions  uint64 `json:"page_evictions"`
	GlyphEvictions uint64 `json:"glyph_evictions"`
	// Rejected non zero means glyphs did not fit and text is missing.
	Rejected uint64 `json:"rejected"`
	Pages    int    `json:"pages"`
	Glyphs   int    `json:"glyphs"`
	Bytes    int    `json:"bytes"`
}

type shaperCounters struct {
	Hits         uint64 `json:"hits"`
	Misses       uint64 `json:"misses"`
	Evictions    uint64 `json:"evictions"`
	AgeEvictions uint64 `json:"age_evictions"`
	ShapedGlyphs uint64 `json:"shaped_glyphs"`
	Entries      int    `json:"entries"`
	Bytes        uint64 `json:"bytes"`
}

// assetCounters is the image pipeline section of a report.
type assetCounters struct {
	Requests       uint64 `json:"requests"`
	Deduplicated   uint64 `json:"deduplicated"`
	Promotions     uint64 `json:"promotions"`
	Dropped        uint64 `json:"dropped"`
	Cancelled      uint64 `json:"cancelled"`
	ReadyDropped   uint64 `json:"ready_dropped"`
	Completed      uint64 `json:"completed"`
	Failed         uint64 `json:"failed"`
	BackoffRefused uint64 `json:"backoff_refused"`
	Quarantined    uint64 `json:"quarantined"`
	NotModified    uint64 `json:"not_modified"`
	Decodes        uint64 `json:"decodes"`
	MemoryHits     uint64 `json:"memory_hits"`
	DiskHits       uint64 `json:"disk_hits"`
	DecodedPixels  uint64 `json:"decoded_pixels"`
	ScaledPixels   uint64 `json:"scaled_pixels"`
	InputBytes     int64  `json:"input_bytes"`
	InputPeak      int64  `json:"input_peak_bytes"`
	InputLimit     int64  `json:"input_limit_bytes"`
	InputWaits     uint64 `json:"input_waits"`
	DecodeBytes    int64  `json:"decode_bytes"`
	DecodePeak     int64  `json:"decode_peak_bytes"`
	DecodeLimit    int64  `json:"decode_limit_bytes"`
	DecodeWaits    uint64 `json:"decode_waits"`
	PixelBytes     int64  `json:"pixel_bytes"`
	PixelPeak      int64  `json:"pixel_peak_bytes"`
	PixelLimit     int64  `json:"pixel_limit_bytes"`
	PixelWaits     uint64 `json:"pixel_waits"`
	CacheEntries   int    `json:"cache_entries"`
	DiskBytes      int64  `json:"disk_bytes"`
	DiskBudget     int64  `json:"disk_budget_bytes"`
}

// report is one line of output.
//
// The three timing series are separate fields on purpose: CPU time in the
// update callback, CPU time in the draw callback and the wall clock distance
// between drawn frames measure three different things, and the project plan,
// sections 11 and 13, forbids adding them up. None of them observes the GPU.
type report struct {
	Kind     string  `json:"kind"`
	ElapsedS float64 `json:"elapsed_s"`

	Gift     *giftCounters     `json:"gift,omitempty"`
	Renderer *rendererCounters `json:"renderer,omitempty"`
	Shaper   *shaperCounters   `json:"shaper,omitempty"`
	Asset    *assetCounters    `json:"asset,omitempty"`

	UpdateCPU     statsMillis `json:"update_cpu"`
	DrawCPU       statsMillis `json:"draw_cpu"`
	FrameInterval statsMillis `json:"frame_interval"`

	// NominalIntervalMs is the interval the target frame rate implies.
	// MissedThresholdMs is the value a frame interval is actually compared
	// against: the nominal interval plus the configured tolerance. The two
	// differ because no display runs at exactly its nominal rate, and
	// comparing against the bare quotient reports every well timed frame as
	// missed.
	NominalIntervalMs float64 `json:"nominal_interval_ms"`
	ToleranceMs       float64 `json:"interval_tolerance_ms"`
	MissedThresholdMs float64 `json:"missed_threshold_ms"`
	MissedIntervals   int     `json:"missed_intervals"`
	MissedRatio       float64 `json:"missed_ratio"`

	// WarmupFrames intervals were discarded before the window started, and
	// SubFrameIntervals draw-to-draw distances were shorter than
	// MinIntervalMs and are not treated as frames. Both are printed because
	// missed_ratio is only interpretable next to what was excluded from it.
	WarmupFrames      int     `json:"warmup_frames"`
	WarmupDropped     uint64  `json:"warmup_dropped"`
	MinIntervalMs     float64 `json:"min_interval_ms"`
	SubFrameIntervals uint64  `json:"sub_frame_intervals"`

	Updates uint64 `json:"updates"`
	Draws   uint64 `json:"draws"`

	Mem struct {
		HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
		TotalAlloc     uint64 `json:"total_alloc_bytes"`
		Mallocs        uint64 `json:"mallocs"`
		NumGC          uint32 `json:"num_gc"`
	} `json:"mem"`
}

// emit writes one report. Everything it reads is a snapshot taken out of
// band: gift's diagnostics and the frame timer are both synchronised, and the
// renderer accessor is supplied by whoever owns the renderer.
func (r *Recorder) emit(kind string) {
	ft := r.timer.Snapshot()

	var m report
	m.Kind = kind
	m.ElapsedS = time.Since(r.start).Seconds()

	if r.opt.Core != nil {
		d := r.opt.Core()
		m.Gift = &giftCounters{
			Frames: d.Frames, Builds: d.Builds, Layouts: d.Layouts,
			PaintedNodes: d.PaintedNodes, PaintedOps: d.PaintedOps,
			LiveNodes: d.LiveNodes, LiveScopes: d.LiveScopes,
			OverflowNodes: d.OverflowNodes, OverflowExtent: d.OverflowExtent,
		}
	}
	if r.opt.Renderer != nil {
		rs := r.opt.Renderer()
		m.Renderer = &rendererCounters{
			Frames: rs.Frames, DrawCalls: rs.DrawCalls, Ops: rs.Ops,
			Skipped:            rs.Skipped(),
			SkippedNone:        rs.SkippedNone,
			SkippedTransparent: rs.SkippedTransparent,
			SkippedEmptyBounds: rs.SkippedEmptyBounds,
			SkippedEmptyClip:   rs.SkippedEmptyClip,
			SkippedOutsideClip: rs.SkippedOutsideClip,
			SkippedZeroStroke:  rs.SkippedZeroStroke,
			SkippedEmptyText:   rs.SkippedEmptyText,
			SkippedNoImage:     rs.SkippedNoImage,
			UnknownKinds:       rs.UnknownKinds,
			Accounted:          rs.Accounted(),
			ShapeDrawCalls:     rs.ShapeDrawCalls,
			GlyphDrawCalls:     rs.GlyphDrawCalls,
			ImageDrawCalls:     rs.ImageDrawCalls,
			GlyphQuads:         rs.GlyphQuads,
			ImageOps:           rs.ImageOps,
			ShadowOps:          rs.ShadowOps,
			ShadowSharpOps:     rs.ShadowSharpOps,
			Glass:              glassOf(rs),
			Targets: targetCounters{
				Leases: rs.Targets.Leases, Reuses: rs.Targets.Reuses,
				Allocations: rs.Targets.Allocations, Deallocations: rs.Targets.Deallocations,
				Evictions: rs.Targets.Evictions, AgeEvictions: rs.Targets.AgeEvictions,
				Rejected: rs.Targets.Rejected,
				Targets:  rs.Targets.Targets, Bytes: rs.Targets.Bytes,
				PeakBytes: rs.Targets.PeakBytes,
			},
			Atlas: atlasCounters{
				Hits: rs.Atlas.Hits, Misses: rs.Atlas.Misses,
				Rasterised: rs.Atlas.Rasterised, UploadedBytes: rs.Atlas.UploadedBytes,
				PageEvictions: rs.Atlas.PageEvictions, GlyphEvictions: rs.Atlas.GlyphEvictions,
				Rejected: rs.Atlas.Rejected,
				Pages:    rs.Atlas.Pages, Glyphs: rs.Atlas.Glyphs, Bytes: rs.Atlas.Bytes,
			},
			Textures: textureCounters{
				Uploads: rs.Textures.Uploads, UploadedBytes: rs.Textures.UploadedBytes,
				Deferred: rs.Textures.Deferred, Rejected: rs.Textures.Rejected,
				Evictions: rs.Textures.Evictions, AgeEvictions: rs.Textures.AgeEvictions,
				ExplicitReleases: rs.Textures.ExplicitReleases,
				Deallocations:    rs.Textures.Deallocations,
				Stale:            rs.Textures.Stale,
				Textures:         rs.Textures.Textures,
				Bytes:            rs.Textures.Bytes, PeakBytes: rs.Textures.PeakBytes,
			},
		}
	}
	if r.opt.Shaper != nil {
		if ss := r.opt.Shaper(); ss.Present {
			m.Shaper = &shaperCounters{
				Hits: ss.Hits, Misses: ss.Misses,
				Evictions: ss.Evictions, AgeEvictions: ss.AgeEvictions,
				ShapedGlyphs: ss.ShapedGlyphs,
				Entries:      ss.Entries, Bytes: ss.Bytes,
			}
		}
	}

	if r.opt.Asset != nil {
		if as := r.opt.Asset(); as.Present {
			m.Asset = &assetCounters{
				Requests: as.Requests, Deduplicated: as.Deduplicated,
				Promotions: as.Promotions, Dropped: as.Dropped,
				Cancelled: as.Cancelled, ReadyDropped: as.ReadyDropped,
				Completed: as.Completed, Failed: as.Failed,
				BackoffRefused: as.BackoffRefused,
				Quarantined:    as.Quarantined, NotModified: as.NotModified,
				Decodes: as.Decodes, MemoryHits: as.MemoryHits, DiskHits: as.DiskHits,
				DecodedPixels: as.DecodedPixels, ScaledPixels: as.ScaledPixels,
				InputBytes: as.InputBytes, InputPeak: as.InputPeak, InputLimit: as.InputLimit,
				DecodeBytes: as.DecodeBytes, DecodePeak: as.DecodePeak, DecodeLimit: as.DecodeLimit,
				PixelBytes: as.PixelBytes, PixelPeak: as.PixelPeak, PixelLimit: as.PixelLimit,
				InputWaits: as.InputWaits, DecodeWaits: as.DecodeWaits, PixelWaits: as.PixelWaits,
				CacheEntries: as.CacheEntries,
				DiskBytes:    as.DiskBytes, DiskBudget: as.DiskBudget,
			}
		}
	}

	m.UpdateCPU = ms(ft.UpdateCPU)
	m.DrawCPU = ms(ft.DrawCPU)
	m.FrameInterval = ms(ft.FrameInterval)
	m.NominalIntervalMs = millis(ft.NominalInterval)
	m.ToleranceMs = millis(ft.Tolerance)
	m.MissedThresholdMs = millis(ft.TargetInterval)
	m.MissedIntervals = ft.MissedIntervals
	m.MissedRatio = ft.MissedRatio
	m.WarmupFrames = ft.Warmup
	m.WarmupDropped = ft.WarmupDropped
	m.MinIntervalMs = millis(ft.MinInterval)
	m.SubFrameIntervals = ft.SubFrameIntervals
	m.Updates = ft.Updates
	m.Draws = ft.Draws

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.Mem.HeapAllocBytes = mem.HeapAlloc
	m.Mem.TotalAlloc = mem.TotalAlloc
	m.Mem.Mallocs = mem.Mallocs
	m.Mem.NumGC = mem.NumGC

	_ = r.enc.Encode(&m)
}
