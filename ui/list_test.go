package ui_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"
)

// listHarness mounts v with the pinned theme and font, so that no test in this
// file depends on what some other test left installed in the process.
func listHarness(t testing.TB, v gift.View, size geom.Size) *gifttest.Harness {
	t.Helper()
	return gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(),
		Font:  loadTestFont(t),
		Size:  size,
		View:  v,
	})
}

// separatorOps returns the fill operations a list emitted for its hairlines.
//
// They are told apart from everything else by their colour, which is the
// theme's [ui.ColorSeparator] and which nothing else in these fixtures uses:
// the rows have no face at rest, the list has no background, and a row's
// pressed face is [ui.ColorControlPressed]. A test that wants a separator with
// a different colour says so and passes it here.
func separatorOps(t testing.TB, h *gifttest.Harness, c render.Color) []render.Op {
	t.Helper()
	var out []render.Op
	for _, op := range h.Ops() {
		if op.Kind == render.OpFillRect && op.Color == c {
			out = append(out, op)
		}
	}
	return out
}

func sepColor() render.Color { return ui.LightTheme().Color(ui.ColorSeparator) }

// --- the separator rule ------------------------------------------------------

// TestALisDrawsOneSeparatorBetweenEveryPairOfAdjacentRowsAndNoneAfterTheLast is
// the first half of the rule [ui.ListView] states, and it is the half that a
// naive implementation gets wrong by one: three rows have two gaps, not three.
func TestAListDrawsOneSeparatorBetweenEveryPairOfAdjacentRowsAndNoneAfterTheLast(t *testing.T) {
	for _, n := range []int{1, 2, 3, 7} {
		t.Run(strconv.Itoa(n)+" rows", func(t *testing.T) {
			rows := make([]gift.View, n)
			for i := range rows {
				rows[i] = ui.Row("row " + strconv.Itoa(i)).Key(strconv.Itoa(i))
			}
			h := listHarness(t, ui.List(rows...).Key("list"), geom.Sz(360, 600))
			got := separatorOps(t, h, sepColor())
			if len(got) != n-1 {
				t.Fatalf("%d rows produced %d separators, want %d.\n%s", n, len(got), n-1, h.Dump())
			}
			// None of them may sit below the last row: a hairline under the
			// last row is the classic off by one and it is visible as a line
			// hanging in the air under a group.
			last := h.Find(gifttest.ByKey(strconv.Itoa(n - 1))).Bounds()
			for _, op := range got {
				if op.Bounds.Min.Y >= last.Min.Y {
					t.Errorf("a separator at y=%v is at or below the top of the last row (%v); "+
						"the run of hairlines must end above it", op.Bounds.Min.Y, last.Min.Y)
				}
			}
		})
	}
}

// TestASectionBoundaryHasNoHairlineOnEitherSideOfIt is the second half, and it
// is the one the requirement calls "must not double up between sections".
//
// The failure it guards against is not one separator too many but two right
// next to each other: a rule of "a separator after every row" plus a header
// with a top padding produces a hairline, a gap and then the caption, which
// reads as two rules. A rule of "a separator before every row" produces the
// same thing on the other side. Only "between two rows" produces neither.
func TestASectionBoundaryHasNoHairlineOnEitherSideOfIt(t *testing.T) {
	h := listHarness(t, ui.List(
		ui.Section("First").Key("h1"),
		ui.Row("a").Key("a"),
		ui.Row("b").Key("b"),
		ui.Section("Second").Key("h2"),
		ui.Row("c").Key("c"),
	).Key("list"), geom.Sz(360, 600))

	got := separatorOps(t, h, sepColor())
	// a|b is the only pair of adjacent rows in the whole list.
	if len(got) != 1 {
		t.Fatalf("want exactly one separator, between a and b; got %d.\n%s", len(got), h.Dump())
	}
	a := h.Find(gifttest.ByKey("a")).Bounds()
	b := h.Find(gifttest.ByKey("b")).Bounds()
	y := got[0].Bounds.Min.Y
	if y < a.Max.Y-1 || y > b.Min.Y+1 {
		t.Errorf("the separator is at y=%v, not in the gap between a (ends %v) and b (starts %v)",
			y, a.Max.Y, b.Min.Y)
	}
}

// TestANestedListPutsItsSeparatorsBetweenItsOwnRows is the regression for a
// defect that reached a user's screen, and it is worth stating exactly what
// went wrong because every other separator test in this file passed while it
// did.
//
// [listNode.Layout] records the position of a hairline in the node's *local*
// space, counted from the node's own top left, which is the space
// [gift.LayoutContext.Place] takes. [gift.PaintContext.Bounds] is the node's
// rectangle in the space the operations are emitted in. The painter used the
// first as if it were the second, and the two agree precisely when the list
// sits at the top of its parent — which is what a list placed directly in a
// [ui.ScrollView] does, which is what every test in this file did, and which
// is why nothing noticed.
//
// Nested one level deeper, in a card in a scroller, the hairlines were emitted
// as far *above* the rows as the list started below its parent. Measured on
// cmd/example-kitchensink at 900x760: rows at y=342, 395 and 448, hairlines at
// y=52 and y=105 — drawn through the page header and the theme switch, which
// is the "line cutting into the theme switcher" the defect was reported as.
//
// The fixture is therefore deliberately *not* a bare list: it is a list in a
// card in a stack with a padding, so that the node's own origin is a long way
// from the origin of the space it paints into. The assertion is the one that
// fails by three hundred pixels: every hairline lies in the gap between the
// two rows it belongs to.
func TestANestedListPutsItsSeparatorsBetweenItsOwnRows(t *testing.T) {
	h := listHarness(t, ui.VStack(
		ui.Box().Frame(geom.Unbounded(), 120).Key("spacer"),
		ui.Card(
			ui.List(
				ui.Row("a").Key("a"),
				ui.Row("b").Key("b"),
				ui.Row("c").Key("c"),
			).Key("list"),
		).Padding(0).Header("Nested").Key("card"),
	).Gap(24).Padding(16), geom.Sz(360, 600))

	got := separatorOps(t, h, sepColor())
	if len(got) != 2 {
		t.Fatalf("three rows in a card produced %d separators, want 2.\n%s", len(got), h.Dump())
	}
	rows := []geom.Rect{
		h.Find(gifttest.ByKey("a")).Bounds(),
		h.Find(gifttest.ByKey("b")).Bounds(),
		h.Find(gifttest.ByKey("c")).Bounds(),
	}
	if !(rows[0].Min.Y > 140) {
		t.Fatalf("the first row is at y=%v; this fixture only means something when the list "+
			"is a long way down its parent", rows[0].Min.Y)
	}
	for i, op := range got {
		above, below := rows[i], rows[i+1]
		y := op.Bounds.Min.Y
		if y < above.Max.Y-1 || y > below.Min.Y+1 {
			t.Errorf("the hairline between row %d and row %d is at y=%v, and the gap between "+
				"them is %v..%v. A separator emitted in the wrong origin is drawn over whatever "+
				"is that far above the list.\n%s",
				i, i+1, y, above.Max.Y, below.Min.Y, h.Dump())
		}
	}
}

