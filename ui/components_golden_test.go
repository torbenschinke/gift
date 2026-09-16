package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/internal/example/components"
	"github.com/torbenschinke/gift/ui"
)

// TestEveryComponentLooksTheWayItLooks is the pixel half of step 9a and 9c,
// and it exists because the structural half was not enough.
//
// Every component of those two steps had tests: the rows of a list are in the
// right places, a toggle emits a knob, a segmented control moves its
// indicator. All of them passed while a nested list painted its separators
// three hundred pixels above its own rows, and while a slider in the demo drew
// no knob at all. Neither defect is expressible as a statement about the shape
// of a tree — the first is about the *origin* an operation is emitted at and
// the second about an operation that is not emitted — and both are obvious in
// a picture.
//
// # What is compared
//
// One scene per component from [components], in both themes, rendered through
// the real backend renderer. The scenes are in a package and not in this file
// on purpose: cmd/gift-shot renders exactly these, so a person can look at the
// same picture this test compares without writing a test first.
//
// Both themes and not one, because half the defects a theme can have are
// invisible in the theme the defaults were designed against: a hairline that
// resolves to the wrong role is nearly invisible on white and obvious on
// slate, and [ColorOnAccent] exists precisely because one of black and white
// is illegible on the accent and which one it is differs between the two.
func TestEveryComponentLooksTheWayItLooks(t *testing.T) {
	for _, theme := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		for _, name := range components.Names() {
			sc, ok := components.Get(name)
			if !ok {
				t.Fatalf("components.Names lists %q and components.Get does not have it", name)
			}
			// One flat loop and no t.Run: a gift.App belongs to the goroutine
			// that created it and a subtest runs on one of its own.
			h := gifttest.New(t, gifttest.Options{
				View:  sc.View,
				Size:  sc.Size,
				Theme: theme.t,
				Font:  ui.MustFont(ui.FontQuery{Family: inter.Family}),
			})
			// A scene that overflows is a scene composed at the wrong size,
			// and a golden of one would pin the mistake. This is the assertion
			// that keeps the fixtures honest, and it runs without a GPU.
			h.AssertNoOverflow()
			// And no holes: a scene is a window, it paints its own
			// background through [ui.Window], and a golden cannot report a
			// pixel that is missing rather than wrong.
			h.AssertOpaque()
			h.Warm()
			h.AssertGolden("component-" + name + "-" + theme.name)
		}
	}
}
