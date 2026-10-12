//go:build darwin && !ios

package app

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// AudioQueue output via purego — no cgo, so this file is not what keeps
// darwin builds on CGO_ENABLED=1 (cocoabackend still is). 3 × 256 frames
// at 44.1kHz ≈ 17ms of output latency. The watchdog covers the queue
// silently dying — the classic case is waking from a longer sleep, where
// coreaudiod may have restarted and the orphaned queue never fires its
// callback again.

const (
	audioToolboxPath = "/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox"

	kAudioFormatLinearPCM        = 0x6C70636D // 'lpcm'
	kLinearPCMFormatFlagIsFloat  = 1
	kLinearPCMFormatFlagIsPacked = 8

	aqChunkFrames = 256
	aqNumBufs     = 3
)

// audioStreamBasicDescription is CoreAudio's AudioStreamBasicDescription (40 bytes).
type audioStreamBasicDescription struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

// audioQueueBuffer is AudioQueueBuffer on 64-bit Darwin (56 bytes).
type audioQueueBuffer struct {
	audioDataBytesCapacity    uint32
	_                         uint32
	audioData                 uintptr
	audioDataByteSize         uint32
	_2                        uint32
	userData                  uintptr
	packetDescriptionCapacity uint32
	_3                        uint32
	packetDescriptions        uintptr
	packetDescriptionCount    uint32
	_4                        uint32
}

var (
	audioOnce    sync.Once
	audioBindErr error

	audioQueueNewOutput      func(asbd *audioStreamBasicDescription, callback, userData, runLoop, runLoopMode uintptr, flags uint32, outAQ *uintptr) int32
	audioQueueAllocateBuffer func(aq uintptr, byteSize uint32, outBuf *uintptr) int32
	audioQueueEnqueueBuffer  func(aq, buf uintptr, numPacketDescs uint32, packetDescs uintptr) int32
	audioQueueStart          func(aq, startTime uintptr) int32
	audioQueuePause          func(aq uintptr) int32
	audioQueueDispose        func(aq uintptr, immediate uint8) int32

	aqCallbackPC uintptr

	gQueue        uintptr
	gSampleRate   float64
	gBufferFrames int
)

func bindAudioToolbox() error {
	audioOnce.Do(func() {
		lib, err := purego.Dlopen(audioToolboxPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			audioBindErr = fmt.Errorf("AudioToolbox: %w", err)
			return
		}
		purego.RegisterLibFunc(&audioQueueNewOutput, lib, "AudioQueueNewOutput")
		purego.RegisterLibFunc(&audioQueueAllocateBuffer, lib, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&audioQueueEnqueueBuffer, lib, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&audioQueueStart, lib, "AudioQueueStart")
		purego.RegisterLibFunc(&audioQueuePause, lib, "AudioQueuePause")
		purego.RegisterLibFunc(&audioQueueDispose, lib, "AudioQueueDispose")
		aqCallbackPC = purego.NewCallback(aqCallback)
	})
	return audioBindErr
}

func ptr[T any](p uintptr) *T {
	return *(**T)(unsafe.Pointer(&p))
}

func aqCallback(_, aq, buf uintptr) {
	b := ptr[audioQueueBuffer](buf)
	n := int(b.audioDataBytesCapacity / 4)
	out := unsafe.Slice(ptr[float32](b.audioData), n)
	audioNoteFill()
	if fill := audioFill; fill != nil {
		fill(out)
	} else {
		clear(out)
	}
	b.audioDataByteSize = b.audioDataBytesCapacity
	audioQueueEnqueueBuffer(aq, buf, 0, 0)
}

func createAndStart() int32 {
	asbd := audioStreamBasicDescription{
		sampleRate:       gSampleRate,
		formatID:         kAudioFormatLinearPCM,
		formatFlags:      kLinearPCMFormatFlagIsFloat | kLinearPCMFormatFlagIsPacked,
		bytesPerPacket:   4,
		framesPerPacket:  1,
		bytesPerFrame:    4,
		channelsPerFrame: 1,
		bitsPerChannel:   32,
	}
	var aq uintptr
	st := audioQueueNewOutput(&asbd, aqCallbackPC, 0, 0, 0, 0, &aq)
	runtime.KeepAlive(&asbd)
	if st != 0 {
		return st
	}
	byteSize := uint32(gBufferFrames * 4)
	for i := 0; i < aqNumBufs; i++ {
		var buf uintptr
		if st := audioQueueAllocateBuffer(aq, byteSize, &buf); st != 0 {
			audioQueueDispose(aq, 1)
			return st
		}
		b := ptr[audioQueueBuffer](buf)
		out := unsafe.Slice(ptr[float32](b.audioData), gBufferFrames)
		clear(out)
		b.audioDataByteSize = b.audioDataBytesCapacity
		audioQueueEnqueueBuffer(aq, buf, 0, 0)
	}
	if st := audioQueueStart(aq, 0); st != 0 {
		audioQueueDispose(aq, 1)
		return st
	}
	gQueue = aq
	return 0
}

func audioRestart() error {
	if gQueue != 0 {
		audioQueueDispose(gQueue, 1)
		gQueue = 0
	}
	if st := createAndStart(); st != 0 {
		return fmt.Errorf("AudioQueue error %d", st)
	}
	return nil
}

func audioStart(sampleRate int) error {
	if err := bindAudioToolbox(); err != nil {
		return err
	}
	gSampleRate = float64(sampleRate)
	gBufferFrames = aqChunkFrames
	if err := audioRestart(); err != nil {
		return err
	}
	audioLastFill.Store(time.Now().UnixNano())
	go audioWatchdog(audioRestart)
	return nil
}

// audioPause stops callbacks without tearing the queue down — it simulates
// the queue dying, so the watchdog test can exercise the revival path.
// Test hook only.
func audioPause() error {
	if gQueue == 0 {
		return fmt.Errorf("AudioQueue error -1")
	}
	if st := audioQueuePause(gQueue); st != 0 {
		return fmt.Errorf("AudioQueue error %d", st)
	}
	return nil
}
