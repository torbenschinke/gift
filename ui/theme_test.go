package ui_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The tests in this file exercise the semantic colours of the project plan,
// section 20. They all restore the installed theme, because it is process wide
// and this package's tests are deliberately not parallel; see the note on
// process wide state in the project plan, section 13.

func withTheme(t *testing.T, th ui.Theme) {
	t.Helper()
	prev := ui.CurrentTheme()
	ui.SetTheme(nil, th)
	t.Cleanup(func() { ui.SetTheme(nil, prev) })
}

// fillOps returns the colours of the solid fills of a display list, in
// emission order. It is what "what is actually drawn" means for these tests:
// an assertion on the theme variable would pass even if no widget read it.
func fillOps(l *render.List) []render.Color {
	var out []render.Color
	for _, op := range l.Ops() {
		switch op.Kind {
		case render.OpFillRect, render.OpFillRoundRect, render.OpStrokeRoundRect, render.OpGlyphs:
			out = append(out, op.Color)
		}
	}
	return out
}

func opColorAt(t *testing.T, l *render.List, i int) render.Color {
	t.Helper()
	got := fillOps(l)
	if i >= len(got) {
		t.Fatalf("the frame has %d coloured operations, wanted index %d", len(got), i)
	}
	return got[i]
}

// TestThemeSwitchChangesWhatIsDrawn is the load bearing test of this work
// unit. It asserts on the display list and not on [ui.CurrentTheme], because a
// theme nobody resolves would satisfy the second and change no pixel.
//
// It also pins the invalidation half: the switch goes through [ui.SetTheme]
// with the application handle, and the very next update must produce the new
// colours without the test touching anything else.
func TestThemeSwitchChangesWhatIsDrawn(t *testing.T) {
	withTheme(t, ui.LightTheme())

	view := ui.VStack(
		ui.Box().Key("plate").Frame(40, 20).Background(ui.ColorSurface),
	)
	a := gift.New(gift.Options{Root: static(view)})

	light := opColorAt(t, frame(t, a, geom.Sz(100, 100)), 0)
	if want := ui.LightTheme().Color(ui.ColorSurface); light != want {
		t.Fatalf("under the light theme the surface painted %v, want %v", light, want)
	}

	ui.SetTheme(a, ui.DarkTheme())
	dark := opColorAt(t, frame(t, a, geom.Sz(100, 100)), 0)
	if want := ui.DarkTheme().Color(ui.ColorSurface); dark != want {
		t.Fatalf("after SetTheme the surface painted %v, want the dark surface %v", dark, want)
	}
	if light == dark {
		t.Fatal("the surface colour did not change with the theme")
	}
}

// TestThemeSwitchWithoutAnAppDoesNotRepaint states the other half of
// [ui.SetTheme] plainly: the application handle is what makes the switch
// visible, because colours are resolved during build and a retained node holds
// the colour it was built with.
//
// A reader who expects a theme to be a live value the frame path consults
// deserves to find that expectation contradicted by a test rather than by a
// window that ignores the switch.
func TestThemeSwitchWithoutAnAppDoesNotRepaint(t *testing.T) {
	withTheme(t, ui.LightTheme())

	a := gift.New(gift.Options{Root: static(
		ui.Box().Frame(40, 20).Background(ui.ColorSurface),
	)})
	before := opColorAt(t, frame(t, a, geom.Sz(100, 100)), 0)

	ui.SetTheme(nil, ui.DarkTheme())
	after := opColorAt(t, frame(t, a, geom.Sz(100, 100)), 0)
	if after != before {
		t.Fatalf("the frame changed to %v without a rebuild; the resolved colour is supposed to "+
			"live in the retained node until the views are built again", after)
	}

	a.Invalidate()
	if got := opColorAt(t, frame(t, a, geom.Sz(100, 100)), 0); got != ui.DarkTheme().Color(ui.ColorSurface) {
		t.Fatalf("after an explicit Invalidate the surface painted %v, want the dark surface", got)
	}
}

