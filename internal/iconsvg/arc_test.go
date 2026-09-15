package iconsvg_test

import (
	"math"
	"testing"

	"github.com/torbenschinke/gift/internal/iconsvg"
)

// The elliptical arc is the most common command in the corpus — 2875 relative
// and 380 absolute, more than every other curve command put together — and it
// is the one whose conversion has a change of parameterisation in it, so a
// mistake produces a plausible wrong shape rather than a parse failure. These
// tests therefore check the *geometry the arc produces*, not merely that it
// parses.

// evalPath samples the resolved path at n points per segment and returns them.
func evalPath(t *testing.T, d string, n int) [][2]float64 {
	t.Helper()
	ops, err := iconsvg.ParsePathData(d)
	if err != nil {
		t.Fatalf("%q: %v", d, err)
	}
	var out [][2]float64
	var cur [2]float64
	for _, op := range ops {
		switch op.Kind {
		case 'M':
			cur = op.P[0]
			out = append(out, cur)
		case 'L':
			cur = op.P[0]
			out = append(out, cur)
		case 'C':
			p0 := cur
			for i := 1; i <= n; i++ {
				tt := float64(i) / float64(n)
				u := 1 - tt
				a, b, c, e := u*u*u, 3*u*u*tt, 3*u*tt*tt, tt*tt*tt
				out = append(out, [2]float64{
					a*p0[0] + b*op.P[0][0] + c*op.P[1][0] + e*op.P[2][0],
					a*p0[1] + b*op.P[0][1] + c*op.P[1][1] + e*op.P[2][1],
				})
			}
			cur = op.P[2]
		}
	}
	return out
}

// TestACircularArcStaysOnItsCircle is the direct check that the conversion
// from SVG's endpoint parameterisation to cubics produces the arc the data
// asks for and not merely a curve between the same two points.
//
// Every sampled point of a circular arc must be at the arc's radius from its
// centre. A conversion that mixed up the centre, the rotation or the kappa of
// the cubic construction would still pass through both endpoints — that is
// what makes this failure mode hard to see — but it would leave the circle in
// between, which is all this test looks at.
//
// The tolerance is 0.0005 of the radius. The theoretical worst case radial
// error of the quarter-turn cubic approximation is about 2.7e-4 of the radius,
// so this admits it and rejects anything an order of magnitude larger.
func TestACircularArcStaysOnItsCircle(t *testing.T) {
	const r = 8.0
	cx, cy := 12.0, 12.0
	for _, tc := range []struct {
		name string
		d    string
	}{
		// From (4,12) to (20,12): the two halves of the circle, each of which
		// can be reached with two of the four flag combinations.
		{"small sweep", "M4 12A8 8 0 0 1 20 12"},
		{"small antisweep", "M4 12A8 8 0 0 0 20 12"},
		{"large sweep", "M4 12A8 8 0 1 1 20 12"},
		{"large antisweep", "M4 12A8 8 0 1 0 20 12"},
		// The same as a relative arc, which is 2875 of the corpus's 3255.
		{"relative", "M4 12a8 8 0 0 1 16 0"},
		// A full circle as two half arcs, the idiom the corpus uses.
		{"full circle", "M4 12a8 8 0 1 0 16 0 8 8 0 1 0-16 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pts := evalPath(t, tc.d, 32)
			if len(pts) < 10 {
				t.Fatalf("only %d sample points, the arc produced nothing", len(pts))
			}
			worst := 0.0
			for _, p := range pts {
				e := math.Abs(math.Hypot(p[0]-cx, p[1]-cy)-r) / r
				worst = math.Max(worst, e)
			}
			if worst > 5e-4 {
				t.Errorf("a point of the arc is %.5f of the radius off the circle; the arc does "+
					"not lie on the circle its own radii describe", worst)
			}
			t.Logf("worst radial error %.2e of the radius over %d samples", worst, len(pts))
		})
	}
}

// TestTheArcFlagsSelectTheRightOneOfTheFourArcs is the other half, and it is
// the half a radius check cannot do: four different arcs join the same two
// points on the same circle, and large-arc-flag and sweep-flag are what pick
// one. Getting the sign of either wrong gives an arc that is still perfectly
// on the circle and is still the wrong picture — a chevron pointing the other
// way, which is a large fraction of what this corpus draws.
//
// The discriminator is the midpoint of the traced arc: for the four
// combinations of the flags on a chord from (4,12) to (20,12) it must be
// above, below, below and above the chord respectively, and the arc must be a
// half circle in each case, so the midpoint is exactly at distance r from the
// chord.
func TestTheArcFlagsSelectTheRightOneOfTheFourArcs(t *testing.T) {
	for _, tc := range []struct {
		d      string
		wantY  float64
		reason string
	}{
		{"M4 12A8 8 0 0 1 20 12", 4, "sweep 1 is clockwise in SVG's y-down space, so it goes over the top"},
		{"M4 12A8 8 0 0 0 20 12", 20, "sweep 0 is anticlockwise, so it goes under the bottom"},
		{"M4 12A8 8 0 1 1 20 12", 4, "the two halves are the same size, so large-arc cannot change the choice"},
		{"M4 12A8 8 0 1 0 20 12", 20, "likewise"},
	} {
		pts := evalPath(t, tc.d, 64)
		mid := pts[len(pts)/2]
		if math.Abs(mid[0]-12) > 0.05 {
			t.Errorf("%s: the midpoint of the arc is at x=%v, not over the centre of the chord", tc.d, mid[0])
		}
		if math.Abs(mid[1]-tc.wantY) > 0.05 {
			t.Errorf("%s: the arc passes through y=%v at its midpoint, want %v — %s",
				tc.d, mid[1], tc.wantY, tc.reason)
		}
	}
}

