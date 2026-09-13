// Package ebiten is gift's Ebitengine backend: the window, the frame loop and
// the GPU output.
//
// It is the only package in the module that imports Ebitengine; see the
// project plan, section 3.
//
//	app := gift.New(gift.Options{Root: root})
//	err := ebiten.Run(app, ebiten.Config{Title: "gift", Width: 1280, Height: 720})
//
// # Frame model
//
// [Run] wires [gift.App.Update] to Ebitengine's Update and [gift.App.Paint] to
// Ebitengine's Draw, and nothing crosses over. Several updates may run before
// a frame is drawn and under load none may, so build and layout live on one
// side of that line and the display list on the other. The whole visible list
// is redrawn every frame; there is no dirty rectangle path and none is
// planned. The project plan, section 6, explains why.
//
// # Drawing
//
// Every operation of the display list is a rounded rectangle, filled or
// stroked, so all of them are drawn by one shared Kage shader that evaluates a
// signed distance field, fed from vertex attributes. A frame is normally one
// draw call. Nothing allocates an image, a texture or a render target per
// widget, per operation or per frame, which is what the project plan, sections
// 8 and 11, require.
//
// Colours are premultiplied on both sides of the boundary — [render.Color] is
// premultiplied by construction and so is everything Ebitengine consumes — so
// the vertex path copies the four floats through without converting anything.
//
// # Clipping and transforms
//
// Clips are axis aligned rectangles and are applied to the geometry, not to a
// scissor or a stencil: the quad of an operation is clipped before it is
// emitted, which is exact for rectangles and costs no state changes. An
// operation whose clip is empty produces no geometry at all.
//
// Transforms are resolved through [render.List.Xform]. gift does not push a
// transform yet, so index zero, the identity, is the only one that occurs in
// practice: the general path is covered by unit tests but has never been
// exercised end to end against a GPU. It is implemented anyway, because
// scrolling will need it.
//
// # Measurement
//
// [FrameTimer] records the three timings the project plan, sections 11 and 13,
// insist on keeping apart: CPU time in the update callback, CPU time in the
// draw callback and the wall clock distance between two draw callbacks. None
// of them is a GPU time.
//
// Two things are excluded from the interval series and reported separately,
// because including them answers a different question than the one the plan
// asks. The first [DefaultWarmupIntervals] intervals are warm-up: opening a
// window costs an interval of well over a hundred milliseconds, and the
// default history is large enough that a sixty second run can never evict it.
// Intervals at or below [DefaultMinInterval] are two draw callbacks back to
// back rather than two presentations. An interval counts as missed when it
// exceeds the nominal interval plus [DefaultIntervalTolerance]; the plan,
// section 13, binds that at 0.5 ms, giving 17.17 ms at sixty hertz, after a
// strict comparison reported half of a cleanly timed measurement as missed.
//
// # Testability
//
// Everything except the draw call itself runs without a graphics context:
// compiling the shader, translating operations, resolving clips and
// transforms, converting colours and recording frame times. The project plan,
// section 12, criterion 4, requires that the core's tests need no window, and
// the tests of this package need one only where a pixel has to be read back.
// Those are behind the build tag giftgpu, so `go test ./...` passes headless.
package ebiten
