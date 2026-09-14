package asset

import (
	"image"
	"sync"
	"sync/atomic"
)

// Thumbnail is one decoded, oriented, scaled picture in main memory.
//
// # Pixel format
//
// Premultiplied RGBA, eight bits per channel, exactly [image.RGBA]. That is
// what the backend wants: the project plan, section 8, records that
// "Ebitengine erwartet premultipliziertes RGBA fuer Pixel-Uploads", so
// converting here means the GPU side can hand the slice on without touching
// it.
//
// # Ownership and lifetime
//
// A Thumbnail is reference counted, and its pixels are charged against the
// pipeline's pixel budget for as long as it lives. A consumer that keeps one
// beyond the call it arrived in must [Thumbnail.Retain] it and
// [Thumbnail.Release] it afterwards. A consumer that only reads it inside the
// delivery callback need do neither.
//
// This is the seam the GPU half of step 4 uses: an uploader retains the
// thumbnail, uploads the pixels, and releases it. Until it does, the bytes
// count against the budget, which is what stops a slow upload queue from
// turning into an unbounded pile of decoded pictures.
//
// After the last release the pixels are returned to the budget and the value
// must not be used again. Using one afterwards is a programming error;
// [Thumbnail.Pix] returns nil for it rather than handing out memory that has
// been accounted as free.
type Thumbnail struct {
	pix    []byte
	w, h   int
	stride int
	bytes  int64

	// orientation and size are carried for diagnostics and for the cache
	// key check, not for rendering: the pixels are already oriented.
	orientation Orientation
	ladder      int

	refs atomic.Int32
	// owner is the budget the bytes are charged against. It is nil for a
	// thumbnail that was never charged, which only happens in tests.
	owner *budget
}

// Bounds is the pixel rectangle, always anchored at the origin.
func (t *Thumbnail) Bounds() image.Rectangle { return image.Rect(0, 0, t.w, t.h) }

// Width and Height are the oriented pixel dimensions.
func (t *Thumbnail) Width() int  { return t.w }
func (t *Thumbnail) Height() int { return t.h }

// Stride is the distance between two rows in bytes.
func (t *Thumbnail) Stride() int { return t.stride }

// Bytes is the size of the pixel data, which is what the budget accounts.
func (t *Thumbnail) Bytes() int64 { return t.bytes }

// LadderSize is the rung of [Config.Sizes] this thumbnail was produced for.
func (t *Thumbnail) LadderSize() int { return t.ladder }

// Orientation is the EXIF orientation that was *applied*. The pixels are
// upright; this is here so that a cache entry can be checked against the key
// it was found under.
func (t *Thumbnail) Orientation() Orientation { return t.orientation }

// Pix is the premultiplied RGBA pixel data, or nil after the last release.
//
// The slice is owned by the thumbnail and must not be modified or retained
// beyond the matching [Thumbnail.Release].
func (t *Thumbnail) Pix() []byte {
	if t.refs.Load() <= 0 {
		return nil
	}
	return t.pix
}

// RGBA returns an [image.RGBA] view of the same pixels, without copying. It
// returns nil after the last release.
func (t *Thumbnail) RGBA() *image.RGBA {
	if t.refs.Load() <= 0 {
		return nil
	}
	return &image.RGBA{Pix: t.pix, Stride: t.stride, Rect: t.Bounds()}
}

// Retain adds a reference.
func (t *Thumbnail) Retain() {
	if t.refs.Add(1) <= 1 {
		// Resurrecting a released thumbnail would hand out pixels that the
		// budget has already been told are free. Undo and leave it dead.
		t.refs.Add(-1)
	}
}

// Release drops a reference and frees the pixels when it was the last one.
func (t *Thumbnail) Release() {
	n := t.refs.Add(-1)
	if n > 0 {
		return
	}
	if n < 0 {
		t.refs.Store(0)
		return
	}
	pix := t.pix
	t.pix = nil
	_ = pix
	if t.owner != nil {
		t.owner.release(t.bytes)
		t.owner = nil
	}
}

// --- the bounded CPU pixel cache ---------------------------------------------

// pixelCache is the bounded cache of decoded thumbnails required by the
// project plan, section 9, point 5.
//
// It is bounded by the *pixel budget* and not by a count of its own, and it
// deliberately does not own a second budget: every live thumbnail, cached or in
// flight, is charged against the one budget, so a single number bounds all
// decoded pixels in the process. A cached entry holds one reference; evicting
// it drops that reference, and the bytes come back when the last consumer has
// released it too.
//
// An entry that is still referenced by a consumer is evicted from the cache but
// not freed. That is the honest behaviour: the cache cannot take memory back
// from somebody who is using it, and pretending otherwise would be a use after
// free on the GPU upload path.
type pixelCache struct {
	mu      sync.Mutex
	entries map[Key]*pixelEntry
	// byID is the index the frame path looks through: a gallery asks
	// "do I have this picture at this rung" once per visible tile per frame,
	// and scanning every entry for that would be sixty tiles times the whole
	// cache, every frame.
	byID map[ID][]Key
	// lru is in least recently used order; index 0 is the next victim.
	lru []Key

	hits, misses, evictions atomic.Uint64
}

