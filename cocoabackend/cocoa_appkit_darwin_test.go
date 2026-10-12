//go:build darwin && !ios

package cocoabackend

import (
	"testing"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

func TestNSRectLayout(t *testing.T) {
	if g, w := unsafe.Sizeof(nsRect{}), uintptr(32); g != w {
		t.Fatalf("nsRect size %d, want %d", g, w)
	}
	if g, w := unsafe.Sizeof(nsRange{}), uintptr(16); g != w {
		t.Fatalf("nsRange size %d, want %d", g, w)
	}
}

func TestEnsureAppkit(t *testing.T) {
	if err := ensureAppkit(); err != nil {
		t.Fatal(err)
	}
	if viewClass == 0 {
		t.Fatal("ShireiView class not registered")
	}
	if appDelegateClass == 0 {
		t.Fatal("ShireiAppDelegate class not registered")
	}
	if kIOSurfaceWidth == 0 || nsPasteboardTypeString == 0 || kCAGravityResize == 0 {
		t.Fatal("AppKit/IOSurface constants not loaded")
	}
	if objc.GetProtocol("NSTextInputClient") == nil {
		t.Fatal("NSTextInputClient protocol missing after AppKit load")
	}
}
