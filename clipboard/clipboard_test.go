package clipboard

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// What this file does and does not prove, because that distinction is the
// whole point of testing a clipboard.
//
// It proves everything above the [platform] seam: the size cap and where
// exactly it cuts, the refusal of invalid UTF-8 in both directions, the
// single attempt at opening a platform that is not there, the log lines that
// are the only report of a truncation, and the fact that a failed open leaves
// copy and paste as harmless no-ops rather than as panics.
//
// It proves **nothing** about libX11 or NSPasteboard. No test in a default
// `go test ./...` talks to a display server, because on the machine this
// project is built on there is none, and a test that passed because the
// platform was absent would be worth less than no test. The two tests that do
// talk to a real system are in clipboard_darwin_test.go and
// clipboard_x11_test.go and are gated by environment variables; what they
// cover and how to run them is written there.

// fakePlatform is a [platform] that lives in the test. It records what it was
// asked to store, so that a test can see what survived the cap.
type fakePlatform struct {
	mu    sync.Mutex
	text_ string
	ok    bool

	readErr  error
	writeErr error
	reads    int
	writes   int
}

func (f *fakePlatform) text() (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.text_, f.ok, f.readErr
}

func (f *fakePlatform) setText(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.writeErr != nil {
		return f.writeErr
	}
	f.text_, f.ok = s, true
	return nil
}

// newTestClipboard wires a clipboard to a fake platform and a logger whose
// output the test can read.
func newTestClipboard(t *testing.T, p *fakePlatform) (*clipboard, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return newClipboard(func(*slog.Logger) (platform, error) { return p, nil }, log), &buf
}

func TestRoundTripThroughThePlatform(t *testing.T) {
	p := &fakePlatform{}
	c, _ := newTestClipboard(t, p)

	if s, ok := c.Text(); ok || s != "" {
		t.Fatalf("an untouched clipboard reported %q, %v; want %q, false", s, ok, "")
	}
	c.SetText("Grüße")
	s, ok := c.Text()
	if !ok || s != "Grüße" {
		t.Fatalf("after SetText the clipboard reported %q, %v; want %q, true", s, ok, "Grüße")
	}
	if p.writes != 1 || p.reads != 2 {
		t.Fatalf("the platform saw %d writes and %d reads; want 1 and 2", p.writes, p.reads)
	}
}

func TestCopyingTheEmptyStringIsACopy(t *testing.T) {
	// The same distinction ui.MemoryClipboard documents: writing "" leaves a
	// clipboard that holds the empty string, not an empty one. A platform
	// that turned this into a clear would make Ctrl+C on an empty selection
	// destroy what the user had — which is why the text field refuses that
	// case above this package, but the behaviour still has to be the same on
	// both sides of the seam.
	p := &fakePlatform{}
	c, _ := newTestClipboard(t, p)
	c.SetText("")
	if s, ok := c.Text(); !ok || s != "" {
		t.Fatalf("after SetText(\"\") the clipboard reported %q, %v; want \"\", true", s, ok)
	}
}

func TestWritingMoreThanMaxBytesTruncatesAndSaysSo(t *testing.T) {
	p := &fakePlatform{}
	c, buf := newTestClipboard(t, p)

	c.SetText(strings.Repeat("a", MaxBytes+1000))

	if got := len(p.text_); got != MaxBytes {
		t.Fatalf("the platform was given %d bytes; want exactly MaxBytes = %d", got, MaxBytes)
	}
	if !strings.Contains(buf.String(), "truncating") {
		t.Fatalf("a truncation was not logged; the log said:\n%s", buf.String())
	}
	// And the caller was told nothing, because ui.Clipboard has no way to
	// tell it. That is the documented limitation, asserted rather than only
	// described: SetText has no result to check, so the only observable
	// difference between a full copy and a truncated one is this log line.
}

func TestReadingMoreThanMaxBytesTruncates(t *testing.T) {
	p := &fakePlatform{text_: strings.Repeat("b", MaxBytes+1), ok: true}
	c, buf := newTestClipboard(t, p)

	s, ok := c.Text()
	if !ok {
		t.Fatal("an over-long clipboard reported nothing to paste; want the first MaxBytes of it")
	}
	if len(s) != MaxBytes {
		t.Fatalf("a paste produced %d bytes; want MaxBytes = %d", len(s), MaxBytes)
	}
	if !strings.Contains(buf.String(), "truncating the paste") {
		t.Fatalf("the truncated paste was not logged; the log said:\n%s", buf.String())
	}
}

func TestTheCapCutsAtARuneBoundary(t *testing.T) {
	// A three byte rune straddling the cap. Cutting in the middle of it
	// would produce invalid UTF-8, which this package then refuses, so an
	// off-by-one here would turn "too long" into "nothing at all".
	head := strings.Repeat("a", MaxBytes-1)
	s := head + "€" + "tail"

	got, cut := capText(s)
	if !cut {
		t.Fatal("capText reported no truncation for a string longer than MaxBytes")
	}
	if got != head {
		t.Fatalf("capText kept %d bytes; want %d, the whole string before the straddling rune", len(got), len(head))
	}
	if !utf8.ValidString(got) {
		t.Fatal("capText produced invalid UTF-8")
	}
}

