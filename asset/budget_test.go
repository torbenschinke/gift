package asset_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/torbenschinke/gift/asset"
)

// TestPixelBudgetSaturates drives the pipeline past its pixel budget and
// asserts that it degrades the documented way rather than growing.
//
// This is the test the project plan, section 13, names first: "Bytebudgets und
// Freigabe bei Fehler, Abbruch, Queue-Saettigung und Shutdown."
func TestPixelBudgetSaturatesAndNeverExceedsItsLimit(t *testing.T) {
	dir := t.TempDir()
	const n = 40
	srcs := make([]asset.Source, n)
	for i := range n {
		p := writeFile(t, dir, fmt.Sprintf("p%02d.png", i), pngBytes(t, 300, 300))
		srcs[i] = asset.File(p)
	}

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 4
	cfg.Sizes = []int{256}
	// 256x256x4 = 262 144 bytes per thumbnail. Eight of them fit.
	cfg.PixelBudget = 8 * 256 * 256 * 4
	cfg.QueueLimit = 1000
	p := asset.NewPipeline(cfg)
	defer p.Close()

	for i := range n {
		p.Request(asset.Request{Source: srcs[i], Size: 256, Priority: asset.Visible,
			Generation: uint64(i), OnResult: c.onResult})
	}
	res := c.waitFor(t, n)

	st := p.Stats()
	if st.Pixels.Peak > st.Pixels.Limit {
		t.Fatalf("pixel budget peaked at %d over a limit of %d", st.Pixels.Peak, st.Pixels.Limit)
	}
	if st.Pixels.Peak < st.Pixels.Limit/2 {
		t.Errorf("pixel budget peaked at only %d of %d; the run did not saturate and proves nothing",
			st.Pixels.Peak, st.Pixels.Limit)
	}
	if st.Pixels.Waits == 0 && st.CacheEvictions == 0 {
		t.Error("neither a wait nor an eviction happened; the budget was never the binding constraint")
	}
	// Every request was answered, none was lost, and the cache holds at most
	// what the budget allows.
	ok := 0
	for _, r := range res {
		if r.Err == nil {
			ok++
		}
	}
	if ok == 0 {
		t.Fatal("no request succeeded under saturation")
	}
	if int64(st.CacheEntries)*256*256*4 > st.Pixels.Limit {
		t.Errorf("%d cache entries exceed the pixel budget", st.CacheEntries)
	}
	t.Logf("saturation: peak %d of %d bytes, %d waits, %d evictions, %d entries, %d/%d succeeded",
		st.Pixels.Peak, st.Pixels.Limit, st.Pixels.Waits, st.CacheEvictions,
		st.CacheEntries, ok, n)
}

// A decode is reserved by the picture's dimensions and the codec's risk, not by
// the size of the thumbnail that comes out. With a budget that fits exactly one
// decode, two workers cannot both be decoding.
func TestDecodeBudgetIsReservedByPictureSize(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.png", pngBytes(t, 512, 512))

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 4
	cfg.Sizes = []int{32}
	// A PNG reserves nine bytes per stored pixel: 512*512*9 = 2 359 296.
	// Room for one at a time.
	cfg.DecodeBudget = 512 * 512 * 9
	p := asset.NewPipeline(cfg)
	defer p.Close()

	const n = 8
	for i := range n {
		// Distinct files, so nothing deduplicates.
		q := writeFile(t, dir, fmt.Sprintf("c%d.png", i), pngBytes(t, 512, 512))
		_ = path
		p.Request(asset.Request{Source: asset.File(q), Size: 32,
			Priority: asset.Visible, Generation: uint64(i), OnResult: c.onResult})
	}
	c.waitFor(t, n)
	st := p.Stats()
	if st.Decode.Peak > st.Decode.Limit {
		t.Fatalf("decode budget peaked at %d over %d", st.Decode.Peak, st.Decode.Limit)
	}
	// The peak is the reservation for exactly one picture, which is the
	// assertion that matters: the formula used the stored 512 by 512 and the
	// PNG risk factor, not the 32 pixel thumbnail that came out. Whether a
	// worker actually had to block is a timing question and is not asserted.
	if want := int64(512 * 512 * 9); st.Decode.Peak != want {
		t.Errorf("decode peak = %d, want exactly one reservation of %d", st.Decode.Peak, want)
	}
	if st.Decode.InUse != 0 {
		t.Errorf("decode budget still holds %d bytes after every job finished", st.Decode.InUse)
	}
	t.Logf("decode budget: peak %d of %d, %d waits", st.Decode.Peak, st.Decode.Limit, st.Decode.Waits)
}

