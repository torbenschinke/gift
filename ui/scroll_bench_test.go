package ui_test

import (
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// The two benchmarks behind the allocation contract of the project plan,
// section 11, for "reine Scroll-/Transform-Updates". TestScrollFramePathIsAllocationFree
// asserts the zero; these are what a measurement run quotes.
//
// Measured on the development machine over a two hundred row scroller with a
// 600 pixel viewport, 0 B/op and 0 allocs/op in the release, giftdebug and
// giftmetrics builds and under -race. Under -race *together with* giftdebug
// the giftdebug goroutine guard leaks its pooled stack buffer — the race
// detector defeats sync.Pool's reuse — and the numbers are 34 B/op and
// 129 B/op respectively. That combination is instrumentation and is excluded
// from the contract; see raceEnabled in buildtag_race_test.go.
func BenchmarkPureScrollFrame(b *testing.B) {
	a := gift.New(gift.Options{Root: scrollTree})
	if err := a.Update(geom.Sz(800, 600)); err != nil {
		b.Fatal(err)
	}
	now := time.Duration(0)
	d := float32(-1)
	step := func() {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		a.PointerWheel(geom.Pt(200, 300), geom.Pt(0, d))
		d = -d
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
	for range 32 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

func BenchmarkKineticScrollFrame(b *testing.B) {
	a := gift.New(gift.Options{Root: scrollTree})
	if err := a.Update(geom.Sz(800, 600)); err != nil {
		b.Fatal(err)
	}
	var sc gift.NodeRef
	var walk func(gift.NodeRef)
	walk = func(r gift.NodeRef) {
		if a.IsScrollable(r) && sc.IsZero() {
			sc = r
		}
		for _, c := range a.NodeChildren(r, nil) {
			walk(c)
		}
	}
	walk(a.Root())
	now := time.Duration(0)
	fling := func() {
		a.BeginInput(now)
		a.ScrollTo(sc, 4000)
		a.PointerDown(1, gift.PointerTouch, geom.Pt(200, 500))
		for i := 1; i <= 8; i++ {
			now += 8 * time.Millisecond
			a.BeginInput(now)
			a.PointerMove(1, gift.PointerTouch, geom.Pt(200, 500-float32(i)*20))
		}
		now += 8 * time.Millisecond
		a.BeginInput(now)
		a.PointerUp(1, gift.PointerTouch, geom.Pt(200, 340))
	}
	fling()
	step := func() {
		if info, _ := a.ScrollInfo(sc); !info.Flinging {
			fling()
		}
		now += 16 * time.Millisecond
		a.BeginInput(now)
		_ = a.Update(geom.Sz(800, 600))
		a.Paint()
	}
	for range 32 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}
