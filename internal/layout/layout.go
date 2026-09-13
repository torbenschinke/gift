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

// Stack runs the two pass stack algorithm and returns the size of the stack
// itself. The origins of the children, relative to the origin of the stack,
// are written into origins.
//
// # The two passes
//
// Pass one measures every child with a Flex of zero. Such a child is offered
// the full cross axis extent of the padded constraints and whatever main axis
// room is still left, so an early child can consume space that a later one
// then no longer sees. Pass two distributes the main axis space that is left
// after the inflexible children and all gaps across the flexible children, in
// proportion to their Flex and with a tight main axis constraint.
//
// Space that cannot be distributed because there is no flexible child is not
// consumed: the stack shrinks to its content unless the incoming constraints
// force it to be larger.
//
// # Unbounded main axis
//
// If the incoming main axis maximum is [geom.Unbounded], there is no
// remaining space to distribute and every flexible child receives exactly its
// MinMain, which is zero by default. A flexible child never produces an
// infinite or NaN extent.
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
func Stack(spec StackSpec, c geom.Constraints, n int, m Measurer, items []Item, origins []geom.Point) geom.Size {
	checkScratch("Stack", n, items, origins)
	ax := spec.Axis
	inner := c.Deflate(spec.Padding)
	crossMax := ax.cross(inner.Max)
	mainMax := ax.main(inner.Max)

	var gaps float32
	if n > 1 {
		gaps = spec.Gap * float32(n-1)
	}

	// remaining is the main axis room still available to children. It stays
	// Unbounded when the incoming main axis is unbounded, because infinity
	// minus a finite extent is still infinity.
	remaining := clampLow(mainMax - gaps)

	var usedMain, maxCross, flexSum float32
	lastFlex := -1
	for i := range n {
		if items[i].Flex > 0 {
			flexSum += items[i].Flex
			lastFlex = i
			continue
		}
		sz := m.MeasureChild(i, ax.constraints(0, remaining, 0, crossMax))
		items[i].Size = sz
		usedMain += ax.main(sz)
		remaining = clampLow(remaining - ax.main(sz))
		if cr := ax.cross(sz); cr > maxCross {
			maxCross = cr
		}
	}

	var flexMain float32
	if flexSum > 0 {
		free := float32(0)
		if isFinite(mainMax) {
			free = remaining
		}
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

	outer := c.Constrain(ax.size(usedMain+gaps+flexMain, maxCross).Outset(spec.Padding))
	contentCross := clampLow(ax.cross(outer) - ax.padCross(spec.Padding))
	af := ax.alignFactor(spec.Alignment)

	pos := ax.padMainLead(spec.Padding)
	crossLead := ax.padCrossLead(spec.Padding)
	for i := range n {
		sz := items[i].Size
		origins[i] = ax.point(pos, crossLead+(contentCross-ax.cross(sz))*af)
		pos += ax.main(sz) + spec.Gap
	}
	return outer
}

// Overlay measures every child against the same padded constraints, sizes the
// container to the largest child and aligns each child inside it. It is the
// algorithm of a ZStack.
//
// Unlike [Stack] it uses both components of the alignment, because neither
// axis is a stacking axis. items and origins are caller owned scratch buffers
// of length at least n; Overlay never allocates.
func Overlay(padding geom.Insets, align geom.Alignment, c geom.Constraints, n int, m Measurer, items []Item, origins []geom.Point) geom.Size {
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

	outer := c.Constrain(geom.Sz(w, h).Outset(padding))
	content := geom.Sz(
		clampLow(outer.W-padding.Horizontal()),
		clampLow(outer.H-padding.Vertical()),
	)
	for i := range n {
		sz := items[i].Size
		origins[i] = geom.Pt(
			padding.Left+(content.W-sz.W)*align.X,
			padding.Top+(content.H-sz.H)*align.Y,
		)
	}
	return outer
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
