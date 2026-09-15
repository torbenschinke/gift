package ui_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// This file is the GPU half of step 4 as seen from ui: the shared image
// service, ui.Image, and the gallery drawing real thumbnails. Everything here
// runs headless. The pixels are in image_gpu_test.go.

// --- test doubles -------------------------------------------------------------

// fakeImages is a [render.Images] that records what was asked of it.
//
// It exists because the properties under test here — how often a picture is
// uploaded, whether two views share one texture, whether a stale handle is
// noticed — are properties of the *protocol* between ui and a backend, not of
// Ebitengine. Testing them against the real texture cache would drag a
// graphics context into a package that the project plan, section 12,
// criterion 4, requires to be testable without one.
type fakeImages struct {
	recs []fakeTex
	// budget is how many uploads one frame admits; -1 is unlimited. It is
	// here so that a ui level test can see a deferred upload without owning a
	// renderer.
	budget int
	spent  int

	uploads, resolves, stales, deallocs int
}

type fakeTex struct {
	gen  uint32
	w, h int
	live bool
}

func newFakeImages() *fakeImages {
	return &fakeImages{recs: make([]fakeTex, 1), budget: -1}
}

func (f *fakeImages) beginFrame() { f.spent = 0 }

func (f *fakeImages) Resolve(h render.ImageHandle) (render.ImageID, bool) {
	i := int(h.ID)
	if i <= 0 || i >= len(f.recs) || !f.recs[i].live || f.recs[i].gen != h.Gen {
		f.stales++
		return 0, false
	}
	f.resolves++
	return h.ID, true
}

func (f *fakeImages) Acquire(px render.Pixels) (render.ImageHandle, bool) {
	if px.IsEmpty() {
		return render.ImageHandle{}, false
	}
	if f.budget >= 0 && f.spent >= f.budget {
		return render.ImageHandle{}, false
	}
	f.spent++
	f.uploads++
	f.recs = append(f.recs, fakeTex{gen: 1, w: px.W, h: px.H, live: true})
	return render.ImageHandle{ID: render.ImageID(len(f.recs) - 1), Gen: 1}, true
}

func (f *fakeImages) Deallocate(h render.ImageHandle) {
	i := int(h.ID)
	if i > 0 && i < len(f.recs) && f.recs[i].live && f.recs[i].gen == h.Gen {
		f.recs[i].live = false
		f.deallocs++
	}
}

// evictAll invalidates every handle, which is what a backend does to a texture
// that has gone unused for long enough.
func (f *fakeImages) evictAll() {
	for i := range f.recs {
		if f.recs[i].live {
			f.recs[i].live = false
			f.recs[i].gen++
		}
	}
}

// deliverer is an [asset.Config.Deliver] that holds the closures until a test
// asks for them, which is what makes the race of the recycling test
// reproducible instead of a sleep.
type deliverer struct {
	ch chan func()
}

func newDeliverer() *deliverer { return &deliverer{ch: make(chan func(), 4096)} }

func (d *deliverer) deliver(fn func()) { d.ch <- fn }

// waitFor blocks until at least n closures are waiting, then returns without
// running them.
func (d *deliverer) waitFor(t testing.TB, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(d.ch) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d results arrived within ten seconds", len(d.ch), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// drain runs every closure that is waiting and returns how many there were.
func (d *deliverer) drain() int {
	n := 0
	for {
		select {
		case fn := <-d.ch:
			fn()
			n++
		default:
			return n
		}
	}
}

// --- test pictures -------------------------------------------------------------

// writePictures generates n PNGs in a temporary directory and returns the
// directory and the catalogue over them.
//
// Generated rather than checked in, so that the test data cannot drift from
// what the test claims about it, and PNG rather than JPEG so that the decoded
// pixels are exactly the ones written — which is what lets a pixel assertion
// name a colour.
func writePictures(t testing.TB, n int) (string, []asset.Metadata, map[asset.ID]asset.Source) {
	t.Helper()
	dir := t.TempDir()
	items := make([]asset.Metadata, n)
	srcs := make(map[asset.ID]asset.Source, n)
	for i := range n {
		name := "p" + strconv.Itoa(i) + ".png"
		w, h := 64+(i%3)*16, 48+(i%5)*16
		if err := os.WriteFile(filepath.Join(dir, name), pngOf(t, w, h, pictureColor(i)), 0o600); err != nil {
			t.Fatalf("write picture: %v", err)
		}
		src := asset.File(filepath.Join(dir, name))
		m := src.Metadata()
		m.Width, m.Height = uint32(w), uint32(h)
		items[i] = m
		srcs[m.ID] = src
	}
	return dir, items, srcs
}

// pictureColor gives every picture a colour that identifies it, so that a
// pixel test can say *which* picture is on screen and not merely that one is.
func pictureColor(i int) color.RGBA {
	return color.RGBA{R: uint8(20 + i*37%200), G: uint8(30 + i*91%200), B: uint8(40 + i*53%200), A: 255}
}

func pngOf(t testing.TB, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// picFixture wires a catalogue of real files to a real pipeline and a real
// gallery, with delivery under the test's control.
type picFixture struct {
	h    *gifttest.Harness
	g    *ui.Gallery
	pipe *asset.Pipeline
	del  *deliverer
	imgs *fakeImages
}

func newPicFixture(t testing.TB, n int, size geom.Size, overscan float32) *picFixture {
	t.Helper()
	_, items, srcs := writePictures(t, n)
	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{
		Deliver: del.deliver,
		Sizes:   []int{64, 128},
		Workers: 2,
	})
	t.Cleanup(func() { pipe.Close(); del.drain() })

	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	t.Cleanup(ui.ResetImageService)

	g := ui.NewGallery(asset.NewCollection(items))
	g.SetSources(func(id asset.ID) asset.Source { return srcs[id] })

	imgs := newFakeImages()
	h := gifttest.New(t, gifttest.Options{
		View: ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(60).Gap(4)).
			Overscan(overscan).
			Frame(size.W, size.H).
			Key("gallery"),
		Size: size,
	})
	h.App().SetImages(imgs)
	return &picFixture{h: h, g: g, pipe: pipe, del: del, imgs: imgs}
}

