package ui

import (
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

var badgeType = gift.RegisterType("ui.Badge")

// Metrics of a badge, in logical pixels. They are constants for the reason
// the metrics of [TabBarView] are: two badges of different heights next to
// each other in one list is a design mistake, not a decision a call site
// should be able to make.
const (
	// BadgeHeight is the fixed height of a badge, and therefore twice its
	// corner radius: a badge is a capsule.
	//
	// Twenty is deliberately *below* [ControlHitTarget]. A badge is not a
	// control — it is not tappable, not focusable and carries no action — so
	// the forty four pixel floor does not apply to it. A badge that needs to
	// be pressed is a [ButtonView] whose label is a badge.
	BadgeHeight = float32(20)

	badgeFontSize = float32(11)
	badgeInset    = float32(7)
)

// BadgeView is a small capsule with a short string in it: a count, a status, a
// unit. It is created by [Badge]; the zero value is not useful.
//
//	ui.Row("Updates").Accessory(ui.Badge("3"))
//
// # It is a fixed height and a shrink-wrapped width
//
// The height is [BadgeHeight] at every density and for every string, so a
// column of badges lines up and the corner radius is exactly half the height —
// which is what makes it a capsule rather than a rounded rectangle that
// happens to look like one. The width is whatever the text needs plus an inset
// at each end, with a floor at the height so that a single digit is a circle
// rather than a slot.
//
// The text is one line and is not truncated. A badge with a sentence in it is
// a badge with the wrong content; a [TextView] with MaxLines is the thing that
// truncates, and silently shortening a count would be worse than a wide badge.
//
// # Colour
//
// [ColorAccent] behind [ColorOnAccent], which is the one pairing in the
// palette that is guaranteed legible in both themes — see [ColorOnAccent] —
// and therefore the only defensible default for a filled shape with text on
// it. [BadgeView.Color] and [BadgeView.Foreground] change them; a design that
// changes only one of the two is asking for the contrast failure that role
// exists to prevent.
type BadgeView struct {
	base
	text   string
	bg, fg Color
	hasBG  bool
	hasFG  bool
}

// Badge returns a badge showing text.
func Badge(text string) BadgeView { return BadgeView{text: text} }

// ViewType implements gift.View.
func (v BadgeView) ViewType() gift.TypeID { return badgeType }

// Build implements gift.View.
//
// It is a [ZStack] and not a [TextView] with a background, and the difference
// is the vertical centring: a text node's size is the size of its paragraph,
// so a fixed height imposed on it leaves the glyphs sitting at the top of the
// capsule. A Z stack measures its one child loosely inside the fixed height
// and aligns it in the middle.
func (v BadgeView) Build(bc *gift.BuildContext) gift.Element {
	bg, fg := ColorAccent, ColorOnAccent
	if v.hasBG {
		bg = v.bg
	}
	if v.hasFG {
		fg = v.fg
	}
	return ZStack(
		Text(v.text).FontSize(badgeFontSize).Foreground(fg).MaxLines(1).Key("label"),
	).
		Align(geom.Center).
		PaddingInsets(geom.Insets{Left: badgeInset, Right: badgeInset}).
		MinWidth(BadgeHeight).
		MinHeight(BadgeHeight).
		MaxHeight(BadgeHeight).
		Background(bg).
		CornerRadius(BadgeHeight / 2).
		Key(v.key).
		Flex(v.flex).
		Build(bc)
}

// Color sets the capsule fill, replacing [ColorAccent].
func (v BadgeView) Color(c Color) BadgeView { v.bg, v.hasBG = c, true; return v }

// Foreground sets the text colour, replacing [ColorOnAccent].
func (v BadgeView) Foreground(c Color) BadgeView { v.fg, v.hasFG = c, true; return v }

// Key sets the reconciliation key of this view among its siblings.
func (v BadgeView) Key(s string) BadgeView { v.setKey(s); return v }

// Flex makes the badge take a share of the remaining main axis space of its
// parent stack. A badge normally wants none.
func (v BadgeView) Flex(f float32) BadgeView { v.setFlex(f); return v }
