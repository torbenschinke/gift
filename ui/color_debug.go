//go:build giftdebug

package ui

import "fmt"

// assertResolved rejects a semantic colour that reached a painter.
//
// # Why this exists next to the identical check in render
//
// [render.List.Add] already refuses an operation whose colour has a negative
// channel, and that check is the backstop for anybody outside this package who
// writes their own painter. It cannot be the check for the painters *in* this
// package, and the reason is the alpha of the encoding: a semantic colour is
// Color{R: -1, G: role, B: fade}, so its alpha is zero until [ResolveColor]
// has run. Every emission site here is gated on a transparency test — a
// background that is transparent is not filled, a border that is invisible is
// not stroked, a glyph run in a transparent colour is not drawn — so an
// unresolved colour is *dropped* before it ever reaches List.Add. The op never
// exists, so the backstop never sees it, and the symptom is a laid out,
// measured, hit testable widget that draws nothing at all.
//
// That was not a hypothesis. A gallery given a TileStyle.Palette of semantic
// colours laid out nine tiles and emitted zero operations, under the giftdebug
// tag, without a word.
//
// So the check has to sit *before* the gate that decides whether to emit,
// which is what every call site of this function does. The two checks are not
// redundant: this one catches the drop, List.Add catches the corruption.
//
// It is a debug build check for the reason the project plan, section 15, gives
// for the others: the call sites are the frame path and the release build must
// not pay for them. Without the tag the whole function compiles away; see
// color_release.go and TestTheAssertionCompilesAwayWithoutTheTag.
func assertResolved(c Color, what string) {
	if !IsSemantic(c) {
		return
	}
	name := "<malformed>"
	if r := roleOf(c); r < numColorRoles {
		name = roleNames[r]
	}
	panic(fmt.Sprintf("gift/ui: %s is the unresolved semantic colour %s (%v). "+
		"A semantic colour has an alpha of zero until ui.ResolveColor has run on it, so this "+
		"would not have drawn a wrong colour — it would have drawn nothing at all. "+
		"Resolve it during Build, where every other colour in this package is resolved",
		what, name, c))
}

// assertResolvedBorder and assertResolvedShadow are the same check for the two
// style structs that carry a colour. They exist so that a call site reads as
// one line next to the visibility test it guards.
func assertResolvedBorder(b Border, what string) { assertResolved(b.Color, what) }

func assertResolvedShadow(s Shadow, what string) { assertResolved(s.Color, what) }