// TestPartialStyleKeepsTheThemedRest is the unset tracking problem of the
// project plan, section 20, stated as a test: a caller who sets one field of a
// style must still get themed values for the rest.
//
// Before the set bits existed this produced a transparent background and no
// border at all — an invisible button with rounded corners. Both spellings of
// "a radius and nothing else" are covered for the reason given on
// TestPartialStyleKeepsTheThemedHoverFace: they reach the set bits by
// different routes and only one of them used to be exercised.
func TestPartialStyleKeepsTheThemedRest(t *testing.T) {
	for _, tc := range []struct {
		name string
		th   ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		for _, sp := range []struct {
			name string
			view func(testing.TB) ui.ButtonView
		}{
			{"CornerRadius", func(t testing.TB) ui.ButtonView {
				return ui.Button(textOf(t, "x"), nil).CornerRadius(11)
			}},
			{"Style", func(t testing.TB) ui.ButtonView {
				return ui.Button(textOf(t, "x"), nil).Style(ui.ButtonStyle{CornerRadius: 11})
			}},
		} {
			t.Run(tc.name+"/"+sp.name, func(t *testing.T) {
				withTheme(t, tc.th)

				l := run(t, sp.view(t), geom.Sz(200, 80))
				var fill, stroke *render.Op
				for i, op := range l.Ops() {
					switch op.Kind {
					case render.OpFillRoundRect:
						fill = &l.Ops()[i]
					case render.OpStrokeRoundRect:
						stroke = &l.Ops()[i]
					}
				}
				if fill == nil {
					t.Fatal("a button with only a corner radius painted no background at all")
				}
				if want := tc.th.Color(ui.ColorControl); fill.Color != want {
					t.Errorf("background = %v, want the themed control face %v", fill.Color, want)
				}
				if fill.CornerRadius != 11 {
					t.Errorf("corner radius = %v, want the 11 the caller asked for", fill.CornerRadius)
				}
				if stroke == nil {
					t.Fatal("a button with only a corner radius painted no border")
				}
				if want := tc.th.Color(ui.ColorSeparator); stroke.Color != want {
					t.Errorf("border = %v, want the themed separator %v", stroke.Color, want)
				}
			})
		}
	}
}

// TestPartialStyleKeepsTheThemedHoverFace covers the second half of the same
// problem, the one the old hasStyle flag got wrong: a caller who set a radius
// used to lose hover, press and disabled feedback entirely, because any style
// call at all switched the three state defaults off.
//
// Both spellings of "a radius and nothing else" are here, and that is the
// point of the table rather than decoration. They took different paths through
// the set bits: .CornerRadius(11) set only bitRadius, while
// .Style(ButtonStyle{CornerRadius: 11}) went through withThemedDefaults, which
// *invented* a background and then recorded bitBackground for it — so
// stateStyle believed the caller had named a control colour of their own and
// refused to light it up on hover. The version of this test that only
// exercised the first spelling passed throughout.
func TestPartialStyleKeepsTheThemedHoverFace(t *testing.T) {
	for _, tc := range []struct {
		name string
		view func(testing.TB) ui.ButtonView
	}{
		{"CornerRadius", func(t testing.TB) ui.ButtonView {
			return ui.Button(textOf(t, "x"), nil).CornerRadius(11)
		}},
		{"Style", func(t testing.TB) ui.ButtonView {
			return ui.Button(textOf(t, "x"), nil).Style(ui.ButtonStyle{CornerRadius: 11})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTheme(t, ui.DarkTheme())

			a := gift.New(gift.Options{Root: static(
				tc.view(t).Frame(60, 30),
			)})
			frame(t, a, geom.Sz(200, 80))

			// Move the pointer onto the button. The hover face lives in the
			// retained node, so this is a repaint and not a rebuild.
			a.BeginInput(0)
			a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(30, 15))

			got := opColorAt(t, a.Paint(), 0)
			if want := ui.DarkTheme().Color(ui.ColorControlHover); got != want {
				t.Fatalf("the hovered face is %v, want the themed hover face %v", got, want)
			}
			// And the radius the caller did ask for survived either spelling.
			if r := a.Paint().Ops()[0].CornerRadius; r != 11 {
				t.Errorf("corner radius = %v, want the 11 the caller asked for", r)
			}
		})
	}
}

// TestExplicitColourIsNotOverriddenByTheTheme is the converse, and it has to
// hold under both themes: a literal colour a caller wrote down is a decision,
// not a default.
func TestExplicitColourIsNotOverriddenByTheTheme(t *testing.T) {
	want := ui.RGB(200, 30, 90)
	for _, tc := range []struct {
		name string
		th   ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		t.Run(tc.name, func(t *testing.T) {
			withTheme(t, tc.th)

			l := run(t, ui.VStack(
				ui.Box().Key("box").Frame(10, 10).Background(want),
				ui.Button(textOf(t, "x"), nil).
					Style(ui.ButtonStyle{Background: want, CornerRadius: 4}),
				textOf(t, "y").Foreground(want),
			), geom.Sz(200, 200))

			// The box fill, the button face and the glyph colour. The
			// button's hairline is themed and is deliberately not in this
			// list: the caller named a background, not a border.
			var boxFill, buttonFace, glyphs int
			for _, op := range l.Ops() {
				if op.Color != want {
					continue
				}
				switch op.Kind {
				case render.OpFillRect:
					boxFill++
				case render.OpFillRoundRect:
					buttonFace++
				case render.OpGlyphs:
					glyphs++
				}
			}
			if boxFill != 1 || buttonFace != 1 || glyphs != 1 {
				t.Errorf("the explicit colour %v survived in %d box fills, %d button faces and "+
					"%d glyph runs; want one of each", want, boxFill, buttonFace, glyphs)
			}
		})
	}
}

