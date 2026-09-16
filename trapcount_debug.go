//go:build giftdebug

package gift

import "fmt"

// checkTrapCount asserts that [App.traps] is still a count of something.
//
// The counter is maintained in two places that have to agree for ever:
// [App.applyFocusTrap], which increments when a node starts declaring
// [Element.FocusTrap] and decrements when it stops, and [App.destroyScopes],
// which decrements for a node that is unmounted while still declaring it. A
// missing decrement leaves the count permanently above zero, and
// [App.focusRoot] then walks the whole tree on every focus change looking for
// a trap that is not there — slow, and silent. A missing *increment*, or a
// double decrement, drives it negative, and then a modal that is genuinely
// open is not a trap at all and tab walks out of an alert.
//
// The reviewer of gate 13 could not construct an imbalance through the public
// API, and that is the reason this is an assertion under a build tag rather
// than a clamp: there is no known way in, so a silent repair would hide the
// day somebody makes one.
func checkTrapCount(a *App) {
	if a.traps < 0 {
		panic(fmt.Sprintf(
			"gift: focus trap count went to %d. Element.FocusTrap is counted up in "+
				"App.applyFocusTrap and down in both App.applyFocusTrap and "+
				"App.destroyScopes; one of them has run without its partner",
			a.traps))
	}
}
