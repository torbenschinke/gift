package geom

// Rect is an axis aligned rectangle in view space.
//
// It is stored as a minimum and a maximum corner rather than as origin plus
// size, like image.Rectangle, because Intersect, Union, Contains and Overlaps
// are cheaper and free of rounding drift in that representation.
//
// Max is exclusive: a Rect covers the half open interval [Min.X, Max.X) in x and
// [Min.Y, Max.Y) in y. A Rect whose Max is not greater than its Min in either
// axis is empty and covers nothing.
type Rect struct {
	Min, Max Point
}

// Rc returns the Rect spanning from minX, minY to maxX, maxY. The corners are
// used as given; call Canon if they may be swapped.
func Rc(minX, minY, maxX, maxY float32) Rect {
	return Rect{Min: Point{X: minX, Y: minY}, Max: Point{X: maxX, Y: maxY}}
}

// RcXYWH returns the Rect with origin x, y and extent w, h.
func RcXYWH(x, y, w, h float32) Rect {
	return Rect{Min: Point{X: x, Y: y}, Max: Point{X: x + w, Y: y + h}}
}

// RcSize returns the Rect with the given origin and size.
func RcSize(origin Point, s Size) Rect {
	return Rect{Min: origin, Max: Point{X: origin.X + s.W, Y: origin.Y + s.H}}
}

// Origin returns the minimum corner of r.
func (r Rect) Origin() Point {
	return r.Min
}

// Size returns the extent of r. For a non canonical Rect the result may be
// negative.
func (r Rect) Size() Size {
	return Size{W: r.Max.X - r.Min.X, H: r.Max.Y - r.Min.Y}
}

// Width returns the horizontal extent of r.
func (r Rect) Width() float32 {
	return r.Max.X - r.Min.X
}

// Height returns the vertical extent of r.
func (r Rect) Height() float32 {
	return r.Max.Y - r.Min.Y
}

// IsEmpty reports whether r covers no area, that is whether its width or its
// height is not strictly positive.
func (r Rect) IsEmpty() bool {
	return r.Width() <= 0 || r.Height() <= 0
}

// Contains reports whether p lies inside r. Because Max is exclusive, a point on
// the minimum edge is contained while a point on the maximum edge is not. An
// empty Rect contains nothing.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.Min.X && p.X < r.Max.X && p.Y >= r.Min.Y && p.Y < r.Max.Y
}

// Intersect returns the rectangle covered by both r and s.
//
// If the two rectangles do not overlap, or if either of them is empty, the
// result is the canonical empty rectangle, the zero Rect. Callers therefore must
// not read Min or Max of the result without checking IsEmpty first.
func (r Rect) Intersect(s Rect) Rect {
	out := Rect{
		Min: Point{X: maxf(r.Min.X, s.Min.X), Y: maxf(r.Min.Y, s.Min.Y)},
		Max: Point{X: minf(r.Max.X, s.Max.X), Y: minf(r.Max.Y, s.Max.Y)},
	}
	if out.IsEmpty() {
		return Rect{}
	}
	return out
}

// Overlaps reports whether r and s share at least one point of positive area.
// An empty rectangle overlaps nothing.
func (r Rect) Overlaps(s Rect) bool {
	if r.IsEmpty() || s.IsEmpty() {
		return false
	}
	return r.Min.X < s.Max.X && s.Min.X < r.Max.X &&
		r.Min.Y < s.Max.Y && s.Min.Y < r.Max.Y
}

// Union returns the smallest rectangle containing both r and s.
//
// Empty operands are ignored: if exactly one of the two is empty the other one
// is returned unchanged, and if both are empty the zero Rect is returned. Any
// other rule would let a zero Rect drag the result towards the origin.
func (r Rect) Union(s Rect) Rect {
	if r.IsEmpty() {
		if s.IsEmpty() {
			return Rect{}
		}
		return s
	}
	if s.IsEmpty() {
		return r
	}
	return Rect{
		Min: Point{X: minf(r.Min.X, s.Min.X), Y: minf(r.Min.Y, s.Min.Y)},
		Max: Point{X: maxf(r.Max.X, s.Max.X), Y: maxf(r.Max.Y, s.Max.Y)},
	}
}

// Translate returns r moved by d.
func (r Rect) Translate(d Point) Rect {
	return Rect{Min: r.Min.Add(d), Max: r.Max.Add(d)}
}

// Inset returns r shrunk by i on each edge.
//
// The result is never inverted. If the insets consume more than the available
// extent along an axis, that axis collapses to zero extent at the midpoint of
// the over inset interval, that is at (Min+Left + Max-Right)/2 for x. The
// midpoint rule is chosen over clamping to Min because it keeps the collapsed
// rectangle centred inside its parent, which is what a padded child that ran out
// of room should look like.
func (r Rect) Inset(i Insets) Rect {
	minX, maxX := r.Min.X+i.Left, r.Max.X-i.Right
	if minX > maxX {
		mid := (minX + maxX) * 0.5
		minX, maxX = mid, mid
	}
	minY, maxY := r.Min.Y+i.Top, r.Max.Y-i.Bottom
	if minY > maxY {
		mid := (minY + maxY) * 0.5
		minY, maxY = mid, mid
	}
	return Rect{Min: Point{X: minX, Y: minY}, Max: Point{X: maxX, Y: maxY}}
}

// Outset returns r grown by i on each edge. It is Inset with negated insets and
// therefore needs no clamping.
func (r Rect) Outset(i Insets) Rect {
	return Rect{
		Min: Point{X: r.Min.X - i.Left, Y: r.Min.Y - i.Top},
		Max: Point{X: r.Max.X + i.Right, Y: r.Max.Y + i.Bottom},
	}
}

// Canon returns r with its corners normalised so that Min is component wise less
// than or equal to Max. A canonical Rect is returned unchanged.
func (r Rect) Canon() Rect {
	if r.Min.X > r.Max.X {
		r.Min.X, r.Max.X = r.Max.X, r.Min.X
	}
	if r.Min.Y > r.Max.Y {
		r.Min.Y, r.Max.Y = r.Max.Y, r.Min.Y
	}
	return r
}
