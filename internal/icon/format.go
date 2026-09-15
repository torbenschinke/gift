// Package icon rasterises pre-parsed icon outlines into coverage masks.
//
// It is the icon counterpart of internal/text: a shape goes in, an eight bit
// coverage bitmap comes out, and nothing here knows about colour, textures or
// a display list. The project plan, section 21, fixes that split — an icon is
// drawn as an image tinted through OpImage.Color, so one cached mask serves
// every colour the icon is ever drawn in, exactly as one glyph mask does.
//
// # There is no SVG parser in this package
//
// Section 21 requires that no SVG parser reaches the frame path. Parsing
// happens once, offline, in cmd/gift-icongen, which writes the byte format
// defined in this file; internal/iconsvg holds the parser and is imported by
// the generator and by nothing else. The encoder lives here rather than next
// to the parser so that the two halves of the format cannot drift apart: a
// change to [Encoder] that [Decoder] does not follow is a compile error in
// this package's own tests rather than a silently misread icon.
package icon

import (
	"encoding/binary"
	"errors"
	"math"
)

// UnitScale is the fixed point denominator of a coordinate in the encoded
// form: a stored value of 128 is one viewBox unit.
//
// 1/128 of a unit was chosen against the source rather than by taste. The
// Flowbite corpus writes at most three decimals, so the worst rounding error
// is half a step, 1/256 of a unit. An icon occupies its viewBox — 24 units for
// this corpus — so at the largest size this framework can produce for it, 24
// logical pixels at [gift.MaxDensity], one unit is four pixels and the error
// is 1/64 of a pixel. That is the same quantum internal/text rounds glyph
// sizes to, and it is two orders of magnitude below the rasteriser's own
// antialiasing step.
//
// The other end matters as much: a signed 16 bit coordinate reaches ±256
// units at this scale, which is ten viewBoxes, so the generator can reject an
// out of range path instead of wrapping one.
const UnitScale = 128

// Paint is how one figure — the geometry of one SVG element — turns into
// coverage.
type Paint uint8

const (
	// PaintFill fills the figure with the nonzero winding rule.
	//
	// There is deliberately no even-odd paint. golang.org/x/image/vector is a
	// nonzero rasteriser and has no rule to choose, so an even-odd path is
	// resolved *at generation time* by orienting its contours so that the two
	// rules agree; see [internal/iconsvg.Orient]. Carrying the rule into the
	// frame path would mean either a second rasteriser or a run time
	// reorientation on every cache miss, and both are work that the offline
	// step can do once.
	PaintFill Paint = iota

	// PaintStroke strokes the figure and fills the resulting outline.
	//
	// The stroke stays declarative here rather than being converted to an
	// outline offline, because the outline of a curve can only be flattened
	// against a tolerance, and the tolerance depends on how many device
	// pixels the icon is being drawn into. A generator would have to pick one
	// size; [Rasterizer] knows the real one.
	PaintStroke

	// PaintErase fills the figure with the nonzero rule and *removes* that
	// coverage from the mask built so far.
	//
	// It exists for one path in the surveyed corpus and is named for what it
	// does rather than for where it came from. Flowbite's solid/visa.svg
	// draws a card in currentColor and then the lettering in a literal
	// #ffffff on top. A gift icon is monochrome by construction — the colour
	// is the tint of the draw operation and the mask holds coverage only — so
	// there is no second colour to put the lettering in. Treating a fill that
	// is not currentColor as a knockout reproduces what the artwork means:
	// the card, with the letters punched out of it, in whatever colour the
	// application tints it. See [internal/iconsvg.Parse], which is the only
	// place that decides a fill is one.
	PaintErase
)

// Cap is the shape drawn at the free end of an open stroked subpath.
type Cap uint8

const (
	// CapButt ends the stroke flat at the endpoint. It is the SVG default.
	CapButt Cap = iota
	// CapRound ends the stroke with a half disc centred on the endpoint.
	CapRound
	// CapSquare ends the stroke with a flat edge half a width beyond the
	// endpoint.
	CapSquare
)

// Join is the shape drawn where two segments of a stroked subpath meet.
type Join uint8

const (
	// JoinMiter extends both outer edges until they meet. It is the SVG
	// default, and unlike the other two it can be arbitrarily far from the
	// vertex, so it is subject to [MiterLimit].
	JoinMiter Join = iota
	// JoinRound fills the notch with a disc centred on the vertex.
	JoinRound
	// JoinBevel closes the notch with the straight line between the two outer
	// corners.
	JoinBevel
)