// frame draws one frame with the fake backend's per frame budget reset, which
// is what the real [backend/ebiten.Renderer] does in BeginFrame.
func (f *picFixture) frame() {
	f.imgs.beginFrame()
	f.h.Frame()
}

// --- the recycling guard -------------------------------------------------------

// TestRecycledTileNeverShowsThePreviousPicture is the test the project plan,
// section 13, names: "Keine falschen Bilder nach Tile-/Slot-Recycling und
// Quellenrevisionen."
//
// The race it reproduces is the real one and not an approximation. Tiles are
// bound, requests go out, the decodes finish — and *before any of the answers
// is delivered* the gallery is scrolled far enough that every slot is rebound
// to a different entry. Only then are the answers handed to the UI executor.
// Every one of them names a slot that now stands for something else.
func TestRecycledTileNeverShowsThePreviousPicture(t *testing.T) {
	f := newPicFixture(t, 300, geom.Sz(240, 200), 0)

	first := f.h.Find(gifttest.ByKey("gallery"))
	bound := f.g.Bindings(nil)
	if len(bound) < 4 {
		t.Fatalf("only %d tiles bound, this test needs a populated band: %v", len(bound), f.g)
	}
	// The decodes are running. Wait for the answers to exist, but do not let
	// them in.
	f.del.waitFor(t, len(bound))

	// Recycle: a jump of two hundred entries rebinds every slot.
	before := f.g.Bindings(nil)
	first.ScrollTo(4000)
	after := f.g.Bindings(nil)
	if sameItems(before, after) {
		t.Fatalf("the scroll did not recycle anything, so this test proves nothing:\nbefore %v\nafter %v",
			itemsOf(before), itemsOf(after))
	}

	// Now let the stale answers in, and give the gallery a frame to react.
	f.del.drain()
	f.frame()

	for _, b := range f.g.Bindings(nil) {
		if b.PictureReady && b.PictureID != b.ID {
			t.Errorf("slot %d stands for %q but draws the picture of %q; a decode that landed "+
				"after the recycle was accepted", b.Slot, b.ID, b.PictureID)
		}
	}
	if s := f.g.Stats(); s.Stale == 0 {
		t.Errorf("no result was dropped as stale, so the guard was never exercised: %+v", s)
	}
}

// TestRecyclingGuardIsLoadBearing is the other half, and it is the one that
// makes the test above worth having.
//
// It replays exactly the same race with the generation comparison switched
// off, and *requires* the wrong picture to appear. A guard whose removal
// changes nothing is not a guard; this is the evidence that
// [ui.TileBinding.Generation] is what delivers the property and not some
// accident of ordering. See ui.SetGalleryGenerationCheck.
func TestRecyclingGuardIsLoadBearing(t *testing.T) {
	defer ui.SetGalleryGenerationCheck(ui.SetGalleryGenerationCheck(false))

	f := newPicFixture(t, 300, geom.Sz(240, 200), 0)
	bound := f.g.Bindings(nil)
	if len(bound) < 4 {
		t.Fatalf("only %d tiles bound: %v", len(bound), f.g)
	}
	f.del.waitFor(t, len(bound))
	f.h.Find(gifttest.ByKey("gallery")).ScrollTo(4000)
	f.del.drain()
	f.frame()

	wrong := 0
	for _, b := range f.g.Bindings(nil) {
		if b.PictureReady && b.PictureID != b.ID {
			wrong++
		}
	}
	if wrong == 0 {
		t.Fatal("with the generation check disabled every tile still drew the right picture. " +
			"Either the race is no longer being reproduced — in which case " +
			"TestRecycledTileNeverShowsThePreviousPicture proves nothing either — or something " +
			"else is now doing the guard's job and the guard should be deleted rather than kept " +
			"as decoration")
	}
}

