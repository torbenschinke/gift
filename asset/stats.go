package asset

import "runtime"

func runtimeProcs() int { return runtime.GOMAXPROCS(0) }

// Stats is a snapshot of the pipeline's counters.
//
// It is the "Zaehler statt Logzeilen" of the project plan, section 15: the
// pipeline writes numbers and never log lines on a path that can run during a
// frame, and the application reads them out of band, at most once a second,
// where the one [slog.Record] of a report is affordable.
//
// The consumer converts it into whatever its metrics package wants. This
// package does not hand it to gift/metrics itself, and the direction of the
// rule is worth stating correctly, since both copies of this comment had it
// upside down until WU-R: gift/metrics imports nothing from this repository at
// all. It declares plain counter structs and the *application* fills them in,
// which is what keeps a measurement package from depending on the thing it
// measures. See [metrics.AssetStats] and ui.ImagePipelineStats, which is the
// conversion.
type Stats struct {
	// Requests is every call to [Pipeline.Request] that named a source.
	Requests uint64
	// Deduplicated is the number of requests that joined work already in
	// flight instead of starting their own. This is the number the "N
	// requests, one fetch" property is asserted on.
	Deduplicated uint64
	// Promotions is the number of queued prefetches that were raised to
	// visible because the user scrolled to them.
	Promotions uint64
	// Dropped is the number of requests refused by queue saturation, and
	// Cancelled the number withdrawn by their requester.
	Dropped, Cancelled uint64
	// ReadyDropped is the number of finished results that were delivered
	// without their image because the ready queue was full; see
	// [Config.ReadyLimit].
	ReadyDropped uint64
	// Completed and Failed are the delivered outcomes.
	Completed, Failed uint64
	// BackoffRefused is the number of requests answered with [ErrBackoff]
	// without any work being done, and Quarantined the number of sources put
	// out of service until their revision changes.
	BackoffRefused, Quarantined uint64
	// Decodes is the number of pictures actually decoded, MemoryHits and
	// DiskHits the ones that did not have to be. NotModified is the number
	// of conditional fetches a server answered with 304.
	Decodes, MemoryHits, DiskHits, NotModified uint64
	// DecodedPixels and ScaledPixels are the work volumes, which is the
	// honest measure: a decode count says nothing about how big the pictures
	// were.
	DecodedPixels, ScaledPixels uint64
	// Input, Decode and Pixels are the three byte budgets.
	Input, Decode, Pixels BudgetStats
	// CacheEntries is the number of thumbnails in the CPU cache and
	// CacheHits, CacheMisses, CacheEvictions its outcomes.
	CacheEntries                            int
	CacheHits, CacheMisses, CacheEvictions  uint64
	QueuedVisible, QueuedPrefetch, InFlight int
	// Disk is the persistent cache; see [DiskCacheStats].
	Disk DiskCacheStats
}

// Stats returns a snapshot of the counters. It is safe to call from any
// goroutine and does not block the workers.
func (p *Pipeline) Stats() Stats {
	vis, pre, infl := p.sched.stats()
	return Stats{
		Requests:       p.counters.requests.Load(),
		Deduplicated:   p.counters.deduped.Load(),
		Promotions:     p.counters.promotions.Load(),
		Dropped:        p.counters.dropped.Load(),
		Cancelled:      p.counters.cancelled.Load(),
		ReadyDropped:   p.counters.readyDrops.Load(),
		Completed:      p.counters.completed.Load(),
		Failed:         p.counters.failed.Load(),
		BackoffRefused: p.counters.backoff.Load(),
		Quarantined:    p.counters.quarantined.Load(),
		Decodes:        p.counters.decodes.Load(),
		MemoryHits:     p.counters.memHits.Load(),
		DiskHits:       p.counters.diskHits.Load(),
		NotModified:    p.counters.notModified.Load(),
		DecodedPixels:  p.counters.decodedPixels.Load(),
		ScaledPixels:   p.counters.scaledPixels.Load(),
		Input:          p.input.snapshot(),
		Decode:         p.decode.snapshot(),
		Pixels:         p.pixels.snapshot(),
		CacheEntries:   p.pix.len(),
		CacheHits:      p.pix.hits.Load(),
		CacheMisses:    p.pix.misses.Load(),
		CacheEvictions: p.pix.evictions.Load(),
		QueuedVisible:  vis,
		QueuedPrefetch: pre,
		InFlight:       infl,
		Disk:           p.disk.snapshot(),
	}
}
