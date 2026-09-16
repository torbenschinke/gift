//go:build !giftdebug

package gift

// checkTrapCount does nothing in a release build. Build with -tags giftdebug
// to have an imbalanced [Element.FocusTrap] count diagnosed; see the giftdebug
// version for what the two halves of the count are.
func checkTrapCount(*App) {}
