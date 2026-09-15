package iconsvg

import (
	"math"
)

// This file is the offline half of the even-odd problem, and it exists because
// golang.org/x/image/vector — the rasteriser the project plan, section 21,
// commits to — fills with the nonzero winding rule and offers no other.
//
// 211 of the 595 paths in the surveyed Flowbite corpus declare
// fill-rule="evenodd". For a shape whose inner contour winds *against* its
// outer one the two rules agree and there is nothing to do; for one that winds
// the same way they do not, and the hole fills in. Which of the 211 are which
// is a measurement, not a guess, and [Differs] is that measurement.
//
// The fix is to orient the contours so that the nonzero rule reproduces the
// even-odd one: a contour nested at an even depth winds one way and a contour
// nested at an odd depth the other. That is exact for a set of contours that
// do not cross each other, which is what an icon is, and [Orient] verifies its
// own output against the original even-odd fill rather than asserting it.
//
// Doing this offline is the point. The generator may spend a second per icon;
// a cache miss inside a frame may not.

// flatten converts one figure's ops into closed polygons, one per subpath.
//
// The tolerance is fixed and fine — a thousandth of a user unit — because this
// runs offline and the polygons are only ever used for point-in-polygon tests,
// where more vertices cost nothing that matters.
// contour is one closed polygon together with the bounding box that lets
// [winding] skip it.
type contour struct {
	pts              []([2]float64)
	minY, maxY, maxX float64
}

func newContour(pts [][2]float64) contour {
	c := contour{pts: pts, minY: math.Inf(1), maxY: math.Inf(-1), maxX: math.Inf(-1)}
	for _, p := range pts {
		c.minY, c.maxY = math.Min(c.minY, p[1]), math.Max(c.maxY, p[1])
		c.maxX = math.Max(c.maxX, p[0])
	}
	return c
}

func flatten(ops []Op) []contour {
	const tol = 0.001
	var out []contour
	var cur [][2]float64
	var start [2]float64
	flush := func() {
		if len(cur) >= 3 {
			out = append(out, newContour(cur))
		}
		cur = nil
	}
	for _, op := range ops {
		switch op.Kind {
		case 'M':
			flush()
			start = op.P[0]
			cur = append(cur, op.P[0])
		case 'L':
			cur = append(cur, op.P[0])
		case 'C':
			if len(cur) == 0 {
				continue
			}
			p0 := cur[len(cur)-1]
			// Uniform subdivision against the standard cubic flatness bound,
			// the same one internal/icon uses at run time.
			ax, ay := p0[0]-2*op.P[0][0]+op.P[1][0], p0[1]-2*op.P[0][1]+op.P[1][1]
			bx, by := op.P[0][0]-2*op.P[1][0]+op.P[2][0], op.P[0][1]-2*op.P[1][1]+op.P[2][1]
			d := math.Max(math.Hypot(ax, ay), math.Hypot(bx, by))
			n := 1
			if d > 0 {
				n = int(math.Ceil(math.Sqrt(0.75 * d / tol)))
			}
			n = min(max(n, 1), 512)
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				u := 1 - t
				a, b, c, e := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				cur = append(cur, [2]float64{
					a*p0[0] + b*op.P[0][0] + c*op.P[1][0] + e*op.P[2][0],
					a*p0[1] + b*op.P[0][1] + c*op.P[1][1] + e*op.P[2][1],
				})
			}
		case 'Z':
			cur = append(cur, start)
			flush()
			cur = append(cur, start)
		}
	}
	flush()
	return out
}

