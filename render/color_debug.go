//go:build giftdebug

package render

import "fmt"

// assertResolvedColor rejects a colour that cannot exist.
//
// A premultiplied colour has every channel in [0, 1], so a negative channel is
// not a dark colour, it is a value that was never a colour. gift/ui uses
// exactly that hole to carry a semantic colour — a role rather than a picture
// — until it is resolved against the installed theme, so the one realistic way
// to reach this panic is a widget that wrote a named colour into an operation
// without calling ui.ResolveColor.
//
// It is a debug build check for the reason the project plan, section 15,
// gives for the others: it sits in [List.Add], which is the frame path, and
// the release build must not pay for it. Without the tag the call compiles
// away to nothing.
func assertResolvedColor(c Color, what string) {
	if c.R >= 0 && c.G >= 0 && c.B >= 0 && c.A >= 0 {
		return
	}
	panic(fmt.Sprintf("render: %s has a negative channel (%v); a premultiplied colour cannot. "+
		"A gift/ui semantic colour such as ui.ColorLabel looks like this until ui.ResolveColor "+
		"has been called on it; a widget that emits its own operations has to do that itself", what, c))
}
