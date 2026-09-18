// Package scene provides the retained node storage of gift.
//
// # Why indices instead of pointers
//
// Nodes live in a small number of large, reused blocks and are referenced by a
// [Handle], not by a pointer. That keeps the tree in a handful of contiguous
// allocations, makes recycling cheap and keeps the garbage collector out of
// the frame path. The price is that a handle can outlive the node it refers
// to, which is why every handle carries a generation; see [Handle].
//
// The blocks are never copied when the store grows: growing appends one more
// block instead of reallocating one big slice. That is what makes the pointer
// returned by [Store.Get], and therefore the pointer to the inline payload,
// stable for the whole lifetime of a slot. The reconciler relies on it: it
// holds the payload of a parent while it allocates the nodes of its children.
//
// # The payload is inline and is not cleared
//
// [Node] carries its payload by value, not behind an interface. A tree of
// N nodes therefore costs N node structs inside the blocks and not N separate
// heap objects, and reading the payload needs no type assertion.
//
// [Store.Free] deliberately does *not* zero the payload. Everything a payload
// keeps that the garbage collector must not retain has to be released by the
// owner before the node is freed; in exchange, the slices a payload holds keep
// their backing arrays and a remount into a recycled slot allocates nothing.
// That is the whole point of the arrangement: unmounting a subtree must not
// feed the collector, and remounting it must not ask for memory back. See the
// project plan, sections 10 and 13.
//
// # Contract
//
// This package knows nothing about views, state or rendering. It only knows
// parent and sibling links, the layout results and a payload whose type the
// module root chooses through the type parameter. It must never import the
// module root.
//
// A Store is not safe for concurrent use. It belongs to the UI executor.
package scene

import (
	"fmt"

	"github.com/worldiety/gift/geom"
)

// Flags is the per node invalidation state.
//
// The three invalidation levels are kept separate on purpose: a rebuild
// implies a relayout and a relayout implies a repaint, but a repaint implies
// neither of the other two. See the project plan, section 6.
type Flags uint32

const (
	// FlagNeedsBuild marks a node whose view description must be rebuilt.
	FlagNeedsBuild Flags = 1 << iota
	// FlagNeedsLayout marks a node that must be measured and arranged again.
	//
	// The flag is set on the node that actually changed and on every node up
	// to the root, so that a layout pass can descend along the dirty path and
	// stop at every clean subtree whose incoming constraints did not change.
	FlagNeedsLayout
	// FlagNeedsPaint marks a node whose drawing operations must be produced
	// again.
	FlagNeedsPaint
	// FlagMounted marks a node that is part of the live tree.
	FlagMounted
)

// MaxDepth is the hard limit on the depth of a tree.
//
// Every recursive traversal in this package and in the module root checks it
// and panics with a diagnosis instead of overflowing the goroutine stack. A
// user interface that is a thousand levels deep is a runaway recursion in a
// component function, not a legitimate layout; see the project plan,
// section 15.
const MaxDepth = 512

// Handle references a node inside a [Store].
//
// A handle is a small comparable value and is safe to copy. It consists of a
// slot index and the generation of that slot at the time of allocation. When
// the slot is freed, the generation changes, so every handle to the freed
// node stops validating. Using such a handle is a programming error and is
// reported, not silently redirected to the node that reuses the slot.
//
// The zero Handle refers to no node; see [Handle.IsZero].
type Handle struct {
	index, gen uint32
}

// IsZero reports whether h is the zero handle, which refers to no node.
func (h Handle) IsZero() bool { return h.index == 0 && h.gen == 0 }

// Index returns the slot index of the handle. It is intended for diagnostics
// and for building side tables keyed by slot.
func (h Handle) Index() uint32 { return h.index }

