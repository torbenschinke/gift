//go:build darwin

package clipboard

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ebitengine/purego/objc"
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
// running it had copied — which is why every test here calls
// [preservePasteboard], and why it still refuses to run without being asked.

func requirePasteboard(t *testing.T) *macPlatform {
	t.Helper()
	if os.Getenv("GIFT_CLIPBOARD_PASTEBOARD") != "1" {
		t.Skip("set GIFT_CLIPBOARD_PASTEBOARD=1 to run this against the real NSPasteboard; it overwrites and restores the clipboard of this machine")
	}
	m, err := newMacPlatform()
	if err != nil {
		t.Fatalf("opening NSPasteboard: %v", err)
	}
	preservePasteboard(t, m)
	return m
}

// preservePasteboard saves the pasteboard and puts it back when the test ends.
//
// The obvious version — remember the text, write it back if there was one — is
// what these tests used to do, and it is wrong in both of the cases where the
// answer is not a string. WU-AH found both:
//
//   - The pasteboard was *empty*. "There was no text, so restore nothing"
//     leaves this test's own string on the machine's clipboard for ever. The
//     restoration for that case is a clearContents, which is what the else
//     branch below does.
//   - The pasteboard held something that is not text — a picture, a file
//     reference, rich text with no plain fallback. That cannot be saved and
//     put back through a string-shaped seam at all, so the test skips instead
//     of destroying it. Copying some text first makes it run.
func preservePasteboard(t *testing.T, m *macPlatform) {
	t.Helper()
	before, had, err := m.text()
	if err != nil {
		t.Fatalf("reading the pasteboard before the test: %v", err)
	}
	if !had && pasteboardTypeCount(m) > 0 {
		t.Skip("the pasteboard holds something that is not text; this test would destroy it and " +
			"cannot put it back. Copy some text, or empty the clipboard, and run it again")
	}
	t.Cleanup(func() {
		if had {
			if err := m.setText(before); err != nil {
				t.Errorf("putting the clipboard back: %v", err)
			}
			return
		}
		// It was empty, so leaving this test's text on it would be a change
		// the person who ran the test did not ask for.
		clearPasteboard(m)
	})
}

// pasteboardTypeCount is [[[NSPasteboard generalPasteboard] types] count]. It
// is the one question the platform layer does not need to ask and the test
// does: "is the pasteboard empty, or does it hold something I cannot read?"
func pasteboardTypeCount(m *macPlatform) uint64 {
	var n uint64
	m.pool(func() {
		pb := objc.ID(m.pasteboardClass).Send(m.generalPasteboard)
		if pb == 0 {
			return
		}
		types := pb.Send(objc.RegisterName("types"))
		if types == 0 {
			return
		}
		n = uint64(types.Send(objc.RegisterName("count")))
	})
	return n
}

// clearPasteboard is [[NSPasteboard generalPasteboard] clearContents], which is
// how an empty pasteboard is restored to being empty.
func clearPasteboard(m *macPlatform) {
	m.pool(func() {
		if pb := objc.ID(m.pasteboardClass).Send(m.generalPasteboard); pb != 0 {
			pb.Send(m.clearContents)
		}
	})
}

// TestPasteboardRoundTripsThroughAnotherProcess is the real verification: the
// text is written with NSPasteboard through purego and read back with
// /usr/bin/pbpaste, which is a different process, and then written by
// /usr/bin/pbcopy and read back through purego. Nothing here would pass if the
// Objective-C calls only appeared to work.
func TestPasteboardRoundTripsThroughAnotherProcess(t *testing.T) {
	m := requirePasteboard(t)

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
