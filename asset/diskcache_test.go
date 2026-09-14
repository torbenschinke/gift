package asset_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/torbenschinke/gift/asset"
)

func diskConfig(c *collector, dir string, budget int64) asset.Config {
	cfg := baseConfig(c)
	cfg.Workers = 1
	cfg.Sizes = []int{64}
	cfg.Disk = asset.DiskCacheConfig{Dir: dir, Budget: budget}
	return cfg
}

// A second pipeline over the same directory serves the thumbnail without
// decoding anything, which is the whole point of a persistent cache.
func TestDiskCacheMissThenHitAcrossPipelines(t *testing.T) {
	pics := t.TempDir()
	cache := t.TempDir()
	path := writeFile(t, pics, "a.png", pngBytes(t, 300, 300))

	c := newCollector()
	p1 := asset.NewPipeline(diskConfig(c, cache, 0))
	p1.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if r.FromDisk {
		t.Error("the first request must be a miss")
	}
	st := p1.Stats()
	if st.Disk.Writes != 1 {
		t.Errorf("Disk.Writes = %d, want 1", st.Disk.Writes)
	}
	p1.Close()

	c.reset()
	p2 := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p2.Close()
	p2.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r2 := c.waitFor(t, 1)[0]
	if r2.Err() != nil {
		t.Fatal(r2.Err())
	}
	if !r2.FromDisk {
		t.Fatal("the second pipeline decoded again instead of reading the cache")
	}
	if st := p2.Stats(); st.Decodes != 0 {
		t.Errorf("Decodes = %d after a disk hit, want 0", st.Decodes)
	}
	if r2.Image.Width() != r.Image.Width() || r2.Image.Height() != r.Image.Height() {
		t.Errorf("cached thumbnail is %dx%d, the decoded one was %dx%d",
			r2.Image.Width(), r2.Image.Height(), r.Image.Width(), r.Image.Height())
	}
	// Metadata survives too: a disk hit must still be able to correct the
	// gallery's guessed aspect ratio.
	if r2.Metadata.Width != 300 || r2.Metadata.Height != 300 {
		t.Errorf("metadata after a disk hit = %dx%d", r2.Metadata.Width, r2.Metadata.Height)
	}
}

// A changed file is a changed revision, and a changed revision is a different
// key. The old entry must not be served.
func TestDiskCacheRevalidatesAChangedFile(t *testing.T) {
	pics := t.TempDir()
	cache := t.TempDir()
	path := writeFile(t, pics, "a.png", pngBytes(t, 400, 200))

	c := newCollector()
	p := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p.Close()
	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	first := c.waitFor(t, 1)[0]
	if first.Err() != nil {
		t.Fatal(first.Err())
	}

	// Replace the picture with one of a different shape. A file source is
	// revalidated by mtime and size.
	writeFile(t, pics, "a.png", pngBytes(t, 200, 400))
	if err := os.Chtimes(path, nowPlus(), nowPlus()); err != nil {
		t.Fatal(err)
	}
	c.reset()
	p.Invalidate(asset.File(path).Metadata().ID)
	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	second := c.waitFor(t, 1)[0]
	if second.Err() != nil {
		t.Fatal(second.Err())
	}
	if second.Metadata.Revision == first.Metadata.Revision {
		t.Fatal("the revision did not change although the file did")
	}
	if second.Metadata.Width != 200 || second.Metadata.Height != 400 {
		t.Errorf("served the old picture: %dx%d", second.Metadata.Width, second.Metadata.Height)
	}
}

func TestDiskCacheEvictsByBudget(t *testing.T) {
	pics := t.TempDir()
	cache := t.TempDir()
	c := newCollector()
	// A 64 rung thumbnail of a square picture is 64*64*4 = 16 384 bytes plus
	// a 32 byte header. Three fit in 50 000.
	p := asset.NewPipeline(diskConfig(c, cache, 50000))
	defer p.Close()

	const n = 10
	for i := range n {
		path := writeFile(t, pics, fmt.Sprintf("e%02d.png", i), pngBytes(t, 100+i, 100+i))
		p.Request(asset.Request{Source: asset.File(path), Size: 64,
			Priority: asset.Visible, Generation: uint64(i), OnResult: c.onResult})
		c.waitFor(t, i+1)
	}
	st := p.Stats()
	if st.Disk.Bytes > st.Disk.Budget {
		t.Errorf("disk cache holds %d bytes over a budget of %d", st.Disk.Bytes, st.Disk.Budget)
	}
	if st.Disk.Evictions == 0 {
		t.Error("ten entries into a three entry budget evicted nothing")
	}
	// And the files really are gone, not merely forgotten.
	var onDisk int64
	_ = filepath.Walk(cache, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(fi.Name(), ".thumb") {
			onDisk += fi.Size()
		}
		return nil
	})
	if onDisk > st.Disk.Budget {
		t.Errorf("%d bytes of thumbnails on disk over a budget of %d", onDisk, st.Disk.Budget)
	}
	t.Logf("disk: %d bytes in %d entries, %d evictions", st.Disk.Bytes, st.Disk.Entries, st.Disk.Evictions)
}

