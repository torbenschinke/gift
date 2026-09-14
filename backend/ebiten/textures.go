package ebiten

import (
	"image"
	"math"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/render"
)

// TextureConfig configures a [TextureCache]. The zero value selects the
// defaults below and is what [NewRenderer] uses.
type TextureConfig struct {
	// UploadBytesPerFrame is the byte budget for uploads in one *drawn*
	// frame. Zero selects [DefaultUploadBytesPerFrame]; a negative value
	// removes the budget, which is what a test that wants every upload to
	// land immediately sets.
	UploadBytesPerFrame int64

	// UploadsPerFrame caps the number of uploads in one drawn frame, whatever
	// their size. Zero selects [DefaultUploadsPerFrame]; a negative value
	// removes the cap.
	//
	// It exists next to the byte budget because the two bound different
	// things. The bytes bound the transfer, and the count bounds the number
	// of glTexSubImage2D calls — each of which can cost a driver side
	// synchronisation, whatever its size. See [TextureCache].
	UploadsPerFrame int

	// MaxBytes is the residency budget across all textures, in logical pixel
	// bytes. Zero selects [DefaultTextureBytes].
	//
	// Logical, and the project plan, section 11, insists on the word: "Logische
	// Pixelbytes sind kein exaktes GPU-Budget: Padding, Fragmentierung,
	// Atlaswachstum, Zwischenziele und Staging brauchen zusaetzlichen
	// Speicher." A 256 by 256 thumbnail is accounted at 256 KiB here and
	// occupies more than that on the device.
	MaxBytes int64

	// MaxAge is the number of drawn frames a texture may go undrawn before
	// [TextureCache.Tick] deallocates it. Zero selects
	// [DefaultTextureMaxAge]; a negative value disables age eviction.
	MaxAge int
}

// Defaults for [TextureConfig].
const (
	// DefaultUploadBytesPerFrame is 2 MiB, that is eight thumbnails at 256
	// pixels or two at 512.
	//
	// It is a starting point and is documented as one. The project plan,
	// section 11, refuses to guess at GPU costs — "CPU-Zeit am Upload-Aufruf
	// ist wegen interner Queues kein GPU-Zeitmass" — so the number that
	// matters is the one measured on a Pi 4 with a cold cache, and the
	// scenario for it is in section 13. What the budget is *for* is not in
	// doubt: the OpenGL upload path calls glFinish before writing pixels when
	// a draw has been issued, so an unbudgeted frame that uploads a whole
	// screenful of newly decoded thumbnails stalls on the GPU no matter how
	// much decoding happened in the background.
	DefaultUploadBytesPerFrame int64 = 2 << 20

	// DefaultUploadsPerFrame is eight.
	DefaultUploadsPerFrame = 8

	// DefaultTextureBytes is 128 MiB of logical pixel bytes, half of the
	// 256 MiB the project plan, section 13, budgets for resident image
	// memory at a hundred thousand entries. The other half is the headroom
	// the word "logical" buys.
	DefaultTextureBytes int64 = 128 << 20

	// DefaultTextureMaxAge is 600 drawn frames, ten seconds at sixty hertz,
	// the same number the glyph atlas and the shaping cache use.
	DefaultTextureMaxAge = 600
)

// texRecord is one slot of the texture pool.
//
// It is plain data in a flat slice addressed by index, like [glyphEntry] and
// like internal/scene's nodes, so that resolving a handle in the frame path is
// a bounds check and two integer comparisons with nothing for the collector to
// scan.
type texRecord struct {
	img *eb.Image
	// gen is the generation of the current occupant. It is bumped on every
	// eviction, which is what makes a handle to an evicted texture detectably
	// stale instead of silently pointing at the next picture that lands in
	// the slot.
	gen uint32
	// w and h are the pixel dimensions and bytes the logical accounting.
	w, h  int
	bytes int64
	// used is the drawn frame number this texture was last resolved or
	// uploaded in. A texture with used == frame is referenced by the frame in
	// progress and must not be evicted.
	used uint64
	// live says whether the slot is occupied.
	live bool
	// pendingFree marks a slot whose owner asked for release while the frame
	// in progress was already drawing it; see [TextureCache.Deallocate].
	pendingFree bool
}

