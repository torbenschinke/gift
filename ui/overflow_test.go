package ui_test

import (
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/ui"
)

// TestBoxGreedySizing pins the sizing rule of [ui.BoxView] against the rule of
// the project plan, section 7, "Overflow-Modell": a Box is greedy on every
// bounded axis, a stack measures an inflexible child with an unbounded main
// axis, and therefore a Box in a stack fills the cross axis and collapses on
// the main one.
//
// Both godocs — ui/box.go and the kindBox branch in ui/node.go — asserted
// exactly this before it was true. A bounded stack starved the siblings that
// followed a Box, and an unframed Box in a VStack ate the whole stack and
// ejected everything after it. These cases are the evidence that the sentence
// is no longer a claim.
func TestBoxGreedySizing(t *testing.T) {
	tests := []struct {
		name string
		view gift.View
		want []geom.Rect
	}{
		{
			name: "as the root both axes are bounded",
			view: ui.Box().Background(fill),
			want: []geom.Rect{geom.RcXYWH(0, 0, 200, 100)},
		},
		{
			name: "in a VStack it fills the width and collapses the height",
			view: ui.VStack(ui.Box().Background(fill)),
			want: []geom.Rect{geom.RcXYWH(0, 0, 200, 0)},
		},
		{
			name: "in an HStack it fills the height and collapses the width",
			view: ui.HStack(ui.Box().Background(fill)),
			want: []geom.Rect{geom.RcXYWH(0, 0, 0, 100)},
		},
		{
			name: "in a ZStack both axes are bounded, so it fills",
			view: ui.ZStack(ui.Box().Background(fill)),
			want: []geom.Rect{geom.RcXYWH(0, 0, 200, 100)},
		},
		{
			name: "a ZStack plate sits behind its sibling",
			view: ui.ZStack(ui.Box().Background(fill), probe(20, 10)),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 200, 100),
				geom.RcXYWH(0, 0, 20, 10),
			},
		},
		{
			name: "a sized sibling in a VStack keeps its size and its place",
			// This is the regression: the Box used to consume the whole main
			// axis and the sized sibling was measured against nothing left.
			view: ui.VStack(ui.Box().Background(fill), probe(20, 10)),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 200, 0),
				geom.RcXYWH(0, 0, 20, 10),
			},
		},
		{
			name: "the Box after a sized sibling is unaffected too",
			view: ui.VStack(probe(20, 10), ui.Box().Background(fill)),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 20, 10),
				geom.RcXYWH(0, 10, 200, 0),
			},
		},
		{
			name: "a sized sibling in an HStack keeps its size and its place",
			view: ui.HStack(ui.Box().Background(fill), probe(20, 10)),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 0, 100),
				geom.RcXYWH(0, 0, 20, 10),
			},
		},
		{
			name: "Flex is how a Box takes main axis space",
			view: ui.VStack(probe(20, 10), ui.Box().Background(fill).Flex(1)).Frame(200, 100),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 20, 10),
				geom.RcXYWH(0, 10, 200, 90),
			},
		},
		{
			name: "Frame is the other way",
			view: ui.VStack(ui.Box().Background(fill).Frame(geom.Unbounded(), 30), probe(20, 10)),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 200, 30),
				geom.RcXYWH(0, 30, 20, 10),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantBounds(t, run(t, tc.view, geom.Sz(200, 100)), tc.want...)
		})
	}
}

// TestOverflowIsCounted covers rule 3 of the overflow model: the excess is a
// number in [gift.Diagnostics] rather than a silently collapsed row.
func TestOverflowIsCounted(t *testing.T) {
	// Forty rows of forty pixels with a gap of eight into a frame of three
	// hundred: 40*40 + 39*8 = 1912 against 300, so 1612 of overflow on one
	// node.
	rows := make([]gift.View, 0, 40)
	for range 40 {
		rows = append(rows, ui.Box().Frame(100, 40).Background(fill))
	}
	v := ui.VStack(rows...).Gap(8).Frame(200, 300)

	a := gift.New(gift.Options{Root: static(v)})
	frame(t, a, geom.Sz(400, 400))

	d := a.Diagnostics()
	if d.OverflowNodes != 1 {
		t.Fatalf("OverflowNodes = %d, want 1", d.OverflowNodes)
	}
	if want := float32(1912 - 300); !approx(d.OverflowExtent, want) {
		t.Fatalf("OverflowExtent = %v, want %v", d.OverflowExtent, want)
	}
}

