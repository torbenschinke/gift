//go:build darwin

package clipboard

import (
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// This file is the macOS half, and it is short because macOS has an actual
// clipboard: NSPasteboard is a service in another process that holds the
// bytes. There is no ownership to defend, no event loop, no thread, and
// nothing that has to stay alive — text copied from a gift application is
// still there after the application has quit, which is the opposite of the X11
// behaviour section 19 documents.
//
// The calls are the four an AppKit program would make:
//
//	[NSPasteboard generalPasteboard]
//	[pb clearContents]
//	[pb setString:s forType:NSPasteboardTypeString]
//	[pb stringForType:NSPasteboardTypeString]
//
// clearContents is not optional: setString: writes into the current pasteboard
// "declaration", and without a clearContents the old contents of other types
// survive next to the new string. A paste in another application then gets
// whichever type it asks for first, which can be the one that is stale.
//
// changeCount is deliberately not used. It is the way to notice that somebody
// else has copied since we last looked, and it is what a clipboard that cached
// its contents or fired a change notification would need. This one reads on
// demand, on the user's keystroke, so the pasteboard's own answer is always
// current and a change counter would only be a second source of truth.

// macOS specific constants and selectors, resolved once. The Objective-C
// runtime is not free to query — RegisterName is a lookup in a global table —
// and these are on the path of every copy and paste.
type macPlatform struct {
	pasteboardClass objc.Class
	stringClass     objc.Class
	poolClass       objc.Class

	generalPasteboard objc.SEL
	clearContents     objc.SEL
	setStringForType  objc.SEL
	stringForType     objc.SEL
	utf8String        objc.SEL
	alloc             objc.SEL
	initWithBytes     objc.SEL
	release           objc.SEL
	drain             objc.SEL
	initSel           objc.SEL

	// pasteboardTypeString is NSPasteboardTypeString as an NSString, built
	// once and never released, so that neither a copy nor a paste has to
	// build it again.
	//
	// It is built from the literal rather than read out of the AppKit symbol
	// of the same name. The symbol is an NSString * const, so reading it
	// means turning a uintptr from dlsym into a pointer, which is exactly
	// the conversion go vet's unsafeptr check refuses and is right to refuse:
	// nothing keeps that address valid in the eyes of the compiler. The value
	// behind the symbol is the uniform type identifier public.utf8-plain-text,
	// which is a documented, versioned string in the UTI system and not an
	// implementation detail that can change under us.
	pasteboardTypeString objc.ID
}

// NSUTF8StringEncoding, from NSString.h. A number in an ABI, not a header
// constant that could change.
const nsUTF8StringEncoding = 4

// openPlatform is the constructor the portable half calls. See [platform].
func openPlatform(log *slog.Logger) (platform, error) {
	return newMacPlatform()
}

var (
	macOnce sync.Once
	macImpl *macPlatform
	macErr  error
)

// newMacPlatform loads AppKit and resolves everything, once per process. The
// once is here rather than only in the portable half because [New] may be
// called twice by an application that installs its own logger over the one the
// side effect import installed, and loading AppKit twice is pointless work.
func newMacPlatform() (*macPlatform, error) {
	macOnce.Do(func() {
		// NSPasteboard lives in AppKit. Ebitengine has already loaded it by
		// the time a user presses a key, but this package must not depend on
		// that: dlopen on an already loaded framework is a reference count
		// increment.
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			macErr = fmt.Errorf("%w: loading AppKit failed: %w", ErrUnsupported, err)
			return
		}
		m := &macPlatform{
			pasteboardClass:   objc.GetClass("NSPasteboard"),
			stringClass:       objc.GetClass("NSString"),
			poolClass:         objc.GetClass("NSAutoreleasePool"),
			generalPasteboard: objc.RegisterName("generalPasteboard"),
			clearContents:     objc.RegisterName("clearContents"),
			setStringForType:  objc.RegisterName("setString:forType:"),
			stringForType:     objc.RegisterName("stringForType:"),
			utf8String:        objc.RegisterName("UTF8String"),
			alloc:             objc.RegisterName("alloc"),
			initWithBytes:     objc.RegisterName("initWithBytes:length:encoding:"),
			release:           objc.RegisterName("release"),
			drain:             objc.RegisterName("drain"),
			initSel:           objc.RegisterName("init"),
		}
		if m.pasteboardClass == 0 || m.stringClass == 0 || m.poolClass == 0 {
			macErr = fmt.Errorf("%w: AppKit loaded but NSPasteboard is not there", ErrUnsupported)
			return
		}
		m.pasteboardTypeString = m.newString(utiUTF8PlainText)
		if m.pasteboardTypeString == 0 {
			macErr = fmt.Errorf("%w: building the NSString for %s failed", ErrUnsupported, utiUTF8PlainText)
			return
		}
		macImpl = m
	})
	return macImpl, macErr
}

// utiUTF8PlainText is the value of NSPasteboardTypeString. See the field of
// the same name.
const utiUTF8PlainText = "public.utf8-plain-text"

