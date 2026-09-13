package layout

import (
	"fmt"
	"math"

	"github.com/torbenschinke/gift/geom"
)

// Axis selects the main axis of a stack.
type Axis uint8

const (
	// Vertical stacks children from top to bottom. The main axis is y, the
	// cross axis is x.
	Vertical Axis = iota
	// Horizontal stacks children from leading to trailing. The main axis is
	// x, the cross axis is y.
	Horizontal
)

// Item is one child as seen by the stack algorithm.
//
// The caller fills in Flex before the call; [Stack] fills in Size while it
// measures. The type is deliberately a plain value: a slice of Items
// is a caller owned scratch buffer that is reused across frames.
type Item struct {
	// Flex is the flexibility of the child along the main axis. Zero means
	// inflexible, a positive value means the child receives a share of the
	// remaining space proportional to it.
	Flex float32
	// Size is the measured size of the child. It is an output.
	Size geom.Size
}

// StackSpec describes a stack layout request.
type StackSpec struct {
	// Axis is the main axis along which children are placed.
	Axis Axis
	// Gap is the space between two adjacent children. It is inserted
	// between children only: n children have n-1 gaps and a stack with zero
	// or one child has none.
	//
	// A negative Gap is well defined and overlaps adjacent children. It must
	// be finite; the ui layer rejects anything else at build time, because an
	// infinite gap would turn every origin below it into an infinity or a NaN
	// and the algorithms here do not check their inputs per frame.
	Gap float32
	// Padding is applied to the incoming constraints before the children are
	// measured and added back to the resulting size afterwards.
	Padding geom.Insets
	// Alignment positions children on the cross axis. Only the cross
	// component is read: a vertical stack uses X, a horizontal stack uses Y.
	Alignment geom.Alignment
}

// Measurer lets the algorithms measure a child without knowing about gift.
type Measurer interface {
	// MeasureChild lays out the child with index i against c and returns the
	// size it chose.
	MeasureChild(i int, c geom.Constraints) geom.Size
}

// Result is what a layout algorithm reports back.
//
// It is a plain value and is returned by value: nothing in this package
// allocates, and a result struct that had to be pointed at would break that.
type Result struct {
	// Size is the size the container reports to its own parent. It is the
	// size the incoming constraints permit, so a parent that asked for a
	// tight size gets it back.
	Size geom.Size

	// Overflow is how far the honest content extent exceeds Size, per axis,
	// clamped at zero. It is the number the project plan, section 7,
	// "Overflow-Modell", requires to be carried out of the algorithm instead
	// of being swallowed.
	//
	// Overflow is not an error. The children keep their honest sizes and
	// positions and are not clipped; a caller that wants them cut off asks
	// for it explicitly. What Overflow buys is that "the content does not
	// fit" is a number somebody can see, rather than a row silently
	// collapsing to zero.
	Overflow geom.Size
}

// IsOverflowing reports whether either axis overflowed.
func (r Result) IsOverflowing() bool { return r.Overflow.W > 0 || r.Overflow.H > 0 }

