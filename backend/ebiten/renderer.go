package ebiten

import (
	_ "embed"
	"fmt"
	"math"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

//go:embed shape.kage
var shapeShaderSrc []byte

// ShapeShaderSource returns the Kage source of the shared shape shader.
//
// It is exported so that a test can compile it without a window;
// [eb.NewShader] needs no graphics context.
func ShapeShaderSource() []byte { return shapeShaderSrc }

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
	r := &Renderer{
		shader:   sh,
		atlas:    NewGlyphAtlas(AtlasConfig{}),
		textures: NewTextureCache(TextureConfig{}),
	}
	r.glyphOpts.ColorScaleMode = eb.ColorScaleModePremultipliedAlpha
	r.imageOpts.ColorScaleMode = eb.ColorScaleModePremultipliedAlpha
	r.imageOpts.Filter = eb.FilterLinear
	// Set explicitly. It was already nearest, but only because
	// eb.FilterNearest happens to be the zero value of eb.Filter, and a
	// comment two fields up claimed the choice was deliberate. One of those
	// two statements had to become true.
	r.glyphOpts.Filter = eb.FilterNearest
	// Grown once, reused forever. The numbers are a starting point, not a
	// limit; a larger scene grows them on its first frames and never again.
	r.verts = make([]eb.Vertex, 0, 4096)
	r.idx = make([]uint32, 0, 6144)
	return r, nil
}

// SetTarget selects the image the next frame is drawn into. The backend does
// not own it and never keeps it beyond [Renderer.EndFrame].
func (r *Renderer) SetTarget(dst *eb.Image) { r.dst = dst }

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
func (r *Renderer) BeginFrame(geom.Size) {
	if r.inFrame {
		panic("gift/backend/ebiten: BeginFrame without a matching EndFrame")
	}
	r.inFrame = true
	r.verts = r.verts[:0]
	r.idx = r.idx[:0]
	r.curMat, r.curPage = MaterialNone, nil
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
	for _, op := range l.Ops() {
		r.appendOp(l, op)
		if len(r.verts) >= maxBatchVertices {
			r.flush()
		}
	}
}

// EndFrame implements [render.Backend].
func (r *Renderer) EndFrame() {
	if !r.inFrame {
		panic("gift/backend/ebiten: EndFrame without a matching BeginFrame")
	}
	r.flush()
	r.inFrame = false
	r.dst = nil
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
		sigma = op.Blur * 0.5
		if !(sigma > 0) {
			sigma = 0
		}
	case render.OpGlyphs:
		r.appendGlyphs(l, op)
		return
	case render.OpImage:
		r.appendImage(l, op)
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
		// does need room for the falloff. The shader measures in device
		// pixels, so the device pad is ShadowSigmas*sigma*sr and the local pad
		// is that divided by the scale of the axis.
		dev := render.ShadowSigmas * sigma * sr
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
	fast := xf.B == 0 && xf.C == 0 && xf.A > 0 && xf.D > 0
	drawn := false
	for i := range gs {
		g := &gs[i]
		ei, ok := r.atlas.Lookup(*g)
		if !ok {
			continue
		}
		e := r.atlas.Entry(ei)
		if !e.inked {
			// A space. It occupies advance, not pixels.
			continue
		}
		r.material(MaterialGlyph, r.atlas.Page(ei))
		dst := geom.Rc(
			g.X+float32(e.left), g.Y+float32(e.top),
			g.X+float32(e.left+e.w), g.Y+float32(e.top+e.h))
		src := geom.Rc(float32(e.x), float32(e.y), float32(e.x+e.w), float32(e.y+e.h))
		var wrote bool
		if fast {
			wrote = r.appendTexturedQuad(dst, src, clip, xf, op.Color)
		} else {
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
// # For glyphs, the scale is assumed to be one
//
// The destination rectangle is the atlas rectangle mapped through xf, so a
// scale other than one stretches a bitmap that was rasterised at the glyph's
// nominal size. Under a nearest filter that is not a smooth resample: it
// duplicates and drops rows of coverage, and the text comes out the wrong
// weight. The correct answer is to rasterise at the *effective* size, which
// means folding the device scale into the atlas key — a change to
// [glyphKey] and to what internal/text is asked for, not to this function.
//
// Nothing in gift produces such a transform today. A scroll container pushes a
// pure translation, which leaves the mapping one to one; see the package
// documentation, "Clipping and transforms". This is recorded here so that the
// first thing to push a scale finds the note rather than the artefact.
//
// None of that applies to an *image*, which is resampled on purpose: a
// thumbnail comes off a ladder of a few sizes and is drawn at whatever the
// tile rectangle happens to be, so the image material uses a linear filter
// while the glyph material uses a nearest one. That is the only difference
// between the two and it lives in the draw options, not here.
func (r *Renderer) appendTexturedQuad(dst, src, clip geom.Rect, xf geom.Affine2D, col render.Color) bool {
	dev := geom.Rc(
		xf.A*dst.Min.X+xf.TX, xf.D*dst.Min.Y+xf.TY,
		xf.A*dst.Max.X+xf.TX, xf.D*dst.Max.Y+xf.TY)
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
	return RendererStats{
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
	}
}
