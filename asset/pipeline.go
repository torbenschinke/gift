package asset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	xdraw "golang.org/x/image/draw"
)

// Priority is the two level scheduling class of the project plan, section 9:
// "Sichtbare Bilder haben Vorrang vor richtungsabhaengigem Prefetch."
//
// There are two and not five because there are two questions: is this picture
// on the screen right now, or is it a guess about where the user is scrolling.
// A guess must never delay an answer.
type Priority uint8

// The scheduling classes, in increasing order of urgency.
const (
	// Prefetch is speculative work for a picture that is not visible.
	Prefetch Priority = iota
	// Visible is work for a picture the user is looking at.
	Visible
)

func (p Priority) String() string {
	if p == Visible {
		return "visible"
	}
	return "prefetch"
}

// Result is the answer to one [Request].
//
// It is delivered on the delivery executor — see [Config.Deliver] — which for
// a gift application is the UI executor, so the callback may touch user
// interface state. Exactly one Result is delivered per accepted request,
// unless the request was cancelled, in which case none is.
type Result struct {
	// ID is the picture the result belongs to.
	ID ID
	// Generation is the number the requester passed in, echoed back
	// unchanged. The pipeline never interprets it; see [Request.Generation].
	Generation uint64
	// Size is the ladder rung actually produced, which may differ from the
	// requested size because of the hysteresis of [Config.Sizes].
	Size int
	// Image is the thumbnail, or nil when Err is set.
	//
	// It is valid for the duration of the callback. A consumer that keeps it
	// longer — a GPU uploader, for instance — must [Thumbnail.Retain] it and
	// release it when it is done.
	Image *Thumbnail
	// Metadata is what the pipeline learned: the oriented dimensions, the
	// revision and the media type. It is what a caller feeds into
	// [Collection.ApplyCorrections].
	Metadata Metadata
	// Orientation is the EXIF orientation that was applied.
	Orientation Orientation

	// Retry and Failure are the two kinds of "no picture this time", and
	// they are two fields rather than one so that a consumer cannot treat
	// them alike by accident.
	//
	// Retry is pressure inside the pipeline that passes by itself:
	// [ErrQueueFull] because the queue was saturated, [ErrBackoff] because
	// the source failed a moment ago and is waiting out its delay. Nothing
	// is wrong with the picture. A consumer must *not* remember it: it
	// clears its state and asks again on a later frame, rate limited by a
	// frame or layout counter. Latching one blanks a tile for the life of
	// the process, which is the defect this split exists to prevent.
	//
	// Failure is the outcome that will repeat until the revision changes or
	// [Pipeline.Forget] is called: not a picture, over the limits, a bad
	// URL, a quarantined source, a closed pipeline. A consumer may and
	// should remember it and show an error state.
	//
	// Both are ordinary values and never panics; see the project plan,
	// section 15. At most one of them is set. Use [Result.Err] when all that
	// is wanted is something to log, and [Retryable] to classify an error
	// that arrived by some other route.
	Retry, Failure error

	// ImageWithheld reports a *successful* result whose thumbnail could not
	// be handed over because the ready queue was full; see
	// [Config.ReadyLimit].
	//
	// It is not an error and Failure and Retry are nil. The pixels are in
	// the CPU cache and everything else in this Result — the identity, the
	// rung, the revision, the metadata — is exactly what a successful
	// delivery carries. A consumer treats it as success and picks the
	// picture up with [Pipeline.Lookup] on the next frame.
	ImageWithheld bool
	// FromDisk and FromMemory report which cache answered, for diagnostics
	// and for the cold versus warm measurements of the project plan,
	// section 13.
	FromDisk, FromMemory bool
}

// Err returns whichever of [Result.Failure] and [Result.Retry] is set, or nil.
//
// It exists for logging and for a test that only wants to know whether
// something went wrong. It is deliberately a method and not the field it
// replaced: a field named Err invites "if r.Err != nil { give up }", and that
// one line, written twice in this repository, is what turned a moment of queue
// saturation into a permanently blank picture.
func (r Result) Err() error {
	if r.Failure != nil {
		return r.Failure
	}
	return r.Retry
}

// OK reports whether the request produced a picture. A result with
// [Result.ImageWithheld] is OK; its pixels are in the CPU cache.
func (r Result) OK() bool { return r.Failure == nil && r.Retry == nil }

// setErr files err under Retry or Failure according to [Retryable].
func (r *Result) setErr(err error) {
	if err == nil {
		return
	}
	if Retryable(err) {
		r.Retry = err
	} else {
		r.Failure = err
	}
}

// errResult is the shorthand for the many "nothing came of it" returns.
func errResult(err error, meta Metadata) Result {
	r := Result{Metadata: meta}
	r.setErr(err)
	return r
}

// Correction turns the metadata of a result into a catalogue correction, so
// that probed dimensions reach the gallery the way section 10 requires: by
// stable ID, in a batch.
func (r Result) Correction() Correction {
	return Correction{
		ID:       r.ID,
		Width:    r.Metadata.Width,
		Height:   r.Metadata.Height,
		Revision: r.Metadata.Revision,
	}
}

// Request is one demand for a thumbnail.
type Request struct {
	// Source is the picture. It must not be nil.
	Source Source
	// Size is the desired length of the longest edge in pixels. The
	// pipeline maps it to a rung of [Config.Sizes] with hysteresis.
	Size int
	// Priority is the scheduling class.
	Priority Priority
	// Generation is an opaque number echoed back in [Result.Generation].
	//
	// The pipeline does not interpret it and deliberately does not check it:
	// the meaning of a generation belongs to whoever issues requests, and
	// asset must not know what a tile or a scope is. The consumer compares
	// it — against [ui.TileBinding.Generation] or [gift.Token] — before
	// accepting the result. See the package documentation.
	Generation uint64
	// OnResult receives the answer on the delivery executor. It must not be
	// nil.
	OnResult func(Result)
}

