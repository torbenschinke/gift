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
	UnknownKinds       uint64 `json:"unknown_kinds"`
	// Accounted is Ops + Skipped + UnknownKinds and must equal painted_ops.
	Accounted uint64 `json:"accounted_ops"`

	// ShapeDrawCalls and GlyphDrawCalls split draw_calls by material, and
	// GlyphQuads says how much of the frame was text. A frame is one draw
	// call per run of same-material operations in display list order; see
	// [RendererStats.DrawCalls].
	ShapeDrawCalls uint64 `json:"shape_draw_calls"`
	GlyphDrawCalls uint64 `json:"glyph_draw_calls"`
	GlyphQuads     uint64 `json:"glyph_quads"`
	// ShadowOps and ShadowSharpOps are the shadow counters. There is no
	// shadow cache and therefore no hit ratio; see [RendererStats].
	ShadowOps      uint64 `json:"shadow_ops"`
	ShadowSharpOps uint64 `json:"shadow_sharp_ops"`

	Atlas atlasCounters `json:"atlas"`
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

	if r.opt.App != nil {
		d := r.opt.App.Diagnostics()
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
			UnknownKinds:       rs.UnknownKinds,
			Accounted:          rs.Accounted(),
			ShapeDrawCalls:     rs.ShapeDrawCalls,
			GlyphDrawCalls:     rs.GlyphDrawCalls,
			GlyphQuads:         rs.GlyphQuads,
			ShadowOps:          rs.ShadowOps,
			ShadowSharpOps:     rs.ShadowSharpOps,
			Atlas: atlasCounters{
				Hits: rs.Atlas.Hits, Misses: rs.Atlas.Misses,
				Rasterised: rs.Atlas.Rasterised, UploadedBytes: rs.Atlas.UploadedBytes,
				PageEvictions: rs.Atlas.PageEvictions, GlyphEvictions: rs.Atlas.GlyphEvictions,
				Rejected: rs.Atlas.Rejected,
				Pages:    rs.Atlas.Pages, Glyphs: rs.Atlas.Glyphs, Bytes: rs.Atlas.Bytes,
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
