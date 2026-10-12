package win32backend

import (
	"fmt"
	"os"
	"unsafe"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
)

var gpuOK bool

func tryInitGPU() {
	if g.EnvFalsy("SHIREI_GPU") {
		fmt.Fprintln(os.Stderr, "gpurender: off (SHIREI_GPU=0)")
		return
	}
	if remote, _, _ := procGetSystemMetrics.Call(smRemoteSession); remote != 0 {
		fmt.Fprintln(os.Stderr, "gpurender: RDP session, software")
		return
	}
	if err := gpurender.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: fallback to software: %v\n", err)
		return
	}
	gpuOK = true
	fmt.Fprintf(os.Stderr, "gpurender: D3D11 %s\n", gpurender.DeviceName())
}

func presentGPU(hdc uintptr, cw, ch int) bool {
	src, err := gpurender.PresentDC()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: GetDC: %v; software from now\n", err)
		gpuOK = false
		return false
	}
	defer gpurender.ReleasePresentDC()
	if hdc != 0 {
		procBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch), src, 0, 0, srccopy)
		return true
	}
	wdc, _, _ := procGetDC.Call(uintptr(hwnd))
	if wdc == 0 {
		return false
	}
	procBitBlt.Call(wdc, 0, 0, uintptr(cw), uintptr(ch), src, 0, 0, srccopy)
	procReleaseDC.Call(uintptr(hwnd), wdc)
	return true
}

func renderGPU(hdc uintptr, cw, ch int, scale float32, out shirei.FrameOutputData) bool {
	err := gpurender.Render(unsafe.Pointer(hwnd), cw, ch, scale, out.Surfaces, out.GlyphRuns, out.GlyphsAdded, out.GlyphsEvicted, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: %v; software from now\n", err)
		gpuOK = false
		return false
	}
	return presentGPU(hdc, cw, ch)
}

func releaseGPU() {
	if gpuOK {
		gpurender.ReleasePresentDC()
		gpurender.Release()
		gpuOK = false
	}
}

func blitLastGPU(hdc uintptr, cw, ch int) bool {
	if !gpuOK {
		return false
	}
	return presentGPU(hdc, cw, ch)
}
