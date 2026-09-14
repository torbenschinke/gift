package gift

import "sync"

// Diagnostics is a snapshot of the frame path counters.
//
// The frame path writes nothing but these plain numbers: no formatting, no
// interface boxing, no locks. Everything that would be a log line in a less
// performance sensitive toolkit is a counter here. The snapshot is taken out
// of band, by the application or by a low rate diagnostic sink; see the
// project plan, section 15.
type Diagnostics struct {
	// Frames counts the calls to App.Paint.
	Frames uint64
	// Builds counts the component scopes that were rebuilt.
	Builds uint64
	// Layouts counts the nodes whose layouter actually ran. A node that was
	// skipped because it is clean and its constraints did not change is not
	// counted, which is what makes partial relayout observable from a test.
	Layouts uint64
	// PaintedNodes counts the nodes whose painter actually ran.
	//
	// A node without background, border and clip supplies no painter at all
	// and gift descends straight into its children; such a node is visited
	// but not counted. That difference is the whole point of the counter: it
	// is the only externally visible evidence that the nil painter fast path
	// is still in place.
	PaintedNodes uint64
	// PaintedOps counts the drawing operations emitted.
	PaintedOps uint64
	// LiveNodes is the number of nodes currently in the retained tree.
	LiveNodes uint64
	// LiveScopes is the number of mounted component instances.
	LiveScopes uint64

	// OverflowNodes is the number of nodes in the retained tree whose
	// content does not fit into the size they reported.
	//
	// It answers "is any container lying about its size" with a single
	// integer, which is the question the project plan, section 7,
	// "Overflow-Modell", makes a first class one. Zero is the expected
	// value of a healthy scene, so a test can assert on it directly.
	//
	// It describes the tree, not the last pass: a node that overflowed and
	// was then skipped by the layout cache still counts. A per pass tally
	// would drop to zero as soon as partial relayout started working, which
	// is precisely when it would be needed.
	OverflowNodes uint64

	// HitTests counts the hit tests performed, that is roughly the number of
	// pointer events that had to find a target.
	HitTests uint64
	// InputEvents counts the calls into an [Interactor], including the ones
	// that happened while an event bubbled towards the root.
	InputEvents uint64
	// Scrolls counts the times a scroll container's offset actually
	// changed: a wheel notch, a drag step, a fling tick or a programmatic
	// jump. It moves while Builds and Layouts do not, which is the whole
	// claim of the scroll fast path stated as two numbers.
	Scrolls uint64
	// DiscardedTouches counts the additional fingers that were recognised
	// and dropped. The project plan, section 7, limits multitouch to exactly
	// that, and a counter is how "it was dropped on purpose" is told apart
	// from "it was never seen".
	DiscardedTouches uint64

	// OverflowExtent is the sum over those nodes of their horizontal plus
	// vertical overflow, in logical pixels.
	//
	// The count alone cannot distinguish a row that is half a pixel too tall
	// from one that is eight hundred pixels too tall. The extent is what
	// makes "it got better" and "it got worse" different numbers, which is
	// what a remediation needs; the count is what a regression test asserts
	// on. Both are cheap, so both are here.
	OverflowExtent float32
}

// diagPublisher makes the counters readable from another goroutine.
//
// The hot path never touches this. It increments the plain fields of
// App.diag, which belong to the UI executor and are not synchronised at all.
// Once per Update and once per Paint the UI executor copies that struct in
// here, and [App.Diagnostics] copies it back out.
//
// # Why a mutex and not an atomic pointer to a double buffer
//
// Publishing into one of two preallocated structs and swapping an
// atomic.Pointer looks attractive because it is lock free, but it is not
// race free: the atomic store orders the writer before a reader that loads
// the pointer, and nothing orders that reader before the writer that
// overwrites the same buffer two frames later. A reader in a loop and a
// writer at 60 Hz do overlap there, and the race detector is right to say so.
// Adding buffers only makes the window smaller, never zero.
//
// An uncontended mutex costs a couple of tens of nanoseconds twice per frame,
// outside the counter increments, and allocates nothing. That is the honest
// price of a snapshot that another goroutine may read; see the project plan,
// section 15, which designates this snapshot as the out of band measurement
// source and section 13, which mandates race tests.
type diagPublisher struct {
	mu   sync.Mutex
	snap Diagnostics
}

// publish copies the UI executor's counters into the shared snapshot. It
// allocates nothing: the destination is a plain struct field.
func (a *App) publishDiagnostics() {
	a.diag.LiveNodes = uint64(a.store.Len())
	a.diag.LiveScopes = a.liveScopes
	a.pub.mu.Lock()
	a.pub.snap = a.diag
	a.pub.mu.Unlock()
}

// Diagnostics returns a snapshot of the counters as of the end of the last
// Update or Paint.
//
// It is safe to call from any goroutine, which is the whole point: it is the
// out of band measurement source of the project plan, section 15, and a
// measurement harness that has to run on the UI executor is not out of band.
// It never blocks the frame path for longer than a struct copy.
func (a *App) Diagnostics() Diagnostics {
	a.pub.mu.Lock()
	d := a.pub.snap
	a.pub.mu.Unlock()
	return d
}
