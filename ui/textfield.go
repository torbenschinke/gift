package ui

import (
	"strings"
	"time"
	"unicode"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/text"
	"github.com/torbenschinke/gift/render"
)

var textFieldType = gift.RegisterType("ui.TextField")

// Timings and metrics of a text field.
//
// They are package level values rather than per view modifiers for the reason
// [LongPressDelay] and [DragSlop] are: a blink rate and a double click window
// are properties of human perception and of the desktop the application runs
// on, not decisions a single field should make differently from its
// neighbour.
const (
	// CaretBlinkInterval is how long the caret stays visible, and then
	// invisible, while it blinks. Half a second each way is the rate X11 and
	// macOS both default to within a few tens of milliseconds.
	CaretBlinkInterval = 500 * time.Millisecond

	// CaretBlinkWindow is how long the caret blinks after the last thing the
	// user did, before it goes solid and stays solid.
	//
	// # Why it ends at all
	//
	// Because a blinking caret is an animation, and an animation is a reason
	// for the backend never to drop to its idle tick rate; see
	// [gift.EventContext.Animate], which says the same thing about itself.
	// A caret that blinked for ever would hold a kiosk at sixty frames a
	// second all night for one flashing rectangle. [gift.ScrollIndicatorLinger]
	// makes exactly this trade for exactly this reason and this value is its
	// sibling.
	//
	// Ten seconds is long enough that the blink has done its whole job — it
	// says "this field has the focus", and a user who has not typed for ten
	// seconds has either read that or is not looking — and short enough that
	// an untouched screen settles while the coffee is being made. The caret
	// stays *visible* afterwards, not hidden: the field still has the focus
	// and must still say so.
	CaretBlinkWindow = 10 * time.Second

	// DoubleClickInterval is how close together two presses must be to count
	// as a double click, and DoubleClickSlop how far apart they may land, in
	// logical pixels.
	//
	// gift has no double click event and deliberately so: what counts as one
	// is a property of the control — a gallery tile opens on it, a text field
	// selects a word — and a runtime that decided centrally would have to
	// guess the slop of every one of them. The field counts them itself, from
	// [gift.Event.Time], which is the injected clock and therefore testable.
	DoubleClickInterval = 400 * time.Millisecond
	DoubleClickSlop     = float32(4)

	// caretWidth is the thickness of the caret in logical pixels. One is what
	// a text caret is; at a device density of two it is drawn two physical
	// pixels wide by the density transform, like every other logical
	// dimension in gift.
	caretWidth = float32(1)

	// defaultFieldWidth is how wide a field makes itself when nothing bounds
	// it — in an [HStack], where rule 1 of the overflow model measures an
	// inflexible child with an unbounded main axis.
	//
	// A field has no intrinsic width worth the name: sizing it to its content
	// would make it grow under the typist and jump back when they erase, which
	// is the one behaviour a text field must never have. So it is a constant,
	// and a caller who wants another one writes [TextFieldView.Frame] or
	// [TextFieldView.Flex].
	defaultFieldWidth = float32(180)
)

// Default appearance of a text field, in the semantic colours of the project
// plan, section 20. Unresolved, like every other default in this package, so
// that they follow a later [SetTheme]; see [defaultButtonStyle] in button.go
// for the full argument.
var (
	// The face is [ColorControl] and not [ColorSurface], which is the one
	// correction the role audit of WU-AF made to this file.
	//
	// A field is a control at rest, and every other control in this package
	// — [ButtonView], the track of a [Slider], the tray of a
	// [SegmentedControl], the off state of a [Toggle], a key of the
	// [OnScreenKeyboard] — spells that [ColorControl]. A surface is what a
	// control sits *on*: a [Card], a sheet, the bar of a [TabBar]. A field
	// with a surface face and a field inside a card were therefore the same
	// colour, and in the dark theme exactly the same colour, so the only
	// thing separating the two was the hairline. That is the flattening of
	// the two-level hierarchy the roles exist to express.
	defaultFieldBackground = ColorControl
	defaultFieldBorder     = Border{Width: 1, Color: ColorSeparator}
	defaultFieldFocusRing  = Border{Width: 2, Color: ColorAccent}
	defaultFieldRadius     = float32(6)
	defaultFieldPadding    = geom.Insets{Top: 5, Right: 8, Bottom: 5, Left: 8}

	// defaultSelectionColor is the highlight behind selected text.
	//
	// It is a [Fade] of [ColorAccent] and not a role of its own. Section 20
	// says a token arrives with the widget that needs it, and this widget does
	// need *a* selection colour — but "the accent, quieter" is exactly what
	// [Fade] documents itself as being for, it follows both themes for free,
	// and an application that wants another one says so with
	// [TextFieldView.SelectionColor]. A twelfth role would have to be kept in
	// step in two themes and in every theme a caller derives.
	defaultSelectionColor = Fade(ColorAccent, 0.30)
)

