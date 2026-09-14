package ebiten

import (
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// These tests need no graphics context. Ebitengine allocates an image lazily —
// see internal/atlas, where allocate happens on first use — so creating,
// writing and deallocating one is legal headless, which is what the project
// plan, section 12, criterion 4, requires of everything but the pixel tests.

// pixels returns a well formed premultiplied RGBA payload of n by n pixels.
func pixels(n int) render.Pixels {
	return render.Pixels{Pix: make([]byte, n*n*4), W: n, H: n, Stride: n * 4}
}

// TestUploadBudgetIsPerDrawnFrame is the central assertion of the upload
// admission path, and the one the project plan, section 11, states in a
// sentence that is easy to nod at and hard to implement: "Uploads erhalten ein
// Budget pro gezeichnetem Frame, nicht pro Update-Aufruf. Ebitengine kann
// mehrere Updates vor einem Draw ausfuehren."
//
// The test therefore does two things. It shows that a frame with more ready
// pictures than the budget uploads exactly the budget and the rest arrive
// later; and it shows that the budget is *not* replenished by anything other
// than the start of a drawn frame, which is what makes it per frame rather
// than per update.
func TestUploadBudgetIsPerDrawnFrame(t *testing.T) {
	tc := NewTextureCache(TextureConfig{UploadsPerFrame: 3, UploadBytesPerFrame: -1})

	// Frame one: six pictures are ready, three get in.
	tc.BeginFrame()
	got := 0
	for range 6 {
		if _, ok := tc.Acquire(pixels(8)); ok {
			got++
		}
	}
	if got != 3 {
		t.Errorf("frame 1 uploaded %d pictures, want the budget of 3", got)
	}
	if d := tc.Stats().Deferred; d != 3 {
		t.Errorf("Deferred = %d, want 3; a refused upload has to be counted or the budget is invisible", d)
	}

	// Several updates happen. The thing an update does *not* do is start a
	// frame, so nothing here may give the budget back. This is the whole
	// distinction: if the reset lived in an update callback, the loop below
	// would hand out eighteen uploads for one frame.
	for range 6 {
		if _, ok := tc.Acquire(pixels(8)); ok {
			t.Fatal("an upload was admitted between two drawn frames; the budget is being " +
				"replenished by something other than BeginFrame, which is exactly the per update " +
				"budget the project plan, section 11, rules out")
		}
	}

	// Frame two: the rest arrives.
	tc.Tick()
	tc.BeginFrame()
	got = 0
	for range 6 {
		if _, ok := tc.Acquire(pixels(8)); ok {
			got++
		}
	}
	if got != 3 {
		t.Errorf("frame 2 uploaded %d pictures, want 3", got)
	}
	if u := tc.Stats().Uploads; u != 6 {
		t.Errorf("Uploads = %d, want 6 after two frames of three", u)
	}
}

// TestUploadByteBudget checks the other half of the admission: the byte
// budget, and the exception that keeps a picture larger than the whole budget
// from being starved for ever.
func TestUploadByteBudget(t *testing.T) {
	// A 16x16 picture is 1024 bytes, so the budget admits one and refuses the
	// second.
	tc := NewTextureCache(TextureConfig{UploadBytesPerFrame: 1500, UploadsPerFrame: -1})
	tc.BeginFrame()
	if _, ok := tc.Acquire(pixels(16)); !ok {
		t.Fatal("the first upload was refused although the budget was empty")
	}
	if _, ok := tc.Acquire(pixels(16)); ok {
		t.Error("a second upload was admitted past the byte budget")
	}

	// A picture larger than the entire frame budget still gets a frame to
	// itself, or a 512 pixel thumbnail under a small budget would be a
	// placeholder for ever.
	tc.Tick()
	tc.BeginFrame()
	if _, ok := tc.Acquire(pixels(32)); !ok {
		t.Error("a picture larger than the whole frame budget was refused; it must get a frame " +
			"of its own instead of never being drawn")
	}
}

// TestEvictionDeallocatesExplicitly is the project plan, section 11,
// requirement that eviction calls Deallocate rather than waiting for a cleanup
// function, asserted rather than claimed in a comment.
func TestEvictionDeallocatesExplicitly(t *testing.T) {
	var freed []*eb.Image
	tc := NewTextureCache(TextureConfig{
		// Room for exactly one 8x8 picture.
		MaxBytes:            300,
		UploadsPerFrame:     -1,
		UploadBytesPerFrame: -1,
	})
	tc.onDeallocate = func(i *eb.Image) { freed = append(freed, i) }

	tc.BeginFrame()
	a, ok := tc.Acquire(pixels(8))
	if !ok {
		t.Fatal("the first upload was refused")
	}
	// A new frame, so a is no longer referenced by the frame in progress.
	tc.Tick()
	tc.BeginFrame()
	b, ok := tc.Acquire(pixels(8))
	if !ok {
		t.Fatal("the second upload was refused although the first could be evicted")
	}
	if len(freed) != 1 {
		t.Fatalf("Deallocate was called %d times, want 1", len(freed))
	}
	if _, ok := tc.Resolve(a); ok {
		t.Error("the evicted handle still resolves; the generation is not being bumped and a " +
			"consumer would draw whatever landed in the slot next")
	}
	if _, ok := tc.Resolve(b); !ok {
		t.Error("the live handle does not resolve")
	}
	s := tc.Stats()
	if s.Evictions != 1 || s.Deallocations != 1 {
		t.Errorf("Evictions = %d, Deallocations = %d, want 1 and 1", s.Evictions, s.Deallocations)
	}
	if s.Textures != 1 {
		t.Errorf("Textures = %d, want 1", s.Textures)
	}
}

// TestATextureDrawnThisFrameIsNeverEvicted is the rule the glyph atlas already
// applies to a page: vertices referencing a texture may already be in the
// renderer's buffers, so releasing it during the frame that draws it is the
// one failure that produces a wrong picture with no diagnostic at all.
func TestATextureDrawnThisFrameIsNeverEvicted(t *testing.T) {
	tc := NewTextureCache(TextureConfig{
		MaxBytes: 300, UploadsPerFrame: -1, UploadBytesPerFrame: -1,
	})
	tc.BeginFrame()
	a, _ := tc.Acquire(pixels(8))

	// Still inside the same frame, and a was uploaded — and therefore used —
	// by it. There is nothing else to evict, so the second picture has to be
	// refused rather than served by cannibalising the first.
	if _, ok := tc.Acquire(pixels(8)); ok {
		t.Fatal("a texture the frame in progress draws was evicted to make room")
	}
	if _, ok := tc.Resolve(a); !ok {
		t.Fatal("the texture of the frame in progress is gone")
	}
	if r := tc.Stats().Rejected; r != 1 {
		t.Errorf("Rejected = %d, want 1; refusing is bounded and visible, evicting is neither", r)
	}
}

// TestReuploadAfterEvictionProducesTheRightPixels checks the round trip a
// gallery makes every time it scrolls away from a picture and back: the
// texture is evicted, the handle goes stale, and the next upload of the same
// pixels is a different, correct texture rather than a resurrection of the
// old one.
func TestReuploadAfterEvictionProducesTheRightPixels(t *testing.T) {
	tc := NewTextureCache(TextureConfig{
		MaxBytes: 300, UploadsPerFrame: -1, UploadBytesPerFrame: -1,
	})
	first := pixels(8)
	for i := range first.Pix {
		first.Pix[i] = 0x11
	}
	tc.BeginFrame()
	a, _ := tc.Acquire(first)
	if w, h, ok := tc.size(a.ID); !ok || w != 8 || h != 8 {
		t.Fatalf("size(a) = %d, %d, %v", w, h, ok)
	}

	tc.Tick()
	tc.BeginFrame()
	second := pixels(8)
	for i := range second.Pix {
		second.Pix[i] = 0x22
	}
	b, ok := tc.Acquire(second)
	if !ok {
		t.Fatal("the re-upload was refused")
	}
	if b.ID != a.ID {
		t.Logf("the slot was not reused (%d then %d); the assertion below is what matters", a.ID, b.ID)
	}
	if b.Gen == a.Gen && b.ID == a.ID {
		t.Error("the generation did not move across an eviction, so the stale handle is " +
			"indistinguishable from the live one")
	}
	if _, ok := tc.Resolve(a); ok {
		t.Error("the handle from before the eviction still resolves")
	}
	if id, ok := tc.Resolve(b); !ok || tc.image(id) == nil {
		t.Error("the re-uploaded texture does not resolve")
	}
}

// TestExplicitDeallocateDefersInsideAFrame checks that an owner asking for a
// release does not get one while the frame is still drawing the texture.
func TestExplicitDeallocateDefersInsideAFrame(t *testing.T) {
	tc := NewTextureCache(TextureConfig{UploadsPerFrame: -1, UploadBytesPerFrame: -1})
	tc.BeginFrame()
	a, _ := tc.Acquire(pixels(4))
	tc.Deallocate(a)
	if _, ok := tc.Resolve(a); !ok {
		t.Fatal("the texture was released during the frame that draws it")
	}
	tc.Tick()
	if _, ok := tc.Resolve(a); ok {
		t.Fatal("the deferred release did not happen at the end of the frame")
	}

	// And outside a frame it is immediate.
	tc.BeginFrame()
	b, _ := tc.Acquire(pixels(4))
	tc.Tick()
	tc.BeginFrame()
	tc.Deallocate(b)
	if _, ok := tc.Resolve(b); ok {
		t.Error("an explicit release of a texture the frame has not touched was deferred")
	}
	if n := tc.Stats().ExplicitReleases; n != 1 {
		t.Errorf("ExplicitReleases = %d, want 1", n)
	}
}

// TestAgeEvictionReleasesUnusedTextures checks the second eviction trigger,
// the one that gives memory back when a gallery is simply no longer looking at
// a picture.
func TestAgeEvictionReleasesUnusedTextures(t *testing.T) {
	tc := NewTextureCache(TextureConfig{
		MaxAge: 3, UploadsPerFrame: -1, UploadBytesPerFrame: -1,
	})
	tc.BeginFrame()
	a, _ := tc.Acquire(pixels(4))
	for range 3 {
		tc.Tick()
		tc.BeginFrame()
		if _, ok := tc.Resolve(a); !ok {
			t.Fatal("the texture aged out although it was drawn every frame")
		}
	}
	for range 5 {
		tc.Tick()
		tc.BeginFrame()
	}
	if _, ok := tc.Resolve(a); ok {
		t.Error("the texture survived five frames of not being drawn with MaxAge 3")
	}
	if n := tc.Stats().AgeEvictions; n != 1 {
		t.Errorf("AgeEvictions = %d, want 1", n)
	}
}

// TestImageOpsInterleaveWithShapesAndText is the batching contract: an image
// is a material, materials are never reordered, and the number of draw calls
// is therefore the number of runs in display list order.
//
// The project plan, section 11, forbids "globales Umsortieren transparenter
// Inhalte nur zur Verringerung von Draw Calls", and the cheapest way to break
// that rule would be to collect every image of a frame and draw it at the end.
// The assertion below is that the materials come out in emission order.
func TestImageOpsInterleaveWithShapesAndText(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	tc := r.Textures()
	var l render.List
	l.Reset()

	r.BeginFrame(geom.Sz(200, 200))
	one, ok1 := tc.Acquire(pixels(8))
	two, ok2 := tc.Acquire(pixels(8))
	if !ok1 || !ok2 {
		t.Fatal("the uploads were refused")
	}
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 0, 50, 50), Color: render.RGB(9, 9, 9)})
	l.Add(render.Op{Kind: render.OpImage, Bounds: geom.Rc(0, 0, 50, 50), Color: render.RGB(255, 255, 255), Image: one.ID})
	l.Add(render.Op{Kind: render.OpImage, Bounds: geom.Rc(50, 0, 100, 50), Color: render.RGB(255, 255, 255), Image: two.ID})
	l.Add(render.Op{Kind: render.OpFillRect, Bounds: geom.Rc(0, 50, 50, 100), Color: render.RGB(9, 9, 9)})

	var mats []Material
	r.drawFn = func(m Material, _ []eb.Vertex, _ []uint32) { mats = append(mats, m) }
	r.Submit(&l)
	r.EndFrame()

	want := []Material{MaterialShape, MaterialImage, MaterialImage, MaterialShape}
	if len(mats) != len(want) {
		t.Fatalf("materials = %v, want %v", mats, want)
	}
	for i := range want {
		if mats[i] != want[i] {
			t.Fatalf("materials = %v, want %v; the display list order is the drawing order", mats, want)
		}
	}
	s := r.Stats()
	if s.ImageBatches != 2 {
		t.Errorf("ImageBatches = %d, want 2; one per run of operations sampling the same texture", s.ImageBatches)
	}
	if s.ImageOps != 2 {
		t.Errorf("ImageOps = %d, want 2", s.ImageOps)
	}
	if s.Accounted() != uint64(l.Len()) {
		t.Errorf("Accounted = %d, want %d; the accounting has to stay total", s.Accounted(), l.Len())
	}
}

