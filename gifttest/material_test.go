package gifttest_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The harness's coverage of steps 4 and 5 of the project plan, section 12.
//
// Before this file `grep -r Glass gifttest/` found nothing: the framework's
// own public testing story could not see the glass material at all, had no
// accessor for a node's background, and had no image assertion. A harness that
// only covers the steps it was written for is a harness that will be found out
// by the first application that uses the later ones.
//
// The order follows the project plan, section 13: structural assertions first,
// because they say *why* something is wrong, then the goldens, which only ever
// say that something changed.

// panelBG is the colour behind the glass panels, and deliberately not white:
// a background the frame has to preserve has to be distinguishable from the
// transparent black a lost one would leave.
var panelBG = ui.RGB(10, 200, 10)

// glassScene is a panel over a patterned backdrop, which is the scene of the
// project plan, section 8: something underneath that the material can bend and
// blur, and margins the panel does not cover.
func glassScene(q ui.GlassQuality) gift.View {
	return ui.ZStack(
		// The backdrop: a dark plate with a bright bar across it, so that a
		// blur has an edge to smear and an unblurred sample has one to keep.
		//
		// It is deliberately smaller than the viewport. The margin it leaves
		// is [Options.Background], so the golden itself is evidence that a
		// frame containing a material composites onto the target instead of
		// replacing it; see TestAFrameWithAMaterialDoesNotDisturbTheTarget.
		ui.VStack(
			ui.Box().Frame(104, 24).Background(ui.RGB(20, 24, 34)),
			ui.Box().Frame(104, 16).Background(ui.RGB(240, 90, 40)),
			ui.Box().Frame(104, 24).Background(ui.RGB(20, 24, 34)),
		),
		ui.Box().Key("panel").Frame(88, 48).
			Background(ui.Glass().Quality(q).Blur(16).Refraction(6).Highlight(1)).
			CornerRadius(12),
	).Frame(120, 80)
}

// TestGlassIsVisibleToTheHarness is the structural half, and it runs in an
// ordinary build with no GPU: the material parameters travel in the side table
// of the display list, so everything an application asked for is readable.
func TestGlassIsVisibleToTheHarness(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: glassScene(ui.Reduced), Size: geom.Sz(120, 80), Background: panelBG,
	})

	panel := h.Find(gifttest.ByKey("panel"))
	panel.AssertMaterial(render.MaterialGlass)
	panel.AssertGlassQuality(ui.Reduced)
	if g := panel.Glass(); g.Blur != 16 || g.Refraction != 6 || g.Highlight != 1 {
		t.Errorf("the glass parameters reached the display list as %+v, want blur 16, "+
			"refraction 6 and highlight 1", g)
	}

	// The material replaces the plain background rather than joining it: the
	// two are mutually exclusive in ui's styleSpec, and a colour under a pane
	// would be a second invisible way of tinting it.
	if c, ok := panel.Background(); ok {
		t.Errorf("the panel also paints a plain background %v; a material and a colour are "+
			"mutually exclusive", c)
	}

	// And the negative: the backdrop underneath is an ordinary coloured box.
	h.Find(gifttest.ByKey("panel")).AssertMaterial(render.MaterialGlass)
}

// TestBackgroundIsVisibleToTheHarness pins the plain colour half of the same
// accessor, which nothing could assert before either.
func TestBackgroundIsVisibleToTheHarness(t *testing.T) {
	want := ui.RGB(30, 34, 44)
	h := gifttest.New(t, gifttest.Options{
		View: ui.Box().Key("plate").Frame(60, 40).Background(want).CornerRadius(6),
		Size: geom.Sz(120, 80),
	})
	plate := h.Find(gifttest.ByKey("plate"))
	plate.AssertBackground(want)
	plate.AssertMaterial(render.MaterialNone)
}

// TestGlassGolden is the pixel half, one golden per level.
//
// It is also the regression test for the scene blit. A frame containing a
// material is drawn into an offscreen and composited back onto the target; it
// used to be *copied* back, which replaced everything the harness had put
// there. Measured at the time: with no glass the corner pixel was the harness
// background {10 200 10 255}, and with glass it was {0 0 0 0} — the panel
// sampling transparent black as its own backdrop and the golden recording a
// picture no window would ever show. The corner assertions below are what
// makes the goldens beneath them trustworthy rather than merely stable.
func TestGlassGolden(t *testing.T) {
	for _, c := range []struct {
		name string
		q    ui.GlassQuality
	}{
		{"glass-reduced", ui.Reduced},
		{"glass-full", ui.Full},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{
				View: glassScene(c.q), Size: geom.Sz(120, 80), Background: panelBG,
			})
			h.Find(gifttest.ByKey("panel")).AssertGlassQuality(c.q)
			h.AssertGolden(c.name)
		})
	}
}

// TestAFrameWithAMaterialDoesNotDisturbTheTarget is the assertion the blit fix
// is owed, stated on its own rather than folded into a golden.
//
// The scene draws a small panel in the middle of a large viewport and touches
// nothing else. Every pixel outside it must still hold what the harness put
// there, and that must be true whether or not the frame contained a material —
// otherwise the same display list means two different things depending on
// whether a glass panel happens to be present.
func TestAFrameWithAMaterialDoesNotDisturbTheTarget(t *testing.T) {
	// Deliberately transparent everywhere but the panel: nothing in the
	// scene paints the margins, so anything there came from the target.
	view := ui.ZStack(
		ui.Box().Key("panel").Frame(40, 24).
			Background(ui.Glass().Quality(ui.Reduced)).CornerRadius(8),
	).Frame(120, 80)

	h := gifttest.New(t, gifttest.Options{
		View: view, Size: geom.Sz(120, 80), Background: panelBG,
	})
	h.Find(gifttest.ByKey("panel")).AssertMaterial(render.MaterialGlass)

	for _, p := range []geom.Point{
		{X: 0, Y: 0}, {X: 119, Y: 0}, {X: 0, Y: 79}, {X: 119, Y: 79}, {X: 4, Y: 40},
	} {
		h.AssertPixel(p, panelBG)
	}
}

// TestShadowGolden covers the remaining new operation kind of step 2.
func TestShadowGolden(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.ZStack(
			ui.Box().Key("card").Frame(72, 40).
				Background(ui.RGB(240, 242, 246)).CornerRadius(10).
				Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 120)}),
		).Frame(120, 80),
		Size:       geom.Sz(120, 80),
		Background: ui.RGB(40, 44, 56),
	})

	// Structural first: a shadow extends the paint bounds and neither the
	// layout size nor the hit area, which is the project plan, section 8.
	card := h.Find(gifttest.ByKey("card"))
	card.AssertSize(geomSz(72, 40))
	h.AssertOps("a blurred shadow", 1, func(op render.Op) bool {
		return op.Kind == render.OpShadow && op.Blur == 16
	})

	h.AssertGolden("shadow")
}

// TestGlyphGolden is the text kind. The counter goldens next door draw glyphs
// too, but they draw a whole application; this one is the single operation, so
// a failure points at the atlas or the glyph quad and not at a layout.
func TestGlyphGolden(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		View: ui.VStack(
			ui.Text("Glyphs").Key("label").FontSize(24).Foreground(ui.RGB(240, 242, 246)),
		).Padding(12).Background(ui.RGB(20, 24, 34)),
		Size:       geom.Sz(160, 60),
		Background: ui.RGB(20, 24, 34),
	})
	h.Find(gifttest.ByKey("label")).AssertDrawsGlyphs()
	h.AssertGolden("glyphs")
}