// TestAHeaderAtTheTopAndAtTheBottomProducesNoSeparatorAtAll covers the two
// degenerate placements a header can have. It is here because the rule is
// stated over *pairs*, and a pair at the edge of the slice is where an index
// based rule runs off the end.
func TestAHeaderAtTheTopAndAtTheBottomProducesNoSeparatorAtAll(t *testing.T) {
	h := listHarness(t, ui.List(
		ui.Section("Top").Key("h1"),
		ui.Row("only").Key("only"),
		ui.Section("Bottom").Key("h2"),
	).Key("list"), geom.Sz(360, 400))
	if got := separatorOps(t, h, sepColor()); len(got) != 0 {
		t.Fatalf("a single row between two headers produced %d separators, want none.\n%s",
			len(got), h.Dump())
	}
}

// TestASeparatorTakesItsThicknessOutOfTheLayout states the difference between
// a hairline that is drawn on top of the boundary and one that is part of the
// run. The second is what [ui.ListView] promises, and the observable
// consequence is arithmetic: the list is exactly one thickness taller per
// separator than the sum of its rows.
func TestASeparatorTakesItsThicknessOutOfTheLayout(t *testing.T) {
	const n = 4
	rows := make([]gift.View, n)
	for i := range rows {
		rows[i] = ui.Row("row " + strconv.Itoa(i)).Key(strconv.Itoa(i))
	}
	// A thickness of three rather than one, so that a list which forgot to
	// reserve the space differs by nine pixels and not by three — well outside
	// any rounding.
	const thick = 3
	h := listHarness(t, ui.List(rows...).SeparatorThickness(thick).Key("list"), geom.Sz(360, 600))

	var sum float32
	for i := range n {
		sum += h.Find(gifttest.ByKey(strconv.Itoa(i))).Bounds().Height()
	}
	list := h.Find(gifttest.ByKey("list")).Bounds().Height()
	want := sum + (n-1)*thick
	if list != want {
		t.Errorf("the list is %v tall for %v of rows and %d separators of %v; want %v. "+
			"A separator that is painted but not reserved overlaps the row under it.",
			list, sum, n-1, float32(thick), want)
	}
}

// --- hit targets and the accessory ------------------------------------------

// TestEveryRowIsAtLeastTheTouchTargetTall is the requirement of the project
// plan, section 23, step 9, applied to a list: the target panel is a
// touchscreen and a row is the thing a finger lands on.
//
// It measures a row whose content is far shorter than the floor — one short
// label at the row's own type size — which is the only case where the floor
// does any work.
func TestEveryRowIsAtLeastTheTouchTargetTall(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  ui.RowView
	}{
		{"inert", ui.Row("x").Key("r")},
		{"tappable", ui.Row("x").OnTap(func() {}).Key("r")},
		{"with a value", ui.Row("x").Value("y").Key("r")},
		{"with an accessory", ui.Row("x").Accessory(ui.Badge("1")).Key("r")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := listHarness(t, ui.List(tc.row).Key("list"), geom.Sz(360, 200))
			if got := h.Find(gifttest.ByKey("r")).Bounds().Height(); got < ui.ControlHitTarget {
				t.Errorf("the row is %v tall, below the %v touch target floor", got, ui.ControlHitTarget)
			}
		})
	}
}

// TestATapOnAToggleAccessoryFlipsTheSwitchAndDoesNotAlsoRunTheRow is the
// interaction [ui.RowView] promises and the one a settings screen lives or
// dies by.
//
// Both halves are asserted, because each without the other is a different
// defect: a switch that does not flip is a broken accessory, and a row that
// also fires is the "tapping the switch also opened the detail screen" bug.
func TestATapOnAToggleAccessoryFlipsTheSwitchAndDoesNotAlsoRunTheRow(t *testing.T) {
	var flips, taps int
	on := false
	root := func(*gift.Context) gift.View {
		return ui.List(
			ui.Row("Dark mode").
				Key("row").
				OnTap(func() { taps++ }).
				Accessory(ui.Toggle(on, func(v bool) { on, flips = v, flips+1 }).Key("switch")),
		).Key("list")
	}
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t),
		Size: geom.Sz(360, 200), Root: root,
	})

	h.Find(gifttest.ByKey("switch")).Tap()
	switch {
	case flips != 1:
		t.Fatalf("the toggle fired %d times, want 1.\n%s", flips, h.Dump())
	case !on:
		t.Fatalf("the toggle reported %v, want true", on)
	case taps != 0:
		t.Fatalf("the row's own action fired %d times as well. The tap landed on the switch; "+
			"a row that also fires makes a switch impossible to operate", taps)
	}

	// And the other direction: the row still works where the switch is not.
	// Without this the test above would pass for a row whose action is simply
	// never called.
	row := h.Find(gifttest.ByKey("row")).Bounds()
	h.ClickAt(geom.Pt(row.Min.X+ui.RowPadding+4, row.Min.Y+row.Height()/2))
	if taps != 1 {
		t.Fatalf("a tap on the row's own label fired its action %d times, want 1.\n%s", taps, h.Dump())
	}
	if flips != 1 {
		t.Errorf("the toggle fired again on a tap that was not on it")
	}
}

