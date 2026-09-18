//go:build giftdebug

package ui_test

import (
	"strings"
	"testing"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/ui"
)

// buf is the mistake the ownership check exists for: a reused buffer that is
// handed to ui.VStack with ... and then written to again. The project plan,
// section 4, gives the slice to gift at that moment.
var buf = []gift.View{ui.Box().Frame(1, 1)}

func TestOwnershipViolationThroughUIIsDetected(t *testing.T) {
	a := gift.New(gift.Options{Root: func(*gift.Context) gift.View {
		return ui.VStack(buf...)
	}})
	if err := a.Update(geom.Sz(100, 100)); err != nil {
		t.Fatal(err)
	}

	// The caller keeps writing into the slice it gave away.
	buf[0] = ui.Spacer()

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "handed to gift") {
			t.Fatalf("panic = %v, want an ownership diagnosis", r)
		}
	}()
	a.Invalidate()
	_ = a.Update(geom.Sz(100, 100))
	t.Fatal("want panic")
}
