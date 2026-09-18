package inter_test

import (
	"testing"

	"github.com/worldiety/gift/font/inter"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// TestFacesParseAndShape checks that both embedded instances survive the trip
// from go:embed through the parser into the shaper. An embedded file is a
// build time asset, so the package panics from init when one is broken; this
// test is what turns that panic into a test failure instead of a failure in
// somebody's application.
func TestFacesParseAndShape(t *testing.T) {
	regular := ui.MustFont(ui.FontQuery{Family: inter.Family})
	bold := ui.MustFont(ui.FontQuery{Family: inter.Family, Weight: ui.WeightBold})

	const sample = "Hamburgefonstiv 123"
	rw := measure(t, regular, sample)
	bw := measure(t, bold, sample)
	t.Logf("regular %.3f px, bold %.3f px at 16 px/em", rw, bw)
	if rw <= 0 || bw <= 0 {
		t.Fatalf("a face shaped to a non positive width: %v, %v", rw, bw)
	}
	// Inter Bold is wider than Inter Regular by construction. If the two
	// widths were equal, the same file would have been embedded twice — the
	// one mistake two adjacent go:embed lines invite.
	if bw <= rw {
		t.Errorf("bold (%v) is not wider than regular (%v), so both embeds are probably the same file", bw, rw)
	}
}

// TestNearestWeightReachesTheBundledFaces states, as a test, what the package
// documentation claims about the weights it does not ship: asking for Medium
// or SemiBold is answered rather than refused, from the nearest face the
// package does ship.
func TestNearestWeightReachesTheBundledFaces(t *testing.T) {
	regular := ui.MustFont(ui.FontQuery{Family: inter.Family})
	bold := ui.MustFont(ui.FontQuery{Family: inter.Family, Weight: ui.WeightBold})

	for _, tc := range []struct {
		weight ui.FontWeight
		want   ui.Font
		name   string
	}{
		{ui.WeightThin, regular, "Thin falls to Regular"},
		{ui.WeightMedium, regular, "Medium is nearer 400 than 700"},
		{ui.WeightSemiBold, bold, "SemiBold is nearer 700 than 400"},
		{ui.WeightBlack, bold, "Black falls to Bold"},
	} {
		got, ok := ui.ResolveFont(ui.FontQuery{Family: inter.Family, Weight: tc.weight})
		if !ok {
			t.Errorf("%s: resolved to nothing", tc.name)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: picked the other face", tc.name)
		}
	}
}

// TestItalicIsNotSynthesised pins the decision, not the implementation: this
// package embeds no italic, and gift does not shear the upright one into a
// substitute.
func TestItalicIsNotSynthesised(t *testing.T) {
	if _, ok := ui.ResolveFont(ui.FontQuery{Family: inter.Family, Style: ui.StyleItalic}); ok {
		t.Fatal("an Inter italic resolved although none is embedded")
	}
}

// measure lays the sample out through the same path a ui.Text would, pinning
// the font through [gifttest.Options.Font] rather than through any global.
func measure(t testing.TB, f ui.Font, s string) float32 {
	t.Helper()
	h := gifttest.New(t, gifttest.Options{
		View: ui.Text(s).FontSize(16),
		Size: geom.Sz(2000, 200),
		Font: f,
	})
	return h.Find(gifttest.ByText(s)).LayoutBounds().Size().W
}
