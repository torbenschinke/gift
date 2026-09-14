//go:build giftgpu

package ebiten

import (
	"image/color"
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// This file is the only place in the backend that names the ui package, and it
// is a test file. The project plan, section 3, keeps concrete controls out of
// the backend; it does not keep the backend from being *checked* against one,
// and a button whose pressed state exists only in a display list assertion has
// not been shown to look different on a screen.

// paintApp drives one gift frame onto a real image and returns it.
func paintApp(t *testing.T, a *gift.App, w, h int) *eb.Image {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	dst := eb.NewImage(w, h)
	dst.Fill(color.RGBA{0, 0, 0, 255})

	size := geom.Sz(float32(w), float32(h))
	if err := a.Update(size); err != nil {
		t.Fatal(err)
	}
	r.SetTarget(dst)
	r.BeginFrame(size)
	r.Submit(a.Paint())
	r.EndFrame()
	return dst
}

// TestPressedButtonLooksDifferentInPixels is the end to end version of the
// interaction state: a press changes the retained node, the painter reads it,
// the renderer draws it, and the pixel is a different colour. No rebuild
// happens anywhere along that path.
func TestPressedButtonLooksDifferentInPixels(t *testing.T) {
	const w, h = 64, 64
	view := ui.ZStack(
		ui.Button(ui.Box().Frame(4, 4), nil).
			Frame(40, 24).
			Style(ui.ButtonStyle{Background: ui.RGB(200, 200, 200)}).
			HoverStyle(ui.ButtonStyle{Background: ui.RGB(120, 120, 120)}).
			PressedStyle(ui.ButtonStyle{Background: ui.RGB(20, 20, 20)}),
	)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View { return view }})

	// The button sits at the top left of the ZStack, so (20, 12) is inside it.
	const px, py = 20, 12
	normal := paintApp(t, a, w, h).At(px, py).(color.RGBA)

	before := a.Diagnostics().Builds
	a.BeginInput(0)
	a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(px, py))
	hover := paintApp(t, a, w, h).At(px, py).(color.RGBA)

	a.BeginInput(0)
	a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(px, py))
	pressed := paintApp(t, a, w, h).At(px, py).(color.RGBA)
	a.BeginInput(0)
	a.PointerUp(gift.MousePointer, gift.PointerMouse, geom.Pt(px, py))

	t.Logf("normal=%v hover=%v pressed=%v", normal, hover, pressed)
	if normal == hover {
		t.Errorf("hovering the button changed no pixel: %v", normal)
	}
	if hover == pressed {
		t.Errorf("pressing the button changed no pixel: %v", pressed)
	}
	if pressed.R > 64 {
		t.Errorf("the pressed button is %v, want the dark pressed style", pressed)
	}
	if got := a.Diagnostics().Builds - before; got != 0 {
		t.Errorf("hovering and pressing rebuilt %d scopes, want 0", got)
	}
}
