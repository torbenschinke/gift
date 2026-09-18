package gift_test

import (
	"math"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

var virtualScrollType = gift.RegisterType("test.VirtualScroll")

// virtualScroll is the smallest possible stand-in for the gallery of the
// project plan, section 10: a scroll container whose content extent is a
// number rather than a subtree, and whose one child is placed relative to a
// document origin the layouter picks near the current offset.
//
// It is what WU-N will do at scale, reduced to the part this work unit has to
// get right: the document coordinates.
type virtualScroll struct {
	// extent is the document height, which may be far beyond anything a
	// float32 can count in single pixels.
	extent float64
	// itemDoc is the document position of the single child, and itemH its
	// height.
	itemDoc float64
	itemH   float32
	// seen records what the layouter observed, so a test can assert on the
	// arithmetic rather than only on the pixels.
	seen *virtualSeen
}

type virtualSeen struct {
	offset float64
	origin float64
	local  float32
	passes int
}

func (v virtualScroll) ViewType() gift.TypeID { return virtualScrollType }

func (v virtualScroll) Build(*gift.BuildContext) gift.Element {
	n := &virtualNode{spec: v}
	n.kids[0] = box{w: 200, h: v.itemH}
	return gift.Element{
		Layouter: n,
		Children: n.kids[:],
		Clip:     true,
		Scroll:   &gift.ScrollSpec{Axis: gift.ScrollVertical},
	}
}

type virtualNode struct {
	spec virtualScroll
	kids [1]gift.View
}

func (n *virtualNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	off := ctx.ScrollOffset()

	// This is the discipline the project plan, section 10, asks for. The
	// document origin is picked next to the viewport, the item's *document*
	// position is reduced by it in float64, and only the small difference is
	// turned into the float32 the layout and the GPU work in.
	origin := math.Floor(off)
	local := float32(n.spec.itemDoc - origin)

	ctx.Measure(0, geom.Tight(geom.Sz(200, n.spec.itemH)))
	ctx.Place(0, geom.Pt(0, local))
	ctx.ReportScrollContent(n.spec.extent, origin)

	if s := n.spec.seen; s != nil {
		s.offset, s.origin, s.local = off, origin, local
		s.passes++
	}
	return c.Constrain(geom.Sz(200, 400))
}

// TestVeryLargeDocumentCoordinatesStayExact is the precision requirement of
// the project plan, section 10: "Sehr grosse Dokumentpositionen werden erst
// nach Abzug des Viewport-Ursprungs in float32-GPU-Koordinaten konvertiert."
//
// # What the numbers mean
//
// float32 has a 24 bit mantissa, so above 2^24 = 16 777 216 it cannot
// represent consecutive integers at all: at 30 million the spacing between
// neighbouring float32 values is two, and above 33.5 million it is four. A
// gallery of 100 000 items 300 pixels tall is a 30 million pixel document, so
// this is not a theoretical limit — it is the middle of the first real
// scenario.
//
// The test therefore asserts three separate things:
//
//  1. float32 really does lose a single pixel step at that magnitude, so the
//     rest of the test is measuring something.
//  2. The offset gift keeps is float64 and a one pixel scroll at document
//     position 30 000 000 really moves it by one pixel.
//  3. The value that reaches layout and the display list is the *difference*
//     between the document position and the document origin, computed in
//     float64 and converted once, so it is small and exact.
func TestVeryLargeDocumentCoordinatesStayExact(t *testing.T) {
	const docHeight = 30_000_000.0
	const itemDoc = 29_999_400.0

	// 1. The premise. If this ever stops holding, float32 grew a mantissa and
	// the rest of this test is pointless.
	if float32(docHeight) == float32(docHeight+1) {
		// This is the expected branch: the two document positions collapse
		// onto the same float32.
	} else {
		t.Fatalf("float32 distinguishes %f from %f; the precision premise of this test is gone",
			docHeight, docHeight+1)
	}

	seen := &virtualSeen{}
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return virtualScroll{extent: docHeight, itemDoc: itemDoc, itemH: 300, seen: seen}
	}})
	mustUpdate(t, a)
	a.Paint()

	root := a.Root()
	sc, ok := findScroller(a, root)
	if !ok {
		t.Fatal("the scene has no scroll container")
	}

	info, _ := a.ScrollInfo(sc)
	if info.ContentExtent != docHeight {
		t.Fatalf("content extent = %f, want %f", info.ContentExtent, docHeight)
	}
	if info.MaxOffset != docHeight-400 {
		t.Fatalf("max offset = %f, want %f", info.MaxOffset, docHeight-400)
	}

	// 2. A single pixel really is a single pixel, 30 million pixels in.
	const near = 29_999_000.0
	a.ScrollTo(sc, near)
	if got, _ := a.ScrollInfo(sc); got.Offset != near {
		t.Fatalf("ScrollTo(%f) landed on %f", float64(near), got.Offset)
	}
	a.ScrollBy(sc, 1)
	after, _ := a.ScrollInfo(sc)
	if after.Offset != near+1 {
		t.Fatalf("a one pixel scroll at document position %f moved the offset to %f, want %f;\n"+
			"the offset is not being kept in float64", float64(near), after.Offset, float64(near+1))
	}
	if float32(after.Offset) == float32(near) {
		// Exactly the point: the same two values as float32 are one value.
		t.Logf("as float32 both offsets are %v, which is why the offset is float64", float32(near))
	} else {
		t.Fatalf("float32 still distinguishes %f from %f at this magnitude", float64(near), after.Offset)
	}

	// 3. The value that reaches the layout is the reduced one.
	//
	// A relayout is forced here on purpose. A pure scroll does not relayout —
	// that is the property TestScrollNeitherBuildsNorLayouts in package ui
	// asserts — so the document origin only moves when the virtualising
	// layouter runs again, which is exactly when it also decides which items
	// are visible. WU-N's gallery will invalidate its own layout as it
	// scrolls for that reason; this stands in for it.
	a.Invalidate()
	mustUpdate(t, a)
	if seen.offset != near+1 {
		t.Fatalf("the layouter saw offset %f, want %f", seen.offset, float64(near+1))
	}
	// origin = floor(offset), item at 29 999 400, so the local position is
	// exactly 399 — a number float32 represents to the bit.
	if seen.local != 399 {
		t.Fatalf("the item was placed at local y %v, want exactly 399; the document position was "+
			"converted to float32 before the origin was subtracted", seen.local)
	}
	if float64(seen.local) != itemDoc-seen.origin {
		t.Fatalf("the local position %v is not exactly the float64 difference %f",
			seen.local, itemDoc-seen.origin)
	}

	// And the translation gift pushes is the small difference too, not the
	// document offset. One pixel of scroll must be one pixel of transform.
	l := a.Paint()
	ty, found := scrollTranslation(l)
	if !found {
		t.Fatal("the display list contains no scroll transform")
	}
	if want := float32(seen.origin - seen.offset); ty != want {
		t.Fatalf("the pushed translation is %v, want %v", ty, want)
	}
	if ty < -2 || ty > 2 {
		t.Fatalf("the pushed translation is %v; at a document offset of 30 million it must be the "+
			"reduced difference and therefore small, or float32 cannot express a one pixel step", ty)
	}

	// The item is placed at document 29 999 400 and the viewport starts at
	// 29 999 001, so its top edge is 399 device pixels down — exactly.
	var item geom.Rect
	forEachNode(a, root, func(r gift.NodeRef) {
		if gift.TypeName(a.NodeType(r)) == "test.Box" {
			item = a.NodeDeviceBounds(r)
		}
	})
	if item.Min.Y != 399 {
		t.Errorf("the item is at device y %v, want exactly 399", item.Min.Y)
	}
}

