//go:build darwin && !ios

// Package cocoabackend is a direct-macOS (AppKit) backend for shirei. AppKit
// provides the window, run loop, and input. Rasterization writes an IOSurface
// that is set as a CALayer's contents — the window server composites it with no
// per-frame CPU copy. Metal (shirei/gpurender) is the default compositor when
// built with cgo; SHIREI_GPU=0 or a CGO_ENABLED=0 build uses the software
// renderer. Init failure also falls back.
package cocoabackend

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/cli/browser"
	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
	"go.hasen.dev/shirei/internal/iconimg"
	"go.hasen.dev/shirei/internal/qwerty"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache; 0 would disable it). 16 MB holds many thousands of glyph masks.
const glyphCacheBudget = 16 << 20

// Window title and dimensions recorded before Run.
var (
	winTitle    string
	winIconPath string
	winIconImg  *image.NRGBA
	winW        int
	winH        int
	winQuiet    bool
	frameFn     shirei.FrameFn
)

// SetupQuiet maps the window without activating the app or making it key.
// Call before Run.
func SetupQuiet() {
	winQuiet = true
}

// SetupWindow records the window parameters. The window is created in Run, on
// the main thread.
func SetupWindow(title string, width int, height int) {
	winTitle = title
	winW = width
	winH = height
}

// SetupIcon records the path of the image (any NSImage-readable format, e.g.
// PNG, including .icns) used as the app's Dock icon — macOS has no title-bar
// icons. Call it before Run; empty means the default icon.
func SetupIcon(imagePath string) {
	winIconPath = imagePath
}

// SetupIconImage is SetupIcon from an in-memory image (e.g. decoded from
// go:embed-ed bytes) instead of a file. It takes precedence over SetupIcon.
func SetupIconImage(img image.Image) {
	winIconImg = iconimg.FromImage(img)
}

// init locks the main goroutine to the main OS thread (thread 0). AppKit requires
// NSApplication/NSWindow to live on thread 0, and init runs there — before main()
// and before any goroutines exist — so main() and therefore Run stay on thread 0
// even after the app spawns background goroutines first. Locking only inside Run
// is too late: by then the scheduler may already have migrated the main goroutine
// off thread 0 (this crashed apps like vbeam/local_gui, which start goroutines
// before Run with "NSWindow should only be instantiated on the main thread").
func init() {
	runtime.LockOSThread()
}

// Run opens the window and runs the AppKit event loop. It must be called
// from the program's main goroutine (AppKit requires the main thread) and
// does not return: quit paths call generic.ExitWithCleanup so AddExitCleanup
// handlers run. AppKit [NSApp terminate:] would otherwise exit without
// returning to Go. System fonts are initialized by shirei on the first
// frame (RunFrameFn), not here.
func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()

	frameFn = fn
	shirei.SetBackendWake(requestRedraw)

	shirei.GetHost().GlyphCacheBudgetBytes = glyphCacheBudget
	shirei.GetHost().EscapeHatchBackendContext = Context{}

	setupWindow(winTitle, winW, winH)
	if winIconImg != nil {
		b := winIconImg.Bounds()
		setAppIconRGBA(winIconImg.Pix, b.Dx(), b.Dy())
	} else if winIconPath != "" {
		setAppIcon(winIconPath)
	}
	enableZerocopy()
	if g.EnvFalsy("SHIREI_GPU") {
		fmt.Fprintln(os.Stderr, "shirei: software renderer (SHIREI_GPU=0)")
	} else if err := gpurender.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "shirei: software renderer (%v)\n", err)
	} else {
		gpuOK = true
		gpurender.SetCompleteFunc(gpuPresentComplete)
	}
	runApp()
	g.ExitWithCleanup(0)
}

func exitWithCleanup() {
	g.ExitWithCleanup(0)
}

// gpuOK is set once at Run if Metal init succeeds and SHIREI_GPU is not 0.
var gpuOK bool

// IOSurface present pool. A few surfaces are rotated so we never render into the
// one the compositor is currently reading (IOSurfaceIsInUse). Static frames present
// nothing — the layer keeps showing the last surface.
type ioSurface struct {
	ref         unsafe.Pointer
	w, h        int
	gpuBusy     bool
	pendingHash uint64
	gpuSeq      uint64
}

var (
	surfacePool   []ioSurface
	lastPresented unsafe.Pointer
	presentW      int
	presentH      int
	havePresented bool

	lastPresentedHash uint64
	presentDeferred   bool
	gpuSubmitSeq      uint64
	gpuPresentedSeq   uint64
)

