package gift_test

import (
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// The recycling tests of [gift.Element.Hidden] and [gift.Element.FocusTrap].
//
// Both fields are written onto the node payload by App.applyElement on every
// build, which makes them look safe. They are not, and the reason is the rule
// of the project plan, section 5, that review gate 12 paid for: a *component*
// node never goes through applyElement — App.mountChild fills a component's
// payload by hand — and the scene store hands a freed slot back with its
// payload untouched. So a component that lands in the recycled slot of an
// inactive tab's layer would be born invisible, and one that lands in the slot
// of a modal layer would be born holding the focus trap of an alert that is
// gone.
//
// These tests live here, in the root package's own external test package,
// rather than next to ui.TabBar, because the proof that the recycled path was
// actually entered is [gift.NodeSlotForTest], which is exported to this
// package and nowhere else. A test that merely swapped a subtree and found the
// new node clean would pass just as happily on a fresh slot, which is exactly
// how the first version of the control gesture recycling test managed to guard
// nothing.

// hiddenLeaf is a leaf view that declares itself hidden.
type hiddenLeaf struct {
	key  string
	trap bool
}

var hiddenLeafType = gift.RegisterType("test.HiddenLeaf")

func (h hiddenLeaf) ViewType() gift.TypeID { return hiddenLeafType }

func (h hiddenLeaf) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:       h.key,
		Layouter:  fixedLayouter{geom.Sz(10, 10)},
		Painter:   markPainter{},
		Hidden:    !h.trap,
		FocusTrap: h.trap,
	}
}

// focusableLeaf is a component whose single child is an ordinary focusable,
// paintable node. It is a *component*, which is the whole point: the component
// node's payload is the one applyElement never touches.
type focusableLeafProps struct{ key string }

func focusableLeafComponent(props focusableLeafProps) gift.View {
	return gift.Component(props.key, func(*gift.Context) gift.View {
		return plainLeaf{key: props.key}
	})
}

type plainLeaf struct{ key string }

var plainLeafType = gift.RegisterType("test.PlainLeaf")

func (l plainLeaf) ViewType() gift.TypeID { return plainLeafType }

func (l plainLeaf) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:        l.key,
		Layouter:   fixedLayouter{geom.Sz(10, 10)},
		Painter:    markPainter{},
		Interactor: noopInteractor{},
		Focusable:  true,
	}
}

type fixedLayouter struct{ s geom.Size }

func (f fixedLayouter) Layout(_ *gift.LayoutContext, c geom.Constraints) geom.Size {
	return c.Constrain(f.s)
}

type markPainter struct{}

func (markPainter) Paint(ctx *gift.PaintContext) { ctx.PaintChildren() }

type noopInteractor struct{}

func (noopInteractor) HandleEvent(*gift.EventContext, gift.Event) bool { return false }

// phases builds an App whose root shows views[phase], and returns a function
// that advances to a given phase.
//
// Three phases and not two, and that is the whole apparatus: a single update
// that replaces one view with another mounts the replacement *before* it
// unmounts the leftover — see App.reconcileChildren — so the newcomer lands in
// a fresh slot and the recycled path is never entered. The middle phase is a
// view of a third type that occupies the tree for one update, which is what
// actually puts the slot on the free list. The same device is used by
// TestAControlThatIsUnmountedWhileGrabbedDoesNotDragOnARecycledSlot, for the
// same reason.
func phases(t *testing.T, views ...gift.View) (*gift.App, func(int)) {
	t.Helper()
	phase := 0
	app := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return views[phase]
	}})
	return app, func(p int) {
		phase = p
		app.Invalidate()
		if err := app.Update(geom.Sz(200, 200)); err != nil {
			t.Fatal(err)
		}
		app.Paint()
	}
}

// occupantView is a leaf of a third type; see [phases].
type occupantView struct{ key string }

var occupantType = gift.RegisterType("test.Occupant")

func (occupantView) ViewType() gift.TypeID { return occupantType }

func (v occupantView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: v.key, Layouter: fixedLayouter{geom.Sz(10, 10)}}
}

// TestAComponentInTheRecycledSlotOfAHiddenLayerIsNotItselfHidden is the
// unmount half of the section 5 rule, and it is written so that the slot reuse
// is proved and not hoped for.
//
// The three updates are not decoration. A single update that swaps the two
// views mounts the replacement *before* it unmounts the leftover — see
// App.reconcileChildren — so the new node lands in a fresh slot and the
// recycled path is never entered. Only after the hidden node has actually been
// freed does the next mount get its slot back.
func TestAComponentInTheRecycledSlotOfAHiddenLayerIsNotItselfHidden(t *testing.T) {
	app, show := phases(t,
		hiddenLeaf{key: "victim"},
		occupantView{key: "filler"},
		focusableLeafComponent(focusableLeafProps{key: "victim"}),
	)
	show(0)
	hiddenSlot, ok := gift.NodeSlotForTest(app, "victim")
	if !ok {
		t.Fatal("the hidden node was not mounted")
	}
	if _, vis := app.NodeVisibleBounds(findRef(app, "victim")); vis {
		t.Fatal("the hidden node reports itself visible; the fixture is not hidden at all")
	}

	// Drop it entirely, so that the slot goes back on the free list, and only
	// then mount the component.
	show(1)
	show(2)

	freshSlot, ok := gift.NodeSlotForTest(app, "victim")
	if !ok {
		t.Fatal("the component was not mounted")
	}
	if freshSlot != hiddenSlot {
		t.Fatalf("the component landed in slot %d and the hidden node had %d, so this test "+
			"did not exercise the recycled path at all and would pass with the clearing in "+
			"nodeData.release deleted. Fix the fixture, do not relax the check",
			freshSlot, hiddenSlot)
	}
	if _, vis := app.NodeVisibleBounds(findRef(app, "victim")); !vis {
		t.Fatal("a component mounted into the recycled slot of a hidden node inherited its " +
			"Hidden flag. It is mounted, it lays out, it hit tests — and it draws nothing, " +
			"with no diagnosis anywhere")
	}
}

