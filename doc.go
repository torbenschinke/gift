// Package gift is the runtime core of the gift UI toolkit.
//
// It owns the view contract, the build context, component scopes, typed
// state and bindings, the retained tree, layout and the frame lifecycle.
// Concrete widgets live in ui, concrete rendering lives in backend/ebiten;
// see the project plan, section 3.
//
// # Frame model
//
// Ebitengine decouples Update from Draw: several updates may run before a
// frame is drawn, and the whole screen is cleared and redrawn every frame.
// gift therefore does all of its CPU work in [App.Update] and only produces
// the display list in [App.Paint]. Build and layout in Paint are a contract
// violation and panic. See the project plan, section 6.
//
// # Invalidation
//
// The three levels build, layout and paint are kept separate. A build implies
// a layout and a layout implies a paint, never the other way round.
// Invalidation saves CPU work, not fill rate: a frame that changes nothing
// still redraws everything, but it neither calls view functions nor measures
// anything.
//
// # Allocation
//
// The frame path without build is allocation free after warmup: an update
// that finds nothing dirty plus a paint over an unchanged tree performs zero
// allocations. Build is explicitly excluded from that contract, because
// composing views boxes them into interfaces by construction. See the project
// plan, section 11.
//
// # Logging
//
// Nothing in the frame path logs, not even behind a level check, because
// constructing the attributes alone would allocate. The frame path only
// increments plain numeric counters, which are read out of band through
// [App.Diagnostics]. gift never logs to slog.Default; without a logger in
// [Options] logging is off. See the project plan, section 15.
//
// # Concurrency
//
// An App, its state and its tree belong to one goroutine, the UI executor.
// Touching state from anywhere else is a programming error and is rejected
// with a panic as far as it is cheaply detectable; see [State.Set].
package gift
