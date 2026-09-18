//go:build giftgpu

package ebiten

import (
	"image/color"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"

	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// The pixel half of the image path. Everything about admission, eviction and
// accounting is in textures_test.go and needs no graphics context; what needs
// one is the claim that the pixels handed to Acquire are the pixels that reach
// the framebuffer.

// checker builds a premultiplied RGBA payload of four solid quadrants, so that
// a readback can say not only "a picture" but "this picture, this way up".
func checker(n int, tl, tr, bl, br color.RGBA) render.Pixels {
	px := render.Pixels{Pix: make([]byte, n*n*4), W: n, H: n, Stride: n * 4}
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
			i := y*px.Stride + x*4
			px.Pix[i], px.Pix[i+1], px.Pix[i+2], px.Pix[i+3] = c.R, c.G, c.B, c.A
		}
	}
	return px
}

var (
	red   = color.RGBA{255, 0, 0, 255}
	green = color.RGBA{0, 255, 0, 255}
	blue  = color.RGBA{0, 0, 255, 255}
	white = color.RGBA{255, 255, 255, 255}
)

// drawImages renders a list built after the textures have been uploaded, which
// is the real order: BeginFrame resets the upload budget, the painters spend
// it, and the draw calls follow.
func drawImages(t *testing.T, w, h int, bg color.RGBA, build func(r *Renderer, l *render.List)) *eb.Image {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	dst := eb.NewImage(w, h)
	dst.Fill(bg)
	var l render.List
	l.Reset()
	r.SetTarget(dst)
	r.BeginFrame(geom.Sz(float32(w), float32(h)))
	build(r, &l)
	r.Submit(&l)
	r.EndFrame()
	return dst
}

// TestImageReachesTheFramebufferTheRightWayUp is the end to end check of
// render.OpImage: the four quadrants come out where they went in, so neither
// the texture coordinates nor the vertex order is flipped.
func TestImageReachesTheFramebufferTheRightWayUp(t *testing.T) {
	dst := drawImages(t, 64, 64, color.RGBA{0, 0, 0, 255}, func(r *Renderer, l *render.List) {
		h, ok := r.Textures().Acquire(checker(16, red, green, blue, white))
		if !ok {
			t.Fatal("the upload was refused")
		}
		id, _ := r.Textures().Resolve(h)
		l.Add(render.Op{
			Kind: render.OpImage, Bounds: geom.Rc(0, 0, 64, 64),
			Color: render.RGB(255, 255, 255), Image: id,
		})
	})
	for _, c := range []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"top left", 8, 8, red},
		{"top right", 56, 8, green},
		{"bottom left", 8, 56, blue},
		{"bottom right", 56, 56, white},
	} {
		got := dst.At(c.x, c.y).(color.RGBA)
		if !near(got, c.want, 4) {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestImageTintIsPremultipliedOverItsBackground is the colour convention of
// the project plan, section 8, on the image material.
//
// A fully opaque white texture drawn with a half transparent white tint over
// an opaque blue background must blend to exactly half of each, because
// render.Color is premultiplied, the vertex colour scale is premultiplied and
// asset.Thumbnail produces premultiplied pixels. If any of the three were
// straight alpha the result would be visibly wrong and this says by how much.
//
//	out = src + dst*(1-src.a) = (0.5, 0.5, 0.5) + (0, 0, 0.5) = (128, 128, 255)
func TestImageTintIsPremultipliedOverItsBackground(t *testing.T) {
	dst := drawImages(t, 32, 32, color.RGBA{0, 0, 255, 255}, func(r *Renderer, l *render.List) {
		h, _ := r.Textures().Acquire(checker(8, white, white, white, white))
		id, _ := r.Textures().Resolve(h)
		l.Add(render.Op{
			Kind: render.OpImage, Bounds: geom.Rc(0, 0, 32, 32),
			Color: render.RGBA(255, 255, 255, 128), Image: id,
		})
	})
	got := dst.At(16, 16).(color.RGBA)
	want := color.RGBA{128, 128, 255, 255}
	if !near(got, want, 4) {
		t.Errorf("half transparent white over blue = %v, want %v", got, want)
	}
}

// TestImageCropIsAClip checks the decision recorded on render.OpImage: there
// is no source rectangle, and a cover crop is an oversized destination under a
// clip. The left half of a two colour texture drawn twice as wide under a clip
// of the left half must show only the first colour.
func TestImageCropIsAClip(t *testing.T) {
	dst := drawImages(t, 32, 32, color.RGBA{0, 0, 0, 255}, func(r *Renderer, l *render.List) {
		h, _ := r.Textures().Acquire(checker(8, red, green, red, green))
		id, _ := r.Textures().Resolve(h)
		l.PushClip(geom.Rc(0, 0, 16, 32))
		l.Add(render.Op{
			Kind: render.OpImage, Bounds: geom.Rc(0, 0, 32, 32),
			Color: render.RGB(255, 255, 255), Image: id,
			Clip: l.CurrentClip(),
		})
		l.PopClip()
	})
	if got := dst.At(8, 16).(color.RGBA); !near(got, red, 4) {
		t.Errorf("inside the clip = %v, want red", got)
	}
	if got := dst.At(24, 16).(color.RGBA); !near(got, color.RGBA{0, 0, 0, 255}, 4) {
		t.Errorf("outside the clip = %v, want the background", got)
	}
}

// TestReuploadAfterEvictionDrawsTheNewPixels is the pixel half of the
// eviction round trip: the same slot, a different picture, and no trace of the
// old one.
func TestReuploadAfterEvictionDrawsTheNewPixels(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	// Room for one 8x8 picture and no more.
	r.SetTextures(NewTextureCache(TextureConfig{
		MaxBytes: 300, UploadsPerFrame: -1, UploadBytesPerFrame: -1,
	}))
	dst := eb.NewImage(32, 32)

	draw := func(px render.Pixels) color.RGBA {
		var l render.List
		l.Reset()
		dst.Fill(color.RGBA{0, 0, 0, 255})
		r.SetTarget(dst)
		r.BeginFrame(geom.Sz(32, 32))
		h, ok := r.Textures().Acquire(px)
		if !ok {
			t.Fatal("the upload was refused")
		}
		id, _ := r.Textures().Resolve(h)
		l.Add(render.Op{Kind: render.OpImage, Bounds: geom.Rc(0, 0, 32, 32),
			Color: render.RGB(255, 255, 255), Image: id})
		r.Submit(&l)
		r.EndFrame()
		return dst.At(16, 16).(color.RGBA)
	}

	if got := draw(checker(8, red, red, red, red)); !near(got, red, 4) {
		t.Fatalf("first picture = %v, want red", got)
	}
	if got := draw(checker(8, green, green, green, green)); !near(got, green, 4) {
		t.Errorf("after eviction and re-upload = %v, want green; the old texture is still being "+
			"drawn, or the new pixels went somewhere else", got)
	}
	if n := r.Textures().Stats().Deallocations; n != 1 {
		t.Errorf("Deallocations = %d, want 1", n)
	}
}

func near(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}
