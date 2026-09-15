package ebiten

import (
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// The keyboard half of the bridge, driven through the two function values the
// type comment describes.
//
// # What these tests can and cannot prove
//
// They cannot change the host's keyboard layout, and no test in any language
// can: the translation from a key press to a character happens in the window
// system, above Ebitengine and far above this package. A test that claimed to
// switch to a German layout would be a test of its own fake.
//
// What they do instead is exercise the boundary honestly. The seam feeds
// characters that deliberately do not correspond to the key codes being
// reported as pressed — the platform saying "the key in the A position was
// pressed and it produced 'ä'" — and the assertion is that the bridge
// forwards both, unchanged, on their own channels. That is the whole of what
// this file is responsible for; whether the platform is telling the truth is
// the platform's business.

// keyboardApp is an application with one focusable node that records every
// event it sees, ready to be typed into.
type keyboardApp struct {
	app    *gift.App
	events []gift.Event

	// quiet makes the node count events instead of recording them.
	//
	// It exists for the benchmark and nothing else. Recording grows a slice
	// per delivered event, which would be measured as an allocation of the
	// input path and is an allocation of the test's own bookkeeping. Counting
	// still proves the events arrive — see delivered — without adding a byte
	// per operation to the number the project plan, section 11, is about.
	quiet     bool
	delivered int
}

var recorderType = gift.RegisterType("backend.test.Recorder")

type recorderView struct{ app *keyboardApp }

func (recorderView) ViewType() gift.TypeID { return recorderType }

func (v recorderView) Build(*gift.BuildContext) gift.Element {
	n := &recorderNode{app: v.app}
	return gift.Element{Key: "field", Layouter: n, Interactor: n, Focusable: true}
}

type recorderNode struct{ app *keyboardApp }

func (n *recorderNode) Layout(*gift.LayoutContext, geom.Constraints) geom.Size {
	return geom.Sz(100, 40)
}

func (n *recorderNode) HandleEvent(_ *gift.EventContext, e gift.Event) bool {
	if n.app.quiet {
		n.app.delivered++
		return false
	}
	n.app.events = append(n.app.events, e)
	return false
}

// newKeyboardApp builds the application and focuses its single node.
//
// It takes a [testing.TB] rather than a *testing.T because the benchmark needs
// the very same thing a test does, and needs it for a reason the benchmark
// used to get wrong: without the focus, [gift.App.TypeRune] drops every rune
// at the "is the focused node still valid" guard and a measurement of "the
// dispatch behind the bridge" measures an early return.
func newKeyboardApp(tb testing.TB) *keyboardApp {
	tb.Helper()
	ka := &keyboardApp{}
	ka.app = gift.New(gift.Options{Root: func(*gift.Context) gift.View { return recorderView{app: ka} }})
	if err := ka.app.Update(geom.Sz(200, 200)); err != nil {
		tb.Fatal(err)
	}
	ka.app.Paint()
	ka.app.BeginInput(0)
	ka.app.KeyDown(gift.KeyTab, 0)
	ka.app.KeyUp(gift.KeyTab, 0)
	if _, ok := ka.app.Focus(); !ok {
		tb.Fatalf("nothing took the focus; nothing typed below would be delivered")
	}
	ka.events = ka.events[:0]
	return ka
}

// text returns the characters the node received.
func (k *keyboardApp) text() string {
	var out []rune
	for _, e := range k.events {
		if e.Kind == gift.EventRune {
			out = append(out, e.Rune)
		}
	}
	return string(out)
}

// keys returns the key presses the node received.
func (k *keyboardApp) keys() []gift.Key {
	var out []gift.Key
	for _, e := range k.events {
		if e.Kind == gift.EventKeyDown {
			out = append(out, e.Key)
		}
	}
	return out
}

// scriptedBridge is a bridge whose two keyboard readings come from the test.
// pressed is the set of physical keys the platform reports as down; chars is
// what it reports as typed this tick.
func scriptedBridge(app *gift.App, pressed map[eb.Key]bool, chars *[]rune) *inputBridge {
	b := newInputBridge(app)
	b.keyPressed = func(k eb.Key) bool { return pressed[k] }
	b.appendChars = func(dst []rune) []rune {
		// Exactly what Ebitengine's own AppendInputChars does: one append of
		// the tick's runes onto the slice it was given.
		return append(dst, (*chars)...)
	}
	return b
}

// TestCharactersDoNotComeFromKeyCodes is the defect the project plan, section
// 19, names: before the rune channel, no printable character reached gift at
// all, and the obvious wrong fix is to map key codes to letters.
//
// The script is a German keyboard as the platform describes it: the physical
// key in the US A position is down, and the character it produced is 'ä'. A
// bridge that derived text from its key table would deliver 'a'; a bridge that
// forwarded only keys would deliver nothing. Both fail here.
func TestCharactersDoNotComeFromKeyCodes(t *testing.T) {
	ka := newKeyboardApp(t)
	pressed := map[eb.Key]bool{eb.KeyA: true}
	chars := []rune{'ä'}
	b := scriptedBridge(ka.app, pressed, &chars)

	ka.app.BeginInput(0)
	b.pollKeys()
	b.pollRunes()

	if got := ka.text(); got != "ä" {
		t.Fatalf("the platform reported 'ä' and the view received %q", got)
	}
	if got := ka.keys(); len(got) != 1 || got[0] != gift.KeyA {
		t.Fatalf("the key channel delivered %v, want exactly the physical key A", got)
	}
	// And the two really are independent: the same key stays down, the
	// platform reports a different character, and that character arrives.
	ka.events = ka.events[:0]
	chars = []rune{'Ä'}
	ka.app.BeginInput(0)
	b.pollKeys()
	b.pollRunes()
	if got := ka.text(); got != "Ä" {
		t.Fatalf("the second character arrived as %q, want %q", got, "Ä")
	}
	if got := ka.keys(); len(got) != 0 {
		t.Fatalf("a held key produced a second press %v; the bridge is an edge detector and the "+
			"repeat belongs to gift", got)
	}
}

// TestAltGrAndDeadKeysPassThrough covers the rest of a Latin keyboard, all of
// which the project plan, section 14, is explicit about not being an IME.
//
// The AltGr case is the one that would survive a naive implementation by
// accident and then break: the alt modifier is genuinely held while the
// character arrives, and gift must not filter the character out because of it.
func TestAltGrAndDeadKeysPassThrough(t *testing.T) {
	ka := newKeyboardApp(t)
	// AltGr arrives as control plus alt on X11 and as alt on macOS; both are
	// held here, which is the harder case.
	pressed := map[eb.Key]bool{eb.KeyAlt: true, eb.KeyControl: true, eb.KeyQ: true}
	chars := []rune{'@'}
	b := scriptedBridge(ka.app, pressed, &chars)

	ka.app.BeginInput(0)
	b.pollKeys()
	b.pollRunes()
	if got := ka.text(); got != "@" {
		t.Fatalf("an AltGr character arrived as %q, want %q; gift must not filter characters by "+
			"modifier, because the platform already did", got, "@")
	}

	// A dead key: the first tick produces no character at all, the second
	// produces the composed one. The key itself is not in gift's table and
	// therefore not reported as a key either, which is correct — there is
	// nothing for a view to do with "the acute accent key went down".
	ka.events = ka.events[:0]
	pressed = map[eb.Key]bool{}
	chars = nil
	ka.app.BeginInput(0)
	b.pollKeys()
	b.pollRunes()
	if got := ka.text(); got != "" {
		t.Fatalf("a dead key produced %q; the composition is not finished yet", got)
	}
	chars = []rune{'é'}
	ka.app.BeginInput(0)
	b.pollRunes()
	if got := ka.text(); got != "é" {
		t.Fatalf("the resolved composition arrived as %q, want %q", got, "é")
	}
	for _, e := range ka.events {
		if e.Kind == gift.EventRune {
			t.Logf("received %q = U+%04X", e.Rune, e.Rune)
		}
	}
}

// TestPollRunesDeliversEveryCharacterOfATick pins that the buffer is drained
// in order and that reusing it does not carry characters into the next tick —
// the classic defect of a slice that is appended to but never truncated,
// which would retype the whole session on every frame.
func TestPollRunesDeliversEveryCharacterOfATick(t *testing.T) {
	ka := newKeyboardApp(t)
	chars := []rune("straße")
	b := scriptedBridge(ka.app, map[eb.Key]bool{}, &chars)

	ka.app.BeginInput(0)
	b.pollRunes()
	if got := ka.text(); got != "straße" {
		t.Fatalf("a tick with six characters delivered %q", got)
	}

	ka.events = ka.events[:0]
	chars = nil
	ka.app.BeginInput(0)
	b.pollRunes()
	if got := ka.text(); got != "" {
		t.Fatalf("a tick with nothing typed delivered %q; the reused buffer was not truncated", got)
	}
}

// TestShortcutLettersAreKeysAndNotText is the counterpart of the first test
// and the reason the five letters are in the key table at all. Ebitengine's
// character callback drops the character of a shortcut combination, so the
// script is "control-C is down and no character was produced": the view must
// see the key with the modifier and no text whatsoever.
func TestShortcutLettersAreKeysAndNotText(t *testing.T) {
	ka := newKeyboardApp(t)
	pressed := map[eb.Key]bool{eb.KeyControl: true, eb.KeyC: true}
	var chars []rune
	b := scriptedBridge(ka.app, pressed, &chars)

	ka.app.BeginInput(0)
	ka.app.SetModifiers(b.modifiers())
	b.pollKeys()
	b.pollRunes()

	if got := ka.text(); got != "" {
		t.Fatalf("the copy shortcut inserted %q into the view", got)
	}
	found := false
	for _, e := range ka.events {
		if e.Kind == gift.EventKeyDown && e.Key == gift.KeyC && e.Mods.Has(gift.ModControl) {
			found = true
		}
	}
	if !found {
		t.Fatalf("control-C did not reach the view as a modified key press, got %v", ka.events)
	}
}

// BenchmarkPollRunes is the allocation measurement the project plan, section
// 11, requires for the frame path, with characters actually flowing.
//
// The stand in appends exactly the way Ebitengine's AppendInputChars does, so
// what is measured is the bridge's own handling of its reused buffer and the
// dispatch behind it. The real function is measured separately below; see the
// comment on inputBridge.runes for why neither measurement alone is honest.
//
// # It has to be focused, and it did not use to be
//
// The application is built by newKeyboardApp, which focuses the node. Until
// WU-AA this benchmark built a throwaway application of its own and never
// focused anything, so every rune died at the focus guard in App.deliverKey
// and the loop measured six calls to a function that returns immediately. The
// number was the same and the sentence above it was false. The assertion after
// the loop is what stops that from coming back silently: a benchmark that
// delivers nothing now says so.
//
// The node counts instead of recording, because the slice a recorder grows
// would be measured here and is the benchmark's own bookkeeping rather than
// gift's.
func BenchmarkPollRunes(b *testing.B) {
	ka := newKeyboardApp(b)
	ka.quiet = true
	chars := []rune("straße")
	br := scriptedBridge(ka.app, map[eb.Key]bool{}, &chars)
	ka.app.BeginInput(0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		br.pollRunes()
	}
	b.StopTimer()
	if want := b.N * len(chars); ka.delivered != want {
		b.Fatalf("the view received %d events for %d runes; this benchmark is not measuring "+
			"the dispatch it claims to measure", ka.delivered, want)
	}
}

// BenchmarkAppendInputChars measures the real Ebitengine function with the
// buffer the bridge gives it.
//
// Outside a running game loop there are no characters to report, so this
// proves only that the empty case allocates nothing — which is the case that
// runs on almost every tick of a real application, and is not the interesting
// one. The interesting one cannot be produced by a test binary: nothing here
// can press a key on the host keyboard. What can be checked without running is
// the implementation, and it is a single append onto the caller's slice, so
// the documented promise that "giving a slice that already has enough capacity
// works efficiently" holds by construction.
func BenchmarkAppendInputChars(b *testing.B) {
	buf := make([]rune, 0, 16)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf = eb.AppendInputChars(buf[:0])
	}
}
