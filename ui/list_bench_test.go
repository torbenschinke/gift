package ui_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// The cost of a [ui.ListView], which is the whole of the argument for not
// virtualising it.
//
// A list builds every row. That is simple, it has none of the recycling defect
// classes, and it is wrong above a certain number of rows. This file is what
// turns "a certain number" into a number, and
// TestTheMeasuredCostOfAListRowStillSupportsTheNumberInTheGodoc is what stops
// that number from quietly becoming a lie.

// listRows is the fixture of the *cost* measurements: n rows of the shape a
// settings screen actually has, which is a title, a subtitle and a trailing
// value. A fixture of bare titles would measure a third of the text this
// component really lays out and would make the limit look twice as generous
// as it is.
//
// # Why the labels repeat every sixty-four rows
//
// Because the cost per row and the shaping cache are two different things and
// a fixture that mixed them measures neither. gift's shaping cache is keyed on
// the string, so a vocabulary of sixty-four titles is sixty-four entries
// whatever n is, and the rebuild cost this fixture reports is the cost of
// building, laying out and painting the rows — a cache *hit* per label, which
// is what a real rebuild is, because the strings of a settings screen do not
// change when one switch does.
//
// It also makes the measurement repeatable. The cache is process wide and
// survives between tests and between `go test -count` iterations; with a
// distinct string per row, a second iteration ran against a cache that the
// first had already filled and reported a cost per row seven times higher.
// That is a real property of the text layer and it is measured deliberately,
// in [uniqueListRows] and the cliff test that uses it, rather than leaking
// into every other number in this file.
func listRows(n int) []gift.View {
	out := make([]gift.View, n)
	for i := range n {
		s := strconv.Itoa(i % 64)
		out[i] = ui.Row("Setting " + s).
			Subtitle("what this setting does").
			Value("value " + s).
			Key(strconv.Itoa(i))
	}
	return out
}

// uniqueRun makes every string [uniqueListRows] produces new to the process,
// so that the cliff test measures a cache filling up rather than a cache that
// a previous iteration already filled with the same strings.
var uniqueRun int

// uniqueListRows is the fixture of the shaping cache measurement: n rows whose
// every label is a string this process has never shaped before.
func uniqueListRows(n int) []gift.View {
	uniqueRun++
	p := "r" + strconv.Itoa(uniqueRun) + "-"
	out := make([]gift.View, n)
	for i := range n {
		s := p + strconv.Itoa(i)
		out[i] = ui.Row("Setting " + s).
			Subtitle("what this setting does").
			Value("value " + s).
			Key(strconv.Itoa(i))
	}
	return out
}

// listApp mounts a list of n rows through the harness, which pins the theme
// and the font for the duration, and returns the App underneath so that a
// measurement can drive Update and Paint itself.
//
// The list sits in a scroll container, because that is where a long list lives
// and because it is the composition that measures the list with an unbounded
// main axis — a list inside a fixed frame would be clipped by the constraints
// and the rows below the fold would still be laid out, which is the cost this
// file is about.
func listApp(tb testing.TB, n int) *gift.App { return listAppOf(tb, listRows(n)) }

// listAppOf is [listApp] over an explicit set of rows.
func listAppOf(tb testing.TB, rows []gift.View) *gift.App {
	tb.Helper()
	h := gifttest.New(tb, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(tb),
		Size:  geom.Sz(480, 800),
		Root: func(*gift.Context) gift.View {
			return ui.VScroll(ui.List(rows...).Key("list")).Key("scroll").Flex(1)
		},
	})
	return h.App()
}

var listSizes = []int{20, 100, 400, 700, 1000}

// BenchmarkListRebuild is the number the limit in [ui.ListView]'s
// documentation is derived from: a frame in which the whole list is dirty, so
// every row is built, laid out and painted again.
//
// It is emphatically *not* allocation free, and that is the design and not a
// defect: a build allocates a node per container, and a row is several
// containers. What the benchmark is for is the slope — the cost per row — and
// whether it is linear.
func BenchmarkListRebuild(b *testing.B) {
	for _, n := range listSizes {
		b.Run(strconv.Itoa(n)+" rows", func(b *testing.B) {
			a := listApp(b, n)
			step := func() {
				a.Invalidate()
				if err := a.Update(geom.Sz(480, 800)); err != nil {
					b.Fatal(err)
				}
				a.Paint()
			}
			for range 8 {
				step()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				step()
			}
		})
	}
}

