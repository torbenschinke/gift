// Package solid embeds the Flowbite solid icon set as [ui.Symbol] values.
//
// Import it and name an icon:
//
//	import (
//		"github.com/torbenschinke/gift/ui"
//		"github.com/torbenschinke/gift/icon/solid"
//	)
//
//	ui.Icon(solid.User)
//	ui.Icon(solid.BadgeCheck).Foreground(ui.ColorAccent)
//
// If you prefer the spelling of the tree these icons were surveyed in, alias
// the import — the identifiers are the same:
//
//	import icons "github.com/torbenschinke/gift/icon/solid"
//
//	ui.Icon(icons.User)
//
// There is no side effect and nothing to register; see
// [github.com/torbenschinke/gift/icon/outline], which explains why that
// differs from gift/font/*.
//
// # What it costs
//
// The geometry of all 239 icons is one embedded blob of 56 607 bytes, and each
// variable is a window into it. Measured the same way as
// [github.com/torbenschinke/gift/icon/outline]: 104 159 bytes of symbols and
// 57 216 bytes of whole binary growth. Importing both sets is additive at the
// symbol level — 196 861 bytes — which is the measurement that decided they
// stay two packages: there is no shared table to amortise, so a binary that
// wants one style pays for one style.
//
// # Two knockouts and one stroke, stated because they are surprising
//
// The set is not quite uniformly "filled shapes in currentColor".
// solid/circle-plus.svg is filled *and* stroked, and is emitted as two figures
// in that order. solid/visa.svg draws a card in currentColor and the lettering
// in a literal #ffffff on top; a gift icon is monochrome, so the lettering is
// emitted as a knockout — see internal/icon.PaintErase — and the icon renders
// as a card with the letters punched out of it in whatever colour it is
// tinted. That is what the artwork means; it is not what the artwork says.
//
// 211 of its paths declare fill-rule="evenodd", and 45 of those genuinely
// paint a different area under the nonzero rule that
// golang.org/x/image/vector offers. Their contours were reoriented offline so
// that the two rules agree, and the generator verifies each one by sampling
// before it emits it. The other 166 were already consistent and were left
// alone. The affected icons say so in their own doc comments.
//
// # Provenance and licence
//
// Flowbite Icons by Themesberg, MIT licensed; the full text is in LICENSE.txt
// next to this file and it travels with any binary that links this package.
//
// The SVGs were copied on 2026-09-15 from presentation/icons/flowbite/solid of
// the surveyed Nago tree (go.wdy.de/nago, commit f4a105b5), which is itself a
// copy of the upstream set. They are in the repository under
// internal/iconsvg/testdata/flowbite/solid, which is where the generator and
// the corpus test read them; testdata is never compiled and never embedded.
//
// icons.bin and icons.gen.go are generated. Regenerate them with
//
//	go generate ./icon/...
//
//go:generate go run ../../cmd/gift-icongen -src ../../internal/iconsvg/testdata/flowbite/solid -out . -pkg solid -set "Flowbite solid"
package solid