// TestNoOverflowInASceneThatFits is the other direction, and it is the
// assertion a regression test wants: a healthy scene reports zero.
func TestNoOverflowInASceneThatFits(t *testing.T) {
	v := ui.VStack(
		ui.HStack(probe(30, 10), probe(20, 10)).Gap(5),
		ui.ZStack(ui.Box().Background(fill), probe(10, 10)),
		ui.Spacer(),
	).Gap(4).Padding(6).Frame(200, 200)

	a := gift.New(gift.Options{Root: static(v)})
	frame(t, a, geom.Sz(400, 400))
	if d := a.Diagnostics(); d.OverflowNodes != 0 || d.OverflowExtent != 0 {
		t.Fatalf("OverflowNodes = %d, OverflowExtent = %v, want zero", d.OverflowNodes, d.OverflowExtent)
	}
}

// TestOverflowIsClearedWhenItGoesAway proves the counters are state of the
// tree and not a monotonic tally: growing the viewport until the content fits
// must bring them back to zero.
func TestOverflowIsClearedWhenItGoesAway(t *testing.T) {
	v := ui.VStack(probe(10, 100), probe(10, 100))
	a := gift.New(gift.Options{Root: static(v)})

	frame(t, a, geom.Sz(50, 150))
	if d := a.Diagnostics(); d.OverflowNodes != 1 || !approx(d.OverflowExtent, 50) {
		t.Fatalf("small viewport: OverflowNodes = %d, OverflowExtent = %v, want 1 and 50",
			d.OverflowNodes, d.OverflowExtent)
	}
	frame(t, a, geom.Sz(50, 400))
	if d := a.Diagnostics(); d.OverflowNodes != 0 || d.OverflowExtent != 0 {
		t.Fatalf("large viewport: OverflowNodes = %d, OverflowExtent = %v, want zero",
			d.OverflowNodes, d.OverflowExtent)
	}
}

// TestOverflowIsNotClipped pins rule 4: the children keep their honest bounds
// and gift does not push a clip behind the application's back.
func TestOverflowIsNotClipped(t *testing.T) {
	v := ui.VStack(probe(10, 100), probe(10, 100)).Frame(50, 50)
	l := run(t, v, geom.Sz(200, 200))
	wantBounds(t, l,
		geom.RcXYWH(0, 0, 10, 100),
		geom.RcXYWH(0, 100, 10, 100),
	)
	for i, op := range l.Ops() {
		if op.Clip != 0 {
			t.Errorf("ops[%d] was clipped automatically; Clip(true) is the only thing that may clip", i)
		}
	}
}

// TestOverflowOfAnUnmountedNodeIsForgotten: the counters must not leak when a
// subtree disappears, or a list that scrolls would accumulate phantom
// overflow forever.
func TestOverflowOfAnUnmountedNodeIsForgotten(t *testing.T) {
	big := true
	root := func(*gift.Context) gift.View {
		if big {
			return ui.VStack(ui.VStack(probe(10, 100), probe(10, 100)).Frame(50, 50))
		}
		return ui.VStack(probe(10, 10))
	}
	a := gift.New(gift.Options{Root: root})
	frame(t, a, geom.Sz(200, 200))
	if d := a.Diagnostics(); d.OverflowNodes == 0 {
		t.Fatal("the fixture does not overflow")
	}
	big = false
	a.Invalidate()
	frame(t, a, geom.Sz(200, 200))
	if d := a.Diagnostics(); d.OverflowNodes != 0 || d.OverflowExtent != 0 {
		t.Fatalf("after the unmount: OverflowNodes = %d, OverflowExtent = %v, want zero",
			d.OverflowNodes, d.OverflowExtent)
	}
}

// TestGapAndPaddingReject covers the decision on non finite and negative
// values: a negative gap is legal and overlaps, everything non finite panics
// at the call site rather than producing NaN vertices later.
func TestGapAndPaddingReject(t *testing.T) {
	t.Run("a negative gap overlaps the children", func(t *testing.T) {
		v := ui.VStack(probe(10, 20), probe(10, 20)).Gap(-5)
		wantBounds(t, run(t, v, geom.Sz(200, 200)),
			geom.RcXYWH(0, 0, 10, 20),
			geom.RcXYWH(0, 15, 10, 20),
		)
	})

	panics := []struct {
		name string
		fn   func()
	}{
		{"infinite gap", func() { ui.VStack().Gap(geom.Unbounded()) }},
		{"NaN gap", func() { ui.VStack().Gap(nan()) }},
		{"infinite padding", func() { ui.VStack().Padding(geom.Unbounded()) }},
		{"negative padding", func() { ui.VStack().Padding(-4) }},
		{"negative padding insets", func() { ui.ZStack().PaddingInsets(geom.Insets{Top: -1}) }},
		{"infinite box padding", func() { ui.Box().Padding(geom.Unbounded()) }},
	}
	for _, tc := range panics {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("want a panic")
				}
			}()
			tc.fn()
		})
	}
}

func nan() float32 {
	var z float32
	return z / z
}
