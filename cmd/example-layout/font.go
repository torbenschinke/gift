// This file is platform plumbing, not user interface. It is separate so that
// main.go stays about views.

package main

import (
	"fmt"
	"os"

	"github.com/torbenschinke/gift/ui"
)

// loadFont installs the application wide default font. Embedding a typeface
// would put it in every binary that links gift, so this is the application's
// job — and the failure is a sentence, not a blank window.
func loadFont() error {
	paths := []string{
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/piboto/Piboto-Regular.ttf", // Raspberry Pi OS
		"C:\\Windows\\Fonts\\arial.ttf",
		"internal/text/testdata/Roboto-Regular.ttf", // run from the repository root
	}
	if p := os.Getenv("GIFT_FONT"); p != "" {
		paths = []string{p}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f, err := ui.LoadFont(data)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		ui.SetDefaultFont(f)
		return nil
	}
	return fmt.Errorf("no usable font found; set GIFT_FONT to a .ttf file")
}