// TestClearIsTransparentUnderEveryTheme pins the other end of the unset rule.
// The zero colour in a [ui.ButtonStyle] means "the theme decides"; a caller who
// wants nothing drawn says so by name.
func TestClearIsTransparentUnderEveryTheme(t *testing.T) {
	for _, th := range []ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		withTheme(t, th)
		if got := th.Color(ui.ColorClear); !got.IsTransparent() {
			t.Fatalf("ColorClear resolved to %v", got)
		}
		l := run(t, ui.Button(textOf(t, "x"), nil).
			Style(ui.ButtonStyle{Background: ui.ColorClear}), geom.Sz(200, 80))
		for _, op := range l.Ops() {
			if op.Kind == render.OpFillRect || op.Kind == render.OpFillRoundRect {
				t.Fatalf("a button with a ColorClear background still painted a fill %v", op.Color)
			}
		}
	}
}

// TestFadeFollowsALaterThemeChange pins the laziness of [ui.Fade], which is
// the property that lets [ui.DefaultScrollBar] be a package variable. A helper
// that multiplied eagerly would freeze whichever theme was installed at
// package initialisation, and nothing would ever diagnose it.
func TestFadeFollowsALaterThemeChange(t *testing.T) {
	faded := ui.Fade(ui.ColorLabel, 0.5)
	if !ui.IsSemantic(faded) {
		t.Fatal("Fade of a semantic colour produced a literal one; it would no longer follow the theme")
	}
	light := ui.LightTheme().Color(faded)
	dark := ui.DarkTheme().Color(faded)
	if light == dark {
		t.Fatalf("a faded label is %v under both themes", light)
	}
	if want := (ui.Color{A: 0.5}); light != want {
		t.Errorf("the faded light label is %v, want half of opaque black, %v", light, want)
	}
	// And the literal path still multiplies straight away, on every channel,
	// because the colour is premultiplied.
	if got, want := ui.Fade(ui.RGB(255, 255, 255), 0.5), (ui.Color{R: 0.5, G: 0.5, B: 0.5, A: 0.5}); got != want {
		t.Errorf("Fade of a literal colour = %v, want %v", got, want)
	}
}

// TestDefaultScrollBarIsThemed is the reason [ui.Fade] exists at all: a black
// scroll bar is invisible on a dark background, which is the one place a bar
// has to be found by eye. It also pins the light values against the literals
// the variable used to hold, so that the committed goldens have a reason not
// to move.
func TestDefaultScrollBarIsThemed(t *testing.T) {
	if got, want := ui.LightTheme().Color(ui.DefaultScrollBar.Thumb), ui.RGBA(0, 0, 0, 130); got != want {
		t.Errorf("the light thumb is %v, want the historical %v", got, want)
	}
	if got, want := ui.LightTheme().Color(ui.DefaultScrollBar.Track), ui.RGBA(0, 0, 0, 20); got != want {
		t.Errorf("the light track is %v, want the historical %v", got, want)
	}
	light := ui.LightTheme().Color(ui.DefaultScrollBar.Thumb)
	dark := ui.DarkTheme().Color(ui.DefaultScrollBar.Thumb)
	if light == dark {
		t.Fatal("the scroll bar thumb is the same colour on both themes; it is invisible on one of them")
	}
}

