package gift_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
)

// The rune channel and the key repeat, from the runtime's side.
//
// The two subjects of this file are one theme: a keyboard produces *keys* and
// it produces *characters*, and they are not the same information. Everything
// here is written against that boundary, because the defect the project plan,
// section 19, describes is precisely a framework that had only one of the two
// channels and could therefore not tell them apart.
//
// What this file cannot do is press a key on the host keyboard, let alone on a
// German one. It drives [gift.App] through the same two entry points a backend
// drives it through, [gift.App.KeyDown] and [gift.App.TypeRune], and the
// question "does the platform really send 'ä' for that key" is answered in
// backend/ebiten's own test against the poll seam. See TestRuneAndKeyAreTwo
// Channels below for the part of that boundary the runtime itself owns.

// --- fixtures ---------------------------------------------------------------

var panelType = gift.RegisterType("test.Panel")

// panelView is a container that *does* take part in input, unlike frameView.
// It exists for the bubbling tests: a rune that the focused leaf declines has
// to travel outwards, and without an interactor on an ancestor there is
// nowhere for it to travel to.
type panelView struct {
	key      string
	w, h     float32
	events   *[]gift.Event
	consume  bool
	children []gift.View
	offsets  []geom.Point
}

func (panelView) ViewType() gift.TypeID { return panelType }

func (p panelView) Build(*gift.BuildContext) gift.Element {
	n := &panelNode{p: p}
	return gift.Element{
		Key:        p.key,
		Layouter:   n,
		Interactor: n,
		Children:   p.children,
	}
}

type panelNode struct{ p panelView }

func (n *panelNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	loose := geom.Constraints{Max: geom.Sz(geom.Unbounded(), geom.Unbounded())}
	for i := range ctx.ChildCount() {
		ctx.Measure(i, loose)
		at := geom.Pt(0, 0)
		if i < len(n.p.offsets) {
			at = n.p.offsets[i]
		}
		ctx.Place(i, at)
	}
	return geom.Sz(n.p.w, n.p.h)
}

func (n *panelNode) HandleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if n.p.events != nil {
		*n.p.events = append(*n.p.events, e)
	}
	return n.p.consume
}

// typedRunes is the text an event log received, as a string.
func typedRunes(evs []gift.Event) string {
	var out []rune
	for _, e := range evs {
		if e.Kind == gift.EventRune {
			out = append(out, e.Rune)
		}
	}
	return string(out)
}

// countKind counts the events of one kind in a log.
func countKind(evs []gift.Event, k gift.EventKind) int {
	n := 0
	for _, e := range evs {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// repeatsIn counts the synthetic presses in a log; see [gift.Event.Repeat].
func repeatsIn(evs []gift.Event) int {
	n := 0
	for _, e := range evs {
		if e.Kind == gift.EventKeyDown && e.Repeat {
			n++
		}
	}
	return n
}

// focusedTypingApp mounts a single target, focuses it with tab, and returns
// the app together with the event log of that target.
func focusedTypingApp(t *testing.T, evs *[]gift.Event) *gift.App {
	t.Helper()
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "field", w: 100, h: 40, events: evs}}}
	}
	a := newInputApp(t, root)
	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	if _, ok := a.Focus(); !ok {
		t.Fatalf("nothing took the focus; the typing tests below would prove nothing")
	}
	*evs = (*evs)[:0]
	return a
}

// typeString feeds a string through the rune channel the way a backend would,
// one character per call.
func typeString(a *gift.App, s string) {
	a.BeginInput(0)
	for _, r := range s {
		a.TypeRune(r)
	}
}

// --- the rune channel -------------------------------------------------------

