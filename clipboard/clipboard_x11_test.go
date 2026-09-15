//go:build linux && !android && !faketime

package clipboard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// These tests need a running X server and are off unless GIFT_CLIPBOARD_X11=1:
//
//	GIFT_CLIPBOARD_X11=1 go test ./clipboard/ -run X11 -v
//
// The gate is an environment variable and not a build tag, for the same reason
// as in clipboard_darwin_test.go: a build tag would take this file out of
// `go vet ./...` as well, and X11 code that nothing compiles is X11 code that
// nothing checks. With the tag combination above the file is compiled and
// vetted by every cross build of this package; only the bodies are skipped.
//
// **Honesty about what has and has not been run.** As of WU-AE these tests
// have never been executed: the machine this package was written on is a Mac,
// and the acceptance matrix cross-compiles for linux/arm64 but does not run
// it. They are written so that the person with a Raspberry Pi can run them in
// one command, and the report of that work unit says plainly that the X11 path
// is unverified. A test file that claimed otherwise would be the exact failure
// the work order warns about.

func requireX11(t *testing.T) *x11Clipboard {
	t.Helper()
	if os.Getenv("GIFT_CLIPBOARD_X11") != "1" {
		t.Skip("set GIFT_CLIPBOARD_X11=1 on a machine with an X server to run this; it takes ownership of the CLIPBOARD selection")
	}
	c, err := newX11Clipboard(nil)
	if err != nil {
		t.Fatalf("opening the X11 clipboard: %v", err)
	}
	return c
}

// TestX11RoundTripsThroughTheServer copies with one connection and pastes with
// a second one, which is a real conversion through the server: the second
// connection asks the first for the selection, and the first has to serve the
// SelectionRequest from its event loop for the read to return anything. It
// therefore exercises ownership, the request handler and the reply path, and
// not just a variable in this process.
func TestX11RoundTripsThroughTheServer(t *testing.T) {
	writer := requireX11(t)
	reader, err := newX11Clipboard(nil)
	if err != nil {
		t.Fatalf("opening the second connection: %v", err)
	}

	const s = "gift clipboard test: Grüße, 日本語"
	if err := writer.setText(s); err != nil {
		t.Fatalf("setText: %v", err)
	}
	got, ok, err := reader.text()
	if err != nil || !ok {
		t.Fatalf("reading it back through a second connection: %q, %v, %v", got, ok, err)
	}
	if got != s {
		t.Fatalf("read %q; want %q", got, s)
	}
}

// TestX11ReadsItsOwnClipboardWithoutDeadlocking is the shortcut in the event
// loop. Without it the loop would ask itself for the selection and then wait,
// on the very thread that would have to answer, until ReadTimeout — a
// half-second freeze on every paste after a copy, which is the most likely way
// for this file to be wrong in a way that still "works".
func TestX11ReadsItsOwnClipboardWithoutDeadlocking(t *testing.T) {
	c := requireX11(t)
	const s = "own contents"
	if err := c.setText(s); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.text()
	if err != nil || !ok || got != s {
		t.Fatalf("reading our own clipboard gave %q, %v, %v; want %q", got, ok, err, s)
	}
}

// TestX11InteroperatesWithXclip is the only test here that proves anything
// about the world outside this process. It is skipped when xclip is not
// installed, and that skip is a real gap and not a pass.
func TestX11InteroperatesWithXclip(t *testing.T) {
	c := requireX11(t)
	xclip, err := exec.LookPath("xclip")
	if err != nil {
		t.Skip("xclip is not installed; the cross-process direction is not covered by this run")
	}

	const written = "from gift: ÄÖÜ"
	if err := c.setText(written); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(xclip, "-selection", "clipboard", "-o").Output()
	if err != nil {
		t.Fatalf("xclip -o: %v", err)
	}
	if string(out) != written {
		t.Fatalf("xclip saw %q; want %q", out, written)
	}

	const fromOutside = "from xclip: 日本語"
	cmd := exec.Command(xclip, "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(fromOutside)
	if err := cmd.Run(); err != nil {
		t.Fatalf("xclip -i: %v", err)
	}
	got, ok, err := c.text()
	if err != nil || !ok || got != fromOutside {
		t.Fatalf("read %q, %v, %v; want %q", got, ok, err, fromOutside)
	}
}

// TestX11EmptyClipboardIsNotAnError covers the state of a fresh session, where
// nobody owns CLIPBOARD at all. It has to be "nothing to paste" and not a
// failure, and it must return immediately rather than after ReadTimeout — so
// run this one first in a session, before anything has copied.
func TestX11EmptyClipboardIsNotAnError(t *testing.T) {
	c := requireX11(t)
	got, ok, err := c.text()
	if err != nil {
		t.Fatalf("reading an empty clipboard reported an error: %v", err)
	}
	if ok {
		t.Skipf("something in this session owns the clipboard (%q), so the empty case cannot be observed here", got)
	}
}