// TextureCache is the image resource service of this backend: residency,
// admission and eviction of decoded pictures on the GPU.
//
// It implements [render.Images].
//
// # One texture per picture, and no atlas of our own
//
// A thumbnail gets its own [eb.Image]. There is deliberately no bespoke page
// packer here, unlike the glyph atlas next door, and the project plan,
// section 11, is the reason twice over.
//
// First, because Ebitengine already has one. An ordinary eb.Image is put on
// Ebitengine's automatic atlas whenever it fits: internal/atlas's
// canBePutOnAtlas returns true for any regular image no larger than maxSize,
// which is floorPowerOf2 of the driver's maximum texture edge and is therefore
// at least 4096 on anything that can run GL ES 3.0. A 512 pixel thumbnail —
// the largest rung of asset.DefaultSizes — is comfortably inside that, so the
// thumbnails of a gallery share backend textures without this package packing
// anything. "Zunaechst automatischer Ebitengine-Atlas und explizites
// Deallocate bei Eviction" is exactly what this is.
//
// Second, because the draw call argument for a bespoke atlas does not survive
// contact with what Ebitengine does with the commands. A batch here ends
// wherever the source image changes, so sixty visible tiles are sixty
// DrawTriangles calls; but graphicscommand's CanMergeWithDrawTrianglesCommand
// merges two consecutive draw commands whose *backend* source images are
// identical, and two thumbnails on one automatic atlas page are exactly that.
// The GPU therefore sees far fewer draws than this package issues, and a
// second atlas on top of the first would be paying for a measurement nobody
// has taken. The plan's sentence for that case is "Falls noetig, wird ein
// begrenzter Pool eigener Atlas-Seiten verglichen. Keine vorsorgliche eigene
// Atlas-Engine ohne Messung", and [RendererStats.ImageBatches] is the number
// that would justify taking it.
//
// The honest cost of one texture per picture is stated rather than hidden:
// every thumbnail pays Ebitengine's atlas padding, a 240 by 180 thumbnail
// wastes nothing but a 513 pixel one would get a texture of its own, and this
// package cannot see which of the two happened. [TextureStats.Bytes] is
// logical pixel bytes and not device memory; see [TextureConfig.MaxBytes].
//
// # Admission
//
// [TextureCache.Acquire] uploads at most [TextureConfig.UploadsPerFrame]
// textures and [TextureConfig.UploadBytesPerFrame] bytes per *drawn* frame.
// The budget is reset in [TextureCache.BeginFrame], which the renderer calls
// from [Renderer.BeginFrame], and painting is the only thing that happens once
// per drawn frame — so several Ebitengine updates before one draw cannot spend
// the budget several times. That is the distinction the project plan,
// section 11, draws and the one this whole type is arranged around.
//
// # Eviction
//
// Least recently drawn first, on two triggers, exactly like [GlyphAtlas]:
// pressure against [TextureConfig.MaxBytes] when a new texture needs room, and
// age in [TextureCache.Tick]. Evicting calls [eb.Image.Deallocate], which is
// the explicit release the project plan, section 11, asks for instead of
// waiting for a cleanup function.
//
// A texture the frame in progress has already resolved or uploaded is never
// evicted. Its vertices may already be in the renderer's buffers, and
// deallocating underneath them is the one failure mode that produces a blank
// or a wrong picture with no diagnostic at all. This is the same rule the
// glyph atlas applies to a page, implemented the same way: a use stamp equal
// to the current frame number is a veto.
type TextureCache struct {
	cfg TextureConfig

	// recs is the slot storage. Index 0 is never used, so that the zero
	// [render.ImageID] can mean "nothing" without a special case in the
	// renderer.
	recs []texRecord
	free []uint32

	// frame is the drawn frame counter and the age stamp.
	frame uint64
	bytes int64

	// frameUploads and frameBytes are the budget spent in the frame in
	// progress.
	frameUploads int
	frameBytes   int64

	stats TextureStats

	// onDeallocate, if non nil, is called with the image immediately before
	// it is deallocated. It exists so that a test can assert the explicit
	// release actually happens rather than merely being claimed in a comment,
	// which is what the glyph atlas does with the same hook.
	onDeallocate func(*eb.Image)
	// newImage overrides texture creation, so that the admission, eviction
	// and accounting logic is testable without a graphics context. A nil
	// value means [eb.NewImage].
	newImage func(w, h int) *eb.Image
}

