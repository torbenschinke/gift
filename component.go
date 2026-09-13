package gift

import (
	"fmt"

	"github.com/torbenschinke/gift/geom"
)

// componentTypeID is the type ID of the internal component views. All
// components, memoised or not, share it; they are told apart by their key.
var componentTypeID = RegisterType("gift.Component")

// compSource is implemented by the views that mount a component scope.
//
// It exists so that the reconciler can recognise a component without knowing
// how many type parameters it carries: [Memo] is generic over its props, so it
// cannot be matched with a single concrete type assertion.
type compSource interface {
	View
	// compKey returns the key the instance is mounted under.
	compKey() string
	// newCell creates the per instance cell that holds the build function
	// and, for a memo, the props of the last build.
	newCell() compCell
}

// compCell is the per instance half of a component. It lives in the scope and
// survives every rebuild.
type compCell interface {
	// update applies the freshly described view to the cell and reports
	// whether a rebuild is required. A plain component always requires one;
	// a memo only when its props changed.
	update(v View) bool
	// build runs the component function.
	build(ctx *Context) View
}

// --- plain component --------------------------------------------------------

// componentView is the view produced by [Component].
type componentView struct {
	key string
	fn  func(*Context) View
}

func (componentView) ViewType() TypeID { return componentTypeID }

// Build is never called by the reconciler: component views are recognised by
// their concrete type and mounted with a scope of their own. The method only
// exists to satisfy the View interface.
func (c componentView) Build(*BuildContext) Element { return Element{Key: c.key} }

func (c componentView) compKey() string { return c.key }

func (c componentView) newCell() compCell { return &plainCell{fn: c.fn} }

type plainCell struct {
	fn func(*Context) View
}

func (p *plainCell) update(v View) bool {
	cv, ok := v.(componentView)
	if !ok {
		panic(fmt.Sprintf(
			"gift: a plain Component and a %T were mounted under the same key; "+
				"Component and Memo are different kinds of component and may not swap places under one key",
			v))
	}
	p.fn = cv.fn
	return true
}

func (p *plainCell) build(ctx *Context) View { return p.fn(ctx) }

// Component mounts fn as a stateful component instance with a state scope of
// its own.
//
// A plain helper function that returns a View does not create a scope: it is
// inlined into the build of its caller and its state, if it used any, would
// belong to the caller. Only a component introduces the identity that state
// and dependency tracking are attached to. See the project plan, section 4.
//
// # Rebuild semantics, stated honestly
//
// Component has no rebuild boundary of its own. Its props, if it has any, are
// captured in the closure fn, and gift cannot compare function values, so it
// cannot tell whether the inputs of this instance changed. Consequently
// *every* rebuild of an enclosing scope also rebuilds this one, all the way
// down. What Component does give you is the other direction: a state write
// inside this instance rebuilds this instance only, not its parent.
//
// When the top down direction matters — a deep tree whose root invalidates
// often — use [Memo], which makes the props explicit and comparable and skips
// the rebuild when they did not change.
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

// --- memoised component -----------------------------------------------------

// memoView is the view produced by [Memo].
type memoView[P comparable] struct {
	key   string
	props P
	fn    func(*Context, P) View
}

func (memoView[P]) ViewType() TypeID { return componentTypeID }

func (m memoView[P]) Build(*BuildContext) Element { return Element{Key: m.key} }

func (m memoView[P]) compKey() string { return m.key }

func (m memoView[P]) newCell() compCell {
	return &memoCell[P]{props: m.props, fn: m.fn}
}

// memoCell stores the props of the last build in their concrete type, so that
// comparing them is a plain == and needs neither reflection nor boxing.
type memoCell[P comparable] struct {
	props P
	fn    func(*Context, P) View
}

func (c *memoCell[P]) update(v View) bool {
	m, ok := v.(memoView[P])
	if !ok {
		panic(fmt.Sprintf(
			"gift: a Memo with props of type %T and a %T were mounted under the same key; "+
				"the props type of a memoised component must be stable for the lifetime of the instance",
			c.props, v))
	}
	c.fn = m.fn
	if c.props == m.props {
		return false
	}
	c.props = m.props
	return true
}

func (c *memoCell[P]) build(ctx *Context) View { return c.fn(ctx, c.props) }

// Memo mounts a stateful component that is only rebuilt when props changes or
// when one of its own state dependencies fires.
//
// It is [Component] with the top down rebuild boundary that Component cannot
// have. The inputs of the instance are passed as an explicit comparable value
// instead of being captured in a closure, so gift can compare them with ==:
// when they are equal to the props of the previous build and no state this
// instance read has changed, the component function is not called at all and
// the whole subtree below it is left untouched. That is the "Build: kein
// Aufruf der View-Funktionen" row of the project plan, section 6.
//
// P must be comparable for the same reason [State] requires it: a deep
// equality default would make every build unpredictably expensive and would
// silently accept in place mutation. A struct of the values the component
// actually reads is the intended shape:
//
//	gift.Memo("row", rowProps{ID: id, Selected: sel}, renderRow)
//
// Do not put a slice, a map or a freshly allocated closure in P. A slice is
// not comparable and will not compile; a func field is comparable only
// against nil and will panic at runtime. Both are the signal that the value
// belongs in state or behind a stable pointer instead.
//
// The key has the same meaning as in [Component]. Changing the props type
// under a stable key is a programming error and is rejected, because the
// instance would otherwise silently keep state that belongs to a different
// component.
func Memo[P comparable](key string, props P, fn func(*Context, P) View) View {
	if fn == nil {
		panic("gift: Memo with a nil function")
	}
	return memoView[P]{key: key, props: props, fn: fn}
}

// --- shared node behaviour --------------------------------------------------

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
