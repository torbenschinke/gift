package gift

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
	// Layouts counts the nodes that were measured.
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

// Diagnostics returns a snapshot of the counters.
//
// Call it outside the frame path, for example once per second. It is not
// synchronised: calling it from another goroutine while the UI executor runs
// races with the counter updates.
func (a *App) Diagnostics() Diagnostics {
	d := a.diag
	d.LiveNodes = uint64(a.store.Len())
	d.LiveScopes = a.liveScopes
	return d
}