// TextFieldView is a single line text field. It is created by [TextField];
// the zero value is not useful.
//
// # What it is
//
//	name := ui.Editor(ctx, "name", "")
//	ui.TextField(name).Placeholder("Your name").OnChange(func(s string) { ... })
//
// A field edits a [TextEditor], which is where the text, the caret and the
// selection live and which is what makes them survive the rebuild that every
// keystroke causes; see [TextEditor]. The view is the declaration around it:
// the look, the placeholder, the callbacks and the disabled state.
//
// It handles what a single line field has to handle. Click to place the caret,
// drag to select, double click to select a word and triple click to select
// everything. The arrows, with shift to extend and with the word modifier to
// move by words, home and end, backspace and delete, all with gift's own key
// repeat. Select all, copy, cut and paste on [gift.ShortcutModifier].
// Horizontal scrolling when the text is wider than the field, with the caret
// always kept in view.
//
// Where [gift.WordModifier] and [gift.ShortcutModifier] are the same key —
// which is every platform except macOS — an arrow held with it is the word
// motion, and home and end are the way to the ends of the line. That is the
// precedence [gift.WordModifier] fixes and this is the view that spells it.
//
// # What it deliberately is not
//
// It is not a text editor and not a multi line one. There is no line break to
// enter: enter is a submission, a pasted line break ends the paste, and the
// project plan's exclusion list never promised more than one line here. There
// is no undo: an undo stack is a model of its own — it has to coalesce typing
// into edits a user recognises, and it has to survive the same rebuild the
// caret does — and a half built one is worse than none, so [gift.KeyZ] is not
// acted on at all.
//
// There is no auto scroll while a drag rests outside the field. Dragging past
// the left or the right edge selects up to wherever the last move reached and
// then stops; holding still there does not walk on through the text the way a
// list with a repeating timer would. That timer is a second animation with a
// rate, an acceleration and a stopping rule, and it is worth building when
// somebody has a value long enough to need it — a drag can already be
// continued by moving again, and shift-click and shift-arrow select any length
// without a drag at all.
//
// A drag is also not always the field's. A gesture that leaves the press more
// vertically than horizontally belongs to the scrolling form above the field,
// so that a column of fields on a touch kiosk can be scrolled by dragging one
// of them; see [textFieldNode.HandleEvent] for the whole rule and what it
// costs.
//
// It is not an IME. CJK composition, a preedit string and a candidate window
// stay excluded by the project plan, section 14. What is *not* excluded, and
// works: umlauts, accents, AltGr and dead keys. Those are the ordinary
// translation of key presses into characters that every keyboard outside the
// United States performs, they arrive fully resolved on [gift.EventRune], and
// the field inserts them like any other character. Typing "ö" on a German
// layout produces one rune here and one rune in the document.
//
// # The shortcut letters are physical keys
//
// Copy is [gift.ShortcutModifier] plus [gift.KeyC], and KeyC is the *position*
// of the C key on a US keyboard — that is Ebitengine's definition of a key
// code and gift inherits it. On a Dvorak or an AZERTY layout the five editing
// shortcuts therefore sit where that keyboard's US-QWERTY equivalents sit,
// which is what every toolkit does with these five and what a user who learned
// the finger position expects. It also means that on a layout where the C key
// produces something else entirely, the shortcut is still "the key in the C
// position". There is no way for gift to do better: the layout mapping lives
// in the platform and is only ever applied to characters, never to shortcuts.
type TextFieldView struct {
	base
	ed *TextEditor

	placeholder string
	name        string

	onChange func(string)
	onSubmit func(string)
	bind     gift.Binding[string]

	fg, placeholderFG, selection, caret Color
	hasFG, hasPlaceholderFG             bool
	hasSelection, hasCaret              bool

	font Font
	size float32

	focusRing    Border
	hasFocusRing bool
	disabled     bool
	hasPadding   bool
}

// TextField returns a field editing ed. A nil editor panics: a field without a
// document has nowhere to put the first character, and the mistake is one line
// away from the call site.
func TextField(ed *TextEditor) TextFieldView {
	if ed == nil {
		panic("gift/ui: TextField with a nil *TextEditor; create one with ui.NewTextEditor " +
			"or, inside a component, with ui.Editor(ctx, name, initial)")
	}
	return TextFieldView{ed: ed}
}

// ViewType implements gift.View.
func (v TextFieldView) ViewType() gift.TypeID { return textFieldType }

// Build implements gift.View.
//
// The colours are resolved here, once, like in every other view of this
// package, and the bound value is read here, which is what registers the
// component as a dependency of it.
func (v TextFieldView) Build(*gift.BuildContext) gift.Element {
	size := v.size
	if size <= 0 {
		size = DefaultFontSize
	}
	// The binding is the *outer* truth. Reading it registers the dependency,
	// and adopting a value that differs from the document is how a change the
	// application made elsewhere — a form reset, a record loaded — reaches the
	// field. A value equal to the document changes nothing, which is the
	// normal case one rebuild after the user typed: the field wrote the
	// binding itself and is now reading its own text back.
	if !v.bind.IsZero() {
		if s := v.bind.Get(); s != v.ed.Text() {
			v.ed.SetText(s)
		}
	}

	st := v.style.resolved()
	if !v.style.isSet(bitBackground) {
		st.background = ResolveColor(defaultFieldBackground)
	}
	if !v.style.isSet(bitBorder) {
		st.border = resolveBorder(defaultFieldBorder)
	}
	if !v.style.isSet(bitRadius) {
		st.radius = defaultFieldRadius
	}

	n := &textFieldNode{
		ed:          v.ed,
		fr:          v.frame,
		st:          st,
		pad:         defaultFieldPadding,
		placeholder: v.placeholder,
		onChange:    v.onChange,
		onSubmit:    v.onSubmit,
		bind:        v.bind,
		disabled:    v.disabled,
		req:         text.Request{Font: resolveFont(v.font), Size: size, MaxWidth: geom.Unbounded()},
	}
	if v.hasPadding {
		n.pad = v.pad
	}
	n.fg = ResolveColor(ColorLabel)
	if v.hasFG {
		n.fg = ResolveColor(v.fg)
	}
	n.placeholderFG = ResolveColor(ColorSecondaryLabel)
	if v.hasPlaceholderFG {
		n.placeholderFG = ResolveColor(v.placeholderFG)
	}
	n.selectionBG = ResolveColor(defaultSelectionColor)
	if v.hasSelection {
		n.selectionBG = ResolveColor(v.selection)
	}
	// The caret is the label colour by default and not the accent: it stands
	// in the text, it is the text insertion point, and a caret in a different
	// hue from the characters around it reads as a decoration rather than as
	// a position.
	n.caretColor = n.fg
	if v.hasCaret {
		n.caretColor = ResolveColor(v.caret)
	}
	n.disabledFG = ResolveColor(Fade(ColorLabel, 0.4))
	n.disabledBG = ResolveColor(ColorControlDisabled)
	n.focusRing = resolveBorder(defaultFieldFocusRing)
	if v.hasFocusRing {
		n.focusRing = resolveBorder(v.focusRing)
	}
	n.phReq = text.Request{Text: v.placeholder, Font: n.req.Font, Size: size, MaxWidth: geom.Unbounded()}

	return gift.Element{
		Key:      v.key,
		Flex:     v.flex,
		Layouter: n,
		Painter:  n,
		// The accessible name, which is the caller's [TextFieldView.Label] or,
		// failing that, the placeholder — the string a person would use to
		// refer to this field. It is deliberately not the *value*: a selector
		// that found a field by what somebody typed into it would stop finding
		// it as soon as they typed. A test addresses a field by key or by
		// placeholder and reads the value from the editor.
		Label:      v.accessibleName(),
		Interactor: n,
		Focusable:  !v.disabled,
		Disabled:   v.disabled,
		// Always clipped. The text scrolls inside the field, so the part that
		// is scrolled out is by definition outside the bounds, and a field
		// that did not clip would paint it across its neighbours.
		Clip: true,
	}
}

