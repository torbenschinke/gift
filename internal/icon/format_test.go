package icon

import (
	"math"
	"testing"
)

// recorder is a [Sink] that writes down everything it is told, so that a test
// can compare an encode-decode round trip point by point.
type recorder struct {
	figs []recFigure
}

type recFigure struct {
	p    Paint
	w    float32
	c    Cap
	j    Join
	segs []recSeg
}

type recSeg struct {
	op byte
	xy [6]float32
	n  int
}

func (r *recorder) Figure(p Paint, w float32, c Cap, j Join) {
	r.figs = append(r.figs, recFigure{p: p, w: w, c: c, j: j})
}
func (r *recorder) push(op byte, n int, xy ...float32) {
	f := &r.figs[len(r.figs)-1]
	var s recSeg
	s.op, s.n = op, n
	copy(s.xy[:], xy)
	f.segs = append(f.segs, s)
}
func (r *recorder) MoveTo(x, y float32) { r.push('M', 2, x, y) }
func (r *recorder) LineTo(x, y float32) { r.push('L', 2, x, y) }
func (r *recorder) CubeTo(a, b, c, d, e, f float32) {
	r.push('C', 6, a, b, c, d, e, f)
}
func (r *recorder) Close() { r.push('Z', 0) }

// TestTheEncodingSurvivesARoundTrip is the unit test of the byte format, and
// it checks the *values*, not merely that the decode does not error.
//
// The coordinates are chosen so that the deltas span one byte varints,
// multi byte ones and both signs, and so that they exercise the fixed point
// quantum: 1/128 of a unit is representable exactly, 1/256 is not and must
// round to the nearer of its neighbours.
func TestTheEncodingSurvivesARoundTrip(t *testing.T) {
	var e Encoder
	e.Figure(PaintFill)
	e.MoveTo(0, 0)
	e.LineTo(23.9921875, 0) // 3071/128, exactly representable
	e.CubeTo(1, -2, -3.5, 4.25, 12, 12)
	e.Close()
	e.StrokeFigure(2, CapRound, JoinRound)
	e.MoveTo(-5, 5)
	e.LineTo(200.5, -200.5)
	e.Figure(PaintErase)
	e.MoveTo(1, 1)
	e.Close()
	data, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	var r recorder
	if err := Walk(data, &r); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(r.figs) != 3 {
		t.Fatalf("decoded %d figures, encoded 3", len(r.figs))
	}
	if r.figs[0].p != PaintFill || r.figs[2].p != PaintErase {
		t.Errorf("paints came back as %v, %v, %v", r.figs[0].p, r.figs[1].p, r.figs[2].p)
	}
	if f := r.figs[1]; f.p != PaintStroke || f.w != 2 || f.c != CapRound || f.j != JoinRound {
		t.Errorf("the stroke figure came back as %+v, want width 2, round cap, round join", f)
	}
	want := [][]recSeg{
		{
			{op: 'M', n: 2, xy: [6]float32{0, 0}},
			{op: 'L', n: 2, xy: [6]float32{23.9921875, 0}},
			{op: 'C', n: 6, xy: [6]float32{1, -2, -3.5, 4.25, 12, 12}},
			{op: 'Z'},
		},
		{
			{op: 'M', n: 2, xy: [6]float32{-5, 5}},
			{op: 'L', n: 2, xy: [6]float32{200.5, -200.5}},
		},
		{
			{op: 'M', n: 2, xy: [6]float32{1, 1}},
			{op: 'Z'},
		},
	}
	for i := range want {
		got := r.figs[i].segs
		if len(got) != len(want[i]) {
			t.Errorf("figure %d: %d segments, want %d", i, len(got), len(want[i]))
			continue
		}
		for k := range got {
			if got[k].op != want[i][k].op {
				t.Errorf("figure %d segment %d: op %q, want %q", i, k, got[k].op, want[i][k].op)
			}
			for c := range want[i][k].n {
				if got[k].xy[c] != want[i][k].xy[c] {
					t.Errorf("figure %d segment %d coordinate %d: %v, want %v",
						i, k, c, got[k].xy[c], want[i][k].xy[c])
				}
			}
		}
	}
	t.Logf("%d bytes for %d figures and %d segments", len(data), 3, 8)
}

// TestTheEncodingRoundsToItsQuantumAndSaysSo pins the precision of the format
// rather than leaving it to a comment. A coordinate lands on the nearest
// 1/128 of a unit, so the error is never more than 1/256 — about 1/64 of a
// pixel at the largest size an icon can be drawn at.
func TestTheEncodingRoundsToItsQuantumAndSaysSo(t *testing.T) {
	var e Encoder
	e.Figure(PaintFill)
	worst := float64(0)
	var in []float32
	for i := range 400 {
		v := float32(i) * 0.06
		in = append(in, v)
		e.MoveTo(v, -v)
	}
	data, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var r recorder
	if err := Walk(data, &r); err != nil {
		t.Fatal(err)
	}
	for i, s := range r.figs[0].segs {
		worst = math.Max(worst, math.Abs(float64(s.xy[0]-in[i])))
	}
	if worst > 1.0/(2*UnitScale)+1e-6 {
		t.Errorf("worst coordinate error %v, the quantum admits at most %v", worst, 1.0/(2*UnitScale))
	}
	t.Logf("worst coordinate error over 400 values: %v of a unit (quantum %v)", worst, 1.0/UnitScale)
}

// TestACoordinateOutsideTheRangeIsRefused checks that the format fails loudly
// rather than wrapping. A wrapped coordinate is an icon with one vertex on the
// far side of the viewBox, which looks like a stray line across the artwork.
func TestACoordinateOutsideTheRangeIsRefused(t *testing.T) {
	var e Encoder
	e.Figure(PaintFill)
	e.MoveTo(0, 0)
	e.LineTo(1000, 0)
	if _, err := e.Bytes(); err == nil {
		t.Fatal("a coordinate of 1000 units was accepted; the signed 16 bit fixed point form " +
			"reaches 256 and it would have wrapped")
	}
}

// TestAMalformedBlobIsAnErrorAndNotAPanic is the outside-world-input rule of
// the project plan, section 15, applied to a byte slice that could come from
// anywhere: every truncation of a valid icon and every corrupted tag has to
// come back as an error.
func TestAMalformedBlobIsAnErrorAndNotAPanic(t *testing.T) {
	var e Encoder
	e.StrokeFigure(2, CapSquare, JoinBevel)
	e.MoveTo(1, 1)
	e.CubeTo(2, 2, 3, 3, 4, 4)
	e.Close()
	good, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for n := range len(good) {
		var r recorder
		if err := Walk(good[:n], &r); err == nil && n != 0 {
			t.Errorf("a blob truncated to %d of %d bytes decoded without complaint", n, len(good))
		}
	}
	for i := range good {
		bad := append([]byte(nil), good...)
		bad[i] ^= 0xff
		var r recorder
		// Not every bit flip is detectable — a flipped coordinate is a valid
		// coordinate — so the assertion is only that it does not panic.
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("byte %d flipped: Walk panicked with %v", i, p)
				}
			}()
			_ = Walk(bad, &r)
		}()
	}
}
