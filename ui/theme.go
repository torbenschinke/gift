package ui

import (
	"fmt"
	"sync/atomic"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/render"
)

// This file is the semantic colour layer of the project plan, section 20:
// named colours that resolve from a process wide theme, and a switch between
// light and dark at runtime.
//
// # What is deliberately not here
//
// There is no environment, no provider and no inherited scope. Section 20
// rules that out by name and points at section 4, which holds that styling is
// a property of the concrete construction site rather than something applied
// to an arbitrary view. A semantic colour is therefore an ordinary value a
// call site writes down, exactly like [RGB]; what makes it semantic is that
// it names a *role* and asks one global table for the answer, not that
// anything above it in the tree supplied it.
//
// A theme change consequently reaches the screen through a rebuild — see
// [SetTheme] — and not through an invalidation of an inherited value.

// colorRole is the identity of one semantic colour inside a [Theme].
//
// It is unexported on purpose. An application never names a role: it names the
// colour, [ColorAccent] and friends, and [Theme.With] recovers the role from
// the colour it is handed. Two parallel vocabularies for one concept — a role
// enum and a colour value per role — is the sort of thing that goes out of
// sync the first time somebody adds a colour and forgets the other list.
type colorRole uint8

const (
	roleNone colorRole = iota
	roleClear
	roleLabel
	roleSecondaryLabel
	roleBackground
	roleSurface
	roleSeparator
	roleAccent
	roleOnAccent
	roleControl
	roleControlHover
	roleControlPressed
	roleControlDisabled
	roleDanger
	numColorRoles
)

// roleNames is used for diagnostics only, so a panic can say which colour the
// caller meant instead of printing four floats.
var roleNames = [numColorRoles]string{
	roleNone:            "<none>",
	roleClear:           "ColorClear",
	roleLabel:           "ColorLabel",
	roleSecondaryLabel:  "ColorSecondaryLabel",
	roleBackground:      "ColorBackground",
	roleSurface:         "ColorSurface",
	roleSeparator:       "ColorSeparator",
	roleAccent:          "ColorAccent",
	roleOnAccent:        "ColorOnAccent",
	roleControl:         "ColorControl",
	roleControlHover:    "ColorControlHover",
	roleControlPressed:  "ColorControlPressed",
	roleControlDisabled: "ColorControlDisabled",
	roleDanger:          "ColorDanger",
}

