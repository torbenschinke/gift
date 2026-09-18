package gifttest

import (
	"fmt"
	"strings"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// boundsTolerance is how far an asserted rectangle may be off.
//
// Layout is float32 arithmetic over integral inputs, and a stack that divides
// leftover space can land a quarter of a pixel away from the number a test
// writes by hand. Half a pixel is below anything a human can see and far below
// anything a layout bug produces.
const boundsTolerance = 0.5

// --- node assertions --------------------------------------------------------

// AssertText fails unless the node's label is exactly want.
func (n Node) AssertText(want string) Node {
	n.h.t.Helper()
	n.check("AssertText")
	if got := n.Text(); got != want {
		n.h.t.Errorf("gifttest: %s\n  text = %q\n  want   %q",
			n.describe(), got, want)
	}
	return n
}

// AssertKey fails unless the node's reconciliation key is want.
func (n Node) AssertKey(want string) Node {
	n.h.t.Helper()
	n.check("AssertKey")
	if got := n.Key(); got != want {
		n.h.t.Errorf("gifttest: %s\n  key = %q\n  want  %q", n.describe(), got, want)
	}
	return n
}

// AssertType fails unless the node was built from the view type registered
// under want, such as "ui.Button".
func (n Node) AssertType(want string) Node {
	n.h.t.Helper()
	n.check("AssertType")
	if got := n.Type(); got != want {
		n.h.t.Errorf("gifttest: %s\n  type = %q\n  want   %q", n.describe(), got, want)
	}
	return n
}

// AssertBounds fails unless the node's rectangle equals want within
// [boundsTolerance], reporting the difference per edge.
func (n Node) AssertBounds(want geom.Rect) Node {
	n.h.t.Helper()
	n.check("AssertBounds")
	got := n.Bounds()
	if nearRect(got, want) {
		return n
	}
	n.h.t.Errorf("gifttest: %s\n  bounds = %s\n  want     %s\n  off by   left %+g, top %+g, right %+g, bottom %+g (tolerance %g)",
		n.describe(), rectString(got), rectString(want),
		got.Min.X-want.Min.X, got.Min.Y-want.Min.Y,
		got.Max.X-want.Max.X, got.Max.Y-want.Max.Y, boundsTolerance)
	return n
}

// AssertSize fails unless the node's size equals want within
// [boundsTolerance]. It is the assertion for a control whose position is the
// layout's business and whose extent is the test's.
func (n Node) AssertSize(want geom.Size) Node {
	n.h.t.Helper()
	n.check("AssertSize")
	got := n.Bounds().Size()
	if near(got.W, want.W) && near(got.H, want.H) {
		return n
	}
	n.h.t.Errorf("gifttest: %s\n  size = %gx%g\n  want   %gx%g", n.describe(), got.W, got.H, want.W, want.H)
	return n
}

// AssertEnabled fails when the node is disabled.
func (n Node) AssertEnabled() Node {
	return n.assertFlag("enabled", func(i gift.Interaction) bool { return !i.Disabled })
}

// AssertDisabled fails when the node is not disabled.
func (n Node) AssertDisabled() Node {
	return n.assertFlag("disabled", func(i gift.Interaction) bool { return i.Disabled })
}

// AssertFocused fails when the node does not hold the keyboard focus.
func (n Node) AssertFocused() Node {
	return n.assertFlag("focused", func(i gift.Interaction) bool { return i.Focused })
}

// AssertNotFocused fails when the node holds the keyboard focus.
func (n Node) AssertNotFocused() Node {
	return n.assertFlag("not focused", func(i gift.Interaction) bool { return !i.Focused })
}

// AssertHovered fails when no mouse is over the node.
func (n Node) AssertHovered() Node {
	return n.assertFlag("hovered", func(i gift.Interaction) bool { return i.Hover })
}

// AssertNotHovered fails when the node is hovered.
//
// This is the assertion behind the touch rule of the project plan, section 7:
// a tap must leave nothing hovered, because a finger that lit up a button and
// left it lit is the classic phone-website bug.
func (n Node) AssertNotHovered() Node {
	return n.assertFlag("not hovered", func(i gift.Interaction) bool { return !i.Hover })
}

// AssertPressed fails when the node is not pressed.
func (n Node) AssertPressed() Node {
	return n.assertFlag("pressed", func(i gift.Interaction) bool { return i.Pressed })
}

// AssertNotPressed fails when the node is pressed.
func (n Node) AssertNotPressed() Node {
	return n.assertFlag("not pressed", func(i gift.Interaction) bool { return !i.Pressed })
}

func (n Node) assertFlag(what string, ok func(gift.Interaction) bool) Node {
	n.h.t.Helper()
	n.check("Assert" + what)
	ia := n.Interaction()
	if ok(ia) {
		return n
	}
	n.h.t.Errorf("gifttest: %s\n  want %s, but hover=%v pressed=%v focused=%v disabled=%v",
		n.describe(), what, ia.Hover, ia.Pressed, ia.Focused, ia.Disabled)
	return n
}

// --- harness assertions -----------------------------------------------------

// AssertExists fails when nothing matches s.
func (h *Harness) AssertExists(s Selector) {
	h.t.Helper()
	if len(h.findAll(s)) == 0 {
		h.t.Errorf("gifttest: no node matches %s.\n%s\n%s", s, h.nearMisses(s), h.dumpMarked(s))
	}
}

// AssertNone fails when anything matches s, naming what did.
func (h *Harness) AssertNone(s Selector) {
	h.t.Helper()
	got := h.findAll(s)
	if len(got) == 0 {
		return
	}
	h.t.Errorf("gifttest: %d node(s) match %s, want none:\n%s", len(got), s, formatMatches(got))
}

// AssertCount fails unless exactly want nodes match s, listing the ones that
// did.
func (h *Harness) AssertCount(s Selector, want int) {
	h.t.Helper()
	got := h.findAll(s)
	if len(got) == want {
		return
	}
	h.t.Errorf("gifttest: %s matched %d node(s), want %d:\n%s\n%s",
		s, len(got), want, formatMatches(got), h.dumpMarked(s))
}

// AssertFocus fails unless the focused node matches s.
func (h *Harness) AssertFocus(s Selector) {
	h.t.Helper()
	f, ok := h.Focused()
	if !ok {
		h.t.Errorf("gifttest: nothing holds the keyboard focus, want a node matching %s.\n%s",
			s, h.dumpMarked(s))
		return
	}
	if !s.match(h, f.ref) {
		h.t.Errorf("gifttest: the focus is on %s, want a node matching %s.\n%s",
			f.describe(), s, h.dumpMarked(s))
	}
}

// AssertNoFocus fails when anything holds the keyboard focus.
func (h *Harness) AssertNoFocus() {
	h.t.Helper()
	if f, ok := h.Focused(); ok {
		h.t.Errorf("gifttest: the keyboard focus is on %s, want nothing focused", f.describe())
	}
}

// AssertNoOverflow fails when any node's content does not fit the size it
// reported.
//
// It is one line for the whole scene, because gift keeps the count and the
// extent as counters; see [gift.Diagnostics.OverflowNodes]. A layout test that
// asserts nothing else should assert this, since overflow is the failure the
// project plan, section 7, made deliberately visible rather than clipped away.
func (h *Harness) AssertNoOverflow() {
	h.t.Helper()
	d := h.app.Diagnostics()
	if d.OverflowNodes == 0 {
		return
	}
	h.t.Errorf("gifttest: %d node(s) overflow by %g logical pixels in total.\n"+
		"Content that does not fit keeps its honest size in gift and is not clipped; "+
		"either give the container more room or set Clip(true) deliberately.\n%s",
		d.OverflowNodes, d.OverflowExtent, h.Dump())
}

// --- display list assertions ------------------------------------------------

// AssertOps fails unless exactly want operations of the most recent frame
// satisfy pred.
//
// This is the assertion for the cases where "what was drawn" is the actual
// question: a background that must be emitted before its children, a shadow
// that must survive a clip, a glyph run that must exist at all. desc is what
// the predicate is called in the failure message and is not optional, for the
// same reason [Where] insists on one.
//
// The failure prints every operation of the frame, which is short — a counter
// is a dozen operations — and is the only representation of "what the frame
// looks like" that exists without a GPU.
func (h *Harness) AssertOps(desc string, want int, pred func(render.Op) bool) {
	h.t.Helper()
	ops := h.List().Ops()
	got := 0
	for _, op := range ops {
		if pred(op) {
			got++
		}
	}
	if got == want {
		return
	}
	h.t.Errorf("gifttest: %d operation(s) match %s, want %d.\nthe frame was:\n%s",
		got, desc, want, formatOps(ops))
}

// AssertPaints fails unless at least one operation of the most recent frame is
// of kind k, covers the node and is drawn in colour c.
//
// It is the readable case of [Harness.AssertOps]: "this button really did fill
// its background in the pressed colour" without spelling out a predicate.
func (n Node) AssertPaints(kind render.OpKind, c render.Color) Node {
	n.h.t.Helper()
	n.check("AssertPaints")
	b := n.Bounds()
	ops := n.h.List().Ops()
	for _, op := range ops {
		if op.Kind == kind && op.Color == c && nearRect(op.Bounds, b) {
			return n
		}
	}
	n.h.t.Errorf("gifttest: %s\n  is not painted as %s in %v anywhere in this frame.\nthe frame was:\n%s",
		n.describe(), kindName(kind), c, formatOps(ops))
	return n
}

// Glyphs returns the glyphs of the most recent frame that lie inside the
// node's bounds, which is how a headless test says "something was actually
// drawn here" for text.
//
// It does not say *which* characters: the display list carries glyph IDs, and
// mapping one back to a rune needs the font's character map, which the harness
// deliberately does not open. Use [Node.AssertText] for the string and this
// for "and it reached the display list".
func (n Node) Glyphs() []render.Glyph {
	n.h.t.Helper()
	n.check("Glyphs")
	b := n.Bounds()
	l := n.h.List()
	var out []render.Glyph
	for _, op := range l.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		for _, g := range l.Glyphs(op.Glyphs, op.GlyphCount) {
			if b.Contains(geom.Pt(g.X, g.Y)) {
				out = append(out, g)
			}
		}
	}
	return out
}

