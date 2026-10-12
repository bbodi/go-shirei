//go:build linux || android

package gpurender

import (
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

var (
	glesDevName string
	glesReady   bool

	eglDpy uintptr
	eglCtx uintptr
	eglCfg uintptr

	glProg     uint32
	glVAO      uint32
	glVBO      uint32
	glTexWhite uint32
	glTexGlyph uint32
	glTexColor uint32
	texImages  map[uint32]uint32

	uViewport int32
	uNdcY     int32
	uDestOrig int32
	uDestFlip int32
	uMode     int32
	uClipN    int32
	uClipRect int32
	uClipRad  int32
	uTex      int32
)

func DeviceName() string { return glesDevName }

func WaitIdle() {
	if glesReady {
		glFinish()
	}
}

func gpuResetAtlases() {
	if !glesReady {
		return
	}
	glesZeroTex(glTexGlyph, glRed, 1, glyphAtlasW, glyphAtlasH)
	glesZeroTex(glTexColor, glRGBA, 4, colorAtlasW, colorAtlasH)
}

func gpuImageEnsure(id uint32, w, h int) bool {
	if !glesReady || w <= 0 || h <= 0 {
		return false
	}
	tex := glesMakeTex(glRGBA8, glRGBA, w, h)
	if tex == 0 {
		return false
	}
	if old, ok := texImages[id]; ok && old != 0 {
		glDeleteTextures(1, &old)
	}
	texImages[id] = tex
	return true
}

func gpuImageForget(id uint32) {
	if !glesReady {
		return
	}
	t, ok := texImages[id]
	if !ok {
		return
	}
	glDeleteTextures(1, &t)
	delete(texImages, id)
}

func Forget(dest unsafe.Pointer) {}

// glesFinishInit builds the program, VAO, and atlases. The EGL context must
// already be current. Idempotent after glesReady.
func glesFinishInit(devName string) error {
	if glesReady {
		return nil
	}
	if err := glesBuildProgram(); err != nil {
		return err
	}
	glGenVertexArrays(1, &glVAO)
	glBindVertexArray(glVAO)
	glGenBuffers(1, &glVBO)
	glBindBuffer(glArrayBuffer, glVBO)
	for i := uint32(0); i < 4; i++ {
		glEnableVertexAttribArray(i)
		glVertexAttribDivisor(i, 1)
	}
	glTexWhite = glesMakeTex(glRGBA8, glRGBA, 1, 1)
	white := [4]byte{255, 255, 255, 255}
	glBindTexture(glTexture2D, glTexWhite)
	glTexSubImage2D(glTexture2D, 0, 0, 0, 1, 1, glRGBA, glUnsignedByte, unsafe.Pointer(&white[0]))
	glTexGlyph = glesMakeTex(glR8, glRed, glyphAtlasW, glyphAtlasH)
	glTexColor = glesMakeTex(glRGBA8, glRGBA, colorAtlasW, colorAtlasH)
	glesZeroTex(glTexGlyph, glRed, 1, glyphAtlasW, glyphAtlasH)
	glesZeroTex(glTexColor, glRGBA, 4, colorAtlasW, colorAtlasH)
	texImages = make(map[uint32]uint32)
	glEnable(glBlend)
	glBlendFunc(glOne, glOneMinusSrcAlpha)
	glEnable(glScissorTest)
	if r := cstring(glGetString(glRenderer)); r != "" {
		v := cstring(glGetString(glVendor))
		glesDevName = strings.TrimSpace(v + " " + r)
	}
	if glesDevName == "" {
		glesDevName = devName
	}
	glesReady = true
	return nil
}

func gpuSubmit(dest unsafe.Pointer, w, h int, quads []Quad, batches []Batch, uploads []gpuUpload, wait bool) (encodeNs, waitNs int64, err error) {
	if !glesReady {
		return 0, 0, fmt.Errorf("gles not initialized")
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("bad target")
	}
	t0 := time.Now()
	if err := gpuBindDest(dest, w, h); err != nil {
		return 0, 0, err
	}

	glesApplyUploads(uploads)

	glBindVertexArray(glVAO)
	glBindBuffer(glArrayBuffer, glVBO)
	qbytes := len(quads) * int(unsafe.Sizeof(Quad{}))
	if len(quads) > 0 {
		glBufferData(glArrayBuffer, qbytes, unsafe.Pointer(&quads[0]), glDynamicDraw)
	}

	glUseProgram(glProg)
	glUniform2f(uViewport, float32(w), float32(h))
	glUniform1i(uTex, 0)
	glActiveTexture(glTexture0)

	for i := range batches {
		b := &batches[i]
		if b.Count <= 0 || b.ClipW <= 0 || b.ClipH <= 0 {
			continue
		}
		x, y, cw, ch := int(b.ClipX), int(b.ClipY), int(b.ClipW), int(b.ClipH)
		if x < 0 {
			cw += x
			x = 0
		}
		if y < 0 {
			ch += y
			y = 0
		}
		if x+cw > w {
			cw = w - x
		}
		if y+ch > h {
			ch = h - y
		}
		if cw <= 0 || ch <= 0 {
			continue
		}
		gpuScissor(x, y, cw, ch, w, h)

		tex := glTexWhite
		mode := uint32(0)
		switch b.TexKind {
		case texGlyph:
			tex = glTexGlyph
			mode = 1
		case texColorGlyph:
			tex = glTexColor
			mode = 2
		case texImage:
			if im, ok := texImages[uint32(b.TexKey)]; ok {
				tex = im
			}
			mode = 2
		}
		glBindTexture(glTexture2D, tex)
		glUniform1ui(uMode, mode)
		n := uint32(b.ClipN)
		if n > 4 {
			n = 4
		}
		glUniform1ui(uClipN, n)
		glUniform4fv(uClipRect, 4, &b.ClipRect[0][0])
		glUniform4fv(uClipRad, 4, &b.ClipRad[0][0])

		off := uintptr(b.First) * unsafe.Sizeof(Quad{})
		stride := int32(unsafe.Sizeof(Quad{}))
		glVertexAttribPointer(0, 4, glFloat, 0, stride, unsafe.Pointer(off))
		glVertexAttribPointer(1, 4, glFloat, 0, stride, unsafe.Pointer(off+16))
		glVertexAttribPointer(2, 4, glFloat, 0, stride, unsafe.Pointer(off+32))
		glVertexAttribPointer(3, 4, glFloat, 0, stride, unsafe.Pointer(off+48))
		glDrawArraysInstanced(glTriangles, 0, 6, b.Count)
	}

	encodeNs = time.Since(t0).Nanoseconds()
	waitNs = gpuPresent(dest, wait)
	if !wait && completeFn != nil {
		completeFn(dest)
	}
	runtime.KeepAlive(quads)
	runtime.KeepAlive(batches)
	runtime.KeepAlive(uploads)
	return encodeNs, waitNs, nil
}

func glesApplyUploads(uploads []gpuUpload) {
	if len(uploads) == 0 {
		return
	}
	glPixelStorei(glUnpackAlignment, 1)
	for i := range uploads {
		u := &uploads[i]
		if len(u.pix) == 0 || u.w <= 0 || u.h <= 0 {
			continue
		}
		var tex uint32
		var format uint32
		var bpp int
		switch u.kind {
		case 1:
			tex = glTexGlyph
			format = glRed
			bpp = 1
		case 2:
			tex = glTexColor
			format = glRGBA
			bpp = 4
		case 3:
			tex = texImages[u.imageID]
			format = glRGBA
			bpp = 4
		}
		if tex == 0 {
			continue
		}
		rowPix := u.stride / bpp
		if rowPix != u.w {
			glPixelStorei(glUnpackRowLength, int32(rowPix))
		} else {
			glPixelStorei(glUnpackRowLength, 0)
		}
		glBindTexture(glTexture2D, tex)
		glTexSubImage2D(glTexture2D, 0, int32(u.x), int32(u.y), int32(u.w), int32(u.h),
			format, glUnsignedByte, unsafe.Pointer(&u.pix[0]))
	}
	glPixelStorei(glUnpackRowLength, 0)
}

func glesMakeTex(internal uint32, format uint32, w, h int) uint32 {
	var id uint32
	glGenTextures(1, &id)
	glBindTexture(glTexture2D, id)
	glTexParameteri(glTexture2D, glTextureMinFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureMagFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureWrapS, glClampToEdge)
	glTexParameteri(glTexture2D, glTextureWrapT, glClampToEdge)
	glPixelStorei(glUnpackAlignment, 1)
	glTexImage2D(glTexture2D, 0, int32(internal), int32(w), int32(h), 0, format, glUnsignedByte, nil)
	return id
}

func glesZeroTex(id uint32, format uint32, bpp, w, h int) {
	z := make([]byte, w*h*bpp)
	glBindTexture(glTexture2D, id)
	glPixelStorei(glUnpackAlignment, 1)
	glPixelStorei(glUnpackRowLength, 0)
	glTexSubImage2D(glTexture2D, 0, 0, 0, int32(w), int32(h), format, glUnsignedByte, unsafe.Pointer(&z[0]))
}

func glesBuildProgram() error {
	vs, err := glesCompile(glVertexShader, glesVertSrc)
	if err != nil {
		return err
	}
	fs, err := glesCompile(glFragmentShader, glesFragSrc)
	if err != nil {
		glDeleteShader(vs)
		return err
	}
	prog := glCreateProgram()
	glAttachShader(prog, vs)
	glAttachShader(prog, fs)
	glLinkProgram(prog)
	glDeleteShader(vs)
	glDeleteShader(fs)
	var ok int32
	glGetProgramiv(prog, glLinkStatus, &ok)
	if ok == 0 {
		return fmt.Errorf("shader link: %s", glesLog(true, prog))
	}
	glProg = prog
	glUseProgram(prog)
	uViewport = glGetUniformLocation(prog, "viewport")
	uNdcY = glGetUniformLocation(prog, "ndcY")
	uDestOrig = glGetUniformLocation(prog, "destOrig")
	uDestFlip = glGetUniformLocation(prog, "destFlip")
	uMode = glGetUniformLocation(prog, "mode")
	uClipN = glGetUniformLocation(prog, "clipN")
	uClipRect = glGetUniformLocation(prog, "clipRects")
	uClipRad = glGetUniformLocation(prog, "clipRads")
	uTex = glGetUniformLocation(prog, "tex")
	return nil
}

func glesCompile(kind uint32, src string) (uint32, error) {
	sh := glCreateShader(kind)
	csrc := append([]byte(src), 0)
	p := uintptr(unsafe.Pointer(&csrc[0]))
	glShaderSource(sh, 1, &p, nil)
	glCompileShader(sh)
	runtime.KeepAlive(csrc)
	var ok int32
	glGetShaderiv(sh, glCompileStatus, &ok)
	if ok == 0 {
		log := glesLog(false, sh)
		glDeleteShader(sh)
		return 0, fmt.Errorf("shader compile: %s", log)
	}
	return sh, nil
}

func glesLog(program bool, id uint32) string {
	var n int32
	if program {
		glGetProgramiv(id, glInfoLogLength, &n)
	} else {
		glGetShaderiv(id, glInfoLogLength, &n)
	}
	if n <= 1 {
		return ""
	}
	buf := make([]byte, n)
	if program {
		glGetProgramInfoLog(id, n, nil, &buf[0])
	} else {
		glGetShaderInfoLog(id, n, nil, &buf[0])
	}
	return strings.TrimSpace(string(buf[:n-1]))
}