// The semantic colours. Every one of them is named for what it is *for* and
// never for what it looks like: a colour called Label survives a switch to a
// dark theme, a colour called DarkGrey does not.
//
// # Why the set stops here
//
// These eleven are the vocabulary the widgets in this package actually need,
// plus the two that a caller cannot express without help. Text needs a primary
// and a subordinate colour; a window needs a backdrop and something raised on
// it; a hairline needs a colour that is neither; a control needs a face in
// four interaction states, which is not a design opinion but the state set
// [ButtonView] already has; and an accent needs a companion that is legible
// *on* it, or the accent cannot be used as a fill at all.
//
// There is no success, warning or danger colour, no elevation ladder and no
// per widget token, because no widget in this package expresses any of those
// today. A token nobody resolves is a name to keep in sync for nothing; they
// arrive with the widget that needs them.
//
// The focus ring is [ColorAccent] and not a role of its own, for the same
// reason: one accent that both tints and marks focus is one decision for an
// application to make, and a separate ring colour would be a second one that
// almost always has to repeat the first.
//
// # These are values, not settings
//
// They are variables because [Color] is a struct and Go has no constant of
// struct type. Assigning to one is a defect: it would rename the role for the
// whole process without changing any theme. Change the *theme* instead, with
// [Theme.With] and [SetTheme].
var (
	// ColorClear is explicit transparency.
	//
	// It exists because the zero [Color] has a second meaning in the style
	// structs of this package — "not set, let the theme decide", see
	// [ButtonStyle] — and a caller who genuinely wants nothing drawn needs a
	// way to say so. It is not part of a theme and resolves to the fully
	// transparent colour under every one of them.
	ColorClear = semanticColor(roleClear)

	// ColorLabel is the colour of primary text.
	ColorLabel = semanticColor(roleLabel)
	// ColorSecondaryLabel is the colour of text that supports the primary
	// text: captions, units, hints. It is legible and visibly quieter.
	ColorSecondaryLabel = semanticColor(roleSecondaryLabel)

	// ColorBackground is the colour of the page: the window itself, behind
	// everything else.
	//
	// Exactly one thing in this package paints it, [Window], and an
	// application that never calls Window never puts it on the screen — gift
	// paints no background of its own. For three work units nothing called
	// it, so tinting this role magenta at run time changed zero pixels on all
	// four screens of cmd/example-kitchensink while every other role changed
	// thousands. It was not a redundant role; it was an unpainted one.
	ColorBackground = semanticColor(roleBackground)
	// ColorSurface is the colour of a panel, card or sheet raised above
	// [ColorBackground]: a [Card], a [Modal] sheet, the bar of a [TabBar] or
	// a [NavigationStack], the keyboard's panel.
	//
	// The two are a hierarchy and not two names for "light grey". A surface
	// is what sits *on* the page, so a view that fills the window with this
	// role has flattened the hierarchy rather than used it — and a control
	// wears [ColorControl], not this, which is the correction the role audit
	// made to [TextField].
	ColorSurface = semanticColor(roleSurface)
	// ColorSeparator is the colour of a hairline: a border, a divider, the
	// edge of a control.
	ColorSeparator = semanticColor(roleSeparator)

	// ColorAccent is the colour that carries attention: a filled primary
	// control, a selection, the focus ring.
	ColorAccent = semanticColor(roleAccent)
	// ColorOnAccent is the colour of content drawn on [ColorAccent]. It is
	// not simply the label colour: on a mid blue, one of black and white is
	// legible and the other is not, and which one it is differs between the
	// light and the dark theme.
	ColorOnAccent = semanticColor(roleOnAccent)

	// ColorControl is the face of a control at rest.
	ColorControl = semanticColor(roleControl)
	// ColorControlHover is the face of a control under the pointer.
	ColorControlHover = semanticColor(roleControlHover)
	// ColorControlPressed is the face of a control being pressed.
	ColorControlPressed = semanticColor(roleControlPressed)
	// ColorControlDisabled is the face of a control that is out of input.
	ColorControlDisabled = semanticColor(roleControlDisabled)

	// ColorDanger is the colour of a choice that destroys something: the
	// "Delete" button of an alert.
	//
	// It is the twelfth role and it arrived with the widget that needs it,
	// which is the rule the paragraph above states. [AlertDestructive]
	// is the only consumer in this package, and there is deliberately still
	// no success and no warning colour, because nothing draws one.
	//
	// It is not simply "red". It is red in both themes and it is a
	// *different* red in each, because a red that is legible on the light
	// theme's white surface is muddy on the dark theme's slate one — which is
	// the same argument [ColorOnAccent] exists for.
	ColorDanger = semanticColor(roleDanger)
)

// --- the encoding -----------------------------------------------------------

// A semantic colour is an ordinary [Color] carrying a role instead of a
// picture, which is what lets it be written into every existing colour field
// — [Border.Color], [ButtonStyle.Background], [TextView.Foreground] — without
// changing a single one of their types.
//
// The encoding uses a value the type cannot otherwise hold. A [Color] is
// premultiplied with components in [0, 1], so every channel of every real
// colour is non negative; a negative red is therefore free, and it is the
// mark. The role travels in green and a fade factor, see [Fade], in blue.
//
// The alternative designs were weighed and are recorded in the work unit
// report: a wrapper type would have broken every composite literal that
// writes a colour into a style struct, and a parallel "was this set" bit per
// colour field cannot exist in an *exported* struct that callers fill with a
// composite literal — there is no bit a literal can set.
//
// Nothing below ui ever sees one of these. Every view type resolves its whole
// style in Build and stores literal colours in its node, which is the one
// moment gift asks for an appearance; see [ResolveColor] and
// [styleSpec.resolved].
//
// # How that is enforced, in two layers
//
// Under the giftdebug build tag, [render.List.Add] and
// [render.List.AddMaterial] reject an operation whose colour has a negative
// channel. That is the backstop, and it is deliberately looser than
// [IsSemantic]: it rejects every impossible premultiplied colour and not only
// this encoding, because a painter outside this package can produce the
// former by arithmetic.
//
// The backstop alone is not enough, and that is the one thing about this
// design worth reading twice. A semantic colour has an alpha of zero until it
// is resolved, so every painter in this package — each of which skips fully
// transparent work — *drops* the operation before the backstop can see it.
// The failure is therefore not a wrong pixel but no pixel, and no diagnosis.
// So this package asserts before each of those gates as well; see
// assertResolved in color_debug.go. The same caveat is restated on
// [IsSemantic] and [ResolveColor], because it is the invariant and not a
// footnote.
const semanticMark = -1