func itemsOf(bs []ui.TileBinding) []int {
	out := make([]int, len(bs))
	for i, b := range bs {
		out[i] = b.Item
	}
	return out
}

func sameItems(a, b []ui.TileBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Item != b[i].Item {
			return false
		}
	}
	return true
}

// --- placeholders --------------------------------------------------------------

// TestFastJumpShowsPlaceholdersAndDoesNotBlock is the project plan,
// section 10: "Schnelle Spruenge zeigen Platzhalter, statt auf Laden oder
// Decode zu warten."
//
// It is asserted on the display list, which is where the claim actually lives:
// after a jump to an entirely cold part of the catalogue the frame contains a
// filled rectangle per visible tile and not one image operation, and it was
// produced without anything blocking.
func TestFastJumpShowsPlaceholdersAndDoesNotBlock(t *testing.T) {
	f := newPicFixture(t, 2000, geom.Sz(240, 200), 0)

	start := time.Now()
	f.h.Find(gifttest.ByKey("gallery")).ScrollTo(20000)
	f.frame()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("a jump took %v; the frame path is waiting for a decode", d)
	}

	fills, imgs := 0, 0
	for _, op := range f.h.Ops() {
		switch op.Kind {
		case render.OpFillRect, render.OpFillRoundRect:
			fills++
		case render.OpImage:
			imgs++
		}
	}
	if imgs != 0 {
		t.Errorf("%d image operations in the first frame after a cold jump; nothing can have been "+
			"decoded and uploaded yet", imgs)
	}
	n := len(f.g.Bindings(nil))
	if n == 0 || fills < n {
		t.Errorf("%d placeholders for %d bound tiles; a cold tile has to draw something", fills, n)
	}
}

// --- the shared service --------------------------------------------------------

// TestImageViewAndGalleryShareTheServiceAndTheTexture is the project plan,
// section 10: "ImageGallery ... verwendet denselben Bildservice wie
// ui.Image(source)."
//
// One source, drawn in two places at the same size, has to cost one fetch, one
// decode and — the part this file is about — one upload. The assertion is on
// the number of Acquire calls the backend saw, because that is the resource
// the two are claimed to share.
func TestImageViewAndGalleryShareTheServiceAndTheTexture(t *testing.T) {
	_, items, srcs := writePictures(t, 1)
	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Sizes: []int{128}, Workers: 2})
	defer func() { pipe.Close(); del.drain() }()

	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	g := ui.NewGallery(asset.NewCollection(items))
	g.SetSources(func(id asset.ID) asset.Source { return srcs[id] })

	imgs := newFakeImages()
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Image(srcs[items[0].ID]).Size(128).Frame(120, 120),
			ui.ImageGallery(g).
				Layout(ui.Masonry().MinColumnWidth(110).Gap(4)).
				Frame(128, 140).Key("gallery"),
		).Frame(160, 280),
		Size: geom.Sz(160, 280),
	})
	h.App().SetImages(imgs)

	// Both views asked for the same picture at the same rung, so the pipeline
	// has one piece of work to do.
	del.waitFor(t, 2)
	del.drain()
	imgs.beginFrame()
	h.Frame()

	if imgs.uploads != 1 {
		t.Errorf("%d uploads for one picture drawn twice; ui.Image and the gallery are not "+
			"sharing the texture table", imgs.uploads)
	}
	if s := pipe.Stats(); s.Decodes != 1 {
		t.Errorf("%d decodes for one picture; the two views are not sharing the pipeline (%+v)", s.Decodes, s)
	}
	if u := ui.ImageServiceUploads(); u != 1 {
		t.Errorf("the service counted %d uploads, want 1", u)
	}

	// Two image operations from one texture: shared, not copied.
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d image operations, want 2 — one per view", n)
	}
}

// TestUploadReleasesTheThumbnail is the reference counting contract of the
// project plan, section 9: an uploader retains the thumbnail, uploads it and
// releases it, and the pixels stay charged against the pixel budget only for
// that long.
//
// A leak here would be invisible in a picture and fatal in a gallery: every
// uploaded thumbnail would hold its pixels for the life of the process and the
// budget would refuse to decode anything after the first few hundred.
func TestUploadReleasesTheThumbnail(t *testing.T) {
	f := newPicFixture(t, 40, geom.Sz(240, 200), 0)
	bound := f.g.Bindings(nil)
	f.del.waitFor(t, len(bound))
	f.del.drain()

	f.frame()
	after := f.pipe.Stats()
	if f.imgs.uploads == 0 {
		t.Fatal("nothing was uploaded, so the release has not been exercised")
	}

	// Everything still charged is charged by the CPU cache, which holds one
	// reference per entry. If the uploader had kept its reference the number
	// below would be twice the cache's.
	var cached int64
	for range after.CacheEntries {
		cached++
	}
	if after.Pixels.InUse <= 0 {
		t.Fatalf("the pixel budget is empty although %d thumbnails are cached", after.CacheEntries)
	}
	// One more frame changes nothing: a second upload of the same textures is
	// a resolve, and a resolve retains nothing.
	before := after.Pixels.InUse
	f.frame()
	f.frame()
	if now := f.pipe.Stats().Pixels.InUse; now != before {
		t.Errorf("the charged pixel bytes moved from %d to %d over two frames that uploaded "+
			"nothing new; the uploader is leaking a reference per frame", before, now)
	}
}

