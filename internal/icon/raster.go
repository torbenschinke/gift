package icon

import (
	"image"
	"math"

	"golang.org/x/image/vector"
)

// Mask is one rasterised icon: an eight bit coverage bitmap of Size by Size
// pixels.
//
// Coverage, not colour — the same principle internal/text records for glyphs
// and for the same payoff: the mask says how much of each pixel the icon
// covers and nothing about what colour it is drawn in, so one cached mask
// serves every colour the icon is ever tinted with.
//
// The bitmap is square because the shapes it comes from are: every icon in the
// corpus declares a square viewBox, and a non square one would make the
// mapping from the logical size of a view to the mask ambiguous. [Rasterizer]
// refuses anything else.
type Mask struct {
	// Size is the edge length in whole device pixels, zero for an icon with
	// no ink.
	Size int
	// Pix holds Size*Size coverage bytes, row major, 0 for none and 255 for
	// full.
	//
	// The slice is owned by the caller and reused across calls, exactly like
	// [text.GlyphMask.Pix]: rasterising a screenful of icons through one Mask
	// asks the allocator for a buffer a handful of times and never again.
	Pix []byte
}

// MaxIconExtent bounds the mask a single icon may produce, per axis, in
// pixels.
//
// It is the same kind of sanity bound internal/text puts on a glyph and it is
// reached the same way: a size arriving from a layout that went wrong becomes
// a request for a bitmap of several gigabytes inside a frame. 1024 is four
// times the largest icon anybody draws and one atlas page of the backend.
const MaxIconExtent = 1024

// Rasterizer turns encoded icons into [Mask] coverage bitmaps.
//
// It carries scratch buffers and is not safe for concurrent use; one per
// process, owned by the UI executor, is the intended number. Everything it
// does is on the cache miss path — see ui.Icon — and nothing on the hit path
// comes here.
type Rasterizer struct {
	z   *vector.Rasterizer
	img *image.Alpha

	// erase is the second coverage buffer that [PaintErase] figures are drawn
	// into. It is allocated lazily, because one icon in the whole surveyed
	// corpus needs one. erasing says whether the icon being rasterised has
	// touched it.
	erase    *vector.Rasterizer
	eraseImg *image.Alpha
	erasePix []byte
	erasing  bool

	// pts is the reused flattening buffer of the stroker: the current subpath
	// as a polyline in device pixels.
	pts []point
	// closed says whether the subpath in pts was closed by the encoding.
	closed bool

	// state of the walk in progress
	scale  float32
	size   int
	tol    float32
	paint  Paint
	half   float32
	cap    Cap
	join   Join
	failed bool
}

type point struct{ x, y float32 }

// NewRasterizer returns a ready to use Rasterizer.
func NewRasterizer() *Rasterizer {
	return &Rasterizer{z: vector.NewRasterizer(0, 0), img: &image.Alpha{}}
}

// flatnessTolerance is the largest distance, in device pixels, that a
// flattened polyline may deviate from the curve it replaces.
//
// A quarter of a pixel. The rasteriser antialiases with 256 coverage levels
// over one pixel, so a deviation of a quarter of a pixel can move the coverage
// of a boundary pixel by at most a quarter of its range and can never move an
// edge to a different pixel. Going finer costs segments — and therefore cache
// miss time — for a difference the eight bit mask cannot represent.
const flatnessTolerance = 0.25

// Rasterize draws the encoded icon into dst at size device pixels per edge and
// reports whether there is any ink.
//
// box is the edge length of the icon's viewBox in its own units, 24 for the
// Flowbite corpus; size is what the icon is being drawn at *after* the device
// density has been applied, which is what makes an icon at 2x a finer raster
// rather than a magnified one. See ui.iconService.
//
// It returns false, with dst.Size zero and dst.Pix's capacity kept, for an
// empty icon, for a malformed blob and for a size outside [MaxIconExtent].
// A caller that gets false records "no mask" for the key and draws nothing;
// that is a bounded outcome and never an error.
func (r *Rasterizer) Rasterize(data []byte, box float32, size int, dst *Mask) bool {
	dst.Size = 0
	if len(data) == 0 || size <= 0 || size > MaxIconExtent || !(box > 0) || !isFinite(box) {
		return false
	}
	r.scale = float32(size) / box
	r.size = size
	r.tol = flatnessTolerance
	r.paint = PaintFill
	r.failed = false
	r.pts = r.pts[:0]
	r.erasing = false

	r.z.Reset(size, size)
	if err := Walk(data, r); err != nil || r.failed {
		return false
	}
	r.flushStroke()

	n := size * size
	if cap(dst.Pix) < n {
		dst.Pix = make([]byte, n)
	}
	dst.Pix = dst.Pix[:n]
	clear(dst.Pix)
	r.img.Pix, r.img.Stride, r.img.Rect = dst.Pix, size, image.Rect(0, 0, size, size)
	// image.Opaque as the source into an *image.Alpha is the combination
	// x/image/vector special cases into a direct coverage write, so this
	// neither allocates nor blends. internal/text relies on the same one.
	r.z.Draw(r.img, r.img.Rect, image.Opaque, image.Point{})
	r.img.Pix = nil

	if r.erasing {
		r.eraseImg.Pix, r.eraseImg.Stride, r.eraseImg.Rect = r.erasePix, size, image.Rect(0, 0, size, size)
		r.erase.Draw(r.eraseImg, r.eraseImg.Rect, image.Opaque, image.Point{})
		r.eraseImg.Pix = nil
		// dst = dst * (1 - erase), in eight bit. The knockout is multiplied
		// rather than subtracted so that a partially covered edge of the
		// erasing shape leaves a partially covered edge behind instead of a
		// hard one; see [PaintErase].
		for i, e := range r.erasePix {
			if e != 0 {
				dst.Pix[i] = uint8((int(dst.Pix[i])*(255-int(e)) + 127) / 255)
			}
		}
	}

	dst.Size = size
	for _, c := range dst.Pix {
		if c != 0 {
			return true
		}
	}
	dst.Size = 0
	return false
}

