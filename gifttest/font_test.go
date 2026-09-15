package gifttest_test

import (
	"testing"

	"github.com/torbenschinke/gift/font/ibmplexmono"
	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// TestOptionsFontPinsTheFontWithoutAGlobal is the test written from the
// position a consumer of this module is in: another repository, no access to
// internal/text, no way to reach the Roboto this package's own fixtures use.
//
// It is also the proof of the property [gifttest.Options.Font] exists for. The
// default font is cleared first, so the harness cannot be riding on whatever
// TestMain installed; if Options.Font were ignored, building a ui.Text would
// panic with "needs a font" and this test would fail rather than silently
// measure somebody else's typeface.
func TestOptionsFontPinsTheFontWithoutAGlobal(t *testing.T) {
	saved := ui.DefaultFont()
	ui.SetDefaultFont(ui.Font{})
	// t.Cleanup and not defer, and the difference bit once already: deferred
	// functions all run before the first t.Cleanup, so a defer here would put
	// Roboto back *before* the harness put the zero Font back, and every later
	// test in the package would then panic for want of a font. Registered as a
	// cleanup it is simply the outermost one and unwinds last.
	t.Cleanup(func() { ui.SetDefaultFont(saved) })

	const sample = "Hamburgefonstiv"
	h := gifttest.New(t, gifttest.Options{
		View: ui.Text(sample).FontSize(16),
		Size: geom.Sz(2000, 200),
		Font: ui.MustFont(ui.FontQuery{Family: inter.Family}),
	})
	w := h.Find(gifttest.ByText(sample)).LayoutBounds().Size().W
	if w <= 0 {
		t.Fatalf("the text measured %v px wide", w)
	}
	t.Logf("Inter measures the sample at %.3f px", w)
}

// TestOptionsFontDecidesTheLayout is the half that a "did it render at all"
// assertion cannot make: two harnesses over the same view, differing only in
// Options.Font, must lay the text out differently. Otherwise the field would
// be accepted, ignored, and every golden would still depend on the process
// default.
func TestOptionsFontDecidesTheLayout(t *testing.T) {
	const sample = "Hamburgefonstiv"
	width := func(f ui.Font) float32 {
		h := gifttest.New(t, gifttest.Options{
			View: ui.Text(sample).FontSize(16),
			Size: geom.Sz(2000, 200),
			Font: f,
		})
		return h.Find(gifttest.ByText(sample)).LayoutBounds().Size().W
	}
	sans := width(ui.MustFont(ui.FontQuery{Family: inter.Family}))
	mono := width(ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family}))
	if sans == mono {
		t.Fatalf("a proportional and a monospaced face measured the same %v px, "+
			"so Options.Font did not reach the layout", sans)
	}
	t.Logf("Inter %.3f px, IBM Plex Mono %.3f px", sans, mono)
}

// TestOptionsFontIsRestoredAfterTheTest checks the other side of the bargain.
// The field is documented as leaving no trace, and a harness that left its
// font installed would make the *next* test in the package depend on the
// order the tests ran in — the very defect the field was added to remove.
func TestOptionsFontIsRestoredAfterTheTest(t *testing.T) {
	before := ui.DefaultFont()

	// A nested test, because TB.Cleanup runs when the test that registered it
	// finishes, and the assertion has to happen after that.
	t.Run("inner", func(t *testing.T) {
		gifttest.New(t, gifttest.Options{
			View: ui.Text("x"),
			Font: ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family}),
		})
	})

	if after := ui.DefaultFont(); after != before {
		t.Fatal("the harness left its font installed as the process default")
	}
}

// TestOptionsFontGoldenIsStableAcrossProcesses is the golden a consumer would
// write, and the reason the whole unit exists: before Options.Font, the pixels
// below depended on which font some other file in the process had installed,
// so the image was reproducible only by accident.
//
// It runs only under the giftgpu tag, like every other golden; without it
// AssertGolden skips.
func TestOptionsFontGoldenIsStableAcrossProcesses(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Text("Consumer golden").FontSize(20),
			ui.Text("pinned to Inter Regular").FontSize(13),
		).Gap(8).Padding(16),
		Size: geom.Sz(320, 120),
		Font: ui.MustFont(ui.FontQuery{Family: inter.Family}),
	})
	h.AssertGolden("consumer-font-inter")
}
