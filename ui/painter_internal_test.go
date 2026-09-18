package ui

import (
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// TestNeedsPainterGateIsInstalled is an internal test on purpose.
//
// The external TestNilPainterFastPath only ever counted operations and clip
// indices, and a reviewer proved that it does not test what its name claims:
// replacing the needsPainter gate in element with an unconditionally installed
// painter left the entire suite green. A forwarding painter emits no
// operations, so no op count can see it. The thing that has to be observed is
// the field itself.
//
// This walks every combination of the four style inputs — background,
// border, corner radius and clip — and asserts both directions: a painter
// exactly when there is something to paint or clip, and none otherwise.
// Deleting the gate turns every "nothing to draw" row into a failure.
func TestNeedsPainterGateIsInstalled(t *testing.T) {
	opaque := RGB(1, 2, 3)
	clear := RGBA(1, 2, 3, 0)
	visibleBorder := Border{Width: 1, Color: opaque}
	clearBorder := Border{Width: 1, Color: clear}
	zeroWidthBorder := Border{Width: 0, Color: opaque}

	backgrounds := []struct {
		name string
		c    Color
		on   bool
	}{
		{"no background", Color{}, false},
		{"transparent background", clear, false},
		{"opaque background", opaque, true},
	}
	borders := []struct {
		name string
		b    Border
		on   bool
	}{
		{"no border", Border{}, false},
		{"zero width border", zeroWidthBorder, false},
		{"transparent border", clearBorder, false},
		{"visible border", visibleBorder, true},
	}
	radii := []float32{0, 6}
	clips := []bool{false, true}

	var withPainter, without int
	for _, bg := range backgrounds {
		for _, bd := range borders {
			for _, r := range radii {
				for _, cl := range clips {
					var b base
					b.setBackground(bg.c)
					b.setBorder(bd.b)
					b.setCornerRadius(r)
					b.setClip(cl)

					// A corner radius alone paints nothing: it is the shape of
					// a background and of a border, and without either there
					// is no shape to give a radius to.
					want := bg.on || bd.on || cl

					if got := b.style.needsPainter(); got != want {
						t.Errorf("needsPainter(%s, %s, radius %v, clip %v) = %v, want %v",
							bg.name, bd.name, r, cl, got, want)
					}
					el := element(b, kindBox, 0, 0, 0, nil)
					if got := el.Painter != nil; got != want {
						t.Errorf("element(%s, %s, radius %v, clip %v) installed a painter = %v, want %v; "+
							"a node with nothing to draw must hand gift a nil Painter so that gift takes its fast path",
							bg.name, bd.name, r, cl, got, want)
					}
					if want {
						withPainter++
					} else {
						without++
					}
				}
			}
		}
	}
	// Guard against a future refactor that makes every row trivially true or
	// trivially false and therefore untestable.
	if withPainter == 0 || without == 0 {
		t.Fatalf("the table degenerated: %d with a painter, %d without", withPainter, without)
	}
}

// TestPainterFastPathIsObservable is the behavioural half: gift must not run a
// painter for a node that does not have one, and PaintedNodes is where that
// becomes a number.
func TestPainterFastPathIsObservable(t *testing.T) {
	// Two styled boxes, two unstyled structural stacks and the root component
	// node, which gift paints itself.
	v := VStack(
		Box().Frame(10, 10).Background(RGB(1, 2, 3)),
		HStack(Box().Frame(4, 4).Background(RGB(1, 2, 3))),
	)
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View { return v }})
	if err := a.Update(geom.Sz(100, 100)); err != nil {
		t.Fatal(err)
	}
	a.Paint()

	d := a.Diagnostics()
	if d.LiveNodes != 5 {
		t.Fatalf("LiveNodes = %d, want 5", d.LiveNodes)
	}
	// The two boxes plus the root component node, which gift paints itself.
	// If the gate is removed, the two stacks install a painter as well and
	// this count rises to five.
	if d.PaintedNodes != 3 {
		t.Fatalf("PaintedNodes = %d, want the 2 styled boxes and the root; "+
			"a structural container without style must not install a painter", d.PaintedNodes)
	}
}
