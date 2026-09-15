//go:build linux && !android && !faketime

package clipboard

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// This file is the X11 half. It is the long one, and the length is X11's
// rather than this package's, so the design is written down here once.
//
// # There is no clipboard in X11
//
// There is a selection, which is a name, and a window that claims to own it.
// A copy is not "put these bytes somewhere"; it is "I, this window, will hand
// out the CLIPBOARD selection when asked". A paste is a request to the current
// owner, which answers by writing a property onto the *requestor's* window and
// sending a SelectionNotify back. Two consequences follow and neither is
// avoidable:
//
//   - The copying process must stay alive and must keep answering. That is why
//     this file has an event loop and a thread; it is not an architecture
//     choice.
//   - When the process ends, the copied text is gone. The project plan,
//     section 19, records that as standard behaviour rather than as a defect.
//     A clipboard manager, if the user runs one, takes ownership from us and
//     keeps the text; that is what those programs are for, and it is the only
//     mechanism there is.
//
// # Why an own connection
//
// Ebitengine has a display connection already, inside GLFW, inside internal/,
// which the import rules make unreachable — the same wall that makes this
// package necessary at all. So this file opens its own with XOpenDisplay and
// creates its own window: 1x1, never mapped, InputOnly, therefore invisible
// and costing the server almost nothing. A window is needed because a
// selection owner is a window; it is never drawn into.
//
// # Threading, and how a request reaches the loop
//
// One goroutine, locked to an OS thread with [runtime.LockOSThread], owns the
// connection and blocks in XNextEvent. Locking the thread is not strictly
// required by Xlib, which cares about concurrent access and not about thread
// identity, but a blocking XNextEvent parks the thread for as long as nothing
// happens, and a parked scheduler thread that the Go runtime is free to reuse
// for other goroutines is exactly the situation LockOSThread exists to
// prevent.
//
// The UI goroutine then has to get a request into a loop that is blocked in a
// read. The usual answers are polling — a wakeup every few milliseconds,
// forever, on a Raspberry Pi, for something the user does twice an hour — or a
// self-pipe, which needs the connection's file descriptor and a select, which
// needs golang.org/x/sys and therefore a new direct dependency.
//
// This file uses X11 itself: a *second* display connection, used only by the
// caller under a mutex, sends a ClientMessage to our own window. The X11
// protocol says of SendEvent that if the event mask is empty, the event goes
// to the client that created the destination window — which is us, on the
// first connection. So the event loop wakes up on a real event, drains the
// command channel, and sleeps again. No polling, no timer, no new dependency.
//
// Each of the two connections is touched from exactly one place: connection A
// only from the event thread, connection B only under [x11Clipboard.mu]. That
// is why XInitThreads is not called, and it must not be called late anyway —
// Xlib documents it as the first Xlib call of a process, and by the time a
// user presses Ctrl+C, GLFW has been talking to X for minutes.

// ReadTimeout is how long a paste waits for the owner of the selection to
// answer before giving up and reporting an empty clipboard.
//
// The documentation of [ui.Clipboard] says an implementation may block for the
// milliseconds an X11 round trip costs and must not block for a second. 500 ms
// is inside that: it is fifty times a healthy round trip, so it never fires by
// accident, and it is half of the limit that would be a visible freeze. There
// is no way to do better, because the thing being waited for is another
// process, which may be swapped out, busy, or — the case this exists for —
// dead without having released the selection.
//
// A write uses the same timeout, for the event thread to acknowledge, which is
// local work and never actually takes it.
const ReadTimeout = 500 * time.Millisecond

// X11 protocol constants used below. They are numbers in the protocol, not in
// a header, so writing them out is not a copy of anything.
const (
	evPropertyNotify  = 28
	evSelectionClear  = 29
	evSelectionReq    = 30
	evSelectionNotify = 31
	evClientMessage   = 33

	classInputOnly  = 2
	propModeReplace = 0
	currentTime     = 0
)

// openPlatform is the constructor the portable half calls. See [platform].
func openPlatform(log *slog.Logger) (platform, error) {
	return newX11Clipboard(log)
}

