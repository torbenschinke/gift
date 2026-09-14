package text

import (
	"image"
	"image/draw"
	"math"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

// GlyphMask is one rasterised glyph: an eight bit coverage bitmap and where it
// sits relative to the glyph origin.
//
// Coverage, not colour. The mask says how much of each pixel the glyph covers
// and nothing about what colour it is drawn in; the colour belongs to the
// drawing operation, which is what lets one atlas entry serve every colour the
// same string is ever drawn in. The project plan, section 7, puts the atlas in
// the backend, so this type is the handover shape and not a texture.
type GlyphMask struct {
	// Width and Height are the extent of the bitmap in whole pixels. Both are
	// zero for a glyph with no ink, for example a space.
	Width, Height int

	// Left is the horizontal offset from the glyph origin to the left edge of
	// the bitmap, positive to the right.
	//
	// Top is the vertical offset from the *baseline* to the top edge of the
	// bitmap, positive downwards. For an ordinary letter it is negative,
	// because the ink is above the baseline. A renderer draws the mask at
	//
	//	glyph.X + mask.Left, glyph.Y + mask.Top
	//
	// with glyph.X and glyph.Y the origin the shaper produced. Both are whole
	// numbers, so the destination rectangle is pixel aligned and no subpixel
	// phase can creep into the atlas key; see the project plan, section 7.
	Left, Top int

	// Pix holds Width*Height coverage bytes, row major, one byte per pixel,
	// 0 for no coverage and 255 for full coverage.
	//
	// The slice is owned by the caller and reused: [Rasterizer.Glyph] grows it
	// when it has to and reslices it otherwise, so rasterising a thousand
	// glyphs through the same GlyphMask asks the allocator for a buffer a
	// handful of times and never again.
	Pix []byte
}

// Rasterizer turns glyph outlines into [GlyphMask] coverage bitmaps.
//
// # Why this exists here and not in the backend
//
// The backend owns the glyph atlas and nothing else about text; the project
// plan, section 3, puts shaping and glyph keys in this package and forbids the
// backend from holding shaping logic. Rasterising an outline needs the
// typesetting face, and exporting that face would put a typesetting type in
// the signature of a public-ish boundary and let any caller shape with it
// behind this package's back. So the face stays unexported and this narrow
// accessor is what crosses the line: glyph id and size in, coverage bitmap
// out, no font object, no shaping, no GPU.
//
// A Rasterizer carries scratch buffers and is not safe for concurrent use,
// like everything else in this package. One per backend is the intended
// number.
type Rasterizer struct {
	z   *vector.Rasterizer
	img *image.Alpha
	// segs is the reused outline buffer. GlyphDataOutline returns a slice
	// that points into the font tables for glyf and into a fresh allocation
	// for CFF; copying into one buffer means the rest of this file never has
	// to care which.
	segs []font.Segment
}

// NewRasterizer returns a ready to use Rasterizer.
func NewRasterizer() *Rasterizer {
	return &Rasterizer{
		z:   vector.NewRasterizer(0, 0),
		img: &image.Alpha{},
	}
}

// maxGlyphExtent bounds the bitmap a single glyph may produce, in pixels per
// axis.
//
// It is a sanity bound, not a design limit. A corrupt or hostile font can
// claim a glyph whose control points are hundreds of ems away from the origin,
// and at a large size that becomes a request for a bitmap of several gigabytes
// inside a frame. Refusing to rasterise it is the same decision
// [ParseFont] makes about a font that panics the parser: outside world input
// becomes a value, not a process death.
const maxGlyphExtent = 4096

// Glyph rasterises glyph id of font f at size pixels per em into dst and
// reports whether there is any ink.
//
// It returns false, with dst zeroed apart from its Pix capacity, for a glyph
// the font does not describe, for a glyph with an empty outline such as a
// space, and for an outline whose extent exceeds [maxGlyphExtent]. A caller
// that gets false records "no mask" for the key and draws nothing; that is a
// normal outcome and happens for every space in every line of text.
//
// The size is quantised to 1/64 pixel exactly as shaping quantises it, so a
// glyph measured at one size can never be rasterised at a size that rounds
// differently.
//
// Glyph allocates when dst.Pix has to grow and otherwise reuses it. It is on
// the atlas miss path only; the atlas hit path never comes here.
func (r *Rasterizer) Glyph(f *Font, id GlyphID, size float32, dst *GlyphMask) bool {
	dst.Width, dst.Height, dst.Left, dst.Top = 0, 0, 0, 0
	if f == nil || !(size > 0) || !isFinite(size) {
		return false
	}
	out, ok := f.face.GlyphDataOutline(font.GID(id))
	if !ok || len(out.Segments) == 0 {
		return false
	}

	// Font units to pixels. The y axis of an outline grows upwards and every
	// raster target in gift grows downwards, so the scale is negated on y
	// once, here, rather than at each of the four places that read a point.
	scale := quantum(size) / f.upem
	r.segs = append(r.segs[:0], out.Segments...)

	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := -float32(math.MaxFloat32), -float32(math.MaxFloat32)
	for i := range r.segs {
		s := &r.segs[i]
		for _, p := range s.ArgsSlice() {
			x, y := p.X*scale, -p.Y*scale
			if !isFinite(x) || !isFinite(y) {
				return false
			}
			minX, maxX = minf(minX, x), maxf(maxX, x)
			minY, maxY = minf(minY, y), maxf(maxY, y)
		}
	}

	// The bounding box of the control points contains the curves, so flooring
	// and ceiling it is conservative. One extra pixel on each side absorbs the
	// coverage of an edge that lands exactly on a boundary.
	x0, y0 := int(math.Floor(float64(minX)))-1, int(math.Floor(float64(minY)))-1
	x1, y1 := int(math.Ceil(float64(maxX)))+1, int(math.Ceil(float64(maxY)))+1
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 || w > maxGlyphExtent || h > maxGlyphExtent {
		return false
	}

	ox, oy := -float32(x0), -float32(y0)
	r.z.Reset(w, h)
	for i := range r.segs {
		s := &r.segs[i]
		a := s.Args
		switch s.Op {
		case ot.SegmentOpMoveTo:
			r.z.MoveTo(a[0].X*scale+ox, -a[0].Y*scale+oy)
		case ot.SegmentOpLineTo:
			r.z.LineTo(a[0].X*scale+ox, -a[0].Y*scale+oy)
		case ot.SegmentOpQuadTo:
			r.z.QuadTo(a[0].X*scale+ox, -a[0].Y*scale+oy, a[1].X*scale+ox, -a[1].Y*scale+oy)
		case ot.SegmentOpCubeTo:
			r.z.CubeTo(
				a[0].X*scale+ox, -a[0].Y*scale+oy,
				a[1].X*scale+ox, -a[1].Y*scale+oy,
				a[2].X*scale+ox, -a[2].Y*scale+oy)
		}
	}

	n := w * h
	if cap(dst.Pix) < n {
		dst.Pix = make([]byte, n)
	}
	dst.Pix = dst.Pix[:n]
	clear(dst.Pix)

	r.img.Pix, r.img.Stride, r.img.Rect = dst.Pix, w, image.Rect(0, 0, w, h)
	// image.Opaque as the source and an *image.Alpha as the destination is the
	// combination x/image/vector special cases into a direct coverage write,
	// so this path neither allocates nor blends.
	r.z.Draw(r.img, r.img.Rect, image.Opaque, image.Point{})
	r.img.Pix = nil

	dst.Width, dst.Height = w, h
	dst.Left, dst.Top = x0, y0
	return true
}

// compile time assertion that *image.Alpha is a draw.Image, which is what the
// rasteriser's fast path keys on.
var _ draw.Image = (*image.Alpha)(nil)

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
