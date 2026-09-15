package ui

import (
	"strconv"
	"strings"

	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/text"
)

// This file exposes the text measurement internals to the external test
// package. It is a _test.go file, so nothing here is part of the API; it
// exists so that a test can compare what layout reserved against what
// internal/text measured, which is the one property of the text stack that has
// to be checked from outside.

// MeasureForTest returns the extent internal/text computes for the given text.
//
// It panics on a string that ends in whitespace, and that is not pedantry. A
// shaped line reports a width that *excludes* its trailing whitespace, on
// purpose — a label ending in a space must not be wider than the text it shows
// — so measuring "hello " returns the width of "hello". A test that converts a
// measured width into a window coordinate, which is what every click test in
// textfield_test.go does, would then compute an x that is one space too far to
// the left and fail somewhere else entirely, with an offset that looks like an
// off-by-one in the widget. The caret position after a trailing space is a
// different quantity with a test of its own; see
// TestTheCaretSitsAfterATrailingSpace.
func MeasureForTest(f Font, s string, size, maxWidth float32) geom.Size {
	if t := strings.TrimRight(s, " \t\n\v\f\r\u0085\u00a0"); t != s {
		panic("gift/ui: MeasureForTest(" + strconv.Quote(s) + "): a shaped line does not count " +
			"its trailing whitespace, so this returns the width of " + strconv.Quote(t) +
			" and a test that turns it into a coordinate lands in the wrong place. Measure the " +
			"prefix you actually mean, or assert the caret position instead.")
	}
	return text.Default().Measure(text.Request{Text: s, Font: f.f, Size: size, MaxWidth: maxWidth})
}

// DefaultFieldWidth is the width a [TextFieldView] gives itself when nothing
// bounds it. It is exported here rather than as API for the one test of the
// unbounded branch; a caller who wants another width writes Frame or Flex.
const DefaultFieldWidth = defaultFieldWidth

// LineCountForTest returns the number of visual lines the given text produces.
func LineCountForTest(f Font, s string, size, maxWidth float32) int {
	return text.Default().Layout(text.Request{Text: s, Font: f.f, Size: size, MaxWidth: maxWidth}).LineCount()
}

// LineHeightForTest returns the distance between two baselines.
func LineHeightForTest(f Font, size float32) float32 {
	return f.f.Metrics(size).LineHeight
}

// SetGalleryGenerationCheck turns the recycling guard of [Gallery] on and off
// and returns the previous setting.
//
// It exists for one test and has no exported counterpart. The project plan,
// section 13, requires that a recycled tile never shows the previous item's
// picture, and the only way to show that the generation comparison is what
// delivers that — rather than some accident of ordering — is to switch it off
// and watch the wrong picture appear. A guard that cannot be made to fail is
// indistinguishable from a comment.
func SetGalleryGenerationCheck(on bool) bool {
	prev := galleryCheckGeneration
	galleryCheckGeneration = on
	return prev
}

// ResetImageService detaches the pipeline and forgets every texture and
// request, so that one test cannot see another's pictures. The service is
// process wide, exactly like the default font; see [SetImagePipeline].
func ResetImageService() { images.setPipeline(nil) }

// ImageServiceUploads is the number of textures the shared service has
// uploaded. It is the number the "ui.Image and the gallery share resources"
// assertion is made on.
func ImageServiceUploads() uint64 { return images.uploads }

// TextureKeysForTest returns the number of resident texture keys and how many
// distinct revisions the service holds for one picture.
//
// It is how the "one set of pixels, one texture" property of WU-R is asserted:
// two entries for one picture and one rung means two keys were built from two
// different revisions, which is the defect and not merely a cache miss.
func TextureKeysForTest(id asset.ID, rung int) int {
	n := 0
	for k := range images.tex {
		if k.id == id && k.rung == rung {
			n++
		}
	}
	return n
}

// WarmKeyRevisionForTest asks the service for the key a warm lookup produces,
// which is the one the gallery writes at bind time.
func WarmKeyRevisionForTest(id asset.ID, size int) (string, bool) {
	if images.pipe == nil {
		return "", false
	}
	t, ok := images.pipe.Lookup(id, size)
	if !ok {
		return "", false
	}
	rev := t.Revision()
	t.Release()
	return rev, true
}