// AssertDrawsGlyphs fails when the node's bounds contain no glyph in the most
// recent frame. It is the headless proof that a label is not just present in
// the tree but on the screen.
func (n Node) AssertDrawsGlyphs() Node {
	n.h.t.Helper()
	if len(n.Glyphs()) == 0 {
		n.h.t.Errorf("gifttest: %s\n  drew no glyphs in this frame; the label exists in the tree but nothing reached the display list",
			n.describe())
	}
	return n
}

func near(a, b float32) bool {
	d := a - b
	return d <= boundsTolerance && d >= -boundsTolerance
}

func nearRect(a, b geom.Rect) bool {
	return near(a.Min.X, b.Min.X) && near(a.Min.Y, b.Min.Y) &&
		near(a.Max.X, b.Max.X) && near(a.Max.Y, b.Max.Y)
}

func kindName(k render.OpKind) string {
	switch k {
	case render.OpFillRect:
		return "fill"
	case render.OpFillRoundRect:
		return "round fill"
	case render.OpStrokeRoundRect:
		return "stroke"
	case render.OpGlyphs:
		return "glyphs"
	case render.OpShadow:
		return "shadow"
	case render.OpImage:
		return "image"
	case render.OpMaterial:
		return "material"
	default:
		return fmt.Sprintf("kind %d", k)
	}
}