// TestNoSemanticColourReachesTheDisplayList is the safety net for the
// encoding, and it is a *visibility* vote rather than a presence vote.
//
// The distinction is the whole reason this test was rewritten. The previous
// version scanned the finished display list for an operation carrying a
// semantic colour, which reads like a thorough check and could not fail for a
// fill, a stroke or a shadow: an unresolved colour has an alpha of zero, so
// the painter's own transparency gate *removes* the operation rather than
// corrupting it. There was nothing left in the list to find. A gallery with a
// semantic palette and a glass pane with a semantic tint both passed it while
// drawing, respectively, nothing at all and saturated cyan.
//
// So each case below names the operation it expects and the themed colour it
// expects on it, and the absence of that operation is the failure. The scan
// for a leaked semantic value is kept as the second half, because it is still
// the right check for the one leak that does *not* vanish — a material tint,
// which travels in a side table and has no transparency gate in front of it.
//
// Two colour bearing widgets are deliberately not in the table. The scroll bar
// resolves through ScrollBar.withDefaults rather than through a styleSpec, and
// its emitted colour is the themed one times an animated opacity, so an
// equality vote here would either be wrong or would have to reimplement the
// fade; scrollbar_test.go votes on its visibility already and
// TestDefaultScrollBarIsThemed pins its value. The gallery tile is covered by
// TestAnUnresolvedColourIsDiagnosedRatherThanDropped instead, because a
// semantic TileStyle.Palette entry is a caller defect by contract — the slice
// belongs to the caller and the library must not resolve it — so the required
// behaviour there is a diagnosis and not a pixel.
func TestNoSemanticColourReachesTheDisplayList(t *testing.T) {
	th := ui.DarkTheme()

	// want is one operation the frame must contain: a kind and the colour it
	// must carry. colorOf pulls the colour out, which for a material means
	// the side table rather than the op.
	type want struct {
		what    string
		kind    render.OpKind
		colorOf func(*render.List, render.Op) render.Color
		color   ui.Color
	}
	opColor := func(_ *render.List, op render.Op) render.Color { return op.Color }
	tintColor := func(l *render.List, op render.Op) render.Color {
		return l.Material(op.Material).Glass.Tint
	}

	for _, tc := range []struct {
		name string
		view func(testing.TB) gift.View
		want want
	}{
		{
			// The default foreground, which no call site writes down. It is
			// the one colour a caller cannot get wrong, so if it is missing
			// the library is.
			name: "an unstyled label draws in the themed label colour",
			view: func(t testing.TB) gift.View { return textOf(t, "plain") },
			want: want{"a glyph run", render.OpGlyphs, opColor, th.Color(ui.ColorLabel)},
		},
		{
			name: "a named foreground draws",
			view: func(t testing.TB) gift.View {
				return textOf(t, "label").Foreground(ui.ColorSecondaryLabel)
			},
			want: want{"a glyph run", render.OpGlyphs, opColor, th.Color(ui.ColorSecondaryLabel)},
		},
		{
			name: "a named background fills",
			view: func(testing.TB) gift.View {
				return ui.Box().Frame(40, 20).Background(ui.ColorSurface)
			},
			want: want{"a fill", render.OpFillRect, opColor, th.Color(ui.ColorSurface)},
		},
		{
			name: "a named border strokes",
			view: func(testing.TB) gift.View {
				return ui.Box().Frame(40, 20).Border(ui.Border{Width: 1, Color: ui.ColorSeparator})
			},
			want: want{"a stroke", render.OpStrokeRoundRect, opColor, th.Color(ui.ColorSeparator)},
		},
		{
			name: "a faded named shadow is cast",
			view: func(testing.TB) gift.View {
				return ui.Box().Frame(40, 20).Background(ui.ColorSurface).
					Shadow(ui.Shadow{Blur: 4, Color: ui.Fade(ui.ColorLabel, 0.3)})
			},
			want: want{"a shadow", render.OpShadow, opColor, th.Color(ui.Fade(ui.ColorLabel, 0.3))},
		},
		{
			name: "a named button face fills",
			view: func(t testing.TB) gift.View {
				return ui.Button(textOf(t, "go"), nil).
					Style(ui.ButtonStyle{Background: ui.ColorAccent, CornerRadius: 4})
			},
			want: want{"a rounded fill", render.OpFillRoundRect, opColor, th.Color(ui.ColorAccent)},
		},
		{
			// The case that reached the GPU. A material carries its colour
			// in the side table, so it passes neither List.Add nor any
			// transparency gate, and styleSpec.resolved did not resolve it:
			// the tint arrived at the shader as {-1, 7, 1, 0} and painted
			// saturated cyan at both quality levels.
			name: "a named glass tint survives into the material table",
			view: func(testing.TB) gift.View {
				return ui.Box().Frame(40, 20).Background(ui.Glass().Tint(ui.ColorAccent))
			},
			want: want{"a material", render.OpMaterial, tintColor, th.Color(ui.ColorAccent)},
		},
		{
			// A themed colour arriving through a plain colour field of a
			// container rather than through a style struct.
			name: "a faded named background fills",
			view: func(testing.TB) gift.View {
				return ui.Box().Frame(20, 20).Background(ui.Fade(ui.ColorAccent, 0.4))
			},
			want: want{"a fill", render.OpFillRect, opColor, th.Color(ui.Fade(ui.ColorAccent, 0.4))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTheme(t, th)
			l := run(t, tc.view(t), geom.Sz(200, 200))

			found := false
			for _, op := range l.Ops() {
				if op.Kind == tc.want.kind && tc.want.colorOf(l, op) == tc.want.color {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s in the themed colour %v.\nThe frame holds:\n%s",
					tc.want.what, tc.want.color, describe(l))
			}

			// The leak scan. It is the right check for a material tint and a
			// backstop for everything else; see the comment on this test for
			// why it cannot stand alone.
			for i, op := range l.Ops() {
				if ui.IsSemantic(op.Color) {
					t.Errorf("operation %d of kind %v carries the unresolved colour %v",
						i, op.Kind, op.Color)
				}
			}
			for i := range l.MaterialsLen() {
				if c := l.Material(uint32(i)).Glass.Tint; ui.IsSemantic(c) {
					t.Errorf("material %d carries the unresolved tint %v", i, c)
				}
			}
		})
	}
}