// TestAnInertRowIsNotInTheFocusOrderAndATappableOneIs pins the difference
// [ui.RowView] draws between a row with an OnTap and a row without one. A row
// that is a tab stop and does nothing is a dead stop for a keyboard user, and
// a row that runs a command and cannot be reached by tab is unreachable
// without a pointer.
func TestAnInertRowIsNotInTheFocusOrderAndATappableOneIs(t *testing.T) {
	h := listHarness(t, ui.List(
		ui.Row("inert").Key("inert"),
		ui.Row("live").OnTap(func() {}).Key("live"),
	).Key("list"), geom.Sz(360, 300))

	if got := h.FindAll(gifttest.Focusable().And(gifttest.Under(gifttest.ByKey("list")))); len(got) != 1 {
		t.Fatalf("the list has %d focusable nodes, want exactly the tappable row.\n%s",
			len(got), h.Dump())
	}
	h.Find(gifttest.ByKey("live")).Focus().AssertFocused()
}

// TestADisabledRowRefusesTheTapAndQuietensItsTitle covers both halves of
// [ui.RowView.Disabled]: the input half, which gift enforces, and the label
// half, which this type does itself because a button cannot recolour a label
// it was handed.
func TestADisabledRowRefusesTheTapAndQuietensItsTitle(t *testing.T) {
	taps := 0
	h := listHarness(t, ui.List(
		ui.Row("Unavailable").Key("row").OnTap(func() { taps++ }).Disabled(true),
	).Key("list"), geom.Sz(360, 200))

	h.Find(gifttest.ByKey("row")).AssertDisabled()
	b := h.Find(gifttest.ByKey("row")).Bounds()
	h.ClickAt(geom.Pt(b.Min.X+b.Width()/2, b.Min.Y+b.Height()/2))
	if taps != 0 {
		t.Errorf("a disabled row fired its action %d times", taps)
	}

	// The title is drawn in the secondary label colour, which is the whole of
	// the visual affordance. Compared against the enabled row's colour rather
	// than against a literal, so that the test says "it changed" and not "it
	// is this particular grey".
	quiet := glyphColor(t, h, "row")
	on := listHarness(t, ui.List(
		ui.Row("Unavailable").Key("row").OnTap(func() {}),
	).Key("list"), geom.Sz(360, 200))
	loud := glyphColor(t, on, "row")
	if quiet == loud {
		t.Errorf("the title of a disabled row is drawn in %v, the same colour as an enabled one. "+
			"Nothing on the screen says the row is unavailable", quiet)
	}
	if want := ui.LightTheme().Color(ui.ColorSecondaryLabel); quiet != want {
		t.Errorf("the disabled title is %v, want ui.ColorSecondaryLabel %v", quiet, want)
	}
}

// glyphColor returns the colour of the first glyph operation inside the node
// with the given key.
func glyphColor(t testing.TB, h *gifttest.Harness, key string) render.Color {
	t.Helper()
	n := h.Find(gifttest.ByKey(key)).Find(gifttest.ByType("ui.Text").And(gifttest.ByText("Unavailable")))
	for _, op := range h.Ops() {
		if op.Kind == render.OpGlyphs && op.Bounds.Overlaps(n.Bounds()) {
			return op.Color
		}
	}
	t.Fatalf("no glyph operation for %q.\n%s", key, h.Dump())
	return render.Color{}
}

// --- density -----------------------------------------------------------------

// TestEverySeparatorLandsOnWholeDevicePixelsAtEveryDensity is the crispness
// claim of [ui.DividerView] and [ui.ListView], measured rather than asserted.
//
// # Why the fixture is what it is
//
// Three things have to be true at once for this test to be able to fail, and
// each of them is a thing a weaker fixture would have removed:
//
//   - the density must be greater than one, or every logical coordinate is
//     already a device coordinate and rounding has nothing to do;
//   - the rows must have a height whose sum is *not* a whole number of device
//     pixels, which text heights supply — a fixture of fixed height boxes
//     would land on integers by luck and prove nothing;
//   - the list must sit at a fractional scroll offset, which is the case
//     local-space rounding gets wrong and the case a fling produces on every
//     frame of its deceleration.
//
// The last one is the point. A snap computed from the local rectangle would
// pass at offset zero and fail at offset 10.25, so the test scrolls there.
func TestEverySeparatorLandsOnWholeDevicePixelsAtEveryDensity(t *testing.T) {
	rows := make([]gift.View, 12)
	for i := range rows {
		rows[i] = ui.Row("row " + strconv.Itoa(i)).
			Subtitle("with a second line").
			Key(strconv.Itoa(i))
	}
	for _, d := range []float64{1, 2, 3} {
		for _, off := range []float64{0, 10.25, 7.4} {
			t.Run(strconv.FormatFloat(d, 'f', -1, 64)+"x at "+strconv.FormatFloat(off, 'f', -1, 64), func(t *testing.T) {
				h := gifttest.New(t, gifttest.Options{
					Theme:   ui.LightTheme(),
					Font:    loadTestFont(t),
					Density: d,
					Size:    geom.Sz(360, 240),
					View: ui.VScroll(ui.List(rows...).Key("list")).
						Key("scroll").Flex(1),
				})
				h.Find(gifttest.ByKey("list")).ScrollTo(off)

				ops := separatorOps(t, h, sepColor())
				if len(ops) != len(rows)-1 {
					t.Fatalf("got %d separators, want %d", len(ops), len(rows)-1)
				}
				// Device space is the local rectangle through the transform
				// the operation carries, which at this point is the scroll
				// translation composed with the density scale.
				list := h.List()
				dens := float32(h.Density())
				for i, op := range ops {
					dev := list.Xform(op.Xform).TransformRect(op.Bounds)
					if !isWhole(dev.Min.Y) || !isWhole(dev.Max.Y) {
						t.Errorf("separator %d spans device rows [%v, %v); a hairline whose edges "+
							"are not whole device pixels is rasterised at partial coverage across "+
							"two rows and is visibly lighter than its neighbours",
							i, dev.Min.Y, dev.Max.Y)
					}
					if got, want := dev.Height(), roundTo(ui.DividerThickness*dens); !nearlyWhole(got - want) {
						t.Errorf("separator %d is %v device pixels thick at density %v, want %v",
							i, got, dens, want)
					}
				}
			})
		}
	}
}

