package ui_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// The allocation contract of the project plan, section 11, for the navigation
// containers.
//
// There is one thing here that the other benchmarks in this package do not
// have to think about, and it is the whole reason these exist: a tab bar keeps
// every tab mounted, so the steady state frame path walks a tree that is
// several times larger than what is on the screen. The layout pass skips a
// clean subtree and the paint pass returns at the hidden node, so the cost
// should be the *visible* tree and not the mounted one — and an implementation
// that got that wrong would still be correct, only slow, which is exactly the
// kind of defect a benchmark is for and a behaviour test is not.

// navTree is a three tab application with a navigation stack in the first tab
// and a modal host around the lot. Every tab holds a column of forty rows, so
// two thirds of the mounted tree is hidden at any moment.
func navTree(*gift.Context) gift.View {
	tab := func(prefix string) gift.View {
		out := make([]gift.View, 40)
		for i := range out {
			out[i] = ui.HStack(
				ui.Box().Frame(24, 24).Background(ui.RGB(40, 60, 90)).CornerRadius(4),
				ui.Text(prefix+strconv.Itoa(i)),
			).Gap(8).Key(strconv.Itoa(i))
		}
		return ui.VStack(out...).Gap(2).Padding(8)
	}
	return ui.Modal(
		ui.TabBar(0, func(int) {},
			ui.Tab("One", ui.Symbol{}, ui.NavigationStack(nil,
				ui.Screen("Root", tab("root ")),
				ui.Screen("Detail", tab("detail ")),
			)),
			ui.Tab("Two", ui.Symbol{}, tab("two ")),
			ui.Tab("Three", ui.Symbol{}, tab("three ")),
		),
		nil,
	)
}

// navModalTree is [navTree] with the alert open, which is the case where
// nothing is hidden and everything is drawn.
func navModalTree(ctx *gift.Context) gift.View {
	inner := navTree(ctx)
	return ui.Modal(inner, ui.Alert("Careful", "Really?",
		ui.AlertCancel("Cancel", nil),
		ui.AlertAction("OK", nil),
	))
}

// navApp mounts root through the harness — which pins the theme and the font
// for the duration of the test and puts them back afterwards — and hands back
// the App underneath, so that a measurement can drive Update and Paint itself
// without any of these files touching process wide state.
func navApp(tb testing.TB, root func(*gift.Context) gift.View) *gift.App {
	tb.Helper()
	h := gifttest.New(tb, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(tb),
		Size:  geom.Sz(640, 480),
		Root:  root,
	})
	return h.App()
}

// TestTheNavigationFramePathIsAllocationFree is the assertion behind the two
// benchmarks below; they are what a measurement quotes.
func TestTheNavigationFramePathIsAllocationFree(t *testing.T) {
	for _, tc := range []struct {
		name string
		root func(*gift.Context) gift.View
	}{
		{"with a modal closed", navTree},
		{"with a modal open", navModalTree},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := navApp(t, tc.root)
			step := func() {
				if err := a.Update(geom.Sz(640, 480)); err != nil {
					t.Fatal(err)
				}
				a.Paint()
			}
			for range 16 {
				step()
			}
			if n := a.Diagnostics().LiveNodes; n < 400 {
				t.Fatalf("the fixture has only %d nodes, the measurement would be meaningless", n)
			}
			if got := testing.AllocsPerRun(200, step); got != 0 {
				t.Fatalf("the idle frame path allocated %v times per run, want 0", got)
			}
		})
	}
}

// TestAHiddenTabCostsNothingToPaint measures the claim [ui.TabBarView] makes
// about the frame cost, in the only currency the display list has: a hidden
// tab contributes no operations, so a three tab application draws what one tab
// draws.
func TestAHiddenTabCostsNothingToPaint(t *testing.T) {
	count := func(root func(*gift.Context) gift.View) int {
		a := navApp(t, root)
		if err := a.Update(geom.Sz(640, 480)); err != nil {
			t.Fatal(err)
		}
		return a.Paint().Len()
	}
	one := count(func(*gift.Context) gift.View {
		return ui.TabBar(0, nil, ui.Tab("One", ui.Symbol{}, heavyColumn("a ")))
	})
	three := count(func(*gift.Context) gift.View {
		return ui.TabBar(0, nil,
			ui.Tab("One", ui.Symbol{}, heavyColumn("a ")),
			ui.Tab("Two", ui.Symbol{}, heavyColumn("b ")),
			ui.Tab("Three", ui.Symbol{}, heavyColumn("c ")),
		)
	})
	// Three bar items instead of one is a handful of extra operations; the
	// forty hidden rows twice over would be hundreds.
	if three-one > 40 {
		t.Fatalf("three tabs draw %d operations where one draws %d. The extra %d is far "+
			"more than the two additional bar items can account for, so the hidden tabs "+
			"are being painted", three, one, three-one)
	}
}

func heavyColumn(prefix string) gift.View {
	out := make([]gift.View, 40)
	for i := range out {
		out[i] = ui.Box().
			Frame(100, 8).
			Background(ui.RGB(10, 20, 30)).
			CornerRadius(2).
			Key(prefix + strconv.Itoa(i))
	}
	return ui.VStack(out...).Gap(1)
}

// BenchmarkNavigationIdleFrame is the steady state of a kiosk sitting on one
// tab of a three tab application: nothing is dirty, everything is mounted, and
// two thirds of it is hidden.
func BenchmarkNavigationIdleFrame(b *testing.B) {
	a := navApp(b, navTree)
	step := func() {
		if err := a.Update(geom.Sz(640, 480)); err != nil {
			b.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

// BenchmarkNavigationIdleFrameWithAModal is the same frame with the alert
// open, where the scrim and the card are drawn on top of an application that
// is still fully drawn itself. It is the honest measurement of what a modal
// costs, and the difference from the benchmark above is that cost.
func BenchmarkNavigationIdleFrameWithAModal(b *testing.B) {
	a := navApp(b, navModalTree)
	step := func() {
		if err := a.Update(geom.Sz(640, 480)); err != nil {
			b.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

// BenchmarkTabSwitch is the cost of the interaction itself: the rebuild of the
// bar, the two layers changing their Hidden flag, and the relayout and repaint
// that follow. It is emphatically *not* allocation free — a build allocates a
// node per container, which is the design — and it is measured so that the
// number is known rather than assumed.
func BenchmarkTabSwitch(b *testing.B) {
	sel := 0
	root := func(*gift.Context) gift.View {
		return ui.TabBar(sel, func(i int) { sel = i },
			ui.Tab("One", ui.Symbol{}, heavyColumn("a ")),
			ui.Tab("Two", ui.Symbol{}, heavyColumn("b ")),
		)
	}
	a := navApp(b, root)
	now := time.Duration(0)
	step := func() {
		sel = 1 - sel
		a.Invalidate()
		now += 16 * time.Millisecond
		a.BeginInput(now)
		if err := a.Update(geom.Sz(640, 480)); err != nil {
			b.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}