// TestEvictedTextureIsReuploadedRatherThanDrawnWrong checks the generation in
// [render.ImageHandle] from this side of the boundary: after the backend has
// thrown every texture away, the next frame notices, uploads again and draws
// the right picture, instead of holding a handle into a recycled slot.
func TestEvictedTextureIsReuploadedRatherThanDrawnWrong(t *testing.T) {
	f := newPicFixture(t, 20, geom.Sz(200, 160), 0)
	f.del.waitFor(t, len(f.g.Bindings(nil)))
	f.del.drain()
	f.frame()
	first := f.imgs.uploads
	if first == 0 {
		t.Fatal("nothing was uploaded")
	}

	f.imgs.evictAll()
	f.frame()
	if f.imgs.stales == 0 {
		t.Error("no stale handle was detected; the generation is not being compared")
	}
	if f.imgs.uploads <= first {
		t.Errorf("uploads stayed at %d after every texture was evicted; the views are drawing "+
			"handles they never re-resolved", f.imgs.uploads)
	}
}

// TestUploadBudgetDefersRatherThanDrops checks the view side of the admission
// control: a frame with more ready pictures than the budget allows draws
// placeholders for the rest, and the rest arrive over the following frames.
func TestUploadBudgetDefersRatherThanDrops(t *testing.T) {
	f := newPicFixture(t, 40, geom.Sz(240, 200), 0)
	f.imgs.budget = 2
	f.del.waitFor(t, len(f.g.Bindings(nil)))
	f.del.drain()

	f.frame()
	if f.imgs.uploads != 2 {
		t.Fatalf("frame 1 uploaded %d pictures, want the budget of 2", f.imgs.uploads)
	}
	f.frame()
	if f.imgs.uploads != 4 {
		t.Fatalf("frame 2 brought the total to %d, want 4", f.imgs.uploads)
	}
	// And the ones that did not get in are placeholders, not blanks.
	imgOps := 0
	for _, op := range f.h.Ops() {
		if op.Kind == render.OpImage {
			imgOps++
		}
	}
	if imgOps != 4 {
		t.Errorf("%d image operations in frame 2, want 4; a deferred upload has to fall back to "+
			"the placeholder and not to nothing", imgOps)
	}
}

// --- priority ------------------------------------------------------------------

// TestOverscanRequestsArePrefetch is the project plan, section 9: "Sichtbare
// Bilder haben Vorrang vor richtungsabhaengigem Prefetch."
//
// The gallery binds tiles for the viewport and for the overscan band alike,
// and the difference between the two is the scheduling class of the request.
// Without a band every request is visible; with one, the tiles outside the
// viewport are prefetches.
func TestOverscanRequestsArePrefetch(t *testing.T) {
	none := newPicFixture(t, 300, geom.Sz(240, 200), 0)
	if s := none.g.Stats(); s.RequestedPrefetch != 0 {
		t.Errorf("%d prefetches without an overscan band, want 0 (%+v)", s.RequestedPrefetch, s)
	}
	if s := none.g.Stats(); s.RequestedVisible == 0 {
		t.Fatalf("no visible request was issued at all: %+v", s)
	}

	band := newPicFixture(t, 300, geom.Sz(240, 200), 300)
	s := band.g.Stats()
	if s.RequestedPrefetch == 0 {
		t.Errorf("an overscan band of 300 px produced no prefetch: %+v", s)
	}
	if s.RequestedVisible == 0 {
		t.Errorf("an overscan band swallowed every visible request: %+v", s)
	}
	if s.RequestedVisible+s.RequestedPrefetch != s.Requested {
		t.Errorf("the two classes do not add up: %+v", s)
	}
	if band.g.SlotCount() <= none.g.SlotCount() {
		t.Errorf("the band did not grow the pool: %d slots with a band, %d without",
			band.g.SlotCount(), none.g.SlotCount())
	}
}

// --- errors --------------------------------------------------------------------

