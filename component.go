package gift

import (
	"github.com/torbenschinke/gift/geom"
)

// componentTypeID is the type ID of the internal component view. All
// components share it; they are told apart by their key.
var componentTypeID = RegisterType("gift.Component")

// componentView is the view produced by [Component].
type componentView struct {
	key string
	fn  func(*Context) View
}

func (componentView) ViewType() TypeID { return componentTypeID }

// Build is never called by the reconciler: component views are recognised by
// their concrete type and mounted with a scope of their own. The method only
// exists to satisfy the View interface.
func (c componentView) Build(*BuildContext) Element {
	return Element{Key: c.key}
}

// Component mounts fn as a stateful component instance with a state scope of
// its own.
//
// A plain helper function that returns a View does not create a scope: it is
// inlined into the build of its caller and its state, if it used any, would
// belong to the caller. Only Component introduces the identity that state,
// dependency tracking and partial rebuilds are attached to. See the project
// plan, section 4.
//
// The key identifies the instance among its siblings and must be stable
// across builds. Two calls with the same key in the same parent refer to the
// same instance, even if the function differs; gift cannot compare function
// values, so changing the function under a stable key keeps the existing
// state instead of remounting.
func Component(key string, fn func(*Context) View) View {
	if fn == nil {
		panic("gift: Component with a nil function")
	}
	return componentView{key: key, fn: fn}
}

// passthrough is the layouter and painter of a component node. A component
// adds identity, not appearance: it takes the size of its only child and
// paints it at the origin.
type passthrough struct{}

func (passthrough) Layout(ctx *LayoutContext, c geom.Constraints) geom.Size {
	if ctx.ChildCount() == 0 {
		return c.Constrain(geom.Size{})
	}
	sz := ctx.Measure(0, c)
	ctx.Place(0, geom.Point{})
	return sz
}

func (passthrough) Paint(ctx *PaintContext) { ctx.PaintChildren() }
