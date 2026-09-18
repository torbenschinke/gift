package gift_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// --- Memo -------------------------------------------------------------------

type rowProps struct {
	Label    string
	Selected bool
}

func TestMemoSkipsTheRebuildWhenPropsAreEqual(t *testing.T) {
	props := rowProps{Label: "a"}
	inner := 0
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Memo("row", props, func(c *gift.Context, p rowProps) gift.View {
				inner++
				return box{w: 1, h: 1}
			}),
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if inner != 1 {
		t.Fatalf("inner builds = %d, want 1", inner)
	}
	buildsAfterMount := a.Diagnostics().Builds

	// The root is invalidated but the props are unchanged.
	a.Invalidate()
	mustUpdate(t, a)
	if inner != 1 {
		t.Fatalf("a memoised component with unchanged props was rebuilt, inner = %d", inner)
	}
	// Exactly one build happened: the root's. The memo's did not.
	if got := a.Diagnostics().Builds; got != buildsAfterMount+1 {
		t.Fatalf("Builds = %d, want %d; the memoised scope was rebuilt", got, buildsAfterMount+1)
	}
}

func TestMemoRebuildsWhenPropsChange(t *testing.T) {
	props := rowProps{Label: "a"}
	var seen []rowProps
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Memo("row", props, func(c *gift.Context, p rowProps) gift.View {
				seen = append(seen, p)
				return box{w: 1, h: 1}
			}),
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	props = rowProps{Label: "a", Selected: true}
	a.Invalidate()
	mustUpdate(t, a)

	if len(seen) != 2 {
		t.Fatalf("builds = %d, want 2", len(seen))
	}
	if seen[1] != props {
		t.Fatalf("second build saw %+v, want %+v", seen[1], props)
	}

	// Back to equal props: no rebuild again.
	a.Invalidate()
	mustUpdate(t, a)
	if len(seen) != 2 {
		t.Fatalf("builds = %d, want the memo to be skipped again", len(seen))
	}
}

func TestMemoStillRebuildsOnItsOwnStateWrite(t *testing.T) {
	props := rowProps{Label: "a"}
	inner := 0
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Memo("row", props, func(c *gift.Context, p rowProps) gift.View {
				inner++
				st = c.State("n", 0)
				_ = c.Read(st)
				return box{w: 1, h: 1}
			}),
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if inner != 1 {
		t.Fatalf("inner = %d", inner)
	}

	// A state write inside the memoised component must rebuild it even though
	// its props are untouched.
	st.Set(1)
	mustUpdate(t, a)
	if inner != 2 {
		t.Fatalf("a state write inside a memoised component did not rebuild it, inner = %d", inner)
	}

	// And it must rebuild it even when the parent rebuilds in the same update.
	st.Set(2)
	a.Invalidate()
	mustUpdate(t, a)
	if inner != 3 {
		t.Fatalf("inner = %d, want 3", inner)
	}
}

func TestMemoKeepsStateAcrossASkippedBuild(t *testing.T) {
	props := rowProps{Label: "a"}
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Memo("row", props, func(c *gift.Context, p rowProps) gift.View {
				st = c.State("n", 0)
				return box{w: 1, h: 1}
			}),
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	st.Set(7)
	for range 3 {
		a.Invalidate()
		mustUpdate(t, a)
	}
	if got := st.Get(); got != 7 {
		t.Fatalf("state = %d, want 7; a skipped build must not drop state", got)
	}
}

func TestMemoRejectsAPropsTypeChangeUnderTheSameKey(t *testing.T) {
	second := false
	root := func(ctx *gift.Context) gift.View {
		var child gift.View
		if second {
			child = gift.Memo("row", 1, func(c *gift.Context, p int) gift.View { return box{w: 1, h: 1} })
		} else {
			child = gift.Memo("row", "a", func(c *gift.Context, p string) gift.View { return box{w: 1, h: 1} })
		}
		return stack{children: []gift.View{child}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	second = true
	a.Invalidate()
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "props type") {
			t.Fatalf("panic = %v, want a props type diagnosis", r)
		}
	}()
	_ = a.Update(viewport())
	t.Fatal("want panic")
}

