package gift

import "github.com/torbenschinke/gift/internal/scene"

// This file is the answer to one question: what happens to a notification that
// the core owes a node, when the moment it is owed is a moment in which no
// application handler may run?
//
// # Why there is such a moment
//
// Reconciliation walks the tree and rewrites it. An application handler that
// ran in the middle of that walk could write state, mark scopes dirty and —
// through [EventContext.RequestFocus] or a view that reshapes itself — change
// the very children the reconciler is halfway through matching. So
// [App.setFocus] and the pointer cleanups in [App.applyHidden] must not call
// an interactor while [App.building] is set.
//
// # Why dropping the notification is not an option
//
// It was, until it was measured. [EventFocusLost] is not a courtesy: the
// handler of ui.TextField uses it to cancel its caret enrolment, to end a
// selection drag and to withdraw its request for an on-screen keyboard. A tab
// hidden under a focused field therefore cost 100 of 120 sampled ticks — the
// full ten second CaretBlinkWindow at 60 Hz, drawing no caret — where an
// ordinary blur of the same field costs none. The same argument applies to the
// pointer: a slider whose tab is hidden mid-drag keeps its grab and keeps
// writing the application's value.
//
// # The shape
//
// The notification is queued and delivered at the first point in the frame
// where a handler is allowed to run: after [App.runBuilds] has finished and
// before layout, in [App.Update]. A handler that writes state there marks a
// scope dirty, and the settle loop rebuilds it in the *same* update, so the
// frame the user sees is the frame after the whole thing has come to rest.
// Nothing is dropped and nothing is delivered a frame late.
//
// The queue is a reused slice in the input state, like [inputState.flings],
// so a frame with no deferred notice costs one length check.

// noticeKind is what the core owes the node.
type noticeKind uint8

const (
	// noticeFocusLost is [EventFocusLost] for a node that held the focus when
	// a build took it away.
	noticeFocusLost noticeKind = iota
	// noticePointerCancel is [EventPointerCancel] for a node that held a
	// pointer capture when a build hid it.
	noticePointerCancel
	// noticePointerLeave is [EventPointerLeave] for a node that was hovered
	// when a build hid it.
	noticePointerLeave
)

// notice is one queued notification.
type notice struct {
	node scene.Handle
	kind noticeKind
	// pointer is the id carried into the synthesised event for the two
	// pointer kinds, and is ignored for noticeFocusLost.
	pointer PointerID
}

// maxNoticeRounds bounds the settle loop of [App.settleNotices].
//
// Four is not a tuning constant with a measurement behind it; it is a stop.
// One round is what every case in this project needs — a field is told it lost
// the focus, it withdraws the keyboard, the keyboard view rebuilds, and
// nothing further is owed. The rounds exist because a handler may legitimately
// cause a build that hides something else, and the bound exists because two
// components that hide each other in response to each other's blur would
// otherwise spin the update for ever. Whatever is still queued when the bound
// is reached stays queued and is delivered in the next update: late is bad,
// and an update that never returns is worse.
const maxNoticeRounds = 4

// deferNotice queues one notification for delivery after the build.
//
// A node may be queued for the same kind only once per settle: hiding a layer
// whose descendant is both hovered and captured queues two different kinds,
// and a rebuild that hides the same layer twice queues nothing the second
// time because the flag only changes once.
func (a *App) deferNotice(h scene.Handle, k noticeKind, id PointerID) {
	if !a.store.Valid(h) {
		return
	}
	for _, n := range a.in.notices {
		if n.node == h && n.kind == k {
			return
		}
	}
	a.in.notices = append(a.in.notices, notice{node: h, kind: k, pointer: id})
}

// dropNotice removes a queued notification, for the case in which the reason
// for it went away before it was delivered: a build that clears the focus and
// a later build in the same update that puts it back on the same node.
func (a *App) dropNotice(h scene.Handle, k noticeKind) {
	for i, n := range a.in.notices {
		if n.node == h && n.kind == k {
			a.in.notices = append(a.in.notices[:i], a.in.notices[i+1:]...)
			return
		}
	}
}

