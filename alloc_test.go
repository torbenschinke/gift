package gift_test

import (
	"io"
	"log/slog"
	"strconv"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// TestFramePathIsAllocationFree is go/no-go criterion 3 of the project plan,
// section 12: after warmup, an update that finds nothing dirty plus a paint
// over an unchanged tree of roughly 200 nodes must not allocate at all.
//
// This variant is the idle case, which is the cheap one: the update finds no
// dirty scope and does nothing. TestFramePathIsAllocationFreeUnderRelayout
// below is the one that actually exercises measure, arrange and paint.
func TestFramePathIsAllocationFree(t *testing.T) {
	a := gift.New(gift.Options{Root: wideTree})
	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if n := a.Diagnostics().LiveNodes; n < 180 {
		t.Fatalf("the test tree has only %d nodes, the benchmark would be meaningless", n)
	}

	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("idle frame path allocated %v times per run, want 0", got)
	}
}

// TestFramePathIsAllocationFreeUnderRelayout is the honest version of the
// contract. The viewport changes on every iteration, so the root constraints
// differ, the constraints cache misses everywhere and the whole tree is
// measured, arranged and painted. That is the load the project plan,
// section 11, describes as the frame path without build: no view function
// runs, but layout and display list generation do.
func TestFramePathIsAllocationFreeUnderRelayout(t *testing.T) {
	a := gift.New(gift.Options{Root: wideTree})
	w := float32(800)
	frame := func() {
		// Alternate the viewport so that every frame is a full relayout.
		if w == 800 {
			w = 801
		} else {
			w = 800
		}
		if err := a.Update(geom.Sz(w, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}

	before := a.Diagnostics()
	frame()
	after := a.Diagnostics()
	if got := after.Layouts - before.Layouts; got < 180 {
		t.Fatalf("only %d nodes were measured, the test does not exercise a full relayout", got)
	}
	if after.Builds != before.Builds {
		t.Fatalf("a resize rebuilt %d scopes", after.Builds-before.Builds)
	}

	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("full relayout plus paint allocated %v times per run, want 0", got)
	}
}

// TestFramePathIsAllocationFreeWithDebugLogger repeats the contract with an
// active debug level logger attached, as required by the project plan,
// section 13. Nothing in the frame path may touch that logger.
func TestFramePathIsAllocationFreeWithDebugLogger(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := gift.New(gift.Options{Logger: log, Root: wideTree})
	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("frame path with an active logger allocated %v times per run, want 0", got)
	}
}

// TestEventHandlerStateWriteIsAllocationFree covers the path an input event
// takes. The project plan, section 11, puts "Eingabeverarbeitung" inside the
// zero allocation contract, and a state write is what an event handler does.
//
// This is the regression test for a UI executor check that obtained the
// goroutine id on every accepted access: runtime.Stack needs a buffer that
// escapes, so the check cost 48 B and microseconds per state read or write.
// In a release build the check is not compiled at all; see uiGuard.
func TestEventHandlerStateWriteIsAllocationFree(t *testing.T) {
	if isDebugBuild {
		t.Skip("the UI executor check is only compiled with -tags giftdebug")
	}
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("v", 0)
		_ = ctx.Read(st)
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	n := 0
	handler := func() {
		// Exactly what a button click does: read without subscribing, then
		// write. This runs outside Update and outside Paint, which used to be
		// the expensive branch.
		n++
		st.Set(st.Get() + 1)
	}
	for range 16 {
		handler()
	}
	if got := testing.AllocsPerRun(1000, handler); got != 0 {
		t.Fatalf("an event handler state write allocated %v times per run, want 0", got)
	}

	b := st.Binding()
	bind := func() { b.Set(b.Get() + 1) }
	for range 16 {
		bind()
	}
	if got := testing.AllocsPerRun(1000, bind); got != 0 {
		t.Fatalf("a binding write allocated %v times per run, want 0", got)
	}
}

