package ui

import (
	"fmt"
	"math"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/text"
	"github.com/torbenschinke/gift/render"
)

var textType = gift.RegisterType("ui.Text")

// TextAlign is the horizontal alignment of the lines of a [TextView] inside
// its own width.
//
// It only does anything when the lines are narrower than the node: a single
// line label that is exactly as wide as its text looks the same under all
// three. Give the view a [TextView.Frame] or a [TextView.Flex] and the
// alignment becomes visible.
type TextAlign uint8

const (
	// AlignLeading puts every line at the left edge. This is the default.
	AlignLeading TextAlign = iota
	// AlignCenter centres every line.
	AlignCenter
	// AlignTrailing puts every line at the right edge.
	AlignTrailing
)

// DefaultFontSize is the em size a [TextView] uses when [TextView.FontSize]
// was not called.
const DefaultFontSize = 14

// DefaultForeground is the colour a [TextView] uses when
// [TextView.Foreground] was not called: opaque black, the convention every
// other toolkit uses.
//
// It is stated here rather than left implicit because the zero [Color] is
// fully transparent, and defaulting to that would make an unstyled label
// invisible — the exact failure this package works to avoid elsewhere.
var DefaultForeground = RGB(0, 0, 0)

// TextView is a run of text. It is created by [Text]; the zero value is not
// useful.
//
// # Size
//
// A TextView measures itself by shaping its string against the width its
// constraints allow and reporting the extent of the result. That is the same
// shaping result the glyphs are drawn from — internal/text has exactly one
// entry point and it returns a size and the glyphs it was computed from
// together — so a label can never be measured differently from how it is
// drawn.
//
// Wrapping happens at word boundaries. A single word wider than the limit is
// *not* broken, hyphenated, scaled or truncated: it keeps its honest width,
// the node reports the width its constraints permit, and the difference is
// reported through [gift.LayoutContext.ReportOverflow] and shows up in
// [gift.Diagnostics] like every other overflow. That is the text form of the
// overflow model of the project plan, section 7.
//
// # Font
//
// gift ships no font. A TextView uses [TextView.Font] if it was given one and
// the application wide [SetDefaultFont] otherwise; with neither, building it
// panics with an explanation rather than rendering nothing. See
// [SetDefaultFont].
type TextView struct {
	base
	s        string
	font     Font
	size     float32
	fg       Color
	hasFG    bool
	align    TextAlign
	maxLines int
}

// Text returns a text view for s at [DefaultFontSize] in [DefaultForeground].
//
// The string is kept by value and is immutable, so unlike a children slice it
// carries no ownership transfer; see the project plan, section 4.
func Text(s string) TextView { return TextView{s: s} }

// ViewType implements gift.View.
func (t TextView) ViewType() gift.TypeID { return textType }

// Build implements gift.View.
//
// This is where the font is resolved, which means a missing font is diagnosed
// during build and never during a frame; see [resolveFont].
func (t TextView) Build(*gift.BuildContext) gift.Element {
	size := t.size
	if size <= 0 {
		size = DefaultFontSize
	}
	fg := t.fg
	if !t.hasFG {
		fg = DefaultForeground
	}
	n := &textNode{
		fr:       t.frame,
		st:       t.style,
		pad:      t.pad,
		fg:       fg,
		align:    t.align,
		maxLines: t.maxLines,
		req: text.Request{
			Text: t.s,
			Font: resolveFont(t.font),
			Size: size,
		},
	}
	return gift.Element{
		Key:      t.key,
		Flex:     t.flex,
		Layouter: n,
		Painter:  n,
		Clip:     t.style.clip,
		// The semantic half of a label: the very string the glyphs below
		// spell. It costs one string header per build and is read by nothing
		// in the frame path; see [gift.Element.Label].
		Label: t.s,
	}
}

// textNode is the retained half of a [TextView]: its layouter and its painter.
//
// It holds the [text.Request] rather than a shaping result, and that is the
// whole lifetime story. A *text.Paragraph is borrowed from the shaping cache
// and is valid only until the next Layout or Tick call, so keeping one across
// the gap between Update and Draw would be reading recycled memory. Holding
// the request instead means paint asks the cache the same question layout
// asked and gets the same answer, for free: the lookup is a map hit and a hit
// allocates nothing.
type textNode struct {
	fr  frameSpec
	st  styleSpec
	pad geom.Insets

	req      text.Request
	fg       Color
	align    TextAlign
	maxLines int

	// lines is how many of the paragraph's lines are drawn, which differs
	// from the paragraph's own count only when MaxLines cut it short.
	lines int
}

