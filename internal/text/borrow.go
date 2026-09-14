package text

import "fmt"

// borrowToken is the shared generation counter of one cache entry.
//
// It lives in its own heap object, allocated once per entry and never
// recycled, which is the whole point: the [Paragraph] a caller borrows lives
// *inside* the entry and is overwritten when the entry is reused, so nothing
// stored at that address can be trusted to describe it. The token can, because
// the borrower holds a pointer to it and eviction only ever increments the
// number it holds.
type borrowToken struct {
	// gen is incremented every time the entry is evicted, which is the
	// moment every outstanding borrow of it became invalid.
	gen uint64
}

// checkBorrow panics when p is a borrow of an entry that has since been
// evicted.
//
// # Why this exists
//
// [Shaper.Layout] returns a pointer into the cache. The contract is that a
// caller reads it and drops it, and the comment on [Shaper.alloc] justified an
// allocation per entry by saying that a violation "should not produce
// plausible stale numbers". It produced exactly that: the entry is recycled at
// a stable address, so a borrow that outlived its eviction went on returning
// numbers, they were simply somebody else's. The review watched a width go
// from 91.3 to 108.6 under a pointer nobody had touched.
//
// So the violation is now a panic rather than a wrong number, under the
// giftdebug build tag, where the project plan, section 15, puts the checks
// whose cost the frame path cannot carry. In the release build borrowChecks is
// a constant false and none of this is compiled in; the field it reads is a
// nil pointer that is never written.
//
// # What it catches and what it cannot
//
// It catches a borrow that is read after its entry was evicted, and a *copy*
// of a borrowed Paragraph read after the same — which is how every such
// violation has actually looked, including the one in this package's own test
// suite.
//
// It cannot catch the case where the evicted slot has already been refilled
// with a different paragraph *and* the caller reads through the original
// pointer, because at that point the memory the pointer names has been
// overwritten with a consistent, valid, different paragraph and there is
// nothing left at that address to disagree with. No check that lives in the
// borrowed value can see that; the only cure would be never to reuse the
// storage, which is the allocation this cache exists to avoid. It is stated
// here rather than implied by silence.
//
// # Where it is called
//
// From the methods of Paragraph. The exported *fields* are not guarded and
// cannot be: reading p.Size is a load, not a call. A caller that keeps a
// borrow long enough to matter almost always asks it a question, and that is
// where the panic comes from.
func (p *Paragraph) checkBorrow(what string) {
	if !borrowChecks || p.tok == nil {
		return
	}
	if p.tok.gen != p.gen {
		panic(fmt.Sprintf(
			"gift/internal/text: Paragraph.%s on a paragraph borrowed from a cache entry that has "+
				"been evicted %d time(s) since (generation %d, current %d).\n"+
				"Shaper.Layout returns a pointer into the shaping cache; it is valid until the next "+
				"Layout or Tick call and must not be kept, and copying the struct copies the same "+
				"borrow rather than the data. Copy what you need out of it instead.",
			what, p.tok.gen-p.gen, p.gen, p.tok.gen))
	}
}
