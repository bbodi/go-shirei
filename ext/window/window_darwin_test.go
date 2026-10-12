//go:build darwin && !ios && !x11darwin

package window

import (
	"testing"
	"unsafe"
)

func TestNSRectLayout(t *testing.T) {
	if g, w := unsafe.Sizeof(nsRect{}), uintptr(32); g != w {
		t.Fatalf("nsRect size %d, want %d", g, w)
	}
	if g, w := unsafe.Sizeof(nsSize{}), uintptr(16); g != w {
		t.Fatalf("nsSize size %d, want %d", g, w)
	}
}

func TestDarwinBind(t *testing.T) {
	if !ensure() {
		t.Fatal("AppKit bind failed")
	}
	if selCenter == 0 || selSetContentMinSize == 0 || selSetContentSize == 0 {
		t.Fatal("selectors not registered")
	}
	if dispatchWorkPC == 0 || dispatchMainQ == 0 {
		t.Fatal("dispatch_async_f not bound")
	}
}