// MiterLimit is the ratio of miter length to stroke width beyond which a
// [JoinMiter] falls back to a [JoinBevel]. Four is the SVG default and no
// icon in the corpus overrides it.
const MiterLimit = 4

// Segment operators of the encoded form.
//
// There are four and there is no quadratic, which is a fact about the corpus
// and not a limitation: the surveyed Flowbite set contains no Q and no T
// command at all, and every arc — the single most common command in it — is
// converted to cubics offline. A quadratic arriving later is an encoder
// change, and the encoder would have to elevate it to a cubic, which is exact.
const (
	opMove uint8 = iota
	opLine
	opCube
	opClose
	numOps
)

// Errors returned by [Decode] and by the walkers in this package.
var (
	// ErrTruncated means the byte stream ended in the middle of a figure or a
	// segment. It is only reachable from a corrupt or hand written blob: the
	// generator cannot emit one.
	ErrTruncated = errors.New("gift/internal/icon: the encoded icon ends mid segment")
	// ErrBadOp means a segment or paint tag is not one this package defines.
	ErrBadOp = errors.New("gift/internal/icon: the encoded icon contains an unknown tag")
	// ErrNoStart means a segment that continues a subpath arrived before any
	// move opened one.
	ErrNoStart = errors.New("gift/internal/icon: the encoded icon continues a subpath that was never started")
)

// Encoder builds the encoded form of one icon.
//
// Coordinates are given in viewBox units and stored as [UnitScale]ths of one,
// delta coded against the previous stored point and written as a zigzag
// varint. The delta coding is what makes the format small: an icon is a walk
// over a 24 unit box in steps of a unit or two, so almost every coordinate
// fits in one byte, whereas an absolute int16 would spend two on all of them.
//
// The zero Encoder is ready to use.
type Encoder struct {
	buf []byte
	// pen is the last stored point in fixed point units, which is what the
	// deltas are relative to.
	penX, penY int32
	// open records whether a figure is currently being written, so that
	// [Encoder.Figure] can patch the segment count of the previous one.
	open bool
	// countAt is the offset of the segment count of the figure in progress,
	// and n is the number written so far.
	countAt int
	n       int
	// err latches the first out of range coordinate. An encoder that has
	// failed produces no bytes at all rather than a shorter icon.
	err error
}

// Figure starts a new fill or erase figure.
func (e *Encoder) Figure(p Paint) {
	if p == PaintStroke {
		panic("gift/internal/icon: Figure with PaintStroke; use StrokeFigure")
	}
	e.begin(p, nil)
}

// StrokeFigure starts a new stroke figure of the given width in viewBox units.
func (e *Encoder) StrokeFigure(width float32, c Cap, j Join) {
	e.begin(PaintStroke, func() {
		w := e.fixed(width)
		if w < 0 {
			w = 0
		}
		e.buf = binary.AppendUvarint(e.buf, uint64(w))
		e.buf = append(e.buf, byte(c), byte(j))
	})
}

// begin writes the paint tag, then whatever extra the paint carries, and then
// reserves the segment count. The order is the order [Walk] reads them in, and
// extra is a callback rather than three optional arguments so that the two
// cannot be put in the wrong order by a later edit.
func (e *Encoder) begin(p Paint, extra func()) {
	e.close()
	e.buf = append(e.buf, byte(p))
	if extra != nil {
		extra()
	}
	e.open = true
	e.penX, e.penY = 0, 0
	// The segment count is patched in [Encoder.close]. Three bytes of uvarint
	// space are reserved, which covers 2 097 151 segments; the largest figure
	// in the corpus has 88.
	e.countAt = len(e.buf)
	e.buf = append(e.buf, 0, 0, 0)
	e.n = 0
}

// close patches the reserved segment count of the figure in progress.
func (e *Encoder) close() {
	if !e.open {
		return
	}
	e.open = false
	var tmp [3]byte
	n := binary.PutUvarint(tmp[:], uint64(e.n))
	// PutUvarint writes the shortest form and the count has to occupy exactly
	// the three reserved bytes, so it is padded out with empty high groups: a
	// continuation bit on the first two bytes and zeroes above the written
	// ones. 5 becomes 0x85 0x80 0x00, which encoding/binary decodes back to 5
	// — it accepts a non minimal uvarint and only rejects an overflowing one.
	for i := n; i < 3; i++ {
		tmp[i] = 0
	}
	tmp[0] |= 0x80
	tmp[1] |= 0x80
	copy(e.buf[e.countAt:e.countAt+3], tmp[:])
}

// MoveTo opens a subpath at x, y.
func (e *Encoder) MoveTo(x, y float32) { e.seg(opMove, x, y) }

