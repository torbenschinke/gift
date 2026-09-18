package gifttest_test

import (
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/ui"
)

// coveredButton is the reviewer's probe, as a fixture: a small button with a
// much larger one on top of it in the same ZStack.
//
// A ZStack draws its children in order and gift hit tests them in reverse, so
// the cover wins every point it contains — including the centre of the button
// underneath, which is exactly the point every action in this package aims at.
func coveredButton(hit *string) gift.View {
	return ui.ZStack(
		ui.Button(ui.Text("under"), func() { *hit = "btn" }).
			Key("btn").Frame(40, 20),
		ui.Button(ui.Text("over"), func() { *hit = "cover" }).
			Key("cover").Frame(300, 200),
	).Frame(300, 200)
}

// TestActionOnACoveredNodeFails is the defect this check exists for.
//
// Before it, Find(ByKey("btn")).Click() activated the *cover* button and the
// harness said nothing at all: the test then failed, if it failed, on an
// assertion about the counter, and the reported cause was the application.
func TestActionOnACoveredNodeFails(t *testing.T) {
	var hit string
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: coveredButton(&hit)})
		h.Find(gifttest.ByKey("btn")).Click()
	})

	if len(r.fatals) != 1 {
		t.Fatalf("want exactly one fatal, got %d: %v", len(r.fatals), r.fatals)
	}
	msg := r.fatals[0]
	wantContains(t, "the covered node message", msg,
		"Click aimed at",
		`key="btn"`,
		"reaches",
		`key="cover"`,
		"instead",
		"covered at that point",
		"ClickAt",
		"the tree was:",
		"want=>",
		"got =>",
	)
	if hit != "" {
		t.Errorf("the cover button was activated (%q); the action must fail before it dispatches", hit)
	}
	t.Logf("the message a developer sees:\n%s", msg)
}

// TestEveryAimedActionChecksItsTarget: the check is on the action, not on
// Click, so none of the others can quietly keep the old behaviour.
func TestEveryAimedActionChecksItsTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(n gifttest.Node)
	}{
		{"Click", func(n gifttest.Node) { n.Click() }},
		{"Press", func(n gifttest.Node) { n.Press() }},
		{"Hover", func(n gifttest.Node) { n.Hover() }},
		{"Tap", func(n gifttest.Node) { n.Tap() }},
		{"LongPress", func(n gifttest.Node) { n.LongPress() }},
		{"DragTo", func(n gifttest.Node) { n.DragTo(geom.Pt(10, 10)) }},
		{"Swipe", func(n gifttest.Node) { n.Swipe(geom.Pt(0, 40)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hit string
			r := capture(func(tb gifttest.TB) {
				h := gifttest.New(tb, gifttest.Options{View: coveredButton(&hit)})
				tc.act(h.Find(gifttest.ByKey("btn")))
			})
			if len(r.fatals) != 1 {
				t.Fatalf("%s on a covered node did not fail: %v", tc.name, r.fatals)
			}
			wantContains(t, tc.name+" message", r.fatals[0],
				tc.name+" aimed at", `key="btn"`, `key="cover"`)
		})
	}
}

// TestDragChecksBothEnds. A drop onto a covered target is the same defect as a
// click on a covered button, so both ends of a Drag are verified.
func TestDragChecksBothEnds(t *testing.T) {
	var hit string
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{
			View: ui.HStack(
				ui.Button(ui.Text("from"), nil).Key("from").Frame(60, 40),
				coveredButton(&hit),
			).Gap(8),
		})
		h.Find(gifttest.ByKey("from")).Drag(h.Find(gifttest.ByKey("btn")))
	})
	if len(r.fatals) != 1 {
		t.Fatalf("Drag onto a covered node did not fail: %v", r.fatals)
	}
	wantContains(t, "the drag message", r.fatals[0], "Drag aimed at", `key="btn"`, `key="cover"`)
}

// TestClickAtIsTheDocumentedOptOut. Clicking through something on purpose is a
// real thing to want, and the coordinate taking verbs are how it is spelled.
// They perform no aim check, because there is no intended node to check.
func TestClickAtIsTheDocumentedOptOut(t *testing.T) {
	var hit string
	h := gifttest.New(t, gifttest.Options{View: coveredButton(&hit)})
	h.ClickAt(h.Find(gifttest.ByKey("btn")).Center())
	if hit != "cover" {
		t.Fatalf("ClickAt activated %q, want the cover; the opt out must dispatch what the "+
			"coordinate really hits", hit)
	}
	// And At says so without dispatching anything, which is the assertion a
	// test about overlapping controls should be written with.
	h.At(h.Find(gifttest.ByKey("btn")).Center()).AssertKey("cover")
}

// TestAnUncoveredNodeIsUnaffected: the check must not make the ordinary case
// fail. Every other test in this package is that assertion too, but a defect
// that only fires on a nested layout would be found here first.
func TestAnUncoveredNodeIsUnaffected(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	h.Find(gifttest.ByText("+")).Click()
	h.Find(gifttest.ByKey("count")).AssertText("1")
}
