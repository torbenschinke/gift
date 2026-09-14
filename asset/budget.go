package asset

import (
	"context"
	"sync"
	"sync/atomic"
)

// budget is a counting semaphore in bytes.
//
// It is the mechanism behind "Jeder Pipelineabschnitt hat ein Bytebudget. Ein
// Channel-Limit allein genuegt nicht" in the project plan, section 9. A channel
// bounds the number of jobs, and the number of jobs says nothing about memory:
// eight slots hold eight thumbnails or eight 8000 by 6000 decodes, and the
// difference is two gigabytes.
//
// A budget is observable. [budget.snapshot] reports the limit, the current
// occupancy, the high water mark and how often somebody had to wait, which is
// what makes saturation an assertable property rather than a comment.
type budget struct {
	mu      sync.Mutex
	limit   int64
	used    int64
	peak    int64
	waiters int
	// free is closed and replaced whenever bytes are returned, which is a
	// broadcast to every waiter. A buffered channel would wake exactly one,
	// and the one it woke might be the one asking for more than was freed.
	free   chan struct{}
	closed bool

	waits    atomic.Uint64
	rejected atomic.Uint64
}

func newBudget(limit int64) *budget {
	return &budget{limit: limit, free: make(chan struct{})}
}

// acquire reserves n bytes, blocking until they are available, the context is
// cancelled or the budget is closed.
//
// A request larger than the whole limit can never be satisfied and is refused
// immediately with [ErrTooLarge] rather than blocking for ever. That is the
// difference between a budget and a deadlock.
func (b *budget) acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		return nil
	}
	if n > b.limit {
		b.rejected.Add(1)
		return ErrTooLarge
	}
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return ErrClosed
		}
		if b.used+n <= b.limit {
			b.used += n
			if b.used > b.peak {
				b.peak = b.used
			}
			b.mu.Unlock()
			return nil
		}
		wait := b.free
		b.waiters++
		b.mu.Unlock()

		b.waits.Add(1)
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.waiters--
			b.mu.Unlock()
			return ctx.Err()
		case <-wait:
			b.mu.Lock()
			b.waiters--
			b.mu.Unlock()
		}
	}
}

// tryAcquire reserves n bytes if they are available right now.
func (b *budget) tryAcquire(n int64) bool {
	if n <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.used+n > b.limit {
		return false
	}
	b.used += n
	if b.used > b.peak {
		b.peak = b.used
	}
	return true
}

// release returns n bytes and wakes every waiter.
func (b *budget) release(n int64) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	b.used -= n
	if b.used < 0 {
		// A negative occupancy means a double release, which is a
		// programming error inside this package and not a condition of the
		// outside world. It is clamped rather than panicked on, because
		// the panic would happen on a worker goroutine and take the
		// application down; the counter makes it visible instead.
		b.used = 0
	}
	if b.waiters > 0 {
		close(b.free)
		b.free = make(chan struct{})
	}
	b.mu.Unlock()
}

func (b *budget) close() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.free)
		b.free = make(chan struct{})
	}
	b.mu.Unlock()
}

// BudgetStats is one byte budget at one instant.
type BudgetStats struct {
	// Limit is the configured ceiling in bytes.
	Limit int64
	// InUse is the number of bytes reserved right now.
	InUse int64
	// Peak is the high water mark since the pipeline started. This is the
	// number a saturation test asserts against: it must approach the limit
	// and must never exceed it.
	Peak int64
	// Waits is the number of times a worker had to wait for bytes, and
	// Rejected the number of reservations refused because they were larger
	// than the whole budget.
	Waits, Rejected uint64
}

func (b *budget) snapshot() BudgetStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BudgetStats{
		Limit:    b.limit,
		InUse:    b.used,
		Peak:     b.peak,
		Waits:    b.waits.Load(),
		Rejected: b.rejected.Load(),
	}
}