func (v TextFieldView) accessibleName() string {
	if v.name != "" {
		return v.name
	}
	return v.placeholder
}

// textFieldNode is the retained half of a [TextFieldView]: layouter, painter
// and interactor in one object, allocated once per build.
//
// It holds no caret, no selection and no scroll offset. Those are in the
// [TextEditor], because this object is replaced on every rebuild and they must
// not be; see [TextEditor].
type textFieldNode struct {
	ed *TextEditor

	fr  frameSpec
	st  styleSpec
	pad geom.Insets

	// req is the shaping request for the document. Its Text field is refreshed
	// from the editor on every use rather than at build time — see
	// [textFieldNode.para] — because an edit changes the text without
	// necessarily causing a build.
	req   text.Request
	phReq text.Request

	placeholder string

	fg, placeholderFG, selectionBG, caretColor Color
	disabledFG, disabledBG                     Color
	focusRing                                  Border

	onChange func(string)
	onSubmit func(string)
	bind     gift.Binding[string]

	disabled bool

	// inner is the content rectangle of the last layout, in local space. The
	// event handlers need its width to keep the caret in view and its left
	// edge to turn a pointer position into a text offset.
	inner geom.Rect
}

// para returns the shaped document, refreshing the request text from the
// editor first.
//
// The lookup is a cache hit for any text that was already shaped, which is
// every frame that did not change it, and a hit allocates nothing. A changed
// text is a miss and allocates in harfbuzz, which the project plan, section
// 11, excludes from the frame path contract by name.
func (n *textFieldNode) para() *text.Paragraph {
	n.req.Text = n.ed.Text()
	return text.Default().Layout(n.req)
}

// --- layout -----------------------------------------------------------------

// Layout sizes the field: one line of the font tall, and as wide as it is
// allowed to be.
//
// The height is the font's line box and not the height of the text in it, so
// an empty field is exactly as tall as a full one. The width is greedy on a
// bounded axis, like [BoxView], and [defaultFieldWidth] on an unbounded one —
// never the width of the content, because a field that grew as you typed would
// move every sibling with every character.
//
// Both of those make the size of a field independent of its text, on both
// axes, and that is worth naming because it is what an edit does *not* cost:
// no keystroke needs a new layout pass, in a column or in a row, so nothing
// here calls [gift.EventContext.RequestLayout]. An earlier version did, for
// the unbounded case; it could not change an outcome, because the answer
// there is a constant.
func (n *textFieldNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.ed.checkSingleMount(ctx)
	cc := n.fr.apply(c)
	p := n.para()
	h := p.Size.H + n.pad.Vertical()

	w := cc.Max.W
	if !isFinite(w) {
		w = defaultFieldWidth
	}
	size := cc.Constrain(geom.Sz(w, h))
	n.inner = geom.Rc(n.pad.Left, n.pad.Top,
		size.W-n.pad.Right, size.H-n.pad.Bottom)
	// The one place that knows both the text and the width, and therefore the
	// only place an offset left over from a wider field or a longer value can
	// be caught before it is painted; see [textFieldNode.clampScroll].
	n.clampScroll()
	// The overflow of the project plan, section 7, is deliberately not
	// reported here. A field whose text is wider than itself is not
	// overflowing: it scrolls, on purpose, and that is the whole feature. The
	// number that would be reported is the length of the line, which would
	// make gifttest.AssertNoOverflow fail for every field somebody typed into.
	ctx.ReportOverflow(geom.Size{})
	ctx.ReportBaseline(n.pad.Top + p.Metrics.FirstBaseline)
	return size
}

// --- caret geometry ----------------------------------------------------------
//
// The two functions below are the whole of the mapping between byte offsets
// and x coordinates, and they read it out of the shaping result rather than
// re-measuring anything. internal/text records, per glyph, the byte offset of
// the cluster it belongs to — so the position of an offset is the position of
// the first glyph that starts at or after it, and the offset at a position is
// the cluster whose half way point the position has passed.
//
// The approximation in here, stated rather than hidden: a ligature is one
// glyph covering several bytes, and neither function can put a caret inside
// one. The caret snaps to the near end of it instead. Subdividing a ligature
// by its advance is what a full text stack does; it needs per-cluster
// character counts the shaper does not carry today, and an fi ligature in a
// field is a rare place to spend that.

// caretX is the x offset of the byte position off inside the shaped line,
// relative to the start of the line.
func caretX(p *text.Paragraph, off int) float32 {
	if len(p.Lines) == 0 {
		return 0
	}
	ln := &p.Lines[0]
	for ri := range ln.Runs {
		run := &ln.Runs[ri]
		for gi := range run.Glyphs {
			g := &run.Glyphs[gi]
			if int(g.Cluster) >= off {
				return g.X
			}
		}
	}
	// Past the last glyph: the end of the line, including the trailing
	// whitespace its Width excludes. This is the one number the shaping result
	// did not carry before this work unit; see [text.Line.Advance].
	return ln.Advance
}

// offsetAt is the inverse: the byte offset the caret takes when the user
// clicks at x, measured from the start of the line.
//
// The rule is the usual one and the reason it is a *midpoint* rule and not a
// containment rule is worth one sentence: clicking on the right half of a
// character means "after it", so that a click near the end of a word does not
// have to land in the two pixels past the last glyph to put the caret there.
func offsetAt(p *text.Paragraph, text string, x float32) int {
	if len(p.Lines) == 0 {
		return 0
	}
	ln := &p.Lines[0]
	if x <= 0 {
		return 0
	}
	for ri := range ln.Runs {
		run := &ln.Runs[ri]
		for gi := range run.Glyphs {
			g := &run.Glyphs[gi]
			next := ln.Advance
			if gi+1 < len(run.Glyphs) {
				next = run.Glyphs[gi+1].X
			} else if ri+1 < len(ln.Runs) && len(ln.Runs[ri+1].Glyphs) > 0 {
				next = ln.Runs[ri+1].Glyphs[0].X
			}
			if x < (g.X+next)/2 {
				return int(g.Cluster)
			}
		}
	}
	return len(text)
}

