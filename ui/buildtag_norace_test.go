//go:build !race

package ui_test

// raceEnabled says whether this binary was built with the race detector; see
// the counterpart in buildtag_race_test.go.
const raceEnabled = false