// TestUnmountRemountCycleIsAllocationFree is the point of storing the node
// payload inline and of not clearing it on free.
//
// A subtree of 37 nodes is unmounted and mounted again on every iteration. The
// freed slots go back on the free list holding their child side tables, so the
// remount finds the buffers it needs already there. If the payload were a heap
// object behind an interface, or if Free zeroed the slices, every cycle would
// feed the collector — which is exactly what the project plan, section 13,
// forbids with "Langes Scrollen, 10 min: RSS-Plateau".
//
// The views below are preconstructed package level values, so that building
// them contributes nothing. Build is excluded from the allocation contract by
// the project plan, section 11, and would otherwise drown the signal this test
// is after.
func TestUnmountRemountCycleIsAllocationFree(t *testing.T) {
	show := true
	root := func(ctx *gift.Context) gift.View {
		if show {
			return recycleWith
		}
		return recycleWithout
	}
	a := gift.New(gift.Options{Root: root})

	cycle := func() {
		show = !show
		a.Invalidate()
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
	}
	// Warm up: the store slots and every reusable buffer have to have reached
	// their steady size before measuring.
	for range 64 {
		cycle()
	}
	if !show {
		cycle()
	}
	with := a.Diagnostics().LiveNodes
	cycle()
	without := a.Diagnostics().LiveNodes
	if with-without < 30 {
		t.Fatalf("the recycled subtree has only %d nodes, the test is too small", with-without)
	}
	cycle() // back to the mounted state, so the run below starts balanced

	if got := testing.AllocsPerRun(200, cycle); got != 0 {
		t.Fatalf("an unmount/remount cycle allocated %v times per run, want 0", got)
	}
}

// leaf and group are allocation free views: everything their Build returns,
// including the boxed Layouter, is preconstructed.
//
// The ordinary test views box a Layouter value on every build, which is
// perfectly normal for a widget but would put one allocation per node into
// these measurements and hide what they are actually about.
type leaf struct {
	key string
	l   gift.Layouter
}

func (leaf) ViewType() gift.TypeID { return boxType }

func (x leaf) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: x.key, Layouter: x.l, Painter: fillPainter{}}
}

func newLeaf(key string, w, h float32) gift.View {
	return leaf{key: key, l: gift.Layouter(fixedSize{w: w, h: h})}
}

type group struct {
	key      string
	l        gift.Layouter
	children []gift.View
}

func (group) ViewType() gift.TypeID { return stackType }

func (g group) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: g.key, Layouter: g.l, Painter: fillPainter{}, Children: g.children}
}

func newGroup(key string, gap float32, children ...gift.View) gift.View {
	return group{key: key, l: gift.Layouter(stackLayout{gap: gap}), children: children}
}

// The recycled fixture is built once, at package initialisation, and the exact
// same View values are handed to gift on every build.
var (
	permanentStack = newGroup("permanent", 0, boxRow("p", 4)...)
	recycledStack  = newGroup("recycled", 1, makeRecycledRows()...)
	recycleWith    = newGroup("top", 0, permanentStack, recycledStack)
	recycleWithout = newGroup("top", 0, permanentStack)
)

func boxRow(prefix string, n int) []gift.View {
	out := make([]gift.View, 0, n)
	for i := range n {
		out = append(out, newLeaf(prefix+strconv.Itoa(i), float32(2+i), 4))
	}
	return out
}

func makeRecycledRows() []gift.View {
	rows := make([]gift.View, 0, 6)
	for r := range 6 {
		rows = append(rows, newGroup(strconv.Itoa(r), 1, boxRow("c", 5)...))
	}
	return rows
}

// TestDiagnosticsPublishIsAllocationFree pins the second half of the
// diagnostics fix: making the snapshot readable from another goroutine must
// not put an allocation into the frame path.
func TestDiagnosticsPublishIsAllocationFree(t *testing.T) {
	a := gift.New(gift.Options{Root: wideTree})
	for range 8 {
		mustUpdate(t, a)
		a.Paint()
	}
	// Update and Paint publish once each, so an idle frame measures both
	// publishes plus the snapshot read.
	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
		_ = a.Diagnostics()
	}
	for range 8 {
		frame()
	}
	if got := testing.AllocsPerRun(500, frame); got != 0 {
		t.Fatalf("publishing and reading the diagnostics allocated %v times per run, want 0", got)
	}
}

