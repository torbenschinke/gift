package gift_test

import (
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// The two tests in this file are about one user visible failure — "the gallery
// keeps scrolling with the bare cursor after the mouse button was let go" —
// and they pin the two independent guards against it. Each one fails on its
// own if the corresponding guard is removed, which is the property that makes
// them worth two tests instead of one.
//
// Guard one lives in the dispatcher: [gift.App.PointerUp] and
// [gift.App.PointerCancel] end the gesture, so the drag flag does not outlive
// it. TestMouseDoesNotReportADragAfterTheRelease is its test and needs no
// scroll container at all: it asserts on the events an ordinary interactor
// sees.
//
// Guard two lives in gift's scroll handler: a move with no button held does not
// start a drag, however the move describes itself. TestScrollerDeclinesADragWithNoButtonHeld
// is its test, and it has to forge the state through
// [gift.SetMouseDraggedForTest] precisely because guard one makes that state
// unreachable through the public API.
//
// Neither existed before, and the reason is written down in the project plan,
// section 23, step 6: the harness verbs that drag — Swipe and Fling — use the
// touch path, whose slot is zeroed on release and which therefore heals
// itself, and no test moved the mouse *after* a release. Both tests below do
// exactly that.

// --- guard one: the dispatcher ------------------------------------------------

var dragRecorderType = gift.RegisterType("test.DragRecorder")

// dragRecorder remembers [gift.Event.Dragged] of every pointer move it sees,
// in order. It is the smallest observable that distinguishes "the pointer is
// in a drag" from "the pointer has moved at some point in the past".
type dragRecorder struct{ seen *[]gift.Event }

func (dragRecorder) ViewType() gift.TypeID { return dragRecorderType }

func (v dragRecorder) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Layouter:   fixedSize{w: 400, h: 400},
		Interactor: eventLog{seen: v.seen},
	}
}

type eventLog struct{ seen *[]gift.Event }

func (l eventLog) HandleEvent(_ *gift.EventContext, e gift.Event) bool {
	*l.seen = append(*l.seen, e)
	return false
}

// TestMouseDoesNotReportADragAfterTheRelease is guard one.
//
// The mouse keeps its slot for the life of the process — see
// [gift.App.pointerFor] — so anything the dispatcher forgets to clear on a
// release is carried by every event afterwards. The drag flag was such a
// field: it was cleared on the next press and nowhere else.
//
// The release itself must still say Dragged, because that is what a button
// reads to decline an activation the user dragged away from. So the assertion
// is directional: true up to and including the up, false on every move after
// it.
func TestMouseDoesNotReportADragAfterTheRelease(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(a *gift.App, at geom.Point)
	}{
		{"release", func(a *gift.App, at geom.Point) {
			a.PointerUp(gift.MousePointer, gift.PointerMouse, at)
		}},
		{"cancel", func(a *gift.App, _ geom.Point) {
			a.PointerCancel(gift.MousePointer)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen []gift.Event
			a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
				return dragRecorder{seen: &seen}
			}})
			if err := a.Update(geom.Sz(400, 400)); err != nil {
				t.Fatal(err)
			}

			now := time.Duration(0)
			step := func(fn func()) {
				now += 16 * time.Millisecond
				a.BeginInput(now)
				fn()
				if err := a.Update(geom.Sz(400, 400)); err != nil {
					t.Fatal(err)
				}
			}

			step(func() { a.PointerDown(gift.MousePointer, gift.PointerMouse, geom.Pt(100, 100)) })
			// Well past DragSlop, so the pointer really is in a drag.
			step(func() { a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(100, 180)) })

			seen = seen[:0]
			step(func() { tc.end(a, geom.Pt(100, 180)) })
			if len(seen) == 0 {
				t.Fatalf("the terminating event was not delivered at all")
			}
			if !seen[0].Dragged {
				t.Errorf("%v reported Dragged = false; a control that declines a dragged "+
					"release has nothing left to read", seen[0].Kind)
			}

			// The move that used to be the defect: no button held, so this is
			// a hover and nothing else.
			seen = seen[:0]
			step(func() { a.PointerMove(gift.MousePointer, gift.PointerMouse, geom.Pt(100, 40)) })
			if len(seen) == 0 {
				t.Fatalf("the button-less move after the %s was not delivered", tc.name)
			}
			for _, e := range seen {
				if e.Dragged {
					t.Errorf("%v after the %s reported Dragged = true; the gesture ended at "+
						"the %s and the mouse is only hovering", e.Kind, tc.name, tc.name)
				}
			}
		})
	}
}

// --- guard two: the scroll handler --------------------------------------------

// TestScrollerDeclinesADragWithNoButtonHeld is guard two.
//
// gift's scroll handler asks [gift.EventContext.StealPointer] for the gesture
// before it starts dragging, and the answer is false when no button is down.
// That answer used to be discarded, so the handler set its dragging flag on a
// steal that had not happened and then moved the content on every hover event.
//
// The state is forged rather than produced, and that is the point: guard one
// now makes it unreachable, so the only way to keep this half honest is to put
// the dispatcher into the state by hand and check that the handler still
// refuses. See [gift.SetMouseDraggedForTest].
func TestScrollerDeclinesADragWithNoButtonHeld(t *testing.T) {
	said := map[gift.EventKind]bool{}
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return delegatingScroller{h: 2000, said: &said}
	}})
	if err := a.Update(geom.Sz(200, 200)); err != nil {
		t.Fatal(err)
	}
	sc, ok := findScroller(a, a.Root())
	if !ok {
		t.Fatal("no scroll container in the tree")
	}

	now := time.Duration(0)
	move := func(to geom.Point) {
		now += 16 * time.Millisecond
		a.BeginInput(now)
		a.PointerMove(gift.MousePointer, gift.PointerMouse, to)
		if err := a.Update(geom.Sz(200, 200)); err != nil {
			t.Fatal(err)
		}
	}

	// Put the content somewhere it can move in both directions, so that a
	// refusal cannot be mistaken for a container that is simply at a bound.
	a.ScrollTo(sc, 500)
	move(geom.Pt(100, 150))
	gift.SetMouseDraggedForTest(a, true)
	move(geom.Pt(100, 50))

	info, _ := a.ScrollInfo(sc)
	if info.Offset != 500 {
		t.Errorf("offset = %g, want 500: a move with no button held moved the content", info.Offset)
	}
	if info.Dragging {
		t.Error("the container is dragging after a move with no button held")
	}
	if said[gift.EventPointerMove] {
		t.Error("the handler claimed the move; an unclaimed hover has to bubble")
	}
}
