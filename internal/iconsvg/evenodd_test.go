package iconsvg_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/worldiety/gift/internal/iconsvg"
)

// grid is the sampling resolution of the even-odd analysis, the same the
// generator uses: ten samples per viewBox unit.
const grid = 240

// TestEvenOddDiffersForExactlyTheseIcons is the measurement the design of this
// package rests on, and the number in it was measured rather than assumed.
//
// golang.org/x/image/vector fills with the nonzero rule and offers no other.
// 211 paths of the corpus declare fill-rule="evenodd", and the two rules agree
// on a shape whose inner contour winds against its outer one and disagree on
// one that winds with it. The question "how many of the 211 actually differ"
// has an answer and it is 45, in 45 distinct icons, all of them in the solid
// set.
//
// The list is written out so that the test says which icons are at stake. If
// the corpus changes, this fails with the new list, and the right response is
// to look at the icons that entered or left it — not to update the constant.
func TestEvenOddDiffersForExactlyTheseIcons(t *testing.T) {
	want := map[string]bool{}
	for _, n := range differingIcons {
		want[n] = true
	}
	got := map[string]bool{}
	checked := 0

	files, _ := filepath.Glob("testdata/flowbite/*/*.svg")
	sort.Strings(files)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := iconsvg.ParseFile(filepath.Base(f), string(src))
		if err != nil {
			t.Fatal(err)
		}
		for _, fig := range doc.Figures {
			if !fig.EvenOdd {
				continue
			}
			checked++
			if differs, _ := iconsvg.DiffersUnderNonzero(fig.Ops, doc.ViewBox, grid); differs {
				got[filepath.Base(filepath.Dir(f))+"/"+filepath.Base(f)] = true
			}
		}
	}
	if checked != 211 {
		t.Errorf("%d even-odd paths in the corpus, expected 211", checked)
	}
	for n := range got {
		if !want[n] {
			t.Errorf("%s now differs between the two fill rules and did not before", n)
		}
	}
	for n := range want {
		if !got[n] {
			t.Errorf("%s no longer differs between the two fill rules", n)
		}
	}
	t.Logf("%d of %d even-odd paths genuinely differ under nonzero", len(got), checked)
}

// TestReorientingMakesNonzeroAgreeWithEvenOdd is the test of the fix, and it
// is the one that would fail if a hole filled in.
//
// For every path that genuinely differs, the reoriented path filled with the
// nonzero rule must paint the same area as the original filled with the
// even-odd rule, at every one of the 57 600 sample points. Not "mostly the
// same" and not "the same bounding box": a filled in hole is a solid region
// where a hole should be, so it shows up as thousands of disagreeing samples,
// and one disagreeing sample is already a failure here.
func TestReorientingMakesNonzeroAgreeWithEvenOdd(t *testing.T) {
	fixed, unchanged := 0, 0
	files, _ := filepath.Glob("testdata/flowbite/*/*.svg")
	sort.Strings(files)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := iconsvg.ParseFile(filepath.Base(f), string(src))
		if err != nil {
			t.Fatal(err)
		}
		for i, fig := range doc.Figures {
			if !fig.EvenOdd {
				continue
			}
			differs, before := iconsvg.DiffersUnderNonzero(fig.Ops, doc.ViewBox, grid)
			if !differs {
				unchanged++
				continue
			}
			out, ok := iconsvg.Orient(fig.Ops, doc.ViewBox, grid)
			if !ok {
				t.Errorf("%s figure %d: reorientation failed; %d of %d samples differed",
					f, i, before, grid*grid)
				continue
			}
			if still, after := iconsvg.DiffersUnderNonzero(out, doc.ViewBox, grid); still {
				t.Errorf("%s figure %d: after reorientation %d samples still differ (%d before)",
					f, i, after, before)
			}
			fixed++
		}
	}
	t.Logf("%d paths reoriented, %d already agreed", fixed, unchanged)
}