// A corrupt cache file is a miss, not garbage on the screen, and it is removed.
func TestDiskCacheCorruptionIsTreatedAsAMiss(t *testing.T) {
	pics := t.TempDir()
	cache := t.TempDir()
	path := writeFile(t, pics, "a.png", pngBytes(t, 200, 200))

	c := newCollector()
	p1 := asset.NewPipeline(diskConfig(c, cache, 0))
	p1.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	c.waitFor(t, 1)
	p1.Close()

	// Flip a byte in the payload of every entry.
	var touched int
	_ = filepath.Walk(cache, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".thumb") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || len(b) < 100 {
			return nil
		}
		b[len(b)-1] ^= 0xFF
		if err := os.WriteFile(p, b, 0o644); err == nil {
			touched++
		}
		return nil
	})
	if touched == 0 {
		t.Fatal("found no cache file to corrupt")
	}

	c.reset()
	p2 := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p2.Close()
	p2.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	r := c.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatalf("a corrupt cache entry broke the request: %v", r.Err())
	}
	if r.FromDisk {
		t.Error("a corrupt entry was served as a hit")
	}
	st := p2.Stats()
	if st.Disk.Corrupt == 0 {
		t.Error("the corruption was not counted")
	}
	if st.Decodes != 1 {
		t.Errorf("Decodes = %d, want 1: the picture had to be decoded again", st.Decodes)
	}
}

// A truncated file is the other half of the same case: a crash during a write.
func TestDiskCacheTruncationIsTreatedAsAMiss(t *testing.T) {
	pics := t.TempDir()
	cache := t.TempDir()
	path := writeFile(t, pics, "a.png", pngBytes(t, 200, 200))
	c := newCollector()
	p1 := asset.NewPipeline(diskConfig(c, cache, 0))
	p1.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	c.waitFor(t, 1)
	p1.Close()

	_ = filepath.Walk(cache, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".thumb") {
			_ = os.Truncate(p, fi.Size()/2)
		}
		return nil
	})

	c.reset()
	p2 := asset.NewPipeline(diskConfig(c, cache, 0))
	defer p2.Close()
	p2.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; r.Err() != nil || r.FromDisk {
		t.Errorf("truncated entry: err=%v fromDisk=%v", r.Err(), r.FromDisk)
	}
}

// Switching the disk cache off means nothing is created, read or written.
func TestDiskCacheCanBeSwitchedOff(t *testing.T) {
	pics := t.TempDir()
	cache := filepath.Join(t.TempDir(), "unused")
	path := writeFile(t, pics, "a.png", pngBytes(t, 200, 200))

	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	cfg.Disk = asset.DiskCacheConfig{} // off
	p := asset.NewPipeline(cfg)
	defer p.Close()

	for range 3 {
		p.Request(asset.Request{Source: asset.File(path), Size: 64,
			Priority: asset.Visible, OnResult: c.onResult})
	}
	res := c.waitFor(t, 3)
	for _, r := range res {
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		if r.FromDisk {
			t.Error("a disabled disk cache produced a hit")
		}
	}
	if st := p.Stats(); st.Disk.Enabled {
		t.Error("Disk.Enabled is true although no directory was configured")
	}
	if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a disabled cache created %s", cache)
	}
}

// --- cache keys --------------------------------------------------------------

