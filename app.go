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

	// images is the backend's image resource service, or nil when no backend
	// installed one. See [App.SetImages].
	images render.Images

	viewport geom.Size
	// density is the integer device density; see [App.SetDensity]. It is 1
	// until a backend says otherwise, and 1 is the identity everywhere.
	density     float32
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

	// traps is the number of mounted nodes that declare [Element.FocusTrap].
	// It is a counter and not a flag because the whole point of the "last one
	// in document order wins" rule is that there may be several; see
	// [App.focusRoot], whose tree walk this number skips entirely in the
	// overwhelmingly common case of an application with no modal on screen.
	traps int

	// reconcileHidden is the inherited [Element.Hidden] state of the node
	// currently being applied: true when any ancestor of it, inside the part
	// of the tree this reconciliation has already walked, declared the flag.
	//
	// It exists because [App.hiddenAbove] cannot answer during a mount. A
	// freshly mounted node is linked to its parent *after* its element has
	// been applied — see [App.mountChild] — so a walk up the parent chain
	// from it reaches nothing. The flag is carried down the descent instead,
	// and [App.buildScope] recomputes it from the tree at the top of every
	// build, because a memoised component deep inside an inactive tab is
	// rebuilt on its own with no enclosing descent to inherit from.
	reconcileHidden bool
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
		log:     opts.Logger,
		store:   scene.NewStore[nodeData](256),
		density: 1,
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

// SetImages installs the image resource service a painter reaches through
// [PaintContext.Images].
//
// It is called once by the backend, before the first frame. gift itself never
// uploads anything and never looks at the service; it only carries it from the
// backend, which owns the textures, to the views, which own the keys — because
// the project plan, section 3, has ui depending on neither backend/ebiten nor
// a GPU object, and there is no other route between the two.
//
// A nil service is the headless case and is normal: every view that draws a
// picture falls back to its placeholder, which is what a layout test and a
// gifttest harness without a graphics context want.
func (a *App) SetImages(im render.Images) { a.images = im }

// Images returns the service installed by [App.SetImages], or nil.
func (a *App) Images() render.Images { return a.images }

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
	a.diag.Updates++
	defer a.endUpdate()

	if viewport != a.viewport {
		a.viewport = viewport
		a.needsLayout = true
	}

	a.drainPosts()
	a.runBuilds()
	// The first point in the frame where an application handler may run
	// again. Everything a build owed a node — the focus it took away, the
	// capture and the hover it hid — is delivered here, and whatever that
	// dirtied is rebuilt before the layout sees the tree; see pending.go.
	a.settleNotices()

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
	// The density transform of the project plan, section 18, and the only
	// place it enters the display list. At density 1 nothing is pushed and
	// the root transform stays index 0, the identity, so a 1x frame is the
	// list it always was — not a list with a scale of one in it. See
	// [App.SetDensity].
	a.pctx.xform = 0
	if a.density != 1 {
		a.pctx.xform = a.list.PushXform(a.densityXform())
	}
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
//
// Like every other entry point that touches tree state, it asserts under the
// giftdebug tag that it was called from the UI goroutine; see
// [App.assertUIGoroutine]. It is worth naming why a *core invalidation* entry
// point carries that cost, because the check parses the goroutine header and
// costs roughly a microsecond. Invalidate is not in the frame path: it is
// called once per external change, and the release build has no check at all.
// Against that, it is the natural thing for a goroutine watching the desktop
// appearance or a file to call, it is the second half of [ui.SetTheme], and
// the two flag writes it performs are exactly the racy state the guard exists
// for. A -race run only reports it when another goroutine happens to touch
// the same flags at the same moment; this reports it the first time.
func (a *App) Invalidate() {
	a.assertUIGoroutine("Invalidate")
	a.markNeedsBuild(a.root)
	a.markNeedsLayout(a.root.node)
}

