package ui

import (
	"strconv"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var navigationStackType = gift.RegisterType("ui.NavigationStack")

// Metrics of the title bar, in logical pixels. See [TabBarHeight] for why
// these are constants and not modifiers.
const (
	// NavigationBarHeight is the height of the title bar of a
	// [NavigationStackView].
	//
	// It is above [ControlHitTarget] so that the back affordance inside it
	// can be a full sized target with a little room around it.
	NavigationBarHeight = float32(52)

	// navBackIconSize is the edge of the chevron of the back affordance.
	navBackIconSize = float32(22)
	// navTitleSize is the font size of the title.
	navTitleSize = float32(17)
	// navBarPadding is the horizontal inset of the bar's contents.
	navBarPadding = float32(8)
)

// ScreenSpec is one screen of a [NavigationStackView]. It is created by
// [Screen]; the zero value is not useful.
type ScreenSpec struct {
	title   string
	content gift.View
	key     string
}

// Screen describes one screen of a navigation stack.
//
// title is drawn in the bar while this screen is on top, and is the accessible
// name of the bar. content is the screen itself and ownership of it passes to
// gift.
//
// The reconciliation identity defaults to the title, like [Tab]'s does, and
// for the same reason: a stack that is popped and pushed again has to be able
// to say which screen is which, and the index is exactly the thing that
// changes. Two screens of one stack with the same title need
// [ScreenSpec.Key] — gift rejects duplicate sibling keys rather than resolving
// them.
func Screen(title string, content gift.View) ScreenSpec {
	if content == nil {
		panic("gift/ui: Screen with a nil content view")
	}
	return ScreenSpec{title: title, content: content}
}

// Key sets the reconciliation identity of this screen, replacing the default
// of the title; see [Screen].
func (s ScreenSpec) Key(v string) ScreenSpec { s.key = v; return s }

func (s ScreenSpec) identity(i int) string {
	switch {
	case s.key != "":
		return s.key
	case s.title != "":
		return s.title
	default:
		return "screen-" + strconv.Itoa(i)
	}
}

// NavigationStackView is a stack of screens within one place: a title bar with
// a back affordance and, under it, the screen on top of the stack. It is
// created by [NavigationStack]; the zero value is not useful.
//
//	ui.NavigationStack(pop, screens...)
//
// # It holds no stack
//
// The name says "stack" and the type stores none. The screens are an argument,
// exactly like the tabs of a [TabBarView] and the children of a [VStack]: the
// application owns the list, a push is appending to it and a pop is dropping
// the last element. There is no Push method and there is deliberately no
// internal history, because a navigation state that lives inside a widget is a
// second source of truth next to the application's own — and the first thing
// any real application wants is to restore it, to deep link into it, or to
// refuse to leave a screen. All three are trivial when the list is the
// application's and impossible when it is not. This is the one-way data flow
// of the project plan, section 5, applied to navigation.
//
// The consequence to keep in mind is that *the back affordance is a request*.
// It calls onBack and nothing happens until the application shortens its list
// and rebuilds. A screen that must not be left simply does not shorten it.
//
// # A covered screen stays mounted and is not drawn
//
// The same decision as [TabBarView], for the same reasons and with the same
// costs; the long form of the argument is there. The screen below the one on
// top keeps its state, its scroll offset and its half typed text, costs
// nothing per frame because it is not painted and not hit tested, and becomes
// visible again when it is popped back to — with the list still where the user
// left it.
//
// "Where the user left it" is meant literally, and it is the one promise here
// that took work rather than falling out of the flag. A fling in flight when
// the screen is covered used to keep ticking: measured at 140 of 600 frames
// and an offset that travelled from 400 to 1395 document units, so the list
// came back a thousand pixels past where the finger left it. The fling is now
// stopped at the moment the screen is hidden, offset intact; see
// [gift.App.stopHiddenWork].
//
// The cost this type has and a tab bar does not: a stack that grows without
// bound keeps every screen it ever pushed. Ten levels of drill-down is ten
// live subtrees. That is the application's list and the application's problem,
// and it is the honest consequence of not owning the history.
//
// # The back affordance
//
// A chevron and the previous screen's title, at the leading edge of the bar,
// as a [ButtonView] of at least [ControlHitTarget] on both axes. It is absent
// on the root screen — there is nowhere to go — which is also why popping the
// root is impossible rather than merely discouraged: no affordance offers it
// and the escape key below refuses it.
//
// Escape pops as well, and it is honestly a secondary path: gift delivers keys
// to the focused node and bubbles them upwards, so escape reaches the stack
// only when the focus is *inside* the stack. On a touch panel with no keyboard
// attached nothing is ever focused and escape never arrives, which is exactly
// the case this widget is built for; the key is there for a desk and for a
// test. There is deliberately no edge swipe: a horizontal drag near the edge
// of the screen is the gesture a horizontally scrolling gallery already owns,
// and gift's pointer arbitration is "whoever steals the pointer last wins"
// rather than an arena — see [gift.EventContext.StealPointer] — so the two
// could not be told apart without building one.
type NavigationStackView struct {
	base
	onBack  func()
	screens []ScreenSpec
	backSym Symbol
}

// NavigationStack returns a navigation stack showing the last of screens, with
// a back affordance that calls onBack.
//
// An empty screens list panics: a navigation stack with nothing in it has no
// title, no content and no meaning, and the application state that produced it
// is broken in a way a blank screen would hide. A nil onBack makes the back
// affordance inert but still drawn, which is the same choice [Button] makes
// for a nil action.
//
// The screens slice belongs to gift from this call onwards.
func NavigationStack(onBack func(), screens ...ScreenSpec) NavigationStackView {
	if len(screens) == 0 {
		panic("gift/ui: NavigationStack with no screens")
	}
	return NavigationStackView{onBack: onBack, screens: screens}
}

// ViewType implements gift.View.
func (v NavigationStackView) ViewType() gift.TypeID { return navigationStackType }

// Build implements gift.View.
//
// A vertical stack of the bar and a flexible content area holding one [layer]
// per screen, all hidden but the last. The NavigationStack node is that stack;
// there is no wrapper node.
func (v NavigationStackView) Build(bc *gift.BuildContext) gift.Element {
	top := len(v.screens) - 1

	layers := make([]gift.View, len(v.screens))
	for i, s := range v.screens {
		layers[i] = newLayer(s.identity(i), s.content).Hidden(i != top)
	}

	e := VStack(
		v.bar(top),
		ZStack(layers...).Flex(1).Key("content"),
	).Key(v.key).Flex(v.flex).Build(bc)
	// The escape key. The stack node is an ancestor of every screen, so a key
	// the focused node did not consume bubbles here; see [App.deliver].
	// Declaring an interactor and not declaring Focusable is the same thing
	// ui.OnScreenKeyboard does: it makes the node reachable by a bubbling
	// event without putting it in the tab order and without letting a tap on
	// the background of a screen land on it — there is no background, the
	// node draws nothing.
	//
	// It does mean a tap anywhere inside the stack that nothing else handled
	// arrives here. That is harmless: the handler answers false to everything
	// that is not the escape key, so the event keeps bubbling exactly as it
	// would have.
	e.Interactor = navBackKey{stack: v, canPop: top > 0}
	return e
}

// bar builds the title bar of the screen at index top.
func (v NavigationStackView) bar(top int) gift.View {
	title := Text(v.screens[top].title).
		FontSize(navTitleSize).
		Foreground(ColorLabel).
		MaxLines(1).
		Align(AlignCenter).
		Flex(1)

	var leading gift.View
	if top > 0 {
		back := Text(v.screens[top-1].title).
			FontSize(navTitleSize).
			Foreground(ColorAccent).
			MaxLines(1)
		var label gift.View = back
		if !v.backSym.IsZero() {
			label = HStack(
				Icon(v.backSym).Size(navBackIconSize).Foreground(ColorAccent),
				back,
			).Gap(2).Align(geom.Center)
		}
		leading = Button(label, v.onBack).
			Key("back").
			MinWidth(ControlHitTarget).
			MinHeight(ControlHitTarget).
			Padding(4).
			Background(ColorClear).
			Border(Border{}).
			CornerRadius(0).
			PressedStyle(ButtonStyle{Background: ColorControlPressed, CornerRadius: 8}).
			Label("Back")
	} else {
		// A spacer of the same minimum width as the affordance would be,
		// so that the centred title does not jump sideways when a screen is
		// pushed. It is not a Spacer with a Flex — that would take the whole
		// remainder — but a fixed, empty box.
		leading = Box().Frame(ControlHitTarget, 0)
	}

	return VStack(
		HStack(
			leading,
			title,
			// The trailing counterweight. Without it the flexible title is
			// centred inside the space that is left over after the back
			// affordance, which is not the middle of the bar.
			Box().Frame(ControlHitTarget, 0),
		).
			Frame(geom.Unbounded(), NavigationBarHeight).
			PaddingInsets(geom.Insets{Left: navBarPadding, Right: navBarPadding}).
			Align(geom.Center),
		Box().Frame(geom.Unbounded(), 1).Background(ColorSeparator),
	).Background(ColorSurface).Key("bar")
}

// navBackKey is the interactor of the stack node: the escape key and nothing
// else.
//
// It is a value type and is stored in the interface by value, so the element
// of a navigation stack allocates nothing beyond the stack node itself.
type navBackKey struct {
	stack  NavigationStackView
	canPop bool
}

// HandleEvent implements gift.Interactor.
func (n navBackKey) HandleEvent(_ *gift.EventContext, e gift.Event) bool {
	if e.Kind != gift.EventKeyDown || e.Key != gift.KeyEscape {
		return false
	}
	if !n.canPop {
		// The root screen. The key is still consumed, because an escape that
		// travelled out of a navigation stack to whatever encloses it would
		// close a modal the user cannot see from here, or dismiss something
		// else entirely. "Back on the root screen does nothing" is a
		// deliberate stop and not an omission.
		return true
	}
	if n.stack.onBack != nil {
		n.stack.onBack()
	}
	return true
}

// --- modifiers -------------------------------------------------------------

// BackIcon sets the symbol drawn before the previous screen's title in the
// back affordance. The conventional choice is a leading chevron:
//
//	ui.NavigationStack(pop, screens...).BackIcon(outline.AngleLeft)
//
// It is a parameter and not a built in glyph because package ui cannot import
// an icon package — gift/icon/outline imports *this* package to declare its
// variables as [Symbol], so the dependency only runs one way; see [Symbol].
// Without it the affordance is the previous title alone, which is legible and
// is what an application that imports no icons gets.
func (v NavigationStackView) BackIcon(s Symbol) NavigationStackView { v.backSym = s; return v }

// Key sets the reconciliation key of this view among its siblings.
func (v NavigationStackView) Key(s string) NavigationStackView { v.setKey(s); return v }

// Flex makes the stack take a share of the remaining main axis space of its
// parent stack. A navigation stack inside a [TabBarView] is already given the
// whole tab and needs none.
func (v NavigationStackView) Flex(s float32) NavigationStackView { v.setFlex(s); return v }