func formatOps(ops []render.Op) string {
	var b strings.Builder
	for i, op := range ops {
		fmt.Fprintf(&b, "  [%2d] %-10s %s %v", i, kindName(op.Kind), rectString(op.Bounds), op.Color)
		if op.CornerRadius != 0 {
			fmt.Fprintf(&b, " radius=%g", op.CornerRadius)
		}
		if op.StrokeWidth != 0 {
			fmt.Fprintf(&b, " stroke=%g", op.StrokeWidth)
		}
		if op.Blur != 0 {
			fmt.Fprintf(&b, " blur=%g", op.Blur)
		}
		if op.GlyphCount != 0 {
			fmt.Fprintf(&b, " glyphs=%d", op.GlyphCount)
		}
		if op.Material != 0 {
			fmt.Fprintf(&b, " material=%d", op.Material)
		}
		if op.Image != 0 {
			fmt.Fprintf(&b, " image=%d", op.Image)
		}
		if op.Clip != 0 {
			fmt.Fprintf(&b, " clip=%d", op.Clip)
		}
		b.WriteByte('\n')
	}
	if len(ops) == 0 {
		return "  <empty display list>\n"
	}
	return b.String()
}

// --- background, material and image assertions ------------------------------
//
// These exist because the harness could not see two fifths of the framework.
// It was built for steps 1 to 3 of the project plan, section 12, and steps 4
// and 5 — pictures and the glass material — were never wired back into it:
// `grep -r Glass gifttest/` returned nothing at all, so a glass scene had no
// coverage in the framework's own public testing story. The project plan,
// section 13, puts structural assertions ahead of images ("Strukturelle
// Assertions haben Vorrang vor Bildern"), so these come first and the goldens
// come after.