// Node is the retained state of one element.
//
// The tree is stored as first child plus next sibling, which needs two words
// per node instead of a child slice per node and therefore never allocates
// while the tree is reshaped.
type Node[P any] struct {
	// Parent is the owning node, or the zero handle for a root.
	Parent Handle
	// FirstChild is the first child, or the zero handle.
	FirstChild Handle
	// NextSibling is the next sibling under the same parent, or the zero
	// handle.
	NextSibling Handle
	// Key is the reconciliation key. An empty key means the node is matched
	// by position among its siblings.
	Key string
	// TypeID identifies the concrete view type that produced this node. The
	// module root owns the meaning of the value.
	TypeID uint32
	// Bounds is the absolute rectangle of the node, set during arrange.
	Bounds geom.Rect
	// Size is the size of the node, set during measure.
	Size geom.Size
	// Flags is the invalidation state.
	Flags Flags
	// Payload is the module root's per node data, stored inline.
	//
	// It survives [Store.Free] untouched and is handed back, as it was, by
	// the [Store.Alloc] that recycles the slot. The owner must release
	// whatever the collector must not retain before freeing, and may rely on
	// the capacity of any slice it keeps here being preserved.
	Payload P
}

const (
	// blockShift sets the number of nodes per block. 256 nodes is large
	// enough that the per block bookkeeping disappears and small enough that
	// a tree of a handful of nodes does not reserve a megabyte.
	blockShift = 8
	blockSize  = 1 << blockShift
	blockMask  = blockSize - 1
)

// Store owns the nodes of one tree.
//
// Slot 0 is reserved so that the zero [Handle] is never a valid node.
// Generations start at 1 and are incremented on both allocation and release,
// so an odd generation means the slot is alive and an even one means it is
// free.
//
// The zero Store is usable and behaves like NewStore(0): the first [Store.Alloc]
// creates the reserved slot 0 and the first block. [NewStore] only decides how
// much warmup it takes before allocation stops.
//
// Generation wraparound: a slot that is allocated and released 2^31 times
// wraps its generation back to a previously used value, at which point a stale
// handle from that far in the past would validate again. At one allocation per
// slot per frame and 60 frames per second that takes more than a year of
// continuous operation for a single slot, so the wraparound is documented
// rather than defended against. Widening the generation to 64 bit would double
// the size of every handle for no practical gain.
type Store[P any] struct {
	// blocks holds the nodes. A block is never reallocated once it exists,
	// which is what keeps the pointers returned by Get stable.
	blocks [][]Node[P]
	// slots is the number of slots that exist, including the reserved slot 0.
	slots uint32

	gens []uint32
	// lastChild caches the last child per slot so that AppendChild is O(1)
	// instead of walking the sibling list. It is kept out of Node because it
	// is an implementation detail of this package.
	lastChild []Handle
	// childCount is the number of children per slot. It exists so that
	// SetChildren can reject a caller that drops a live child, and so that a
	// child can be released without walking the sibling list.
	childCount []uint32
	free       []uint32
	alive      int
}

// NewStore returns a store preallocated for capacity nodes. The store grows on
// demand; the capacity only decides how much warmup it takes before allocation
// stops.
func NewStore[P any](capacity int) *Store[P] {
	s := new(Store[P])
	if capacity < 0 {
		capacity = 0
	}
	s.reserve(capacity + 1)
	s.grow() // create the reserved slot 0
	return s
}

// reserve makes room for n slots without creating them.
func (s *Store[P]) reserve(n int) {
	if n <= 0 {
		return
	}
	blocks := (n + blockSize - 1) / blockSize
	if cap(s.blocks) < blocks {
		nb := make([][]Node[P], len(s.blocks), blocks)
		copy(nb, s.blocks)
		s.blocks = nb
	}
	if cap(s.gens) < n {
		s.gens = append(make([]uint32, 0, n), s.gens...)
		s.lastChild = append(make([]Handle, 0, n), s.lastChild...)
		s.childCount = append(make([]uint32, 0, n), s.childCount...)
	}
	if cap(s.free) < n {
		s.free = append(make([]uint32, 0, n), s.free...)
	}
}

// grow creates one more slot and returns its index.
func (s *Store[P]) grow() uint32 {
	idx := s.slots
	if int(idx>>blockShift) == len(s.blocks) {
		s.blocks = append(s.blocks, make([]Node[P], blockSize))
	}
	s.gens = append(s.gens, 0)
	s.lastChild = append(s.lastChild, Handle{})
	s.childCount = append(s.childCount, 0)
	s.slots++
	return idx
}

// at returns the node in slot idx. The pointer stays valid for as long as the
// store exists, because blocks are never reallocated.
func (s *Store[P]) at(idx uint32) *Node[P] {
	return &s.blocks[idx>>blockShift][idx&blockMask]
}