// --- the Sink implementation -------------------------------------------------

// Figure implements [Sink]. It ends the stroke in progress, if any, and
// selects where the next figure's geometry goes.
func (r *Rasterizer) Figure(p Paint, w float32, c Cap, j Join) {
	r.flushStroke()
	r.paint = p
	r.half = w * r.scale * 0.5
	r.cap, r.join = c, j
	if p == PaintErase && !r.erasing {
		r.beginErase()
	}
}

func (r *Rasterizer) beginErase() {
	if r.erase == nil {
		r.erase, r.eraseImg = vector.NewRasterizer(0, 0), &image.Alpha{}
	}
	n := r.size * r.size
	if cap(r.erasePix) < n {
		r.erasePix = make([]byte, n)
	}
	r.erasePix = r.erasePix[:n]
	clear(r.erasePix)
	r.erase.Reset(r.size, r.size)
	r.erasing = true
}

// target is the rasteriser the figure in progress writes into.
func (r *Rasterizer) target() *vector.Rasterizer {
	if r.paint == PaintErase {
		return r.erase
	}
	return r.z
}

// MoveTo implements [Sink].
func (r *Rasterizer) MoveTo(x, y float32) {
	x, y = r.dev(x, y)
	if r.paint == PaintStroke {
		r.flushSubpath()
		r.pts = append(r.pts, point{x, y})
		return
	}
	r.target().MoveTo(x, y)
}

// LineTo implements [Sink].
func (r *Rasterizer) LineTo(x, y float32) {
	x, y = r.dev(x, y)
	if r.paint == PaintStroke {
		r.pts = append(r.pts, point{x, y})
		return
	}
	r.target().LineTo(x, y)
}

// CubeTo implements [Sink].
func (r *Rasterizer) CubeTo(c1x, c1y, c2x, c2y, x, y float32) {
	c1x, c1y = r.dev(c1x, c1y)
	c2x, c2y = r.dev(c2x, c2y)
	x, y = r.dev(x, y)
	if r.paint == PaintStroke {
		r.flattenCube(c1x, c1y, c2x, c2y, x, y)
		return
	}
	r.target().CubeTo(c1x, c1y, c2x, c2y, x, y)
}

// Close implements [Sink].
func (r *Rasterizer) Close() {
	if r.paint == PaintStroke {
		r.closed = true
		r.flushSubpath()
		return
	}
	r.target().ClosePath()
}

func (r *Rasterizer) dev(x, y float32) (float32, float32) {
	x, y = x*r.scale, y*r.scale
	if !isFinite(x) || !isFinite(y) {
		r.failed = true
		return 0, 0
	}
	return x, y
}

// flushStroke finishes the stroked figure in progress.
func (r *Rasterizer) flushStroke() {
	if r.paint == PaintStroke {
		r.flushSubpath()
	}
}

