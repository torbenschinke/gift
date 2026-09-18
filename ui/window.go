package ui

import (
	"github.com/worldiety/gift"
)

// Window wraps the root view of an application in the window background.
//
//	func screen(ctx *gift.Context) gift.View {
//		return ui.Window(ui.VStack(/* ... */))
//	}
//
//	app := gift.New(gift.Options{Root: screen})
//
// # Why an application has to say this at all
//
// Because gift draws exactly what the view tree says and nothing else. There
// is one display list, no per-widget render target and no window chrome; the
// project plan, section 6, adds that Ebitengine clears the screen every frame,
// and the colour it clears to is transparent black. A tree of cards on nothing
// therefore does not produce "cards on the default background" — it produces
// cards on whatever the framebuffer holds, which on a desktop is a composited
// hole and on a Raspberry Pi kiosk is the console that was there before.
//
// That was a real defect and it is the reason this function exists. Four demo
// screens shipped with thirty to forty per cent of their pixels at
// {0, 0, 0, 0}, and every golden image of them looked correct, because the
// test harness cleared its own canvas to a colour of its own before rendering.
// A picture of a background nobody paints is the one defect a golden cannot
// report.
//
// # Why it is here and not in gift
//
// [gift.App] has no notion of an appearance. It owns a tree, a layout and a
// display list, and a colour it painted on its own behalf would be a colour no
// view asked for — invisible to the gifttest harness, to cmd/gift-shot and to
// every assertion in this module, all of which look at the display list. A
// background that is a view is a background the tests can see, which is
// exactly the property that was missing.
//
// So the obligation is the application's, and this function is the whole of
// it. Harness.AssertOpaque in gifttest is the gate that says it was met.
//
// # What it draws
//
// A greedy [Box] filled with [ColorBackground], with content on top of it in
// the order given, in a [ZStack]. The Box is the documented idiom of
// [BoxView]: a ZStack hands every child loose but bounded constraints, so an
// unframed Box takes the whole viewport whatever the content does. Painting
// the ZStack's own background instead would only cover the union of the
// children, which is the bug again for any tree that does not fill the window.
//
// The result is an ordinary [Overlay], so the alignment and the modifiers of
// that type are available:
//
//	return ui.Window(body, ui.OnScreenKeyboard()).Align(geom.Bottom)
//
// # The two-level hierarchy this makes real
//
// [ColorBackground] is the page and [ColorSurface] is what is raised on it —
// a [Card], a [Modal] sheet, the bar of a [TabBar] or a [NavigationStack].
// Until something painted the page, every one of those sat on nothing and the
// distinction between the two roles existed only in the palette: tinting
// [ColorBackground] magenta at runtime changed zero pixels on all four screens
// of cmd/example-kitchensink.
func Window(content ...gift.View) Overlay {
	// A fresh slice, because the ownership rule of the project plan,
	// section 4, gives a container its children slice for keeps and the
	// caller's variadic array must not become it.
	children := make([]gift.View, 0, len(content)+1)
	children = append(children, Box().Background(ColorBackground))
	children = append(children, content...)
	return ZStack(children...)
}
