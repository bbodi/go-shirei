//go:build android

package gpurender

import (
	"fmt"
	"unsafe"
)

var (
	eglSurf   uintptr
	nativeWin uintptr
	winW      int
	winH      int
	conL      int
	conT      int
	conW      int
	conH      int
	origX     int // GL window x of content
	origY     int // GL window y of content (bottom-left origin)
)

func gpuInit() error {
	if eglDpy != 0 {
		return nil
	}
	if err := loadGLESLibs(); err != nil {
		return err
	}
	eglDpy = eglGetDisplay(nil)
	if eglDpy == 0 {
		return fmt.Errorf("eglGetDisplay failed (%#x)", uint32(eglGetError()))
	}
	var maj, min int32
	if eglInitialize(eglDpy, &maj, &min) == eglFalse {
		return fmt.Errorf("eglInitialize failed (%#x)", uint32(eglGetError()))
	}
	if eglBindAPI(eglOpenGLESAPI) == eglFalse {
		return fmt.Errorf("eglBindAPI GLES failed")
	}
	cfgAttribs := []int32{
		eglSurfaceType, eglWindowBit,
		eglRenderableType, eglOpenGLES3Bit,
		eglRedSize, 8,
		eglGreenSize, 8,
		eglBlueSize, 8,
		eglAlphaSize, 8,
		eglNone,
	}
	var ncfg int32
	if eglChooseConfig(eglDpy, &cfgAttribs[0], &eglCfg, 1, &ncfg) == eglFalse || ncfg < 1 {
		cfgAttribs[3] = eglOpenGLES2Bit
		if eglChooseConfig(eglDpy, &cfgAttribs[0], &eglCfg, 1, &ncfg) == eglFalse || ncfg < 1 {
			return fmt.Errorf("eglChooseConfig failed (%#x)", uint32(eglGetError()))
		}
	}
	ctxAttribs := []int32{
		eglContextMajorVersion, 3,
		eglContextMinorVersion, 0,
		eglNone,
	}
	eglCtx = eglCreateContext(eglDpy, eglCfg, 0, &ctxAttribs[0])
	if eglCtx == 0 {
		ctxAttribs = []int32{eglContextClientVersion, 3, eglNone}
		eglCtx = eglCreateContext(eglDpy, eglCfg, 0, &ctxAttribs[0])
	}
	if eglCtx == 0 {
		return fmt.Errorf("eglCreateContext ES3 failed (%#x)", uint32(eglGetError()))
	}
	return nil
}

// BindWindow creates (or recreates) the EGL window surface for nativeWin
// (ANativeWindow*). winW/winH are the full surface size in device pixels.
func BindWindow(win unsafe.Pointer, w, h int) error {
	if err := gpuInit(); err != nil {
		return err
	}
	nw := uintptr(win)
	if nw == 0 || w <= 0 || h <= 0 {
		return fmt.Errorf("bad native window")
	}
	if eglSurf != 0 && nativeWin == nw && winW == w && winH == h {
		return nil
	}
	if eglSurf != 0 {
		eglMakeCurrent(eglDpy, 0, 0, 0)
		eglDestroySurface(eglDpy, eglSurf)
		eglSurf = 0
	}
	eglSurf = eglCreateWindowSurface(eglDpy, eglCfg, nw, nil)
	if eglSurf == 0 {
		return fmt.Errorf("eglCreateWindowSurface failed (%#x)", uint32(eglGetError()))
	}
	if eglMakeCurrent(eglDpy, eglSurf, eglSurf, eglCtx) == eglFalse {
		eglDestroySurface(eglDpy, eglSurf)
		eglSurf = 0
		return fmt.Errorf("eglMakeCurrent failed (%#x)", uint32(eglGetError()))
	}
	nativeWin, winW, winH = nw, w, h
	if err := glesFinishInit("android"); err != nil {
		return err
	}
	return nil
}

// UnbindWindow drops the EGL surface (TERM_WINDOW). The context stays.
func UnbindWindow() {
	if eglDpy == 0 {
		return
	}
	eglMakeCurrent(eglDpy, 0, 0, 0)
	if eglSurf != 0 {
		eglDestroySurface(eglDpy, eglSurf)
		eglSurf = 0
	}
	nativeWin = 0
}

// WindowDest is the GPU present target: full window size plus the content
// rect in Android top-left device pixels (same as the CPU lock path).
type WindowDest struct {
	WinW, WinH int
	Left, Top  int
	W, H       int
}

func (d *WindowDest) Handle() unsafe.Pointer { return unsafe.Pointer(d) }

func gpuBindDest(dest unsafe.Pointer, w, h int) error {
	if dest == nil || eglSurf == 0 {
		return fmt.Errorf("no egl surface")
	}
	d := (*WindowDest)(dest)
	if d.W != w || d.H != h {
		return fmt.Errorf("target size mismatch")
	}
	if eglMakeCurrent(eglDpy, eglSurf, eglSurf, eglCtx) == eglFalse {
		return fmt.Errorf("eglMakeCurrent failed (%#x)", uint32(eglGetError()))
	}
	conL, conT, conW, conH = d.Left, d.Top, d.W, d.H
	winW, winH = d.WinW, d.WinH
	origX = conL
	origY = winH - conT - conH

	glBindFramebuffer(glFramebuffer, 0)
	glDisable(glScissorTest)
	glViewport(0, 0, int32(winW), int32(winH))
	glClearColor(1, 1, 1, 1)
	glClear(glColorBufferBit)
	glViewport(int32(origX), int32(origY), int32(conW), int32(conH))
	glEnable(glScissorTest)
	glUseProgram(glProg)
	glUniform2f(uNdcY, -2, 1)
	glUniform2f(uDestOrig, float32(origX), float32(origY))
	glUniform1i(uDestFlip, 1)
	return nil
}

func gpuScissor(x, y, cw, ch, sceneW, sceneH int) {
	sy := origY + (sceneH - y - ch)
	glScissor(int32(origX+x), int32(sy), int32(cw), int32(ch))
}

func gpuPresent(dest unsafe.Pointer, wait bool) int64 {
	if eglSwapBuffers(eglDpy, eglSurf) == eglFalse {
		return 0
	}
	if wait {
		glFinish()
	}
	return 0
}
