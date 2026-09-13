package layout_test

import (
	"testing"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

const eps = 1e-4

func approx(a, b float32) bool {
	d := a - b
	return d < eps && d > -eps
}

func approxPt(a, b geom.Point) bool { return approx(a.X, b.X) && approx(a.Y, b.Y) }
func approxSz(a, b geom.Size) bool  { return approx(a.W, b.W) && approx(a.H, b.H) }

// fake is a Measurer of fixed size children. A child with a zero desired size
// simply obeys the constraints, which is what a flexible child does.
type fake struct {
	want []geom.Size
	// got records the constraints each child was measured with, in call
	// order, so that constraint propagation can be asserted directly.
	got   []geom.Constraints
	order []int
}

func (f *fake) MeasureChild(i int, c geom.Constraints) geom.Size {
	f.got = append(f.got, c)
	f.order = append(f.order, i)
	return c.Constrain(f.want[i])
}

func run(t *testing.T, spec layout.StackSpec, c geom.Constraints, want []geom.Size) (geom.Size, []geom.Point, *fake) {
	t.Helper()
	n := len(want)
	f := &fake{want: want}
	items := make([]layout.Item, n)
	origins := make([]geom.Point, n)
	res := layout.Stack(spec, c, n, f, items, origins)
	return res.Size, origins, f
}

func TestStackGaps(t *testing.T) {
	tests := []struct {
		name    string
		gap     float32
		want    []geom.Size
		wantSz  geom.Size
		wantPos []geom.Point
	}{
		{
			name: "no children", gap: 8, want: nil,
			wantSz: geom.Sz(0, 0), wantPos: nil,
		},
		{
			name: "one child has no gap", gap: 8,
			want:   []geom.Size{geom.Sz(30, 10)},
			wantSz: geom.Sz(30, 10), wantPos: []geom.Point{geom.Pt(0, 0)},
		},
		{
			name: "two children have one gap", gap: 8,
			want:   []geom.Size{geom.Sz(30, 10), geom.Sz(20, 5)},
			wantSz: geom.Sz(30, 23),
			wantPos: []geom.Point{
				geom.Pt(0, 0), geom.Pt(0, 18),
			},
		},
		{
			name: "three children have two gaps", gap: 4,
			want:   []geom.Size{geom.Sz(10, 10), geom.Sz(10, 20), geom.Sz(10, 30)},
			wantSz: geom.Sz(10, 68),
			wantPos: []geom.Point{
				geom.Pt(0, 0), geom.Pt(0, 14), geom.Pt(0, 38),
			},
		},
		{
			name: "zero gap", gap: 0,
			want:   []geom.Size{geom.Sz(10, 10), geom.Sz(10, 20)},
			wantSz: geom.Sz(10, 30),
			wantPos: []geom.Point{
				geom.Pt(0, 0), geom.Pt(0, 10),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := layout.StackSpec{Axis: layout.Vertical, Gap: tc.gap, Alignment: geom.TopLeading}
			sz, pos, _ := run(t, spec, geom.Loose(geom.Sz(200, 200)), tc.want)
			if !approxSz(sz, tc.wantSz) {
				t.Errorf("size = %v, want %v", sz, tc.wantSz)
			}
			for i := range tc.wantPos {
				if !approxPt(pos[i], tc.wantPos[i]) {
					t.Errorf("origin[%d] = %v, want %v", i, pos[i], tc.wantPos[i])
				}
			}
		})
	}
}

func TestStackPadding(t *testing.T) {
	spec := layout.StackSpec{
		Axis:      layout.Vertical,
		Gap:       10,
		Padding:   geom.Insets{Top: 1, Right: 2, Bottom: 3, Left: 4},
		Alignment: geom.TopLeading,
	}
	sz, pos, f := run(t, spec, geom.Loose(geom.Sz(200, 100)),
		[]geom.Size{geom.Sz(30, 10), geom.Sz(20, 20)})

	// Content is 30 wide and 10+10+20 = 40 high, plus 6 horizontal and 4
	// vertical padding.
	if want := geom.Sz(36, 44); !approxSz(sz, want) {
		t.Errorf("size = %v, want %v", sz, want)
	}
	if want := geom.Pt(4, 1); !approxPt(pos[0], want) {
		t.Errorf("origin[0] = %v, want %v", pos[0], want)
	}
	if want := geom.Pt(4, 21); !approxPt(pos[1], want) {
		t.Errorf("origin[1] = %v, want %v", pos[1], want)
	}

	// Both children see the padded cross extent and an unbounded main axis.
	// The second one in particular does not see "what the first one left
	// over": the project plan, section 7, "Overflow-Modell", rule 1, forbids
	// exactly that, and TestChildSizeIsIndependentOfSiblings pins it.
	want := geom.Constraints{Min: geom.Size{}, Max: geom.Sz(194, geom.Unbounded())}
	for i := range 2 {
		if f.got[i] != want {
			t.Errorf("child %d constraints = %v, want %v", i, f.got[i], want)
		}
	}
}

func TestStackFlexDistribution(t *testing.T) {
	tests := []struct {
		name         string
		flex         []float32
		want         []geom.Size
		c            geom.Constraints
		wantMain     []float32
		wantSize     geom.Size
		wantOverflow geom.Size
	}{
		{
			name: "single spacer takes the rest",
			flex: []float32{0, 1, 0},
			want: []geom.Size{geom.Sz(10, 20), {}, geom.Sz(10, 30)},
			c:    geom.Tight(geom.Sz(100, 100)),
			// 100 - 20 - 30 = 50 for the spacer.
			wantMain: []float32{20, 50, 30},
			wantSize: geom.Sz(100, 100),
		},
		{
			name: "two spacers split evenly",
			flex: []float32{1, 0, 1},
			want: []geom.Size{{}, geom.Sz(10, 40), {}},
			c:    geom.Tight(geom.Sz(100, 100)),
			// 60 free, 30 each.
			wantMain: []float32{30, 40, 30},
			wantSize: geom.Sz(100, 100),
		},
		{
			name: "proportional to flex",
			flex: []float32{1, 3},
			want: []geom.Size{{}, {}},
			c:    geom.Tight(geom.Sz(100, 80)),
			// 80 free, 20 and 60.
			wantMain: []float32{20, 60},
			wantSize: geom.Sz(100, 80),
		},
		{
			name: "no flexible child leaves the space unused",
			flex: []float32{0, 0},
			want: []geom.Size{geom.Sz(10, 20), geom.Sz(10, 30)},
			c:    geom.Loose(geom.Sz(100, 100)),
			// The stack shrinks to its content.
			wantMain: []float32{20, 30},
			wantSize: geom.Sz(10, 50),
		},
		{
			name: "unbounded main axis gives the spacer nothing",
			flex: []float32{0, 1},
			want: []geom.Size{geom.Sz(10, 20), {}},
			c:    geom.Constraints{Max: geom.Sz(100, geom.Unbounded())},
			// The spacer cannot expand into infinity.
			wantMain: []float32{20, 0},
			wantSize: geom.Sz(10, 20),
		},
		{
			name: "inflexible children overflow, flex gets nothing",
			flex: []float32{0, 1},
			want: []geom.Size{geom.Sz(10, 200), {}},
			c:    geom.Loose(geom.Sz(100, 100)),
			// The first child keeps its honest 200: it is measured with an
			// unbounded main axis and is not clamped to what is left. The
			// stack reports the 100 its constraints permit and carries the
			// difference as overflow.
			wantMain:     []float32{200, 0},
			wantSize:     geom.Sz(10, 100),
			wantOverflow: geom.Sz(0, 100),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := len(tc.want)
			f := &fake{want: tc.want}
			items := make([]layout.Item, n)
			for i, fl := range tc.flex {
				items[i].Flex = fl
			}
			origins := make([]geom.Point, n)
			spec := layout.StackSpec{Axis: layout.Vertical, Alignment: geom.TopLeading}
			res := layout.Stack(spec, tc.c, n, f, items, origins)

			if !approxSz(res.Size, tc.wantSize) {
				t.Errorf("size = %v, want %v", res.Size, tc.wantSize)
			}
			if !approxSz(res.Overflow, tc.wantOverflow) {
				t.Errorf("overflow = %v, want %v", res.Overflow, tc.wantOverflow)
			}
			var y float32
			for i := range n {
				if got := items[i].Size.H; !approx(got, tc.wantMain[i]) {
					t.Errorf("child %d main extent = %v, want %v", i, got, tc.wantMain[i])
				}
				if !approxPt(origins[i], geom.Pt(0, y)) {
					t.Errorf("origin[%d] = %v, want %v", i, origins[i], geom.Pt(0, y))
				}
				y += items[i].Size.H
			}
		})
	}
}

