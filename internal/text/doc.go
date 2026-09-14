// Package text contains the renderer neutral text stack of gift: font loading,
// shaping, measurement and line breaking.
//
// # Scope
//
// The package turns a string, a font, a size and a width limit into a
// [Paragraph]: lines with integer baselines and runs of positioned glyph ids.
// It owns no GPU object and knows nothing about a glyph atlas; the project
// plan, section 3, forbids both, and the same rule forbids importing the
// module root. Only geom is used.
//
// It does produce one kind of pixels, and the boundary is worth being precise
// about. [Rasterizer] turns a glyph outline into an eight bit coverage
// [GlyphMask] and nothing else: no colour, no texture, no packing, no budget.
// It exists because rasterising needs the typesetting face, and exporting that
// face so that the backend could rasterise for itself would put a typesetting
// type on a package boundary and hand every caller a way to shape behind this
// package's back. A narrow accessor is the smaller concession. Everything
// about the *atlas* — packing, pages, eviction, upload — stays in the backend.
//
// What is in scope, per the project plan, section 7:
//
//   - horizontal left to right shaping,
//   - kerning and ligatures exactly as the font specifies them,
//   - line breaking at word boundaries following UAX 14 in the form
//     github.com/go-text/typesetting provides,
//   - integer baseline and integer glyph positions, no subpixel positioning.
//
// What is deliberately not in scope, and what this package therefore does not
// pretend to do:
//
//   - Bidi and RTL. Text is laid out in logical order, always left to right.
//     Hebrew or Arabic input is shaped, but it is *not* reordered, so it comes
//     out visually reversed. That is wrong output, not supported output; see
//     the pinning test in the package test suite.
//   - Vertical writing.
//   - Automatic font fallback across several fonts. A [Request] names exactly
//     one [Font], and a rune the font does not cover becomes the notdef glyph.
//     Fallback is the most likely first extension after the MVP, which is why
//     a [Line] is a list of [Run] values that each carry their own font even
//     though today there is never more than one. The shape of the output does
//     not have to change when a fallback chain arrives; only the code that
//     fills it does.
//   - Hinting variants, text editing, IME.
//
// # One shaping result, used twice
//
// The single most common class of text bug is a measurement path that computes
// a width differently from the drawing path. This package makes that
// impossible by construction rather than by convention: [Shaper.Layout] is the
// only function in the package that shapes anything, [Paragraph.Size] is
// derived from the glyphs of the paragraph it belongs to, and
// [Shaper.Measure] is literally `s.Layout(req).Size`. There is no entry point
// that returns a width without also returning the glyphs it was computed from,
// and no entry point that returns glyphs without a size. Layout and the
// backend call the same function with the same [Request] and get the same
// pointer out of the same cache entry.
//
// # Allocation boundary
//
// Text measurement happens during layout, and layout is inside the zero
// allocation frame path of the project plan, section 11. The boundary is drawn
// explicitly here:
//
//   - A cache hit allocates nothing at all. This is asserted by a benchmark in
//     the test suite.
//   - A cache miss allocates. Shaping a new string is real work with real
//     memory; pretending otherwise would mean either a fixed size arena that
//     silently fails on long text, or unsafe tricks. This is the same boundary
//     the plan draws for build: invalidation driven, not allocation free. The
//     cold cost is measured and reported, not asserted.
//
// A caller that needs a guaranteed hit warms the cache outside the frame path
// with the same [Request] it will use inside it.
//
// # No logging
//
// Nothing in this package logs, not even behind a level check, because all of
// it can run during layout; see the project plan, section 15. Everything
// observable is a counter in [Stats].
//
// # Concurrency
//
// A [Shaper] and every [Font] it has been used with belong to one goroutine,
// normally the UI executor. [Default] returns the process wide shaper that ui
// measures through and that the backend reads counters from; it is bound to
// the UI executor like any other, and two Apps measuring text on two
// goroutines is a data race. [Lookup] is the exception: it takes a read lock,
// because the backend resolves a [FontID] from a display list on its own
// schedule. A Font caches rune to glyph lookups and glyph
// extents internally, so even two Shapers using the same Font concurrently is
// a data race. Load a Font per Shaper if you need more than one.
//
// # Borrowed results
//
// [Shaper.Layout] returns a pointer into the shaping cache. It is borrowed,
// never owned, and it stays valid until the entry is evicted, which can happen
// at the next [Shaper.Layout] or [Shaper.Tick] call. Read it, copy what you
// need out of it, do not keep it across frames. This is the same lifetime rule
// as for a borrowed render.List.
package text
