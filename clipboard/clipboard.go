// Package clipboard connects [ui.Clipboard] to the clipboard of the operating
// system, without cgo.
//
// Import it for its side effect and the text views exchange text with other
// programs; do not import it and they exchange text only inside this process,
// through [ui.MemoryClipboard], which stays the default:
//
//	import _ "github.com/torbenschinke/gift/clipboard"
//
// This is the shape of font/inter and it is chosen for the same reason: a
// binary that does not import this package pays nothing for it — no purego, no
// dlopen, no thread, no libX11 on the machine. What importing it costs, in
// bytes and on which platform, is measured in "What it costs" below.
//
// The side effect is the whole of the public surface that an application
// needs. [New] exists for the one thing the import cannot do, which is hand
// this package the application's *slog.Logger; see "Logging".
//
// # Why this is a separate package at all
//
// Ebitengine 2.10.1 has no public clipboard API. It has a working GLFW one,
// under internal/, which the import rules of Go put permanently out of reach.
// An external mechanism is therefore unavoidable, and the project plan,
// section 19, picks github.com/ebitengine/purego for it: purego was already an
// indirect dependency through Ebitengine, so this package adds no module to
// the graph, and purego reaches libX11 and the Objective-C runtime with
// CGO_ENABLED=0, which is what keeps the Raspberry Pi cross build of section 1
// intact.
//
// # Platforms
//
//   - darwin: NSPasteboard through the Objective-C runtime. No event loop, no
//     thread, no state; every call is a round trip to the pasteboard server.
//     See clipboard_darwin.go.
//   - linux, X11: an own display connection, an own invisible window and an
//     own OS thread that serves SelectionRequest. This is not elaborateness,
//     it is how X11 works — whoever copies becomes the owner of the CLIPBOARD
//     selection and has to hand the text out on request for as long as it is
//     meant to be available. See clipboard_x11.go, which documents the design,
//     the timeout and what happens at process exit.
//   - Everything else, including Windows and Wayland without XWayland: not
//     supported, and it says so rather than pretending. A read reports "no
//     text" and a write is dropped, each with one log line at Error level the
//     first time. Windows is best effort per section 19 and section 1 does not
//     list it as a target; see "Windows" below.
//
// # What it costs
//
// First, binary size, because the package documentation above promises a
// number and until WU-AH there was none. Measured with two programs that
// differ only in this package's blank import, both of which build a [ui.Text],
// compared with `go build` and `stat`, Go 1.27:
//
//   - CGO_ENABLED=0 GOOS=linux GOARCH=arm64, the Raspberry Pi target of
//     section 1: **+1 310 927 bytes**, 1.25 MiB.
//   - darwin/arm64: **+627 424 bytes**, 613 KiB.
//
// Most of that is not the clipboard. It is log/slog and what slog pulls in —
// reflect and encoding/json — for a binary that did not already use it, plus
// purego and the runtime callback machinery [purego.NewCallback] requires. An
// application that already hands [gift.Options] an *slog.Logger, which is what
// section 15 of the project plan expects of one, has paid most of it already:
// against a baseline that logs, the same measurement gives **+195 527 bytes**
// on linux/arm64 and **+503 776 bytes** on darwin/arm64, and the remaining
// darwin figure is the Objective-C runtime binding. Both pairs are quoted
// because an application is one or the other and the difference between them
// is larger than the package.
//
// Second, allocations.
//
// A call is a user action — Ctrl+C, Ctrl+V — and never happens on the frame
// path, so the zero allocation contract of section 11 is untouched by
// construction rather than by care. The calls do allocate, and roughly: on
// darwin one NSString for a write and one Go string for a read, plus the
// autorelease pool; on X11 one command struct, one reply channel and one byte
// slice of the text's length per call, plus the property buffer libX11
// allocates and this package frees. Nothing here is pooled, because a human
// produces at most a few of these per second.
//
// A read costs a round trip to another process. On X11 that is the owner's
// response time, capped by [ReadTimeout]; on darwin it is a Mach message to
// the pasteboard server. Both are milliseconds in practice and both block the
// UI goroutine while they last, which is exactly what [ui.Clipboard] permits
// and bounds.
//
// # The size cap
//
// [MaxBytes] is the largest text this package transfers, in bytes of UTF-8.
// Anything longer is truncated at a rune boundary, on write and on read.
//
// The cap exists because section 19 drops the INCR protocol of X11 for now and
// requires the transferable size to be capped and documented instead, and
// [ui.Clipboard] cannot report a truncation: SetText returns nothing. So the
// truncation is silent to the caller and visible only as a log line. That is a
// real limitation and it is the reason the documentation of [ui.Clipboard]
// tells its readers that a copy of a megabyte may not arrive.
//
// # Logging
//
// Section 15: log/slog, never to slog.Default, and nothing on the frame path.
// A side effect import has nowhere to pass a logger, so the clipboard
// installed from init logs nothing at all — silence is the documented default
// of gift when no logger is given. An application that wants to see the
// failures builds one itself:
//
//	ui.SetClipboard(clipboard.New(logger))
//
// and that is the same *slog.Logger it gives [gift.Options].
//
// # Windows
//
// Deliberately not implemented. It would be an OpenClipboard,
// GetClipboardData and SetClipboardData against user32 through
// syscall.NewLazyDLL, which is perhaps forty lines, and every one of them
// would be unverified: there is no Windows machine in this project, section 1
// does not name Windows as a target, and the acceptance matrix never builds
// for it. Untested clipboard code that compiles is worse than an honest
// refusal, because it looks supported. GOOS=windows therefore gets the same
// [ErrUnsupported] path as any other platform this package cannot serve.
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"unicode/utf8"

	"github.com/torbenschinke/gift/ui"
)

