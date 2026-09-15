package ebiten

import (
	"math"

	eb "github.com/hajimehoshi/ebiten/v2"
)

// TargetConfig configures a [TargetPool]. The zero value selects the defaults
// below and is what [NewRenderer] uses.
type TargetConfig struct {
	// MaxBytes is the residency budget across all pooled targets, in logical
	// pixel bytes. Zero selects [DefaultTargetBytes].
	//
	// "Logical" carries the same warning as [TextureConfig.MaxBytes]: a
	// 1920x1088 target is accounted at 8 MiB here and occupies more than that
	// on the device, because Ebitengine pads and because a render target is
	// not necessarily on an atlas.
	MaxBytes int64

	// MaxAge is the number of drawn frames a target may go unused before
	// [TargetPool.Tick] deallocates it. Zero selects [DefaultTargetMaxAge];
	// a negative value disables age eviction.
	//
	// It is much shorter than the texture cache's ten seconds. A thumbnail is
	// worth keeping because re-acquiring it means decoding a JPEG; a render
	// target is worth keeping only because allocating one costs a driver
	// round trip, and holding eight megabytes of it for ten seconds after the
	// glass panel closed is the wrong trade on a Raspberry Pi where the CPU
	// and the GPU share physical memory.
	MaxAge int

	// Bucket is the granularity targets are rounded up to, in pixels. Zero
	// selects [DefaultTargetBucket]; it is forced to a power of two.
	//
	// Without it a window resize of one pixel would allocate a new full
	// screen target every frame of the drag.
	Bucket int
}

// Defaults for [TargetConfig].
const (
	// DefaultTargetBytes is 48 MiB of logical pixel bytes.
	//
	// The shape of the budget, not a measurement: a 1920x1080 scene target is
	// 8 MiB, a full width glass panel of 1920x200 buckets to 1920x256 and is
	// 2 MiB, and its four blur levels together are another 0.7 MiB. That is
	// under 11 MiB for the scenario of section 8, so the budget holds roughly
	// four such scenarios before it starts evicting. A number small enough to
	// notice a leak and large enough that an ordinary application never
	// reaches it.
	DefaultTargetBytes int64 = 48 << 20

	// DefaultTargetMaxAge is 120 drawn frames, two seconds at sixty hertz.
	DefaultTargetMaxAge = 120

	// DefaultTargetBucket is 64 pixels.
	DefaultTargetBucket = 64
)

// targetRec is one slot of the target pool. Plain data in a flat slice, like
// [texRecord].
type targetRec struct {
	img   *eb.Image
	w, h  int
	bytes int64
	// leased is true between [TargetPool.Acquire] and [TargetPool.Release].
	// A leased target is never handed out again and never evicted.
	leased bool
	// used is the drawn frame number this target was last leased in.
	used uint64
	live bool
}

// TargetPool owns the intermediate render targets of the glass material.
//
// These are the first [eb.Image]s gift allocates that are neither a glyph
// atlas page nor a picture, and the project plan, section 11, is suspicious of
// them for good reason: "Keine Vollbild-Textur pro Widget". So the rules are
// narrow and are enforced here rather than promised in a comment.
//
// # There is no target per widget
//
// A target is leased for the duration of one material pass and returned
// immediately afterwards. Two glass panels in one frame use the *same* five
// targets, one after the other, as long as their regions fall into the same
// size bucket — and a bucket is 64 pixels, so two panels of the same width
// normally do. A hundred glass panels would still hold at most five targets at
// a time, at the cost of a hundred pass chains, which is a fill rate problem
// and not a memory one.
//
// The scene target is the one exception and it is one per *window*, not one
// per widget; see [Renderer] and the package documentation for why a screen
// sized target is unavoidable on Ebitengine.
//
// # Bounded
//
// By [TargetConfig.MaxBytes] across the pool, checked before every allocation,
// with least recently used eviction. A lease that cannot be satisfied returns
// nil, and the caller degrades the material rather than allocating anyway.
// [TargetStats.Rejected] is how that becomes visible.
//
// # Released explicitly
//
// By [eb.Image.Deallocate], on eviction and on [TargetPool.Close], exactly
// like [TextureCache]. Not by dropping the reference and waiting for a
// cleanup function.
//
// A TargetPool belongs to the UI executor and is not safe for concurrent use.
type TargetPool struct {
	cfg  TargetConfig
	recs []targetRec
	free []uint32

	bytes int64
	frame uint64
	stats TargetStats

	// newImage and onDeallocate exist for the same reason they do on
	// [TextureCache]: the admission, reuse and eviction logic is testable
	// without a graphics context, and "it is released explicitly" is an
	// assertion rather than a claim.
	newImage     func(w, h int) *eb.Image
	onDeallocate func(*eb.Image)
}

