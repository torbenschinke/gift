package ui

import (
	"fmt"

	"github.com/torbenschinke/gift/internal/text"
)

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
// an error here.
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
// # gift ships no font
//
// There is no built-in fallback and there will not be one. Embedding a
// typeface would add somewhere between a hundred kilobytes and several
// megabytes to every binary that links gift, would bind the project to that
// typeface's licence, and would mean the first thing most applications do is
// pay for a font they then replace. internal/text uses Roboto in testdata and
// nowhere else, which is exactly the line this function keeps.
//
// So font provision is the application's job, and the diagnosis when it is not
// done is loud: see [Text].
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
			"gift ships no font binary on purpose; the application supplies one.\n" +
			"Load it once at startup and install it as the default:\n" +
			"\n" +
			"\tdata, err := os.ReadFile(\"MyFont.ttf\")   // or go:embed\n" +
			"\tf, err := ui.LoadFont(data)\n" +
			"\tui.SetDefaultFont(f)\n" +
			"\n" +
			"or name a font on the view itself with ui.Text(s).Font(f)."))
}