func renderAndPresent() {
	scale := shirei.GetHost().WindowScale
	if scale <= 0 {
		scale = 1
	}
	dw := int(shirei.GetHost().WindowSize[0]*scale + 0.5)
	dh := int(shirei.GetHost().WindowSize[1]*scale + 0.5)
	if dw <= 0 || dh <= 0 {
		return
	}

	if havePresented && frameHash == lastPresentedHash && dw == presentW && dh == presentH && !presentDeferred {
		shirei.EmitFrameMetrics()
		return
	}
	if gpuOK && gpuInFlightHash(frameHash) {
		shirei.EmitFrameMetrics()
		return
	}

	ensureSurfacePool(dw, dh)
	slot := pickFreeSurface()
	if slot == nil {
		presentDeferred = true
		setWantsFrame(true)
		return
	}
	presentDeferred = false

	if gpuOK {
		err := gpurender.Render(slot.ref, dw, dh, scale, frameSurfaces, frameGlyphRuns, frameGlyphsAdded, frameGlyphsEvicted, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gpurender: %v; software this frame\n", err)
			renderSoftware(slot.ref, dw, dh, scale, frameSurfaces, frameGlyphRuns)
		} else {
			gpuSubmitSeq++
			slot.gpuBusy = true
			slot.pendingHash = frameHash
			slot.gpuSeq = gpuSubmitSeq
			t := &shirei.ActiveUI().FrameTimings
			t.Painted = true
			t.PaintEnd = time.Now()
			shirei.EmitFrameMetrics()
			return
		}
	} else {
		renderSoftware(slot.ref, dw, dh, scale, frameSurfaces, frameGlyphRuns)
	}

	setLayerContents(slot.ref)
	lastPresented = slot.ref
	lastPresentedHash = frameHash
	presentW, presentH, havePresented = dw, dh, true
	t := &shirei.ActiveUI().FrameTimings
	t.Painted = true
	t.PaintEnd = time.Now()
	shirei.EmitFrameMetrics()
}

func renderSoftware(s unsafe.Pointer, dw, dh int, scale float32, surfaces []shirei.Surface, runs []shirei.GlyphRun) {
	base, stride := ioSurfaceLockBuf(s)
	if base == nil {
		return
	}
	buf := unsafe.Slice((*byte)(base), stride*dh)
	softRenderer.RenderInto(buf, stride, dw, dh, scale, surfaces, runs)
	ioSurfaceUnlockBuf(s)
}

func ensureSurfacePool(w, h int) {
	if len(surfacePool) > 0 && surfacePool[0].w == w && surfacePool[0].h == h {
		return
	}
	if gpuOK {
		gpurender.WaitIdle()
	}
	for _, s := range surfacePool {
		if gpuOK {
			gpurender.Forget(s.ref)
		}
		ioSurfaceRelease(s.ref)
	}
	surfacePool = surfacePool[:0]
	lastPresented = nil
	havePresented = false
	gpuPresentedSeq = gpuSubmitSeq
	for i := 0; i < 3; i++ {
		ref := ioSurfaceCreateBuf(w, h)
		if ref == nil {
			continue
		}
		surfacePool = append(surfacePool, ioSurface{ref: ref, w: w, h: h})
	}
}

func gpuInFlightHash(h uint64) bool {
	for i := range surfacePool {
		s := &surfacePool[i]
		if s.gpuBusy && s.pendingHash == h {
			return true
		}
	}
	return false
}

func gpuPresentComplete(surf unsafe.Pointer) {
	var slot *ioSurface
	for i := range surfacePool {
		if surfacePool[i].ref == surf {
			slot = &surfacePool[i]
			break
		}
	}
	if slot == nil || !slot.gpuBusy {
		return
	}
	if slot.gpuSeq < gpuPresentedSeq {
		slot.gpuBusy = false
		return
	}
	setLayerContents(slot.ref)
	lastPresented = slot.ref
	lastPresentedHash = slot.pendingHash
	presentW, presentH, havePresented = slot.w, slot.h, true
	gpuPresentedSeq = slot.gpuSeq
	slot.gpuBusy = false
}

func pickFreeSurface() *ioSurface {
	for i := range surfacePool {
		s := &surfacePool[i]
		if s.ref == lastPresented || s.gpuBusy || ioSurfaceInUseBuf(s.ref) {
			continue
		}
		return s
	}
	return nil
}

var (
	frameSurfaces      []shirei.Surface
	frameGlyphRuns     []shirei.GlyphRun
	frameGlyphsAdded   []shirei.GlyphKey
	frameGlyphsEvicted []shirei.GlyphKey
	lastProducedW      float32
	lastProducedH      float32
	haveFrame          bool
	pendingText        string
	pendingPaste       string
	hasPendingPaste    bool
	frameHash          uint64
	softRenderer       shirei.SoftRenderer
)

