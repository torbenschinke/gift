package gifttest_test

import (
	"testing"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
)

// TestCounterGolden is the same counter, compared as pixels.
//
// It is written exactly like the headless tests around it — no build tag, no
// second file, no conditional — and that is deliberate: a developer adds a
// screenshot assertion to a test they already have. Without the giftgpu tag
// the method skips with an explanation; with it, it renders the display list
// through the real backend and compares against testdata/counter.png.
//
//	go test -tags giftgpu ./gifttest/
//	GIFT_UPDATE_GOLDEN=1 go test -tags giftgpu ./gifttest/   # rewrite
//
// The viewport is fixed and small, because a golden of an 800x600 window is
// mostly background and takes longer to diff by eye than to regenerate.
func TestCounterGolden(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter, Size: geom.Sz(240, 200)})

	// The structural assertions come first and stand on their own. A golden
	// says "this changed", never "this is wrong"; the assertions below say
	// what is actually required, and they still work without a GPU.
	h.Find(gifttest.ByKey("count")).AssertText("0")
	h.Find(gifttest.ByKey("minus")).AssertDisabled()

	h.AssertGolden("counter-at-zero")

	h.Find(gifttest.ByKey("plus")).Click()
	h.Find(gifttest.ByKey("count")).AssertText("1")
	// The plus button is hovered and the minus button has become enabled, so
	// this frame differs from the one above in three independent places. A
	// golden that did not change here would mean the renderer is ignoring
	// interaction state.
	h.AssertGolden("counter-at-one")
}
