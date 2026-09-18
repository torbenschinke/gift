package gift

import (
	"fmt"

	"github.com/worldiety/gift/internal/scene"
)

// Context is the interface of a component function to the runtime.
//
// It is owned by the scope of the component instance and is valid only for
// the duration of the build that received it. Keeping it and using it later,
// for example from an event handler or a goroutine, is a programming error
// and is rejected.
type Context struct {
	app   *App
	scope *scope
}

// State returns the typed state with the given name, creating it with the
// value initial on first use.
//
// The name is local to the component instance: two instances of the same
// component function, and two different component functions, may use the same
// name without interfering. The initial value is evaluated by the caller on
// every build but only used on the first one.
//
// Using the same name with a different element type in the same component
// instance panics. That situation always means the identity of the component
// instance is wrong, and silently starting over with a fresh value would hide
// it.
//
// This is a generic method on a concrete type, which is what Go 1.27 makes
// possible. It keeps the value typed all the way through, without a type
// assertion at the call site and without boxing the value on every read.
func (c *Context) State[T comparable](name string, initial T) *State[T] {
	c.assertBuilding("Context.State")
	sc := c.scope
	if existing, ok := sc.states[name]; ok {
		typed, ok := existing.(*State[T])
		if !ok {
			base := existing.stateBase()
			panic(fmt.Sprintf(
				"gift: state %q in component %q changed its type from %s to %s; "+
					"the same name must keep one type for the lifetime of a component instance",
				name, sc.path, base.typeName, typeNameOf(initial)))
		}
		return typed
	}
	s := &State[T]{
		base: stateBase{
			app:      c.app,
			owner:    sc,
			name:     name,
			typeName: typeNameOf(initial),
		},
		v: initial,
	}
	if sc.states == nil {
		sc.states = make(map[string]anyState, 4)
	}
	sc.states[name] = s
	return s
}

// Read returns the current value of s and registers the state as a dependency
// of the component instance currently being built.
//
// The dependency set is replaced after every successful build, not
// accumulated: a state that is no longer read stops invalidating this
// component.
func (c *Context) Read[T comparable](s *State[T]) T {
	c.assertBuilding("Context.Read")
	c.app.registerDep(c.scope, &s.base)
	return s.v
}

// Key returns the key this component instance was mounted under. It is meant
// for diagnostics, not for building identity on top of it.
func (c *Context) Key() string { return c.scope.key }

// Path returns the slash separated path of component keys from the root to
// this component instance. It is meant for diagnostics and appears in the
// panic messages of this package.
func (c *Context) Path() string { return c.scope.path }

func (c *Context) assertBuilding(what string) {
	if c.app.building != c.scope {
		panic(fmt.Sprintf(
			"gift: %s used outside the build of component %q; a Context is valid only during the build that received it",
			what, c.scope.path))
	}
}

// typeNameOf returns the Go type name of v. It uses fmt and therefore
// reflection, which is acceptable because it runs once per created state and
// once per diagnosed contract violation, never in the frame path.
func typeNameOf(v any) string { return fmt.Sprintf("%T", v) }

// scope is one component instance: a unit of rebuild and the owner of state.
type scope struct {
	app *App
	// key is the key the component was mounted under.
	key string
	// path is the diagnostic path from the root, for example
	// "/root/left-counter".
	path string
	// cell holds the component function and, for a memoised component, the
	// props of the last build. It is created once at mount and survives
	// every rebuild.
	cell compCell
	// node is the scene node representing the component itself. Its single
	// child is the subtree the component function produced.
	node scene.Handle
	// parent is the enclosing component instance, nil for the root.
	parent *scope
	// states maps local state names to the states themselves.
	states map[string]anyState
	// deps are the states read during the last successful build.
	deps []*stateBase
	// one is the reusable one element view slice used to reconcile the
	// single child of the component node without allocating per build.
	one [1]View

	// gen is the request generation of this instance; see [Token].
	gen uint64

	ctx        Context
	needsBuild bool
	queued     bool
	alive      bool
}