// Budgets come back on the error path too. A run of failures must not leak a
// single byte, or a gallery pointed at a directory of broken files would
// deadlock after a few hundred entries.
func TestBudgetsAreReleasedOnError(t *testing.T) {
	dir := t.TempDir()
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 2
	p := asset.NewPipeline(cfg)
	defer p.Close()

	const n = 30
	for i := range n {
		var data []byte
		switch i % 3 {
		case 0:
			data = []byte("not a picture")
		case 1:
			good := jpegBytes(t, 64, 64)
			data = good[:len(good)/2]
		case 2:
			data = nil
		}
		q := writeFile(t, dir, fmt.Sprintf("b%02d.jpg", i), data)
		p.Request(asset.Request{Source: asset.File(q), Size: 64,
			Priority: asset.Visible, Generation: uint64(i), OnResult: c.onResult})
	}
	res := c.waitFor(t, n)
	for _, r := range res {
		if r.Err == nil {
			t.Fatalf("%s decoded successfully", r.ID)
		}
	}
	st := p.Stats()
	if st.Input.InUse != 0 || st.Decode.InUse != 0 || st.Pixels.InUse != 0 {
		t.Errorf("after %d failures the budgets hold input=%d decode=%d pixels=%d, want all zero",
			n, st.Input.InUse, st.Decode.InUse, st.Pixels.InUse)
	}
}

// The delivered thumbnail is released when the callback returns, so the pixel
// budget drains back to what the cache holds.
func TestDeliveredThumbnailsAreReleasedAfterTheCallback(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.png", pngBytes(t, 128, 128))
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	p := asset.NewPipeline(cfg)
	defer p.Close()

	var kept *asset.Thumbnail
	p.Request(asset.Request{Source: asset.File(path), Size: 64, Priority: asset.Visible,
		OnResult: func(r asset.Result) {
			if r.Err != nil {
				t.Errorf("unexpected error %v", r.Err)
				return
			}
			// This is what a GPU uploader does: retain inside the
			// callback, release when the upload is done.
			r.Image.Retain()
			kept = r.Image
		}})
	waitUntil(t, func() bool { c.drain(); return kept != nil })

	before := p.Stats().Pixels.InUse
	if before == 0 {
		t.Fatal("a retained thumbnail is not accounted")
	}
	p.Invalidate(asset.File(path).Metadata().ID)
	if mid := p.Stats().Pixels.InUse; mid != before {
		t.Errorf("dropping the cache entry freed bytes a consumer still holds: %d -> %d", before, mid)
	}
	kept.Release()
	if after := p.Stats().Pixels.InUse; after != 0 {
		t.Errorf("after the last release the budget holds %d bytes, want 0", after)
	}
	if kept.Pix() != nil {
		t.Error("a released thumbnail still hands out pixels")
	}
}

// --- queue saturation --------------------------------------------------------

