package gift_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// TestNodeOverflowNamesTheNodeThatTheCountersOnlyCount is the per node half of
// the overflow model: the diagnostics say how many nodes overflow, this says
// which one and by how much.
func TestNodeOverflowNamesTheNodeThatTheCountersOnlyCount(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return overflowView{over: geom.Sz(30, 12), w: 100, h: 100, children: []gift.View{
			overflowView{key: "inner", over: geom.Sz(0, 8), w: 50, h: 50},
			box{w: 10, h: 10},
		}}
	}})
	mustUpdate(t, a)

	root := a.Root()
	// The root component wraps the view, so the overflowing node is below it.
	outer := onlyOverflowing(t, a, root)
	if got, ok := a.NodeOverflow(outer); !ok || got != geom.Sz(30, 12) {
		t.Fatalf("NodeOverflow(outer) = %v, %v, want {30 12}, true", got, ok)
	}

	var kids []gift.NodeRef
	kids = a.NodeChildren(outer, kids)
	if len(kids) != 2 {
		t.Fatalf("outer has %d children, want 2", len(kids))
	}
	if got, ok := a.NodeOverflow(kids[0]); !ok || got != geom.Sz(0, 8) {
		t.Fatalf("NodeOverflow(inner) = %v, %v, want {0 8}, true", got, ok)
	}
	if got, ok := a.NodeOverflow(kids[1]); ok || got != (geom.Size{}) {
		t.Fatalf("NodeOverflow(box) = %v, %v, want the zero size and false", got, ok)
	}
	if _, ok := a.NodeOverflow(gift.NodeRef{}); ok {
		t.Fatal("NodeOverflow of the zero reference reports an overflow")
	}
}

// onlyOverflowing descends from r until it finds the first node that reports
// an overflow, which is how this test avoids depending on how many wrapper
// nodes a component mounts.
func onlyOverflowing(t *testing.T, a *gift.App, r gift.NodeRef) gift.NodeRef {
	t.Helper()
	if _, ok := a.NodeOverflow(r); ok {
		return r
	}
	var kids []gift.NodeRef
	for _, c := range a.NodeChildren(r, kids) {
		if found := onlyOverflowing(t, a, c); !found.IsZero() {
			return found
		}
	}
	return gift.NodeRef{}
}

// TestUpdatesCountEveryUpdateAndFramesOnlyThePaintedOnes is the pair of
// numbers that tells a wedged application apart from one nobody is looking
// at: Ebitengine keeps calling Update for an invisible window and stops
// calling Draw.
func TestUpdatesCountEveryUpdateAndFramesOnlyThePaintedOnes(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return box{w: 10, h: 10}
	}})
	for range 5 {
		mustUpdate(t, a)
	}
	a.Paint()
	if d := a.Diagnostics(); d.Updates != 5 || d.Frames != 1 {
		t.Fatalf("Updates = %d, Frames = %d, want 5 and 1", d.Updates, d.Frames)
	}
	mustUpdate(t, a)
	if d := a.Diagnostics(); d.Updates != 6 || d.Frames != 1 {
		t.Fatalf("after one more update: Updates = %d, Frames = %d, want 6 and 1",
			d.Updates, d.Frames)
	}
}