// MaxBytes is the largest amount of text this package moves in either
// direction, in bytes of UTF-8, and it is the cap that section 19 of the
// project plan asks for in place of the INCR protocol.
//
// 256 KiB. The number comes from the X11 side, which is the constrained one:
// without INCR a selection has to fit into a single XChangeProperty, and the
// maximum request a server accepts is XMaxRequestSize, which is 262 140
// *four-byte units* — a megabyte — on every server this was checked against,
// and at least 16 384 units, 64 KiB, by protocol. 256 KiB sits above what
// anybody pastes into a text field and far below the smallest limit a
// conforming server may impose on a connection that has BIG-REQUESTS, which
// every modern server does. The same cap is applied on darwin, where no
// transport requires it, so that an application does not see one limit on one
// machine and another limit on another.
//
// For scale: 256 KiB of prose is around forty thousand words. The single line
// text field that is the only caller today drops everything from the first
// line break anyway.
const MaxBytes = 256 << 10

// ErrUnsupported is the error the platform layer returns when there is no
// clipboard to talk to on this operating system, or when the pieces it needs —
// libX11, a DISPLAY, the AppKit framework — are not there.
//
// It is wrapped, not returned bare, so that a log line says which piece was
// missing. [errors.Is] finds it.
var ErrUnsupported = errors.New("gift/clipboard: no system clipboard on this platform")

// New returns a clipboard that talks to the operating system, logging its
// failures to log. A nil logger logs nothing, which is what section 15 of the
// project plan prescribes for a missing logger and what the side effect import
// installs.
//
// The returned value is ready immediately and connects to the platform lazily,
// on the first call. That is deliberate: opening a display connection and
// starting a thread from an init function would make every binary that imports
// this package pay for a clipboard it may never use, and would make a missing
// DISPLAY a startup failure rather than a copy that does not work. A platform
// that fails to open is remembered as failed, logged once and never retried,
// so a keyboard held down on Ctrl+V cannot turn one missing library into a
// dlopen per keystroke.
//
// The result implements [ui.Clipboard] and, additionally,
// [ui.CheckedClipboard], so that a cut can find out that its copy failed
// before it deletes anything. That is an assertion the caller makes and not a
// wider return type, because the interface a clipboard is installed under is
// still [ui.Clipboard]. It has no Close: the documentation of [ui.Clipboard]
// explains why there is no shutdown hook, and the X11 implementation explains
// what that means for the content at process exit.
func New(log *slog.Logger) ui.Clipboard {
	return newClipboard(openPlatform, log)
}

// init installs this package as the process wide clipboard.
//
// This is the side effect the package exists for, and it is a heavier one than
// font/inter's: that package registers a font and leaves the choice of the
// default to the application, because there can be many fonts. There is one
// clipboard, the application asked for it by importing this package, and there
// is nothing to choose. An application that wants a logger calls
// [ui.SetClipboard] with [New] afterwards and overwrites this; an application
// that wants the in-process clipboard back passes nil.
func init() {
	ui.SetClipboard(New(nil))
}

// platform is the seam between the part of this package that can be tested
// anywhere and the part that needs an operating system.
//
// Everything above this interface — the size cap, the UTF-8 handling, the
// once-only failure, the logging — is ordinary Go and is tested on any machine
// by [go test ./...]. Everything below it is libX11 or Objective-C and is not.
// The precedent is backend/ebiten/input.go, whose keyPressed and appendChars
// function values let the real input bridge be driven headlessly; this is the
// same trick with an interface, because there are two methods and a
// constructor rather than one reading.
//
// The methods return errors even though [ui.Clipboard] cannot, because the
// layer above is where an error becomes a log line. text returning ("", false,
// nil) is an empty clipboard; returning an error is a broken one.
type platform interface {
	text() (string, bool, error)
	setText(s string) error
}

// clipboard is the platform independent half: it caps, it validates, it logs,
// and it owns the lazy opening of the platform below it.
type clipboard struct {
	log *slog.Logger

	// open is the constructor of the platform layer, as a field so that a
	// test can substitute one. In a built binary it is always openPlatform.
	open func(*slog.Logger) (platform, error)

	// once guards the single attempt at open. p is nil and stays nil when
	// that attempt failed; there is no retry, see [New].
	once sync.Once
	p    platform
}

