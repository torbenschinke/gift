//go:build giftdebug

package ui

import (
	"strings"
	"testing"

	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// TestEveryVisibilityGateIsGuarded is the unit test of [assertResolved], and
// it is an internal test because what it checks is a property of the painters
// of this package and not of anything a caller can reach.
//
// The property: at every point where a painter decides *not* to draw because
// the colour is transparent, the semantic case has already been rejected. That
// ordering is the whole content of the fix, because it is the ordering that
// was wrong. render.List.Add has rejected an unresolved colour since the
// encoding was introduced, and no path in this package could reach it: an
// unresolved colour has an alpha of zero, so each gate below dropped the
// operation first and the backstop never saw it. The symptom was a widget that
// laid out, measured and hit tested normally and drew nothing at all, under
// the very build tag whose job is to explain that.
//
// Each case calls the painter with a nil PaintContext on purpose. The
// assertion is the first statement of each of these functions, so a case that
// panics with a nil dereference instead of the diagnosis below is a case where
// the assertion moved back behind the gate — which is precisely the regression
// worth catching.
func TestEveryVisibilityGateIsGuarded(t *testing.T) {
	tile := TileStyle{
		CornerRadius: 4,
		Selected:     Border{Width: 2, Color: ColorAccent},
		Cursor:       Border{Width: 1, Color: ColorAccent},
	}
	for _, tc := range []struct {
		name string
		call func()
		want string
	}{
		{
			name: "the background gate",
			call: func() { paintBackground(nil, styleSpec{background: ColorSurface}, geom.Rect{}) },
			want: "the background of a node",
		},
		{
			name: "the shadow gate",
			call: func() {
				paintBackground(nil, styleSpec{shadow: Shadow{Blur: 4, Color: ColorLabel}}, geom.Rect{})
			},
			want: "the shadow colour of a node",
		},
		{
			name: "the material gate",
			call: func() {
				m := render.Material{Kind: render.MaterialGlass}
				m.Glass.Tint = ColorAccent
				paintBackground(nil, styleSpec{material: m}, geom.Rect{})
			},
			want: "the glass tint of a node",
		},
		{
			name: "the border gate",
			call: func() {
				paintBorder(nil, styleSpec{border: Border{Width: 1, Color: ColorSeparator}}, geom.Rect{})
			},
			want: "the border colour of a node",
		},
		{
			name: "the glyph gate",
			call: func() { (&textNode{fg: ColorLabel}).paintGlyphs(nil, geom.Rect{}) },
			want: "the foreground of a TextView",
		},
		{
			name: "the palette lookup",
			call: func() {
				placeholderColor(asset.ID("x"), TileStyle{Palette: []Color{ColorAccent}})
			},
			want: "TileStyle.Palette",
		},
		{
			name: "the selection gate",
			call: func() {
				st := tile
				st.Cursor = Border{}
				(&tileNode{}).paintTileState(nil, nil, st, geom.Rect{})
			},
			want: "TileStyle.Selected",
		},
		{
			name: "the cursor gate",
			call: func() {
				st := tile
				st.Selected = Border{}
				(&tileNode{}).paintTileState(nil, nil, st, geom.Rect{})
			},
			want: "TileStyle.Cursor",
		},
		{
			name: "the tile error fill",
			call: func() { assertResolved(ColorAccent, "TileStyle.Error") },
			want: "TileStyle.Error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("the gate accepted an unresolved colour. It is transparent, so this "+
						"did not draw a wrong pixel — it drew nothing, and %q was never said",
						tc.want)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, tc.want) {
					t.Fatalf("the panic does not name %q, so the assertion is behind the gate "+
						"rather than in front of it: %v", tc.want, r)
				}
			}()
			tc.call()
		})
	}
}

// TestAResolvedColourPassesEveryGate is the other direction, so that an
// assertion which rejected everything could not pass as a working one. A nil
// context is fine here too: none of these has anything visible to emit.
func TestAResolvedColourPassesEveryGate(t *testing.T) {
	paintBackground(nil, styleSpec{background: RGBA(0, 0, 0, 0)}, geom.Rect{})
	paintBorder(nil, styleSpec{border: Border{Width: 1}}, geom.Rect{})
	(&textNode{fg: Color{}}).paintGlyphs(nil, geom.Rect{})
	if got := placeholderColor(asset.ID("x"), TileStyle{Palette: []Color{RGB(1, 2, 3)}}); got != RGB(1, 2, 3) {
		t.Fatalf("the palette lookup returned %v", got)
	}
	// paintTileState is deliberately not called here. Its guards are the
	// first two statements, so getting past them is all this direction can
	// show, and everything after them dereferences the gallery and the slot
	// that a nil context cannot supply. The gallery's own tests paint it for
	// real; see gallery_test.go.
}
