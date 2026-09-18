package ebiten

import (
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// TestAtlasKeysDoNotCollideAcrossDensities is the collision half of the
// project plan, section 18: the atlas key carries the size in *device* pixels,
// so one label on a 1x and on a 2x display is two entries with two bitmaps.
//
// A collision here would be invisible in a screenshot at one density and
// catastrophic at two: whichever density happened to ask first would decide
// the resolution of the text for the other, and a window dragged between two
// monitors would keep the mask of the monitor it left.
func TestAtlasKeysDoNotCollideAcrossDensities(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{})
	g := shapeGlyphs(t, "A", 16)[0]

	i1, ok := a.Lookup(g, 1)
	if !ok {
		t.Fatal("lookup at density 1 failed")
	}
	i2, ok := a.Lookup(g, 2)
	if !ok {
		t.Fatal("lookup at density 2 failed")
	}
	i3, ok := a.Lookup(g, 3)
	if !ok {
		t.Fatal("lookup at density 3 failed")
	}
	if i1 == i2 || i1 == i3 || i2 == i3 {
		t.Fatalf("one glyph at three densities shares entries: %d, %d, %d", i1, i2, i3)
	}

	// And the entries are the sizes they claim to be. The 32 pixel mask of a
	// 16 point glyph at 2x has about twice the edge of the 16 pixel one; the
	// comparison is deliberately "strictly larger" per axis and not a ratio,
	// because the exact pixel count depends on the outline and on the
	// rasteriser's rounding, and a test that pinned it would be a test of
	// Roboto.
	e1, e2, e3 := a.Entry(i1), a.Entry(i2), a.Entry(i3)
	if !(e2.w > e1.w && e2.h > e1.h) {
		t.Errorf("the 2x mask is %dx%d, the 1x mask %dx%d; 2x must be larger", e2.w, e2.h, e1.w, e1.h)
	}
	if !(e3.w > e2.w && e3.h > e2.h) {
		t.Errorf("the 3x mask is %dx%d, the 2x mask %dx%d; 3x must be larger", e3.w, e3.h, e2.w, e2.h)
	}
	// The identity that matters for the plan's wording: the 2x mask covers
	// about four times the area of the 1x one, which is what quadruples the
	// atlas budget. Three and a half to four and a half leaves room for the
	// one pixel of rounding on each edge.
	ratio := float64(e2.w*e2.h) / float64(e1.w*e1.h)
	if ratio < 3.0 || ratio > 5.0 {
		t.Errorf("the 2x mask covers %.2f times the area of the 1x one, want about four", ratio)
	}
	t.Logf("mask area: 1x %dx%d = %d px, 2x %dx%d = %d px, 3x %dx%d = %d px",
		e1.w, e1.h, e1.w*e1.h, e2.w, e2.h, e2.w*e2.h, e3.w, e3.h, e3.w*e3.h)
}

// TestAtlasScaleOneIsTheOldKey states the no-regression half. Density 1 is the
// target platform of the project plan, section 1, and a scale of one must
// produce the entry the atlas produced before scales existed: Size*1 is Size
// and quantize is unchanged, so the second lookup is a hit and not a second
// entry.
func TestAtlasScaleOneIsTheOldKey(t *testing.T) {
	a := NewGlyphAtlas(AtlasConfig{})
	g := shapeGlyphs(t, "A", 16)[0]
	i1, _ := a.Lookup(g, 1)
	before := a.Stats()
	i2, _ := a.Lookup(g, 1)
	after := a.Stats()
	if i1 != i2 {
		t.Fatalf("two lookups at scale 1 gave entries %d and %d", i1, i2)
	}
	if after.Misses != before.Misses {
		t.Errorf("the second lookup at scale 1 missed; misses went from %d to %d",
			before.Misses, after.Misses)
	}
	// A non-finite or non-positive scale falls back to one rather than
	// poisoning the key with a NaN, which would never equal itself and would
	// leak an entry per lookup.
	if i3, ok := a.Lookup(g, 0); !ok || i3 != i1 {
		t.Errorf("a zero scale gave entry %d, ok=%v; want the scale-1 entry %d", i3, ok, i1)
	}
}

