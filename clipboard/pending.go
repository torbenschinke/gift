package clipboard

import "time"

// This file is the bookkeeping of a paste in flight, and it carries **no build
// tag on purpose**, for the reason selection.go gives for the same choice: the
// only machine this project is developed on has no X server, so logic that
// lives behind `//go:build linux` is logic no test in the acceptance matrix
// ever executes. What is here is arithmetic over a clock and a pointer, with
// no libX11 in it, and it is covered by `go test ./...` everywhere.
//
// # What went wrong before, and why this type exists
//
// Under X11 a paste is not a call, it is a request to another process: send
// ConvertSelection, then wait for a SelectionNotify that only the current
// owner of the selection can cause. The event thread therefore has to remember
// the request while it waits, and there is room for exactly one — there is one
// property on one window to deliver into, so a second paste cannot be
// interleaved with the first.
//
// The defect that this type was extracted to fix: the slot was cleared only on
// a SelectionNotify, which is precisely the event that does not arrive when
// the owner is dead, wedged or stopped. The caller's own timeout, [ReadTimeout],
// is the caller's: it unblocks the UI goroutine and tells it nothing reached
// it, but it cannot reach into the event thread. So one unresponsive owner —
// `xclip -i` followed by SIGSTOP is a two command reproduction — left the slot
// occupied for the life of the process, and every later paste was refused with
// "a previous paste is still waiting for the selection owner". Paste was dead
// until the application was restarted.
//
// The cure is to stamp the read with a deadline and to let the *next* command
// clear an expired one. That is enough, and it is deliberately not a timer:
// nothing needs to happen when a read expires unattended, because the only
// thing an occupied slot obstructs is another paste, and another paste is an
// event that arrives on this very thread.

// readSlot holds the paste in flight, if there is one, and decides when a
// paste that never got its answer stops standing in the way of the next one.
//
// The zero value is not usable; timeout must be set. It is a field rather than
// the [ReadTimeout] constant so that this file stays free of the X11 build tag
// and so that a test can drive a deadline without sleeping.
type readSlot struct {
	// cur is the read in flight, or nil.
	cur *x11Read

	// timeout is how long a read may occupy the slot. It is the same value
	// the caller waits for, and it is measured from later — the caller
	// started its timer before it woke this thread — so when it expires here
	// the caller has certainly already given up.
	timeout time.Duration

	// now is the clock, so that a test does not have to sleep. nil means
	// [time.Now].
	now func() time.Time

	// abandoned counts reads dropped because they outlived their deadline.
	// It is the number worth logging: every increment is a selection owner
	// that did not answer, and a value that climbs is the diagnosis of a
	// misbehaving clipboard manager rather than of gift.
	abandoned uint64
}

func (s *readSlot) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// begin claims the slot for cmd and returns the read to drive, or false when a
// paste that is genuinely still in flight holds it.
//
// "Genuinely" is the whole of the decision: a read whose deadline has passed
// is dropped, counted and replaced, because its caller is gone and the only
// thing keeping it is the answer that never came. A read inside its deadline
// is left alone and the newcomer is refused, which is the original and correct
// behaviour — two pastes cannot share one property.
func (s *readSlot) begin(cmd *x11Command) (*x11Read, bool) {
	now := s.clock()
	if s.cur != nil {
		if now.Before(s.cur.deadline) {
			return nil, false
		}
		s.cur = nil
		s.abandoned++
	}
	s.cur = &x11Read{cmd: cmd, deadline: now.Add(s.timeout)}
	return s.cur, true
}

// current returns the read a SelectionNotify belongs to, or nil when there is
// none — a late answer to a paste that has been given up on.
//
// The deadline is deliberately *not* consulted here. An answer that arrives
// late but before anything else wanted the slot is a good answer to a question
// somebody asked, and refusing it would throw away a paste for no gain; the
// caller that timed out is gone either way and [x11Command.done] is buffered
// for exactly that.
func (s *readSlot) current() *x11Read { return s.cur }

// done releases the slot.
func (s *readSlot) done() { s.cur = nil }

// x11Command is one unit of work for the event thread. done is buffered with
// one slot, so the thread can always answer even if the caller has already
// timed out and walked away.
type x11Command struct {
	write bool
	text  string
	done  chan x11Result
}

type x11Result struct {
	text string
	ok   bool
	err  error
}

// x11Read is a paste in flight: the command that asked for it, how far through
// [atoms.readTargets] it has got, and when it stops being worth waiting for.
//
// The deadline does not move when the read falls back to the next target. It
// is the caller's budget for the whole paste and not for one round trip, and
// an owner that refuses three targets slowly is exactly as unhelpful as one
// that refuses one target slowly.
//
// What remains, stated rather than hidden: if the owner of an abandoned read
// eventually does answer, and it answers while a *later* paste is in flight,
// that SelectionNotify is indistinguishable from the later paste's own — the
// event names the selection and the target, and both reads used both. The
// later paste then returns the older owner's text or, for a late refusal, an
// empty clipboard. Both are wrong answers to one paste; neither can wedge the
// slot, because every read now has a deadline. Making even that impossible
// means a distinct delivery property per read, so that the property in the
// event identifies the read — about ten lines, worth doing on a machine where
// the X11 path can be executed, and not worth doing blind.
type x11Read struct {
	cmd      *x11Command
	target   int
	deadline time.Time
}