// TestUmlautsSurviveEndToEnd is the test the whole work unit exists for: a
// character that is not ASCII reaches a view unchanged.
//
// It would catch any truncation of the channel to a byte — a rune passed
// through a byte, a string built with append on []byte, an Event.Rune that was
// narrowed to a uint8 — and it would catch a runtime that dropped everything
// above U+007F. The text is not decoration, and each character is here for a
// different reason: 'ä' and 'ß' are two bytes in UTF-8, 'é' is the shape a
// dead key resolves to, '€' is three bytes and is the character a
// latin-1 assumption drops, and '🧊' is outside the BMP — four bytes in UTF-8
// and a surrogate pair in UTF-16 — so it is the one that fails if anything on
// the path assumed sixteen bits per character.
//
// The last of those replaced a claim that '€' "sits above the BMP boundary".
// U+20AC is inside the BMP, so the sentence named a property the character
// does not have and the test proved something narrower than it said.
func TestUmlautsSurviveEndToEnd(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	const want = "Grüße, Ærø! Café 10 € 🧊"
	typeString(a, want)

	got := typedRunes(evs)
	if got != want {
		t.Fatalf("the view received %q, want %q", got, want)
	}
	// Print the actual runes, so that a reader of the test output sees the
	// code points and not a string that merely looks right in a terminal.
	for _, r := range got {
		t.Logf("received %q = U+%04X", r, r)
	}
	if n := countKind(evs, gift.EventRune); n != len([]rune(want)) {
		t.Fatalf("%d rune events for %d characters; the channel must be one event per character",
			n, len([]rune(want)))
	}
}

// TestDeadKeyCompositionArrivesResolved pins the contract at gift's boundary
// for a dead key: the framework never sees the composition, only its result.
//
// The composition itself happens in the platform, so this test cannot perform
// it; what it can pin, and does, is that gift makes no assumption of one rune
// per key press. Two key presses — the dead accent and then the 'e' — produce
// exactly one character, and it is the composed one. A runtime that derived
// characters from key presses, or that asserted a one to one relation
// anywhere, fails here.
func TestDeadKeyCompositionArrivesResolved(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	// The dead key. The platform reports the key press and no character at
	// all, because it is waiting for the next one.
	a.BeginInput(0)
	a.KeyDown(gift.KeyOther, 0)
	a.KeyUp(gift.KeyOther, 0)
	if n := countKind(evs, gift.EventRune); n != 0 {
		t.Fatalf("a dead key produced %d character(s); it must produce none", n)
	}

	// The next key resolves it: one press, one composed character.
	a.BeginInput(0)
	a.KeyDown(gift.KeyOther, 0)
	a.TypeRune('é')
	a.KeyUp(gift.KeyOther, 0)

	if got := typedRunes(evs); got != "é" {
		t.Fatalf("the composition delivered %q, want %q", got, "é")
	}
	t.Logf("the dead key sequence delivered %q = U+%04X", 'é', 'é')
}

// TestRuneAndKeyAreTwoChannels is the distinction stated as a test: a key
// press delivers no character and a character delivers no key press.
//
// It is the runtime half of the layout question. A test cannot change the
// host's keyboard layout, so it cannot prove that the key in the US semicolon
// position yields 'ö' on a German keyboard — that is the platform's job and
// backend/ebiten's test drives it through the poll seam with characters that
// deliberately do not match the keys being pressed. What is provable here, and
// is the thing that would actually break, is that gift keeps the two apart
// instead of synthesising one from the other.
func TestRuneAndKeyAreTwoChannels(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	a.BeginInput(0)
	a.KeyDown(gift.KeyA, gift.ShortcutModifier)
	a.KeyUp(gift.KeyA, gift.ShortcutModifier)
	if n := countKind(evs, gift.EventRune); n != 0 {
		t.Fatalf("pressing a key produced %d character(s); characters come only from the platform's "+
			"layout translation, never from a key code", n)
	}

	evs = evs[:0]
	typeString(a, "ä")
	if n := countKind(evs, gift.EventKeyDown); n != 0 {
		t.Fatalf("typing a character produced %d key event(s); a character is not a key press", n)
	}
	if got := typedRunes(evs); got != "ä" {
		t.Fatalf("typing delivered %q, want %q", got, "ä")
	}
}