// InvalidateAll forces a rebuild of *every* mounted component instance in the
// next update, not only of the root.
//
// # Why [App.Invalidate] is not enough, and when that matters
//
// Invalidate marks the root scope. A plain [Component] has no rebuild boundary
// — its props live in a closure gift cannot compare — so a root rebuild does
// reach every plain component under it. A [Memo] is exactly the thing that
// stops it: [App.updateChild] returns without calling the component function
// when the props are equal and no state the instance read has fired, which is
// the boundary section 6 of the project plan asks for. A change that is
// neither a prop nor a state, but which nevertheless decides what the subtree
// looks like, therefore does not reach it.
//
// The change that is like that today is the colour theme. Colours are resolved
// during build and stored literally in the retained node — that is what keeps
// the frame path free of theme lookups — so a subtree which is not rebuilt
// keeps the palette it was built with. The result on a screen is not subtle:
// the background turns light while every memoised card stays dark, and the
// text on it becomes unreadable. [ui.SetTheme] calls this method for that
// reason.
//
// # The cost, stated plainly
//
// One walk of the retained tree, which is O(nodes) and allocates nothing, plus
// a rebuild of every component instance in the next update — which is the most
// expensive update the application can have, and is exactly what "every colour
// on the screen is resolved again" means. It is the right price for a theme
// switch, which happens when a person presses a button, and it would be the
// wrong price for anything per frame. Nothing in gift calls it from the frame
// path, and nothing should.
//
// The blunter alternatives were weighed and rejected. Making the theme a
// [State] that every widget reads would put a dependency registration on every
// colour lookup, in ui, which has no [Context] to register against; a
// generation counter compared during reconciliation would have to be consulted
// for every node in the tree, once per update, to save work in the one update
// per hour where a theme changes. This is the cheap version of the honest
// answer: a change nobody can attribute invalidates everything that could have
// consumed it.
//
// Like [App.Invalidate] it asserts under the giftdebug tag that it was called
// from the UI goroutine, and for the same reason.
func (a *App) InvalidateAll() {
	a.assertUIGoroutine("InvalidateAll")
	if a.root == nil || !a.store.Valid(a.root.node) {
		return
	}
	a.invalidateScopesIn(a.root.node, 0)
	a.markNeedsLayout(a.root.node)
}

// invalidateScopesIn marks every scope at or below h as needing a build.
//
// It walks the children rather than the dirty queue, because the queue is the
// set of scopes that *asked* for a rebuild and the point here is the ones that
// did not. The depth guard is the one every recursive walk in this file
// carries; a tree deeper than [scene.MaxDepth] is a component building an
// unbounded tree, and a stack overflow is a worse diagnosis than a panic that
// says so.
func (a *App) invalidateScopesIn(h scene.Handle, depth int) {
	if depth > scene.MaxDepth {
		panic(fmt.Sprintf("gift: tree deeper than %d levels while invalidating every scope", scene.MaxDepth))
	}
	nd := a.data(h)
	if sc := nd.scope; sc != nil {
		a.markNeedsBuild(sc)
	}
	for _, c := range nd.children {
		a.invalidateScopesIn(c, depth+1)
	}
}

// NeedsPaint reports whether the tree changed since the last Paint.
//
// It is informational. gift redraws the whole screen every frame anyway,
// because Ebitengine clears it; see the project plan, section 6. The flag
// exists so that the three invalidation levels stay distinct and observable,
// not so that frames can be skipped.
func (a *App) NeedsPaint() bool { return a.needsPaint }

// Viewport returns the logical size the application was last laid out at,
// that is the size handed to the most recent [App.Update].
//
// It is the denominator of every coordinate this package hands out: node
// bounds, hit test points and pointer positions are all in this space, and a
// caller outside the frame loop — a diagnostic dump or the automation
// interface of the gift/auto package — otherwise has no way to relate them to
// the pixels of a framebuffer, which are this times [App.Density].
//
// It reports the zero size before the first update.
func (a *App) Viewport() geom.Size { return a.viewport }

// Logger returns the logger passed in [Options], or nil. It is never
// slog.Default.
func (a *App) Logger() *slog.Logger { return a.log }
