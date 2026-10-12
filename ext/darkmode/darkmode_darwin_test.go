//go:build darwin && !ios && !x11darwin

package darkmode

import "testing"

func TestDarwinInit(t *testing.T) {
	_ = OSDarkMode()
	if selIsEqualToString == 0 || selStringForKey == 0 {
		t.Fatal("AppKit selectors not registered")
	}
	if themeBlock == 0 {
		t.Fatal("theme observer block not created")
	}
	if themeObserver == 0 {
		t.Fatal("NSDistributedNotificationCenter observer not registered")
	}
}