// isWhole reports whether v is a whole device pixel to within the precision
// float32 actually has here.
//
// The tolerance is measured and not chosen, and it is worth two sentences
// because a tolerance is the classic place to hide a failure. The snap
// produces the local coordinate that maps onto an integer, and this test maps
// it back with a second float32 multiply and add; near y = 1024 one unit in
// the last place of a float32 is about 6e-5, and the observed residual at the
// worst case of this fixture — density 3, scroll offset 7.4 — is 6.1e-5. The
// threshold below is a thousandth, sixteen times that residual.
//
// The distance to an actual defect is three orders of magnitude the other way:
// a hairline that was *not* snapped sits half a device pixel off, 0.5, which
// is five hundred times this threshold. There is no value of the residual that
// could be confused with an unsnapped hairline. Verified by deleting the snap,
// which moved the worst case of this fixture from 6.1e-5 to 0.75.
func isWhole(v float32) bool { return nearlyWhole(v - float32(int32(v+0.5))) }

// nearlyWhole reports whether a difference is zero to the precision above.
func nearlyWhole(d float32) bool { return d < 1e-3 && d > -1e-3 }

func roundTo(v float32) float32 {
	if v < 1 {
		return 1
	}
	return float32(int32(v + 0.5))
}

// TestTheLayoutOfAListIsTheSameAtEveryDensity is the other half of the
// project plan, section 18: the density changes the pixels and never the
// program. It would fail if the snap had been done in the layout pass, which
// is the obvious wrong place to put it — and the place it is easiest to put
// it, because the layouter is where the density is available without any
// arithmetic.
func TestTheLayoutOfAListIsTheSameAtEveryDensity(t *testing.T) {
	scene := func(d float64) gifttest.Options {
		rows := make([]gift.View, 6)
		for i := range rows {
			rows[i] = ui.Row("row " + strconv.Itoa(i)).Key(strconv.Itoa(i))
		}
		return gifttest.Options{
			Theme: ui.LightTheme(), Font: loadTestFont(t), Density: d,
			Size: geom.Sz(360, 600), View: ui.List(rows...).Key("list"),
		}
	}
	one := gifttest.New(t, scene(1))
	two := gifttest.New(t, scene(2))
	for i := range 6 {
		k := strconv.Itoa(i)
		a := one.Find(gifttest.ByKey(k)).Bounds()
		b := two.Find(gifttest.ByKey(k)).Bounds()
		if a != b {
			t.Errorf("row %s is at %v at density 1 and at %v at density 2; the layout is logical", k, a, b)
		}
	}
	if a, b := one.Find(gifttest.ByKey("list")).Bounds(), two.Find(gifttest.ByKey("list")).Bounds(); a != b {
		t.Errorf("the list is %v at density 1 and %v at density 2", a, b)
	}
}

// --- separators as a value ---------------------------------------------------

// TestSeparatorInsetsShortenTheHairlineWithoutMovingAnything covers the
// modifier that makes a separator line up with a row's text.
func TestSeparatorInsetsShortenTheHairlineWithoutMovingAnything(t *testing.T) {
	rows := []gift.View{ui.Row("a").Key("a"), ui.Row("b").Key("b")}
	plain := listHarness(t, ui.List(rows...).Key("list"), geom.Sz(360, 300))
	inset := listHarness(t, ui.List(ui.Row("a").Key("a"), ui.Row("b").Key("b")).
		SeparatorInsets(50, 20).Key("list"), geom.Sz(360, 300))

	p := separatorOps(t, plain, sepColor())
	i := separatorOps(t, inset, sepColor())
	if len(p) != 1 || len(i) != 1 {
		t.Fatalf("want one separator in each, got %d and %d", len(p), len(i))
	}
	if got, want := i[0].Bounds.Min.X, float32(50); got != want {
		t.Errorf("the inset separator starts at x=%v, want %v", got, want)
	}
	if got, want := i[0].Bounds.Max.X, float32(360-20); got != want {
		t.Errorf("the inset separator ends at x=%v, want %v", got, want)
	}
	if got, want := p[0].Bounds.Min.X, ui.ListSeparatorInset; got != want {
		t.Errorf("the default separator starts at x=%v, want ui.ListSeparatorInset %v", got, want)
	}
	// The rows did not move: an inset is a property of the hairline alone.
	if a, b := plain.Find(gifttest.ByKey("b")).Bounds(), inset.Find(gifttest.ByKey("b")).Bounds(); a != b {
		t.Errorf("the second row is at %v without insets and at %v with them", a, b)
	}
}

// TestSeparatorColorClearKeepsTheSpaceAndDrawsNothingVisible is the documented
// way to switch hairlines off, and the documentation makes a claim about the
// layout that is worth checking: the rows must not move.
func TestSeparatorColorClearKeepsTheSpaceAndDrawsNothingVisible(t *testing.T) {
	build := func(c ui.Color) *gifttest.Harness {
		return listHarness(t, ui.List(
			ui.Row("a").Key("a"), ui.Row("b").Key("b"),
		).SeparatorColor(c).Key("list"), geom.Sz(360, 300))
	}
	on := build(ui.ColorSeparator)
	off := build(ui.ColorClear)
	if a, b := on.Find(gifttest.ByKey("b")).Bounds(), off.Find(gifttest.ByKey("b")).Bounds(); a != b {
		t.Errorf("the second row is at %v with separators and at %v without; "+
			"switching a colour off must not move the layout", a, b)
	}
	if got := separatorOps(t, off, sepColor()); len(got) != 0 {
		t.Errorf("a ColorClear separator still emitted %d operations in the separator colour", len(got))
	}
}

