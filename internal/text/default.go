package text

// defaultShaper is the process wide shaper.
//
// # Why a package level shaper exists at all
//
// Two packages need the same shaping cache and neither may import the other.
// ui measures text during layout, and the project plan, section 3, routes that
// through this package; backend/ebiten needs the cache counters for the
// metrics report and needs somewhere to call [Shaper.Tick] from once per
// frame, and the same section forbids it from importing ui. A shaper owned by
// one of them and reached through the other would be exactly the kind of
// back edge the package table rules out.
//
// The alternative, a shaper per [gift.App], was rejected for a smaller reason:
// gift's layout contract hands a layouter a *LayoutContext and no application
// handle, so a per App shaper would have to be threaded through every layout
// signature to reach the one view that wants it.
//
// # Ownership
//
// It belongs to the UI executor, like every Shaper; see the package
// documentation. gift supports one UI executor per process for text, and two
// Apps measuring text on two goroutines is a data race that -race will report.
// That is a real limitation and it is stated rather than guarded, because a
// mutex on the measurement path would buy nothing for the single window
// applications the project plan, section 1, targets.
var defaultShaper *Shaper

// Default returns the process wide [Shaper].
//
// It is created on first use with the default [Config] and lives for the rest
// of the process. Callers that want their own cache budget construct a Shaper
// with [NewShaper] instead.
func Default() *Shaper {
	if defaultShaper == nil {
		defaultShaper = NewShaper(Config{})
	}
	return defaultShaper
}
