//go:build !giftdebug

package ui

// The release build of the checks in color_debug.go.
//
// They are empty, the compiler inlines them to nothing, and the arguments are
// unnamed so that nothing in the frame path is even evaluated for them. The
// zero allocation contract of the project plan, section 11, holds in every tag
// combination, and this file is how it holds in the one that matters.
func assertResolved(Color, string) {}

func assertResolvedBorder(Border, string) {}

func assertResolvedShadow(Shadow, string) {}
