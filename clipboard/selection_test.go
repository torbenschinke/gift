package clipboard

import (
	"errors"
	"strings"
	"testing"
)

// The X11 selection logic is tested here, on any machine, with no X server
// anywhere in sight. That is possible because selection.go is deliberately
// free of syscalls and of build tags; see the comment at the top of it.
//
// What this covers is the target negotiation in both directions — which is the
// part of X11 clipboard code that is actually easy to get wrong, and the part
// where a mistake shows up as "pasting into xterm gives nothing" rather than
// as a crash.

// testAtoms is an atom table with distinguishable numbers. Real atoms are
// whatever the server interned; nothing in the logic may depend on their
// values, and using 4 and 19 here would hide a confusion with the predefined
// XA_ATOM and XA_INTEGER.
var testAtoms = atoms{
	clipboard:     100,
	targets:       101,
	timestamp:     102,
	multiple:      103,
	incr:          104,
	utf8String:    105,
	textPlainUTF8: 106,
	textPlain:     107,
	xaString:      31,
	giftSelection: 108,
}

func TestAnswerOffersTargetsIncludingItself(t *testing.T) {
	r, ok := testAtoms.answer(testAtoms.targets, "hello")
	if !ok {
		t.Fatal("a request for TARGETS was refused")
	}
	if r.typ != atomAtom || r.format != 32 {
		t.Fatalf("TARGETS answered with type %d format %d; want XA_ATOM (%d) and format 32", r.typ, r.format, atomAtom)
	}
	got := decodeLongs(r.data)
	if len(got) != r.items() {
		t.Fatalf("items() reported %d for %d atoms; the count XChangeProperty gets is elements, not bytes", r.items(), len(got))
	}
	want := map[xAtom]bool{testAtoms.targets: false, testAtoms.utf8String: false, testAtoms.xaString: false}
	for _, a := range got {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for a, seen := range want {
		if !seen {
			t.Errorf("the TARGETS list does not contain atom %d; ICCCM wants TARGETS itself in the list and a requestor wants the text targets", a)
		}
	}
}

func TestAnswerServesTheUTF8Targets(t *testing.T) {
	for _, target := range []xAtom{testAtoms.utf8String, testAtoms.textPlainUTF8, testAtoms.textPlain} {
		r, ok := testAtoms.answer(target, "Grüße")
		if !ok {
			t.Fatalf("a request for atom %d was refused", target)
		}
		if r.format != 8 || r.typ != target {
			t.Fatalf("atom %d answered with type %d format %d; want the requested type at format 8", target, r.typ, r.format)
		}
		if string(r.data) != "Grüße" {
			t.Fatalf("atom %d answered with %q", target, r.data)
		}
		if r.items() != len("Grüße") {
			t.Fatalf("items() = %d at format 8; want the byte count %d", r.items(), len("Grüße"))
		}
	}
}

func TestAnswerConvertsToLatin1ForXAStringAndRefusesWhatWillNotFit(t *testing.T) {
	// The bug this prevents: handing UTF-8 bytes out under XA_STRING, which
	// is Latin-1, so that an old application pastes "GrÃ¼ÃŸe".
	r, ok := testAtoms.answer(testAtoms.xaString, "Grüße")
	if !ok {
		t.Fatal("XA_STRING was refused for text that fits into Latin-1")
	}
	if want := []byte{'G', 'r', 0xFC, 0xDF, 'e'}; string(r.data) != string(want) {
		t.Fatalf("XA_STRING answered with % x; want the Latin-1 bytes % x", r.data, want)
	}

	if _, ok := testAtoms.answer(testAtoms.xaString, "日本語"); ok {
		t.Fatal("XA_STRING was served for text that does not exist in Latin-1; a requestor must be told no so that it asks for UTF8_STRING")
	}
}

func TestAnswerRefusesMultipleAndAnythingUnknown(t *testing.T) {
	if _, ok := testAtoms.answer(testAtoms.multiple, "x"); ok {
		t.Fatal("MULTIPLE was accepted; the file documents it as refused")
	}
	if _, ok := testAtoms.answer(999, "x"); ok {
		t.Fatal("an unknown target was accepted")
	}
	if _, ok := testAtoms.answer(testAtoms.incr, "x"); ok {
		t.Fatal("INCR was accepted as a target")
	}
}

func TestAnswerServesTimestampAsAnInteger(t *testing.T) {
	r, ok := testAtoms.answer(testAtoms.timestamp, "x")
	if !ok || r.typ != atomInteger || r.format != 32 {
		t.Fatalf("TIMESTAMP answered %+v, ok=%v; want XA_INTEGER at format 32", r, ok)
	}
}

func TestDecodeAcceptsEveryTargetItAsksFor(t *testing.T) {
	// The invariant worth holding: every target in readTargets can be
	// decoded. A target this package asks for and then cannot read would be
	// a paste that silently produces nothing.
	for _, target := range testAtoms.readTargets() {
		s, ok, err := testAtoms.decode(target, 8, []byte("ok"))
		if err != nil || !ok || s != "ok" {
			t.Fatalf("decode of a reply to the requested target %d gave %q, %v, %v", target, s, ok, err)
		}
	}
}

func TestDecodeLatin1FromXAString(t *testing.T) {
	s, ok, err := testAtoms.decode(testAtoms.xaString, 8, []byte{'G', 'r', 0xFC, 0xDF, 'e'})
	if err != nil || !ok {
		t.Fatalf("decode of XA_STRING failed: %v, %v", ok, err)
	}
	if s != "Grüße" {
		t.Fatalf("XA_STRING decoded to %q; want %q — those bytes are Latin-1, not UTF-8", s, "Grüße")
	}
}

func TestDecodeEmptyPropertyIsTheEmptyStringAndNotAnAbsence(t *testing.T) {
	s, ok, err := testAtoms.decode(testAtoms.utf8String, 8, nil)
	if err != nil || !ok || s != "" {
		t.Fatalf("an empty UTF8_STRING property decoded to %q, %v, %v; want \"\", true, nil", s, ok, err)
	}
	// Type None is the absence: there was no property at all.
	if _, ok, err := testAtoms.decode(0, 0, nil); ok || err != nil {
		t.Fatalf("a missing property decoded to ok=%v, err=%v; want false and no error", ok, err)
	}
}

func TestDecodeReportsINCRAsAnErrorAndNotAsEmpty(t *testing.T) {
	_, ok, err := testAtoms.decode(testAtoms.incr, 32, []byte("\x00\x00\x00\x00\x00\x00\x00\x00"))
	if ok || err == nil {
		t.Fatal("an INCR reply was treated as an ordinary empty clipboard")
	}
	if !strings.Contains(err.Error(), "INCR") || !strings.Contains(err.Error(), "MaxBytes") {
		t.Fatalf("the INCR error does not say what it is or what the limit is: %v", err)
	}
}

func TestDecodeRefusesNonTextAndWrongFormats(t *testing.T) {
	if _, ok, err := testAtoms.decode(555, 8, []byte("x")); ok || err == nil {
		t.Fatal("a reply of an unknown type was accepted as text")
	}
	if _, ok, err := testAtoms.decode(testAtoms.utf8String, 32, make([]byte, 8)); ok || err == nil {
		t.Fatal("a UTF8_STRING at format 32 was accepted; text is bytes")
	}
}

func TestLongEncodingRoundTrips(t *testing.T) {
	// Format 32 property data is an array of C long, which is eight bytes on
	// every platform this package supports. Reading it as four would return
	// half the atoms and a lot of zeroes.
	in := []xAtom{1, 2, 0xDEADBEEF}
	b := encodeLongs(in)
	if len(b) != 8*len(in) {
		t.Fatalf("encodeLongs produced %d bytes for %d atoms; want 8 per atom", len(b), len(in))
	}
	out := decodeLongs(b)
	if len(out) != len(in) {
		t.Fatalf("round trip produced %d atoms; want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Fatalf("atom %d round tripped to %d", in[i], out[i])
		}
	}
}

func TestLatin1RoundTrip(t *testing.T) {
	for _, s := range []string{"", "plain ascii", "Grüße", "ÿ"} {
		b, ok := encodeLatin1(s)
		if !ok {
			t.Fatalf("encodeLatin1(%q) refused text that fits", s)
		}
		if got := decodeLatin1(b); got != s {
			t.Fatalf("%q round tripped to %q", s, got)
		}
	}
}

func TestErrUnsupportedIsFindable(t *testing.T) {
	_, err := openPlatform(nil)
	if err == nil {
		return // a machine with a clipboard; nothing to assert here
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a platform that would not open returned %v, which errors.Is does not match to ErrUnsupported", err)
	}
}
