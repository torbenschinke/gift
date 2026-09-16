package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The tests of [ui.Window] and of the two-level colour hierarchy it makes
// real. See window.go for why an application has to paint its own background
// at all.

// TestWindowFillsTheWholeViewportWithTheBackgroundRole is the claim of
// [ui.Window] stated as an operation in the display list: one fill, in the
// theme's background colour, exactly the size of the viewport, underneath
// everything the content draws.
//
// It is headless on purpose. The defect this function exists for was found in
// pixels, but the fix is a view, and a view is assertable without a GPU —
// which is what makes this run in `go test ./...` rather than only under
// giftgpu.
func TestWindowFillsTheWholeViewportWithTheBackgroundRole(t *testing.T) {
	size := geom.Sz(240, 160)
	h := gifttest.New(t, gifttest.Options{
		View:  ui.Window(ui.Box().Frame(20, 20).Background(ui.ColorSurface)),
		Size:  size,
		Theme: ui.LightTheme(),
	})
	want := ui.LightTheme().Color(ui.ColorBackground)

	n := 0
	first := -1
	for i, op := range h.Ops() {
		if op.Kind != render.OpFillRect || op.Color != want {
			continue
		}
		if op.Bounds.Size() != size {
			t.Errorf("the background fill is %v, want the whole %v viewport", op.Bounds.Size(), size)
		}
		if first < 0 {
			first = i
		}
		n++
	}
	if n != 1 {
		t.Fatalf("%d operations paint the background colour, want exactly 1", n)
	}
	if first != 0 {
		t.Errorf("the background fill is operation %d of the frame; it has to be the first, "+
			"or something the application drew is underneath it", first)
	}
}

// TestWindowPutsItsContentOnTopInOrder is the other half: [ui.Window] is a
// [ui.ZStack] with one extra child at the bottom, so the content keeps the Z
// order it was given and the modifiers of an Overlay still apply.
func TestWindowPutsItsContentOnTopInOrder(t *testing.T) {
	lower := ui.RGB(10, 20, 30)
	upper := ui.RGB(40, 50, 60)
	h := gifttest.New(t, gifttest.Options{
		View: ui.Window(
			ui.Box().Key("lower").Frame(40, 40).Background(lower),
			ui.Box().Key("upper").Frame(20, 20).Background(upper),
		),
		Size:  geom.Sz(80, 80),
		Theme: ui.LightTheme(),
	})
	order := map[render.Color]int{}
	for i, op := range h.Ops() {
		if op.Kind != render.OpFillRect {
			continue
		}
		if _, seen := order[op.Color]; !seen {
			order[op.Color] = i
		}
	}
	bg, okBG := order[ui.LightTheme().Color(ui.ColorBackground)]
	lo, okLo := order[lower]
	up, okUp := order[upper]
	if !okBG || !okLo || !okUp {
		t.Fatalf("not all three fills are in the frame: background=%v lower=%v upper=%v",
			okBG, okLo, okUp)
	}
	if !(bg < lo && lo < up) {
		t.Errorf("the paint order is background=%d lower=%d upper=%d, want strictly increasing",
			bg, lo, up)
	}
}

// TestTheBackgroundRoleIsBehindTheSurfaceRoleAndDiffersFromIt is the role
// audit as a test.
//
// [ui.ColorBackground] is the page and [ui.ColorSurface] is what is raised on
// it. Until [ui.Window] existed nothing in this module painted the page at
// all, the distinction lived only in the palette, and tinting the background
// role at runtime changed zero pixels on all four screens of the kitchen sink
// demo. This asserts the two halves that made that possible: the two roles are
// different colours in both built-in themes, and a card really is drawn on top
// of the page rather than instead of it.
func TestTheBackgroundRoleIsBehindTheSurfaceRoleAndDiffersFromIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		theme ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		bg := tc.theme.Color(ui.ColorBackground)
		sf := tc.theme.Color(ui.ColorSurface)
		if bg == sf {
			t.Errorf("in the %s theme the background and the surface are the same colour %v; "+
				"the two-level hierarchy those roles exist for is flat", tc.name, bg)
		}
	}
	h := gifttest.New(t, gifttest.Options{
		View:  ui.Window(ui.Card(ui.Text("x").Font(windowFont(t))).Key("card")),
		Size:  geom.Sz(200, 120),
		Theme: ui.LightTheme(),
	})
	light := ui.LightTheme()
	page, card := -1, -1
	for i, op := range h.Ops() {
		// Both fill kinds: a card has a corner radius and is therefore an
		// OpFillRoundRect, and the page is a plain OpFillRect.
		if op.Kind != render.OpFillRect && op.Kind != render.OpFillRoundRect {
			continue
		}
		if op.Color == light.Color(ui.ColorBackground) && page < 0 {
			page = i
		}
		if op.Color == light.Color(ui.ColorSurface) && card < 0 {
			card = i
		}
	}
	switch {
	case page < 0:
		t.Fatal("nothing paints the background role; ui.Window did not reach the display list")
	case card < 0:
		t.Fatal("nothing paints the surface role; the card is missing from the fixture")
	case page > card:
		t.Errorf("the page is painted at %d and the card at %d; the card is underneath the page",
			page, card)
	}
}

// TestATextFieldWearsTheControlFaceAndNotTheSurfaceOne is the second
// correction of the role audit.
//
// Every other control in this package spells its resting face
// [ui.ColorControl]: a button, the track of a slider, the tray of a segmented
// control, the off state of a toggle, a key of the on-screen keyboard. A text
// field used [ui.ColorSurface], which is the colour of the card it usually
// sits in — identically so in the dark theme — so the only thing separating a
// field from its container was the hairline around it.
func TestATextFieldWearsTheControlFaceAndNotTheSurfaceOne(t *testing.T) {
	theme := ui.DarkTheme()
	h := gifttest.New(t, gifttest.Options{
		View: ui.Window(ui.Card(
			ui.TextField(ui.NewTextEditor("name")).Key("field").Frame(120, geom.Unbounded()),
		).Key("card")),
		Size:  geom.Sz(220, 120),
		Theme: theme,
		Font:  windowFont(t),
	})
	got, ok := h.Find(gifttest.ByKey("field")).Background()
	if !ok {
		t.Fatal("the field paints no background at all")
	}
	if want := theme.Color(ui.ColorControl); got != want {
		t.Errorf("the field's face is %v, want ColorControl %v", got, want)
	}
	if surface := theme.Color(ui.ColorSurface); got == surface {
		t.Errorf("the field's face is the surface colour %v, which is the card it sits on",
			surface)
	}
}

// windowFont is the typeface the two fixtures above that draw text are
// measured with, named rather than inherited so that these tests do not depend
// on what another file in this package left as the process default.
func windowFont(t *testing.T) ui.Font {
	t.Helper()
	return ui.MustFont(ui.FontQuery{Family: inter.Family})
}