// describe renders a display list as one line per operation, so that a failure
// above says what *was* drawn instead of only what was not. A frame with no
// operations at all is the characteristic symptom of an unresolved colour, and
// "the frame holds: nothing" is the sentence that names it.
func describe(l *render.List) string {
	if l.Len() == 0 {
		return "\tnothing — which is what an unresolved semantic colour looks like"
	}
	var b strings.Builder
	for i, op := range l.Ops() {
		fmt.Fprintf(&b, "\t%d: %v %v", i, op.Kind, op.Color)
		if op.Kind == render.OpMaterial {
			fmt.Fprintf(&b, " tint=%v", l.Material(op.Material).Glass.Tint)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestEverySemanticColourResolves guards against a role that was declared and
// never given a value in one of the two themes — a colour that would silently
// come out transparent, which is the failure mode this package works hardest
// to avoid elsewhere.
func TestEverySemanticColourResolves(t *testing.T) {
	named := map[string]ui.Color{
		"ColorLabel":           ui.ColorLabel,
		"ColorSecondaryLabel":  ui.ColorSecondaryLabel,
		"ColorBackground":      ui.ColorBackground,
		"ColorSurface":         ui.ColorSurface,
		"ColorSeparator":       ui.ColorSeparator,
		"ColorAccent":          ui.ColorAccent,
		"ColorOnAccent":        ui.ColorOnAccent,
		"ColorControl":         ui.ColorControl,
		"ColorControlHover":    ui.ColorControlHover,
		"ColorControlPressed":  ui.ColorControlPressed,
		"ColorControlDisabled": ui.ColorControlDisabled,
	}
	for _, th := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		for name, c := range named {
			got := th.t.Color(c)
			if ui.IsSemantic(got) {
				t.Errorf("%s under the %s theme resolved to another semantic colour", name, th.name)
			}
			if got.IsTransparent() {
				t.Errorf("%s under the %s theme is transparent; the role has no value", name, th.name)
			}
		}
	}
}

// TestThemeWithRejectsNonsense keeps the two mistakes that produce a colour
// nobody can explain out of the setup path, where a panic is cheap.
func TestThemeWithRejectsNonsense(t *testing.T) {
	for _, tc := range []struct {
		name        string
		role, value ui.Color
	}{
		{"a literal as the role", ui.RGB(1, 2, 3), ui.RGB(4, 5, 6)},
		{"a role as the value", ui.ColorAccent, ui.ColorLabel},
		{"redefining ColorClear", ui.ColorClear, ui.RGB(4, 5, 6)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Theme.With accepted it")
				}
			}()
			ui.LightTheme().With(tc.role, tc.value)
		})
	}
}

// TestThemeIsSafeToWriteFromAnotherGoroutine is the concurrency half of
// [ui.SetTheme], and it is meaningful only under -race.
//
// The theme is process wide and is read during every build, so the realistic
// mistake is a plain package variable written by a goroutine watching the
// desktop appearance. That is what [ScrollIndicatorLinger] and
// [ShortcutModifier] are, and it is why both are documented as "set before the
// first frame"; a theme cannot be, because switching it at runtime is the
// feature. The atomic pointer is what buys the difference, and this test is
// what would notice if somebody replaced it with a struct field.
//
// The writer passes no application handle, because gift.App.Invalidate is not
// concurrency safe and cannot be made so here; the documented way to repaint
// from another goroutine is app.Post.
func TestThemeIsSafeToWriteFromAnotherGoroutine(t *testing.T) {
	withTheme(t, ui.LightTheme())

	a := gift.New(gift.Options{Root: static(
		ui.Box().Frame(40, 20).Background(ui.ColorSurface),
	)})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			if i%2 == 0 {
				ui.SetTheme(nil, ui.DarkTheme())
			} else {
				ui.SetTheme(nil, ui.LightTheme())
			}
		}
	}()
	for range 200 {
		a.Invalidate()
		frame(t, a, geom.Sz(100, 100))
	}
	<-done

	// Whatever the race produced, it is one of the two palettes and never a
	// half written table: that is the property an atomic pointer has and a
	// struct of eleven colours does not.
	got := opColorAt(t, frame(t, a, geom.Sz(100, 100)), 0)
	if got != ui.LightTheme().Color(ui.ColorSurface) && got != ui.DarkTheme().Color(ui.ColorSurface) {
		t.Fatalf("the surface painted %v, which is neither theme's", got)
	}
}