// NewTargetPool returns an empty pool. It allocates no GPU memory until the
// first [TargetPool.Acquire].
func NewTargetPool(cfg TargetConfig) *TargetPool {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultTargetBytes
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = DefaultTargetMaxAge
	}
	if cfg.Bucket <= 0 {
		cfg.Bucket = DefaultTargetBucket
	}
	// A power of two, so that rounding up is a mask and so that the number of
	// distinct buckets stays small.
	b := 1
	for b < cfg.Bucket {
		b <<= 1
	}
	cfg.Bucket = b
	return &TargetPool{cfg: cfg}
}

// bucket rounds an extent up to the configured granularity, with a floor of
// one bucket.
func (p *TargetPool) bucket(v int) int {
	if v < 1 {
		v = 1
	}
	m := p.cfg.Bucket
	return (v + m - 1) &^ (m - 1)
}

// Acquire leases a target at least w by h pixels, reusing a pooled one when
// the bucketed size matches.
//
// The returned image is *larger* than asked for whenever the request is not a
// multiple of the bucket, and its surplus holds whatever the previous tenant
// left there. Every shader that samples a pooled target therefore clamps to
// the valid area; see kawase_down.kage.
//
// It returns nil when the budget cannot accommodate the target. The caller
// must degrade rather than allocate its own.
func (p *TargetPool) Acquire(w, h int) *eb.Image {
	if w <= 0 || h <= 0 {
		p.stats.Malformed++
		return nil
	}
	bw, bh := p.bucket(w), p.bucket(h)
	for i := range p.recs {
		r := &p.recs[i]
		if r.live && !r.leased && r.w == bw && r.h == bh {
			r.leased = true
			r.used = p.frame
			p.stats.Reuses++
			p.stats.Leases++
			return r.img
		}
	}
	need := int64(bw) * int64(bh) * 4
	if !p.makeRoom(need) {
		p.stats.Rejected++
		return nil
	}
	i := p.alloc()
	r := &p.recs[i]
	r.img = p.newTarget(bw, bh)
	if r.img == nil {
		// Headless: the caller gets nil and degrades, which is what a test
		// without a graphics context observes.
		*r = targetRec{}
		p.free = append(p.free, i)
		p.stats.Rejected++
		return nil
	}
	r.w, r.h, r.bytes = bw, bh, need
	r.live, r.leased, r.used = true, true, p.frame
	p.bytes += need
	p.stats.Allocations++
	p.stats.Leases++
	if n := p.count(); n > p.stats.PeakTargets {
		p.stats.PeakTargets = n
	}
	if p.bytes > p.stats.PeakBytes {
		p.stats.PeakBytes = p.bytes
	}
	return r.img
}

// Release returns a leased target to the pool. Releasing something that is not
// leased is a no-op, so a caller may release unconditionally after a pass
// chain that may have failed half way through.
func (p *TargetPool) Release(img *eb.Image) {
	if img == nil {
		return
	}
	for i := range p.recs {
		r := &p.recs[i]
		if r.live && r.leased && r.img == img {
			r.leased = false
			r.used = p.frame
			return
		}
	}
}

// Leased reports how many targets are currently leased. It is zero between
// frames in a correct implementation, and a test asserts exactly that.
func (p *TargetPool) Leased() int {
	n := 0
	for i := range p.recs {
		if p.recs[i].live && p.recs[i].leased {
			n++
		}
	}
	return n
}

// Tick advances the frame clock and deallocates targets that have gone unused
// for longer than [TargetConfig.MaxAge]. The renderer calls it once per drawn
// frame from [Renderer.EndFrame], after the last draw call.
func (p *TargetPool) Tick() {
	p.frame++
	if p.cfg.MaxAge < 0 {
		return
	}
	age := uint64(p.cfg.MaxAge)
	if p.frame <= age {
		return
	}
	deadline := p.frame - age
	for i := range p.recs {
		r := &p.recs[i]
		if r.live && !r.leased && r.used < deadline {
			p.deallocate(uint32(i))
			p.stats.AgeEvictions++
		}
	}
}

