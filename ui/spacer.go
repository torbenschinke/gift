package ui

import (
	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

var spacerType = gift.RegisterType("ui.Spacer")

// SpacerView is a flexible, invisible gap. It is created by [Spacer].
//
// It has no style modifiers at all, which is the point of variant A: a spacer
// that accepted Background and then ignored it would be a lie. It has a flex
// of one by default, a minimum length of zero and draws nothing.
type SpacerView struct {
	key  string
	flex float32
	min  float32
}

// Spacer returns a flexible gap with a flex of one and no minimum length.
func Spacer() SpacerView { return SpacerView{flex: 1} }

// ViewType implements gift.View.
func (s SpacerView) ViewType() gift.TypeID { return spacerType }

// Build implements gift.View.
func (s SpacerView) Build(*gift.BuildContext) gift.Element {
	l := zeroSpacer
	if s.min != 0 {
		l = &spacerNode{min: s.min}
	}
	return gift.Element{Key: s.key, Flex: s.flex, Layouter: l}
}

// Flex sets the share of the remaining main axis space this spacer takes.
// Two spacers with flex 1 and 3 split the free space one to three.
func (s SpacerView) Flex(v float32) SpacerView { s.flex = v; return s }

// MinLength sets the smallest main axis extent of the spacer.
//
// It is honoured even when the stack has no space left, including in an
// unbounded axis, where the stack offers every flexible child a tight extent
// of zero. The spacer then deliberately returns more than its constraints
// allow and pushes its siblings along, which gift passes through unchanged;
// see [gift.Layouter]. A spacer that silently collapsed to zero would be the
// worse surprise.
func (s SpacerView) MinLength(v float32) SpacerView { s.min = v; return s }

// Key sets the reconciliation key of this view among its siblings.
func (s SpacerView) Key(v string) SpacerView { s.key = v; return s }

// zeroSpacer is the layouter of every spacer without a minimum length, which
// is the common case. It carries no state, so one shared instance is enough
// and a build does not allocate for it.
var zeroSpacer = &spacerNode{}

// spacerNode is the layouter of a spacer. It takes exactly the minimum its
// constraints ask for, which is the extent a stack assigned to it.
type spacerNode struct{ min float32 }

// Layout returns the minimum of the constraints, raised to the minimum length
// along the main axis.
//
// The main axis is recognised as the tight one: a stack measures a flexible
// child with a tight main axis and a loose cross axis, which is exactly the
// information a spacer needs and the reason it does not have to know whether
// it sits in a VStack or an HStack. If both axes or neither are tight, there
// is no main axis to speak of and the minimum length is not applied.
func (s *spacerNode) Layout(_ *gift.LayoutContext, c geom.Constraints) geom.Size {
	w, h := c.Min.W, c.Min.H
	if s.min > 0 {
		tightW := c.Min.W == c.Max.W
		tightH := c.Min.H == c.Max.H
		switch {
		case tightH && !tightW:
			if s.min > h {
				h = s.min
			}
		case tightW && !tightH:
			if s.min > w {
				w = s.min
			}
		}
	}
	return geom.Sz(w, h)
}
