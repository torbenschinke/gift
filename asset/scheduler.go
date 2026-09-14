package asset

import (
	"sync"
	"time"
)

// job is one unit of work: one picture at one ladder rung.
//
// Several requests share a job; that is what deduplication means here. The job
// is the thing the worker runs, the waiters are the things that want the
// answer.
type job struct {
	id   ID
	rung int
	src  Source
	prio Priority

	// waiters are the accepted requests. They are protected by the
	// scheduler's mutex.
	waiters []waiter

	queued  bool
	running bool
	done    bool

	// encoded, ifRevision and fresh are worker scratch, touched only by the
	// goroutine that is running the job.
	encoded    int64
	ifRevision string
	fresh      time.Duration
}

type waiter struct {
	id   uint64
	gen  uint64
	prio Priority
	fn   func(Result)
}

type jobKey struct {
	id   ID
	rung int
}

// scheduler is the priority queue and the deduplication table.
//
// It is deliberately one mutex and two slices. A lock free design here would be
// defending against contention that does not exist: the producers are a
// gallery's layout pass, sixty times a second, and the consumers are four
// workers that hold the lock for a map lookup.
type scheduler struct {
	mu    sync.Mutex
	cond  *sync.Cond
	jobs  map[jobKey]*job
	vis   []*job
	pre   []*job
	limit int

	nextID uint64
	closed bool
}

func newScheduler(limit int) *scheduler {
	s := &scheduler{jobs: make(map[jobKey]*job), limit: limit}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// add joins or creates a job for req.
//
// The returns are, in order: the job, the identity of this request's waiter,
// the jobs evicted to make room, whether an existing job was joined, whether a
// queued prefetch was promoted, and whether the request was accepted at all.
func (s *scheduler) add(id ID, rung int, req Request) (
	j *job, wid uint64, evicted []*job, joined, promoted, ok bool,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, nil, false, false, false
	}
	k := jobKey{id, rung}
	s.nextID++
	wid = s.nextID
	w := waiter{id: wid, gen: req.Generation, prio: req.Priority, fn: req.OnResult}

	if j, exists := s.jobs[k]; exists {
		j.waiters = append(j.waiters, w)
		if req.Priority > j.prio {
			j.prio = req.Priority
			if j.queued {
				// Promotion: move it to the visible queue. The stale
				// entry in the prefetch queue is skipped on pop,
				// because a job only runs once.
				s.vis = append(s.vis, j)
				promoted = true
				s.cond.Signal()
			}
		}
		return j, wid, nil, true, promoted, true
	}

	if s.queued() >= s.limit {
		if req.Priority != Visible {
			return nil, 0, nil, false, false, false
		}
		// A visible request evicts the oldest waiting prefetch. Refusing
		// the visible one instead would mean the picture the user is
		// looking at loses to a guess about where they might scroll.
		victim := s.evictOldestPrefetch()
		if victim == nil {
			return nil, 0, nil, false, false, false
		}
		evicted = append(evicted, victim)
	}

	j = &job{id: id, rung: rung, src: req.Source, prio: req.Priority,
		waiters: []waiter{w}, queued: true}
	s.jobs[k] = j
	if req.Priority == Visible {
		s.vis = append(s.vis, j)
	} else {
		s.pre = append(s.pre, j)
	}
	s.cond.Signal()
	return j, wid, evicted, false, false, true
}

func (s *scheduler) queued() int { return len(s.vis) + len(s.pre) }

// evictOldestPrefetch removes the least recent waiting prefetch job and
// returns it, or nil when there is none.
func (s *scheduler) evictOldestPrefetch() *job {
	for i, j := range s.pre {
		if !j.queued {
			continue
		}
		s.pre = append(s.pre[:i], s.pre[i+1:]...)
		j.queued = false
		j.done = true
		delete(s.jobs, jobKey{j.id, j.rung})
		return j
	}
	return nil
}

// next blocks until a job is available and returns it, or nil once the
// scheduler is closed.
//
// Visible work is taken first, always. A prefetch that was promoted while it
// waited appears in both queues and is taken from the visible one; the entry
// left behind is skipped because the job is no longer queued.
func (s *scheduler) next() *job {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if j := s.pop(&s.vis); j != nil {
			return j
		}
		if j := s.pop(&s.pre); j != nil {
			return j
		}
		if s.closed {
			return nil
		}
		s.cond.Wait()
	}
}

func (s *scheduler) pop(q *[]*job) *job {
	for len(*q) > 0 {
		j := (*q)[0]
		*q = (*q)[1:]
		if !j.queued {
			continue // cancelled, evicted, or the duplicate of a promotion
		}
		j.queued = false
		j.running = true
		return j
	}
	return nil
}

// done removes the job from the table and returns its waiters.
func (s *scheduler) done(j *job) []waiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !j.done {
		j.done = true
		if cur, ok := s.jobs[jobKey{j.id, j.rung}]; ok && cur == j {
			delete(s.jobs, jobKey{j.id, j.rung})
		}
	}
	ws := j.waiters
	j.waiters = nil
	j.running = false
	return ws
}

// remove withdraws one waiter and reports whether it was still there.
//
// When the last waiter of a queued job is withdrawn the job is dropped
// entirely: nobody wants the answer any more, and doing the work would be
// spending a decode on a picture that has scrolled off the screen. A running
// job is left to finish — the decoders are not interruptible — but its result
// is delivered to nobody.
func (s *scheduler) remove(j *job, wid uint64) bool {
	if j == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i, w := range j.waiters {
		if w.id == wid {
			j.waiters = append(j.waiters[:i], j.waiters[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if len(j.waiters) == 0 && j.queued {
		j.queued = false
		j.done = true
		if cur, ok := s.jobs[jobKey{j.id, j.rung}]; ok && cur == j {
			delete(s.jobs, jobKey{j.id, j.rung})
		}
	}
	return true
}

// close wakes every worker and returns the jobs that never ran, so that their
// waiters can be told.
func (s *scheduler) close() []*job {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	var out []*job
	for _, q := range [][]*job{s.vis, s.pre} {
		for _, j := range q {
			if j.queued {
				j.queued = false
				out = append(out, j)
			}
		}
	}
	s.vis, s.pre = nil, nil
	clear(s.jobs)
	s.cond.Broadcast()
	return out
}

// stats reports the queue occupancy.
func (s *scheduler) stats() (visible, prefetch, inflight int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.vis {
		if j.queued {
			visible++
		}
	}
	for _, j := range s.pre {
		if j.queued {
			prefetch++
		}
	}
	return visible, prefetch, len(s.jobs)
}
