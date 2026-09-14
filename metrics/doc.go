// Package metrics is the measurement half of gift: frame timings, the
// counters of the frame path and the machine readable report the project
// plan, section 13, asks for.
//
// It exists so that an application does not have to write a measurement
// harness. A gift program gets a measurement by being rebuilt with a build
// tag and by being started with an environment variable. It writes no code
// for it, registers no flags and links nothing extra when the tag is absent.
//
// # The build tag
//
// The package has two implementations behind [build constraints], with the
// same exported API:
//
//	go build ./...                    // the stub: Enabled reports false
//	go build -tags giftmetrics ./...  // the real thing
//
// Without the tag [Enabled] is a function whose body is the constant false,
// every Record method has an empty body and every constructor returns nil.
// The compiler inlines all of them, the branches guarded by Enabled fold
// away, and nothing is allocated or retained. That is the entire point of
// the tag: measurement is not something a shipped binary should carry.
//
// The measured build is a different binary and therefore a different
// measurement. Timing three callbacks per frame costs a few tens of
// nanoseconds each and the snapshot path sorts; neither is free. A number
// produced by the measured build describes the measured build.
//
// # Configuration
//
// A library must not register command line flags: flag.Int in a package
// init is a name taken away from every program that imports it, and a
// -measure-interval on a photo viewer is nonsense. The configuration is
// therefore read from the environment, once, at package initialisation of
// the measured build:
//
//	GIFT_METRICS           "1", "true", "yes" or "on" enables reporting.
//	                       Anything else, including unset, disables it.
//	GIFT_METRICS_INTERVAL  A [time.ParseDuration] string, for example "5s".
//	                       A report is then also emitted this often, not only
//	                       on exit. Empty or zero means only on exit.
//	GIFT_METRICS_OUT       A file path to write the reports to. The file is
//	                       created or truncated. Empty means standard output.
//
// A variable that is unset or malformed never panics and never keeps the
// program from running. A malformed value is reported once on standard
// error and then ignored, because a measurement that silently used a
// different setting than the one asked for is worse than no measurement.
//
// # What is measured and what is not
//
// Three timings are kept apart and are never added up, as the project plan,
// sections 11 and 13, require: the CPU time of the update callback, the CPU
// time of the draw callback and the wall clock interval between two draw
// callbacks. None of them is a GPU time. Issuing a draw call returns when
// the command has been queued, not when it has been executed and not when
// the frame has been presented, and this package has no way to observe
// either.
//
// # Dependencies
//
// This package may be imported by a backend; it imports no backend. The
// renderer side numbers are the plain [RendererStats] struct, which the
// backend fills in. The module root, gift, does not import this package at
// all.
//
// [build constraints]: https://pkg.go.dev/cmd/go#hdr-Build_constraints
package metrics
