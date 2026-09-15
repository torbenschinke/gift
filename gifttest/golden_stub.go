//go:build !giftgpu

package gifttest

import (
	"image"
	"testing"
)

// AssertGolden compares the rendered frame against the PNG in testdata.
//
// This is the build without the giftgpu tag, so there is no graphics context
// and no pixels to compare. The method exists and compiles, which is the
// point: a test file reads the same in both modes and does not need a build
// tag of its own.
//
// What happens instead:
//
//   - By default the test is skipped, with a message naming the tag to use.
//     That is right for a developer running `go test ./...`, who is not
//     testing rendering and should not be told they broke it.
//   - With GIFT_REQUIRE_GOLDEN=1 in the environment it fails instead. That is
//     for the CI job which believes it checks rendering; a job like that must
//     never pass because every golden quietly skipped.
//
// What never happens is a silent pass.
func (h *Harness) AssertGolden(name string) {
	h.t.Helper()
	const msg = "gifttest: golden image %q needs real pixels.\n" +
		"Run it with the build tag:  go test -tags giftgpu ./...\n" +
		"Set GIFT_REQUIRE_GOLDEN=1 to turn this skip into a failure in a CI job that " +
		"is supposed to check rendering."
	if requireGolden() {
		h.t.Fatalf(msg, name)
		return
	}
	h.t.Skipf(msg, name)
}

// Image renders the current frame and returns it.
//
// Without the giftgpu tag there is nothing to render into, so this fails. Use
// [Harness.Ops] for the headless answer to "what was drawn": it is the display
// list, which is the representation gift actually produces, and every
// structural assertion can be made on it.
func (h *Harness) Image() image.Image {
	h.t.Helper()
	h.t.Fatalf("gifttest: Image needs a graphics context; build with -tags giftgpu, " +
		"or assert on the display list with Harness.Ops")
	return nil
}

// Warm is a no-op in a build without a graphics context.
//
// With the giftgpu tag it renders one throwaway frame so that a picture is
// resident before a golden is taken; see the tagged half. Without it there is
// no renderer, no texture cache and no upload budget, so there is nothing to
// warm and nothing to report. This is the one method of the package that is
// deliberately silent in both modes, because "no GPU" is not a missing answer
// here — it is the absence of the question.
func (h *Harness) Warm() {}

// framebuffer is the pixel source of [Harness.AssertPixel].
//
// Without the giftgpu tag there is none, so it skips — or fails under
// GIFT_REQUIRE_GOLDEN — under exactly the rule [Harness.AssertGolden] uses,
// and for the same reason: a developer running `go test ./...` is not testing
// rendering, and a CI job that believes it is must not pass on skips.
func (h *Harness) framebuffer(what string) (image.Image, bool) {
	h.t.Helper()
	const msg = "gifttest: %s needs real pixels.\n" +
		"Run it with the build tag:  go test -tags giftgpu ./...\n" +
		"Set GIFT_REQUIRE_GOLDEN=1 to turn this skip into a failure in a CI job that " +
		"is supposed to check rendering."
	if requireGolden() {
		h.t.Fatalf(msg, what)
		return nil, false
	}
	h.t.Skipf(msg, what)
	return nil, false
}

// Main is the TestMain a package with golden tests installs:
//
//	func TestMain(m *testing.M) { gifttest.Main(m) }
//
// Without the giftgpu tag it is an ordinary m.Run. With the tag it starts
// Ebitengine's run loop first, because reading pixels back is only legal
// inside it — the pinned source says so at ebiten.Image.ReadPixels: "ReadPixels
// can't be called outside the main loop". A package that installs it therefore
// works in both modes with one line and no build tag of its own.
func Main(m *testing.M) { osExit(m.Run()) }
