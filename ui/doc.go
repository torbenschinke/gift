// Package ui is the public view layer of gift.
//
// # Shape of the API
//
// Views are concrete value types and modifiers are fluent methods that return
// the same concrete type, following variant A of the project plan, section 4.
// There is deliberately no shared modifier interface: a method that exists on
// a widget which then ignores it is a lie the compiler cannot catch.
//
// There is exactly one exception, and it is named here so that it is not
// discovered by surprise: a [ZStack] honours its own Flex, because its parent
// stack is the one dividing space, but ignores the Flex of its children. A Z
// stack has no main axis and therefore no remainder to divide. Its children
// are measured with the full bounded constraints of the box instead, which is
// the behaviour Flex would have been asked for. See [Overlay.Flex].
//
//	ui.VStack(
//	    ui.Box().Frame(80, 24).Background(ui.RGB(200, 40, 40)),
//	    ui.Spacer(),
//	    ui.Box().Frame(80, 24),
//	).Gap(8).Padding(16).CornerRadius(12)
//
// # Modifiers are fields, not wrappers
//
// A modifier sets a named field on the view value. It does not wrap the view
// in another view, and it therefore does not create a node. Setting the same
// modifier twice replaces the value, so Padding(8).Padding(16) means sixteen
// points of padding and produces exactly one node. The drawing order is fixed
// per node — shadow, background, content, border — and is not the position
// dependent modifier semantics of SwiftUI; see the project plan, section 8,
// which says so explicitly.
//
// Shadow is part of step 2 and is not implemented here.
//
// # Text
//
// [Text] measures itself by shaping its string against the width its
// constraints allow, and the glyphs it draws are that very shaping result:
// internal/text has one entry point and it returns a size together with the
// glyphs the size was computed from, so a label cannot be measured differently
// from how it is drawn.
//
// gift ships no font, and there is no built-in fallback. An application loads
// one with [LoadFont] and installs it with [SetDefaultFont], or names one per
// view with [TextView.Font]. Building a [Text] with neither panics with an
// explanation, because the alternative — measuring it as empty — is a blank
// area with no diagnosis.
//
// # Input
//
// [Button] is the only interactive view so far. It is the full control the
// project plan, section 7, describes and not a click handler: a press captures
// the pointer, so a release somewhere else does not activate; a tap works
// without ever producing a hover; tab reaches it and space or enter fires it;
// and [ButtonView.Disabled] takes it out of all of that at once.
//
// Hover, pressed and disabled are presentation state living in the retained
// node, so they repaint and never rebuild. The consequence for the API is that
// the looks are declared up front, with [ButtonView.HoverStyle],
// [ButtonView.PressedStyle] and [ButtonView.DisabledStyle], rather than chosen
// per frame by the application.
//
// Only a view that declares an interactor is a hit target. A plain [VStack] is
// transparent to input: a click in its padding or in the gap between two
// children hits whatever is behind it, not the stack.
//
// # Content that does not fit
//
// Nothing is clipped unless Clip(true) was asked for, and no container starves
// a child to make its siblings fit. A stack measures an inflexible child with
// an unbounded main axis, the children keep their honest sizes and positions,
// and the amount by which the content exceeded the container is counted in
// [gift.Diagnostics]: OverflowNodes and OverflowExtent. Build with
// -tags giftdebug to have the offending node named. This is the overflow model
// of the project plan, section 7.
//
// # Scrolling and the gallery
//
// [ScrollView] is a stack in a window that is smaller than it. The offset is
// presentation state in the retained node, so scrolling rebuilds nothing and
// measures nothing: gift translates the children with a matrix.
//
// [ImageGallery] is the other kind. It is a *virtualising* container over an
// [github.com/torbenschinke/gift/asset.Collection]: only the entries inside
// the viewport have nodes, and which entry a node stands for is decided during
// layout rather than during build. A hundred thousand entries and a hundred
// entries therefore produce the same handful of nodes, and scrolling still
// never builds — it relayouts one node. The retained half is a [Gallery] the
// application creates once and holds; see the documentation of that type for
// why it is not the view.
//
// # Ownership of children
//
// ui.VStack(a, b, c) creates a fresh slice that gift keeps. ui.VStack(items...)
// hands over the caller's slice without copying it, and the caller must not
// touch it afterwards; see the project plan, section 4. A violation is
// diagnosed in builds with the giftdebug tag.
//
// # Allocation
//
// Building views allocates, by design and by the project plan, section 11.
// Layout and paint do not: the per child scratch buffers of a container live
// in the layouter that the retained node holds and are reused for as long as
// the node is not rebuilt.
package ui