// settleNotices delivers the queued notifications and rebuilds whatever they
// dirtied, until nothing is owed or [maxNoticeRounds] is reached.
//
// It is called from [App.Update] between the builds and the layout. That is
// the only place it may be called from: it runs application handlers, so it
// needs [App.building] to be nil, and it must run before the layout so that a
// handler which cancelled an animation or withdrew a keyboard is reflected in
// the geometry of the frame it is part of.
func (a *App) settleNotices() {
	for range maxNoticeRounds {
		if len(a.in.notices) == 0 {
			return
		}
		a.flushNotices()
		a.runBuilds()
	}
}

// flushNotices delivers one round of queued notifications.
//
// The queue is swapped out first, so that a handler which causes a further
// deferral — it cannot, today, because a handler does not run during a build,
// but a future one might through a nested build — appends to a fresh queue
// rather than to the slice being iterated.
func (a *App) flushNotices() {
	pending := a.in.notices
	a.in.notices = a.in.noticeSpare[:0]
	for _, n := range pending {
		a.deliverNotice(n)
	}
	a.in.noticeSpare = pending[:0]
}

// deliverNotice sends one queued notification, if it is still owed.
//
// Still owed is a real question. The node may have been unmounted between the
// build that queued the notice and this call — a tab that was hidden and then
// removed from the bar in the same update — in which case there is nobody to
// tell and [App.forgetNode] has already done the core's own cleanup. And the
// focus may have come back to the node, which is why the focus case asks.
func (a *App) deliverNotice(n notice) {
	if !a.store.Valid(n.node) {
		return
	}
	switch n.kind {
	case noticeFocusLost:
		if a.in.focus == n.node {
			return
		}
		a.notifyDirect(n.node, Event{Kind: EventFocusLost, Time: a.in.now, Pointer: MousePointer})
	case noticePointerCancel:
		a.notifyDirect(n.node, Event{Kind: EventPointerCancel, Time: a.in.now, Pointer: n.pointer})
	case noticePointerLeave:
		a.notifyDirect(n.node, Event{Kind: EventPointerLeave, Time: a.in.now, Pointer: n.pointer})
	}
}

// notifyDirect hands e to the interactor of h and to nobody else, whether or
// not h is disabled.
//
// The disabled part is the whole reason this is not [App.deliver] with direct
// set. [App.deliver] skips a disabled node because a disabled control takes no
// input, and that is right for a tap and a key. These three events are not
// input: they are the core telling a node about a transition the *core* made
// to state the node is holding. A field that is disabled in the same build
// that takes the focus off it still has a caret enrolment, a drag mode and
// possibly an on-screen keyboard request outstanding, and it is the only thing
// in the process that can let go of them.
//
// There is no bubbling, for the reason the focus and enter/leave events are
// already delivered with direct set: an ancestor never held the focus, the
// capture or the hover that is being taken away, so an ancestor has nothing to
// clean up and would only see an event about a child it was told nothing else
// about.
func (a *App) notifyDirect(h scene.Handle, e Event) {
	nd := a.data(h)
	if nd.interactor == nil {
		return
	}
	a.diag.InputEvents++
	c := &a.in.ectx
	prevH, prevD := c.cur, c.nd
	c.cur, c.nd = h, nd
	nd.interactor.HandleEvent(c, e)
	c.cur, c.nd = prevH, prevD
}

// forgetNotices drops every queued notification for h. It is called from
// [App.forgetNode]: an unmounted node cannot be told anything, and leaving a
// dead handle in the queue would make the delivery depend on whether the store
// has reused the slot.
func (a *App) forgetNotices(h scene.Handle) {
	out := a.in.notices[:0]
	for _, n := range a.in.notices {
		if n.node != h {
			out = append(out, n)
		}
	}
	a.in.notices = out
}
