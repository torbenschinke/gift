package render_test

import (
	"testing"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

func TestColorPremultiplied(t *testing.T) {
	c := render.RGBA(255, 0, 0, 128)
	if c.A <= 0.5 || c.A >= 0.51 {
		t.Fatalf("alpha = %v", c.A)
	}
	if c.R != 1*c.A {
		t.Fatalf("red is not premultiplied: %v", c)
	}
	if !render.RGB(1, 2, 3).IsOpaque() {
		t.Fatal("RGB must be opaque")
	}
	if !(render.Color{}).IsTransparent() {
		t.Fatal("zero colour must be transparent")
	}
}

func TestClipNesting(t *testing.T) {
	var l render.List
	l.Reset()

	if got := l.CurrentClip(); got != 0 {
		t.Fatalf("initial clip = %d, want 0", got)
	}

	outer := l.PushClip(geom.Rc(0, 0, 100, 100))
	if got := l.Clip(outer); got != geom.Rc(0, 0, 100, 100) {
		t.Fatalf("outer = %v", got)
	}

	inner := l.PushClip(geom.Rc(50, 50, 200, 200))
	if got := l.Clip(inner); got != geom.Rc(50, 50, 100, 100) {
		t.Fatalf("inner clip not intersected: %v", got)
	}
	if l.CurrentClip() != inner {
		t.Fatal("inner must be active")
	}

	l.PopClip()
	if l.CurrentClip() != outer {
		t.Fatal("outer must be active again after pop")
	}

	// A sibling of the inner clip intersects the outer clip again, not the
	// popped one.
	sibling := l.PushClip(geom.Rc(-10, -10, 20, 20))
	if got := l.Clip(sibling); got != geom.Rc(0, 0, 20, 20) {
		t.Fatalf("sibling = %v", got)
	}
	l.PopClip()
	l.PopClip()

	if l.CurrentClip() != 0 {
		t.Fatal("stack must be empty")
	}
}

func TestPopClipUnderflowPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	var l render.List
	l.Reset()
	l.PopClip()
}

func TestDisjointClip(t *testing.T) {
	var l render.List
	l.Reset()
	l.PushClip(geom.Rc(0, 0, 10, 10))
	i := l.PushClip(geom.Rc(20, 20, 30, 30))
	if !l.Clip(i).IsEmpty() {
		t.Fatalf("disjoint clips must be empty, got %v", l.Clip(i))
	}
}

func TestXform(t *testing.T) {
	var l render.List
	l.Reset()
	if !l.Xform(0).IsIdentity() {
		t.Fatal("index 0 must be identity")
	}
	m := geom.Translate(geom.Pt(3, 4))
	i := l.PushXform(m)
	if i == 0 {
		t.Fatal("index 0 is reserved")
	}
	if l.Xform(i) != m {
		t.Fatal("transform not stored")
	}
}

func fill(l *render.List, n int) {
	l.Reset()
	l.PushXform(geom.Translate(geom.Pt(1, 1)))
	for i := range n {
		if i%10 == 0 {
			l.PushClip(geom.RcXYWH(float32(i), 0, 50, 50))
		}
		l.Add(render.Op{
			Kind:   render.OpFillRoundRect,
			Bounds: geom.RcXYWH(float32(i), float32(i), 10, 10),
			Color:  render.RGBA(10, 20, 30, 255),
			Clip:   l.CurrentClip(),
			Xform:  1,
		})
		if i%10 == 9 {
			l.PopClip()
		}
	}
}

func TestResetKeepsCapacity(t *testing.T) {
	var l render.List
	fill(&l, 200)
	if l.Len() != 200 {
		t.Fatalf("len = %d", l.Len())
	}
	capBefore := cap(l.Ops())
	l.Reset()
	if l.Len() != 0 {
		t.Fatal("reset must empty the list")
	}
	if got := cap(l.Ops()); got != capBefore {
		t.Fatalf("capacity dropped from %d to %d", capBefore, got)
	}
}

func TestListBuildIsAllocationFree(t *testing.T) {
	var l render.List
	// Warm up so that all backing arrays have grown to their working size.
	for range 8 {
		fill(&l, 200)
	}
	if got := testing.AllocsPerRun(200, func() { fill(&l, 200) }); got != 0 {
		t.Fatalf("building a 200 op list allocated %v times per run, want 0", got)
	}
}
