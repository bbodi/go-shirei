// Embedding the overlay in a host that is not a Shirei app: a game with its
// own OpenGL loop, a video canvas, anything that already owns the window and
// the frame timing. Such a host has no Shirei backend — no event loop, no
// window, nothing to present a frame — so this file supplies the missing
// half: a frame driver, input translation, and a pixel buffer the host
// composites over its own picture.
//
// The buffer is transparent everywhere the frame does not paint, which is the
// whole point: the host keeps its scene and gets the panel on top of it.
package perfhud

import (
	"time"

	. "go.hasen.dev/shirei"
)

// An Overlay drives the panel inside a foreign host.
//
//	var hud perfhud.Overlay
//
//	hud.Init() // once, before the first frame
//
//	// every frame, after drawing the scene:
//	hud.Pointer(cursorX, cursorY) // logical points, or PointerAway()
//	if pix := hud.Frame(fbW, fbH, scale, nil); pix != nil {
//		w, h := hud.Size()
//		uploadRGBATexture(pix, w, h)
//	}
//
// The pixels are premultiplied, top-down, at device resolution, so the host
// composites them as `dst = src.rgb + dst*(1-src.a)`. Frame returns nil when
// the picture has not changed since the last upload, so a host that keeps its
// texture around uploads only on a real change.
//
// One Overlay per process: Shirei has one UI, and Frame runs a frame of it.
type Overlay struct {
	// Repaint is the shortest gap between repaints while only the numbers are
	// moving. Input repaints immediately, whatever this says. Zero means the
	// default below — the charts do not need 200 Hz, and every repaint is a
	// software render plus a texture upload the host pays for.
	Repaint time.Duration

	// GlyphCacheBytes is the budget for the shared glyph cache. Text does not
	// render at all with no cache, so zero means the default below.
	GlyphCacheBytes int

	rend    SoftRenderer
	buf     []byte
	devW    int
	devH    int
	content bool // the last frame painted something
	painted bool // buf holds a frame the host has been given
	hash    uint64
	dirty   bool // input arrived since the last repaint
	away    bool // the pointer is parked: the next move is not a drag
	next    time.Time
}

const (
	defaultRepaint         = 50 * time.Millisecond
	defaultGlyphCacheBytes = 16 << 20
)

// Init prepares the Shirei host for a foreign embedder. Call it once before
// the first Frame. It leaves the pixel order at RGBA, which is what a GL
// texture or a canvas wants; a host that presents BGRA sets
// GetHost().PixelOrder = PixelOrderBGRA afterwards.
func (o *Overlay) Init() {
	h := GetHost()
	budget := o.GlyphCacheBytes
	if budget == 0 {
		budget = defaultGlyphCacheBytes
	}
	h.GlyphCacheBudgetBytes = budget
	h.PixelOrder = PixelOrderRGBA
	o.rend.Transparent = true
	o.PointerAway()
}

// Frame runs one UI frame at the given device size and scale (device pixels
// per logical point) and returns the pixels to upload, or nil when the
// picture is unchanged. fn builds the host's own UI first, so the panel
// floats over it; pass nil for the panel alone.
//
// It samples on every call and runs the hitch detector whether or not the
// panel is visible, so a host calls it every frame and stops compositing when
// HasContent goes false.
func (o *Overlay) Frame(devW, devH int, scale float32, fn FrameFn) []byte {
	if devW <= 0 || devH <= 0 {
		o.content = false
		return nil
	}
	if scale <= 0 {
		scale = 1
	}
	h := GetHost()
	h.WindowScale = scale
	h.WindowSize = Vec2{float32(devW) / scale, float32(devH) / scale}

	out := RunFrameFn(func() {
		if fn != nil {
			fn()
		}
		Draw() // last, so the panel floats over the host's own UI
	})

	o.content = anyPaints(out.Surfaces)
	if !o.content {
		o.painted = false
		return nil
	}
	resized := o.devW != devW || o.devH != devH
	if !o.repaintNow(out.SurfacesHash, resized) {
		return nil
	}
	o.devW, o.devH, o.hash, o.painted, o.dirty = devW, devH, out.SurfacesHash, true, false

	if need := devW * devH * 4; cap(o.buf) < need {
		o.buf = make([]byte, need)
	} else {
		o.buf = o.buf[:need]
	}
	o.rend.RenderInto(o.buf, devW*4, devW, devH, scale, out.Surfaces)
	return o.buf
}