// pool runs f inside an NSAutoreleasePool, on one OS thread.
//
// Every one of these calls returns an autoreleased object — the pasteboard
// itself, the string that comes back from stringForType:. On the main thread
// of a Cocoa application there is a pool around the run loop iteration that
// would catch them, but gift's UI goroutine is not guaranteed to be on a
// thread that has one, and an autoreleased object with no pool is a leak plus
// a warning on stderr that the application did not write. So this file brings
// its own, one per call.
//
// # Why the thread is locked, and why leaving it unlocked crashed
//
// An NSAutoreleasePool is not an object that happens to live on a thread: it
// is pushed onto the *calling thread's* pool stack by init and popped off the
// same stack by drain. Draining it on a different thread pops a stack it was
// never on, and the result is a segmentation fault inside libobjc with the
// drain as the program counter.
//
// A goroutine is not a thread. It may be moved to another OS thread at any
// preemption point, and the purego calls inside f are exactly such points. So
// without [runtime.LockOSThread] the init above and the drain below are only
// *usually* on the same thread, and the failure is a rare, unreproducible
// crash rather than an error — one in about thirty runs of this package's
// tests, which is what review gate 14 finally root-caused after two units of
// "ghost" segmentation faults with an identical program counter.
//
// The lock is therefore a correctness requirement and not a performance
// tuning. It is the same requirement the X11 half of this package states at
// the head of clipboard_x11.go, for a different reason: there the thread is
// locked because it parks in XNextEvent, here because Objective-C counted on
// it staying put. gift's own UI goroutine is unaffected — the lock is released
// before pool returns, and a copy or a paste is a keystroke, not a frame.
func (m *macPlatform) pool(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	p := objc.ID(m.poolClass).Send(m.alloc).Send(m.initSel)
	defer p.Send(m.drain)
	f()
}

func (m *macPlatform) text() (s string, ok bool, err error) {
	m.pool(func() {
		pb := objc.ID(m.pasteboardClass).Send(m.generalPasteboard)
		if pb == 0 {
			err = fmt.Errorf("gift/clipboard: [NSPasteboard generalPasteboard] returned nil")
			return
		}
		str := pb.Send(m.stringForType, m.pasteboardTypeString)
		if str == 0 {
			// No string on the pasteboard. An image, a file, or nothing at
			// all; all three are "nothing to paste".
			return
		}
		c := objc.Send[unsafe.Pointer](str, m.utf8String)
		if c == nil {
			err = fmt.Errorf("gift/clipboard: the pasteboard string has no UTF8String")
			return
		}
		s, ok = goString(c), true
	})
	return s, ok, err
}

func (m *macPlatform) setText(s string) (err error) {
	m.pool(func() {
		pb := objc.ID(m.pasteboardClass).Send(m.generalPasteboard)
		if pb == 0 {
			err = fmt.Errorf("gift/clipboard: [NSPasteboard generalPasteboard] returned nil")
			return
		}
		ns := m.newString(s)
		if ns == 0 {
			err = fmt.Errorf("gift/clipboard: building an NSString of %d bytes failed", len(s))
			return
		}
		defer ns.Send(m.release)

		pb.Send(m.clearContents)
		if ok := objc.Send[bool](pb, m.setStringForType, ns, m.pasteboardTypeString); !ok {
			err = fmt.Errorf("gift/clipboard: [NSPasteboard setString:forType:] refused %d bytes", len(s))
		}
	})
	return err
}

// newString builds an NSString from Go bytes with alloc/init rather than with
// the convenience constructor, so that the result is owned and released here
// instead of being handed to the autorelease pool. The empty string needs a
// valid pointer, hence the one byte stand-in.
func (m *macPlatform) newString(s string) objc.ID {
	b := []byte(s)
	if len(b) == 0 {
		b = []byte{0}
		return objc.ID(m.stringClass).Send(m.alloc).Send(m.initWithBytes, unsafe.Pointer(&b[0]), 0, nsUTF8StringEncoding)
	}
	return objc.ID(m.stringClass).Send(m.alloc).Send(m.initWithBytes, unsafe.Pointer(&b[0]), len(b), nsUTF8StringEncoding)
}

// goString copies a NUL terminated C string into Go memory, stopping at
// [MaxBytes] plus the slack that lets [capText] cut at a rune boundary.
//
// The length is found by scanning, because UTF8String does not report one. The
// scan is bounded: a pasteboard string longer than the cap is going to be cut
// anyway, so there is no reason to walk a hundred megabytes of it first.
//
// The scan reads one byte at a time rather than forming a slice of the bound
// first. A slice of MaxBytes+4 bytes over a string that is ten bytes long is a
// slice over memory this process may not own, and although nothing here would
// read past the NUL, the slice itself is already the lie.
func goString(p unsafe.Pointer) string {
	const limit = MaxBytes + 4
	b := (*byte)(p)
	n := 0
	for n < limit && *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	if n == 0 {
		return ""
	}
	return string(unsafe.Slice(b, n))
}
