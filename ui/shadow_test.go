package ui_test

import (
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The shadow of the project plan, section 8, spelled exactly as the plan
// spells it.
var planShadow = ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)}

func kindsOf(l *render.List) string {
	var b strings.Builder
	for i, op := range l.Ops() {
		if i > 0 {
			b.WriteByte(',')
		}
		switch op.Kind {
		case render.OpShadow:
			b.WriteString("shadow")
		case render.OpFillRect:
			b.WriteString("fill")
		case render.OpFillRoundRect:
			b.WriteString("round")
		case render.OpStrokeRoundRect:
			b.WriteString("stroke")
		case render.OpGlyphs:
			b.WriteString("text")
		default:
			b.WriteString("none")
		}
	}
	return b.String()
}

// TestShadowDrawOrderInEveryCombination is the "Modifier-Zeichenreihenfolge"
// check of the project plan, section 13, extended to the shadow slot.
//
// It enumerates shadow, background, border, radius and clip rather than
// picking a representative case, because the drawing order is implemented in
// one function with several early exits and the interesting failure is an
// early exit that reorders the remaining steps.
func TestShadowDrawOrderInEveryCombination(t *testing.T) {
	bools := []bool{false, true}
	for _, shadow := range bools {
		for _, bg := range bools {
			for _, border := range bools {
				for _, radius := range bools {
					for _, clip := range bools {
						v := ui.VStack(probe(20, 10)).Frame(100, 60)
						var want []string
						if shadow {
							v = v.Shadow(planShadow)
							want = append(want, "shadow")
						}
						if bg {
							v = v.Background(ui.RGB(200, 0, 0))
							if radius {
								want = append(want, "round")
							} else {
								want = append(want, "fill")
							}
						}
						if radius {
							v = v.CornerRadius(8)
						}
						if clip {
							v = v.Clip(true)
						}
						// The child is the content step, and it is always
						// there so that "content between background and
						// border" is observable.
						want = append(want, "fill")
						if border {
							v = v.Border(ui.Border{Width: 2, Color: ui.RGB(0, 0, 255)})
							want = append(want, "stroke")
						}

						l := run(t, v, geom.Sz(200, 200))
						if got, w := kindsOf(l), strings.Join(want, ","); got != w {
							t.Errorf("shadow=%v bg=%v border=%v radius=%v clip=%v: order %q, want %q",
								shadow, bg, border, radius, clip, got, w)
						}
					}
				}
			}
		}
	}
}

// TestShadowIsBehindTheBackgroundOfTheSameNode is the single property the
// whole ordering exists for, stated once on its own so that a failure is
// readable without decoding a five-flag combination.
func TestShadowIsBehindTheBackgroundOfTheSameNode(t *testing.T) {
	l := run(t, ui.Box().Frame(50, 30).Background(ui.RGB(255, 255, 255)).Shadow(planShadow), geom.Sz(200, 200))
	ops := l.Ops()
	if len(ops) != 2 || ops[0].Kind != render.OpShadow || ops[1].Kind != render.OpFillRect {
		t.Fatalf("got %q, want shadow,fill", kindsOf(l))
	}
}

// TestShadowExtendsPaintBoundsButNotLayout is the other half of the section 8
// sentence: the paint rectangle grows, the layout size does not.
func TestShadowExtendsPaintBoundsButNotLayout(t *testing.T) {
	const w, h = 60, 40
	plain := ui.Box().Frame(w, h).Background(fill)
	shaded := ui.Box().Frame(w, h).Background(fill).Shadow(planShadow)

	// Layout: the two nodes are placed identically. The sibling after each of
	// them is where a layout change would show up.
	a := run(t, ui.VStack(plain, probe(10, 10)).Gap(5), geom.Sz(200, 200))
	b := run(t, ui.VStack(shaded, probe(10, 10)).Gap(5), geom.Sz(200, 200))
	if len(a.Ops()) != 2 || len(b.Ops()) != 3 {
		t.Fatalf("op counts %d and %d, want 2 and 3", len(a.Ops()), len(b.Ops()))
	}
	if got, want := b.Ops()[1].Bounds, a.Ops()[0].Bounds; got != want {
		t.Errorf("the shaded node itself is at %v, want %v", got, want)
	}
	if got, want := b.Ops()[2].Bounds, a.Ops()[1].Bounds; got != want {
		t.Errorf("the sibling below the shadow moved to %v, want %v; a shadow is not layout", got, want)
	}

	// Paint: the shadow operation reaches beyond the node on all four sides,
	// by three sigma, plus the offset downwards.
	node := b.Ops()[1].Bounds
	pb := b.Ops()[0].PaintBounds()
	if want := planShadow.PaintBounds(node); pb != want {
		t.Errorf("shadow PaintBounds = %v, want %v", pb, want)
	}
	if !(pb.Min.X < node.Min.X && pb.Min.Y < node.Min.Y &&
		pb.Max.X > node.Max.X && pb.Max.Y > node.Max.Y) {
		t.Errorf("shadow paint bounds %v do not extend the node bounds %v on all sides", pb, node)
	}
	// The offset is down, so the shadow reaches further below than above.
	if below, above := pb.Max.Y-node.Max.Y, node.Min.Y-pb.Min.Y; !(below > above) {
		t.Errorf("OffsetY 4 gave %v below and %v above; the offset did not reach the operation", below, above)
	}
}

