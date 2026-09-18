// The demo's views live alongside main and their golden tests in this package.
// Screenshots of the running demo use giftauto, not an importable UI copy.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/icon/outline"
	"github.com/worldiety/gift/icon/solid"
	"github.com/worldiety/gift/ui"
)

// App is the application the theme switch asks to rebuild.
//
// It is a package variable for the reason cmd/example-counter gives for the
// same compromise: [ui.SetTheme] needs the [gift.App], the switch is built
// deep inside a view function, and threading the handle through every builder
// would be noise in a demo whose subject is something else. The command sets
// it before the first frame; a test sets it to the harness's App.
var App *gift.App

// Pictures are the sources the Home tab shows. [Samples] generates them.
//
// They are a package variable and not a state slot because they never change
// after start-up: the command fills them in before the window opens and a test
// fills them in before it mounts.
var Pictures []asset.Source

// --- the application state ----------------------------------------------------

// Screen is the root view function. Everything the application knows is a
// state slot read here and written by a closure below; nothing is stored in a
// widget, which is the one-way data flow of the project plan, section 5.
//
// # Every tab is a component, and that is the whole point of this file
//
// The obvious spelling of this program is one root function that reads all ten
// slots and builds all four tabs. It works, and it is wrong, and the earlier
// version of this file said the opposite in a comment: "the other three tabs
// are not dirty and are not rebuilt at all". They were. A dependency is
// registered on the *scope* that read the slot, so a root that reads
// everything is a root that is invalidated by everything, and a root rebuild
// rebuilds all four tabs including the three nobody can see.
//
// Measured on this demo at 900x760, the rebuild one tap on the Settings
// "Nudge it along" row causes — it writes one float:
//
//	before   702 µs   6255 allocations   (a full root rebuild, all four tabs)
//	after     70 µs    485 allocations   (the Settings tab only)
//
// On a Raspberry Pi 4 at this project's ×10 derate that is 7.0 ms against
// 0.70 ms: 42 % of a 60 Hz frame against 4 %, for one tap. A slider drag pays
// it on every frame of the drag, which is where it stops being an academic
// figure.
//
// So each tab is a [gift.Component] with a scope of its own, and each one
// reads the slots it actually uses. The root reads two: the selected tab, and
// whether the alert is up — the two things the root itself has to decide. A
// write to any other slot now reaches exactly the tab that shows it.
// TestATapOnASettingsRowDoesNotRebuildTheOtherThreeTabs next door is that
// property as a test.
//
// The alert is deliberately still a root dependency. The modal layer is
// outermost, so the root is the scope that has to know; opening a dialog is a
// once-in-a-while action and a full rebuild there is the honest price of
// having the scrim cover the tab bar.
func Screen(ctx *gift.Context) gift.View {
	st := state{
		tab:      ctx.State("tab", 0),
		depth:    ctx.State("depth", 0),
		alert:    ctx.State("alert", false),
		on:       ctx.State("on", true),
		notify:   ctx.State("notify", false),
		volume:   ctx.State("volume", 0.62),
		quality:  ctx.State("quality", 1),
		progress: ctx.State("progress", 0.35),
		busy:     ctx.State("busy", false),
		saved:    ctx.State("saved", ""),
	}
	// The two the root itself decides with, and nothing else; see above.
	ctx.Read(st.tab)
	ctx.Read(st.alert)

	body := ui.TabBar(st.tab.Get(), st.tab.Set,
		ui.Tab("Home", outline.Home, st.tabComponent("home")).SelectedIcon(solid.Home),
		ui.Tab("Settings", outline.Cog, st.tabComponent("settings")).SelectedIcon(solid.Cog),
		ui.Tab("List", outline.List, st.tabComponent("list")).SelectedIcon(solid.ClipboardList),
		ui.Tab("Form", outline.Edit, st.tabComponent("form")).SelectedIcon(solid.Edit),
	)

	// The modal layer is outermost, so its scrim covers the tab bar as well.
	// A dialog that leaves the tab bar live is a dialog the user can walk
	// away from, which is the opposite of modal.
	// The alert view is built whether or not it is on the screen, and
	// ui.ModalView.Presented decides. Passing nil to close would unmount it,
	// and an unmounted dialog cannot be seen to leave; see that method. The
	// price is a handful of nodes that stay mounted and hidden after the
	// first time the alert is opened.
	alert := ui.Alert(
		"Reset everything?",
		"Every setting on this screen goes back to its default. This cannot be undone.",
		ui.AlertCancel("Cancel", func() { st.alert.Set(false) }),
		ui.AlertDestructive("Reset", func() {
			st.on.Set(true)
			st.notify.Set(false)
			st.volume.Set(0.62)
			st.quality.Set(1)
			st.progress.Set(0.35)
			st.busy.Set(false)
			st.saved.Set("")
			st.alert.Set(false)
		}),
	)

	// The keyboard is an overlay over the whole application and is the last
	// child, so it is drawn last and hit tested first. It draws nothing at all
	// while no field has the focus.
	//
	// [ui.Window] is the first thing here and not a detail: gift paints
	// nothing an application did not ask for, so without it the gaps between
	// the cards are transparent black — which is what this demo shipped as,
	// on thirty to forty per cent of its pixels, while every golden of it
	// looked right because the harness cleared its own canvas. See ui.Window.
	return ui.Window(
		ui.Modal(body, alert).
			Presented(st.alert.Get()).
			OnDismiss(func() { st.alert.Set(false) }),
		ui.OnScreenKeyboard(),
	).Align(geom.Bottom)
}