// Ticket is a handle on an accepted request.
//
// Its only purpose is [Ticket.Cancel]. The zero Ticket is inert.
type Ticket struct {
	p  *Pipeline
	j  *job
	id uint64
}

// Cancel withdraws the request. If nothing else wants the same work it is
// dropped from the queue.
//
// # What it does and does not promise
//
// It does *not* promise that the callback will not run. The waiter is removed
// from the job, so a job that has not finished will not call it — but a job
// whose worker has already taken the waiters out of the scheduler delivers to
// all of them, and the delivery executor may run that closure later still.
// Cancel and an in flight delivery are a race, and the loser is whichever
// arrived second.
//
// What makes that harmless is [Request.Generation], and it is the reason the
// pipeline insists on one: the consumer compares the generation it gets back
// against the one the tile or the scope has now and drops the answer if they
// differ. Both consumers in this repository do; see ui.Gallery.onImage.
//
// Cancelling work that has already started also does not stop it: the standard
// decoders are not interruptible, and the project plan, section 9, says so
// explicitly. What cancelling guarantees is that the work is dropped if nobody
// else wants it and that no *new* work is started for it.
func (t Ticket) Cancel() {
	if t.p == nil {
		return
	}
	t.p.cancel(t.j, t.id)
}

// BackoffPolicy governs how a failing source is retried.
//
// The project plan, section 15, requires that "ein Fehler erzeugt keinen
// Retry-Sturm" and that failed sources are not reloaded until their revision
// changes. Both halves are implemented, and they apply to different failures:
//
//   - A failure that a retry cannot fix — a 404, a file that is not a
//     picture, a picture over the pixel limit — quarantines the source. It is
//     not attempted again until its revision changes or [Pipeline.Forget] is
//     called for it. A request in the meantime gets [ErrBackoff] without any
//     work being done.
//   - A failure that a retry might fix — a timeout, a 503, a dropped
//     connection — gets an exponential delay from Base, doubling per attempt
//     and capped at Max. After Attempts failures the source is quarantined as
//     above.
type BackoffPolicy struct {
	// Base is the delay after the first temporary failure. Zero selects
	// 500 ms.
	Base time.Duration
	// Max caps the delay. Zero selects 30 s.
	Max time.Duration
	// Attempts is how many temporary failures are tolerated before the
	// source is quarantined. Zero selects three.
	Attempts int
}

func (b BackoffPolicy) withDefaults() BackoffPolicy {
	if b.Base <= 0 {
		b.Base = 500 * time.Millisecond
	}
	if b.Max <= 0 {
		b.Max = 30 * time.Second
	}
	if b.Attempts <= 0 {
		b.Attempts = 3
	}
	return b
}

// Config configures a [Pipeline]. The zero value is usable and picks the
// defaults documented on each field.
type Config struct {
	// Workers is the number of decode goroutines. Zero selects
	// min(4, GOMAXPROCS).
	Workers int

	// Sizes is the thumbnail ladder: the lengths of the longest edge the
	// pipeline is willing to produce, in pixels. Nil selects
	// [DefaultSizes].
	//
	// A ladder and not a free size, because a gallery whose column width
	// changes by one pixel per resize step would otherwise re-decode
	// everything, and because the disk cache would fill with near duplicates.
	Sizes []int

	// Hysteresis is how far a cached rung may be from the requested size and
	// still be used, as a factor. Zero selects [DefaultHysteresis]. A value
	// at or below one disables it, so every request snaps to the nearest
	// rung upward.
	Hysteresis float64

	// MaxPixels refuses a picture whose declared stored dimensions exceed it,
	// before a single pixel is allocated. Zero selects [DefaultMaxPixels].
	MaxPixels int

	// MaxEncodedBytes refuses an encoded picture larger than this. Zero
	// selects [DefaultMaxEncodedBytes]. It also bounds a source that lies
	// about its length or does not declare one: the body is read through a
	// limit and a stream that exceeds it fails with [ErrTooLarge].
	MaxEncodedBytes int64

	// InputBudget bounds the encoded bytes held in memory across all workers.
	// Zero selects [DefaultInputBudget].
	InputBudget int64

	// DecodeBudget bounds the working memory reserved for decoding across all
	// workers. The reservation is computed from the picture's stored
	// dimensions and the codec's [Decoder.MemoryFactor], not from the size of
	// the thumbnail that comes out; the project plan, section 9, is explicit
	// about that. Zero selects [DefaultDecodeBudget].
	DecodeBudget int64

	// PixelBudget bounds every live thumbnail in the process: the ones in
	// the CPU cache, the ones on their way to the user interface and the
	// ones a GPU uploader still holds. Zero selects [DefaultPixelBudget].
	PixelBudget int64

	// QueueLimit is the number of requests that may be waiting for a worker.
	// Zero selects [DefaultQueueLimit]. At the limit a prefetch is refused
	// with [ErrQueueFull] and a visible request evicts the oldest waiting
	// prefetch; see [Pipeline.Request].
	QueueLimit int

	// ReadyLimit is the number of finished results that may be waiting for
	// the delivery executor to pick them up. Zero selects
	// [DefaultReadyLimit].
	ReadyLimit int

	// Timeout bounds one request end to end. Zero selects [DefaultTimeout];
	// a negative value disables it.
	Timeout time.Duration

	// Backoff governs retries; see [BackoffPolicy].
	Backoff BackoffPolicy

	// Disk configures the persistent thumbnail cache. An empty
	// [DiskCacheConfig.Dir] switches it off.
	Disk DiskCacheConfig

	// ProcessingVersion overrides [ProcessingVersion] in cache keys. It is
	// for applications that post-process thumbnails themselves and need to
	// invalidate on their own schedule. Zero selects [ProcessingVersion].
	ProcessingVersion uint32

	// Deliver runs a closure on the executor that owns the user interface.
	//
	// This is the seam of the project plan, section 3: asset must not import
	// gift, so the *consumer* wires the two together, normally with
	//
	//	cfg.Deliver = app.Post
	//
	// which posts the closure to the UI executor, where it runs at the
	// beginning of the next update. Nil selects a direct call on the worker
	// goroutine, which is only appropriate in a test.
	//
	// Every closure handed to Deliver must eventually run, including the
	// ones delivered during [Pipeline.Close]. The delivery reference on a
	// [Thumbnail] is released inside the closure, so an executor that
	// silently drops one holds those pixels against [Config.PixelBudget]
	// until the process exits. Drain the executor once more after Close; for a
	// gift application that is gift.App.DrainPosts.
	Deliver func(func())

	// Logger receives pipeline diagnostics. Nil disables logging entirely.
	// Gift never logs to [slog.Default]; see the project plan, section 15.
	// Nothing in this package logs on a path that can run during layout.
	Logger *slog.Logger

	// Now supplies the clock, for tests. Nil selects [time.Now].
	Now func() time.Time

	// Scaler is the interpolator used to produce thumbnails. Nil selects
	// [xdraw.ApproxBiLinear], which is the quality per cycle compromise a
	// Raspberry Pi wants.
	Scaler xdraw.Interpolator
}

