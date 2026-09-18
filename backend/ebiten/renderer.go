package ebiten

import (
	_ "embed"
	"fmt"
	"math"
	"time"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

//go:embed shape.kage
var shapeShaderSrc []byte

//go:embed glass.kage
var glassShaderSrc []byte

//go:embed kawase_down.kage
var kawaseDownSrc []byte

//go:embed kawase_up.kage
var kawaseUpSrc []byte

// ShapeShaderSource returns the Kage source of the shared shape shader.
//
// It is exported so that a test can compile it without a window;
// [eb.NewShader] needs no graphics context.
func ShapeShaderSource() []byte { return shapeShaderSrc }

// GlassShaderSource returns the Kage source of the glass composite shader, and
// KawaseShaderSources the two blur passes, under the same rule as
// [ShapeShaderSource]: compiling them is a CPU operation and a test may do it
// headless.
func GlassShaderSource() []byte { return glassShaderSrc }

// KawaseShaderSources returns the downsample and upsample shaders of the
// dual-Kawase chain.
func KawaseShaderSources() (down, up []byte) { return kawaseDownSrc, kawaseUpSrc }

// aaPad is how far the geometry of an antialiased shape is grown beyond its
// bounds, in device pixels. It is converted to local units per axis by
// dividing by the scale factors of the transform.
//
// Without it the outer half of the coverage ramp would fall outside the quad
// and be lost, which makes every rounded edge render about half a pixel thin.
// It is not applied to a plain fill rectangle, whose quad is the shape.
const aaPad = 1

// maxBatchVertices is the soft cap after which the renderer flushes.
//
// Ebitengine's 32 bit index path has no practical vertex limit on a 64 bit
// platform, so this is a memory bound and not a correctness bound. Flushing
// early is always safe because the operations are emitted in order and a
// flush preserves that order, which is what alpha blending depends on.
const maxBatchVertices = 1 << 16

// Renderer is the [render.Backend] implementation on top of Ebitengine.
//
// It translates a display list into one vertex stream and draws it with a
// single shared shader, so a frame normally costs exactly one draw call
// regardless of how many rectangles it contains. No image, texture or render
// target is allocated per widget, per operation or per frame; the project
// plan, sections 8 and 11, forbid all three.
//
// A Renderer belongs to the goroutine that drives the window, like the [gift.App]
// it renders. It is not safe for concurrent use.
type Renderer struct {
	shader *eb.Shader
	opts   eb.DrawTrianglesShaderOptions

	// The glass material of the project plan, section 8. glassShader is the
	// composite pass of both quality levels; downShader and upShader are the
	// dual-Kawase chain of the Full level. They are separate shaders and
	// therefore separate materials, which is why a material region is a
	// batching barrier; see [Renderer.appendMaterial].
	glassShader, downShader, upShader *eb.Shader
	glassOpts                         eb.DrawTrianglesShaderOptions
	blurOpts                          eb.DrawTrianglesShaderOptions
	// copyOpts replace the destination instead of blending into it. Used for
	// the region copy, which genuinely wants replace: the backdrop target is
	// scratch memory and whatever a previous tenant left in it must not
	// survive.
	copyOpts eb.DrawTrianglesOptions
	// blitOpts composite the scene onto the real target with source over.
	// See [Renderer.SetTarget] for why that is not interchangeable with
	// copyOpts.
	blitOpts eb.DrawTrianglesOptions

	// targets owns the intermediate render targets. See [TargetPool].
	targets *TargetPool
	// policy chooses between Reduced and Full. See [GlassPolicy].
	policy *GlassPolicy
	// frameQuality is the level [GlassPolicy.BeginFrame] chose for the frame
	// in progress, so that two panels of one frame cannot differ.
	frameQuality render.GlassQuality

	// scene is the offscreen the frame is drawn into when it contains a
	// material, and nil otherwise. See [Renderer.Submit] for why it has to
	// exist at all.
	scene          *eb.Image
	sceneW, sceneH int
	// screen is what [Renderer.SetTarget] was given: the real destination,
	// which scene is blitted to in EndFrame.
	screen *eb.Image
	// lastDraw is the timestamp of the previous BeginFrame, which is what
	// feeds the interval window of the policy.
	lastDraw time.Time

	// passVerts and passIdx are the four vertex scratch of the material
	// passes. They are separate from verts and idx because a pass is issued
	// immediately rather than batched, and reusing the batch buffers would
	// mean a pass could not happen while one is being built — which is true
	// today, because a material flushes first, and would be a trap the moment
	// it stopped being true.
	passVerts []eb.Vertex
	passIdx   []uint32

	// passFn, if non nil, receives every material pass in order. It is what
	// lets a headless test assert the shape of the chain — copy, down, down,
	// up, up, composite — without a graphics context.
	passFn func(glassPass)

	// textures is the image resource cache: residency, the per drawn frame
	// upload budget and eviction with an explicit Deallocate. It is the
	// [render.Images] the application reaches through gift.App.SetImages.
	textures *TextureCache
	// imageOpts are the draw options of the image material. Premultiplied,
	// like the glyph options, because [render.Color] is and because
	// asset.Thumbnail produces premultiplied RGBA — so a tint multiplies
	// correctly and nothing in the frame path converts a colour. The filter
	// is linear and not nearest: a thumbnail is scaled to whatever the tile
	// rectangle happens to be, and the ladder of asset.Config.Sizes only
	// promises to be *near* it.
	imageOpts eb.DrawTrianglesOptions

	// atlas is the glyph atlas. It is created by NewRenderer and is the only
	// thing in this package that knows what a glyph looks like.
	atlas *GlyphAtlas
	// glyphOpts are the draw options of the glyph material. The colour scale
	// is premultiplied because render.Color is, and the atlas holds
	// premultiplied white coverage, so the multiply is exact; see
	// [GlyphAtlas]. The filter is set to nearest in [NewRenderer], explicitly
	// rather than by relying on it being the zero value of eb.Filter, and it
	// is not a quality compromise but the correct choice: glyph positions are
	// whole pixels and the atlas rectangle maps one to one onto the
	// destination, so any interpolation would only blur a mapping that is
	// already exact. See [Renderer.appendTexturedQuad] for the assumption that
	// rests on it.
	glyphOpts eb.DrawTrianglesOptions

	// dst is the image of the frame in progress. It is set by [Renderer.SetTarget].
	dst *eb.Image

	// verts and idx are the reused vertex and index buffers. They are the
	// reason Submit does not allocate in the steady state.
	verts []eb.Vertex
	idx   []uint32

	// drawFn overrides the actual draw call. It is nil in normal operation
	// and set by tests, which is what makes the whole translation path — op
	// decoding, clipping, transform lookup and colour conversion — testable
	// without a graphics context, as the project plan, section 12, criterion
	// 4 demands.
	//
	// It receives the material of the batch, which is what lets a test assert
	// the interleaving of shapes and text without a window.
	drawFn func(m Material, verts []eb.Vertex, idx []uint32)

	// curMat and curPage are the material of the batch under construction.
	// See [Renderer.material].
	curMat  Material
	curPage *eb.Image

	// scratch polygons of the general clipping path. Two buffers of eight
	// vertices are enough: clipping a convex quad against four half planes
	// adds at most one vertex per plane.
	poly [2][8]clipVertex

	inFrame bool
	drawn   uint64
	batches uint64
	emitted uint64
	// frameSize is the drawable size of the frame in progress, for the
	// material area budget of the glass policy.
	frameSize geom.Size

	// The skip counters, one per reason. They used to be a single number,
	// which conflated "the application asked for something invisible" with
	// "a container collapsed to nothing". The second is a layout defect and
	// the first is not, and merging them is why a stack that starved thirty
	// of forty rows produced nothing but a slightly larger skip count that
	// nobody could interpret. See [RendererStats].
	skipNone        uint64
	skipTransparent uint64
	skipEmptyBounds uint64
	skipEmptyClip   uint64
	skipOutsideClip uint64
	skipZeroStroke  uint64
	skipEmptyText   uint64
	skipNoImage     uint64
	unknowns        uint64

	shapeBatches uint64
	glyphBatches uint64
	imageBatches uint64
	glyphQuads   uint64
	imageOps     uint64

	// shadowOps counts shadow operations that produced geometry and
	// shadowSharpOps the subset of them with no blur at all. There is no
	// cache counter next to them, and that absence is the point: the blur is
	// evaluated in the shader, so there is nothing to hit, miss, upload or
	// evict. See the shadow section of the package documentation.
	shadowOps      uint64
	shadowSharpOps uint64

	// The glass counters. See [RendererStats].
	glassOps        uint64
	glassReducedOps uint64
	glassFullOps    uint64
	glassFallbacks  uint64
	glassPasses     uint64
	glassBatches    uint64
	glassLate       uint64
	// frameBatches is the number of draw calls issued in the frame in
	// progress. It is how [Renderer.Submit] knows whether it is still early
	// enough to redirect the frame into the scene target.
	frameBatches uint64
}

// Material is what a batch is drawn with. A batch ends where the material
// changes; see [Renderer.material].
type Material uint8

const (
	// MaterialNone is the empty batch.
	MaterialNone Material = iota
	// MaterialShape is the shared shape shader: fills, rounded fills and
	// strokes, all of them untextured.
	MaterialShape
	// MaterialGlyph is a textured quad sampling one glyph atlas page. Two
	// pages are two materials.
	MaterialGlyph
	// MaterialImage is a textured quad sampling one image texture. Two
	// pictures are two materials, which is the honest cost of not packing
	// thumbnails into an atlas of our own; see [TextureCache].
	MaterialImage
	// MaterialGlass is the composite pass of a glass material. It is never
	// batched with anything: a material region is a barrier, so it is always
	// a batch of exactly one quad. See [Renderer.appendMaterial].
	MaterialGlass
)

// String makes a failing test readable.
func (m Material) String() string {
	switch m {
	case MaterialShape:
		return "shape"
	case MaterialGlyph:
		return "glyph"
	case MaterialImage:
		return "image"
	case MaterialGlass:
		return "glass"
	default:
		return "none"
	}
}

// NewRenderer compiles the shape shader and returns a renderer.
//
// It needs no window and no graphics context: Kage is compiled on the CPU, and
// the compiled program is uploaded on first use. A test may therefore
// construct a Renderer headless.
func NewRenderer() (*Renderer, error) {
	sh, err := eb.NewShader(shapeShaderSrc)
	if err != nil {
		return nil, fmt.Errorf("gift/backend/ebiten: compiling the shape shader: %w", err)
	}
	gl, err := eb.NewShader(glassShaderSrc)
	if err != nil {
		return nil, fmt.Errorf("gift/backend/ebiten: compiling the glass shader: %w", err)
	}
	down, err := eb.NewShader(kawaseDownSrc)
	if err != nil {
		return nil, fmt.Errorf("gift/backend/ebiten: compiling the Kawase downsample shader: %w", err)
	}
	up, err := eb.NewShader(kawaseUpSrc)
	if err != nil {
		return nil, fmt.Errorf("gift/backend/ebiten: compiling the Kawase upsample shader: %w", err)
	}
	r := &Renderer{
		shader:      sh,
		glassShader: gl,
		downShader:  down,
		upShader:    up,
		atlas:       NewGlyphAtlas(AtlasConfig{}),
		textures:    NewTextureCache(TextureConfig{}),
		targets:     NewTargetPool(TargetConfig{}),
		policy:      NewGlassPolicy(GlassPolicyConfig{}),
	}
	r.glyphOpts.ColorScaleMode = eb.ColorScaleModePremultipliedAlpha
	r.imageOpts.ColorScaleMode = eb.ColorScaleModePremultipliedAlpha
	r.imageOpts.Filter = eb.FilterLinear
	// A copy and not a composite, and only for the region copy of a backdrop:
	// the pooled target is scratch memory, it is bucketed larger than the
	// request, and replacing it is the point.
	r.copyOpts.Blend = eb.BlendCopy
	r.copyOpts.ColorScaleMode = eb.ColorScaleModePremultipliedAlpha
	r.copyOpts.Filter = eb.FilterNearest
	// The scene to screen blit is a *composite*, source over, and that is a
	// semantic choice and not a performance one. Porter-Duff over is
	// associative, so compositing the scene onto the destination is pixel
	// identical to having drawn the frame onto the destination directly —
	// which is exactly what a frame without a material does. A copy would
	// make the same display list mean two different things depending on
	// whether a material happened to be present, and would erase whatever
	// the caller had already put in the target. The price is one destination
	// read per pixel per glass frame; see [Renderer.SetTarget].
	r.blitOpts.Blend = eb.BlendSourceOver
	r.blitOpts.ColorScaleMode = eb.ColorScaleModePremultipliedAlpha
	r.blitOpts.Filter = eb.FilterNearest
	// A blur pass replaces its target rather than blending into it. There is
	// deliberately no Filter here and there could not be one:
	// DrawTrianglesShaderOptions has no such field, because Ebitengine samples
	// nearest for a Kage shader and the blur shaders do their own filtering.
	// See kawase_down.kage.
	r.blurOpts.Blend = eb.BlendCopy
	// Set explicitly. It was already nearest, but only because
	// eb.FilterNearest happens to be the zero value of eb.Filter, and a
	// comment two fields up claimed the choice was deliberate. One of those
	// two statements had to become true.
	r.glyphOpts.Filter = eb.FilterNearest
	// Grown once, reused forever. The numbers are a starting point, not a
	// limit; a larger scene grows them on its first frames and never again.
	r.verts = make([]eb.Vertex, 0, 4096)
	r.idx = make([]uint32, 0, 6144)
	// Exactly one quad, forever: every material pass is four vertices.
	r.passVerts = make([]eb.Vertex, 0, 4)
	r.passIdx = make([]uint32, 0, 6)
	return r, nil
}

// SetTarget selects the image the next frame is drawn into. The backend does
// not own it and never keeps it beyond [Renderer.EndFrame].
//
// # What the target holds afterwards
//
// The frame is composited onto dst, source over, whatever it contains.
// Nothing in dst that the frame did not draw over is disturbed, and that is
// true whether or not the frame contains a material: a frame with a material
// is drawn into an offscreen and composited back with the same operator, so
// the two cases are pixel identical. Ebitengine clears the real screen before
// every Draw anyway; a caller that hands over its own image — a golden
// harness, for instance — keeps whatever it put there.
func (r *Renderer) SetTarget(dst *eb.Image) { r.screen, r.dst = dst, dst }

// Targets returns the intermediate render target pool, for [TargetStats] and
// for a test that wants a small budget in order to observe reuse, rejection
// and release.
func (r *Renderer) Targets() *TargetPool { return r.targets }

// SetTargets replaces the target pool. It is for tests.
func (r *Renderer) SetTargets(p *TargetPool) { r.targets = p }

// GlassPolicy returns the adaptive quality policy. See [GlassPolicy].
func (r *Renderer) GlassPolicy() *GlassPolicy { return r.policy }

// SetGlassPolicy replaces the policy. It is for tests and for an application
// that wants different thresholds.
func (r *Renderer) SetGlassPolicy(p *GlassPolicy) { r.policy = p }

// PinGlassQuality fixes the effective glass level, or returns to the adaptive
// policy when given [render.Adaptive].
//
// The project plan, section 13, makes this mandatory for any measurement that
// is to be compared with another one, because an adaptive level changes what
// is being measured half way through the run.
func (r *Renderer) PinGlassQuality(q render.GlassQuality) {
	if r.policy != nil {
		r.policy.Pin(q)
	}
}

// GlassLevel returns the level the last drawn frame actually used. It is never
// [render.Adaptive].
func (r *Renderer) GlassLevel() render.GlassQuality {
	if r.policy == nil {
		return render.Reduced
	}
	return r.policy.Level()
}

// BeginFrame implements [render.Backend].
//
// The size is not retained. It was, in a field that nothing ever read, and the
// obvious use for it — culling operations against the screen rectangle — is
// deliberately not implemented: every operation is already clipped against its
// clip rectangle, so a screen cull would save nothing but a few vertices while
// giving a collapsed container a second place to disappear quietly. The
// project plan, section 7, wants overflow visible, and the honest counter for
// "this was outside the visible area" is SkippedOutsideClip, which is about
// clips the application asked for rather than about the window.
func (r *Renderer) BeginFrame(size geom.Size) {
	if r.inFrame {
		panic("gift/backend/ebiten: BeginFrame without a matching EndFrame")
	}
	r.inFrame = true
	r.verts = r.verts[:0]
	r.idx = r.idx[:0]
	r.curMat, r.curPage = MaterialNone, nil
	r.frameBatches = 0
	r.frameSize = size

	// The glass level of the frame is decided here, once, from the intervals
	// of the frames already drawn and the material area of the previous one.
	// Deciding per material would give two panels of one frame different
	// appearances; deciding in Update would decide several times for one
	// drawn frame, which is the same mistake the upload budget avoids. See
	// the project plan, sections 6 and 8.
	if r.policy != nil {
		now := time.Now()
		if !r.lastDraw.IsZero() {
			r.policy.RecordInterval(now.Sub(r.lastDraw))
		}
		r.lastDraw = now
		r.frameQuality = r.policy.BeginFrame(float64(size.W) * float64(size.H))
	} else {
		r.frameQuality = render.Reduced
	}

	// The upload budget is per *drawn* frame, and this is the only callback
	// that happens once per drawn frame. Resetting it in an update instead
	// would hand the same budget out several times for one frame, because
	// Ebitengine may update more often than it draws; see the project plan,
	// sections 6 and 11.
	if r.textures != nil {
		r.textures.BeginFrame()
	}
}

// Images returns the image resource service of this renderer.
//
// It is what an application installs with gift.App.SetImages, which is the
// only route from a painter in ui to a texture in this package; see
// [render.Images].
func (r *Renderer) Images() render.Images { return r.textures }

// Textures returns the texture cache, for [TextureStats] and for a test that
// wants a small budget in order to observe admission and eviction.
func (r *Renderer) Textures() *TextureCache { return r.textures }

// SetTextures replaces the texture cache. It is for tests; the one installed
// by [NewRenderer] is the one an application wants.
func (r *Renderer) SetTextures(t *TextureCache) { r.textures = t }

// Atlas returns the glyph atlas of this renderer, for [GlyphAtlas.Stats] and
// for a test that wants to configure the budget.
func (r *Renderer) Atlas() *GlyphAtlas { return r.atlas }

// SetAtlas replaces the glyph atlas. It is for tests that need a small budget
// in order to observe eviction; the atlas installed by [NewRenderer] is the
// one an application wants.
func (r *Renderer) SetAtlas(a *GlyphAtlas) { r.atlas = a }

// Submit implements [render.Backend].
//
// The list is consumed completely before the call returns: every operation is
// turned into vertices here and now. Nothing derived from l survives the call,
// which is what [render.Backend] requires — the producer reuses the backing
// arrays for the next frame.
func (r *Renderer) Submit(l *render.List) {
	if !r.inFrame {
		panic("gift/backend/ebiten: Submit outside BeginFrame")
	}
	if l == nil {
		return
	}
	r.ensureScene(l)
	for _, op := range l.Ops() {
		r.appendOp(l, op)
		if len(r.verts) >= maxBatchVertices {
			r.flush()
		}
	}
}

// ensureScene redirects the frame into an offscreen when the list contains a
// material, because on Ebitengine a backdrop cannot be read from the screen.
//
// # The one place this implementation departs from section 8
//
// The project plan, section 8, says the glass backdrop is a "Regionskopie" and
// limits the extra cost to one target holding the material region. That is not
// implementable against Ebitengine's screen image, and the reason is in the
// pinned source rather than in an opinion: internal/atlas's (*Image).allocate
// panics with "atlas: a screen image cannot be created as a source" the moment
// a screen image is used as a draw source. Kage has no framebuffer fetch
// either — that is section 8's own first reason — so there are exactly two
// ways to obtain the pixels underneath a material, and reading the screen is
// not one of them. The other is to draw the frame somewhere that *can* be a
// source.
//
// So gift renders the whole frame into one screen sized offscreen and blits it
// to the screen in [Renderer.EndFrame]. The costs, stated rather than buried:
//
//   - One screen sized target, 8 MiB of logical pixel bytes at 1080p. It is
//     one per window and not one per widget, which is the thing the project
//     plan, section 11, actually forbids, and it is leased from the same
//     bounded pool as everything else.
//   - One full screen blit per frame, plus one full screen clear. On a fill
//     rate bound GPU that is two extra passes over the framebuffer.
//   - Both are paid only while a material is on screen. A frame with no
//     material never allocates the target and never blits; the pool ages it
//     out two seconds after the last glass panel disappears.
//
// The region copy itself is still a region copy, and the blur chain is still
// confined to the region. What section 8 could not have known is that the
// *source* of that copy has to be manufactured first.
//
// # Why the decision is taken here and not in BeginFrame
//
// Because BeginFrame has no list. The scan is one pass over the operations
// comparing a byte, which is cheap next to translating them, and it happens
// before any of them is drawn.
//
// A material in a *second* list, submitted after something has already been
// drawn, is too late: the pixels are on the screen and the screen cannot be
// read. Such a material degrades to the fallback and is counted in
// [RendererStats.GlassLate]. gift submits one list per frame, so the counter
// is expected to stay at zero; it exists because "expected to" is not the same
// as "does".
func (r *Renderer) ensureScene(l *render.List) {
	if r.scene != nil || r.screen == nil || r.targets == nil {
		return
	}
	b := r.screen.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	if !listHasVisibleMaterial(l, geom.Rc(0, 0, float32(w), float32(h))) {
		return
	}
	if r.frameBatches != 0 || len(r.idx) != 0 {
		r.glassLate++
		return
	}
	img := r.targets.Acquire(w, h)
	if img == nil {
		// The pool refused. Every material of this frame takes the fallback
		// path and TargetStats.Rejected says why.
		return
	}
	// The target is bucketed up to a multiple of 64 and may still hold the
	// previous frame's content, and unlike the screen nothing clears it for
	// us. Ebitengine clears the screen every frame — the project plan,
	// section 6, notes there is no partial repaint — so the scene has to look
	// the same.
	if r.drawFn == nil {
		img.Clear()
	}
	r.scene, r.sceneW, r.sceneH = img, w, h
	r.dst = img
}

// listHasVisibleMaterial reports whether l contains a material that will
// actually be drawn inside screen.
//
// # Why it is not merely "is there an OpMaterial"
//
// Because the project plan, section 11, as amended after WU-S, draws the line
// exactly here: the screen sized scene target is paid "nur solange ein
// Material *sichtbar* ist - nicht bloss, solange eines in der Display-Liste
// steht. Wer diesen Unterschied nicht erzwingt, zahlt bei 1080p acht Megabyte,
// ein Clear und einen Vollbild-Blit je Frame fuer ein Panel, das niemand
// sieht." A glass header scrolled out of its clip is precisely that panel.
//
// So the scan applies the same three tests [Renderer.appendMaterial] applies
// — empty bounds, empty clip, nothing surviving the clip and the screen — and
// it applies them in the same order and with the same arithmetic, so the two
// cannot disagree about whether a material is going to draw. It is still one
// pass over the operations with no allocation, and it short circuits on the
// first material that survives.
func listHasVisibleMaterial(l *render.List, screen geom.Rect) bool {
	if l.MaterialsLen() <= 1 {
		// The side table holds nothing but its sentinel, so no painter added
		// a material. One integer compare for the overwhelmingly common case.
		return false
	}
	for _, op := range l.Ops() {
		if op.Kind != render.OpMaterial || op.Material == 0 {
			continue
		}
		if l.Material(op.Material).Kind != render.MaterialGlass {
			continue
		}
		if op.Bounds.IsEmpty() {
			continue
		}
		clip := l.Clip(op.Clip)
		if clip.IsEmpty() {
			continue
		}
		region := l.Xform(op.Xform).TransformRect(op.Bounds).Canon()
		if region.Intersect(clip).Intersect(screen).IsEmpty() {
			continue
		}
		return true
	}
	return false
}

// EndFrame implements [render.Backend].
func (r *Renderer) EndFrame() {
	if !r.inFrame {
		panic("gift/backend/ebiten: EndFrame without a matching BeginFrame")
	}
	r.flush()
	// The scene to screen blit. It happens after the last batch and before
	// anything is released, which is the only order in which the frame is
	// complete and the target is still alive.
	if r.scene != nil {
		if r.screen != nil {
			r.glassPass(glassPassScene)
			r.blitOver(r.screen, r.scene, geom.Rc(0, 0, float32(r.sceneW), float32(r.sceneH)), 0, 0)
		}
		r.targets.Release(r.scene)
		r.scene = nil
	}
	r.inFrame = false
	r.dst, r.screen = nil, nil
	r.drawn++
	// Once per drawn frame, not once per update: the atlas budget is a per
	// frame budget and Ebitengine may update several times between two
	// frames. See the project plan, section 6.
	if r.atlas != nil {
		r.atlas.Tick()
	}
	// After the last draw call has been issued, which is the point at which
	// "referenced by the frame in progress" stops being true of any texture.
	if r.textures != nil {
		r.textures.Tick()
	}
	if r.targets != nil {
		if n := r.targets.Leased(); n != 0 {
			// A leaked lease is a pooled target nothing will ever hand back,
			// and the symptom a frame or two later is a glass panel showing
			// the backdrop of an older frame. Loud here, where the cause is
			// one function away, rather than quiet and visual.
			panic(fmt.Sprintf(
				"gift/backend/ebiten: %d render targets still leased at EndFrame; "+
					"every material pass must release what it acquired", n))
		}
		r.targets.Tick()
	}
}

// appendOp translates one operation into vertices.
func (r *Renderer) appendOp(l *render.List, op render.Op) {
	var radius, stroke, sigma float32
	switch op.Kind {
	case render.OpNone:
		// Counted, not silently dropped: Ops + Skipped + UnknownKinds has to
		// equal the length of the list, or the accounting cannot be used to
		// check anything.
		r.skipNone++
		return
	case render.OpFillRect:
		// radius and stroke stay zero.
	case render.OpFillRoundRect:
		radius = op.CornerRadius
	case render.OpStrokeRoundRect:
		radius = op.CornerRadius
		stroke = op.StrokeWidth
		if stroke <= 0 {
			r.skipZeroStroke++
			return
		}
	case render.OpShadow:
		// The shape and the blur arrive separately; see [render.OpShadow].
		// Nothing is cached, uploaded or rasterised here — the blur is
		// evaluated analytically in the shape shader, so a shadow is one quad
		// in the same batch as everything else. See shape.kage.
		radius = op.CornerRadius
		// Through render.Shadow and not inline: the exported formulation and
		// the renderer's must be one piece of arithmetic, or they drift.
		sigma = render.Shadow{Blur: op.Blur}.Sigma()
	case render.OpGlyphs:
		r.appendGlyphs(l, op)
		return
	case render.OpImage:
		r.appendImage(l, op)
		return
	case render.OpMaterial:
		r.appendMaterial(l, op)
		return
	default:
		// An unknown kind is skipped rather than fatal, as [render.OpKind]
		// documents: a newer gift with an older backend must still run.
		r.unknowns++
		return
	}

	if op.Color.IsTransparent() {
		r.skipTransparent++
		return
	}
	b := op.Bounds
	if b.IsEmpty() {
		r.skipEmptyBounds++
		return
	}
	clip := l.Clip(op.Clip)
	if clip.IsEmpty() {
		r.skipEmptyClip++
		return
	}

	halfW, halfH := b.Width()*0.5, b.Height()*0.5
	// Clamping here and not in the shader: it is a per operation constant,
	// and the shader runs per fragment.
	lim := halfW
	if halfH < lim {
		lim = halfH
	}
	if radius > lim {
		radius = lim
	}
	if radius < 0 {
		radius = 0
	}
	if stroke > lim {
		stroke = lim
	}

	xf := l.Xform(op.Xform)
	sx, sy := deviceScale(xf)
	// A single factor has to do for the radius and the stroke width, because
	// the distance field has one radius and not two. The smaller of the two
	// is the safe choice: it can never exceed the clamp limit min(halfW*sx,
	// halfH*sy) that the baked half extents imply, so the shape stays a valid
	// rounded box. See deviceScale for what this approximates.
	sr := sx
	if sy < sr {
		sr = sy
	}

	// The antialiasing pad is one device pixel, so in local units it is one
	// pixel divided by the scale of the axis it grows along.
	padX, padY := float32(0), float32(0)
	if radius > 0 || stroke > 0 {
		padX, padY = aaPad/sx, aaPad/sy
	}
	if sigma > 0 {
		// A shadow needs no antialiasing pad — it has no hard edge — but it
		// does need room for the falloff. [render.Shadow.Extent] is how far
		// the drawn falloff reaches in logical pixels; the shader measures in
		// device pixels, so the device pad is that times the scale and the
		// local pad is the device pad divided by the scale of the axis.
		dev := render.Shadow{Blur: op.Blur}.Extent() * sr
		padX, padY = dev/sx, dev/sy
	}
	quad := geom.Rect{
		Min: geom.Point{X: b.Min.X - padX, Y: b.Min.Y - padY},
		Max: geom.Point{X: b.Max.X + padX, Y: b.Max.Y + padY},
	}

	shape := shapeParams{
		color: op.Color,
		// Everything geometric is emitted in device pixels. That is what
		// lets the shader use a constant one pixel coverage band instead of
		// a screen space derivative; see shape.kage.
		halfW:  halfW * sx,
		halfH:  halfH * sy,
		radius: radius * sr,
		stroke: stroke * sr,
		// The local origin is the unpadded bounds, because that is the
		// coordinate system the distance field is defined in.
		originX: b.Min.X,
		originY: b.Min.Y,
		scaleX:  sx,
		scaleY:  sy,
	}
	if op.Kind == render.OpShadow {
		// The shadow encoding: a negative stroke slot carrying sigma. A zero
		// sigma leaves the slot at zero, which is a hard edged rounded fill
		// and is exactly what a shadow with a spread but no blur is.
		shape.stroke = -sigma * sr
		r.shadowOps++
		if sigma == 0 {
			r.shadowSharpOps++
		}
	}

	r.material(MaterialShape, nil)
	if xf.B == 0 && xf.C == 0 && xf.A != 0 && xf.D != 0 {
		r.appendAxisAligned(quad, clip, xf, shape)
		return
	}
	r.appendTransformed(quad, clip, xf, shape)
}

// appendImage turns one [render.OpImage] into a textured quad.
//
// The whole texture is mapped onto the operation's bounds. There is no source
// rectangle in the display list — see [render.OpImage] for why — so cropping
// is a clip, and the clip is applied here exactly as it is for a glyph: the
// visible rectangle is the intersection and the texture coordinates are
// interpolated into it. A cropped tile therefore costs no fragments for the
// part that is cut away, because the geometry is trimmed before it is emitted.
func (r *Renderer) appendImage(l *render.List, op render.Op) {
	if op.Color.IsTransparent() {
		r.skipTransparent++
		return
	}
	b := op.Bounds
	if b.IsEmpty() {
		r.skipEmptyBounds++
		return
	}
	clip := l.Clip(op.Clip)
	if clip.IsEmpty() {
		r.skipEmptyClip++
		return
	}
	if r.textures == nil {
		r.skipNoImage++
		return
	}
	img := r.textures.image(op.Image)
	w, h, ok := r.textures.size(op.Image)
	if !ok || img == nil || w <= 0 || h <= 0 {
		// An operation whose resource is not resident. It is not fatal and it
		// is not silent: a producer that resolved its handle this frame
		// cannot get here, because a resolved texture cannot be evicted
		// during the frame that resolved it.
		r.skipNoImage++
		return
	}

	src := geom.Rc(0, 0, float32(w), float32(h))
	xf := l.Xform(op.Xform)
	r.material(MaterialImage, img)
	var wrote bool
	if xf.B == 0 && xf.C == 0 && xf.A > 0 && xf.D > 0 {
		wrote = r.appendTexturedQuad(b, src, clip, xf, op.Color)
	} else {
		wrote = r.appendTexturedQuadTransformed(b, src, clip, xf, op.Color)
	}
	if !wrote {
		r.skipOutsideClip++
		return
	}
	r.imageOps++
	r.emitted++
}

// material starts a new batch when the material of the next primitive differs
// from the one under construction.
//
// # Why interleaving and not sorting
//
// Glyphs are textured quads and shapes are not, so "one draw call per frame"
// becomes "one draw call per material run". The cheap way to get the old
// number back would be to collect all the text of a frame and draw it in one
// pass at the end. gift does not do that, and the project plan, section 11, is
// why: globally reordering transparent content merely to reduce draw calls is
// forbidden, and for a good reason — a label drawn between two overlapping
// panels would move in front of the second one, and the bug would appear only
// when two things happened to overlap.
//
// So the display list order is the drawing order, always, and a batch ends
// wherever the material changes. A scene pays one draw call per run of
// same-material operations, which for the usual "panel, text, panel, text"
// nesting is two per text bearing container and one for everything that is not
// text.
func (r *Renderer) material(m Material, page *eb.Image) {
	if r.curMat == m && r.curPage == page {
		return
	}
	r.flush()
	r.curMat, r.curPage = m, page
}

// appendGlyphs turns one [render.OpGlyphs] into textured quads.
//
// It never shapes, measures or lays out anything: the positions arrive in the
// display list and the only lookup is the atlas one, which is a map read on a
// comparable struct key. The project plan, section 3, puts shaping in
// internal/text, and this function is where that boundary is actually visible.
func (r *Renderer) appendGlyphs(l *render.List, op render.Op) {
	if op.Color.IsTransparent() {
		r.skipTransparent++
		return
	}
	clip := l.Clip(op.Clip)
	if clip.IsEmpty() {
		r.skipEmptyClip++
		return
	}
	gs := l.Glyphs(op.Glyphs, op.GlyphCount)
	if len(gs) == 0 || r.atlas == nil {
		r.skipEmptyText++
		return
	}

	xf := l.Xform(op.Xform)
	// The raster scale of the run. It is the density of the display at the
	// root of the display list, times whatever a container above this text
	// added, and it is the factor the atlas rasterises at; see
	// [GlyphAtlas.Lookup]. One factor and not two, because a glyph mask has
	// one resolution: under a non-uniform scale the text is drawn uniformly
	// at the smaller factor rather than stretched, which is the same choice
	// and the same reason as the corner radius in [Renderer.appendOp].
	sx, sy := deviceScale(xf)
	scale := sx
	if sy < scale {
		scale = sy
	}
	fast := xf.B == 0 && xf.C == 0 && xf.A > 0 && xf.D > 0
	drawn := false
	for i := range gs {
		g := &gs[i]
		ei, ok := r.atlas.Lookup(*g, scale)
		if !ok {
			continue
		}
		e := r.atlas.Entry(ei)
		if !e.inked {
			// A space. It occupies advance, not pixels.
			continue
		}
		r.material(MaterialGlyph, r.atlas.Page(ei))
		src := geom.Rc(float32(e.x), float32(e.y), float32(e.x+e.w), float32(e.y+e.h))
		var wrote bool
		if fast {
			// The mask is already in device pixels, so only its *origin* is
			// transformed and its extent is copied one to one. Mapping the
			// whole rectangle through xf instead would scale a bitmap that
			// was rasterised for this scale in the first place, and under a
			// nearest filter that duplicates rows of coverage rather than
			// resampling them. At scale one this is the arithmetic it always
			// was: the origin is a translation and the extent is unchanged.
			ox := xf.A*g.X + xf.TX
			oy := xf.D*g.Y + xf.TY
			dev := geom.Rc(
				ox+float32(e.left), oy+float32(e.top),
				ox+float32(e.left+e.w), oy+float32(e.top+e.h))
			wrote = r.appendDeviceQuad(dev, src, clip, op.Color)
		} else {
			// The general path keeps mapping a *local* rectangle, because a
			// rotation has to rotate the glyph with the text. The extent is
			// divided by the scale it was rasterised at so that the product
			// is the mask's own size again. gift produces no such transform;
			// see [Renderer.appendTexturedQuadTransformed].
			dst := geom.Rc(
				g.X+float32(e.left)/scale, g.Y+float32(e.top)/scale,
				g.X+float32(e.left+e.w)/scale, g.Y+float32(e.top+e.h)/scale)
			wrote = r.appendTexturedQuadTransformed(dst, src, clip, xf, op.Color)
		}
		if wrote {
			r.glyphQuads++
			drawn = true
		}
	}
	if drawn {
		r.emitted++
		return
	}
	// Exactly one counter per operation, or the accounting in
	// [RendererStats.Accounted] stops adding up. A run whose every glyph was
	// clipped away, blank or unresolvable is an operation that drew nothing.
	r.skipEmptyText++
}

// appendTexturedQuad is the fast path for a translation and a positive scale,
// which is everything gift produces. The clip is a rectangle intersection in
// device space and the texture coordinates follow from a linear interpolation
// inside it.
//
// It serves both textured materials: a glyph quad and an image quad differ
// only in which texture the batch samples, and the mapping from a destination
// rectangle to a source rectangle through a clip is the same arithmetic. See
// [Renderer.appendImage].
//
// # Glyphs no longer come through here, and that is the fix of WU-W
//
// This function used to serve glyphs too, and its documentation carried a
// warning: the destination rectangle was the atlas rectangle mapped through
// xf, so a scale other than one stretched a bitmap rasterised at the glyph's
// nominal size, and under a nearest filter that duplicates and drops rows of
// coverage instead of resampling them. The note said the correct answer was
// to rasterise at the effective size and fold the device scale into the atlas
// key. That is what the device density of the project plan, section 18, now
// does — see [GlyphAtlas.Lookup] — and the note has become the behaviour:
// [Renderer.appendGlyphs] transforms the glyph *origin* and hands the mask's
// own device extent to [Renderer.appendDeviceQuad].
//
// The warning is therefore gone rather than more urgent. What remains true is
// the assumption *this* function still makes, which is nothing: it maps a
// rectangle through a transform and interpolates texture coordinates into the
// clip, at any scale, and an image is resampled on purpose. A thumbnail comes
// off a ladder of a few sizes and is drawn at whatever the tile rectangle
// happens to be, so the image material uses a linear filter while the glyph
// material uses a nearest one. That is the only difference between the two
// and it lives in the draw options, not here.
func (r *Renderer) appendTexturedQuad(dst, src, clip geom.Rect, xf geom.Affine2D, col render.Color) bool {
	dev := geom.Rc(
		xf.A*dst.Min.X+xf.TX, xf.D*dst.Min.Y+xf.TY,
		xf.A*dst.Max.X+xf.TX, xf.D*dst.Max.Y+xf.TY)
	return r.appendDeviceQuad(dev, src, clip, col)
}

// appendDeviceQuad emits one textured quad whose destination is already in
// device space, clipped to clip with the texture coordinates interpolated
// into the visible part.
//
// It is the second half of [Renderer.appendTexturedQuad] and the whole of the
// glyph path, which has a device rectangle to begin with because a glyph mask
// is rasterised in device pixels.
func (r *Renderer) appendDeviceQuad(dev, src, clip geom.Rect, col render.Color) bool {
	vis := dev.Intersect(clip)
	if vis.IsEmpty() {
		return false
	}
	du, dv := src.Width()/dev.Width(), src.Height()/dev.Height()
	u0 := src.Min.X + (vis.Min.X-dev.Min.X)*du
	u1 := src.Min.X + (vis.Max.X-dev.Min.X)*du
	v0 := src.Min.Y + (vis.Min.Y-dev.Min.Y)*dv
	v1 := src.Min.Y + (vis.Max.Y-dev.Min.Y)*dv

	base := uint32(len(r.verts))
	r.verts = append(r.verts,
		texturedVertex(vis.Min.X, vis.Min.Y, u0, v0, col),
		texturedVertex(vis.Max.X, vis.Min.Y, u1, v0, col),
		texturedVertex(vis.Max.X, vis.Max.Y, u1, v1, col),
		texturedVertex(vis.Min.X, vis.Max.Y, u0, v1, col),
	)
	r.idx = append(r.idx, base, base+1, base+2, base, base+2, base+3)
	return true
}

// appendTexturedQuadTransformed is the general path: the quad is mapped into
// device space and clipped as a convex polygon, with the texture coordinates
// interpolated along with the corners. gift produces no transform that needs
// it — a scroll container pushes a translation, which takes the fast path
// above — so this is exercised by tests only. It exists so that a rotated or
// mirrored container later is a display list change and not a backend
// rewrite.
func (r *Renderer) appendTexturedQuadTransformed(dst, src, clip geom.Rect, xf geom.Affine2D, col render.Color) bool {
	poly := &r.poly[0]
	other := &r.poly[1]
	corners := [4]geom.Point{
		{X: dst.Min.X, Y: dst.Min.Y},
		{X: dst.Max.X, Y: dst.Min.Y},
		{X: dst.Max.X, Y: dst.Max.Y},
		{X: dst.Min.X, Y: dst.Max.Y},
	}
	uvs := [4]geom.Point{
		{X: src.Min.X, Y: src.Min.Y},
		{X: src.Max.X, Y: src.Min.Y},
		{X: src.Max.X, Y: src.Max.Y},
		{X: src.Min.X, Y: src.Max.Y},
	}
	for i, c := range corners {
		d := xf.Apply(c)
		// lx and ly carry the texture coordinate here rather than a local
		// position; the clipper interpolates whatever is in them.
		poly[i] = clipVertex{dx: d.X, dy: d.Y, lx: uvs[i].X, ly: uvs[i].Y}
	}
	n := 4
	n = clipHalfPlane(poly, n, other, edgeLeft, clip.Min.X)
	poly, other = other, poly
	n = clipHalfPlane(poly, n, other, edgeRight, clip.Max.X)
	poly, other = other, poly
	n = clipHalfPlane(poly, n, other, edgeTop, clip.Min.Y)
	poly, other = other, poly
	n = clipHalfPlane(poly, n, other, edgeBottom, clip.Max.Y)
	poly, other = other, poly
	if n < 3 {
		return false
	}
	base := uint32(len(r.verts))
	for i := 0; i < n; i++ {
		v := poly[i]
		r.verts = append(r.verts, texturedVertex(v.dx, v.dy, v.lx, v.ly, col))
	}
	for i := 1; i < n-1; i++ {
		r.idx = append(r.idx, base, base+uint32(i), base+uint32(i)+1)
	}
	return true
}

// texturedVertex builds one vertex of a textured quad, glyph or image.
//
// The colour travels unconverted, exactly as for shapes: render.Color is
// premultiplied, the glyph options say the vertex colour scale is
// premultiplied, and the atlas holds premultiplied white coverage. Nothing in
// the frame path converts a colour.
func texturedVertex(dx, dy, u, v float32, c render.Color) eb.Vertex {
	return eb.Vertex{
		DstX: dx, DstY: dy,
		SrcX: u, SrcY: v,
		ColorR: c.R, ColorG: c.G, ColorB: c.B, ColorA: c.A,
	}
}

// shapeParams are the per operation values that end up in the vertex
// attributes.
//
// halfW, halfH, radius and stroke are already in device pixels. scaleX and
// scaleY are the factors that got them there and are applied to the local
// position of every vertex as it is written, so that the distance field the
// shader evaluates is measured in device pixels throughout.
type shapeParams struct {
	color            render.Color
	halfW, halfH     float32
	radius, stroke   float32
	originX, originY float32
	scaleX, scaleY   float32
}

// deviceScale returns how many device pixels one local unit covers along the
// local x and y axis under xf.
//
// Until WU-W this answered (1, 1) for every operation gift produced, because
// the root of the display list was the identity and the only other producer
// was a scroll container's translation. It is now the device density of the
// project plan, section 18: on a 2x display every operation arrives under a
// uniform scale of two, and everything below that is measured in device
// pixels — the radius, the stroke width, the antialiasing pad, the shadow
// sigma, and in glass.go the blur radius and the refraction — is finally
// exercised with a factor other than one.
//
// This is the whole trick that lets the shape shader work without dfdx and
// dfdy, so it is worth being precise about when it is exact.
//
//   - Identity and pure translation: (1, 1), exactly. A translation does not
//     change lengths.
//   - Pure rotation: (1, 1) up to the rounding of sin and cos. A rotation does
//     not change lengths either.
//   - Axis aligned scale, including a mirror: (|sx|, |sy|), exactly.
//   - Rotation composed with a scale: the column norms are exactly the two
//     scale factors, because the rotation contributes no length.
//
// Straight edges stay exact in all of these, because the box is axis aligned
// in local space and a distance to a vertical edge is a pure x distance. The
// one approximation is a *rounded corner under a non-uniform scale*: a circle
// scaled by different factors is an ellipse, and this distance field can only
// express a circle. gift clamps the radius with the smaller factor, so such a
// corner is drawn slightly tighter than a true ellipse would be. The
// alternative — rejecting the transform — was not chosen because nothing in
// gift produces a non-uniform scale today, the error is bounded by
// radius*|sx-sy| and confined to the four corner arcs, and a slightly rounder
// corner is a better failure than a missing widget.
//
// A skewing transform has non-orthogonal columns and is the only case where
// even the edges are approximate; the result is a marginally soft or hard
// edge, never a wrong shape. gift cannot currently produce one.
//
// The function allocates nothing and is inlinable.
func deviceScale(xf geom.Affine2D) (sx, sy float32) {
	if xf.IsAxisAligned() {
		// Exact and square-root free, which is also the only path any
		// display list gift produces today ever takes.
		sx, sy = xf.A, xf.D
		if sx < 0 {
			sx = -sx
		}
		if sy < 0 {
			sy = -sy
		}
	} else {
		sx, sy = xf.ScaleFactors()
	}
	// A singular or non-finite transform collapses the shape anyway; falling
	// back to 1 only keeps the padding arithmetic finite.
	if !(sx > 0) || !(sx < inf) {
		sx = 1
	}
	if !(sy > 0) || !(sy < inf) {
		sy = 1
	}
	return sx, sy
}

// inf bounds the scale sanity check in [deviceScale].
var inf = float32(math.Inf(1))

// clipVertex is a polygon corner during clipping: a device space position and
// the local space position that belongs to it.
type clipVertex struct {
	dx, dy float32
	lx, ly float32
}

// appendAxisAligned is the fast path for a transform whose linear part maps
// the axes onto themselves, which covers the identity, a translation and a
// scale — that is, everything gift produces today and everything scrolling
// will produce.
//
// The clip is then a rectangle intersection in device space, and the local
// coordinates of the clipped corners follow from the inverse of a diagonal
// matrix, so no polygon clipping is needed at all.
func (r *Renderer) appendAxisAligned(quad, clip geom.Rect, xf geom.Affine2D, sh shapeParams) {
	dev := xf.TransformRect(quad).Canon()
	vis := dev.Intersect(clip)
	if vis.IsEmpty() {
		r.skipOutsideClip++
		return
	}

	// Inverse of x -> A*x + TX and y -> D*y + TY.
	invA, invD := 1/xf.A, 1/xf.D
	l0x := (vis.Min.X-xf.TX)*invA - sh.originX
	l1x := (vis.Max.X-xf.TX)*invA - sh.originX
	l0y := (vis.Min.Y-xf.TY)*invD - sh.originY
	l1y := (vis.Max.Y-xf.TY)*invD - sh.originY
	// A negative scale flips the mapping; keep the local coordinates paired
	// with the device corner they belong to.
	if invA < 0 {
		l0x, l1x = l1x, l0x
	}
	if invD < 0 {
		l0y, l1y = l1y, l0y
	}

	base := uint32(len(r.verts))
	r.verts = append(r.verts,
		vertex(vis.Min.X, vis.Min.Y, l0x, l0y, sh),
		vertex(vis.Max.X, vis.Min.Y, l1x, l0y, sh),
		vertex(vis.Max.X, vis.Max.Y, l1x, l1y, sh),
		vertex(vis.Min.X, vis.Max.Y, l0x, l1y, sh),
	)
	r.idx = append(r.idx, base, base+1, base+2, base, base+2, base+3)
	r.emitted++
}

// appendTransformed is the general path: the quad is transformed into device
// space, clipped against the four half planes of the clip rectangle as a
// convex polygon and triangulated as a fan.
//
// It is exact for any affine transform, including rotation and skew. gift does
// not push such a transform yet, so this path is exercised by tests only; see
// the package documentation.
func (r *Renderer) appendTransformed(quad, clip geom.Rect, xf geom.Affine2D, sh shapeParams) {
	src := &r.poly[0]
	dstBuf := &r.poly[1]

	corners := [4]geom.Point{
		{X: quad.Min.X, Y: quad.Min.Y},
		{X: quad.Max.X, Y: quad.Min.Y},
		{X: quad.Max.X, Y: quad.Max.Y},
		{X: quad.Min.X, Y: quad.Max.Y},
	}
	for i, c := range corners {
		d := xf.Apply(c)
		src[i] = clipVertex{dx: d.X, dy: d.Y, lx: c.X - sh.originX, ly: c.Y - sh.originY}
	}
	n := 4

	n = clipHalfPlane(src, n, dstBuf, edgeLeft, clip.Min.X)
	src, dstBuf = dstBuf, src
	n = clipHalfPlane(src, n, dstBuf, edgeRight, clip.Max.X)
	src, dstBuf = dstBuf, src
	n = clipHalfPlane(src, n, dstBuf, edgeTop, clip.Min.Y)
	src, dstBuf = dstBuf, src
	n = clipHalfPlane(src, n, dstBuf, edgeBottom, clip.Max.Y)
	src, dstBuf = dstBuf, src

	if n < 3 {
		r.skipOutsideClip++
		return
	}
	base := uint32(len(r.verts))
	for i := 0; i < n; i++ {
		v := src[i]
		r.verts = append(r.verts, vertex(v.dx, v.dy, v.lx, v.ly, sh))
	}
	for i := 1; i < n-1; i++ {
		r.idx = append(r.idx, base, base+uint32(i), base+uint32(i)+1)
	}
	r.emitted++
}

// edge selects which half plane [clipHalfPlane] keeps.
type edge uint8

const (
	edgeLeft edge = iota
	edgeRight
	edgeTop
	edgeBottom
)

// clipHalfPlane clips the convex polygon in src, which has n vertices, against
// one axis aligned half plane and writes the result to dst. It returns the
// number of vertices written.
//
// This is Sutherland–Hodgman over fixed size arrays: it allocates nothing, and
// a convex quad clipped by four planes can never exceed eight vertices.
func clipHalfPlane(src *[8]clipVertex, n int, dst *[8]clipVertex, e edge, v float32) int {
	if n == 0 {
		return 0
	}
	out := 0
	prev := src[n-1]
	prevIn := inside(prev, e, v)
	for i := 0; i < n; i++ {
		cur := src[i]
		curIn := inside(cur, e, v)
		if curIn != prevIn && out < len(dst) {
			dst[out] = intersectEdge(prev, cur, e, v)
			out++
		}
		if curIn && out < len(dst) {
			dst[out] = cur
			out++
		}
		prev, prevIn = cur, curIn
	}
	return out
}

func inside(p clipVertex, e edge, v float32) bool {
	switch e {
	case edgeLeft:
		return p.dx >= v
	case edgeRight:
		return p.dx <= v
	case edgeTop:
		return p.dy >= v
	default:
		return p.dy <= v
	}
}

// intersectEdge returns the point where the segment a-b crosses the half plane
// boundary, with the local coordinate interpolated along with it.
func intersectEdge(a, b clipVertex, e edge, v float32) clipVertex {
	var t float32
	switch e {
	case edgeLeft, edgeRight:
		if d := b.dx - a.dx; d != 0 {
			t = (v - a.dx) / d
		}
	default:
		if d := b.dy - a.dy; d != 0 {
			t = (v - a.dy) / d
		}
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return clipVertex{
		dx: a.dx + (b.dx-a.dx)*t,
		dy: a.dy + (b.dy-a.dy)*t,
		lx: a.lx + (b.lx-a.lx)*t,
		ly: a.ly + (b.ly-a.ly)*t,
	}
}

// vertex builds one Ebitengine vertex.
//
// lx and ly arrive in local units and are scaled to device pixels here, which
// is the last step of the bake described on [deviceScale].
//
// The colour is copied straight through: [render.Color] is premultiplied and
// so is everything Ebitengine consumes, so there is no conversion here and in
// particular none in the frame path. See the project plan, section 8.
func vertex(dx, dy, lx, ly float32, sh shapeParams) eb.Vertex {
	return eb.Vertex{
		DstX:    dx,
		DstY:    dy,
		SrcX:    lx * sh.scaleX,
		SrcY:    ly * sh.scaleY,
		ColorR:  sh.color.R,
		ColorG:  sh.color.G,
		ColorB:  sh.color.B,
		ColorA:  sh.color.A,
		Custom0: sh.halfW,
		Custom1: sh.halfH,
		Custom2: sh.radius,
		Custom3: sh.stroke,
	}
}

// flush issues the accumulated geometry as one draw call and empties the
// buffers without releasing their capacity.
func (r *Renderer) flush() {
	if len(r.idx) == 0 {
		r.verts = r.verts[:0]
		r.curMat, r.curPage = MaterialNone, nil
		return
	}
	switch {
	case r.drawFn != nil:
		r.drawFn(r.curMat, r.verts, r.idx)
	case r.dst == nil:
		// No target: the geometry is still accounted for, which is what makes
		// the counters usable from a headless test.
	case r.curMat == MaterialGlyph:
		r.dst.DrawTriangles32(r.verts, r.idx, r.curPage, &r.glyphOpts)
	case r.curMat == MaterialImage:
		r.dst.DrawTriangles32(r.verts, r.idx, r.curPage, &r.imageOpts)
	default:
		r.dst.DrawTrianglesShader32(r.verts, r.idx, r.shader, &r.opts)
	}
	r.batches++
	r.frameBatches++
	switch r.curMat {
	case MaterialGlyph:
		r.glyphBatches++
	case MaterialImage:
		r.imageBatches++
	default:
		r.shapeBatches++
	}
	r.verts = r.verts[:0]
	r.idx = r.idx[:0]
	r.curMat, r.curPage = MaterialNone, nil
}

// RendererStats are the counters of the renderer. Like [gift.Diagnostics]
// they are plain numbers written in the frame path and read out of band.
//
// # The accounting is total
//
// For every submitted list, Ops + Skipped() + UnknownKinds equals the number
// of operations in it. Nothing falls off the edge — [render.OpNone] used to,
// which meant the three numbers did not add up and no consumer could tell
// whether a discrepancy was a dropped no-op or a defect.
//
// # Why the skip reasons are separate
//
// There used to be one Skipped counter, and it answered two questions that
// have nothing to do with each other: "the application asked for something
// invisible", which is normal, and "a container collapsed and its content
// disappeared", which is a layout defect. A stack that starved thirty of forty
// rows showed up as a slightly larger number in a field whose documentation
// said "transparent, empty, or entirely outside their clip", and the defect
// survived a whole work unit. Separated, SkippedEmptyBounds rising is a
// question worth asking and SkippedTransparent rising is not.
type RendererStats struct {
	// Frames is the number of completed frames.
	Frames uint64
	// Batches is the number of draw calls issued. It is ShapeBatches plus
	// GlyphBatches.
	Batches uint64
	// ShapeBatches and GlyphBatches split the draw calls by material. A
	// shapes only scene has exactly one of the former and none of the
	// latter; text costs one extra batch per run of text in display list
	// order, and per atlas page switch inside such a run. See
	// [Renderer.material] for why they are not sorted together.
	ShapeBatches, GlyphBatches uint64
	// ImageBatches is the number of draw calls issued for image material.
	// One per run of consecutive operations sampling the same texture, so a
	// gallery of sixty visible thumbnails issues sixty of them.
	//
	// That number looks alarming and mostly is not, which is why it is
	// reported rather than hidden behind an atlas nobody measured.
	// Ebitengine's graphicscommand merges two consecutive draw commands whose
	// backend source images are the same object — see
	// CanMergeWithDrawTrianglesCommand in the pinned module — and thumbnails
	// that fit its automatic atlas share one. So this counts the calls this
	// package makes, not the draws the GPU performs, and the two differ by
	// however much of the working set happens to share a page. See
	// [TextureCache] for why there is no atlas of our own.
	ImageBatches uint64
	// ImageOps is the number of image operations that produced geometry.
	ImageOps uint64
	// GlyphQuads is the number of glyph quads emitted. Together with Ops it
	// says how much of a frame is text.
	GlyphQuads uint64
	// ShadowOps is the number of [render.OpShadow] operations that produced
	// geometry, and ShadowSharpOps the subset of them whose blur was zero or
	// less and which therefore drew a hard edged rounded rectangle.
	//
	// There is deliberately no cache hit ratio beside these. gift evaluates
	// the Gaussian analytically in the shape shader, so a shadow allocates no
	// texture, uploads no pixels and evicts nothing.
	//
	// What it does cost is fill rate, and that is the number worth watching
	// on a GPU that is fill rate bound — which the Raspberry Pi 4 of the
	// project plan, section 1, is. A shadow's quad is its shape grown by
	// [render.ShadowSigmas] times sigma on every side, and sigma is half the
	// blur: a 100x40 button with Blur 16 draws a 148x88 quad, 3.3 times the
	// area of the button it sits behind, every one of whose fragments runs
	// two or three exp calls. Twenty such shadows are 260 kilopixels of
	// shaded area before anything else on the screen is drawn.
	//
	// So the thing to do about a slow frame full of shadows is to reduce the
	// blur, not to look for a cache. ShadowOps times the extent of the
	// operations is the whole cost model.
	ShadowOps, ShadowSharpOps uint64

	// GlassOps is the number of material regions that produced passes, and
	// GlassReducedOps and GlassFullOps split them by the level each was
	// actually drawn at. GlassFallbacks is the number drawn as a plain tinted
	// rounded rectangle because no backdrop could be obtained.
	//
	// The three add up to GlassOps. A non zero GlassFallbacks on a machine
	// with a window means either the target budget refused a lease — see
	// [TargetStats.Rejected] — or a material region larger than the screen.
	GlassOps, GlassReducedOps, GlassFullOps, GlassFallbacks uint64
	// GlassPasses is the number of material pass stages executed: one copy,
	// two per blur level at Full, and one composite. A Reduced panel is two,
	// a Full panel with three levels is eight.
	GlassPasses uint64
	// GlassDrawCalls is the number of draw calls the material passes issued,
	// including the scene to screen blit. It is part of DrawCalls.
	//
	// # What a material does to the draw call count
	//
	// A material region is a batching barrier: everything before it has to
	// reach the target before its backdrop can be copied. So a frame that
	// used to be one shape batch becomes, with one glass panel in the middle
	// of it: the shape batch before the panel, the region copy, the blur
	// passes, the composite, and the shape batch after the panel — plus the
	// scene blit at the end of the frame, which is paid once however many
	// panels there are.
	//
	// That is the honest number and it is not small in relative terms. What
	// it is not is proportional to the scene: a panel costs the same handful
	// of calls over a gallery of sixty tiles as over an empty window.
	GlassDrawCalls uint64
	// GlassLate is the number of frames in which a material was found after
	// something had already been drawn to the screen, so the frame could not
	// be redirected into the scene target any more. gift submits one list per
	// frame, so this is expected to stay at zero; see [Renderer.ensureScene].
	GlassLate uint64
	// GlassLevel is the level the last drawn frame used and GlassPinned
	// whether the application fixed it. The project plan, section 8, requires
	// the effective level to be visible in the diagnostics.
	GlassLevel  render.GlassQuality
	GlassPinned bool

	// Ops is the number of operations that produced geometry.
	Ops uint64

	// SkippedNone counts [render.OpNone], the explicit no-op.
	SkippedNone uint64
	// SkippedTransparent counts operations with a fully transparent colour.
	// This is normal: it is how a view says "no background".
	SkippedTransparent uint64
	// SkippedEmptyBounds counts operations whose own bounds are empty.
	//
	// This is the interesting one. A node that reports a zero extent on an
	// axis lands here, and in a scene where every node is supposed to have a
	// size, a non zero value is a layout defect rather than a saving. An
	// unframed Box on the main axis of a stack legitimately produces these,
	// which is documented on [ui.Box].
	SkippedEmptyBounds uint64
	// SkippedEmptyClip counts operations under a clip rectangle that is
	// itself empty, so nothing below it could be visible.
	SkippedEmptyClip uint64
	// SkippedOutsideClip counts operations that are non empty but lie
	// entirely outside their clip rectangle. This is the counter that a
	// scrolled or deliberately clipped overflow produces.
	SkippedOutsideClip uint64
	// SkippedZeroStroke counts strokes with a width of zero or less.
	SkippedZeroStroke uint64
	// SkippedNoImage counts image operations whose resource was not resident:
	// an id of zero, or one whose texture was evicted between the resolution
	// and the draw. A view that draws a placeholder when [render.Images]
	// refuses it never produces one; a non zero value means somebody emitted
	// an operation for a handle it had not resolved this frame.
	SkippedNoImage uint64
	// SkippedEmptyText counts glyph operations that produced no quad: an
	// empty range, a run of nothing but spaces, a run entirely outside its
	// clip, or — the one worth watching — a run whose glyphs the atlas
	// refused. Cross check it against [AtlasStats.Rejected] before blaming
	// the layout.
	SkippedEmptyText uint64

	// UnknownKinds is the number of operations whose kind this backend does
	// not know.
	UnknownKinds uint64
}

// Skipped is the total number of operations that produced no geometry. It is
// the sum of the seven reasons above and exists so that the total accounting
// is one expression.
func (s RendererStats) Skipped() uint64 {
	return s.SkippedNone + s.SkippedTransparent + s.SkippedEmptyBounds +
		s.SkippedEmptyClip + s.SkippedOutsideClip + s.SkippedZeroStroke +
		s.SkippedEmptyText + s.SkippedNoImage
}

// Accounted is Ops + Skipped + UnknownKinds. It must equal the total number of
// operations submitted.
func (s RendererStats) Accounted() uint64 {
	return s.Ops + s.Skipped() + s.UnknownKinds
}

// Stats returns the renderer counters. It is not synchronised and belongs to
// the UI executor; the frame timings, which another goroutine may read, are in
// [FrameTimer] instead.
func (r *Renderer) Stats() RendererStats {
	s := RendererStats{
		Frames:             r.drawn,
		Batches:            r.batches,
		Ops:                r.emitted,
		SkippedNone:        r.skipNone,
		SkippedTransparent: r.skipTransparent,
		SkippedEmptyBounds: r.skipEmptyBounds,
		SkippedEmptyClip:   r.skipEmptyClip,
		SkippedOutsideClip: r.skipOutsideClip,
		SkippedZeroStroke:  r.skipZeroStroke,
		SkippedEmptyText:   r.skipEmptyText,
		SkippedNoImage:     r.skipNoImage,
		UnknownKinds:       r.unknowns,
		ShapeBatches:       r.shapeBatches,
		GlyphBatches:       r.glyphBatches,
		ImageBatches:       r.imageBatches,
		ImageOps:           r.imageOps,
		GlyphQuads:         r.glyphQuads,
		ShadowOps:          r.shadowOps,
		ShadowSharpOps:     r.shadowSharpOps,
		GlassOps:           r.glassOps,
		GlassReducedOps:    r.glassReducedOps,
		GlassFullOps:       r.glassFullOps,
		GlassFallbacks:     r.glassFallbacks,
		GlassPasses:        r.glassPasses,
		GlassDrawCalls:     r.glassBatches,
		GlassLate:          r.glassLate,
	}
	if r.policy != nil {
		s.GlassLevel = r.policy.Level()
		s.GlassPinned = r.policy.IsPinned()
	}
	return s
}
