//go:build !giftdebug

package render

// assertResolvedColor is the release build of the check in color_debug.go: it
// is empty, it inlines away, and the frame path costs nothing for it.
func assertResolvedColor(Color, string) {}