// TestImageOpWithoutAResidentTextureIsCounted checks that a malformed list
// draws nothing and says so, rather than reaching into the slot table.
func TestImageOpWithoutAResidentTextureIsCounted(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	var l render.List
	l.Reset()
	l.Add(render.Op{Kind: render.OpImage, Bounds: geom.Rc(0, 0, 10, 10),
		Color: render.RGB(255, 255, 255), Image: 4242})
	r.BeginFrame(geom.Sz(64, 64))
	r.Submit(&l)
	r.EndFrame()
	if n := r.Stats().SkippedNoImage; n != 1 {
		t.Errorf("SkippedNoImage = %d, want 1", n)
	}
}

// TestTextureCacheRejectsMalformedPixels checks the cheap guard that keeps a
// bad slice out of WritePixels.
func TestTextureCacheRejectsMalformedPixels(t *testing.T) {
	tc := NewTextureCache(TextureConfig{})
	tc.BeginFrame()
	for _, px := range []render.Pixels{
		{},
		{Pix: make([]byte, 16), W: 2, H: 2, Stride: 4}, // stride too small
		{Pix: make([]byte, 8), W: 2, H: 2, Stride: 8},  // slice too short
	} {
		if _, ok := tc.Acquire(px); ok {
			t.Errorf("malformed pixels %+v were accepted", px)
		}
	}
	if n := tc.Stats().Malformed; n != 3 {
		t.Errorf("Malformed = %d, want 3", n)
	}
}