// TestAHoleThatWouldFillIn is the unit test of the whole problem on a shape
// small enough to reason about, and it is the test that fails if somebody
// decides the reorientation is unnecessary.
//
// Two concentric squares traced in the *same* direction. Under the even-odd
// rule the inner one is a hole; under the nonzero rule the windings add to two
// and the ring becomes a solid square. The middle of the shape is therefore
// the discriminator, and it is checked in both directions: empty after
// reorientation, and — the half that makes the test honest — filled before it,
// so that a reorientation which did nothing at all could not pass.
func TestAHoleThatWouldFillIn(t *testing.T) {
	const d = "M2 2H22V22H2Z M8 8H16V16H8Z"
	ops, err := iconsvg.ParsePathData(d)
	if err != nil {
		t.Fatal(err)
	}
	differs, n := iconsvg.DiffersUnderNonzero(ops, 24, grid)
	if !differs {
		t.Fatal("two co-oriented concentric squares are reported as identical under both fill " +
			"rules. They are not: the nonzero rule fills the inner one in, which is the entire " +
			"problem this file exists for, so the measurement is broken and every icon that " +
			"relies on it is unverified")
	}
	// The inner square is 8 by 8 of a 24 by 24 box, that is 64/576 of the
	// area, and the grid has 57 600 points.
	if wantN := 64 * grid * grid / 576; n < wantN*9/10 || n > wantN*11/10 {
		t.Errorf("%d samples differ, expected about %d — the area of the hole", n, wantN)
	}

	out, ok := iconsvg.Orient(ops, 24, grid)
	if !ok {
		t.Fatal("the reorientation refused a shape as simple as two nested squares")
	}
	if still, after := iconsvg.DiffersUnderNonzero(out, 24, grid); still {
		t.Errorf("after reorientation %d samples still differ; the hole is still filled in", after)
	}
}

// TestOppositelyWoundContoursAreLeftAlone is the other direction: a shape that
// already agrees under both rules must come out of the reorientation
// unchanged, so that "fix everything" cannot pass as "fix what is broken".
func TestOppositelyWoundContoursAreLeftAlone(t *testing.T) {
	// The same two squares, the inner one traced the other way round.
	const d = "M2 2H22V22H2Z M8 16H16V8H8Z"
	ops, err := iconsvg.ParsePathData(d)
	if err != nil {
		t.Fatal(err)
	}
	if differs, n := iconsvg.DiffersUnderNonzero(ops, 24, grid); differs {
		t.Fatalf("oppositely wound contours disagree in %d samples; they must not", n)
	}
	out, ok := iconsvg.Orient(ops, 24, grid)
	if !ok {
		t.Fatal("the reorientation refused a shape it did not have to touch")
	}
	if len(out) != len(ops) {
		t.Fatalf("the reorientation changed the op count from %d to %d", len(ops), len(out))
	}
	for i := range ops {
		if out[i] != ops[i] {
			t.Errorf("op %d changed from %v to %v; a path that already agreed was rewritten",
				i, ops[i], out[i])
		}
	}
}

// differingIcons is the measured list: every corpus icon whose even-odd fill
// paints a different area than its nonzero fill.
var differingIcons = []string{
	"solid/badge-check.svg",
	"solid/briefcase.svg",
	"solid/calendar-edit.svg",
	"solid/calendar-month.svg",
	"solid/calendar-plus.svg",
	"solid/chart-mixed-dollar.svg",
	"solid/clipboard-check.svg",
	"solid/clipboard-list.svg",
	"solid/clipboard.svg",
	"solid/cog.svg",
	"solid/credit-card-plus.svg",
	"solid/download.svg",
	"solid/file-chart-bar.svg",
	"solid/file-circle-plus.svg",
	"solid/file-csv.svg",
	"solid/file-image.svg",
	"solid/file-invoice.svg",
	"solid/file-lines.svg",
	"solid/file-pdf.svg",
	"solid/file-pen.svg",
	"solid/file-ppt.svg",
	"solid/file-shield.svg",
	"solid/folder-plus.svg",
	"solid/globe.svg",
	"solid/image.svg",
	"solid/inbox-full.svg",
	"solid/landmark.svg",
	"solid/lock.svg",
	"solid/map-pin-alt.svg",
	"solid/mastercard.svg",
	"solid/message-dots.svg",
	"solid/newspaper.svg",
	"solid/profile-card.svg",
	"solid/reddit.svg",
	"solid/rocket.svg",
	"solid/scale-balanced.svg",
	"solid/server.svg",
	"solid/store.svg",
	"solid/table-column.svg",
	"solid/table-row.svg",
	"solid/terminal.svg",
	"solid/upload.svg",
	"solid/user-circle.svg",
	"solid/user-settings.svg",
	"solid/window.svg",
}
