package ui

import (
	"fmt"

	"github.com/worldiety/gift/internal/text"
)

// ErrBadFont is what [LoadFont] wraps when the bytes are not a font this
// toolkit can use: truncated data, an image, an empty slice, a font collection
// or a file that makes the parser itself panic. Test for it with errors.Is.
//
// It is re-exported here rather than left inside internal/text because the
// distinction it draws is one a caller acts on. "The file is not there" is a
// packaging or configuration problem and the application can look elsewhere;
// "the file is there and is not a font" is a corrupt asset and looking
// elsewhere will not help. Without a sentinel the only available test is
// err != nil, which cannot tell those apart — the rule the project plan,
// section 15, states for errors two packages must agree about.
var ErrBadFont = text.ErrBadFont

// Font is a loaded font, ready to shape and to rasterise with.
//
// It is a handle. Copy it freely; the parsed tables behind it are shared and
// immutable. A Font is bound to the UI executor, like everything else in a
// gift application.
type Font struct{ f *text.Font }

// IsZero reports whether f is the zero Font, that is no font at all.
func (f Font) IsZero() bool { return f.f == nil }

// LoadFont parses a TrueType or OpenType font.
//
// The data is not copied and must not be modified afterwards: the parser reads
// glyph outlines from it lazily, and so does the glyph rasteriser in the
// backend. A font collection (.ttc) is rejected rather than silently reduced
// to its first face.
//
// An error is a plain value, not a panic: a font file is outside world input
// under the project plan, section 15, and a corrupt one must not take the
// process down. Even a file that makes the parser itself panic comes back as
// an error here. Every such error wraps [ErrBadFont].
func LoadFont(data []byte) (Font, error) {
	f, err := text.ParseFont(data)
	if err != nil {
		return Font{}, err
	}
	return Font{f: f}, nil
}

// defaultFont is the font [Text] uses when no [TextView.Font] was set.
//
// It is package level state owned by the UI executor, like the shaping cache
// it feeds. A per App default was considered and rejected for the same reason
// the shaper is process wide: gift's layout contract hands a layouter no
// application handle, so a per App font would have to be threaded through
// every layout signature to reach the one view that reads it.
var defaultFont Font

// SetDefaultFont installs the font every [Text] uses unless it names another
// one with [TextView.Font]. Passing the zero Font removes the default.
//
// It is the one slot for the one default, and it is the *application* that
// fills it — never a library, and never a font package. A font package
// registers its faces with [RegisterFont] and leaves the choice alone, because
// two packages writing this slot from init would overwrite each other in an
// order the language does not fix. Together the two calls read:
//
//	import _ "github.com/worldiety/gift/font/inter"
//	...
//	ui.SetDefaultFont(ui.MustFont(ui.FontQuery{Family: inter.Family}))
//
// # gift links no font unless you ask for one
//
// The library packages embed nothing. A binary that imports gift and no font
// package carries no typeface, pays no kilobytes for one and is bound by no
// font licence — which is what this sentence used to protect by shipping no
// font at all.
//
// Not linking a font is not the same as not offering one. There are two
// opt-in packages, font/inter and font/ibmplexmono, each of which embeds its
// typeface and registers it from init. They cost exactly nothing to a binary
// that does not import them, and an application that wants its own typeface
// still loads it with [LoadFont] and ignores both.
//
// What has not changed is the other half: there is no built-in fallback and
// there will not be one. The register picks a face by family, weight and style;
// it never consults a second face for a glyph the chosen one lacks. See
// [RegisterFont] and the project plan, section 14.
func SetDefaultFont(f Font) { defaultFont = f }

// DefaultFont returns the font installed by [SetDefaultFont], or the zero
// Font.
func DefaultFont() Font { return defaultFont }

// resolveFont picks the font of a text view and refuses to continue without
// one.
//
// # Why this panics
//
// The alternative was measuring the text as empty and drawing nothing, and
// that produces the one failure mode a toolkit must never produce: a blank
// area with no explanation, three layers away from the mistake. There is no
// middle ground available either — gift never logs to slog.Default (the
// project plan, section 15), so a log line would vanish for every application
// that has not configured a logger, and a magenta placeholder box would still
// leave the developer guessing which of several boxes is the missing text.
//
// This is a construction time programming error in the same class as the ones
// the project plan, section 15, assigns to a panic: it happens during build,
// never in the frame path, and it is deterministic. An application either
// installed a font or it did not, and it finds out on the first frame rather
// than on the first frame that happens to contain a label.
func resolveFont(f Font) *text.Font {
	if f.f != nil {
		return f.f
	}
	if defaultFont.f != nil {
		return defaultFont.f
	}
	panic(fmt.Sprintf(
		"gift/ui: ui.Text needs a font and none is installed.\n" +
			"gift links no font unless the application asks for one.\n" +
			"The quickest way is one of the bundled typefaces:\n" +
			"\n" +
			"\timport _ \"github.com/worldiety/gift/font/inter\"\n" +
			"\t...\n" +
			"\tui.SetDefaultFont(ui.MustFont(ui.FontQuery{Family: inter.Family}))\n" +
			"\n" +
			"or load your own once at startup:\n" +
			"\n" +
			"\tdata, err := os.ReadFile(\"MyFont.ttf\")   // or go:embed\n" +
			"\tf, err := ui.LoadFont(data)\n" +
			"\tui.SetDefaultFont(f)\n" +
			"\n" +
			"or name a font on the view itself with ui.Text(s).Font(f)."))
}
