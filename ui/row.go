package ui

import (
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

var rowType = gift.RegisterType("ui.Row")

// Metrics of a list row, in logical pixels. Constants, for the reason the
// metrics of [TabBarView] are.
const (
	// RowHeight is the smallest height a row takes. It is
	// [ControlHitTarget], because a tappable row is a control and the target
	// panel is a touchscreen; a row that is taller than this because its
	// content is taller keeps the larger height.
	RowHeight = ControlHitTarget

	// RowPadding is the horizontal inset of a row's content, and therefore
	// the leading inset a separator has to match to line up with the text.
	RowPadding = float32(16)

	rowVPadding  = float32(8)
	rowGap       = float32(12)
	rowIconSize  = float32(22)
	rowChevron   = float32(16)
	rowTitleSize = float32(15)
	rowSubSize   = float32(12)
	rowValueSize = float32(14)
	rowTextGap   = float32(2)
)

// RowView is the standard row of a [ListView]: a leading icon, a title over an
// optional subtitle, and a trailing accessory. It is created by [Row]; the
// zero value is not useful.
//
//	ui.Row("Wi-Fi").Icon(outline.Globe).Value("kiosk-net").Chevron(outline.AngleRight).OnTap(open)
//	ui.Row("Dark mode").Icon(outline.Moon).Accessory(ui.Toggle(dark, setDark))
//
// # What a row is made of
//
// Leading icon, then the labels, then a flexible gap, then the value label,
// then the accessory, then the chevron — in that order, all of it optional
// except the title. The flexible gap is what pushes the trailing group to the
// edge, and it is also the one thing to know about putting a row somewhere
// other than a [ListView]: a stack measures an inflexible child with an
// unbounded main axis (the overflow model of the project plan, section 7), so
// a row in an [HStack] has no width to spread into and shrink-wraps its
// content. A [ListView] measures its rows with a tight width, which is the
// composition this type is for.
//
// # A row with no title is an accessory row
//
// The flexible gap is only inserted when the row has a title, a subtitle or a
// value, that is when there is something leading that the trailing group has
// to be pushed away from. A row with none of those gives its whole width to
// its accessory:
//
//	ui.Row("").Icon(outline.VolumeUp).Accessory(ui.Slider(v, set).Flex(1))
//
// That is not a convenience, it is the only way the composition works. A
// [Spacer] takes a share of the remainder like any other flexible child, so a
// row that had one *and* a flexible accessory would split the width between
// them and the slider would come out half as wide as the row — which is
// exactly what the settings screen of cmd/example-kitchensink did until this
// rule existed, and it showed up as a segmented control overflowing its own
// row by twelve pixels rather than as anything anybody would notice by
// looking.
//
// # A row does not truncate
//
// A title, a subtitle and a value are each one line and none of them is
// shortened to fit: gift's [TextView] has no ellipsis, and a stack measures an
// inflexible child with an unbounded main axis, so the labels are laid out at
// their natural width whatever the row's width is. A row whose content is too
// wide therefore *overflows*, which the project plan, section 7, requires to
// be reported rather than clipped — it appears in
// [gift.Diagnostics.OverflowNodes] and under the giftdebug tag it is logged.
// The answer is shorter strings, or a [RowView.Subtitle] instead of a longer
// title.
//
// # Tapping
//
// Without [RowView.OnTap] a row is inert: no interactor, no focus stop, no
// pressed face. With it the row is a [ButtonView] — the same pointer capture,
// the same space and enter activation, the same "a release outside does not
// fire" — whose label happens to be the row's content, and the press is shown
// by the row's own face going to [ColorControlPressed].
//
// # An interactive accessory takes the tap, and the row does not also fire
//
// This is the property that makes a settings list work and it falls out of
// gift rather than out of a special case here: the hit test finds the deepest
// interactive node under the finger, and a [ToggleView] answers true to the
// press and the release, so nothing bubbles up to the row. Tapping the switch
// flips the switch; tapping anywhere else on the same row runs OnTap. Both
// still appear in the focus order, which is correct — a keyboard user needs to
// be able to reach the switch *and* the row's own command — and is the one
// consequence worth knowing about.
//
// # Disabled
//
// [RowView.Disabled] takes the row out of input and draws its title in
// [ColorSecondaryLabel]. The title colour is the row's to change because the
// row builds the label; a [ButtonView] handed an arbitrary label view cannot
// do the same, for the reason its documentation gives. The accessory is *not*
// disabled with it: it is a view the caller passed in, and reaching into it
// would mean this type deciding what "disabled" means for something it has
// never seen. Disable the accessory at the call site as well.
type RowView struct {
	base
	title     string
	subtitle  string
	value     string
	sym       Symbol
	chevron   Symbol
	accessory gift.View
	onTap     func()
	disabled  bool
	name      string
}

// Row returns a row with the given title.
func Row(title string) RowView { return RowView{title: title} }

// ViewType implements gift.View.
func (v RowView) ViewType() gift.TypeID { return rowType }

// Build implements gift.View.
func (v RowView) Build(bc *gift.BuildContext) gift.Element {
	content := HStack(v.parts()...).
		Gap(rowGap).
		Align(geom.Center).
		PaddingInsets(geom.Insets{
			Top:    rowVPadding,
			Right:  RowPadding,
			Bottom: rowVPadding,
			Left:   RowPadding,
		})

	if v.onTap == nil {
		// Inert. No interactor, no focus stop, and — because the content
		// stack has no background, border or clip — no painter either.
		return content.MinHeight(RowHeight).Key(v.key).Flex(v.flex).Build(bc)
	}
	return Button(content, v.onTap).
		Key(v.key).
		Flex(v.flex).
		// The button's own padding is the row's, applied above, so that the
		// pressed face reaches the edges of the row rather than stopping
		// twelve pixels short of them.
		Padding(0).
		Align(geom.Leading).
		MinHeight(RowHeight).
		// A row draws no face at rest: the list or the card behind it does.
		Background(ColorClear).
		Border(Border{}).
		CornerRadius(0).
		// Hover and press are the states a rebuild cannot deliver, because
		// neither causes one; see [gift.Interaction].
		HoverStyle(ButtonStyle{Background: ColorControlHover}).
		PressedStyle(ButtonStyle{Background: ColorControlPressed}).
		DisabledStyle(ButtonStyle{Background: ColorClear}).
		Disabled(v.disabled).
		Label(v.accessibleName()).
		Build(bc)
}

// accessibleName is the string a person would use to refer to the row's
// command. It is [RowView.Label] when one was given and the title otherwise,
// which is what makes an icon-and-title row need no Label at all.
func (v RowView) accessibleName() string {
	if v.name != "" {
		return v.name
	}
	return v.title
}

// parts builds the children of the row's content stack.
//
// The capacity is exact for the largest row this type can produce, so the
// slice is one allocation whatever the combination of options. That is six and
// not five: leading icon, label group, flexible gap, value, accessory,
// chevron. The example in [RowView]'s own documentation plus an
// [RowView.Accessory] reaches all six, and at a capacity of five that slice
// grew and copied on the last append — a second allocation per row per
// rebuild, which on a two hundred row list is two hundred of them on a frame
// the rest of this package counts single allocations on.
// TestTheLargestRowBuildsItsChildrenInOneAllocation pins it.
func (v RowView) parts() []gift.View {
	titleFG := ColorLabel
	subFG := ColorSecondaryLabel
	if v.disabled {
		// The whole label group steps back, not only the title. A quiet
		// title over a normal subtitle reads as an emphasis, not as an
		// absence.
		titleFG, subFG = ColorSecondaryLabel, Fade(ColorSecondaryLabel, 0.6)
	}

	out := make([]gift.View, 0, 6)
	if !v.sym.IsZero() {
		fg := ColorAccent
		if v.disabled {
			fg = ColorSecondaryLabel
		}
		out = append(out, Icon(v.sym).Size(rowIconSize).Foreground(fg).Key("icon"))
	}

	switch {
	case v.title == "" && v.subtitle == "":
		// An accessory row; see [RowView]. No label, and — unless there is a
		// value to push to the trailing edge — no flexible gap either.
	case v.subtitle == "":
		out = append(out, Text(v.title).
			FontSize(rowTitleSize).Foreground(titleFG).MaxLines(1).Key("title"))
	default:
		out = append(out, VStack(
			Text(v.title).FontSize(rowTitleSize).Foreground(titleFG).MaxLines(1).Key("title"),
			Text(v.subtitle).FontSize(rowSubSize).Foreground(subFG).MaxLines(1).Key("subtitle"),
		).Gap(rowTextGap).Align(geom.Leading).Key("labels"))
	}

	// The flexible gap, which is what pushes the trailing group to the edge
	// and what makes the content stack fill the width it was offered.
	//
	// It is omitted for a row with no labels, and that is not a
	// micro-optimisation: a Spacer takes a share of the remainder, so a row
	// that has one *and* a flexible accessory splits the width between them
	// and the accessory comes out half as wide as the row. A slider or a
	// segmented control in a list is exactly that composition. See [RowView].
	if v.title != "" || v.subtitle != "" || v.value != "" {
		out = append(out, Spacer().Key("gap"))
	}

	if v.value != "" {
		out = append(out, Text(v.value).
			FontSize(rowValueSize).
			Foreground(ColorSecondaryLabel).
			MaxLines(1).
			Key("value"))
	}
	if v.accessory != nil {
		out = append(out, v.accessory)
	}
	if !v.chevron.IsZero() {
		out = append(out, Icon(v.chevron).
			Size(rowChevron).
			Foreground(Fade(ColorSecondaryLabel, 0.7)).
			Key("chevron"))
	}
	return out
}

// --- modifiers -------------------------------------------------------------

// Subtitle sets a second, quieter line under the title.
func (v RowView) Subtitle(s string) RowView { v.subtitle = s; return v }

// Value sets a trailing label, for the current setting of whatever the row
// names: "kiosk-net", "60 %", "Off".
func (v RowView) Value(s string) RowView { v.value = s; return v }

// Icon sets the leading symbol, drawn in [ColorAccent]. The zero [Symbol] is
// no icon and no space taken, which is what an application that imports no
// icon package gets.
func (v RowView) Icon(s Symbol) RowView { v.sym = s; return v }

// Chevron sets the trailing symbol that says "this row leads somewhere",
// drawn faintly at the very end of the row:
//
//	ui.Row("Network").Chevron(outline.AngleRight).OnTap(push)
//
// It is a parameter rather than a built in glyph for the reason
// [NavigationStackView.BackIcon] is one: package ui cannot import an icon
// package, because the icon packages import it.
func (v RowView) Chevron(s Symbol) RowView { v.chevron = s; return v }

// Accessory sets the trailing view: a [ToggleView], a [BadgeView], anything.
// Ownership of it passes to gift like any other child.
//
// An interactive accessory takes the tap without the row also firing; see
// [RowView].
func (v RowView) Accessory(a gift.View) RowView { v.accessory = a; return v }

// OnTap makes the whole row a control that runs fn when it is activated. A nil
// fn, the default, is an inert row.
func (v RowView) OnTap(fn func()) RowView { v.onTap = fn; return v }

// Disabled takes the row out of input and draws its labels in
// [ColorSecondaryLabel]. It has no effect on a row without an
// [RowView.OnTap] — an inert row is already out of input — beyond the label
// colour, which is the honest way to show "this setting is unavailable" on a
// row that only reports a value.
func (v RowView) Disabled(b bool) RowView { v.disabled = b; return v }

// Label sets the accessible name of the row's command, replacing the title.
func (v RowView) Label(s string) RowView { v.name = s; return v }

// Key sets the reconciliation key of this view among its siblings.
func (v RowView) Key(s string) RowView { v.setKey(s); return v }

// Flex makes the row take a share of the remaining main axis space of its
// parent stack. A row in a [ListView] wants none.
func (v RowView) Flex(f float32) RowView { v.setFlex(f); return v }