// TestGlyphQuadIsNotStretchedByTheDensity is the renderer half. A glyph mask
// is rasterised in device pixels, so the quad that draws it must be the mask's
// own size in device pixels and only its origin may be transformed. Mapping
// the whole rectangle through the transform — which is what the code did
// before WU-W — would produce a quad twice the mask at 2x and would resample a
// bitmap under a nearest filter.
func TestGlyphQuadIsNotStretchedByTheDensity(t *testing.T) {
	gs := shapeGlyphs(t, "H", 16)

	one := glyphQuadsAt(t, gs, 1)
	two := glyphQuadsAt(t, gs, 2)
	if len(one) == 0 {
		t.Fatal("no inked glyph quad at density 1; the fixture draws nothing")
	}
	if len(one) != len(two) {
		t.Fatalf("%d quads at 1x but %d at 2x", len(one), len(two))
	}
	for i := range one {
		w1, h1 := one[i].Width(), one[i].Height()
		w2, h2 := two[i].Width(), two[i].Height()
		// Twice the mask, not twice the quad of a 1x mask. Both happen to be
		// "about twice as wide", which is why the test also checks that the
		// *source* rectangle grew: a stretched quad reads the same atlas
		// rectangle, a rasterised one reads a bigger one.
		if w2 < w1 || h2 < h1 {
			t.Errorf("quad %d shrank from %gx%g to %gx%g", i, w1, h1, w2, h2)
		}
		// The origin is at twice the coordinate, because the baseline is a
		// position and positions do scale.
		if two[i].Min.X < 2*one[i].Min.X-2 || two[i].Min.X > 2*one[i].Min.X+2 {
			t.Errorf("quad %d starts at x=%g at 2x, want about twice %g", i, two[i].Min.X, one[i].Min.X)
		}
	}
}

// glyphQuadsAt renders one glyph run through a real renderer at the given
// density and returns the device rectangles of the quads it emitted.
//
// It uses the vertex buffer rather than a GPU: the quads are what the backend
// computed, and computing them needs no graphics context.
func glyphQuadsAt(t *testing.T, gs []render.Glyph, density float32) []geom.Rect {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	var l render.List
	l.Reset()
	xf := l.PushXform(geom.Scale(density, density))
	first := l.GlyphsLen()
	for _, g := range gs {
		l.AppendGlyph(g)
	}
	l.Add(render.Op{
		Kind: render.OpGlyphs, Bounds: geom.Rc(0, 0, 400, 100),
		Color: render.RGB(1, 1, 1), Xform: xf,
		Glyphs: first, GlyphCount: l.GlyphsLen() - first,
	})

	r.BeginFrame(geom.Sz(400*density, 100*density))
	r.Submit(&l)
	quads := quadsOf(r.verts, r.idx)
	r.EndFrame()
	return quads
}

// quadsOf recovers the axis aligned rectangles from the vertex stream. Every
// glyph quad is four vertices and two triangles, and the renderer emits them
// in order.
func quadsOf(verts []eb.Vertex, idx []uint32) []geom.Rect {
	out := make([]geom.Rect, 0, len(verts)/4)
	for i := 0; i+4 <= len(verts); i += 4 {
		out = append(out, geom.Rc(verts[i].DstX, verts[i].DstY, verts[i+2].DstX, verts[i+2].DstY))
	}
	return out
}