// x11lib is the set of libX11 entry points this package binds. Every field is
// bound once in [loadX11]; a missing symbol is a failure to open, not a nil
// call later.
type x11lib struct {
	openDisplay       func(name string) uintptr
	defaultRootWindow func(dpy uintptr) uint64
	internAtom        func(dpy uintptr, name string, onlyIfExists bool) uint64
	createWindow      func(dpy uintptr, parent uint64, x, y int32, w, h, borderWidth uint32, depth int32, class uint32, visual uintptr, valueMask uint64, attrs uintptr) uint64
	selectInput       func(dpy uintptr, w uint64, mask int64) int32
	setSelectionOwner func(dpy uintptr, selection, owner, t uint64) int32
	getSelectionOwner func(dpy uintptr, selection uint64) uint64
	convertSelection  func(dpy uintptr, selection, target, property, requestor, t uint64) int32
	nextEvent         func(dpy uintptr, ev unsafe.Pointer) int32
	sendEvent         func(dpy uintptr, w uint64, propagate bool, mask int64, ev unsafe.Pointer) int32
	changeProperty    func(dpy uintptr, w, property, typ uint64, format, mode int32, data unsafe.Pointer, nelements int32) int32
	getWindowProperty func(dpy uintptr, w, property uint64, offset, length int64, del bool, reqType uint64, actualType, actualFormat, nitems, bytesAfter unsafe.Pointer, prop *unsafe.Pointer) int32
	free              func(p unsafe.Pointer) int32
	flush             func(dpy uintptr) int32
	setErrorHandler   func(h uintptr) uintptr
}

// x11Clipboard is the platform implementation. Everything mutable that X11
// touches lives on the event goroutine; the fields here are either immutable
// after construction or guarded by mu.
type x11Clipboard struct {
	log *slog.Logger
	lib x11lib

	dpy uintptr // connection A, event thread only
	win uint64
	a   atoms

	// mu serialises callers and guards connection B, which exists only to
	// wake the event thread. See the file comment.
	mu      sync.Mutex
	wakeDpy uintptr

	// wakeAtom is the message type of the ClientMessage that unblocks
	// XNextEvent. Its only job is to be recognisable.
	wakeAtom xAtom

	// cmds carries work to the event thread. Buffered, so that a caller that
	// is a little ahead of the loop does not block before it has even sent
	// the wakeup.
	cmds chan *x11Command
}

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

func newX11Clipboard(log *slog.Logger) (*x11Clipboard, error) {
	display := os.Getenv("DISPLAY")
	if display == "" {
		return nil, fmt.Errorf("%w: DISPLAY is not set, so there is no X server to talk to", ErrUnsupported)
	}
	lib, err := loadX11()
	if err != nil {
		return nil, err
	}

	c := &x11Clipboard{log: log, lib: lib, cmds: make(chan *x11Command, 8)}

	// The error handler goes in before the first request that could provoke
	// one. See [installErrorHandler] for why this is chained rather than
	// simply replaced.
	installErrorHandler(lib, log)

	c.dpy = lib.openDisplay(display)
	if c.dpy == 0 {
		return nil, fmt.Errorf("%w: XOpenDisplay(%q) failed", ErrUnsupported, display)
	}
	c.wakeDpy = lib.openDisplay(display)
	if c.wakeDpy == 0 {
		return nil, fmt.Errorf("%w: the second XOpenDisplay(%q), used only to wake the event loop, failed", ErrUnsupported, display)
	}

	root := lib.defaultRootWindow(c.dpy)
	c.win = lib.createWindow(c.dpy, root, 0, 0, 1, 1, 0, 0, classInputOnly, 0, 0, 0)
	if c.win == 0 {
		return nil, fmt.Errorf("gift/clipboard: XCreateWindow for the invisible selection owner failed")
	}
	// PropertyChangeMask, so that a later INCR implementation would see its
	// PropertyNotify without a protocol change here. Today the loop ignores
	// them; asking for them costs nothing and the events only arrive for
	// properties on our own unmapped window.
	const propertyChangeMask = 1 << 22
	lib.selectInput(c.dpy, c.win, propertyChangeMask)

	c.a = atoms{
		clipboard:     lib.internAtom(c.dpy, "CLIPBOARD", false),
		targets:       lib.internAtom(c.dpy, "TARGETS", false),
		timestamp:     lib.internAtom(c.dpy, "TIMESTAMP", false),
		multiple:      lib.internAtom(c.dpy, "MULTIPLE", false),
		incr:          lib.internAtom(c.dpy, "INCR", false),
		utf8String:    lib.internAtom(c.dpy, "UTF8_STRING", false),
		textPlainUTF8: lib.internAtom(c.dpy, "text/plain;charset=utf-8", false),
		textPlain:     lib.internAtom(c.dpy, "text/plain", false),
		xaString:      31, // XA_STRING is predefined by the protocol.
		giftSelection: lib.internAtom(c.dpy, "GIFT_CLIPBOARD", false),
	}
	c.wakeAtom = lib.internAtom(c.dpy, "GIFT_CLIPBOARD_WAKE", false)
	lib.flush(c.dpy)

	go c.loop()
	return c, nil
}