// TestAFailedPictureBecomesAVisibleTileState is the project plan, section 15:
// a runtime failure of the outside world is an ordinary value and produces
// "einen sichtbaren Fehlerzustand der betroffenen Kachel, nie zum Abbruch des
// Frames".
func TestAFailedPictureBecomesAVisibleTileState(t *testing.T) {
	dir := t.TempDir()
	// A file that exists and is not a picture, which is the failure a real
	// directory of photographs actually produces.
	if err := os.WriteFile(filepath.Join(dir, "notes.png"), []byte("this is not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := asset.File(filepath.Join(dir, "notes.png"))
	meta := src.Metadata()
	meta.Width, meta.Height = 100, 100

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Workers: 1})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	g := ui.NewGallery(asset.NewCollection([]asset.Metadata{meta}))
	g.SetSources(func(asset.ID) asset.Source { return src })
	h := gifttest.New(t, gifttest.Options{
		View: ui.ImageGallery(g).Frame(200, 200).Key("gallery"),
		Size: geom.Sz(200, 200),
	})
	h.App().SetImages(newFakeImages())

	del.waitFor(t, 1)
	del.drain()
	h.Frame()

	bs := g.Bindings(nil)
	if len(bs) != 1 {
		t.Fatalf("%d tiles bound, want 1", len(bs))
	}
	if !bs[0].PictureFailed {
		t.Errorf("the tile of an undecodable file is not in the error state: %+v", bs[0])
	}
	if s := g.Stats(); s.Failed != 1 {
		t.Errorf("Failed = %d, want 1", s.Failed)
	}
	// And the frame still happened.
	if h.Diagnostics().Frames == 0 {
		t.Error("no frame was produced")
	}
}

// --- ui.Image ------------------------------------------------------------------

// TestImageViewMeasuresFromTheAspectRatio checks that a picture takes the
// space its shape asks for, which is what makes ui.Image usable in a stack
// without a Frame.
func TestImageViewMeasuresFromTheAspectRatio(t *testing.T) {
	_, items, srcs := writePictures(t, 1)
	_ = items
	var src asset.Source
	for _, s := range srcs {
		src = s
	}
	ui.ResetImageService()
	defer ui.ResetImageService()

	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Image(src).MaxWidth(200).MaxHeight(200).Key("pic"),
		).Frame(300, 300),
		Size: geom.Sz(300, 300),
	})
	// No pipeline at all: the view still lays out and still draws its
	// placeholder, which is the headless case the project plan, section 12,
	// criterion 4, requires.
	b := h.Find(gifttest.ByKey("pic")).Bounds()
	if b.Width() <= 0 || b.Height() <= 0 {
		t.Fatalf("the image collapsed to %v without a pipeline", b)
	}
	m := src.Metadata()
	want := float32(m.Width) / float32(m.Height)
	if got := b.Width() / b.Height(); got < want-0.02 || got > want+0.02 {
		t.Errorf("aspect ratio %.3f, want %.3f (bounds %v)", got, want, b)
	}
}

// TestImageWithoutAPipelineDrawsItsPlaceholder pins the headless behaviour:
// no service, no panic, no blank.
func TestImageWithoutAPipelineDrawsItsPlaceholder(t *testing.T) {
	_, _, srcs := writePictures(t, 1)
	var src asset.Source
	for _, s := range srcs {
		src = s
	}
	ui.ResetImageService()
	defer ui.ResetImageService()

	h := gifttest.New(t, gifttest.Options{
		View: ui.Image(src).Frame(100, 100).Placeholder(ui.RGB(200, 30, 30)),
		Size: geom.Sz(120, 120),
	})
	found := false
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRect && op.Color == ui.RGB(200, 30, 30) {
			found = true
		}
	}
	if !found {
		t.Errorf("the placeholder was not drawn:\n%s", h.Dump())
	}
}

// TestGalleryWithoutSourcesStillDrawsPlaceholders is the step 3 behaviour,
// kept: a gallery with no source resolver is the synthetic catalogue of the
// previous work unit and must not have regressed.
func TestGalleryWithoutSourcesStillDrawsPlaceholders(t *testing.T) {
	ui.ResetImageService()
	defer ui.ResetImageService()
	items := make([]asset.Metadata, 50)
	for i := range items {
		items[i] = asset.Metadata{ID: asset.ID("x" + strconv.Itoa(i)), Width: 100, Height: 80}
	}
	g := ui.NewGallery(asset.NewCollection(items))
	h := gifttest.New(t, gifttest.Options{
		View: ui.ImageGallery(g).Frame(200, 200).Key("gallery"),
		Size: geom.Sz(200, 200),
	})
	if len(g.Bindings(nil)) == 0 {
		t.Fatal("no tiles bound")
	}
	if s := g.Stats(); s.Requested != 0 {
		t.Errorf("a gallery without a source resolver issued %d requests", s.Requested)
	}
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRoundRect {
			n++
		}
	}
	if n == 0 {
		t.Error("no placeholder was drawn")
	}
}

var _ = gift.TypeID(0)

// --- the allocation contract ---------------------------------------------------