func produceFrame(w, h float64) {
	shirei.GetHost().WindowSize = shirei.Vec2{float32(w), float32(h)}
	shirei.GetHost().WindowScale = float32(backingScale())
	lastProducedW, lastProducedH = float32(w), float32(h)

	flushPendingFrameText()
	flushAccessAction()

	out := shirei.RunFrameFn(frameFn)
	if out.AccessChanged {
		updateAccess(out.Access)
	}

	clear(frameSurfaces) // Release immutable glyph data from the previous snapshot.
	frameSurfaces = append(frameSurfaces[:0], out.Surfaces...)
	frameGlyphRuns = append(frameGlyphRuns[:0], out.GlyphRuns...)
	frameGlyphsAdded = append(frameGlyphsAdded[:0], out.GlyphsAdded...)
	frameGlyphsEvicted = append(frameGlyphsEvicted[:0], out.GlyphsEvicted...)
	haveFrame = true

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		pendingPaste = getClipboard()
		hasPendingPaste = true
		requestRedraw()
	}
	if out.OpenURL != "" {
		openURL(out.OpenURL)
	}

	setWantsFrame(out.NextFrameRequested || len(accessPending) > 0)
	frameHash = out.SurfacesHash
}

func flushPendingFrameText() {
	if hasPendingPaste {
		pendingText += pendingPaste
		pendingPaste = ""
		hasPendingPaste = false
	}
	if pendingText != "" {
		shirei.GetFrameInput().Text += pendingText
		pendingText = ""
	}
}

func needsProduce(w, h float64) bool {
	return !haveFrame || float32(w) != lastProducedW || float32(h) != lastProducedH
}

func frameRequested() bool { return shirei.FrameRequested() }

func caretX() float64      { return float64(shirei.GetHost().CaretPos[0]) }
func caretY() float64      { return float64(shirei.GetHost().CaretPos[1]) }
func caretHeight() float64 { return float64(shirei.GetHost().CaretHeight) }

func openURL(url string) {
	if url == "" {
		return
	}
	_ = browser.OpenURL(url)
}

const (
	mouseMove = 0
	mouseDown = 1
	mouseUp   = 2
	mouseDrag = 3
)

func onMouse(x, y float64, action, button int) {
	np := shirei.Vec2{float32(x), float32(y)}
	prev := shirei.GetInputState().MousePoint
	shirei.GetFrameInput().Motion = shirei.Vec2Add(shirei.GetFrameInput().Motion, shirei.Vec2Sub(np, prev))
	shirei.GetInputState().MousePoint = np
	shirei.GetInputState().MouseButton = shirei.MouseButton(button)

	switch action {
	case mouseDown:
		shirei.GetFrameInput().Mouse = shirei.MouseClick
	case mouseUp:
		shirei.GetFrameInput().Mouse = shirei.MouseRelease
	}
}

func onScroll(dx, dy float64) {
	shirei.GetFrameInput().Scroll = shirei.Vec2Add(shirei.GetFrameInput().Scroll,
		shirei.Vec2{float32(dx), float32(dy)})
}

func onWindowFocus(focused bool) {
	shirei.GetHost().WindowFocused = focused
	shirei.RequestNextFrame()
}

const (
	nsShift   = 1 << 17
	nsControl = 1 << 18
	nsOption  = 1 << 19
	nsCommand = 1 << 20
)

func onModifiers(flags uint) {
	var m shirei.Modifiers
	if flags&nsShift != 0 {
		m |= shirei.ModShift
	}
	if flags&nsControl != 0 {
		m |= shirei.ModCtrl
	}
	if flags&nsOption != 0 {
		m |= shirei.ModAlt
	}
	if flags&nsCommand != 0 {
		m |= shirei.ModCmd
	}
	shirei.GetInputState().Modifiers = m
	syncModKey(m, shirei.ModShift, shirei.KeyShift)
	syncModKey(m, shirei.ModCtrl, shirei.KeyCtrl)
	syncModKey(m, shirei.ModAlt, shirei.KeyAlt)
	syncModKey(m, shirei.ModCmd, shirei.KeyCommand)
}

func syncModKey(m, bit shirei.Modifiers, k shirei.KeyCode) {
	if m&bit != 0 {
		g.SliceAddUniq(&shirei.GetInputState().DownKeys, k)
	} else {
		g.SliceRemove(&shirei.GetInputState().DownKeys, k)
	}
}