// TestShadowDoesNotChangeTheHitArea. A shadow that were clickable would make
// the gap between two buttons hit one of them, which is the exact bug the
// section 8 sentence exists to prevent.
func TestShadowDoesNotChangeTheHitArea(t *testing.T) {
	var clicked int
	btn := ui.Button(textOf(t, "x"), func() { clicked++ }).
		Frame(40, 20).
		Shadow(ui.Shadow{Blur: 40, Color: ui.RGB(0, 0, 0)})

	a := gift.New(gift.Options{Root: static(ui.ZStack(btn))})
	l := frame(t, a, geom.Sz(200, 200))

	// The button's own box, taken from the display list rather than assumed,
	// so that a change of the default stack alignment cannot turn this test
	// into a tautology.
	node := l.Ops()[0].Bounds
	cx, cy := (node.Min.X+node.Max.X)/2, (node.Min.Y+node.Max.Y)/2
	if _, ok := a.HitTest(geom.Pt(cx, cy)); !ok {
		t.Fatalf("the centre %v,%v of the button %v is not hit testable", cx, cy, node)
	}
	// Well inside the shadow but outside the node. Blur 40 reaches 60 pixels
	// past every edge; ten is comfortably inside that and outside the box.
	for _, p := range []geom.Point{
		{X: cx, Y: node.Max.Y + 10}, {X: node.Min.X - 10, Y: cy},
		{X: cx, Y: node.Min.Y - 10}, {X: node.Max.X + 10, Y: cy},
	} {
		if ref, ok := a.HitTest(p); ok {
			t.Errorf("%v is inside the shadow and hit %v; a shadow is not a hit area", p, ref)
		}
	}
	if clicked != 0 {
		t.Errorf("clicked = %d, want 0", clicked)
	}
}

// TestShadowDoesNotChangeOverflow. Overflow is measured against the layout
// size, and the project plan, section 7, makes it a reported number; a shadow
// growing it would turn a decorative style into a diagnosed defect.
func TestShadowDoesNotChangeOverflow(t *testing.T) {
	build := func(s ui.Shadow) gift.Diagnostics {
		a := gift.New(gift.Options{Root: static(
			ui.VStack(ui.Box().Frame(50, 500).Background(fill).Shadow(s)).Frame(100, 100),
		)})
		frame(t, a, geom.Sz(200, 200))
		return a.Diagnostics()
	}
	plain, shaded := build(ui.Shadow{}), build(ui.Shadow{Blur: 100, OffsetY: 100, Color: ui.RGB(0, 0, 0)})
	if plain.OverflowNodes == 0 {
		t.Fatal("the fixture no longer overflows; it is supposed to")
	}
	if plain.OverflowNodes != shaded.OverflowNodes || plain.OverflowExtent != shaded.OverflowExtent {
		t.Errorf("overflow with a shadow is %d/%v, without it %d/%v; a shadow is not layout",
			shaded.OverflowNodes, shaded.OverflowExtent, plain.OverflowNodes, plain.OverflowExtent)
	}
}