// TestTypingSpaceDoesNotActivate is the same boundary from the application's
// side, and it is the one a button gets wrong.
//
// The space *bar* activates a button; the space *character* must not, or every
// text field inside a focusable container would fire its surroundings while
// the user typed a sentence. The target fixture activates on KeySpace, which
// is what a button does.
func TestTypingSpaceDoesNotActivate(t *testing.T) {
	activations := 0
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "b", w: 50, h: 50, activations: &activations}}}
	}
	a := newInputApp(t, root)
	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)

	typeString(a, "  a b ")
	if activations != 0 {
		t.Fatalf("typing spaces activated the button %d time(s); a character is not the space bar",
			activations)
	}

	a.BeginInput(0)
	a.KeyDown(gift.KeySpace, 0)
	a.KeyUp(gift.KeySpace, 0)
	if activations != 1 {
		t.Fatalf("the space bar activated the button %d time(s), want 1", activations)
	}
}

// TestRunesGoToTheFocusedNodeAndBubble pins the routing promised by the Godoc
// of [gift.EventRune]: the focused node first, then its ancestors, exactly
// like a key.
//
// It would catch a rune delivered to the hovered node, to the root, or to the
// focused node alone. The panel is the ancestor and declines to consume, so
// both see the character; the unfocused sibling must see nothing at all.
func TestRunesGoToTheFocusedNodeAndBubble(t *testing.T) {
	var leafEvents, panelEvents, otherEvents []gift.Event
	root := func(*gift.Context) gift.View {
		return frameView{w: 400, h: 400, offsets: []geom.Point{at(0, 0), at(0, 200)},
			children: []gift.View{
				panelView{key: "panel", w: 200, h: 100, events: &panelEvents,
					offsets:  []geom.Point{at(0, 0)},
					children: []gift.View{target{name: "leaf", w: 100, h: 40, events: &leafEvents}}},
				target{name: "other", w: 100, h: 40, events: &otherEvents},
			}}
	}
	a := newInputApp(t, root)

	// Focus the leaf. The panel takes part in input but did not declare
	// itself focusable, so it is not in the tab order and one tab is enough;
	// that it still receives the bubbled rune is exactly the point.
	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	leafEvents, panelEvents, otherEvents = leafEvents[:0], panelEvents[:0], otherEvents[:0]

	typeString(a, "ö")

	if got := typedRunes(leafEvents); got != "ö" {
		t.Fatalf("the focused node received %q, want %q", got, "ö")
	}
	if got := typedRunes(panelEvents); got != "ö" {
		t.Fatalf("the ancestor received %q, want %q; an unconsumed rune has to bubble like a key",
			got, "ö")
	}
	if got := typedRunes(otherEvents); got != "" {
		t.Fatalf("an unfocused node received %q; characters go to the focus and to nothing else", got)
	}
}

// TestConsumedRuneDoesNotBubble is the other half of bubbling and the reason a
// text field can live inside a container that has its own keyboard handling.
func TestConsumedRuneDoesNotBubble(t *testing.T) {
	var panelEvents []gift.Event
	root := func(*gift.Context) gift.View {
		return frameView{w: 400, h: 400, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{
				panelView{key: "panel", w: 200, h: 100, events: &panelEvents,
					offsets:  []geom.Point{at(0, 0)},
					children: []gift.View{target{name: "leaf", w: 100, h: 40, swallowEverything: true}}},
			}}
	}
	a := newInputApp(t, root)
	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	panelEvents = panelEvents[:0]

	typeString(a, "x")
	if got := typedRunes(panelEvents); got != "" {
		t.Fatalf("the ancestor received %q although the focused node consumed it", got)
	}
}

// TestUnfocusedApplicationSwallowsCharacters covers the case a kiosk sits in
// most of the time: nothing is focused, somebody types.
//
// Nothing must be delivered, and the counter must still move — that is the
// whole purpose of [gift.Diagnostics.RunesTyped]. "The keyboard does not reach
// the framework" and "the framework has nowhere to put the characters" are
// different faults with the same symptom on screen, and this is what tells
// them apart.
func TestUnfocusedApplicationSwallowsCharacters(t *testing.T) {
	var evs []gift.Event
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "field", w: 100, h: 40, events: &evs}}}
	}
	a := newInputApp(t, root)

	before := a.Diagnostics().RunesTyped
	typeString(a, "abc")
	if err := a.Update(viewport()); err != nil {
		t.Fatal(err)
	}
	if got := typedRunes(evs); got != "" {
		t.Fatalf("an unfocused application delivered %q to a node", got)
	}
	if got := a.Diagnostics().RunesTyped - before; got != 3 {
		t.Fatalf("RunesTyped moved by %d, want 3; the counter has to see characters even when "+
			"nothing is focused, or a missing focus and a missing backend look alike", got)
	}
}

