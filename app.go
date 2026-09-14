package gift

import (
	"fmt"
	"log/slog"

	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/scene"
	"github.com/torbenschinke/gift/render"
)

// store is the concrete instantiation of the retained node storage.
//
// The scene package is generic over the payload so that it can store gift's
// per node data inline without importing the module root, which the project
// plan, section 3, forbids. A type parameter satisfies that rule where an
// interface field would have cost a heap object per node.
type store = scene.Store[nodeData]

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
// An App belongs to exactly one goroutine, the UI executor: the goroutine that
// called [New]. In a windowed application that is the goroutine Ebitengine
// drives; in tests it is the test goroutine. The only method that may be
// called from anywhere is [App.Diagnostics].
type App struct {
	log   *slog.Logger
	store *store
	root  *scope
	list  render.List

	bctx       BuildContext
	pctx       PaintContext
	layoutCtxs []*LayoutContext

	// dirty is the queue of scopes that need a rebuild in the next update.
	dirty []*scope
	// pendingLayout are the nodes that asked for another layout pass from
	// inside one; see [LayoutContext.RequestLayout].
	pendingLayout []scene.Handle
	// building is the scope whose build is currently running, nil outside a
	// build. Dependency registration attaches to it.
	building *scope

	viewport    geom.Size
	needsLayout bool
	needsPaint  bool
	layoutDepth int
	paintDepth  int
	// layoutPass is the ordinal of the layout pass; see [LayoutContext.Pass].
	layoutPass uint64

	updating bool
	painting bool

	// ui identifies the goroutine that owns this App. It is an empty struct
	// unless the giftdebug build tag is set; see uiGuard.
	ui uiGuard

	// diag are the counters of the frame path. They belong to the UI
	// executor and are deliberately unsynchronised; they are copied into pub
	// once per Update and once per Paint.
	// box is the inbox for results posted from worker goroutines.
	box postbox

	// in is the input dispatcher: pointers, focus and the reusable event
	// context. It is a plain struct, so nothing about input allocates.
	in inputState

	diag       Diagnostics
	pub        diagPublisher
	liveScopes uint64
}

// New creates an App and mounts the root component. It does not build
// anything yet; the first [App.Update] does.
//
// The calling goroutine becomes the UI executor of this App. Everything except
// [App.Diagnostics] must be called from it, and the state of every component
// belongs to it.
func New(opts Options) *App {
	if opts.Root == nil {
		panic("gift: Options.Root must not be nil")
	}
	a := &App{
		log:   opts.Logger,
		store: scene.NewStore[nodeData](256),
	}
	a.ui.capture()
	a.bctx = BuildContext{app: a}
	a.pctx = PaintContext{app: a, list: &a.list}
	a.in.ectx = EventContext{app: a}
	a.list.Reset()

	// The root is an ordinary component instance, so that the root has state
	// and a rebuild boundary like every other component.
	h := a.store.Alloc()
	n := a.store.Get(h)
	n.Key = "root"
	n.TypeID = uint32(componentTypeID)
	nd := &n.Payload
	nd.layouter = passthrough{}
	nd.painter = passthrough{}

	a.root = &scope{app: a, key: "root", path: "/root", cell: &plainCell{fn: opts.Root}, node: h, alive: true}
	a.root.ctx = Context{app: a, scope: a.root}
	nd.scope = a.root
	a.liveScopes++
	a.markNeedsBuild(a.root)

	if a.log != nil {
		a.log.Info("gift: app created")
	}
	a.publishDiagnostics()
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
	if a.updating {
		panic("gift: App.Update called during App.Update")
	}
	a.updating = true
	defer a.endUpdate()

	if viewport != a.viewport {
		a.viewport = viewport
		a.needsLayout = true
	}

	a.drainPosts()
	a.runBuilds()

	if a.needsLayout {
		a.layoutPass++
		a.layoutDepth = 0
		a.layoutNode(a.root.node, geom.Loose(a.viewport))
		a.assignBounds(a.root.node, geom.Point{}, 0)
		a.needsLayout = false
		a.needsPaint = true
	}
	// After the pass, never during it: a mark set during layout is cleared by
	// the very pass that is running. See [LayoutContext.RequestLayout].
	a.flushPendingLayout()
	return nil
}

// endUpdate runs deferred, so the flag is cleared even when a component
// function panicked. A contract violation is diagnosed with a panic that the
// application may recover from; leaving the App permanently marked "inside an
// update" would turn that diagnosis into a second, unrelated failure.
func (a *App) endUpdate() {
	a.updating = false
	a.publishDiagnostics()
}

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

// markNeedsLayout marks h and every node above it as needing a layout.
//
// This is the propagation half of the three level invalidation the project
// plan, section 6, asks for. The layout pass descends along this path and
// stops at every clean subtree whose incoming constraints did not change, so
// a state write in one subtree does not measure a sibling subtree.
//
// The walk stops as soon as it meets a node that is already marked, so a
// second dirty node under the same ancestors costs only its own depth.
func (a *App) markNeedsLayout(h scene.Handle) {
	a.needsLayout = true
	for depth := 0; !h.IsZero() && a.store.Valid(h); depth++ {
		if depth > scene.MaxDepth {
			panic(fmt.Sprintf("gift: tree deeper than %d levels while marking layout invalidation", scene.MaxDepth))
		}
		n := a.store.Get(h)
		if n.Flags&scene.FlagNeedsLayout != 0 {
			return
		}
		n.Flags |= scene.FlagNeedsLayout | scene.FlagNeedsPaint
		h = n.Parent
	}
}

// Paint produces the display list of this frame.
//
// The returned list is borrowed: it is valid until the next call to Paint,
// which resets and refills it. Copy what has to outlive the frame.
//
// Building or laying out during Paint is a contract violation. State writes
// from a painter panic, and so does a nested Update. Calling Paint from inside
// Update is rejected as well: Ebitengine drives Update and Draw separately and
// gift relies on that separation, so a paint nested in an update means the
// backend is wired up wrongly.
func (a *App) Paint() *render.List {
	if a.updating {
		panic("gift: App.Paint called during App.Update")
	}
	if a.painting {
		panic("gift: App.Paint called during App.Paint")
	}
	a.painting = true
	defer a.endPaint()

	a.list.Reset()
	a.paintDepth = 0
	if a.store.Valid(a.root.node) {
		a.paintNode(a.root.node)
	}
	a.needsPaint = false
	a.diag.Frames++
	return &a.list
}

// endPaint runs deferred for the same reason as [App.endUpdate].
func (a *App) endPaint() {
	a.painting = false
	a.publishDiagnostics()
}

// Invalidate forces a rebuild of the root component in the next update. It is
// the blunt instrument for tests, for a resize and for external changes that
// gift cannot observe.
func (a *App) Invalidate() {
	a.markNeedsBuild(a.root)
	a.markNeedsLayout(a.root.node)
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
