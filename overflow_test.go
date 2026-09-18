package gift_test

import (
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// overflowing is a layouter that reports the size its constraints permit while
// telling gift that its content needed `over` more. It is the minimal
// implementation of the contract of [gift.LayoutContext.ReportOverflow] and
// stands in for the stack algorithm, which is tested in internal/layout.
type overflowing struct {
	over geom.Size
	w, h float32
}

func (o overflowing) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	for i := range ctx.ChildCount() {
		ctx.Measure(i, c.Loosen())
		ctx.Place(i, geom.Point{})
	}
	ctx.ReportOverflow(o.over)
	return c.Constrain(geom.Sz(o.w, o.h))
}

type overflowView struct {
	key      string
	over     geom.Size
	w, h     float32
	children []gift.View
}

func (v overflowView) ViewType() gift.TypeID { return stackType }

func (v overflowView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:      v.key,
		Layouter: overflowing{over: v.over, w: v.w, h: v.h},
		Children: v.children,
	}
}

// TestDiagnosticsCountOverflow covers rule 3 of the project plan, section 7:
// the excess is carried out of layout as a number rather than swallowed.
func TestDiagnosticsCountOverflow(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return overflowView{over: geom.Sz(30, 12), w: 100, h: 100, children: []gift.View{
			overflowView{key: "inner", over: geom.Sz(0, 8), w: 50, h: 50},
			box{w: 10, h: 10},
		}}
	}})
	mustUpdate(t, a)

	d := a.Diagnostics()
	if d.OverflowNodes != 2 {
		t.Fatalf("OverflowNodes = %d, want 2", d.OverflowNodes)
	}
	if d.OverflowExtent != 50 { // 30+12 plus 0+8
		t.Fatalf("OverflowExtent = %v, want 50", d.OverflowExtent)
	}
}

// TestOverflowIsIdempotent: the counters must not grow by one node per frame.
func TestOverflowIsIdempotent(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return overflowView{over: geom.Sz(5, 5), w: 10, h: 10}
	}})
	for range 8 {
		mustUpdate(t, a)
		a.Invalidate()
	}
	if d := a.Diagnostics(); d.OverflowNodes != 1 || d.OverflowExtent != 10 {
		t.Fatalf("after eight frames: OverflowNodes = %d, OverflowExtent = %v, want 1 and 10",
			d.OverflowNodes, d.OverflowExtent)
	}
}

// TestOverflowRejectsNonFiniteAmounts keeps an infinity out of the counters. A
// layouter is free to be wrong; the diagnostics must stay readable anyway.
func TestOverflowRejectsNonFiniteAmounts(t *testing.T) {
	var zero float32
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return overflowView{over: geom.Sz(geom.Unbounded(), zero/zero), w: 10, h: 10}
	}})
	mustUpdate(t, a)
	if d := a.Diagnostics(); d.OverflowNodes != 0 || d.OverflowExtent != 0 {
		t.Fatalf("OverflowNodes = %d, OverflowExtent = %v, want zero for a non finite report",
			d.OverflowNodes, d.OverflowExtent)
	}
}

// TestOverflowReportIsAllocationFree keeps the new accounting inside the
// 0 B/op contract of the project plan, section 11.
func TestOverflowReportIsAllocationFree(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return overflowView{over: geom.Sz(7, 9), w: 100, h: 100, children: []gift.View{
			box{w: 10, h: 10}, box{w: 10, h: 10},
		}}
	}})
	w := float32(800)
	frame := func() {
		if w == 800 {
			w = 801
		} else {
			w = 800
		}
		if err := a.Update(geom.Sz(w, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if a.Diagnostics().OverflowNodes == 0 {
		t.Fatal("the fixture does not overflow")
	}
	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("a relayout of an overflowing tree allocated %v times per run, want 0", got)
	}
}