// --- key repeat -------------------------------------------------------------

// TestKeyRepeatDelayThenInterval is the timing contract: nothing before
// [gift.KeyRepeatDelay], then one press per [gift.KeyRepeatInterval].
//
// It would catch a repeat that starts immediately (a held arrow key jumping
// two cells on a deliberate single press), one that never starts, and one
// whose rate is the tick rate rather than the interval. The clock is injected,
// so it costs microseconds and cannot flake.
func TestKeyRepeatDelayThenInterval(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	now := time.Duration(0)
	a.BeginInput(now)
	a.KeyDown(gift.KeyLeft, 0)
	if n := repeatsIn(evs); n != 0 {
		t.Fatalf("the press itself produced %d repeat(s); the first press is not a repeat", n)
	}

	// Just short of the delay: still nothing.
	now = gift.KeyRepeatDelay - time.Millisecond
	a.BeginInput(now)
	if n := repeatsIn(evs); n != 0 {
		t.Fatalf("%v after the press there were already %d repeat(s); the delay is %v",
			now, n, gift.KeyRepeatDelay)
	}

	// Just past it: exactly one.
	now = gift.KeyRepeatDelay
	a.BeginInput(now)
	if n := repeatsIn(evs); n != 1 {
		t.Fatalf("at the delay there were %d repeat(s), want exactly 1", n)
	}

	// Then the interval governs. Step at a 60 Hz tick, which is what a
	// backend does, and check the count after a known span.
	const ticks = 60
	const step = time.Second / 60
	for range ticks {
		now += step
		a.BeginInput(now)
	}
	want := int(time.Duration(ticks) * step / gift.KeyRepeatInterval)
	if got := repeatsIn(evs) - 1; got != want {
		t.Fatalf("one second of holding produced %d repeats after the first, want %d "+
			"(%v per repeat)", got, want, gift.KeyRepeatInterval)
	}
	for _, e := range evs {
		if e.Kind == gift.EventKeyDown && e.Repeat && e.Key != gift.KeyLeft {
			t.Fatalf("a repeat carried key %v, want the held key", e.Key)
		}
	}
	if err := a.Update(viewport()); err != nil {
		t.Fatal(err)
	}
	if got := a.Diagnostics().KeyRepeats; got == 0 {
		t.Fatalf("KeyRepeats is %d; the synthetic presses have to be counted", got)
	}
}

// TestKeyUpStopsTheRepeat is the release. Without it a held key would repeat
// forever, which is the failure mode that makes a device unusable rather than
// merely wrong.
func TestKeyUpStopsTheRepeat(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	now := time.Duration(0)
	a.BeginInput(now)
	a.KeyDown(gift.KeyBackspace, 0)
	now += gift.KeyRepeatDelay + gift.KeyRepeatInterval
	a.BeginInput(now)
	if repeatsIn(evs) == 0 {
		t.Fatalf("nothing repeated at all, so the release below would prove nothing")
	}

	a.BeginInput(now)
	a.KeyUp(gift.KeyBackspace, 0)
	after := repeatsIn(evs)

	for range 60 {
		now += time.Second / 60
		a.BeginInput(now)
	}
	if got := repeatsIn(evs); got != after {
		t.Fatalf("%d more repeat(s) arrived after the key was released", got-after)
	}
}

