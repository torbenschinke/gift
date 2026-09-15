package clipboard

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// This file is the part of the X11 selection protocol that is arithmetic and
// decision rather than syscall: which target a requestor asked for, what to
// answer with, and how to turn the bytes of a property back into a Go string.
//
// It carries **no build tag on purpose**. The code below is dead weight in a
// darwin binary — a few hundred bytes the linker keeps because the test file
// next to it references it — and in exchange it is covered by `go test ./...`
// on a machine with no X server, which is where this project's CI and the
// author's laptop both are. The alternative, guarding it with `//go:build
// linux`, would have put the only genuinely tricky logic in this package
// behind a tag that nothing in the acceptance matrix builds. The syscall half
// is in clipboard_x11.go and is tagged.

// xAtom is an X11 atom, an interned name. libX11 declares Atom as an unsigned
// long, which is 64 bit on every platform this package supports, so uintptr
// would be the same width; uint64 is used to keep this file free of anything
// platform shaped.
type xAtom = uint64

// atoms is the set of interned names this package needs. It is filled once per
// connection by the X11 layer and is a plain value here so that the tests can
// make one up.
type atoms struct {
	clipboard     xAtom // CLIPBOARD, the selection a Ctrl+C fills.
	targets       xAtom // TARGETS, "what forms can you give me?"
	timestamp     xAtom // TIMESTAMP, "when did you take ownership?"
	multiple      xAtom // MULTIPLE, a batch of conversions. Refused, see below.
	incr          xAtom // INCR, the chunked transfer. Refused, see section 19.
	utf8String    xAtom // UTF8_STRING, the modern text target.
	textPlainUTF8 xAtom // text/plain;charset=utf-8, what GTK and browsers ask for.
	textPlain     xAtom // text/plain.
	xaString      xAtom // XA_STRING, the Latin-1 target from 1987.
	giftSelection xAtom // GIFT_SELECTION, the property a read is delivered into.
}

// readTargets is the order in which a paste asks for the text, best first.
//
// UTF8_STRING before text/plain;charset=utf-8 because it is what every toolkit
// offers, and XA_STRING last because it is Latin-1 and therefore lossy for
// everything outside the first 256 code points. Asking in this order and
// stopping at the first owner that answers is the whole of the negotiation on
// the reading side; there is no TARGETS round trip first, because that would
// double the number of round trips to learn something the fallback discovers
// anyway.
func (a atoms) readTargets() [3]xAtom {
	return [3]xAtom{a.utf8String, a.textPlainUTF8, a.xaString}
}

// selectionReply is what this package puts into the requestor's property when
// it owns the selection and somebody asks for it.
//
// format is the X11 property format, 8 or 32, and it decides how data is read
// back out: at format 32 the data is an array of C long, which is 64 bit here,
// which is a libX11 peculiarity rather than an X11 one — the wire carries 32
// bit values and the library widens them. [selectionReply.items] does that
// arithmetic in one place so that the call site cannot get it wrong.
type selectionReply struct {
	typ    xAtom
	format int
	data   []byte
}

// items is the count XChangeProperty wants, which is elements and not bytes.
func (r selectionReply) items() int {
	if r.format == 32 {
		return len(r.data) / 8
	}
	return len(r.data)
}

