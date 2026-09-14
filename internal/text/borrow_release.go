//go:build !giftdebug

package text

// borrowChecks enables the borrow generation check of [Paragraph]. It is a
// compile time constant, so the release build contains none of the code it
// guards; see borrow.go.
const borrowChecks = false