// winding returns the nonzero winding number of the polygons around p and the
// parity of the crossing count, which is the even-odd answer.
//
// One walk produces both, because they are the same crossings counted two
// ways. That matters for the cost: [Differs] runs this over a grid for every
// even-odd path in the corpus.
func winding(polys []contour, px, py float64) (int, int) {
	w, cross := 0, 0
	for _, c := range polys {
		if py < c.minY || py > c.maxY || px > c.maxX {
			// The ray runs to the right from the sample, so a sample above,
			// below or to the right of the contour's bounding box crosses
			// none of its edges. It changes no answer and it is what keeps
			// the corpus sweep to seconds rather than minutes: the
			// measurement takes 57 600 samples per path over 211 paths.
			continue
		}
		poly := c.pts
		for i := range poly {
			a, b := poly[i], poly[(i+1)%len(poly)]
			if a[1] <= py {
				if b[1] > py && isLeft(a, b, px, py) > 0 {
					w++
					cross++
				}
			} else if b[1] <= py && isLeft(a, b, px, py) < 0 {
				w--
				cross++
			}
		}
	}
	return w, cross & 1
}

func isLeft(a, b [2]float64, px, py float64) float64 {
	return (b[0]-a[0])*(py-a[1]) - (px-a[0])*(b[1]-a[1])
}

// DiffersUnderNonzero reports whether filling ops with the nonzero rule paints
// a different area than filling it with the even-odd rule, and how many sample
// points out of n*n disagree.
//
// It is a sampled measurement over a grid inside the viewBox, with the samples
// offset by half a cell so that none of them lands on an axis aligned edge —
// the corpus is full of horizontal and vertical edges at integer coordinates,
// and a sample exactly on one would be decided by the tie breaking of the
// crossing rule rather than by the fill rule.
//
// It is a sample and says so. A disagreement smaller than one grid cell is
// invisible to it; at the resolution the generator uses, that is a feature
// thinner than a tenth of a pixel of the rendered icon.
func DiffersUnderNonzero(ops []Op, box float64, n int) (differs bool, disagreeing int) {
	polys := flatten(ops)
	if len(polys) < 2 {
		// One contour: the two rules cannot disagree, because a point is
		// inside it under both or under neither.
		return false, 0
	}
	step := box / float64(n)
	for iy := range n {
		y := (float64(iy) + 0.5) * step
		for ix := range n {
			x := (float64(ix) + 0.5) * step
			w, parity := winding(polys, x, y)
			nz := 0
			if w != 0 {
				nz = 1
			}
			if nz != parity {
				disagreeing++
			}
		}
	}
	return disagreeing > 0, disagreeing
}

// Orient reorients the contours of an even-odd path so that the nonzero rule
// paints the same area, and reports whether it succeeded.
//
// The rule is nesting depth: a contour contained in an even number of others
// winds positively, one contained in an odd number winds negatively, and the
// windings then cancel to zero exactly where the even-odd parity is even. It
// is exact whenever no two contours cross, which holds for artwork and is
// verified rather than assumed — the returned ops are re-measured against the
// original under [DiffersUnderNonzero], and a failure is reported instead of
// being emitted.
func Orient(ops []Op, box float64, n int) ([]Op, bool) {
	subs := splitSubpaths(ops)
	polys := make([]contour, len(subs))
	for i, sub := range subs {
		p := flatten(sub)
		if len(p) != 1 {
			// A subpath that is not one closed contour — too few points, or a
			// Z followed by more geometry. Neither appears in the corpus, and
			// guessing what the parity rule means for it would be exactly the
			// kind of silent approximation this file avoids.
			return nil, false
		}
		polys[i] = p[0]
	}
	if len(subs) < 2 {
		return ops, true
	}
	depth := make([]int, len(subs))
	for i := range polys {
		for j := range polys {
			if i != j && contains(polys[j], polys[i]) {
				depth[i]++
			}
		}
	}
	out := make([]Op, 0, len(ops))
	for i, sub := range subs {
		want := 1.0
		if depth[i]%2 == 1 {
			want = -1
		}
		if signOfArea(sub) != want {
			sub = reverseSubpath(sub)
		}
		out = append(out, sub...)
	}
	if differs, _ := compare(ops, out, box, n); differs {
		return nil, false
	}
	return out, true
}