// The defaults of [Config]. They are sized for the reference machine of the
// project plan, section 1: a Raspberry Pi 4 with a gallery of a hundred
// thousand entries at 1920 by 1080.
var (
	// DefaultSizes is the thumbnail ladder. 256 is the size the project plan,
	// section 10, computes its memory figures with.
	DefaultSizes = []int{128, 256, 512}
)

// Numeric defaults of [Config].
const (
	// DefaultHysteresis keeps a cached rung for a request up to a quarter
	// away from it, in either direction.
	DefaultHysteresis = 1.25

	// DefaultMaxPixels is 64 megapixels, comfortably above any camera a
	// gallery will meet and far below the point where a decode kills a Pi.
	DefaultMaxPixels = 64 << 20

	// DefaultMaxEncodedBytes is 64 MiB.
	DefaultMaxEncodedBytes int64 = 64 << 20

	// DefaultInputBudget is 96 MiB of encoded bytes in flight.
	DefaultInputBudget int64 = 96 << 20

	// DefaultDecodeBudget is 256 MiB of decoder working memory in flight.
	DefaultDecodeBudget int64 = 256 << 20

	// DefaultPixelBudget is 64 MiB of thumbnails, which is 256 entries at
	// 256 by 256 — exactly the working set the project plan, section 10,
	// computes.
	DefaultPixelBudget int64 = 64 << 20

	// DefaultQueueLimit is 512 waiting requests.
	DefaultQueueLimit = 512

	// DefaultReadyLimit is 64 finished results waiting for the executor.
	DefaultReadyLimit = 64

	// DefaultTimeout bounds one request at thirty seconds.
	DefaultTimeout = 30 * time.Second

	// inputProbeBytes is what a fetch of unknown length reserves against
	// [Config.InputBudget] until its Content-Length is known. It is a token
	// and not a guess at a picture size: it only has to cover the moment
	// between issuing the request and reading the response headers.
	inputProbeBytes int64 = 64 << 10
)

