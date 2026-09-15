package example_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/internal/example"
	"github.com/torbenschinke/gift/ui"
)

// TestLoadFontIsDeterministicWithoutTheEnvironment is the point of the whole
// rewrite: the examples get the same typeface on a developer's macOS machine,
// on a Debian box and on a bare Raspberry Pi OS image, where the old path
// search found nothing at all.
func TestLoadFontIsDeterministicWithoutTheEnvironment(t *testing.T) {
	restoreDefault(t)
	t.Setenv("GIFT_FONT", "")

	if err := example.LoadFont(); err != nil {
		t.Fatalf("LoadFont without GIFT_FONT: %v", err)
	}
	if got := ui.DefaultFont(); got != ui.MustFont(ui.FontQuery{Family: inter.Family}) {
		t.Fatal("the default font is not the embedded Inter Regular")
	}
}

// TestGiftFontOverrideIsHonoured keeps the one feature of the old loader that
// was worth keeping.
func TestGiftFontOverrideIsHonoured(t *testing.T) {
	restoreDefault(t)
	interRegular := ui.MustFont(ui.FontQuery{Family: inter.Family})
	t.Setenv("GIFT_FONT", filepath.Join("..", "text", "testdata", "Roboto-Regular.ttf"))

	if err := example.LoadFont(); err != nil {
		t.Fatalf("LoadFont with GIFT_FONT: %v", err)
	}
	if ui.DefaultFont() == interRegular {
		t.Fatal("GIFT_FONT was ignored and Inter was installed anyway")
	}
}

// TestGiftFontFailsLoudly is the deliberate difference from the old loader,
// which fell through to the next candidate path. An override that is silently
// ignored teaches the user that the setting does nothing, which is worse than
// having no setting.
func TestGiftFontFailsLoudly(t *testing.T) {
	restoreDefault(t)

	t.Run("missing file", func(t *testing.T) {
		t.Setenv("GIFT_FONT", filepath.Join(t.TempDir(), "not-here.ttf"))
		err := example.LoadFont()
		if err == nil {
			t.Fatal("a missing GIFT_FONT file was accepted")
		}
		if !strings.Contains(err.Error(), "GIFT_FONT") {
			t.Errorf("the error does not name the setting that caused it: %v", err)
		}
	})

	t.Run("not a font", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "prose.ttf")
		if err := os.WriteFile(p, []byte("not a font"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIFT_FONT", p)
		err := example.LoadFont()
		if err == nil {
			t.Fatal("a file that is not a font was accepted")
		}
		// The classification the re-exported sentinel exists for: this is a
		// broken file, not a missing one.
		if !errors.Is(err, ui.ErrBadFont) {
			t.Errorf("the error is not classifiable as a bad font: %v", err)
		}
	})
}

// restoreDefault puts the process wide default font back, so that the order
// these tests run in cannot matter.
func restoreDefault(t *testing.T) {
	t.Helper()
	saved := ui.DefaultFont()
	t.Cleanup(func() { ui.SetDefaultFont(saved) })
}
