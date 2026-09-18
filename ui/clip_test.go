package ui_test

import (
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// The clip used to mean two things. gift read [gift.Element.Clip] for hit
// testing, and every styled view was separately expected to push a paint clip
// around its children. ui.Stack and ui.Text did. ui.Button did not, so
// Clip(true) on a button confined its hit area and let a child four times its
// size paint over the entire window, while ButtonView.Clip's own godoc said
// "for paint and for hit testing alike".
//
// gift now applies the flag itself, on the one route a container's children
// take. These tests are the assertion that it does so for every family, and
// that it still leaves the node's own drawing outside — which is not a detail:
// a shadow deliberately extends past the bounds and clipping it to them would
// delete it.

// clipProbe is a child much larger than the container that clips it, so that
// "was it clipped" is a visible difference rather than a rounding question.
const (
	clipBoxW, clipBoxH     = 50, 50
	clipChildW, clipChildH = 400, 400
)

// clipOf returns the clip rectangle of the op with the given index, and
// whether the op was clipped at all.
func clipOf(l *render.List, op render.Op) (geom.Rect, bool) {
	if op.Clip == 0 {
		return geom.Rect{}, false
	}
	return l.Clip(op.Clip), true
}

// firstChildFill returns the op that drew the oversized child, which is the
// only fill in these lists whose bounds are the child's size.
func firstChildFill(t *testing.T, l *render.List) render.Op {
	t.Helper()
	for _, op := range l.Ops() {
		if op.Kind != render.OpFillRect && op.Kind != render.OpFillRoundRect {
			continue
		}
		if op.Bounds.Width() == clipChildW && op.Bounds.Height() == clipChildH {
			return op
		}
	}
	t.Fatalf("the list contains no %gx%g child fill: %v", float32(clipChildW), float32(clipChildH),
		boundsOf(l.Ops()))
	return render.Op{}
}

// TestClipConfinesEveryStyledWidgetFamily is the regression test for the
// button, written so that it covers every container family at once: a new
// styled view that routes its children through PaintChildren is clipped by
// construction, and one that invents its own descent is caught here.
func TestClipConfinesEveryStyledWidgetFamily(t *testing.T) {
	child := func() ui.BoxView { return probe(clipChildW, clipChildH) }
	want := geom.RcXYWH(0, 0, clipBoxW, clipBoxH)

	for _, tc := range []struct {
		name string
		view gift.View
	}{
		{"ui.VStack", ui.VStack(child()).Frame(clipBoxW, clipBoxH).Clip(true)},
		{"ui.HStack", ui.HStack(child()).Frame(clipBoxW, clipBoxH).Clip(true)},
		{"ui.ZStack", ui.ZStack(child()).Frame(clipBoxW, clipBoxH).Clip(true)},
		// The one that was broken. A button's label is an arbitrary view and
		// the padding is set to zero so the expected rectangle is the plain
		// bounds.
		{"ui.Button", ui.Button(child(), nil).Frame(clipBoxW, clipBoxH).Padding(0).Clip(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := run(t, ui.ZStack(tc.view), geom.Sz(800, 600))
			op := firstChildFill(t, l)
			got, ok := clipOf(l, op)
			if !ok {
				t.Fatalf("the %gx%g child of a Clip(true) %s was drawn unclipped; it covers the "+
					"whole window", float32(clipChildW), float32(clipChildH), tc.name)
			}
			if !approxRect(got, want) {
				t.Fatalf("the child is clipped to %v, want the container's bounds %v", got, want)
			}
		})
	}
}

// TestClipLeavesBackgroundBorderAndShadowOutside pins the drawing order of the
// project plan, section 8, against the new owner of the clip.
//
// The shadow is the one that matters. A background is exactly the bounds and a
// border lies inside them, so clipping either would be invisible; a shadow is
// the bounds grown by its blur and offset, and a clip to the bounds would
// remove most of it. That is why the clip surrounds the subtree and not the
// node.
func TestClipLeavesBackgroundBorderAndShadowOutside(t *testing.T) {
	v := ui.VStack(probe(clipChildW, clipChildH)).
		Frame(clipBoxW, clipBoxH).
		Background(ui.RGB(200, 200, 200)).
		Border(ui.Border{Width: 2, Color: ui.RGB(0, 0, 0)}).
		Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)}).
		Clip(true)

	l := run(t, ui.ZStack(v), geom.Sz(800, 600))
	ops := l.Ops()
	if len(ops) != 4 {
		t.Fatalf("painted %d ops, want shadow, background, child, border: %v", len(ops), boundsOf(ops))
	}
	for i, name := range []string{"the shadow", "the background", "", "the border"} {
		if name == "" {
			continue
		}
		if ops[i].Clip != 0 {
			t.Errorf("%s is clipped with index %d, want the unclipped sentinel; the clip must "+
				"surround the subtree and not the node's own drawing", name, ops[i].Clip)
		}
	}
	if ops[2].Clip == 0 {
		t.Error("the child is unclipped")
	}
	// And the shadow really does reach past the bounds, so the assertion
	// above is about something. Its op carries the shape and the blur
	// separately — see render.OpShadow — so the painted extent is the shape
	// grown by the falloff, and both halves have to leave the bounds here.
	if sh := ops[0]; sh.Bounds.Max.Y <= clipBoxH || !(sh.Blur > 0) {
		t.Errorf("the shadow shape is %v with blur %g, which does not exceed the %gx%g bounds; "+
			"the test proves nothing", sh.Bounds, sh.Blur, float32(clipBoxW), float32(clipBoxH))
	}
}