// NewTextureCache returns an empty cache. It allocates no GPU memory until the
// first [TextureCache.Acquire].
func NewTextureCache(cfg TextureConfig) *TextureCache {
	if cfg.UploadBytesPerFrame == 0 {
		cfg.UploadBytesPerFrame = DefaultUploadBytesPerFrame
	}
	if cfg.UploadsPerFrame == 0 {
		cfg.UploadsPerFrame = DefaultUploadsPerFrame
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultTextureBytes
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = DefaultTextureMaxAge
	}
	// One dead slot so that id 0 is invalid.
	return &TextureCache{cfg: cfg, recs: make([]texRecord, 1, 64)}
}

// BeginFrame resets the per drawn frame upload budget. The renderer calls it
// from [Renderer.BeginFrame] and from nowhere else.
func (t *TextureCache) BeginFrame() {
	t.frameUploads = 0
	t.frameBytes = 0
}

// Tick advances the frame clock and evicts textures that have gone undrawn for
// longer than [TextureConfig.MaxAge]. The renderer calls it once per drawn
// frame, from [Renderer.EndFrame], after the last draw call has been issued —
// which is the point at which "used by the frame in progress" stops being
// true of anything.
func (t *TextureCache) Tick() {
	// Deferred releases first: a slot whose owner asked for it while the
	// frame was drawing it is now free to go.
	for i := range t.recs {
		if t.recs[i].live && t.recs[i].pendingFree {
			t.evict(uint32(i))
		}
	}
	t.frame++
	if t.cfg.MaxAge < 0 {
		return
	}
	age := uint64(t.cfg.MaxAge)
	if t.frame <= age {
		return
	}
	deadline := t.frame - age
	for i := range t.recs {
		r := &t.recs[i]
		if r.live && r.used < deadline {
			t.evict(uint32(i))
			t.stats.AgeEvictions++
		}
	}
}

// Resolve implements [render.Images].
//
// The hit path is a bounds check, two integer comparisons and one integer
// write. It allocates nothing, which is what keeps a frame full of resident
// pictures inside the 0 B/op contract of the project plan, section 11.
func (t *TextureCache) Resolve(h render.ImageHandle) (render.ImageID, bool) {
	i := uint32(h.ID)
	if i == 0 || int(i) >= len(t.recs) {
		t.stats.Stale++
		return 0, false
	}
	r := &t.recs[i]
	if !r.live || r.gen != h.Gen {
		t.stats.Stale++
		return 0, false
	}
	r.used = t.frame
	t.stats.Resolves++
	return h.ID, true
}

