package text

import (
	"fmt"
	"testing"

	"github.com/torbenschinke/gift/geom"
)

// TestCacheHitIsAllocationFree is the assertion behind the allocation boundary
// of the package documentation and of the project plan, section 11: text
// measurement runs during layout, and layout is in the zero allocation frame
// path, so a warm measurement must not allocate at all.
func TestCacheHitIsAllocationFree(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	reqs := []Request{
		{Text: "Library", Font: f, Size: 24, MaxWidth: geom.Unbounded()},
		{Text: "the quick brown fox jumps over the lazy dog", Font: f, Size: 13, MaxWidth: 120},
		{Text: "line one\nline two", Font: f, Size: 16, MaxWidth: 200},
		{Text: "", Font: f, Size: 16, MaxWidth: 200},
	}
	for _, r := range reqs { // warm up
		s.Layout(r)
	}
	before := s.Stats()

	var sink geom.Size
	var lines int
	got := testing.AllocsPerRun(200, func() {
		for _, r := range reqs {
			sink = sink.Add(s.Measure(r))
			lines += s.Layout(r).LineCount()
		}
	})
	if got != 0 {
		t.Errorf("a warm cache hit allocated %v times per run, want 0", got)
	}
	_ = sink
	if lines == 0 {
		t.Fatal("the measurements were optimised away")
	}
	if after := s.Stats(); after.Misses != before.Misses {
		t.Errorf("%d misses during the warm run", after.Misses-before.Misses)
	}
}

// TestTickIsAllocationFree covers the other method that runs once per frame.
func TestTickIsAllocationFree(t *testing.T) {
	f := loadRoboto(t)
	s := NewShaper(Config{MaxAge: 4})
	s.Layout(Request{Text: "x", Font: f, Size: 16, MaxWidth: geom.Unbounded()})
	if got := testing.AllocsPerRun(100, s.Tick); got != 0 {
		t.Errorf("Tick allocated %v times per run, want 0", got)
	}
}

func TestCacheCounters(t *testing.T) {
	f := loadRoboto(t)
	s := newTestShaper()
	req := Request{Text: "counted", Font: f, Size: 16, MaxWidth: geom.Unbounded()}

	if st := s.Stats(); st.HitRatio() != 0 || st.Hits != 0 || st.Misses != 0 {
		t.Fatalf("fresh shaper reports %+v", st)
	}
	s.Layout(req) // miss
	for range 3 {
		s.Layout(req) // hits
	}
	st := s.Stats()
	if st.Misses != 1 || st.Hits != 3 {
		t.Fatalf("hits %d misses %d, want 3 and 1", st.Hits, st.Misses)
	}
	if got := st.HitRatio(); got != 0.75 {
		t.Errorf("hit ratio %v, want 0.75", got)
	}
	if st.Entries != 1 {
		t.Errorf("entries %d, want 1", st.Entries)
	}
	if st.Bytes <= 0 {
		t.Errorf("bytes %d, want a positive accounted size", st.Bytes)
	}
	if st.ShapedGlyphs != 7 {
		t.Errorf("shaped glyphs %d, want 7 for %q", st.ShapedGlyphs, req.Text)
	}
}

// TestCacheKeyDistinguishes asserts that the four components of the key really
// are part of it. A key that ignored the width limit would hand a caller the
// layout of a different column width, which is the kind of bug that only shows
// up after a resize.
func TestCacheKeyDistinguishes(t *testing.T) {
	f1, f2 := loadRoboto(t), loadRoboto(t)
	s := newTestShaper()
	base := Request{Text: "size matters", Font: f1, Size: 16, MaxWidth: 200}

	variants := []struct {
		name string
		req  Request
	}{
		{"base", base},
		{"other text", func() Request { r := base; r.Text = "size matter"; return r }()},
		{"other size", func() Request { r := base; r.Size = 17; return r }()},
		{"other width", func() Request { r := base; r.MaxWidth = 60; return r }()},
		{"other font", func() Request { r := base; r.Font = f2; return r }()},
		{"unbounded width", func() Request { r := base; r.MaxWidth = geom.Unbounded(); return r }()},
	}
	for _, v := range variants {
		s.Layout(v.req)
	}
	if st := s.Stats(); st.Misses != uint64(len(variants)) {
		t.Fatalf("%d misses for %d distinct requests: the key collapses cases it must not", st.Misses, len(variants))
	}
	// And the reverse: a size difference below the 1/64 pixel the shaper can
	// represent is deliberately *not* a distinct key, because it cannot
	// produce a different result.
	before := s.Stats().Misses
	r := base
	r.Size = 16 + 1.0/512
	s.Layout(r)
	if after := s.Stats().Misses; after != before {
		t.Errorf("a size difference of 1/512 pixel created a new entry; sizes are quantised to 1/64")
	}
}