// --- the divider on its own ---------------------------------------------------

// TestADividerFillsTheWidthItIsOfferedAndIsOnePixelTall is the smallest claim
// [ui.DividerView] makes and the one every other one rests on.
func TestADividerFillsTheWidthItIsOfferedAndIsOnePixelTall(t *testing.T) {
	h := listHarness(t, ui.VStack(
		ui.Text("above").Key("above"),
		ui.Divider().Key("rule"),
		ui.Text("below").Key("below"),
	).Frame(200, 100), geom.Sz(360, 300))

	b := h.Find(gifttest.ByKey("rule")).Bounds()
	if b.Width() != 200 {
		t.Errorf("the divider is %v wide inside a 200 wide stack", b.Width())
	}
	if b.Height() != ui.DividerThickness {
		t.Errorf("the divider is %v tall, want %v", b.Height(), ui.DividerThickness)
	}
	if got := separatorOps(t, h, sepColor()); len(got) != 1 {
		t.Fatalf("the divider emitted %d fills in the separator colour, want 1.\n%s", len(got), h.Dump())
	}
}

// TestADividerInsetShortensBothEnds covers the modifier a list uses and an
// application reaches for directly.
func TestADividerInsetShortensBothEnds(t *testing.T) {
	h := listHarness(t, ui.VStack(
		ui.Divider().Inset(12, 30).Key("rule"),
	).Frame(200, 40), geom.Sz(360, 300))
	got := separatorOps(t, h, sepColor())
	if len(got) != 1 {
		t.Fatalf("want one fill, got %d", len(got))
	}
	b := h.Find(gifttest.ByKey("rule")).Bounds()
	if got[0].Bounds.Min.X != b.Min.X+12 || got[0].Bounds.Max.X != b.Max.X-30 {
		t.Errorf("the inset divider spans [%v, %v] inside bounds %v",
			got[0].Bounds.Min.X, got[0].Bounds.Max.X, b)
	}
}

// --- badge and card -----------------------------------------------------------

// TestABadgeIsAFixedHeightCapsuleWhateverIsInIt pins the two properties
// [ui.BadgeView] promises and that a column of badges depends on.
func TestABadgeIsAFixedHeightCapsuleWhateverIsInIt(t *testing.T) {
	for _, s := range []string{"1", "12", "New", "1234567"} {
		t.Run(s, func(t *testing.T) {
			h := listHarness(t, ui.VStack(ui.Badge(s).Key("b")).Padding(10), geom.Sz(360, 200))
			b := h.Find(gifttest.ByKey("b")).Bounds()
			if b.Height() != ui.BadgeHeight {
				t.Errorf("the badge is %v tall for %q, want a fixed %v", b.Height(), s, ui.BadgeHeight)
			}
			if b.Width() < ui.BadgeHeight {
				t.Errorf("the badge is %v wide, below the %v floor that keeps one digit a circle",
					b.Width(), ui.BadgeHeight)
			}
		})
	}
	// Longer text is wider: a fixed height must not have turned into a fixed
	// size that clips the string.
	one := listHarness(t, ui.VStack(ui.Badge("1").Key("b")), geom.Sz(360, 200))
	many := listHarness(t, ui.VStack(ui.Badge("1234567").Key("b")), geom.Sz(360, 200))
	if a, b := one.Find(gifttest.ByKey("b")).Bounds().Width(),
		many.Find(gifttest.ByKey("b")).Bounds().Width(); b <= a {
		t.Errorf("a seven character badge is %v wide and a one character badge %v; "+
			"the width must follow the text", b, a)
	}
}

// TestTheGlyphsOfABadgeAreCentredInItsCapsule is the reason [ui.BadgeView] is
// a ZStack and not a TextView with a background, and it is exactly the defect
// the simpler spelling has: a fixed height imposed on a text node leaves the
// glyphs at the top of the box.
func TestTheGlyphsOfABadgeAreCentredInItsCapsule(t *testing.T) {
	h := listHarness(t, ui.VStack(ui.Badge("9").Key("b")).Padding(10), geom.Sz(360, 200))
	outer := h.Find(gifttest.ByKey("b")).Bounds()
	label := h.Find(gifttest.ByKey("b")).Find(gifttest.ByKey("label")).Bounds()
	above := label.Min.Y - outer.Min.Y
	below := outer.Max.Y - label.Max.Y
	if d := above - below; d > 1 || d < -1 {
		t.Errorf("the label has %v above it and %v below it inside the capsule; it is not centred",
			above, below)
	}
}

// TestACardHeaderKeepsItsInsetWhenTheContentPaddingIsZero is the one
// non-obvious claim in [ui.CardView]'s documentation, and the composition it
// exists for: a card wrapping a list, whose rows bring their own padding.
func TestACardHeaderKeepsItsInsetWhenTheContentPaddingIsZero(t *testing.T) {
	h := listHarness(t, ui.Card(
		ui.List(ui.Row("a").Key("a"), ui.Row("b").Key("b")).Key("list"),
	).Padding(0).Header("Display").Key("card"), geom.Sz(360, 400))

	card := h.Find(gifttest.ByKey("card")).Bounds()
	list := h.Find(gifttest.ByKey("list")).Bounds()

	// The header's *glyphs* and not its node bounds: a [ui.TextView]'s padding
	// lives inside its own rectangle, so the node starts at the card's edge
	// either way and measuring it would prove nothing.
	header := glyphLeft(t, h.Find(gifttest.ByKey("card")).Find(gifttest.ByKey("header")))
	if got := header - card.Min.X; got != ui.CardPadding {
		t.Errorf("the header's glyphs start %v from the card's edge, want ui.CardPadding %v",
			got, ui.CardPadding)
	}
	if got := list.Min.X - card.Min.X; got != 0 {
		t.Errorf("the content is inset by %v with Padding(0); a flush card is what Padding(0) is for", got)
	}
	// And the row inside the flush list still has its own inset, so the text
	// of the rows and the header line up.
	title := glyphLeft(t, h.Find(gifttest.ByKey("a")).Find(gifttest.ByKey("title")))
	if got := title - card.Min.X; got != ui.RowPadding {
		t.Errorf("a row's title starts %v from the card's edge, want ui.RowPadding %v",
			got, ui.RowPadding)
	}
}

