package ui

import (
	"sync"
	"sync/atomic"
)

// Clipboard is the system clipboard as the text views of this package use it.
//
// It is an interface with two methods because that is the whole of what copy,
// cut and paste need, and because the implementation that can actually talk to
// a desktop cannot live here: Ebitengine 2.10.1 has no public clipboard API and
// the GLFW one is under internal/, permanently out of reach of the import
// rules. The project plan, section 19, therefore puts the platform side in an
// optional package that binds libX11 or NSPasteboard through purego, as a side
// effect import, so that the cgo free core of section 1 stays cgo free:
//
//	import _ "github.com/torbenschinke/gift/clipboard"
//
// That package exists since WU-AE. This one stays what it was, so that the
// text field is complete, testable and useful with the in-process default
// below on a machine with no display, and so that the platform unit is a
// single call to [SetClipboard] rather than a change to a widget. The cap the
// next section asks for is clipboard.MaxBytes, 256 KiB, and the truncation
// past it is silent to a caller of [Clipboard.SetText] and visible only as a
// log line — exactly as this documentation predicted it would have to be.
//
// # The contract
//
// Both methods are called from the UI goroutine, from inside an event handler,
// and never from the paint path. An implementation may block for the
// milliseconds an X11 round trip costs; it must not block for a second, and it
// must not call back into gift.
//
// Neither method reports an error. That is deliberate and it is the same
// argument the project plan, section 15, makes for `Result`: the only thing a
// text field could do with a failed paste is nothing, and the only thing it
// could do with a failed copy is nothing. A refused read is simply "no text",
// which is a state the user can see.
//
// # What an implementation owes, and where it writes it
//
// Three things, and the first two are the ones section 19 leaves open.
//
// A *size cap*. Section 19 drops the INCR protocol of X11 for now and requires
// that the transferable size be capped and documented instead. The cap belongs
// to the implementation, because it is a property of the transport and not of
// a text field, and it has to be in that implementation's documentation
// because this interface cannot express it: [Clipboard.SetText] returns
// nothing, so a value that is too large is *silently* truncated or *silently*
// dropped, and which of the two it is is exactly what a caller has to be able
// to read somewhere. A conservative reading of this interface is therefore
// that a copy of a megabyte may not arrive.
//
// A *shutdown*. There is no Close here and no hook that gift calls, so an
// implementation that owns an operating system resource owns it until the
// process ends. Under X11 that is a display connection, an invisible window
// and the OS thread that serves SelectionRequest for as long as the content is
// meant to stay valid — see section 19, which records the content vanishing
// with the process as standard behaviour rather than as a defect. An
// implementation that needs an orderly end exposes its own function for it and
// says so; a Close on this interface would be a method every widget would have
// to be told never to call.
//
// A *log line*, for the failures neither method can return. Section 15 is the
// policy and it applies unchanged: slog, never to slog.Default, nothing on the
// frame path — and none of these calls is on the frame path, they all run in
// an event handler. gift hands no logger to a clipboard, because there is no
// construction call to hand it at: [SetClipboard] takes an already built
// value, so the implementation is configured by its own constructor, by the
// application, with the same *slog.Logger the application gave [gift.Options].
type Clipboard interface {
	// Text returns the current clipboard contents and whether there are any.
	//
	// False means "nothing to paste": an empty clipboard, a clipboard holding
	// something that is not text, or a platform that declined to answer. A
	// caller pastes nothing in all three cases.
	Text() (string, bool)

	// SetText replaces the clipboard contents with s.
	//
	// It cannot refuse and cannot report a truncation; an implementation that
	// caps the size documents the cap and what it does past it. See the type
	// documentation above.
	//
	// Under X11 the process that copied stays the owner of the selection, so
	// the content disappears when the process exits; the project plan,
	// section 19, records that as standard behaviour and not as a defect.
	SetText(s string)
}

// clipboard is the process wide clipboard, as an atomic pointer to an
// interface value so that [SetClipboard] from an init function races with
// nothing.
//
// Process wide, like [SetTheme] and [SetDefaultFont] and for the same reason:
// there is one machine with one clipboard, and section 1 of the project plan
// has one window. A per App clipboard would be a second answer to a question
// the operating system has already answered.
var clipboard atomic.Pointer[Clipboard]

// defaultClipboard is the in-process clipboard every application gets until
// something installs a platform one.
//
// It is not a stub that does nothing: copy, cut and paste work completely
// inside one process, which is what makes the text field fully testable
// without a display server and what makes a kiosk — which has no other
// application to exchange text with — behave correctly with no extra import.
// The one thing it cannot do is exchange text with another program, and that
// is exactly the gap the platform package fills.
var defaultClipboard = &MemoryClipboard{}

// SetClipboard installs the clipboard implementation for the whole process. A
// nil argument restores the in-process default.
//
// Call it before the first frame. It is safe to call from any goroutine, but
// calling it while the user is in the middle of a paste is a race in the
// obvious sense and not in the detectable one: the paste that is running reads
// whichever clipboard it loaded.
func SetClipboard(c Clipboard) {
	if c == nil {
		clipboard.Store(nil)
		return
	}
	clipboard.Store(&c)
}

// CurrentClipboard returns the installed clipboard, or the in-process default.
// It never returns nil, so a caller never has to check.
func CurrentClipboard() Clipboard {
	if p := clipboard.Load(); p != nil {
		return *p
	}
	return defaultClipboard
}

// MemoryClipboard is a clipboard that lives in this process and exchanges text
// with nothing outside it. It is the default; see [SetClipboard].
//
// The zero value is an empty clipboard and is ready to use. It is safe for
// concurrent use because a test may fill it from a helper goroutine, not
// because gift ever touches it off the UI goroutine.
type MemoryClipboard struct {
	mu   sync.Mutex
	s    string
	have bool
}

// Text implements [Clipboard].
func (c *MemoryClipboard) Text() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s, c.have
}

// SetText implements [Clipboard].
//
// Copying the empty string is a copy and not a clear: [MemoryClipboard.Text]
// afterwards reports "" and true. Selecting nothing does not reach here — the
// text field declines to copy an empty selection — so the distinction only
// shows up for a caller that writes "" on purpose, and for that caller "I put
// nothing there" is the honest answer.
func (c *MemoryClipboard) SetText(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s, c.have = s, true
}

// Clear empties the clipboard, so that [MemoryClipboard.Text] reports false
// again. It exists for tests; no keyboard produces it.
func (c *MemoryClipboard) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s, c.have = "", false
}