func TestEvictionByBytes(t *testing.T) {
	f := loadRoboto(t)
	// Small enough that only a couple of entries fit.
	s := NewShaper(Config{MaxBytes: 2 * sizeofEntry})
	const n = 40
	for i := range n {
		s.Layout(Request{Text: fmt.Sprintf("entry number %d", i), Font: f, Size: 16, MaxWidth: geom.Unbounded()})
	}
	st := s.Stats()
	if st.Evictions == 0 {
		t.Fatal("nothing was evicted although the budget is tiny")
	}
	if st.Entries >= n {
		t.Fatalf("%d entries survived a budget of %d bytes", st.Entries, 2*sizeofEntry)
	}
	if st.Bytes > s.cfg.MaxBytes && st.Entries > 1 {
		t.Errorf("cache holds %d bytes over a budget of %d with %d entries", st.Bytes, s.cfg.MaxBytes, st.Entries)
	}
	// The most recent request must still be there: evicting what the caller
	// just asked for would turn every frame into a miss.
	before := s.Stats().Misses
	s.Layout(Request{Text: fmt.Sprintf("entry number %d", n-1), Font: f, Size: 16, MaxWidth: geom.Unbounded()})
	if s.Stats().Misses != before {
		t.Error("the most recently used entry was evicted")
	}
	// The oldest one must be gone.
	s.Layout(Request{Text: "entry number 0", Font: f, Size: 16, MaxWidth: geom.Unbounded()})
	if s.Stats().Misses != before+1 {
		t.Error("the least recently used entry survived")
	}
}

func TestEvictionByAge(t *testing.T) {
	f := loadRoboto(t)
	s := NewShaper(Config{MaxAge: 3})
	keep := Request{Text: "kept alive", Font: f, Size: 16, MaxWidth: geom.Unbounded()}
	drop := Request{Text: "left alone", Font: f, Size: 16, MaxWidth: geom.Unbounded()}
	s.Layout(keep)
	s.Layout(drop)

	for range 5 {
		s.Tick()
		s.Layout(keep) // used every tick, must survive
	}
	st := s.Stats()
	if st.AgeEvictions != 1 {
		t.Fatalf("age evictions %d, want exactly the unused entry", st.AgeEvictions)
	}
	if st.Entries != 1 {
		t.Fatalf("entries %d, want 1", st.Entries)
	}
	before := s.Stats().Misses
	s.Layout(keep)
	if s.Stats().Misses != before {
		t.Error("the entry used on every tick was evicted by age")
	}
	s.Layout(drop)
	if s.Stats().Misses != before+1 {
		t.Error("the unused entry was still cached")
	}
}

// TestRecycledEntryIsNotStale guards the buffer recycling in the cache: an
// evicted entry keeps its backing arrays, and a later miss reusing that slot
// must not inherit any of the old content.
func TestRecycledEntryIsNotStale(t *testing.T) {
	f := loadRoboto(t)
	s := NewShaper(Config{MaxBytes: 2 * sizeofEntry})
	long := Request{Text: "a considerably longer piece of text than the next one", Font: f, Size: 16, MaxWidth: geom.Unbounded()}
	short := Request{Text: "hi", Font: f, Size: 16, MaxWidth: geom.Unbounded()}
	wantLong := *s.Layout(long)
	for i := range 20 {
		s.Layout(Request{Text: fmt.Sprintf("filler %d", i), Font: f, Size: 16, MaxWidth: geom.Unbounded()})
	}
	p := s.Layout(short)
	if p.GlyphCount() != 2 {
		t.Fatalf("%d glyphs for %q in a recycled entry", p.GlyphCount(), short.Text)
	}
	if p.LineCount() != 1 {
		t.Fatalf("%d lines in a recycled entry", p.LineCount())
	}
	// And the long one, shaped again, is identical to the first time.
	again := s.Layout(long)
	if again.Size != wantLong.Size || again.GlyphCount() != wantLong.GlyphCount() {
		t.Errorf("reshaped %v/%d glyphs, first time %v/%d", again.Size, again.GlyphCount(), wantLong.Size, wantLong.GlyphCount())
	}
}

// BenchmarkLayoutWarm is the number the allocation contract is about.
func BenchmarkLayoutWarm(b *testing.B) {
	f := loadRoboto(b)
	s := newTestShaper()
	req := Request{Text: "the quick brown fox jumps over the lazy dog", Font: f, Size: 16, MaxWidth: 200}
	s.Layout(req)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = s.Measure(req)
	}
}

// BenchmarkLayoutCold measures the other side of the boundary. It is reported,
// not asserted: shaping new text allocates and is supposed to.
func BenchmarkLayoutCold(b *testing.B) {
	f := loadRoboto(b)
	texts := make([]string, 512)
	for i := range texts {
		texts[i] = fmt.Sprintf("the quick brown fox jumps over the lazy dog %d", i)
	}
	s := NewShaper(Config{MaxBytes: 1})
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		_ = s.Measure(Request{Text: texts[i%len(texts)], Font: f, Size: 16, MaxWidth: 200})
		i++
	}
}