// Background returns the colour the node fills its shape with in the most
// recent frame, and whether it fills one at all.
//
// It reads the display list rather than the view, which is the whole point: a
// modifier that was set and never painted is exactly the defect worth
// catching. A node whose background is a material has no colour here; use
// [Node.Material].
func (n Node) Background() (render.Color, bool) {
	n.h.t.Helper()
	n.check("Background")
	b := n.Bounds()
	for _, op := range n.h.List().Ops() {
		if op.Kind != render.OpFillRect && op.Kind != render.OpFillRoundRect {
			continue
		}
		if nearRect(op.Bounds, b) {
			return op.Color, true
		}
	}
	return render.Color{}, false
}

// AssertBackground fails unless the node fills its shape with want.
func (n Node) AssertBackground(want render.Color) Node {
	n.h.t.Helper()
	got, ok := n.Background()
	switch {
	case !ok:
		n.h.t.Errorf("gifttest: %s\n  paints no background at all in this frame, want %v.\nthe frame was:\n%s",
			n.describe(), want, formatOps(n.h.List().Ops()))
	case got != want:
		n.h.t.Errorf("gifttest: %s\n  background = %v\n  want         %v", n.describe(), got, want)
	}
	return n
}

// Material returns the backdrop dependent background of the node in the most
// recent frame, and whether it has one.
//
// This is how a test asserts a glass panel without a GPU: the material
// parameters travel in the side table of the display list, so the level, the
// tint, the blur radius and the corner radius are all readable headless. A
// golden says the panel changed; this says what was asked for.
func (n Node) Material() (render.Material, bool) {
	n.h.t.Helper()
	n.check("Material")
	b := n.Bounds()
	l := n.h.List()
	for _, op := range l.Ops() {
		if op.Kind != render.OpMaterial || op.Material == 0 {
			continue
		}
		if nearRect(op.Bounds, b) {
			return l.Material(op.Material), true
		}
	}
	return render.Material{}, false
}

// AssertMaterial fails unless the node paints a material of kind want.
//
// [render.MaterialNone] asserts the absence of one, which is the assertion a
// test writes after removing a glass background — the failure mode otherwise
// is a panel that costs a screen sized render target and looks almost the
// same.
func (n Node) AssertMaterial(want render.MaterialKind) Node {
	n.h.t.Helper()
	m, ok := n.Material()
	got := render.MaterialNone
	if ok {
		got = m.Kind
	}
	if got != want {
		n.h.t.Errorf("gifttest: %s\n  material = %s\n  want       %s\nthe frame was:\n%s",
			n.describe(), got, want, formatOps(n.h.List().Ops()))
	}
	return n
}

// Glass returns the glass parameters of the node's material and fails when it
// has none. It is the accessor behind assertions like "this panel asked for
// Reduced" and "the blur radius survived the modifier".
func (n Node) Glass() render.GlassParams {
	n.h.t.Helper()
	m, ok := n.Material()
	if !ok || m.Kind != render.MaterialGlass {
		n.h.t.Fatalf("gifttest: %s\n  paints no glass material in this frame.\nthe frame was:\n%s",
			n.describe(), formatOps(n.h.List().Ops()))
		return render.GlassParams{}
	}
	return m.Glass
}

// AssertGlassQuality fails unless the node's glass material requested want.
//
// It asserts what the *display list* asked for, which is a property of the
// application. Which level the backend then drew is a property of the machine
// and of the adaptive policy, and is in the backend's own counters; the two
// are deliberately not the same assertion.
func (n Node) AssertGlassQuality(want render.GlassQuality) Node {
	n.h.t.Helper()
	if got := n.Glass().Level; got != want {
		n.h.t.Errorf("gifttest: %s\n  glass quality = %s\n  want            %s", n.describe(), got, want)
	}
	return n
}

