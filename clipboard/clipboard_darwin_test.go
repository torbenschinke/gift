//go:build darwin

package clipboard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// This test talks to the real pasteboard of the machine it runs on, so it is
// off unless GIFT_CLIPBOARD_PASTEBOARD=1:
//
//	GIFT_CLIPBOARD_PASTEBOARD=1 go test ./clipboard/ -run Pasteboard -v
//
// The gate is an environment variable rather than a build tag for one reason:
// a build tag would also hide the file from `go vet ./...` and from
// compilation, and code that is not compiled rots. This way the file is built
// and vetted by every default run and only its body is skipped.
//
// It is gated at all because the pasteboard is a shared resource of a logged-in
// human. A test that overwrites it is a test that loses whatever the person
// running it had copied — which is why this one reads the old contents first
// and puts them back, and why it still refuses to run without being asked.

func requirePasteboard(t *testing.T) *macPlatform {
	t.Helper()
	if os.Getenv("GIFT_CLIPBOARD_PASTEBOARD") != "1" {
		t.Skip("set GIFT_CLIPBOARD_PASTEBOARD=1 to run this against the real NSPasteboard; it overwrites and restores the clipboard of this machine")
	}
	m, err := newMacPlatform()
	if err != nil {
		t.Fatalf("opening NSPasteboard: %v", err)
	}
	return m
}

// TestPasteboardRoundTripsThroughAnotherProcess is the real verification: the
// text is written with NSPasteboard through purego and read back with
// /usr/bin/pbpaste, which is a different process, and then written by
// /usr/bin/pbcopy and read back through purego. Nothing here would pass if the
// Objective-C calls only appeared to work.
func TestPasteboardRoundTripsThroughAnotherProcess(t *testing.T) {
	m := requirePasteboard(t)

	before, hadBefore, err := m.text()
	if err != nil {
		t.Fatalf("reading the pasteboard before the test: %v", err)
	}
	t.Cleanup(func() {
		if hadBefore {
			_ = m.setText(before)
		}
	})

	const written = "gift clipboard test: Grüße, 日本語, and a \t tab"
	if err := m.setText(written); err != nil {
		t.Fatalf("setText: %v", err)
	}
	out, err := exec.Command("/usr/bin/pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste: %v", err)
	}
	if string(out) != written {
		t.Fatalf("pbpaste, in another process, saw %q; want %q", out, written)
	}

	const fromOutside = "written by pbcopy: ÄÖÜ"
	cmd := exec.Command("/usr/bin/pbcopy")
	cmd.Stdin = strings.NewReader(fromOutside)
	if err := cmd.Run(); err != nil {
		t.Fatalf("pbcopy: %v", err)
	}
	got, ok, err := m.text()
	if err != nil || !ok {
		t.Fatalf("reading back what pbcopy wrote: %q, %v, %v", got, ok, err)
	}
	if got != fromOutside {
		t.Fatalf("read %q; want %q", got, fromOutside)
	}
}

// TestPasteboardClearsBeforeWriting checks the one call that is easy to leave
// out and impossible to notice: without clearContents the new string is added
// to the existing declaration, and a reader that asks for a different type
// gets the stale one.
func TestPasteboardClearsBeforeWriting(t *testing.T) {
	m := requirePasteboard(t)
	before, hadBefore, _ := m.text()
	t.Cleanup(func() {
		if hadBefore {
			_ = m.setText(before)
		}
	})

	if err := m.setText("first"); err != nil {
		t.Fatal(err)
	}
	if err := m.setText("second"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := m.text()
	if got != "second" {
		t.Fatalf("after two writes the pasteboard holds %q; want %q", got, "second")
	}
}

// TestPasteboardHandlesTheEmptyString is here because the empty string takes
// the other branch of newString — the one with the stand-in byte, because
// there is no &b[0] of an empty slice.
func TestPasteboardHandlesTheEmptyString(t *testing.T) {
	m := requirePasteboard(t)
	before, hadBefore, _ := m.text()
	t.Cleanup(func() {
		if hadBefore {
			_ = m.setText(before)
		}
	})

	if err := m.setText(""); err != nil {
		t.Fatalf("setText(\"\"): %v", err)
	}
	got, ok, err := m.text()
	if err != nil {
		t.Fatalf("reading back the empty string: %v", err)
	}
	if !ok || got != "" {
		t.Fatalf("after copying the empty string the pasteboard reports %q, %v; want \"\", true", got, ok)
	}
}
