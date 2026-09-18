package gifttest_test

import (
	"strconv"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// Review Gate 3 named exactly three things that break in this package without
// a scroll container, and this file is the answer to all three:
//
//  1. An action has to scroll its target into view before aiming at it, or a
//     test clicks a point that is clipped away.
//  2. Node.Center has to be the centre of the *visible* part of a node.
//  3. The aim check from WU-K has to keep working: scrolling changes where a
//     node is, so the scroll and the check must not fight each other.

const (
	scrollRows     = 20
	scrollRowH     = 60
	scrollViewport = 180
)

func rowKey(i int) string { return "row" + strconv.Itoa(i) }

// scrollList is a scroller of buttons, most of which are below the fold. The
// last row is nine rows past the bottom edge, which is the case the harness
// used to be unable to reach at all.
func scrollList(hit *string) gift.View {
	rows := make([]gift.View, 0, scrollRows)
	for i := range scrollRows {
		name := rowKey(i)
		rows = append(rows, ui.Button(ui.Text(name), func() { *hit = name }).
			Key(name).Frame(200, scrollRowH))
	}
	return ui.VStack(
		ui.VScroll(rows...).Frame(200, scrollViewport).Key("list"),
	)
}

// TestNodeBelowTheFoldCanBeFoundScrolledToAndClicked is the whole feature in
// one test.
//
// The selector finds the node regardless of visibility — a selector runs over
// the tree, not over the screen, and a virtualised list is the only thing that
// could hide a node from it. The action then brings it into view itself.
func TestNodeBelowTheFoldCanBeFoundScrolledToAndClicked(t *testing.T) {
	var hit string
	h := gifttest.New(t, gifttest.Options{View: scrollList(&hit)})

	last := h.Find(gifttest.ByKey(rowKey(scrollRows - 1)))
	last.AssertNotVisible()

	last.Click()

	if hit != rowKey(scrollRows-1) {
		t.Errorf("the click activated %q, want %q", hit, rowKey(scrollRows-1))
	}
	h.Find(gifttest.ByKey(rowKey(scrollRows - 1))).AssertVisible()
	// Content 1200, viewport 180, so the end of the document is 1020.
	h.Find(gifttest.ByKey("list")).AssertAtScrollEnd()
}

// TestCenterIsTheVisiblePart is point 2 of the gate.
//
// A row that is exactly half scrolled into view has its full bounds centred
// outside the viewport, and an action aimed there would be clipped away. The
// centre of the *visible* part is inside, which is where a user would put
// their finger too.
func TestCenterIsTheVisiblePart(t *testing.T) {
	var hit string
	h := gifttest.New(t, gifttest.Options{View: scrollList(&hit)})
	list := h.Find(gifttest.ByKey("list"))

	// Row 3 spans document 180..240. At offset 30 it sits at device 150..210
	// and the viewport ends at 180, so only its top half is visible.
	list.ScrollTo(30)
	row := h.Find(gifttest.ByKey(rowKey(3)))

	full := row.Bounds()
	if got := full.Min.Y; got != 150 {
		t.Fatalf("the scene is not what the test assumes: row3 is at %v", full)
	}
	vis, ok := row.VisibleBounds()
	if !ok {
		t.Fatal("row3 should be partly visible")
	}
	if vis.Max.Y != scrollViewport {
		t.Errorf("the visible part ends at %g, want %d", vis.Max.Y, scrollViewport)
	}
	if got := row.Center().Y; got != 165 {
		t.Errorf("Center().Y = %g, want 165 — the middle of the visible half, not %g which is "+
			"the middle of the full bounds and is outside the viewport",
			got, full.Min.Y+full.Height()/2)
	}
	if got := h.At(row.Center()); got.Key() != rowKey(3) {
		t.Errorf("a hit test at the centre reaches %s, want row3", got.Describe())
	}
}

// TestAimCheckStillCatchesACoveredNodeAfterAScroll is point 3.
//
// The scroll and the check have to compose: the scroll decides *where* the
// node is, and the check decides whether anything is on top of it there. A
// node that is genuinely covered is still a failure after being scrolled into
// view, and the failure still names both nodes.
func TestAimCheckStillCatchesACoveredNodeAfterAScroll(t *testing.T) {
	var hit string
	view := ui.VStack(
		ui.VScroll(
			ui.Box().Frame(200, 400).Background(ui.RGB(20, 20, 20)).Key("filler"),
			ui.ZStack(
				ui.Button(ui.Text("under"), func() { hit = "btn" }).Key("btn").Frame(40, 20),
				ui.Button(ui.Text("over"), func() { hit = "cover" }).Key("cover").Frame(200, 100),
			).Frame(200, 100).Key("pair"),
		).Frame(200, scrollViewport).Key("list"),
	)

	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: view})
		// Not visible to begin with, so the action has to scroll first — and
		// then still has to notice the cover.
		h.Find(gifttest.ByKey("btn")).Click()
	})
	if len(r.fatals) != 1 {
		t.Fatalf("want exactly one fatal, got %d: %v", len(r.fatals), r.fatals)
	}
	wantContains(t, "the covered-after-scroll message", r.fatals[0],
		"Click aimed at",
		`key="btn"`,
		`key="cover"`,
		"covered at that point",
	)
	if hit != "" {
		t.Errorf("something was activated (%q); the action must fail before it dispatches", hit)
	}

	// And the scroll really did happen, so the failure is about the cover and
	// not about visibility: the same button without a cover is clickable.
	var hit2 string
	h := gifttest.New(t, gifttest.Options{View: ui.VStack(
		ui.VScroll(
			ui.Box().Frame(200, 400).Background(ui.RGB(20, 20, 20)).Key("filler"),
			ui.Button(ui.Text("under"), func() { hit2 = "btn" }).Key("btn").Frame(200, 100),
		).Frame(200, scrollViewport).Key("list"),
	)})
	h.Find(gifttest.ByKey("btn")).Click()
	if hit2 != "btn" {
		t.Errorf("the same button without a cover was not activated after the scroll")
	}
}

