package clipboard

import (
	"testing"
	"time"
)

// The tests of the paste slot. They run everywhere, including on a machine
// with no X server, which is the reason pending.go carries no build tag; see
// the comment at the top of that file.

// slotTimeout is the value the X11 half passes in, spelled out here because
// ReadTimeout itself only exists in the linux build and this file is compiled
// everywhere.
const slotTimeout = 500 * time.Millisecond

func newTestSlot() (*readSlot, *time.Time) {
	t0 := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	now := &t0
	return &readSlot{timeout: slotTimeout, now: func() time.Time { return *now }}, now
}

func cmd() *x11Command { return &x11Command{done: make(chan x11Result, 1)} }

// TestAPasteThatNeverGotAnAnswerDoesNotWedgeEveryLaterPaste is the regression
// test of the blocker this file was extracted for. The sequence is the one a
// stopped selection owner produces:
//
//	xclip -selection clipboard -i <<< hi
//	kill -STOP $(pgrep xclip)
//
// and then two pastes. Before the fix the first one timed out in the caller,
// the slot stayed occupied for ever, and every paste for the life of the
// process was refused immediately. Paste was dead until a restart.
func TestAPasteThatNeverGotAnAnswerDoesNotWedgeEveryLaterPaste(t *testing.T) {
	s, now := newTestSlot()

	first := cmd()
	if _, ok := s.begin(first); !ok {
		t.Fatal("the first paste could not claim an empty slot")
	}

	// The owner is stopped, so no SelectionNotify ever arrives and the slot
	// is never released. The caller times out on its own, elsewhere.
	*now = now.Add(slotTimeout + time.Millisecond)

	second := cmd()
	r, ok := s.begin(second)
	if !ok {
		t.Fatal("the paste after a timed out one was refused; one unresponsive selection " +
			"owner has disabled paste for the life of the process")
	}
	if r.cmd != second {
		t.Fatal("the slot handed back the abandoned read rather than the new one")
	}
	if s.abandoned != 1 {
		t.Errorf("abandoned = %d, want 1; the count is what a log line reports and what tells "+
			"an operator that the owner, not gift, is at fault", s.abandoned)
	}

	// And it keeps working: a third one after the second also expires.
	*now = now.Add(slotTimeout + time.Millisecond)
	if _, ok := s.begin(cmd()); !ok {
		t.Fatal("the third paste was refused, so the recovery is single use")
	}
	if s.abandoned != 2 {
		t.Errorf("abandoned = %d, want 2", s.abandoned)
	}
}

// TestASecondPasteInsideTheDeadlineIsStillRefused is the other direction, and
// it is what keeps the fix from being "clear the slot always": two pastes
// cannot share one delivery property, so a paste that is genuinely in flight
// must still turn the newcomer away.
func TestASecondPasteInsideTheDeadlineIsStillRefused(t *testing.T) {
	s, now := newTestSlot()
	if _, ok := s.begin(cmd()); !ok {
		t.Fatal("the first paste could not claim an empty slot")
	}
	*now = now.Add(slotTimeout - time.Millisecond)
	if _, ok := s.begin(cmd()); ok {
		t.Fatal("a paste that is still inside its deadline was displaced; its answer would then " +
			"be delivered into the newcomer's read")
	}
	if s.abandoned != 0 {
		t.Errorf("abandoned = %d, want 0: nothing was given up on", s.abandoned)
	}
	// The boundary, pinned rather than left to chance: a read is in flight
	// strictly before its deadline and is displaceable from the instant it
	// is reached. Which side of the equality the boundary falls on does not
	// matter to anything real — the two clocks are not the same clock — but
	// an unpinned boundary is how a "never fires" and an "always fires"
	// version of this look identical in review.
	*now = now.Add(time.Millisecond)
	if _, ok := s.begin(cmd()); !ok {
		t.Fatal("a read was not displaceable at exactly its deadline")
	}
}

// TestAFinishedPasteFreesTheSlotImmediately, so that the deadline is only ever
// the exceptional path and the ordinary one costs nothing.
func TestAFinishedPasteFreesTheSlotImmediately(t *testing.T) {
	s, _ := newTestSlot()
	r, _ := s.begin(cmd())
	if s.current() != r {
		t.Fatal("current() did not return the read in flight, so a SelectionNotify would be dropped")
	}
	s.done()
	if s.current() != nil {
		t.Fatal("done() left the slot occupied")
	}
	if _, ok := s.begin(cmd()); !ok {
		t.Fatal("the slot was not free after done(), with no time having passed")
	}
	if s.abandoned != 0 {
		t.Errorf("abandoned = %d after an orderly finish, want 0", s.abandoned)
	}
}

// TestALateAnswerIsStillAnAnswer pins the deliberate asymmetry: the deadline
// decides who may take the slot and never decides whether an arriving
// SelectionNotify is used. Consulting it in current() would throw away a good
// answer that nothing else was waiting for.
func TestALateAnswerIsStillAnAnswer(t *testing.T) {
	s, now := newTestSlot()
	r, _ := s.begin(cmd())
	*now = now.Add(10 * slotTimeout)
	if s.current() != r {
		t.Fatal("an answer arriving after the deadline, with nobody else wanting the slot, was dropped")
	}
}
