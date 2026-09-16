//go:build giftgpu

package ui_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// The pixel half of the ui side of step 4. The headless tests in image_test.go
// say which picture a tile stands for and when a request goes out; these say
// that the picture reaches the framebuffer, the right way up, in the right
// colour and over the right background.

// TestImageViewDrawsTheRealPixels is the smallest possible end to end run of
// the whole step: a real file on disk, the real pipeline, a real decode, a
// real upload through the per drawn frame budget, and a readback.
func TestImageViewDrawsTheRealPixels(t *testing.T) {
	dir := t.TempDir()
	want := color.RGBA{200, 60, 40, 255}
	path := filepath.Join(dir, "solid.png")
	if err := os.WriteFile(path, pngOf(t, 40, 40, want), 0o600); err != nil {
		t.Fatal(err)
	}

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Sizes: []int{64}, Workers: 1})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	h := gifttest.New(t, gifttest.Options{
		View: ui.Image(asset.File(path)).Size(64).Frame(64, 64).
			Fit(ui.FitStretch).Placeholder(ui.RGB(0, 0, 0)),
		Size: geom.Sz(64, 64),
	})
	del.waitFor(t, 1)
	del.drain()
	// A frame to lay out with the answer, then the golden path's own frame,
	// which is the one that uploads.
	h.Frame()
	img := h.Image()
	img = h.Image()

	// And structurally: a *picture* reached the display list, not the
	// placeholder that looks just like success. See gifttest.Node.AssertDrawsImage.
	h.Find(gifttest.ByType("ui.Image")).AssertDrawsImage()

	got := colorAt(img, 32, 32)
	if !nearRGBA(got, want, 6) {
		t.Errorf("the picture at its centre is %v, want %v; a decoded PNG reaches the "+
			"framebuffer unchanged apart from the resample", got, want)
	}
}

// TestImageOrientationIsAppliedOnScreen is the orientation half of step 4 of
// the project plan, section 12: "Zunaechst JPEG und PNG mit dokumentierter
// Orientierungsbehandlung."
//
// The picture is stored with its quadrants in one arrangement and an EXIF
// orientation of 6, which means "rotate 90 degrees clockwise for display". The
// assertion is on the screen and not on the decoder: what the user sees is
// upright, whatever the file says.
func TestImageOrientationIsAppliedOnScreen(t *testing.T) {
	dir := t.TempDir()
	// Stored: red top left, green top right. Orientation 6 rotates the stored
	// image 90 degrees clockwise, so the stored top left corner ends up top
	// right and the stored top right corner ends up bottom right.
	stored := quadrants(64, color.RGBA{220, 30, 30, 255}, color.RGBA{30, 200, 30, 255},
		color.RGBA{30, 30, 220, 255}, color.RGBA{230, 230, 30, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, stored, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rotated.jpg")
	if err := os.WriteFile(path, withOrientation(t, buf.Bytes(), 6), 0o600); err != nil {
		t.Fatal(err)
	}

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Sizes: []int{64}, Workers: 1})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	h := gifttest.New(t, gifttest.Options{
		View: ui.Image(asset.File(path)).Size(64).Frame(64, 64).Fit(ui.FitStretch),
		Size: geom.Sz(64, 64),
	})
	del.waitFor(t, 1)
	del.drain()
	h.Frame()
	h.Image()
	img := h.Image()

	// After a clockwise quarter turn: stored bottom left is top left, stored
	// top left is top right.
	for _, c := range []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"top left", 12, 12, color.RGBA{30, 30, 220, 255}},
		{"top right", 52, 12, color.RGBA{220, 30, 30, 255}},
		{"bottom right", 52, 52, color.RGBA{30, 200, 30, 255}},
		{"bottom left", 12, 52, color.RGBA{230, 230, 30, 255}},
	} {
		if got := colorAt(img, c.x, c.y); !nearRGBA(got, c.want, 24) {
			t.Errorf("%s = %v, want %v; the EXIF orientation is not reaching the screen",
				c.name, got, c.want)
		}
	}
}