// TestGalleryFrameWithResidentImagesIsAllocationFree extends the contract of
// the project plan, section 11, over the one path this work unit adds: a frame
// in which images are resident and drawn.
//
// The frame path being measured is the warm one — resolve a handle, emit an
// image operation, clip it — and none of those may allocate. Uploading and
// decoding are outside the contract and are outside this measurement too,
// which is why the pictures are made resident first and the step below only
// nudges the offset by half a pixel.
//
// It runs in every tag combination, including -tags giftdebug, because the
// project plan, section 15, makes the debug build part of the same contract.
func TestGalleryFrameWithResidentImagesIsAllocationFree(t *testing.T) {
	f := newPicFixture(t, 120, geom.Sz(320, 240), 0)
	f.del.waitFor(t, len(f.g.Bindings(nil)))
	f.del.drain()
	// Enough frames for every visible picture to get past the upload budget
	// and for the buffers to reach their working size.
	for range 24 {
		f.frame()
	}
	drawn := 0
	for _, op := range f.h.Ops() {
		if op.Kind == render.OpImage {
			drawn++
		}
	}
	if drawn == 0 {
		t.Fatalf("no picture is resident, so this measures the placeholder path: %+v", f.g.Stats())
	}

	node := f.h.Find(gifttest.ByKey("gallery")).Scroller()
	up := true
	step := func() {
		if up {
			f.h.App().ScrollBy(node.Ref(), 0.5)
		} else {
			f.h.App().ScrollBy(node.Ref(), -0.5)
		}
		up = !up
		f.imgs.beginFrame()
		if err := f.h.App().Update(geom.Sz(320, 240)); err != nil {
			t.Fatal(err)
		}
		f.h.App().Paint()
	}
	for range 32 {
		step()
	}
	gen := f.g.Generation()
	for range 8 {
		step()
	}
	if f.g.Generation() != gen {
		t.Fatalf("a half pixel scroll rebound %d tiles; this is not the steady state",
			f.g.Generation()-gen)
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Errorf("a frame with %d resident pictures allocated %v times per run, want 0", drawn, got)
	}
}

// --- WU-R: saturation must not latch -------------------------------------------

// gatedSource holds every Open until the test opens the gate, which is how a
// test produces queue saturation without a slow disk.
type gatedSource struct {
	inner asset.Source
	gate  chan struct{}
}

func (g gatedSource) Metadata() asset.Metadata { return g.inner.Metadata() }

func (g gatedSource) Open(ctx context.Context) (io.ReadCloser, error) {
	select {
	case <-g.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return g.inner.Open(ctx)
}

// TestSaturationRefusalIsNotPermanent is the WU-R regression test for the
// error seam between asset and ui.
//
// [asset.ErrQueueFull] is documented saturation behaviour, not a verdict on a
// picture. Before WU-R both consumers stored any non-nil error and never asked
// again, so a moment of pressure left a picture blank for the life of the
// process: six views, a queue of one, five refusals, and nothing on screen
// three seconds and a hundred and eighty frames after the pressure had
// cleared.
//
// The assertion is the recovery: once the gate opens, every picture is
// resident within a bounded number of frames.
func TestSaturationRefusalIsNotPermanent(t *testing.T) {
	const n = 6
	_, items, srcs := writePictures(t, n)
	gate := make(chan struct{})
	views := make([]gift.View, 0, n)
	for i, m := range items {
		src := gatedSource{inner: srcs[m.ID], gate: gate}
		views = append(views, ui.Image(src).Frame(40, 40).Key("pic"+strconv.Itoa(i)))
	}

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{
		Deliver:    del.deliver,
		Sizes:      []int{64},
		Workers:    1,
		QueueLimit: 1,
	})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	imgs := newFakeImages()
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(views...).Frame(300, 300),
		Size: geom.Sz(300, 300),
	})
	h.App().SetImages(imgs)

	// Under pressure: the queue holds one, the worker is blocked on the gate
	// and the rest are refused. Run a few frames so that the refusals are
	// delivered and, before WU-R, latched.
	refused := 0
	for range 10 {
		del.drain()
		imgs.beginFrame()
		h.Frame()
	}
	if got := countImageOps(h); got != 0 {
		t.Fatalf("%d pictures were drawn while every source was gated", got)
	}
	refused = int(pipe.Stats().Dropped)
	if refused == 0 {
		t.Fatal("no request was refused: the scenario did not saturate the queue")
	}

	// Pressure clears.
	close(gate)
	// Frames, generously: the reviewer measured none at all in a hundred and
	// eighty. The recovery this asserts is a handful; the rest of the budget
	// is there so that a loaded machine cannot turn "slow" into "latched".
	const budget = 120
	for frame := 1; frame <= budget; frame++ {
		// A real frame is 16 ms long; the pipeline is allowed to use it.
		time.Sleep(5 * time.Millisecond)
		del.drain()
		imgs.beginFrame()
		h.Frame()
		if countImageOps(h) == n {
			t.Logf("%d refusals, all %d pictures resident %d frames after the pressure cleared",
				refused, n, frame)
			return
		}
	}
	t.Fatalf("only %d of %d pictures are resident %d frames after the pressure cleared; "+
		"a retryable refusal was latched\n%s", countImageOps(h), n, budget, h.Dump())
}

func countImageOps(h *gifttest.Harness) int {
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			n++
		}
	}
	return n
}

