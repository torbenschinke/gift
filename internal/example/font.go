// Package example holds the plumbing the three example programs share.
//
// It exists because they used to share it by copy: the same forty line
// loadFont sat in cmd/example-counter, cmd/example-effects and
// cmd/example-gallery, byte for byte. Three copies of a function is three
// places to fix a defect and three chances to fix only two of them.
//
// It is internal so that nothing outside this module can build on it. This is
// example plumbing, not API: an application that wants a font imports a font
// package and calls [ui.SetDefaultFont] itself, which is the two lines
// [LoadFont] mostly consists of.
package example

import (
	"fmt"
	"os"

	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/ui"
)

// LoadFont installs the default font of the example programs.
//
// # What it replaced
//
// A search through four operating system font paths and then a path relative
// to the working directory. It was wrong in three ways: on a minimal Raspberry
// Pi OS image not one of those paths exists; the typeface that *was* found
// differed per platform — Arial on macOS, DejaVu on Debian, Piboto on a Pi —
// so any pixel an example produced was machine dependent by construction; and
// the last entry only worked when the example was started from the repository
// root.
//
// Now the examples state which typeface they want and get that one
// everywhere. Inter comes from an opt-in package, which is why importing it
// here puts a font into these three binaries and into nobody else's.
//
// # Why GIFT_FONT survives
//
// It no longer papers over a missing font, because there is no longer one to
// miss. It is kept because seeing a layout under a different typeface is a
// real thing to want from an example program and it costs four lines. When it
// is set and unusable the example fails and says so, rather than falling back
// to Inter: an override that is silently ignored is worse than no override,
// since the user then concludes the layout is insensitive to the font.
func LoadFont() error {
	if p := os.Getenv("GIFT_FONT"); p != "" {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("GIFT_FONT: %w", err)
		}
		f, err := ui.LoadFont(data)
		if err != nil {
			return fmt.Errorf("GIFT_FONT=%s: %w", p, err)
		}
		ui.SetDefaultFont(f)
		return nil
	}
	ui.SetDefaultFont(ui.MustFont(ui.FontQuery{Family: inter.Family}))
	return nil
}
