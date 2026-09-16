// Package auto is an opt-in, out-of-band automation interface for a running
// gift application: HTTP on loopback, in the real process, driving the real
// window.
//
// # It is a debugging tool and not a product feature
//
// It opens an unauthenticated HTTP server that can synthesise any input the
// application can receive and can read back every pixel it draws. Nothing
// about that belongs in a shipped kiosk, so it cannot get there: every line of
// this package is behind the build tag giftauto, and without that tag the
// package is this file — no imports, no symbols, no goroutine, no server.
// Linking it into a production binary therefore takes a deliberate
// `-tags giftauto` on the build command line and not a forgotten import.
//
//	//go:build giftauto
//
//	package main
//
//	import _ "github.com/torbenschinke/gift/auto"
//
// Build the application with the tag, run it, and drive it:
//
//	go build -tags giftauto ./cmd/example-kitchensink
//	./example-kitchensink &
//	curl -s localhost:7391/tree | jq .
//	curl -s -XPOST localhost:7391/input -d '{"steps":[{"op":"tap","x":100,"y":200}]}'
//	curl -s localhost:7391/screenshot -o shot.png
//
// # Why this exists rather than a second, testable copy of the application
//
// A golden image of a demo that was restructured so a test could import it is
// a picture of a reconstruction: the harness clears its own background, mounts
// its own root and renders offscreen, while the program that ships has a
// window, a real frame loop and a real framebuffer. Twelve such goldens once
// passed against a deliberately broken theme while the window was visibly
// wrong. This package takes the other route — it instruments the application
// that ships, drives no reconstruction, and its screenshot is a read-back of
// the very image the window presents, taken inside Draw.
//
// # What it guarantees
//
// Every request marshals its work onto the UI goroutine through
// [gift.App.Post] and waits for that work to have happened before it answers,
// so a shell script driving it is sequential rather than racy. A screenshot
// answers with a frame drawn after the input that preceded it was processed.
// See the giftauto-tagged files of this package for the implementation and its
// tests.
//
// # Coordinates and units
//
// Every coordinate in and out of this interface is in gift's logical pixels,
// which is the space [gift.App.NodeDeviceBounds] and [gift.App.PointerDown]
// use. A screenshot is in physical pixels, that is logical times the device
// density, and every screenshot answer carries that density in a header so the
// two can be mapped onto each other.
package auto
