// Package components holds one small, deterministic scene per component of
// the ui package: a list, a row, a card, a badge, a divider, a toggle, a
// slider, a segmented control and a progress bar.
//
// # Why the scenes are here and not in a test file
//
// Because two consumers need the same picture. The golden tests of the ui
// package compare these scenes pixel by pixel in both themes, and cmd/gift-shot
// renders them to a PNG so that a person can look at one without writing a
// test. A scene declared inside a _test.go file can only ever serve the first
// of those, and the reason this package exists at all is that a framework
// whose appearance is only ever asserted structurally ships screens that are
// wrong in ways no structural assertion can see.
//
// # What makes a scene here different from a demo screen
//
// It is deterministic and it is small. Every value a scene shows is a literal
// — no clock, no random number, no picture that has to be decoded — and the
// callbacks are nil, because a golden is taken of a settled frame and nothing
// in it is ever tapped. The sizes are chosen so that the whole scene fits with
// a margin around it: a golden of a component squeezed against the edge of the
// viewport is a golden of the viewport.
//
// The indeterminate progress bar is deliberately absent. Its pill travels with
// the clock — see [ui.ProgressPeriod] — so it has no single settled frame, and
// a golden of it would pin whichever phase the harness's injected clock
// happened to be at.
package components

import (
	"sort"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/icon/outline"
	"github.com/worldiety/gift/ui"
)

// Scene is one named picture: the view and the viewport it is meant to be
// rendered at.
//
// The size travels with the view because it is part of the scene and not a
// choice the caller gets to make: a list scene needs room for four rows and a
// badge scene does not, and a golden taken at the wrong size is a golden of
// mostly background — or of a component reporting an overflow, which is worse,
// because it pins a defect.
type Scene struct {
	// View is the whole content of the frame, background included.
	View gift.View
	// Size is the logical viewport the scene is composed for.
	Size geom.Size
}

// scenes is the table. It is a map and not a slice of structs with a name
// field, because every consumer looks a scene up by name; [Names] provides the
// ordering a consumer that wants to walk all of them needs.
var scenes = map[string]func() Scene{
	"list":      listScene,
	"row":       rowScene,
	"card":      cardScene,
	"badge":     badgeScene,
	"divider":   dividerScene,
	"toggle":    toggleScene,
	"slider":    sliderScene,
	"segmented": segmentedScene,
	"progress":  progressScene,
}

// Names returns the scene names in sorted order.
//
// Sorted and not in declaration order, so that a test which walks every scene
// produces its subtests in a stable sequence and a failure list does not
// reorder itself between runs.
func Names() []string {
	out := make([]string, 0, len(scenes))
	for k := range scenes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Get returns the scene with the given name, and whether there is one.
//
// It builds the scene on every call rather than returning a shared value. A
// [gift.View] is a description and is safe to share, but the slices inside one
// — the items of a [ui.List] — belong to gift from the moment the view is
// constructed, so handing the same instance to two applications would be two
// owners of one slice.
func Get(name string) (Scene, bool) {
	f, ok := scenes[name]
	if !ok {
		return Scene{}, false
	}
	return f(), true
}

// page wraps a scene in the window background and a margin, which is what
// makes a golden of a component a golden of the component and of nothing else.
//
// It is [ui.Window], the same one line an application writes, and that is the
// point: the harness contributes no pixel to a golden, so the fill behind a
// component in a picture has to come from the view tree exactly as it does in
// a window. It used to be able to come from a gifttest option instead, and
// that option is why four demo screens with no background at all had eight
// green goldens.
func page(size geom.Size, children ...gift.View) Scene {
	return Scene{
		View: ui.Window(ui.VStack(children...).
			Gap(12).
			Padding(16).
			Frame(size.W, size.H)),
		Size: size,
	}
}

func listScene() Scene {
	return page(geom.Sz(360, 380),
		ui.Card(
			ui.List(
				ui.Section("Device").Key("s-device"),
				ui.Row("Screen").Icon(outline.DesktopPc).Value("On").Key("screen"),
				ui.Row("Notifications").
					Icon(outline.Bell).
					Subtitle("two rows, one hairline").
					Chevron(outline.AngleRight).
					Key("notify"),
				ui.Section("About").Key("s-about"),
				ui.Row("Version").Value("1.4.0").Key("version"),
			).SeparatorInsets(ui.RowPadding+22+12, 0).Key("list"),
		).Padding(0).Header("Go somewhere").Key("card"),
	)
}

func rowScene() Scene {
	return page(geom.Sz(360, 220),
		ui.Row("Plain").Key("plain"),
		ui.Row("With everything").
			Icon(outline.Cog).
			Subtitle("icon, subtitle, value, chevron").
			Value("42").
			Chevron(outline.AngleRight).
			Key("full"),
		ui.Row("Disabled").
			Icon(outline.Lock).
			Subtitle("takes no tap, and says so").
			Disabled(true).
			Key("disabled"),
	)
}

func cardScene() Scene {
	return page(geom.Sz(360, 260),
		ui.Card(
			ui.Text("A card is a surface raised above the window background, with a "+
				"hairline so that its edge is visible in both themes.").
				FontSize(13).Foreground(ui.ColorSecondaryLabel),
		).Header("With a header").Key("with-header"),
		ui.Card(
			ui.Text("And one without.").FontSize(13),
		).Key("without-header"),
	)
}

func badgeScene() Scene {
	return page(geom.Sz(320, 120),
		ui.HStack(
			ui.Badge("1").Key("one"),
			ui.Badge("42").Key("many"),
			ui.Badge("kiosk").Color(ui.ColorDanger).Key("danger"),
			ui.Spacer(),
		).Gap(8).Align(geom.Center),
	)
}

func dividerScene() Scene {
	return page(geom.Sz(320, 160),
		ui.Text("Above").FontSize(13),
		ui.Divider().Key("plain"),
		ui.Text("Between").FontSize(13),
		ui.Divider().Inset(32, 8).Key("inset"),
		ui.Text("Below").FontSize(13),
	)
}

func toggleScene() Scene {
	return page(geom.Sz(320, 160),
		ui.HStack(
			ui.Toggle(false, nil).Key("off"),
			ui.Toggle(true, nil).Key("on"),
			ui.Spacer(),
		).Gap(16).Align(geom.Center),
	)
}

func sliderScene() Scene {
	return page(geom.Sz(360, 200),
		ui.Slider(0, nil).Key("at-zero"),
		ui.Slider(0.62, nil).Key("in-the-middle"),
		ui.Slider(1, nil).Key("at-one"),
	)
}

func segmentedScene() Scene {
	return page(geom.Sz(360, 160),
		ui.SegmentedControl(1, []string{"Low", "Medium", "High"}, nil).Key("three"),
		ui.SegmentedControl(0, []string{"On", "Off"}, nil).Key("two"),
	)
}

func progressScene() Scene {
	return page(geom.Sz(320, 160),
		ui.ProgressBar(0).Key("empty"),
		ui.ProgressBar(0.35).Key("part"),
		ui.ProgressBar(1).Key("full"),
	)
}
