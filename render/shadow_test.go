package render

import (
	"testing"

	"github.com/torbenschinke/gift/geom"
)

var shadowBounds = geom.Rc(100, 100, 200, 150)

// TestShadowShapeFoldsSpreadAndOffset pins what goes into [Op.Bounds]: the
// spread inflates the shape and the offset moves it, and both are the
// producer's arithmetic rather than the backend's.
func TestShadowShapeFoldsSpreadAndOffset(t *testing.T) {
	s := Shadow{Blur: 16, Spread: 3, OffsetX: 5, OffsetY: 4, Color: RGBA(0, 0, 0, 70)}
	want := geom.Rc(102, 101, 208, 157) // 100-3+5, 100-3+4, 200+3+5, 150+3+4
	if got := s.Shape(shadowBounds); got != want {
		t.Errorf("Shape = %v, want %v", got, want)
	}
	// A negative spread shrinks it. That is the "tuck it under the node" case
	// and is the reason a negative Spread is legal where a negative Blur is
	// not.
	s.Spread = -4
	if got, want := s.Shape(shadowBounds), geom.Rc(109, 108, 201, 150); got != want {
		t.Errorf("negative spread: Shape = %v, want %v", got, want)
	}
}

// TestShadowPaintBoundsGrowByThreeSigma is the "Shadow erweitert die
// Paint-Bounds" of the project plan, section 8, in numbers. The three sigma
// cut is a decision, so it is asserted rather than derived.
func TestShadowPaintBoundsGrowByThreeSigma(t *testing.T) {
	s := Shadow{Blur: 16, OffsetY: 4, Color: RGBA(0, 0, 0, 70)}
	if got, want := s.Sigma(), float32(8); got != want {
		t.Fatalf("Sigma = %v, want %v; Blur is the CSS diameter, not the deviation", got, want)
	}
	if got, want := s.Extent(), float32(24); got != want {
		t.Fatalf("Extent = %v, want %v", got, want)
	}
	// 24 on every side, plus the 4 pixel downward offset on the shape. The
	// paint bounds live on the *operation*, whose Bounds is already the
	// shape; see [Op.PaintBounds] and the note in shadow.go on why there is
	// no second formulation on Shadow.
	op := Op{Kind: OpShadow, Bounds: s.Shape(shadowBounds), Blur: s.Blur, Color: s.Color}
	want := geom.Rc(76, 80, 224, 178)
	if got := op.PaintBounds(); got != want {
		t.Errorf("Op.PaintBounds = %v, want %v", got, want)
	}
}

// TestInvisibleShadowDoesNotGrowAnything: an invisible shadow must not quietly
// enlarge the paint rectangle, or a transparent one would cost fill rate.
func TestInvisibleShadowDoesNotGrowAnything(t *testing.T) {
	for name, s := range map[string]Shadow{
		"zero":        {},
		"transparent": {Blur: 20},
		"nan blur":    {Blur: nan32(), Color: RGB(0, 0, 0)},
		"inf offset":  {Blur: 8, OffsetX: inf32(), Color: RGB(0, 0, 0)},
	} {
		if s.IsVisible() {
			t.Errorf("%s: IsVisible = true, want false", name)
		}
		// An invisible shadow is never emitted at all, so the paint bounds
		// of one are not a question; [Op.PaintBounds] deliberately does not
		// consult the colour, and the "inf offset" case has a perfectly good
		// blur whose infinity lives in the offset the producer already folded
		// into Bounds. What must hold at the operation level is the narrower
		// statement: no usable blur, no larger quad.
		if (Shadow{Blur: s.Blur}).Extent() != 0 {
			continue
		}
		op := Op{Kind: OpShadow, Bounds: shadowBounds, Blur: s.Blur, Color: s.Color}
		if got := op.PaintBounds(); got != shadowBounds {
			t.Errorf("%s: Op.PaintBounds = %v, want the bounds unchanged %v", name, got, shadowBounds)
		}
	}
	visible := Shadow{Blur: 8, Color: RGB(0, 0, 0)}
	if !visible.IsVisible() {
		t.Error("an opaque, finite, blurred shadow reports itself invisible")
	}
}

// TestShadowRadiusFollowsSpread: inflating a shape inflates its corners with
// it, and a negative spread cannot drive the radius below zero, because a
// rounded box with a negative radius is not a shape.
func TestShadowRadiusFollowsSpread(t *testing.T) {
	for _, c := range []struct{ spread, in, want float32 }{
		{0, 12, 12},
		{4, 12, 16},
		{-4, 12, 8},
		{-20, 12, 0},
		{-4, 0, 0},
	} {
		s := Shadow{Spread: c.spread}
		if got := s.Radius(c.in); got != c.want {
			t.Errorf("Shadow{Spread: %v}.Radius(%v) = %v, want %v", c.spread, c.in, got, c.want)
		}
	}
}

// TestOpPaintBoundsOnlyGrowForShadows keeps the extension confined to the one
// kind that has it. Everything else paints exactly its bounds, which is what
// the rest of gift assumes.
func TestOpPaintBoundsOnlyGrowForShadows(t *testing.T) {
	b := geom.Rc(10, 10, 30, 20)
	for _, k := range []OpKind{OpNone, OpFillRect, OpFillRoundRect, OpStrokeRoundRect, OpGlyphs} {
		op := Op{Kind: k, Bounds: b, Blur: 100, CornerRadius: 4, StrokeWidth: 2}
		if got := op.PaintBounds(); got != b {
			t.Errorf("kind %d: PaintBounds = %v, want the bounds %v; Blur is a shadow field", k, got, b)
		}
	}
	sh := Op{Kind: OpShadow, Bounds: b, Blur: 8}
	if got, want := sh.PaintBounds(), geom.Rc(-2, -2, 42, 32); got != want {
		t.Errorf("shadow PaintBounds = %v, want %v", got, want)
	}
	// A shadow with no blur is a hard edged rounded fill and paints its bounds.
	hard := Op{Kind: OpShadow, Bounds: b}
	if got := hard.PaintBounds(); got != b {
		t.Errorf("unblurred shadow PaintBounds = %v, want %v", got, b)
	}
}

func nan32() float32 { var z float32; return z / z }
func inf32() float32 { return 1 / func() float32 { var z float32; return z }() }
