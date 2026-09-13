package render

import "github.com/torbenschinke/gift/geom"

// sentinelExtent is the half extent of the unbounded clip rectangle at index
// 0. It is large enough to contain any plausible view space coordinate and
// small enough that intersecting it with a finite rectangle can never produce
// an infinity or a NaN.
const sentinelExtent = 1e30

// List is the display list of a single frame.
//
// A list owns three flat slices: the operations themselves, the clip
// rectangles and the transforms. Operations reference the latter two by
// index, and index 0 of both is a reserved sentinel meaning "unclipped"
// respectively "identity".
//
// A list is reused across frames. [List.Reset] empties it while keeping the
// capacity of all backing arrays, so a steady state frame performs no
// allocation.
//
// A List is not safe for concurrent use. It belongs to the UI executor.
type List struct {
	ops    []Op
	clips  []geom.Rect
	xforms []geom.Affine2D
	// clipStack holds indices into clips. Its first element is always 0,
	// the unbounded sentinel, so the stack is never empty.
	clipStack []uint32
}

func (l *List) ensure() {
	if len(l.clips) == 0 {
		l.clips = append(l.clips, geom.Rc(-sentinelExtent, -sentinelExtent, sentinelExtent, sentinelExtent))
	}
	if len(l.xforms) == 0 {
		l.xforms = append(l.xforms, geom.Identity())
	}
	if len(l.clipStack) == 0 {
		l.clipStack = append(l.clipStack, 0)
	}
}

// Reset empties the list and resets the clip stack, keeping the capacity of
// all backing arrays.
//
// Every slice previously returned by [List.Ops] becomes invalid at this
// point. A consumer that still holds such a slice is reading recycled memory.
func (l *List) Reset() {
	l.ops = l.ops[:0]
	l.clips = l.clips[:0]
	l.xforms = l.xforms[:0]
	l.clipStack = l.clipStack[:0]
	l.ensure()
}

// PushClip intersects r with the currently active clip, appends the result
// and returns its index. The result becomes the active clip until the
// matching [List.PopClip].
//
// Nested clips therefore never widen the visible area, which is what a
// scrolling container inside another scrolling container requires.
func (l *List) PushClip(r geom.Rect) uint32 {
	l.ensure()
	cur := l.clips[l.clipStack[len(l.clipStack)-1]]
	l.clips = append(l.clips, cur.Intersect(r))
	idx := uint32(len(l.clips) - 1)
	l.clipStack = append(l.clipStack, idx)
	return idx
}

// PopClip removes the innermost clip pushed by [List.PushClip]. The clip
// rectangle itself stays in the list, because operations already emitted
// still reference it by index.
//
// Popping the sentinel is a programming error and panics.
func (l *List) PopClip() {
	l.ensure()
	if len(l.clipStack) <= 1 {
		panic("gift/render: List.PopClip without matching PushClip")
	}
	l.clipStack = l.clipStack[:len(l.clipStack)-1]
}

// CurrentClip returns the index of the currently active clip rectangle.
// It is 0 while no clip is pushed.
func (l *List) CurrentClip() uint32 {
	l.ensure()
	return l.clipStack[len(l.clipStack)-1]
}

// PushXform appends the transform m and returns its index. Transforms are not
// deduplicated; the caller is expected to push once and reuse the index for
// all operations that share the transform.
func (l *List) PushXform(m geom.Affine2D) uint32 {
	l.ensure()
	l.xforms = append(l.xforms, m)
	return uint32(len(l.xforms) - 1)
}

// Add appends op to the list. The caller is responsible for setting the Clip
// and Xform indices; see [List.CurrentClip].
func (l *List) Add(op Op) {
	l.ops = append(l.ops, op)
}

// Ops returns the operations of the list in emission order.
//
// The slice is borrowed, not owned. It stays valid only until the producer of
// the list calls [List.Reset], which normally happens at the start of the
// next frame. A consumer that keeps the slice beyond that point reads
// recycled memory and will observe operations of a different frame. Copy what
// you need to keep.
func (l *List) Ops() []Op { return l.ops }

// Clip returns the clip rectangle with index i. Index 0 is the unbounded
// sentinel, which contains every plausible view space coordinate.
func (l *List) Clip(i uint32) geom.Rect {
	l.ensure()
	return l.clips[i]
}

// Xform returns the transform with index i. Index 0 is the identity.
func (l *List) Xform(i uint32) geom.Affine2D {
	l.ensure()
	return l.xforms[i]
}

// Len returns the number of operations in the list.
func (l *List) Len() int { return len(l.ops) }
