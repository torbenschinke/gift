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
	sz := layout.Stack(spec, c, n, f, items, origins)
	return sz, origins, f
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

	// The first child sees the padded cross extent and the padded main room
	// minus the single gap.
	if want := (geom.Constraints{Min: geom.Size{}, Max: geom.Sz(194, 86)}); f.got[0] != want {
		t.Errorf("child 0 constraints = %v, want %v", f.got[0], want)
	}
	// The second child sees what the first one left over.
	if want := (geom.Constraints{Min: geom.Size{}, Max: geom.Sz(194, 76)}); f.got[1] != want {
		t.Errorf("child 1 constraints = %v, want %v", f.got[1], want)
	}
}

func TestStackFlexDistribution(t *testing.T) {
	tests := []struct {
		name     string
		flex     []float32
		want     []geom.Size
		c        geom.Constraints
		wantMain []float32
		wantSize geom.Size
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
			// The first child is clamped to the 100 that are available.
			wantMain: []float32{100, 0},
			wantSize: geom.Sz(10, 100),
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
			sz := layout.Stack(spec, tc.c, n, f, items, origins)

			if !approxSz(sz, tc.wantSize) {
				t.Errorf("size = %v, want %v", sz, tc.wantSize)
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
	sz := layout.Stack(spec, geom.Tight(geom.Sz(10, total)), n, f, items, origins)

	if !approxSz(sz, geom.Sz(10, total)) {
		t.Fatalf("size = %v", sz)
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

	sz := layout.Overlay(geom.InsetsAll(5), geom.Center, geom.Loose(geom.Sz(200, 200)), n, f, items, origins)
	if got := (geom.Sz(50, 40)); !approxSz(sz, got) {
		t.Fatalf("size = %v, want %v", sz, got)
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