// TestScrollOffsetIsClampedToTheContent covers the documented bounds,
// including the "content smaller than the viewport" case.
func TestScrollOffsetIsClampedToTheContent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		extent    float64
		want      float64
		wantAfter float64
	}{
		{name: "taller than the viewport", extent: 1000, want: 600, wantAfter: 600},
		{name: "exactly the viewport", extent: 400, want: 0, wantAfter: 0},
		{name: "smaller than the viewport", extent: 100, want: 0, wantAfter: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
				return virtualScroll{extent: tc.extent, itemDoc: 0, itemH: 10}
			}})
			mustUpdate(t, a)
			sc, _ := findScroller(a, a.Root())
			a.ScrollTo(sc, 1e9)
			if got, _ := a.ScrollInfo(sc); got.Offset != tc.want {
				t.Errorf("scrolling to the far end landed on %f, want %f", got.Offset, tc.want)
			}
			a.ScrollTo(sc, -1e9)
			if got, _ := a.ScrollInfo(sc); got.Offset != 0 {
				t.Errorf("scrolling before the start landed on %f, want 0", got.Offset)
			}
			a.ScrollTo(sc, math.NaN())
			if got, _ := a.ScrollInfo(sc); got.Offset != 0 {
				t.Errorf("a NaN offset landed on %f, want 0", got.Offset)
			}
		})
	}
}