func (c Config) withDefaults() Config {
	if c.Workers <= 0 {
		c.Workers = min(4, max(1, runtimeProcs()))
	}
	if len(c.Sizes) == 0 {
		c.Sizes = DefaultSizes
	}
	if c.Hysteresis <= 0 {
		c.Hysteresis = DefaultHysteresis
	}
	if c.MaxPixels <= 0 {
		c.MaxPixels = DefaultMaxPixels
	}
	if c.MaxEncodedBytes <= 0 {
		c.MaxEncodedBytes = DefaultMaxEncodedBytes
	}
	if c.InputBudget <= 0 {
		c.InputBudget = DefaultInputBudget
	}
	if c.DecodeBudget <= 0 {
		c.DecodeBudget = DefaultDecodeBudget
	}
	if c.PixelBudget <= 0 {
		c.PixelBudget = DefaultPixelBudget
	}
	if c.QueueLimit <= 0 {
		c.QueueLimit = DefaultQueueLimit
	}
	if c.ReadyLimit <= 0 {
		c.ReadyLimit = DefaultReadyLimit
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.ProcessingVersion == 0 {
		c.ProcessingVersion = ProcessingVersion
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Scaler == nil {
		c.Scaler = xdraw.ApproxBiLinear
	}
	c.Backoff = c.Backoff.withDefaults()
	return c
}

// Pipeline is the application wide image service of the project plan,
// section 9.
//
// One per application. It deduplicates requests, probes and validates sources,
// schedules bounded fetch and decode work, orients and scales, keeps a
// persistent thumbnail cache and a bounded CPU pixel cache, and hands results
// to the user interface through [Config.Deliver].
//
// Every method is safe to call from any goroutine. Nothing in it calls into
// gift: results travel as closures through [Config.Deliver], which the consumer
// wires to the UI executor.
type Pipeline struct {
	cfg  Config
	disk *diskCache
	pix  *pixelCache

	input  *budget
	decode *budget
	pixels *budget

	sched  *scheduler
	ready  chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
	ctx    context.Context
	stop   context.CancelFunc

	hintMu sync.Mutex
	hints  map[ID]hint

	failMu sync.Mutex
	fails  map[ID]*failure

	counters counters
}

type failure struct {
	revision   string
	until      time.Time
	attempts   int
	quarantine bool
	last       error
}

type counters struct {
	requests, deduped, dropped, cancelled  atomic.Uint64
	completed, failed, backoff, readyDrops atomic.Uint64
	decodes, diskHits, memHits, promotions atomic.Uint64
	decodedPixels, scaledPixels            atomic.Uint64
	notModified, quarantined               atomic.Uint64
}

// NewPipeline starts a pipeline with cfg. Close it with [Pipeline.Close].
func NewPipeline(cfg Config) *Pipeline {
	cfg = cfg.withDefaults()
	ctx, stop := context.WithCancel(context.Background())
	p := &Pipeline{
		cfg:    cfg,
		disk:   newDiskCache(cfg.Disk),
		pix:    newPixelCache(),
		input:  newBudget(cfg.InputBudget),
		decode: newBudget(cfg.DecodeBudget),
		pixels: newBudget(cfg.PixelBudget),
		ready:  make(chan struct{}, cfg.ReadyLimit),
		ctx:    ctx,
		stop:   stop,
		hints:  make(map[ID]hint),
		fails:  make(map[ID]*failure),
	}
	p.sched = newScheduler(cfg.QueueLimit)
	p.wg.Add(cfg.Workers)
	for range cfg.Workers {
		go p.worker()
	}
	if cfg.Logger != nil {
		cfg.Logger.Info("asset pipeline started",
			slog.Int("workers", cfg.Workers),
			slog.Int64("pixel_budget", cfg.PixelBudget),
			slog.Bool("disk_cache", p.disk != nil))
	}
	return p
}

// Close stops the workers and releases everything the pipeline holds.
//
// Requests still in flight are cancelled and their callbacks receive
// [ErrClosed]; the project plan, section 13, asks for exactly that — "Freigabe
// bei Fehler, Abbruch, Queue-Saettigung und Shutdown". Close blocks until every
// worker has returned and is idempotent.
func (p *Pipeline) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	p.stop()
	for _, j := range p.sched.close() {
		p.finish(j, errResult(ErrClosed, Metadata{ID: j.id}), nil)
	}
	p.input.close()
	p.decode.close()
	p.pixels.close()
	p.wg.Wait()
	p.pix.clear()
	if p.cfg.Logger != nil {
		p.cfg.Logger.Info("asset pipeline stopped")
	}
	return nil
}

// Forget clears the failure state of a source, so that the next request for it
// is attempted again.
//
// It is the manual half of [BackoffPolicy]: a quarantined source is not
// retried by itself, because retrying something that will fail the same way is
// the retry storm the project plan forbids. An application that knows better —
// the network came back, the user pressed reload — says so here.
func (p *Pipeline) Forget(id ID) {
	p.failMu.Lock()
	delete(p.fails, id)
	p.failMu.Unlock()
}

// Invalidate drops every cached thumbnail of one picture from memory and
// forgets its revision hint, so the next request revalidates.
//
// The disk entries stay. They are keyed by revision, so a new revision writes a
// new entry and the old one is evicted by the budget in its own time; deleting
// it here would throw away a perfectly good entry for a revision that may come
// back.
func (p *Pipeline) Invalidate(id ID) {
	p.hintMu.Lock()
	delete(p.hints, id)
	p.hintMu.Unlock()
	p.pix.dropByID(id)
}

// Lookup answers "do I already have this, right now" without scheduling
// anything.
//
// It is the frame path's question: a gallery calls it once per visible tile
// while painting and requests only what is missing. It never blocks, never
// performs I/O and never logs. A hit returns a retained thumbnail; the caller
// releases it.
func (p *Pipeline) Lookup(id ID, size int) (*Thumbnail, bool) {
	return p.pix.lookup(id, p.rungFor(id, size))
}

// Request schedules work and returns a ticket that can cancel it.
//
// # Deduplication
//
// Requests are deduplicated by picture and ladder rung. N requests for one
// source produce one fetch and one decode, and every requester gets its own
// [Result] with its own generation. A request that arrives while an identical
// one is running joins it rather than starting a second.
//
// # Priority
//
// A [Visible] request overtakes every waiting [Prefetch], and a prefetch that
// is already queued when the same picture becomes visible is *promoted* rather
// than duplicated.
//
// # Saturation
//
// At [Config.QueueLimit] waiting requests a new prefetch is refused
// immediately with [ErrQueueFull], and a new visible request evicts the oldest
// waiting prefetch, which is refused the same way. A visible request is only
// refused when there is no prefetch left to evict, that is when the queue is
// full of visible work. Nothing grows without bound and nothing is silently
// dropped.
func (p *Pipeline) Request(req Request) Ticket {
	if req.Source == nil {
		panic("gift/asset: Pipeline.Request with a nil Source")
	}
	if req.OnResult == nil {
		panic("gift/asset: Pipeline.Request without an OnResult callback")
	}
	p.counters.requests.Add(1)
	meta := req.Source.Metadata()
	id := meta.ID
	if id == "" {
		r := errResult(fmt.Errorf("gift/asset: %w: source has an empty ID", ErrNotAPicture), Metadata{})
		r.Generation = req.Generation
		p.deliver(req, r)
		return Ticket{}
	}
	if p.closed.Load() {
		r := errResult(ErrClosed, Metadata{ID: id})
		r.ID, r.Generation = id, req.Generation
		p.deliver(req, r)
		return Ticket{}
	}
	rung := p.rungFor(id, req.Size)

	if err := p.backoffCheck(id, req.Source); err != nil {
		p.counters.backoff.Add(1)
		r := errResult(err, Metadata{ID: id})
		r.ID, r.Generation, r.Size = id, req.Generation, rung
		p.deliver(req, r)
		return Ticket{}
	}

	j, wid, evicted, joined, promoted, ok := p.sched.add(id, rung, req)
	for _, e := range evicted {
		p.counters.dropped.Add(1)
		p.finish(e, errResult(ErrQueueFull, Metadata{ID: e.id}), nil)
	}
	if !ok {
		p.counters.dropped.Add(1)
		r := errResult(ErrQueueFull, Metadata{ID: id})
		r.ID, r.Generation, r.Size = id, req.Generation, rung
		p.deliver(req, r)
		return Ticket{}
	}
	if joined {
		p.counters.deduped.Add(1)
	}
	if promoted {
		p.counters.promotions.Add(1)
	}
	return Ticket{p: p, j: j, id: wid}
}

func (p *Pipeline) cancel(j *job, wid uint64) {
	if p.sched.remove(j, wid) {
		p.counters.cancelled.Add(1)
	}
}

// rungFor maps a requested size to a ladder rung, with hysteresis against what
// is already in the CPU cache.
//
// The hysteresis is the point: a gallery whose columns are 243 pixels wide
// asks for 243, gets the 256 rung, and keeps getting it when the window is
// resized to 251. Without it every resize would invalidate every thumbnail,
// which is the "wenigen Pixelgroessen mit Hysterese" of the project plan,
// section 9.
func (p *Pipeline) rungFor(id ID, size int) int {
	sizes := p.cfg.Sizes
	if size <= 0 {
		size = sizes[0]
	}
	if best, ok := p.pix.hysteresisRung(id, size, p.cfg.Hysteresis); ok {
		return best
	}
	pick := sizes[0]
	for _, s := range sizes {
		if s > pick && pick < size {
			pick = s
		}
	}
	for _, s := range sizes {
		if s >= size && (pick < size || s < pick) {
			pick = s
		}
	}
	return pick
}

// backoffCheck refuses a request that must not be attempted yet.
//
// A quarantined source is let through when it can be probed, because a probe
// is a stat and the quarantine lifts on a revision change — which is exactly
// what the project plan, section 15, makes the condition. The worker checks
// again once it knows the revision; see [Pipeline.quarantineCheck]. For a
// source that cannot be probed there is nothing cheap to check, so the refusal
// happens here and [Pipeline.Forget] is the way out.
func (p *Pipeline) backoffCheck(id ID, src Source) error {
	p.failMu.Lock()
	defer p.failMu.Unlock()
	f, ok := p.fails[id]
	if !ok {
		return nil
	}
	if p.cfg.Now().Before(f.until) {
		return fmt.Errorf("%w: %v", ErrBackoff, f.last)
	}
	if f.quarantine {
		if _, canProbe := src.(Prober); canProbe {
			return nil
		}
		return fmt.Errorf("%w: %v", ErrQuarantined, f.last)
	}
	return nil
}

// quarantineCheck is the second half, run on the worker once the revision is
// known: a quarantined source whose content did not change is not loaded again.
func (p *Pipeline) quarantineCheck(id ID, rev string) error {
	p.failMu.Lock()
	defer p.failMu.Unlock()
	f, ok := p.fails[id]
	if !ok || !f.quarantine {
		return nil
	}
	if rev != "" && f.revision != rev {
		delete(p.fails, id)
		return nil
	}
	return fmt.Errorf("%w: %v", ErrQuarantined, f.last)
}

// noteFailure records a failure and computes the next retry moment.
func (p *Pipeline) noteFailure(id ID, revision string, err error) {
	p.failMu.Lock()
	defer p.failMu.Unlock()
	f, ok := p.fails[id]
	if !ok || f.revision != revision {
		f = &failure{revision: revision}
		p.fails[id] = f
	}
	f.attempts++
	f.last = err
	if !temporary(err) || f.attempts >= p.cfg.Backoff.Attempts {
		f.quarantine = true
		p.counters.quarantined.Add(1)
		return
	}
	d := p.cfg.Backoff.Base << (f.attempts - 1)
	if d > p.cfg.Backoff.Max || d <= 0 {
		d = p.cfg.Backoff.Max
	}
	f.until = p.cfg.Now().Add(d)
}

func (p *Pipeline) noteSuccess(id ID) {
	p.failMu.Lock()
	if len(p.fails) > 0 {
		delete(p.fails, id)
	}
	p.failMu.Unlock()
}

// clearFailureOnNewRevision lifts a quarantine when the source's content
// changed, which is the condition the project plan, section 15, names.
func (p *Pipeline) clearFailureOnNewRevision(id ID, rev string) {
	if rev == "" {
		return
	}
	p.failMu.Lock()
	if f, ok := p.fails[id]; ok && f.revision != rev {
		delete(p.fails, id)
	}
	p.failMu.Unlock()
}

// --- workers -----------------------------------------------------------------

func (p *Pipeline) worker() {
	defer p.wg.Done()
	for {
		j := p.sched.next()
		if j == nil {
			return
		}
		p.run(j)
	}
}

func (p *Pipeline) run(j *job) {
	ctx := p.ctx
	var cancel context.CancelFunc
	if p.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
	}
	res, t := p.produce(ctx, j)
	p.finish(j, res, t)
}

