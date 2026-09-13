package ui_test

import (
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// idProbe mounts a component whose state carries a serial number. The number
// is handed out once, when the component instance is created, so a row that
// keeps its number kept its node and its state, and a row whose number changed
// was unmounted and mounted again.
type idProbe struct {
	next int
	seen map[string]int
}

func (p *idProbe) view(label string) gift.View {
	return gift.Component("probe", func(ctx *gift.Context) gift.View {
		p.next++
		st := ctx.State("id", p.next)
		p.seen[label] = ctx.Read(st)
		return ui.Box().Frame(1, 1)
	})
}

// TestKeyedReorderPreservesIdentity drives reconciliation through the public
// ui API: reordering keyed children must move the nodes, not recreate them.
func TestKeyedReorderPreservesIdentity(t *testing.T) {
	p := &idProbe{seen: map[string]int{}}
	order := []string{"a", "b", "c"}

	root := func(*gift.Context) gift.View {
		kids := make([]gift.View, 0, len(order))
		for _, k := range order {
			kids = append(kids, ui.VStack(p.view(k)).Key(k))
		}
		return ui.VStack(kids...)
	}
	a := gift.New(gift.Options{Root: root})
	if err := a.Update(geom.Sz(100, 100)); err != nil {
		t.Fatal(err)
	}

	before := map[string]int{}
	for k, v := range p.seen {
		before[k] = v
	}
	if len(before) != 3 {
		t.Fatalf("mounted %d probes, want 3", len(before))
	}

	order = []string{"c", "a", "b"}
	a.Invalidate()
	if err := a.Update(geom.Sz(100, 100)); err != nil {
		t.Fatal(err)
	}
	for k, want := range before {
		if got := p.seen[k]; got != want {
			t.Errorf("row %q has id %d after the reorder, want the preserved %d", k, got, want)
		}
	}
}

// TestTypeChangeUnderTheSameKeyRemounts is the other half: a VStack and an
// HStack are different view types, so the subtree below them is discarded
// rather than silently turned by ninety degrees while keeping its state.
func TestTypeChangeUnderTheSameKeyRemounts(t *testing.T) {
	p := &idProbe{seen: map[string]int{}}
	horizontal := false

	root := func(*gift.Context) gift.View {
		if horizontal {
			return ui.VStack(ui.HStack(p.view("row")).Key("row"))
		}
		return ui.VStack(ui.VStack(p.view("row")).Key("row"))
	}
	a := gift.New(gift.Options{Root: root})
	if err := a.Update(geom.Sz(100, 100)); err != nil {
		t.Fatal(err)
	}
	before := p.seen["row"]

	horizontal = true
	a.Invalidate()
	if err := a.Update(geom.Sz(100, 100)); err != nil {
		t.Fatal(err)
	}
	if got := p.seen["row"]; got == before {
		t.Fatalf("the probe kept its id %d although the container type changed", got)
	}
}

// TestKeyedReorderMovesTheBoxes checks the visible half of the same thing: the
// painted order follows the new child order.
func TestKeyedReorderMovesTheBoxes(t *testing.T) {
	sizes := []float32{10, 20, 30}
	root := func(*gift.Context) gift.View {
		kids := make([]gift.View, 0, len(sizes))
		for _, s := range sizes {
			kids = append(kids, probe(s, s).Key(keyOf(s)))
		}
		return ui.VStack(kids...)
	}
	a := gift.New(gift.Options{Root: root})
	l := frame(t, a, geom.Sz(100, 100))
	wantBounds(t, l,
		geom.RcXYWH(0, 0, 10, 10),
		geom.RcXYWH(0, 10, 20, 20),
		geom.RcXYWH(0, 30, 30, 30),
	)
	nodes := a.Diagnostics().LiveNodes

	sizes = []float32{30, 10, 20}
	a.Invalidate()
	l = frame(t, a, geom.Sz(100, 100))
	wantBounds(t, l,
		geom.RcXYWH(0, 0, 30, 30),
		geom.RcXYWH(0, 30, 10, 10),
		geom.RcXYWH(0, 40, 20, 20),
	)
	if got := a.Diagnostics().LiveNodes; got != nodes {
		t.Fatalf("LiveNodes = %d after the reorder, want the unchanged %d", got, nodes)
	}
}

func keyOf(v float32) string {
	switch v {
	case 10:
		return "a"
	case 20:
		return "b"
	default:
		return "c"
	}
}
