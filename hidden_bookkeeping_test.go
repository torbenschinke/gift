package gift_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// The core tests of the two bookkeeping rules [gift.Element.Hidden] carries
// that no widget can be made to show: the focus trap counter, which has no
// observable effect while it is merely too high, and the obstruction rule,
// whose only reader is the scroll reveal.
//
// Both are written against gift's own element API rather than against ui,
// because both are about a node that is hidden while still declaring something
// — and the ui widgets that produce that shape all produce the *same* shape,
// so a test built out of them could not tell "the visible one won" from "they
// are identical".

// settleApp runs updates until nothing is dirty any more. Two are enough for
// every fixture here — one for the state write, one for anything the deferred
// notifications of pending.go caused — and the third is slack.
func settleApp(t *testing.T, a *gift.App) {
	t.Helper()
	for range 3 {
		if err := a.Update(geom.Sz(400, 400)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
}

// --- the focus trap counter --------------------------------------------------

// trapView is a node that declares [gift.Element.FocusTrap] around one child.
type trapView struct {
	trap   bool
	hidden bool
	kids   []gift.View
}

var trapType = gift.RegisterType("test.trap")

func (v trapView) ViewType() gift.TypeID { return trapType }

func (v trapView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Layouter:  fixedLayouter{geom.Sz(100, 100)},
		Painter:   markPainter{},
		FocusTrap: v.trap,
		Hidden:    v.hidden,
		Children:  v.kids,
	}
}

// TestTheFocusTrapCountReturnsToZeroWhenTheTrapIsUnmounted is MAJOR 6 of
// review gate 13.
//
// [gift.App.traps] is a counter maintained in two places — the build that
// starts and stops a declaration, and the unmount of a node that was still
// declaring one — and it had no test at all. Deleting the decrement in
// App.destroyScopes left the whole suite green, because a count stuck above
// zero only makes App.focusRoot walk the tree looking for a trap that is not
// there: slower, finds nothing, changes no behaviour. It is the kind of defect
// that is invisible until the day the imbalance goes the other way, at which
// point an open alert stops confining the keyboard.
func TestTheFocusTrapCountReturnsToZeroWhenTheTrapIsUnmounted(t *testing.T) {
	var open *gift.State[bool]
	a := gift.New(gift.Options{Root: func(ctx *gift.Context) gift.View {
		open = ctx.State("open", false)
		kids := []gift.View{}
		if ctx.Read(open) {
			kids = append(kids, trapView{trap: true})
		}
		return trapView{kids: kids}
	}})
	settleApp(t, a)
	if got := gift.FocusTrapCountForTest(a); got != 0 {
		t.Fatalf("a tree with no trap in it counts %d, want 0", got)
	}
	for i := range 5 {
		open.Set(true)
		settleApp(t, a)
		if got := gift.FocusTrapCountForTest(a); got != 1 {
			t.Fatalf("round %d: one mounted trap counts %d. A count that is too high makes "+
				"App.focusRoot walk the whole tree on every focus change and find nothing",
				i, got)
		}
		open.Set(false)
		settleApp(t, a)
		if got := gift.FocusTrapCountForTest(a); got != 0 {
			t.Fatalf("round %d: the trap was unmounted and the count is still %d. The "+
				"decrement for this case is the one in App.destroyScopes, and nothing "+
				"else in the suite notices when it is gone", i, got)
		}
	}
}

// TestATrapThatStopsDeclaringItselfIsCountedDown is the other decrement: the
// node stays mounted and the flag goes false, which is what a ui.Modal built
// with a nil modal view does *not* do — it unmounts the layer — but which any
// application view may do.
func TestATrapThatStopsDeclaringItselfIsCountedDown(t *testing.T) {
	var on *gift.State[bool]
	a := gift.New(gift.Options{Root: func(ctx *gift.Context) gift.View {
		on = ctx.State("on", true)
		return trapView{trap: ctx.Read(on)}
	}})
	settleApp(t, a)
	if got := gift.FocusTrapCountForTest(a); got != 1 {
		t.Fatalf("the declared trap counts %d, want 1", got)
	}
	on.Set(false)
	settleApp(t, a)
	if got := gift.FocusTrapCountForTest(a); got != 0 {
		t.Fatalf("the node stopped declaring a trap and the count is still %d", got)
	}
}

// --- the obstruction ---------------------------------------------------------

// obstructView is a node of a fixed size that declares
// [gift.Element.Obstructs].
type obstructView struct {
	key    string
	hidden bool
}

var obstructType = gift.RegisterType("test.obstruct")

func (v obstructView) ViewType() gift.TypeID { return obstructType }

func (v obstructView) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:       v.key,
		Layouter:  fixedLayouter{geom.Sz(100, 40)},
		Painter:   markPainter{},
		Obstructs: true,
		Hidden:    v.hidden,
	}
}

// TestAnObstructionInsideAHiddenSubtreeIsNotTheObstruction is MINOR 8.
//
// [gift.Element.Obstructs] says "the last one built wins", and every tab of a
// ui.TabBar is built. A ui.OnScreenKeyboard inside each tab therefore
// registered the keyboard of an *inactive* tab, and App.ScrollIntoView then
// kept a focused field clear of a rectangle nobody could see.
//
// The visible obstruction is built *first* here, deliberately: under the old
// rule the hidden one, being later in document order, won. So a test that had
// them the other way round would pass without the fix.
func TestAnObstructionInsideAHiddenSubtreeIsNotTheObstruction(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return trapView{kids: []gift.View{
			obstructView{key: "on-screen"},
			trapView{hidden: true, kids: []gift.View{obstructView{key: "in-the-hidden-tab"}}},
		}}
	}})
	settleApp(t, a)
	got, ok := gift.ObstructionForTest(a)
	if !ok {
		t.Fatal("nothing obstructs; the fixture declared two obstructions")
	}
	if key := a.NodeKey(got); key != "on-screen" {
		t.Fatalf("the obstruction is %q, want \"on-screen\". Every node is built, so "+
			"'the last one built wins' picks the one inside the hidden subtree and the "+
			"reveal aims at a rectangle that is not there", key)
	}
}

// TestAnObstructionThatBecomesHiddenGivesUpTheRecord is the half the rebuild
// does not cover: the node that holds the record is not re-applied when an
// ancestor is hidden, because a memoised subtree is not rebuilt at all.
func TestAnObstructionThatBecomesHiddenGivesUpTheRecord(t *testing.T) {
	var hide *gift.State[bool]
	a := gift.New(gift.Options{Root: func(ctx *gift.Context) gift.View {
		hide = ctx.State("hide", false)
		return trapView{kids: []gift.View{
			trapView{hidden: ctx.Read(hide), kids: []gift.View{
				gift.Memo("kb", 0, func(*gift.Context, int) gift.View {
					return obstructView{key: "kb"}
				}),
			}},
		}}
	}})
	settleApp(t, a)
	if _, ok := gift.ObstructionForTest(a); !ok {
		t.Fatal("nothing obstructs; the fixture is wrong")
	}
	hide.Set(true)
	settleApp(t, a)
	if _, ok := gift.ObstructionForTest(a); ok {
		t.Fatal("the obstruction is still recorded after the subtree holding it was " +
			"hidden. A reveal now pushes a focused field up around a rectangle nobody " +
			"can see")
	}
}
