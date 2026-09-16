package ui

import (
	"strconv"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var tabBarType = gift.RegisterType("ui.TabBar")

// Metrics of the tab bar, in logical pixels.
//
// None of them is a modifier, for the reason the metrics of [ToggleView] are
// not: a tab bar that is a different height on two screens of one application
// is a design mistake, and a knob that lets it happen is worse than a constant
// that does not.
const (
	// TabBarHeight is the height of the bar itself, not counting anything
	// below it.
	//
	// Fifty-six is Material's figure and is comfortably above
	// [ControlHitTarget]; iOS uses forty-nine, which is above it by five
	// pixels and leaves a two line item — icon over label — genuinely cramped
	// at the label sizes this package's default font produces.
	TabBarHeight = float32(56)

	// tabIconSize is the edge of the icon of a tab item.
	tabIconSize = float32(24)
	// tabLabelSize is the font size of the caption under the icon.
	tabLabelSize = float32(11)
	// tabItemGap is the space between the icon and the caption.
	tabItemGap = float32(2)
)

// TabSpec is one tab: what the bar shows for it and what it contains. It is
// created by [Tab]; the zero value is not useful.
type TabSpec struct {
	title    string
	sym      Symbol
	selected Symbol
	content  gift.View
	key      string
}

// Tab describes one tab of a [TabBarView].
//
// title is drawn under the icon and is also the accessible name of the item.
// sym may be the zero [Symbol], which draws no icon and leaves the caption
// centred; a tab bar of captions alone is a legitimate design and is what an
// application that does not import an icon package gets.
//
// content is the tab's screen. Ownership of it passes to gift like any other
// child view.
//
// # The key is the tab's identity and it defaults to the title
//
// Reconciliation matches the mounted tabs against the built ones by this key,
// and that is what makes the state inside a tab survive its neighbours coming
// and going: a tab bar that is built with three tabs and then with the middle
// one removed keeps the third tab's scroll offset, because the third tab is
// still the tab with that key and not "the child at index two". Use
// [TabSpec.Key] when two tabs share a title, which is the only case the
// default gets wrong — and which gift reports as a duplicate key rather than
// resolving silently.
func Tab(title string, sym Symbol, content gift.View) TabSpec {
	if content == nil {
		panic("gift/ui: Tab with a nil content view")
	}
	return TabSpec{title: title, sym: sym, content: content}
}

// SelectedIcon sets the symbol drawn while this tab is the selected one. The
// usual pairing is an outline icon at rest and the solid one when selected,
// which is what every phone platform draws and what the two generated icon
// packages of this project are for:
//
//	ui.Tab("Home", outline.Home, home).SelectedIcon(solid.Home)
//
// Without it the same symbol is used in both states and only the colour
// changes.
func (t TabSpec) SelectedIcon(v Symbol) TabSpec { t.selected = v; return t }

// Key sets the reconciliation identity of this tab, replacing the default of
// the title; see [Tab].
func (t TabSpec) Key(v string) TabSpec { t.key = v; return t }

// identity is the reconciliation key of the tab.
func (t TabSpec) identity(i int) string {
	switch {
	case t.key != "":
		return t.key
	case t.title != "":
		return t.title
	default:
		// A tab with neither a key nor a title. Anything stable will do; the
		// index is stable for as long as nothing is inserted before it,
		// which is the same guarantee an unkeyed child of a stack has.
		return "tab-" + strconv.Itoa(i)
	}
}

// TabBarView is the primary navigation of a kiosk: a row of icon-and-caption
// items along the bottom of the window, one screen above them. It is created
// by [TabBar]; the zero value is not useful.
//
//	ui.TabBar(sel.Get(), sel.Set,
//		ui.Tab("Home", outline.Home, homeScreen).SelectedIcon(solid.Home),
//		ui.Tab("Settings", outline.Cog, settingsScreen),
//	)
//
// # An inactive tab stays mounted and is not drawn
//
// This is the decision the type is built around and it is worth the paragraph.
//
// The two obvious answers are both wrong here. *Unmounting* an inactive tab
// destroys its scope and therefore its state — the project plan, section 5,
// "State lebt bis zum Unmount seines Scopes" — so returning to a tab would
// reset its scroll position and empty its half typed text field. On a kiosk,
// where the user's whole interaction is switching between a handful of
// screens, that is not a detail. *Leaving it mounted and drawn* is worse in a
// different way: gift has no paint culling whatsoever, so every node of every
// tab would be painted every frame, and anything in an inactive tab that keeps
// the device awake would keep it awake for ever — an indeterminate
// [ProgressBarView] is documented to do exactly that for as long as it is
// mounted.
//
// So an inactive tab is mounted and hidden; see [gift.Element.Hidden]. It
// keeps its state, its scroll offsets and its text, and it costs nothing per
// frame: it is not painted, not hit tested and not in the tab order, and the
// indeterminate progress bar stops holding the device awake because the thing
// that re-arms its repaint enrolment is its own painter, which no longer runs.
// Switching back is a rebuild of nothing at all — no view function in the tab
// runs, because nothing in it became dirty — and a repaint.
//
// The costs of the choice, stated rather than implied:
//
//   - memory. Every tab's whole subtree exists from the first frame. A tab
//     bar is a handful of screens, so this is bounded and known; a list of a
//     hundred screens is not a tab bar and wants a [NavigationStackView].
//   - the first build. All tabs are built before the first frame is drawn, so
//     the window opens later than it would with one screen in it. The
//     pictures on the tabs are fetched at asset.Prefetch rather than at
//     asset.Visible priority for as long as their tab is hidden, so they do
//     not compete with the tab the user is looking at for what on a Pi 4 is
//     one core of decode budget; see [ImageView].
//   - a state write inside a hidden tab still rebuilds and re-lays out that
//     tab. A background task pushing rows into an inactive list costs a build
//     and a layout per change and no paint. This is the one cost that is
//     *not* avoided, and it is the application's to avoid if it matters.
//
// The cost that used to be on this list and is not any more: an enrolment
// taken out before the tab was hidden. A fling in flight, a caret blink, a
// control's slide animation and a scroll indicator's fade are all ended at the
// moment the tab is hidden rather than left to run to their deadlines — the
// fling was measured at 140 of 600 frames and a thousand document units of
// travel into a tab nobody was looking at. See [gift.App.stopHiddenWork].
//
// # It must be given a bounded area
//
// A tab bar is the window. Measured with an unbounded axis — inside a scroll
// container, or as an inflexible child of a stack, ui.VStack(bar) rather than
// ui.VStack(bar.Flex(1)) — it panics rather than shrink-wrapping, because a
// screen that shrink-wraps is a screen whose background does not reach the
// edges and, in the [ModalView] case, a scrim that blocks nothing. The message
// names the fix. See the layouter of the layer these three containers share.
//
// # The bar
//
// Each item is at least [ControlHitTarget] on both axes and in practice much
// wider, because the items divide the width of the window equally. The
// selected item is drawn in [ColorAccent] and the others in
// [ColorSecondaryLabel], which is the whole of the selection affordance: an
// item is an ordinary [ButtonView] whose label happens to be an icon over a
// caption, and a button cannot recolour a label view it was handed — the
// project plan, section 4, "Styling endet an der View-Grenze". The colour
// therefore comes from the rebuild that the selection change causes, which is
// also why there is no hover or press *colour* on the caption and only a face
// behind it.
//
// # It is not stateful
//
// Like every other control in this package it draws the selection it was given
// and reports the one that was asked for. Nothing moves until the application
// stores the new index and rebuilds, which is what makes "a tab that refuses
// to be left" — an unsaved form — expressible.
type TabBarView struct {
	base
	selected int
	onSelect func(int)
	tabs     []TabSpec
}

// TabBar returns a tab bar showing the content of tabs[selected] and calling
// onSelect with the index of the tab the user asked for.
//
// selected is clamped into range: a bar built with an index that is out of
// bounds shows the first tab rather than panicking, because the index is
// application state and an application that has just deleted a tab will have
// one for a frame. A bar with no tabs at all is a programming error and
// panics, because there is nothing sensible to draw and nothing to select.
//
// The tabs slice belongs to gift from this call onwards, like the children of
// a [VStack].
func TabBar(selected int, onSelect func(int), tabs ...TabSpec) TabBarView {
	if len(tabs) == 0 {
		panic("gift/ui: TabBar with no tabs")
	}
	return TabBarView{selected: selected, onSelect: onSelect, tabs: tabs}
}

// ViewType implements gift.View.
func (t TabBarView) ViewType() gift.TypeID { return tabBarType }

// Build implements gift.View.
//
// The tree is a vertical stack of two things: a flexible content area holding
// one [layer] per tab, of which all but the selected one are hidden, and the
// bar. The TabBar node *is* that stack — this view delegates rather than
// wrapping, so a tab bar costs no node of its own.
func (t TabBarView) Build(bc *gift.BuildContext) gift.Element {
	sel := t.selected
	if sel < 0 || sel >= len(t.tabs) {
		sel = 0
	}

	layers := make([]gift.View, len(t.tabs))
	for i, tab := range t.tabs {
		layers[i] = newLayer(tab.identity(i), tab.content).Hidden(i != sel)
	}
	items := make([]gift.View, len(t.tabs))
	for i, tab := range t.tabs {
		items[i] = t.item(i, tab, i == sel)
	}

	return VStack(
		ZStack(layers...).Flex(1).Key("content"),
		VStack(
			// The hairline above the bar. It is a one pixel box and not a
			// [Border], because a border strokes all four edges and the
			// other three would be a frame around the bottom of the window.
			Box().Frame(geom.Unbounded(), 1).Background(ColorSeparator),
			HStack(items...).Frame(geom.Unbounded(), TabBarHeight),
		).Background(ColorSurface).Key("bar"),
	).Key(t.key).Flex(t.flex).Build(bc)
}

// item builds one tab item.
func (t TabBarView) item(i int, tab TabSpec, selected bool) gift.View {
	fg := ColorSecondaryLabel
	sym := tab.sym
	if selected {
		fg = ColorAccent
		if !tab.selected.IsZero() {
			sym = tab.selected
		}
	}

	var label gift.View
	caption := Text(tab.title).FontSize(tabLabelSize).Foreground(fg).MaxLines(1)
	if sym.IsZero() {
		label = caption
	} else {
		label = VStack(
			Icon(sym).Size(tabIconSize).Foreground(fg),
			caption,
		).Gap(tabItemGap).Align(geom.Center)
	}

	idx := i
	var act func()
	if t.onSelect != nil {
		// Captured by value. The closure is rebuilt on every build like the
		// rest of the view, so there is no stale index to worry about, and
		// the loop variable of Go 1.22 and later is per iteration anyway.
		act = func() { t.onSelect(idx) }
	}
	return Button(label, act).
		Key(tab.identity(i)).
		Flex(1).
		// The full height of the bar, not [ControlHitTarget]. The item is
		// top aligned in the row — a stack places a child that is smaller
		// than the row wherever its alignment says — so an item the size of
		// the hit target floor would leave the bottom twelve pixels of the
		// bar dead, which on a touch panel is the twelve pixels a thumb
		// coming up from the bottom bezel arrives at first.
		MinHeight(TabBarHeight).
		MinWidth(ControlHitTarget).
		Padding(4).
		// The bar draws the surface; an item draws nothing of its own at
		// rest, so the two do not stack two faces on top of each other.
		Background(ColorClear).
		Border(Border{}).
		CornerRadius(0).
		// ...but a press has to be visible, and it is the one state whose
		// colour a rebuild cannot deliver, because a press does not cause
		// one. See [gift.Interaction].
		PressedStyle(ButtonStyle{Background: ColorControlPressed}).
		Label(tab.title)
}

// --- modifiers -------------------------------------------------------------

// Key sets the reconciliation key of this view among its siblings.
func (t TabBarView) Key(v string) TabBarView { t.setKey(v); return t }

// Flex makes the tab bar take a share of the remaining main axis space of its
// parent stack. A tab bar is normally the whole window and needs none.
func (t TabBarView) Flex(v float32) TabBarView { t.setFlex(v); return t }