// state is the handful of slots the screens read and write. It is a struct of
// slots and not of values, so a screen builder can hand a setter to a control
// without the root having to thread one through.
type state struct {
	tab, depth, quality *gift.State[int]
	alert, on, notify   *gift.State[bool]
	busy                *gift.State[bool]
	volume, progress    *gift.State[float64]
	saved               *gift.State[string]
}

// tabComponent wraps one tab's screen in a [gift.Component], which is what
// gives it a rebuild scope of its own, and subscribes that scope to the slots
// that tab reads and no others.
//
// [gift.Context.Read] registers a dependency and [gift.State.Get] deliberately
// does not — Get is what an event handler uses, where reading a value must not
// subscribe anything. The screen builders below are ordinary methods that have
// no Context, so they use Get; the Read calls here are what makes a write to
// one of those slots reach this tab. Getting this wrong has two failure modes
// and both are silent:
//
//   - too few Reads and a write invalidates nothing. The value is correct, the
//     handler runs, and the screen does not move.
//   - too many Reads — the mistake this file used to make at the root — and a
//     write invalidates screens that do not depend on it. Everything looks
//     right and the frame costs an order of magnitude more than it should.
//
// The list tab reads nothing at all, and that is not an oversight: it only
// *writes* the saved slot, which the form tab displays. It is therefore built
// once and never rebuilt, which is the right answer for two hundred rows.
func (s state) tabComponent(which string) gift.View {
	return gift.Component(which, func(ctx *gift.Context) gift.View {
		switch which {
		case "home":
			ctx.Read(s.depth)
			ctx.Read(s.progress)
			return s.home()
		case "settings":
			ctx.Read(s.on)
			ctx.Read(s.notify)
			ctx.Read(s.volume)
			ctx.Read(s.quality)
			ctx.Read(s.progress)
			ctx.Read(s.busy)
			return s.settings()
		case "list":
			return s.longList()
		case "form":
			ctx.Read(s.saved)
			return s.form()
		}
		panic("no such tab: " + which)
	})
}

// nameEditor is the retained text buffer of the form's field.
//
// It is a package variable and not a state slot, for the reason
// ui.TextFieldView documents: an editor is the model, it is mutated in place
// by typing, and a gift.State is comparable-valued and would therefore never
// see a change. A program with several fields keeps several of these, or one
// per record; a program with one window and one field keeps one here.
var nameEditor = ui.NewTextEditor("")

// --- tab one: navigation, cards, images ----------------------------------------

// home is a navigation stack. The stack holds no history — see
// ui.NavigationStackView — so "push" here is the depth going up by one and
// "pop" is it going down, and the list of screens is derived from it.
func (s state) home() gift.View {
	depth := s.depth.Get()
	screens := make([]ui.ScreenSpec, 0, 3)
	screens = append(screens, ui.Screen("Kitchen sink", s.overview()))
	if depth >= 1 {
		screens = append(screens, ui.Screen("Details", s.details()))
	}
	if depth >= 2 {
		screens = append(screens, ui.Screen("Deeper", ui.VScroll(
			ui.Card(
				ui.Text("There is nothing down here.").Foreground(ui.ColorSecondaryLabel),
				ui.Text("Tap the back affordance, or press escape.").
					FontSize(13).Foreground(ui.ColorSecondaryLabel),
			).Header("The end"),
		).Padding(16).Flex(1)))
	}
	return ui.NavigationStack(func() { s.depth.Set(max(0, s.depth.Get()-1)) }, screens...).
		BackIcon(outline.AngleLeft)
}