// TestALargeArcIsTheLongWayRound uses a chord that is not a diameter, which is
// the only configuration in which the large-arc flag actually chooses
// something. The two endpoints below sit an eighth of a turn apart on a circle
// of radius 8 centred on (12,12), so the two arcs between them are 2pi/8 and
// 14pi/8 of its circumference — a factor of seven, which no rounding or
// flattening error can blur.
func TestALargeArcIsTheLongWayRound(t *testing.T) {
	const r = 8.0
	short := arcLength(evalPath(t, "M12 4A8 8 0 0 1 17.657 6.343", 256))
	long := arcLength(evalPath(t, "M12 4A8 8 0 1 1 17.657 6.343", 256))
	if want := r * math.Pi / 4; math.Abs(short-want) > 0.02 {
		t.Errorf("the short arc is %.3f long, want %.3f", short, want)
	}
	if want := r * 7 * math.Pi / 4; math.Abs(long-want) > 0.02 {
		t.Errorf("the large-arc flag produced an arc %.3f long, want %.3f. A large arc that comes "+
			"out short means the flag was ignored or the sweep was reversed", long, want)
	}
}

// TestARotatedEllipseHonoursItsAxes checks the one arc parameter the corpus
// never exercises and the specification still defines: x-axis-rotation. A
// rotated ellipse is not on any circle, so the check is that its points
// satisfy the ellipse equation in the rotated frame.
func TestARotatedEllipseHonoursItsAxes(t *testing.T) {
	// An ellipse with rx=8, ry=4, rotated 30 degrees, traced as a full turn
	// from (12,12) through the two half arcs.
	const rx, ry, deg = 8.0, 4.0, 30.0
	phi := deg * math.Pi / 180
	// The two endpoints are the ends of the major axis of that ellipse
	// centred on (12,12): (12,12) +/- 8*(cos30, sin30).
	pts := evalPath(t, "M18.9282 16 A8 4 30 1 0 5.0718 8 A8 4 30 1 0 18.9282 16", 64)
	cx, cy := 12.0, 12.0
	worst := 0.0
	for _, p := range pts {
		dx, dy := p[0]-cx, p[1]-cy
		u := dx*math.Cos(phi) + dy*math.Sin(phi)
		v := -dx*math.Sin(phi) + dy*math.Cos(phi)
		worst = math.Max(worst, math.Abs(u*u/(rx*rx)+v*v/(ry*ry)-1))
	}
	if worst > 2e-3 {
		t.Errorf("a point of the rotated ellipse misses its own equation by %.2e; "+
			"x-axis-rotation is not being applied correctly", worst)
	}
}

// TestOutOfRangeRadiiAreScaledUp pins the one repair the specification
// mandates: radii too small to span the chord are scaled up uniformly until
// they just reach, which makes the arc a half turn of the enlarged ellipse.
// Dropping the repair makes the square root under the centre calculation
// negative, and the usual accident is a NaN that propagates into every vertex
// of the icon.
func TestOutOfRangeRadiiAreScaledUp(t *testing.T) {
	// A chord of length 16 with radii of 1: the radii must be scaled to 8.
	pts := evalPath(t, "M4 12A1 1 0 0 1 20 12", 64)
	for _, p := range pts {
		if math.IsNaN(p[0]) || math.IsNaN(p[1]) {
			t.Fatalf("a NaN vertex: the out of range radius repair is missing")
		}
	}
	mid := pts[len(pts)/2]
	if math.Abs(mid[1]-4) > 0.05 {
		t.Errorf("the arc's midpoint is at y=%v, want 4: the radii were not scaled to 8", mid[1])
	}
}

// TestAZeroRadiusArcIsALine is the other degenerate the specification names.
func TestAZeroRadiusArcIsALine(t *testing.T) {
	ops, err := iconsvg.ParsePathData("M4 12A0 8 0 0 1 20 12")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[1].Kind != 'L' || ops[1].P[0] != [2]float64{20, 12} {
		t.Errorf("a zero radius arc produced %v, want a move and a line to (20,12)", ops)
	}
}

// TestCompressedArcFlagsParse is the lexical trap of the arc command and it is
// in the corpus: SVG's grammar makes the two flags single characters, so
// "a1 1 0 104-4" means large-arc=1, sweep=0, dx=4, dy=-4. Reading the flags as
// ordinary numbers swallows "104" and fails three values later complaining
// about something else entirely.
func TestCompressedArcFlagsParse(t *testing.T) {
	loose := evalPath(t, "M4 12a8 8 0 1 0 16 0", 16)
	tight := evalPath(t, "M4 12a8 8 0 10 16 0", 16)
	if len(loose) != len(tight) {
		t.Fatalf("the compressed form produced %d points and the spaced form %d", len(tight), len(loose))
	}
	for i := range loose {
		if math.Abs(loose[i][0]-tight[i][0]) > 1e-9 || math.Abs(loose[i][1]-tight[i][1]) > 1e-9 {
			t.Fatalf("point %d differs: %v spaced, %v compressed", i, loose[i], tight[i])
		}
	}
}

func arcLength(pts [][2]float64) float64 {
	l := 0.0
	for i := 1; i < len(pts); i++ {
		l += math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
	}
	return l
}