// Acquire implements [render.Images].
func (t *TextureCache) Acquire(px render.Pixels) (render.ImageHandle, bool) {
	if px.IsEmpty() {
		t.stats.Malformed++
		return render.ImageHandle{}, false
	}
	need := px.Bytes()
	if t.cfg.UploadsPerFrame >= 0 && t.frameUploads >= t.cfg.UploadsPerFrame {
		t.stats.Deferred++
		return render.ImageHandle{}, false
	}
	if t.cfg.UploadBytesPerFrame >= 0 && t.frameBytes+need > t.cfg.UploadBytesPerFrame &&
		t.frameBytes > 0 {
		// The "and something was already uploaded" clause matters: a single
		// picture larger than the whole frame budget must still get in
		// eventually, or a 512 pixel thumbnail with a 512 KiB budget would
		// never be drawn and the tile would be a placeholder for ever. It
		// gets a frame to itself instead.
		t.stats.Deferred++
		return render.ImageHandle{}, false
	}
	if !t.makeRoom(need) {
		t.stats.Rejected++
		return render.ImageHandle{}, false
	}

	i := t.alloc()
	r := &t.recs[i]
	r.img = t.newTexture(px.W, px.H)
	r.w, r.h = px.W, px.H
	r.bytes = need
	r.used = t.frame
	r.live = true
	r.pendingFree = false
	t.bytes += need
	t.writePixels(r, px)

	t.frameUploads++
	t.frameBytes += need
	t.stats.Uploads++
	t.stats.UploadedBytes += uint64(need)
	if n := len(t.recs) - len(t.free) - 1; n > t.stats.PeakTextures {
		t.stats.PeakTextures = n
	}
	if t.bytes > t.stats.PeakBytes {
		t.stats.PeakBytes = t.bytes
	}
	return render.ImageHandle{ID: render.ImageID(i), Gen: r.gen}, true
}

// Deallocate implements [render.Images].
//
// A texture the frame in progress has already drawn is *not* released here.
// Its vertices are in the renderer's buffers and the draw call has not been
// issued yet, so deallocating now would draw from a dead texture. The slot is
// marked instead and released by the next [TextureCache.Tick], which runs
// after the last draw call of the frame.
func (t *TextureCache) Deallocate(h render.ImageHandle) {
	i := uint32(h.ID)
	if i == 0 || int(i) >= len(t.recs) {
		return
	}
	r := &t.recs[i]
	if !r.live || r.gen != h.Gen {
		return
	}
	if r.used == t.frame {
		r.pendingFree = true
		return
	}
	t.evict(i)
	t.stats.ExplicitReleases++
}

// makeRoom evicts least recently drawn textures until need bytes fit, and
// reports whether they do.
func (t *TextureCache) makeRoom(need int64) bool {
	if need > t.cfg.MaxBytes {
		// A single picture larger than the whole residency budget. Refusing
		// it is the only bounded answer; growing the budget for one picture
		// would make it depend on the largest thumbnail the application ever
		// asked for.
		return false
	}
	for t.bytes+need > t.cfg.MaxBytes {
		if !t.evictLRU() {
			return false
		}
	}
	return true
}

// evictLRU releases the least recently drawn texture that the frame in
// progress does not reference, and reports whether it found one.
func (t *TextureCache) evictLRU() bool {
	best, bestUsed := -1, uint64(math.MaxUint64)
	for i := range t.recs {
		r := &t.recs[i]
		if !r.live || r.used == t.frame {
			continue
		}
		if r.used < bestUsed {
			best, bestUsed = i, r.used
		}
	}
	if best < 0 {
		return false
	}
	t.evict(uint32(best))
	t.stats.Evictions++
	return true
}

// evict releases one texture and invalidates every handle to it.
func (t *TextureCache) evict(i uint32) {
	r := &t.recs[i]
	if !r.live {
		return
	}
	if r.img != nil {
		if t.onDeallocate != nil {
			t.onDeallocate(r.img)
		}
		// Deallocate and not "keep the image and forget its contents": the
		// second never gives memory back, which is the whole point of the
		// explicit release the project plan, section 11, asks for.
		r.img.Deallocate()
	}
	t.bytes -= r.bytes
	*r = texRecord{gen: r.gen + 1}
	t.free = append(t.free, i)
	t.stats.Deallocations++
}

