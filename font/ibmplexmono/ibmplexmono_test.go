package ibmplexmono_test

import (
	"testing"

	"github.com/torbenschinke/gift/font/ibmplexmono"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// TestWOFF1FacesParseAndShape is the load bearing test of this package.
//
// The embedded files are WOFF version 1, and the only reason that works is
// that the loader behind font.ParseTTF happens to recognise the wOFF signature
// despite the function's name. That is an undocumented property of a dependency
// rather than a promise, so it is pinned here: if a future typesetting release
// drops the branch, this test fails on the next `go test ./...` instead of the
// package panicking from init in an application.
//
// It shapes as well as parses, because "the header was accepted" and "the
// glyph outlines and the layout tables came through" are different claims, and
// only the second one is what an importer needs.
func TestWOFF1FacesParseAndShape(t *testing.T) {
	for _, tc := range []struct {
		weight ui.FontWeight
		style  ui.FontStyle
	}{
		{ui.WeightRegular, ui.StyleNormal},
		{ui.WeightBold, ui.StyleNormal},
		{ui.WeightRegular, ui.StyleItalic},
		{ui.WeightBold, ui.StyleItalic},
	} {
		q := ui.FontQuery{Family: ibmplexmono.Family, Weight: tc.weight, Style: tc.style}
		f, ok := ui.ResolveFont(q)
		if !ok {
			t.Errorf("%s is not registered", q)
			continue
		}
		w := measure(t, f, "Hamburgefonstiv 123")
		if w <= 0 {
			t.Errorf("%s shaped to a width of %v", q, w)
		}
		t.Logf("%s shapes the sample to %.3f px at 16 px/em", q, w)
	}
}

// TestItIsAMonospace checks that the four faces are the ones the package name
// promises. A proportional font accidentally embedded here would parse, shape
// and register perfectly happily, and only look wrong.
func TestItIsAMonospace(t *testing.T) {
	f := ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family})
	narrow := measure(t, f, "iiiiiiiiii")
	wide := measure(t, f, "MMMMMMMMMM")
	if narrow != wide {
		t.Errorf("ten i are %v px and ten M are %v px, so this is not a monospace", narrow, wide)
	}
}

// TestTheStylesDiffer catches the copy-paste failure this package is most
// exposed to: four embeds of four file names, any two of which could be the
// same file. Identical widths for the upright and the italic of the same
// weight would mean one file was embedded twice.
func TestTheStylesDiffer(t *testing.T) {
	const sample = "Hamburgefonstiv"
	upright := measure(t, ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family}), sample)
	bold := measure(t, ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family, Weight: ui.WeightBold}), sample)
	if upright != bold {
		// A monospace keeps the advance across weights, so this is not a
		// failure — it is worth logging if it ever stops being true.
		t.Logf("the bold advance differs from the upright one: %v vs %v", bold, upright)
	}
	if ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family}) ==
		ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family, Style: ui.StyleItalic}) {
		t.Error("the upright and the italic are the same loaded font, so one file was embedded twice")
	}
}

// measure lays the sample out through the same path a ui.Text would, which is
// the only measurement that proves the face is usable by the toolkit rather
// than merely parsable. It pins the font through [gifttest.Options.Font], so
// it also stands as a small example of a consumer doing that.
func measure(t testing.TB, f ui.Font, s string) float32 {
	t.Helper()
	h := gifttest.New(t, gifttest.Options{
		View: ui.Text(s).FontSize(16),
		Size: geom.Sz(2000, 200),
		Font: f,
	})
	return h.Find(gifttest.ByText(s)).LayoutBounds().Size().W
}
