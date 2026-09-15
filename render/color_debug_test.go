//go:build giftdebug

package render_test

import (
	"strings"
	"testing"

	"github.com/torbenschinke/gift/render"
)

// TestAddRejectsAnUnresolvedColour pins the debug build check of color_debug.go.
//
// The value below is what gift/ui's semantic colours look like before
// ui.ResolveColor has been called on them: a negative red channel, which no
// premultiplied colour can have. Without this check such a colour reaches the
// backend, where it is neither a colour nor an error — it is whatever the
// blend makes of a negative number, which on the two backends tried was a
// black rectangle and an invisible one.
//
// The test is tagged rather than conditional because the check itself is: the
// project plan, section 15, keeps this class of diagnosis out of the release
// build so that the frame path does not pay for it.
func TestAddRejectsAnUnresolvedColour(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("List.Add accepted an operation whose colour has a negative channel")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "ResolveColor") {
			t.Errorf("the panic does not say what to do about it: %v", r)
		}
	}()
	var l render.List
	l.Add(render.Op{Kind: render.OpFillRect, Color: render.Color{R: -1, G: 2}})
}

// TestAddAcceptsAnOrdinaryColour is the other direction, so that a check that
// rejected everything could not pass as a working one.
func TestAddAcceptsAnOrdinaryColour(t *testing.T) {
	var l render.List
	l.Add(render.Op{Kind: render.OpFillRect, Color: render.RGBA(10, 20, 30, 40)})
	l.Add(render.Op{Kind: render.OpFillRect})
	if got := len(l.Ops()); got != 2 {
		t.Fatalf("the list holds %d operations, want 2", got)
	}
}

// TestAddMaterialRejectsAnUnresolvedTint covers the colour that does not
// travel on an [render.Op].
//
// A material goes into a side table through [render.List.AddMaterial], so it
// never passes [render.List.Add] and the check above could not see it. That
// gap was not theoretical: gift/ui resolved the background, the border and the
// shadow of a style and not its material, so a glass pane tinted with
// ui.ColorAccent arrived at the shader carrying a red channel of -1 and, in a
// build without this tag, painted saturated cyan at both quality levels. The
// backend's own tint.IsTransparent guard did not engage, because a negative
// tint is not a transparent one.
func TestAddMaterialRejectsAnUnresolvedTint(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("AddMaterial accepted a tint with a negative channel")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "material tint") {
			t.Errorf("the panic does not say which colour it means: %v", r)
		}
	}()
	var l render.List
	m := render.Material{Kind: render.MaterialGlass}
	m.Glass.Tint = render.Color{R: -1, G: 7, B: 1}
	l.AddMaterial(m)
}

// TestAddMaterialAcceptsAnOrdinaryTint is the other direction again, including
// the [render.MaterialNone] sentinel that a painter is invited to pass
// unconditionally.
func TestAddMaterialAcceptsAnOrdinaryTint(t *testing.T) {
	var l render.List
	m := render.Material{Kind: render.MaterialGlass}
	m.Glass.Tint = render.RGBA(255, 255, 255, 30)
	if got := l.AddMaterial(m); got != 1 {
		t.Errorf("the material landed at index %d, want 1", got)
	}
	if got := l.AddMaterial(render.Material{}); got != 0 {
		t.Errorf("the sentinel landed at index %d, want 0", got)
	}
}
