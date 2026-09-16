package kitchensink

import (
	"fmt"
	"testing"
	"time"

	backend "github.com/torbenschinke/gift/backend/ebiten"
	"github.com/torbenschinke/gift/font/inter"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// These are the tests of the demo itself, and they exist because "no test for
// the surface a user sees first" is the most productive criticism this project
// has. They run the *actual* view functions of this program, not a replica of
// them: a replica drifts, and the first thing it drifts away from is whatever
// made the real screen interesting.
//
// They are in the same package as the views, which is what lets them call
// the real view functions instead of a copy.

func TestMain(m *testing.M) { gifttest.Main(m) }

// mount brings the demo up in the harness with a scratch picture directory and
// the tab the test cares about selected.
//
// It writes the sample pictures into t.TempDir rather than into the user's
// cache directory: a test that leaves files in ~/Library/Caches is a test that
// behaves differently the second time it runs.
func mount(t *testing.T, size geom.Size) *gifttest.Harness {
	t.Helper()
	return mountTheme(t, size, ui.LightTheme())
}

// mountTheme is [mount] with the theme named rather than assumed. Every golden
// below uses it, because a golden of an application built on semantic colours
// is a golden of whichever theme the process happens to have installed.
func mountTheme(t *testing.T, size geom.Size, theme ui.Theme) *gifttest.Harness {
	t.Helper()
	return gifttest.New(t, mountOptions(t, size, theme))
}

// mountOptions is what mountTheme hands the harness, separated out for the one
// test that has to change a field of it.
//
// There is no background option in it, and that is deliberate: this demo
// paints its own window background through [ui.Window], the harness
// contributes no pixel of its own, and every golden below is therefore a
// picture of the application and of nothing else.
func mountOptions(t *testing.T, size geom.Size, theme ui.Theme) gifttest.Options {
	t.Helper()
	var err error
	if Pictures, err = Samples(t.TempDir()); err != nil {
		t.Fatalf("generating the sample pictures: %v", err)
	}
	t.Cleanup(func() { Pictures = nil })
	return gifttest.Options{
		Theme: theme,
		// The same typeface example.LoadFont installs in main, named here
		// rather than inherited, so that these tests do not depend on what
		// some other test in the process left as the default.
		Font: ui.MustFont(ui.FontQuery{Family: inter.Family}),
		Size: size,
		Root: Screen,
	}
}

// TestTheDemoComesUpAndSettles is the smallest thing worth asserting about a
// demo and the one that would have caught every mistake that made one of the
// earlier examples unrunnable: the tree builds, the layout terminates, and
// nothing invalidates itself every frame.
//
// [gifttest.Harness.New] settles as part of mounting, so reaching the body of
// this test at all is most of the assertion; the rest is that the four tabs
// really are there and that nothing overflowed.
func TestTheDemoComesUpAndSettles(t *testing.T) {
	h := mount(t, geom.Sz(900, 760))
	for _, name := range []string{"Home", "Settings", "List", "Form"} {
		if tabButton(h, name).IsZero() {
			t.Errorf("no tab bar item for %q", name)
		}
	}
	h.AssertNoOverflow()
}

// tabButton is the item of the tab bar with the given title.
//
// It is selected by key and type and not by text, because the title is also
// the reconciliation key of the tab's whole layer and of a row or two on the
// screens themselves; ui.TabSpec.identity is what makes the key the title in
// the first place.
func tabButton(h *gifttest.Harness, title string) gifttest.Node {
	return h.Find(gifttest.ByKey(title).And(gifttest.ByType("ui.Button")))
}

// TestEveryTabOfTheDemoBuildsAndTheHiddenOnesAreNotDrawn walks the tab bar the
// way a person would and checks the property step 9b rests on: a tab that is
// not selected is mounted and contributes nothing to the display list.
//
// The tab bar is tapped rather than the state being written, because tapping
// is what a user does and because it also exercises the bar's own buttons.
//
// It is one flat loop and not a set of subtests, which is deliberate: a
// [gift.App] belongs to the goroutine that created it, t.Run gives a subtest a
// goroutine of its own, and under the giftdebug tag the resulting cross
// goroutine call panics naming both. That is the check working; a harness
// shared with a subtest is a real defect and not a testing inconvenience.
func TestEveryTabOfTheDemoBuildsAndTheHiddenOnesAreNotDrawn(t *testing.T) {
	h := mount(t, geom.Sz(900, 760))
	markers := []string{"Everything at once", "Notifications", "Two hundred rows", "Type something"}
	for i, tab := range []string{"Home", "Settings", "List", "Form"} {
		tabButton(h, tab).Click()
		h.AssertExists(gifttest.ByText(markers[i]))
		h.AssertNoOverflow()
		// The markers of the other three tabs are mounted — that is the whole
		// point of hiding rather than unmounting — but none of them may be
		// visible.
		for j, other := range markers {
			if j == i {
				continue
			}
			if n := h.First(gifttest.ByText(other)); !n.IsZero() && n.IsVisible() {
				t.Errorf("%q is visible while the %s tab is selected; a hidden tab is "+
					"mounted and not drawn", other, tab)
			}
		}
	}
}

// TestTheIndeterminateBarOfThisDemoActuallyMoves is the regression test for a
// defect that shipped: both call sites in this file wrote ui.ProgressBar(-1),
// which is a *determinate* bar whose fraction was clamped to zero, so the
// demo showed an empty grey track that never moved while its author believed
// it was a spinner. The indeterminate mode is a modifier and -1 was never a
// sentinel for it; ui.ProgressBar now rejects a negative fraction outright.
//
// The evidence has to be a pair of frames. Six consecutive frames of the
// broken demo were pixel identical across the whole screen, and any single
// frame of it looks exactly like a legitimate bar at 0 %.
func TestTheIndeterminateBarOfThisDemoActuallyMoves(t *testing.T) {
	h := mount(t, geom.Sz(900, 760))
	tabButton(h, "Settings").Click()
	h.First(gifttest.ByText("Busy")).Click()
	h.AssertExists(gifttest.ByKey("busy-bar"))

	// Every filled shape inside the bar's own rectangle: the track, and the
	// pill when there is one.
	shapes := func() []geom.Rect {
		b := h.Find(gifttest.ByKey("busy-bar")).Bounds()
		var out []geom.Rect
		for _, op := range h.Ops() {
			if op.Kind != render.OpFillRoundRect {
				continue
			}
			r := h.List().Xform(op.Xform).TransformRect(op.Bounds)
			if r.Min.X >= b.Min.X-1 && r.Max.X <= b.Max.X+1 &&
				r.Min.Y >= b.Min.Y-1 && r.Max.Y <= b.Max.Y+1 {
				out = append(out, r)
			}
		}
		return out
	}

	// A third of a period apart, three times: an indeterminate bar is a pill
	// travelling across the track, so no two of these may be the same
	// picture, and at least one of them has to have a pill in it at all.
	var seen [][]geom.Rect
	for range 3 {
		seen = append(seen, shapes())
		h.Advance(ui.ProgressPeriod / 3)
	}
	withPill := 0
	for _, s := range seen {
		if len(s) > 1 {
			withPill++
		}
	}
	if withPill == 0 {
		t.Fatalf("the indeterminate bar drew nothing but its track at any of three points "+
			"of its period; it is a determinate bar at 0 %%. Frames: %v", seen)
	}
	same := 0
	for i := 1; i < len(seen); i++ {
		if fmt.Sprint(seen[i]) == fmt.Sprint(seen[0]) {
			same++
		}
	}
	if same == len(seen)-1 {
		t.Fatalf("three frames a third of a period apart drew the identical bar %v; "+
			"nothing is moving", seen[0])
	}
	if !h.Diagnostics().Animating {
		t.Error("an indeterminate bar on screen leaves the application reporting that " +
			"nothing is animating, so no automated driver can tell it apart from a " +
			"still picture")
	}
}

// TestPushingAndPoppingTheNavigationStackKeepsTheScrollOffset is the promise of
// step 9b as this demo exercises it, and it is the one a kiosk user would
// notice within a minute of the promise being broken.
func TestPushingAndPoppingTheNavigationStackKeepsTheScrollOffset(t *testing.T) {
	h := mount(t, geom.Sz(900, 620))
	// Scroll the root screen somewhere non-trivial, then drill in.
	root := h.First(gifttest.ByText("Everything at once")).Scroller()
	root.ScrollTo(120)

	h.First(gifttest.ByText("Details")).Click()
	h.AssertExists(gifttest.ByText("Covered, not unmounted"))

	// Back, by the affordance the user sees.
	h.Find(gifttest.ByKey("back")).Click()
	h.AssertExists(gifttest.ByText("Everything at once"))
	h.First(gifttest.ByText("Everything at once")).Scroller().AssertScrollOffset(120)
}

// TestTheAlertBlocksTheScreenUnderneath is the modal claim of step 9b, checked
// against the composition this demo actually uses rather than against a
// minimal one — which is exactly the difference review gate 13 found, where a
// modal documented as blocking every tap blocked nothing in an ordinary
// composition.
//
// The tap aims at a coordinate on purpose. [gifttest.Node.Click] refuses to
// click a covered node, which is the right default and is precisely what has
// to be bypassed to ask "what happens when the user stabs at the button under
// the dialog".
func TestTheAlertBlocksTheScreenUnderneath(t *testing.T) {
	h := mount(t, geom.Sz(900, 760))
	target := h.First(gifttest.ByText("Details"))
	at := target.Center()

	h.First(gifttest.ByText("Reset everything")).Click()
	h.AssertExists(gifttest.ByText("Reset everything?"))

	// The row that would push a screen is still there, underneath. A tap where
	// it is must not reach it.
	//
	// It *does* close the alert, and that is not a hole in the scrim: this
	// demo passes ui.ModalView.OnDismiss, so a tap outside the card means
	// "cancel". The two are different things and the test has to separate
	// them, because "the alert went away" would otherwise look like evidence
	// that the tap went through. What says it did not is that the Details
	// screen was never pushed.
	h.ClickAt(at)
	if h.Exists(gifttest.ByText("Covered, not unmounted")) {
		t.Fatalf("a tap through the scrim pushed the Details screen.\n%s", h.Dump())
	}
	// "Gone" means "not on the screen" and not "not in the tree": this demo
	// keeps the alert mounted and hidden so that the dismissal can be seen to
	// happen, which is ui.ModalView.Presented. See [gifttest.Visible].
	h.AssertNone(gifttest.ByText("Reset everything?").And(gifttest.Visible()))

	// And the other half of the same property: an alert that does *not*
	// dismiss on an outside tap is still there afterwards. The demo has no
	// such alert, so this is the ordinary one again with the tap aimed at a
	// corner of the window, where there is nothing at all under the scrim —
	// which is the case a modal that only covered its own rectangle would get
	// wrong and the reason the scrim is full size.
	h.First(gifttest.ByText("Reset everything")).Click()
	h.AssertExists(gifttest.ByText("Reset everything?"))
	if n := h.At(geom.Pt(4, 4)); n.Type() != "ui.scrim" {
		t.Errorf("the top left corner of the window is %s and not the scrim; a modal that does "+
			"not cover the whole window is a dialog with a hole in it", n.Type())
	}

	// And the alert's own buttons still work.
	h.First(gifttest.ByText("Cancel")).Click()
	h.AssertNone(gifttest.ByText("Reset everything?").And(gifttest.Visible()))
}

// TestTheSwitchInsideARowTakesTheTapInTheRealScreen is the row-and-accessory
// rule of ui.RowView, checked in the composition it was written for: a
// settings row that is itself tappable and carries a switch.
func TestTheSwitchInsideARowTakesTheTapInTheRealScreen(t *testing.T) {
	h := mount(t, geom.Sz(900, 760))
	tabButton(h, "Settings").Click()

	sw := h.Find(gifttest.ByKey("notify-switch"))
	if toggleIsOn(t, h, "notify-switch") {
		t.Fatal("the notifications switch starts on; this fixture needs it off")
	}
	// Tap the switch. The row's own action toggles the same state, so if the
	// row also fired the value would come back to where it started and the
	// screen would look like nothing happened at all — which is why this
	// fixture is the right one and a row with an unrelated action is not.
	sw.Tap()
	// Past the control's transition, so that the track is the accent exactly
	// rather than most of the way towards it; see ui.ControlAnimation.
	h.Advance(ui.ControlAnimation + time.Millisecond)
	on := toggleIsOn(t, h, "notify-switch")
	if !on {
		t.Fatalf("tapping the switch did not turn it on. Either the accessory did not get "+
			"the tap, or the row got it as well and the two cancelled out.\n%s", h.Dump())
	}
}

// toggleIsOn reads a switch's state off the display list: a switch that is on
// draws its track in ui.ColorAccent.
//
// It reads the picture rather than the application's state slot on purpose.
// The slot is what the test wrote; the track colour is what the user sees, and
// the two disagreeing is the failure worth catching.
func toggleIsOn(t *testing.T, h *gifttest.Harness, key string) bool {
	t.Helper()
	b := h.Find(gifttest.ByKey(key)).Bounds()
	accent := ui.LightTheme().Color(ui.ColorAccent)
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRoundRect && op.Color == accent && op.Bounds.Overlaps(b) {
			return true
		}
	}
	return false
}