func semanticColor(r colorRole) Color {
	return Color{R: semanticMark, G: float32(r), B: 1}
}

// IsSemantic reports whether c is one of the named colours of this package
// rather than a literal one.
//
// It is exported because an application that writes its own widget has to
// know: a semantic colour must be passed through [ResolveColor] before it
// reaches a display list, and there is otherwise no way to ask.
//
// # The one thing to know about an unresolved colour
//
// It is transparent. The encoding lives in the negative red channel, and the
// alpha is zero, so a semantic colour that escaped [ResolveColor] does not
// paint the wrong colour — it paints *nothing*, because every painter that is
// worth anything skips fully transparent work. A widget of your own that
// reaches for this predicate should therefore check the colour where it
// decides whether to draw at all, not where it appends the operation; by the
// latter point the mistake has already turned into an absence.
//
// The test is an equality against the single mark value and not "is the red
// channel negative". The looser form classified a colour a hair below zero —
// which a caller's own arithmetic can produce — as a role, and resolved it to
// nothing at all, so a rounding error became an invisible widget.
func IsSemantic(c Color) bool { return c.R == semanticMark }

// Fade returns c at f times its opacity, where f is between 0 and 1.
//
// On a semantic colour the result is *still* semantic: the fade is remembered
// and applied after the theme is consulted, so
//
//	var track = ui.Fade(ui.ColorLabel, 0.08)
//
// works as a package variable and follows every later theme change. That is
// the whole reason this is not simply a multiplication at the call site: a
// helper that resolved eagerly would freeze whichever theme happened to be
// installed when the package was initialised, which is a trap that would be
// sprung once per application and diagnosed never.
//
// It is how a design expresses emphasis without inventing a role: the theme
// decides the hue, the call site decides how present it is.
func Fade(c Color, f float32) Color {
	if !isFinite(f) || f < 0 || f > 1 {
		panic(fmt.Sprintf("gift/ui: Fade(%v) must be a finite number between 0 and 1", f))
	}
	if IsSemantic(c) {
		c.B *= f
		return c
	}
	// Premultiplied, so every channel scales with the alpha.
	return Color{R: c.R * f, G: c.G * f, B: c.B * f, A: c.A * f}
}

// --- the theme --------------------------------------------------------------

// Theme is one complete assignment of colours to the semantic roles.
//
// It is a value: copying it is copying the palette, and nothing in it is
// shared. Derive one from [LightTheme] or [DarkTheme] with [Theme.With] and
// install it with [SetTheme].
type Theme struct {
	c    [numColorRoles]Color
	dark bool
}

// LightTheme returns the built in light theme.
//
// It is deliberately the look this package already had: the button faces, the
// hairline and the black label are the literals that used to sit in button.go
// and text.go, promoted to names. A theming unit that also restyles everything
// makes it impossible to tell a defect in the theming from a change of taste.
func LightTheme() Theme {
	var t Theme
	t.c[roleLabel] = RGB(0, 0, 0)
	t.c[roleSecondaryLabel] = RGBA(0, 0, 0, 150)
	t.c[roleBackground] = RGB(246, 247, 249)
	t.c[roleSurface] = RGB(255, 255, 255)
	t.c[roleSeparator] = RGBA(0, 0, 0, 40)
	t.c[roleAccent] = RGB(40, 110, 225)
	t.c[roleOnAccent] = RGB(255, 255, 255)
	t.c[roleControl] = RGB(232, 234, 238)
	t.c[roleControlHover] = RGB(244, 246, 250)
	t.c[roleControlPressed] = RGB(200, 204, 212)
	t.c[roleControlDisabled] = RGBA(0, 0, 0, 20)
	t.c[roleDanger] = RGB(200, 30, 40)
	return t
}

