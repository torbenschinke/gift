//go:build giftdebug

package gift_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// TestStateFromOtherGoroutinePanics lives here because the check lives here.
//
// Obtaining a goroutine id requires parsing the runtime stack header, which
// costs microseconds and allocates. An event handler is inside the zero
// allocation contract of the frame path, so the check cannot be on that path
// in a release build; see the project plan, section 11. Without the tag, the
// race detector is the tool for this class of bug, which is why there is also
// a race test for it.
func TestStateFromOtherGoroutinePanics(t *testing.T) {
	var st *gift.State[int]
	root := func(ctx *gift.Context) gift.View {
		st = ctx.State("v", 0)
		return box{w: 1, h: 1}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)

	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		st.Set(1)
	}()
	r := <-done
	msg, _ := r.(string)
	if !strings.Contains(msg, "UI executor") {
		t.Fatalf("panic = %v, want a UI executor diagnosis", r)
	}
}

// TestDuplicateSiblingKeysAreDiagnosed covers the requirement of the project
// plan, section 5, that dynamic children carry stable model keys. Duplicates
// do not fail loudly on their own: the matcher falls back to matching the
// duplicates by their position among each other, which looks like it works
// until the list is reordered and state follows the wrong row.
func TestDuplicateSiblingKeysAreDiagnosed(t *testing.T) {
	root := func(ctx *gift.Context) gift.View {
		return stack{children: []gift.View{
			box{key: "same", w: 1, h: 1},
			box{key: "same", w: 2, h: 2},
		}}
	}

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, `share the key "same"`) {
			t.Fatalf("panic = %v, want a duplicate key diagnosis", r)
		}
	}()
	a := gift.New(gift.Options{Root: root})
	_ = a.Update(viewport())
	t.Fatal("want panic")
}

// TestOverflowIsDiagnosed covers the giftdebug half of rule 3 of the project
// plan, section 7: the counters say how much, the debug build says where.
//
// It lives here because the diagnosis lives here. Naming a node means
// formatting a string, which allocates, and the frame path of a release build
// allocates nothing; see the project plan, section 11. The release
// counterpart of diagnoseOverflow compiles to nothing at all.
func TestOverflowIsDiagnosed(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	over := geom.Sz(30, 12)
	a := gift.New(gift.Options{Logger: log, Root: func(*gift.Context) gift.View {
		return overflowView{key: "panel", over: over, w: 100, h: 100}
	}})
	mustUpdate(t, a)

	got := buf.String()
	for _, want := range []string{"overflows", "key:panel", "30", "12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the diagnosis does not mention %q:\n%s", want, got)
		}
	}

	// It is emitted on the transition and not once per frame, or a permanently
	// overflowing scene would drown the log and allocate on every frame.
	buf.Reset()
	for range 4 {
		a.Invalidate()
		mustUpdate(t, a)
	}
	if s := buf.String(); s != "" {
		t.Fatalf("the diagnosis was repeated while nothing changed:\n%s", s)
	}

	// And it says so again when the overflow goes away.
	over = geom.Size{}
	a.Invalidate()
	mustUpdate(t, a)
	if s := buf.String(); !strings.Contains(s, "no longer overflows") {
		t.Fatalf("the recovery was not reported:\n%s", s)
	}
}