// TestCounterScopeBuildAllocations covers the "Build eines Counter-Scopes:
// < 8 Allokationen pro Build" row of the project plan, section 13.
//
// The budget is asserted against what gift itself costs, because that is the
// only part this work unit controls and the only part that can be held to
// zero. The number for the whole stack is measured and logged next to it, and
// it does not meet the plan's threshold. That is reported rather than hidden:
//
// The Counter of the project plan, section 4, has a structural floor of nine
// allocations that no runtime implementation can remove, because they are all
// created by the caller before gift ever sees them:
//
//	2  the two variadic children slices of the VStack and the HStack
//	5  boxing the five child view values into the View interface
//	2  the two button closures, each capturing the state pointer
//
// Boxing and the variadic slice are exactly what the project plan, section 11,
// names as the reason build is excluded from the zero allocation contract.
// The threshold of eight in section 13 is therefore not reachable for that
// shape as long as heterogeneous children go through a View interface, which
// is variant A of section 4. It is a finding about the plan, not a licence to
// relax the number here.
func TestCounterScopeBuildAllocations(t *testing.T) {
	// Whole stack, including the stand-in views.
	_, rebuild := newCounterApp(t)
	for range 32 {
		rebuild()
	}
	full := testing.AllocsPerRun(500, rebuild)

	// The same rebuild driven through preconstructed views, so that only
	// gift's own work is left.
	_, leanRebuild := newLeanScopeApp(t)
	for range 32 {
		leanRebuild()
	}
	lean := testing.AllocsPerRun(500, leanRebuild)

	t.Logf("counter scope build: %.2f allocations in total, %.2f of them inside gift", full, lean)
	if lean >= 8 {
		t.Fatalf("gift itself takes %.2f allocations per scope build, the budget is < 8", lean)
	}
	if lean != 0 {
		t.Logf("note: gift's own per build cost is %.2f, not 0", lean)
	}
}

// newLeanScopeApp rebuilds one component scope whose view tree is a set of
// preconstructed values, so the measurement contains nothing but gift.
func newLeanScopeApp(tb testing.TB) (*gift.App, func()) {
	var st *gift.State[int]
	inner := func(c *gift.Context) gift.View {
		st = c.State("count", 0)
		_ = c.Read(st)
		return leanTree
	}
	a := gift.New(gift.Options{Root: func(c *gift.Context) gift.View {
		return gift.Component("counter", inner)
	}})
	if err := a.Update(viewport()); err != nil {
		tb.Fatal(err)
	}
	n := 0
	return a, func() {
		n++
		st.Set(n)
		if err := a.Update(viewport()); err != nil {
			tb.Fatal(err)
		}
	}
}

var leanTree = newGroup("lean", 16,
	newLeaf("title", 120, 24),
	newLeaf("value", 30, 20),
	newGroup("buttons", 8, newLeaf("minus", 32, 24), newLeaf("plus", 32, 24)),
)

// newCounterApp builds the shape of the Counter example of the project plan,
// section 4: a component with one state, a title, the value rendered from that
// state and a row of two buttons with closures.
func newCounterApp(tb testing.TB) (*gift.App, func()) {
	var st *gift.State[int]
	counter := func(c *gift.Context) gift.View {
		st = c.State("count", 0)
		n := c.Read(st)
		return stack{gap: 16, children: []gift.View{
			box{key: "title", w: 120, h: 24},
			box{key: "value", w: float32(10 + n%7), h: 20},
			stack{key: "buttons", gap: 8, children: []gift.View{
				button{key: "-", onTap: func() { st.Set(st.Get() - 1) }},
				button{key: "+", onTap: func() { st.Set(st.Get() + 1) }},
			}},
		}}
	}
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{gift.Component("counter", counter)}}
	}
	a := gift.New(gift.Options{Root: root})
	if err := a.Update(viewport()); err != nil {
		tb.Fatal(err)
	}
	n := 0
	return a, func() {
		n++
		st.Set(n)
		if err := a.Update(viewport()); err != nil {
			tb.Fatal(err)
		}
	}
}

// button is the smallest stand-in for a control with a callback.
type button struct {
	key   string
	onTap func()
}

func (button) ViewType() gift.TypeID { return otherType }

func (b button) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: b.key, Layouter: fixedSize{w: 32, h: 24}, Painter: fillPainter{}}
}

func BenchmarkFramePath(b *testing.B) {
	a := gift.New(gift.Options{Root: wideTree})
	for range 4 {
		_ = a.Update(viewport())
		a.Paint()
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = a.Update(viewport())
		a.Paint()
	}
}

func BenchmarkFramePathRelayout(b *testing.B) {
	a := gift.New(gift.Options{Root: wideTree})
	w := float32(800)
	for range 4 {
		_ = a.Update(geom.Sz(w, 600))
		a.Paint()
	}
	b.ReportAllocs()
	for b.Loop() {
		if w == 800 {
			w = 801
		} else {
			w = 800
		}
		_ = a.Update(geom.Sz(w, 600))
		a.Paint()
	}
}