// TestStackFlexRemainderIsExact pins the rounding rule: whatever the shares
// come out to, the flexible extents add up to the available space exactly and
// the remainder lands on the last flexible child.
func TestStackFlexRemainderIsExact(t *testing.T) {
	const n = 7
	const total = 100
	f := &fake{want: make([]geom.Size, n)}
	items := make([]layout.Item, n)
	for i := range items {
		items[i].Flex = 1
	}
	origins := make([]geom.Point, n)
	spec := layout.StackSpec{Axis: layout.Vertical, Alignment: geom.TopLeading}
	res := layout.Stack(spec, geom.Tight(geom.Sz(10, total)), n, f, items, origins)

	if !approxSz(res.Size, geom.Sz(10, total)) {
		t.Fatalf("size = %v", res.Size)
	}
	var sum float32
	for i := range n {
		sum += items[i].Size.H
	}
	// Hand computed: 7 equal shares of 100 cannot be represented exactly, so
	// the sum is what matters, and it must be exactly 100.
	if sum != total {
		t.Errorf("flexible extents sum to %v, want exactly %v", sum, float32(total))
	}
	// The last child differs from the nominal share; that is the remainder
	// rule, and the first six are the nominal share.
	for i := range n - 1 {
		if !approx(items[i].Size.H, total/float32(n)) {
			t.Errorf("child %d = %v, want the nominal share %v", i, items[i].Size.H, total/float32(n))
		}
	}
	// The children must be flush: the origin of the last one is the sum of
	// the ones before it.
	if !approx(origins[n-1].Y, sum-items[n-1].Size.H) {
		t.Errorf("origin of the last child = %v, want %v", origins[n-1].Y, sum-items[n-1].Size.H)
	}
}