// alloc returns a free slot index, recycling one if there is any.
func (t *TextureCache) alloc() uint32 {
	if n := len(t.free); n > 0 {
		i := t.free[n-1]
		t.free = t.free[:n-1]
		return i
	}
	t.recs = append(t.recs, texRecord{})
	return uint32(len(t.recs) - 1)
}

func (t *TextureCache) newTexture(w, h int) *eb.Image {
	if t.newImage != nil {
		return t.newImage(w, h)
	}
	return eb.NewImage(w, h)
}

// writePixels uploads the pixel data.
//
// The rows are written one call at a time only when the stride is padded,
// which asset.Thumbnail's never is; the common case is a single WritePixels of
// a contiguous slice and therefore a single glTexSubImage2D.
func (t *TextureCache) writePixels(r *texRecord, px render.Pixels) {
	if r.img == nil {
		return
	}
	n := px.W * 4
	if px.Stride == n {
		r.img.WritePixels(px.Pix[:px.H*px.Stride])
		return
	}
	for y := range px.H {
		row := px.Pix[y*px.Stride : y*px.Stride+n]
		sub := r.img.SubImage(image.Rect(0, y, px.W, y+1)).(*eb.Image)
		sub.WritePixels(row)
	}
}

// image returns the texture of an id, or nil.
func (t *TextureCache) image(id render.ImageID) *eb.Image {
	i := uint32(id)
	if i == 0 || int(i) >= len(t.recs) || !t.recs[i].live {
		return nil
	}
	return t.recs[i].img
}

// size returns the pixel dimensions of an id.
func (t *TextureCache) size(id render.ImageID) (w, h int, ok bool) {
	i := uint32(id)
	if i == 0 || int(i) >= len(t.recs) || !t.recs[i].live {
		return 0, 0, false
	}
	return t.recs[i].w, t.recs[i].h, true
}

// TextureStats are the counters of the texture cache. Like every other counter
// in gift they are plain numbers written in the frame path and read out of
// band; see the project plan, section 15.
type TextureStats struct {
	// Uploads and UploadedBytes are what actually reached the GPU.
	Uploads       uint64
	UploadedBytes uint64
	// Deferred is the number of [TextureCache.Acquire] calls refused by the
	// per drawn frame upload budget. It is not an error: the caller drew a
	// placeholder and the picture arrived a frame or two later. A Deferred
	// that keeps climbing in a steady scene means the budget is smaller than
	// the rate at which pictures become ready.
	Deferred uint64
	// Rejected is the number refused because no room could be made: every
	// resident texture was already drawn by the frame in progress, or the
	// picture was larger than the whole residency budget. Unlike Deferred
	// this one says the budget is too small for the scene.
	Rejected uint64
	// Malformed is the number refused for bad pixel data.
	Malformed uint64
	// Resolves is the number of successful handle resolutions and Stale the
	// number of failed ones. A Stale in a steady scene is a consumer holding
	// handles across an eviction, which is legal and is exactly what the
	// generation in [render.ImageHandle] is for.
	Resolves, Stale uint64
	// Evictions is the number of textures released by pressure,
	// AgeEvictions the subset released by [TextureCache.Tick] for going
	// undrawn, and ExplicitReleases the ones an owner asked to release.
	// Deallocations is the total number of [eb.Image.Deallocate] calls and
	// must equal the sum of the reasons.
	Evictions, AgeEvictions, ExplicitReleases, Deallocations uint64
	// Textures and Bytes are the current residency, PeakTextures and
	// PeakBytes their high water marks. Bytes is logical pixel bytes; see
	// [TextureConfig.MaxBytes].
	Textures     int
	Bytes        int64
	PeakTextures int
	PeakBytes    int64
}

// Stats returns a snapshot of the counters.
func (t *TextureCache) Stats() TextureStats {
	s := t.stats
	s.Textures = len(t.recs) - len(t.free) - 1
	s.Bytes = t.bytes
	return s
}
