package ui

import (
	"github.com/torbenschinke/gift"
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
	return e
}
