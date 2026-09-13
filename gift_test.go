package gift_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

// The test views below are the smallest possible stand-ins for the real
// widgets, which are part of WU-C. They exist to exercise the runtime, not to
// look like a toolkit.

var (
	boxType   = gift.RegisterType("test.Box")
	stackType = gift.RegisterType("test.Stack")
	otherType = gift.RegisterType("test.Other")
)

// counters records what a layouter did, so that a test can assert that every
// child was measured and placed exactly once.
type counters struct {
	measured map[int]int
	placed   map[int]int
}

func newCounters() *counters {
	return &counters{measured: map[int]int{}, placed: map[int]int{}}
}

// box is a leaf with a fixed size.
type box struct {
	key  string
	w, h float32
}

func (b box) ViewType() gift.TypeID { return boxType }

func (b box) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:      b.key,
		Layouter: fixedSize{w: b.w, h: b.h},
		Painter:  fillPainter{},
	}
}

type fixedSize struct{ w, h float32 }

func (f fixedSize) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	return c.Constrain(geom.Sz(f.w, f.h))
}

type fillPainter struct{}

func (fillPainter) Paint(ctx *gift.PaintContext) {
	ctx.Add(render.Op{
		Kind:   render.OpFillRect,
		Bounds: ctx.Bounds(),
		Color:  render.RGB(10, 20, 30),
	})
	ctx.PaintChildren()
}

// stack is a vertical stack with a gap.
type stack struct {
	key      string
	gap      float32
	children []gift.View
	count    *counters
}

func (s stack) ViewType() gift.TypeID { return stackType }

func (s stack) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Key:      s.key,
		Layouter: stackLayout{gap: s.gap, count: s.count},
		Painter:  fillPainter{},
		Children: s.children,
	}
}

type stackLayout struct {
	gap   float32
	count *counters
}

func (s stackLayout) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	var y, w float32
	child := geom.Constraints{Min: geom.Size{}, Max: geom.Sz(c.Max.W, geom.Unbounded())}
	for i := range ctx.ChildCount() {
		sz := ctx.Measure(i, child)
		ctx.Place(i, geom.Pt(0, y))
		if s.count != nil {
			s.count.measured[i]++
			s.count.placed[i]++
		}
		y += sz.H
		if i < ctx.ChildCount()-1 {
			y += s.gap
		}
		if sz.W > w {
			w = sz.W
		}
	}
	return c.Constrain(geom.Sz(w, y))
}

// other is a second leaf type with the same key space as box, used to test
// that a type change under a stable key forces a remount.
type other struct{ key string }

func (o other) ViewType() gift.TypeID { return otherType }

func (o other) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Key: o.key, Layouter: fixedSize{w: 1, h: 1}, Painter: fillPainter{}}
}

// callback is a leaf that runs fn during Paint. It is used for the contract
// test that painting must not build.
type callback struct{ fn func() }

func (callback) ViewType() gift.TypeID { return boxType }

func (c callback) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: fixedSize{w: 1, h: 1}, Painter: callbackPainter{c.fn}}
}

type callbackPainter struct{ fn func() }

func (p callbackPainter) Paint(ctx *gift.PaintContext) { p.fn() }

func viewport() geom.Size { return geom.Sz(800, 600) }

// --- tests -----------------------------------------------------------------

func TestStateIsIsolatedPerComponentInstance(t *testing.T) {
	type seen struct{ left, right int }
	var got seen

	counter := func(name string) func(*gift.Context) gift.View {
		return func(ctx *gift.Context) gift.View {
			s := ctx.State("count", 0)
			v := ctx.Read(s)
			switch name {
			case "left":
				got.left = v
			case "right":
				got.right = v
			}
			// Grow the value so that the two instances drift apart.
			if v == 0 && name == "left" {
				s.Set(7)
			}
			return box{w: 1, h: 1}
		}
	}

	var leftState, rightState *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Component("left", func(c *gift.Context) gift.View {
				leftState = c.State("count", 0)
				return counter("left")(c)
			}),
			gift.Component("right", func(c *gift.Context) gift.View {
				rightState = c.State("count", 0)
				return counter("right")(c)
			}),
		}}
	}

	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	mustUpdate(t, a)

	if leftState == rightState {
		t.Fatal("the two instances share one state")
	}
	if leftState.Get() != 7 || rightState.Get() != 0 {
		t.Fatalf("left = %d, right = %d, want 7 and 0", leftState.Get(), rightState.Get())
	}
	if got.left != 7 || got.right != 0 {
		t.Fatalf("built values: %+v", got)
	}
}

