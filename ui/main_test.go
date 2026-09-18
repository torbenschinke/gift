package ui_test

import (
	"testing"

	"github.com/worldiety/gift/gifttest"
)

// TestMain is the one line a package with golden tests writes.
//
// Without the giftgpu tag [gifttest.Main] is an ordinary m.Run and this file
// changes nothing. With it, the test binary runs inside Ebitengine's loop, so
// that [gifttest.Harness.Image] can read pixels back — Ebitengine rejects
// ReadPixels before the game has started, with exactly that message.
func TestMain(m *testing.M) { gifttest.Main(m) }
