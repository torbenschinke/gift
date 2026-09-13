package ebiten

import (
	_ "embed"
	"fmt"

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
// bounds, in local units.
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
	drawFn func(verts []eb.Vertex, idx []uint32)

	// scratch polygons of the general clipping path. Two buffers of eight
	// vertices are enough: clipping a convex quad against four half planes
	// adds at most one vertex per plane.
	poly [2][8]clipVertex

	size     geom.Size
	inFrame  bool
	drawn    uint64
	batches  uint64
	skipped  uint64
	emitted  uint64
	unknowns uint64
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
	r := &Renderer{shader: sh}
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
func (r *Renderer) BeginFrame(size geom.Size) {
	if r.inFrame {
		panic("gift/backend/ebiten: BeginFrame without a matching EndFrame")
	}
	r.inFrame = true
	r.size = size
	r.verts = r.verts[:0]
	r.idx = r.idx[:0]
}

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
}

// appendOp translates one operation into vertices.
func (r *Renderer) appendOp(l *render.List, op render.Op) {
	var radius, stroke float32
	switch op.Kind {
	case render.OpNone:
		return
	case render.OpFillRect:
		// radius and stroke stay zero.
	case render.OpFillRoundRect:
		radius = op.CornerRadius
	case render.OpStrokeRoundRect:
		radius = op.CornerRadius
		stroke = op.StrokeWidth
		if stroke <= 0 {
			r.skipped++
			return
		}
	default:
		// An unknown kind is skipped rather than fatal, as [render.OpKind]
		// documents: a newer gift with an older backend must still run.
		r.unknowns++
		return
	}

	if op.Color.IsTransparent() {
		r.skipped++
		return
	}
	b := op.Bounds
	if b.IsEmpty() {
		r.skipped++
		return
	}
	clip := l.Clip(op.Clip)
	if clip.IsEmpty() {
		r.skipped++
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

	pad := float32(0)
	if radius > 0 || stroke > 0 {
		pad = aaPad
	}
	quad := geom.Rect{
		Min: geom.Point{X: b.Min.X - pad, Y: b.Min.Y - pad},
		Max: geom.Point{X: b.Max.X + pad, Y: b.Max.Y + pad},
	}

	xf := l.Xform(op.Xform)
	shape := shapeParams{
		color:  op.Color,
		halfW:  halfW,
		halfH:  halfH,
		radius: radius,
		stroke: stroke,
		// The local origin is the unpadded bounds, because that is the
		// coordinate system the distance field is defined in.
		originX: b.Min.X,
		originY: b.Min.Y,
	}

	if xf.B == 0 && xf.C == 0 && xf.A != 0 && xf.D != 0 {
		r.appendAxisAligned(quad, clip, xf, shape)
		return
	}
	r.appendTransformed(quad, clip, xf, shape)
}

// shapeParams are the per operation values that end up in the vertex
// attributes.
type shapeParams struct {
	color            render.Color
	halfW, halfH     float32
	radius, stroke   float32
	originX, originY float32
}

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
		r.skipped++
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
		r.skipped++
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
// The colour is copied straight through: [render.Color] is premultiplied and
// so is everything Ebitengine consumes, so there is no conversion here and in
// particular none in the frame path. See the project plan, section 8.
func vertex(dx, dy, lx, ly float32, sh shapeParams) eb.Vertex {
	return eb.Vertex{
		DstX:    dx,
		DstY:    dy,
		SrcX:    lx,
		SrcY:    ly,
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
		return
	}
	if r.drawFn != nil {
		r.drawFn(r.verts, r.idx)
	} else if r.dst != nil {
		r.dst.DrawTrianglesShader32(r.verts, r.idx, r.shader, &r.opts)
	}
	r.batches++
	r.verts = r.verts[:0]
	r.idx = r.idx[:0]
}

// RendererStats are the counters of the renderer. Like [gift.Diagnostics]
// they are plain numbers written in the frame path and read out of band.
type RendererStats struct {
	// Frames is the number of completed frames.
	Frames uint64
	// Batches is the number of draw calls issued.
	Batches uint64
	// Ops is the number of operations that produced geometry.
	Ops uint64
	// Skipped is the number of operations dropped as invisible: fully
	// transparent, empty, or entirely outside their clip.
	Skipped uint64
	// UnknownKinds is the number of operations whose kind this backend does
	// not know.
	UnknownKinds uint64
}

// Stats returns the renderer counters. It is not synchronised and belongs to
// the UI executor; the frame timings, which another goroutine may read, are in
// [FrameTimer] instead.
func (r *Renderer) Stats() RendererStats {
	return RendererStats{
		Frames:       r.drawn,
		Batches:      r.batches,
		Ops:          r.emitted,
		Skipped:      r.skipped,
		UnknownKinds: r.unknowns,
	}
}