func (s state) overview() gift.View {
	return ui.VScroll(
		ui.HStack(
			ui.Text("Everything at once").FontSize(22),
			ui.Spacer(),
			themeSwitch(),
		).Align(geom.Center),

		ui.Card(
			ui.Text("gift draws this window with one shader, one display list and no "+
				"per-widget render target. Scroll, tap, tab and type; the theme switch "+
				"in the corner rebuilds every colour on the screen.").
				FontSize(13).Foreground(ui.ColorSecondaryLabel),
			ui.HStack(
				ui.Badge("9a").Color(ui.ColorAccent),
				ui.Badge("9b").Color(ui.ColorAccent),
				ui.Badge("9c").Color(ui.ColorAccent),
				ui.Badge("kiosk").Color(ui.ColorDanger),
				ui.Spacer(),
			).Gap(6),
		).Header("What this is"),

		// A card wrapping a list: the card supplies the face and the corner,
		// the list supplies the rows and the hairlines, and the card's content
		// padding is zero so that a pressed row reaches both edges.
		ui.Card(
			ui.List(
				ui.Row("Details").
					Icon(outline.InfoCircle).
					Subtitle("push a navigation screen").
					Chevron(outline.AngleRight).
					OnTap(func() { s.depth.Set(1) }).
					Key("details"),
				ui.Row("Reset everything").
					Icon(outline.TrashBin).
					Subtitle("opens a modal alert").
					Chevron(outline.AngleRight).
					OnTap(func() { s.alert.Set(true) }).
					Key("reset"),
				ui.Row("Unavailable").
					Icon(outline.Lock).
					Subtitle("takes no tap, and says so").
					Disabled(true).
					OnTap(func() { panic("a disabled row must never fire") }).
					Key("locked"),
			).SeparatorInsets(ui.RowPadding+22+12, 0),
		).Padding(0).Header("Go somewhere"),

		ui.Card(pictureStrip()...).Padding(12).Gap(8).Header("Pictures"),

		ui.Text("Section 21: every symbol below is a CPU-rasterised coverage mask, "+
			"uploaded once per size and tinted by the foreground colour.").
			FontSize(12).Foreground(ui.ColorSecondaryLabel),
		iconStrip(),
	).Gap(16).Padding(16).Flex(1)
}

func (s state) details() gift.View {
	return ui.VScroll(
		ui.Card(
			ui.Text("This screen is mounted the whole time the one under it is on top, "+
				"and the other way round: neither is unmounted, so neither loses its "+
				"scroll offset or its half typed text.").
				FontSize(13).Foreground(ui.ColorSecondaryLabel),
		).Header("Covered, not unmounted"),
		ui.Card(
			ui.List(
				ui.Row("Go deeper").
					Icon(outline.ArrowRight).
					Chevron(outline.AngleRight).
					OnTap(func() { s.depth.Set(2) }).
					Key("deeper"),
				ui.Row("Progress").Accessory(ui.ProgressBar(s.progress.Get()).Frame(90, 6)).Key("p"),
				ui.Row("Indeterminate").
					Subtitle("holds the device awake while it is mounted").
					Accessory(ui.ProgressBar(0).Indeterminate().Frame(120, 6)).
					Key("i"),
			),
		).Padding(0).Header("More rows"),
	).Gap(16).Padding(16).Flex(1)
}