// DarkTheme returns the built in dark theme.
//
// It is not the light theme inverted. An inversion produces pure white text on
// pure black, which glares, and it turns a light control that sits *above* its
// background into a dark one that sits below it — so the raised surface reads
// as a hole. This palette instead keeps the ordering the light one has: the
// background is the darkest thing, a surface is a step lighter, a control is a
// step lighter again, and the label is an off white rather than white.
//
// The accent is lighter than the light theme's, because it has to separate
// from a dark background rather than from a white one, and [ColorOnAccent]
// flips to near black with it — which is the case a mechanical inversion gets
// wrong and the reason that role exists at all.
//
// The starting point was the hand rolled palette of cmd/example-counter,
// which was a dark design somebody actually looked at.
func DarkTheme() Theme {
	var t Theme
	t.c[roleLabel] = RGB(236, 238, 242)
	t.c[roleSecondaryLabel] = RGBA(255, 255, 255, 150)
	t.c[roleBackground] = RGB(18, 20, 26)
	t.c[roleSurface] = RGB(30, 34, 44)
	t.c[roleSeparator] = RGBA(255, 255, 255, 40)
	t.c[roleAccent] = RGB(96, 160, 255)
	t.c[roleOnAccent] = RGB(12, 16, 22)
	t.c[roleControl] = RGB(52, 58, 72)
	t.c[roleControlHover] = RGB(70, 78, 98)
	t.c[roleControlPressed] = RGB(36, 40, 52)
	t.c[roleControlDisabled] = RGBA(255, 255, 255, 20)
	t.c[roleDanger] = RGB(255, 105, 110)
	t.dark = true
	return t
}

// IsDark reports whether the theme describes a dark appearance.
//
// It is a flag set by [DarkTheme] and carried by [Theme.With], not a guess
// made from the luminance of a colour. An application uses it to pick the
// things a colour cannot express — which of two images to show, which way a
// toggle points — and a guess would answer differently for a palette that is
// merely dim.
func (t Theme) IsDark() bool { return t.dark }

// Dark returns a copy of the theme marked as dark or light. It exists for an
// application that builds a palette from scratch rather than from [DarkTheme].
func (t Theme) Dark(v bool) Theme { t.dark = v; return t }

// With returns a copy of the theme in which the given semantic colour resolves
// to value:
//
//	ui.SetTheme(app, ui.DarkTheme().With(ui.ColorAccent, ui.RGB(255, 149, 0)))
//
// semantic must be one of the named colours of this package, and value must be
// a literal one — a theme that resolves a role to another role would be a
// chain this package deliberately does not have. Both are rejected with a
// panic during setup rather than producing a colour nobody can explain.
//
// [ColorClear] cannot be redefined; it is transparency, not a palette entry.
func (t Theme) With(semantic, value Color) Theme {
	if !IsSemantic(semantic) {
		panic("gift/ui: Theme.With takes one of the named colours, such as ui.ColorAccent, as its first argument")
	}
	r := roleOf(semantic)
	switch {
	case r == roleClear:
		panic("gift/ui: ColorClear is transparency and cannot be redefined by a theme")
	case r == roleNone || r >= numColorRoles:
		panic("gift/ui: Theme.With was given a malformed semantic colour")
	case IsSemantic(value):
		panic(fmt.Sprintf("gift/ui: Theme.With(%s, ...) takes a literal colour such as ui.RGB(40, 110, 225); "+
			"a theme entry that pointed at another role would be a chain this package does not resolve",
			roleNames[r]))
	}
	t.c[r] = value
	return t
}

// Color returns the literal colour the theme gives to one of the named
// colours. It is [ResolveColor] against this theme rather than the installed
// one, and is what a test or a widget with its own palette arithmetic uses.
func (t Theme) Color(semantic Color) Color {
	if !IsSemantic(semantic) {
		return semantic
	}
	r := roleOf(semantic)
	if r == roleNone || r == roleClear || r >= numColorRoles {
		return Color{}
	}
	return fadeLiteral(t.c[r], semantic.B)
}

func roleOf(c Color) colorRole { return colorRole(c.G) }