// glyphLeft is the pen origin of the leftmost glyph a text node emitted, which
// is where the ink actually starts. The *operation* a text node emits carries
// the node's bounds and not the extent of the run, so it cannot answer this.
func glyphLeft(t testing.TB, n gifttest.Node) float32 {
	t.Helper()
	gs := n.Glyphs()
	if len(gs) == 0 {
		t.Fatalf("no glyphs under %v", n.Bounds())
	}
	x := gs[0].X
	for _, g := range gs[1:] {
		if g.X < x {
			x = g.X
		}
	}
	return x
}

// TestACardWithoutAHeaderCostsNoExtraNode is the claim [ui.CardView.Build]
// makes about its own shape. It is a cost claim and it is the sort that rots
// silently, because nothing looks different when it stops being true.
func TestACardWithoutAHeaderCostsNoExtraNode(t *testing.T) {
	count := func(v gift.View) uint64 {
		return listHarness(t, v, geom.Sz(360, 400)).Diagnostics().LiveNodes
	}
	plain := count(ui.Card(ui.Text("x").Key("x")).Key("card"))
	headed := count(ui.Card(ui.Text("x").Key("x")).Header("H").Key("card"))
	// The header adds its own text node and the wrapping stack: two.
	if got := int(headed) - int(plain); got != 2 {
		t.Errorf("a header costs %d extra nodes, want 2 (the caption and the stack that holds "+
			"the caption and the body). A card without a header must not carry the wrapper.", got)
	}
}

// --- interaction with step 9b -------------------------------------------------

// TestAListInAHiddenTabIsNotPaintedAndKeepsItsScrollOffset is the interaction
// between this work unit and the navigation containers of the one before it.
//
// Both halves matter and they pull in opposite directions: not painting is
// what makes a tab bar affordable, and keeping the offset is what makes it
// usable. An implementation that unmounted the tab would pass the first half
// and fail the second.
func TestAListInAHiddenTabIsNotPaintedAndKeepsItsScrollOffset(t *testing.T) {
	rows := func(prefix string) gift.View {
		out := make([]gift.View, 30)
		for i := range out {
			out[i] = ui.Row(prefix + strconv.Itoa(i)).Key(strconv.Itoa(i))
		}
		return ui.VScroll(ui.List(out...).Key(prefix + "list")).Key(prefix + "scroll").Flex(1)
	}
	sel := 0
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(360, 480),
		Root: func(*gift.Context) gift.View {
			return ui.TabBar(sel, func(i int) { sel = i },
				ui.Tab("A", ui.Symbol{}, rows("a")),
				ui.Tab("B", ui.Symbol{}, rows("b")),
			)
		},
	})

	h.Find(gifttest.ByKey("alist")).ScrollTo(300)
	visible := len(separatorOps(t, h, sepColor()))
	if visible == 0 {
		t.Fatal("the visible tab drew no separators at all; the fixture is wrong")
	}

	sel = 1
	h.App().Invalidate()
	h.Settle()
	// While the switch is in flight both tabs are on the screen, because one
	// of them is sliding out; that is [gift.TransitionSpec] and it is the
	// bounded exception to the rule this test is about. Tab B's list is a
	// different list of the same length, so "both are drawn" is the count
	// roughly doubling.
	if got := len(separatorOps(t, h, sepColor())); got <= visible {
		t.Errorf("during the tab transition the frame has %d separators where one tab draws "+
			"%d; the outgoing tab is not being painted, so the switch is a cut", got, visible)
	}

	// And the rule itself, one transition later: the outgoing tab stops being
	// painted and the guarantee is back in force. The pair is the test — a
	// single count proves neither half.
	h.Advance(ui.ControlAnimation + 32*time.Millisecond)
	if got := len(separatorOps(t, h, sepColor())); got > visible {
		t.Errorf("a transition after selecting tab B the frame has %d separators where one tab "+
			"draws %d; the hidden tab is still being painted", got, visible)
	}

	sel = 0
	h.App().Invalidate()
	h.Settle()
	h.Find(gifttest.ByKey("alist")).Scroller().AssertScrollOffset(300)
}

// TestAFlingInAVisibleListStillTravels is the counterweight to the rule that a
// fling in a hidden subtree is stopped. That rule is what
// [gift.App.stopHiddenWork] does, and a rule of that shape is one small
// mistake away from stopping every fling; nothing else in this package would
// notice.
func TestAFlingInAVisibleListStillTravels(t *testing.T) {
	out := make([]gift.View, 40)
	for i := range out {
		out[i] = ui.Row("row " + strconv.Itoa(i)).Key(strconv.Itoa(i))
	}
	h := gifttest.New(t, gifttest.Options{
		Theme: ui.LightTheme(), Font: loadTestFont(t), Size: geom.Sz(360, 300),
		Root: func(*gift.Context) gift.View {
			return ui.TabBar(0, nil,
				ui.Tab("A", ui.Symbol{}, ui.VScroll(ui.List(out...).Key("list")).Key("scroll").Flex(1)),
				ui.Tab("B", ui.Symbol{}, ui.Text("other")),
			)
		},
	})
	scroller := func() gifttest.Node { return h.Find(gifttest.ByKey("scroll")) }
	// The same throw the scroll tests use: 160 pixels in 80 ms is 2000 px/s,
	// which the default friction leaves several hundred units of travel in.
	h.Find(gifttest.ByKey("0")).Fling(geom.Pt(0, -160), 80*time.Millisecond)
	if info := scroller().ScrollInfo(); !info.Flinging {
		t.Fatalf("the throw started no fling at all: %+v.\n%s", info, h.Dump())
	}
	afterDrag := scroller().ScrollOffset()
	for range 20 {
		h.Advance(16 * time.Millisecond)
	}
	if got := scroller().ScrollOffset(); got <= afterDrag {
		t.Errorf("the list is at %v after the fling and was at %v when the finger left it; "+
			"a fling in a visible list must keep travelling", got, afterDrag)
	}
}

