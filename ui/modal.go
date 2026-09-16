package ui

import (
	"strconv"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var (
	modalType = gift.RegisterType("ui.Modal")
	scrimType = gift.RegisterType("ui.scrim")
	alertType = gift.RegisterType("ui.Alert")
)

// Metrics of the modal layer, in logical pixels.
const (
	// alertWidth is the width of an [AlertView]'s card. It is the figure
	// every phone platform uses for an alert, and it is a fixed width rather
	// than a fraction of the window because an alert stretched across a
	// 1920 pixel kiosk panel would be a line of text a metre long.
	alertWidth = float32(320)
	// alertPadding is the inset of the card's contents.
	alertPadding = float32(20)
	// alertRadius is the corner radius of the card.
	alertRadius = float32(14)
	// alertTitleSize and alertMessageSize are the two type sizes of the card.
	alertTitleSize   = float32(17)
	alertMessageSize = float32(14)
)

// defaultScrimColor is the wash drawn over the application while a modal is
// open.
//
// It is [ColorLabel] at a fifth rather than a literal black, so that it is the
// theme's own "ink" colour in both themes. A literal black over the dark theme
// is the same dimming as over the light one and reads as a smudge; the label
// colour is near-black in light and near-white in dark, and a light wash over
// a dark application separates the alert from it just as well as a dark wash
// does over a light one.
var defaultScrimColor = Fade(ColorLabel, 0.2)

// ModalView presents one view on top of another and takes the input of
// everything underneath. It is created by [Modal]; the zero value is not
// useful.
//
//	ui.Modal(screen, alertIfAny)
//
// # What "modal" means here, precisely
//
// Three things, and they are three separate mechanisms rather than one:
//
//   - a *scrim*: a full size interactive node between the content and the
//     modal. It swallows every pointer event, so a tap that lands on a button
//     beneath the alert reaches the scrim and stops there — including a tap
//     far away from the alert's own pixels, which is the case a modal that
//     merely covered its own rectangle would get wrong. "Full size" is the
//     load bearing word and it is enforced rather than hoped for: a modal
//     measured with an unbounded axis panics, because there the scrim would
//     measure zero while the alert above it sized and painted exactly as
//     usual, and every tap would go straight through a dialog that is on the
//     screen. See [TabBarView] for the compositions that get there and the
//     one line that fixes each.
//   - a *focus trap*: [gift.Element.FocusTrap] on the modal layer, so tab
//     cannot walk into the content underneath, and the build that opens the
//     modal takes the focus away from whatever held it. Without this the
//     scrim would stop the finger and let the keyboard straight through.
//   - the *drawing order*: the modal is the last child, so it is painted last
//     and hit tested first.
//
// What it does not do: it does not stop the content underneath from
// *rebuilding*, laying out, painting or running its own timers. A modal is a
// layer, not a pause button, and an application that has to suspend work while
// an alert is open suspends it itself.
//
// # The content underneath stays mounted and is still drawn
//
// Unlike an inactive tab — see [TabBarView] — the content below a modal is not
// hidden. It has to be visible: that is what a scrim over it is for. So the
// full frame cost of the application is still paid while a modal is open, and
// an indeterminate [ProgressBarView] in the content behind an alert still
// holds the device at full tick rate. That is the correct behaviour, because
// the bar is on the screen and is still spinning; it is written down because
// it is the one place in navigation where the frame cost is *not* avoided.
//
// # Nil is the closed state
//
//	ui.Modal(screen, nil)
//
// costs one node, no scrim, no trap, and paints exactly what screen paints.
// That is the shape an application uses: it builds the modal view or nil from
// its own state, and the presence of the value *is* the presentation.
type ModalView struct {
	base
	content gift.View
	modal   gift.View
	// onDismiss is called when the scrim is tapped, or nil when a tap
	// outside the modal should do nothing.
	onDismiss func()
	// scrim is the wash colour; the zero value means [defaultScrimColor].
	scrim    Color
	hasScrim bool
}

// Modal returns content with modal presented on top of it. A nil modal is the
// closed state and is the normal way to express "no alert right now"; see
// [ModalView].
//
// content must not be nil.
func Modal(content, modal gift.View) ModalView {
	if content == nil {
		panic("gift/ui: Modal with a nil content view")
	}
	return ModalView{content: content, modal: modal}
}

// ViewType implements gift.View.
func (m ModalView) ViewType() gift.TypeID { return modalType }

// Build implements gift.View.
func (m ModalView) Build(bc *gift.BuildContext) gift.Element {
	if m.modal == nil {
		// The closed case, and it is deliberately not "a ZStack with one
		// child and an invisible scrim". A scrim that is mounted and
		// transparent is still an interactive node covering the window, and
		// the day somebody forgets to make it decline events the whole
		// application stops responding. Closed means absent.
		return ZStack(m.content).Key(m.key).Flex(m.flex).Build(bc)
	}
	wash := defaultScrimColor
	if m.hasScrim {
		wash = m.scrim
	}
	return ZStack(
		m.content,
		newLayer("modal", ZStack(
			scrim{color: wash, onTap: m.onDismiss},
			m.modal,
		).Align(geom.Center)).Trap(true),
	).Key(m.key).Flex(m.flex).Build(bc)
}

// --- modifiers -------------------------------------------------------------

// OnDismiss makes a tap on the scrim — anywhere outside the modal itself —
// call fn. A nil fn, the default, makes the scrim swallow the tap and do
// nothing, which is what an alert that demands an answer wants.
//
// It changes nothing about what the scrim *blocks*: the event is consumed
// either way and never reaches the content below. The only question here is
// whether it also means something.
func (m ModalView) OnDismiss(fn func()) ModalView { m.onDismiss = fn; return m }

// Scrim sets the wash drawn over the content while the modal is open. It may
// be a semantic colour and is resolved during build. [ColorClear] gives an
// invisible scrim that still blocks input, which is what a sheet that should
// not dim its background wants.
func (m ModalView) Scrim(c Color) ModalView { m.scrim, m.hasScrim = c, true; return m }

// Key sets the reconciliation key of this view among its siblings.
func (m ModalView) Key(v string) ModalView { m.setKey(v); return m }

// Flex makes the modal host take a share of the remaining main axis space of
// its parent stack.
func (m ModalView) Flex(v float32) ModalView { m.setFlex(v); return m }

// --- the scrim ---------------------------------------------------------------

// scrim is the full size input barrier under a modal.
//
// It is a [BoxView]'s element with an interactor bolted on, rather than a view
// with a painter of its own, and that is worth a sentence: a painter would be
// a new place where a colour is asked whether it is visible, which is a
// visibility gate and therefore a new entry in this package's gate inventory,
// for a rectangle [BoxView] already draws correctly.
type scrim struct {
	color Color
	onTap func()
}

// ViewType implements gift.View.
func (s scrim) ViewType() gift.TypeID { return scrimType }

// Build implements gift.View.
func (s scrim) Build(bc *gift.BuildContext) gift.Element {
	e := Box().Background(s.color).Build(bc)
	// Interactive but emphatically not focusable. A scrim in the tab order
	// would be a stop on the way round the alert's buttons that shows no ring
	// and does nothing, and pressing space on it would dismiss the alert by
	// accident.
	e.Interactor = scrimInteractor{onTap: s.onTap}
	e.Focusable = false
	return e
}

// scrimInteractor swallows everything.
//
// Returning true from the default branch is the whole mechanism and it is the
// opposite of what every other interactor in this package does. gift bubbles
// an event to the ancestors of the node it hit until somebody answers true —
// see [gift.App.deliver] — and the scrim is not above the content, it is a
// *sibling* on top of it, so what actually stops the tap is the hit test:
// [gift.App.hitNode] visits children in reverse and returns the scrim, whose
// bounds are the whole window. Answering true then stops the event from
// bubbling up to a scroll container that encloses the whole modal host, which
// would otherwise drag the page under the alert.
type scrimInteractor struct{ onTap func() }

// HandleEvent implements gift.Interactor.
func (s scrimInteractor) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	switch e.Kind {
	case gift.EventPointerDown:
		// The focus goes nowhere. gift clears the focus on a press that
		// nothing claimed; claiming the press and not asking for the focus
		// leaves it where it was, which for a modal is inside the modal.
		return true
	case gift.EventPointerUp:
		if e.Inside && !e.Dragged && s.onTap != nil {
			s.onTap()
		}
		return true
	}
	// Moves, wheels, cancels and keys. A key event never arrives here — the
	// scrim is not focusable and nothing below it can be focused while the
	// trap is in force — but a wheel does, and letting a wheel through would
	// scroll the page behind the alert.
	_ = ctx
	return true
}

