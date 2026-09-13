package geom

import "math"

// Affine2D is a 2D affine transform stored as a 2x3 matrix:
//
//	| A C TX |
//	| B D TY |
//
// The implied third row is 0 0 1. Applying it to a point yields
// (A*x + C*y + TX, B*x + D*y + TY).
//
// The zero Affine2D is not a valid transform; use [Identity].
type Affine2D struct {
	A, B, C, D, TX, TY float32
}

// Identity returns the transform that leaves every point unchanged.
func Identity() Affine2D {
	return Affine2D{A: 1, B: 0, C: 0, D: 1, TX: 0, TY: 0}
}

// Translate returns the transform that moves every point by d.
func Translate(d Point) Affine2D {
	return Affine2D{A: 1, B: 0, C: 0, D: 1, TX: d.X, TY: d.Y}
}

// Scale returns the transform that scales x by sx and y by sy about the origin.
func Scale(sx, sy float32) Affine2D {
	return Affine2D{A: sx, B: 0, C: 0, D: sy, TX: 0, TY: 0}
}

// Rotate returns the transform that rotates about the origin by the given angle
// in radians. Because y grows downwards, a positive angle rotates clockwise on
// screen.
func Rotate(radians float32) Affine2D {
	s, c := math.Sincos(float64(radians))
	sin, cos := float32(s), float32(c)
	return Affine2D{A: cos, B: sin, C: -sin, D: cos, TX: 0, TY: 0}
}

// Mul returns the transform that applies m first and n afterwards.
//
// In other words, for every point p the identity
// m.Mul(n).Apply(p) == n.Apply(m.Apply(p)) holds up to floating point rounding.
// Read left to right as the chronological order of operations, so
// Translate(d).Mul(Scale(2, 2)) translates and then scales the translated
// result. In matrix notation the result is the product n*m.
//
// Mul is not commutative; m.Mul(n) and n.Mul(m) generally differ.
func (m Affine2D) Mul(n Affine2D) Affine2D {
	return Affine2D{
		A:  n.A*m.A + n.C*m.B,
		B:  n.B*m.A + n.D*m.B,
		C:  n.A*m.C + n.C*m.D,
		D:  n.B*m.C + n.D*m.D,
		TX: n.A*m.TX + n.C*m.TY + n.TX,
		TY: n.B*m.TX + n.D*m.TY + n.TY,
	}
}

// Apply returns p transformed by m.
func (m Affine2D) Apply(p Point) Point {
	return Point{
		X: m.A*p.X + m.C*p.Y + m.TX,
		Y: m.B*p.X + m.D*p.Y + m.TY,
	}
}

// Det returns the determinant of the linear part of m. It is zero exactly when m
// is singular and therefore not invertible.
func (m Affine2D) Det() float32 {
	return m.A*m.D - m.B*m.C
}

// Invert returns the inverse of m and true, or the zero Affine2D and false if m
// is singular. Callers must check the boolean; the returned transform is
// meaningless when it is false.
func (m Affine2D) Invert() (Affine2D, bool) {
	det := m.Det()
	if det == 0 || !isFinite(det) {
		return Affine2D{}, false
	}
	inv := 1 / det
	a := m.D * inv
	b := -m.B * inv
	c := -m.C * inv
	d := m.A * inv
	return Affine2D{
		A:  a,
		B:  b,
		C:  c,
		D:  d,
		TX: -(a*m.TX + c*m.TY),
		TY: -(b*m.TX + d*m.TY),
	}, true
}

// TransformRect returns the axis aligned bounding box of the four transformed
// corners of r.
//
// For a rotating or skewing transform this is a conservative hull and generally
// larger than the transformed shape. For a translation only transform it is
// exact and equal to r.Translate(Pt(m.TX, m.TY)).
//
// An empty r is mapped to the canonical empty rectangle, the zero Rect, so that
// emptiness is not accidentally turned into a degenerate box somewhere else.
func (m Affine2D) TransformRect(r Rect) Rect {
	if r.IsEmpty() {
		return Rect{}
	}
	if m.IsTranslationOnly() {
		return r.Translate(Point{X: m.TX, Y: m.TY})
	}
	p0 := m.Apply(Point{X: r.Min.X, Y: r.Min.Y})
	p1 := m.Apply(Point{X: r.Max.X, Y: r.Min.Y})
	p2 := m.Apply(Point{X: r.Min.X, Y: r.Max.Y})
	p3 := m.Apply(Point{X: r.Max.X, Y: r.Max.Y})
	return Rect{
		Min: Point{
			X: minf(minf(p0.X, p1.X), minf(p2.X, p3.X)),
			Y: minf(minf(p0.Y, p1.Y), minf(p2.Y, p3.Y)),
		},
		Max: Point{
			X: maxf(maxf(p0.X, p1.X), maxf(p2.X, p3.X)),
			Y: maxf(maxf(p0.Y, p1.Y), maxf(p2.Y, p3.Y)),
		},
	}
}

// IsIdentity reports whether m leaves every point unchanged. The comparison is
// exact, because this is a fast path predicate and not a similarity test.
func (m Affine2D) IsIdentity() bool {
	return m.A == 1 && m.B == 0 && m.C == 0 && m.D == 1 && m.TX == 0 && m.TY == 0
}

// IsTranslationOnly reports whether the linear part of m is the identity, so
// that m is a pure translation.
//
// This is the predicate behind the scroll fast path: a pure translation can be
// applied by patching offsets in the display list, without re-measuring, without
// re-shaping text and without a general matrix transform per draw operation.
func (m Affine2D) IsTranslationOnly() bool {
	return m.A == 1 && m.B == 0 && m.C == 0 && m.D == 1
}
