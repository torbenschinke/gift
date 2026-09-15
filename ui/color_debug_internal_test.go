//go:build giftdebug

package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// TestTheGateInventoryIsComplete is the part of this file that generalises,
// and it exists because the table below does not.
//
// TestEveryVisibilityGateIsGuarded is hand written. A fifth colour arriving
// with a transparency gate and no assertion in front of it is simply absent
// from that table, and an absent case fails nothing — which is not a
// hypothetical: ui.Image had exactly such a gate, unguarded and unenumerated,
// from the day it was written until this work unit. So the table is paired
// with a scan of the package's own source for the call that *is* the gate,
// [Color.IsTransparent], and the scan fails when a function that did not
// contain one starts to.
//
// What it can and cannot see, stated rather than implied. It works on
// functions and not on statements, so a new gate inside a function that
// already has one — a fifth colour in paintContent — is invisible to it and
// only the table would catch it. It is a source scan and not a proof: it says
// "somebody has to think about this function", and the thinking is the entry
// below. That is the cheap ninety percent of a checker, and an AST analysis
// that tried to decide *which* colour a gate reads and whether an assertion
// dominates it would be a static analyser living in a test file.
func TestTheGateInventoryIsComplete(t *testing.T) {
	// The inventory. Every function in this package that asks a colour
	// whether it is transparent, and why that is safe.
	known := map[string]string{
		"paintBackground":            "guarded: assertResolved on the background, the shadow and the glass tint",
		"textNode.paintGlyphs":       "guarded: assertResolved on the foreground of a TextView",
		"textFieldNode.paintContent": "guarded: four assertResolved calls, the first four statements",
		"tileNode.Paint":             "guarded: assertResolved on TileStyle.Error and TileStyle.Provisional",
		"imageNode.Paint":            "guarded: assertResolved on the placeholder colour of an Image",
		"iconNode.Paint":             "guarded: assertResolved on the foreground of an Icon",
		"keyboardNode.paintKeys": "guarded: five assertResolved calls, the first five statements of the " +
			"function",
		"styleSpec.needsPainter": "not a gate: it decides whether a painter is allocated at all, so an " +
			"unresolved colour there costs an interface and never a pixel",
		"ScrollBar.withDefaults": "not a gate: it resolves first and then reads the transparency as " +
			"\"the caller set nothing\", which is a fallback and not a refusal to draw",
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the package: %v", err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}
				who := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) == 1 {
					who = recvName(fn.Recv.List[0].Type) + "." + who
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "IsTransparent" {
						return true
					}
					if seen[who] {
						return false
					}
					seen[who] = true
					if _, ok := known[who]; !ok {
						t.Errorf("%s: %s asks a colour whether it is transparent and is not in "+
							"the inventory of this test.\nIf it is a visibility gate, put an "+
							"assertResolved in front of it and a case in "+
							"TestEveryVisibilityGateIsGuarded; an unresolved colour is "+
							"transparent, so the gate would drop the operation and the widget "+
							"would draw nothing at all.\nIf it is not a gate, add it here with "+
							"the reason.", fset.Position(call.Pos()), who)
					}
					return false
				})
				return false
			})
			_ = name
		}
	}
	for name := range known {
		if !seen[name] {
			t.Errorf("the inventory names %s, which no longer asks a colour whether it is "+
				"transparent. Remove the entry, so that the list stays the list of things "+
				"somebody has thought about.", name)
		}
	}
}

// recvName is the type name of a method receiver, pointer or not.
func recvName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

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
// It does not find a new gate on its own. It is a table somebody writes, so a
// colour added with a gate and without a case here is absent rather than
// failing; TestTheGateInventoryIsComplete above is the part that notices, at
// the granularity of a whole function. Both are needed and neither replaces
// the other.
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
			name: "the text field foreground",
			call: func() {
				(&textFieldNode{fg: ColorLabel, ed: NewTextEditor("")}).
					paintContent(nil, geom.Rect{}, gift.Interaction{})
			},
			want: "the foreground of a TextField",
		},
		{
			name: "the text field placeholder",
			call: func() {
				(&textFieldNode{placeholderFG: ColorSecondaryLabel, ed: NewTextEditor("")}).
					paintContent(nil, geom.Rect{}, gift.Interaction{})
			},
			want: "the placeholder colour of a TextField",
		},
		{
			name: "the text field selection",
			call: func() {
				(&textFieldNode{selectionBG: Fade(ColorAccent, 0.3), ed: NewTextEditor("x")}).
					paintContent(nil, geom.Rect{}, gift.Interaction{Focused: true})
			},
			want: "the selection colour of a TextField",
		},
		{
			name: "the text field caret",
			call: func() {
				(&textFieldNode{caretColor: ColorLabel, ed: NewTextEditor("")}).
					paintContent(nil, geom.Rect{}, gift.Interaction{Focused: true})
			},
			want: "the caret colour of a TextField",
		},
		{
			name: "the keyboard key face",
			call: func() {
				(&keyboardNode{face: ColorControl, st8: &keyboardState{pressed: -1}}).
					paintKeys(nil, geom.Rect{})
			},
			want: "the key face of an OnScreenKeyboard",
		},
		{
			name: "the keyboard pressed key face",
			call: func() {
				(&keyboardNode{pressed: ColorControlPressed, st8: &keyboardState{pressed: -1}}).
					paintKeys(nil, geom.Rect{})
			},
			want: "the pressed key face of an OnScreenKeyboard",
		},
		{
			name: "the keyboard shift colour",
			call: func() {
				(&keyboardNode{accent: ColorAccent, st8: &keyboardState{pressed: -1}}).
					paintKeys(nil, geom.Rect{})
			},
			want: "the armed shift colour of an OnScreenKeyboard",
		},
		{
			name: "the keyboard label colour",
			call: func() {
				(&keyboardNode{fg: ColorLabel, st8: &keyboardState{pressed: -1}}).
					paintKeys(nil, geom.Rect{})
			},
			want: "the key label colour of an OnScreenKeyboard",
		},
		{
			name: "the keyboard key border",
			call: func() {
				(&keyboardNode{
					keyBorder: Border{Width: 1, Color: ColorSeparator},
					st8:       &keyboardState{pressed: -1},
				}).paintKeys(nil, geom.Rect{})
			},
			want: "the key border colour of an OnScreenKeyboard",
		},
		{
			name: "the icon foreground",
			call: func() { (&iconNode{fg: ColorLabel}).Paint(nil) },
			want: "the foreground of an Icon",
		},
		{
			name: "the image placeholder",
			call: func() { (&imageNode{placeholder: ColorAccent}).Paint(nil) },
			want: "the placeholder colour of an Image",
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