// Stack runs the two pass stack algorithm and returns the size of the stack
// itself together with its overflow. The origins of the children, relative to
// the origin of the stack, are written into origins.
//
// # The two passes
//
// Pass one measures every child with a Flex of zero. Such a child is offered
// the full cross axis extent of the padded constraints and an **unbounded**
// main axis. It is deliberately not offered "whatever is left": the size of a
// child must not depend on how many siblings precede it, because a shrinking
// remainder makes later children collapse to zero while their own fixed size
// grandchildren keep their real extents and end up painted on top of each
// other below a parent that claims to be empty. That is the defect the
// project plan, section 7, "Overflow-Modell", was written to forbid.
//
// Pass two distributes the main axis space that is left after the inflexible
// children and all gaps across the flexible children, in proportion to their
// Flex and with a tight main axis constraint. If nothing is left, every
// flexible child gets zero.
//
// # Overflow
//
// The content extent is the sum of the measured main extents plus n-1 gaps.
// When it exceeds what the constraints permit, the stack still reports the
// permitted size — a child does not get to resize its parent — but the
// children keep their honest positions and the excess is reported in
// [Result.Overflow]. Nothing is clipped automatically.
//
// Consequence worth stating plainly: when a stack overflows, the sum of the
// children plus the gaps is larger than the reported size. That is not an
// inconsistency, it is what overflow means, and the difference is exactly
// Result.Overflow.
//
// # Unbounded main axis
//
// If the incoming main axis maximum is [geom.Unbounded], there is no
// remaining space to distribute and every flexible child receives exactly its
// MinMain, which is zero by default. A flexible child never produces an
// infinite or NaN extent, and an unbounded axis can never overflow.
//
// # Rounding
//
// The shares are computed from a running sum and the last flexible child
// receives whatever is left over, so the flexible extents add up to the
// available space exactly and no drift accumulates over many children.
//
// # Scratch buffers
//
// items and origins are owned by the caller and must have a length of at
// least n; a shorter buffer is a programming error and panics. Stack itself
// never allocates.
func Stack(spec StackSpec, c geom.Constraints, n int, m Measurer, items []Item, origins []geom.Point) Result {
	checkScratch("Stack", n, items, origins)
	ax := spec.Axis
	inner := c.Deflate(spec.Padding)
	crossMax := ax.cross(inner.Max)
	mainMax := ax.main(inner.Max)

	var gaps float32
	if n > 1 {
		gaps = spec.Gap * float32(n-1)
	}

	// Pass one. The main axis maximum is unbounded for every inflexible
	// child, so the constraints are the same for all of them and no child is
	// starved by its predecessors.
	rigid := ax.constraints(0, geom.Unbounded(), 0, crossMax)

	var usedMain, maxCross, flexSum float32
	lastFlex := -1
	for i := range n {
		if items[i].Flex > 0 {
			flexSum += items[i].Flex
			lastFlex = i
			continue
		}
		sz := m.MeasureChild(i, rigid)
		items[i].Size = sz
		usedMain += ax.main(sz)
		if cr := ax.cross(sz); cr > maxCross {
			maxCross = cr
		}
	}

	// The remainder is what the constraints allow minus what the inflexible
	// children and the gaps actually took, never negative. An unbounded main
	// axis has no remainder to give away.
	var free float32
	if isFinite(mainMax) {
		free = clampLow(mainMax - usedMain - gaps)
	}

	var flexMain float32
	if flexSum > 0 {
		var givenFlex, givenFree float32
		for i := range n {
			f := items[i].Flex
			if f <= 0 {
				continue
			}
			var share float32
			if i == lastFlex {
				// The last flexible child absorbs the rounding remainder, so
				// that the shares add up to free exactly.
				share = free - givenFree
			} else {
				givenFlex += f
				share = free*givenFlex/flexSum - givenFree
			}
			givenFree += share
			ext := clampLow(share)
			sz := m.MeasureChild(i, ax.constraints(ext, ext, 0, crossMax))
			items[i].Size = sz
			flexMain += ax.main(sz)
			if cr := ax.cross(sz); cr > maxCross {
				maxCross = cr
			}
		}
	}

	// content is the honest extent of the children including the gaps; outer
	// is what the constraints permit. The difference, per axis, is the
	// overflow.
	content := ax.size(usedMain+gaps+flexMain, maxCross).Outset(spec.Padding)
	outer := c.Constrain(content)

	contentCross := clampLow(ax.cross(outer) - ax.padCross(spec.Padding))
	af := ax.alignFactor(spec.Alignment)

	pos := ax.padMainLead(spec.Padding)
	crossLead := ax.padCrossLead(spec.Padding)
	for i := range n {
		sz := items[i].Size
		origins[i] = ax.point(pos, crossLead+(contentCross-ax.cross(sz))*af)
		pos += ax.main(sz) + spec.Gap
	}
	return Result{Size: outer, Overflow: excess(content, outer)}
}