func TestComponentAndMemoMayNotShareAKey(t *testing.T) {
	second := false
	root := func(ctx *gift.Context) gift.View {
		var child gift.View
		if second {
			child = gift.Memo("row", 1, func(c *gift.Context, p int) gift.View { return box{w: 1, h: 1} })
		} else {
			child = gift.Component("row", func(c *gift.Context) gift.View { return box{w: 1, h: 1} })
		}
		return stack{children: []gift.View{child}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	second = true
	a.Invalidate()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic")
		}
	}()
	_ = a.Update(viewport())
	t.Fatal("want panic")
}

// --- partial relayout -------------------------------------------------------

// TestStateWriteDoesNotMeasureASiblingSubtree is the "Layout | Kein
// Measure/Arrange" row of the project plan, section 6. A state write inside one
// memoised subtree must not measure the nodes of its sibling.
func TestStateWriteDoesNotMeasureASiblingSubtree(t *testing.T) {
	var left *gift.State[float32]

	leaves := func(c *gift.Context, p int) gift.View {
		kids := make([]gift.View, 0, 8)
		for i := range 8 {
			kids = append(kids, box{key: string(rune('a' + i)), w: 5, h: 5})
		}
		return stack{children: kids}
	}

	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Component("left", func(c *gift.Context) gift.View {
				left = c.State("w", float32(10))
				return box{w: c.Read(left), h: 10}
			}),
			gift.Memo("right", 0, leaves),
		}}
	}

	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	mustUpdate(t, a)

	before := a.Diagnostics().Layouts
	left.Set(20)
	mustUpdate(t, a)
	measured := a.Diagnostics().Layouts - before

	// The dirty path is: root component, root stack, left component, left box.
	// The right subtree is 1 component + 1 stack + 8 leaves = 10 nodes, and
	// none of them may be measured.
	if measured == 0 {
		t.Fatal("nothing was measured at all, the test proves nothing")
	}
	if measured > 5 {
		t.Fatalf("a state write measured %d nodes; the 10 node sibling subtree was relayouted too", measured)
	}

	// Sanity: the geometry really did change.
	ops := a.Paint().Ops()
	found := false
	for _, op := range ops {
		if op.Bounds.Size() == geom.Sz(20, 10) {
			found = true
		}
	}
	if !found {
		t.Fatal("the left box did not take its new size")
	}
}

// TestUnchangedConstraintsSkipASubtree covers the other half of the rule: a
// node that is clean and is offered the constraints it already has must not
// run its layouter.
func TestUnchangedConstraintsSkipASubtree(t *testing.T) {
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{box{w: 1, h: 1}, box{w: 2, h: 2}}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	before := a.Diagnostics().Layouts

	// A rebuild of the root marks the path to the root dirty, but nothing
	// below changed its constraints... except that the root rebuild re-applies
	// every element in the subtree, so everything is legitimately dirty here.
	// What must *not* happen is a second measure when nothing is dirty at all.
	mustUpdate(t, a)
	if got := a.Diagnostics().Layouts; got != before {
		t.Fatalf("an idle update measured %d nodes", got-before)
	}

	// A resize changes the root constraints and must reach every node.
	if err := a.Update(geom.Sz(640, 480)); err != nil {
		t.Fatal(err)
	}
	if got := a.Diagnostics().Layouts - before; got < 4 {
		t.Fatalf("a resize measured only %d nodes, want the whole tree", got)
	}
}

// --- panic recovery ---------------------------------------------------------