// loadX11 opens libX11 and binds every function in [x11lib].
//
// Two sonames are tried. libX11.so.6 is the one that is installed on a system
// that merely *runs* X programs; libX11.so is the development symlink and is
// absent on a stock Raspberry Pi OS image, so it is the fallback and not the
// first guess.
func loadX11() (lib x11lib, err error) {
	var handle uintptr
	for _, name := range []string{"libX11.so.6", "libX11.so"} {
		handle, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			break
		}
	}
	if handle == 0 {
		return lib, fmt.Errorf("%w: libX11.so.6 could not be loaded: %w", ErrUnsupported, err)
	}

	// RegisterLibFunc panics on a missing symbol, and a missing symbol in
	// libX11 would mean a library that is not libX11. Turning that into an
	// error keeps the promise of this package that an unavailable clipboard
	// degrades rather than takes the application down. The results are named
	// for this deferred function's sake: recovering in a deferred function
	// that cannot assign to them would return a zero x11lib and a nil error,
	// which is the worst of both.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: binding libX11 failed: %v", ErrUnsupported, r)
		}
	}()
	purego.RegisterLibFunc(&lib.openDisplay, handle, "XOpenDisplay")
	purego.RegisterLibFunc(&lib.defaultRootWindow, handle, "XDefaultRootWindow")
	purego.RegisterLibFunc(&lib.internAtom, handle, "XInternAtom")
	purego.RegisterLibFunc(&lib.createWindow, handle, "XCreateWindow")
	purego.RegisterLibFunc(&lib.selectInput, handle, "XSelectInput")
	purego.RegisterLibFunc(&lib.setSelectionOwner, handle, "XSetSelectionOwner")
	purego.RegisterLibFunc(&lib.getSelectionOwner, handle, "XGetSelectionOwner")
	purego.RegisterLibFunc(&lib.convertSelection, handle, "XConvertSelection")
	purego.RegisterLibFunc(&lib.nextEvent, handle, "XNextEvent")
	purego.RegisterLibFunc(&lib.sendEvent, handle, "XSendEvent")
	purego.RegisterLibFunc(&lib.changeProperty, handle, "XChangeProperty")
	purego.RegisterLibFunc(&lib.getWindowProperty, handle, "XGetWindowProperty")
	purego.RegisterLibFunc(&lib.free, handle, "XFree")
	purego.RegisterLibFunc(&lib.flush, handle, "XFlush")
	purego.RegisterLibFunc(&lib.setErrorHandler, handle, "XSetErrorHandler")
	return lib, err
}

// errorHandler state. Xlib's error handler is a single process wide function
// pointer, which is the uncomfortable part of this file: GLFW installs one
// during Ebitengine's startup, and replacing it would silence the errors of a
// connection this package has nothing to do with.
//
// So it is chained. Ours logs errors that arrive on our connections and hands
// everything else to the handler that was installed before us. The default
// Xlib handler prints and calls exit(1); an X error caused by a requestor
// window that died between its request and our answer is routine, and a
// clipboard that can kill the application is not acceptable.
var (
	errorHandlerOnce sync.Once
	previousHandler  uintptr
	errorLog         *slog.Logger
	errorDisplays    sync.Map // uintptr -> struct{}, the connections that are ours
)