// AssertDrawsImage fails when the node draws no picture in the most recent
// frame.
//
// It is the picture counterpart of [Node.AssertDrawsGlyphs], and it catches
// the failure that every image test has: a tile whose thumbnail never arrived,
// or whose texture was never resolved, draws its placeholder and looks
// plausible. An [render.OpImage] with a resolved [render.ImageID] is the
// headless proof that a real picture reached the display list.
func (n Node) AssertDrawsImage() Node {
	n.h.t.Helper()
	n.check("AssertDrawsImage")
	b := n.Bounds()
	ops := n.h.List().Ops()
	for _, op := range ops {
		if op.Kind == render.OpImage && op.Image != 0 && nearRect(op.Bounds, b) {
			return n
		}
	}
	n.h.t.Errorf("gifttest: %s\n  draws no picture in this frame; either no texture was resolved for it "+
		"or it fell back to its placeholder.\nthe frame was:\n%s", n.describe(), formatOps(ops))
	return n
}

// --- scroll and visibility assertions ---------------------------------------

// AssertVisible fails when the node is entirely clipped away by an ancestor,
// naming the clip that removed it.
//
// It is the counterpart of the aim check: the aim check says "something is on
// top of this", and this says "this is not on screen at all". A test that
// expects a node below the fold to be reachable asserts it *after*
// [Node.ScrollIntoView]; a test that expects it to be hidden asserts
// [Node.AssertNotVisible] before.
func (n Node) AssertVisible() Node {
	n.h.t.Helper()
	n.check("AssertVisible")
	if n.IsVisible() {
		return n
	}
	n.h.t.Errorf("gifttest: %s\n  is entirely clipped away; its device bounds are %s and nothing of it survives "+
		"the clips of its ancestors.\n"+
		"  If it is inside a scroll container, scroll it into view first — the pointer actions do that "+
		"themselves, an assertion does not.\n%s",
		n.describe(), rectString(n.Bounds()), n.h.Dump())
	return n
}

// AssertNotVisible fails when any part of the node survives the clips above
// it. It is how a test states that something really is below the fold.
func (n Node) AssertNotVisible() Node {
	n.h.t.Helper()
	n.check("AssertNotVisible")
	vis, ok := n.VisibleBounds()
	if !ok {
		return n
	}
	n.h.t.Errorf("gifttest: %s\n  is visible at %s, want it clipped away entirely",
		n.describe(), rectString(vis))
	return n
}

// AssertScrollOffset fails unless the nearest scroll container at or above the
// node sits at the document offset want, within [boundsTolerance].
//
// The tolerance is there because a fling and a scroll into view both land on a
// computed number rather than a round one. A test that wants the exact value —
// the precision of very large document coordinates, for instance — reads
// [Node.ScrollInfo] and compares itself.
func (n Node) AssertScrollOffset(want float64) Node {
	n.h.t.Helper()
	s := n.Scroller()
	info, ok := n.h.app.ScrollInfo(s.ref)
	if !ok {
		return n
	}
	if d := info.Offset - want; d <= boundsTolerance && d >= -boundsTolerance {
		return n
	}
	n.h.t.Errorf("gifttest: %s\n  scroll offset = %g\n  want            %g\n"+
		"  (content %g, viewport %g, max offset %g, axis %s)",
		s.describe(), info.Offset, want,
		info.ContentExtent, info.ViewportExtent, info.MaxOffset, info.Axis)
	return n
}

// AssertAtScrollStart fails unless the container is at offset zero.
func (n Node) AssertAtScrollStart() Node {
	n.h.t.Helper()
	return n.AssertScrollOffset(0)
}

// AssertAtScrollEnd fails unless the container is at its maximum offset.
func (n Node) AssertAtScrollEnd() Node {
	n.h.t.Helper()
	s := n.Scroller()
	info, ok := n.h.app.ScrollInfo(s.ref)
	if !ok {
		return n
	}
	return n.AssertScrollOffset(info.MaxOffset)
}