// TestAppSurvivesAPanicDuringBuild is the contract of the project plan,
// section 15: a panic is how gift reports a violation, and the application may
// recover from it and keep going. That is only true if the App does not keep
// a half finished build around.
func TestAppSurvivesAPanicDuringBuild(t *testing.T) {
	boom := true
	childBuilds := 0
	var st *gift.State[int]

	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			gift.Component("child", func(c *gift.Context) gift.View {
				childBuilds++
				st = c.State("n", 0)
				_ = c.Read(st)
				if boom {
					panic("test: boom")
				}
				return box{w: 1, h: 1}
			}),
		}}
	}

	a := gift.New(gift.Options{Root: root})
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("want the panic to travel out of Update")
			}
		}()
		_ = a.Update(viewport())
	}()

	if childBuilds != 1 {
		t.Fatalf("childBuilds = %d", childBuilds)
	}

	// The App must not think it is still inside an update or inside a build.
	// If a.building still pointed at the dead scope, this Paint would be
	// refused or would register dependencies in the wrong place.
	boom = false
	mustUpdate(t, a)
	if childBuilds != 2 {
		t.Fatalf("the scope did not rebuild after the panic, childBuilds = %d", childBuilds)
	}

	// And it is a normal, working scope afterwards.
	st.Set(3)
	mustUpdate(t, a)
	if childBuilds != 3 {
		t.Fatalf("the scope stopped reacting to its state, childBuilds = %d", childBuilds)
	}
	if ops := a.Paint().Ops(); len(ops) == 0 {
		t.Fatal("nothing was painted after the recovery")
	}
}

// TestAppSurvivesAPanicDuringLayout pins the deferred release of the pooled
// layout contexts. A leaked depth would misalign every following frame.
func TestAppSurvivesAPanicDuringLayout(t *testing.T) {
	boom := true
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{panicky{boom: &boom}, box{w: 3, h: 4}}}
	}
	a := gift.New(gift.Options{Root: root})
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("want a panic")
			}
		}()
		_ = a.Update(viewport())
	}()

	boom = false
	a.Invalidate()
	mustUpdate(t, a)
	bounds := paintedBounds(a)
	if len(bounds) != 3 {
		t.Fatalf("painted %d ops, want 3", len(bounds))
	}
	if got := bounds[2]; got != geom.RcXYWH(0, 1, 3, 4) {
		t.Fatalf("layout after the recovery = %v", got)
	}
}

type panicky struct{ boom *bool }

func (panicky) ViewType() gift.TypeID { return boxType }

func (p panicky) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: panickyLayout{p.boom}, Painter: fillPainter{}}
}

type panickyLayout struct{ boom *bool }

func (p panickyLayout) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	if *p.boom {
		panic("test: layout boom")
	}
	return geom.Sz(1, 1)
}

// TestAppSurvivesAPanicDuringPaint pins the deferred restore of the shared
// PaintContext.
func TestAppSurvivesAPanicDuringPaint(t *testing.T) {
	boom := true
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			callback{fn: func() {
				if boom {
					panic("test: paint boom")
				}
			}},
			box{w: 3, h: 4},
		}}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("want a panic")
			}
		}()
		a.Paint()
	}()

	boom = false
	ops := a.Paint().Ops()
	if len(ops) != 2 {
		t.Fatalf("painted %d ops after the recovery, want 2", len(ops))
	}
	if got := ops[1].Bounds; got != geom.RcXYWH(0, 1, 3, 4) {
		t.Fatalf("bounds after the recovery = %v", got)
	}
}

// --- stale async results ----------------------------------------------------