// TestTheLongListStaysInsideTheLimitsItsOwnDocumentationStates is the check
// that the demo does not quietly sit on the wrong side of a number this work
// unit wrote down.
//
// ui.ListView documents two limits: about 280 rows for a rebuild on a
// Raspberry Pi 4, and a hard cliff at about 750 rows where the shaping cache
// overflows and every frame re-shapes every label. The demo's list has to be
// below both, and the *measurable* half of that is the second one: once past
// the cliff the idle frame allocates, and the frame path of the project plan,
// section 11, must not.
//
// This measures an idle frame, and an idle frame is the one frame shape in
// which a hidden tab costs nothing at all. It therefore says nothing about how
// much a *rebuild* costs or about which scopes a write reaches;
// TestATapOnASettingsRowDoesNotRebuildTheOtherThreeTabs below is that
// measurement, and it exists because this one passed for a version of this
// program in which every tap rebuilt all four tabs.
func TestTheLongListStaysInsideTheLimitsItsOwnDocumentationStates(t *testing.T) {
	if longListRows > 280 {
		t.Fatalf("the demo's list has %d rows; ui.ListView documents about 280 as the point at "+
			"which a rebuild stops fitting in half a frame on a Raspberry Pi 4", longListRows)
	}
	h := mount(t, geom.Sz(900, 760))
	tabButton(h, "List").Click()

	a := h.App()
	step := func() {
		if err := a.Update(geom.Sz(900, 760)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	if got := testing.AllocsPerRun(50, step); got != 0 {
		t.Errorf("an idle frame of the demo allocated %v times. The long list is past the "+
			"shaping cache cliff ui.ListView documents, or something else in this program "+
			"allocates in the frame path.", got)
	}
}

// TestTheFirstScreenFillsInItsIconsWithinTheUploadBudget is the measurement the
// project plan, section 21, asks for, taken on the screen this demo actually
// opens on rather than on a row of twenty icons in a test fixture.
//
// # Why it is not obvious how many frames this takes
//
// Section 21 records "eight per frame, so three frames for twenty icons", and
// the number of *new* icons on this screen is not twenty. It is whatever the
// selected tab plus the tab bar puts on the screen, at whatever sizes, counted
// per (symbol, device size) pair — the tab bar's four at 24, the icon strip's
// twenty at 22, the three row icons at 22, two chevrons at 16 and the theme
// switch's one at 16. Two of those pairs collide with the strip's, and the
// three hidden tabs contribute nothing because a hidden tab is not painted.
//
// So the answer has to be measured. This test drives the real
// [backend.TextureCache] with its default budget, counts the image operations
// per drawn frame and reports the sequence; what it *asserts* is the property
// that matters, which is that the screen completes and stays complete. An icon
// whose upload was refused has to draw nothing and ask again on the next
// drawn frame, and must never latch into permanent invisibility.
//
// The observed sequence is in the work unit report and is also logged here, so
// that a change in the budget shows up as a different line rather than as a
// silently slower fill.
func TestTheFirstScreenFillsInItsIconsWithinTheUploadBudget(t *testing.T) {
	// This measurement does not need a cold icon mask cache, and it is worth
	// a sentence why, because the first version of it insisted on one and
	// then failed under `go test -count=3`.
	//
	// The masks may well still be in ui's process wide cache from an earlier
	// run of this very test. What is *not* still there is a texture: the
	// handles in that cache belong to the [backend.TextureCache] of the
	// earlier run, a fresh one is created below, and a handle from another
	// cache does not resolve — so every mask on this screen is acquired
	// again here, which is exactly the state a freshly opened window is in.
	// The counter this test reads is therefore the new cache's upload count
	// and never ui's own.
	t.Logf("ui's icon mask cache holds %d masks before this test runs",
		ui.IconCacheStats().Entries)

	h := mount(t, geom.Sz(900, 760))
	// The pictures go through the same texture cache and would be counted as
	// image operations too, so they are taken out of the way first: this test
	// is about the icon budget, and a thumbnail arriving on frame five would
	// otherwise look like an icon that took five frames.
	Pictures = nil
	h.App().Invalidate()
	h.Settle()

	const budget = backend.DefaultUploadsPerFrame
	if budget != 8 {
		t.Fatalf("DefaultUploadsPerFrame is %d; section 21 of the project plan quotes eight "+
			"and the arithmetic below is derived from it", budget)
	}
	tex := backend.NewTextureCache(backend.TextureConfig{})
	h.App().SetImages(tex)

	drawn := make([]int, 0, 10)
	newTextures := make([]int, 0, 10)
	prev := uint64(0)
	for range 10 {
		// BeginFrame and Tick are what the renderer does around a drawn
		// frame, and they are the whole reason the budget is per *drawn*
		// frame; the harness has no renderer, so the test stands in for one.
		tex.BeginFrame()
		h.Frame()
		tex.Tick()
		drawn = append(drawn, drawnImages(h))
		up := tex.Stats().Uploads
		newTextures = append(newTextures, int(up-prev))
		prev = up
	}
	want := drawn[len(drawn)-1]
	total := int(prev)
	t.Logf("from cold: %d icon operations on the settled screen out of %d distinct masks; "+
		"drawn per frame %v; uploaded per frame %v; budget %d",
		want, total, drawn, newTextures, budget)

	// The fixture has to be worth measuring: a screen inside the budget fills
	// in on the first frame and says nothing about it.
	if total <= budget {
		t.Fatalf("the first screen needs only %d masks, which is inside the %d per frame "+
			"budget; this test would measure nothing", total, budget)
	}

	// The budget itself, stated directly: no drawn frame uploads more than it
	// allows. This is on *textures* and not on operations, because two icons
	// showing the same symbol at the same device size share one texture —
	// which is why the two rows of this screen that both end in a chevron
	// contribute two operations and one upload.
	for i, n := range newTextures {
		if n > budget {
			t.Errorf("drawn frame %d uploaded %d masks, above the budget of %d", i+1, n, budget)
		}
	}
	// And the consequence: the screen completes, and on the way there it
	// never goes backwards. An icon whose upload was refused draws nothing
	// and asks again on the next drawn frame; one that is already on the
	// screen stays there.
	for i, n := range drawn {
		if i > 0 && n < drawn[i-1] {
			t.Errorf("frame %d drew %d icons after frame %d drew %d; an icon that was on the "+
				"screen must not disappear", i+1, n, i, drawn[i-1])
		}
	}
	// The arithmetic the project plan, section 21, states: a screen of n new
	// masks needs at least ceil(n / budget) drawn frames.
	least := (total + budget - 1) / budget
	complete := -1
	for i, n := range drawn {
		if n == want {
			complete = i + 1
			break
		}
	}
	switch {
	case complete < 0:
		t.Fatalf("the screen never settled at %d icons: %v", want, drawn)
	case complete < least:
		t.Errorf("the screen was complete after %d drawn frames, fewer than the %d the %d per "+
			"frame budget allows for %d masks", complete, least, budget, total)
	}
}

// drawnImages is the number of image operations in the most recent frame,
// which for this screen is the number of icon masks that were resident when it
// was painted.
func drawnImages(h *gifttest.Harness) int {
	n := 0
	for _, op := range h.Ops() {
		if op.Kind == render.OpImage {
			n++
		}
	}
	return n
}

// TestTheDemoHasNoOverflowAtASmallerWindow is the resize case. A kitchen sink
// is the screen most likely to be laid out at a size nobody tried, and the
// overflow model of the project plan, section 7, exists precisely so that this
// is a number rather than a judgement call.
//
// # Why the narrowest size is 360 and not 480
//
// Because the claim this demo makes about itself is about *width*, and the
// version of this test that stopped at 480 was not testing the claim. Review
// gate 14 ran the same sweep one step narrower and found two overflows at
// 360x640 that had been there all along:
//
//	ui.HStack{index:111}   15.8 px   the Settings "Notifications" row, whose
//	                                 subtitle plus its switch did not fit
//	ui.Row{key:sync}        0.938 px the "Uploading" row, whose 120 px
//	                                 progress bar was 0.938 px too wide
//
// Both were fixed by making the demo fit — a shorter subtitle and a 90 px bar
// — rather than by narrowing the test, because the whole point of a kitchen
// sink is that it is the composition most likely to be laid out at a size
// nobody tried. 360 logical pixels is the narrowest phone-shaped window worth
// supporting; below it the tab bar's four items stop being a sensible shape
// and this demo does not claim to work there.
//
// # And the target
//
// 1920x1080 is the kiosk panel of the project plan, section 13, and it is in
// the sweep for the same reason the small sizes are: it is the size this
// program is actually for and it was not being checked either.
//
// The sweep runs at three densities. The layout is logical at every density —
// the project plan, section 18 — so all three must produce the same answer,
// and a difference between them would be a density leaking into a measurement.
func TestTheDemoHasNoOverflowAtASmallerWindow(t *testing.T) {
	sizes := []geom.Size{
		geom.Sz(1920, 1080), // the kiosk panel
		geom.Sz(900, 760),   // the window this demo opens
		geom.Sz(640, 480),
		geom.Sz(480, 800),
		geom.Sz(360, 640), // the narrowest this demo claims to work at
	}
	// One flat loop over the densities, for the reason
	// TestEveryTabOfTheDemoBuildsAndTheHiddenOnesAreNotDrawn gives: a
	// [gift.App] belongs to the goroutine that created it, so no t.Run.
	for _, d := range []float64{1, 1.5, 2} {
		var err error
		if Pictures, err = Samples(t.TempDir()); err != nil {
			t.Fatalf("generating the sample pictures: %v", err)
		}
		h := gifttest.New(t, gifttest.Options{
			Theme:   ui.LightTheme(),
			Font:    ui.MustFont(ui.FontQuery{Family: inter.Family}),
			Size:    geom.Sz(900, 760),
			Density: d,
			Root:    Screen,
		})
		for _, s := range sizes {
			h.Resize(s)
			for _, tab := range []string{"Home", "Settings", "List", "Form"} {
				tabButton(h, tab).Click()
				if dg := h.Diagnostics(); dg.OverflowNodes != 0 {
					t.Errorf("at density %v, %v, the %s tab: %d node(s) overflow by %g "+
						"logical pixels in total.\n%s",
						d, s, tab, dg.OverflowNodes, dg.OverflowExtent, h.Dump())
				}
			}
		}
	}
	Pictures = nil
}

// goldenSize is the viewport every golden in this file is taken at. It is the
// window the demo opens, so a reviewer comparing a golden with the running
// program compares the same picture.
var goldenSize = geom.Sz(900, 760)

// warm renders a few throwaway frames before a golden is taken.
//
// The texture upload budget of the project plan, section 21, is per *drawn*
// frame and this screen needs more than one budget's worth of icon masks — the
// measurement is in TestTheFirstScreenFillsInItsIconsWithinTheUploadBudget
// above. A golden taken on the first frame would therefore be a golden of a
// screen with twenty symbols missing, and it would pass for ever.
func warm(h *gifttest.Harness) {
	// A transition first. Since the navigation containers of this demo move
	// rather than cut — see [gift.TransitionSpec] — a golden taken in the
	// frame after a tab switch or after an alert was opened would be a
	// picture of a screen that is halfway in, and would therefore depend on
	// how many frames the harness happened to run. Moving the clock past the
	// end of the movement is what makes the picture the resting state again.
	h.Advance(ui.ControlAnimation + 32*time.Millisecond)
	for range 6 {
		h.Warm()
	}
}

// TestEveryTabLooksTheWayItLooks is the regression that three defects got past
// because it did not exist.
//
// # Why a golden and not an assertion
//
// Every one of the three defects a person found on a real screen in two
// minutes is a statement about where a pixel is and what colour it has: a
// theme switch that left memoised subtrees in the old palette, a nested list
// whose separators were painted three hundred pixels above its rows and
// through the page header, and a slider that drew no knob. Fourteen review
// gates and a full structural test suite saw none of them, and that is not
// because those gates were careless — it is because a structural assertion
// cannot see any of it. Nothing in steps 9a, 9b or 9c ever rendered a pixel.
//
// So: four tabs, two themes, eight files that a reviewer can open. The
// structural tests above still say what is *required*; this says what it looks
// like, which is the only thing that would have caught any of the three.
func TestEveryTabLooksTheWayItLooks(t *testing.T) {
	for _, theme := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		// One flat loop and no t.Run, for the reason
		// TestEveryTabOfTheDemoBuildsAndTheHiddenOnesAreNotDrawn gives: a
		// gift.App belongs to the goroutine that created it.
		h := mountTheme(t, goldenSize, theme.t)
		for _, tab := range []string{"Home", "Settings", "List", "Form"} {
			tabButton(h, tab).Click()
			warm(h)
			h.AssertGolden("kitchensink-" + tab + "-" + theme.name)
		}
	}
}

// TestEveryTabOfTheDemoPaintsAWindowBackgroundWithNoHolesInIt is the pixel
// gate on the one obligation gift puts on an application: the window
// background.
//
// # What it would have caught
//
// This demo shipped with no background at all. Thirty to forty per cent of the
// pixels of every one of its four screens were {0, 0, 0, 0} — the gaps between
// the cards, read back from the framebuffer of the running program — and the
// eight goldens next door were all green throughout, because the test harness
// filled its canvas with a colour of its own before rendering. A golden of a
// scene with holes in it is a perfectly stable golden; that is precisely why
// this assertion cannot be a golden and has to be its own.
//
// It runs over both themes and all four tabs, because a background is painted
// by one line in one place and a screen that escapes that line escapes it
// completely.
func TestEveryTabOfTheDemoPaintsAWindowBackgroundWithNoHolesInIt(t *testing.T) {
	for _, theme := range []struct {
		name string
		t    ui.Theme
	}{{"light", ui.LightTheme()}, {"dark", ui.DarkTheme()}} {
		h := mountTheme(t, goldenSize, theme.t)
		for _, tab := range []string{"Home", "Settings", "List", "Form"} {
			tabButton(h, tab).Click()
			warm(h)
			h.AssertOpaque()
		}
	}
}

// TestTheThemeSwitchRebuildsEveryColourOnTheScreen replaces a test that
// counted fills by colour, and it is worth recording why that one was not good
// enough, because its own author said so in his report before the defect it
// missed reached a user: counting fills "is a proxy that would pass if the
// surface colour were applied to the wrong nodes".
//
// This compares the whole frame after a switch made at run time, on the tab
// the switch is on and on a tab that was built before it.
//
// # Why it is not compared against the born-light golden next door
//
// That was the first shape of this test, and it is the stronger claim — "a
// theme changed at run time is indistinguishable from the theme having been
// there all along" — but the two frames differ for a reason that has nothing
// to do with colour: hover and focus are part of the picture, the harness
// clicks by pressing at a coordinate and leaves the pointer and the focus
// there, and the two tests arrive here by different routes. Equalising that
// needs another click, and another click rebuilds the root — which is the very
// thing this test must not do afterwards, because a root rebuild repaints
// every colour and would hide the defect.
//
// So this has goldens of its own, and the claim that the *mechanism* reaches a
// subtree behind a rebuild boundary is made where it can be made without a
// window full of interaction state:
// TestAMemoisedSubtreeIsRepaintedByAThemeSwitch in the ui package.
func TestTheThemeSwitchRebuildsEveryColourOnTheScreen(t *testing.T) {
	opts := mountOptions(t, goldenSize, ui.DarkTheme())
	// Nothing has to be said about a clear colour any more, and that is the
	// point of this paragraph. The harness contributes no pixel; the demo
	// paints its own window background through [ui.Window]; so the gaps
	// between the cards are part of what the theme switch reaches, and the
	// golden below compares a screen that is entirely the application's.
	h := gifttest.New(t, opts)
	App = h.App()
	t.Cleanup(func() { App = nil })

	// The switch offers the theme it would move to, so in the dark theme it
	// says "Light".
	h.First(gifttest.ByText("Light")).Click()
	if ui.CurrentTheme().IsDark() {
		t.Fatal("pressing the switch did not install the light theme; the fixture is wrong")
	}
	warm(h)
	h.AssertGolden("kitchensink-after-switch-Home")

	// And the same for a tab that was built before the switch and is not the
	// one the switch is on.
	tabButton(h, "Settings").Click()
	warm(h)
	h.AssertGolden("kitchensink-after-switch-Settings")
}

// TestTheAlertLooksLikeAnAlert is the modal, which has no golden anywhere else
// in the repository and is the one composition where the scrim, the card and
// the two buttons have to be looked at together.
func TestTheAlertLooksLikeAnAlert(t *testing.T) {
	h := mountTheme(t, goldenSize, ui.LightTheme())
	h.First(gifttest.ByText("Reset everything")).Click()
	h.AssertExists(gifttest.ByText("Reset everything?"))
	warm(h)
	h.AssertGolden("kitchensink-alert")
}

// TestTheKeyboardLooksLikeAKeyboard is the kiosk case of step 8: a focused
// text field with gift's own on-screen keyboard up, which is the one screen in
// this program that is composed of three overlapping layers.
func TestTheKeyboardLooksLikeAKeyboard(t *testing.T) {
	// The on-screen keyboard is a process wide policy, so it is installed for
	// the duration of this test and restored afterwards, exactly as main
	// installs it for the duration of the program.
	prev := ui.OnScreenKeyboardEnabled()
	ui.SetOnScreenKeyboard(nil, true)
	t.Cleanup(func() { ui.SetOnScreenKeyboard(nil, prev) })

	h := mountTheme(t, goldenSize, ui.LightTheme())
	tabButton(h, "Form").Click()
	h.Find(gifttest.ByKey("name")).Click()
	h.AssertFocus(gifttest.ByKey("name"))
	// The keyboard collapses to nothing at all while no field has the focus,
	// so a positive height is the evidence that it came up. The keys
	// themselves are painted by the node and are not nodes of their own,
	// which is why this is a height and not a selector.
	if kb := h.Find(gifttest.ByType("ui.OnScreenKeyboard")).Bounds(); !(kb.Height() > 0) {
		t.Fatalf("the field took the focus and the keyboard is %v tall; the fixture is wrong", kb)
	}
	warm(h)
	h.AssertGolden("kitchensink-keyboard")
}

// TestATapOnASettingsRowDoesNotRebuildTheOtherThreeTabs is the scoping
// property the root of this program is arranged around; see [Screen].
//
// # Why this is a rebuild measurement and not an idle one
//
// TestTheLongListStaysInsideTheLimitsItsOwnDocumentationStates above measures
// an *idle* frame, and an idle frame is the one frame shape in which a hidden
// tab genuinely costs nothing: it is not painted, nothing is dirty, and no
// view function runs at all. That test therefore passes whether the tabs are
// scoped or not, and it did pass for the version of this program in which a
// single tap rebuilt all four tabs. The cost of a wrongly scoped write is only
// visible on the frame the write causes.
//
// # What it measures, and the subtraction it has to make
//
// Three quantities on one harness:
//
//   - a full root rebuild, driven directly;
//   - a tap on the Settings "Nudge it along" row, which writes one float that
//     only the Settings tab shows;
//   - a tap on the inert "Volume" row next to it, which writes nothing.
//
// The third is the subtraction, and it is not a fudge. A tap through the
// harness costs a fixed amount that has nothing to do with this program: the
// pointer down and up, the hover and press bookkeeping and the repaint they
// enrol. What is left after the subtraction is the rebuild the write caused,
// and that is the number this program is responsible for.
//
// Both taps are aimed at a coordinate taken once, before the measurement, so
// that neither pays for a selector run inside the loop; see the note on
// tapInert for the second reason that matters here. The constant is
// consequently small — it used to be about 1400 allocations of node lookup,
// and is now close to nothing — which makes the subtraction less important
// than it was and the comparison sharper.
//
// Observed at 900x760:
//
//	full root rebuild                6255 allocations
//	tap on the inert Volume row         0
//	tap on the Nudge row              485
//	                                  485 of rebuild
//
// Moving the six Read calls of the Settings tab back to the root scope — which
// is exactly the regression this guards, and is what this file used to do —
// makes the tap pay the root figure on top of the constant.
func TestATapOnASettingsRowDoesNotRebuildTheOtherThreeTabs(t *testing.T) {
	h := mount(t, geom.Sz(900, 760))
	tabButton(h, "Settings").Click()
	a := h.App()

	rootRebuild := func() {
		a.Invalidate()
		if err := a.Update(geom.Sz(900, 760)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	nudgeAt := h.Find(gifttest.ByKey("nudge")).Center()
	tapNudge := func() { h.ClickAt(nudgeAt) }
	// The Volume row has no OnTap, so this is the harness constant and
	// nothing else; see above.
	//
	// It is [gifttest.Harness.ClickAt] and not Node.Click, and the reason is
	// worth recording. A row with no action is not interactive, so Click aims
	// at the nearest interactive ancestor — which is the whole scroll
	// container — and then refuses, correctly, because the centre of the
	// container is covered by whatever row happens to lie there. That made
	// this measurement depend on the vertical layout of the Settings tab: it
	// started failing the moment the volume slider stopped being zero pixels
	// tall. The coordinate of the row itself is the thing this test means.
	volumeAt := h.Find(gifttest.ByKey("volume")).Center()
	tapInert := func() { h.ClickAt(volumeAt) }

	// Warm every path: the first rebuild of anything grows scratch buffers and
	// shapes strings, and neither is the steady state being measured.
	for range 8 {
		rootRebuild()
		tapNudge()
		tapInert()
	}
	root := testing.AllocsPerRun(20, rootRebuild)
	inert := testing.AllocsPerRun(20, tapInert)
	tap := testing.AllocsPerRun(20, tapNudge)
	rebuild := tap - inert
	t.Logf("full root rebuild: %.0f allocations; tap on the Nudge row: %.0f; "+
		"tap on the inert Volume row: %.0f; rebuild attributable to the write: %.0f",
		root, tap, inert, rebuild)

	if root < 1000 {
		t.Fatalf("a full root rebuild of this demo allocates only %.0f times; the fixture is "+
			"too small for this comparison to mean anything", root)
	}
	if rebuild < 0 {
		t.Fatalf("the tap on the Nudge row cost %.0f allocations and the tap on the inert row "+
			"%.0f; the subtraction is meaningless and this fixture needs looking at", tap, inert)
	}
	if rebuild*4 > root {
		t.Errorf("a tap on the Settings Nudge row causes %.0f allocations of rebuild, against "+
			"the %.0f of a full rebuild of all four tabs. That row writes one float that only "+
			"the Settings tab shows, so it must not cost a rebuild of the whole window. Check "+
			"that the Read calls in state.tabComponent have not migrated back to the root "+
			"scope in Screen.", rebuild, root)
	}
}