// contains reports whether the closed contour outer encloses the closed
// contour inner.
//
// The test is taken on inner's own *boundary* rather than in its interior, and
// that distinction is the whole of it. A point inside the badge of
// solid/badge-check.svg is quite likely to be inside the check mark punched
// out of it as well, so an interior sample makes the outer contour look nested
// inside the inner one and the depth parity comes out inverted — which is
// exactly how the first version of this file got that icon wrong. Two contours
// of a piece of artwork never cross, so every point of inner's boundary is on
// the same side of outer, and a boundary point of the *inner* one is inside
// the outer one precisely when it is nested.
//
// Several samples are taken and the majority wins, because a single sample can
// land exactly on a horizontal edge of outer, where the half open crossing
// rule decides by tie break rather than by geometry.
func contains(outer, inner contour) bool {
	one := []contour{outer}
	in, n := 0, 0
	for k := range 9 {
		i := k * len(inner.pts) / 9
		j := (i + 1) % len(inner.pts)
		x := (inner.pts[i][0] + inner.pts[j][0]) / 2
		y := (inner.pts[i][1] + inner.pts[j][1]) / 2
		if w, _ := winding(one, x, y); w != 0 {
			in++
		}
		n++
	}
	return in*2 > n
}

func splitSubpaths(ops []Op) [][]Op {
	var out [][]Op
	var cur []Op
	for _, op := range ops {
		if op.Kind == 'M' && len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, op)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func signOfArea(sub []Op) float64 {
	polys := flatten(sub)
	if len(polys) == 0 {
		return 1
	}
	a := 0.0
	poly := polys[0].pts
	for i := range poly {
		p, q := poly[i], poly[(i+1)%len(poly)]
		a += p[0]*q[1] - q[0]*p[1]
	}
	if a < 0 {
		return -1
	}
	return 1
}

func reverseSubpath(sub []Op) []Op {
	// Collect the point sequence: the opening move, then one entry per
	// segment carrying its end point and, for a cubic, its controls.
	var pts [][2]float64
	type seg struct {
		cubic  bool
		c1, c2 [2]float64
		to     [2]float64
	}
	var segs []seg
	closed := false
	var start [2]float64
	for _, op := range sub {
		switch op.Kind {
		case 'M':
			start = op.P[0]
			pts = append(pts, start)
		case 'L':
			segs = append(segs, seg{to: op.P[0]})
		case 'C':
			segs = append(segs, seg{cubic: true, c1: op.P[0], c2: op.P[1], to: op.P[2]})
		case 'Z':
			closed = true
		}
	}
	if len(segs) == 0 {
		return sub
	}
	// The endpoint chain, forwards.
	chain := append([][2]float64{start}, func() [][2]float64 {
		v := make([][2]float64, len(segs))
		for i, s := range segs {
			v[i] = s.to
		}
		return v
	}()...)

	out := []Op{{Kind: 'M', P: [3][2]float64{chain[len(chain)-1]}}}
	for i := len(segs) - 1; i >= 0; i-- {
		s := segs[i]
		from := chain[i]
		if s.cubic {
			out = append(out, Op{Kind: 'C', P: [3][2]float64{s.c2, s.c1, from}})
		} else {
			out = append(out, Op{Kind: 'L', P: [3][2]float64{from}})
		}
	}
	if closed {
		out = append(out, Op{Kind: 'Z'})
	}
	return out
}

// compare reports whether filling a with the even-odd rule and b with the
// nonzero rule paint different areas, sampled like [DiffersUnderNonzero].
func compare(a, b []Op, box float64, n int) (bool, int) {
	pa, pb := flatten(a), flatten(b)
	step := box / float64(n)
	bad := 0
	for iy := range n {
		y := (float64(iy) + 0.5) * step
		for ix := range n {
			x := (float64(ix) + 0.5) * step
			_, parity := winding(pa, x, y)
			w, _ := winding(pb, x, y)
			nz := 0
			if w != 0 {
				nz = 1
			}
			if nz != parity {
				bad++
			}
		}
	}
	return bad > 0, bad
}