// anyPaints reports whether the frame drew anything. The root container
// always emits a surface, so the count alone says nothing.
func anyPaints(surfaces []Surface) bool {
	for i := range surfaces {
		if SurfacePaints(&surfaces[i]) {
			return true
		}
	}
	return false
}

// repaintNow decides whether this frame is worth rendering: anything the user
// did, a resize, or a content change that is past the repaint interval.
func (o *Overlay) repaintNow(hash uint64, resized bool) bool {
	if o.painted && !resized && !o.dirty {
		if hash == o.hash {
			return false
		}
		if time.Now().Before(o.next) {
			return false
		}
	}
	gap := o.Repaint
	if gap == 0 {
		gap = defaultRepaint
	}
	o.next = time.Now().Add(gap)
	return true
}

// Size reports the device size of the last buffer Frame returned.
func (o *Overlay) Size() (w, h int) { return o.devW, o.devH }

// HasContent reports whether the last frame painted anything. False means the
// host has nothing to composite — the panel is hidden and the host's own UI,
// if any, built nothing.
func (o *Overlay) HasContent() bool { return o.content }

// WantsPointer reports whether the pointer is over the UI, so the host can
// tell a click meant for a panel row from a click meant for its own scene. It
// answers for the frame on screen, which is the frame the user is aiming at.
func (o *Overlay) WantsPointer() bool { return AnyHovered() }

// Pointer moves the cursor, in logical points relative to the surface's top
// left corner.
func (o *Overlay) Pointer(x, y float32) {
	np := Vec2{x, y}
	in := GetInputState()
	if o.away {
		o.away = false // the jump back from the parking spot is not a drag
	} else {
		fi := GetFrameInput()
		fi.Motion = Vec2Add(fi.Motion, Vec2Sub(np, in.MousePoint))
	}
	in.MousePoint = np
	o.dirty = true
}

// PointerAway parks the cursor out of reach, so nothing is left hovered. A
// host calls it when it takes the cursor for itself (mouse-look capture) or
// when the pointer leaves the window.
func (o *Overlay) PointerAway() {
	GetInputState().MousePoint = Vec2{-1 << 20, -1 << 20}
	o.away = true
	o.dirty = true
}

// Button names a pointer button, so a host names one without importing
// Shirei core.
type Button int

const (
	ButtonPrimary Button = iota
	ButtonSecondary
	ButtonTertiary
)

func (b Button) mouseButton() MouseButton {
	switch b {
	case ButtonSecondary:
		return MouseSecondary
	case ButtonTertiary:
		return MouseTertiary
	default:
		return MousePrimary
	}
}

// Press and Release report a button edge. Shirei carries one edge per frame,
// so a host that sees a press and its release between two Frame calls must
// deliver them one frame apart or the press is lost.
func (o *Overlay) Press(b Button) { o.edge(b, MouseClick) }

// Release reports the button coming back up. See Press.
func (o *Overlay) Release(b Button) { o.edge(b, MouseRelease) }

func (o *Overlay) edge(b Button, action MouseAction) {
	GetInputState().MouseButton = b.mouseButton()
	GetFrameInput().Mouse = action
	o.dirty = true
}

// WheelNotch is the scroll distance of one wheel detent, in points. It
// matches Shirei's own backends, so a host with a notched wheel passes
// multiples of it.
const WheelNotch = 30

// Scroll adds wheel movement, in points: negative dy is up, positive dx is
// right (Shirei's convention, which is the inverse of a typical windowing
// toolkit's vertical sign).
func (o *Overlay) Scroll(dx, dy float32) {
	fi := GetFrameInput()
	fi.Scroll = Vec2Add(fi.Scroll, Vec2{dx, dy})
	o.dirty = true
}