// TestARowWithNoTitleGivesItsWholeWidthToAFlexibleAccessory is the accessory
// row rule of [ui.RowView], and it is here because the defect it describes is
// invisible by inspection: a slider in a list simply comes out half as wide as
// the row, which looks like a design choice.
//
// The mechanism is that [ui.RowView] inserts a flexible [ui.Spacer] to push
// the trailing group to the edge, and a Spacer takes a share of the remainder
// like any other flexible child. With a flexible accessory next to it, the two
// split the width.
func TestARowWithNoTitleGivesItsWholeWidthToAFlexibleAccessory(t *testing.T) {
	const width = 400
	h := listHarness(t, ui.List(
		ui.Row("").Accessory(ui.Slider(0.5, nil).Flex(1).Key("slider")).Key("bare"),
		ui.Row("Volume").Accessory(ui.Slider(0.5, nil).Flex(1).Key("labelled-slider")).Key("labelled"),
	).Key("list"), geom.Sz(width, 300))

	row := h.Find(gifttest.ByKey("bare")).Bounds()
	bare := h.Find(gifttest.ByKey("slider")).Bounds()
	inner := row.Width() - 2*ui.RowPadding
	if got := bare.Width(); got < inner-0.5 {
		t.Errorf("the slider in a row with no title is %v wide inside a row whose content area "+
			"is %v. A row with no labels is an accessory row and its accessory gets the whole "+
			"width; a flexible Spacer next to it would take half.", got, inner)
	}

	// And the other side of the rule: a row that *does* have a title keeps the
	// gap, so the title stays at the leading edge and does not get pushed
	// around by the accessory.
	labelled := h.Find(gifttest.ByKey("labelled-slider")).Bounds()
	if labelled.Width() >= bare.Width() {
		t.Errorf("the slider in a labelled row is %v wide and the one in a bare row %v; "+
			"the labelled row must still spend width on its label and its gap",
			labelled.Width(), bare.Width())
	}
	title := h.Find(gifttest.ByKey("labelled")).Find(gifttest.ByKey("title")).Bounds()
	if title.Min.X != row.Min.X+ui.RowPadding {
		t.Errorf("the title of the labelled row starts at %v, not at the row's own inset %v",
			title.Min.X, row.Min.X+ui.RowPadding)
	}
}

// --- the clip ----------------------------------------------------------------

// TestTheSeparatorsOfAClippedListStayInsideIt is the property
// [ui.ListView.Clip] promises, and it is the one that was false.
//
// # What was wrong and why nothing noticed
//
// gift applies [gift.Element.Clip] on the way into a node's *subtree*, and a
// node's own drawing stays outside it deliberately, so that a shadow is not cut
// off by the shape that casts it. A list paints its hairlines itself, after
// PaintChildren, so they went out through that same door: measured on this very
// fixture, nine separators of which seven were entirely below the clip
// rectangle, the furthest 285 logical pixels below the bottom edge, painted
// over whatever was underneath.
//
// It was invisible because the usual home of a long list is a scroll viewport,
// and a scroll viewport clips its content, so the parent caught them. The
// composition that has no parent viewport is a list in a fixed frame — which is
// exactly the composition Clip(true) exists for.
//
// # The fixture
//
// Ten rows in a frame 120 logical pixels tall, which is room for two. Both
// halves are asserted, because either alone is satisfied by a mistake: that no
// separator is drawn outside the list's rectangle, and that the ones inside it
// are still drawn — a painter that simply stopped emitting them would pass the
// first half and make the modifier useless.
func TestTheSeparatorsOfAClippedListStayInsideIt(t *testing.T) {
	const rows = 10
	items := make([]gift.View, rows)
	for i := range items {
		items[i] = ui.Row("row " + strconv.Itoa(i)).Key(strconv.Itoa(i))
	}
	h := listHarness(t, ui.VStack(
		ui.List(items...).Frame(300, 120).Clip(true).Key("list"),
	).Key("outer"), geom.Sz(360, 600))

	list := h.Find(gifttest.ByKey("list")).Bounds()
	if list.Height() != 120 {
		t.Fatalf("the list is %v tall; the fixture needs it clipped to 120", list.Height())
	}

	ops := separatorOps(t, h, sepColor())
	if len(ops) != rows-1 {
		t.Fatalf("the list emitted %d separators for %d rows, want %d.\n%s",
			len(ops), rows, rows-1, h.Dump())
	}

	// Device space, because that is the space a clip rectangle lives in; see
	// [gift.PaintContext.PushClip].
	l := h.List()
	inside := 0
	for i, op := range ops {
		dev := l.Xform(op.Xform).TransformRect(op.Bounds)
		clip := l.Clip(op.Clip)
		visible := clip.Intersect(dev)
		if visible.Width() <= 0 || visible.Height() <= 0 {
			continue // correctly clipped away
		}
		inside++
		// A separator that survives the clip has to be one that belongs
		// inside the list's own rectangle.
		listDev := l.Xform(op.Xform).TransformRect(list)
		if dev.Min.Y < listDev.Min.Y-1 || dev.Max.Y > listDev.Max.Y+1 {
			t.Errorf("separator %d spans device rows [%v, %v) and is still visible under clip "+
				"%v, while the clipped list occupies [%v, %v). A hairline drawn outside the "+
				"rectangle a list was told to stay inside is drawn over whatever is beneath it.",
				i, dev.Min.Y, dev.Max.Y, clip, listDev.Min.Y, listDev.Max.Y)
		}
	}
	if inside == 0 {
		t.Errorf("every one of the %d separators was clipped away. Clip(true) confines the "+
			"hairlines; it does not switch them off, and a list of ten rows in a frame with "+
			"room for two still shows the ones between the rows that are visible", len(ops))
	}
}