// TestGallerySaturationDoesNotFailTiles is the same property for the other
// consumer. A tile refused by a full queue must not enter the error state the
// project plan, section 15, reserves for a source that really is broken.
func TestGallerySaturationDoesNotFailTiles(t *testing.T) {
	const n = 8
	_, items, srcs := writePictures(t, n)
	gate := make(chan struct{})
	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{
		Deliver:    del.deliver,
		Sizes:      []int{64},
		Workers:    1,
		QueueLimit: 1,
	})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	g := ui.NewGallery(asset.NewCollection(items))
	g.SetSources(func(id asset.ID) asset.Source {
		return gatedSource{inner: srcs[id], gate: gate}
	})
	h := gifttest.New(t, gifttest.Options{
		View: ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(60).Gap(4)).
			Frame(300, 300).Key("gallery"),
		Size: geom.Sz(300, 300),
	})
	h.App().SetImages(newFakeImages())

	for range 10 {
		del.drain()
		h.Frame()
	}
	if s := g.Stats(); s.Refused == 0 {
		t.Fatalf("the queue never saturated: %+v", s)
	} else if s.Failed != 0 {
		t.Fatalf("Failed = %d: a saturation refusal was turned into an error tile", s.Failed)
	}
	for _, b := range g.Bindings(nil) {
		if b.PictureFailed {
			t.Fatalf("tile %d is in the error state after a queue refusal", b.Slot)
		}
	}

	close(gate)
	for frame := 1; frame <= 120; frame++ {
		time.Sleep(5 * time.Millisecond)
		del.drain()
		h.Frame()
		ready, total := 0, 0
		for _, b := range g.Bindings(nil) {
			total++
			if b.PictureReady {
				ready++
			}
		}
		if total > 0 && ready == total {
			t.Logf("%d tiles ready %d frames after the pressure cleared (%d refusals)",
				ready, frame, g.Stats().Refused)
			return
		}
	}
	t.Fatalf("tiles are still without pictures 120 frames after the pressure cleared: %+v", g.Stats())
}

// TestWarmBindUsesThePipelineRevision covers the key mismatch WU-R found
// between the two paths that write a tile's imageKey.
//
// The result path keys the texture by the revision the *pipeline* established.
// The warm path — a tile bound to a picture that is already in the CPU pixel
// cache, because another view loaded it — used the revision the *catalogue*
// holds, which for an uncorrected entry is empty. Two keys, two texture slots
// and two uploads for one set of pixels.
//
// The scenario is the ordinary one: ui.Image draws a picture, the gallery
// scrolls the same picture into view, and the gallery has never had a
// correction for it because it never requested it.
func TestWarmBindUsesThePipelineRevision(t *testing.T) {
	_, items, srcs := writePictures(t, 1)
	src := srcs[items[0].ID]
	// The catalogue knows the shape but not the revision, which is the state
	// the project plan, section 9, calls "noch nicht validiert".
	items[0].Revision = ""

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Sizes: []int{128}, Workers: 2})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	// Prime the CPU pixel cache behind the user interface's back, so that the
	// gallery's very first bind is a warm one.
	done := make(chan struct{}, 1)
	pipe.Request(asset.Request{Source: src, Size: 128, Priority: asset.Visible,
		OnResult: func(asset.Result) { done <- struct{}{} }})
	for len(done) == 0 {
		time.Sleep(time.Millisecond)
		del.drain()
	}
	<-done
	if _, ok := pipe.Lookup(items[0].ID, 128); !ok {
		t.Fatal("priming did not put the picture in the CPU cache")
	} else {
		t2, _ := pipe.Lookup(items[0].ID, 128)
		t2.Release()
		t2.Release()
	}

	g := ui.NewGallery(asset.NewCollection(items))
	g.SetSources(func(id asset.ID) asset.Source { return srcs[id] })
	imgs := newFakeImages()
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Image(src).Size(128).Frame(120, 120),
			ui.ImageGallery(g).
				Layout(ui.Masonry().MinColumnWidth(110).Gap(4)).
				Frame(128, 140).Key("gallery"),
		).Frame(160, 280),
		Size: geom.Sz(160, 280),
	})
	h.App().SetImages(imgs)
	for range 6 {
		time.Sleep(2 * time.Millisecond)
		del.drain()
		imgs.beginFrame()
		h.Frame()
	}

	if n := ui.TextureKeysForTest(items[0].ID, 128); n != 1 {
		t.Errorf("%d texture keys for one picture at one rung: the warm bind and the "+
			"result used different revisions", n)
	}
	if imgs.uploads != 1 {
		t.Errorf("%d uploads for one set of pixels drawn twice", imgs.uploads)
	}
	if n := countImageOps(h); n != 2 {
		t.Errorf("%d image operations, want 2 — both views drew the picture\n%s", n, h.Dump())
	}
}

// --- the device density ---------------------------------------------------------