// Alloc returns a handle to a fresh node.
//
// Released slots are reused, so after warmup Alloc performs no allocation at
// all. The node is reset except for its payload, which is whatever the
// previous occupant of the slot left behind; see the package documentation.
func (s *Store[P]) Alloc() Handle {
	if s.slots == 0 {
		s.grow() // reserved slot 0 of a zero Store
	}
	var idx uint32
	if n := len(s.free); n > 0 {
		idx = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		idx = s.grow()
	}
	s.gens[idx]++ // even -> odd, the slot is now alive
	n := s.at(idx)
	n.Parent = Handle{}
	n.FirstChild = Handle{}
	n.NextSibling = Handle{}
	n.Key = ""
	n.TypeID = 0
	n.Bounds = geom.Rect{}
	n.Size = geom.Size{}
	n.Flags = FlagMounted | FlagNeedsBuild | FlagNeedsLayout | FlagNeedsPaint
	s.lastChild[idx] = Handle{}
	s.childCount[idx] = 0
	s.alive++
	return Handle{index: idx, gen: s.gens[idx]}
}

// Valid reports whether h refers to a live node of this store.
func (s *Store[P]) Valid(h Handle) bool {
	return h.index != 0 && h.index < s.slots &&
		s.gens[h.index] == h.gen && h.gen&1 == 1
}

// Get returns the node h refers to.
//
// The returned pointer is stable: blocks are never reallocated, so it stays
// usable across further calls to Alloc. It stops being meaningful once the
// slot is freed and recycled, which the generation in the handle detects.
//
// Get panics if h does not refer to a live node. A stale handle is a use
// after free and is reported instead of silently returning the node that now
// occupies the slot.
func (s *Store[P]) Get(h Handle) *Node[P] {
	if !s.Valid(h) {
		panic(s.diagnose(h))
	}
	return s.at(h.index)
}

func (s *Store[P]) diagnose(h Handle) string {
	switch {
	case h.IsZero():
		return "gift/internal/scene: Get on the zero handle"
	case h.index == 0 || h.index >= s.slots:
		return fmt.Sprintf("gift/internal/scene: handle {index:%d gen:%d} is out of range, the store has %d slots",
			h.index, h.gen, s.slots)
	default:
		return fmt.Sprintf("gift/internal/scene: use after free, handle {index:%d gen:%d} refers to a slot that is now at generation %d",
			h.index, h.gen, s.gens[h.index])
	}
}

// ChildCount returns the number of children currently linked under parent.
func (s *Store[P]) ChildCount(parent Handle) int {
	_ = s.Get(parent)
	return int(s.childCount[parent.index])
}

// AppendChild appends child to the child list of parent.
//
// The child must not currently have a parent; reparenting is not supported,
// because the reconciler always frees and remounts instead.
func (s *Store[P]) AppendChild(parent, child Handle) {
	p := s.Get(parent)
	c := s.Get(child)
	if !c.Parent.IsZero() {
		panic(fmt.Sprintf("gift/internal/scene: node {index:%d} already has a parent", child.index))
	}
	c.Parent = parent
	c.NextSibling = Handle{}
	if last := s.lastChild[parent.index]; !last.IsZero() {
		s.at(last.index).NextSibling = child
	} else {
		p.FirstChild = child
	}
	s.lastChild[parent.index] = child
	s.childCount[parent.index]++
}

// SetChildren rewrites the child list of parent to exactly kids, in that
// order.
//
// Every handle in kids must already be a child of parent; this reorders, it
// does not adopt. The reconciler uses it after it has matched keyed children,
// so that the sibling order follows the order of the view list without
// detaching and reattaching anything.
//
// kids must list *every* current child. Omitting one would leave a live node
// with its Parent still pointing here but unreachable from the child list, so
// nothing would ever free it. That is a leak, not a removal, and it is
// rejected with a panic. Remove a child with [Store.Free] or
// [Store.FreeChild] first and then call SetChildren with what is left.
func (s *Store[P]) SetChildren(parent Handle, kids []Handle) {
	p := s.Get(parent)
	if n := int(s.childCount[parent.index]); n != len(kids) {
		panic(fmt.Sprintf(
			"gift/internal/scene: SetChildren on {index:%d} with %d handles, but the node has %d children; "+
				"every current child must be listed, free the ones that go away first",
			parent.index, len(kids), n))
	}
	prev := Handle{}
	for _, k := range kids {
		c := s.Get(k)
		if c.Parent != parent {
			panic(fmt.Sprintf("gift/internal/scene: SetChildren with node {index:%d} that is not a child of {index:%d}", k.index, parent.index))
		}
		if prev.IsZero() {
			p.FirstChild = k
		} else {
			s.at(prev.index).NextSibling = k
		}
		c.NextSibling = Handle{}
		prev = k
	}
	if prev.IsZero() {
		p.FirstChild = Handle{}
	}
	s.lastChild[parent.index] = prev
}

