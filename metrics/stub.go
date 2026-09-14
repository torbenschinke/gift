//go:build !giftmetrics

package metrics

import "time"

// This file is the whole package in a build without the giftmetrics tag.
//
// Every function in it is a leaf with an empty or constant body, so the
// inliner removes all of them. There is no package level state, no init, no
// environment access and no allocation: [Start] returns a nil pointer and the
// methods never dereference their receiver. A program that calls
//
//	rec := metrics.Start(o)
//	...
//	rec.RecordDraw(d)
//
// therefore contains a nil constant and nothing else after inlining, and a
// program that writes the guarded form
//
//	if metrics.Enabled() {
//		rec.RecordDraw(time.Since(start))
//	}
//
// does not even call time.Now, because the condition is the constant false
// and the whole branch is dead code. That guarded form is what the backend
// uses, and it is the reason the tag costs the shipped binary nothing at all.

// Enabled reports whether measurement is compiled in and switched on. In this
// build it is the constant false.
func Enabled() bool { return false }

// FrameTimer is the stub frame timer. It holds nothing and records nothing.
type FrameTimer struct{}

// NewFrameTimerWith returns nil.
func NewFrameTimerWith(FrameTimerOptions) *FrameTimer { return nil }

// NewFrameTimer returns nil.
func NewFrameTimer(time.Duration, int) *FrameTimer { return nil }

// RecordUpdate does nothing.
func (*FrameTimer) RecordUpdate(time.Duration) {}

// RecordDraw does nothing.
func (*FrameTimer) RecordDraw(time.Duration) {}

// RecordInterval does nothing.
func (*FrameTimer) RecordInterval(time.Duration) {}

// Snapshot returns the zero value.
func (*FrameTimer) Snapshot() FrameTimes { return FrameTimes{} }

// Recorder is the stub measurement session. It holds nothing.
type Recorder struct{}

// Start returns nil: there is nothing to measure with.
func Start(Options) *Recorder { return nil }

// Timer returns nil.
func (*Recorder) Timer() *FrameTimer { return nil }

// RecordUpdate does nothing.
func (*Recorder) RecordUpdate(time.Duration) {}

// RecordDraw does nothing.
func (*Recorder) RecordDraw(time.Duration) {}

// RecordInterval does nothing.
func (*Recorder) RecordInterval(time.Duration) {}

// Snapshot returns the zero value.
func (*Recorder) Snapshot() FrameTimes { return FrameTimes{} }

// Tick does nothing.
func (*Recorder) Tick() {}

// Close does nothing and returns nil.
func (*Recorder) Close() error { return nil }
