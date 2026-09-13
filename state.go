package gift

import "fmt"

// anyState is the type erased view of a [State] that the scope bookkeeping
// needs. It exists so that states of different element types can live in one
// map without reflection.
type anyState interface {
	stateBase() *stateBase
}

// stateBase is the part of a state that does not depend on its element type.
type stateBase struct {
	app   *App
	owner *scope
	name  string
	// typeName is the Go type of the value, kept for the diagnosis of a type
	// change under the same name. It is computed once, when the state is
	// created, never in the frame path.
	typeName string
	// subs are the scopes that read this state during their last successful
	// build. A set as a slice: it is tiny and linear scanning beats a map
	// for the sizes that occur here.
	subs []*scope
}

// State is a typed, observable value owned by one component instance.
//
// The element type is constrained to comparable because change detection uses
// ==. This is deliberate: a deep equality default would make every write
// unpredictably expensive and would silently accept in place mutation of maps
// and slices, which cannot be detected at all. Values that are not comparable
// get an explicit comparator or version based path later; see the project
// plan, section 5.
//
// A state lives until its scope is unmounted, not until a build stops looking
// it up. A cached subtree that is not rebuilt keeps its state.
type State[T comparable] struct {
	base stateBase
	v    T
}

func (s *State[T]) stateBase() *stateBase { return &s.base }

// Get returns the current value without registering a dependency.
//
// This is the right call inside an event handler, where reading a value must
// not subscribe anything to it. Inside a build, use [Context.Read].
func (s *State[T]) Get() T {
	s.base.app.assertUIGoroutine("State.Get")
	return s.v
}

// Set stores v and invalidates every scope that read this state in its last
// build.
//
// Writing the same value again, compared with ==, changes nothing and
// invalidates nothing. A real change marks the dependent scopes as needing a
// build; the rebuild happens in the next [App.Update], not inside Set.
// Several writes before the next update are therefore coalesced into one
// rebuild.
//
// Calling Set from an event handler is the normal case. Calling it from
// another goroutine is a programming error and panics as far as it is
// detectable, see [App.Update]. Calling it during [App.Paint] is a contract
// violation and panics, because it would mean building during draw.
func (s *State[T]) Set(v T) {
	a := s.base.app
	a.assertUIGoroutine("State.Set")
	if a.painting {
		panic(fmt.Sprintf("gift: State.Set on %q during Paint, which would build during draw", s.base.path()))
	}
	if s.v == v {
		return
	}
	s.v = v
	for _, sc := range s.base.subs {
		a.markNeedsBuild(sc)
	}
}

// Binding returns a read/write handle to this state.
//
// A binding does not transfer ownership: the state stays with its scope and
// outlives every binding handed to a control.
func (s *State[T]) Binding() Binding[T] { return Binding[T]{s: s} }

// Binding connects reading and writing of one state for a control, without
// giving the control ownership of the state.
//
// The zero Binding is unusable; obtain one from [State.Binding].
type Binding[T comparable] struct {
	s *State[T]
}

// Get returns the current value. If a build is in progress, the building
// scope is registered as a dependency, so a control that renders a bound
// value is rebuilt when the value changes.
func (b Binding[T]) Get() T {
	if b.s == nil {
		panic("gift: Get on the zero Binding")
	}
	a := b.s.base.app
	a.assertUIGoroutine("Binding.Get")
	if a.building != nil {
		a.registerDep(a.building, &b.s.base)
	}
	return b.s.v
}

// Set stores v, with the same semantics as [State.Set].
func (b Binding[T]) Set(v T) {
	if b.s == nil {
		panic("gift: Set on the zero Binding")
	}
	b.s.Set(v)
}

// IsZero reports whether the binding is the unusable zero value.
func (b Binding[T]) IsZero() bool { return b.s == nil }

func (b *stateBase) path() string {
	if b.owner == nil {
		return b.name
	}
	return b.owner.path + "." + b.name
}

// registerDep records that sc read the state b during the build currently in
// progress.
func (a *App) registerDep(sc *scope, b *stateBase) {
	for _, d := range sc.deps {
		if d == b {
			return
		}
	}
	sc.deps = append(sc.deps, b)
	b.subs = append(b.subs, sc)
}

// clearDeps drops all dependencies of sc. It runs at the start of a build, so
// that the dependency set after the build is the set of states actually read
// during it, not the union with everything read before.
func (a *App) clearDeps(sc *scope) {
	for _, b := range sc.deps {
		for i, s := range b.subs {
			if s == sc {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
	}
	sc.deps = sc.deps[:0]
}
