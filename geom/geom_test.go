// Package geom_test exercises the package through its exported API only.
//
// The tests deliberately live in the external test package geom_test rather than
// in geom itself. Everything the package promises is exported, so an internal
// test package would add no coverage but would allow the tests to silently
// depend on unexported helpers. Compiling against the public surface also proves
// that the API is usable as documented from the outside.
package geom_test

import (
	"math"
	"math/rand"
	"testing"

	"github.com/torbenschinke/gift/geom"
)

// eps is the relative tolerance for float comparisons. It is generous enough for
// accumulated float32 rounding in chained matrix products on values of the
// magnitude used here.
const eps = 1e-3

// approx reports whether a and b are equal within eps, scaled by the magnitude
// of the operands. Exactly equal values, including infinities, compare equal
// without any arithmetic, because Inf minus Inf would be NaN.
func approx(a, b float32) bool {
	if a == b {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	scale := float32(1)
	if av := abs32(a); av > scale {
		scale = av
	}
	if bv := abs32(b); bv > scale {
		scale = bv
	}
	return float64(d) <= eps*float64(scale)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func approxPt(a, b geom.Point) bool { return approx(a.X, b.X) && approx(a.Y, b.Y) }

func approxSz(a, b geom.Size) bool { return approx(a.W, b.W) && approx(a.H, b.H) }

func approxRect(a, b geom.Rect) bool { return approxPt(a.Min, b.Min) && approxPt(a.Max, b.Max) }

func approxAff(a, b geom.Affine2D) bool {
	return approx(a.A, b.A) && approx(a.B, b.B) && approx(a.C, b.C) &&
		approx(a.D, b.D) && approx(a.TX, b.TX) && approx(a.TY, b.TY)
}

func TestPoint(t *testing.T) {
	p := geom.Pt(3, 4)
	q := geom.Pt(-1, 2)

	if got, want := p.Add(q), geom.Pt(2, 6); got != want {
		t.Errorf("Add = %v, want %v", got, want)
	}
	if got, want := p.Sub(q), geom.Pt(4, 2); got != want {
		t.Errorf("Sub = %v, want %v", got, want)
	}
	if got, want := p.Mul(2), geom.Pt(6, 8); got != want {
		t.Errorf("Mul = %v, want %v", got, want)
	}
	if p.IsZero() {
		t.Error("Pt(3,4).IsZero() = true")
	}
	if !(geom.Point{}).IsZero() {
		t.Error("zero Point IsZero() = false")
	}
}

func TestSize(t *testing.T) {
	tests := []struct {
		name string
		got  geom.Size
		want geom.Size
	}{
		{"add", geom.Sz(10, 20).Add(geom.Sz(5, 1)), geom.Sz(15, 21)},
		{"sub", geom.Sz(10, 20).Sub(geom.Sz(5, 1)), geom.Sz(5, 19)},
		{"sub negative not clamped", geom.Sz(1, 1).Sub(geom.Sz(5, 5)), geom.Sz(-4, -4)},
		{"inset", geom.Sz(100, 50).Inset(geom.InsetsAll(10)), geom.Sz(80, 30)},
		{"inset clamps at zero", geom.Sz(10, 10).Inset(geom.InsetsAll(20)), geom.Sz(0, 0)},
		{"inset asymmetric", geom.Sz(100, 50).Inset(geom.InsetsSymmetric(5, 20)), geom.Sz(60, 40)},
		{"outset", geom.Sz(100, 50).Outset(geom.InsetsAll(10)), geom.Sz(120, 70)},
		{"inset unbounded stays unbounded", geom.Sz(geom.Unbounded, 10).Inset(geom.InsetsAll(4)), geom.Sz(geom.Unbounded, 2)},
		{"outset unbounded stays unbounded", geom.Sz(geom.Unbounded, 10).Outset(geom.InsetsAll(4)), geom.Sz(geom.Unbounded, 18)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !approxSz(tc.got, tc.want) {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}

	if !(geom.Size{}).IsZero() {
		t.Error("zero Size IsZero() = false")
	}
	if geom.Sz(1, 0).IsZero() {
		t.Error("Sz(1,0).IsZero() = true")
	}
	if !geom.Sz(1, 2).IsFinite() {
		t.Error("Sz(1,2).IsFinite() = false")
	}
	if geom.Sz(geom.Unbounded, 2).IsFinite() {
		t.Error("unbounded Size IsFinite() = true")
	}
	if geom.Sz(float32(math.NaN()), 2).IsFinite() {
		t.Error("NaN Size IsFinite() = true")
	}
}

func TestInsets(t *testing.T) {
	i := geom.InsetsAll(4)
	if i != (geom.Insets{Top: 4, Right: 4, Bottom: 4, Left: 4}) {
		t.Errorf("InsetsAll = %v", i)
	}
	j := geom.InsetsSymmetric(2, 8)
	if j != (geom.Insets{Top: 2, Right: 8, Bottom: 2, Left: 8}) {
		t.Errorf("InsetsSymmetric = %v", j)
	}
	if got, want := j.Horizontal(), float32(16); got != want {
		t.Errorf("Horizontal = %v, want %v", got, want)
	}
	if got, want := j.Vertical(), float32(4); got != want {
		t.Errorf("Vertical = %v, want %v", got, want)
	}
	if got, want := i.Add(j), (geom.Insets{Top: 6, Right: 12, Bottom: 6, Left: 12}); got != want {
		t.Errorf("Add = %v, want %v", got, want)
	}
	if !(geom.Insets{}).IsZero() {
		t.Error("zero Insets IsZero() = false")
	}
	if i.IsZero() {
		t.Error("InsetsAll(4).IsZero() = true")
	}
}

func TestRectConstruction(t *testing.T) {
	r := geom.RcXYWH(10, 20, 30, 40)
	if got, want := r, geom.Rc(10, 20, 40, 60); got != want {
		t.Errorf("RcXYWH = %v, want %v", got, want)
	}
	if got, want := geom.RcSize(geom.Pt(10, 20), geom.Sz(30, 40)), r; got != want {
		t.Errorf("RcSize = %v, want %v", got, want)
	}
	if got, want := r.Origin(), geom.Pt(10, 20); got != want {
		t.Errorf("Origin = %v, want %v", got, want)
	}
	if got, want := r.Size(), geom.Sz(30, 40); got != want {
		t.Errorf("Size = %v, want %v", got, want)
	}
	if got, want := r.Width(), float32(30); got != want {
		t.Errorf("Width = %v, want %v", got, want)
	}
	if got, want := r.Height(), float32(40); got != want {
		t.Errorf("Height = %v, want %v", got, want)
	}
	if got, want := r.Translate(geom.Pt(1, 2)), geom.Rc(11, 22, 41, 62); got != want {
		t.Errorf("Translate = %v, want %v", got, want)
	}
}

func TestRectIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		r    geom.Rect
		want bool
	}{
		{"normal", geom.Rc(0, 0, 10, 10), false},
		{"zero", geom.Rect{}, true},
		{"zero width", geom.Rc(5, 0, 5, 10), true},
		{"zero height", geom.Rc(0, 5, 10, 5), true},
		{"inverted x", geom.Rc(10, 0, 0, 10), true},
		{"inverted y", geom.Rc(0, 10, 10, 0), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.IsEmpty(); got != tc.want {
				t.Errorf("IsEmpty = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRectContains(t *testing.T) {
	r := geom.Rc(0, 0, 10, 10)
	tests := []struct {
		name string
		r    geom.Rect
		p    geom.Point
		want bool
	}{
		{"inside", r, geom.Pt(5, 5), true},
		{"min corner is inclusive", r, geom.Pt(0, 0), true},
		{"max corner is exclusive", r, geom.Pt(10, 10), false},
		{"max x edge excluded", r, geom.Pt(10, 5), false},
		{"max y edge excluded", r, geom.Pt(5, 10), false},
		{"min x edge included", r, geom.Pt(0, 5), true},
		{"just outside left", r, geom.Pt(-0.001, 5), false},
		{"just inside right", r, geom.Pt(9.999, 5), true},
		{"empty contains nothing", geom.Rect{}, geom.Pt(0, 0), false},
		{"offset rect", geom.Rc(-5, -5, -1, -1), geom.Pt(-3, -3), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Contains(tc.p); got != tc.want {
				t.Errorf("Contains(%v) = %v, want %v", tc.p, got, tc.want)
			}
		})
	}
}

func TestRectIntersectAndOverlaps(t *testing.T) {
	tests := []struct {
		name        string
		a, b        geom.Rect
		want        geom.Rect
		wantOverlap bool
	}{
		{
			name: "partial overlap", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(5, 5, 15, 15),
			want: geom.Rc(5, 5, 10, 10), wantOverlap: true,
		},
		{
			name: "b inside a", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(2, 2, 4, 4),
			want: geom.Rc(2, 2, 4, 4), wantOverlap: true,
		},
		{
			name: "identical", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(0, 0, 10, 10),
			want: geom.Rc(0, 0, 10, 10), wantOverlap: true,
		},
		{
			name: "touching edges only", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(10, 0, 20, 10),
			want: geom.Rect{}, wantOverlap: false,
		},
		{
			name: "touching corners only", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(10, 10, 20, 20),
			want: geom.Rect{}, wantOverlap: false,
		},
		{
			name: "disjoint x", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(20, 0, 30, 10),
			want: geom.Rect{}, wantOverlap: false,
		},
		{
			name: "disjoint y", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(0, 20, 10, 30),
			want: geom.Rect{}, wantOverlap: false,
		},
		{
			name: "overlapping x but disjoint y", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(5, 11, 15, 20),
			want: geom.Rect{}, wantOverlap: false,
		},
		{
			name: "empty operand", a: geom.Rect{}, b: geom.Rc(-5, -5, 5, 5),
			want: geom.Rect{}, wantOverlap: false,
		},
		{
			name: "sliver overlap", a: geom.Rc(0, 0, 10, 10), b: geom.Rc(9.5, -100, 100, 100),
			want: geom.Rc(9.5, 0, 10, 10), wantOverlap: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Intersect(tc.b); !approxRect(got, tc.want) {
				t.Errorf("Intersect = %v, want %v", got, tc.want)
			}
			// Intersect must be commutative.
			if got := tc.b.Intersect(tc.a); !approxRect(got, tc.want) {
				t.Errorf("reversed Intersect = %v, want %v", got, tc.want)
			}
			if got := tc.a.Overlaps(tc.b); got != tc.wantOverlap {
				t.Errorf("Overlaps = %v, want %v", got, tc.wantOverlap)
			}
			if got := tc.b.Overlaps(tc.a); got != tc.wantOverlap {
				t.Errorf("reversed Overlaps = %v, want %v", got, tc.wantOverlap)
			}
		})
	}
}

func TestRectUnion(t *testing.T) {
	tests := []struct {
		name string
		a, b geom.Rect
		want geom.Rect
	}{
		{"disjoint", geom.Rc(0, 0, 10, 10), geom.Rc(20, 20, 30, 30), geom.Rc(0, 0, 30, 30)},
		{"overlapping", geom.Rc(0, 0, 10, 10), geom.Rc(5, 5, 15, 15), geom.Rc(0, 0, 15, 15)},
		{"b inside a", geom.Rc(0, 0, 10, 10), geom.Rc(2, 2, 4, 4), geom.Rc(0, 0, 10, 10)},
		{"empty b ignored", geom.Rc(20, 20, 30, 30), geom.Rect{}, geom.Rc(20, 20, 30, 30)},
		{"empty a ignored", geom.Rect{}, geom.Rc(20, 20, 30, 30), geom.Rc(20, 20, 30, 30)},
		{"degenerate b ignored", geom.Rc(20, 20, 30, 30), geom.Rc(0, 0, 0, 100), geom.Rc(20, 20, 30, 30)},
		{"both empty", geom.Rect{}, geom.Rect{}, geom.Rect{}},
		{"negative coordinates", geom.Rc(-10, -10, -5, -5), geom.Rc(1, 1, 2, 2), geom.Rc(-10, -10, 2, 2)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Union(tc.b); !approxRect(got, tc.want) {
				t.Errorf("Union = %v, want %v", got, tc.want)
			}
			if got := tc.b.Union(tc.a); !approxRect(got, tc.want) {
				t.Errorf("reversed Union = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRectInsetOutset(t *testing.T) {
	tests := []struct {
		name string
		r    geom.Rect
		i    geom.Insets
		want geom.Rect
	}{
		{"uniform", geom.Rc(0, 0, 100, 100), geom.InsetsAll(10), geom.Rc(10, 10, 90, 90)},
		{
			"per edge", geom.Rc(0, 0, 100, 100),
			geom.Insets{Top: 1, Right: 2, Bottom: 3, Left: 4}, geom.Rc(4, 1, 98, 97),
		},
		{"zero insets", geom.Rc(1, 2, 3, 4), geom.Insets{}, geom.Rc(1, 2, 3, 4)},
		{"exact collapse", geom.Rc(0, 0, 20, 20), geom.InsetsAll(10), geom.Rc(10, 10, 10, 10)},
		{
			// Over inset: collapses to the midpoint of the over inset interval,
			// here (0+30 + 100-30)/2 = 50 on both axes.
			"over inset collapses at midpoint", geom.Rc(0, 0, 100, 100), geom.InsetsAll(60),
			geom.Rc(50, 50, 50, 50),
		},
		{
			"over inset asymmetric", geom.Rc(0, 0, 10, 100),
			geom.Insets{Left: 8, Right: 8, Top: 1, Bottom: 1}, geom.Rc(5, 1, 5, 99),
		},
		{"negative insets grow", geom.Rc(0, 0, 10, 10), geom.InsetsAll(-5), geom.Rc(-5, -5, 15, 15)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.Inset(tc.i)
			if !approxRect(got, tc.want) {
				t.Errorf("Inset = %v, want %v", got, tc.want)
			}
			if got.Width() < 0 || got.Height() < 0 {
				t.Errorf("Inset produced an inverted rect: %v", got)
			}
		})
	}

	// Outset is the exact inverse of Inset as long as nothing collapses.
	r := geom.Rc(0, 0, 100, 100)
	i := geom.Insets{Top: 1, Right: 2, Bottom: 3, Left: 4}
	if got := r.Inset(i).Outset(i); !approxRect(got, r) {
		t.Errorf("Inset then Outset = %v, want %v", got, r)
	}
	if got, want := r.Outset(geom.InsetsAll(10)), geom.Rc(-10, -10, 110, 110); !approxRect(got, want) {
		t.Errorf("Outset = %v, want %v", got, want)
	}
}

func TestRectCanon(t *testing.T) {
	tests := []struct {
		name string
		r    geom.Rect
		want geom.Rect
	}{
		{"already canonical", geom.Rc(0, 0, 10, 10), geom.Rc(0, 0, 10, 10)},
		{"swapped x", geom.Rc(10, 0, 0, 10), geom.Rc(0, 0, 10, 10)},
		{"swapped y", geom.Rc(0, 10, 10, 0), geom.Rc(0, 0, 10, 10)},
		{"swapped both", geom.Rc(10, 10, 0, 0), geom.Rc(0, 0, 10, 10)},
		{"negative", geom.Rc(-1, -1, -10, -10), geom.Rc(-10, -10, -1, -1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Canon(); got != tc.want {
				t.Errorf("Canon = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConstraintsConstructors(t *testing.T) {
	s := geom.Sz(100, 50)
	if got, want := geom.Tight(s), (geom.Constraints{Min: s, Max: s}); got != want {
		t.Errorf("Tight = %v, want %v", got, want)
	}
	if got, want := geom.Loose(s), (geom.Constraints{Min: geom.Size{}, Max: s}); got != want {
		t.Errorf("Loose = %v, want %v", got, want)
	}
	u := geom.Unconstrained()
	if u.Min != (geom.Size{}) || u.HasBoundedWidth() || u.HasBoundedHeight() {
		t.Errorf("Unconstrained = %v", u)
	}
}

func TestConstraintsConstrain(t *testing.T) {
	unbounded := geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(geom.Unbounded, geom.Unbounded)}
	tests := []struct {
		name string
		c    geom.Constraints
		in   geom.Size
		want geom.Size
	}{
		{"within range", geom.Loose(geom.Sz(100, 100)), geom.Sz(30, 40), geom.Sz(30, 40)},
		{"clamped to max", geom.Loose(geom.Sz(100, 100)), geom.Sz(300, 400), geom.Sz(100, 100)},
		{"clamped to min", geom.Tight(geom.Sz(50, 50)), geom.Sz(1, 1), geom.Sz(50, 50)},
		{"tight ignores input", geom.Tight(geom.Sz(50, 50)), geom.Sz(999, 999), geom.Sz(50, 50)},
		{"unbounded keeps huge width", unbounded, geom.Sz(1e9, 1e9), geom.Sz(1e9, 1e9)},
		{"unbounded still applies min", unbounded, geom.Sz(1, 1), geom.Sz(10, 10)},
		{
			"mixed boundedness",
			geom.Constraints{Min: geom.Sz(0, 0), Max: geom.Sz(100, geom.Unbounded)},
			geom.Sz(500, 500), geom.Sz(100, 500),
		},
		{"negative input clamped to min", geom.Loose(geom.Sz(100, 100)), geom.Sz(-5, -5), geom.Sz(0, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.Constrain(tc.in)
			if !approxSz(got, tc.want) {
				t.Errorf("Constrain = %v, want %v", got, tc.want)
			}
			if math.IsNaN(float64(got.W)) || math.IsNaN(float64(got.H)) {
				t.Errorf("Constrain produced NaN: %v", got)
			}
			if w := tc.c.ConstrainWidth(tc.in.W); !approx(w, tc.want.W) {
				t.Errorf("ConstrainWidth = %v, want %v", w, tc.want.W)
			}
			if h := tc.c.ConstrainHeight(tc.in.H); !approx(h, tc.want.H) {
				t.Errorf("ConstrainHeight = %v, want %v", h, tc.want.H)
			}
			if !tc.c.IsSatisfiedBy(got) {
				t.Errorf("Constrain result %v does not satisfy %v", got, tc.c)
			}
		})
	}
}

func TestConstraintsLoosenTighten(t *testing.T) {
	c := geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(100, 100)}

	l := c.Loosen()
	if l.Min != (geom.Size{}) || l.Max != c.Max {
		t.Errorf("Loosen = %v", l)
	}

	tt := c.Tighten(geom.Sz(500, 5))
	want := geom.Tight(geom.Sz(100, 10))
	if tt != want {
		t.Errorf("Tighten = %v, want %v", tt, want)
	}
	if !tt.IsNormalized() {
		t.Errorf("Tighten result not normalized: %v", tt)
	}
}

func TestConstraintsDeflate(t *testing.T) {
	tests := []struct {
		name string
		c    geom.Constraints
		i    geom.Insets
		want geom.Constraints
	}{
		{
			"plain", geom.Constraints{Min: geom.Sz(20, 20), Max: geom.Sz(100, 100)},
			geom.InsetsAll(10),
			geom.Constraints{Min: geom.Sz(0, 0), Max: geom.Sz(80, 80)},
		},
		{
			"min survives", geom.Constraints{Min: geom.Sz(80, 80), Max: geom.Sz(100, 100)},
			geom.InsetsAll(5),
			geom.Constraints{Min: geom.Sz(70, 70), Max: geom.Sz(90, 90)},
		},
		{
			"clamped at zero, never negative",
			geom.Constraints{Min: geom.Sz(5, 5), Max: geom.Sz(10, 10)},
			geom.InsetsAll(50),
			geom.Constraints{Min: geom.Sz(0, 0), Max: geom.Sz(0, 0)},
		},
		{
			"unbounded stays unbounded", geom.Unconstrained(), geom.InsetsAll(12),
			geom.Constraints{Min: geom.Sz(0, 0), Max: geom.Sz(geom.Unbounded, geom.Unbounded)},
		},
		{
			"asymmetric insets",
			geom.Constraints{Min: geom.Sz(0, 0), Max: geom.Sz(100, 100)},
			geom.Insets{Top: 1, Right: 2, Bottom: 3, Left: 4},
			geom.Constraints{Min: geom.Sz(0, 0), Max: geom.Sz(94, 96)},
		},
		{"zero insets are a no-op", geom.Loose(geom.Sz(7, 9)), geom.Insets{}, geom.Loose(geom.Sz(7, 9))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.Deflate(tc.i)
			if !approxSz(got.Min, tc.want.Min) || !approxSz(got.Max, tc.want.Max) {
				t.Errorf("Deflate = %v, want %v", got, tc.want)
			}
			if !got.IsNormalized() {
				t.Errorf("Deflate result not normalized: %v", got)
			}
		})
	}

	if got := geom.Unconstrained().Deflate(geom.InsetsAll(12)); got.HasBoundedWidth() || got.HasBoundedHeight() {
		t.Errorf("Deflate made an unbounded constraint bounded: %v", got)
	}
}

func TestConstraintsPredicates(t *testing.T) {
	tests := []struct {
		name          string
		c             geom.Constraints
		wantNorm      bool
		wantBoundedW  bool
		wantBoundedH  bool
		probe         geom.Size
		wantSatisfied bool
	}{
		{
			"normal", geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(100, 100)},
			true, true, true, geom.Sz(50, 50), true,
		},
		{
			"probe below min", geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(100, 100)},
			true, true, true, geom.Sz(9, 50), false,
		},
		{
			"probe above max", geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(100, 100)},
			true, true, true, geom.Sz(50, 101), false,
		},
		{
			"probe exactly on bounds", geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(100, 100)},
			true, true, true, geom.Sz(10, 100), true,
		},
		{"unconstrained", geom.Unconstrained(), true, false, false, geom.Sz(1e9, 1e9), true},
		{
			"half bounded", geom.Constraints{Min: geom.Size{}, Max: geom.Sz(100, geom.Unbounded)},
			true, true, false, geom.Sz(100, 1e9), true,
		},
		{
			"min greater than max", geom.Constraints{Min: geom.Sz(10, 10), Max: geom.Sz(5, 5)},
			false, true, true, geom.Sz(7, 7), false,
		},
		{
			"negative min", geom.Constraints{Min: geom.Sz(-1, 0), Max: geom.Sz(5, 5)},
			false, true, true, geom.Sz(1, 1), true,
		},
		{
			"infinite min", geom.Constraints{Min: geom.Sz(geom.Unbounded, 0), Max: geom.Sz(geom.Unbounded, 5)},
			false, false, true, geom.Sz(1, 1), false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsNormalized(); got != tc.wantNorm {
				t.Errorf("IsNormalized = %v, want %v", got, tc.wantNorm)
			}
			if got := tc.c.HasBoundedWidth(); got != tc.wantBoundedW {
				t.Errorf("HasBoundedWidth = %v, want %v", got, tc.wantBoundedW)
			}
			if got := tc.c.HasBoundedHeight(); got != tc.wantBoundedH {
				t.Errorf("HasBoundedHeight = %v, want %v", got, tc.wantBoundedH)
			}
			if got := tc.c.IsSatisfiedBy(tc.probe); got != tc.wantSatisfied {
				t.Errorf("IsSatisfiedBy(%v) = %v, want %v", tc.probe, got, tc.wantSatisfied)
			}
		})
	}
}

func TestAlignmentPosition(t *testing.T) {
	within := geom.Rc(0, 0, 100, 100)
	child := geom.Sz(20, 10)

	tests := []struct {
		name string
		a    geom.Alignment
		want geom.Rect
	}{
		{"top leading", geom.TopLeading, geom.RcXYWH(0, 0, 20, 10)},
		{"top", geom.Top, geom.RcXYWH(40, 0, 20, 10)},
		{"top trailing", geom.TopTrailing, geom.RcXYWH(80, 0, 20, 10)},
		{"leading", geom.Leading, geom.RcXYWH(0, 45, 20, 10)},
		{"center", geom.Center, geom.RcXYWH(40, 45, 20, 10)},
		{"trailing", geom.Trailing, geom.RcXYWH(80, 45, 20, 10)},
		{"bottom leading", geom.BottomLeading, geom.RcXYWH(0, 90, 20, 10)},
		{"bottom", geom.Bottom, geom.RcXYWH(40, 90, 20, 10)},
		{"bottom trailing", geom.BottomTrailing, geom.RcXYWH(80, 90, 20, 10)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.a.Position(child, within)
			if !approxRect(got, tc.want) {
				t.Errorf("Position = %v, want %v", got, tc.want)
			}
			if !approxSz(got.Size(), child) {
				t.Errorf("Position changed the child size: %v", got.Size())
			}
		})
	}

	t.Run("offset container", func(t *testing.T) {
		got := geom.Center.Position(geom.Sz(10, 10), geom.Rc(100, 200, 200, 300))
		want := geom.RcXYWH(145, 245, 10, 10)
		if !approxRect(got, want) {
			t.Errorf("Position = %v, want %v", got, want)
		}
	})

	t.Run("oversized child overflows", func(t *testing.T) {
		got := geom.Center.Position(geom.Sz(200, 200), within)
		want := geom.RcXYWH(-50, -50, 200, 200)
		if !approxRect(got, want) {
			t.Errorf("Position = %v, want %v", got, want)
		}
	})

	t.Run("exact fit", func(t *testing.T) {
		for _, a := range []geom.Alignment{geom.TopLeading, geom.Center, geom.BottomTrailing} {
			if got := a.Position(geom.Sz(100, 100), within); !approxRect(got, within) {
				t.Errorf("Position for %v = %v, want %v", a, got, within)
			}
		}
	})
}

func TestAffineConstructors(t *testing.T) {
	if !geom.Identity().IsIdentity() {
		t.Error("Identity().IsIdentity() = false")
	}
	if !geom.Identity().IsTranslationOnly() {
		t.Error("Identity().IsTranslationOnly() = false")
	}

	tr := geom.Translate(geom.Pt(5, -3))
	if tr.IsIdentity() {
		t.Error("Translate.IsIdentity() = true")
	}
	if !tr.IsTranslationOnly() {
		t.Error("Translate.IsTranslationOnly() = false")
	}
	if got, want := tr.Apply(geom.Pt(1, 1)), geom.Pt(6, -2); !approxPt(got, want) {
		t.Errorf("Translate.Apply = %v, want %v", got, want)
	}

	sc := geom.Scale(2, 3)
	if sc.IsTranslationOnly() {
		t.Error("Scale.IsTranslationOnly() = true")
	}
	if got, want := sc.Apply(geom.Pt(2, 2)), geom.Pt(4, 6); !approxPt(got, want) {
		t.Errorf("Scale.Apply = %v, want %v", got, want)
	}
	if got, want := sc.Det(), float32(6); !approx(got, want) {
		t.Errorf("Scale.Det = %v, want %v", got, want)
	}

	rot := geom.Rotate(math.Pi / 2)
	if got, want := rot.Apply(geom.Pt(1, 0)), geom.Pt(0, 1); !approxPt(got, want) {
		t.Errorf("Rotate(90deg).Apply((1,0)) = %v, want %v", got, want)
	}
	if got, want := rot.Det(), float32(1); !approx(got, want) {
		t.Errorf("Rotate.Det = %v, want %v", got, want)
	}
	if got, want := geom.Rotate(0), geom.Identity(); !approxAff(got, want) {
		t.Errorf("Rotate(0) = %v, want %v", got, want)
	}
	// Scale(1,1) is the identity, so it counts as translation only.
	if !geom.Scale(1, 1).IsTranslationOnly() {
		t.Error("Scale(1,1).IsTranslationOnly() = false")
	}
}

// TestAffineMulOrder pins down the documented composition order: m.Mul(n)
// applies m first and n afterwards.
func TestAffineMulOrder(t *testing.T) {
	tr := geom.Translate(geom.Pt(10, 0))
	sc := geom.Scale(2, 2)

	t.Run("translate then scale", func(t *testing.T) {
		m := tr.Mul(sc)
		// The point is translated to (11, 1) and then scaled to (22, 2).
		if got, want := m.Apply(geom.Pt(1, 1)), geom.Pt(22, 2); !approxPt(got, want) {
			t.Errorf("Apply = %v, want %v", got, want)
		}
	})

	t.Run("scale then translate", func(t *testing.T) {
		m := sc.Mul(tr)
		// The point is scaled to (2, 2) and then translated to (12, 2).
		if got, want := m.Apply(geom.Pt(1, 1)), geom.Pt(12, 2); !approxPt(got, want) {
			t.Errorf("Apply = %v, want %v", got, want)
		}
	})

	t.Run("matches nested apply", func(t *testing.T) {
		ms := []geom.Affine2D{
			geom.Translate(geom.Pt(3, -7)),
			geom.Scale(1.5, -2),
			geom.Rotate(0.7),
			{A: 1, B: 0.3, C: -0.2, D: 1, TX: 4, TY: 5},
		}
		p := geom.Pt(2.5, -1.25)
		for i, m := range ms {
			for j, n := range ms {
				got := m.Mul(n).Apply(p)
				want := n.Apply(m.Apply(p))
				if !approxPt(got, want) {
					t.Errorf("ms[%d].Mul(ms[%d]).Apply(p) = %v, want %v", i, j, got, want)
				}
			}
		}
	})

	t.Run("identity is neutral", func(t *testing.T) {
		m := geom.Affine2D{A: 2, B: 0.5, C: -1, D: 3, TX: 7, TY: -8}
		if got := m.Mul(geom.Identity()); !approxAff(got, m) {
			t.Errorf("m.Mul(Identity) = %v, want %v", got, m)
		}
		if got := geom.Identity().Mul(m); !approxAff(got, m) {
			t.Errorf("Identity.Mul(m) = %v, want %v", got, m)
		}
	})
}

func TestAffineInvert(t *testing.T) {
	t.Run("singular", func(t *testing.T) {
		for _, m := range []geom.Affine2D{
			{},
			geom.Scale(0, 1),
			geom.Scale(1, 0),
			{A: 1, B: 2, C: 2, D: 4, TX: 9, TY: 9},
		} {
			if inv, ok := m.Invert(); ok {
				t.Errorf("Invert(%v) = %v, true; want false", m, inv)
			}
		}
	})

	t.Run("known inverse", func(t *testing.T) {
		m := geom.Translate(geom.Pt(10, 20))
		inv, ok := m.Invert()
		if !ok {
			t.Fatal("Invert = false")
		}
		if want := geom.Translate(geom.Pt(-10, -20)); !approxAff(inv, want) {
			t.Errorf("Invert = %v, want %v", inv, want)
		}
	})
}

// TestAffineInvertRoundTrip is the property style check: for random non singular
// matrices, composing with the inverse must yield the identity. The seed is
// fixed so that failures are reproducible.
func TestAffineInvertRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5eed))
	tested := 0
	for i := 0; i < 2000; i++ {
		m := geom.Affine2D{
			A:  float32(rng.Float64()*4 - 2),
			B:  float32(rng.Float64()*4 - 2),
			C:  float32(rng.Float64()*4 - 2),
			D:  float32(rng.Float64()*4 - 2),
			TX: float32(rng.Float64()*200 - 100),
			TY: float32(rng.Float64()*200 - 100),
		}
		det := m.Det()
		if det < 0 {
			det = -det
		}
		// Near singular matrices amplify float32 error beyond any fixed epsilon.
		// They are a numerics fact, not a bug, so they are skipped explicitly.
		if det < 0.25 {
			continue
		}
		tested++

		inv, ok := m.Invert()
		if !ok {
			t.Fatalf("Invert(%v) = false although det = %v", m, m.Det())
		}
		if got := m.Mul(inv); !approxAff(got, geom.Identity()) {
			t.Errorf("m.Mul(inv) = %v, want identity (m = %v)", got, m)
		}
		if got := inv.Mul(m); !approxAff(got, geom.Identity()) {
			t.Errorf("inv.Mul(m) = %v, want identity (m = %v)", got, m)
		}

		p := geom.Pt(float32(rng.Float64()*100), float32(rng.Float64()*100))
		if got := inv.Apply(m.Apply(p)); !approxPt(got, p) {
			t.Errorf("inv(m(p)) = %v, want %v", got, p)
		}
	}
	if tested < 1000 {
		t.Fatalf("only %d matrices actually tested, sampling is broken", tested)
	}
}