// finish delivers one result to every waiter and releases the pipeline's own
// reference to the thumbnail.
func (p *Pipeline) finish(j *job, res Result, t *Thumbnail) {
	ws := p.sched.done(j)
	res.ID = j.id
	res.Size = j.rung
	if !res.OK() {
		p.counters.failed.Add(1)
	} else {
		p.counters.completed.Add(1)
	}
	for _, w := range ws {
		r := res
		r.Generation = w.gen
		if t != nil {
			// One reference per delivery, released after the callback
			// has run. A consumer that wants to keep it retains it
			// again inside the callback.
			t.Retain()
			r.Image = t
		}
		p.deliverBounded(w.prio, w.fn, r, t)
	}
	if t != nil {
		t.Release() // the producing reference
	}
}

// deliverBounded enforces the ready queue bound of the project plan, section 9,
// point 5.
//
// A visible result waits for a slot. A prefetch result that finds the queue
// full is delivered *without* its image and with [Result.ImageWithheld] set:
// the thumbnail stays in the CPU cache, so [Pipeline.Lookup] finds it on the
// next frame, and the requester learns that the picture is there rather than
// waiting for a callback that never comes. Dropping the callback silently
// would be the one behaviour that cannot be debugged.
//
// What it must not do is stamp an error on a result that succeeded. That was
// the defect WU-R fixes: a withheld thumbnail arrived as [ErrQueueFull], every
// consumer read "this picture failed", and the picture it was about was
// already decoded and resident.
func (p *Pipeline) deliverBounded(prio Priority, fn func(Result), r Result, t *Thumbnail) {
	acquired := false
	select {
	case p.ready <- struct{}{}:
		acquired = true
	default:
		if prio == Visible {
			select {
			case p.ready <- struct{}{}:
				acquired = true
			case <-p.ctx.Done():
			}
		}
	}
	if !acquired {
		p.counters.readyDrops.Add(1)
		if t != nil {
			t.Release()
		}
		r.Image = nil
		if r.OK() {
			// A successful result whose slot was unavailable. Stamping
			// an error on it would be a lie: the thumbnail is in the
			// CPU cache and Lookup finds it.
			r.ImageWithheld = true
		}
		p.call(func() { fn(r) })
		return
	}
	p.call(func() {
		defer func() { <-p.ready }()
		fn(r)
		if r.Image != nil {
			r.Image.Release()
		}
	})
}

