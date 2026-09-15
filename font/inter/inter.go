// Package inter embeds the Inter typeface and registers it with gift.
//
// Import it for its side effect and then make it the default, or ask for one
// of its faces by name:
//
//	import (
//		"github.com/torbenschinke/gift/ui"
//		"github.com/torbenschinke/gift/font/inter"
//	)
//
//	ui.SetDefaultFont(ui.MustFont(ui.FontQuery{Family: inter.Family}))
//	bold := ui.MustFont(ui.FontQuery{Family: inter.Family, Weight: ui.WeightBold})
//
// The package does *not* install itself as the default font. Choosing the
// default is the application's decision, and a package that made it from init
// would be the very clobbering [ui.RegisterFont] exists to prevent.
//
// # What it costs
//
// Importing this package adds the two embedded files, about 832 KiB, to the
// binary, and parses both of them during init. A binary that does not import
// it pays nothing at all: gift's own packages embed no typeface, which is the
// entire reason this is a separate package rather than a default inside ui.
//
// # Why these two weights
//
// Regular and Bold, upright only. Inter's static instances are a little over
// 400 KiB each because each one carries the full Latin, Greek and Cyrillic
// coverage plus hinting, so every additional face is a real cost paid by every
// application that imports this package for the one face it actually uses.
// Regular and Bold are the pair an interface cannot do without: body text and
// emphasis. Medium and SemiBold are a refinement, and [ui.ResolveFont]
// approximates them from the nearest registered weight rather than refusing.
//
// Italics are not embedded. They would double the size for a style that a
// control-heavy interface uses rarely, and gift has no italic text API to
// reach them with yet. If you need them, load Inter-Italic.ttf yourself and
// register it under this same family with [ui.RegisterFont]; that is what the
// register is for.
//
// # Why the static instances and not the variable font
//
// InterVariable.ttf is 880 KiB and covers every weight from 100 to 900, which
// next to 832 KiB for two static faces looks like the obvious choice. It was
// measured rather than assumed, and it is not.
//
// Instancing does work: setting the wght axis through the parser of
// github.com/go-text/typesetting v0.3.5 changes both the advances and the
// outlines. Inter Variable at wght 700 gives the letter H a right edge at
// 1394.92 font units where the static Inter Bold has it at 1395, and the
// advance of a test string differs by about one part in 2048 — the rounding of
// the static instances to integer units, and nothing else.
//
// What it costs is worse than what it saves. The variation coordinates live on
// the parsed face, not on the bytes, so each instance needs its own parse of
// the whole 880 KiB file: two weights means two parses and two sets of parsed
// tables, and the file size advantage only appears from three or four weights
// upwards. Worse, it would mean mutating a [ui.Font] after it was constructed,
// and internal/text documents a parsed font as immutable and shared precisely
// because the backend's glyph rasteriser reads it lazily, on a later frame,
// from another call stack. Setting an axis at the wrong moment would not fail
// — it would quietly restyle glyphs that are already in the atlas.
//
// So: static instances, and the variable font stays out until something asks
// for a range of weights rather than two of them.
//
// # Provenance and licence
//
// Inter is by Rasmus Andersson. The embedded files are Inter-Regular.ttf and
// Inter-Bold.ttf from extras/ttf of the upstream release v4.1
// (https://github.com/rsms/inter/releases/tag/v4.1), copied unmodified on
// 2026-09-15. They are licensed under the SIL Open Font License 1.1; the full
// text is in LICENSE.txt next to them, and it travels with any binary that
// links this package.
package inter

import (
	_ "embed"
	"fmt"

	"github.com/torbenschinke/gift/ui"
)

// Family is the family name these faces are registered under. Use the constant
// rather than the literal so that a typo is a compile error and not an
// unexplained panic from [ui.MustFont].
const Family = "Inter"

// The embedded files. go:embed cannot see out of its own directory, so the
// typeface is copied into this package rather than referenced from elsewhere
// in the tree; internal/stress/font.go records the same constraint.
//
//go:embed Inter-Regular.ttf
var regularTTF []byte

//go:embed Inter-Bold.ttf
var boldTTF []byte

func init() {
	ui.RegisterFont(Family, ui.WeightRegular, ui.StyleNormal, mustLoad("Inter-Regular.ttf", regularTTF))
	ui.RegisterFont(Family, ui.WeightBold, ui.StyleNormal, mustLoad("Inter-Bold.ttf", boldTTF))
}

// mustLoad parses an embedded face and panics if it will not parse.
//
// A panic is right here and would be wrong in [ui.LoadFont]. The bytes are
// inside the binary: there is no user, no disk and no network between the
// build and this line, so a failure is a broken build, which the project plan,
// section 15, puts firmly on the programming error side. Returning an error
// would only move the same certain failure to a place where an application
// might ignore it and then draw nothing.
func mustLoad(name string, data []byte) ui.Font {
	f, err := ui.LoadFont(data)
	if err != nil {
		panic(fmt.Sprintf("gift/font/inter: parsing the embedded %s: %v", name, err))
	}
	return f
}
