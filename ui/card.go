package ui

import (
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

var cardType = gift.RegisterType("ui.Card")

// Metrics of a card, in logical pixels.
const (
	// CardRadius is the corner radius of a card.
	CardRadius = float32(12)
	// CardPadding is the default inset of a card's content.
	CardPadding = float32(16)

	cardGap        = float32(12)
	cardHeaderSize = float32(13)
	cardHeaderGap  = float32(8)
)

// CardView is a panel raised above the window background: a [ColorSurface]
// face, a corner radius, a hairline and an optional header. It is created by
// [Card]; the zero value is not useful.
//
//	ui.Card(
//		ui.Text("Signed in as kiosk@example.com"),
//		ui.Slider(v, set),
//	).Header("Account")
//
// # The header is outside the content padding
//
// Deliberately, and it is the only thing about this type that needs
// explaining. [CardView.Padding] sets the inset of the *content* and may be
// zero, which is what a card wrapping a [ListView] wants: the rows bring their
// own horizontal padding, and a second one inside the card would leave their
// pressed highlight floating in the middle of the panel instead of reaching
// its edges. The header still needs an inset in that case, so it has its own,
// [CardPadding], which does not follow the content padding.
//
//	ui.Card(ui.List(rows...)).Padding(0).Header("Display")
//
// # It shrink-wraps vertically
//
// A card is as tall as its content. Give it a [CardView.Flex] to make it take
// a share of a stack, and remember that its children are then measured by an
// ordinary [VStack] inside it — a scroll container in a card needs a Flex of
// its own, like it does anywhere else.
//
// # The corners are not a clip
//
// A card does not clip its children, and turning the clip on would not round
// them: gift's clip is the bounding rectangle and not the rounded shape; see
// paintBorder. A child painted into a corner therefore covers it. The answer
// is to leave the corner alone — a [ListView] inside a card draws no
// background of its own, so the card's face is what shows through — rather
// than to promise a rounding that the backend cannot do yet.
type CardView struct {
	base
	header     string
	children   []gift.View
	gap        float32
	hasGap     bool
	hasPadding bool
}

// Card returns a card holding children, stacked vertically.
//
// The children slice belongs to gift from this call onwards, exactly like
// [VStack]'s.
func Card(children ...gift.View) CardView { return CardView{children: children} }

// ViewType implements gift.View.
func (v CardView) ViewType() gift.TypeID { return cardType }

// Build implements gift.View.
//
// Without a header the card *is* the padded stack of its children and there is
// no wrapper node; with one it is a stack of the header and that stack. A card
// therefore costs one node, or two when it has a header, and never a node
// whose only job is to exist.
func (v CardView) Build(bc *gift.BuildContext) gift.Element {
	pad := geom.InsetsAll(CardPadding)
	if v.hasPadding {
		pad = v.pad
	}
	gap := cardGap
	if v.hasGap {
		gap = v.gap
	}
	body := VStack(v.children...).Gap(gap).PaddingInsets(pad)

	if v.header == "" {
		return v.face(body).Key(v.key).Flex(v.flex).Build(bc)
	}
	return v.face(VStack(
		Text(v.header).
			FontSize(cardHeaderSize).
			Foreground(ColorSecondaryLabel).
			MaxLines(1).
			PaddingInsets(geom.Insets{
				Top:    CardPadding,
				Right:  CardPadding,
				Bottom: cardHeaderGap,
				Left:   CardPadding,
			}).
			Key("header"),
		body.Key("body"),
	)).Key(v.key).Flex(v.flex).Build(bc)
}

// face applies the card's own look to the stack that carries it.
//
// The hairline is [ColorSeparator] and not omitted, because a surface on a
// background that is only a few units away from it — which is what the light
// theme's 255 against 246 is — is otherwise an edge nobody can see, and the
// whole point of a card is that its edge is visible.
func (v CardView) face(s Stack) Stack {
	return s.
		Background(ColorSurface).
		CornerRadius(CardRadius).
		Border(Border{Width: 1, Color: ColorSeparator})
}

// Header sets the caption drawn above the content, in [ColorSecondaryLabel].
// An empty header, the default, draws nothing and costs no node.
func (v CardView) Header(s string) CardView { v.header = s; return v }

// Padding sets the inset of the card's content on all four edges, replacing
// the default of [CardPadding]. It does not move the header; see [CardView].
func (v CardView) Padding(f float32) CardView { v.setPadding(f); v.hasPadding = true; return v }

// PaddingInsets sets the inset of the card's content per edge, replacing the
// default. It does not move the header; see [CardView].
func (v CardView) PaddingInsets(i geom.Insets) CardView {
	v.setPaddingInsets(i)
	v.hasPadding = true
	return v
}

// Gap sets the space between two adjacent children, replacing the default.
func (v CardView) Gap(f float32) CardView { v.gap, v.hasGap = checkGap(f), true; return v }

// Key sets the reconciliation key of this view among its siblings.
func (v CardView) Key(s string) CardView { v.setKey(s); return v }

// Flex makes the card take a share of the remaining main axis space of its
// parent stack.
func (v CardView) Flex(f float32) CardView { v.setFlex(f); return v }
