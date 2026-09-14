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
