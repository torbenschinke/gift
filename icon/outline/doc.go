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
// variable is a window into it rather than a slice of its own.
//
// The method, written out because the number was wrong before and an
// application budgets against it. Two programs, identical except for one
// import: both import gift and gift/ui, both build `ui.HStack(ui.Text("hello"),
// ui.Icon(...))`, and the one that does not import this package passes
// ui.Symbol{} to ui.Icon so that the icon view, the mask cache and every other
// line in ui is in *both* binaries and the difference is this package and
// nothing else. Built with `CGO_ENABLED=0 go build`, no linker flags, Go 1.27,
// and compared with `stat`; the symbol figure is
// `go tool nm -size` summed over every symbol whose name contains this
// package's import path. Re-measured in WU-AH.
//
//   - this package contributes 92 702 bytes of symbols — the 36 398 byte blob,
//     282 Symbol values, their initialisation and their names;
//   - the whole binary grows by 135 872 bytes on darwin/arm64 and by 107 329
//     bytes on linux/arm64, which is the Raspberry Pi target of section 1;
//   - with `-ldflags="-s -w"` the growth is 99 088 bytes.
//
// The whole binary therefore grows by *more* than the symbols, not less. The
// previous version of this paragraph claimed 60 240 bytes "because the linker
// and the segment alignment recover some of that", which is both the wrong
// number and the mechanism backwards: on top of the symbols themselves the
// linker emits the symbol name table, the pclntab entries and — in a default
// build — the DWARF that describes 282 more package level variables, and then
// pads the segments. Nothing is recovered. The difference between the default
// and the stripped build above is exactly that debug information, and it is
// the reason the whole binary figure is quoted per platform and per build mode
// while the symbol figure is quoted once: the symbols are a property of this
// package, the binary growth is a property of a build.
//
// A binary that imports gift/ui and never names an icon keeps four bytes of
// this machinery — the registered type id of the icon view — and the linker
// eliminates the rest. That is the whole reason this is a package of its own
// and not a table inside ui; gift/font/inter makes the same argument about a
// typeface.
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
