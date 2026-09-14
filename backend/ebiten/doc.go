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
// Measurement lives in the metrics package and not here. This package is the
// Ebitengine adapter; 429 lines of ring buffers and percentiles were not that,
// and the project plan, section 3, gives this package a different job.
//
// What remains here is the wiring. [Run] starts a [metrics.Recorder], times
// the two callbacks, records the interval between drawn frames and hands the
// renderer counters over as [metrics.RendererStats]. Every one of those steps
// is behind metrics.Enabled, which is a compile time constant false without
// the giftmetrics build tag, so an ordinary build does not even call
// time.Now. An application therefore gets a measurement by rebuilding with
// -tags giftmetrics and setting GIFT_METRICS=1, and writes no code for it.
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