func installErrorHandler(lib x11lib, log *slog.Logger) {
	errorHandlerOnce.Do(func() {
		errorLog = log
		cb := purego.NewCallback(func(dpy uintptr, ev unsafe.Pointer) uintptr {
			if _, ours := errorDisplays.Load(dpy); !ours && previousHandler != 0 {
				return xcallPrevious(previousHandler, dpy, ev)
			}
			if errorLog != nil {
				// The XErrorEvent layout: type, display, resourceid,
				// serial, error_code, request_code, minor_code. The three
				// codes are single bytes at the end of the struct; reading
				// them is a fixed offset and is done here rather than in a
				// helper because this is the only error path.
				b := unsafe.Slice((*byte)(ev), 40)
				errorLog.Error("an X11 error arrived on the clipboard connection, ignoring it",
					"error_code", b[32], "request_code", b[33], "minor_code", b[34])
			}
			return 0
		})
		previousHandler = lib.setErrorHandler(cb)
	})
}

// xcallPrevious calls the error handler that was installed before this
// package's, so that GLFW keeps seeing the errors of its own connection.
var xcallPrevious = func(fn uintptr, dpy uintptr, ev unsafe.Pointer) uintptr {
	r, _, _ := purego.SyscallN(fn, dpy, uintptr(ev))
	return r
}

func (c *x11Clipboard) text() (string, bool, error) {
	res, err := c.submit(&x11Command{})
	if err != nil {
		return "", false, err
	}
	return res.text, res.ok, res.err
}

func (c *x11Clipboard) setText(s string) error {
	res, err := c.submit(&x11Command{write: true, text: s})
	if err != nil {
		return err
	}
	return res.err
}

// submit hands a command to the event thread, wakes it, and waits for the
// answer or for [ReadTimeout].
//
// A timeout is not an error the caller can do anything about, but it is one
// the log has to show, because the only two ways to reach it are an owner that
// does not answer and an event loop that has stopped.
func (c *x11Clipboard) submit(cmd *x11Command) (x11Result, error) {
	cmd.done = make(chan x11Result, 1)

	c.mu.Lock()
	select {
	case c.cmds <- cmd:
	default:
		c.mu.Unlock()
		return x11Result{}, fmt.Errorf("gift/clipboard: the X11 event loop is not keeping up, %d requests are already queued", cap(c.cmds))
	}
	c.wake()
	c.mu.Unlock()

	timer := time.NewTimer(ReadTimeout)
	defer timer.Stop()
	select {
	case res := <-cmd.done:
		return res, nil
	case <-timer.C:
		return x11Result{}, fmt.Errorf("gift/clipboard: no answer within %v", ReadTimeout)
	}
}

// wake sends the ClientMessage that unblocks XNextEvent. It runs on the
// caller's goroutine and therefore uses connection B; see the file comment.
// c.mu is held.
func (c *x11Clipboard) wake() {
	var ev xEvent
	ev[0] = evClientMessage
	ev[3] = uint64(c.wakeDpy)
	ev[4] = c.win
	ev[5] = c.wakeAtom
	ev[6] = 32 // format
	c.lib.sendEvent(c.wakeDpy, c.win, false, 0, unsafe.Pointer(&ev[0]))
	c.lib.flush(c.wakeDpy)
}

// xEvent is an XEvent, which is a union of every event structure and is
// documented to be 24 words long on a 64 bit machine. It is read and written
// word by word, because purego has no struct for it and building one would
// mean describing every event type this file ignores.
//
// The word offsets used below come from the C declarations, all of which begin
// with int type, unsigned long serial, Bool send_event, Display *display —
// four words after padding — and then differ:
//
//	SelectionRequest: owner 4, requestor 5, selection 6, target 7, property 8, time 9
//	SelectionNotify:  requestor 4, selection 5, target 6, property 7, time 8
//	SelectionClear:   window 4, selection 5, time 6
//	ClientMessage:    window 4, message_type 5, format 6, data 7..11
type xEvent [24]uint64

func (e *xEvent) kind() uint32 { return uint32(e[0]) }