// Free releases h and its whole subtree and detaches h from the child list of
// its parent.
//
// Every handle into the released subtree becomes invalid. Freeing an already
// invalid handle panics, because doing so twice is always a bug in the caller.
func (s *Store[P]) Free(h Handle) {
	n := s.Get(h)
	parent := n.Parent
	if !parent.IsZero() && s.Valid(parent) {
		s.detach(parent, h)
	}
	s.freeSubtree(h, 0)
}

// FreeChild releases the subtree at child without repairing the sibling list
// of parent.
//
// It is the cheap counterpart of [Store.Free] for a caller that is going to
// rewrite the whole child list anyway: the reconciler drops the children that
// went away and then calls [Store.SetChildren] with the survivors, so paying
// for an O(n) sibling walk per removed child only to have the chain rewritten
// immediately afterwards is wasted work.
//
// The child list of parent is left dangling: it still contains the freed
// handles. The caller must call [Store.SetChildren] before anything traverses
// the tree again. The child count is updated, so SetChildren will accept
// exactly the surviving children.
func (s *Store[P]) FreeChild(parent, child Handle) {
	c := s.Get(child)
	if c.Parent != parent {
		panic(fmt.Sprintf("gift/internal/scene: FreeChild with node {index:%d} that is not a child of {index:%d}", child.index, parent.index))
	}
	_ = s.Get(parent)
	s.childCount[parent.index]--
	s.freeSubtree(child, 0)
}

// detach removes child from the sibling list of parent.
func (s *Store[P]) detach(parent, child Handle) {
	p := s.at(parent.index)
	prev := Handle{}
	for cur := p.FirstChild; !cur.IsZero(); {
		next := s.at(cur.index).NextSibling
		if cur == child {
			if prev.IsZero() {
				p.FirstChild = next
			} else {
				s.at(prev.index).NextSibling = next
			}
			if s.lastChild[parent.index] == child {
				s.lastChild[parent.index] = prev
			}
			s.childCount[parent.index]--
			return
		}
		prev = cur
		cur = next
	}
}

// freeSubtree releases h and all of its descendants without touching the
// sibling list of the parent.
func (s *Store[P]) freeSubtree(h Handle, depth int) {
	if depth > MaxDepth {
		panic(fmt.Sprintf("gift/internal/scene: tree deeper than %d levels while freeing a subtree; "+
			"this is a runaway recursion in a component function, not a layout", MaxDepth))
	}
	if !s.Valid(h) {
		return
	}
	n := s.at(h.index)
	for c := n.FirstChild; !c.IsZero(); {
		next := s.at(c.index).NextSibling
		s.freeSubtree(c, depth+1)
		c = next
	}
	// Drop the references that would otherwise keep memory alive. The payload
	// is deliberately left alone; see the package documentation.
	n.Parent = Handle{}
	n.FirstChild = Handle{}
	n.NextSibling = Handle{}
	n.Key = ""
	n.TypeID = 0
	n.Bounds = geom.Rect{}
	n.Size = geom.Size{}
	n.Flags = 0
	s.lastChild[h.index] = Handle{}
	s.childCount[h.index] = 0
	s.gens[h.index]++ // odd -> even, the slot is now free
	s.free = append(s.free, h.index)
	s.alive--
}

// Len returns the number of live nodes.
func (s *Store[P]) Len() int { return s.alive }

// Cap returns the number of node slots currently backed by memory, including
// free ones and the reserved slot 0.
func (s *Store[P]) Cap() int { return len(s.blocks) * blockSize }