// Close deallocates every target the pool holds, leased or not. It is for
// shutdown and for a test that wants to prove the release path runs.
//
// A still-leased target is deallocated too, and counted in
// [TargetStats.ClosedWhileLeased]. That counter is not decoration: after Close
// the holder of such a lease has a pointer to a deallocated image, and its
// [TargetPool.Release] is a silent no-op because the record is gone. Closing
// mid-frame is a caller error, and [Renderer.EndFrame] panics for the same
// condition, but Close cannot panic — it is the shutdown path and is called
// from a defer — so it counts instead of pretending the pool was empty.
func (p *TargetPool) Close() {
	for i := range p.recs {
		if !p.recs[i].live {
			continue
		}
		if p.recs[i].leased {
			p.stats.ClosedWhileLeased++
		}
		p.deallocate(uint32(i))
	}
}

func (p *TargetPool) makeRoom(need int64) bool {
	if need > p.cfg.MaxBytes {
		return false
	}
	for p.bytes+need > p.cfg.MaxBytes {
		if !p.evictLRU() {
			return false
		}
	}
	return true
}

func (p *TargetPool) evictLRU() bool {
	best, bestUsed := -1, uint64(math.MaxUint64)
	for i := range p.recs {
		r := &p.recs[i]
		if !r.live || r.leased {
			continue
		}
		if r.used < bestUsed {
			best, bestUsed = i, r.used
		}
	}
	if best < 0 {
		return false
	}
	p.deallocate(uint32(best))
	p.stats.Evictions++
	return true
}

func (p *TargetPool) deallocate(i uint32) {
	r := &p.recs[i]
	if !r.live {
		return
	}
	if r.img != nil {
		if p.onDeallocate != nil {
			p.onDeallocate(r.img)
		}
		r.img.Deallocate()
	}
	p.bytes -= r.bytes
	*r = targetRec{}
	p.free = append(p.free, i)
	p.stats.Deallocations++
}

func (p *TargetPool) alloc() uint32 {
	if n := len(p.free); n > 0 {
		i := p.free[n-1]
		p.free = p.free[:n-1]
		return i
	}
	p.recs = append(p.recs, targetRec{})
	return uint32(len(p.recs) - 1)
}

func (p *TargetPool) count() int { return len(p.recs) - len(p.free) }

func (p *TargetPool) newTarget(w, h int) *eb.Image {
	if p.newImage != nil {
		return p.newImage(w, h)
	}
	return eb.NewImage(w, h)
}

// TargetStats are the counters of the target pool. Plain numbers written in
// the frame path and read out of band, like every other counter in gift.
type TargetStats struct {
	// Leases is the number of successful [TargetPool.Acquire] calls and
	// Reuses the subset of them served from the pool without allocating.
	//
	// In a steady scene with glass on screen, Leases grows by five per glass
	// panel per frame at the Full level and by one at Reduced, and Reuses
	// grows by the same amount: a rising Allocations in a steady scene means
	// the bucketing is not catching a size that changes every frame.
	Leases, Reuses uint64
	// Allocations is the number of [eb.NewImage] calls and Deallocations the
	// number of [eb.Image.Deallocate] calls. In a scene that opens and closes
	// a glass panel repeatedly the two converge; a gap that only grows is a
	// leak.
	Allocations, Deallocations uint64
	// Evictions is the number of targets released under budget pressure and
	// AgeEvictions the subset released for going unused.
	Evictions, AgeEvictions uint64
	// Rejected is the number of leases the budget refused. A non zero value
	// means glass degraded on screen, which is the visible symptom, and the
	// project plan, section 13, forbids hiding it.
	Rejected uint64
	// Malformed counts leases asked for with a non positive extent.
	Malformed uint64
	// ClosedWhileLeased counts targets that [TargetPool.Close] deallocated
	// while they were still leased.
	//
	// It is a caller error and, unlike the same condition at
	// [Renderer.EndFrame], it cannot be a panic, because Close is the
	// shutdown path. A non zero value means somebody closed the pool in the
	// middle of a frame and is holding a pointer to a deallocated image whose
	// Release will do nothing.
	ClosedWhileLeased uint64
	// Targets and Bytes are the current residency, PeakTargets and PeakBytes
	// their high water marks. Bytes is logical pixel bytes.
	Targets     int
	Bytes       int64
	PeakTargets int
	PeakBytes   int64
}

// Stats returns a snapshot of the counters.
func (p *TargetPool) Stats() TargetStats {
	s := p.stats
	s.Targets = p.count()
	s.Bytes = p.bytes
	return s
}
