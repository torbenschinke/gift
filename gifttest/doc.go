// Package gifttest drives a gift application from a Go test.
//
// It is the tool an *application* developer uses: mount a view, find a node by
// what it says, click it, assert what happened. The style is deliberately the
// one Playwright made familiar, because the lesson it encodes is the one that
// matters here — a UI test that names coordinates breaks every time the design
// moves, and a UI test that names *what the user sees* does not.
//
//	h := gifttest.New(t, gifttest.Options{Root: counter})
//	h.Find(gifttest.ByText("+")).Click()
//	h.Find(gifttest.ByKey("count")).AssertText("1")
//
// Nothing above mentions a pixel, a frame or a timer.
//
// # Actions verify where they landed
//
// An action on a [Node] computes a point, hit tests it, and fails unless the
// hit test reaches the node the test named. That is not a nicety. Without it a
// button covered by a transparent overlay, by a later sibling of a ZStack or
// by a full bleed [ui.Box] activated the *covering* control and the harness
// said nothing at all: the test failed later, on an assertion about a counter,
// and blamed the application. A test tool that can pass while the interface is
// broken is the worst thing a test tool can be, and this is the check that
// closes it. The failure names the intended node, the node actually reached
// and the tree; see [Node.Click].
//
// Clicking through something on purpose is a separate, spelled out thing. The
// coordinate taking verbs — [Harness.ClickAt], [Harness.PressAt],
// [Harness.ReleaseAt], [Harness.MoveTo] — take a point and dispatch it, with
// no intended node and therefore no check, and [Harness.At] answers "what is
// really here" without dispatching anything. Use them when the coordinate,
// the overlap or the clip boundary *is* the subject.
//
// # Two layers
//
// The default layer is headless: build, layout, input and paint all run on the
// display list, with no window, no GPU and no font rasteriser beyond the CPU
// one gift already uses. It works under a plain `go test ./...` and that is
// where every assertion in this package lives except one.
//
// The exception is [Harness.AssertGolden], which compares real pixels and
// therefore needs a graphics context. It is compiled against the build tag
// giftgpu. Without the tag the method still exists and still compiles — a test
// file is not split in two — but it skips, loudly, and it can be made to fail
// instead; see [Harness.AssertGolden] and [Main].
//
// # Do not call t.Parallel
//
// A [Harness] owns a [gift.App], and a gift.App belongs to one goroutine. That
// much is documented on gift.App and is enforced under giftdebug. What is not
// obvious, and is the reason this paragraph exists, is that the text stack is
// *process* wide and not per App: internal/text keeps one shaping cache for
// the whole process — the project plan, section 3, forbids ui and
// backend/ebiten from importing each other, so the cache neither of them may
// own lives in a package variable — and [ui.SetDefaultFont] installs the
// default font in another one.
//
// So two parallel tests that both lay out text share a mutable cache and race,
// and two that both call ui.SetDefaultFont race on the font. `go test -race`
// reports it, which is the right tool for it and the one the project plan,
// section 15, names; nothing here guards it, because a mutex on the
// measurement path would cost every single window application something to
// buy a test suite a property it does not need.
//
// The practical rule: do not call t.Parallel in a test that uses this package.
// Tests in different packages are separate processes and are unaffected, so
// `go test ./...` parallelises at the level that matters anyway.
//
// # Time
//
// The clock is injected. [Harness.Advance] moves it, which is how a long press
// is tested in microseconds and how an animation will be tested later. This
// package never sleeps and never reads the wall clock, so a test of a gesture
// with a 500 ms threshold costs nothing and cannot flake on a loaded machine.
//
// # Frames
//
// Every action settles the application before it returns: frames are pumped
// until a frame produces no build and no layout. A view that rebuilds itself
// forever is therefore caught by the harness with a diagnosis instead of
// hanging the test binary; see [Harness.Settle].
//
// # What it cannot do yet
//
//   - Type text. There is no text input view and no character event in gift;
//     [Harness.TypeText] says so at the call site, with what it would take,
//     rather than faking an event the runtime never delivers.
//   - Scroll to a node that is off screen. gift has no scrollable view before
//     step 3 of the project plan, so every node a test can find is laid out
//     and hit testable. When scrolling arrives, the actions will need to bring
//     a node into view first, and [Node.Center] will have to be the centre of
//     the *visible* part of a node rather than of its bounds.
//   - Assert on anything the backend does with the display list, short of a
//     golden image: batching, atlas pressure and draw call counts live in
//     backend/ebiten's own stats.
//   - Drive more than one finger. Multitouch is limited to recognising and
//     discarding extra fingers by the project plan, section 7, so the harness
//     has one touch pointer.
package gifttest