// LineTo appends a straight segment.
func (e *Encoder) LineTo(x, y float32) { e.seg(opLine, x, y) }

// CubeTo appends a cubic Bezier through the two control points.
func (e *Encoder) CubeTo(c1x, c1y, c2x, c2y, x, y float32) {
	e.seg(opCube, c1x, c1y, c2x, c2y, x, y)
}

// Close closes the current subpath back to its opening point.
func (e *Encoder) Close() { e.seg(opClose) }

func (e *Encoder) seg(op uint8, xy ...float32) {
	if !e.open {
		panic("gift/internal/icon: a segment before any Figure")
	}
	e.buf = append(e.buf, op)
	for i := 0; i+1 < len(xy); i += 2 {
		vx, vy := e.fixed(xy[i]), e.fixed(xy[i+1])
		e.buf = binary.AppendVarint(e.buf, int64(vx-e.penX))
		e.buf = binary.AppendVarint(e.buf, int64(vy-e.penY))
		e.penX, e.penY = vx, vy
	}
	e.n++
}

// fixed converts a viewBox coordinate and latches an out of range one.
func (e *Encoder) fixed(v float32) int32 {
	q := math.Round(float64(v) * UnitScale)
	if math.IsNaN(q) || q < math.MinInt16 || q > math.MaxInt16 {
		if e.err == nil {
			e.err = errors.New("gift/internal/icon: coordinate out of range for the encoded form")
		}
		return 0
	}
	return int32(q)
}

// Bytes returns the encoded icon, or an error if any coordinate was out of
// range. The Encoder must not be used afterwards.
func (e *Encoder) Bytes() ([]byte, error) {
	e.close()
	if e.err != nil {
		return nil, e.err
	}
	return e.buf, nil
}

// Sink receives the decoded geometry of one icon. It is what [Walk] drives and
// what both the rasteriser and the generator's own verification implement, so
// that the two cannot disagree about what a blob means.
//
// Coordinates arrive in viewBox units.
type Sink interface {
	// Figure starts a figure. w, c and j are meaningful only for
	// [PaintStroke] and are zero otherwise.
	Figure(p Paint, w float32, c Cap, j Join)
	MoveTo(x, y float32)
	LineTo(x, y float32)
	CubeTo(c1x, c1y, c2x, c2y, x, y float32)
	Close()
}

// Walk decodes data and drives s.
//
// It allocates nothing: the decode is a walk over a byte slice with a handful
// of integers of state, and every coordinate is passed by value. That is what
// keeps a cache miss — which is where this runs, and the only place it runs —
// down to the rasterisation itself.
func Walk(data []byte, s Sink) error {
	const inv = 1.0 / float32(UnitScale)
	for len(data) > 0 {
		p := Paint(data[0])
		data = data[1:]
		var w float32
		var c Cap
		var j Join
		switch p {
		case PaintFill, PaintErase:
		case PaintStroke:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return ErrTruncated
			}
			data = data[n:]
			if len(data) < 2 {
				return ErrTruncated
			}
			w, c, j = float32(v)*inv, Cap(data[0]), Join(data[1])
			if c > CapSquare || j > JoinBevel {
				return ErrBadOp
			}
			data = data[2:]
		default:
			return ErrBadOp
		}
		count, n := binary.Uvarint(data)
		if n <= 0 {
			return ErrTruncated
		}
		data = data[n:]
		s.Figure(p, w, c, j)

		var penX, penY int32
		started := false
		for range count {
			if len(data) == 0 {
				return ErrTruncated
			}
			op := data[0]
			data = data[1:]
			if op >= numOps {
				return ErrBadOp
			}
			if op == opClose {
				if !started {
					return ErrNoStart
				}
				s.Close()
				continue
			}
			if op != opMove && !started {
				return ErrNoStart
			}
			var pts [3][2]float32
			k := 1
			if op == opCube {
				k = 3
			}
			for i := range k {
				dx, m := binary.Varint(data)
				if m <= 0 {
					return ErrTruncated
				}
				data = data[m:]
				dy, m := binary.Varint(data)
				if m <= 0 {
					return ErrTruncated
				}
				data = data[m:]
				penX, penY = penX+int32(dx), penY+int32(dy)
				pts[i][0], pts[i][1] = float32(penX)*inv, float32(penY)*inv
			}
			switch op {
			case opMove:
				s.MoveTo(pts[0][0], pts[0][1])
				started = true
			case opLine:
				s.LineTo(pts[0][0], pts[0][1])
			case opCube:
				s.CubeTo(pts[0][0], pts[0][1], pts[1][0], pts[1][1], pts[2][0], pts[2][1])
			}
		}
	}
	return nil
}
