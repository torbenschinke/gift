// Package ui is the public view layer of gift.
//
// # Shape of the API
//
// Views are concrete value types and modifiers are fluent methods that return
// the same concrete type, following variant A of the project plan, section 4.
// There is deliberately no shared modifier interface: a method that exists on
// a widget which then ignores it is a lie the compiler cannot catch.
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
