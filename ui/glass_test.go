package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// The material of the project plan, section 8, spelled exactly as the plan
// spells it.
func planGlass() ui.GlassMaterial { return ui.Glass().Quality(ui.Adaptive) }

// kindsWithMaterial is [kindsOf] extended by the one kind it does not know.
// It is a separate function so that the shadow tests keep asserting on the
// vocabulary they were written against.
func kindsWithMaterial(l *render.List) string {
	var b strings.Builder
	for i, op := range l.Ops() {
		if i > 0 {
			b.WriteByte(',')
		}
		if op.Kind == render.OpMaterial {
			b.WriteString("material")
			continue
		}
		b.WriteString(kindName(op.Kind))
	}
	return b.String()
}

func kindName(k render.OpKind) string {
	switch k {
	case render.OpShadow:
		return "shadow"
	case render.OpFillRect:
		return "fill"
	case render.OpFillRoundRect:
		return "round"
	case render.OpStrokeRoundRect:
		return "stroke"
	case render.OpGlyphs:
		return "text"
	case render.OpImage:
		return "image"
	default:
		return "none"
	}
}

// TestGlassDrawOrderInEveryCombination is the shadow ordering test of section
// 13 with the material in the background slot.
//
// It enumerates rather than sampling for the same reason the shadow one does:
// the order is one function with several early exits, and the failure worth
// catching is an early exit that reorders what follows it.
func TestGlassDrawOrderInEveryCombination(t *testing.T) {
	bools := []bool{false, true}
	for _, shadow := range bools {
		for _, glass := range bools {
			for _, border := range bools {
				for _, radius := range bools {
					for _, clip := range bools {
						v := ui.VStack(probe(20, 10)).Frame(100, 60)
						var want []string
						if shadow {
							v = v.Shadow(planShadow)
							want = append(want, "shadow")
						}
						if glass {
							v = v.Background(planGlass())
							want = append(want, "material")
						}
						if radius {
							v = v.CornerRadius(8)
						}
						if clip {
							v = v.Clip(true)
						}
						want = append(want, "fill") // the content
						if border {
							v = v.Border(ui.Border{Width: 2, Color: ui.RGB(0, 0, 255)})
							want = append(want, "stroke")
						}
						l := run(t, v, geom.Sz(200, 200))
						if got, w := kindsWithMaterial(l), strings.Join(want, ","); got != w {
							t.Errorf("shadow=%v glass=%v border=%v radius=%v clip=%v: order %q, want %q",
								shadow, glass, border, radius, clip, got, w)
						}
					}
				}
			}
		}
	}
}

// TestBackdropIsOnlyWhatCameBefore is the property of the project plan,
// section 8, that is easiest to get subtly wrong: "Der Backdrop enthaelt nur
// vorher gezeichnete Inhalte, nicht das Material selbst oder seine Kinder."
//
// A display list has no z field. Emission order *is* z order, so the whole
// claim reduces to one assertion about indices: everything that is supposed to
// be behind the material has a lower index, and everything that is supposed to
// be in front — including the material's own children and its own border — has
// a higher one. A backend that copies the target at the material's index
// therefore copies exactly the right pixels, and one that did not could not be
// told apart from one that did without this test.
func TestBackdropIsOnlyWhatCameBefore(t *testing.T) {
	behind := ui.Box().Frame(200, 200).Background(ui.RGB(10, 20, 30))
	child := ui.Box().Frame(40, 20).Background(ui.RGB(200, 0, 0))
	pane := ui.VStack(child).
		Frame(120, 60).
		Background(planGlass()).
		Border(ui.Border{Width: 2, Color: ui.RGB(255, 255, 255)}).
		Shadow(planShadow)
	after := ui.Box().Frame(30, 30).Background(ui.RGB(0, 200, 0))

	l := run(t, ui.ZStack(behind, pane, after), geom.Sz(300, 300))
	if got, want := kindsWithMaterial(l), "fill,shadow,material,fill,stroke,fill"; got != want {
		t.Fatalf("order %q, want %q", got, want)
	}
	ops := l.Ops()
	mat := -1
	for i, op := range ops {
		if op.Kind == render.OpMaterial {
			mat = i
		}
	}
	if mat < 0 {
		t.Fatal("no material operation")
	}

	// The sibling drawn before the pane is behind it, so it is part of the
	// backdrop.
	if ops[0].Color != render.RGB(10, 20, 30) {
		t.Errorf("operation 0 is %v, want the backdrop fill", ops[0].Color)
	}
	if !(0 < mat) {
		t.Error("the backdrop fill is not before the material")
	}
	// The pane's own shadow is behind it too, which is the drawing order of
	// section 8 and not an accident of this scene.
	if !(1 < mat) || ops[1].Kind != render.OpShadow {
		t.Error("the pane's shadow is not before its own material")
	}
	// The pane's child and its border are in front and therefore are *not*
	// in the backdrop. This is the half that a painter which emitted the
	// material last would get wrong, and nothing else would notice.
	if !(ops[mat+1].Kind == render.OpFillRect && ops[mat+1].Color == render.RGB(200, 0, 0)) {
		t.Errorf("the child of the pane is not immediately after the material; got %q", kindsWithMaterial(l))
	}
	if ops[mat+2].Kind != render.OpStrokeRoundRect {
		t.Error("the pane's border is not after its material")
	}
	// A later sibling is in front of the pane, so it is not in the backdrop
	// either, even though it overlaps it.
	if ops[mat+3].Color != render.RGB(0, 200, 0) {
		t.Errorf("the later sibling is not after the material; got %q", kindsWithMaterial(l))
	}
}