// flattenCube appends a cubic to the polyline buffer.
//
// The segment count comes from the standard bound on the error of uniform
// subdivision of a cubic: it is at most (3/(4n^2)) times the larger of the two
// second differences of the control polygon. Solving that for the tolerance
// gives the n below. It is cheap, it never under-subdivides, and it costs a
// few segments too many only on curves that are nearly straight, where a
// segment is nearly free.
func (r *Rasterizer) flattenCube(c1x, c1y, c2x, c2y, x, y float32) {
	if len(r.pts) == 0 {
		r.failed = true
		return
	}
	p0 := r.pts[len(r.pts)-1]
	ax, ay := p0.x-2*c1x+c2x, p0.y-2*c1y+c2y
	bx, by := c1x-2*c2x+x, c1y-2*c2y+y
	d := maxf(ax*ax+ay*ay, bx*bx+by*by)
	n := 1
	if d > 0 {
		n = int(math.Ceil(math.Sqrt(0.75 * math.Sqrt(float64(d)) / flatnessTolerance)))
	}
	n = min(max(n, 1), 128)
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n)
		u := 1 - t
		a, b, c, dd := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		r.pts = append(r.pts, point{
			a*p0.x + b*c1x + c*c2x + dd*x,
			a*p0.y + b*c1y + c*c2y + dd*y,
		})
	}
}

// --- stroking ----------------------------------------------------------------

// flushSubpath converts the polyline in r.pts into filled outlines.
//
// # Why stamping, and why it is correct
//
// golang.org/x/image/vector fills; it cannot stroke. The outline of a stroke
// is produced here instead, and it is produced as a *union of convex stamps*
// rather than as a single offset outline: one quad per segment, one join shape
// per interior vertex and one cap per free end.
//
// The union is free. Every stamp is emitted as a closed contour with the same
// orientation, so at any point the nonzero winding number is the count of
// stamps covering it — at least one inside the stroke and exactly zero outside
// it. Nonzero filling of the lot is therefore exactly their union, with no
// self intersection artefact and no need to compute one. An offset outline,
// by contrast, self intersects wherever the path turns tighter than half the
// stroke width, and resolving that is a boolean operation this package would
// have to own.
//
// The price is stated rather than hidden. A stamp overlaps its neighbours, so
// this cannot stroke with a transparent colour: the coverage would add up
// along the seams. It does not have to. The mask goes to the GPU as coverage
// and the *tint* carries the alpha, so a half transparent icon is a fully
// opaque mask multiplied by a half transparent colour, and the seams never
// exist.
//
// # What the corpus made easy
//
// 261 of the corpus's stroked paths ask for round caps and 257 for round
// joins, and a round join is a disc: the cheapest stamp there is. Miter is
// still implemented, because it is the SVG *default* and 29 paths in the
// corpus simply do not mention a join and therefore ask for it.
func (r *Rasterizer) flushSubpath() {
	pts, closed := r.pts, r.closed
	r.pts, r.closed = r.pts[:0], false
	if r.half <= 0 || len(pts) == 0 {
		return
	}
	// Drop repeated points: a zero length segment has no direction and would
	// produce a NaN normal.
	k := 0
	for i := 1; i < len(pts); i++ {
		if dist2(pts[i], pts[k]) > 1e-12 {
			k++
			pts[k] = pts[i]
		}
	}
	pts = pts[:k+1]
	if closed && len(pts) > 1 && dist2(pts[0], pts[len(pts)-1]) <= 1e-12 {
		pts = pts[:len(pts)-1]
	}

	if len(pts) == 1 {
		// A degenerate subpath. SVG paints a dot for a round or square cap
		// and nothing for a butt one, and the corpus contains such subpaths
		// where a dot is genuinely the artwork.
		switch r.cap {
		case CapRound:
			r.disc(pts[0])
		case CapSquare:
			r.quad(
				point{pts[0].x - r.half, pts[0].y - r.half}, point{pts[0].x + r.half, pts[0].y - r.half},
				point{pts[0].x + r.half, pts[0].y + r.half}, point{pts[0].x - r.half, pts[0].y + r.half})
		}
		return
	}

	n := len(pts)
	segs := n - 1
	if closed {
		segs = n
	}
	for i := range segs {
		a, b := pts[i], pts[(i+1)%n]
		nx, ny := normal(a, b, r.half)
		r.quad(
			point{a.x + nx, a.y + ny}, point{b.x + nx, b.y + ny},
			point{b.x - nx, b.y - ny}, point{a.x - nx, a.y - ny})
	}
	// Joins: every vertex that has a segment on both sides. A closed subpath
	// has one at every vertex, including the one the closing segment returns
	// to; an open one has none at either end, where a cap goes instead.
	lo, hi := 1, n-2
	if closed {
		lo, hi = 0, n-1
	}
	for i := lo; i <= hi; i++ {
		r.joinAt(pts[(i-1+n)%n], pts[i], pts[(i+1)%n])
	}
	if !closed {
		r.capAt(pts[1], pts[0])
		r.capAt(pts[n-2], pts[n-1])
	}
}