// revealCaret scrolls the text so that the caret is inside the field.
//
// It is the field's own [gift.App.ScrollIntoView]: a single line field is not
// an [HScroll] — the content of a scroll container is its children and glyphs
// are nobody's children — so it keeps one offset of its own and moves it by
// the least amount that puts the caret back in the window.
//
// The trailing slack matters. Without the second branch, a caret at the end of
// a text exactly as wide as the field would sit on the right edge, half of it
// clipped away; with it, the text is pulled one caret width further left.
func (n *textFieldNode) revealCaret() {
	w := n.inner.Width()
	if !(w > 0) {
		return
	}
	x := caretX(n.para(), n.ed.caret)
	switch {
	case x-n.ed.scroll < 0:
		n.ed.scroll = x
	case x-n.ed.scroll > w-caretWidth:
		n.ed.scroll = x - w + caretWidth
	}
	n.clampScroll()
}

// clampScroll keeps the offset between zero and the end of the text.
//
// Either side of that range shows empty space where the text is: a negative
// offset a gap on the left, one past the end a field that has gone blank. Only
// shrinking text can produce them — moving the caret cannot — and text shrinks
// on three paths, which is why this is its own function and is called from all
// three. [textFieldNode.revealCaret] is the editing one. [textFieldNode.Layout]
// is the one that covers a *width* that changed, including the first layout
// after a rebuild. And [TextEditor.SetText] does not come here at all: it sets
// the offset to zero outright, because a wholly new document has no position
// worth preserving.
func (n *textFieldNode) clampScroll() {
	w := n.inner.Width()
	p := n.para()
	if !(w > 0) || len(p.Lines) == 0 {
		return
	}
	if max := p.Lines[0].Advance - w + caretWidth; n.ed.scroll > max {
		n.ed.scroll = max
	}
	if n.ed.scroll < 0 {
		n.ed.scroll = 0
	}
}

// --- paint -------------------------------------------------------------------

// Paint draws the field in the fixed order of the project plan, section 8:
// background, content, border — with the selection under the glyphs, the caret
// over them, and the focus ring last of all.
//
// The content is clipped to the bounds and the clip is pushed here rather than
// left to gift, for the reason [textNode.Paint] gives: the content of this
// node is its glyphs and not its children, so [gift.PaintContext.PaintChildren],
// where gift would apply [gift.Element.Clip], is never reached.
func (n *textFieldNode) Paint(ctx *gift.PaintContext) {
	// In front of the focus ring gate below; see [assertResolved] and
	// [buttonNode.Paint], which has the same gate for the same reason.
	assertResolvedBorder(n.focusRing, "the focus ring of a TextField")

	ia := ctx.Interaction()
	st := n.st
	if ia.Disabled {
		st.background = n.disabledBG
	}
	b := ctx.Bounds()
	paintBackground(ctx, st, b)

	ctx.PushClip(ctx.DeviceBounds())
	n.paintContent(ctx, b, ia)
	ctx.PopClip()

	paintBorder(ctx, st, b)
	if ia.FocusVisible && !ia.Disabled && n.focusRing.IsVisible() {
		paintBorder(ctx, styleSpec{border: n.focusRing, radius: focusRingRadius(st.radius)}, focusRingRect(b))
	}
}

// paintContent draws the selection, the text or the placeholder, and the
// caret.
//
// The four assertions are the first four statements, in front of every
// visibility gate below them, for the reason [assertResolved] gives: an
// unresolved semantic colour is *transparent*, so each of those gates would
// drop its operation and the field would lay out, measure and hit test
// perfectly while drawing nothing at all. TestEveryVisibilityGateIsGuarded
// enumerates all four by hand, and TestTheGateInventoryIsComplete is what
// notices a *function* that grows a gate; neither would notice a fifth colour
// gated inside this one, so a fifth colour belongs in that table by hand.
func (n *textFieldNode) paintContent(ctx *gift.PaintContext, b geom.Rect, ia gift.Interaction) {
	assertResolved(n.fg, "the foreground of a TextField")
	assertResolved(n.placeholderFG, "the placeholder colour of a TextField")
	assertResolved(n.selectionBG, "the selection colour of a TextField")
	assertResolved(n.caretColor, "the caret colour of a TextField")

	p := n.para()
	line := &p.Lines[0]
	x := roundf(b.Min.X + n.pad.Left - n.ed.scroll)
	baseline := roundf(b.Min.Y + n.pad.Top + line.Baseline)
	top := b.Min.Y + n.pad.Top
	bottom := b.Max.Y - n.pad.Bottom

	fg := n.fg
	if ia.Disabled {
		fg = n.disabledFG
	}

	if n.ed.Len() == 0 {
		if n.placeholder != "" && !n.placeholderFG.IsTransparent() {
			n.paintLine(ctx, b, n.phReq, n.placeholderFG, x, baseline)
		}
	} else {
		if lo, hi := n.ed.selection(); lo != hi && ia.Focused && !n.selectionBG.IsTransparent() {
			r := geom.Rc(x+caretX(p, lo), top, x+caretX(p, hi), bottom)
			ctx.Add(render.Op{Kind: render.OpFillRect, Bounds: r, Color: n.selectionBG})
		}
		if !fg.IsTransparent() {
			n.paintLine(ctx, b, n.req, fg, x, baseline)
		}
	}

	if ia.Focused && !ia.Disabled && !n.caretColor.IsTransparent() &&
		n.ed.caretVisible(ctx.Now()) {
		cx := roundf(x + caretX(p, n.ed.caret))
		ctx.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(cx, top, cx+caretWidth, bottom),
			Color:  n.caretColor,
		})
	}
}

