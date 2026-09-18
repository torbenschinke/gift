//go:build giftdebug

package gift_test

import (
	"strings"
	"testing"

	"github.com/worldiety/gift"
)

// sharedChildren is the mistake this check exists for: a caller that keeps a
// buffer and hands the same slice to gift on every build.
var sharedChildren = []gift.View{box{w: 1, h: 1}}

type hoarder struct{}

func (hoarder) ViewType() gift.TypeID { return stackType }

func (hoarder) Build(*gift.BuildContext) gift.Element {
	return gift.Element{
		Layouter: stackLayout{},
		Painter:  fillPainter{},
		Children: sharedChildren,
	}
}

func TestOwnershipViolationIsDetected(t *testing.T) {
	root := func(ctx *gift.Context) gift.View { return hoarder{} }
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	// The caller keeps writing into the slice it gave away.
	sharedChildren[0] = other{}

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "handed to gift") {
			t.Fatalf("panic = %v, want an ownership diagnosis", r)
		}
	}()
	a.Invalidate()
	_ = a.Update(viewport())
	t.Fatal("want panic")
}
