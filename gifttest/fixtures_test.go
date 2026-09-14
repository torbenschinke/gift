package gifttest_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/gifttest"
	"github.com/torbenschinke/gift/ui"
)

// TestMain is the one line a package with golden tests writes. Without the
// giftgpu tag it is an ordinary m.Run; with it, the test binary runs inside
// Ebitengine's loop so that pixels can be read back. See [gifttest.Main].
func TestMain(m *testing.M) {
	loadFont()
	gifttest.Main(m)
}

// The tests share the Roboto that internal/text keeps in its testdata, for the
// same reason ui's tests do: gift ships no font and a second copy in the tree
// would be the first step towards one.
const testFontPath = "../internal/text/testdata/Roboto-Regular.ttf"

var fontOnce sync.Once

func loadFont() {
	fontOnce.Do(func() {
		data, err := os.ReadFile(testFontPath)
		if err != nil {
			panic(fmt.Sprintf("gifttest tests: reading %s: %v", testFontPath, err))
		}
		f, err := ui.LoadFont(data)
		if err != nil {
			panic(fmt.Sprintf("gifttest tests: parsing %s: %v", testFontPath, err))
		}
		ui.SetDefaultFont(f)
	})
}

// counter is the view from the project plan, section 4, with the two
// refinements that make it a control: the minus button is disabled at zero,
// and every part that a test refers to carries a key.
//
// It is deliberately the same program as cmd/example-layout, minus the
// colours. A harness that only works against fixtures written for it is not a
// harness.
func counter(ctx *gift.Context) gift.View {
	count := ctx.State("count", 0)
	n := ctx.Read(count)

	return ui.VStack(
		ui.Text("Counter").FontSize(20),
		ui.Text(strconv.Itoa(n)).Key("count").FontSize(32),
		ui.HStack(
			ui.Button(ui.Text("-"), func() { count.Set(count.Get() - 1) }).
				Key("minus").Frame(56, 44).Disabled(n == 0),
			ui.Button(ui.Text("+"), func() { count.Set(count.Get() + 1) }).
				Key("plus").Frame(56, 44),
		).Gap(8),
	).Gap(16).Padding(24)
}

// --- a recorder that stands in for *testing.T -------------------------------
//
// The failure messages of this package are part of its contract: a selector
// that matches two nodes has to say which two, and a view that rebuilds
// forever has to be diagnosed rather than hang. Asserting that needs a TB the
// test controls, which is exactly why the harness takes [gifttest.TB] and not
// testing.TB.

// fatal is what a recorder's Fatalf panics with, so that it does not return —
// the contract [gifttest.TB] states.
type fatal struct{ msg string }

// recorder records failures instead of reporting them.
type recorder struct {
	errors []string
	fatals []string
	skips  []string
	logs   []string
}

func (r *recorder) Helper() {}
func (r *recorder) Name() string {
	return "recorder"
}
func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}
func (r *recorder) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.fatals = append(r.fatals, msg)
	panic(fatal{msg})
}
func (r *recorder) Skipf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.skips = append(r.skips, msg)
	panic(fatal{msg})
}
func (r *recorder) Log(args ...any) {
	r.logs = append(r.logs, fmt.Sprint(args...))
}

// all is every message the recorder saw, for a substring assertion.
func (r *recorder) all() string {
	return strings.Join(append(append(append([]string{}, r.errors...), r.fatals...), r.skips...), "\n")
}

// capture runs fn against a recorder and returns what it reported, swallowing
// the panic a Fatalf raises.
func capture(fn func(t gifttest.TB)) *recorder {
	r := &recorder{}
	func() {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(fatal); !ok {
					panic(v)
				}
			}
		}()
		fn(r)
	}()
	return r
}

// wantContains fails unless got contains every fragment.
func wantContains(t *testing.T, what, got string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(got, f) {
			t.Errorf("%s does not mention %q. It said:\n%s", what, f, got)
		}
	}
}

// pt is a short geom.Pt for the few tests whose subject really is a
// coordinate.
func pt(x, y float32) geom.Point { return geom.Pt(x, y) }

// geomSz is a short geom.Sz for the size assertions.
func geomSz(w, h float32) geom.Size { return geom.Sz(w, h) }
