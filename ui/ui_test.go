package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
	"github.com/torbenschinke/gift/ui"
)

// fill is the colour every probe box is painted with. A box needs a
// background to become visible in the display list, which is how these tests
// observe the layout result.
var fill = ui.RGB(10, 20, 30)

// probe is a fixed size, visible box.
func probe(w, h float32) ui.BoxView { return ui.Box().Frame(w, h).Background(fill) }

// static returns a root component that always returns v.
func static(v gift.View) func(*gift.Context) gift.View {
	return func(*gift.Context) gift.View { return v }
}

// frame drives one update and one paint and returns the display list.
func frame(t *testing.T, a *gift.App, size geom.Size) *render.List {
	t.Helper()
	if err := a.Update(size); err != nil {
		t.Fatal(err)
	}
	return a.Paint()
}

// run mounts v, draws it once and returns the display list.
func run(t *testing.T, v gift.View, size geom.Size) *render.List {
	t.Helper()
	return frame(t, gift.New(gift.Options{Root: static(v)}), size)
}

func wantBounds(t *testing.T, l *render.List, want ...geom.Rect) {
	t.Helper()
	ops := l.Ops()
	if len(ops) != len(want) {
		t.Fatalf("painted %d ops, want %d: %v", len(ops), len(want), boundsOf(ops))
	}
	for i, w := range want {
		if got := ops[i].Bounds; !approxRect(got, w) {
			t.Errorf("ops[%d].Bounds = %v, want %v", i, got, w)
		}
	}
}

func boundsOf(ops []render.Op) []geom.Rect {
	out := make([]geom.Rect, len(ops))
	for i, op := range ops {
		out[i] = op.Bounds
	}
	return out
}

const eps = 1e-4

func approx(a, b float32) bool {
	d := a - b
	return d < eps && d > -eps
}

func approxRect(a, b geom.Rect) bool {
	return approx(a.Min.X, b.Min.X) && approx(a.Min.Y, b.Min.Y) &&
		approx(a.Max.X, b.Max.X) && approx(a.Max.Y, b.Max.Y)
}

// TestStackOfStacks checks sizes, origins and constraint propagation of a
// nested tree against values computed by hand in the comments, not by a second
// implementation of the algorithm under test.
func TestStackOfStacks(t *testing.T) {
	// Outer VStack: padding 4, gap 8.
	//   inner constraints 192 x 92
	//   child 0, HStack with gap 5: sees main 0..84, cross 0..192.
	//     its own children are 30x10 and 20x10, so it is 55x10.
	//     origins inside it: (0,0) and (35,0).
	//   child 1, a 40x20 box: sees main 0..74, cross 0..192.
	//   content: main 10+8+20 = 38, cross max(55,40) = 55.
	//   outer size: 55+8 x 38+8 = 63 x 46.
	//   origins: (4,4) and (4, 4+10+8) = (4,22).
	v := ui.VStack(
		ui.HStack(probe(30, 10), probe(20, 10)).Gap(5),
		probe(40, 20),
	).Gap(8).Padding(4).Background(fill)

	l := run(t, v, geom.Sz(200, 100))
	wantBounds(t, l,
		geom.RcXYWH(0, 0, 63, 46), // the outer stack paints its background first
		geom.RcXYWH(4, 4, 30, 10),
		geom.RcXYWH(39, 4, 20, 10),
		geom.RcXYWH(4, 22, 40, 20),
	)
}

// TestConstraintPropagation makes the constraints a child actually sees
// observable: a flexible box gets exactly the room the fixed sibling and the
// gaps left over.
func TestConstraintPropagation(t *testing.T) {
	// Frame 100 x 20 on the HStack, gap 6, one fixed 30 wide child and one
	// flexible child: 100 - 30 - 6 = 64 for the flexible one.
	v := ui.HStack(
		probe(30, 20),
		// A Box has no intrinsic size, so the cross axis has to be given
		// to it explicitly; the main axis comes from the flex.
		ui.Box().Background(fill).Flex(1).MinHeight(20),
	).Gap(6).Frame(100, 20)

	l := run(t, v, geom.Sz(200, 200))
	wantBounds(t, l,
		geom.RcXYWH(0, 0, 30, 20),
		geom.RcXYWH(36, 0, 64, 20),
	)
}

