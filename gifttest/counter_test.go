package gifttest_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/gifttest"
)

// TestCounter is the worked example: the whole of the counter's behaviour,
// written the way an application developer would write it.
//
// Read it as prose. Nothing in it names a coordinate, a frame or a duration,
// and nothing in it would have to change if the counter were restyled, moved
// or wrapped in another container.
func TestCounter(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	h.Find(gifttest.ByKey("count")).AssertText("0")
	h.Find(gifttest.ByKey("minus")).AssertDisabled()

	plus := h.Find(gifttest.ByText("+"))
	plus.Click()
	plus.Click()
	plus.Click()
	h.Find(gifttest.ByKey("count")).AssertText("3")

	h.Find(gifttest.ByText("-")).Click()
	h.Find(gifttest.ByKey("count")).AssertText("2")
}

// TestCounterMinusIsDisabledAtZero is the same statement from the other side:
// the control is not merely greyed out, it is inert, it is skipped by the
// focus order, and it still swallows the click rather than letting it fall
// through.
func TestCounterMinusIsDisabledAtZero(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	minus := h.Find(gifttest.ByKey("minus"))
	minus.AssertDisabled()
	minus.Click()
	h.Find(gifttest.ByKey("count")).AssertText("0")

	// It is not focusable either, so the first tab reaches the plus button.
	h.Tab()
	h.AssertFocus(gifttest.ByKey("plus"))

	// One click on plus enables it again. The node reference is refreshed
	// because a rebuild replaced the subtree; the harness says so loudly if
	// it is not, which is the whole reason Node.check exists.
	h.Find(gifttest.ByKey("plus")).Click()
	h.Find(gifttest.ByKey("minus")).AssertEnabled()
}

// TestCounterKeyboardActivation covers the project plan, section 7, sentence
// about keyboard input: tab order, then space or enter.
func TestCounterKeyboardActivation(t *testing.T) {
	for _, k := range []struct {
		name string
		key  gift.Key
	}{{"space", gift.KeySpace}, {"enter", gift.KeyEnter}} {
		t.Run(k.name, func(t *testing.T) {
			h := gifttest.New(t, gifttest.Options{Root: counter})

			h.Tab()
			h.AssertFocus(gifttest.ByKey("plus"))
			h.Find(gifttest.ByKey("plus")).AssertFocused()

			h.Key(k.key)
			h.Find(gifttest.ByKey("count")).AssertText("1")

			// And the focus survived the rebuild the activation caused,
			// because interaction state lives in the retained node.
			h.AssertFocus(gifttest.ByKey("plus"))
		})
	}
}

// TestCounterShiftTabWalksBackwards checks the other direction of the focus
// order, once the minus button is in it.
func TestCounterShiftTabWalksBackwards(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	h.Find(gifttest.ByKey("plus")).Click()

	h.Tab()
	h.AssertFocus(gifttest.ByKey("minus"))
	h.Tab()
	h.AssertFocus(gifttest.ByKey("plus"))
	h.ShiftTab()
	h.AssertFocus(gifttest.ByKey("minus"))
}

// TestCounterHoverCostsNoRebuild is an assertion about the frame model rather
// than about the counter: hover is presentation state in the retained node, so
// moving the mouse over a button must not run a single view function. The
// project plan, section 5, requires it and gift.Diagnostics is where it is
// visible.
func TestCounterHoverCostsNoRebuild(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	before := h.Diagnostics().Builds

	plus := h.Find(gifttest.ByKey("plus"))
	plus.Hover()
	plus.AssertHovered()

	if got := h.Diagnostics().Builds; got != before {
		t.Errorf("hovering rebuilt %d scope(s); hover must not cause a build", got-before)
	}
}

// TestCounterLayoutIsHonest is the structural assertion a golden image cannot
// make and does not replace: the two buttons are the size they asked for, side
// by side with the declared gap, and nothing in the scene overflows.
func TestCounterLayoutIsHonest(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})

	minus := h.Find(gifttest.ByKey("minus")).Bounds()
	plus := h.Find(gifttest.ByKey("plus")).Bounds()

	h.Find(gifttest.ByKey("plus")).AssertSize(geomSz(56, 44))
	if gap := plus.Min.X - minus.Max.X; gap != 8 {
		t.Errorf("the gap between the buttons is %g, want the declared 8", gap)
	}
	if minus.Min.Y != plus.Min.Y {
		t.Errorf("the buttons are not on the same row: %g vs %g", minus.Min.Y, plus.Min.Y)
	}
	h.AssertNoOverflow()
}

// TestCounterDrawsItsLabel closes the loop between the tree and the frame: the
// label is not just in the tree, its glyphs reached the display list.
func TestCounterDrawsItsLabel(t *testing.T) {
	h := gifttest.New(t, gifttest.Options{Root: counter})
	h.Find(gifttest.ByKey("plus")).Click()
	h.Find(gifttest.ByKey("count")).AssertText("1").AssertDrawsGlyphs()
}