func TestSetSameValueDoesNotInvalidate(t *testing.T) {
	var st *gift.State[int]
	builds := 0
	root := func(ctx *gift.Context) gift.View {
		builds++
		st = ctx.State("v", 1)
		_ = ctx.Read(st)
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if builds != 1 {
		t.Fatalf("builds = %d, want 1", builds)
	}

	st.Set(1)
	mustUpdate(t, a)
	if builds != 1 {
		t.Fatalf("identical value triggered a rebuild, builds = %d", builds)
	}

	st.Set(2)
	mustUpdate(t, a)
	if builds != 2 {
		t.Fatalf("changed value did not trigger exactly one rebuild, builds = %d", builds)
	}

	// Several writes before one update coalesce into a single rebuild.
	st.Set(3)
	st.Set(4)
	st.Set(5)
	mustUpdate(t, a)
	if builds != 3 {
		t.Fatalf("coalescing failed, builds = %d, want 3", builds)
	}
}

func TestSetInvalidatesOnlyReadingScopes(t *testing.T) {
	var shared *gift.State[int]
	readerBuilds, nonReaderBuilds := 0, 0

	root := func(ctx *gift.Context) gift.View {
		shared = ctx.State("shared", 0)
		return stack{children: []gift.View{
			gift.Component("reader", func(c *gift.Context) gift.View {
				readerBuilds++
				_ = c.Read(shared)
				return box{w: 1, h: 1}
			}),
			gift.Component("nonreader", func(c *gift.Context) gift.View {
				nonReaderBuilds++
				return box{w: 1, h: 1}
			}),
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if readerBuilds != 1 || nonReaderBuilds != 1 {
		t.Fatalf("initial builds = %d/%d", readerBuilds, nonReaderBuilds)
	}

	shared.Set(1)
	mustUpdate(t, a)
	if readerBuilds != 2 {
		t.Fatalf("reader was not rebuilt, builds = %d", readerBuilds)
	}
	if nonReaderBuilds != 1 {
		t.Fatalf("non reader was rebuilt, builds = %d", nonReaderBuilds)
	}
}

func TestDependenciesAreReplacedAfterBuild(t *testing.T) {
	var a1, b1 *gift.State[int]
	readSecond := true
	builds := 0

	root := func(ctx *gift.Context) gift.View {
		builds++
		a1 = ctx.State("a", 0)
		b1 = ctx.State("b", 0)
		_ = ctx.Read(a1)
		if readSecond {
			_ = ctx.Read(b1)
		}
		return box{w: 1, h: 1}
	}
	app := gift.New(gift.Options{Root: root})
	mustUpdate(t, app)

	// Second build stops reading b.
	readSecond = false
	b1.Set(1)
	mustUpdate(t, app)
	if builds != 2 {
		t.Fatalf("builds = %d, want 2", builds)
	}

	// b is no longer a dependency, so it must not invalidate any more.
	b1.Set(2)
	mustUpdate(t, app)
	if builds != 2 {
		t.Fatalf("a dropped dependency still invalidates, builds = %d", builds)
	}

	// a is still a dependency.
	a1.Set(1)
	mustUpdate(t, app)
	if builds != 3 {
		t.Fatalf("builds = %d, want 3", builds)
	}
}

func TestGetDoesNotRegisterReadDoes(t *testing.T) {
	var st *gift.State[int]
	builds := 0
	useRead := false
	root := func(ctx *gift.Context) gift.View {
		builds++
		st = ctx.State("v", 0)
		if useRead {
			_ = ctx.Read(st)
		} else {
			_ = st.Get()
		}
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	st.Set(1)
	mustUpdate(t, a)
	if builds != 1 {
		t.Fatalf("Get registered a dependency, builds = %d", builds)
	}

	useRead = true
	a.Invalidate()
	mustUpdate(t, a)
	if builds != 2 {
		t.Fatalf("builds = %d, want 2", builds)
	}

	st.Set(2)
	mustUpdate(t, a)
	if builds != 3 {
		t.Fatalf("Read did not register a dependency, builds = %d", builds)
	}
}

// TestUnmountReleasesStateAndNodes checks that state is actually released, not
// merely that two counters went down. Counting alone would pass even if the
// unmounted scope stayed subscribed to its state and kept invalidating.
func TestUnmountReleasesStateAndNodes(t *testing.T) {
	show := true
	childBuilds := 0
	var captured *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		kids := []gift.View{box{w: 1, h: 1}}
		if show {
			kids = append(kids, gift.Component("child", func(c *gift.Context) gift.View {
				childBuilds++
				captured = c.State("v", 42)
				_ = c.Read(captured)
				return stack{children: []gift.View{box{w: 1, h: 1}, box{w: 2, h: 2}}}
			}))
		}
		return stack{children: kids}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	before := a.Diagnostics()
	if before.LiveScopes != 2 {
		t.Fatalf("LiveScopes = %d, want 2", before.LiveScopes)
	}
	if childBuilds != 1 {
		t.Fatalf("childBuilds = %d, want 1", childBuilds)
	}

	show = false
	a.Invalidate()
	mustUpdate(t, a)

	after := a.Diagnostics()
	if after.LiveScopes != 1 {
		t.Fatalf("LiveScopes = %d, want 1", after.LiveScopes)
	}
	if after.LiveNodes >= before.LiveNodes {
		t.Fatalf("LiveNodes did not drop: %d -> %d", before.LiveNodes, after.LiveNodes)
	}

	// The real assertion: a state handle captured before the unmount still
	// reads and writes, but no longer invalidates anything. If the dead scope
	// were still subscribed, this write would queue a rebuild of it.
	buildsBefore := a.Diagnostics().Builds
	captured.Set(99)
	mustUpdate(t, a)
	if got := a.Diagnostics().Builds; got != buildsBefore {
		t.Fatalf("a write to the state of an unmounted scope caused %d builds; the dead scope is still subscribed", got-buildsBefore)
	}
	if childBuilds != 1 {
		t.Fatalf("the unmounted component was rebuilt, childBuilds = %d", childBuilds)
	}
	if got := captured.Get(); got != 99 {
		t.Fatalf("the detached state stopped working: %d", got)
	}
}

func TestStateTypeChangePanics(t *testing.T) {
	second := false
	root := func(ctx *gift.Context) gift.View {
		if second {
			_ = ctx.State("value", "text")
		} else {
			_ = ctx.State("value", 0)
		}
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	second = true
	a.Invalidate()

	defer func() {
		r := recover()
		msg, _ := r.(string)
		for _, want := range []string{`state "value"`, `/root`, "int", "string"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic message %q does not mention %q", msg, want)
			}
		}
	}()
	_ = a.Update(viewport())
	t.Fatal("want panic")
}

func TestContextOutsideBuildPanics(t *testing.T) {
	var escaped *gift.Context
	root := func(ctx *gift.Context) gift.View {
		escaped = ctx
		_ = ctx.State("v", 0)
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic")
		}
	}()
	_ = escaped.State("v", 0)
}

// TestKeyedReorderKeepsState covers exactly what its name says. The type
// change half that used to be bolted onto this test never changed a type; it
// lives in TestTypeChangeUnderSameKeyRemounts, which asserts real remount
// evidence.
func TestKeyedReorderKeepsState(t *testing.T) {
	order := []string{"a", "b", "c"}
	states := map[string]*gift.State[int]{}
	mounts := map[string]int{}

	makeChild := func(key string) gift.View {
		return gift.Component(key, func(c *gift.Context) gift.View {
			s := c.State("n", 0)
			if _, ok := states[key]; !ok {
				mounts[key]++
			}
			states[key] = s
			return box{key: key, w: 1, h: 1}
		})
	}

	root := func(ctx *gift.Context) gift.View {
		kids := make([]gift.View, 0, len(order))
		for _, k := range order {
			kids = append(kids, makeChild(k))
		}
		return stack{children: kids}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	for _, k := range order {
		states[k].Set(len(k) + int(k[0]))
	}
	want := map[string]int{}
	for k, s := range states {
		want[k] = s.Get()
	}

	order = []string{"c", "a", "b"}
	a.Invalidate()
	mustUpdate(t, a)

	for k, w := range want {
		if got := states[k].Get(); got != w {
			t.Fatalf("state of %q lost across reorder: %d, want %d", k, got, w)
		}
		if mounts[k] != 1 {
			t.Fatalf("%q was remounted %d times", k, mounts[k])
		}
	}
}

// TestTypeChangeUnderSameKeyRemounts asserts real remount evidence.
//
// Counting live nodes proves nothing here: a remount and an in place update
// produce the same node count, so the old version of this test passed with the
// type check deleted. What actually distinguishes the two is that a remount
// throws the component instance below the key away, taking its state with it,
// and that the replacement paints a different geometry.
func TestTypeChangeUnderSameKeyRemounts(t *testing.T) {
	useOther := false
	mounts := 0
	var st *gift.State[int]

	root := func(ctx *gift.Context) gift.View {
		var child gift.View
		if useOther {
			// A different concrete view type under the same key "x".
			child = other{key: "x"}
		} else {
			child = gift.Component("x", func(c *gift.Context) gift.View {
				s := c.State("n", 0)
				if _ = c.Read(s); s.Get() == 0 {
					mounts++
				}
				st = s
				return box{w: 40, h: 20}
			})
		}
		return stack{children: []gift.View{child}}
	}

	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if mounts != 1 {
		t.Fatalf("mounts = %d, want 1", mounts)
	}
	st.Set(5)
	mustUpdate(t, a)
	beforeScopes := a.Diagnostics().LiveScopes
	if beforeScopes != 2 {
		t.Fatalf("LiveScopes = %d, want 2", beforeScopes)
	}
	beforeBounds := paintedBounds(a)

	// Swap the type under the stable key.
	useOther = true
	a.Invalidate()
	mustUpdate(t, a)

	// Evidence 1: the component instance is gone, so its state went with it.
	if got := a.Diagnostics().LiveScopes; got != 1 {
		t.Fatalf("LiveScopes = %d after the type change, want 1; the old instance was kept alive", got)
	}
	// Evidence 2: the geometry is the one of the replacement, not the one of
	// the node that used to be there.
	afterBounds := paintedBounds(a)
	if len(afterBounds) != 2 {
		t.Fatalf("painted %d ops, want 2", len(afterBounds))
	}
	if afterBounds[1] == beforeBounds[len(beforeBounds)-1] {
		t.Fatalf("the painted child is unchanged at %v; the node was updated in place instead of remounted", afterBounds[1])
	}
	if got := afterBounds[1]; got != geom.RcXYWH(0, 0, 1, 1) {
		t.Fatalf("replacement bounds = %v, want the 1x1 of other{}", got)
	}

	// Evidence 3: swapping back mounts a *fresh* instance with the initial
	// state, not the one that was set to 5.
	useOther = false
	a.Invalidate()
	mustUpdate(t, a)
	if mounts != 2 {
		t.Fatalf("mounts = %d, want a fresh component instance after the round trip", mounts)
	}
	if got := st.Get(); got != 0 {
		t.Fatalf("state survived the remount: %d, want the initial 0", got)
	}
}

// paintedBounds returns the bounds of every operation of one frame.
func paintedBounds(a *gift.App) []geom.Rect {
	var out []geom.Rect
	for _, op := range a.Paint().Ops() {
		out = append(out, op.Bounds)
	}
	return out
}

func TestLayoutMeasuresAndPlacesEachChildOnce(t *testing.T) {
	outer := newCounters()
	inner := newCounters()
	root := func(ctx *gift.Context) gift.View {
		return stack{
			gap:   10,
			count: outer,
			children: []gift.View{
				box{w: 100, h: 20},
				stack{
					gap:      5,
					count:    inner,
					children: []gift.View{box{w: 30, h: 10}, box{w: 40, h: 10}},
				},
			},
		}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	for i := range 2 {
		if outer.measured[i] != 1 || outer.placed[i] != 1 {
			t.Fatalf("outer child %d measured %d, placed %d", i, outer.measured[i], outer.placed[i])
		}
		if inner.measured[i] != 1 || inner.placed[i] != 1 {
			t.Fatalf("inner child %d measured %d, placed %d", i, inner.measured[i], inner.placed[i])
		}
	}

	// The display list mirrors the computed geometry, which is the only way
	// to observe the layout result from outside the package.
	ops := a.Paint().Ops()
	// The root component itself emits nothing: outer stack, box, inner
	// stack and the two inner boxes.
	if len(ops) != 5 {
		t.Fatalf("ops = %d, want 5", len(ops))
	}
	var rects []geom.Rect
	for _, op := range ops {
		rects = append(rects, op.Bounds)
	}
	// rects[1] is the first box, rects[3] and rects[4] the inner boxes.
	if got := rects[1]; got != geom.RcXYWH(0, 0, 100, 20) {
		t.Fatalf("first box = %v", got)
	}
	if got := rects[3]; got != geom.RcXYWH(0, 30, 30, 10) {
		t.Fatalf("inner box 0 = %v", got)
	}
	if got := rects[4]; got != geom.RcXYWH(0, 45, 40, 10) {
		t.Fatalf("inner box 1 = %v", got)
	}
}

func TestConstraintsArePropagated(t *testing.T) {
	var seen geom.Constraints
	root := func(ctx *gift.Context) gift.View {
		return probe{out: &seen}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if seen.Max != viewport() || !seen.Min.IsZero() {
		t.Fatalf("root constraints = %+v, want a loose viewport", seen)
	}
}

type probe struct{ out *geom.Constraints }

func (probe) ViewType() gift.TypeID { return boxType }

func (p probe) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: probeLayout{p.out}, Painter: fillPainter{}}
}

type probeLayout struct{ out *geom.Constraints }

func (p probeLayout) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	*p.out = c
	return geom.Sz(1, 1)
}

func TestBuildDuringPaintPanics(t *testing.T) {
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("v", 0)
		return callback{fn: func() { st.Set(st.Get() + 1) }}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "during Paint") {
			t.Fatalf("panic = %v, want a paint contract diagnosis", r)
		}
	}()
	a.Paint()
	t.Fatal("want panic")
}

func TestIdleUpdateDoesNothing(t *testing.T) {
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{box{w: 1, h: 1}, box{w: 2, h: 2}}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	first := a.Diagnostics()
	if first.Builds == 0 || first.Layouts == 0 {
		t.Fatalf("first update did nothing: %+v", first)
	}

	mustUpdate(t, a)
	second := a.Diagnostics()
	if second.Builds != first.Builds {
		t.Fatalf("idle update rebuilt: %d -> %d", first.Builds, second.Builds)
	}
	if second.Layouts != first.Layouts {
		t.Fatalf("idle update relayouted: %d -> %d", first.Layouts, second.Layouts)
	}

	// A paint does not build or lay out either.
	a.Paint()
	third := a.Diagnostics()
	if third.Builds != first.Builds || third.Layouts != first.Layouts {
		t.Fatalf("paint built or laid out: %+v", third)
	}
	if third.Frames != 1 {
		t.Fatalf("Frames = %d, want 1", third.Frames)
	}
}

func TestResizeRelayoutsWithoutRebuilding(t *testing.T) {
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{box{w: 1, h: 1}}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	before := a.Diagnostics()

	if err := a.Update(geom.Sz(640, 480)); err != nil {
		t.Fatal(err)
	}
	after := a.Diagnostics()
	if after.Builds != before.Builds {
		t.Fatalf("resize rebuilt: %d -> %d", before.Builds, after.Builds)
	}
	if after.Layouts <= before.Layouts {
		t.Fatalf("resize did not lay out again: %d -> %d", before.Layouts, after.Layouts)
	}
}

func TestBindingReadsAndWrites(t *testing.T) {
	var b gift.Binding[int]
	builds := 0
	root := func(ctx *gift.Context) gift.View {
		builds++
		s := ctx.State("v", 0)
		b = s.Binding()
		_ = b.Get() // registers a dependency, because a build is running
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	b.Set(5)
	mustUpdate(t, a)
	if builds != 2 {
		t.Fatalf("binding write did not rebuild, builds = %d", builds)
	}
	if b.Get() != 5 {
		t.Fatalf("binding value = %d", b.Get())
	}
	if (gift.Binding[int]{}).IsZero() != true {
		t.Fatal("zero binding must report IsZero")
	}
}

func TestDuplicateTypeRegistrationPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	gift.RegisterType("test.Box")
}

func TestTypeName(t *testing.T) {
	if got := gift.TypeName(boxType); got != "test.Box" {
		t.Fatalf("TypeName = %q", got)
	}
}

func mustUpdate(t *testing.T, a *gift.App) {
	t.Helper()
	if err := a.Update(viewport()); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

// wideTree builds a tree of roughly 200 nodes.
func wideTree(ctx *gift.Context) gift.View {
	rows := make([]gift.View, 0, 20)
	for r := range 20 {
		cols := make([]gift.View, 0, 9)
		for c := range 9 {
			cols = append(cols, box{key: strconv.Itoa(c), w: float32(10 + c), h: 12})
		}
		rows = append(rows, stack{key: strconv.Itoa(r), gap: 2, children: cols})
	}
	return stack{gap: 4, children: rows}
}
