package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// TestEveryControlRefusesToBeSmallerThanTheThingItDraws is the rule of
// controlSize applied to all four controls that have it, in the one case that
// produced a silent defect: a frame of zero on an axis.
//
// # Why all four and not only the slider
//
// Because the slider was not special. It was simply the one that shipped:
// cmd/example-kitchensink asked for `.Frame(geom.Unbounded(), 0)` and got a
// track a finger could find and could not move, because the knob was clamped
// into a zero height rectangle. [ui.ToggleView], [ui.SegmentedControlView] and
// [ui.ProgressBarView] all clamp or fill in exactly the same way and had
// exactly the same exposure; the only reason none of them was reported is that
// nobody wrote the same line for them.
//
// # What is asserted
//
// Three things per control, and each one fails a different way of getting it
// wrong: the node is at least as large as what it draws; the shortfall against
// the frame is *reported* as an overflow rather than swallowed, which is the
// overflow model of the project plan, section 7, one level down; and something
// with a positive area is actually emitted, which is the property the defect
// broke and which no amount of layout arithmetic implies.
func TestEveryControlRefusesToBeSmallerThanTheThingItDraws(t *testing.T) {
	cases := []struct {
		name string
		view func() gift.View
		min  geom.Size
	}{
		{"Slider", func() gift.View {
			return ui.Slider(0.5, nil).Key("c").Frame(geom.Unbounded(), 0)
		}, geom.Sz(28, 28)},
		{"Toggle", func() gift.View {
			return ui.Toggle(true, nil).Key("c").Frame(0, 0)
		}, geom.Sz(51, 31)},
		{"SegmentedControl", func() gift.View {
			return ui.SegmentedControl(0, []string{"A", "B"}, nil).Key("c").Frame(geom.Unbounded(), 0)
		}, geom.Sz(4, 4)},
		{"ProgressBar", func() gift.View {
			return ui.ProgressBar(0.5).Key("c").Frame(120, 0)
		}, geom.Sz(1, 1)},
	}
	for _, c := range cases {
		// One flat loop and no t.Run: a gift.App belongs to the goroutine that
		// created it, and a subtest runs on one of its own.
		h := gifttest.New(t, gifttest.Options{
			Theme: ui.LightTheme(),
			Font:  loadTestFont(t),
			Size:  geom.Sz(400, 200),
			View:  ui.VStack(c.view()).Padding(20),
		})
		b := h.Find(gifttest.ByKey("c")).Bounds()
		if b.Height() < c.min.H || b.Width() < c.min.W {
			t.Errorf("%s framed to zero is %v; it must be at least %v, which is what it draws",
				c.name, b, c.min)
		}
		if dg := h.Diagnostics(); dg.OverflowNodes == 0 {
			t.Errorf("%s framed to zero reports no overflow; a control that quietly ignores "+
				"its frame is a layout nobody can debug", c.name)
		}
		var painted int
		for _, op := range h.Ops() {
			if op.Kind == render.OpFillRoundRect && op.Bounds.Width() > 0 && op.Bounds.Height() > 0 {
				painted++
			}
		}
		if painted == 0 {
			t.Errorf("%s framed to zero drew nothing with an area. That is the defect this "+
				"rule exists for: the control lays out, hit tests, and is invisible.\n%s",
				c.name, h.Dump())
		}
	}
}