// loop is the event thread. It never returns: there is no shutdown hook on
// [ui.Clipboard], and under X11 the content must stay available for as long as
// the process lives anyway. See the documentation of [ui.Clipboard] on why
// there is no Close.
func (c *x11Clipboard) loop() {
	runtime.LockOSThread()
	errorDisplays.Store(c.dpy, struct{}{})
	errorDisplays.Store(c.wakeDpy, struct{}{})

	var (
		ev      xEvent
		owned   string   // the text this process is currently offering
		haveOwn bool     // whether we own CLIPBOARD as far as we know
		ownTime uint64   // the timestamp ownership was taken at
		pending *x11Read // the paste in flight, if any
	)

	for {
		c.lib.nextEvent(c.dpy, unsafe.Pointer(&ev[0]))
		switch ev.kind() {
		case evClientMessage:
			// A wakeup. Everything interesting is in the channel.
			for {
				select {
				case cmd := <-c.cmds:
					if cmd.write {
						owned, haveOwn, ownTime = cmd.text, true, currentTime
						cmd.done <- x11Result{err: c.takeOwnership()}
						continue
					}
					if pending != nil {
						// A second paste while the first is still in
						// flight. The first one's caller is still waiting
						// or has already timed out; either way there is
						// one property to read into, so the newcomer is
						// answered with nothing rather than being mixed up
						// with it.
						cmd.done <- x11Result{err: fmt.Errorf("gift/clipboard: a previous paste is still waiting for the selection owner")}
						continue
					}
					if haveOwn && c.lib.getSelectionOwner(c.dpy, c.a.clipboard) == c.win {
						// We are the owner. Asking ourselves would mean
						// waiting on this very thread for an answer only
						// this very thread can send, which is a deadlock
						// dressed up as a timeout.
						cmd.done <- x11Result{text: owned, ok: true}
						continue
					}
					pending = c.beginRead(cmd)
					if pending.finished {
						pending = nil
					}
				default:
				}
				break
			}

		case evSelectionReq:
			c.serve(&ev, owned, haveOwn, ownTime)

		case evSelectionNotify:
			if pending == nil {
				continue // a late answer to a paste that has been given up on
			}
			if c.finishRead(pending, &ev) {
				pending = nil
			}

		case evSelectionClear:
			// Somebody else copied. Our text is no longer on the
			// clipboard and offering it again would be a lie.
			owned, haveOwn = "", false

		case evPropertyNotify:
			// Only ever about our own window. INCR would use these; see
			// section 19, which drops INCR.
		}
	}
}

// takeOwnership claims CLIPBOARD for our window and checks that the server
// agrees.
//
// The timestamp handed to XSetSelectionOwner is CurrentTime, and ICCCM asks
// for the timestamp of the event that caused the copy instead. The difference
// matters only when two clients race for ownership in the same millisecond,
// where CurrentTime cannot lose an ordering it does not take part in — the
// server simply grants it. Doing it properly means a round trip of its own:
// change a property on our own window, wait for the PropertyNotify, use its
// time. That is a second wait inside the write path, for a race between a
// human pressing Ctrl+C and another program copying at the same instant. It is
// written down here rather than done, and the TIMESTAMP target consequently
// answers with the same CurrentTime, which is what a requestor that asks will
// see. The check is the point: XSetSelectionOwner has no return value worth
// reading, and a copy that silently did not happen is the failure mode this
// whole file exists to avoid.
func (c *x11Clipboard) takeOwnership() error {
	c.lib.setSelectionOwner(c.dpy, c.a.clipboard, c.win, currentTime)
	c.lib.flush(c.dpy)
	if got := c.lib.getSelectionOwner(c.dpy, c.a.clipboard); got != c.win {
		return fmt.Errorf("gift/clipboard: the X server gave ownership of CLIPBOARD to window %#x and not to ours (%#x)", got, c.win)
	}
	return nil
}

// x11Read is a paste in flight: the command that asked for it and how far
// through [atoms.readTargets] it has got.
type x11Read struct {
	cmd      *x11Command
	target   int
	finished bool
}

// beginRead asks the current owner for the first target, and is also the
// function that gives up when there is no owner at all.
func (c *x11Clipboard) beginRead(cmd *x11Command) *x11Read {
	if c.lib.getSelectionOwner(c.dpy, c.a.clipboard) == 0 {
		// Nobody owns the clipboard. That is the state of a fresh session
		// and it is an empty clipboard, not a failure.
		cmd.done <- x11Result{}
		return &x11Read{finished: true}
	}
	r := &x11Read{cmd: cmd}
	c.ask(r)
	return r
}

// ask sends the conversion request for the target r is currently on.
func (c *x11Clipboard) ask(r *x11Read) {
	targets := c.a.readTargets()
	c.lib.convertSelection(c.dpy, c.a.clipboard, targets[r.target], c.a.giftSelection, c.win, currentTime)
	c.lib.flush(c.dpy)
}