func (p *Pipeline) deliver(req Request, r Result) {
	fn := req.OnResult
	if fn == nil {
		return
	}
	p.call(func() { fn(r) })
}

func (p *Pipeline) call(fn func()) {
	if p.cfg.Deliver != nil {
		p.cfg.Deliver(fn)
		return
	}
	fn()
}

// produce is the whole of stages one to five for one job.
func (p *Pipeline) produce(ctx context.Context, j *job) (Result, *Thumbnail) {
	src := j.src
	meta := src.Metadata()
	ns := ""
	if n, ok := src.(Namespacer); ok {
		ns = n.CacheNamespace()
	}

	// Stage 1: establish a revision without transferring pixels where the
	// source allows it.
	rev, known := p.resolve(ctx, j, ns)
	if known.err != nil {
		p.noteFailure(j.id, rev, known.err)
		return errResult(known.err, meta), nil
	}
	p.clearFailureOnNewRevision(j.id, rev)
	if err := p.quarantineCheck(j.id, rev); err != nil {
		p.counters.backoff.Add(1)
		return errResult(err, meta), nil
	}

	if rev != "" && known.haveShape {
		key := Key{Namespace: ns, ID: j.id, Revision: rev, Size: j.rung,
			Orientation: known.orientation, ProcessingVersion: p.cfg.ProcessingVersion}
		if t, ok := p.pix.get(key); ok {
			p.counters.memHits.Add(1)
			return Result{
				Metadata:    Metadata{ID: j.id, Revision: rev, Width: uint32(known.w), Height: uint32(known.h), MIMEType: known.mime},
				Orientation: known.orientation, FromMemory: true,
			}, t
		}
		if t, ok := p.fromDisk(ctx, key); ok {
			p.counters.diskHits.Add(1)
			p.noteSuccess(j.id)
			return Result{
				Metadata:    Metadata{ID: j.id, Revision: rev, Width: uint32(known.w), Height: uint32(known.h), MIMEType: known.mime},
				Orientation: known.orientation, FromDisk: true,
			}, t
		}
	}

	// Stages 2 and 3: bounded transfer, bounded decode, orientation and
	// scaling.
	t, out, err := p.fetchDecode(ctx, j, ns, rev, known)
	if err != nil {
		p.noteFailure(j.id, rev, err)
		return errResult(err, meta), nil
	}
	p.noteSuccess(j.id)
	return out, t
}

// resolved is what stage one established.
type resolved struct {
	haveShape   bool
	w, h        int
	orientation Orientation
	mime        string
	skipNetwork bool
	err         error
}

func (p *Pipeline) resolve(ctx context.Context, j *job, ns string) (string, resolved) {
	var out resolved
	switch s := j.src.(type) {
	case Prober:
		pr, err := s.Probe(ctx)
		if err != nil {
			out.err = err
			return "", out
		}
		out.mime = pr.MIMEType
		j.encoded = pr.Size
		h, ok := p.hintOf(ns, j.id)
		if ok && h.Revision == pr.Revision && pr.Revision != "" {
			out.haveShape, out.w, out.h = true, h.W, h.H
			out.orientation = h.O
			if h.MIME != "" {
				out.mime = h.MIME
			}
		}
		return pr.Revision, out

	case Fetcher:
		h, ok := p.hintOf(ns, j.id)
		if ok && h.Revision != "" && p.cfg.Now().Before(h.Expires) {
			out.haveShape, out.w, out.h = true, h.W, h.H
			out.orientation, out.mime = h.O, h.MIME
			out.skipNetwork = true
			return h.Revision, out
		}
		if ok {
			// Stale but known: the conditional fetch below may come
			// back not-modified, in which case this shape is still
			// correct.
			out.w, out.h, out.orientation, out.mime = h.W, h.H, h.O, h.MIME
			j.ifRevision = h.Revision
		}
		return "", out

	default:
		return "", out
	}
}

func (p *Pipeline) hintOf(ns string, id ID) (hint, bool) {
	p.hintMu.Lock()
	h, ok := p.hints[id]
	p.hintMu.Unlock()
	if ok {
		return h, true
	}
	h, ok = p.disk.getHint(ns, id)
	if ok {
		p.hintMu.Lock()
		p.hints[id] = h
		p.hintMu.Unlock()
	}
	return h, ok
}

func (p *Pipeline) putHint(ns string, id ID, h hint) {
	p.hintMu.Lock()
	p.hints[id] = h
	p.hintMu.Unlock()
	p.disk.putHint(ns, id, h)
}

