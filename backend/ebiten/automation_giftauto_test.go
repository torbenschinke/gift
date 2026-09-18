//go:build giftauto

package ebiten

import "testing"

func TestAutomationIsInstalledByTheBuildTag(t *testing.T) {
	if _, ok := installedAutomation().(defaultAutomation); !ok {
		t.Fatal("giftauto must install the driver without an application import")
	}
	if autoWantsFrame() {
		t.Fatal("an idle driver must not request framebuffer readbacks")
	}
}
