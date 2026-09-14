package render

import "testing"

// TestMaterialSideTableIndices is the headless half of "a material op reaches
// the display list with the right side table index": no backend, no GPU, just
// the mechanism, in the same spirit as TestGlyphSideTableIndices next door.
func TestMaterialSideTableIndices(t *testing.T) {
	var l List
	l.Reset()

	if got := l.MaterialsLen(); got != 1 {
		t.Errorf("a fresh list has %d materials, want 1 (the sentinel)", got)
	}
	if got := l.Material(0); got.Kind != MaterialNone {
		t.Errorf("index 0 is %v, want none", got.Kind)
	}
	// The sentinel is what makes "no material" expressible without a flag,
	// so an unset Op.Material must resolve to it.
	if got := l.Material(Op{}.Material); got.IsVisible() {
		t.Error("the zero Op resolves to a visible material")
	}

	a := l.AddMaterial(NewGlass().Blur(8).Material())
	b := l.AddMaterial(NewGlass().Blur(24).Material())
	if a == 0 || b == 0 || a == b {
		t.Fatalf("indices %d and %d; a material must get a distinct non zero index", a, b)
	}
	if got := l.Material(a).Glass.Blur; got != 8 {
		t.Errorf("material %d has blur %v, want 8", a, got)
	}
	if got := l.Material(b).Glass.Blur; got != 24 {
		t.Errorf("material %d has blur %v, want 24", b, got)
	}

	// An invisible material costs no entry, so a painter may call AddMaterial
	// unconditionally.
	if got := l.AddMaterial(Material{}); got != 0 {
		t.Errorf("AddMaterial of the zero material returned %d, want 0", got)
	}

	// Out of range is the sentinel and not a panic, under the same rule as
	// List.Glyphs: a backend reading a malformed list draws nothing rather
	// than taking the process down.
	if got := l.Material(9999); got.IsVisible() {
		t.Error("an out of range material index resolved to something visible")
	}

	// Reset empties the table but keeps the sentinel, or the first material
	// of the next frame would land at index 0 and mean "none".
	l.Reset()
	if got := l.MaterialsLen(); got != 1 {
		t.Errorf("after Reset the table has %d entries, want 1", got)
	}
	if got := l.AddMaterial(NewGlass().Material()); got != 1 {
		t.Errorf("the first material after Reset got index %d, want 1", got)
	}
}

// TestMaterialTableIsReused is the allocation property the display list rests
// on, applied to the new side table.
func TestMaterialTableIsReused(t *testing.T) {
	var l List
	l.Reset()
	for range 8 {
		l.AddMaterial(NewGlass().Material())
	}
	l.Reset()
	c := cap(l.materials)
	if got := testing.AllocsPerRun(100, func() {
		l.Reset()
		for range 8 {
			l.AddMaterial(NewGlass().Material())
		}
	}); got != 0 {
		t.Errorf("%v allocations per frame, want 0", got)
	}
	if cap(l.materials) != c {
		t.Errorf("the backing array was reallocated, %d to %d", c, cap(l.materials))
	}
}

// TestGlassZeroValueIsTheDefaults pins the one thing that distinguishes Glass
// from Border and Shadow: a zero Glass is the default material and not an
// invisible one.
//
// It matters because ui stores a resolved [Material] in a struct field and has
// no "was it set" flag next to it. If Glass{} meant "no blur, no tint, no
// highlight", a caller who wrote ui.Glass() and never touched a setter would
// get an invisible pane, and there would be nothing in the display list to
// explain it.
func TestGlassZeroValueIsTheDefaults(t *testing.T) {
	var zero Glass
	if got, want := zero.Params(), NewGlass().Params(); got != want {
		t.Errorf("the zero Glass is %+v, want %+v", got, want)
	}
	// A level set on the zero value survives the substitution, which is the
	// case that made the flag necessary in the first place:
	// Glass{}.Quality(Full) must be the defaults *at Full*.
	if got := (Glass{}).Quality(Full).Params(); got.Level != Full || got.Blur != DefaultGlassBlur {
		t.Errorf("Glass{}.Quality(Full) = %+v, want the defaults at Full", got)
	}
}

// TestGlassSettersClamp. The clamps are not cosmetic: the backend packs
// refraction, highlight and grain into one float32 vertex attribute with eight
// bits each, and a value outside the range would wrap into a neighbouring
// field. The failure would be a bright streak in a corner with nothing in the
// code to explain it, so the range is enforced where the number enters.
func TestGlassSettersClamp(t *testing.T) {
	g := NewGlass().Refraction(1e9).Highlight(5).Grain(-1).Blur(1e9)
	p := g.Params()
	if p.Refraction != 63 {
		t.Errorf("Refraction = %v, want 63", p.Refraction)
	}
	if p.Highlight != 1 {
		t.Errorf("Highlight = %v, want 1", p.Highlight)
	}
	if p.Grain != 0 {
		t.Errorf("Grain = %v, want 0", p.Grain)
	}
	if p.Blur != 64 {
		t.Errorf("Blur = %v, want 64", p.Blur)
	}
	// NaN goes to the low end rather than propagating into a vertex.
	nan := float32(0)
	nan = nan / nan
	if got := NewGlass().Refraction(nan).Params().Refraction; got != 0 {
		t.Errorf("Refraction(NaN) = %v, want 0", got)
	}
}

// TestGlassIsAValueAndDoesNotAlias. The setters return a new Glass, so a
// material handed to two views and then modified for one of them does not
// change the other. Border and Shadow get this for free by being struct
// literals; Glass has to be checked, because it has a hidden field.
func TestGlassIsAValueAndDoesNotAlias(t *testing.T) {
	base := NewGlass().Blur(8)
	other := base.Blur(32)
	if got := base.Params().Blur; got != 8 {
		t.Errorf("the original was modified: blur %v, want 8", got)
	}
	if got := other.Params().Blur; got != 32 {
		t.Errorf("the copy has blur %v, want 32", got)
	}
}

// TestBackgroundIsClosed. Color and Glass are the only two implementations and
// there is no route to a third from outside this package, which is what lets
// ui's type switch have a default branch that is genuinely unreachable.
func TestBackgroundIsClosed(t *testing.T) {
	var _ Background = Color{}
	var _ Background = Glass{}
	// A nil Background is legal and means "no background"; ui handles it.
	var b Background
	if b != nil {
		t.Error("the zero Background is not nil")
	}
}
