package gifttest_test

import (
	"testing"

	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// TestHarnessInstallsAndRestoresTheTheme pins [gifttest.Options.Theme].
//
// The theme is process wide, so without this a golden of a view that uses
// semantic colours depends on which theme some earlier test in the binary left
// behind. The restore half matters just as much as the install half: a harness
// that set the theme and kept it would move the failure to the next test
// instead of removing it.
func TestHarnessInstallsAndRestoresTheTheme(t *testing.T) {
	before := ui.CurrentTheme()
	if before.IsDark() {
		t.Fatal("the tests of this package expect to start on a light theme")
	}

	t.Run("installed", func(t *testing.T) {
		h := gifttest.New(t, gifttest.Options{
			View:  ui.Box().Key("plate").Frame(20, 10).Background(ui.ColorSurface),
			Size:  geom.Sz(60, 40),
			Theme: ui.DarkTheme(),
		})
		if !ui.CurrentTheme().IsDark() {
			t.Fatal("the harness did not install the theme it was given")
		}
		got, ok := h.Find(gifttest.ByKey("plate")).Background()
		if !ok {
			t.Fatal("the plate has no background")
		}
		if want := ui.DarkTheme().Color(ui.ColorSurface); got != want {
			t.Fatalf("the plate is %v, want the dark surface %v", got, want)
		}
	})

	if ui.CurrentTheme() != before {
		t.Fatal("the harness did not restore the theme when the subtest ended")
	}
}