// TestAClearSeparatorEmitsNoOperationAtAll is the gate [listNode.Paint]
// documents, and the reason it has one where [ui.DividerView] deliberately
// does not.
//
// A divider emits one transparent fill; a list emits one per separator, and
// [ui.ListView.SeparatorColor] with [ui.ColorClear] is the documented way to
// switch them off. The divider's "one operation the backend discards" is a
// thousand of them in a thousand row list, on every frame, for something the
// caller asked not to be drawn.
//
// It is checked at two sizes, because the defect this guards is one that grows
// with the list: a gate that was accidentally applied per *list* rather than
// per frame would still show a constant, and a constant is what "one operation"
// meant.
func TestAClearSeparatorEmitsNoOperationAtAll(t *testing.T) {
	count := func(n int) int {
		items := make([]gift.View, n)
		for i := range items {
			items[i] = ui.Row("row " + strconv.Itoa(i)).Key(strconv.Itoa(i))
		}
		h := listHarness(t, ui.VScroll(
			ui.List(items...).SeparatorColor(ui.ColorClear).Key("list"),
		).Key("scroll").Flex(1), geom.Sz(360, 600))
		got := 0
		for _, op := range h.Ops() {
			if op.Kind == render.OpFillRect && op.Color.A == 0 {
				got++
			}
		}
		return got
	}
	for _, n := range []int{10, 200} {
		if got := count(n); got != 0 {
			t.Errorf("a %d row list with ui.ColorClear separators emitted %d fully transparent "+
				"fills. The backend discards every one of them, and SeparatorColor(ColorClear) "+
				"is the documented way to switch separators off — so the list is paying one "+
				"operation per separator per frame for nothing.", n, got)
		}
	}
}

// --- the one degenerate case that is documented rather than fixed -------------

// hiddenItem is an item of a list whose element declares [gift.Element.Hidden].
// It exists for the test below and is the shape a caller's own conditional
// wrapper would have.
type hiddenItem struct{ inner gift.View }

func (hiddenItem) ViewType() gift.TypeID { return hiddenItemType }

// Build builds the wrapped view and hides the element it produced, which is
// how [gift.Element.Hidden] reaches a list item in practice: it is a field of
// the element a view returns, set during Build, and the list's separator rule
// was decided from the view before this ran.
func (v hiddenItem) Build(bc *gift.BuildContext) gift.Element {
	e := v.inner.Build(bc)
	e.Hidden = true
	return e
}

var hiddenItemType = gift.RegisterType("ui_test.hiddenItem")

// TestAHiddenItemInAListLeavesABlankBandBetweenTwoHairlines pins the sharp edge
// [ui.ListView]'s separatorRule documents, so that the documentation is a
// measurement and not a guess.
//
// It is a pinning test and not a failing one. The separator rule is computed
// from the *views*, before anything is built, and [gift.Element.Hidden] is set
// during Build and deliberately does not skip layout — so a hidden item keeps
// its height and keeps counting as an item, and the result is two hairlines
// bracketing a blank band where the rule says "between two rows and nowhere
// else". The documented answer for a caller is to leave the item out of the
// slice rather than to hide it, and the test below is what would notice if this
// behaviour ever changed underneath that sentence.
//
// Every other degenerate arrangement — an empty list, a single header, adjacent
// headers, a header first, a header last — is correct and is covered above.
func TestAHiddenItemInAListLeavesABlankBandBetweenTwoHairlines(t *testing.T) {
	h := listHarness(t, ui.List(
		ui.Row("first").Key("first"),
		hiddenItem{inner: ui.Row("middle").Key("middle")},
		ui.Row("last").Key("last"),
	).Key("list"), geom.Sz(360, 400))

	ops := separatorOps(t, h, sepColor())
	if len(ops) != 2 {
		t.Fatalf("three items with the middle one hidden produced %d separators, want the 2 "+
			"that ui.ListView's separatorRule documents. If this is now 1, the behaviour was "+
			"fixed and the documentation has to stop describing the blank band.\n%s",
			len(ops), h.Dump())
	}
	first := h.Find(gifttest.ByKey("first")).Bounds()
	last := h.Find(gifttest.ByKey("last")).Bounds()
	gap := last.Min.Y - first.Max.Y
	if gap < ui.RowHeight {
		t.Errorf("the hidden item takes %v of vertical space between the two visible rows; "+
			"gift.Element.Hidden deliberately does not skip layout, so it is supposed to keep "+
			"its full height and this test's premise is wrong", gap)
	}
	// The two hairlines really do bracket the band, which is what makes it
	// read as two rules rather than as one boundary.
	if !(ops[0].Bounds.Min.Y <= first.Max.Y+1 && ops[1].Bounds.Min.Y >= last.Min.Y-ui.DividerThickness-1) {
		t.Errorf("the two separators are at y=%v and y=%v; the documented shape is one under "+
			"the first row (ends %v) and one immediately above the last (starts %v)",
			ops[0].Bounds.Min.Y, ops[1].Bounds.Min.Y, first.Max.Y, last.Min.Y)
	}
	t.Logf("three rows, middle hidden: separators at y=%v and y=%v, bracketing a %v pixel "+
		"blank band", ops[0].Bounds.Min.Y, ops[1].Bounds.Min.Y, gap)
}
