// Package scene provides the retained node storage of gift.
//
// # Why indices instead of pointers
//
// Nodes live in one flat, reused slice and are referenced by a [Handle], not
// by a pointer. That keeps the tree in a small number of contiguous
// allocations, makes recycling cheap and keeps the garbage collector out of
// the frame path. The price is that a handle can outlive the node it refers
// to, which is why every handle carries a generation; see [Handle].
//
// # Contract
//
// This package knows nothing about views, state or rendering. It only knows
// parent and sibling links, the layout results and an opaque payload that the
// module root fills in. It must never import the module root.
//
// A Store is not safe for concurrent use. It belongs to the UI executor.
package scene

import (
	"fmt"

	"github.com/torbenschinke/gift/geom"
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
	FlagNeedsLayout
	// FlagNeedsPaint marks a node whose drawing operations must be produced
	// again.
	FlagNeedsPaint
	// FlagMounted marks a node that is part of the live tree.
	FlagMounted
)

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
type Node struct {
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
	// Payload points at type specific data. The module root owns its
	// contents; this package only clears it on free so that the garbage
	// collector can reclaim it.
	Payload any
}

// Store owns the nodes of one tree.
//
// Slot 0 is reserved so that the zero [Handle] is never a valid node.
// Generations start at 1 and are incremented on both allocation and release,
// so an odd generation means the slot is alive and an even one means it is
// free.
//
// Generation wraparound: a slot that is allocated and released 2^31 times
// wraps its generation back to a previously used value, at which point a stale
// handle from that far in the past would validate again. At one allocation per
// slot per frame and 60 frames per second that takes more than a year of
// continuous operation for a single slot, so the wraparound is documented
// rather than defended against. Widening the generation to 64 bit would double
// the size of every handle for no practical gain.
type Store struct {
	nodes []Node
	gens  []uint32
	// lastChild caches the last child per slot so that AppendChild is O(1)
	// instead of walking the sibling list. It is kept out of Node because it
	// is an implementation detail of this package.
	lastChild []Handle
	free      []uint32
	alive     int
}

// NewStore returns a store preallocated for capacity nodes. The store grows on
// demand; the capacity only decides how much warmup it takes before allocation
// stops.
func NewStore(capacity int) *Store {
	if capacity < 1 {
		capacity = 1
	}
	s := &Store{
		nodes:     make([]Node, 1, capacity+1),
		gens:      make([]uint32, 1, capacity+1),
		lastChild: make([]Handle, 1, capacity+1),
		free:      make([]uint32, 0, capacity),
	}
	return s
}

// Alloc returns a handle to a fresh, zeroed node.
//
// Released slots are reused, so after warmup Alloc performs no allocation at
// all.
func (s *Store) Alloc() Handle {
	if len(s.nodes) == 0 {
		s.nodes = append(s.nodes, Node{})
		s.gens = append(s.gens, 0)
		s.lastChild = append(s.lastChild, Handle{})
	}
	var idx uint32
	if n := len(s.free); n > 0 {
		idx = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		s.nodes = append(s.nodes, Node{})
		s.gens = append(s.gens, 0)
		s.lastChild = append(s.lastChild, Handle{})
		idx = uint32(len(s.nodes) - 1)
	}
	s.gens[idx]++ // even -> odd, the slot is now alive
	s.nodes[idx] = Node{Flags: FlagMounted | FlagNeedsBuild | FlagNeedsLayout | FlagNeedsPaint}
	s.lastChild[idx] = Handle{}
	s.alive++
	return Handle{index: idx, gen: s.gens[idx]}
}

// Valid reports whether h refers to a live node of this store.
func (s *Store) Valid(h Handle) bool {
	return h.index != 0 && int(h.index) < len(s.nodes) &&
		s.gens[h.index] == h.gen && h.gen&1 == 1
}

