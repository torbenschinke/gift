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
// A shape operation — a rectangle, a rounded rectangle or a stroke — is drawn
// by one shared Kage shader that evaluates a signed distance field fed from
// vertex attributes, so a frame of nothing but shapes is one draw call. A text
// operation is a run of textured quads sampling a [GlyphAtlas] page, which is
// a different material, so "one draw call per frame" becomes one per run of
// same-material operations.
//
// Those runs are *not* reordered to merge them. Display list order is drawing
// order and a batch ends wherever the material changes, because the project
// plan, section 11, forbids globally reordering transparent content to reduce
// draw calls — a label sorted into a late text pass would slide in front of
// panels declared after it. See [Renderer.material] and [RendererStats.DrawCalls].
//
// An image operation is a textured quad too, sampling one texture of the
// [TextureCache], and it is therefore a third material with the same rule: a
// batch ends where the texture changes, so a gallery of sixty visible
// thumbnails issues sixty draw calls from this package. That number is
// reported rather than engineered away, because engineering it away would mean
// an atlas of our own and the project plan, section 11, forbids building one
// ahead of a measurement — and because Ebitengine already merges consecutive
// draw commands whose backend source images coincide, which for thumbnails on
// its automatic atlas they usually do. See [TextureCache] for the evidence and
// [RendererStats.ImageBatches] for the number.
//
// Uploads are admitted per *drawn* frame and not per update, which is the
// distinction the project plan, section 11, draws and the reason the budget is
// reset in [Renderer.BeginFrame]: several Ebitengine updates may precede one
// frame. It matters because the OpenGL driver path calls glFinish before
// writing pixels when a draw has already been issued — WritePixels in
// internal/graphicsdriver/opengl/image.go of the pinned module does so
// explicitly, "wait for completion of the pending draw commands" — so an
// unbudgeted frame stalls on the GPU however much decoding happened in the
// background. gift additionally does every upload of a frame *before* every
// draw of it, because painters run inside App.Paint and draw calls are issued
// afterwards in Submit, so a frame costs at most one such wait.
//
// A shadow is a shape operation too, and that is a deliberate departure from
// the project plan, section 8, which proposes "wiederverwendbare, gecachte
// Formmaske; Blur nur bei Form-/Parameterwechsel" for it. gift evaluates the
// Gaussian analytically in the same shape shader instead, from the same signed
// distance field the rounded rectangle already uses, with the standard
// deviation carried in the sign bit of the stroke slot.
//
// The reasons are in the plan rather than against it. A cached mask is a
// texture per distinct shape-and-blur pair, and section 11 warns that the
// OpenGL upload path may call glFinish before a pixel update — so the cache
// would stall the frame that missed, which is every frame a panel resizes or a
// list scrolls a new card into view. It would also be a third material, and
// since section 11 forbids reordering transparent content to merge materials,
// every shadowed panel would have cost its own draw call. And section 8 itself
// prescribes "analytische Geometrie im gemeinsamen Shape-Shader" one row above,
// for the border and the radius, on exactly this reasoning.
//
// The price is stated rather than hidden, and it is larger than this
// paragraph used to claim. The analytic form is exact along a straight edge —
// measured at 0.002 of the alpha down the middle of one — and exact at the
// corner of a sharp rectangle, because that corner separates into a product of
// two one dimensional answers and the shader now uses it. It is *not* exact at
// the corner of a rounded box, where the true convolution is neither. Measured
// against a numerically convolved reference, the corner error reaches 0.13 of
// the shadow alpha and is largest when sigma and the radius are comparable.
// This comment previously said "about two per cent", which was wrong by an
// order of magnitude and had survived because the one pixel test that could
// have caught it deliberately sampled away from the corners; see
// TestShadowCornerMatchesTheConvolution and the measured table on
// shadowCoverage in shape.kage.
//
// There is no cache, so there is nothing to hit, miss, upload or evict, and
// [RendererStats] has no shadow cache counters for that reason. What a shadow
// costs is fill rate: its quad is the shape grown by [render.ShadowSigmas]
// standard deviations on every side, so a 100x40 button with Blur 16 shades a
// 148x88 quad, 3.3 times its own area. See [RendererStats.ShadowOps],
// shape.kage and [render.Shadow].
//
// Nothing allocates an image, a texture or a render target per widget, per
// operation or per frame, which is what the project plan, sections 8 and 11,
// require. The glyph atlas is a bounded, evicting pool of pages; see
// [GlyphAtlas].
//
// Glyph coverage is a single channel mask and the colour comes from the
// operation, so the atlas never holds a colour and the same glyph serves every
// colour it is ever drawn in. Glyph positions are whole pixels and the atlas
// key carries no subpixel phase; the project plan, section 7, rules subpixel
// positioning out.
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
// Transforms are resolved through [render.List.Xform]. Since WU-L a scroll
// container pushes one: a pure translation, which takes the axis aligned fast
// path in [Renderer.appendAxisAligned] and in [Renderer.appendTexturedQuad] and
// is exercised end to end against a GPU by gifttest's scroll pixel test. Since
// WU-W there is a second producer and it is at the root: gift pushes the
// device density of the project plan, section 18, so on a 2x display every
// operation arrives under a uniform scale of two. The general polygon path
// still has no producer in gift and is covered by unit tests only.
//
// One consequence is worth stating rather than implying, and it is what WU-W
// changed. Shapes are shaded from a distance field that this package rescales
// into device pixels, so a scaling transform was always correct for them.
// *Glyphs* were not: the atlas held a bitmap rasterised at the glyph's nominal
// size, and a scale would have stretched it under a nearest filter, giving
// text the wrong weight. The answer was never to abandon the nearest filter —
// it is to make the mapping from atlas pixel to screen pixel one to one by
// *construction* rather than by the accident that every transform happened to
// be a translation. So the scale of the transform enters the atlas key and the
// rasteriser, [Renderer.appendGlyphs] transforms only the glyph origin, and
// the mask is drawn at its own size in device pixels at any density. See
// [GlyphAtlas.Lookup].
//
// # Glass, and the one place Ebitengine forced a departure from the plan
//
// The material of the project plan, section 8, is here: [render.OpMaterial] is
// a region whose appearance depends on what was drawn under it, and this
// package turns it into passes. [Reduced] is a region copy and one composite
// pass; [Full] adds a dual-Kawase down and up chain, confined to the region.
// [GlassPolicy] chooses between them from frame intervals and material area,
// because Ebitengine exposes no capability to ask.
//
// Section 8 gives two reasons why one shader pass cannot do it, and both hold
// in the pinned source. There is no framebuffer fetch and no programmable
// blending in Kage. And a wide convolution is unaffordable at 1080p, which
// downsampling defuses.
//
// What section 8 could not have known is that the *source* of its
// "Regionskopie" has to be manufactured. Ebitengine's internal/atlas panics
// with "atlas: a screen image cannot be created as a source" the moment the
// screen image is used as a draw source, so the pixels under a material cannot
// be copied off the screen at all. gift therefore renders a frame that
// contains a material into one screen sized offscreen and composites that onto
// the screen at the end. The costs — one screen sized target, one clear and
// one blit, all of them only while a material is *visible*, not merely while
// one is in the display list — are stated on [Renderer.ensureScene] rather than
// buried. The region copy is still a region copy and the blur chain is still
// confined to the region.
//
// The final blit is source over and not a replace, which is a semantic point
// and not a performance one: Porter-Duff over is associative, so compositing
// the offscreen onto the target gives exactly the pixels drawing straight onto
// the target would have given. A copy made the same display list mean two
// different things depending on whether a material happened to be in it, and
// erased whatever the caller had already put in the target. See
// [Renderer.SetTarget].
//
// Two more numbers worth knowing before using it. A material region is a
// *batching barrier*: everything before it has to reach the target before its
// backdrop can be copied, so a frame that was one shape draw call becomes five
// with a Reduced panel in the middle of it and eleven with a Full one, whatever
// the rest of the scene contains. And section 8's claim that the chain costs
// "unter 1,5x der Flaeche der Materialregion" is not reachable by a chain that
// ends at the region's resolution; see TestBlurChainStaysInsideTheRegion, which
// works the arithmetic out and asserts the number this implementation actually
// achieves.
//
// The intermediate targets live in [TargetPool]: bucketed, reused across
// frames and across panels, bounded by bytes, and released with an explicit
// Deallocate exactly as [TextureCache] releases a picture. There is no target
// per widget, and [Renderer.EndFrame] panics rather than continuing if a pass
// chain leaked a lease.
//
// # Measurement
//
// Measurement lives in the metrics package and not here. This package is the
// Ebitengine adapter; 429 lines of ring buffers and percentiles were not that,
// and the project plan, section 3, gives this package a different job.
//
// What remains here is the wiring. [Run] starts a [metrics.Recorder], times
// the two callbacks, records the interval between drawn frames and hands the
// renderer counters, the glyph atlas counters and the shaping cache counters
// of internal/text over as [metrics.RendererStats] and [metrics.ShaperStats],
// together with the glass and render target counters. Every one of those steps
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