// TestGalleryWithRealPicturesGolden is the golden image the brief asks for: a
// deterministic catalogue at a deterministic scroll position, drawn from real
// files through the real pipeline.
//
// The pictures are generated rather than checked in, so the golden cannot
// drift away from the data it was made of, and they are flat colours, so a
// difference in the golden is a difference in the *layout or the upload* and
// never in a JPEG quantisation table.
func TestGalleryWithRealPicturesGolden(t *testing.T) {
	dir := t.TempDir()
	items := make([]asset.Metadata, 24)
	srcs := make(map[asset.ID]asset.Source, len(items))
	for i := range items {
		w, h := 120+(i%3)*40, 90+(i%4)*40
		p := filepath.Join(dir, "g"+strconv.Itoa(i)+".png")
		if err := os.WriteFile(p, pngOf(t, w, h, pictureColor(i*7)), 0o600); err != nil {
			t.Fatal(err)
		}
		src := asset.File(p)
		m := src.Metadata()
		m.Width, m.Height = uint32(w), uint32(h)
		items[i] = m
		srcs[m.ID] = src
	}

	del := newDeliverer()
	pipe := asset.NewPipeline(asset.Config{Deliver: del.deliver, Sizes: []int{128}, Workers: 2})
	defer func() { pipe.Close(); del.drain() }()
	ui.ResetImageService()
	ui.SetImagePipeline(pipe)
	defer ui.ResetImageService()

	g := ui.NewGallery(asset.NewCollection(items))
	g.SetSources(func(id asset.ID) asset.Source { return srcs[id] })
	h := gifttest.New(t, gifttest.Options{
		View: ui.ImageGallery(g).
			Layout(ui.Masonry().MinColumnWidth(90).Gap(6)).
			Padding(8).
			Background(ui.RGB(245, 246, 248)).
			Frame(320, 240).Key("gallery"),
		Size: geom.Sz(320, 240),
	})

	settlePictures(t, h, g, del)
	h.AssertGolden("gallery-pictures-top")

	// Twice, with a settle in between, and the reason is worth stating. The
	// first scroll brings entries into view that have never been probed;
	// their results carry a revision, that is a correction, and a correction
	// batch takes a scroll anchor and reflows — so the offset after the first
	// settle is not the offset that was asked for, and *when* it stops moving
	// depends on how the decodes interleaved. The second scroll happens after
	// every visible entry is already corrected, so it is the position it says
	// it is. This is the gallery behaving exactly as the project plan,
	// section 10, requires ("ein Korrekturbuendel verschiebt das bereits
	// uebernommene Layout nicht ... wann der Inhalt nachrueckt, ist eine
	// Ankerentscheidung"), and it is why a golden of a cold gallery at a
	// scrolled position needs two rounds rather than a longer sleep.
	h.Find(gifttest.ByKey("gallery")).ScrollTo(180)
	settlePictures(t, h, g, del)
	h.Find(gifttest.ByKey("gallery")).ScrollTo(180)
	settlePictures(t, h, g, del)
	h.AssertGolden("gallery-pictures-180")
}

// settlePictures runs frames until every bound tile has its picture and the
// per drawn frame upload budget has caught up.
//
// Without it a golden of a gallery would be a golden of a race: which
// thumbnails happen to have been decoded by frame twelve depends on the
// machine, and the budget of the project plan, section 11, deliberately
// spreads the uploads over several frames on top of that. Waiting for the
// steady state is the only way a picture of a cold gallery can be compared at
// all.
func settlePictures(t *testing.T, h *gifttest.Harness, g *ui.Gallery, del *deliverer) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		del.drain()
		h.Frame()
		h.Image()
		ready := true
		bs := g.Bindings(nil)
		for _, b := range bs {
			if !b.PictureReady && !b.PictureFailed {
				ready = false
				break
			}
		}
		if ready && len(bs) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the gallery did not finish loading within twenty seconds: %+v", g.Stats())
		}
		time.Sleep(time.Millisecond)
	}
	// And enough further frames for the deferred uploads to land.
	for range 16 {
		del.drain()
		h.Frame()
		h.Image()
	}
}

// --- helpers -------------------------------------------------------------------

func quadrants(n int, tl, tr, bl, br color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			c := tl
			switch {
			case x >= n/2 && y < n/2:
				c = tr
			case x < n/2 && y >= n/2:
				c = bl
			case x >= n/2 && y >= n/2:
				c = br
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// withOrientation splices a minimal APP1 EXIF segment carrying the given
// orientation directly after the SOI of a JPEG.
//
// Generated rather than checked in, for the reason the asset package gives for
// the same helper: a fixture photograph cannot be read by a reviewer, and all
// eight orientations are one argument apart.
func withOrientation(t testing.TB, jpg []byte, o uint16) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("not a JPEG")
	}
	var tiff []byte
	tiff = append(tiff, 'I', 'I')
	tiff = binary.LittleEndian.AppendUint16(tiff, 42)
	tiff = binary.LittleEndian.AppendUint32(tiff, 8)
	tiff = binary.LittleEndian.AppendUint16(tiff, 1)
	tiff = binary.LittleEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.LittleEndian.AppendUint16(tiff, 3)
	tiff = binary.LittleEndian.AppendUint32(tiff, 1)
	tiff = binary.LittleEndian.AppendUint16(tiff, o)
	tiff = binary.LittleEndian.AppendUint16(tiff, 0)
	tiff = binary.LittleEndian.AppendUint32(tiff, 0)

	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	seg = append(seg, payload...)

	out := make([]byte, 0, len(jpg)+len(seg))
	out = append(out, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, jpg[2:]...)
	return out
}

func colorAt(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func nearRGBA(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}
