package stress

import (
	_ "embed"
	"fmt"

	"github.com/worldiety/gift/ui"
)

// robotoTTF is the typeface the stress scene draws its text with.
//
// # Why a font is embedded here of all places
//
// Because the scene is a measurement fixture and a measurement has to be
// reproducible. The scene's own documentation promises that the same
// parameters produce the same tree, the same display list and the same
// numbers on every run; a font supplied by a flag, by an environment variable
// or by whatever the host happens to have installed would break that promise
// in the one place it matters most — the shaped advance widths, which decide
// the layout, the glyph count and the atlas occupancy.
//
// It is also why this does not contradict [ui.SetDefaultFont], which says gift
// links no font unless the application asks for one. It still does not: gift's
// own packages embed nothing, the typefaces of font/inter and font/ibmplexmono
// are paid for only by a binary that imports them, and the only binary that
// carries these 168 kilobytes is cmd/gift-stress, which exists to be measured
// and not to be shipped. The package is internal precisely so that nothing
// outside this module can link it by accident.
//
// It stays Roboto rather than moving to font/inter for the reason the fixture
// exists at all: the numbers of an old measurement and a new one have to be
// comparable, and changing the typeface would change every advance width, the
// glyph count and the atlas occupancy in one step.
//
// It is the same Roboto internal/text uses in its own testdata, under the same
// Apache 2.0 licence, copied rather than reached across a package boundary
// because go:embed cannot see out of its own directory and a public accessor
// in internal/text would put the font into every binary that links gift.
//
//go:embed testdata/Roboto-Regular.ttf
var robotoTTF []byte

// Font parses and returns the embedded typeface.
//
// It panics on failure, which is correct for a fixture: the file is embedded
// in the binary, so a failure here is a broken build and not a condition a
// measurement harness can sensibly carry on without. See the project plan,
// section 15, on the difference between a programming error and outside world
// input — an embedded asset is the former.
func Font() ui.Font {
	f, err := ui.LoadFont(robotoTTF)
	if err != nil {
		panic(fmt.Sprintf("gift/internal/stress: parsing the embedded Roboto: %v", err))
	}
	return f
}