// TestFocusChangeCancelsTheRepeat is the rule focus.go implements, and the
// defect it prevents is the ugliest one in this file: a held backspace that
// follows the focus into the next text field and eats its contents.
func TestFocusChangeCancelsTheRepeat(t *testing.T) {
	var first, second []gift.Event
	root := func(*gift.Context) gift.View {
		return frameView{w: 400, h: 400, offsets: []geom.Point{at(0, 0), at(0, 200)},
			children: []gift.View{
				target{name: "first", w: 100, h: 40, events: &first},
				target{name: "second", w: 100, h: 40, events: &second},
			}}
	}
	a := newInputApp(t, root)
	now := time.Duration(0)
	a.BeginInput(now)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)

	a.BeginInput(now)
	a.KeyDown(gift.KeyBackspace, 0)
	now += gift.KeyRepeatDelay
	a.BeginInput(now)
	if repeatsIn(first) == 0 {
		t.Fatalf("the held key never repeated into the first node, so this test proves nothing")
	}

	// The focus moves while the key is still held down.
	a.MoveFocus(true)
	firstAfter, secondAfter := repeatsIn(first), repeatsIn(second)
	for range 60 {
		now += time.Second / 60
		a.BeginInput(now)
	}
	if got := repeatsIn(second) - secondAfter; got != 0 {
		t.Fatalf("%d repeat(s) of the still held key arrived at the newly focused node; "+
			"a held backspace must not follow the focus", got)
	}
	if got := repeatsIn(first) - firstAfter; got != 0 {
		t.Fatalf("%d repeat(s) still went to the node that lost the focus", got)
	}
}

// TestOnlyRepeatableKeysRepeat pins the exclusion list. Space and enter
// activate, tab navigates, home and end are idempotent; repeating any of them
// is a bug with visible consequences — a form submitted thirty times a second.
func TestOnlyRepeatableKeysRepeat(t *testing.T) {
	cases := []struct {
		key  gift.Key
		want bool
	}{
		{gift.KeyLeft, true}, {gift.KeyRight, true}, {gift.KeyUp, true}, {gift.KeyDown, true},
		{gift.KeyPageUp, true}, {gift.KeyPageDown, true},
		{gift.KeyBackspace, true}, {gift.KeyDelete, true},
		{gift.KeySpace, false}, {gift.KeyEnter, false}, {gift.KeyEscape, false},
		{gift.KeyTab, false}, {gift.KeyHome, false}, {gift.KeyEnd, false},
		{gift.KeyA, false}, {gift.KeyC, false}, {gift.KeyV, false}, {gift.KeyX, false},
		{gift.KeyZ, false}, {gift.KeyOther, false},
	}
	for _, c := range cases {
		var evs []gift.Event
		a := focusedTypingApp(t, &evs)
		now := time.Duration(0)
		a.BeginInput(now)
		a.KeyDown(c.key, 0)
		for range 60 {
			now += time.Second / 60
			a.BeginInput(now)
		}
		got := repeatsIn(evs) > 0
		if got != c.want {
			t.Errorf("key %v repeats=%v, want %v", c.key, got, c.want)
		}
	}
}

// TestRepeatCatchUpIsBounded covers the stalled frame: a tick that is a whole
// second late must not deliver a second's worth of backspaces at once.
func TestRepeatCatchUpIsBounded(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	a.BeginInput(0)
	a.KeyDown(gift.KeyBackspace, 0)
	// One enormous step, as a debugger breakpoint or a stalled frame
	// produces. Thirty repeats are due; only the cap may arrive.
	a.BeginInput(gift.KeyRepeatDelay + time.Second)
	// Against the constant and not against a number of its own. The bound is
	// the statement being tested, so a change to it must break this test
	// rather than slip under a threshold somebody once chose to be generous.
	if got := repeatsIn(evs); got > gift.MaxRepeatsPerTickForTest {
		t.Fatalf("a one second stall delivered %d repeats in a single tick, the cap is %d; "+
			"the catch up is supposed to be bounded, or a held backspace eats a paragraph",
			got, gift.MaxRepeatsPerTickForTest)
	}
	if repeatsIn(evs) == 0 {
		t.Fatalf("a stalled tick delivered no repeat at all; the bound must cap the backlog, " +
			"not discard the key")
	}
}