func TestStackCrossAlignment(t *testing.T) {
	// A vertical stack aligns on x, a horizontal stack on y. All nine
	// alignments are exercised through both.
	aligns := []struct {
		name string
		a    geom.Alignment
	}{
		{"TopLeading", geom.TopLeading}, {"Top", geom.Top}, {"TopTrailing", geom.TopTrailing},
		{"Leading", geom.Leading}, {"Center", geom.Center}, {"Trailing", geom.Trailing},
		{"BottomLeading", geom.BottomLeading}, {"Bottom", geom.Bottom}, {"BottomTrailing", geom.BottomTrailing},
	}
	// The wide child fixes the cross extent at 40, the narrow one is placed
	// inside it.
	want := []geom.Size{geom.Sz(40, 10), geom.Sz(10, 10)}

	for _, tc := range aligns {
		t.Run("vertical/"+tc.name, func(t *testing.T) {
			spec := layout.StackSpec{Axis: layout.Vertical, Alignment: tc.a}
			sz, pos, _ := run(t, spec, geom.Loose(geom.Sz(200, 200)), want)
			if !approxSz(sz, geom.Sz(40, 20)) {
				t.Fatalf("size = %v", sz)
			}
			if wantX := 30 * tc.a.X; !approx(pos[1].X, wantX) {
				t.Errorf("x = %v, want %v", pos[1].X, wantX)
			}
			if !approx(pos[1].Y, 10) {
				t.Errorf("y = %v, want 10", pos[1].Y)
			}
		})
		t.Run("horizontal/"+tc.name, func(t *testing.T) {
			spec := layout.StackSpec{Axis: layout.Horizontal, Alignment: tc.a}
			sz, pos, _ := run(t, spec, geom.Loose(geom.Sz(200, 200)),
				[]geom.Size{geom.Sz(10, 40), geom.Sz(10, 10)})
			if !approxSz(sz, geom.Sz(20, 40)) {
				t.Fatalf("size = %v", sz)
			}
			if wantY := 30 * tc.a.Y; !approx(pos[1].Y, wantY) {
				t.Errorf("y = %v, want %v", pos[1].Y, wantY)
			}
			if !approx(pos[1].X, 10) {
				t.Errorf("x = %v, want 10", pos[1].X)
			}
		})
	}
}

func TestStackShortScratchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	layout.Stack(layout.StackSpec{}, geom.Loose(geom.Sz(10, 10)), 3,
		&fake{want: make([]geom.Size, 3)}, make([]layout.Item, 2), make([]geom.Point, 3))
}

func TestOverlay(t *testing.T) {
	want := []geom.Size{geom.Sz(40, 30), geom.Sz(10, 10)}
	n := len(want)
	f := &fake{want: want}
	items := make([]layout.Item, n)
	origins := make([]geom.Point, n)

	res := layout.Overlay(geom.InsetsAll(5), geom.Center, geom.Loose(geom.Sz(200, 200)), n, f, items, origins)
	if got := (geom.Sz(50, 40)); !approxSz(res.Size, got) {
		t.Fatalf("size = %v, want %v", res.Size, got)
	}
	if (res.Overflow != geom.Size{}) {
		t.Errorf("overflow = %v, want none", res.Overflow)
	}
	if !approxPt(origins[0], geom.Pt(5, 5)) {
		t.Errorf("origin[0] = %v, want (5,5)", origins[0])
	}
	// The small child is centred in the 40x30 content box.
	if !approxPt(origins[1], geom.Pt(20, 15)) {
		t.Errorf("origin[1] = %v, want (20,15)", origins[1])
	}
	// Every child sees the same, loosened constraints.
	for i, c := range f.got {
		if want := geom.Loose(geom.Sz(190, 190)); c != want {
			t.Errorf("child %d constraints = %v, want %v", i, c, want)
		}
	}
}

func TestOverlayAlignments(t *testing.T) {
	for _, a := range []geom.Alignment{
		geom.TopLeading, geom.Top, geom.TopTrailing,
		geom.Leading, geom.Center, geom.Trailing,
		geom.BottomLeading, geom.Bottom, geom.BottomTrailing,
	} {
		f := &fake{want: []geom.Size{geom.Sz(40, 40), geom.Sz(10, 20)}}
		items := make([]layout.Item, 2)
		origins := make([]geom.Point, 2)
		layout.Overlay(geom.Insets{}, a, geom.Loose(geom.Sz(100, 100)), 2, f, items, origins)
		want := geom.Pt(30*a.X, 20*a.Y)
		if !approxPt(origins[1], want) {
			t.Errorf("alignment %v: origin = %v, want %v", a, origins[1], want)
		}
	}
}

// noallocMeasurer answers without touching the heap. It is used through a
// pointer because boxing a non pointer struct into an interface is itself an
// allocation, and the subject of this test is the algorithm.
type noallocMeasurer struct{ h float32 }

func (m *noallocMeasurer) MeasureChild(_ int, c geom.Constraints) geom.Size {
	return c.Constrain(geom.Sz(10, m.h))
}

func TestStackIsAllocationFree(t *testing.T) {
	const n = 32
	items := make([]layout.Item, n)
	origins := make([]geom.Point, n)
	for i := range items {
		if i%8 == 0 {
			items[i].Flex = 1
		}
	}
	m := &noallocMeasurer{h: 7}
	spec := layout.StackSpec{Axis: layout.Vertical, Gap: 3, Padding: geom.InsetsAll(4), Alignment: geom.Center}
	c := geom.Tight(geom.Sz(400, 900))

	work := func() {
		layout.Stack(spec, c, n, m, items, origins)
		layout.Overlay(geom.InsetsAll(2), geom.Center, c, n, m, items, origins)
	}
	work()
	if got := testing.AllocsPerRun(200, work); got != 0 {
		t.Fatalf("AllocsPerRun = %v, want 0", got)
	}
}

// greedy is a Measurer that deliberately disobeys its constraints: it returns
// want[i] whatever it is offered.
//
// Every other measurer in this file ends in c.Constrain, which means no test
// here ever saw a child that returned more than it was given — which is
// exactly why an entire class of overflow defects was invisible to this
// package. [gift.Layouter] explicitly permits a constraint violating size and
// requires it to be passed through unchanged, so this is a legal child, not a
// pathological one.
type greedy struct {
	want []geom.Size
	got  []geom.Constraints
}

func (g *greedy) MeasureChild(i int, c geom.Constraints) geom.Size {
	g.got = append(g.got, c)
	return g.want[i]
}

