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
// Shadow, background, border, corner radius, clipping and the glass material
// are all implemented. [BoxView.Shadow] and its equivalents on every other
// styled view take a [Shadow]; it extends the paint bounds and neither the
// layout size nor the hit area, and a parent clip cuts it like anything else.
//
// # Text
//
// [Text] measures itself by shaping its string against the width its
// constraints allow, and the glyphs it draws are that very shaping result:
// internal/text has one entry point and it returns a size together with the
// glyphs the size was computed from, so a label cannot be measured differently
// from how it is drawn.
//
// gift links no font unless the application asks for one, and there is no
// built-in fallback. An application either imports one of the opt-in packages
// font/inter and font/ibmplexmono, which register their faces with
// [RegisterFont], or loads its own with [LoadFont]; either way it installs one
// with [SetDefaultFont] or names one per view with [TextView.Font]. Building a
// [Text] with neither panics with an explanation, because the alternative —
// measuring it as empty — is a blank area with no diagnosis.
//
// [ResolveFont] picks a registered face by family, weight and style. It is not
// a fallback chain: a glyph the chosen face lacks is not looked for in a
// second one, and the project plan, section 14, keeps it that way.
//
// # The window background is the application's
//
// gift paints nothing an application did not ask for, and Ebitengine hands a
// real window a transparent black screen at the top of every frame; see the
// project plan, section 6. So the root view of an application paints the page
// itself, and [Window] is the whole of it:
//
//	func screen(ctx *gift.Context) gift.View { return ui.Window(content) }
//
// Forgetting it is silent. Nothing panics, nothing logs, every structural
// assertion passes, and the window has holes in it — which on a kiosk is
// whatever the framebuffer held before. gifttest's Harness.AssertOpaque is
// the gate; see [Window] for how this module found out the expensive way.
//
// # Colour
//
// Colours are either literal — [RGB] and [RGBA] — or semantic. A semantic
// colour names the *role* a colour plays: [ColorLabel], [ColorSurface],
// [ColorAccent] and the rest resolve against the process wide theme, and
// [SetTheme] switches the whole application between [LightTheme] and
// [DarkTheme] at runtime. The project plan, section 20, fixes both the feature
// and its limits.
//
// The resolution happens in Build, once per view. A retained node holds the
// literal colour it was built with, which is what keeps theme lookups out of
// the frame path and the frame path at zero allocations — and it is also why a
// theme change has to rebuild, which is what [SetTheme] does through
// [gift.App.Invalidate].
//
// There is deliberately no environment and no inherited scope. Section 20 rules
// that out by name, on the grounds section 4 gives for styling in general: a
// style is a property of the concrete construction site, not something applied
// to an arbitrary [gift.View]. A semantic colour is therefore an ordinary value
// a call site writes down; what is global is the table it is looked up in, not
// a value handed down the tree.
//
// In the style structs of this package — [ButtonStyle] today — the zero
// [Color] means "not set, let the theme decide" rather than "transparent", so
// that a theme can sit underneath a partially specified style. A caller who
// wants nothing drawn writes [ColorClear].
//
// # An unresolved semantic colour is invisible, not wrong
//
// This is the one property of the encoding worth knowing before writing a
// widget of your own. A semantic colour is a [Color] whose red channel is -1
// and whose *alpha is zero* — see [IsSemantic] — so a colour that never
// reached [ResolveColor] is fully transparent. Every painter worth the name
// skips fully transparent work, so the operation is not emitted at all: the
// widget lays out, measures and hit tests exactly as it should, and draws
// nothing. There is no wrong pixel to notice and, in a release build, no
// diagnosis.
//
// Consequences, in order of how likely you are to need them:
//
//   - Resolve during Build and store the literal colour in your node, which is
//     what every view here does and what keeps theme lookups out of the frame
//     path.
//   - If you check [IsSemantic] yourself, check it where you decide whether to
//     draw, not where you append the operation. By the latter point the
//     mistake has already become an absence.
//   - Under the giftdebug tag this package asserts in front of each of its own
//     visibility gates, and [render.List.Add] and [render.List.AddMaterial]
//     reject any impossible premultiplied colour as a backstop.
//
// # Removed
//
// DefaultForeground, a package variable that set the colour of every [Text]
// without one, is gone. It was process wide, mutable and able to recolour
// exactly one thing. The replacement is [ColorLabel] and [SetTheme], which
// move the text and the controls together and do so for both appearances; see
// [TextView] for the longer version.
//
// # Input
//
// [Button], [TextField] and [Gallery] are the interactive views: the three
// that declare themselves focusable and carry a keyboard handler. Button is
// the full control the project plan, section 7, describes and not a click
// handler: a press captures the pointer, so a release elsewhere does not
// activate; a tap works
// without ever producing a hover; tab reaches it and space or enter fires it;
// and [ButtonView.Disabled] takes it out of all of that at once.
//
// Hover, pressed and disabled are presentation state living in the retained
// node, so they repaint and never rebuild. The consequence for the API is that
// the looks are declared up front, with [ButtonView.HoverStyle],
// [ButtonView.PressedStyle] and [ButtonView.DisabledStyle], rather than chosen
// per frame by the application.
//
// [TextField] is the single line text field of the project plan, section 19.
// It edits a [TextEditor], which the application holds — that is where the
// text, the caret and the selection live, and it is what makes them survive
// the rebuild that every keystroke causes. Click to place the caret, drag to
// select, double click for a word; the arrows with shift and with
// [gift.WordModifier]; backspace and delete with gift's own key repeat; and
// select all, copy, cut and paste on [gift.ShortcutModifier].
//
// A drag that leaves the press more vertically than horizontally is not a
// selection but a scroll of whatever container the field sits in, so a form
// of text fields stays scrollable by a finger that lands on one. The cost, and
// it is deliberate: a caret drag that starts with an upward stroke scrolls
// instead of selecting. See [TextFieldView].
//
// [OnScreenKeyboard] is the other half of section 19: a German on-screen
// keyboard with umlauts and ß, drawn by gift because Ebitengine can raise no
// native one and the kiosk has none to raise. It appears when a field takes
// the focus and only with the kiosk switch on — an explicit
// [SetOnScreenKeyboard] and emphatically not the pointer kind, which on the
// target hardware reports a finger as a mouse and would therefore never fire.
// A drawn key injects through [gift.App.TypeRune] and [gift.App.KeyDown], so
// what reaches a widget is the ordinary event and no widget has a second code
// path for it. See [KeyboardView] for what it costs and for what its keyboard
// avoidance cannot do.
//
// Copy, cut and paste go through [Clipboard], a seam with an in-process
// default, so they work and are testable before the platform implementation of
// section 19 exists. Install one with [SetClipboard].
//
// Umlauts, accents, AltGr and dead keys work, because they are ordinary
// characters that arrive on [gift.EventRune] fully resolved. CJK composition,
// a preedit string and a candidate window do not, and stay excluded by the
// project plan, section 14. There is no undo and no multi line editing.
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
// # Icons
//
// [Icon] is a [Symbol] — pre-parsed outlines — rasterised on the CPU into a
// coverage mask and drawn as a tinted image. It deliberately does not go
// through the picture path above: the project plan, section 21, keeps icons
// out of the asset pipeline, because that pipeline is built for photographs
// with a size ladder, a disk cache and asynchronous resolution, and a
// placeholder flashing in place of a sixteen pixel symbol would be a visible
// defect. It also adds no operation kind and no shader: the mask is
// premultiplied white, the colour of a
// [github.com/torbenschinke/gift/render.Op] is a multiply, and white times a
// premultiplied foreground is that foreground at that coverage.
//
// The symbols live in packages above this one, like the typefaces:
//
//	import "github.com/torbenschinke/gift/icon/outline"
//
//	ui.Icon(outline.User).Size(16).Foreground(ui.ColorAccent)
//
// The default foreground is [ColorLabel], so an icon follows the theme without
// the call site saying anything. A mask is cached per symbol *and per device
// pixel size*, so an icon at 2x is rasterised at 2x rather than magnified, and
// a warmed frame that draws icons allocates nothing.
//
// # Pictures
//
// [Image] and [ImageGallery] draw through one application wide service, which
// an application installs once with [SetImagePipeline]. That is the project
// plan, section 10, taken literally — "ImageGallery ... verwendet denselben
// Bildservice wie ui.Image(source)" — and it means one fetch, one decode, one
// disk cache entry and one GPU texture for a picture that is on screen twice.
//
// The two halves of getting a picture on screen live in the two halves of a
// frame, and deliberately so. A *request* is scheduled during layout, because
// scheduling is CPU work and belongs to the update callback. The *upload* is
// admitted during paint, because the project plan, section 11, budgets uploads
// per drawn frame rather than per update and painting is the only thing that
// happens exactly once per drawn frame. A picture that is decoded but has not
// been admitted yet draws its placeholder, which is also what a fast jump
// draws and what a headless test — one with no backend and therefore no
// [github.com/torbenschinke/gift/render.Images] — draws for everything.
//
// Nothing in this package holds a pixel, a texture or a file handle. What a
// view holds is a key; what the backend holds is the texture; and what
// connects them is a handle with a generation, so that a texture the backend
// evicted becomes a placeholder and never somebody else's picture.
//
// # Navigation
//
// [TabBar] is the primary navigation of a kiosk, [NavigationStack] is push and
// pop within one of its tabs, and [Modal] presents an [Alert] — or any other
// view — over the lot with a scrim and a focus trap under it.
//
//	ui.Modal(
//		ui.TabBar(sel.Get(), sel.Set,
//			ui.Tab("Home", outline.Home, home).SelectedIcon(solid.Home),
//			ui.Tab("Settings", outline.Cog, settings),
//		),
//		alertOrNil,
//	)
//
// None of the three owns any navigation state. The selected index and the list
// of pushed screens are the application's, and the widgets report what the
// user asked for; see [TabBarView] and [NavigationStackView].
//
// The decision worth knowing before using them: an inactive tab and a covered
// screen stay **mounted** and are taken out of the frame with
// [gift.Element.Hidden]. They keep their scroll offsets and their state, they
// are not painted, not hit tested and not in the tab order, and the price is
// memory plus a build and a layout when something writes state inside one.
// [TabBarView] states the full argument and every cost.
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