// TestUnmountUnderAHeldKeyStopsTheRepeat is the other way the focus can go
// away: the node is removed while the key is down. Nothing can be delivered,
// and nothing may be requested either — see the idle test below.
func TestUnmountUnderAHeldKeyStopsTheRepeat(t *testing.T) {
	show := true
	var evs []gift.Event
	root := func(*gift.Context) gift.View {
		kids := []gift.View{target{name: "a", w: 50, h: 50}}
		if show {
			kids = append(kids, target{name: "b", w: 50, h: 50, events: &evs})
		}
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0), at(0, 100)}, children: kids}
	}
	a := gift.New(gift.Options{Root: root})
	mustUpdate(t, a)
	a.Paint()

	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	a.KeyDown(gift.KeyTab, 0) // focus "b"
	a.KeyUp(gift.KeyTab, 0)
	a.BeginInput(0)
	a.KeyDown(gift.KeyDown, 0)

	show = false
	a.Invalidate()
	mustUpdate(t, a)
	a.Paint()

	before := a.Diagnostics().KeyRepeats
	now := gift.KeyRepeatDelay
	for range 60 {
		a.BeginInput(now)
		now += time.Second / 60
	}
	// Diagnostics is the published snapshot, and publishing happens in
	// Update; without this the counter would still read the value from
	// before the loop and the assertion could not fail.
	if err := a.Update(viewport()); err != nil {
		t.Fatal(err)
	}
	if got := a.Diagnostics().KeyRepeats - before; got != 0 {
		t.Fatalf("%d repeat(s) were produced for a node that no longer exists", got)
	}
}

// --- idle behaviour ---------------------------------------------------------

// TestHeldKeyKeepsPaintingAndReleaseSettles is the idle policy as it applies to
// the repeat.
//
// Two claims, and they pull in opposite directions. While a key is held, gift
// has to keep asking for paint, because the backend's idle policy drops to
// Config.IdleTPS when nothing asks and would then sample the repeat ten times
// a second and deliver it in visible bursts. After the release, gift must stop
// asking, or a kiosk would run at the busy tick rate for the rest of the day
// because somebody once pressed an arrow key. That second half is the same
// settling [gift.ScrollIndicatorLinger] needed and the reason it is tested
// here at all.
func TestHeldKeyKeepsPaintingAndReleaseSettles(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	now := time.Duration(0)
	// tick is one backend tick in the order backend/ebiten uses: input,
	// update, *then* the idle policy reads NeedsPaint, and only afterwards
	// does the draw clear it. See game.Update there.
	tick := func() bool {
		now += time.Second / 60
		a.BeginInput(now)
		if err := a.Update(viewport()); err != nil {
			t.Fatal(err)
		}
		busy := a.NeedsPaint()
		a.Paint()
		return busy
	}
	settled := func() bool {
		for range 8 {
			if !tick() {
				return true
			}
		}
		return false
	}
	if !settled() {
		t.Fatalf("the application never went quiet before the key was even pressed")
	}

	a.BeginInput(now)
	a.KeyDown(gift.KeyDown, 0)
	for i := range 8 {
		if !tick() {
			t.Fatalf("tick %d with a key held reported no repaint; the backend would drop to its "+
				"idle tick rate and deliver the repeats in bursts. See gift.KeyRepeatDelay.", i)
		}
	}

	a.BeginInput(now)
	a.KeyUp(gift.KeyDown, 0)
	if !settled() {
		t.Fatalf("the application never settled after the key was released; a repeat must not " +
			"keep an idle application awake")
	}
	// And it stays settled while the clock keeps running.
	for range 60 {
		if tick() {
			t.Fatalf("the application asked for a repaint again after the key was released")
		}
	}
}

// --- the shortcut modifier --------------------------------------------------

// TestShortcutModifierFollowsTheHost pins the documented default. It is one
// line of code and it is the kind of line that gets inverted in a refactor,
// after which copy works on exactly one of the two supported platforms.
func TestShortcutModifierFollowsTheHost(t *testing.T) {
	want := gift.ModControl
	if runtime.GOOS == "darwin" {
		want = gift.ModMeta
	}
	if gift.ShortcutModifier != want {
		t.Fatalf("ShortcutModifier is %v on %s, want %v", gift.ShortcutModifier, runtime.GOOS, want)
	}
	if !gift.ShortcutModifier.Has(want) {
		t.Fatalf("Mods.Has disagrees with the value it was asked about")
	}
}

