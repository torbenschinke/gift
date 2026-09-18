package icon

import (
	"math"
	"testing"

	"github.com/worldiety/gift/internal/iconsvg"
)

// build encodes one figure from a tiny path description and returns the blob.
func buildFill(t testing.TB, p Paint, f func(e *Encoder)) []byte {
	t.Helper()
	var e Encoder
	e.Figure(p)
	f(&e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func buildStroke(t testing.TB, w float32, c Cap, j Join, f func(e *Encoder)) []byte {
	t.Helper()
	var e Encoder
	e.StrokeFigure(w, c, j)
	f(&e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// at reads one coverage value, or -1 outside the mask.
func at(m *Mask, x, y int) int {
	if x < 0 || y < 0 || x >= m.Size || y >= m.Size {
		return -1
	}
	return int(m.Pix[y*m.Size+x])
}

// inked is the number of pixels with any coverage, and full the number with
// complete coverage.
func inked(m *Mask) (any_, full int) {
	for _, c := range m.Pix {
		if c > 0 {
			any_++
		}
		if c == 255 {
			full++
		}
	}
	return
}

// TestAFilledSquareCoversExactlyItsArea is the baseline: if the fill path is
// wrong, nothing below means anything.
func TestAFilledSquareCoversExactlyItsArea(t *testing.T) {
	data := buildFill(t, PaintFill, func(e *Encoder) {
		e.MoveTo(6, 6)
		e.LineTo(18, 6)
		e.LineTo(18, 18)
		e.LineTo(6, 18)
		e.Close()
	})
	r := NewRasterizer()
	var m Mask
	if !r.Rasterize(data, 24, 48, &m) {
		t.Fatal("a 12 by 12 square in a 24 unit box produced no ink")
	}
	// At 48 pixels for 24 units the square is pixels 12..36 on both axes.
	if got := at(&m, 24, 24); got != 255 {
		t.Errorf("the centre of the square has coverage %d, want 255", got)
	}
	if got := at(&m, 4, 4); got != 0 {
		t.Errorf("a pixel outside the square has coverage %d, want 0", got)
	}
	a, _ := inked(&m)
	if want := 24 * 24; a < want || a > want+4*24+4 {
		t.Errorf("%d pixels carry ink, want about %d (the 24 by 24 square plus its antialiased "+
			"edge)", a, want)
	}
}

// TestAnArcRendersAsACircleAndNotMerelyAsACurve is the end to end check of the
// most common command in the corpus. The parser's own tests verify that the
// converted cubics lie on the circle; this one verifies that what actually
// reaches the coverage mask is a disc.
//
// The measure is per pixel: for every pixel of the mask, a disc of radius 8
// units centred on the box says whether it should be inside, outside or on the
// boundary, and the mask has to agree except within a pixel of the boundary.
// A conversion that produced a plausible but wrong curve — a wrong centre, a
// wrong kappa — would put thousands of pixels on the wrong side.
func TestAnArcRendersAsACircleAndNotMerelyAsACurve(t *testing.T) {
	// The idiom the corpus uses for a circle: two half arcs, run through the
	// real parser so that this test covers the conversion and not a
	// hand written approximation of it.
	ops, err := iconsvg.ParsePathData("M4 12a8 8 0 1 0 16 0 8 8 0 1 0-16 0")
	if err != nil {
		t.Fatal(err)
	}
	const px = 96
	data := buildFill(t, PaintFill, func(e *Encoder) {
		for _, op := range ops {
			switch op.Kind {
			case 'M':
				e.MoveTo(float32(op.P[0][0]), float32(op.P[0][1]))
			case 'L':
				e.LineTo(float32(op.P[0][0]), float32(op.P[0][1]))
			case 'C':
				e.CubeTo(
					float32(op.P[0][0]), float32(op.P[0][1]),
					float32(op.P[1][0]), float32(op.P[1][1]),
					float32(op.P[2][0]), float32(op.P[2][1]))
			case 'Z':
				e.Close()
			}
		}
	})
	r := NewRasterizer()
	var m Mask
	if !r.Rasterize(data, 24, px, &m) {
		t.Fatal("the circle produced no ink")
	}
	const s = px / 24.0
	cx, cy, rad := 12*s, 12*s, 8*s
	wrong := 0
	for y := range px {
		for x := range px {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			cov := at(&m, x, y)
			switch {
			case d < rad-1.5 && cov < 250:
				wrong++
			case d > rad+1.5 && cov > 5:
				wrong++
			}
		}
	}
	if wrong != 0 {
		t.Errorf("%d of %d pixels are on the wrong side of the circle the arcs describe",
			wrong, px*px)
	}
	// And the area, which catches a circle of the right shape and the wrong
	// size. pi*r^2 in pixels.
	a, _ := inked(&m)
	want := math.Pi * rad * rad
	if float64(a) < want*0.95 || float64(a) > want*1.1 {
		t.Errorf("the disc covers %d pixels, want about %.0f", a, want)
	}
}

// TestAStrokedLineIsAsWideAsItSays is the first stroke test and it measures
// the one number a stroker can silently halve or double.
func TestAStrokedLineIsAsWideAsItSays(t *testing.T) {
	for _, w := range []float32{1, 2, 3} {
		data := buildStroke(t, w, CapButt, JoinMiter, func(e *Encoder) {
			e.MoveTo(4, 12)
			e.LineTo(20, 12)
		})
		r := NewRasterizer()
		var m Mask
		const px = 96
		if !r.Rasterize(data, 24, px, &m) {
			t.Fatalf("width %v: no ink", w)
		}
		// Sum the coverage down the column through the middle of the line:
		// that is the stroke width in pixels, antialiasing included.
		sum := 0
		for y := range px {
			sum += at(&m, px/2, y)
		}
		got := float64(sum) / 255
		want := float64(w) * px / 24
		if math.Abs(got-want) > 0.1 {
			t.Errorf("a stroke-width of %v renders %.2f pixels wide at %d pixels per 24 units, "+
				"want %.2f", w, got, px, want)
		}
	}
}

// TestRoundCapsAndJoinsAreRound checks the shapes the corpus overwhelmingly
// asks for — 262 round caps and 257 round joins — by measuring where the ink
// ends beyond the geometry.
//
// A round cap puts a half disc beyond the endpoint, so the ink reaches half a
// stroke width past it and the corner of that square is empty. A butt cap
// stops at the endpoint. Both directions are checked, because a stroker that
// drew a square cap everywhere would pass a test that only looked at how far
// the ink reached.
func TestRoundCapsAndJoinsAreRound(t *testing.T) {
	const px, box = 96, 24.0
	s := float64(px) / box
	line := func(c Cap) *Mask {
		data := buildStroke(t, 4, c, JoinMiter, func(e *Encoder) {
			e.MoveTo(8, 12)
			e.LineTo(16, 12)
		})
		r := NewRasterizer()
		m := &Mask{}
		if !r.Rasterize(data, box, px, m) {
			t.Fatalf("cap %v: no ink", c)
		}
		return m
	}
	// Half a stroke width is 2 units, so a round or square cap reaches to
	// x = 6 units = 24 px, and a butt cap stops at x = 8 units = 32 px.
	butt, round, square := line(CapButt), line(CapRound), line(CapSquare)
	mid := int(12 * s)
	if got := at(butt, 26, mid); got > 5 {
		t.Errorf("a butt cap has coverage %d two units beyond the endpoint; it must stop there", got)
	}
	if got := at(round, 26, mid); got < 250 {
		t.Errorf("a round cap has coverage %d on the axis two units beyond the endpoint, want full", got)
	}
	if got := at(square, 26, mid); got < 250 {
		t.Errorf("a square cap has coverage %d two units beyond the endpoint, want full", got)
	}
	// The corner of the cap square is what separates round from square: at
	// x = 26 (one unit and a half beyond the endpoint) and y offset by nearly
	// two units, a square cap is solid and a round one is empty.
	cornerY := mid - int(1.8*s)
	if got := at(square, 26, cornerY); got < 250 {
		t.Errorf("a square cap has coverage %d at the corner of its square, want full", got)
	}
	if got := at(round, 26, cornerY); got > 5 {
		t.Errorf("a round cap has coverage %d at the corner of the square a square cap would "+
			"fill; it is not round, it is square", got)
	}
}

// TestAMiterJoinReachesTheCornerAndARoundOneDoesNot is the join counterpart,
// and it is the test that pays for the miter implementation.
//
// The brief that commissioned this work said the corpus has zero miter joins.
// That is true of the joins that are written down: 257 say round, six say
// nothing about caps, and *35 stroked figures do not mention a join at all* —
// and the SVG default is miter. So miter is not an unused branch, it is what a
// tenth of the stroked corpus asks for.
//
// A right angle of a stroke of width 4 has its outer corner at the miter
// point, one half width diagonally out from the vertex. A round join leaves
// that corner empty, a bevel cuts it off, a miter fills it.
func TestAMiterJoinReachesTheCornerAndARoundOneDoesNot(t *testing.T) {
	const px, box = 120, 24.0
	s := float64(px) / box
	corner := func(j Join) *Mask {
		data := buildStroke(t, 4, CapButt, j, func(e *Encoder) {
			e.MoveTo(6, 6)
			e.LineTo(18, 6)
			e.LineTo(18, 18)
		})
		r := NewRasterizer()
		m := &Mask{}
		if !r.Rasterize(data, box, px, m) {
			t.Fatalf("join %v: no ink", j)
		}
		return m
	}
	// The vertex is at (18,6). The outer side is up and to the right, so the
	// miter tip is at (20,4). A point just inside it, (19.5, 4.5), is covered
	// by a miter and by nothing else.
	x, y := int(19.5*s), int(4.5*s)
	if got := at(corner(JoinMiter), x, y); got < 250 {
		t.Errorf("a miter join has coverage %d just inside its miter tip, want full", got)
	}
	if got := at(corner(JoinRound), x, y); got > 5 {
		t.Errorf("a round join has coverage %d at the miter tip; a round join stops at the "+
			"disc of radius two around the vertex, which does not reach there", got)
	}
	if got := at(corner(JoinBevel), x, y); got > 5 {
		t.Errorf("a bevel join has coverage %d at the miter tip; a bevel is the straight cut "+
			"across the corner", got)
	}
	// And the inside of the corner must be solid for all three: a stroker
	// that forgot the join entirely leaves a notch there, which is the defect
	// this whole family of shapes exists to fill.
	ix, iy := int(17.0*s), int(7.0*s)
	for _, j := range []Join{JoinMiter, JoinRound, JoinBevel} {
		if got := at(corner(j), ix, iy); got < 250 {
			t.Errorf("join %v: coverage %d just inside the corner; there is a notch where the "+
				"two segments meet", j, got)
		}
	}
}

// TestAClosedStrokeHasAJoinAtItsStartingPoint is the case an off by one in the
// join loop drops, and it is invisible in anything but a close look: 143 of
// the corpus's stroked paths are closed, so a missing join at the point the
// path opened is a notch in a hundred and forty three icons.
func TestAClosedStrokeHasAJoinAtItsStartingPoint(t *testing.T) {
	const px, box = 120, 24.0
	s := float64(px) / box
	data := buildStroke(t, 4, CapButt, JoinRound, func(e *Encoder) {
		e.MoveTo(6, 6)
		e.LineTo(18, 6)
		e.LineTo(18, 18)
		e.LineTo(6, 18)
		e.Close()
	})
	r := NewRasterizer()
	var m Mask
	if !r.Rasterize(data, box, px, &m) {
		t.Fatal("no ink")
	}
	// The starting point (6,6) is the one vertex whose join only exists if
	// the closing segment is joined to the first one.
	if got := at(&m, int(6*s), int(6*s)); got < 250 {
		t.Errorf("coverage %d at the corner the closed path started from, want full: the join "+
			"between the closing segment and the first one is missing", got)
	}
	// All four corners, so that a fix which special cased this one is not
	// enough.
	for _, c := range [][2]float64{{6, 6}, {18, 6}, {18, 18}, {6, 18}} {
		if got := at(&m, int(c[0]*s), int(c[1]*s)); got < 250 {
			t.Errorf("coverage %d at corner %v", got, c)
		}
	}
}

// TestOverlappingStampsDoNotAddUp is the correctness argument of the stamping
// stroker, turned into a measurement.
//
// The stroke is built as a union of overlapping convex stamps, and the union
// is only free because the nonzero winding rule counts an overlap once. A
// stamp emitted with the wrong orientation would subtract instead — a hole
// down the middle of the stroke — and one counted twice would be invisible in
// the mask but would break the moment somebody drew the icon at partial alpha.
// The check is that no pixel of a heavily overlapped stroke is anything but
// empty or full.
func TestOverlappingStampsDoNotAddUp(t *testing.T) {
	// A zigzag with very tight turns, so that every join disc overlaps both
	// of its segment quads and its neighbours.
	pts := [][2]float64{{4, 12}, {8, 10}, {6, 14}, {10, 11}, {8, 13}, {12, 12}}
	data := buildStroke(t, 4, CapRound, JoinRound, func(e *Encoder) {
		e.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
		for _, p := range pts[1:] {
			e.LineTo(float32(p[0]), float32(p[1]))
		}
	})
	const px, box = 96, 24.0
	s := float64(px) / box
	r := NewRasterizer()
	var m Mask
	if !r.Rasterize(data, box, px, &m) {
		t.Fatal("no ink")
	}

	// The centre line of the stroke is at least half a width inside it, so
	// every point of it must be fully covered. A stamp emitted with the wrong
	// winding subtracts instead of adding, and the first thing that
	// disappears is the overlap between a join disc and its segments — which
	// is exactly where this samples densely.
	bad := 0
	for i := 1; i < len(pts); i++ {
		for k := range 200 {
			t0 := float64(k) / 199
			x := pts[i-1][0] + (pts[i][0]-pts[i-1][0])*t0
			y := pts[i-1][1] + (pts[i][1]-pts[i-1][1])*t0
			if at(&m, int(x*s), int(y*s)) < 255 {
				bad++
			}
		}
	}
	if bad != 0 {
		t.Errorf("%d of %d points on the centre line of the stroke are not fully covered. "+
			"The stroke is a union of overlapping stamps and the union is only free because "+
			"the nonzero rule counts an overlap once; a stamp with the opposite winding "+
			"subtracts, and the overlaps are the first thing to vanish",
			bad, 200*(len(pts)-1))
	}
	a, full := inked(&m)
	if full == 0 {
		t.Fatal("nothing is fully covered; the stamps are cancelling each other out")
	}
	// And the boundary is thin: on a shape this size only the outline may be
	// partially covered, so a large partial share means coverage is leaking
	// into the interior.
	partial := a - full
	if float64(partial) > 0.45*float64(a) {
		t.Errorf("%d of %d inked pixels are partially covered (%.0f %%); on a solid stroke only "+
			"the outline may be", partial, a, 100*float64(partial)/float64(a))
	}
	t.Logf("%d inked, %d full, %d partial", a, full, partial)
}

// TestAKnockoutRemovesCoverage is the test of [PaintErase], which exists for
// solid/visa.svg and for nothing else.
func TestAKnockoutRemovesCoverage(t *testing.T) {
	var e Encoder
	e.Figure(PaintFill)
	e.MoveTo(2, 2)
	e.LineTo(22, 2)
	e.LineTo(22, 22)
	e.LineTo(2, 22)
	e.Close()
	e.Figure(PaintErase)
	e.MoveTo(8, 8)
	e.LineTo(16, 8)
	e.LineTo(16, 16)
	e.LineTo(8, 16)
	e.Close()
	data, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRasterizer()
	var m Mask
	if !r.Rasterize(data, 24, 96, &m) {
		t.Fatal("no ink")
	}
	if got := at(&m, 48, 48); got != 0 {
		t.Errorf("the centre of the knocked out square has coverage %d, want 0", got)
	}
	if got := at(&m, 20, 20); got != 255 {
		t.Errorf("a point of the card outside the knockout has coverage %d, want 255", got)
	}
	// And the same icon *without* the knockout must be solid there, so that a
	// knockout which happened to be a no-op could not pass.
	solid := buildFill(t, PaintFill, func(e *Encoder) {
		e.MoveTo(2, 2)
		e.LineTo(22, 2)
		e.LineTo(22, 22)
		e.LineTo(2, 22)
		e.Close()
	})
	var m2 Mask
	r.Rasterize(solid, 24, 96, &m2)
	if got := at(&m2, 48, 48); got != 255 {
		t.Fatalf("the unknocked square is not solid at its centre (%d); the comparison is void", got)
	}
}

// TestARasterizerIsReusedWithoutLeakingState draws three different icons
// through one Rasterizer and one Mask, which is how the ui cache uses it, and
// checks that each result matches the same icon rasterised on its own.
//
// The scratch buffers — the polyline, the knockout plane, the coverage — are
// reused on purpose and that is what makes the miss path cheap; a leftover
// from the previous icon is the classic price.
func TestARasterizerIsReusedWithoutLeakingState(t *testing.T) {
	square := buildFill(t, PaintFill, func(e *Encoder) {
		e.MoveTo(6, 6)
		e.LineTo(18, 6)
		e.LineTo(18, 18)
		e.LineTo(6, 18)
		e.Close()
	})
	line := buildStroke(t, 2, CapRound, JoinRound, func(e *Encoder) {
		e.MoveTo(4, 12)
		e.LineTo(20, 12)
	})
	var withKnockout Encoder
	withKnockout.Figure(PaintFill)
	withKnockout.MoveTo(2, 2)
	withKnockout.LineTo(22, 2)
	withKnockout.LineTo(22, 22)
	withKnockout.LineTo(2, 22)
	withKnockout.Close()
	withKnockout.Figure(PaintErase)
	withKnockout.MoveTo(8, 8)
	withKnockout.LineTo(16, 8)
	withKnockout.LineTo(16, 16)
	withKnockout.LineTo(8, 16)
	withKnockout.Close()
	knock, err := withKnockout.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	blobs := [][]byte{square, line, knock, square, line}
	shared := NewRasterizer()
	var sm Mask
	for round := range 3 {
		for i, b := range blobs {
			if !shared.Rasterize(b, 24, 64, &sm) {
				t.Fatalf("round %d blob %d: no ink", round, i)
			}
			fresh := NewRasterizer()
			var fm Mask
			fresh.Rasterize(b, 24, 64, &fm)
			for k := range fm.Pix {
				if sm.Pix[k] != fm.Pix[k] {
					t.Fatalf("round %d blob %d: pixel %d is %d through a reused rasteriser and "+
						"%d through a fresh one; state leaked from the previous icon",
						round, i, k, sm.Pix[k], fm.Pix[k])
				}
			}
		}
	}
}

// TestAnIconWithNoInkIsReportedAsSuch is the negative result the cache needs:
// an empty shape must come back as false so that ui can remember "nothing" and
// stop rasterising it every frame, exactly as the glyph atlas caches a space.
func TestAnIconWithNoInkIsReportedAsSuch(t *testing.T) {
	r := NewRasterizer()
	var m Mask
	for _, tc := range []struct {
		name string
		data []byte
		box  float32
		px   int
	}{
		{"empty blob", nil, 24, 32},
		{"a degenerate subpath", buildFill(t, PaintFill, func(e *Encoder) {
			e.MoveTo(1, 1)
			e.Close()
		}), 24, 32},
		{"a zero width stroke", buildStroke(t, 0, CapRound, JoinRound, func(e *Encoder) {
			e.MoveTo(4, 12)
			e.LineTo(20, 12)
		}), 24, 32},
		{"a size of zero", buildFill(t, PaintFill, func(e *Encoder) {
			e.MoveTo(6, 6)
			e.LineTo(18, 18)
			e.Close()
		}), 24, 0},
		{"a size beyond the extent bound", buildFill(t, PaintFill, func(e *Encoder) {
			e.MoveTo(6, 6)
			e.LineTo(18, 18)
			e.Close()
		}), 24, MaxIconExtent + 1},
		{"a corrupt blob", []byte{0x7f, 0x01, 0x02}, 24, 32},
	} {
		if r.Rasterize(tc.data, tc.box, tc.px, &m) {
			t.Errorf("%s: reported ink", tc.name)
		}
		if m.Size != 0 {
			t.Errorf("%s: Size is %d after a failure, want 0", tc.name, m.Size)
		}
	}
}

// BenchmarkRasterizeAnIcon measures the miss path, which is what an icon costs
// the first time it appears at a given size and density. It is not the frame
// path — see ui — and it is here so that the cost of a miss is a number rather
// than a shrug.
func BenchmarkRasterizeAnIcon(b *testing.B) {
	data := buildStroke(b, 2, CapRound, JoinRound, func(e *Encoder) {
		e.MoveTo(7, 17)
		e.LineTo(7, 18)
		e.CubeTo(7, 18.55, 7.45, 19, 8, 19)
		e.LineTo(16, 19)
		e.CubeTo(16.55, 19, 17, 18.55, 17, 18)
		e.LineTo(17, 17)
		e.CubeTo(17, 15.34, 15.66, 14, 14, 14)
		e.LineTo(10, 14)
		e.CubeTo(8.34, 14, 7, 15.34, 7, 17)
		e.Close()
	})
	r := NewRasterizer()
	var m Mask
	b.ReportAllocs()
	for b.Loop() {
		r.Rasterize(data, 24, 40, &m)
	}
}