// TestScrollContentShrinkPullsTheOffsetBack is the resize case: a viewport
// that outgrows its content must not keep looking past the end of it.
func TestScrollContentShrinkPullsTheOffsetBack(t *testing.T) {
	extent := 4000.0
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return virtualScroll{extent: extent, itemDoc: 0, itemH: 10}
	}})
	mustUpdate(t, a)
	sc, _ := findScroller(a, a.Root())
	a.ScrollTo(sc, 3000)

	extent = 500
	a.Invalidate()
	mustUpdate(t, a)
	if got, _ := a.ScrollInfo(sc); got.Offset != 100 {
		t.Errorf("after the content shrank to 500 with a 400 viewport the offset is %f, want 100",
			got.Offset)
	}
}

// TestScrollReportingOnANonScrollNodePanics keeps the contract loud.
func TestScrollReportingOnANonScrollNodePanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(*gift.LayoutContext)
	}{
		{"ReportScrollContent", func(c *gift.LayoutContext) { c.ReportScrollContent(1, 0) }},
		{"ScrollOffset", func(c *gift.LayoutContext) { _ = c.ScrollOffset() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("%s on a plain node did not panic", tc.name)
				}
			}()
			a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
				return callbackLayout{fn: tc.fn}
			}})
			_ = a.Update(viewport())
		})
	}
}

type callbackLayout struct{ fn func(*gift.LayoutContext) }

func (callbackLayout) ViewType() gift.TypeID { return boxType }

func (c callbackLayout) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: c}
}

func (c callbackLayout) Layout(ctx *gift.LayoutContext, cs geom.Constraints) geom.Size {
	c.fn(ctx)
	return geom.Size{}
}

// --- helpers -----------------------------------------------------------------

func findScroller(a *gift.App, r gift.NodeRef) (gift.NodeRef, bool) {
	var out gift.NodeRef
	found := false
	forEachNode(a, r, func(n gift.NodeRef) {
		if !found && a.IsScrollable(n) {
			out, found = n, true
		}
	})
	return out, found
}

func forEachNode(a *gift.App, r gift.NodeRef, fn func(gift.NodeRef)) {
	fn(r)
	for _, c := range a.NodeChildren(r, nil) {
		forEachNode(a, c, fn)
	}
}

// scrollTranslation returns the vertical translation of the first non identity
// transform referenced by the display list.
func scrollTranslation(l *render.List) (float32, bool) {
	for _, op := range l.Ops() {
		if op.Xform == 0 {
			continue
		}
		return l.Xform(op.Xform).TY, true
	}
	return 0, false
}

// --- WU-O: a cancel a scroller had no part in --------------------------------