// Layout implements gift.Layouter.
//
// The width limit handed to the shaper is the one the constraints allow after
// padding, which may be [geom.Unbounded] — a label in a stack is measured with
// an unbounded main axis under rule 1 of the overflow model, but its *cross*
// axis is bounded, and for a vertical stack that is exactly the axis text
// wraps on.
func (n *textNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	cc := n.fr.apply(c)
	inner := cc.Deflate(n.pad)
	n.req.MaxWidth = inner.Max.W

	p := text.Default().Layout(n.req)
	m := p.Metrics

	h := p.Size.H
	n.lines = len(p.Lines)
	var cut float32
	if n.maxLines > 0 && n.lines > n.maxLines {
		n.lines = n.maxLines
		h = m.FirstBaseline + float32(n.maxLines-1)*m.LineHeight + ceilf(m.Descent)
		cut = p.Size.H - h
	}

	size := geom.Sz(p.Size.W+n.pad.Horizontal(), h+n.pad.Vertical())
	// The reported size is what the constraints permit and the excess is a
	// number, which is rule 3 of the overflow model: the content keeps its
	// honest extent, the container reports the allowed one, and the
	// difference is visible in gift.Diagnostics instead of being silently
	// clipped or silently grown.
	out := cc.Constrain(size)
	ctx.ReportOverflow(geom.Sz(sizeOverflow(size.W, out.W), sizeOverflow(size.H, out.H)+cut))
	// The first baseline of the paragraph, offset by the top padding. Nothing
	// reads it yet; see gift.LayoutContext.ReportBaseline for why the channel
	// exists before a consumer does.
	ctx.ReportBaseline(n.pad.Top + m.FirstBaseline)
	return out
}

// Paint implements gift.Painter, in the fixed drawing order of the project
// plan, section 8: background, content, border.
//
// The clip is pushed here and not left to gift, because a TextView's content
// is its glyphs and not its children: [gift.PaintContext.PaintChildren], where
// gift applies [gift.Element.Clip] for every container, is never reached. This
// is the one place in the package that still spells the clip out, and the
// rectangle is deliberately the same one gift would have used — the node's
// bounds — so the input half and this half cannot disagree.
func (n *textNode) Paint(ctx *gift.PaintContext) {
	b := ctx.Bounds()
	paintBackground(ctx, n.st, b)
	if n.st.clip {
		ctx.PushClip(b)
	}
	n.paintGlyphs(ctx, b)
	if n.st.clip {
		ctx.PopClip()
	}
	paintBorder(ctx, n.st, b)
}

// paintGlyphs copies the positioned glyphs of the paragraph into the display
// list and emits one operation spanning them.
//
// One operation per view and not per line: a display list operation carries a
// colour and a clip, and every line of one label shares both. Splitting per
// line would multiply the op count by the line count for no gain, and the
// backend batches by material anyway.
//
// Every position written here is a whole number. The project plan, section 7,
// rules out subpixel positioning, internal/text already rounds glyph offsets
// within a line, and the line origin is rounded here — which is the one place
// a fractional node bounds could otherwise have leaked a subpixel phase into
// the atlas.
func (n *textNode) paintGlyphs(ctx *gift.PaintContext, b geom.Rect) {
	if n.fg.IsTransparent() || n.req.Text == "" {
		return
	}
	// The same request layout used, so this is a cache hit and allocates
	// nothing. It is a lookup, not a layout: nothing is measured or placed
	// here, which is what keeps App.Paint free of layout work.
	p := text.Default().Layout(n.req)

	lines := p.Lines
	if n.lines > 0 && n.lines < len(lines) {
		lines = lines[:n.lines]
	}
	innerW := b.Width() - n.pad.Horizontal()
	originX := b.Min.X + n.pad.Left
	originY := b.Min.Y + n.pad.Top

	first := ctx.GlyphsLen()
	for li := range lines {
		ln := &lines[li]
		lx := roundf(originX + alignOffset(n.align, innerW, ln.Width))
		by := roundf(originY + ln.Baseline)
		for ri := range ln.Runs {
			run := &ln.Runs[ri]
			id := render.FontID(run.Font.ID())
			for gi := range run.Glyphs {
				g := &run.Glyphs[gi]
				ctx.AppendGlyph(render.Glyph{
					Font: id,
					ID:   render.GlyphID(g.ID),
					Size: run.Size,
					X:    lx + g.X,
					Y:    by + g.Y,
				})
			}
		}
	}
	if count := ctx.GlyphsLen() - first; count > 0 {
		ctx.Add(render.Op{
			Kind:       render.OpGlyphs,
			Bounds:     b,
			Color:      n.fg,
			Glyphs:     first,
			GlyphCount: count,
		})
	}
}

// alignOffset is how far a line of width w is shifted inside an area of width
// avail. A line wider than the area is never shifted left, because pulling
// overflowing text out on both sides hides where it starts.
func alignOffset(a TextAlign, avail, w float32) float32 {
	slack := avail - w
	if slack <= 0 || !isFinite(slack) {
		return 0
	}
	switch a {
	case AlignCenter:
		return roundf(slack / 2)
	case AlignTrailing:
		return slack
	default:
		return 0
	}
}

// sizeOverflow is how far want exceeds got, clamped at zero.
func sizeOverflow(want, got float32) float32 {
	if d := want - got; d > 0 && isFinite(d) {
		return d
	}
	return 0
}

func roundf(v float32) float32 { return float32(math.Round(float64(v))) }
func ceilf(v float32) float32  { return float32(math.Ceil(float64(v))) }