// TestChildSizeIsIndependentOfSiblings is the regression test for the blocker
// of review gate 2 and for rule 1 of the project plan, section 7,
// "Overflow-Modell": the constraints a child sees must not depend on how many
// siblings precede it.
//
// The defect it pins was not theoretical. A stack handed child i a main axis
// maximum of a monotonically shrinking remainder; once that hit zero, every
// further child reported a size of zero while its own fixed size children kept
// their real extents and were painted, unclipped, below a parent that claimed
// to be empty. Forty rows in a bounded viewport turned into ten rows and a
// pile.
func TestChildSizeIsIndependentOfSiblings(t *testing.T) {
	const n = 40
	const each = 30
	// Forty children of thirty each need 1200; the stack is given 400.
	want := make([]geom.Size, n)
	for i := range want {
		want[i] = geom.Sz(10, each)
	}
	f := &fake{want: want}
	items := make([]layout.Item, n)
	origins := make([]geom.Point, n)
	spec := layout.StackSpec{Axis: layout.Vertical, Alignment: geom.TopLeading}
	res := layout.Stack(spec, geom.Loose(geom.Sz(100, 400)), n, f, items, origins)

	for i := range n {
		if got := items[i].Size.H; !approx(got, each) {
			t.Fatalf("child %d collapsed to %v, want %v; a later child must not be starved by its predecessors",
				i, got, float32(each))
		}
		if want := float32(i * each); !approx(origins[i].Y, want) {
			t.Errorf("origin[%d].Y = %v, want %v", i, origins[i].Y, want)
		}
		if c := f.got[i]; c != f.got[0] {
			t.Errorf("child %d was measured with %v, child 0 with %v; the constraints must be identical",
				i, c, f.got[0])
		}
	}
	if !approxSz(res.Size, geom.Sz(10, 400)) {
		t.Errorf("size = %v, want the permitted 10x400", res.Size)
	}
	if !approxSz(res.Overflow, geom.Sz(0, n*each-400)) {
		t.Errorf("overflow = %v, want 0x%v", res.Overflow, float32(n*each-400))
	}
}

// TestStackChildrenAreFlush pins the second symptom of the same defect:
// VStack(40,40,40).Gap(10).Frame(50,100) reported a height of 100 and placed
// its children at 0..40, 50..90 and 100..140, so the sum of the children plus
// the gaps did not equal the reported size and nothing said why.
//
// It still does not equal it — the stack cannot report 140 when it was told
// 100 — but the difference is now exactly the reported overflow.
func TestStackChildrenAreFlush(t *testing.T) {
	want := []geom.Size{geom.Sz(10, 40), geom.Sz(10, 40), geom.Sz(10, 40)}
	f := &fake{want: want}
	items := make([]layout.Item, 3)
	origins := make([]geom.Point, 3)
	spec := layout.StackSpec{Axis: layout.Vertical, Gap: 10, Alignment: geom.TopLeading}
	res := layout.Stack(spec, geom.Tight(geom.Sz(50, 100)), 3, f, items, origins)

	if !approxSz(res.Size, geom.Sz(50, 100)) {
		t.Fatalf("size = %v, want 50x100", res.Size)
	}
	// Content: 3*40 + 2*10 = 140. Overflow: 140 - 100 = 40.
	if !approxSz(res.Overflow, geom.Sz(0, 40)) {
		t.Fatalf("overflow = %v, want 0x40", res.Overflow)
	}
	var end float32
	for i := range 3 {
		if i > 0 {
			if !approx(origins[i].Y, end+10) {
				t.Errorf("origin[%d].Y = %v, want %v", i, origins[i].Y, end+10)
			}
		}
		end = origins[i].Y + items[i].Size.H
	}
	// end is the bottom of the last child. size + overflow must equal it.
	if got, want := res.Size.H+res.Overflow.H, end; !approx(got, want) {
		t.Errorf("size + overflow = %v, want the content extent %v", got, want)
	}
}