var delegatingScrollerType = gift.RegisterType("test.DelegatingScroller")

// delegatingScroller is the shape [gift.ScrollInteractor] exists for and the
// one ui.Gallery uses: a container that brings its own Interactor — because it
// also wants the keyboard — and hands the gesture back to gift's handler,
// taking the answer as its own.
//
//	return gift.ScrollInteractor().HandleEvent(ctx, e)
//
// That answer is the observable this test is about.
type delegatingScroller struct {
	h    float32
	said *map[gift.EventKind]bool
}

func (delegatingScroller) ViewType() gift.TypeID { return delegatingScrollerType }

func (v delegatingScroller) Build(*gift.BuildContext) gift.Element {
	n := &delegatingScrollNode{h: v.h, said: v.said}
	return gift.Element{
		Layouter:   n,
		Interactor: n,
		Children:   []gift.View{box{w: 200, h: v.h}},
		Clip:       true,
		Scroll:     &gift.ScrollSpec{Axis: gift.ScrollVertical},
	}
}

type delegatingScrollNode struct {
	h    float32
	said *map[gift.EventKind]bool
}

func (n *delegatingScrollNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	ctx.Measure(0, geom.Tight(geom.Sz(200, n.h)))
	ctx.Place(0, geom.Pt(0, -float32(ctx.ScrollOffset())))
	ctx.ReportScrollContent(float64(n.h), 0)
	return c.Constrain(geom.Sz(200, 200))
}

func (n *delegatingScrollNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	got := gift.ScrollInteractor().HandleEvent(ctx, e)
	(*n.said)[e.Kind] = got
	return got
}

// TestScrollerDeclinesACancelItHadNoPartIn matches [gift.EventPointerCancel] to
// [gift.EventPointerUp], which declines when this container was not the one
// dragging.
//
// # What is actually wrong with consuming it
//
// Returning true from an [gift.Interactor] means "handled, stop here". A
// scroll container that returned it for every cancel said that about a gesture
// it had never taken part in. The consumer of that answer is not gift's
// dispatcher — [App.PointerCancel] delivers a cancel *directly* to the node
// that holds the press and ignores the result, so nothing bubbles and nothing
// is swallowed, and the WU-O review's description of the symptom does not hold
// for this dispatcher. The consumer is the delegating container below: it
// takes gift's answer as its own, and it is entitled to a true one so that it
// can decide what to do with an event nobody handled.
//
// [gift.EventPointerUp] already answered honestly. This is the same answer for
// the event that is its twin.
func TestScrollerDeclinesACancelItHadNoPartIn(t *testing.T) {
	for _, tc := range []struct {
		name string
		drag bool
		want bool
	}{
		{"idle", false, false},
		{"dragging", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			said := map[gift.EventKind]bool{}
			a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
				return delegatingScroller{h: 2000, said: &said}
			}})
			mustUpdate(t, a)
			a.Paint()

			at := geom.Pt(100, 100)
			a.PointerMove(gift.MousePointer, gift.PointerMouse, at)
			a.PointerDown(gift.MousePointer, gift.PointerMouse, at)
			if tc.drag {
				// Past gift.DragSlop, so the container takes the gesture.
				a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(100, 20))
				sc, ok := findScroller(a, a.Root())
				if !ok {
					t.Fatal("no scroll container")
				}
				if info, _ := a.ScrollInfo(sc); !info.Dragging {
					t.Fatalf("the container is not dragging after an 80 pixel move: %+v", info)
				}
			}
			delete(said, gift.EventPointerCancel)
			a.PointerCancel(gift.MousePointer)

			got, saw := said[gift.EventPointerCancel]
			if !saw {
				t.Fatalf("the container never saw the cancel (saw %v)", said)
			}
			if got != tc.want {
				t.Errorf("gift.ScrollInteractor answered %v for a cancel while dragging=%v, want %v; "+
					"a container that delegates takes this answer as its own",
					got, tc.drag, tc.want)
			}
		})
	}
}
