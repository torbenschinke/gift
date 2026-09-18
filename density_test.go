package gift_test

import (
	"math"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// TestRoundDensity pins the one rounding of the project plan, section 18.
//
// Fractional DPI scaling is excluded by section 14, so a fractional factor is
// not an error and not an approximation: it is rounded to the nearest integer
// and the result is what the frame is actually drawn at. The table states what
// a display reporting each of these gets, which is the documentation section
// 18 asks for in place of a claim of accuracy.
func TestRoundDensity(t *testing.T) {
	cases := []struct {
		in   float64
		want float32
	}{
		{0, 1},        // no monitor, or a platform that reports nothing
		{-3, 1},       // nonsense from the platform
		{1, 1},        // the Raspberry Pi of section 1
		{1.25, 1},     // a Windows 125 % display rounds down and stays as it was
		{1.4, 1},      // still down
		{1.5, 2},      // half rounds away from zero: supersampled, see [gift.App.SetDensity]
		{1.75, 2},     //
		{2, 2},        // the Retina case this work unit exists for
		{2.5, 3},      //
		{3, 3},        //
		{4, 4},        // the cap, exactly
		{8, 4},        // clamped rather than believed
		{1e300, 4},    //
		{nan(), 1},    // a NaN key would never equal itself; see the atlas
		{posInf(), 4}, //
	}
	for _, c := range cases {
		if got := gift.RoundDensity(c.in); got != c.want {
			t.Errorf("RoundDensity(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func nan() float64    { var z float64; return z / z }
func posInf() float64 { return math.Inf(1) }

// densitySize is the viewport of the scene below. Its value is irrelevant; it
// only has to be bounded.
func densitySize() geom.Size { return geom.Sz(200, 100) }

var densitySceneType = gift.RegisterType("test.DensityScene")

// TestDensityOneChangesNothing is the regression the project plan, section 18,
// implicitly demands: the target platform is a Pi at density 1, so a frame at
// density 1 must be the frame gift drew before densities existed — the same
// operations, the same bounds, and in particular *no* transform at the root.
//
// A scale of one in the transform table would be harmless arithmetically and
// would still be a change: it would send every operation down the general
// transform path in the backend and would make the identity check of
// [render.List.Xform] index 0 stop being true of the root.
func TestDensityOneChangesNothing(t *testing.T) {
	before := paintOps(t, 0)
	after := paintOps(t, 1)
	if len(before) != len(after) {
		t.Fatalf("op count changed: %d without SetDensity, %d at density 1", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("op %d differs:\n before %+v\n  after %+v", i, before[i], after[i])
		}
		if before[i].Xform != 0 {
			t.Fatalf("op %d has transform index %d at density 1; the root must stay the identity sentinel",
				i, before[i].Xform)
		}
	}
}

// TestDensityScalesTheRootTransformOnly states the division of labour of
// [gift.App.SetDensity]: the display list is scaled at its root and the
// operations inside it are untouched, because the layout they came from is in
// logical pixels at every density.
func TestDensityScalesTheRootTransformOnly(t *testing.T) {
	one := paintOps(t, 1)
	two := paintOps(t, 2)
	if len(one) != len(two) {
		t.Fatalf("op count changed with the density: %d at 1x, %d at 2x", len(one), len(two))
	}
	for i := range one {
		if one[i].Bounds != two[i].Bounds {
			t.Errorf("op %d: bounds %v at 1x but %v at 2x; layout must not depend on the density",
				i, one[i].Bounds, two[i].Bounds)
		}
		if two[i].Xform == 0 {
			t.Errorf("op %d is on the identity transform at 2x", i)
		}
	}
}

// paintOps lays out a small scene at the given density and returns its
// operations. A density of zero means SetDensity is never called at all, which
// is the state of every App built before this work unit.
func paintOps(t *testing.T, density float64) []render.Op {
	t.Helper()
	app := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return densityScene{}
	}})
	if density != 0 {
		app.SetDensity(density)
	}
	if err := app.Update(densitySize()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	l := app.Paint()
	out := make([]render.Op, len(l.Ops()))
	copy(out, l.Ops())
	// The transform itself, so that a caller can assert on the scale without
	// reaching into the list after the next frame reset it.
	for i := range out {
		if out[i].Xform != 0 {
			m := l.Xform(out[i].Xform)
			if m.A != float32(density) || m.D != float32(density) {
				t.Fatalf("op %d is under transform %+v, want a uniform scale of %v", i, m, density)
			}
		}
	}
	return out
}

// densityScene is a minimal view: one node that paints one rectangle. It is
// deliberately not a ui view — this package must not import ui — and one
// operation is enough, because the claim is about the transform and not about
// what is drawn.
type densityScene struct{}

func (densityScene) ViewType() gift.TypeID { return densitySceneType }

func (densityScene) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: densityScene{}, Painter: densityScene{}}
}

func (densityScene) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	ctx.ReportOverflow(geom.Size{})
	return c.Constrain(geom.Sz(40, 20))
}

func (densityScene) Paint(ctx *gift.PaintContext) {
	ctx.Add(render.Op{Kind: render.OpFillRect, Bounds: ctx.Bounds(), Color: render.RGB(1, 2, 3)})
}