func TestSpacer(t *testing.T) {
	tests := []struct {
		name string
		view gift.View
		want []geom.Rect
	}{
		{
			name: "leading spacer pushes to the end",
			view: ui.VStack(ui.Spacer(), probe(10, 20)).Frame(50, 100),
			want: []geom.Rect{geom.RcXYWH(0, 80, 10, 20)},
		},
		{
			name: "trailing spacer keeps the child at the start",
			view: ui.VStack(probe(10, 20), ui.Spacer()).Frame(50, 100),
			want: []geom.Rect{geom.RcXYWH(0, 0, 10, 20)},
		},
		{
			name: "spacers on both sides centre the child",
			view: ui.VStack(ui.Spacer(), probe(10, 20), ui.Spacer()).Frame(50, 100),
			want: []geom.Rect{geom.RcXYWH(0, 40, 10, 20)},
		},
		{
			name: "two spacers split the space by their flex",
			view: ui.VStack(ui.Spacer(), probe(10, 20), ui.Spacer().Flex(3)).Frame(50, 100),
			// 80 free, 20 above and 60 below.
			want: []geom.Rect{geom.RcXYWH(0, 20, 10, 20)},
		},
		{
			name: "horizontal spacer",
			view: ui.HStack(ui.Spacer(), probe(10, 20)).Frame(100, 50),
			want: []geom.Rect{geom.RcXYWH(90, 0, 10, 20)},
		},
		{
			name: "gaps are counted around a spacer",
			view: ui.VStack(probe(10, 20), ui.Spacer(), probe(10, 20)).Gap(10).Frame(50, 100),
			// 100 - 40 - 20 = 40 for the spacer.
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 10, 20),
				geom.RcXYWH(0, 80, 10, 20),
			},
		},
		{
			name: "a nested stack inherits the room its parent has left",
			// The outer stack offers the inner one the main axis room that
			// is left, so the spacer inside it expands to 200-20 = 180 even
			// though no Frame was given anywhere.
			view: ui.VStack(ui.VStack(probe(10, 20), ui.Spacer())),
			want: []geom.Rect{geom.RcXYWH(0, 0, 10, 20)},
		},
		{
			name: "MinLength wins when there is no free space left",
			// The two boxes already fill the frame, so the spacer would get
			// nothing; its minimum length pushes the second box out instead
			// of silently collapsing.
			view: ui.VStack(probe(10, 20), ui.Spacer().MinLength(20), probe(10, 20)).Frame(50, 40),
			want: []geom.Rect{
				geom.RcXYWH(0, 0, 10, 20),
				geom.RcXYWH(0, 40, 10, 20),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantBounds(t, run(t, tc.view, geom.Sz(200, 200)), tc.want...)
		})
	}
}

// TestDrawOrder pins the order the project plan, section 8, prescribes:
// background, content, border. Shadow will be inserted before the background
// in step 2.
func TestDrawOrder(t *testing.T) {
	t.Run("box", func(t *testing.T) {
		v := ui.Box().Frame(20, 10).
			Background(ui.RGB(1, 2, 3)).
			Border(ui.Border{Width: 2, Color: ui.RGB(4, 5, 6)}).
			CornerRadius(4)
		ops := run(t, v, geom.Sz(100, 100)).Ops()
		if len(ops) != 2 {
			t.Fatalf("painted %d ops, want 2", len(ops))
		}
		if ops[0].Kind != render.OpFillRoundRect {
			t.Errorf("ops[0].Kind = %v, want a rounded fill", ops[0].Kind)
		}
		if ops[0].CornerRadius != 4 {
			t.Errorf("background radius = %v, want 4", ops[0].CornerRadius)
		}
		if ops[1].Kind != render.OpStrokeRoundRect {
			t.Errorf("ops[1].Kind = %v, want a stroke", ops[1].Kind)
		}
		if ops[1].StrokeWidth != 2 || ops[1].CornerRadius != 4 {
			t.Errorf("border = %+v", ops[1])
		}
	})

	t.Run("stack with children", func(t *testing.T) {
		v := ui.VStack(probe(10, 10), probe(10, 10)).
			Background(ui.RGB(1, 2, 3)).
			Border(ui.Border{Width: 1, Color: ui.RGB(4, 5, 6)})
		ops := run(t, v, geom.Sz(100, 100)).Ops()
		if len(ops) != 4 {
			t.Fatalf("painted %d ops, want 4", len(ops))
		}
		kinds := []render.OpKind{
			render.OpFillRect,        // the background of the stack
			render.OpFillRect,        // child 0
			render.OpFillRect,        // child 1
			render.OpStrokeRoundRect, // the border of the stack, last
		}
		for i, k := range kinds {
			if ops[i].Kind != k {
				t.Errorf("ops[%d].Kind = %v, want %v", i, ops[i].Kind, k)
			}
		}
		if got, want := ops[0].Bounds, geom.RcXYWH(0, 0, 10, 20); !approxRect(got, want) {
			t.Errorf("background bounds = %v, want %v", got, want)
		}
		if got, want := ops[3].Bounds, geom.RcXYWH(0, 0, 10, 20); !approxRect(got, want) {
			t.Errorf("border bounds = %v, want %v", got, want)
		}
	})

	t.Run("a transparent background draws nothing", func(t *testing.T) {
		v := ui.Box().Frame(10, 10).Background(ui.RGBA(255, 0, 0, 0))
		if ops := run(t, v, geom.Sz(100, 100)).Ops(); len(ops) != 0 {
			t.Fatalf("painted %d ops, want none", len(ops))
		}
	})
}

