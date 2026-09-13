// Package geom provides the numeric geometry primitives shared by the layout,
// input and rendering layers of gift.
//
// # Scope
//
// The package contains Point, Size, Rect, Insets, Constraints, Affine2D and
// Alignment. It deliberately contains no views, no state and no backend types,
// so that it can be imported from every other package without creating a
// dependency cycle.
//
// # Why float32
//
// All coordinates are float32 because they describe the view space, which is
// ultimately handed to the GPU. Ebitengine and the underlying graphics drivers
// consume float32 vertices, so using float64 here would only add conversions
// without adding precision at the point of use.
//
// This is not a general purpose numeric type. Very large document space
// positions, such as the scroll offset of a gallery with 100.000 entries, live
// in float64 inside internal/layout. They are converted to float32 only after
// the viewport origin has been subtracted, so that the values reaching this
// package are small and precise. Callers that carry document coordinates must
// respect that rule; see the project plan, section 10.
//
// # Value semantics
//
// Every type in this package is a small, comparable value type. All methods use
// value receivers and return new values; nothing is mutated in place. There are
// no slices, maps, interfaces or pointers anywhere in the package, so every
// operation is allocation free and inlinable. This is a hard contract: the
// package test suite asserts exactly zero allocations for a representative mix
// of all operations (see the project plan, section 11).
//
// # Unbounded
//
// Constraints may be unbounded in either axis. The sentinel for that is
// [Unbounded], which is positive infinity. All operations in this package are
// written so that an Unbounded maximum never produces a NaN.
package geom
