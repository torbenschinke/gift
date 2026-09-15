package ui_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// This is the check the project plan, section 4, decided on in place of a
// generator: "ein Test, der ueber alle exportierten View-Typen reflektiert und
// den gemeinsamen Modifier-Satz mit korrekter Signatur einfordert."
//
// The failure it catches is real and silent in the small: a new view type that
// is missing Padding compiles perfectly, and the omission only shows up when
// somebody tries to use it, at which point the fix is one line in a file they
// did not expect to touch. A generator would have prevented the same failure
// at the cost of a build step and a second place to look for
// "func (t TextView) Padding".

// modifierSet is the group of modifiers a view type belongs to.
type modifierSet uint8

const (
	// setMinimal is what every view has, no matter what it is.
	setMinimal modifierSet = iota
	// setStyled adds the size and box style modifiers of a view that has
	// bounds it can fill, stroke and clip.
	setStyled
	// setInternal is a view type that exists only as a part of a composite
	// and that an application never constructs. It has no public modifier
	// contract to check, because it has no public constructor; it is listed
	// so that TestEveryViewTypeIsListed stays a check rather than a list
	// with an escape hatch.
	setInternal
)

// views lists one value of every exported view type together with the set it
// promises to implement.
//
// Adding a view type to this package without adding it here fails
// TestEveryViewTypeIsListed below, which is what makes this table a check
// rather than a list somebody forgets.
var views = []struct {
	name string // the name it registered under
	set  modifierSet
	v    any
	// why, for a type that is deliberately not fully styled.
	why string
}{
	{name: "ui.VStack", set: setStyled, v: ui.VStack()},
	{name: "ui.HStack", set: setStyled, v: ui.HStack()},
	{name: "ui.ZStack", set: setStyled, v: ui.ZStack()},
	{name: "ui.Box", set: setStyled, v: ui.Box()},
	{name: "ui.Text", set: setStyled, v: ui.Text("x")},
	{name: "ui.Button", set: setStyled, v: ui.Button(ui.Box(), nil)},
	{name: "ui.VScroll", set: setStyled, v: ui.VScroll()},
	{name: "ui.HScroll", set: setStyled, v: ui.HScroll()},
	{name: "ui.Image", set: setStyled, v: ui.Image(asset.File("x.jpg"))},
	{name: "ui.Icon", set: setStyled, v: ui.Icon(ui.Symbol{})},
	{name: "ui.TextField", set: setStyled, v: ui.TextField(ui.NewTextEditor(""))},
	{name: "ui.ImageGallery", set: setStyled, v: ui.ImageGallery(ui.NewGallery(asset.NewCollection(nil)))},
	{name: "ui.OnScreenKeyboard", set: setStyled, v: ui.OnScreenKeyboard()},
	{
		name: "ui.GalleryTile", set: setInternal,
		why: "a tile is one slot of the gallery's recycled pool. It has no exported " +
			"constructor and no styling of its own: what it looks like is ui.TileStyle, " +
			"set on the gallery, because a tile is bound to an item during layout and a " +
			"per tile modifier would have nowhere to be written.",
	},
	{
		name: "ui.Spacer", set: setMinimal, v: ui.Spacer(),
		why: "a Spacer draws nothing and has no bounds of its own; a Background it then " +
			"ignored would be exactly the lie variant A exists to avoid. See SpacerView.",
	},
}

// minimalModifiers are required of every view type. The signature is given as
// the argument types; the result is always the receiver's own concrete type,
// which is the property variant A rests on and which is checked separately.
var minimalModifiers = []struct {
	name string
	args []reflect.Type
}{
	{"Key", []reflect.Type{stringType}},
	{"Flex", []reflect.Type{float32Type}},
}

