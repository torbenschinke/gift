package gifttest_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// threeRows is a tree with exactly three nodes carrying the label "row".
func threeRows() gift.View {
	return ui.VStack(
		ui.Text("row").Key("a"),
		ui.Text("row").Key("b"),
		ui.Text("row").Key("c"),
	).Gap(4).Padding(8)
}

// TestReentrantWhereDoesNotCorruptTheResult is the second harness defect the
// review found with a probe.
//
// [gifttest.Where] documents an arbitrary predicate over a [gifttest.Node], and
// the obvious predicate for a list is "the row whose button is enabled", which
// is a query inside a query. The result buffer used to be one scratch slice on
// the Harness, truncated at the top of every run, so the inner query wiped the
// outer one's partial result and then appended its own matches on top of it.
// Three "row" nodes came back as four Nodes, two of them belonging to the
// inner query.
//
// The visible consequence was worse than a wrong count: [gifttest.Harness.Find]
// could see one match where there were two, so an ambiguous selector passed and
// the test went on to assert against an arbitrary node.
func TestReentrantWhereDoesNotCorruptTheResult(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{View: threeRows()})

	inner := 0
	reentrant := gifttest.ByText("row").And(gifttest.Where("has-a-sibling-b", func(n gifttest.Node) bool {
		// A full query from inside a predicate, which is what the doc of
		// Where invites.
		inner++
		return len(h.FindAll(gifttest.ByKey("b"))) == 1
	}))

	got := h.FindAll(reentrant)
	if inner == 0 {
		t.Fatal("the predicate never ran; the test proves nothing")
	}
	if len(got) != 3 {
		t.Fatalf("a re entrant selector matched %d nodes, want the 3 that carry the label; "+
			"the inner query corrupted the outer result", len(got))
	}
	for i, n := range got {
		if n.Text() != "row" {
			t.Errorf("match [%d] is %s, which the outer selector never matched", i, n.Type())
		}
	}
}

// TestReentrantWhereKeepsFindAmbiguous is the half that matters: an ambiguous
// selector must still be reported as ambiguous when its predicate runs a query.
func TestReentrantWhereKeepsFindAmbiguous(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: threeRows()})
		h.Find(gifttest.ByText("row").And(gifttest.Where("runs-a-query", func(gifttest.Node) bool {
			return h.Exists(gifttest.ByKey("a"))
		})))
	})
	if len(r.fatals) != 1 {
		t.Fatalf("want exactly one fatal, got %d: %v", len(r.fatals), r.fatals)
	}
	wantContains(t, "the ambiguity message", r.fatals[0], "matched 3 nodes, want exactly 1")
	t.Logf("the message a developer sees:\n%s", r.fatals[0])
}

// TestUnderStillWorks pins the selector that was already re entrant, so that
// the fix cannot be "stop recursing".
func TestUnderStillWorks(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{View: twoDeleteButtons()})
	h.Find(gifttest.ByText("Delete").And(gifttest.Under(gifttest.ByKey("row-7")))).AssertText("Delete")
	h.AssertCount(gifttest.ByText("Delete"), 2)
}