// TestTheDisabledHairlineIsWeakerThanTheEnabledOne pins the one deliberate
// exception to "one separator in every state"; see ui.defaultStyle.
//
// Collapsing the old per state alpha ladder into a single [ui.ColorSeparator]
// took the disabled outline from alpha 20 to alpha 40 — twice as strong as it
// had been, and identical to an enabled button's. Disabled is the one state
// where a weaker outline is the affordance, and the face cannot carry it
// alone: [ui.ColorControlDisabled] is a twenty-alpha wash, so a full strength
// ring around it reads as an enabled button nobody filled in.
//
// The test compares the two rather than asserting a literal, so a change of
// palette moves both and only a change of *rule* fails.
func TestTheDisabledHairlineIsWeakerThanTheEnabledOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		th   ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		t.Run(tc.name, func(t *testing.T) {
			withTheme(t, tc.th)

			stroke := func(disabled bool) ui.Color {
				b := ui.Button(textOf(t, "x"), nil)
				if disabled {
					b = b.Disabled(true)
				}
				l := run(t, b, geom.Sz(200, 80))
				for _, op := range l.Ops() {
					if op.Kind == render.OpStrokeRoundRect {
						return op.Color
					}
				}
				t.Fatalf("a button with disabled=%v painted no hairline at all", disabled)
				return ui.Color{}
			}
			on, off := stroke(false), stroke(true)
			if want := tc.th.Color(ui.ColorSeparator); on != want {
				t.Errorf("the enabled hairline is %v, want the themed separator %v", on, want)
			}
			if off.A >= on.A {
				t.Errorf("the disabled hairline has alpha %v and the enabled one %v; "+
					"disabled is supposed to be the weaker outline", off.A, on.A)
			}
			if want := tc.th.Color(ui.Fade(ui.ColorSeparator, 0.5)); off != want {
				t.Errorf("the disabled hairline is %v, want half the separator %v", off, want)
			}
		})
	}
}

// TestACallerSuppliedBorderIsNotWeakenedWhenDisabled is the boundary of the
// rule above. The unset rule of [ui.ButtonStyle] fills in what the caller left
// out; it does not reach into a value the caller named.
func TestACallerSuppliedBorderIsNotWeakenedWhenDisabled(t *testing.T) {
	withTheme(t, ui.LightTheme())

	mine := ui.Border{Width: 2, Color: ui.RGB(200, 30, 90)}
	l := run(t, ui.Button(textOf(t, "x"), nil).Border(mine).Disabled(true), geom.Sz(200, 80))
	for _, op := range l.Ops() {
		if op.Kind == render.OpStrokeRoundRect {
			if op.Color != mine.Color {
				t.Fatalf("the caller's border was repainted as %v, want %v", op.Color, mine.Color)
			}
			return
		}
	}
	t.Fatal("the caller's border was not painted at all")
}

// TestIsSemanticIsAnEqualityAndNotASignTest pins the tightening of the
// predicate, which is a small change with a disproportionate failure mode.
//
// [ui.IsSemantic] used to ask whether the red channel was negative. The mark
// is a specific value, -1, and nothing ever compared against it — so a colour
// a hair below zero, which a caller's own arithmetic can produce, was
// classified as a role, had its green channel read as a role index and
// resolved to the transparent zero. A rounding error became an invisible
// widget with no diagnosis anywhere.
//
// With the equality, such a colour is not semantic, [ui.ResolveColor] returns
// it untouched, and it reaches [render.List.Add] — whose check is deliberately
// the looser "any negative channel" and which therefore rejects it under the
// giftdebug tag. The mistake is loud instead of silent, which is the whole
// point of the two checks having different strictness.
func TestIsSemanticIsAnEqualityAndNotASignTest(t *testing.T) {
	nearZero := ui.Color{R: -1e-9, G: 0.5, B: 0.5, A: 1}
	if ui.IsSemantic(nearZero) {
		t.Fatalf("%v was classified as a semantic colour; it would resolve to nothing at all",
			nearZero)
	}
	if got := ui.ResolveColor(nearZero); got != nearZero {
		t.Errorf("ResolveColor turned %v into %v; a colour that is not a role must pass through",
			nearZero, got)
	}

	// And the real marks still are semantic, faded or not.
	for _, c := range []ui.Color{ui.ColorAccent, ui.ColorClear, ui.Fade(ui.ColorLabel, 0.08)} {
		if !ui.IsSemantic(c) {
			t.Errorf("%v is no longer recognised as a semantic colour", c)
		}
	}
	// A literal is never one, however dark.
	for _, c := range []ui.Color{{}, ui.RGB(0, 0, 0), ui.RGBA(255, 255, 255, 1)} {
		if ui.IsSemantic(c) {
			t.Errorf("the literal %v was classified as a semantic colour", c)
		}
	}
}

