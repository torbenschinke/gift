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
// Nothing on the frame path allocates. Every function that needs per child
// scratch space takes it from the caller as a slice; see [Stack].
//
// The one exception is the gallery index, and it is a deliberate one. [Index]
// owns O(N) arrays for a hundred thousand items, so it cannot take them from a
// caller's stack; it allocates them while a layout is being *built*, which the
// project plan, section 10, explicitly permits to cost O(N) off the hot path,
// and it reuses them across rebuilds. Querying the index — which is the part
// that runs every frame — allocates nothing; see [Index.Visible].
package layout