// fromDisk turns a disk entry into a live thumbnail, charging the pixel budget
// for it and putting it in the CPU cache.
func (p *Pipeline) fromDisk(ctx context.Context, key Key) (*Thumbnail, bool) {
	w, h, pix, ok := p.disk.get(key)
	if !ok {
		return nil, false
	}
	n := int64(len(pix))
	if err := p.reservePixels(ctx, n); err != nil {
		return nil, false
	}
	t := &Thumbnail{pix: pix, w: w, h: h, stride: w * 4, bytes: n,
		orientation: key.Orientation, ladder: key.Size, revision: key.Revision,
		owner: p.pixels}
	t.refs.Store(1)
	p.pix.put(key, t)
	return t, true
}

// reservePixels acquires from the pixel budget, evicting the CPU cache first
// when it is in the way.
//
// Eviction before blocking is what makes the budget a budget rather than a
// deadlock: the cache is discardable memory and the request is not, so the
// cache gives way.
func (p *Pipeline) reservePixels(ctx context.Context, n int64) error {
	if n > p.cfg.PixelBudget {
		return ErrTooLarge
	}
	for {
		if p.pixels.tryAcquire(n) {
			return nil
		}
		p.pix.evictFor(p.pixels, n)
		if p.pixels.tryAcquire(n) {
			return nil
		}
		// Wait for *any* release and then evict again. Evicting once and
		// then blocking inside acquire is what made this stall: by the
		// time bytes came back the cache had refilled behind the waiter,
		// and the waiter had already had its one look at it.
		if err := p.pixels.waitRelease(ctx); err != nil {
			return err
		}
	}
}

// fetchDecode is stages two and three.
func (p *Pipeline) fetchDecode(ctx context.Context, j *job, ns, rev string, known resolved) (*Thumbnail, Result, error) {
	if known.skipNetwork {
		// The hint was fresh and both caches missed, so the thumbnail has
		// to be produced from the bytes after all.
		known.skipNetwork = false
	}

	// Reserve the encoded bytes against [Config.InputBudget].
	//
	// A [Prober] told us the size in stage one, so its reservation is exact.
	// A [Fetcher] did not, and reserving [Config.MaxEncodedBytes] on the way
	// in — which is what this did before WU-R — reserves 64 MiB of a 96 MiB
	// budget for a 90 KiB JPEG and makes HTTP single threaded whatever
	// [Config.Workers] says. It is measurable from outside: the example
	// gallery reported an input peak of 64 MiB plus one file.
	//
	// So an unknown size reserves a token, and the reservation is corrected
	// once the response headers have arrived and Content-Length is known.
	// The correction *releases before it acquires*. Growing a held
	// reservation would be hold-and-wait, and N workers each holding part of
	// the budget and waiting for the rest is a deadlock that no amount of
	// budget makes impossible.
	held := int64(0)
	defer func() { p.input.release(held) }()
	reserve := func(n int64) error {
		if n <= 0 || n > p.cfg.MaxEncodedBytes {
			n = p.cfg.MaxEncodedBytes
		}
		if n == held {
			return nil
		}
		p.input.release(held)
		held = 0
		if err := p.input.acquire(ctx, n); err != nil {
			return err
		}
		held = n
		return nil
	}
	if j.encoded > p.cfg.MaxEncodedBytes {
		return nil, Result{}, fmt.Errorf("%w: %d encoded bytes exceed the limit of %d",
			ErrTooLarge, j.encoded, p.cfg.MaxEncodedBytes)
	}
	first := j.encoded
	if first <= 0 {
		first = min(inputProbeBytes, p.cfg.MaxEncodedBytes)
	}
	if err := reserve(first); err != nil {
		return nil, Result{}, err
	}

	var (
		body io.ReadCloser
		err  error
	)
	switch s := j.src.(type) {
	case Fetcher:
		var fr FetchResult
		fr, err = s.Fetch(ctx, j.ifRevision)
		if err != nil {
			return nil, Result{}, err
		}
		if fr.NotModified {
			p.counters.notModified.Add(1)
			rev = j.ifRevision
			exp := p.cfg.Now().Add(fr.Fresh)
			p.putHint(ns, j.id, hint{Revision: rev, Expires: exp,
				W: known.w, H: known.h, O: known.orientation, MIME: known.mime})
			key := Key{Namespace: ns, ID: j.id, Revision: rev, Size: j.rung,
				Orientation: known.orientation, ProcessingVersion: p.cfg.ProcessingVersion}
			if t, ok := p.pix.get(key); ok {
				return t, Result{FromMemory: true, Orientation: known.orientation,
					Metadata: Metadata{ID: j.id, Revision: rev, Width: uint32(known.w), Height: uint32(known.h), MIMEType: known.mime}}, nil
			}
			if t, ok := p.fromDisk(ctx, key); ok {
				p.counters.diskHits.Add(1)
				return t, Result{FromDisk: true, Orientation: known.orientation,
					Metadata: Metadata{ID: j.id, Revision: rev, Width: uint32(known.w), Height: uint32(known.h), MIMEType: known.mime}}, nil
			}
			// Nothing cached after all: fetch unconditionally once.
			j.ifRevision = ""
			fr, err = s.Fetch(ctx, "")
			if err != nil {
				return nil, Result{}, err
			}
			if fr.NotModified || fr.Body == nil {
				return nil, Result{}, fmt.Errorf("%w: source answered not-modified without a cached entry", ErrNotAPicture)
			}
		}
		body = fr.Body
		if fr.Revision != "" {
			rev = fr.Revision
		}
		if fr.MIMEType != "" {
			known.mime = fr.MIMEType
		}
		j.fresh = fr.Fresh
		if fr.Size > 0 {
			j.encoded = fr.Size
		}
	default:
		body, err = j.src.Open(ctx)
		if err != nil {
			return nil, Result{}, err
		}
	}
	defer body.Close()

	if j.encoded > p.cfg.MaxEncodedBytes {
		return nil, Result{}, fmt.Errorf("%w: %d encoded bytes exceed the limit of %d",
			ErrTooLarge, j.encoded, p.cfg.MaxEncodedBytes)
	}
	// Now that the length is known — or known to be unknown — correct the
	// reservation to the real one.
	if err := reserve(j.encoded); err != nil {
		return nil, Result{}, err
	}

	// Read the whole encoded picture, bounded. It is read into memory rather
	// than streamed so that the input budget accounts real bytes and so that
	// the header can be probed and then decoded without a second open; the
	// project plan, section 9, asks for "begrenzte komprimierte
	// Eingabedaten" and this is the form in which that is checkable.
	// The read is bounded by what was reserved, not by the configured
	// maximum, so that the budget tells the truth: a source that declares
	// 90 KiB and then sends 60 MiB would otherwise occupy memory nobody
	// accounted for. When the length was unknown the reservation is the
	// configured maximum and this is the old bound exactly.
	raw, err := readAllLimited(body, held)
	if err != nil {
		return nil, Result{}, err
	}

	info, err := probeHeader(raw, known.mime)
	if err != nil {
		return nil, Result{}, err
	}

	// The pixel limit is checked here, on the declared dimensions, before
	// any pixel is allocated. That is the whole point of it.
	if info.StoredW*info.StoredH > p.cfg.MaxPixels {
		return nil, Result{}, fmt.Errorf("%w: %dx%d is %d pixels, the limit is %d",
			ErrTooLarge, info.StoredW, info.StoredH,
			info.StoredW*info.StoredH, p.cfg.MaxPixels)
	}

	need := int64(float64(info.StoredW) * float64(info.StoredH) * info.Dec.MemoryFactor())
	if err := p.decode.acquire(ctx, need); err != nil {
		return nil, Result{}, err
	}
	img, derr := info.Dec.Decode(bytes.NewReader(raw))
	if derr != nil {
		p.decode.release(need)
		return nil, Result{}, fmt.Errorf("%w: %v", ErrNotAPicture, derr)
	}
	p.counters.decodes.Add(1)
	p.counters.decodedPixels.Add(uint64(info.StoredW * info.StoredH))

	t, terr := p.thumbnail(ctx, img, info, j.rung)
	p.decode.release(need)
	if terr != nil {
		return nil, Result{}, terr
	}

	key := Key{Namespace: ns, ID: j.id, Revision: rev, Size: j.rung,
		Orientation: info.Orientation, ProcessingVersion: p.cfg.ProcessingVersion}
	p.pix.put(key, t)
	if rev != "" {
		if err := p.disk.put(key, t); err != nil && p.cfg.Logger != nil {
			p.cfg.Logger.Debug("thumbnail not cached on disk",
				slog.String("key", key.String()), slog.String("err", err.Error()))
		}
		exp := p.cfg.Now().Add(j.fresh)
		p.putHint(ns, j.id, hint{Revision: rev, Expires: exp,
			W: info.W, H: info.H, O: info.Orientation, MIME: info.MIME})
	}

	return t, Result{
		Metadata: Metadata{ID: j.id, Revision: rev,
			Width: uint32(info.W), Height: uint32(info.H), MIMEType: info.MIME},
		Orientation: info.Orientation,
	}, nil
}