// TestAtlasBudgetAtTwoX is the measurement the project plan, section 18,
// demands instead of a guess: rasterising in device pixels quadruples the area
// of every mask, so the question is whether [AtlasConfig]'s defaults still
// hold a realistic application's text at 2x.
//
// The corpus is the printable ASCII range at the five sizes a desktop
// application actually uses — a caption, body text, a subheading, a heading
// and a display number — which is the whole of what a gift program can put on
// screen today, since there is no font fallback and no second family. That is
// a deliberate overestimate of one screen and a fair estimate of everything
// one application will ever ask for.
func TestAtlasBudgetAtTwoX(t *testing.T) {
	const corpus = " !\"#$%&'()*+,-./0123456789:;<=>?@" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`" +
		"abcdefghijklmnopqrstuvwxyz{|}~"
	sizes := []float32{13, 16, 20, 24, 32}

	for _, density := range []float32{1, 2, 3} {
		a := NewGlyphAtlas(AtlasConfig{})
		for _, size := range sizes {
			for _, g := range shapeGlyphs(t, corpus, size) {
				a.Lookup(g, density)
			}
		}
		s := a.Stats()
		cfg := AtlasConfig{PageSize: DefaultAtlasPageSize, MaxPages: DefaultAtlasMaxPages}
		budget := cfg.PageSize * cfg.PageSize * 4 * cfg.MaxPages
		// Bytes is page granular — a page costs its whole four megabytes the
		// moment one glyph lands on it — so UploadedBytes is reported next to
		// it as the coverage actually packed, which is the number that grows
		// with the square of the density.
		t.Logf("density %v: %d glyphs, %d coverage bytes packed, %d pages = %d bytes resident, "+
			"%.1f %% of the %d byte default budget, %d rejected",
			density, s.Glyphs, s.UploadedBytes, s.Pages, s.Bytes,
			100*float64(s.Bytes)/float64(budget), budget, s.Rejected)
		if s.Rejected != 0 {
			t.Errorf("density %v: the default atlas budget turned away %d glyphs; "+
				"text would be missing from the screen", density, s.Rejected)
		}
	}
}

// TestLogicalDividesPositionsByTheDensity is the input half of the project
// plan, section 18, and it is the half with the worst failure mode. Ebitengine
// reports the cursor in the coordinates of the framebuffer, which after
// [game.LayoutF] is in *device* pixels, while gift's pointer model, its hit
// testing and every layout rectangle are in logical ones. If the conversion
// were inverted, or missing, every click on a 2x display would land at twice
// the coordinate it was made at — off the bottom of most windows — and nothing
// in a golden image or a layout test would notice.
//
// The guard is as important as the arithmetic. The density starts at zero on a
// bridge that has never seen a frame, and a division by zero would make the
// first pointer position of the run an infinity rather than a coordinate.
func TestLogicalDividesPositionsByTheDensity(t *testing.T) {
	for _, c := range []struct {
		name    string
		density float32
		x, y    float64
		want    geom.Point
	}{
		{"density 1 is the identity", 1, 100, 50, geom.Pt(100, 50)},
		{"density 2 halves", 2, 100, 50, geom.Pt(50, 25)},
		{"density 3 thirds", 3, 120, 60, geom.Pt(40, 20)},
		// The guard. Zero is the value of a bridge that has not been told a
		// density yet, and a negative or a NaN one could only come from a
		// monitor reading that went wrong; all of them mean "assume 1"
		// rather than "produce an infinity".
		{"an unset density is the identity", 0, 100, 50, geom.Pt(100, 50)},
		{"a negative density is the identity", -2, 100, 50, geom.Pt(100, 50)},
		// Odd physical coordinates at 2x are the case the halving actually
		// meets: a pointer can sit on any device pixel, and half of them are
		// not whole logical ones. The half is kept rather than rounded,
		// because gift's pointer model is in float32 and rounding here would
		// quantise a drag to whole logical pixels on a display whose whole
		// point is that it has finer ones.
		{"an odd device coordinate keeps its half", 2, 101, 51, geom.Pt(50.5, 25.5)},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newInputBridge(nil)
			b.density = c.density
			if got := b.logical(c.x, c.y); got != c.want {
				t.Errorf("logical(%v, %v) at density %v = %v, want %v",
					c.x, c.y, c.density, got, c.want)
			}
		})
	}
}

// TestPollMouseConvertsThePositionAndNotTheWheel is the whole of the density
// rule for the mouse, driven through [inputBridge.cursor] and
// [inputBridge.wheel] and asserted on the events a view actually receives.
//
// Two facts, and they point in opposite directions, which is why they are one
// test. The cursor is a *position* in device pixels and is divided by the
// density. The wheel is a *notch count* and is not: [gift.ScrollConfig]
// turns a notch into a distance in logical pixels, so halving it here would
// make a mouse wheel scroll half as far on a Retina display, and there is no
// way for an application to see that, let alone correct it.
//
// Until WU-AA the second half was asserted only by a comment in pollMouse.
func TestPollMouseConvertsThePositionAndNotTheWheel(t *testing.T) {
	ka := newKeyboardApp(t)
	b := newInputBridge(ka.app)
	b.density = 2
	b.cursor = func() (float64, float64) { return 100, 50 }
	b.wheel = func() (float64, float64) { return 0, -3 }

	ka.events = ka.events[:0]
	ka.app.BeginInput(0)
	b.pollMouse()

	var moved, wheeled bool
	for _, e := range ka.events {
		switch e.Kind {
		case gift.EventPointerMove:
			moved = true
			if e.Pos != geom.Pt(50, 25) {
				t.Errorf("a cursor at the device pixel (100, 50) reached the view at %v, "+
					"want the logical (50, 25) at density 2", e.Pos)
			}
		case gift.EventWheel:
			wheeled = true
			if e.Delta != geom.Pt(0, -3) {
				t.Errorf("three notches of wheel reached the view as %v, want (0, -3); "+
					"the wheel is a notch count and must not be divided by the density", e.Delta)
			}
			if e.Pos != geom.Pt(50, 25) {
				t.Errorf("the wheel event carries the position %v, want the logical (50, 25)", e.Pos)
			}
		}
	}
	if !moved {
		t.Error("no pointer move reached the view; this test asserted nothing")
	}
	if !wheeled {
		t.Error("no wheel event reached the view; this test asserted nothing")
	}
}

// TestLayoutFAsksForDevicePixels is the other side of the same coin: LayoutF
// is what makes the framebuffer the size the input conversion assumes.
//
// The reading of the monitor is replaced through [game.scale]; see the field
// for why a test binary must not call [eb.Monitor] itself. Everything else is
// the real code, including the rounding, which lives in [gift.App.SetDensity].
func TestLayoutFAsksForDevicePixels(t *testing.T) {
	for _, c := range []struct {
		name         string
		raw          float64
		outW, outH   float64
		wantW, wantH float64
		wantDensity  float32
	}{
		{"1x is the size it was given", 1, 800, 600, 800, 600, 1},
		{"2x is twice the size", 2, 800, 600, 1600, 1200, 2},
		// The rounding of section 18, seen where an application would meet
		// it. 1.5 becomes 2, so Ebitengine is handed a 2x frame and
		// downsamples it by three quarters — supersampling, which is stated
		// on LayoutF rather than pretended away.
		{"1.5 rounds up to 2", 1.5, 800, 600, 1600, 1200, 2},
		{"1.4 rounds down to 1", 1.4, 800, 600, 800, 600, 1},
		// A monitor that reports nothing usable is density 1, never zero: a
		// zero would ask Ebitengine for a screen of no pixels.
		{"a zero reading is density 1", 0, 800, 600, 800, 600, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			app := gift.New(gift.Options{
				Root: func(*gift.Context) gift.View { return recorderView{app: &keyboardApp{}} },
			})
			g := &game{app: app, scale: func() float64 { return c.raw }}
			w, h := g.LayoutF(c.outW, c.outH)
			if w != c.wantW || h != c.wantH {
				t.Errorf("LayoutF(%v, %v) at a raw factor of %v = (%v, %v), want (%v, %v)",
					c.outW, c.outH, c.raw, w, h, c.wantW, c.wantH)
			}
			if got := app.Density(); got != c.wantDensity {
				t.Errorf("the application was told density %v, want %v", got, c.wantDensity)
			}
			// Layout exists only to satisfy eb.Game and must not be a second
			// policy; it answers whatever LayoutF answers, in whole numbers.
			iw, ih := g.Layout(int(c.outW), int(c.outH))
			if float64(iw) != c.wantW || float64(ih) != c.wantH {
				t.Errorf("Layout gave (%d, %d) where LayoutF gave (%v, %v)", iw, ih, w, h)
			}
		})
	}
}

// TestLayoutFRemembersTheLastUsableSize covers the degenerate call. Ebitengine
// calls Layout on a minimised or zero sized window, and a viewport of zero
// would make the next build lay everything out at nothing and throw away the
// scroll offsets that depend on it. The last usable size is kept instead.
func TestLayoutFRemembersTheLastUsableSize(t *testing.T) {
	app := gift.New(gift.Options{
		Root: func(*gift.Context) gift.View { return recorderView{app: &keyboardApp{}} },
	})
	g := &game{app: app, scale: func() float64 { return 2 }}
	if w, h := g.LayoutF(800, 600); w != 1600 || h != 1200 {
		t.Fatalf("LayoutF(800, 600) = (%v, %v)", w, h)
	}
	if w, h := g.LayoutF(0, 0); w != 1600 || h != 1200 {
		t.Errorf("a zero sized window gave (%v, %v); the last usable size must be kept", w, h)
	}
}
