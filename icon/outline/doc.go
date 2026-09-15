// Package outline embeds the Flowbite outline icon set as [ui.Symbol] values.
//
// Import it and name an icon:
//
//	import (
//		"github.com/torbenschinke/gift/ui"
//		"github.com/torbenschinke/gift/icon/outline"
//	)
//
//	ui.Icon(outline.User)
//	ui.Icon(outline.AddressBook).Size(16).Foreground(ui.ColorAccent)
//
// If you prefer the spelling of the tree these icons were surveyed in, alias
// the import — the identifiers are the same:
//
//	import icons "github.com/torbenschinke/gift/icon/outline"
//
//	ui.Icon(icons.User)
//
// There is no side effect and nothing to register. Unlike gift/font/*, which
// has to put a typeface into a process wide register because text names its
// font indirectly, an icon is named directly by the variable, so this package
// has no init that does anything and cannot clash with a second icon package.
// That is why [solid] and this one can be imported together.
//
// # What it costs
//
// The geometry of all 282 icons is one embedded blob of 36 398 bytes, and each
// variable is a window into it rather than a slice of its own. Measured on
// darwin/arm64 with Go 1.27, against a binary that imports gift/ui and draws a
// [ui.Text]:
//
//   - this package contributes 92 702 bytes of symbols — the 36 398 byte blob,
//     282 Symbol values, their initialisation and their names;
//   - the whole binary grows by 60 240 bytes, because the linker and the
//     segment alignment recover some of that.
//
// A binary that does not import it pays 624 bytes, which is what the icon view
// and the mask cache cost inside ui when nothing ever calls them. That is the
// whole reason this is a package of its own and not a table inside ui;
// gift/font/inter makes the same argument about a typeface.
//
// Nothing is rasterised at init. A [ui.Symbol] is pre-parsed outlines, and the
// coverage mask is produced on the first frame that draws it at a given size
// and device density, then cached; see [ui.IconView].
//
// # Outline or solid
//
// This set is drawn as strokes, two units wide on a 24 unit grid, with round
// caps and joins. It reads as lighter and is the usual choice for toolbars and
// list rows. [github.com/torbenschinke/gift/icon/solid] is the filled
// counterpart and is the usual choice for a selected state or a small size,
// where a two unit stroke starts to close up. The two sets do not cover the
// same names: this one has 282 icons and solid has 239.
//
// # Provenance and licence
//
// Flowbite Icons by Themesberg, MIT licensed; the full text is in LICENSE.txt
// next to this file and it travels with any binary that links this package.
//
// The SVGs were copied on 2026-09-15 from presentation/icons/flowbite/outline
// of the surveyed Nago tree (go.wdy.de/nago, commit f4a105b5), which is itself
// a copy of the upstream set. They are in the repository under
// internal/iconsvg/testdata/flowbite/outline, which is where the generator
// reads them and where the corpus test reads them; testdata is never compiled
// and never embedded, so the 2 MB of SVG is a repository cost and not a binary
// one.
//
// icons.bin and icons.gen.go are generated. Regenerate them with
//
//	go generate ./icon/...
//
//go:generate go run ../../cmd/gift-icongen -src ../../internal/iconsvg/testdata/flowbite/outline -out . -pkg outline -set "Flowbite outline"
package outline