// pictureStrip is a row of real pictures, drawn through the same pipeline and
// the same GPU texture table the gallery uses.
//
// It is a horizontal scroller and not a plain row, for the reason iconStrip is
// one: a fixed number of fixed width things in a window whose width is the
// user's is an overflow waiting for a narrow window, and the overflow model of
// the project plan, section 7, does not clip it away — it reports it. A demo
// that overflowed at 360 logical pixels of *width* would be a demo that fails
// its own AssertNoOverflow.
//
// The sentence used to say "640 pixels", which was true and was not the claim
// it looked like: this demo was checked down to 640 tall and only to 480 wide,
// and it did overflow at 360 wide — in two other places, not here. See
// TestTheDemoHasNoOverflowAtASmallerWindow, which now sweeps 360 to 1920 at
// three densities.
func pictureStrip() []gift.View {
	cols := make([]gift.View, 0, len(Pictures))
	for i, p := range Pictures {
		cols = append(cols, ui.Image(p).
			Fit(ui.FitCover).
			Frame(96, 72).
			CornerRadius(8).
			Clip(true).
			Placeholder(ui.Fade(ui.ColorLabel, 0.08)).
			Label("sample "+strconv.Itoa(i)).
			Key("pic"+strconv.Itoa(i)))
	}
	if len(cols) == 0 {
		return nil
	}
	return []gift.View{ui.HScroll(cols...).Gap(8).MinHeight(72)}
}

// iconStrip is twenty distinct symbols at one size, which is the exact figure
// the project plan, section 21, uses when it says a screen with more new icons
// than the upload budget fills in over several drawn frames.
func iconStrip() gift.View {
	syms := []ui.Symbol{
		outline.Home, outline.Cog, outline.User, outline.Bell, outline.Search,
		outline.Heart, outline.Star, outline.Clock, outline.CalendarMonth, outline.Envelope,
		outline.CameraPhoto, outline.CloudArrowUp, outline.Download, outline.Upload, outline.Folder,
		outline.Globe, outline.Lock, outline.Moon, outline.Sun, outline.Wallet,
	}
	cols := make([]gift.View, 0, len(syms))
	for i, sym := range syms {
		cols = append(cols, ui.Icon(sym).
			Size(22).
			Foreground(ui.ColorSecondaryLabel).
			Key("icon"+strconv.Itoa(i)))
	}
	return ui.HScroll(cols...).Gap(10).MinHeight(22)
}

// --- tab two: every control of step 9a -------------------------------------------

func (s state) settings() gift.View {
	quality := []string{"Low", "Medium", "High"}
	return ui.VScroll(
		ui.Card(
			ui.List(
				ui.Section("Device").Key("s-device"),
				ui.Row("Screen").
					Icon(outline.DesktopPc).
					Accessory(ui.Toggle(s.on.Get(), s.on.Set).Key("screen-switch")).
					Key("screen"),
				ui.Row("Notifications").
					Icon(outline.Bell).
					Subtitle("two taps, not one").
					Accessory(ui.Toggle(s.notify.Get(), s.notify.Set).Key("notify-switch")).
					OnTap(func() { s.notify.Set(!s.notify.Get()) }).
					Key("notify"),

				ui.Section("Sound").Key("s-sound"),
				ui.Row("Volume").
					Icon(outline.VolumeUp).
					Value(strconv.Itoa(int(s.volume.Get()*100+0.5))+" %").
					Key("volume"),
				ui.Row("").
					Accessory(ui.Slider(s.volume.Get(), s.volume.Set).
						Flex(1).
						Key("volume-slider")).
					Key("volume-row"),

				ui.Section("Quality").Key("s-quality"),
				ui.Row("").
					Accessory(ui.SegmentedControl(s.quality.Get(), quality, s.quality.Set).
						Flex(1).
						Key("quality-control")).
					Key("quality-row"),

				ui.Section("Sync").Key("s-sync"),
				ui.Row("Uploading").
					Icon(outline.CloudArrowUp).
					Value(strconv.Itoa(int(s.progress.Get()*100+0.5))+" %").
					Accessory(ui.ProgressBar(s.progress.Get()).Frame(90, 6).Key("sync-bar")).
					Key("sync"),
				ui.Row("Nudge it along").
					Icon(outline.ArrowRight).
					OnTap(func() {
						v := s.progress.Get() + 0.1
						if v > 1 {
							v = 0
						}
						s.progress.Set(v)
					}).
					Key("nudge"),
				ui.Row("Busy").
					Icon(outline.Refresh).
					Subtitle("an indeterminate bar stays awake").
					Accessory(busyBar(s.busy.Get())).
					OnTap(func() { s.busy.Set(!s.busy.Get()) }).
					Key("busy"),
			).SeparatorInsets(ui.RowPadding+22+12, 0),
		).Padding(0).Header("Settings"),

		ui.Card(
			ui.Text("Every control here is at least 44 logical pixels tall whatever it "+
				"draws, because the target is a touchscreen. The switch inside a row "+
				"takes the tap; the row around it does not also fire.").
				FontSize(12).Foreground(ui.ColorSecondaryLabel),
		),
	).Gap(16).Padding(16).Flex(1)
}