// TestParentClipCutsTheShadow. "Eltern-Clips gelten auch fuer den Schatten",
// project plan, section 8. The shadow travels in the display list under the
// clip index of its parent like any other operation, which is checked here
// against the clip table rather than against a screenshot.
func TestParentClipCutsTheShadow(t *testing.T) {
	l := run(t, ui.VStack(
		ui.Box().Frame(40, 20).Background(fill).Shadow(planShadow),
	).Frame(100, 60).Padding(10).Clip(true).Background(ui.RGB(1, 1, 1)), geom.Sz(200, 200))

	var shadow render.Op
	found := false
	for _, op := range l.Ops() {
		if op.Kind == render.OpShadow {
			shadow, found = op, true
		}
	}
	if !found {
		t.Fatalf("no shadow operation in %q", kindsOf(l))
	}
	if shadow.Clip == 0 {
		t.Fatal("the shadow is under the unclipped sentinel; the parent clip did not reach it")
	}
	clip := l.Clip(shadow.Clip)
	pb := shadow.PaintBounds()
	if clip.Intersect(pb) == pb {
		t.Errorf("the clip %v contains the whole shadow %v; the fixture no longer tests anything", clip, pb)
	}
	if clip.Intersect(pb).IsEmpty() {
		t.Errorf("the clip %v removed the shadow %v entirely; the fixture is wrong", clip, pb)
	}
}

// TestShadowOnEveryStyledView: the modifier is on the shared base, so this
// checks the wiring per type rather than the mechanism, which is why it only
// asserts that an operation appears and that it comes first.
func TestShadowOnEveryStyledView(t *testing.T) {
	for name, v := range map[string]gift.View{
		"VStack": ui.VStack(probe(10, 10)).Shadow(planShadow),
		"HStack": ui.HStack(probe(10, 10)).Shadow(planShadow),
		"ZStack": ui.ZStack(probe(10, 10)).Shadow(planShadow),
		"Box":    ui.Box().Frame(30, 20).Shadow(planShadow),
		"Text":   textOf(t, "hi").Shadow(planShadow),
		"Button": ui.Button(textOf(t, "hi"), nil).Shadow(planShadow),
	} {
		l := run(t, v, geom.Sz(200, 200))
		ops := l.Ops()
		if len(ops) == 0 || ops[0].Kind != render.OpShadow {
			t.Errorf("%s: painted %q, want a shadow first", name, kindsOf(l))
		}
	}
}

// TestShadowWithoutBlurIsAHardShape. A spread and an offset with no blur is a
// legitimate thing to ask for, and it must not silently disappear.
func TestShadowWithoutBlurIsAHardShape(t *testing.T) {
	l := run(t, ui.Box().Frame(40, 20).Shadow(ui.Shadow{Spread: 2, OffsetY: 3, Color: ui.RGB(0, 0, 0)}), geom.Sz(100, 100))
	ops := l.Ops()
	if len(ops) != 1 || ops[0].Kind != render.OpShadow {
		t.Fatalf("painted %q, want one shadow", kindsOf(l))
	}
	if ops[0].Blur != 0 {
		t.Errorf("Blur = %v, want 0", ops[0].Blur)
	}
	if got := ops[0].PaintBounds(); got != ops[0].Bounds {
		t.Errorf("an unblurred shadow paints %v beyond its shape %v", got, ops[0].Bounds)
	}
}

// TestInvisibleShadowNeedsNoPainter. A view with nothing but a transparent
// shadow must not acquire a painter, or every such node would cost an
// interface call and a heap object for nothing; see styleSpec.needsPainter.
func TestInvisibleShadowNeedsNoPainter(t *testing.T) {
	painted := func(v gift.View) uint64 {
		a := gift.New(gift.Options{Root: static(v)})
		l := frame(t, a, geom.Sz(100, 100))
		if got := kindsOf(l); got != "fill" {
			t.Fatalf("painted %q, want just the child's fill", got)
		}
		return a.Diagnostics().PaintedNodes
	}
	plain := painted(ui.VStack(probe(10, 10)))
	shaded := painted(ui.VStack(probe(10, 10)).Shadow(ui.Shadow{Blur: 20}))
	if plain != shaded {
		t.Errorf("PaintedNodes = %d with a transparent shadow and %d without; "+
			"an invisible shadow installed a painter", shaded, plain)
	}
}

// TestShadowRejectsNonsense. The same argument as checkGap: an infinite blur
// is an infinite quad and NaN vertex positions, and the failure surfaces three
// layers below the mistake.
func TestShadowRejectsNonsense(t *testing.T) {
	for name, s := range map[string]ui.Shadow{
		"negative blur": {Blur: -1},
		"infinite blur": {Blur: geom.Unbounded()},
		"NaN offset":    {OffsetX: nan32()},
		"infinite spre": {Spread: geom.Unbounded()},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Shadow(%+v) was accepted", name, s)
				}
			}()
			ui.Box().Shadow(s)
		}()
	}
	// A negative spread is legal and useful and must not panic.
	ui.Box().Shadow(ui.Shadow{Spread: -4, Blur: 8, Color: ui.RGB(0, 0, 0)})
}

func nan32() float32 { var z float32; return z / z }