func newClipboard(open func(*slog.Logger) (platform, error), log *slog.Logger) *clipboard {
	return &clipboard{log: log, open: open}
}

// impl returns the platform layer, or nil if it could not be opened. The
// failure was already logged, once, so a caller does not log it again.
func (c *clipboard) impl() platform {
	c.once.Do(func() {
		p, err := c.open(c.log)
		if err != nil {
			c.logf(slog.LevelError, "clipboard unavailable, copy and paste will not leave this process", "error", err)
			return
		}
		c.p = p
		c.logf(slog.LevelInfo, "system clipboard ready", "max_bytes", MaxBytes)
	})
	return c.p
}

// Text implements [ui.Clipboard].
//
// It reports false for an empty clipboard, for a clipboard holding something
// that is not text, for a platform that is not there and for an answer this
// package refuses — which is the case worth naming: text that is not valid
// UTF-8. X11 hands out bytes and a UTF8_STRING that is not UTF-8 is a lying
// owner, so the bytes are dropped rather than repaired. Repairing them would
// put replacement characters into a text field that the user did not copy and
// cannot explain.
//
// An over-long answer is truncated rather than dropped, because the first
// [MaxBytes] of the text the user copied is what they asked for and nothing of
// it is invented.
func (c *clipboard) Text() (string, bool) {
	p := c.impl()
	if p == nil {
		return "", false
	}
	s, ok, err := p.text()
	if err != nil {
		c.logf(slog.LevelError, "reading the clipboard failed", "error", err)
		return "", false
	}
	if !ok {
		return "", false
	}
	if !utf8.ValidString(s) {
		c.logf(slog.LevelError, "the clipboard holds text that is not valid UTF-8, ignoring it", "bytes", len(s))
		return "", false
	}
	if capped, cut := capText(s); cut {
		c.logf(slog.LevelWarn, "the clipboard holds more than this package transfers, truncating the paste",
			"bytes", len(s), "kept", len(capped), "max_bytes", MaxBytes)
		return capped, true
	}
	return s, true
}

// SetText implements [ui.Clipboard]. It is [clipboard.SetTextErr] with the
// error already logged and then dropped, and not a second code path.
func (c *clipboard) SetText(s string) {
	_ = c.SetTextErr(s)
}

// SetTextErr implements [ui.CheckedClipboard].
//
// Over-long text is truncated at a rune boundary and the truncation is
// reported as a log line and in no other way, not as an error: the cap is
// documented, the first [MaxBytes] did arrive, and a cut whose text was merely
// shortened must still delete. See the package documentation, "The size cap".
//
// Text that is not valid UTF-8 cannot arrive here from a gift text field,
// which builds its strings from runes, but can arrive from an application that
// calls [ui.CurrentClipboard] itself. It is refused rather than handed to a
// platform whose transfer types all promise UTF-8.
//
// The errors are returned as well as logged because the caller that asks for
// them can act: [ui.CheckedClipboard] exists so that a cut does not delete the
// text it failed to copy.
func (c *clipboard) SetTextErr(s string) error {
	p := c.impl()
	if p == nil {
		return fmt.Errorf("%w: there is no platform clipboard, the failure was logged when it was first opened", ErrUnsupported)
	}
	if !utf8.ValidString(s) {
		c.logf(slog.LevelError, "refusing to copy text that is not valid UTF-8", "bytes", len(s))
		return fmt.Errorf("gift/clipboard: refusing to copy %d bytes that are not valid UTF-8", len(s))
	}
	if capped, cut := capText(s); cut {
		c.logf(slog.LevelWarn, "copying more than this package transfers, truncating",
			"bytes", len(s), "kept", len(capped), "max_bytes", MaxBytes)
		s = capped
	}
	if err := p.setText(s); err != nil {
		c.logf(slog.LevelError, "writing the clipboard failed", "error", err)
		return err
	}
	return nil
}

// logf is the one place this package logs. A nil logger is silence, per
// section 15; none of these calls is on the frame path, so there is no level
// check for the sake of allocations, only the nil check.
func (c *clipboard) logf(level slog.Level, msg string, args ...any) {
	if c.log == nil {
		return
	}
	c.log.Log(context.Background(), level, msg, args...)
}

// capText cuts s to at most [MaxBytes] and reports whether it had to.
//
// The cut is at a rune boundary: a truncated multi-byte sequence would be
// invalid UTF-8, and this package refuses invalid UTF-8 on both sides, so
// cutting in the middle of a rune would turn "too long" into "rejected". Up to
// three bytes are therefore given back on top of the cap.
//
// It allocates nothing — the result is a slice of s — and it does not copy.
func capText(s string) (string, bool) {
	if len(s) <= MaxBytes {
		return s, false
	}
	n := MaxBytes
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}
