package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// buttonApp mounts v and brings it to a laid out, painted steady state.
func buttonApp(t *testing.T, v gift.View) *gift.App {
	t.Helper()
	a := gift.New(gift.Options{Root: static(v)})
	frame(t, a, geom.Sz(400, 300))
	return a
}

// The four tests below are written with the gifttest harness rather than by
// driving gift.App directly, and they are the proof that the harness is usable
// from outside its own package. They read better for the same reason the
// harness exists: "find the button, press it, move away, release" is what the
// test is about, and the three coordinates it used to contain were all the
// same made up point. The tests further down keep driving the App by hand,
// because their subject is the display list and the frame counters rather than
// the interaction.

// harness mounts v as the whole application, ready for interaction.
func harness(t *testing.T, v gift.View) *gifttest.Harness {
	t.Helper()
	return gifttest.New(t, gifttest.Options{View: v, Size: geom.Sz(400, 300)})
}

// TestButtonClickActivates is the shortest statement of what a button is.
func TestButtonClickActivates(t *testing.T) {
	n := 0
	h := harness(t, ui.ZStack(ui.Button(probe(40, 20), func() { n++ }).Key("b").Frame(80, 40)))

	h.Find(gifttest.ByKey("b")).Click()
	if n != 1 {
		t.Fatalf("a click activated the button %d times, want 1", n)
	}
}

// TestButtonReleaseOutsideDoesNotActivate is pointer capture as a user sees
// it: press, slide off, let go, nothing happens.
func TestButtonReleaseOutsideDoesNotActivate(t *testing.T) {
	n := 0
	h := harness(t, ui.ZStack(ui.Button(probe(40, 20), func() { n++ }).Key("b").Frame(80, 40)))

	h.Find(gifttest.ByKey("b")).Press().AssertPressed()
	h.MoveTo(geom.Pt(300, 200))
	h.Find(gifttest.ByKey("b")).AssertNotPressed()
	h.Release()

	if n != 0 {
		t.Fatalf("a release outside activated the button %d times, want 0", n)
	}
}

// TestButtonKeyboardActivation covers tab, space and enter together, because
// the project plan, section 7, names them in one breath and a button that can
// be focused but not fired is not one of them.
func TestButtonKeyboardActivation(t *testing.T) {
	for _, k := range []gift.Key{gift.KeySpace, gift.KeyEnter} {
		n := 0
		h := harness(t, ui.ZStack(ui.Button(probe(40, 20), func() { n++ }).Key("b")))

		h.Tab()
		h.AssertFocus(gifttest.ByKey("b"))
		h.Key(k)

		if n != 1 {
			t.Fatalf("key %v activated %d times, want 1", k, n)
		}
	}
}

func TestDisabledButtonIsInert(t *testing.T) {
	n := 0
	h := harness(t, ui.ZStack(
		ui.Button(probe(40, 20), func() { n++ }).Key("b").Frame(80, 40).Disabled(true)))

	h.Find(gifttest.ByKey("b")).AssertDisabled()
	h.Tab()
	h.AssertNoFocus()
	h.Find(gifttest.ByKey("b")).Click()

	if n != 0 {
		t.Fatalf("a disabled button activated %d times", n)
	}
}

// TestButtonPressedLooksDifferent checks the visual half on the display list,
// which is what the GPU test under giftgpu then confirms in pixels.
func TestButtonPressedLooksDifferent(t *testing.T) {
	v := ui.ZStack(ui.Button(probe(20, 10), nil).
		Frame(80, 40).
		Style(ui.ButtonStyle{Background: ui.RGB(200, 200, 200)}).
		HoverStyle(ui.ButtonStyle{Background: ui.RGB(150, 150, 150)}).
		PressedStyle(ui.ButtonStyle{Background: ui.RGB(10, 10, 10)}))
	a := buttonApp(t, v)

	normal := backgroundColour(t, a.Paint())

	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))
	hover := backgroundColour(t, a.Paint())
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))
	pressed := backgroundColour(t, a.Paint())
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))

	if normal == hover || hover == pressed || normal == pressed {
		t.Fatalf("the three states are not visually distinct: normal=%v hover=%v pressed=%v", normal, hover, pressed)
	}
}

// backgroundColour returns the colour of the first fill in the list, which is
// the button's own box: it is painted before its label.
func backgroundColour(t *testing.T, l *render.List) render.Color {
	t.Helper()
	for _, op := range l.Ops() {
		if op.Kind == render.OpFillRect || op.Kind == render.OpFillRoundRect {
			return op.Color
		}
	}
	t.Fatal("the button painted no background at all")
	return render.Color{}
}

// TestHoverAndPressDoNotRebuildTheApplication is the ui level repeat of the
// core assertion, and the one that matters: it goes through the real button.
//
// The project plan, section 5, makes hover and press local presentation state
// that must not force a rebuild of the application's view tree. Diagnostics is
// the only way to see the difference from outside, because a rebuilt tree
// looks exactly the same on screen.
func TestHoverAndPressDoNotRebuildTheApplication(t *testing.T) {
	builds := 0
	root := func(ctx *gift.Context) gift.View {
		builds++
		return ui.ZStack(ui.Button(probe(20, 10), nil).Frame(80, 40))
	}
	a := gift.New(gift.Options{Root: root})
	frame(t, a, geom.Sz(400, 300))
	before := a.Diagnostics()

	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(300, 200))
	frame(t, a, geom.Sz(400, 300))

	if got := a.Diagnostics().Builds - before.Builds; got != 0 {
		t.Fatalf("hovering and pressing a button rebuilt %d scopes, want 0", got)
	}
	if builds != 1 {
		t.Fatalf("the root view function ran %d times, want once", builds)
	}
	if got := a.Diagnostics().Layouts - before.Layouts; got != 0 {
		t.Fatalf("hovering and pressing a button caused %d layouts, want 0", got)
	}
}

// TestButtonStateSurvivesAnUnrelatedRebuild: interaction state lives in the
// retained node, so a rebuild triggered by something else must not make a
// pressed button spring back.
func TestButtonStateSurvivesAnUnrelatedRebuild(t *testing.T) {
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("n", 0)
		_ = ctx.Read(st)
		return ui.ZStack(ui.Button(probe(20, 10), nil).Frame(80, 40))
	}
	a := gift.New(gift.Options{Root: root})
	frame(t, a, geom.Sz(400, 300))

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(40, 20))
	h, ok := a.HitTest(geom.Pt(40, 20))
	if !ok || !a.NodeInteraction(h).Pressed {
		t.Fatalf("the button is not pressed after a press")
	}
	st.Set(1) // an unrelated state change, which rebuilds the root
	frame(t, a, geom.Sz(400, 300))
	if !a.NodeInteraction(h).Pressed {
		t.Fatalf("a rebuild for an unrelated reason cleared the press state")
	}
}
