package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/layout"
)

var layerType = gift.RegisterType("ui.layer")

// layer is the one node type the three navigation containers of this file
// share: a full size box around one caller supplied view that can be taken out
// of the frame without being taken out of the tree, and that can confine the
// keyboard focus to itself.
//
// It is unexported on purpose. The two flags it carries,
// [gift.Element.Hidden] and [gift.Element.FocusTrap], are not modifiers an
// application should reach for on an arbitrary view: hiding a subtree that
// still lays itself out, still holds its state and still rebuilds when its
// state changes is a correct thing for a tab bar to do to its inactive tabs
// and a confusing thing for anybody else to do to anything else. The moment
// there is a second legitimate caller the decision can be revisited; until
// then, the exported surface is [TabBarView], [NavigationStackView] and
// [ModalView], and what they do to their children is their documented
// behaviour rather than a knob.
//
// Its layouter is [node.layoutLayer]: it fills the area it is given and
// stretches its one child to fill it too, which is what "a screen" means.
type layer struct {
	base
	hidden bool
	trap   bool
	// parked is where this layer sits while it is hidden, as a fraction of
	// its own size; see [gift.TransitionSpec]. The zero value is the hard cut
	// every navigation container performed before transitions existed.
	parked geom.Point
	// entry is where it comes from when it is pushed and ret is where it
	// comes back from when it is uncovered; the zero values mean parked.
	entry geom.Point
	ret   geom.Point
	// kids is a one element array rather than a slice literal so that
	// building a layer allocates the node and nothing else; see
	// [buttonNode.kids].
	kids [1]gift.View
}

// newLayer wraps child. A nil child is a programming error in this package and
// not something an application can cause.
func newLayer(key string, child gift.View) layer {
	l := layer{}
	l.setKey(key)
	l.kids[0] = child
	return l
}

// Hidden takes the layer out of paint, hit testing and the focus order; see
// [gift.Element.Hidden].
func (l layer) Hidden(v bool) layer { l.hidden = v; return l }

// Trap confines the keyboard focus to this layer; see
// [gift.Element.FocusTrap].
func (l layer) Trap(v bool) layer { l.trap = v; return l }

// Parked puts this layer in motion when it is hidden or shown: p is where it
// waits while hidden, as a fraction of its own size. See [gift.TransitionSpec]
// for the model, and [TabBarView] for what the three containers of this
// package pass and why.
//
// The duration is [ControlAnimation] and is not a parameter, for the reason
// that constant is not one: a tab switch that takes a different time from the
// switch of a toggle in the tab it arrives at is two designs in one window.
func (l layer) Parked(p geom.Point) layer { l.parked = p; return l }

// Entry is where this layer comes from when it is mounted into an application
// that is already on the screen — a navigation push. The zero value means the
// same place [layer.Parked] names; see [gift.TransitionSpec].
func (l layer) Entry(p geom.Point) layer { l.entry = p; return l }

// Return is where this layer comes back from when it is shown again after
// having been hidden. The zero value means "from where it went"; see
// [gift.TransitionSpec].
func (l layer) Return(p geom.Point) layer { l.ret = p; return l }

// ViewType implements gift.View.
func (l layer) ViewType() gift.TypeID { return layerType }

// Build implements gift.View.
func (l layer) Build(*gift.BuildContext) gift.Element {
	// The array belongs to the value receiver's own copy, which is a fresh
	// one per call, so the slice handed to gift is never shared with a second
	// element. That is the ownership rule of the project plan, section 4, and
	// the reason this is not a package level scratch buffer.
	e := element(l.base, kindLayer, 0, layout.Vertical, layout.CrossAlignPosition, l.kids[:])
	e.Hidden = l.hidden
	e.FocusTrap = l.trap
	// A duration and no offset is not the same thing as no transition: it is
	// "stay on the screen, where you are, until the layer that is covering
	// you has arrived". The navigation stack relies on it for the screen
	// underneath a push; see [NavigationStackView].
	e.Transition = gift.TransitionSpec{
		Parked:   l.parked,
		Entry:    l.entry,
		Return:   l.ret,
		Duration: ControlAnimation,
	}
	return e
}