// finishRead handles a SelectionNotify and reports whether the paste is over.
//
// A refusal — property None — moves on to the next target, which is the whole
// of the negotiation on the reading side and the reason an owner that only
// speaks XA_STRING still produces a paste.
func (c *x11Clipboard) finishRead(r *x11Read, ev *xEvent) bool {
	property := ev[7]
	if property == 0 {
		r.target++
		if r.target < len(c.a.readTargets()) {
			c.ask(r)
			return false
		}
		r.cmd.done <- x11Result{}
		return true
	}

	typ, format, data, err := c.readProperty(property)
	if err != nil {
		r.cmd.done <- x11Result{err: err}
		return true
	}
	s, ok, err := c.a.decode(typ, format, data)
	r.cmd.done <- x11Result{text: s, ok: ok, err: err}
	return true
}

// readProperty reads the property the owner wrote onto our window and deletes
// it, so that the next paste starts from an empty one.
//
// The length asked for is [MaxBytes] in the 32 bit units XGetWindowProperty
// counts in, plus one, so that "there is more" is distinguishable from "that
// was all" — bytes_after is non-zero exactly when the text was longer than the
// cap, and then the text has already been cut by the server rather than by
// [capText]. Either way the caller gets the first MaxBytes; the warning about
// it comes from the portable half, which compares lengths.
func (c *x11Clipboard) readProperty(property xAtom) (xAtom, int, []byte, error) {
	var (
		actualType   uint64
		actualFormat int32
		nitems       uint64
		bytesAfter   uint64
		prop         unsafe.Pointer
	)
	const anyPropertyType = 0
	status := c.lib.getWindowProperty(c.dpy, c.win, property,
		0, int64(MaxBytes/4)+1, true, anyPropertyType,
		unsafe.Pointer(&actualType), unsafe.Pointer(&actualFormat),
		unsafe.Pointer(&nitems), unsafe.Pointer(&bytesAfter), &prop)
	if status != 0 { // Success is 0
		return 0, 0, nil, fmt.Errorf("gift/clipboard: XGetWindowProperty failed with status %d", status)
	}
	if prop == nil {
		return actualType, int(actualFormat), nil, nil
	}
	defer c.lib.free(prop)

	// The width of an element depends on the format, and at format 32 it is
	// a C long, which is eight bytes here and not four. Getting this wrong
	// reads half the text or twice the buffer.
	width := int(actualFormat) / 8
	if actualFormat == 32 {
		width = 8
	}
	n := int(nitems) * width
	data := make([]byte, n)
	copy(data, unsafe.Slice((*byte)(prop), n))
	return actualType, int(actualFormat), data, nil
}

// serve answers a SelectionRequest. This is the half of X11 that makes a copy
// work, and it runs for every paste in every other application for as long as
// this process owns the selection.
func (c *x11Clipboard) serve(ev *xEvent, owned string, haveOwn bool, ownTime uint64) {
	requestor, selection, target, property := ev[5], ev[6], ev[7], ev[8]
	if property == 0 {
		// An obsolete requestor. ICCCM says to use the target as the
		// property in that case.
		property = target
	}

	refuse := func() {
		c.notify(requestor, selection, target, 0, ev[9])
	}
	if !haveOwn || selection != c.a.clipboard {
		refuse()
		return
	}
	reply, ok := c.a.answer(target, owned)
	if !ok {
		refuse()
		return
	}
	if target == c.a.timestamp {
		reply.data = encodeLongs([]xAtom{ownTime})
	}

	data := reply.data
	if len(data) == 0 {
		// XChangeProperty with zero elements still wants a valid pointer.
		data = make([]byte, 1)
	}
	c.lib.changeProperty(c.dpy, requestor, property, reply.typ, int32(reply.format), propModeReplace,
		unsafe.Pointer(&data[0]), int32(reply.items()))
	c.notify(requestor, selection, target, property, ev[9])
}

// notify sends the SelectionNotify that ends a request. A property of 0 is the
// refusal; there is no other way to say no in this protocol.
func (c *x11Clipboard) notify(requestor, selection, target, property, t uint64) {
	var out xEvent
	out[0] = evSelectionNotify
	out[3] = uint64(c.dpy)
	out[4] = requestor
	out[5] = selection
	out[6] = target
	out[7] = property
	out[8] = t
	c.lib.sendEvent(c.dpy, requestor, false, 0, unsafe.Pointer(&out[0]))
	c.lib.flush(c.dpy)
}