// TestNilPainterFastPath is the ui side of the core change: a container
// without style must not install a painter at all, so that gift takes its fast
// path and the display list contains nothing but the children.
func TestNilPainterFastPath(t *testing.T) {
	v := ui.VStack(probe(10, 10), ui.HStack(probe(4, 4)))
	l := run(t, v, geom.Sz(100, 100))
	ops := l.Ops()
	if len(ops) != 2 {
		t.Fatalf("painted %d ops, want exactly the 2 of the children", len(ops))
	}
	for i, op := range ops {
		if op.Clip != 0 {
			t.Errorf("ops[%d] is clipped with index %d, want the unclipped sentinel", i, op.Clip)
		}
	}
}

func TestClip(t *testing.T) {
	v := ui.VStack(probe(10, 10)).Padding(4).Clip(true).Background(fill).Frame(40, 40)
	l := run(t, v, geom.Sz(100, 100))
	ops := l.Ops()
	if len(ops) != 2 {
		t.Fatalf("painted %d ops, want 2", len(ops))
	}
	if ops[0].Clip != 0 {
		t.Errorf("the background must be drawn outside the clip, got index %d", ops[0].Clip)
	}
	if ops[1].Clip == 0 {
		t.Fatal("the child must be clipped")
	}
	if got, want := l.Clip(ops[1].Clip), geom.RcXYWH(0, 0, 40, 40); !approxRect(got, want) {
		t.Errorf("clip rect = %v, want the full bounds %v", got, want)
	}
}

// TestClipDoesNotCutThePadding pins the decision that Clip uses the full
// bounds rather than the padded bounds, so that content which deliberately
// overflows into the padding survives.
func TestClipDoesNotCutThePadding(t *testing.T) {
	v := ui.VStack(probe(10, 10)).Padding(4).Clip(true).Frame(40, 40)
	l := run(t, v, geom.Sz(100, 100))
	ops := l.Ops()
	if len(ops) != 1 {
		t.Fatalf("painted %d ops, want 1", len(ops))
	}
	c := l.Clip(ops[0].Clip)
	if c.Min.X != 0 || c.Min.Y != 0 {
		t.Errorf("clip origin = %v, want the unpadded origin", c.Min)
	}
}

// TestModifierReplacement pins the field semantics of the project plan,
// section 8: the second call replaces the first and neither of them creates a
// node.
func TestModifierReplacement(t *testing.T) {
	v := ui.VStack(probe(10, 10)).Padding(8).Padding(16)
	a := gift.New(gift.Options{Root: static(v)})
	l := frame(t, a, geom.Sz(100, 100))

	wantBounds(t, l, geom.RcXYWH(16, 16, 10, 10))

	// The root component node, the VStack node and the Box node. A wrapper
	// based modifier would add one node per call.
	if got := a.Diagnostics().LiveNodes; got != 3 {
		t.Fatalf("LiveNodes = %d, want 3", got)
	}
}