// busyBar is the one accessory that has two shapes, so that the indeterminate
// progress bar can be switched off and the device allowed to go idle.
func busyBar(on bool) gift.View {
	if !on {
		return ui.Text("Idle").FontSize(13).Foreground(ui.ColorSecondaryLabel)
	}
	return ui.ProgressBar(0).Indeterminate().Frame(120, 6).Key("busy-bar")
}

// --- tab three: the long list ------------------------------------------------------

// longListRows is deliberately 200 and the caption says why.
//
// ui.ListView documents two limits: a rebuild of about 280 rows is as much as
// a Raspberry Pi 4 can do in half a frame, and a hard cliff at about 750 rows
// where the shaping cache overflows. Two hundred is inside both with room to
// spare, and a demo that quietly sat on the wrong side of a documented limit
// would be worse than no demo.
const longListRows = 200

func (s state) longList() gift.View {
	items := make([]gift.View, 0, longListRows+8)
	items = append(items, ui.Section("Two hundred rows").Key("head"))
	for i := range longListRows {
		id := strconv.Itoa(i)
		row := ui.Row("Item " + id).
			Subtitle("row " + id).
			Value(durationish(i)).
			Chevron(outline.AngleRight).
			OnTap(func() { s.saved.Set("you tapped item " + id) }).
			Key(id)
		if i%25 == 0 && i > 0 {
			items = append(items, ui.Section("After "+id).Key("sec"+id))
		}
		if i%7 == 3 {
			row = row.Accessory(ui.Badge(strconv.Itoa(i % 9)))
		}
		items = append(items, row)
	}
	return ui.VStack(
		ui.Text("Scrolling this list rebuilds nothing: the offset lives in the "+
			"retained node. Fling it.").
			FontSize(12).Foreground(ui.ColorSecondaryLabel).Padding(12),
		ui.VScroll(ui.List(items...).SeparatorInsets(ui.RowPadding, 0)).Flex(1),
	)
}

// durationish is a stable, varied trailing label, so that the rows are not all
// the same width and the layout is exercised rather than repeated.
func durationish(i int) string {
	return strconv.Itoa(1+i%59) + " min"
}

// --- tab four: the text field, the keyboard and the alert -----------------------