// paintLine copies the glyphs of the single line of req into the display list
// at the given origin. It is [textNode.paintGlyphs] for one line and with a
// horizontal offset, and it emits one operation for the whole run.
func (n *textFieldNode) paintLine(ctx *gift.PaintContext, b geom.Rect, req text.Request, fg Color, x, baseline float32) {
	p := text.Default().Layout(req)
	if len(p.Lines) == 0 {
		return
	}
	ln := &p.Lines[0]
	first := ctx.GlyphsLen()
	for ri := range ln.Runs {
		run := &ln.Runs[ri]
		id := render.FontID(run.Font.ID())
		for gi := range run.Glyphs {
			g := &run.Glyphs[gi]
			ctx.AppendGlyph(render.Glyph{
				Font: id,
				ID:   render.GlyphID(g.ID),
				Size: run.Size,
				X:    x + g.X,
				Y:    baseline + g.Y,
			})
		}
	}
	if count := ctx.GlyphsLen() - first; count > 0 {
		ctx.Add(render.Op{
			Kind:       render.OpGlyphs,
			Bounds:     b,
			Color:      fg,
			Glyphs:     first,
			GlyphCount: count,
		})
	}
}

// --- input -------------------------------------------------------------------

// HandleEvent implements gift.Interactor.
//
// # The one trap, and what it costs
//
// A drag inside a scroll container normally has its press taken away: the
// container recognises the movement past [gift.DragSlop] and calls
// [gift.EventContext.StealPointer], which is exactly what should happen to a
// button and exactly what must not happen to a caret drag. The defence is the
// one [ScrollBar] uses for its thumb: this handler *consumes* every move while
// it is selecting, so the move never bubbles to the container, the container
// never recognises a drag, and the steal never happens.
//
// Consuming every move unconditionally, from the press onwards, would be the
// simple version and it has a price the simple version does not state: a form
// whose rows are mostly text fields could not be scrolled by dragging any of
// them. That is not a corner case on the kiosk of the project plan, section
// 19, where a finger arrives as a mouse under X11 and lands wherever the row
// happens to put a field. Measured on a column of one field and eleven boxes:
// a swipe over the field moved the form zero of a possible three hundred and
// thirteen pixels, a swipe over a box moved it eighty.
//
// So the gesture is decided by direction, once, at the slop:
//
//   - Between the press and [gift.DragSlop] nothing is consumed and nothing is
//     selected. Both this field and the container above it see every move, and
//     neither has claimed anything yet.
//   - On the first move past the slop, a gesture that has travelled further
//     down or up than sideways is *not* a selection. The field drops it, the
//     move bubbles, and the container scrolls as it would over any other row.
//   - Otherwise the field takes the capture and consumes every move until the
//     release.
//
// The cost of *this* version, stated: a caret drag that begins with a stroke
// more vertical than horizontal scrolls the form instead of selecting, and the
// first few pixels of every drag select nothing. Both are the standard
// behaviour of a text field in a scrolling list, and the alternative — decide
// on [gift.Event.Device] — is the one thing section 19 rules out by name,
// because on the target platform a touch *is* a mouse and the branch would
// never be taken where it is needed.
func (n *textFieldNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if n.disabled && e.Kind != gift.EventFocusLost {
		// A disabled field takes no input, and the core does not offer it
		// any: see [gift.App.deliver]. The one thing it is still told is that
		// the focus has gone, because a field that is disabled in the very
		// build that takes the focus off it is still holding a ten second
		// caret enrolment and possibly a request for the on-screen keyboard,
		// and it is the only thing in the process that can let go of them.
		// gift delivers that one through [gift.App.notifyDirect], which is
		// the core telling a node about a transition the core made rather
		// than input arriving at a control.
		return false
	}
	switch e.Kind {
	case gift.EventFocusGained:
		n.ed.restartBlink(e.Time)
		ctx.Animate(CaretBlinkWindow)
		// A field at the bottom of a scrolling form that was reached by
		// tabbing is off screen; this is what brings it back. It is a jump and
		// it moves every container above the field, which is what
		// [gift.App.ScrollIntoView] does and what the project plan, section
		// 19, names it for.
		ctx.ScrollIntoView()
		// The on-screen keyboard of the project plan, section 19. The switch
		// is read here rather than in gift, because gift has no business
		// knowing what a kiosk is, and it is read *before* the request rather
		// than inside it so that an application which is not a kiosk pays one
		// atomic load per focus change and no rebuild at all; see
		// [SetOnScreenKeyboard].
		if onScreenKeyboard.Load() {
			ctx.RequestSoftKeyboard(true)
		}
		return true

	case gift.EventFocusLost:
		if onScreenKeyboard.Load() {
			// Dismissed on blur, unconditionally. A focus move from one field
			// to the next is a lost and a gained in that order, so the
			// keyboard is taken down and put back up within one dispatch and
			// the user sees it stay; the cost is one extra build, once per
			// field the user walks through.
			ctx.RequestSoftKeyboard(false)
		}
		n.ed.drag = dragNone
		// Stop asking for frames. Without this the application would keep
		// ticking at full rate for the rest of the blink window with nothing
		// on screen to show for it. The value is not touched: every edit is
		// committed the moment it happens, so there is nothing to commit here
		// and nothing to revert; see [TextFieldView.OnChange].
		ctx.Animate(0)
		return true

	case gift.EventPointerDown:
		ctx.RequestFocus()
		n.press(ctx, e)
		return true

	case gift.EventPointerMove:
		return n.move(ctx, e)

	case gift.EventPointerUp, gift.EventPointerCancel:
		if n.ed.drag == dragNone {
			return false
		}
		n.ed.drag = dragNone
		return true

	case gift.EventKeyDown:
		return n.key(ctx, e)

	case gift.EventRune:
		// A rune with the shortcut modifier held is the tail of a shortcut
		// that the platform did not filter out, not a character somebody
		// meant to type. Ebitengine's own callback drops most of them; this
		// is the belt and braces for the ones it does not.
		if e.Mods.Has(gift.ShortcutModifier) {
			return false
		}
		n.ed.InsertRune(e.Rune)
		n.afterEdit(ctx, e.Time)
		return true
	}
	return false
}

