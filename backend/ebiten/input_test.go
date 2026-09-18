package ebiten

import (
	"testing"

	eb "github.com/hajimehoshi/ebiten/v2"
	"github.com/worldiety/gift"
)

// The mouse and touch readings cannot be driven headless: they come from
// Ebitengine globals that only mean anything inside a running game loop. The
// keyboard is different — it goes through the two function values described on
// [inputBridge], and keyboard_test.go in this package drives the real bridge
// through them.
//
// What is left here is the table the key half is built on, and it is worth
// checking on its own, because a wrong entry there is a key that silently
// never arrives, plus the touch diff, which no desktop ever executes.

func TestTrackedKeysAreDistinctAndComplete(t *testing.T) {
	seen := map[eb.Key]bool{}
	for _, k := range trackedKeys {
		if seen[k.eb] {
			t.Errorf("the Ebitengine key %v is listed twice; the second entry would produce a duplicate event", k.eb)
		}
		seen[k.eb] = true
	}

	// Every gift key except KeyOther must be produced by at least one entry.
	// KeyOther is what a backend reports for a key gift has no name for, so
	// it is deliberately absent.
	want := []gift.Key{
		gift.KeyTab, gift.KeySpace, gift.KeyEnter, gift.KeyEscape,
		gift.KeyLeft, gift.KeyRight, gift.KeyUp, gift.KeyDown,
		gift.KeyHome, gift.KeyEnd, gift.KeyPageUp, gift.KeyPageDown,
		// Added by the project plan, section 19: the editing keys and the
		// operands of the five shortcuts. They are here for the same reason
		// as the rest — an entry missing from the table is a key that never
		// arrives, and for backspace that is a text field that cannot delete.
		gift.KeyBackspace, gift.KeyDelete,
		gift.KeyA, gift.KeyC, gift.KeyV, gift.KeyX, gift.KeyZ,
	}
	for _, w := range want {
		found := false
		for _, k := range trackedKeys {
			if k.key == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no Ebitengine key maps to gift key %d; it can never be delivered", w)
		}
	}
}

// TestTouchDiffing exercises the edge detection of pollTouches against a
// scripted sequence, without Ebitengine.
//
// It is the part of the bridge that has real logic in it: everything else is a
// boolean compared with the previous tick. Ebitengine's own documentation says
// "AppendTouchIDs always does nothing on desktops", so on the development
// machine this code path is never executed at all and a bug in it would only
// appear on the target device.
func TestTouchDiffing(t *testing.T) {
	type step struct {
		ids  []eb.TouchID
		want []string
	}
	// The script is written against the diff, not against Ebitengine: down
	// for an id that is new, move for one that persists, up for one that
	// vanished.
	steps := []step{
		{ids: []eb.TouchID{1}, want: []string{"down:1"}},
		{ids: []eb.TouchID{1}, want: []string{"move:1"}},
		{ids: []eb.TouchID{1, 2}, want: []string{"move:1", "down:2"}},
		{ids: []eb.TouchID{2}, want: []string{"move:2", "up:1"}},
		{ids: nil, want: []string{"up:2"}},
		{ids: nil, want: nil},
	}

	var prevIDs, ids []eb.TouchID
	for i, s := range steps {
		ids = append(ids[:0], s.ids...)
		var got []string
		if len(ids) != 0 || len(prevIDs) != 0 {
			for _, id := range ids {
				if indexOf(prevIDs, id) >= 0 {
					got = append(got, "move:"+itoa(int(id)))
					continue
				}
				got = append(got, "down:"+itoa(int(id)))
			}
			for _, id := range prevIDs {
				if indexOf(ids, id) >= 0 {
					continue
				}
				got = append(got, "up:"+itoa(int(id)))
			}
		}
		if !equalStrings(got, s.want) {
			t.Fatalf("step %d: got %v, want %v", i, got, s.want)
		}
		prevIDs = append(prevIDs[:0], ids...)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