// --- the alert ----------------------------------------------------------------

// AlertButton is one choice in an [AlertView]. It is created by
// [AlertAction], [AlertCancel] or [AlertDestructive].
type AlertButton struct {
	title  string
	action func()
	role   alertRole
}

type alertRole uint8

const (
	// roleDefault is the ordinary affirmative choice.
	roleDefault alertRole = iota
	// roleCancel backs out. It is drawn in the plain label colour, because
	// the accent is the colour of the thing the user is most likely to want
	// and "cancel" is not it.
	roleCancel
	// roleDestructive deletes something. It is drawn in [ColorDanger].
	roleDestructive
)

// AlertAction is the ordinary choice of an alert, drawn in [ColorAccent].
func AlertAction(title string, fn func()) AlertButton {
	return AlertButton{title: title, action: fn}
}

// AlertCancel is the choice that backs out, drawn in [ColorLabel].
func AlertCancel(title string, fn func()) AlertButton {
	return AlertButton{title: title, action: fn, role: roleCancel}
}

// AlertDestructive is the choice that destroys something, drawn in
// [ColorDanger].
func AlertDestructive(title string, fn func()) AlertButton {
	return AlertButton{title: title, action: fn, role: roleDestructive}
}

// AlertView is a card with a title, a message and a row of choices, meant to
// be handed to [Modal]. It is created by [Alert]; the zero value is not
// useful.
//
//	ui.Modal(screen, ui.Alert("Delete the file?", "This cannot be undone.",
//		ui.AlertCancel("Cancel", cancel),
//		ui.AlertDestructive("Delete", del),
//	))
//
// # It is an ordinary view and knows nothing about being modal
//
// Every modal property — the scrim, the focus trap, the drawing order — is
// [ModalView]'s and none of it is here. An alert placed in a [ZStack] by hand
// is a card on the screen that anything can be tapped around, and that is the
// honest consequence of keeping the two apart rather than a trap: the
// presentation is a separate decision from the content, and [Modal] takes any
// view at all, so a bottom sheet is
//
//	ui.Modal(screen, ui.VStack(rows...).Background(ui.ColorSurface)).Scrim(ui.ColorClear)
//
// and needs no type of its own.
//
// # The buttons are a row and stay a row
//
// Three choices is the practical maximum and two is the common case. There is
// no vertical fallback for long titles: an alert whose buttons do not fit is
// an alert with too much text on its buttons, and a layout that silently
// turned ninety degrees would hide that rather than showing it.
type AlertView struct {
	base
	title   string
	message string
	buttons []AlertButton
}