// move is the drag half of [textFieldNode.HandleEvent], where the gesture is
// decided and then carried out.
//
// A drag that rests outside the field does not scroll the text further by
// itself: the offset only follows the caret, and the caret only follows a
// move. Holding the pointer still past the left edge therefore stops at
// whatever the last move reached, rather than running to the start of the
// document the way a list with an auto scroll timer would. That is a deliberate
// omission and it is stated on [TextFieldView] as one.
func (n *textFieldNode) move(ctx *gift.EventContext, e gift.Event) bool {
	ed := n.ed
	switch ed.drag {
	case dragNone:
		return false

	case dragPending:
		if !e.Dragged {
			// Still inside the slop, exactly as [scrollHandler] reads it. The
			// gesture may still become a tap, a selection or a scroll, and
			// nobody may consume the move until it is one of them.
			return false
		}
		dx, dy := absf(e.Pos.X-ed.dragFrom.X), absf(e.Pos.Y-ed.dragFrom.Y)
		if dy > dx {
			// The container's gesture. Give it up for good rather than for
			// this event: a stroke that starts downwards is a scroll even if
			// it turns sideways later, and re-deciding per move would tear the
			// selection out of the middle of a fling.
			ed.drag = dragNone
			return false
		}
		if ed.clicks >= 2 {
			ed.drag = dragWord
		} else {
			ed.drag = dragChar
		}
		// Only now, and this is the whole of the capture: the press was ours
		// already — the field is the node the hit test found — and what this
		// takes back is a capture a container may have claimed in the same
		// move.
		ctx.StealPointer()
	}

	off := n.offsetOf(ctx, e.Pos)
	if ed.drag == dragWord {
		ed.dragExtendWord(off)
	} else {
		ed.MoveCaret(off, true)
	}
	n.afterCaretMove(ctx, e.Time)
	return true
}

// press places the caret, or selects a word or everything, depending on how
// many clicks in a row this is.
//
// Every count arms the drag, not just the first: a double click and drag
// extends by whole words on every platform there is, and a version that armed
// only the single click neither did that nor consumed the moves, so the same
// gesture scrolled the form instead. The third click has the whole document
// selected and nothing left to extend, so it arms nothing and a drag after it
// belongs to the container.
func (n *textFieldNode) press(ctx *gift.EventContext, e gift.Event) {
	x := e.Pos.X
	if e.Time-n.ed.clickAt <= DoubleClickInterval && absf(x-n.ed.clickX) <= DoubleClickSlop {
		n.ed.clicks++
	} else {
		n.ed.clicks = 1
	}
	n.ed.clickAt, n.ed.clickX = e.Time, x
	n.ed.dragFrom = e.Pos

	off := n.offsetOf(ctx, e.Pos)
	switch n.ed.clicks {
	case 1:
		// Shift-click extends the existing selection instead of starting a
		// new one, which is the other half of shift-arrow and the way a long
		// selection is made with two clicks rather than one careful drag.
		n.ed.MoveCaret(off, e.Mods.Has(gift.ModShift))
		n.ed.drag = dragPending
	case 2:
		n.ed.SelectWordAt(off)
		n.ed.dragWordLo, n.ed.dragWordHi = n.ed.selection()
		n.ed.drag = dragPending
	default:
		n.ed.SelectAll()
		n.ed.drag = dragNone
	}
	n.afterCaretMove(ctx, e.Time)
}

// key is the keyboard half. Every branch that changes the caret or the text
// returns true, so nothing it acts on bubbles to an ancestor; tab is
// deliberately not one of them, which is what lets gift move the focus out of
// a field that has it.
func (n *textFieldNode) key(ctx *gift.EventContext, e gift.Event) bool {
	ed := n.ed
	extend := e.Mods.Has(gift.ModShift)
	shortcut := e.Mods.Has(gift.ShortcutModifier)
	word := e.Mods.Has(gift.WordModifier)

	switch e.Key {
	case gift.KeyLeft:
		// The word modifier is tested *first*, and that order is the whole of
		// the collision rule [gift.WordModifier] states: on X11 and on Windows
		// the two modifiers are the same key, and the convention there is that
		// control-left is the previous word. Testing the shortcut first made
		// [TextEditor.MoveWordLeft] unreachable from the keyboard on the
		// platform of the project plan, section 1.
		switch {
		case word:
			ed.MoveWordLeft(extend)
		case shortcut:
			ed.MoveHome(extend)
		default:
			ed.MoveLeft(extend)
		}
	case gift.KeyRight:
		switch {
		case word:
			ed.MoveWordRight(extend)
		case shortcut:
			ed.MoveEnd(extend)
		default:
			ed.MoveRight(extend)
		}
	case gift.KeyHome:
		ed.MoveHome(extend)
	case gift.KeyEnd:
		ed.MoveEnd(extend)
	case gift.KeyUp:
		// One line, so up and down are the ends of it. They are handled
		// rather than ignored because a field that let them bubble would
		// scroll the form under the caret, which is not what a user pressing
		// up inside a text field means.
		ed.MoveHome(extend)
	case gift.KeyDown:
		ed.MoveEnd(extend)

	case gift.KeyBackspace:
		if ed.DeleteBackward() {
			n.afterEdit(ctx, e.Time)
		}
		return true
	case gift.KeyDelete:
		if ed.DeleteForward() {
			n.afterEdit(ctx, e.Time)
		}
		return true

	case gift.KeyEnter:
		if n.onSubmit != nil {
			n.onSubmit(ed.Text())
		}
		return true

	case gift.KeyA:
		if !shortcut {
			return false
		}
		ed.SelectAll()
	case gift.KeyC:
		if !shortcut {
			return false
		}
		n.copy()
		return true
	case gift.KeyX:
		if !shortcut {
			return false
		}
		if n.copy() && ed.DeleteSelection() {
			n.afterEdit(ctx, e.Time)
		}
		return true
	case gift.KeyV:
		if !shortcut {
			return false
		}
		if s, ok := CurrentClipboard().Text(); ok {
			if s = singleLine(s); s != "" && ed.Insert(s) {
				n.afterEdit(ctx, e.Time)
			}
		}
		return true

	default:
		return false
	}
	n.afterCaretMove(ctx, e.Time)
	return true
}

