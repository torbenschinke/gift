// Package ibmplexmono embeds IBM Plex Mono and registers it with gift.
//
// Import it for its side effect and ask for a face by name:
//
//	import (
//		"github.com/torbenschinke/gift/ui"
//		"github.com/torbenschinke/gift/font/ibmplexmono"
//	)
//
//	code := ui.MustFont(ui.FontQuery{Family: ibmplexmono.Family})
//	ui.Text(src).Font(code)
//
// Like every font package it registers and nothing else; installing a default
// font is the application's call. See [ui.RegisterFont].
//
// # What it costs
//
// The four embedded files are about 256 KiB together, and all four are parsed
// during init. That is a third of what two weights of a proportional text
// family cost, because IBM Plex Mono's web fonts carry Latin coverage rather
// than Latin, Greek and Cyrillic, and because WOFF compresses the tables.
//
// # Why four faces here and two in font/inter
//
// Because they are cheap enough here to be worth having. A monospaced family
// is used for code, logs and identifiers, where an italic comment and a bold
// keyword are the normal thing rather than a refinement, and 64 KiB per face
// does not force the choice that 420 KiB per face forces in font/inter.
//
// # These are WOFF, and that is deliberate
//
// The files are WOFF version 1, not TrueType. The parser of
// github.com/go-text/typesetting v0.3.5 accepts them: font.ParseTTF delegates
// to the loader in font/opentype/reader.go, whose signature switch handles the
// wOFF tag alongside the plain OpenType ones, despite what the name ParseTTF
// suggests. That was verified by parsing these very files and shaping with the
// result, and the package's own test pins it, because the day that switch
// changes this package must fail loudly rather than at a customer's first
// label.
//
// WOFF2 is a different matter and is not supported: the same switch does not
// know the wOF2 tag, and adding it would mean Brotli plus the glyf
// retransformation. The project plan, section 17, decides against that.
//
// # Provenance and licence
//
// IBM Plex Mono is by IBM, designed by Mike Abbink and Bold Monday. The four
// embedded files are IBMPlexMono-Regular, -Bold, -Italic and -BoldItalic in
// WOFF form, taken unmodified on 2026-09-15 from the vendored web font set of
// the Nago project (web/vuejs/src/assets/fonts/ibm-plex-mono/complete/woff),
// which is the upstream IBM Plex release as published for the web. They are
// licensed under the SIL Open Font License 1.1; the full text is in LICENSE.txt
// next to them and travels with any binary that links this package.
package ibmplexmono

import (
	_ "embed"
	"fmt"

	"github.com/torbenschinke/gift/ui"
)

// Family is the family name these faces are registered under.
const Family = "IBM Plex Mono"

//go:embed IBMPlexMono-Regular.woff
var regularWOFF []byte

//go:embed IBMPlexMono-Bold.woff
var boldWOFF []byte

//go:embed IBMPlexMono-Italic.woff
var italicWOFF []byte

//go:embed IBMPlexMono-BoldItalic.woff
var boldItalicWOFF []byte

func init() {
	ui.RegisterFont(Family, ui.WeightRegular, ui.StyleNormal, mustLoad("IBMPlexMono-Regular.woff", regularWOFF))
	ui.RegisterFont(Family, ui.WeightBold, ui.StyleNormal, mustLoad("IBMPlexMono-Bold.woff", boldWOFF))
	ui.RegisterFont(Family, ui.WeightRegular, ui.StyleItalic, mustLoad("IBMPlexMono-Italic.woff", italicWOFF))
	ui.RegisterFont(Family, ui.WeightBold, ui.StyleItalic, mustLoad("IBMPlexMono-BoldItalic.woff", boldItalicWOFF))
}

// mustLoad parses an embedded face. It panics for the reason font/inter's
// mustLoad gives: the bytes are in the binary, so a failure is a broken build.
func mustLoad(name string, data []byte) ui.Font {
	f, err := ui.LoadFont(data)
	if err != nil {
		panic(fmt.Sprintf("gift/font/ibmplexmono: parsing the embedded %s: %v", name, err))
	}
	return f
}
