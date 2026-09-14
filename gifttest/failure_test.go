package gifttest_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// twoDeleteButtons is the ambiguity every real UI test suite runs into: a list
// whose rows all carry the same verb.
func twoDeleteButtons() gift.View {
	return ui.VStack(
		ui.HStack(ui.Text("Invoice 7"), ui.Button(ui.Text("Delete"), nil)).Key("row-7").Gap(8),
		ui.HStack(ui.Text("Invoice 8"), ui.Button(ui.Text("Delete"), nil)).Key("row-8").Gap(8),
	).Gap(4).Padding(24)
}

// TestAmbiguousSelectorNamesBothNodes is the assertion the brief asks for, and
// the reason this package takes a [gifttest.TB] rather than testing.TB.
//
// The requirement is not "it fails". It is that the failure contains enough to
// fix it without opening a debugger: both matches with their bounds, the tree
// with the matches marked so the distinguishing ancestor is visible, and the
// list of ways to narrow the query.
func TestAmbiguousSelectorNamesBothNodes(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: twoDeleteButtons()})
		h.Find(gifttest.ByText("Delete"))
	})

	if len(r.fatals) != 1 {
		t.Fatalf("want exactly one fatal, got %d: %v", len(r.fatals), r.fatals)
	}
	msg := r.fatals[0]
	wantContains(t, "the ambiguity message", msg,
		`Find(text=="Delete") matched 2 nodes, want exactly 1`,
		"matches:",
		"[0] ui.Text",
		"[1] ui.Text",
		"bounds=",
		`.Key("...")`,
		"the tree was:",
		"=> ",
		`key="row-7"`,
		`key="row-8"`,
	)
	t.Logf("the message a developer sees:\n%s", msg)
}

// TestAmbiguityIsResolvedByNarrowing is the other half: the hint the message
// gives actually works.
func TestAmbiguityIsResolvedByNarrowing(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{View: twoDeleteButtons()})

	h.AssertCount(gifttest.ByText("Delete"), 2)
	h.Find(gifttest.ByText("Delete").And(gifttest.Under(gifttest.ByKey("row-8")))).
		AssertText("Delete")
	// And First is the deliberate way to say "any of them will do".
	h.First(gifttest.ByText("Delete")).AssertText("Delete")
}

// TestMissingSelectorShowsWhatDoesExist covers the other common failure. A
// test looking for "Save" gets the labels that are there, which is where the
// typo, the trailing space or the capitalisation is visible.
func TestMissingSelectorShowsWhatDoesExist(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: twoDeleteButtons()})
		h.Find(gifttest.ByText("delete"))
	})
	wantContains(t, "the empty match message", r.all(),
		`Find(text=="delete") matched no node`,
		"labels that do exist:",
		`"Delete"`,
		`"Invoice 7"`,
	)
}

// TestUnknownTypeNameIsDistinguishedFromAbsence is the failure that is
// otherwise indistinguishable: "ui.Buton" matching nothing looks exactly like
// a button that is not on screen.
func TestUnknownTypeNameIsDistinguishedFromAbsence(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: twoDeleteButtons()})
		h.Find(gifttest.ByType("ui.Buton"))
	})
	wantContains(t, "the unknown type message", r.all(),
		"No view type is registered under",
		"ui.Button",
		"ui.Text",
	)
}

// TestMissingKeyListsTheKeys is the same courtesy for ByKey.
func TestMissingKeyListsTheKeys(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: twoDeleteButtons()})
		h.Find(gifttest.ByKey("row-9"))
	})
	wantContains(t, "the missing key message", r.all(),
		"keys that do exist:", `"row-7"`, `"row-8"`)
}

// TestActionOnANonInteractiveNodeExplains covers the one place where the
// automatic climb to an interactive ancestor cannot help: a plain label with
// no control above it.
func TestActionOnANonInteractiveNodeExplains(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		h := gifttest.New(tb, gifttest.Options{View: ui.VStack(ui.Text("just a label"))})
		h.Find(gifttest.ByText("just a label")).Click()
	})
	wantContains(t, "the non interactive message", r.all(),
		"not interactive and has no interactive ancestor",
		"gift.Interactor",
		"ui.Button")
}

// --- Settle -----------------------------------------------------------------

// runaway is the bug this harness has to catch rather than hang on: a
// component whose build writes the state its build reads. In a window it looks
// like a program that is merely busy.
func runaway(ctx *gift.Context) gift.View {
	n := ctx.State("n", 0)
	v := ctx.Read(n)
	n.Set(v + 1)
	return ui.Text("forever")
}

// TestSettleCatchesAnInfiniteRebuild is the assertion that the bound on
// [gifttest.Harness.Settle] is a feature.
//
// Without it this test would hang until the package timeout and print nothing
// useful. With it, it fails in a few milliseconds with the two numbers that
// identify the problem — how many scopes the last frame still rebuilt, and how
// many builds it took to get there — plus the tree.
func TestSettleCatchesAnInfiniteRebuild(t *testing.T) {
	r := capture(func(tb gifttest.TB) {
		gifttest.New(tb, gifttest.Options{Root: runaway, MaxFrames: 8})
	})
	wantContains(t, "the runaway message", r.all(),
		"did not settle within 8 frames",
		"invalidates itself every frame",
		"writes a state its build reads",
		"the tree was:")
	t.Logf("the message a developer sees:\n%s", r.all())
}

// TestSettleReturnsPromptlyForAnHonestView is the control: a normal view
// settles, and it does so in a small number of frames rather than by
// exhausting the bound.
func TestSettleReturnsPromptlyForAnHonestView(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	before := h.Diagnostics().Frames
	h.Find(gifttest.ByKey("plus")).Click()
	if got := h.Diagnostics().Frames - before; got > 6 {
		t.Errorf("a click cost %d frames to settle; something is invalidating more than it should", got)
	}
}

// TestFrameIsExactlyOne pins the other frame verb: a test that counts frames
// needs one that does not settle.
func TestFrameIsExactlyOne(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	before := h.Diagnostics().Frames
	h.Frame()
	if got := h.Diagnostics().Frames - before; got != 1 {
		t.Errorf("Frame advanced %d frames, want 1", got)
	}
}

// TestDumpShowsTheStateOfTheWorld is a small guarantee about the diagnostic
// itself: it names types, keys, labels, bounds and the input flags, because a
// dump that omits the flag you are debugging is a dump you cannot use.
func TestDumpShowsTheStateOfTheWorld(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	h.Find(gifttest.ByKey("plus")).Hover()
	dump := h.Dump()
	wantContains(t, "the dump", dump,
		"gift.Component", "ui.VStack", "ui.Button", "ui.Text",
		`key="plus"`, `text="+"`, "bounds=", "interactive", "hover", "disabled")
	t.Logf("Dump:\n%s", dump)
}