// copy puts the selection on the clipboard and reports whether it is now
// there. An empty selection is not a copy: silently replacing the clipboard
// with nothing is how a user loses what they had in it.
//
// The return value is what Ctrl+X hangs its deletion on, so "there was
// something to copy" is not good enough an answer. A [CheckedClipboard] is
// asked for the error and a failed write reports false, which leaves the
// selection in the document; that is the whole of the defence against a cut
// that destroys text it never managed to copy. An X11 owner that does not
// answer, a display connection that died with the session, a platform layer
// that failed to open — all of them come back as an error here and none of
// them is exotic on a kiosk.
//
// A plain [Clipboard] reports true, because a two method implementation has no
// way to say anything else and refusing to cut into one would break every
// clipboard that works. [MemoryClipboard], the default, is such an
// implementation.
func (n *textFieldNode) copy() bool {
	s := n.ed.SelectedText()
	if s == "" {
		return false
	}
	cb := CurrentClipboard()
	if w, ok := cb.(CheckedClipboard); ok {
		return w.SetTextErr(s) == nil
	}
	cb.SetText(s)
	return true
}

// singleLine is what a paste has to survive to get into a single line field.
//
// Everything from the first line break onwards is dropped, and control
// characters elsewhere are dropped too. Two honest alternatives were
// available: refuse a multi line paste entirely, which loses the line the user
// almost certainly wanted, or join the lines with spaces, which invents text
// nobody wrote. Taking the first line is what every single line field does.
//
// A tab is a control character and goes with the rest, because inserting one
// into a field where tab means "next field" produces a glyph the user cannot
// see and cannot delete by pressing tab again.
func singleLine(s string) string {
	if i := strings.IndexAny(s, "\n\r\v\f\u0085\u2028\u2029"); i >= 0 {
		s = s[:i]
	}
	if strings.IndexFunc(s, isNotPrintable) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isNotPrintable(r) {
			return -1
		}
		return r
	}, s)
}

func isNotPrintable(r rune) bool { return !unicode.IsPrint(r) }

// afterEdit is the one place a change to the document leaves the widget.
//
// The value is committed immediately and always: there is no "commit on blur"
// and no pending state, so an application that reads the binding or the
// callback sees what is on the screen at the moment it is on the screen. The
// alternative — hold the edit and publish it on blur or on enter — is a second
// copy of the value that can disagree with the one the user is looking at, and
// the way it fails is that a form submitted by a button click keeps the
// previous value because the field never lost the focus.
func (n *textFieldNode) afterEdit(ctx *gift.EventContext, now time.Duration) {
	s := n.ed.Text()
	if !n.bind.IsZero() {
		n.bind.Set(s)
	}
	if n.onChange != nil {
		n.onChange(s)
	}
	n.afterCaretMove(ctx, now)
}

// afterCaretMove restarts the blink, keeps the caret in view and asks for the
// frames the blink needs.
func (n *textFieldNode) afterCaretMove(ctx *gift.EventContext, now time.Duration) {
	n.ed.restartBlink(now)
	n.revealCaret()
	ctx.Animate(CaretBlinkWindow)
	ctx.Repaint()
}