// TestShortcutModifierReachesTheHandler is the value in use: the modifier is
// on the event, so a view can write the one test that is right on both
// platforms.
func TestShortcutModifierReachesTheHandler(t *testing.T) {
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	a.BeginInput(0)
	a.SetModifiers(gift.ShortcutModifier)
	a.KeyDown(gift.KeyC, gift.ShortcutModifier)

	found := false
	for _, e := range evs {
		if e.Kind == gift.EventKeyDown && e.Key == gift.KeyC && e.Mods.Has(gift.ShortcutModifier) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no copy shortcut reached the handler, got %d events", len(evs))
	}
}

// --- allocations ------------------------------------------------------------

// TestRuneAndRepeatAreAllocationFree puts the two new paths inside the
// contract of the project plan, section 11, which names input processing
// explicitly. A rune event is a value and the repeat state is a struct in the
// App; if either ever starts boxing something, this fails.
func TestRuneAndRepeatAreAllocationFree(t *testing.T) {
	if isDebugBuild {
		t.Skip("the UI executor check is only compiled with -tags giftdebug")
	}
	var evs []gift.Event
	a := focusedTypingApp(t, &evs)

	now := time.Duration(0)
	a.BeginInput(now)
	a.KeyDown(gift.KeyLeft, 0)

	frame := func() {
		now += time.Second / 60
		a.BeginInput(now)
		a.TypeRune('ä')
		a.TypeRune('ß')
		// The event log would grow without bound and allocate; it is the
		// test's own bookkeeping and not gift's.
		evs = evs[:0]
	}
	for range 32 {
		frame()
	}
	if got := testing.AllocsPerRun(200, frame); got != 0 {
		t.Fatalf("typing and repeating allocated %v times per run, want 0", got)
	}
}

// BenchmarkTypeRune measures the per character cost of the rune channel with a
// node focused and consuming, which is the shape a text field has.
func BenchmarkTypeRune(b *testing.B) {
	var evs []gift.Event
	a := benchTypingApp(b, &evs)
	a.BeginInput(0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		a.TypeRune('ä')
		evs = evs[:0]
	}
}

// BenchmarkKeyRepeatTick measures what a held key costs every tick, which is
// the number that matters: it is paid sixty times a second for as long as
// somebody leans on an arrow key. Most ticks deliver nothing and only compare
// two timestamps.
func BenchmarkKeyRepeatTick(b *testing.B) {
	var evs []gift.Event
	a := benchTypingApp(b, &evs)
	now := time.Duration(0)
	a.BeginInput(now)
	a.KeyDown(gift.KeyLeft, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now += time.Second / 60
		a.BeginInput(now)
		evs = evs[:0]
	}
}

// BenchmarkKeyRepeatIdleTick is the same tick with nothing held: the cost gift
// imposes on every application that is not repeating anything.
func BenchmarkKeyRepeatIdleTick(b *testing.B) {
	var evs []gift.Event
	a := benchTypingApp(b, &evs)
	now := time.Duration(0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now += time.Second / 60
		a.BeginInput(now)
		evs = evs[:0]
	}
}

// benchTypingApp is focusedTypingApp for a benchmark, which has no *testing.T.
func benchTypingApp(b *testing.B, evs *[]gift.Event) *gift.App {
	b.Helper()
	root := func(*gift.Context) gift.View {
		return frameView{w: 300, h: 300, offsets: []geom.Point{at(0, 0)},
			children: []gift.View{target{name: "field", w: 100, h: 40, events: evs}}}
	}
	a := gift.New(gift.Options{Root: root})
	if err := a.Update(viewport()); err != nil {
		b.Fatal(err)
	}
	a.Paint()
	a.BeginInput(0)
	a.KeyDown(gift.KeyTab, 0)
	a.KeyUp(gift.KeyTab, 0)
	if _, ok := a.Focus(); !ok {
		b.Fatalf("nothing took the focus")
	}
	*evs = (*evs)[:0]
	return a
}