type pixelEntry struct {
	t *Thumbnail
}

func newPixelCache() *pixelCache {
	return &pixelCache{entries: make(map[Key]*pixelEntry), byID: make(map[ID][]Key)}
}

// get returns a retained thumbnail. The caller releases it.
func (c *pixelCache) get(k Key) (*Thumbnail, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	c.touch(k)
	e.t.Retain()
	c.hits.Add(1)
	return e.t, true
}

// put adds a thumbnail and takes the cache's own reference.
func (c *pixelCache) put(k Key, t *Thumbnail) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[k]; ok {
		old.t.Release()
	} else {
		c.byID[k.ID] = append(c.byID[k.ID], k)
	}
	c.removeLRU(k)
	t.Retain()
	c.entries[k] = &pixelEntry{t: t}
	c.lru = append(c.lru, k)
}

// lookup returns a retained thumbnail for the most recently used entry of one
// picture at one rung, whatever its revision. The caller releases it.
//
// Revision is not part of the question because the frame path does not know it:
// a tile knows which picture it shows and how big it is. An entry produced from
// an older revision is still a picture of the same thing and is better than a
// placeholder; the next completed request replaces it.
func (c *pixelCache) lookup(id ID, rung int) (*Thumbnail, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := c.byID[id]
	var best Key
	bestAt := -1
	for _, k := range keys {
		if k.Size != rung {
			continue
		}
		for i := len(c.lru) - 1; i > bestAt; i-- {
			if c.lru[i] == k {
				best, bestAt = k, i
				break
			}
		}
	}
	if bestAt < 0 {
		c.misses.Add(1)
		return nil, false
	}
	e := c.entries[best]
	e.t.Retain()
	c.hits.Add(1)
	return e.t, true
}

// hysteresisRung returns the smallest cached rung of one picture that is
// within factor hyst of size, which is what [Config.Hysteresis] consults.
//
// It returns a number and not a slice because the frame path calls it once per
// visible tile per frame, through [Pipeline.Lookup]. Building a slice of the
// cached rungs there would be one allocation per tile per frame, which is
// exactly the kind of cost the project plan, section 11, spends its 0 B/op
// contract on avoiding.
func (c *pixelCache) hysteresisRung(id ID, size int, hyst float64) (int, bool) {
	if hyst <= 1 {
		return 0, false
	}
	lo, hi := float64(size)/hyst, float64(size)*hyst
	c.mu.Lock()
	defer c.mu.Unlock()
	best := 0
	for _, k := range c.byID[id] {
		f := float64(k.Size)
		if f >= lo && f <= hi && (best == 0 || k.Size < best) {
			best = k.Size
		}
	}
	return best, best != 0
}

// dropByID removes every entry of one picture.
func (c *pixelCache) dropByID(id ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range c.byID[id] {
		if e, ok := c.entries[k]; ok {
			delete(c.entries, k)
			c.removeLRU(k)
			e.t.Release()
		}
	}
	delete(c.byID, id)
}

// evictFor drops least recently used entries until n bytes could plausibly be
// reserved, and reports how many bytes it gave back.
func (c *pixelCache) evictFor(b *budget, n int64) int64 {
	var freed int64
	c.mu.Lock()
	defer c.mu.Unlock()
	for freed < n && len(c.lru) > 0 {
		k := c.lru[0]
		c.lru = c.lru[1:]
		e, ok := c.entries[k]
		if !ok {
			continue
		}
		delete(c.entries, k)
		c.forgetID(k)
		// Count the bytes only when this was the last reference; an entry
		// somebody still holds does not come back yet.
		if e.t.refs.Load() == 1 {
			freed += e.t.bytes
		}
		e.t.Release()
		c.evictions.Add(1)
	}
	return freed
}

func (c *pixelCache) drop(k Key) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok {
		delete(c.entries, k)
		c.forgetID(k)
		c.removeLRU(k)
		e.t.Release()
	}
}

func (c *pixelCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		delete(c.entries, k)
		e.t.Release()
	}
	clear(c.byID)
	c.lru = c.lru[:0]
}

// forgetID removes k from the per picture index.
func (c *pixelCache) forgetID(k Key) {
	ks := c.byID[k.ID]
	for i, x := range ks {
		if x == k {
			ks = append(ks[:i], ks[i+1:]...)
			break
		}
	}
	if len(ks) == 0 {
		delete(c.byID, k.ID)
	} else {
		c.byID[k.ID] = ks
	}
}

func (c *pixelCache) touch(k Key) {
	c.removeLRU(k)
	c.lru = append(c.lru, k)
}

func (c *pixelCache) removeLRU(k Key) {
	for i, x := range c.lru {
		if x == k {
			c.lru = append(c.lru[:i], c.lru[i+1:]...)
			return
		}
	}
}

func (c *pixelCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