// Alert returns an alert card. Either of title and message may be empty, and
// an empty one draws nothing and takes no space. An alert with no buttons is
// legal and is a card the user cannot answer; it is the caller's business,
// because [ModalView.OnDismiss] may be the answer.
//
// The buttons slice belongs to gift from this call onwards.
func Alert(title, message string, buttons ...AlertButton) AlertView {
	return AlertView{title: title, message: message, buttons: buttons}
}

// ViewType implements gift.View.
func (a AlertView) ViewType() gift.TypeID { return alertType }

// Build implements gift.View.
func (a AlertView) Build(bc *gift.BuildContext) gift.Element {
	rows := make([]gift.View, 0, 3)
	if a.title != "" {
		rows = append(rows, Text(a.title).
			FontSize(alertTitleSize).
			Foreground(ColorLabel).
			Align(AlignCenter).
			Key("title"))
	}
	if a.message != "" {
		rows = append(rows, Text(a.message).
			FontSize(alertMessageSize).
			Foreground(ColorSecondaryLabel).
			Align(AlignCenter).
			Key("message"))
	}
	if len(a.buttons) > 0 {
		cols := make([]gift.View, len(a.buttons))
		for i, b := range a.buttons {
			cols[i] = a.button(i, b)
		}
		rows = append(rows, HStack(cols...).Gap(8).Key("buttons"))
	}

	return VStack(rows...).
		Gap(12).
		// The rows are centred on the cross axis, which is what puts a short
		// title in the middle of the card. TextView.Align centres the *lines
		// inside the text node*, and a text node in a stack is only as wide
		// as its longest line, so without this a one line title would be
		// centred inside itself and left aligned inside the card.
		Align(geom.Center).
		Padding(alertPadding).
		Frame(alertWidth, geom.Unbounded()).
		Background(ColorSurface).
		CornerRadius(alertRadius).
		Border(Border{Width: 1, Color: ColorSeparator}).
		Shadow(Shadow{Blur: 24, OffsetY: 8, Color: Fade(ColorLabel, 0.25)}).
		Key(a.key).
		Flex(a.flex).
		Build(bc)
}

func (a AlertView) button(i int, b AlertButton) gift.View {
	fg := ColorAccent
	switch b.role {
	case roleCancel:
		fg = ColorLabel
	case roleDestructive:
		fg = ColorDanger
	}
	return Button(
		Text(b.title).FontSize(alertTitleSize).Foreground(fg).MaxLines(1),
		b.action,
	).
		Key("alert-button-" + strconv.Itoa(i)).
		Flex(1).
		MinHeight(ControlHitTarget).
		MinWidth(ControlHitTarget).
		Align(geom.Center).
		Label(b.title)
}

// Key sets the reconciliation key of this view among its siblings.
func (a AlertView) Key(v string) AlertView { a.setKey(v); return a }

// Flex makes the alert take a share of the remaining main axis space of its
// parent stack. Inside a [ModalView] there is no remainder to take and this
// does nothing.
func (a AlertView) Flex(v float32) AlertView { a.setFlex(v); return a }