// styledModifiers are required in addition of every view with bounds.
var styledModifiers = []struct {
	name string
	args []reflect.Type
}{
	{"Padding", []reflect.Type{float32Type}},
	{"PaddingInsets", []reflect.Type{insetsType}},
	{"Frame", []reflect.Type{float32Type, float32Type}},
	{"MinWidth", []reflect.Type{float32Type}},
	{"MinHeight", []reflect.Type{float32Type}},
	{"MaxWidth", []reflect.Type{float32Type}},
	{"MaxHeight", []reflect.Type{float32Type}},
	{"Background", []reflect.Type{backgroundType}},
	{"Border", []reflect.Type{borderType}},
	{"Shadow", []reflect.Type{shadowType}},
	{"CornerRadius", []reflect.Type{float32Type}},
	{"Clip", []reflect.Type{boolType}},
}

var (
	stringType  = reflect.TypeOf("")
	float32Type = reflect.TypeOf(float32(0))
	boolType    = reflect.TypeOf(false)
	insetsType  = reflect.TypeOf(geom.Insets{})
	colorType   = reflect.TypeOf(ui.Color{})
	// Background takes an interface and not a Color, because the project
	// plan, section 8, spells a material as
	// ".Background(ui.Glass().Quality(ui.Adaptive))" and Go has no
	// overloading. ui.Color still satisfies it, so ".Background(ui.RGB(...))"
	// is unchanged; TestBackgroundAcceptsBothKinds pins that.
	backgroundType = reflect.TypeOf((*ui.Background)(nil)).Elem()
	borderType     = reflect.TypeOf(ui.Border{})
	shadowType     = reflect.TypeOf(ui.Shadow{})
)

func TestViewsCarryTheSharedModifierSet(t *testing.T) {
	for _, view := range views {
		if view.set == setInternal {
			continue
		}
		rt := reflect.TypeOf(view.v)
		want := minimalModifiers
		if view.set == setStyled {
			want = append(append([]struct {
				name string
				args []reflect.Type
			}{}, minimalModifiers...), styledModifiers...)
		}
		for _, mod := range want {
			m, ok := rt.MethodByName(mod.name)
			if !ok {
				t.Errorf("%s has no method %s; every view with bounds carries the shared modifier set, "+
					"see the project plan, section 4", rt, mod.name)
				continue
			}
			ft := m.Type
			// Method obtained from a type, so argument 0 is the receiver.
			if got, want := ft.NumIn()-1, len(mod.args); got != want {
				t.Errorf("%s.%s takes %d arguments, want %d", rt, mod.name, got, want)
				continue
			}
			for i, at := range mod.args {
				if got := ft.In(i + 1); got != at {
					t.Errorf("%s.%s argument %d is %v, want %v", rt, mod.name, i, got, at)
				}
			}
			if ft.NumOut() != 1 || ft.Out(0) != rt {
				t.Errorf("%s.%s returns %v, want its own type %v; a modifier that returned an "+
					"interface would end the fluent chain and is what variant A rejects", rt, mod.name, outOf(ft), rt)
			}
		}
	}
}

func outOf(ft reflect.Type) string {
	if ft.NumOut() == 0 {
		return "nothing"
	}
	return ft.Out(0).String()
}

// TestEveryViewTypeIsListed is the half that makes the test above fail for a
// view type that does not exist yet.
//
// Reflection cannot enumerate the types of a package, so the list of view
// types is taken from gift's type registry instead: a view type that is not
// registered cannot be reconciled, so every one of them is in there.
func TestEveryViewTypeIsListed(t *testing.T) {
	registered := gift.RegisteredTypeNames(nil)
	for _, name := range registered {
		if !strings.HasPrefix(name, "ui.") {
			continue
		}
		found := false
		for _, v := range views {
			if v.name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the view type %q is registered but not listed in views; add it there and "+
				"say which modifier set it implements", name)
		}
	}
	for _, v := range views {
		found := false
		for _, name := range registered {
			if name == v.name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("views lists %q, which no longer registers itself under that name", v.name)
		}
	}
}

// TestEveryViewIsAView keeps the table honest about what it contains.
func TestEveryViewIsAView(t *testing.T) {
	for _, v := range views {
		if v.set == setInternal {
			continue
		}
		if _, ok := v.v.(gift.View); !ok {
			t.Errorf("%T is in views but does not implement gift.View", v.v)
		}
	}
}