// answer decides what to reply to a SelectionRequest for target, given the
// text this process currently owns.
//
// It reports false for "refused", which on the wire is a SelectionNotify with
// property None. Refusing is the correct answer for a target this package does
// not speak, and a requestor that gets it tries another target or gives up.
//
// MULTIPLE is refused deliberately. Serving it means parsing an
// ATOM_PAIR property out of the requestor's window and answering each half,
// which is a second protocol on top of this one for a convenience nothing in
// the wild needs from a text-only owner: every toolkit that asks for MULTIPLE
// falls back to individual conversions.
//
// INCR is never answered either, in either direction. Section 19 drops it and
// [MaxBytes] is what replaces it, so a reply is always one property and never
// a stream.
func (a atoms) answer(target xAtom, owned string) (selectionReply, bool) {
	switch target {
	case a.targets:
		// The list must include TARGETS itself; ICCCM says so and some
		// requestors check. The order is best first, for a requestor that
		// takes the first one it recognises.
		list := []xAtom{a.targets, a.timestamp, a.utf8String, a.textPlainUTF8, a.textPlain, a.xaString}
		return selectionReply{typ: atomAtom, format: 32, data: encodeLongs(list)}, true

	case a.utf8String, a.textPlainUTF8, a.textPlain:
		return selectionReply{typ: target, format: 8, data: []byte(owned)}, true

	case a.xaString:
		// XA_STRING is Latin-1. Handing UTF-8 bytes out under it is what
		// produces the mojibake that a paste into an old application shows,
		// so the text is converted, and refused when it does not fit.
		b, ok := encodeLatin1(owned)
		if !ok {
			return selectionReply{}, false
		}
		return selectionReply{typ: a.xaString, format: 8, data: b}, true

	case a.timestamp:
		// Answered by the caller, which is the only place that knows the
		// timestamp of the ownership. The zero reply here is a marker.
		return selectionReply{typ: atomInteger, format: 32}, true

	default:
		return selectionReply{}, false
	}
}

// decode turns a property that was read back into text.
//
// The three outcomes are different and a caller has to tell them apart: text,
// "there is nothing here for me" and "something went wrong and should be
// logged". An empty property of the right type is text — the empty string —
// and not an absence, because an owner that copied nothing is a state and not
// a failure.
func (a atoms) decode(typ xAtom, format int, data []byte) (string, bool, error) {
	switch {
	case typ == 0:
		return "", false, nil

	case typ == a.incr:
		// Section 19 records this as out of scope. It is reported as an
		// error rather than as an empty clipboard because the difference
		// matters to whoever reads the log: the text is there, this package
		// declines to fetch it.
		return "", false, fmt.Errorf("gift/clipboard: the owner offers the selection through INCR, which is not implemented; see MaxBytes (%d)", MaxBytes)

	case format != 8:
		return "", false, fmt.Errorf("gift/clipboard: the selection came back at format %d, which is not text", format)

	case typ == a.utf8String || typ == a.textPlainUTF8 || typ == a.textPlain:
		return string(data), true, nil

	case typ == a.xaString:
		return decodeLatin1(data), true, nil

	default:
		return "", false, fmt.Errorf("gift/clipboard: the selection came back as atom %d, which is not a text target", typ)
	}
}

// The two atoms every X server predefines, so that they need no round trip.
// XA_ATOM is 4 and XA_INTEGER is 19 in X11/Xatom.h, and those numbers are part
// of the protocol rather than of a server.
const (
	atomAtom    xAtom = 4
	atomInteger xAtom = 19
)

// encodeLongs lays atoms out the way libX11 wants format 32 data: an array of
// C long in the machine's byte order. This is the one place in this package
// that has to know that a 32 bit X11 value is a 64 bit C long on LP64.
func encodeLongs(v []xAtom) []byte {
	b := make([]byte, 8*len(v))
	for i, a := range v {
		binary.NativeEndian.PutUint64(b[8*i:], a)
	}
	return b
}

// decodeLongs is the inverse, for a property read back at format 32.
func decodeLongs(b []byte) []xAtom {
	v := make([]xAtom, len(b)/8)
	for i := range v {
		v[i] = binary.NativeEndian.Uint64(b[8*i:])
	}
	return v
}

// encodeLatin1 converts UTF-8 to the Latin-1 that XA_STRING means, and reports
// false if the text does not fit into it.
//
// Refusing is better than substituting: a question mark where the user's ä was
// is text they did not write, and the requestor that asked for XA_STRING has a
// documented way to hear no and ask for UTF8_STRING instead.
func encodeLatin1(s string) ([]byte, bool) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, false
		}
		b = append(b, byte(r))
	}
	return b, true
}

// decodeLatin1 is the inverse and cannot fail: every byte is a code point.
//
// The fast path matters less than the fact that it exists: a pure ASCII
// property, which is the overwhelming majority, is returned without a
// conversion and therefore allocates once instead of twice.
func decodeLatin1(b []byte) string {
	if utf8.Valid(b) && isASCII(b) {
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