// TestGlassMaterialCarriesItsShapeAndParameters checks that the operation says
// what the backend needs to know, because everything else about the material
// is in a side table and an operation that forgot its index would draw
// nothing with no diagnostic.
func TestGlassMaterialCarriesItsShapeAndParameters(t *testing.T) {
	g := ui.Glass().Quality(ui.Full).Blur(24).Tint(ui.RGBA(10, 20, 30, 40)).
		Refraction(9).Highlight(0.25).Grain(0.5)
	l := run(t, ui.Box().Frame(120, 60).CornerRadius(18).Background(g), geom.Sz(300, 300))

	ops := l.Ops()
	if len(ops) != 1 || ops[0].Kind != render.OpMaterial {
		t.Fatalf("got %q, want one material", kindsWithMaterial(l))
	}
	op := ops[0]
	if op.CornerRadius != 18 {
		t.Errorf("CornerRadius = %v, want 18", op.CornerRadius)
	}
	if op.Bounds.Width() != 120 || op.Bounds.Height() != 60 {
		t.Errorf("Bounds = %v, want 120x60", op.Bounds)
	}
	if op.Material == 0 {
		t.Fatal("Material index is 0, which means no material")
	}
	m := l.Material(op.Material)
	if m.Kind != render.MaterialGlass {
		t.Fatalf("Kind = %v, want glass", m.Kind)
	}
	want := render.GlassParams{
		Tint: ui.RGBA(10, 20, 30, 40), Blur: 24, Refraction: 9,
		Highlight: 0.25, Grain: 0.5, Level: render.Full,
	}
	if m.Glass != want {
		t.Errorf("params = %+v, want %+v", m.Glass, want)
	}
}

// TestParentClipAppliesToTheMaterial. A material is a background, and a
// background under a clipping ancestor is cut like anything else; the project
// plan, section 8, says the backdrop is clipped to the material shape, and
// section 7 says a parent clip composes with it.
func TestParentClipAppliesToTheMaterial(t *testing.T) {
	pane := ui.Box().Frame(300, 300).Background(planGlass())
	l := run(t, ui.ZStack(pane).Frame(100, 80).Clip(true), geom.Sz(400, 400))

	ops := l.Ops()
	if len(ops) != 1 || ops[0].Kind != render.OpMaterial {
		t.Fatalf("got %q, want one material", kindsWithMaterial(l))
	}
	clip := l.Clip(ops[0].Clip)
	if ops[0].Clip == 0 {
		t.Fatal("the material is unclipped; the parent's Clip(true) did not reach it")
	}
	if clip.Width() > 100 || clip.Height() > 80 {
		t.Errorf("clip %v is larger than the clipping parent", clip)
	}
}

// TestGlassBackdropGoesDirtyWhenTheContentUnderItScrolls.
//
// The claim of the project plan, section 8, is "Beim Scrollen darunter wird er
// dirty. Ein statischer Glasrahmen bedeutet deshalb nicht, dass sein Blur
// wiederverwendbar ist." There is no cache to invalidate here, and that is
// precisely the point: the display list is rebuilt every frame, so the
// operations under the material move while the material itself does not. This
// test pins that, because the tempting optimisation — noticing that the
// material's own operation is unchanged and skipping its passes — would be
// wrong, and this is the evidence that it would be.
func TestGlassBackdropGoesDirtyWhenTheContentUnderItScrolls(t *testing.T) {
	content := ui.VStack(
		ui.Box().Frame(200, 120).Background(ui.RGB(200, 0, 0)),
		ui.Box().Frame(200, 120).Background(ui.RGB(0, 200, 0)),
		ui.Box().Frame(200, 120).Background(ui.RGB(0, 0, 200)),
	)
	scene := ui.ZStack(
		ui.VScroll(content).Frame(200, 150).Key("scroller"),
		ui.Box().Frame(180, 60).Background(planGlass()),
	)
	h := gifttest.New(t, gifttest.Options{View: scene, Size: geom.Sz(300, 300)})

	beforeMaterial, material := snapshotAround(t, h.List())

	// Scroll the container underneath. Nothing rebuilds and nothing relayouts
	// — that is the scroll fast path — but the display list is produced
	// afresh and the transform of the content has moved.
	d0 := h.Diagnostics()
	h.Find(gifttest.ByKey("scroller")).ScrollBy(90)
	d1 := h.Diagnostics()
	if d1.Builds != d0.Builds || d1.Layouts != d0.Layouts {
		t.Fatalf("scrolling built or laid out; this test is about the paint path")
	}

	afterMaterial, material2 := snapshotAround(t, h.List())

	if material != material2 {
		t.Errorf("the material operation itself changed: %+v then %+v; the pane did not move", material, material2)
	}
	if beforeMaterial == afterMaterial {
		t.Errorf("the backdrop is identical after scrolling (%v); a static glass frame over "+
			"moving content must still be redrawn", beforeMaterial)
	}
}