// BenchmarkListIdleFrame is the cost a list actually pays most of the time:
// nothing is dirty, no view function runs, the layout pass skips every clean
// subtree, and the frame is a paint.
//
// It has to be zero allocations. That is the contract of the project plan,
// section 11, and it is the one this component would break by, for instance,
// building its separator geometry in the painter.
func BenchmarkListIdleFrame(b *testing.B) {
	for _, n := range listSizes {
		b.Run(strconv.Itoa(n)+" rows", func(b *testing.B) {
			a := listApp(b, n)
			step := func() {
				if err := a.Update(geom.Sz(480, 800)); err != nil {
					b.Fatal(err)
				}
				a.Paint()
			}
			for range 8 {
				step()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				step()
			}
		})
	}
}

// BenchmarkListScrollFrame is a frame in the middle of a fling: the offset
// moves, so the scroll container's transform changes and everything under it
// is repainted, and still nothing is built or laid out.
func BenchmarkListScrollFrame(b *testing.B) {
	a := listApp(b, 400)
	now := time.Duration(0)
	off := 0.0
	step := func() {
		off += 7
		if off > 4000 {
			off = 0
		}
		now += 16 * time.Millisecond
		a.BeginInput(now)
		if err := a.Update(geom.Sz(480, 800)); err != nil {
			b.Fatal(err)
		}
		a.Paint()
	}
	for range 8 {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

// TestTheListFramePathIsAllocationFree is the assertion behind
// BenchmarkListIdleFrame; the benchmark is what a measurement quotes.
func TestTheListFramePathIsAllocationFree(t *testing.T) {
	a := listApp(t, 200)
	step := func() {
		if err := a.Update(geom.Sz(480, 800)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		step()
	}
	if n := a.Diagnostics().LiveNodes; n < 1000 {
		t.Fatalf("the fixture has only %d nodes, the measurement would be meaningless", n)
	}
	if got := testing.AllocsPerRun(100, step); got != 0 {
		t.Fatalf("the idle frame path of a 200 row list allocated %v times per run, want 0", got)
	}
}

// TestTheMeasuredCostOfAListRowStillSupportsTheNumberInTheGodoc is the guard
// on the limit [ui.ListView] documents.
//
// # What it does and what it deliberately does not
//
// It does not check the limit. The limit is a statement about a Raspberry
// Pi 4, this test runs wherever the test suite runs, and a threshold in
// wall clock time on unknown hardware is a flaky test rather than a
// measurement.
//
// What it checks is the thing the limit was computed *from*: the cost of a
// rebuild is linear in the number of rows, and the cost per row has not moved
// by more than a factor of four since the number in the documentation was
// derived. Both failures the guard exists for are covered by that. A change
// that makes a row twice as expensive — a second text node, a painter where
// there was none, a colour resolved per row per frame — makes the documented
// limit wrong by that factor, and the documentation is the only place anybody
// would find out. And a change that makes the cost *non* linear, which is what
// an accidental quadratic in the separator bookkeeping would look like, makes
// the whole shape of the claim wrong.
//
// The generous factor of four is the honest one for a wall clock measurement
// inside `go test`: the machines that run this range from a laptop on battery
// to a loaded CI box. A regression worth catching here is an algorithmic one,
// and those are not factors of two.
//
// # Why the larger size is 700 and not 1000
//
// Because at about 750 rows of this shape the shaping cache overflows and the
// cost per row jumps by a factor of ten, which is the second limit
// [ui.ListView] documents and is measured by
// TestTheLimitOfTheShapingCacheIsWhereTheDocumentationSaysItIs below. A
// linearity check that straddled that cliff would fail every time and would be
// measuring the text layer rather than this one. Both sizes here are on the
// flat side of it on purpose, and the cliff has a test of its own.
func TestTheMeasuredCostOfAListRowStillSupportsTheNumberInTheGodoc(t *testing.T) {
	if testing.Short() {
		t.Skip("a timing measurement")
	}
	cost := func(n int) time.Duration {
		a := listApp(t, n)
		step := func() {
			a.Invalidate()
			if err := a.Update(geom.Sz(480, 800)); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
		for range 8 {
			step()
		}
		// The minimum of several runs rather than the mean: a scheduler
		// hiccup can only make a sample slower, so the smallest one is the
		// closest to the cost of the work itself.
		best := time.Duration(1 << 62)
		for range 9 {
			start := time.Now()
			for range 4 {
				step()
			}
			if d := time.Since(start) / 4; d < best {
				best = d
			}
		}
		return best
	}

	small, large := cost(100), cost(700)
	perRowSmall := float64(small) / 100
	perRowLarge := float64(large) / 700
	t.Logf("rebuild of 100 rows: %v (%.0f ns/row); of 700 rows: %v (%.0f ns/row)",
		small, perRowSmall, large, perRowLarge)

	// Linearity, with a threshold of two rather than the one and a half it
	// would take in a release build, and the difference is measured rather
	// than padded.
	//
	// Without a build tag the cost per row is flat: 2274 ns at 100 rows and
	// 2330 at 700, a ratio of 1.02. Under giftdebug it is 2500 and 3900, a
	// ratio of 1.57, consistently over five runs. That extra term is the debug
	// build's own — the per node diagnosis and the allocation it formats are
	// exactly what the frame path contract of the project plan, section 11,
	// excludes from a release build — and it is not this component's to
	// flatten. The limit in the documentation is a release build number.
	//
	// Two still catches what this check is for. A quadratic term in the
	// separator bookkeeping would show up as a ratio of seven over this range,
	// not of one and a half.
	if perRowLarge > 2*perRowSmall {
		t.Errorf("a row costs %.0f ns in a 100 row list and %.0f ns in a 700 row list. "+
			"The cost of a rebuild is supposed to be linear in the row count, and the limit "+
			"documented on ui.ListView extrapolates on that assumption.",
			perRowSmall, perRowLarge)
	}

	// The absolute figure the documentation was derived from, on the machine
	// this was written on: about 2.8 µs per row. Four times that is the alarm.
	//
	// Not under the race detector, which rewrites every memory access and
	// measured this fixture at 37 µs per row — thirteen times the release
	// figure. That is the instrumentation, exactly as [raceEnabled] says for
	// the allocation contract, and a threshold wide enough to accommodate it
	// would catch nothing in the build the number is about. The linearity
	// check above still runs there, because a quadratic term is a quadratic
	// term whatever the constant is.
	const derivedPerRow = 2800.0 // nanoseconds
	if !raceEnabled && perRowLarge > 4*derivedPerRow {
		t.Errorf("a row now costs %.0f ns to rebuild, against the %.0f ns the row count limit "+
			"documented on ui.ListView was derived from. Either this machine is much slower "+
			"than the one the number came from, or a row got more expensive — in which case "+
			"the limit in the documentation is now wrong and has to be re-derived.",
			perRowLarge, derivedPerRow)
	}
}

// TestALongListDoesNotOverflowAnything is the boring companion to the cost
// measurement, and it is here because a list of a thousand rows is exactly the
// size at which a layout mistake stops being visible in a screenshot.
//
// A scroll container measures its content with an unbounded main axis, so a
// tall list is not an overflow — see the layouter of a scroll node — and
// [gifttest.Harness.AssertNoOverflow] must stay quiet. If it did not, the one
// diagnostic that says "a container in this scene is lying about its size"
// would be useless in every application that has a list in it.
func TestALongListDoesNotOverflowAnything(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(480, 800),
		View: ui.VScroll(ui.List(listRows(1000)...).Key("list")).Key("scroll").Flex(1),
	})
	h.AssertNoOverflow()
	// And the list really is taller than the viewport, so the assertion above
	// is about a scrollable list and not about one that happened to fit.
	if got := h.Find(gifttest.ByKey("list")).Bounds().Height(); got < 800 {
		t.Fatalf("the fixture list is only %v tall; it fits and proves nothing", got)
	}
}

// TestTheLimitOfTheShapingCacheIsWhereTheDocumentationSaysItIs measures the
// second and sharper of the two limits [ui.ListView] documents: the point at
// which a list has more distinct labels than gift's shaping cache holds, and
// every frame starts re-shaping all of them.
//
// # Why this is a test and not a note
//
// Because it is the limit that bites first on a kiosk and it is completely
// invisible until it is measured. Nothing looks wrong: the list is correct,
// the layout is correct, no diagnostic fires, and the frame path — which the
// project plan, section 11, requires to allocate nothing — quietly starts
// allocating megabytes. The only symptom is that the device is slow, and the
// last place anybody would look is a list that is not being rebuilt.
//
// # What it asserts
//
// Two things, in both directions, because each alone would be satisfied by a
// broken measurement. That a list comfortably below the cliff is genuinely
// allocation free in its idle frame — otherwise "the cliff" would just be
// "this component allocates". And that the cliff exists somewhere below a size
// this documentation calls unusable, so that the number in the godoc is not a
// story about a machine nobody has.
//
// It reports the size it found rather than pinning one. The exact row count
// depends on the font, on the strings and on gift's cache budget, none of
// which belong to this component; what belongs to this component is the
// statement that a long enough List falls off it.
func TestTheLimitOfTheShapingCacheIsWhereTheDocumentationSaysItIs(t *testing.T) {
	if testing.Short() {
		t.Skip("builds several large lists")
	}
	idleAllocs := func(n int) float64 {
		// Every label new to the process, which is what fills a cache that is
		// keyed on the string; see [uniqueListRows].
		a := listAppOf(t, uniqueListRows(n))
		step := func() {
			if err := a.Update(geom.Sz(480, 800)); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
		for range 16 {
			step()
		}
		return testing.AllocsPerRun(20, step)
	}

	// The flat side. A hundred rows is two hundred labels, a small fraction of
	// the cache, and its idle frame must cost nothing at all whatever else
	// this process has shaped — the cache evicts the least recently used, and
	// these two hundred are touched every frame.
	if got := idleAllocs(100); got != 0 {
		t.Fatalf("the idle frame of a 100 row list allocated %v times per run. That is far "+
			"below every limit ui.ListView documents; if it allocates here, the cliff "+
			"described there is not the thing this test is finding.", got)
	}

	// And the cliff. The search is coarse on purpose: the claim is "somewhere
	// around this order of magnitude", not a pinned row count, and the exact
	// number depends on the font, on the strings and on gift's cache budget —
	// none of which belong to this component.
	const unusable = 4000
	for _, n := range []int{400, 600, 800, 1200, 2000, unusable} {
		got := idleAllocs(n)
		t.Logf("idle frame of a %d row list of labels new to the process: %v allocations", n, got)
		if got > 0 {
			if n > 2000 {
				t.Errorf("the shaping cache only overflowed at %d rows. ui.ListView documents "+
					"the cliff at about 750; the number is stale and has to be re-derived.", n)
			}
			return
		}
	}
	t.Errorf("no size up to %d rows made the idle frame allocate. ui.ListView documents a hard "+
		"cliff at about 750 rows where the shaping cache overflows and every frame re-shapes "+
		"every label; that limitation appears to be gone, and the documentation now overstates "+
		"the cost of a long list.", unusable)
}

// BenchmarkListInAHiddenTab is what a hidden tab really costs on a frame that
// rebuilds, as opposed to on an idle one.
//
// [ui.ListView]'s documentation used to present the hidden-tab case as relief:
// such a list "is not painted, so it escapes the cliff as well as the paint:
// only its build and layout are paid". Build and layout are most of the cost,
// and this benchmark is the number that says so. The fixture is a two tab
// [ui.TabBar] whose *hidden* tab holds the rows and whose visible tab holds one
// short label, rebuilt from the root every iteration; the difference between
// the zero row case and the others is the whole price of the invisible tab.
//
// The right conclusion is not "do not use a tab bar". It is that a write must
// be scoped to the component that shows it, so that the frame which rebuilds
// the visible tab does not also rebuild the hidden ones;
// cmd/example-kitchensink is arranged that way and
// TestATapOnASettingsRowDoesNotRebuildTheOtherThreeTabs there is the guard.
func BenchmarkListInAHiddenTab(b *testing.B) {
	for _, n := range []int{0, 20, 100, 200} {
		b.Run(strconv.Itoa(n)+" hidden rows", func(b *testing.B) {
			rows := listRows(n)
			h := gifttest.New(b, gifttest.Options{
				Theme: ui.LightTheme(),
				Font:  loadTestFont(b),
				Size:  geom.Sz(480, 800),
				Root: func(*gift.Context) gift.View {
					return ui.TabBar(0, nil,
						ui.Tab("Visible", ui.Symbol{}, ui.Text("nothing to see").Key("label")),
						ui.Tab("Hidden", ui.Symbol{},
							ui.VScroll(ui.List(rows...).Key("list")).Key("scroll").Flex(1)),
					)
				},
			})
			a := h.App()
			step := func() {
				a.Invalidate()
				if err := a.Update(geom.Sz(480, 800)); err != nil {
					b.Fatal(err)
				}
				a.Paint()
			}
			for range 8 {
				step()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				step()
			}
		})
	}
}