// --- contrast ----------------------------------------------------------------

// TestBothThemesAreLegible asserts a contrast ratio rather than exact values,
// so that a change of taste in the palette survives the test and an unreadable
// pairing does not.
//
// The ratio is the WCAG 2 one, computed on the sRGB relative luminance. Two of
// the colours involved are translucent — the secondary label is the label at
// 59 % — so each pair is composited over its own background first. Comparing
// the premultiplied colours directly would have called a barely visible
// caption perfectly legible.
//
// The thresholds are not all the same, and that is deliberate rather than
// sloppy: 7 is WCAG AAA for body text, 4.5 is AA, and 3 is the level WCAG 2.1
// asks of a user interface component that is not text, which is what the
// accent is when it draws a focus ring. A single number would have forced
// either an accent nobody uses or a caption nobody can read.
func TestBothThemesAreLegible(t *testing.T) {
	type pair struct {
		name   string
		fg, bg ui.Color
		min    float64
	}
	pairs := []pair{
		{"label on background", ui.ColorLabel, ui.ColorBackground, 7},
		{"label on surface", ui.ColorLabel, ui.ColorSurface, 7},
		{"label on a control", ui.ColorLabel, ui.ColorControl, 4.5},
		{"label on a pressed control", ui.ColorLabel, ui.ColorControlPressed, 4.5},
		{"secondary label on background", ui.ColorSecondaryLabel, ui.ColorBackground, 4.5},
		{"secondary label on surface", ui.ColorSecondaryLabel, ui.ColorSurface, 4.5},
		{"content on the accent", ui.ColorOnAccent, ui.ColorAccent, 4.5},
		{"the accent on background", ui.ColorAccent, ui.ColorBackground, 3},
		{"the accent on surface", ui.ColorAccent, ui.ColorSurface, 3},
		{"a control against its background", ui.ColorControl, ui.ColorBackground, 1.1},
	}
	for _, th := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		t.Run(th.name, func(t *testing.T) {
			for _, p := range pairs {
				bg := th.t.Color(p.bg)
				fg := over(th.t.Color(p.fg), bg)
				if got := contrast(fg, bg); got < p.min {
					t.Errorf("%s: contrast %.2f, want at least %.2f", p.name, got, p.min)
				}
			}
		})
	}
}

// over composites a premultiplied colour onto an opaque background. The
// backgrounds of a theme are opaque by construction, which is why the result
// needs no alpha of its own.
func over(fg, bg ui.Color) ui.Color {
	a := 1 - fg.A
	return ui.Color{R: fg.R + bg.R*a, G: fg.G + bg.G*a, B: fg.B + bg.B*a, A: 1}
}