// thumbnail scales and orients one decoded picture.
//
// It scales *before* it rotates. Rotating a 24 megapixel picture costs 96 MB of
// destination and a cache miss per pixel; rotating its thumbnail costs a
// quarter of a megabyte. The target rectangle is computed in stored space by
// undoing the axis swap, so the result after rotation has exactly the requested
// longest edge.
func (p *Pipeline) thumbnail(ctx context.Context, img image.Image, info probeInfo, rung int) (*Thumbnail, error) {
	ow, oh := fitWithin(info.W, info.H, rung)
	// The scaled buffer in stored space.
	sw, sh := ow, oh
	if info.Orientation.SwapsAxes() {
		sw, sh = oh, ow
	}
	scaledBytes := int64(sw) * int64(sh) * 4
	if err := p.reservePixels(ctx, scaledBytes); err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
	p.cfg.Scaler.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	p.counters.scaledPixels.Add(uint64(sw * sh))

	final := dst
	finalBytes := scaledBytes
	if info.Orientation.Normalised() != OrientationTopLeft {
		rotBytes := int64(ow) * int64(oh) * 4
		if err := p.reservePixels(ctx, rotBytes); err != nil {
			p.pixels.release(scaledBytes)
			return nil, err
		}
		final = applyOrientation(dst, info.Orientation)
		p.pixels.release(scaledBytes)
		finalBytes = rotBytes
	}
	t := &Thumbnail{
		pix: final.Pix, w: final.Rect.Dx(), h: final.Rect.Dy(), stride: final.Stride,
		bytes: finalBytes, orientation: info.Orientation.Normalised(), ladder: rung,
		owner: p.pixels,
	}
	t.refs.Store(1)
	return t, nil
}

// fitWithin scales w by h down so that the longest edge is at most n, never
// up: enlarging a small picture wastes memory and bandwidth to add no
// information.
func fitWithin(w, h, n int) (int, int) {
	if w <= 0 || h <= 0 {
		return 1, 1
	}
	if w <= n && h <= n {
		return w, h
	}
	if w >= h {
		nh := int(float64(h)*float64(n)/float64(w) + 0.5)
		return n, max(1, nh)
	}
	nw := int(float64(w)*float64(n)/float64(h) + 0.5)
	return max(1, nw), n
}

// readAllLimited reads r into memory, refusing anything over limit.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	buf := make([]byte, 0, 64<<10)
	lr := io.LimitReader(r, limit+1)
	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := lr.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if int64(len(buf)) > limit {
			return nil, fmt.Errorf("%w: encoded picture exceeds %d bytes", ErrTooLarge, limit)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, nil
			}
			return nil, err
		}
	}
}
