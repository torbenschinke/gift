package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// memoisedScene is a card behind a rebuild boundary.
//
// The [gift.Memo] is the whole point: its props are a constant, so nothing
// about its own inputs ever changes and gift is entitled to skip its build
// forever. Everything it draws is a semantic colour, so every pixel of it
// depends on something that is not a prop and not a state.
func memoisedScene() gift.View {
	return ui.VStack(
		ui.Text("Outside the memo").FontSize(13).Key("outside"),
		gift.Memo("panel", 0, func(*gift.Context, int) gift.View {
			return ui.Card(
				ui.Text("Inside the memo").FontSize(13).Key("inside"),
				ui.Row("A row").Value("42").Key("row"),
			).Header("Memoised").Key("card")
		}),
	).Gap(12).Padding(16).Frame(300, 200).Background(ui.ColorBackground)
}

// TestAMemoisedSubtreeIsRepaintedByAThemeSwitch is the regression for the
// defect that made this work unit necessary, in the smallest scene that can
// contain it.
//
// # The defect
//
// Colours are resolved during build and stored literally in the retained node,
// which is what keeps the frame path free of theme lookups. [ui.SetTheme] used
// to ask for a rebuild with [gift.App.Invalidate], which marks the *root*
// scope — and a root rebuild stops at the first rebuild boundary below it. A
// [gift.Memo] whose props did not change is such a boundary, so it kept the
// palette it was built with: on a real screen, a light window background with
// dark cards on it and text nobody can read. A theme is neither a prop nor a
// state, so nothing in the dependency graph could carry the change; the
// invalidation has to come from outside it, and [gift.App.InvalidateAll] is
// that.
//
// # Why this is a golden and a count
//
// The count is the assertion that works without a GPU and names the defect
// exactly: no operation in the frame may still carry a colour of the theme
// that was switched away from. The golden is the one that would have caught it
// anyway — the previous test of this behaviour counted fills by colour, and
// its own author wrote in his report that this "would pass if the surface
// colour were applied to the wrong nodes".
//
// The switched frame is compared against the golden of the *same scene built
// in the dark theme from the start*, which is the strongest form of the claim
// of the project plan, section 20: a theme changed at run time is
// indistinguishable, pixel for pixel, from the theme having been there all
// along. Nothing here is ever clicked, so the two frames have the same hover
// and focus state and the comparison is about colour and nothing else.
func TestAMemoisedSubtreeIsRepaintedByAThemeSwitch(t *testing.T) {
	dark := ui.DarkTheme()
	light := ui.LightTheme()
	font := ui.MustFont(ui.FontQuery{Family: inter.Family})

	// Born dark: the reference.
	born := gifttest.New(t, gifttest.Options{
		View:  memoisedScene(),
		Size:  geom.Sz(300, 200),
		Theme: dark,
		Font:  font,
	})
	// Born light, switched at run time. Both scenes paint their own window
	// background over the whole 300 by 200 viewport — the harness paints
	// nothing at all — so the two frames differ in the theme and in nothing
	// else, which is what makes the comparison below mean something.
	switched := gifttest.New(t, gifttest.Options{
		View:  memoisedScene(),
		Size:  geom.Sz(300, 200),
		Theme: light,
		Font:  font,
	})
	if n := opsInColour(switched, light.Color(ui.ColorSurface)); n == 0 {
		t.Fatal("the light theme's surface colour is nowhere in the frame; the fixture is wrong")
	}
	ui.SetTheme(switched.App(), dark)
	switched.Settle()

	for _, c := range []struct {
		name string
		v    render.Color
	}{
		{"ColorSurface", light.Color(ui.ColorSurface)},
		{"ColorLabel", light.Color(ui.ColorLabel)},
		{"ColorBackground", light.Color(ui.ColorBackground)},
	} {
		if n := opsInColour(switched, c.v); n != 0 {
			t.Errorf("%d operations still carry the light theme's %s after the switch to dark; "+
				"a subtree behind a rebuild boundary kept the palette it was built with",
				n, c.name)
		}
	}
	// The pixels last, so that the structural assertions above still run in a
	// build without the giftgpu tag, where a golden skips the whole test.
	born.AssertGolden("memo-theme-dark")
	switched.AssertGolden("memo-theme-dark")
}

// opsInColour counts the operations of the most recent frame drawn in c.
func opsInColour(h *gifttest.Harness, c render.Color) int {
	n := 0
	for _, op := range h.Ops() {
		if op.Color == c {
			n++
		}
	}
	return n
}