// TestAssertVisibleNamesTheProblem checks the message rather than the boolean:
// a node that is clipped away should say so and should point at the fix.
func TestAssertVisibleNamesTheProblem(t *testing.T) {
	var hit string
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: scrollList(&hit)})
		h.Find(gifttest.ByKey(rowKey(scrollRows - 1))).AssertVisible()
	})
	if len(r.errors) != 1 {
		t.Fatalf("want exactly one error, got %d: %v", len(r.errors), r.errors)
	}
	wantContains(t, "the AssertVisible message", r.errors[0],
		"entirely clipped away",
		"scroll it into view first",
		"the tree was:",
	)
}

// TestScrollAssertionsReportTheGeometry keeps the failure message of
// AssertScrollOffset useful: a wrong offset is much easier to read next to the
// content extent and the viewport it is bounded by.
func TestScrollAssertionsReportTheGeometry(t *testing.T) {
	var hit string
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: scrollList(&hit)})
		h.Find(gifttest.ByKey("list")).AssertScrollOffset(500)
	})
	if len(r.errors) != 1 {
		t.Fatalf("want exactly one error, got %d: %v", len(r.errors), r.errors)
	}
	wantContains(t, "the AssertScrollOffset message", r.errors[0],
		"scroll offset = 0",
		"want            500",
		"content 1200",
		"viewport 180",
		"max offset 1020",
		"axis vertical",
	)
}

// TestScrollerOnSomethingOutsideAScrollerFails is the honest answer for a test
// that asks about the scroll state of a node that has none.
func TestScrollerOnSomethingOutsideAScrollerFails(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: ui.VStack(ui.Box().Frame(10, 10).Key("plain"))})
		h.Find(gifttest.ByKey("plain")).ScrollOffset()
	})
	if len(r.fatals) != 1 {
		t.Fatalf("want exactly one fatal, got %d: %v", len(r.fatals), r.fatals)
	}
	wantContains(t, "the Scroller message", r.fatals[0], "not inside a scroll container")
}

// TestWheelReachesTheScrollerFromAnyDescendant is the harness level check that
// the wheel verb still lands where a user's wheel would.
func TestWheelReachesTheScrollerFromAnyDescendant(t *testing.T) {
	var hit string
	h := gifttest.New(t, gifttest.Options{View: scrollList(&hit)})
	h.Find(gifttest.ByKey(rowKey(0))).Wheel(geom.Pt(0, -1))
	h.Find(gifttest.ByKey("list")).AssertScrollOffset(float64(gift.DefaultScrollWheelStep))
	if hit != "" {
		t.Errorf("a wheel event activated %q", hit)
	}
}