// TestAComponentInTheRecycledSlotOfAFocusTrapDoesNotInheritIt is the same
// again for the other new payload field, and its symptom is different and
// worse: the trap has no visible effect at all until somebody presses tab, at
// which point the focus refuses to leave a subtree nobody declared special.
func TestAComponentInTheRecycledSlotOfAFocusTrapDoesNotInheritIt(t *testing.T) {
	// A second, genuine trap stays mounted throughout, and it is what makes
	// this test able to fail at all. The App keeps a *count* of mounted
	// traps and skips the search entirely when it is zero, so a leaked flag
	// on a recycled payload is invisible in a tree with no other trap in it:
	// the count says nothing is trapping and the search never runs. With the
	// keeper present the count is one either way, the search does run, and
	// the rule "the last trap in document order wins" makes the leak decide
	// where the focus goes.
	app, show := phases(t,
		container{first: hiddenLeaf{key: "victim", trap: true}},
		container{first: occupantView{key: "filler"}},
		container{first: focusableLeafComponent(focusableLeafProps{key: "victim"})},
	)
	show(0)
	trapSlot, ok := gift.NodeSlotForTest(app, "victim")
	if !ok {
		t.Fatal("the trap node was not mounted")
	}
	// The trap works while it is there: it is the last one in document order
	// and it contains nothing focusable, so there is nowhere for tab to go.
	app.MoveFocus(true)
	if f, ok := app.Focus(); ok {
		t.Fatalf("the focus trap did not trap anything: tab reached %q; the fixture is wrong",
			app.NodeKey(f))
	}

	show(1)
	show(2)

	freshSlot, ok := gift.NodeSlotForTest(app, "victim")
	if !ok {
		t.Fatal("the component was not mounted")
	}
	if freshSlot != trapSlot {
		t.Fatalf("the component landed in slot %d and the trap had %d, so this test did not "+
			"exercise the recycled path at all. Fix the fixture, do not relax the check",
			freshSlot, trapSlot)
	}
	// The only trap left is the keeper, so that is where the focus has to
	// land. If the recycled payload kept its flag, the component is the last
	// trap in document order and tab lands inside it instead.
	app.MoveFocus(true)
	f, ok := app.Focus()
	if !ok {
		t.Fatal("tab found nothing focusable at all")
	}
	if got := app.NodeKey(f); got != "inside-keeper" {
		t.Fatalf("tab landed on %q, want \"inside-keeper\". A component mounted into the "+
			"recycled slot of a focus trap inherited the trap, so the keyboard focus is "+
			"confined to a subtree nothing declared — and there is no way to see that "+
			"from the screen", got)
	}
}

// container is a node with one variable child and one fixed focusable sibling.
type container struct{ first gift.View }

var containerType = gift.RegisterType("test.Container")

func (container) ViewType() gift.TypeID { return containerType }

func (c container) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Layouter: fixedLayouter{geom.Sz(100, 100)},
		Children: []gift.View{keeperView{}, c.first, plainLeaf{key: "sibling"}},
	}
}

// keeperView is a focus trap that is mounted for the whole of
// TestAComponentInTheRecycledSlotOfAFocusTrapDoesNotInheritIt; see there.
type keeperView struct{}

var keeperType = gift.RegisterType("test.Keeper")

func (keeperView) ViewType() gift.TypeID { return keeperType }

func (keeperView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:       "keeper",
		Layouter:  fixedLayouter{geom.Sz(10, 10)},
		FocusTrap: true,
		Children:  []gift.View{plainLeaf{key: "inside-keeper"}},
	}
}

// findRef locates the node carrying key, by walking the tree the way an
// application would.
func findRef(a *gift.App, key string) gift.NodeRef {
	var walk func(gift.NodeRef) gift.NodeRef
	walk = func(r gift.NodeRef) gift.NodeRef {
		if a.NodeKey(r) == key && a.NodeType(r) == plainLeafType {
			return r
		}
		for _, c := range a.NodeChildren(r, nil) {
			if got := walk(c); !got.IsZero() {
				return got
			}
		}
		return gift.NodeRef{}
	}
	return walk(a.Root())
}