func BenchmarkRootBuild(b *testing.B) {
	a := gift.New(gift.Options{Root: wideTree})
	_ = a.Update(viewport())
	b.ReportAllocs()
	for b.Loop() {
		a.Invalidate()
		_ = a.Update(viewport())
	}
}

// BenchmarkCounterScopeBuild reports the number the project plan, section 13,
// puts a budget of 8 allocations on.
func BenchmarkCounterScopeBuild(b *testing.B) {
	_, rebuild := newCounterApp(b)
	for range 8 {
		rebuild()
	}
	b.ReportAllocs()
	for b.Loop() {
		rebuild()
	}
}

// BenchmarkEventHandlerSet reports the cost of the path an input event takes.
func BenchmarkEventHandlerSet(b *testing.B) {
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("v", 0)
		_ = ctx.Read(st)
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	_ = a.Update(viewport())
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		n++
		st.Set(n)
	}
}

// BenchmarkUnmountRemount reports the cost of the tile recycling path.
func BenchmarkUnmountRemount(b *testing.B) {
	show := true
	root := func(ctx *gift.Context) gift.View {
		if show {
			return recycleWith
		}
		return recycleWithout
	}
	a := gift.New(gift.Options{Root: root})
	cycle := func() {
		show = !show
		a.Invalidate()
		_ = a.Update(viewport())
	}
	for range 64 {
		cycle()
	}
	b.ReportAllocs()
	for b.Loop() {
		cycle()
	}
}

// labelled is a leaf that carries a semantic label, and unlabelled is the same
// leaf without one. They exist to measure the price of [gift.Element.Label].
type labelled struct {
	key   string
	label string
	l     gift.Layouter
}

func (labelled) ViewType() gift.TypeID { return boxType }

func (x labelled) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: x.key, Label: x.label, Layouter: x.l, Painter: fillPainter{}}
}

var (
	labelledTree = newGroup("labelled", 4,
		labelled{key: "a", label: "Save", l: fixedSize{w: 40, h: 20}},
		labelled{key: "b", label: "Cancel", l: fixedSize{w: 40, h: 20}},
		labelled{key: "c", label: "Delete every invoice older than a year", l: fixedSize{w: 40, h: 20}},
	)
	unlabelledTree = newGroup("labelled", 4,
		labelled{key: "a", l: fixedSize{w: 40, h: 20}},
		labelled{key: "b", l: fixedSize{w: 40, h: 20}},
		labelled{key: "c", l: fixedSize{w: 40, h: 20}},
	)
)

// TestLabelCostsNothingPerFrame is the price tag of [gift.Element.Label].
//
// The field exists for the test harness in package gifttest and, later, for an
// accessibility bridge. Neither is a reason to make a frame more expensive, so
// this pins two things:
//
//  1. A frame over a tree that carries labels allocates nothing, exactly like
//     the frame path tests above. Nothing in update, layout or paint reads the
//     field at all.
//  2. A *build* of a tree with labels costs exactly as many allocations as the
//     same tree without them. A string header is copied from the element onto
//     the node; the bytes belong to the view, which already owns them.
func TestLabelCostsNothingPerFrame(t *testing.T) {
	build := func(tree gift.View) func() {
		var st *gift.State[int]
		root := func(c *gift.Context) gift.View {
			st = c.State("n", 0)
			_ = c.Read(st)
			return tree
		}
		a := gift.New(gift.Options{Root: root})
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		n := 0
		return func() {
			n++
			st.Set(n)
			if err := a.Update(viewport()); err != nil {
				t.Fatal(err)
			}
			a.Paint()
		}
	}

	withLabels, withoutLabels := build(labelledTree), build(unlabelledTree)
	for range 32 {
		withLabels()
		withoutLabels()
	}
	got := testing.AllocsPerRun(300, withLabels)
	want := testing.AllocsPerRun(300, withoutLabels)
	t.Logf("rebuild plus frame: %.2f allocations with labels, %.2f without", got, want)
	if got != want {
		t.Fatalf("labels cost %.2f allocations per rebuild; a string header is a copy, not an allocation", got-want)
	}

	// And the steady state frame, which is the contract of the project plan,
	// section 11: no build, no label read, nothing allocated.
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View { return labelledTree }})
	frame := func() {
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	for range 16 {
		frame()
	}
	if n := testing.AllocsPerRun(200, frame); n != 0 {
		t.Fatalf("a frame over a labelled tree allocated %v times per run, want 0", n)
	}
}