// TestStackOverflowFromADisobedientChild uses the greedy measurer: the child
// returns more than it was offered on both axes, which is legal.
func TestStackOverflowFromADisobedientChild(t *testing.T) {
	g := &greedy{want: []geom.Size{geom.Sz(300, 60), geom.Sz(40, 60)}}
	items := make([]layout.Item, 2)
	origins := make([]geom.Point, 2)
	spec := layout.StackSpec{Axis: layout.Vertical, Padding: geom.InsetsAll(5), Alignment: geom.TopLeading}
	res := layout.Stack(spec, geom.Loose(geom.Sz(100, 100)), 2, g, items, origins)

	// Cross: the widest child is 300, plus 10 padding, against a permitted
	// 100. Main: 120 plus 10 padding against 100.
	if !approxSz(res.Size, geom.Sz(100, 100)) {
		t.Fatalf("size = %v, want the permitted 100x100", res.Size)
	}
	if !approxSz(res.Overflow, geom.Sz(210, 30)) {
		t.Fatalf("overflow = %v, want 210x30", res.Overflow)
	}
	if !res.IsOverflowing() {
		t.Error("IsOverflowing = false")
	}
	// The children are still where they honestly belong.
	if !approxPt(origins[0], geom.Pt(5, 5)) || !approxPt(origins[1], geom.Pt(5, 65)) {
		t.Errorf("origins = %v, %v; a disobedient child must not move its siblings on top of each other",
			origins[0], origins[1])
	}
}

// TestOverlayOverflow is the same for the Z stack algorithm.
func TestOverlayOverflow(t *testing.T) {
	g := &greedy{want: []geom.Size{geom.Sz(30, 30), geom.Sz(140, 60)}}
	items := make([]layout.Item, 2)
	origins := make([]geom.Point, 2)
	res := layout.Overlay(geom.InsetsAll(5), geom.Center, geom.Loose(geom.Sz(100, 100)), 2, g, items, origins)

	// Content is 150x70; only the horizontal axis exceeds the permitted 100,
	// so the reported height is the honest 70.
	if !approxSz(res.Size, geom.Sz(100, 70)) {
		t.Fatalf("size = %v, want 100x70", res.Size)
	}
	if !approxSz(res.Overflow, geom.Sz(50, 0)) {
		t.Fatalf("overflow = %v, want 50x0", res.Overflow)
	}
	// Both axes are bounded for an overlay child; an unbounded axis here
	// would make an unframed greedy child collapse instead of filling.
	for i, c := range g.got {
		if !c.HasBoundedWidth() || !c.HasBoundedHeight() {
			t.Errorf("child %d was measured with %v, want both axes bounded", i, c)
		}
	}
}

// TestStackOverflowIsNotReportedForAnUnboundedAxis: an axis that may grow
// arbitrarily cannot overflow, and in particular must never report an infinite
// overflow.
func TestStackOverflowIsNotReportedForAnUnboundedAxis(t *testing.T) {
	g := &greedy{want: []geom.Size{geom.Sz(400, 400)}}
	items := make([]layout.Item, 1)
	origins := make([]geom.Point, 1)
	spec := layout.StackSpec{Axis: layout.Vertical}
	res := layout.Stack(spec, geom.Unconstrained(), 1, g, items, origins)
	if (res.Overflow != geom.Size{}) {
		t.Fatalf("overflow = %v, want none", res.Overflow)
	}
	if !approxSz(res.Size, geom.Sz(400, 400)) {
		t.Errorf("size = %v", res.Size)
	}
}

// TestOverflowIsAllocationFree keeps the new accounting inside the zero
// allocation contract of the project plan, section 11: Result is a value and
// nothing on the overflow path escapes.
func TestOverflowIsAllocationFree(t *testing.T) {
	const n = 16
	items := make([]layout.Item, n)
	origins := make([]geom.Point, n)
	m := &noallocMeasurer{h: 90} // 16*90 = 1440 into 300: overflowing every time
	spec := layout.StackSpec{Axis: layout.Vertical, Gap: 2, Alignment: geom.Center}
	c := geom.Tight(geom.Sz(100, 300))
	var sink layout.Result
	work := func() {
		sink = layout.Stack(spec, c, n, m, items, origins)
	}
	work()
	if !sink.IsOverflowing() {
		t.Fatal("the fixture does not overflow, the measurement would be meaningless")
	}
	if got := testing.AllocsPerRun(200, work); got != 0 {
		t.Fatalf("AllocsPerRun = %v, want 0", got)
	}
}
