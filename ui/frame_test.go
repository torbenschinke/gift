package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// TestFrameMinMaxPrecedence pins the rule documented on frameSpec, on both
// axes and over all six orderings of the three modifiers.
//
// The point of running all six is that modifiers are fields, not wrappers: the
// call order must be irrelevant, and the applied order — Frame, then Max, then
// Min — must be the only thing that decides. Before this work unit the
// documentation claimed "Frame first, Min and Max afterwards and therefore
// win" while the code measured Frame(20,20).MinWidth(80) as 20 wide and
// MinWidth(80).MaxWidth(40) as 40 wide, so the minimum was lost twice over and
// neither loss was written down anywhere.
func TestFrameMinMaxPrecedence(t *testing.T) {
	type step func(ui.BoxView) ui.BoxView

	frameW := func(v float32) step {
		return func(b ui.BoxView) ui.BoxView { return b.Frame(v, v) }
	}
	minW := func(v float32) step {
		return func(b ui.BoxView) ui.BoxView { return b.MinWidth(v).MinHeight(v) }
	}
	maxW := func(v float32) step {
		return func(b ui.BoxView) ui.BoxView { return b.MaxWidth(v).MaxHeight(v) }
	}

	tests := []struct {
		name  string
		frame float32
		min   float32
		max   float32
		// hasFrame, hasMin, hasMax select which modifiers take part.
		hasFrame, hasMin, hasMax bool
		want                     float32
	}{
		{
			name: "Max beats Frame", frame: 200, max: 50,
			hasFrame: true, hasMax: true, want: 50,
		},
		{
			name: "Min beats Frame", frame: 20, min: 80,
			hasFrame: true, hasMin: true, want: 80,
		},
		{
			name: "Min beats Max", min: 80, max: 40,
			hasMin: true, hasMax: true, want: 80,
		},
		{
			name: "all three, Min last and largest", frame: 200, min: 80, max: 50,
			hasFrame: true, hasMin: true, hasMax: true, want: 80,
		},
		{
			name: "all three, Min below Max", frame: 200, min: 30, max: 50,
			hasFrame: true, hasMin: true, hasMax: true, want: 50,
		},
		{
			name: "Frame alone", frame: 64,
			hasFrame: true, want: 64,
		},
		{
			name: "Min alone on a greedy box still fills", min: 30,
			hasMin: true, want: 150,
		},
		{
			name: "Max alone clamps the greedy box", max: 30,
			hasMax: true, want: 30,
		},
	}

	// The six orderings of three modifiers. A modifier that is not selected
	// by the case is left out, which is what makes the same table cover the
	// two and one modifier cases as well.
	orders := [][3]int{
		{0, 1, 2}, {0, 2, 1}, {1, 0, 2},
		{1, 2, 0}, {2, 0, 1}, {2, 1, 0},
	}
	names := [3]string{"Frame", "Min", "Max"}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps := [3]step{}
			use := [3]bool{tc.hasFrame, tc.hasMin, tc.hasMax}
			steps[0] = frameW(tc.frame)
			steps[1] = minW(tc.min)
			steps[2] = maxW(tc.max)

			for _, ord := range orders {
				label := ""
				b := ui.Box().Background(fill)
				for _, k := range ord {
					if !use[k] {
						continue
					}
					label += names[k]
					b = steps[k](b)
				}
				// A ZStack bounds both axes, so the greedy Box is measured
				// against a real range and the result is the frame contract
				// alone.
				l := run(t, ui.ZStack(b), geom.Sz(150, 150))
				want := geom.RcXYWH(0, 0, tc.want, tc.want)
				ops := l.Ops()
				if len(ops) != 1 {
					t.Fatalf("%s: painted %d ops, want 1", label, len(ops))
				}
				if got := ops[0].Bounds; !approxRect(got, want) {
					t.Errorf("order %s: bounds = %v, want %v", label, got, want)
				}
			}
		})
	}
}

// TestFrameMinMaxIsNormalised guards the invariant the old repair pass was
// there for: whatever combination is given, the constraints handed down must
// never have a minimum above a maximum.
func TestFrameMinMaxIsNormalised(t *testing.T) {
	// A tight frame of 20 with a minimum of 80 and a maximum of 40. Min wins,
	// so both ends end up at 80 and the child is 80 wide even though every
	// other number in the expression is smaller.
	v := ui.VStack(probe(10, 10)).Frame(20, 20).MinWidth(80).MaxWidth(40).Background(fill)
	l := run(t, v, geom.Sz(200, 200))
	if got, want := l.Ops()[0].Bounds, geom.RcXYWH(0, 0, 80, 20); !approxRect(got, want) {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
}

// TestNonFiniteMinIsIgnored: Unbounded and NaN are not sizes.
func TestNonFiniteMinIsIgnored(t *testing.T) {
	v := ui.ZStack(ui.Box().Background(fill).MinWidth(geom.Unbounded()).MinHeight(nan()))
	a := gift.New(gift.Options{Root: static(v)})
	l := frame(t, a, geom.Sz(100, 60))
	if got, want := l.Ops()[0].Bounds, geom.RcXYWH(0, 0, 100, 60); !approxRect(got, want) {
		t.Fatalf("bounds = %v, want %v; a non finite minimum must be ignored, not propagated", got, want)
	}
}