func TestTheCapIsInclusive(t *testing.T) {
	// Exactly MaxBytes is not too much. An off-by-one here is invisible in
	// use and would cut one byte off every maximal copy.
	s := strings.Repeat("a", MaxBytes)
	if got, cut := capText(s); cut || len(got) != MaxBytes {
		t.Fatalf("capText(%d bytes) = %d bytes, cut=%v; want unchanged", MaxBytes, len(got), cut)
	}
	if got, cut := capText("short"); cut || got != "short" {
		t.Fatalf("capText(%q) = %q, cut=%v; want unchanged", "short", got, cut)
	}
}

func TestInvalidUTF8IsRefusedInBothDirections(t *testing.T) {
	// Reading. X11 hands out bytes and an owner that labels them UTF8_STRING
	// can still be wrong. Repairing them would put replacement characters
	// into the user's text field.
	p := &fakePlatform{text_: "a\xffb", ok: true}
	c, buf := newTestClipboard(t, p)
	if s, ok := c.Text(); ok {
		t.Fatalf("invalid UTF-8 was pasted as %q; want nothing to paste", s)
	}
	if !strings.Contains(buf.String(), "not valid UTF-8") {
		t.Fatalf("the refusal was not logged; the log said:\n%s", buf.String())
	}

	// Writing. A gift text field cannot produce this, an application calling
	// ui.CurrentClipboard().SetText can.
	p2 := &fakePlatform{}
	c2, buf2 := newTestClipboard(t, p2)
	c2.SetText("a\xffb")
	if p2.writes != 0 {
		t.Fatalf("invalid UTF-8 reached the platform; want it refused above the seam")
	}
	if !strings.Contains(buf2.String(), "refusing to copy") {
		t.Fatalf("the refusal was not logged; the log said:\n%s", buf2.String())
	}
}

func TestAPlatformThatWillNotOpenIsTriedOnceAndThenIsHarmless(t *testing.T) {
	var opens int
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	boom := errors.New("no libX11 here")
	c := newClipboard(func(*slog.Logger) (platform, error) {
		opens++
		return nil, boom
	}, log)

	for range 5 {
		c.SetText("hello")
		if s, ok := c.Text(); ok || s != "" {
			t.Fatalf("a clipboard with no platform pasted %q, %v; want %q, false", s, ok, "")
		}
	}
	if opens != 1 {
		t.Fatalf("the platform was opened %d times; want exactly 1, see New on why there is no retry", opens)
	}
	if n := strings.Count(buf.String(), "clipboard unavailable"); n != 1 {
		t.Fatalf("the failure was logged %d times; want exactly 1", n)
	}
	if !strings.Contains(buf.String(), boom.Error()) {
		t.Fatalf("the log does not name the cause; it said:\n%s", buf.String())
	}
}

func TestPlatformErrorsAreLoggedAndLookLikeAnEmptyClipboard(t *testing.T) {
	boom := errors.New("the owner never answered")
	p := &fakePlatform{readErr: boom, writeErr: boom}
	c, buf := newTestClipboard(t, p)

	if s, ok := c.Text(); ok || s != "" {
		t.Fatalf("a failed read produced %q, %v; want %q, false", s, ok, "")
	}
	c.SetText("x")

	out := buf.String()
	if !strings.Contains(out, "reading the clipboard failed") || !strings.Contains(out, "writing the clipboard failed") {
		t.Fatalf("a failing platform did not produce both log lines; the log said:\n%s", out)
	}
}

func TestANilLoggerIsSilenceAndNotAPanic(t *testing.T) {
	// Section 15: without a logger, logging is off rather than "default".
	// This is what the side effect import installs, so it is the
	// configuration most binaries actually run.
	p := &fakePlatform{readErr: errors.New("boom")}
	c := newClipboard(func(*slog.Logger) (platform, error) { return p, nil }, nil)
	c.SetText(strings.Repeat("a", MaxBytes+1)) // would log a truncation
	if _, ok := c.Text(); ok {                 // would log an error
		t.Fatal("a failing read reported success")
	}
}

func TestNewDegradesRatherThanPanicking(t *testing.T) {
	// The real constructor, on whatever machine this runs on. On a headless
	// one it finds no platform; on a Mac it finds NSPasteboard. Either way
	// the call returns and does not panic, and that — not the content — is
	// all this test claims.
	//
	// It only reads. Writing here would put a test string into the
	// clipboard of whoever ran `go test ./...`, which is somebody's actual
	// clipboard and not a fixture. The write path against a real pasteboard
	// is in clipboard_darwin_test.go, behind an environment variable, and it
	// puts back what it found.
	_, _ = New(nil).Text()
}