func TestAffineTransformRect(t *testing.T) {
	r := geom.Rc(10, 20, 30, 60)

	tests := []struct {
		name string
		m    geom.Affine2D
		r    geom.Rect
		want geom.Rect
	}{
		{"identity", geom.Identity(), r, r},
		{"translate", geom.Translate(geom.Pt(5, -5)), r, geom.Rc(15, 15, 35, 55)},
		{"scale", geom.Scale(2, 0.5), r, geom.Rc(20, 10, 60, 30)},
		{"negative scale is canonicalised", geom.Scale(-1, 1), r, geom.Rc(-30, 20, -10, 60)},
		{"rotate 90 degrees", geom.Rotate(math.Pi / 2), geom.Rc(0, 0, 2, 1), geom.Rc(-1, 0, 0, 2)},
		{"rotate 180 degrees", geom.Rotate(math.Pi), geom.Rc(0, 0, 2, 1), geom.Rc(-2, -1, 0, 0)},
		{
			// A square rotated by 45 degrees has a hull of side sqrt(2).
			"rotate 45 degrees hull", geom.Rotate(math.Pi / 4), geom.Rc(-0.5, -0.5, 0.5, 0.5),
			geom.Rc(-0.70710678, -0.70710678, 0.70710678, 0.70710678),
		},
		{"empty stays empty", geom.Scale(3, 3), geom.Rect{}, geom.Rect{}},
		{"degenerate stays empty", geom.Rotate(0.3), geom.Rc(5, 5, 5, 50), geom.Rect{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.TransformRect(tc.r); !approxRect(got, tc.want) {
				t.Errorf("TransformRect = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("hull contains all transformed corners", func(t *testing.T) {
		m := geom.Rotate(0.6).Mul(geom.Scale(1.3, -2.2)).Mul(geom.Translate(geom.Pt(7, 9)))
		hull := m.TransformRect(r)
		corners := []geom.Point{
			{X: r.Min.X, Y: r.Min.Y}, {X: r.Max.X, Y: r.Min.Y},
			{X: r.Min.X, Y: r.Max.Y}, {X: r.Max.X, Y: r.Max.Y},
		}
		for _, c := range corners {
			p := m.Apply(c)
			if p.X < hull.Min.X-eps || p.X > hull.Max.X+eps ||
				p.Y < hull.Min.Y-eps || p.Y > hull.Max.Y+eps {
				t.Errorf("corner %v maps to %v outside hull %v", c, p, hull)
			}
		}
	})
}

// TestTransformRectPureTranslationIsExact checks the scroll fast path: for a
// translation only transform, TransformRect must be bit for bit identical to
// Rect.Translate, not merely close.
func TestTransformRectPureTranslationIsExact(t *testing.T) {
	rects := []geom.Rect{
		geom.Rc(0, 0, 1, 1),
		geom.Rc(-1234.5, 9876.25, -1000.125, 10000),
		geom.Rc(0.1, 0.2, 0.30000001, 100000),
		geom.RcXYWH(3, 4, 1e-3, 1e-3),
	}
	deltas := []geom.Point{
		geom.Pt(0, 0),
		geom.Pt(1, -1),
		geom.Pt(-0.125, 0.375),
		geom.Pt(123456.75, -0.0009765625),
	}
	for _, r := range rects {
		for _, d := range deltas {
			m := geom.Translate(d)
			if !m.IsTranslationOnly() {
				t.Fatalf("Translate(%v).IsTranslationOnly() = false", d)
			}
			got := m.TransformRect(r)
			want := r.Translate(d)
			if got != want {
				t.Errorf("TransformRect(%v) with delta %v = %v, want exactly %v", r, d, got, want)
			}
		}
	}
}

// Sinks prevent the compiler from eliminating the operations measured by
// TestNoAllocs.
var (
	sinkPoint  geom.Point
	sinkSize   geom.Size
	sinkRect   geom.Rect
	sinkInsets geom.Insets
	sinkCons   geom.Constraints
	sinkAff    geom.Affine2D
	sinkF32    float32
	sinkBool   bool
)

// TestNoAllocs asserts the core contract of this package and of section 11 of
// the plan: a representative mix of every operation must allocate exactly
// nothing, so that geom can be used freely inside the frame hot path.
func TestNoAllocs(t *testing.T) {
	mix := func() {
		p := geom.Pt(1.5, -2.5)
		q := geom.Pt(3, 4)
		sinkPoint = p.Add(q).Sub(q).Mul(2)
		sinkBool = p.IsZero()

		i := geom.InsetsAll(4).Add(geom.InsetsSymmetric(2, 8))
		sinkInsets = i
		sinkF32 = i.Horizontal() + i.Vertical()
		sinkBool = i.IsZero()

		s := geom.Sz(100, 50).Add(geom.Sz(1, 1)).Sub(geom.Sz(0.5, 0.5))
		s = s.Inset(i).Outset(i)
		sinkSize = s
		sinkBool = s.IsZero() || s.IsFinite()

		r := geom.RcXYWH(10, 20, 300, 400)
		r2 := geom.RcSize(geom.Pt(100, 100), geom.Sz(50, 50))
		sinkRect = r.Intersect(r2).Union(r).Translate(q).Inset(i).Outset(i).Canon()
		sinkSize = r.Size()
		sinkPoint = r.Origin()
		sinkF32 = r.Width() + r.Height()
		sinkBool = r.IsEmpty() || r.Contains(q) || r.Overlaps(r2)

		c := geom.Loose(geom.Sz(300, geom.Unbounded))
		c = c.Deflate(i).Loosen().Tighten(geom.Sz(120, 90))
		c2 := geom.Unconstrained().Deflate(i)
		sinkCons = c
		sinkSize = c.Constrain(s)
		sinkF32 = c.ConstrainWidth(10) + c2.ConstrainHeight(10)
		sinkBool = c.HasBoundedWidth() || c.HasBoundedHeight() ||
			c.IsSatisfiedBy(s) || c.IsNormalized()
		sinkCons = geom.Tight(s)

		m := geom.Translate(q).Mul(geom.Scale(2, 2)).Mul(geom.Rotate(0.5))
		sinkAff = m
		sinkPoint = m.Apply(p)
		sinkF32 = m.Det()
		inv, ok := m.Invert()
		sinkAff = inv
		sinkBool = ok || m.IsIdentity() || m.IsTranslationOnly()
		sinkRect = m.TransformRect(r)
		sinkRect = geom.Translate(q).TransformRect(r)

		sinkRect = geom.Center.Position(s, r)
		sinkRect = geom.BottomTrailing.Position(s, r)
	}

	if got := testing.AllocsPerRun(100, mix); got != 0 {
		t.Errorf("AllocsPerRun = %v, want 0", got)
	}
}