// offsetOf turns a device space pointer position into a byte offset in the
// document.
//
// The comparison happens in device space, which is the space [gift.Event.Pos]
// lives in; inside a scroll container that is not the same as the local space
// the layout put the field in. See [gift.EventContext.DeviceBounds].
func (n *textFieldNode) offsetOf(ctx *gift.EventContext, p geom.Point) int {
	b := ctx.DeviceBounds()
	x := p.X - b.Min.X - n.pad.Left + n.ed.scroll
	return offsetAt(n.para(), n.ed.Text(), x)
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// --- the editor's view side state --------------------------------------------

// restartBlink puts the caret into the visible half of a fresh blink cycle and
// opens a new blink window.
func (e *TextEditor) restartBlink(now time.Duration) {
	e.blinkAt = now
	e.blinkUntil = now + CaretBlinkWindow
}

// caretVisible reports whether the caret is in the visible half of its blink
// cycle at now. After [CaretBlinkWindow] it is always visible; see that
// constant for why the blinking stops.
func (e *TextEditor) caretVisible(now time.Duration) bool {
	if now >= e.blinkUntil || now < e.blinkAt {
		return true
	}
	return (now-e.blinkAt)/CaretBlinkInterval%2 == 0
}

// checkSingleMount rejects a second [TextFieldView] over the same
// [TextEditor], with the argument and the mechanism of
// [Gallery.checkSingleMount]: two mounted views would be two carets writing
// one position and two scroll offsets fighting over one number, in the same
// frame, with no symptom a user could describe.
func (e *TextEditor) checkSingleMount(ctx *gift.LayoutContext) {
	node, pass := ctx.Node(), ctx.Pass()
	if e.haveOwner && e.ownerPass == pass && e.owner != node {
		panic("gift/ui: two TextField views over one ui.TextEditor; an editor holds a single " +
			"caret, selection and scroll offset, and two mounted fields would write all three " +
			"against two different geometries in the same frame. Create one ui.NewTextEditor " +
			"per field.")
	}
	e.owner, e.ownerPass, e.haveOwner = node, pass, true
}

// --- modifiers ---------------------------------------------------------------

// Placeholder sets the text shown while the field is empty, in
// [ColorSecondaryLabel] unless [TextFieldView.PlaceholderColor] says otherwise.
//
// It doubles as the accessible name of the field when no
// [TextFieldView.Label] was given, because "Your name" is what a person would
// call that field; see [gift.Element.Label].
func (v TextFieldView) Placeholder(s string) TextFieldView { v.placeholder = s; return v }

// Label sets the accessible name of the field, overriding the placeholder for
// that purpose. It is never drawn.
func (v TextFieldView) Label(s string) TextFieldView { v.name = s; return v }

// OnChange registers a callback that runs after every change to the text, with
// the new value.
//
// Every change, immediately: one keystroke is one call. There is no commit on
// blur and no debounce here — a debounce is a decision about the *consumer*
// of the value, and a field that made it would be wrong for the consumer that
// wants to enable a button as you type and for the one that wants to hit a
// database, in opposite directions.
func (v TextFieldView) OnChange(f func(string)) TextFieldView { v.onChange = f; return v }

// OnSubmit registers a callback that runs when the user presses enter in the
// field, with the current value. It is the "go" of a search box and the
// default button of a form.
func (v TextFieldView) OnSubmit(f func(string)) TextFieldView { v.onSubmit = f; return v }

// Bind connects the field to a [gift.State] holding the text, in both
// directions: every edit writes the state, and a value written to the state
// from anywhere else replaces the document.
//
// It is [gift.Binding] and not a pair of callbacks because a binding is what
// this framework already has for exactly this — see [gift.State.Binding] — and
// because the read half registers the dependency that makes the external
// direction work at all. The field still needs a [TextEditor]: the state holds
// the value, and the editor holds the caret, the selection and the scroll,
// which are not values an application wants to own.
//
// Binding and [TextFieldView.OnChange] compose: both run, the binding first.
func (v TextFieldView) Bind(b gift.Binding[string]) TextFieldView { v.bind = b; return v }

// Disabled takes the field out of input: it is skipped by the focus order,
// receives no events, shows no caret and draws in a quieter colour on the
// disabled control face. It still occupies its place and still blocks clicks
// from reaching what is behind it.
func (v TextFieldView) Disabled(b bool) TextFieldView { v.disabled = b; return v }

// Font sets the font the text is shaped and drawn with, overriding
// [SetDefaultFont] for this field only.
func (v TextFieldView) Font(f Font) TextFieldView { v.font = f; return v }

// FontSize sets the em size in pixels. It also sets the height of the field,
// which is one line box of this font at this size.
func (v TextFieldView) FontSize(s float32) TextFieldView {
	if !isFinite(s) || s <= 0 {
		panic("gift/ui: TextField.FontSize must be a finite number greater than zero")
	}
	v.size = s
	return v
}

// Foreground sets the colour of the text. A semantic colour is resolved in
// Build like everywhere else.
func (v TextFieldView) Foreground(c Color) TextFieldView { v.fg, v.hasFG = c, true; return v }

// PlaceholderColor sets the colour of the placeholder text.
func (v TextFieldView) PlaceholderColor(c Color) TextFieldView {
	v.placeholderFG, v.hasPlaceholderFG = c, true
	return v
}

// SelectionColor sets the highlight drawn behind selected text. The default is
// [ColorAccent] at thirty percent; see [defaultSelectionColor].
func (v TextFieldView) SelectionColor(c Color) TextFieldView {
	v.selection, v.hasSelection = c, true
	return v
}

// CaretColor sets the colour of the caret. The default is the text colour.
func (v TextFieldView) CaretColor(c Color) TextFieldView { v.caret, v.hasCaret = c, true; return v }

// FocusRing sets the border drawn on top of the field while it holds the
// keyboard focus. A zero width border removes it.
func (v TextFieldView) FocusRing(b Border) TextFieldView {
	v.focusRing, v.hasFocusRing = b, true
	return v
}

// Padding sets the same padding on all four edges around the text, replacing
// the default of 5 by 8.
func (v TextFieldView) Padding(f float32) TextFieldView {
	v.setPadding(f)
	v.hasPadding = true
	return v
}

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (v TextFieldView) PaddingInsets(i geom.Insets) TextFieldView {
	v.setPaddingInsets(i)
	v.hasPadding = true
	return v
}

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free; the width then falls back to [defaultFieldWidth] and the height to one
// line of the font.
func (v TextFieldView) Frame(w, h float32) TextFieldView { v.setFrame(w, h); return v }

// MinWidth raises the minimum width of the field, and the maximum with it if
// that is lower; see [frameSpec].
func (v TextFieldView) MinWidth(f float32) TextFieldView { v.setMinWidth(f); return v }

// MinHeight raises the minimum height of the field, and the maximum with it if
// that is lower; see [frameSpec].
func (v TextFieldView) MinHeight(f float32) TextFieldView { v.setMinHeight(f); return v }

// MaxWidth lowers the maximum width of the field, and the minimum with it if
// that is higher; see [frameSpec].
func (v TextFieldView) MaxWidth(f float32) TextFieldView { v.setMaxWidth(f); return v }

// MaxHeight lowers the maximum height of the field, and the minimum with it if
// that is higher; see [frameSpec]. The text stays one line tall inside it.
func (v TextFieldView) MaxHeight(f float32) TextFieldView { v.setMaxHeight(f); return v }

// Background fills the bounds behind the text, replacing the themed
// [ColorControl].
func (v TextFieldView) Background(b Background) TextFieldView { v.setBackgroundSpec(b); return v }

// Border strokes the inside of the bounds, replacing the themed hairline.
func (v TextFieldView) Border(b Border) TextFieldView { v.setBorder(b); return v }

// Shadow draws a blurred copy of the field's box behind it. It extends the
// paint bounds but not the layout size and not the hit area.
func (v TextFieldView) Shadow(s Shadow) TextFieldView { v.setShadow(s); return v }

// CornerRadius rounds the background and the border, replacing the default.
func (v TextFieldView) CornerRadius(f float32) TextFieldView { v.setCornerRadius(f); return v }

// Clip is accepted only as Clip(true), which is what a field already does.
//
// A field that did not clip would paint the scrolled out part of its text
// across its neighbours, and the horizontal scrolling that makes a long value
// editable at all is built on that clip. [ScrollView.Clip] refuses the same
// thing for the same reason; the package documentation promises that a view
// never accepts a modifier it then ignores, and this is how the promise is
// kept here.
func (v TextFieldView) Clip(b bool) TextFieldView {
	if !b {
		panic("gift/ui: Clip(false) on a TextField; the text scrolls inside the field, so a " +
			"field that did not clip would paint the scrolled out part over its neighbours")
	}
	return v
}

// Key sets the reconciliation key of this view among its siblings.
func (v TextFieldView) Key(s string) TextFieldView { v.setKey(s); return v }

// Flex makes the field take a share of the remaining main axis space of its
// parent stack, proportional to f. In an [HStack] it is the usual way to make
// a field fill a row.
func (v TextFieldView) Flex(f float32) TextFieldView { v.setFlex(f); return v }