// joinAt stamps the join shape at cur, between the segments prev-cur and
// cur-next.
func (r *Rasterizer) joinAt(prev, cur, next point) {
	if r.join == JoinRound {
		r.disc(cur)
		return
	}
	d0x, d0y := unit(prev, cur)
	d1x, d1y := unit(cur, next)
	cross := d0x*d1y - d0y*d1x
	if cross == 0 {
		// Collinear: either straight through, where the two quads already
		// meet, or a reversal, where a bevel is a zero area line and SVG
		// draws nothing without a round cap.
		return
	}
	// Outer side of the turn. The normal used by [normal] is (-dy, dx); for a
	// left turn in a y-down space the outer corners are on the opposite side.
	s := float32(1)
	if cross > 0 {
		s = -1
	}
	n0 := point{-d0y * r.half * s, d0x * r.half * s}
	n1 := point{-d1y * r.half * s, d1x * r.half * s}
	a := point{cur.x + n0.x, cur.y + n0.y}
	b := point{cur.x + n1.x, cur.y + n1.y}
	if r.join == JoinMiter {
		// The miter point is where the two outer edges meet. mx, my is the
		// bisector of the two outer normals; its length relative to half the
		// stroke width is the miter ratio SVG limits.
		mx, my := n0.x+n1.x, n0.y+n1.y
		l := float32(math.Hypot(float64(mx), float64(my)))
		if l > 1e-6 {
			// cos of half the angle between the normals.
			c := l / (2 * r.half)
			if c > 1e-6 {
				ratio := 1 / c
				if ratio <= MiterLimit {
					m := point{cur.x + mx/l*r.half*ratio, cur.y + my/l*r.half*ratio}
					r.quad(cur, a, m, b)
					return
				}
			}
		}
	}
	r.tri(cur, a, b)
}

// capAt stamps the cap shape at end, for a segment arriving from from.
func (r *Rasterizer) capAt(from, end point) {
	switch r.cap {
	case CapRound:
		r.disc(end)
	case CapSquare:
		dx, dy := unit(from, end)
		nx, ny := -dy*r.half, dx*r.half
		ex, ey := end.x+dx*r.half, end.y+dy*r.half
		r.quad(
			point{end.x + nx, end.y + ny}, point{ex + nx, ey + ny},
			point{ex - nx, ey - ny}, point{end.x - nx, end.y - ny})
	}
}

// discSegments is how many straight segments approximate a disc of radius rad
// within [flatnessTolerance].
//
// The sagitta of a chord subtending an angle a on a circle of radius rad is
// rad*(1-cos(a/2)), so a = 2*acos(1 - tol/rad) is the largest angle that stays
// inside the tolerance. The result is clamped: a disc smaller than the
// tolerance still gets a triangle, and a very large one stops at 64 sides,
// which is under a tenth of a pixel of error at the largest icon this package
// will rasterise.
func discSegments(rad float32) int {
	if rad <= flatnessTolerance {
		return 3
	}
	a := 2 * math.Acos(1-flatnessTolerance/float64(rad))
	n := int(math.Ceil(2 * math.Pi / a))
	return min(max(n, 3), 64)
}

func (r *Rasterizer) disc(c point) {
	n := discSegments(r.half)
	z := r.target()
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		x := c.x + r.half*float32(math.Cos(a))
		y := c.y + r.half*float32(math.Sin(a))
		if i == 0 {
			z.MoveTo(x, y)
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
}

// quad emits a four sided stamp, reversed if necessary so that every stamp of
// a stroke winds the same way. See [Rasterizer.flushSubpath] for why that is
// the whole correctness argument.
func (r *Rasterizer) quad(a, b, c, d point) {
	if area4(a, b, c, d) < 0 {
		a, b, c, d = d, c, b, a
	}
	z := r.target()
	z.MoveTo(a.x, a.y)
	z.LineTo(b.x, b.y)
	z.LineTo(c.x, c.y)
	z.LineTo(d.x, d.y)
	z.ClosePath()
}

func (r *Rasterizer) tri(a, b, c point) {
	if area4(a, b, c, a) < 0 {
		a, c = c, a
	}
	z := r.target()
	z.MoveTo(a.x, a.y)
	z.LineTo(b.x, b.y)
	z.LineTo(c.x, c.y)
	z.ClosePath()
}

// area4 is twice the signed area of the polygon a, b, c, d.
func area4(a, b, c, d point) float32 {
	return (a.x*b.y - b.x*a.y) + (b.x*c.y - c.x*b.y) + (c.x*d.y - d.x*c.y) + (d.x*a.y - a.x*d.y)
}

// normal returns the left normal of a to b, scaled to half the stroke width.
func normal(a, b point, half float32) (float32, float32) {
	dx, dy := unit(a, b)
	return -dy * half, dx * half
}

func unit(a, b point) (float32, float32) {
	dx, dy := b.x-a.x, b.y-a.y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return 0, 0
	}
	return dx / l, dy / l
}

func dist2(a, b point) float32 {
	dx, dy := a.x-b.x, a.y-b.y
	return dx*dx + dy*dy
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func isFinite(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}
