package gift_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

var (
	plainType = gift.RegisterType("test.Plain")
	flexType  = gift.RegisterType("test.Flexible")
)

// plain is a container without a painter. Its children must still be drawn;
// see [gift.Element.Painter].
type plain struct {
	key      string
	children []gift.View
}

func (plain) ViewType() gift.TypeID { return plainType }

func (p plain) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: p.key, Layouter: stackLayout{}, Children: p.children}
}

// silent is a container with a painter that deliberately does not paint its
// children.
type silent struct{ children []gift.View }

func (silent) ViewType() gift.TypeID { return plainType }

func (s silent) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: stackLayout{}, Painter: silentPainter{}, Children: s.children}
}

type silentPainter struct{}

func (silentPainter) Paint(ctx *gift.PaintContext) {
	ctx.Add(render.Op{Kind: render.OpFillRect, Bounds: ctx.Bounds(), Color: render.RGB(1, 1, 1)})
}

// TestNilPainterPaintsChildren pins the semantics a reviewer asked for: a
// purely structural container has nothing of its own to draw, and making it
// and its whole subtree invisible would be a trap whose only symptom is a
// blank screen.
func TestNilPainterPaintsChildren(t *testing.T) {
	root := func(ctx *gift.Context) gift.View {
		return plain{children: []gift.View{box{w: 3, h: 4}, box{w: 5, h: 6}}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	ops := a.Paint().Ops()
	if len(ops) != 2 {
		t.Fatalf("painted %d ops, want the 2 of the children", len(ops))
	}
	if got, want := ops[0].Bounds, geom.RcXYWH(0, 0, 3, 4); got != want {
		t.Errorf("ops[0].Bounds = %v, want %v", got, want)
	}
	if got, want := ops[1].Bounds, geom.RcXYWH(0, 4, 5, 6); got != want {
		t.Errorf("ops[1].Bounds = %v, want %v", got, want)
	}
}

// TestPainterOwnsItsChildren is the other branch: a non nil painter is fully
// responsible and children appear only where it asks for them.
func TestPainterOwnsItsChildren(t *testing.T) {
	root := func(ctx *gift.Context) gift.View {
		return silent{children: []gift.View{box{w: 3, h: 4}, box{w: 5, h: 6}}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	ops := a.Paint().Ops()
	if len(ops) != 1 {
		t.Fatalf("painted %d ops, want only the one of the painter itself", len(ops))
	}
}

// --- Element.Flex -----------------------------------------------------------

// flexible is a leaf that reports a flex and obeys its constraints.
type flexible struct{ flex float32 }

func (flexible) ViewType() gift.TypeID { return flexType }

func (f flexible) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Flex: f.flex, Layouter: fixedSize{}}
}

// flexReader records the flex of every child it is given.
type flexReader struct {
	children []gift.View
	got      *[]float32
}

func (flexReader) ViewType() gift.TypeID { return flexType }

func (f flexReader) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: flexReaderLayout{got: f.got}, Children: f.children}
}

type flexReaderLayout struct{ got *[]float32 }

func (f flexReaderLayout) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	*f.got = (*f.got)[:0]
	for i := range ctx.ChildCount() {
		*f.got = append(*f.got, ctx.ChildFlex(i))
		ctx.Measure(i, c)
		ctx.Place(i, geom.Point{})
	}
	return c.Constrain(geom.Size{})
}

func TestChildFlexIsVisibleToTheParent(t *testing.T) {
	var got []float32
	root := func(ctx *gift.Context) gift.View {
		return flexReader{got: &got, children: []gift.View{
			box{w: 1, h: 1},
			flexible{flex: 2.5},
			flexible{},
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	want := []float32{0, 2.5, 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ChildFlex = %v, want %v", got, want)
		}
	}
}
