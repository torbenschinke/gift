// Package layout contains the renderer neutral layout algorithms of gift.
//
// It knows nothing about views, nodes or the module root: the project plan,
// section 3, forbids importing gift from an internal helper package. The
// algorithms therefore operate on plain values that the caller passes in, and
// measure children through the narrow [Measurer] interface. The ui package
// owns the gift.Layouter implementations and delegates the arithmetic here,
// which is what keeps one stack algorithm for every backend.
//
// # Allocation
//
// Nothing in this package allocates. Every function that needs per child
// scratch space takes it from the caller as a slice; see [Stack].
package layout