// contrast is the WCAG 2 contrast ratio of two opaque colours.
func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// luminance is the sRGB relative luminance. The colour is premultiplied and
// opaque here, so the channels are already the straight ones.
func luminance(c ui.Color) float64 {
	lin := func(v float32) float64 {
		d := float64(v)
		switch {
		case d <= 0:
			return 0
		case d <= 0.04045:
			return d / 12.92
		default:
			return math.Pow((d+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// TestFadedPairsAreLegible extends the previous test to the one derivation
// this package offers, [ui.Fade], and it exists because the previous test
// could not see the mistake the examples actually made.
//
// A faded colour is not a darker or lighter version of itself: premultiplied,
// it composites towards whatever is behind it. So a faded accent moves towards
// white on the light theme and towards the surface on the dark one, and the
// content drawn on it — [ui.ColorOnAccent], whose entire job is to be legible
// on the accent — stops being legible in *both* directions at once. The
// theme's own pairs all held; cmd/example-counter painted its theme switch at
// a contrast of 3.65 at rest and 2.61 pressed, and nothing said so.
//
// The headroom is the thing to take away from the numbers below: the accent
// with ColorOnAccent on it clears AA by 0.26 on the light theme, so it cannot
// be faded at all and still carry text. That is a fact about the palette and
// not about the example, which is why it is pinned here rather than argued in
// a comment.
func TestFadedPairsAreLegible(t *testing.T) {
	type pair struct {
		name   string
		fg, bg ui.Color
		// base is what a translucent bg is composited over before the ratio
		// is taken. A faded fill shows the thing behind it, and comparing the
		// premultiplied value directly would call a nearly invisible wash
		// perfectly legible.
		base ui.Color
		min  float64
	}
	pairs := []pair{
		// The theme switch of cmd/example-counter, which is the only control
		// in the examples that carries ColorOnAccent on a derived face.
		{"switch label on the accent face", ui.ColorOnAccent, ui.ColorAccent, ui.ColorSurface, 4.5},
		// The scroll bar, which is a Fade of ColorLabel and is the reason
		// Fade exists. It is a user interface component and not text, so the
		// WCAG 2.1 level for that is 3.
		{"the scroll bar thumb on a surface",
			ui.DefaultScrollBar.Thumb, ui.ColorSurface, ui.ColorSurface, 3},
		{"the scroll bar thumb on the background",
			ui.DefaultScrollBar.Thumb, ui.ColorBackground, ui.ColorBackground, 3},
	}
	for _, th := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		t.Run(th.name, func(t *testing.T) {
			for _, p := range pairs {
				base := th.t.Color(p.base)
				bg := over(th.t.Color(p.bg), base)
				fg := over(th.t.Color(p.fg), bg)
				got := contrast(fg, bg)
				if got < p.min {
					t.Errorf("%s: contrast %.2f, want at least %.2f", p.name, got, p.min)
				}
				t.Logf("%s: %.2f (floor %.2f)", p.name, got, p.min)
			}
		})
	}
}

// TestFadingTheAccentDestroysItsLegibility is the general statement behind the
// case above, and it is here so that a later change of palette cannot quietly
// make a faded accent look safe again.
//
// It asserts the *negative*: below a fade this shallow, ColorOnAccent is no
// longer AA legible on the result. A design that wants a faded accent to carry
// text therefore needs a new role and not a multiplication, which is the
// boundary the project plan, section 20, draws around the colour set.
func TestFadingTheAccentDestroysItsLegibility(t *testing.T) {
	for _, th := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		t.Run(th.name, func(t *testing.T) {
			base := th.t.Color(ui.ColorSurface)
			full := contrast(over(th.t.Color(ui.ColorOnAccent), over(th.t.Color(ui.ColorAccent), base)),
				over(th.t.Color(ui.ColorAccent), base))
			if full < 4.5 {
				t.Fatalf("the unfaded accent is already illegible at %.2f; "+
					"TestBothThemesAreLegible should have caught that", full)
			}
			for _, f := range []float32{0.85, 0.65} {
				bg := over(th.t.Color(ui.Fade(ui.ColorAccent, f)), base)
				got := contrast(over(th.t.Color(ui.ColorOnAccent), bg), bg)
				t.Logf("accent at %.0f%%: contrast %.2f (unfaded %.2f)", f*100, got, full)
				if got >= full {
					t.Errorf("fading the accent to %v improved the contrast to %.2f from %.2f, "+
						"which would mean Fade no longer composites towards the background", f, got, full)
				}
			}
		})
	}
}

// --- allocation --------------------------------------------------------------

// TestThemedFramePathIsAllocationFree is the section 11 contract applied to
// this feature: a colour lookup that allocated once per operation would be a
// defect, and the natural way to get one is to resolve a colour during paint
// instead of during build.
//
// The tree below uses semantic colours everywhere a widget in this package
// accepts one, so a resolution that leaked into the frame path has somewhere
// to show up.
func TestThemedFramePathIsAllocationFree(t *testing.T) {
	withTheme(t, ui.DarkTheme())

	a := gift.New(gift.Options{Root: themedTree(loadTestFont(t))})
	step := func() {
		if err := a.Update(geom.Sz(800, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	if n := a.Diagnostics().LiveNodes; n < 100 {
		t.Fatalf("the themed tree has only %d nodes, the measurement would be meaningless", n)
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("the frame path of a themed tree allocated %v times per run, want 0", got)
	}
}

func themedTree(f ui.Font) func(*gift.Context) gift.View {
	return func(*gift.Context) gift.View {
		rows := make([]gift.View, 0, 24)
		for r := range 24 {
			rows = append(rows, ui.HStack(
				ui.Text("row").Font(f).FontSize(12).Foreground(ui.ColorSecondaryLabel).Key("t"),
				ui.Box().Frame(12, 12).Background(ui.ColorAccent).Key("b"),
				ui.Button(ui.Text("go").Font(f), nil).CornerRadius(4).Key("go"),
			).Gap(6).Key(string(rune('a'+r%26))))
		}
		return ui.VStack(rows...).
			Gap(4).
			Padding(8).
			Background(ui.ColorSurface).
			Border(ui.Border{Width: 1, Color: ui.ColorSeparator}).
			CornerRadius(6)
	}
}

// BenchmarkResolveColor is the per lookup half of the same contract. Build is
// exempt from the zero allocation rule of the project plan, section 11, but a
// colour lookup is called once per styled field of every view, so it has to be
// a load and an index and nothing else.
func BenchmarkResolveColor(b *testing.B) {
	var sink ui.Color
	for b.Loop() {
		sink = ui.ResolveColor(ui.ColorSecondaryLabel)
	}
	if sink.IsTransparent() {
		b.Fatal("the secondary label resolved to nothing")
	}
}