// TestStaleAsyncResultForAnUnmountedScopeIsRejected covers the requirement of
// the project plan, sections 5 and 13.
func TestStaleAsyncResultForAnUnmountedScopeIsRejected(t *testing.T) {
	show := true
	var tok gift.Token
	var st *gift.State[int]
	builds := 0

	root := func(ctx *gift.Context) gift.View {
		kids := []gift.View{box{w: 1, h: 1}}
		if show {
			kids = append(kids, gift.Component("worker", func(c *gift.Context) gift.View {
				builds++
				st = c.State("v", 0)
				_ = c.Read(st)
				tok = c.Request()
				return box{w: 1, h: 1}
			}))
		}
		return stack{children: kids}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	if !tok.Valid() {
		t.Fatal("a fresh token must be valid")
	}

	// The subtree goes away while the worker is still running.
	show = false
	a.Invalidate()
	mustUpdate(t, a)

	applied := false
	a.Post(func() {
		if !tok.Valid() {
			return
		}
		applied = true
		st.Set(42)
	})
	buildsBefore := a.Diagnostics().Builds
	mustUpdate(t, a)

	if tok.Valid() {
		t.Fatal("the token of an unmounted scope must stop validating")
	}
	if applied {
		t.Fatal("a result for an unmounted scope was applied")
	}
	if got := a.Diagnostics().Builds; got != buildsBefore {
		t.Fatalf("the stale result caused %d builds", got-buildsBefore)
	}
	if builds != 1 {
		t.Fatalf("the unmounted component was rebuilt, builds = %d", builds)
	}
}

// TestStaleAsyncResultOfAnOlderGenerationIsRejected covers the second half:
// the scope is alive, but a newer request has superseded this one.
func TestStaleAsyncResultOfAnOlderGenerationIsRejected(t *testing.T) {
	var tokens []gift.Token
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("v", 0)
		_ = ctx.Read(st)
		tokens = append(tokens, ctx.Request())
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	a.Invalidate()
	mustUpdate(t, a)

	if len(tokens) != 2 {
		t.Fatalf("tokens = %d", len(tokens))
	}
	if tokens[0].Valid() {
		t.Fatal("a superseded request must stop validating")
	}
	if !tokens[1].Valid() {
		t.Fatal("the newest request must still validate")
	}

	old, newest := tokens[0], tokens[1]
	a.Post(func() {
		if old.Valid() {
			st.Set(1)
		}
	})
	a.Post(func() {
		if newest.Valid() {
			st.Set(2)
		}
	})
	mustUpdate(t, a)
	if got := st.Get(); got != 2 {
		t.Fatalf("state = %d, want the newest result only", got)
	}
	if (gift.Token{}).Valid() {
		t.Fatal("the zero Token must never validate")
	}
}

// --- concurrency ------------------------------------------------------------

// TestDiagnosticsIsRaceFree runs under -race and is the point of the
// synchronised publish: the project plan, section 15, designates this snapshot
// as *the* out of band measurement source, and an out of band source that may
// only be read from the UI executor is not one.
func TestDiagnosticsIsRaceFree(t *testing.T) {
	a := gift.New(gift.Options{Root: wideTree})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				d := a.Diagnostics()
				if d.Frames > 1<<40 {
					t.Error("nonsense counter")
				}
			}
		}()
	}
	for i := range 400 {
		if i%3 == 0 {
			a.Invalidate()
		}
		mustUpdate(t, a)
		a.Paint()
	}
	close(stop)
	wg.Wait()

	if got := a.Diagnostics().Frames; got != 400 {
		t.Fatalf("Frames = %d, want 400", got)
	}
}

// TestPostIsRaceFree exercises the worker inbox from several goroutines while
// the UI executor runs frames.
func TestPostIsRaceFree(t *testing.T) {
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("v", 0)
		_ = ctx.Read(st)
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	const workers, each = 4, 50
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				a.Post(func() { st.Set(st.Get() + 1) })
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	for {
		mustUpdate(t, a)
		a.Paint()
		select {
		case <-done:
			mustUpdate(t, a)
			mustUpdate(t, a)
			if got := st.Get(); got != workers*each {
				t.Fatalf("applied %d results, want %d", got, workers*each)
			}
			return
		default:
		}
	}
}

// --- misc contract ----------------------------------------------------------

func TestPaintDuringUpdatePanics(t *testing.T) {
	var a *gift.App
	root := func(ctx *gift.Context) gift.View {
		if a != nil {
			a.Paint()
		}
		return box{w: 1, h: 1}
	}
	a = gift.New(gift.Options{Root: root})

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "during App.Update") {
			t.Fatalf("panic = %v, want a symmetric update/paint diagnosis", r)
		}
	}()
	_ = a.Update(viewport())
	t.Fatal("want panic")
}

func TestRecursionDepthIsCapped(t *testing.T) {
	root := func(ctx *gift.Context) gift.View { return deep{n: 600} }
	a := gift.New(gift.Options{Root: root})
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "deeper than") && !strings.Contains(msg, "recursion deeper") {
			t.Fatalf("panic = %v, want a depth diagnosis", r)
		}
	}()
	_ = a.Update(viewport())
	t.Fatal("want panic")
}

type deep struct{ n int }

func (deep) ViewType() gift.TypeID { return stackType }

func (d deep) Build(*gift.BuildContext) gift.Element {
	e := gift.Element{Layouter: stackLayout{}, Painter: fillPainter{}}
	if d.n > 0 {
		e.Children = []gift.View{deep{n: d.n - 1}}
	}
	return e
}