// Overlay measures every child against the same padded constraints, sizes the
// container to the largest child and aligns each child inside it. It is the
// algorithm of a ZStack.
//
// Unlike [Stack] it uses both components of the alignment, because neither
// axis is a stacking axis, and neither axis is measured unbounded: both are
// bounded, so a greedy child such as an unframed [ui.BoxView] fills the box on
// both axes. That is the second half of the rule in the project plan,
// section 7, "Overflow-Modell".
//
// Overflow is reported the same way as in [Stack]: a child that returns more
// than it was offered — because it carries a Frame that does not fit, or
// because it disobeys its constraints, which [gift.Layouter] explicitly allows
// so that bugs stay visible — keeps its size and position, the overlay reports
// the permitted size, and the excess is in [Result.Overflow]. Nothing is
// clipped automatically.
//
// A child's Flex is not read here. A Z stack has no main axis, so there is no
// remainder to take a share of; see [ui.Overlay.Flex].
//
// items and origins are caller owned scratch buffers of length at least n;
// Overlay never allocates.
func Overlay(padding geom.Insets, align geom.Alignment, c geom.Constraints, n int, m Measurer, items []Item, origins []geom.Point) Result {
	checkScratch("Overlay", n, items, origins)
	inner := c.Deflate(padding).Loosen()

	var w, h float32
	for i := range n {
		sz := m.MeasureChild(i, inner)
		items[i].Size = sz
		if sz.W > w {
			w = sz.W
		}
		if sz.H > h {
			h = sz.H
		}
	}

	content := geom.Sz(w, h).Outset(padding)
	outer := c.Constrain(content)
	inside := geom.Sz(
		clampLow(outer.W-padding.Horizontal()),
		clampLow(outer.H-padding.Vertical()),
	)
	for i := range n {
		sz := items[i].Size
		origins[i] = geom.Pt(
			padding.Left+(inside.W-sz.W)*align.X,
			padding.Top+(inside.H-sz.H)*align.Y,
		)
	}
	return Result{Size: outer, Overflow: excess(content, outer)}
}

// excess returns the per axis amount by which content exceeds permitted,
// clamped at zero. An unbounded or NaN component yields zero rather than an
// infinity that would then travel through the diagnostics.
func excess(content, permitted geom.Size) geom.Size {
	return geom.Sz(overBy(content.W, permitted.W), overBy(content.H, permitted.H))
}

func overBy(content, permitted float32) float32 {
	d := content - permitted
	if !(d > 0) || !isFinite(d) {
		return 0
	}
	return d
}

func checkScratch(what string, n int, items []Item, origins []geom.Point) {
	if len(items) < n || len(origins) < n {
		panic(fmt.Sprintf(
			"gift/internal/layout: %s needs scratch buffers of length %d, got items=%d origins=%d; the caller owns these buffers and must size them to the child count",
			what, n, len(items), len(origins)))
	}
}

// --- axis helpers -----------------------------------------------------------

func (a Axis) main(s geom.Size) float32 {
	if a == Horizontal {
		return s.W
	}
	return s.H
}

func (a Axis) cross(s geom.Size) float32 {
	if a == Horizontal {
		return s.H
	}
	return s.W
}

func (a Axis) size(main, cross float32) geom.Size {
	if a == Horizontal {
		return geom.Sz(main, cross)
	}
	return geom.Sz(cross, main)
}

func (a Axis) point(main, cross float32) geom.Point {
	if a == Horizontal {
		return geom.Pt(main, cross)
	}
	return geom.Pt(cross, main)
}

func (a Axis) constraints(minMain, maxMain, minCross, maxCross float32) geom.Constraints {
	return geom.Constraints{
		Min: a.size(minMain, minCross),
		Max: a.size(maxMain, maxCross),
	}
}

func (a Axis) padMainLead(i geom.Insets) float32 {
	if a == Horizontal {
		return i.Left
	}
	return i.Top
}

func (a Axis) padCrossLead(i geom.Insets) float32 {
	if a == Horizontal {
		return i.Top
	}
	return i.Left
}

func (a Axis) padCross(i geom.Insets) float32 {
	if a == Horizontal {
		return i.Vertical()
	}
	return i.Horizontal()
}

func (a Axis) alignFactor(al geom.Alignment) float32 {
	if a == Horizontal {
		return al.Y
	}
	return al.X
}

// clampLow returns v, or zero when v is negative. Unbounded passes through
// unchanged.
func clampLow(v float32) float32 {
	if v < 0 {
		return 0
	}
	return v
}

func isFinite(v float32) bool {
	return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v))
}