// TestClipAppliesWithoutAPainter. Clip is a property of the node, not of the
// fact that somebody installed a painter on it, so a container with nothing to
// draw still clips.
func TestClipAppliesWithoutAPainter(t *testing.T) {
	v := ui.VStack(probe(clipChildW, clipChildH)).Frame(clipBoxW, clipBoxH).Clip(true)
	l := run(t, ui.ZStack(v), geom.Sz(800, 600))
	op := firstChildFill(t, l)
	got, ok := clipOf(l, op)
	if !ok {
		t.Fatal("a Clip(true) container with no background, border or shadow did not clip; " +
			"the flag is being read off the painter instead of off the node")
	}
	if want := geom.RcXYWH(0, 0, clipBoxW, clipBoxH); !approxRect(got, want) {
		t.Fatalf("clipped to %v, want %v", got, want)
	}
}

// TestClipNestsWithTheParentClip. Two clips intersect rather than replace,
// which is the property the display list's clip stack provides and which a
// virtual scroller in step 3 depends on.
func TestClipNestsWithTheParentClip(t *testing.T) {
	inner := ui.VStack(probe(clipChildW, clipChildH)).Frame(400, 400).Clip(true)
	outer := ui.VStack(inner).Frame(clipBoxW, clipBoxH).Clip(true)
	l := run(t, ui.ZStack(outer), geom.Sz(800, 600))
	op := firstChildFill(t, l)
	got, ok := clipOf(l, op)
	if !ok {
		t.Fatal("the child of two nested clips is unclipped")
	}
	if want := geom.RcXYWH(0, 0, clipBoxW, clipBoxH); !approxRect(got, want) {
		t.Fatalf("nested clips produced %v, want the intersection %v", got, want)
	}
}

// TestClipAndHitTestAgree is the whole point of the one flag: a node that is
// clipped away must not be clickable either.
func TestClipAndHitTestAgree(t *testing.T) {
	btn := ui.Button(ui.Box().Frame(clipChildW, clipChildH), nil).
		Frame(clipBoxW, clipBoxH).Padding(0).Clip(true)
	a := gift.New(gift.Options{Root: static(ui.ZStack(btn))})
	frame(t, a, geom.Sz(800, 600))

	if _, ok := a.HitTest(geom.Pt(25, 25)); !ok {
		t.Fatal("the button is not hit inside its own bounds")
	}
	if _, ok := a.HitTest(geom.Pt(200, 200)); ok {
		t.Error("a point far outside a Clip(true) button is still a hit; " +
			"input and paint disagree about the clip")
	}
}

// TestTextClipsItsOwnGlyphs. A leaf whose content is not its children applies
// the clip itself, and the rectangle has to be the same one gift would have
// used.
func TestTextClipsItsOwnGlyphs(t *testing.T) {
	f := loadTestFont(t)
	v := ui.Text("a long line of text that does not fit").Font(f).FontSize(20).
		Frame(clipBoxW, clipBoxH).Clip(true)
	l := run(t, ui.ZStack(v), geom.Sz(800, 600))
	var found bool
	for _, op := range l.Ops() {
		if op.Kind != render.OpGlyphs {
			continue
		}
		found = true
		got, ok := clipOf(l, op)
		if !ok {
			t.Fatal("a Clip(true) ui.Text drew its glyphs unclipped")
		}
		if want := geom.RcXYWH(0, 0, clipBoxW, clipBoxH); !approxRect(got, want) {
			t.Fatalf("the glyphs are clipped to %v, want %v", got, want)
		}
	}
	if !found {
		t.Fatal("no glyph run in the list")
	}
}