// --- modifiers -------------------------------------------------------------
//
// Every one of these is a one line forwarder that exists only to return the
// concrete type, which is what variant A of the project plan, section 4,
// requires and what that section already records the price of: the surface
// grows as widgets times modifiers and the compiler does not notice a
// forgotten one. With Box, Stack and now Text there are three copies of the
// same fourteen methods, and section 4 says to decide about generating them
// before step 3. The recommendation from this work unit is in the report: the
// point has arrived, but a generator is not built here.

// Font sets the font this view is shaped and drawn with, overriding
// [SetDefaultFont] for this view only.
func (t TextView) Font(v Font) TextView { t.font = v; return t }

// FontSize sets the em size in pixels. A value that is not positive and finite
// panics, because it would reach the shaper as a malformed request and fail
// there with less context.
func (t TextView) FontSize(v float32) TextView {
	if !isFinite(v) || v <= 0 {
		panic(fmt.Sprintf("gift/ui: FontSize(%v) must be a finite number greater than zero", v))
	}
	t.size = v
	return t
}

// Foreground sets the colour the glyphs are drawn in. The glyph atlas holds
// coverage only, so a colour costs nothing: the same glyph in ten colours is
// one atlas entry.
func (t TextView) Foreground(v Color) TextView { t.fg, t.hasFG = v, true; return t }

// Align sets the horizontal alignment of the lines inside the node's width.
// It is visible only when the node is wider than its longest line; see
// [TextAlign].
func (t TextView) Align(v TextAlign) TextView { t.align = v; return t }

// MaxLines limits the view to the first n visual lines. Zero or less, the
// default, means no limit.
//
// # Why this is not a silent truncation
//
// The project plan, section 7, forbids content that does not fit from
// disappearing quietly, and MaxLines is the one modifier that deliberately
// drops content — so it pays the same price as everything else: the height of
// the lines that were cut is reported as vertical overflow and appears in
// [gift.Diagnostics]. The difference from an accidental collapse is that this
// one was asked for, at the call site, by name.
//
// It exists because the alternative for a fixed height row is worse. Without
// it a two line label in a list row either overflows into the row below it or
// forces every row to be tall enough for the longest entry, and both are
// decided by the data rather than by the design. There is no ellipsis: that
// needs a trailing glyph substitution the shaper does not do yet, and
// pretending otherwise by drawing "..." after a hard cut would put the
// ellipsis at a position no shaping result contains.
func (t TextView) MaxLines(v int) TextView { t.maxLines = v; return t }

// Padding sets the same padding on all four edges, replacing any previous
// padding. It must be finite and non negative; see [Stack.Padding].
func (t TextView) Padding(v float32) TextView { t.setPadding(v); return t }

// PaddingInsets sets the padding per edge, replacing any previous padding.
func (t TextView) PaddingInsets(v geom.Insets) TextView { t.setPaddingInsets(v); return t }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free. Fixing the width is also how a label is given a wrapping width
// independently of its parent.
//
// Precedence: Frame is applied first, then Max, then Min; see [frameSpec].
func (t TextView) Frame(w, h float32) TextView { t.setFrame(w, h); return t }

// MinWidth raises the minimum width of the node, and the maximum with it if
// that is lower; see [frameSpec].
func (t TextView) MinWidth(v float32) TextView { t.setMinWidth(v); return t }

// MinHeight raises the minimum height of the node, and the maximum with it if
// that is lower; see [frameSpec].
func (t TextView) MinHeight(v float32) TextView { t.setMinHeight(v); return t }

// MaxWidth lowers the maximum width of the node, and the minimum with it if
// that is higher. This is the usual way to give a label a wrapping width.
func (t TextView) MaxWidth(v float32) TextView { t.setMaxWidth(v); return t }

// MaxHeight lowers the maximum height of the node, and the minimum with it if
// that is higher; see [frameSpec].
func (t TextView) MaxHeight(v float32) TextView { t.setMaxHeight(v); return t }

// Background fills the bounds behind the glyphs.
func (t TextView) Background(v Color) TextView { t.setBackground(v); return t }

// Border strokes the inside of the bounds after the glyphs were drawn. It does
// not change the layout.
func (t TextView) Border(v Border) TextView { t.setBorder(v); return t }

// Shadow draws a blurred copy of the background shape behind the view.
//
// It extends the paint bounds but not the layout size and not the hit area, so
// a shadow never moves a sibling and never makes a gap clickable; the project
// plan, section 8, fixes that. A parent clip cuts it.
func (t TextView) Shadow(v Shadow) TextView { t.setShadow(v); return t }

// CornerRadius rounds the background and the border.
func (t TextView) CornerRadius(v float32) TextView { t.setCornerRadius(v); return t }

// Clip confines the glyphs to the bounds. Without it a line that overflows
// stays visible, which is the default the project plan, section 7, asks for.
func (t TextView) Clip(v bool) TextView { t.setClip(v); return t }

// Key sets the reconciliation key of this view among its siblings.
func (t TextView) Key(v string) TextView { t.setKey(v); return t }

// Flex makes the view take a share of the remaining main axis space of its
// parent stack, proportional to v.
func (t TextView) Flex(v float32) TextView { t.setFlex(v); return t }
