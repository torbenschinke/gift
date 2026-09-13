package geom

// Point is a position in view space.
//
// The zero Point is the origin. X grows to the right and Y grows downwards,
// matching the usual screen coordinate convention.
type Point struct {
	X, Y float32
}

// Pt returns the Point at x, y.
func Pt(x, y float32) Point {
	return Point{X: x, Y: y}
}

// Add returns the component wise sum of p and q.
func (p Point) Add(q Point) Point {
	return Point{X: p.X + q.X, Y: p.Y + q.Y}
}

// Sub returns the component wise difference p minus q.
func (p Point) Sub(q Point) Point {
	return Point{X: p.X - q.X, Y: p.Y - q.Y}
}

// Mul returns p with both components scaled by s.
func (p Point) Mul(s float32) Point {
	return Point{X: p.X * s, Y: p.Y * s}
}

// IsZero reports whether both components are exactly zero.
//
// The comparison is exact on purpose: callers use it as a fast path test, for
// example to skip a translation, and an epsilon would make that decision depend
// on an arbitrary tolerance.
func (p Point) IsZero() bool {
	return p.X == 0 && p.Y == 0
}
