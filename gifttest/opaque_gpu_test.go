//go:build giftgpu

package gifttest_test

import (
	"testing"

	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// These two tests are the pixel evidence for the rule that replaced
// Options.Background: the harness paints nothing, and there is an assertion
// that says so when an application paints nothing either.

// TestTheHarnessPaintsNothingBehindTheApplication is the honesty property of
// every golden image in this module.
//
// The harness used to fill its canvas with opaque white before rendering, so a
// golden was a picture of the application plus a rectangle the application
// never drew — which is exactly the reconstruction that hid a demo with no
// window background at all behind twelve green goldens. A fresh Ebitengine
// image is transparent black, which is what Ebitengine hands a real window at
// the top of every Draw, so the two now agree.
//
// The scene is a small opaque square in the top left corner of a large
// viewport — a ZStack places an undersized child at its origin. Inside the
// square the pixels are the square; outside it they must be {0, 0, 0, 0} and
// not white.
func TestTheHarnessPaintsNothingBehindTheApplication(t *testing.T) {
	red := ui.RGB(220, 40, 40)
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(ui.Box().Key("plate").Frame(20, 20).Background(red)),
		Size: geom.Sz(60, 60),
	})
	img := h.Image()
	if img == nil {
		t.Fatal("no framebuffer")
	}
	if _, _, _, a := img.At(40, 40).RGBA(); a != 0 {
		r, g, b, _ := img.At(40, 40).RGBA()
		t.Errorf("the corner the application did not paint is {%d %d %d %d}; "+
			"the harness is contributing a pixel of its own",
			r>>8, g>>8, b>>8, a>>8)
	}
	// And the square really is there, so the test above is about the margin
	// and not about an empty frame.
	h.AssertPixel(geom.Pt(5, 5), red)
}

// TestAssertOpaqueReportsAWindowWithNoBackground is the gate on the obligation
// [ui.Window] exists to meet, tested by failing.
//
// A window with holes in it is the one defect a golden cannot report — a
// golden of a scene with holes is a perfectly stable golden — so the
// assertion has to exist separately and it has to bite. This runs it against a
// scene that paints a card and nothing behind it, which is precisely the shape
// cmd/example-kitchensink shipped in, and requires the failure to name how
// many pixels are missing and what to do about it.
func TestAssertOpaqueReportsAWindowWithNoBackground(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{
			View: ui.ZStack(ui.Box().Frame(20, 20).Background(ui.RGB(220, 40, 40))),
			Size: geom.Sz(60, 60),
		})
		h.AssertOpaque()
	})
	if len(r.errors) != 1 {
		t.Fatalf("want exactly one error from a window with no background, got %d: %v",
			len(r.errors), r.errors)
	}
	wantContains(t, "the AssertOpaque failure", r.errors[0],
		"not fully opaque",
		"of 3600 pixels",
		"ui.Window",
	)
	t.Logf("the message a developer sees:\n%s", r.errors[0])
}

// TestAssertOpaqueAcceptsAWindowThatPaintsItsBackground is the other half: the
// same scene wrapped in [ui.Window] passes, so the assertion is not simply
// "always fails on a small view".
func TestAssertOpaqueAcceptsAWindowThatPaintsItsBackground(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{
			View: ui.Window(ui.Box().Frame(20, 20).Background(ui.RGB(220, 40, 40))),
			Size: geom.Sz(60, 60),
		})
		h.AssertOpaque()
	})
	if len(r.errors) != 0 || len(r.fatals) != 0 {
		t.Fatalf("ui.Window should satisfy AssertOpaque; errors=%v fatals=%v", r.errors, r.fatals)
	}
}
