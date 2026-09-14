package ui

import (
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/text"
)

// This file exposes the text measurement internals to the external test
// package. It is a _test.go file, so nothing here is part of the API; it
// exists so that a test can compare what layout reserved against what
// internal/text measured, which is the one property of the text stack that has
// to be checked from outside.

// MeasureForTest returns the extent internal/text computes for the given text.
func MeasureForTest(f Font, s string, size, maxWidth float32) geom.Size {
	return text.Default().Measure(text.Request{Text: s, Font: f.f, Size: size, MaxWidth: maxWidth})
}

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
