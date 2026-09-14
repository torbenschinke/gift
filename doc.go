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
// The layout level is partial, not all or nothing. A node that is rebuilt
// marks itself and every node up to the root as needing layout, and the layout
// pass descends along that path only: a clean node that is offered the
// constraints it was last measured with returns its cached size without
// running its layouter, so a state write in one subtree does not measure a
// sibling subtree. [App.Diagnostics] makes that observable.
//
// The build level has a top down boundary too, but only where the application
// asks for one. [Component] is always rebuilt when its parent is, because its
// inputs are captured in a closure that gift cannot compare. [Memo] takes its
// inputs as an explicit comparable value and is skipped when they are
// unchanged.
//
// # Allocation
//
// The frame path without build is allocation free after warmup: an update
// that finds nothing dirty plus a paint over an unchanged tree performs zero
// allocations. Build is explicitly excluded from that contract, because
// composing views boxes them into interfaces by construction. See the project
// plan, section 11.
//
// # Input
//
// Input is polled by the backend, turned into events and dispatched into the
// App during Ebitengine's Update, before build and layout. Event handlers
// therefore run where state writes are allowed and are picked up by the build
// of the same tick; see the project plan, section 6.
//
// A node takes part in hit testing only if its [Element] carries an
// [Interactor]. Hit testing walks the retained tree front to back and honours
// the clips and transforms of ancestors, using the same device space
// convention as the display list, so what is visible and what is clickable
// cannot disagree. Hover, press and focus are kept in the retained node as
// [Interaction] and are read by painters; changing them repaints and does not
// rebuild anything, which is what the project plan, section 5, requires of
// presentation state.
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
// An App, its state and its tree belong to one goroutine, the UI executor:
// the goroutine that called [New]. Touching state from anywhere else is a
// programming error. A build with the giftdebug tag rejects it with a panic; a
// release build does not check, because the check cannot be made cheap enough
// for the event handler path, and the race detector covers the same ground.
//
// Exactly two methods may be called from any goroutine. [App.Post] hands a
// closure to the UI executor, which runs it at the beginning of the next
// update; that is how a worker result reaches the interface, and [Token] is
// how a result that has since gone stale is rejected. [App.Diagnostics]
// returns a synchronised snapshot of the frame counters.
package gift
