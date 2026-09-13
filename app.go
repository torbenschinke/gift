package gift

import (
	"fmt"
	"log/slog"
	"runtime"
	"sync/atomic"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
	"github.com/torbenschinke/gift/render"
)

// Options configures a new [App].
type Options struct {
	// Logger is used outside the frame path only: construction, lifecycle
	// and error paths. A nil Logger means logging is off. gift never falls
	// back to slog.Default, because a library that logs into a destination
	// the application did not choose is a nuisance; see the project plan,
	// section 15.
	Logger *slog.Logger

	// Root is the root component function. It is mounted as a component with
	// the key "root" and therefore owns a state scope like any other
	// component.
	Root func(*Context) View
}

// App owns one user interface: the retained tree, the component scopes, the
// state and the display list.
//
// An App belongs to exactly one goroutine, the UI executor. In a windowed
// application that is the goroutine Ebitengine drives; in tests it is the test
// goroutine.
type App struct {
	log   *slog.Logger
	store *scene.Store
	root  *scope
	list  render.List

	bctx       BuildContext
	pctx       PaintContext
	layoutCtxs []*LayoutContext

	// dirty is the queue of scopes that need a rebuild in the next update.
	dirty []*scope
	// building is the scope whose build is currently running, nil outside a
	// build. Dependency registration attaches to it.
	building *scope

	viewport    geom.Size
	needsLayout bool
	needsPaint  bool
	layoutDepth int

	updating bool
	painting bool

	// owner is the goroutine id of the UI executor, captured on the first
	// update. See assertUIGoroutine for what this does and does not catch.
	owner    atomic.Uint64
	ownerSet atomic.Bool

	diag       Diagnostics
	liveScopes uint64
}

// New creates an App and mounts the root component. It does not build
// anything yet; the first [App.Update] does.
func New(opts Options) *App {
	if opts.Root == nil {
		panic("gift: Options.Root must not be nil")
	}
	a := &App{
		log:   opts.Logger,
		store: scene.NewStore(256),
	}
	a.bctx = BuildContext{app: a}
	a.pctx = PaintContext{app: a, list: &a.list}
	a.list.Reset()

	// The root is an ordinary component instance, so that the root has state
	// and a rebuild boundary like every other component.
	h := a.store.Alloc()
	nd := &nodeData{layouter: passthrough{}, painter: passthrough{}}
	n := a.store.Get(h)
	n.Key = "root"
	n.TypeID = uint32(componentTypeID)
	n.Payload = nd

	a.root = &scope{app: a, key: "root", path: "/root", fn: opts.Root, node: h, alive: true}
	a.root.ctx = Context{app: a, scope: a.root}
	nd.scope = a.root
	a.liveScopes++
	a.markNeedsBuild(a.root)

	if a.log != nil {
		a.log.Info("gift: app created")
	}
	return a
}

// Update runs build, reconciliation and layout. The backend calls it once per
// Ebitengine update.
//
// Build and layout happen here and nowhere else. An update that finds no dirty
// scope and no layout invalidation does nothing at all and, in particular,
// allocates nothing.
//
// The error return is part of the Ebitengine update contract and is currently
// always nil. It exists so that a fatal application error can travel out of
// the frame loop later without changing every call site.
func (a *App) Update(viewport geom.Size) error {
	if a.painting {
		panic("gift: App.Update called during App.Paint")
	}
	if !a.ownerSet.Load() {
		a.owner.Store(goid())
		a.ownerSet.Store(true)
	}
	a.updating = true
	defer a.endUpdate()

	if viewport != a.viewport {
		a.viewport = viewport
		a.needsLayout = true
	}

	a.runBuilds()

	if a.needsLayout {
		a.layoutDepth = 0
		a.layoutNode(a.root.node, geom.Loose(a.viewport))
		a.assignBounds(a.root.node, geom.Point{})
		a.needsLayout = false
		a.needsPaint = true
	}
	return nil
}

func (a *App) endUpdate() { a.updating = false }