func fadeLiteral(c Color, f float32) Color {
	if f == 1 {
		return c
	}
	return Color{R: c.R * f, G: c.G * f, B: c.B * f, A: c.A * f}
}

// --- the installed theme ------------------------------------------------------

// theme is the process wide theme. See [SetTheme] for the concurrency story.
//
// It is a pointer to the slot rather than the slot itself, so that the light
// default is installed by ordinary package variable initialisation and not by
// an init function. Go orders variable initialisation by dependency, so every
// other variable in this package that reads the theme — and every variable in
// a package importing ui — is guaranteed to find one; an init function would
// only be guaranteed to run before main.
var theme = newThemeSlot(LightTheme())

func newThemeSlot(t Theme) *atomic.Pointer[Theme] {
	p := new(atomic.Pointer[Theme])
	p.Store(&t)
	return p
}

// CurrentTheme returns the theme in force. The default is [LightTheme].
func CurrentTheme() Theme { return *theme.Load() }

// SetTheme installs t as the process wide theme and asks app to rebuild
// everything, which is what puts the new colours on the screen.
//
// # Why the app is an argument
//
// Because a theme change that does not repaint is the defect this function
// exists to prevent. Colours are resolved during build — a retained node holds
// the literal colour it was built with, which is what keeps the frame path
// free of theme lookups and of allocations — so nothing at all changes until
// the views are built again. The project plan, section 20, names the mechanism:
// a theme change triggers a rebuild. Making the caller remember that
// separately would be a two line ritual whose second line is easy to forget
// and whose symptom is a window that ignores the switch.
//
// # Why it is [gift.App.InvalidateAll] and not [gift.App.Invalidate]
//
// Because Invalidate marks the *root* scope, and a root rebuild stops at the
// first rebuild boundary below it. A [gift.Memo] whose props did not change is
// not rebuilt, keeps the palette it was built with, and the screen ends up
// half in each theme: a light background with dark cards on it and text that
// cannot be read. A theme is not a prop and not a state, so nothing about the
// memo's own inputs changed — which is precisely why the invalidation has to
// come from outside the dependency graph.
//
// The cost is a rebuild of every component instance in the process, and it is
// the honest one: section 20's promise is that the switch rebuilds every
// colour on the screen, and every colour on the screen is resolved in a build.
// See [gift.App.InvalidateAll] for what that is worth per switch.
//
// app may be nil, which sets the theme and repaints nothing. That is for a
// call before the application exists — the usual place, in main, next to
// [SetDefaultFont] — and for a test that renders a fresh tree afterwards.
//
// # Concurrency
//
// The theme itself is an atomic pointer, so reading it from a build and
// replacing it from another goroutine is well defined and race free: a build
// sees either the old palette or the new one, never a half written table.
// That is deliberately more than [ScrollIndicatorLinger] and
// [ShortcutModifier] offer, both of which are plain variables documented as
// "set before the first frame"; a theme is different in kind, because
// switching it *at runtime* is the feature.
//
// [gift.App.InvalidateAll] is not concurrency safe, however, and nothing here
// can make it so. A goroutine that is not the UI goroutine — one watching the
// desktop appearance, say — therefore posts the switch:
//
//	app.Post(func() { ui.SetTheme(app, ui.DarkTheme()) })
//
// Calling it with a non nil app from another goroutine is a defect. Under the
// giftdebug tag the invalidation catches it directly and panics naming both
// goroutines. Without the tag it is a data race on the tree's dirty flags,
// which -race reports when it observes the two accesses — which is to say when
// the UI goroutine is genuinely building or laying out at that moment, not
// merely because the call came from elsewhere. That is the usual property of
// the race detector and the reason the giftdebug check exists next to it
// rather than instead of it.
func SetTheme(app *gift.App, t Theme) {
	theme.Store(&t)
	if app != nil {
		app.InvalidateAll()
	}
}