// Get returns the node h refers to.
//
// The returned pointer is borrowed and stays valid only until the next Alloc,
// because the backing array may be reallocated. Do not keep it across calls
// that can grow the store.
//
// Get panics if h does not refer to a live node. A stale handle is a use
// after free and is reported instead of silently returning the node that now
// occupies the slot.
func (s *Store) Get(h Handle) *Node {
	if !s.Valid(h) {
		panic(s.diagnose(h))
	}
	return &s.nodes[h.index]
}

func (s *Store) diagnose(h Handle) string {
	switch {
	case h.IsZero():
		return "gift/internal/scene: Get on the zero handle"
	case h.index == 0 || int(h.index) >= len(s.nodes):
		return fmt.Sprintf("gift/internal/scene: handle {index:%d gen:%d} is out of range, the store has %d slots",
			h.index, h.gen, len(s.nodes))
	default:
		return fmt.Sprintf("gift/internal/scene: use after free, handle {index:%d gen:%d} refers to a slot that is now at generation %d",
			h.index, h.gen, s.gens[h.index])
	}
}

// AppendChild appends child to the child list of parent.
//
// The child must not currently have a parent; reparenting is not supported,
// because the reconciler always frees and remounts instead.
func (s *Store) AppendChild(parent, child Handle) {
	p := s.Get(parent)
	c := s.Get(child)
	if !c.Parent.IsZero() {
		panic(fmt.Sprintf("gift/internal/scene: node {index:%d} already has a parent", child.index))
	}
	c.Parent = parent
	c.NextSibling = Handle{}
	if last := s.lastChild[parent.index]; !last.IsZero() {
		s.nodes[last.index].NextSibling = child
	} else {
		p.FirstChild = child
	}
	s.lastChild[parent.index] = child
}

// SetChildren rewrites the child list of parent to exactly kids, in that
// order.
//
// Every handle in kids must already be a child of parent; this reorders, it
// does not adopt. The reconciler uses it after it has matched keyed children,
// so that the sibling order follows the order of the view list without
// detaching and reattaching anything.
func (s *Store) SetChildren(parent Handle, kids []Handle) {
	p := s.Get(parent)
	prev := Handle{}
	for _, k := range kids {
		c := s.Get(k)
		if c.Parent != parent {
			panic(fmt.Sprintf("gift/internal/scene: SetChildren with node {index:%d} that is not a child of {index:%d}", k.index, parent.index))
		}
		if prev.IsZero() {
			p.FirstChild = k
		} else {
			s.nodes[prev.index].NextSibling = k
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
func (s *Store) Free(h Handle) {
	n := s.Get(h)
	parent := n.Parent
	if !parent.IsZero() && s.Valid(parent) {
		s.detach(parent, h)
	}
	s.freeSubtree(h)
}

// detach removes child from the sibling list of parent.
func (s *Store) detach(parent, child Handle) {
	p := &s.nodes[parent.index]
	prev := Handle{}
	for cur := p.FirstChild; !cur.IsZero(); {
		next := s.nodes[cur.index].NextSibling
		if cur == child {
			if prev.IsZero() {
				p.FirstChild = next
			} else {
				s.nodes[prev.index].NextSibling = next
			}
			if s.lastChild[parent.index] == child {
				s.lastChild[parent.index] = prev
			}
			return
		}
		prev = cur
		cur = next
	}
}

// freeSubtree releases h and all of its descendants without touching the
// sibling list of the parent.
func (s *Store) freeSubtree(h Handle) {
	if !s.Valid(h) {
		return
	}
	n := &s.nodes[h.index]
	for c := n.FirstChild; !c.IsZero(); {
		next := s.nodes[c.index].NextSibling
		s.freeSubtree(c)
		c = next
	}
	// Drop references so that the payload and the key can be collected.
	*n = Node{}
	s.lastChild[h.index] = Handle{}
	s.gens[h.index]++ // odd -> even, the slot is now free
	s.free = append(s.free, h.index)
	s.alive--
}

// Len returns the number of live nodes.
func (s *Store) Len() int { return s.alive }

// Cap returns the number of node slots currently allocated, including free
// ones and the reserved slot 0.
func (s *Store) Cap() int { return cap(s.nodes) }