// runBuilds rebuilds every scope queued for a build.
//
// A build may queue further scopes, for example because an event handler ran
// during it. Those are processed in the next update rather than in an
// unbounded loop here, so that a state write cycle cannot stall a frame
// forever.
func (a *App) runBuilds() {
	n := len(a.dirty)
	if n == 0 {
		return
	}
	for i := range n {
		sc := a.dirty[i]
		sc.queued = false
		if !sc.alive || !sc.needsBuild {
			continue
		}
		a.buildScope(sc)
		a.needsLayout = true
	}
	// Keep whatever was queued while we were building.
	k := copy(a.dirty, a.dirty[n:])
	a.dirty = a.dirty[:k]
}

// markNeedsBuild queues sc for a rebuild in the next update. Writing the same
// state twice before an update therefore costs one rebuild, not two.
func (a *App) markNeedsBuild(sc *scope) {
	if !sc.alive {
		return
	}
	sc.needsBuild = true
	if !sc.queued {
		sc.queued = true
		a.dirty = append(a.dirty, sc)
	}
}

// Paint produces the display list of this frame.
//
// The returned list is borrowed: it is valid until the next call to Paint,
// which resets and refills it. Copy what has to outlive the frame.
//
// Building or laying out during Paint is a contract violation. State writes
// from a painter panic, and so does a nested Update.
func (a *App) Paint() *render.List {
	a.painting = true
	defer a.endPaint()

	a.list.Reset()
	if a.store.Valid(a.root.node) {
		a.paintNode(a.root.node)
	}
	a.needsPaint = false
	a.diag.Frames++
	return &a.list
}

func (a *App) endPaint() { a.painting = false }

// Invalidate forces a rebuild of the root component in the next update. It is
// the blunt instrument for tests, for a resize and for external changes that
// gift cannot observe.
func (a *App) Invalidate() {
	a.markNeedsBuild(a.root)
	a.needsLayout = true
}

// NeedsPaint reports whether the tree changed since the last Paint.
//
// It is informational. gift redraws the whole screen every frame anyway,
// because Ebitengine clears it; see the project plan, section 6. The flag
// exists so that the three invalidation levels stay distinct and observable,
// not so that frames can be skipped.
func (a *App) NeedsPaint() bool { return a.needsPaint }

// Logger returns the logger passed in [Options], or nil. It is never
// slog.Default.
func (a *App) Logger() *slog.Logger { return a.log }

// assertUIGoroutine rejects state access from outside the UI executor.
//
// The check is deliberately cheap and therefore incomplete:
//
//   - While an update or a paint is running, access is accepted without any
//     further test. That is the hot case, and paying for a goroutine identity
//     lookup per state read would show up in the allocation benchmark.
//     Consequence: a background goroutine that writes state exactly while the
//     UI executor is inside Update is not detected here. The race detector
//     is the tool for that case.
//   - Outside an update, the goroutine identity of the caller is compared
//     with the one captured at the first update. This catches the common
//     mistake of writing state from a worker goroutine after a frame has
//     finished.
//   - Goroutine ids are reused by the runtime, so a new goroutine can in
//     principle inherit the id of the UI executor after it has exited. That
//     is a theoretical false negative, never a false positive.
func (a *App) assertUIGoroutine(what string) {
	if a.updating || a.painting {
		return
	}
	if !a.ownerSet.Load() {
		return
	}
	if id := goid(); id != a.owner.Load() {
		panic(fmt.Sprintf(
			"gift: %s called from goroutine %d, but the UI executor is goroutine %d; "+
				"post the result to the UI executor instead of touching state directly",
			what, id, a.owner.Load()))
	}
}

var goroutinePrefix = []byte("goroutine ")

// goid returns the id of the calling goroutine.
//
// There is no supported API for this. Parsing the stack header is the
// portable way and costs roughly a microsecond, which is why it is only ever
// called outside the frame path: once when the UI executor is captured and
// once per rejected state access.
func goid() uint64 {
	var buf [48]byte
	n := runtime.Stack(buf[:], false)
	b := buf[:n]
	if len(b) < len(goroutinePrefix) {
		return 0
	}
	b = b[len(goroutinePrefix):]
	var id uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}