func TestQueueSaturationRefusesPrefetchAndEvictsForVisible(t *testing.T) {
	held := newBlocking("held", jpegBytes(t, 64, 64))
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1
	cfg.QueueLimit = 3
	p := asset.NewPipeline(cfg)
	defer p.Close()

	p.Request(asset.Request{Source: held, Size: 64, Priority: asset.Visible,
		Generation: 0, OnResult: c.onResult})
	waitUntil(t, func() bool { return held.openCount() == 1 })

	mk := func(name string) *blockingSource {
		s := newBlocking(name, jpegBytes(t, 64, 64))
		s.gate = nil
		return s
	}
	// Fill the queue with prefetches.
	for i := range 3 {
		p.Request(asset.Request{Source: mk(fmt.Sprintf("pre%d", i)), Size: 64,
			Priority: asset.Prefetch, Generation: uint64(10 + i), OnResult: c.onResult})
	}
	// One more prefetch is refused outright.
	p.Request(asset.Request{Source: mk("overflow"), Size: 64,
		Priority: asset.Prefetch, Generation: 99, OnResult: c.onResult})
	c.drain()
	c.resMu.Lock()
	var refused []asset.Result
	for _, r := range c.results {
		if errors.Is(r.Err, asset.ErrQueueFull) {
			refused = append(refused, r)
		}
	}
	c.resMu.Unlock()
	if len(refused) != 1 || refused[0].Generation != 99 {
		t.Fatalf("expected exactly the overflowing prefetch to be refused, got %+v", refused)
	}

	// A visible request evicts the oldest waiting prefetch instead of being
	// refused itself.
	p.Request(asset.Request{Source: mk("urgent"), Size: 64,
		Priority: asset.Visible, Generation: 1000, OnResult: c.onResult})
	held.release()
	res := c.waitFor(t, 6)

	var evicted, served bool
	for _, r := range res {
		if r.Generation == 10 && errors.Is(r.Err, asset.ErrQueueFull) {
			evicted = true
		}
		if r.Generation == 1000 && r.Err == nil {
			served = true
		}
	}
	if !evicted {
		t.Error("the oldest prefetch was not evicted for the visible request")
	}
	if !served {
		t.Error("the visible request was not served")
	}
	if st := p.Stats(); st.Dropped != 2 {
		t.Errorf("Dropped = %d, want 2 (one refused, one evicted)", st.Dropped)
	}
}

// --- shutdown ----------------------------------------------------------------