// snapshotAround returns a value summarising everything emitted before the
// material — that is, its backdrop — and the material operation itself.
func snapshotAround(t *testing.T, l *render.List) (string, render.Op) {
	t.Helper()
	var b strings.Builder
	var mat render.Op
	found := false
	for _, op := range l.Ops() {
		if op.Kind == render.OpMaterial {
			mat, found = op, true
			break
		}
		x := l.Xform(op.Xform)
		r := x.TransformRect(op.Bounds)
		fmt.Fprintf(&b, "%s%v;", kindName(op.Kind), r)
	}
	if !found {
		t.Fatal("no material operation in the list")
	}
	return b.String(), mat
}

// TestBackgroundAcceptsBothKinds is the compile-and-behave check for the
// signature change the project plan, section 8, forced: Background takes an
// interface so that a colour and a material can both be spelled through it.
func TestBackgroundAcceptsBothKinds(t *testing.T) {
	col := run(t, ui.Box().Frame(40, 20).Background(ui.RGB(1, 2, 3)), geom.Sz(100, 100))
	if got := kindsWithMaterial(col); got != "fill" {
		t.Errorf("a Color background gave %q, want fill", got)
	}
	mat := run(t, ui.Box().Frame(40, 20).Background(planGlass()), geom.Sz(100, 100))
	if got := kindsWithMaterial(mat); got != "material" {
		t.Errorf("a Glass background gave %q, want material", got)
	}
	// The last setter wins, which is the "Wiederholtes Setzen ersetzt den
	// jeweiligen Style-Wert" of section 8. The interesting direction is glass
	// then colour, because a stale material would still emit an operation and
	// the frame would cost a pass chain nobody asked for.
	back := run(t, ui.Box().Frame(40, 20).Background(planGlass()).Background(ui.RGB(1, 2, 3)), geom.Sz(100, 100))
	if got := kindsWithMaterial(back); got != "fill" {
		t.Errorf("Background(glass).Background(colour) gave %q, want fill", got)
	}
	fwd := run(t, ui.Box().Frame(40, 20).Background(ui.RGB(1, 2, 3)).Background(planGlass()), geom.Sz(100, 100))
	if got := kindsWithMaterial(fwd); got != "material" {
		t.Errorf("Background(colour).Background(glass) gave %q, want material", got)
	}
	// And nil is not a panic.
	none := run(t, ui.Box().Frame(40, 20).Background(nil), geom.Sz(100, 100))
	if got := none.Len(); got != 0 {
		t.Errorf("Background(nil) emitted %d operations, want 0", got)
	}
}

// TestGlassFramePathIsAllocationFree is the allocation contract of the project
// plan, section 11, with a material on screen.
//
// A material adds a side table entry and an operation per frame, both of them
// into slices the display list reuses, so the answer has to stay zero. The
// interesting failure is a material that is boxed into the [ui.Background]
// interface every paint rather than once at build: the interface is a build
// time cost and the plan exempts build, but only build.
func TestGlassFramePathIsAllocationFree(t *testing.T) {
	scene := func(*gift.Context) gift.View {
		return ui.ZStack(
			ui.Box().Frame(400, 300).Background(ui.RGB(10, 12, 18)),
			ui.VStack(
				ui.Box().Frame(80, 20).Background(ui.RGB(200, 200, 200)),
			).
				Frame(300, 80).
				Padding(20).
				Background(ui.Glass().Quality(ui.Adaptive)).
				Border(ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 90)}).
				CornerRadius(18).
				Shadow(planShadow),
		)
	}
	a := gift.New(gift.Options{Root: scene})
	step := func() {
		if err := a.Update(geom.Sz(500, 400)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	// The material really is in the list being measured.
	if got := kindsWithMaterial(a.Paint()); !strings.Contains(got, "material") {
		t.Fatalf("no material in %q; the measurement would be meaningless", got)
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Fatalf("the frame path with glass allocated %v times per run, want 0", got)
	}
}