// TestImageRungIsChosenInDevicePixels is the image half of the project plan,
// section 18: "Die Bildleiter waehlt ihre Sprosse in Geraetepixeln. Heute
// waehlt sie nach der Layoutgroesse in Punkten, ein Foto ist also auf einem
// 2x-Display unabhaengig vom Framebuffer-Problem um den Faktor zwei
// unterversorgt."
//
// The view is 60 logical pixels on a ladder of 64, 128 and 256. At density 1
// it asks for 60 and gets the 64 rung; at density 2 it covers 120 real pixels,
// asks for 120 and gets 128. The texture that reaches the backend is therefore
// twice as wide, which is the number this test reads — a rung chosen from the
// logical size would hand a 2x display the 64 pixel thumbnail and magnify it.
func TestImageRungIsChosenInDevicePixels(t *testing.T) {
	for _, c := range []struct {
		density float64
		wantPx  int
	}{
		{1, 64},
		{2, 128},
		// A fractional factor is rounded before it ever reaches the ladder;
		// see gift.RoundDensity. 1.5 is 2, so it gets the same rung as 2 and
		// not a third one between the two.
		{1.5, 128},
	} {
		t.Run(fmt.Sprintf("density-%v", c.density), func(t *testing.T) {
			// A source large enough that the ladder, and not the picture,
			// decides the rung. The fixtures next door are 64 pixels wide, so
			// against them every density would answer 64 and the test would
			// pass without measuring anything.
			dir := t.TempDir()
			path := filepath.Join(dir, "big.png")
			if err := os.WriteFile(path, pngOf(t, 512, 512, color.RGBA{200, 80, 60, 255}), 0o600); err != nil {
				t.Fatal(err)
			}
			src := asset.File(path)
			del := newDeliverer()
			pipe := asset.NewPipeline(asset.Config{
				Deliver: del.deliver, Sizes: []int{64, 128, 256}, Workers: 1,
			})
			defer func() { pipe.Close(); del.drain() }()
			ui.ResetImageService()
			ui.SetImagePipeline(pipe)
			defer ui.ResetImageService()

			imgs := newFakeImages()
			h := gifttest.New(t, gifttest.Options{
				View:    ui.Image(src).Frame(60, 60).Key("pic"),
				Size:    geom.Sz(80, 80),
				Density: c.density,
			})
			h.App().SetImages(imgs)
			del.waitFor(t, 1)
			del.drain()
			imgs.beginFrame()
			h.Frame()
			imgs.beginFrame()
			h.Frame()

			if imgs.uploads == 0 {
				t.Fatalf("nothing was uploaded at density %v", c.density)
			}
			got := imgs.recs[len(imgs.recs)-1].w
			if got != c.wantPx {
				t.Errorf("density %v: the uploaded thumbnail is %d pixels wide, want %d",
					c.density, got, c.wantPx)
			}
		})
	}
}

// TestGalleryRungIsChosenInDevicePixels is the same claim for the gallery,
// which goes through the same pipeline by the requirement of the project plan,
// section 10, and therefore had to take the same change: the tile rectangle is
// a document rectangle in logical pixels and the rung is that times the
// density.
//
// It asserts on the width of the texture that reaches the backend rather than
// on an internal counter, because that is the thing the user sees: a 2x
// display drawing a 128 pixel thumbnail into a 200 pixel tile is the
// undersupply section 18 names.
func TestGalleryRungIsChosenInDevicePixels(t *testing.T) {
	for _, c := range []struct {
		density float64
		wantPx  int
	}{
		{1, 128},
		{2, 256},
	} {
		t.Run(fmt.Sprintf("density-%v", c.density), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "big.png")
			if err := os.WriteFile(path, pngOf(t, 512, 512, color.RGBA{60, 160, 200, 255}), 0o600); err != nil {
				t.Fatal(err)
			}
			src := asset.File(path)
			m := src.Metadata()
			m.Width, m.Height = 512, 512

			del := newDeliverer()
			pipe := asset.NewPipeline(asset.Config{
				Deliver: del.deliver, Sizes: []int{128, 256}, Workers: 1,
			})
			defer func() { pipe.Close(); del.drain() }()
			ui.ResetImageService()
			ui.SetImagePipeline(pipe)
			defer ui.ResetImageService()

			g := ui.NewGallery(asset.NewCollection([]asset.Metadata{m}))
			g.SetSources(func(asset.ID) asset.Source { return src })

			imgs := newFakeImages()
			h := gifttest.New(t, gifttest.Options{
				View: ui.ImageGallery(g).
					Layout(ui.Masonry().MinColumnWidth(100).Gap(4)).
					Frame(110, 140).Key("gallery"),
				Size:    geom.Sz(140, 160),
				Density: c.density,
			})
			h.App().SetImages(imgs)
			del.waitFor(t, 1)
			del.drain()
			imgs.beginFrame()
			h.Frame()
			imgs.beginFrame()
			h.Frame()

			if imgs.uploads == 0 {
				t.Fatalf("the gallery uploaded nothing at density %v", c.density)
			}
			got := imgs.recs[len(imgs.recs)-1].w
			if got != c.wantPx {
				t.Errorf("density %v: the tile texture is %d pixels wide, want %d",
					c.density, got, c.wantPx)
			}
		})
	}
}
