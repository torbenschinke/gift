package ui_test

import (
	"strconv"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// galleryApp mounts a 100 000 entry gallery directly on a [gift.App], without
// the harness, because the harness copies the display list and that copy would
// be counted as an allocation of the frame path it is measuring.
func galleryApp(t testing.TB, n int, l ui.GalleryLayout) (*gift.App, *ui.Gallery) {
	t.Helper()
	g := ui.NewGallery(asset.NewCollection(synth(n)))
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(ui.ImageGallery(g).Layout(l).Flex(1))
	}})
	// Settle: the first layout fills the index, the second sizes the tile
	// pool, the third binds it. A few more for the pool to stop growing.
	for range 8 {
		if err := a.Update(geom.Sz(800, 600)); err != nil {
			t.Fatal(err)
		}
		a.Paint()
	}
	if !g.Ready() || g.VisibleCount() == 0 {
		t.Fatalf("gallery did not settle: %v", g)
	}
	return a, g
}

// TestGalleryScrollFrameIsAllocationFree is the allocation contract of the
// project plan, section 11, applied to the one path this work unit adds:
// scrolling a virtualised gallery.
//
// Two scenarios, because they are different claims.
//
// "steady" is a frame in which the offset moves but no tile changes item — a
// sub-tile scroll step. That is the case the contract of section 11 covers
// literally: input, index query, transform and display list, no build. It must
// be zero.
//
// "rebinding" is a frame in which the offset moves far enough that tiles are
// recycled. It is not exempt from the contract: recycling is rebinding a
// struct and re-measuring a node, and neither allocates. It is measured
// separately because it is the more interesting number and because "new
// tiles" in section 11's exemption list means *mounting* nodes, which this
// does not do.
func TestGalleryScrollFrameIsAllocationFree(t *testing.T) {
	for _, tc := range []struct {
		name string
		l    ui.GalleryLayout
	}{
		{"masonry", ui.Masonry().MinColumnWidth(240).Gap(8)},
		{"justified", ui.Justified().RowHeight(180).Gap(8)},
	} {
		t.Run(tc.name+"/steady", func(t *testing.T) {
			// The App is created inside the leaf subtest and never in a
			// parent: an App belongs to the goroutine that called gift.New,
			// and a subtest runs on a goroutine of its own, which the
			// giftdebug UI executor check catches immediately.
			a, g := galleryApp(t, 100000, tc.l)
			node, ok := galleryNodeOf(a, a.Root())
			if !ok {
				t.Fatal("no scroll container in the tree")
			}
			{
				// Half a pixel each way: the offset changes, so layout runs,
				// but no item enters or leaves the viewport.
				up := true
				step := func() {
					if up {
						a.ScrollBy(node, 0.5)
					} else {
						a.ScrollBy(node, -0.5)
					}
					up = !up
					if err := a.Update(geom.Sz(800, 600)); err != nil {
						t.Fatal(err)
					}
					a.Paint()
				}
				for range 16 {
					step()
				}
				gen := g.Generation()
				for range 8 {
					step()
				}
				if g.Generation() != gen {
					t.Fatalf("a sub tile scroll rebound %d tiles; this is not the steady state",
						g.Generation()-gen)
				}
				if got := testing.AllocsPerRun(200, step); got != 0 {
					t.Fatalf("steady state scroll frame allocated %v times per run, want 0", got)
				}
			}
		})

		t.Run(tc.name+"/rebinding", func(t *testing.T) {
			a, g := galleryApp(t, 100000, tc.l)
			node, ok := galleryNodeOf(a, a.Root())
			if !ok {
				t.Fatal("no scroll container in the tree")
			}
			{
				step := func() {
					a.ScrollBy(node, 137)
					if err := a.Update(geom.Sz(800, 600)); err != nil {
						t.Fatal(err)
					}
					a.Paint()
				}
				for range 16 {
					step()
				}
				gen := g.Generation()
				step()
				if g.Generation() == gen {
					t.Fatal("no tile was rebound; this is not the recycling case")
				}
				if got := testing.AllocsPerRun(200, step); got != 0 {
					t.Fatalf("recycling scroll frame allocated %v times per run, want 0", got)
				}
			}
		})
	}
}

// galleryNodeOf finds the scroll container below r.
func galleryNodeOf(a *gift.App, r gift.NodeRef) (gift.NodeRef, bool) {
	if a.IsScrollable(r) {
		return r, true
	}
	var kids []gift.NodeRef
	for _, c := range a.NodeChildren(r, kids) {
		if n, ok := galleryNodeOf(a, c); ok {
			return n, true
		}
	}
	return gift.NodeRef{}, false
}

// BenchmarkGalleryScroll is the measured form of the claim in the project
// plan, section 12: the scroll path is independent of N apart from the index
// query.
//
// Run it at both sizes and compare:
//
//	go test ./ui -run x -bench GalleryScroll -benchmem
//
// Each iteration is one wheel notch worth of movement plus a full update and
// paint of the visible tiles, which is what a frame of a scrolling gallery
// really costs.
func BenchmarkGalleryScroll(b *testing.B) {
	for _, n := range []int{100, 10000, 100000} {
		for _, tc := range []struct {
			name string
			l    ui.GalleryLayout
		}{
			{"masonry", ui.Masonry().MinColumnWidth(240).Gap(8)},
			{"justified", ui.Justified().RowHeight(180).Gap(8)},
		} {
			b.Run(tc.name+"/"+strconv.Itoa(n), func(b *testing.B) {
				a, g := galleryApp(b, n, tc.l)
				node, _ := galleryNodeOf(a, a.Root())
				dir := 1.0
				for range 32 {
					a.ScrollBy(node, 48)
					_ = a.Update(geom.Sz(800, 600))
					a.Paint()
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if info, _ := a.ScrollInfo(node); info.Offset >= info.MaxOffset-1 || info.Offset <= 1 {
						dir = -dir
					}
					a.ScrollBy(node, 48*dir)
					if err := a.Update(geom.Sz(800, 600)); err != nil {
						b.Fatal(err)
					}
					a.Paint()
				}
				b.StopTimer()
				b.ReportMetric(float64(g.SlotCount()), "slots")
			})
		}
	}
}
