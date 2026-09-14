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
