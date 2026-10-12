//go:build darwin && !ios

package app

import (
	"testing"
	"unsafe"
)

func TestAudioQueueLayouts(t *testing.T) {
	if g, w := unsafe.Sizeof(audioStreamBasicDescription{}), uintptr(40); g != w {
		t.Fatalf("AudioStreamBasicDescription size %d, want %d", g, w)
	}
	if g, w := unsafe.Sizeof(audioQueueBuffer{}), uintptr(56); g != w {
		t.Fatalf("AudioQueueBuffer size %d, want %d", g, w)
	}
	b := audioQueueBuffer{}
	if g, w := unsafe.Offsetof(b.audioData), uintptr(8); g != w {
		t.Fatalf("mAudioData offset %d, want %d", g, w)
	}
	if g, w := unsafe.Offsetof(b.audioDataByteSize), uintptr(16); g != w {
		t.Fatalf("mAudioDataByteSize offset %d, want %d", g, w)
	}
}
