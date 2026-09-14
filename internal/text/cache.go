package text

import "unsafe"

// This file holds the bookkeeping of the shaping cache: a map from [cacheKey]
// to an entry index plus an intrusive doubly linked LRU list over the entry
// slice.
//
// Indices rather than pointers, for the same reason internal/scene uses them:
// the entries live in one growable slice, the list needs no per node
// allocation, and a lookup plus an LRU move is a handful of integer writes with
// nothing for the garbage collector to scan. That is what makes a cache hit
// allocation free, which the project plan, section 11, requires of anything
// that runs during layout.

const (
	sizeofGlyph = int(unsafe.Sizeof(Glyph{}))
	sizeofRun   = int(unsafe.Sizeof(Run{}))
	sizeofLine  = int(unsafe.Sizeof(Line{}))
	// sizeofEntry is the fixed overhead of an entry: the struct itself, its
	// map key and the map bucket share. It is an estimate, and it is
	// deliberately generous, because underestimating the overhead of many tiny
	// entries is how a byte budget quietly becomes a suggestion.
	sizeofEntry = int(unsafe.Sizeof(entry{})) + int(unsafe.Sizeof(cacheKey{})) + 32
)

// alloc returns the index of an entry ready to be filled.
//
// Evicted entries are recycled with their backing arrays intact, so a workload
// that keeps missing - scrolling through changing labels, say - reuses the same
// glyph arrays instead of asking the allocator for a new one every time.
func (s *Shaper) alloc() int32 {
	if n := len(s.free); n > 0 {
		i := s.free[n-1]
		s.free = s.free[:n-1]
		return i
	}
	// A pointer, not a value in a growable slice: [Shaper.Layout] hands out
	// a *Paragraph that lives inside an entry, and growing a []entry would
	// move it, leaving an earlier borrow pointing at a copy that stops being
	// updated. Borrows are short lived by contract, but a contract violation
	// should not produce plausible stale numbers. One allocation per entry,
	// on the miss path only, buys a stable address.
	s.entries = append(s.entries, &entry{prev: -1, next: -1})
	return int32(len(s.entries) - 1)
}

// account computes and records the size of an entry.
//
// Capacities are counted, not lengths: the capacity is what is actually
// retained, and a recycled entry whose arrays are much larger than its current
// contents must not look cheap.
func (s *Shaper) account(i int32) {
	e := s.entries[i]
	b := sizeofEntry + len(e.key.text) +
		cap(e.glyphs)*sizeofGlyph +
		cap(e.runs)*sizeofRun +
		cap(e.lines)*sizeofLine
	s.bytes += b - e.bytes
	e.bytes = b
}

// pushFront links i at the head of the LRU list and stamps it as used now.
func (s *Shaper) pushFront(i int32) {
	e := s.entries[i]
	e.used = s.tick
	e.prev = -1
	e.next = s.head
	if s.head >= 0 {
		s.entries[s.head].prev = i
	}
	s.head = i
	if s.tail < 0 {
		s.tail = i
	}
}

// unlink removes i from the LRU list.
func (s *Shaper) unlink(i int32) {
	e := s.entries[i]
	if e.prev >= 0 {
		s.entries[e.prev].next = e.next
	} else {
		s.head = e.next
	}
	if e.next >= 0 {
		s.entries[e.next].prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev, e.next = -1, -1
}

// touch marks i as the most recently used entry. It is on the cache hit path
// and must not allocate.
func (s *Shaper) touch(i int32) {
	if s.head == i {
		s.entries[i].used = s.tick
		return
	}
	s.unlink(i)
	s.pushFront(i)
}

// evictToBudget drops least recently used entries until the byte budget is met.
//
// keep is never evicted: it is the entry the caller just asked for, and
// throwing it away would mean the very next identical request misses again. A
// single paragraph larger than the whole budget therefore lives in the cache
// alone, over budget, until something else replaces it. That is reported
// through Stats.Bytes rather than hidden.
func (s *Shaper) evictToBudget(keep int32) {
	for s.bytes > s.cfg.MaxBytes && s.tail >= 0 && s.tail != keep {
		s.evictTail()
		s.stats.Evictions++
	}
}

// evictTail removes the least recently used entry and recycles its slot.
func (s *Shaper) evictTail() {
	i := s.tail
	if i < 0 {
		return
	}
	s.unlink(i)
	e := s.entries[i]
	delete(s.index, e.key)
	s.bytes -= e.bytes
	e.bytes = 0
	// Release the references the collector must not have to keep alive, but
	// keep the backing arrays for the next miss; see alloc.
	e.key = cacheKey{}
	e.par = Paragraph{}
	e.glyphs = e.glyphs[:0]
	clear(e.runs) // a Run holds a *Font, which must not be retained by a dead entry
	e.runs = e.runs[:0]
	clear(e.lines)
	e.lines = e.lines[:0]
	s.free = append(s.free, i)
}