func TestShutdownAnswersEveryQueuedRequestAndReleasesEverything(t *testing.T) {
	held := newBlocking("held", jpegBytes(t, 64, 64))
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 1
	p := asset.NewPipeline(cfg)

	p.Request(asset.Request{Source: held, Size: 64, Priority: asset.Visible,
		Generation: 0, OnResult: c.onResult})
	waitUntil(t, func() bool { return held.openCount() == 1 })

	const n = 5
	for i := range n {
		s := newBlocking(fmt.Sprintf("q%d", i), jpegBytes(t, 64, 64))
		s.gate = nil
		p.Request(asset.Request{Source: s, Size: 64, Priority: asset.Prefetch,
			Generation: uint64(100 + i), OnResult: c.onResult})
	}

	done := make(chan struct{})
	go func() { defer close(done); p.Close() }()
	// Closing cancels the context the blocked Open is waiting on.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}

	res := c.waitFor(t, n+1)
	closed := 0
	for _, r := range res {
		if errors.Is(r.Err, asset.ErrClosed) {
			closed++
		}
	}
	if closed < n {
		t.Errorf("%d of %d queued requests were told the pipeline closed", closed, n)
	}
	st := p.Stats()
	if st.Pixels.InUse != 0 || st.Input.InUse != 0 || st.Decode.InUse != 0 {
		t.Errorf("after shutdown the budgets hold input=%d decode=%d pixels=%d",
			st.Input.InUse, st.Decode.InUse, st.Pixels.InUse)
	}
	if st.CacheEntries != 0 {
		t.Errorf("after shutdown the CPU cache holds %d entries", st.CacheEntries)
	}
	// Close is idempotent, and a request afterwards is answered rather than
	// swallowed.
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	c.reset()
	s := newBlocking("late", jpegBytes(t, 8, 8))
	s.gate = nil
	p.Request(asset.Request{Source: s, Size: 64, Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; !errors.Is(r.Err, asset.ErrClosed) {
		t.Errorf("a request after Close got %v, want ErrClosed", r.Err)
	}
}

// --- the race test -----------------------------------------------------------

// TestConcurrentLoad is the one the project plan, section 13, requires to be
// run under -race: "Race-Tests fuer CPU-Pipeline". It exercises every path at
// once — hits, misses, failures, cancellations, promotions, saturation and
// shutdown — from many goroutines.
func TestConcurrentLoad(t *testing.T) {
	dir := t.TempDir()
	const files = 24
	srcs := make([]asset.Source, files)
	for i := range files {
		var data []byte
		switch i % 4 {
		case 0:
			data = jpegBytes(t, 200, 120)
		case 1:
			data = pngBytes(t, 120, 200)
		case 2:
			data = withEXIFOrientation(t, jpegBytes(t, 160, 80), asset.OrientationRightTop)
		case 3:
			data = []byte("broken")
		}
		srcs[i] = asset.File(writeFile(t, dir, fmt.Sprintf("m%02d", i), data))
	}

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Workers = 4
	cfg.Sizes = []int{64, 128}
	cfg.PixelBudget = 2 << 20
	cfg.QueueLimit = 32
	cfg.ReadyLimit = 8
	cfg.Disk = asset.DiskCacheConfig{Dir: t.TempDir(), Budget: 1 << 20}
	p := asset.NewPipeline(cfg)

	// A drainer plays the UI executor.
	stop := make(chan struct{})
	var drained sync.WaitGroup
	drained.Add(1)
	go func() {
		defer drained.Done()
		for {
			select {
			case <-stop:
				c.drain()
				return
			default:
				if c.drain() == 0 {
					time.Sleep(time.Millisecond)
				}
			}
		}
	}()

	var wg sync.WaitGroup
	const goroutines = 8
	const each = 60
	var kept sync.Map
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range each {
				idx := (g*7 + i*3) % files
				size := 64
				if i%2 == 0 {
					size = 128
				}
				prio := asset.Prefetch
				if i%3 == 0 {
					prio = asset.Visible
				}
				tk := p.Request(asset.Request{
					Source: srcs[idx], Size: size, Priority: prio,
					Generation: uint64(g*1000 + i),
					OnResult: func(r asset.Result) {
						if r.Err == nil && r.Image != nil {
							// Hold some of them past the callback,
							// which is what the GPU side will do.
							if r.Generation%17 == 0 {
								r.Image.Retain()
								kept.Store(r.Generation, r.Image)
							}
							_ = r.Image.Pix()
						}
					},
				})
				if i%11 == 0 {
					tk.Cancel()
				}
				// Also exercise the frame path question.
				if th, ok := p.Lookup(srcs[idx].Metadata().ID, size); ok {
					_ = th.Width()
					th.Release()
				}
			}
		}(g)
	}
	wg.Wait()
	// Close first, with the executor still draining: every closure handed to
	// Deliver has to run, because that is where the delivery reference is
	// released. An executor that stops early leaks exactly those references,
	// which is the contract documented on Config.Deliver.
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	drained.Wait()
	c.drain()

	kept.Range(func(k, v any) bool {
		v.(*asset.Thumbnail).Release()
		return true
	})
	st := p.Stats()
	if st.Pixels.Peak > st.Pixels.Limit {
		t.Errorf("pixel budget peaked at %d over %d", st.Pixels.Peak, st.Pixels.Limit)
	}
	if st.Pixels.InUse != 0 {
		t.Errorf("after shutdown and every release the pixel budget holds %d bytes", st.Pixels.InUse)
	}
	t.Logf("load: %+v", struct {
		Requests, Dedup, Decodes, MemHits, DiskHits, Failed, Dropped, Cancelled uint64
		PeakPixels                                                              int64
	}{st.Requests, st.Deduplicated, st.Decodes, st.MemoryHits, st.DiskHits,
		st.Failed, st.Dropped, st.Cancelled, st.Pixels.Peak})
}