func keyDown(vkey int, bare string) { onKeyDown(vkey, bare) }

func setCompositionFromUTF16Offsets(text string, startUTF16, endUTF16 int) {
	onSetComposition(text, startUTF16, endUTF16)
}

func onKeyDown(vkey int, bare string) {
	if code := mapVKey(uint16(vkey), bare); code != shirei.KeyCodeNone {
		shirei.GetFrameInput().Key = code
		g.SliceAddUniq(&shirei.GetInputState().DownKeys, code)
	}
}

func queueCommittedText(text string) {
	if isPrintable(text) {
		pendingText += text
	}
}

func onCommitText(text string) { queueCommittedText(text) }

func utf16OffsetToRuneOffset(s string, units int) int {
	if units <= 0 {
		return 0
	}
	u16 := utf16.Encode([]rune(s))
	if units > len(u16) {
		units = len(u16)
	}
	return len(utf16.Decode(u16[:units]))
}

func onSetComposition(text string, startUTF16, endUTF16 int) {
	start := utf16OffsetToRuneOffset(text, startUTF16)
	end := utf16OffsetToRuneOffset(text, endUTF16)
	if start > end {
		start, end = end, start
	}
	shirei.GetInputState().Composition = text
	shirei.GetInputState().CompositionSel = [2]int{start, end}
	shirei.RequestNextFrame()
}

func onKeyUp(vkey int, bare string) {
	if code := mapVKey(uint16(vkey), bare); code != shirei.KeyCodeNone {
		g.SliceRemove(&shirei.GetInputState().DownKeys, code)
	}
}

func isPrintable(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)[0]
	if r >= 0xF700 && r <= 0xF8FF {
		return false
	}
	return r >= 0x20 && r != 0x7f
}

const (
	vkReturn        = 0x24
	vkTab           = 0x30
	vkSpace         = 0x31
	vkDelete        = 0x33
	vkEscape        = 0x35
	vkKeypadEnter   = 0x4C
	vkForwardDelete = 0x75
	vkHome          = 0x73
	vkEnd           = 0x77
	vkPageUp        = 0x74
	vkPageDown      = 0x79
	vkLeft          = 0x7B
	vkRight         = 0x7C
	vkDown          = 0x7D
	vkUp            = 0x7E
	vkF1            = 0x7A
	vkF2            = 0x78
	vkF3            = 0x63
	vkF4            = 0x76
	vkF5            = 0x60
	vkF6            = 0x61
	vkF7            = 0x62
	vkF8            = 0x64
	vkF9            = 0x65
	vkF10           = 0x6D
	vkF11           = 0x67
	vkF12           = 0x6F
)

func mapVKey(vk uint16, bare string) shirei.KeyCode {
	switch vk {
	case vkLeft:
		return shirei.KeyLeft
	case vkRight:
		return shirei.KeyRight
	case vkUp:
		return shirei.KeyUp
	case vkDown:
		return shirei.KeyDown
	case vkReturn, vkKeypadEnter:
		return shirei.KeyEnter
	case vkEscape:
		return shirei.KeyEscape
	case vkDelete:
		return shirei.KeyDeleteBackward
	case vkForwardDelete:
		return shirei.KeyDeleteForward
	case vkHome:
		return shirei.KeyHome
	case vkEnd:
		return shirei.KeyEnd
	case vkPageUp:
		return shirei.KeyPageUp
	case vkPageDown:
		return shirei.KeyPageDown
	case vkTab:
		return shirei.KeyTab
	case vkSpace:
		return shirei.KeySpace
	case vkF1:
		return shirei.KeyF1
	case vkF2:
		return shirei.KeyF2
	case vkF3:
		return shirei.KeyF3
	case vkF4:
		return shirei.KeyF4
	case vkF5:
		return shirei.KeyF5
	case vkF6:
		return shirei.KeyF6
	case vkF7:
		return shirei.KeyF7
	case vkF8:
		return shirei.KeyF8
	case vkF9:
		return shirei.KeyF9
	case vkF10:
		return shirei.KeyF10
	case vkF11:
		return shirei.KeyF11
	case vkF12:
		return shirei.KeyF12
	}
	if code := qwerty.FromMacVK(vk); code != shirei.KeyCodeNone {
		return code
	}
	if bare != "" {
		r := []rune(bare)[0]
		if r >= 'a' && r <= 'z' {
			return shirei.KeyCode(r - 'a' + 'A')
		}
		if r < 128 {
			return shirei.KeyCode(r)
		}
	}
	return shirei.KeyCodeNone
}
