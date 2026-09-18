package asset_test

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"testing"

	"github.com/worldiety/gift/asset"
)

// benchPicture is a representative photograph: six megapixels, the size a
// phone or a compact camera produces, which is what a real gallery is full of.
const benchW, benchH = 3000, 2000

// BenchmarkColdDecode measures one full cold request: open, probe, decode,
// scale, orient. It is the number the "kalter Cache" scenario of the project
// plan, section 13, is built out of.
func BenchmarkColdDecode(b *testing.B) {
	for _, rung := range []int{128, 256, 512} {
		b.Run(fmt.Sprintf("rung%d", rung), func(b *testing.B) {
			dir := b.TempDir()
			data := jpegBytes(b, benchW, benchH)
			c := newCollector()
			cfg := asset.Config{Workers: 1, Sizes: []int{rung}, Deliver: c.Deliver}
			p := asset.NewPipeline(cfg)
			defer p.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				// A fresh file per iteration, so nothing is a cache
				// hit. Writing it is not part of the measurement.
				b.StopTimer()
				path := writeFile(b, dir, fmt.Sprintf("p%d.jpg", i), data)
				b.StartTimer()
				p.Request(asset.Request{Source: asset.File(path), Size: rung,
					Priority: asset.Visible, OnResult: c.onResult})
				c.waitOne(b)
			}
			b.StopTimer()
			b.ReportMetric(float64(benchW*benchH)/1e6, "MPixel/op")
		})
	}
}

// BenchmarkWarmMemory is the same request when the CPU pixel cache already has
// the answer: the frame path's cost.
func BenchmarkWarmMemory(b *testing.B) {
	dir := b.TempDir()
	path := writeFile(b, dir, "a.jpg", jpegBytes(b, benchW, benchH))
	c := newCollector()
	p := asset.NewPipeline(asset.Config{Workers: 1, Sizes: []int{256}, Deliver: c.Deliver})
	defer p.Close()
	src := asset.File(path)
	p.Request(asset.Request{Source: src, Size: 256, Priority: asset.Visible, OnResult: c.onResult})
	c.waitOne(b)

	id := src.Metadata().ID
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		t, ok := p.Lookup(id, 256)
		if !ok {
			b.Fatal("miss")
		}
		t.Release()
	}
}

// BenchmarkDiskHit measures a cold *process* with a warm disk cache: the
// entry is read and checked, nothing is decoded. Compare with
// BenchmarkColdDecode/rung256 for what the persistent cache buys.
func BenchmarkDiskHit(b *testing.B) {
	pics := b.TempDir()
	cache := b.TempDir()
	path := writeFile(b, pics, "a.jpg", jpegBytes(b, benchW, benchH))
	c := newCollector()
	cfg := asset.Config{Workers: 1, Sizes: []int{256}, Deliver: c.Deliver,
		Disk: asset.DiskCacheConfig{Dir: cache}}
	warm := asset.NewPipeline(cfg)
	warm.Request(asset.Request{Source: asset.File(path), Size: 256,
		Priority: asset.Visible, OnResult: c.onResult})
	c.waitOne(b)
	warm.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		p := asset.NewPipeline(cfg)
		b.StartTimer()
		p.Request(asset.Request{Source: asset.File(path), Size: 256,
			Priority: asset.Visible, OnResult: c.onResult})
		c.waitOne(b)
		b.StopTimer()
		if st := p.Stats(); st.Decodes != 0 {
			b.Fatalf("decoded %d times, the disk cache did not answer", st.Decodes)
		}
		p.Close()
		b.StartTimer()
	}
}

// BenchmarkRawDecode is the reference point: image/jpeg alone, with no
// pipeline around it. The difference to BenchmarkColdDecode is everything gift
// adds — open, probe, budget reservations, scaling, orientation, cache and
// delivery — and it is the honest way to say how much of the cost is ours.
func BenchmarkRawDecode(b *testing.B) {
	data := jpegBytes(b, benchW, benchH)
	b.ReportAllocs()
	for b.Loop() {
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
		_ = img.Bounds()
	}
}
