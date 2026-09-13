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
	// PaintedNodes counts the nodes whose painter ran.
	PaintedNodes uint64
	// PaintedOps counts the drawing operations emitted.
	PaintedOps uint64
	// LiveNodes is the number of nodes currently in the retained tree.
	LiveNodes uint64
	// LiveScopes is the number of mounted component instances.
	LiveScopes uint64
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