func (s state) form() gift.View {
	ed := nameEditor
	return ui.VScroll(
		ui.Card(
			ui.Text("Tap the field. gift's own on-screen keyboard comes up, because "+
				"ui.SetOnScreenKeyboard is on; the field asks for it when it takes the "+
				"focus and dismisses it when it loses it. Put it away with the key in "+
				"its bottom right corner, by tapping anywhere outside it, or with escape.").
				FontSize(12).Foreground(ui.ColorSecondaryLabel),
			ui.TextField(ed).
				Placeholder("Your name").
				OnSubmit(func(v string) { s.saved.Set("hello, " + v) }).
				Key("name"),
			ui.HStack(
				ui.Button(ui.Text("Save").Foreground(ui.ColorOnAccent),
					func() { s.saved.Set("hello, " + ed.Text()) }).
					Style(ui.ButtonStyle{Background: ui.ColorAccent, CornerRadius: 9}).
					MinHeight(ui.ControlHitTarget),
				ui.Button(ui.Text("Reset everything"), func() { s.alert.Set(true) }).
					MinHeight(ui.ControlHitTarget),
				ui.Spacer(),
			).Gap(8),
			ui.Text(orDash(s.saved.Get())).FontSize(13).Foreground(ui.ColorSecondaryLabel),
		).Header("Type something"),

		ui.Card(
			ui.List(
				ui.Row("Divider below").Key("d1"),
			),
			ui.Divider(),
			ui.Text("A ui.Divider on its own, outside a list. It is snapped onto whole "+
				"device pixels at every density, which is the one place a rounding "+
				"error is visible to the naked eye.").
				FontSize(12).Foreground(ui.ColorSecondaryLabel),
		).Padding(12).Header("Divider"),

		// Room under the last card for the keyboard. A scroll container can
		// only lift a field by as much as it can still scroll, so a form whose
		// last field is at the bottom stays under the keyboard without this;
		// see ui.KeyboardView.
		ui.Box().Frame(geom.Unbounded(), ui.OnScreenKeyboardHeight()),
	).Gap(16).Padding(16).Flex(1)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// --- the theme switch -------------------------------------------------------------

// themeSwitch is the runtime switch of the project plan, section 20. It is the
// same control cmd/example-counter carries, and the comment there explains why
// the face stays the full accent in every state.
func themeSwitch() gift.View {
	dark := ui.CurrentTheme().IsDark()
	label, next, sym := "Dark", ui.DarkTheme(), outline.Moon
	if dark {
		label, next, sym = "Light", ui.LightTheme(), outline.Sun
	}
	face := func(ring float32) ui.ButtonStyle {
		s := ui.ButtonStyle{Background: ui.ColorAccent, CornerRadius: 9}
		if ring > 0 {
			s.Border = ui.Border{Width: ring, Color: ui.ColorOnAccent}
		}
		return s
	}
	return ui.Button(ui.HStack(
		ui.Icon(sym).Size(16).Foreground(ui.ColorOnAccent),
		ui.Text(label).FontSize(13).Foreground(ui.ColorOnAccent),
	).Gap(6).Align(geom.Center), func() {
		ui.SetTheme(App, next)
	}).
		Style(face(0)).
		HoverStyle(face(1)).
		PressedStyle(face(2)).
		MinHeight(ui.ControlHitTarget).
		PaddingInsets(geom.Insets{Top: 7, Right: 14, Bottom: 7, Left: 14}).
		Label("Switch theme")
}

// --- the icon budget trace ----------------------------------------------------------

// TraceIconCache prints the icon cache counters once per tick for a second and
// a half, which is what turns the upload budget of the project plan,
// section 21, into numbers somebody can read.
//
// # Two things that made the first version of this function lie
//
// It read [ui.IconCacheStats] directly from this goroutine. The icon service
// belongs to the UI goroutine, its counters are plain integers written without
// synchronisation, and a reader on another goroutine is entitled by the memory
// model to observe a value that never moves again — which is exactly what
// happened: a plausible looking sequence that froze at eight uploads for ever
// and was very nearly reported as a defect in the upload budget.
// [gift.App.Post] is the seam that exists for this, and the closure below runs
// on the UI goroutine between frames.
//
// It also printed only the cache counters, and a frozen cache has two very
// different causes. Ebitengine does not call Draw for a window that is not
// visible, so a program launched into the background from a terminal paints
// exactly one frame and then stops — with the icons on the screen frozen at
// the first frame's eight. [gift.Diagnostics.Frames] is what tells the two
// apart, so it is printed next to them. Bring the window to the front and the
// sequence continues.
func TraceIconCache(a *gift.App) {
	tick := time.NewTicker(16 * time.Millisecond)
	defer tick.Stop()
	for i := range 90 {
		<-tick.C
		n := i
		a.Post(func() {
			s := ui.IconCacheStats()
			d := a.Diagnostics()
			fmt.Printf("tick %2d  drawn frames=%d  icons rasterised=%d uploaded=%d resident=%d\n",
				n, d.Frames, s.Rasterised, s.Uploads, s.Entries)
		})
	}
}

// --- the sample pictures ---------------------------------------------------------

// Samples writes a handful of small PNGs into dir on first run and returns
// sources over them. It exists so that the example needs no assets, no flags
// and no network and still drives the real pipeline rather than a placeholder.
//
// The directory is a parameter rather than being derived here, so that the
// test next door can point it at a scratch directory instead of writing into
// the developer's cache.
func Samples(dir string) ([]asset.Source, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	const n = 5
	out := make([]asset.Source, 0, n)
	for i := range n {
		path := filepath.Join(dir, "sample"+strconv.Itoa(i)+".png")
		if _, err := os.Stat(path); err != nil {
			if err := writeSample(path, i); err != nil {
				return nil, err
			}
		}
		out = append(out, asset.File(path))
	}
	return out, nil
}

func writeSample(path string, i int) error {
	const w, h = 320, 240
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{
				R: uint8(40 + (x*160)/w + i*30),
				G: uint8(70 + (y*140)/h),
				B: uint8(200 - (x*90)/w - i*20),
				A: 255,
			})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