// Every component of the key changes the file name, and nothing else does.
func TestCacheKeyComponentsEachChangeTheKey(t *testing.T) {
	base := asset.Key{
		Namespace: "ns1", ID: "file:///a.jpg", Revision: "r1",
		Size: 256, Orientation: asset.OrientationTopLeft, ProcessingVersion: 1,
	}
	variants := map[string]asset.Key{
		"revision":   {Namespace: "ns1", ID: "file:///a.jpg", Revision: "r2", Size: 256, Orientation: asset.OrientationTopLeft, ProcessingVersion: 1},
		"size":       {Namespace: "ns1", ID: "file:///a.jpg", Revision: "r1", Size: 128, Orientation: asset.OrientationTopLeft, ProcessingVersion: 1},
		"orientaton": {Namespace: "ns1", ID: "file:///a.jpg", Revision: "r1", Size: 256, Orientation: asset.OrientationRightTop, ProcessingVersion: 1},
		"processing": {Namespace: "ns1", ID: "file:///a.jpg", Revision: "r1", Size: 256, Orientation: asset.OrientationTopLeft, ProcessingVersion: 2},
		"namespace":  {Namespace: "ns2", ID: "file:///a.jpg", Revision: "r1", Size: 256, Orientation: asset.OrientationTopLeft, ProcessingVersion: 1},
		"id":         {Namespace: "ns1", ID: "file:///b.jpg", Revision: "r1", Size: 256, Orientation: asset.OrientationTopLeft, ProcessingVersion: 1},
	}
	baseName := asset.FileNameForTest(base)
	seen := map[string]string{baseName: "base"}
	for name, v := range variants {
		got := asset.FileNameForTest(v)
		if prev, dup := seen[got]; dup {
			t.Errorf("changing the %s did not change the key (collides with %s)", name, prev)
		}
		seen[got] = name
		if got == baseName {
			t.Errorf("changing the %s did not change the key", name)
		}
	}
	// An unknown orientation and an explicit top-left produce the same
	// pixels and therefore share an entry. That is deliberate.
	unknown := base
	unknown.Orientation = asset.OrientationUnknown
	if asset.FileNameForTest(unknown) != baseName {
		t.Error("unknown and top-left orientation must share a cache entry")
	}
}

// nowPlus is a modification time in the future, so that a rewritten file is
// certain to have a different mtime even on a coarse clock.
func nowPlus() time.Time { return time.Now().Add(2 * time.Second) }

// A second request for the same picture is answered from the CPU pixel cache:
// no probe result is needed beyond the revision, nothing is decoded, nothing is
// read from disk.
func TestMemoryCacheAnswersTheSecondRequest(t *testing.T) {
	pics := t.TempDir()
	path := writeFile(t, pics, "a.png", pngBytes(t, 300, 200))
	c := newCollector()
	cfg := baseConfig(c)
	cfg.Sizes = []int{64}
	p := asset.NewPipeline(cfg)
	defer p.Close()

	for i := range 3 {
		c.reset()
		p.Request(asset.Request{Source: asset.File(path), Size: 64,
			Priority: asset.Visible, Generation: uint64(i), OnResult: c.onResult})
		r := c.waitFor(t, 1)[0]
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		if i > 0 && !r.FromMemory {
			t.Errorf("request %d did not come from the CPU cache", i)
		}
	}
	st := p.Stats()
	if st.Decodes != 1 {
		t.Errorf("Decodes = %d, want 1", st.Decodes)
	}
	if st.MemoryHits != 2 {
		t.Errorf("MemoryHits = %d, want 2", st.MemoryHits)
	}
}

// TestCrashedWriteLeftoverIsCollected covers the leftovers of a process that
// died between the temporary write and the rename.
//
// Before WU-R the scan indexed them as if they were entries. Their names are
// not cache keys, so path() resolved ".tmp-x" into the wrong sub directory and
// remove() deleted nothing: the bytes stayed on disk for ever and the cache
// counted them against its budget for ever, which eventually evicted real
// entries to make room for garbage.
func TestCrashedWriteLeftoverIsCollected(t *testing.T) {
	pics := t.TempDir()
	cache := t.TempDir()
	path := writeFile(t, pics, "a.png", pngBytes(t, 120, 120))

	// Write one entry so that the layout on disk is the real one.
	c := newCollector()
	p := asset.NewPipeline(diskConfig(c, cache, 0))
	p.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c.onResult})
	if r := c.waitFor(t, 1)[0]; r.Err() != nil {
		t.Fatal(r.Err())
	}
	p.Close()

	// Now simulate the crash: a big temporary file next to the entry.
	var dirs []string
	root := filepath.Join(cache, "v1")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	if len(dirs) == 0 {
		t.Fatal("the disk cache wrote nothing")
	}
	junk := filepath.Join(dirs[0], ".tmp-1234567")
	if err := os.WriteFile(junk, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}

	c2 := newCollector()
	p2 := asset.NewPipeline(diskConfig(c2, cache, 0))
	defer p2.Close()
	p2.Request(asset.Request{Source: asset.File(path), Size: 64,
		Priority: asset.Visible, OnResult: c2.onResult})
	r := c2.waitFor(t, 1)[0]
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	st := p2.Stats().Disk
	if st.SweptTemps != 1 {
		t.Errorf("SweptTemps = %d, want 1", st.SweptTemps)
	}
	if _, err := os.Stat(junk); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the leftover %s was not deleted", junk)
	}
	if st.Bytes >= 1<<20 {
		t.Errorf("the cache counts %d bytes; the leftover is still in the total", st.Bytes)
	}
}