// ResolveColor turns a semantic colour into the literal colour of the theme in
// force. A colour that is already literal is returned unchanged, so it is safe
// to call on anything.
//
// Every colour entering a view passes through here, once, during build. An
// application that writes its own widget and emits its own
// [render.Op] has to call it too; the alternative is an operation carrying a
// role where the backend expects a colour.
//
// # Forgetting it does not look like a wrong colour
//
// It looks like nothing at all. An unresolved semantic colour has an alpha of
// zero — see [IsSemantic] — so the usual "do not bother drawing transparent
// things" test in your own painter throws the operation away before anything
// can complain about it. The widget lays out, measures, hit tests and is
// completely invisible.
//
// The practical consequence for a widget of your own: resolve in Build and
// store the literal colour in your node, the way every view in this package
// does. Resolving in Paint would also work and would cost a global load per
// operation, but resolving *nowhere* is the mistake this paragraph exists for,
// and it is silent in a release build.
func ResolveColor(c Color) Color {
	if !IsSemantic(c) {
		return c
	}
	return theme.Load().Color(c)
}

// resolveBorder and resolveShadow are the same thing for the two style structs
// that contain a colour. They are not methods on [Border] and [Shadow],
// because those types live in render, which has no idea that a theme exists
// and should not acquire one.
//
// They resolve and nothing else. In particular a [Border] with a width and no
// colour stays invisible here rather than picking up [ColorSeparator]: a
// border is not a field with a themed default, it is a value a caller passed
// in, and "I want this stroke to be transparent" is a thing people write — the
// internal painter test in this package writes exactly that. The unset rule
// lives one level up, in [ButtonStyle], where a themed default for the field
// actually exists.
func resolveBorder(b Border) Border {
	b.Color = ResolveColor(b.Color)
	return b
}

func resolveShadow(s Shadow) Shadow {
	s.Color = ResolveColor(s.Color)
	return s
}

// resolveMaterial is the same thing again for the one colour a material
// carries, [render.GlassParams.Tint].
//
// It was missing for a whole work unit, and the omission is worth recording
// because it is the shape of mistake this layer invites. A material does not
// travel through [render.List.Add] — it goes into the side table through
// [render.List.AddMaterial] — so it bypassed the one choke point the encoding
// relied on, and a glass pane tinted with [ColorAccent] reached the GPU
// carrying a red channel of -1. The backend's own transparency guard did not
// catch it either, because the tint was not transparent as far as the shader
// was concerned: it was negative. It painted saturated cyan.
//
// Every colour bearing field of every style struct in this package now has a
// resolver next to it, and [assertResolved] is the check that says so at run
// time rather than by inspection.
func resolveMaterial(m render.Material) render.Material {
	m.Glass.Tint = ResolveColor(m.Glass.Tint)
	return m
}

// --- enumerating the palette --------------------------------------------------

// NamedColor pairs one semantic colour with the name it is exported under.
type NamedColor struct {
	// Name is the Go identifier, for example "ColorAccent".
	Name string
	// Color is the semantic colour itself, ready for [Theme.Color] or
	// [ResolveColor].
	Color Color
}

// SemanticColors returns every semantic colour of this package that a theme
// assigns a value to, in declaration order.
//
// [ColorClear] is not in it. It is transparency rather than a palette entry —
// [Theme.With] refuses it — so a caller iterating the palette would have to
// special-case it, and every caller that forgot would report a thirteenth
// colour that no theme can change.
//
// # Why this is exported
//
// Because the alternative is a hand-maintained list, and this module has
// already paid for one of those twice. Review gate 13 found [ColorDanger]
// outside both palette tests, so making it fully transparent in both themes
// left the suite green; the role audit of WU-AF found the same shape again in
// gift/auto, whose /diag route listed the roles it reports as a literal slice
// that nothing checked for completeness. A list that is derived cannot fall
// behind an enum that grows, and a debugging tool that silently omits a role
// is the worst possible place to learn that it did.
//
// It is a small enough thing to keep for ever, and it is the right shape for
// an application that offers a theme editor, a diagnostics overlay, or a
// contrast check of its own palette:
//
//	for _, c := range ui.SemanticColors() {
//		fmt.Println(c.Name, ui.CurrentTheme().Color(c.Color))
//	}
//
// The returned slice is freshly allocated, so a caller may keep it and sort
// it. Do not call it per frame.
func SemanticColors() []NamedColor {
	out := make([]NamedColor, 0, numColorRoles)
	for r := roleClear + 1; r < numColorRoles; r++ {
		out = append(out, NamedColor{Name: roleNames[r], Color: semanticColor(r)})
	}
	return out
}
