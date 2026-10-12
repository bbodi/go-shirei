//go:build js

package jsbackend

import (
	"strconv"
	"syscall/js"

	. "go.hasen.dev/shirei"
)

// Client-side decorations for top-level fixed-size shells, modeled on
// waylandbackend's CSD (waylanddecor_linux.go):
//
//   - SetupWindow sizes are content/client (parity with macOS/Win32). The
//     floating shell is grown by titlebarHeight so chrome does not eat the body,
//     then clamped to the host slot so it never opens larger than the desk.
//     Mobile hosts and short/narrow host slots skip this and fill #shirei-root.
//   - The canvas is the full surface (titlebar + content).
//   - wrapFrame draws the titlebar in shirei and narrows Host.WindowSize so
//     the app (and popups) see only the content area.
//   - Titlebar drag moves the canvas inside #shirei-root (the host slot).
//   - Edge presses (resizeBorder px) start an interactive resize of that shell.
//
// #shirei-root fills leftover space in the page; in-flow siblings (a source
// strip, a toolbar, …) keep theirs. The shell is sized against that slot.
// Iframe embeds, mobile hosts, short/narrow host slots, and SetupWindow 0×0
// leave csd off. The 1px edge is a box-shadow outside the layout box and does
// not need a size inflate.

const titlebarHeight = 34
// resizeBorder is the surface-edge hit zone for interactive resize. Slightly
// wider than Wayland's 6 so the web shell is easier to grab (no compositor
// frame; only this interior strip counts).
const resizeBorder = 10
const chromeMinW = 200
const chromeMinH = 120 + titlebarHeight
// chromeMargin is the gap between the floating shell and the host slot edge.
const chromeMargin = 8
// A host slot this narrow or short uses the full-bleed shell (no CSD), even
// on a desktop OS — there is not enough desk for a floating window.
const fullBleedMaxW = 700
const fullBleedMaxH = 500

// csdActive is true while the top-level floating shell is in use.
func csdActive() bool {
	return floatingShell
}

// useFullBleedShell is true on a mobile host, or when the host slot is too
// small for a floating window. Iframe embeds are handled separately.
func useFullBleedShell() bool {
	if isMobileHost() {
		return true
	}
	vw, vh := deskSize()
	if vw <= 0 || vh <= 0 {
		return false
	}
	return vw <= fullBleedMaxW || vh <= fullBleedMaxH
}

// wrapFrame is the backend chrome wrapper (core knows nothing of decorations):
// root is sized to the full surface (Host.WindowSize set by tick before the
// frame), titlebar flows first, and WindowSize is narrowed to the content
// area while the app builds. It is restored to the surface size before
// returning so a settle pass still sizes the root to the full surface (leaving
// it narrowed permanently made the settle root too short by titlebarHeight —
// a white strip under the body). tick sets Host.WindowSize back to content
// after RunFrameFn so readers between frames see the client size.
func wrapFrame(appFn FrameFn) FrameFn {
	return func() {
		full := GetHost().WindowSize
		if csdActive() {
			GetHost().WindowSize[1] = full[1] - titlebarHeight
			drawTitlebar()
		}
		ContainerWithKey("app-content", Attrs(Viewport), func() {
			appFn()
		})
		GetHost().WindowSize = full
	}
}

// drawTitlebar builds the client-side title bar (title + close) and starts a
// page-level move when the bar is pressed (not on the close control).
func drawTitlebar() {
	Container(Attrs(Row, Expand, FixHeight(titlebarHeight), Background(0, 0, 88, 1),
		Grad(0, 0, -5, 0), CrossAlign(AlignMiddle), Pad2(0, 8), Gap(8), NoAnimate), func() {
		startDrag := IsClicked()
		title := winTitle
		if title == "" {
			title = "shirei"
		}
		Label(title, FontSize(14), TextColor(0, 0, 25, 1))
		Element(Attrs(Grow(1)))
		if closeButton() {
			closeShell()
		} else if startDrag {
			startMove()
		}
	})
}

func closeButton() bool {
	clicked := false
	Container(Attrs(Row, Center, FixSize(26, 26), Corners(5)), func() {
		if IsHovered() {
			ModAttrs(Background(5, 70, 62, 1))
		}
		if IsClicked() {
			clicked = true
		}
		Label("×", FontSize(20), TextColor(0, 0, 25, 1))
	})
	return clicked
}

func closeShell() {
	// Script-opened windows can close; otherwise hide the floating shell.
	win := js.Global()
	win.Call("close")
	doc := win.Get("document")
	if root := doc.Call("getElementById", "shirei-root"); root.Truthy() {
		root.Get("style").Set("display", "none")
	}
}

// --- interactive move / resize (page-level; compositor has no grab) ---------

type chromeMode int

const (
	chromeIdle chromeMode = iota
	chromeMove
	chromeResize
)

var (
	chromeAction   chromeMode
	chromeEdge     uint32 // bit flags, same layout as xdg edges (see below)
	chromeStartX   float64
	chromeStartY   float64
	chromeOrigL    float64
	chromeOrigT    float64
	chromeOrigW    int
	chromeOrigH    int
	frameLeft      float64
	frameTop       float64
	framePlaced    bool
)

// Edge flags (mirror xdg_toplevel resize edges; only used locally).
const (
	edgeNone   uint32 = 0
	edgeTop    uint32 = 1
	edgeBottom uint32 = 2
	edgeLeft   uint32 = 4
	edgeRight  uint32 = 8
)

func startMove() {
	if !csdActive() {
		return
	}
	chromeAction = chromeMove
	// Seed start from current pointer so the first move is a no-op offset.
	// client coords are filled on the next pointer event via chromePointerMove.
	pt := GetInputState().MousePoint
	dl, dt := deskClientOrigin()
	chromeStartX = dl + frameLeft + float64(pt[0])
	chromeStartY = dt + frameTop + float64(pt[1])
	chromeOrigL = frameLeft
	chromeOrigT = frameTop
	chromeOrigW = winW
	chromeOrigH = winH
}

// tryStartResize begins a resize if the press is in the edge border of the
// full surface. Returns true if the press was consumed (do not deliver click).
func tryStartResize(clientX, clientY float64) bool {
	if !csdActive() {
		return false
	}
	dl, dt := deskClientOrigin()
	sx := float32(clientX - dl - frameLeft)
	sy := float32(clientY - dt - frameTop)
	edge := resizeEdgeAt(sx, sy, float32(winW), float32(winH))
	if edge == edgeNone {
		return false
	}
	chromeAction = chromeResize
	chromeEdge = edge
	chromeStartX = clientX
	chromeStartY = clientY
	chromeOrigL = frameLeft
	chromeOrigT = frameTop
	chromeOrigW = winW
	chromeOrigH = winH
	return true
}

func chromePointerMove(clientX, clientY float64) {
	if chromeAction == chromeIdle {
		return
	}
	dx := clientX - chromeStartX
	dy := clientY - chromeStartY
	if chromeAction == chromeMove {
		frameLeft = chromeOrigL + dx
		frameTop = chromeOrigT + dy
		framePlaced = true
		applyFrameGeometry()
		return
	}
	// resize
	w, h := chromeOrigW, chromeOrigH
	l, t := chromeOrigL, chromeOrigT
	if chromeEdge&edgeRight != 0 {
		w = chromeOrigW + int(dx+0.5)
	}
	if chromeEdge&edgeBottom != 0 {
		h = chromeOrigH + int(dy+0.5)
	}
	if chromeEdge&edgeLeft != 0 {
		w = chromeOrigW - int(dx+0.5)
		l = chromeOrigL + dx
	}
	if chromeEdge&edgeTop != 0 {
		h = chromeOrigH - int(dy+0.5)
		t = chromeOrigT + dy
	}
	if w < chromeMinW {
		if chromeEdge&edgeLeft != 0 {
			l -= float64(chromeMinW - w)
		}
		w = chromeMinW
	}
	if h < chromeMinH {
		if chromeEdge&edgeTop != 0 {
			t -= float64(chromeMinH - h)
		}
		h = chromeMinH
	}
	winW, winH = w, h
	frameLeft, frameTop = l, t
	framePlaced = true
	constrainShell()
	applyFrameGeometry()
}

func chromePointerUp() {
	chromeAction = chromeIdle
	chromeEdge = edgeNone
}

func resizeEdgeAt(x, y, w, h float32) uint32 {
	left := x < resizeBorder
	right := x > w-resizeBorder
	top := y < resizeBorder
	bottom := y > h-resizeBorder
	var edge uint32
	if top {
		edge |= edgeTop
	}
	if bottom {
		edge |= edgeBottom
	}
	if left {
		edge |= edgeLeft
	}
	if right {
		edge |= edgeRight
	}
	return edge
}

// deskElem is the host slot (#shirei-root): leftover page space after in-flow
// siblings. The floating canvas is positioned inside it.
func deskElem() js.Value {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return js.Undefined()
	}
	return doc.Call("getElementById", "shirei-root")
}

func deskSize() (w, h float64) {
	if root := deskElem(); root.Truthy() {
		w = root.Get("clientWidth").Float()
		h = root.Get("clientHeight").Float()
	}
	if w <= 0 {
		w = js.Global().Get("innerWidth").Float()
	}
	if h <= 0 {
		h = js.Global().Get("innerHeight").Float()
	}
	return w, h
}

func deskClientOrigin() (left, top float64) {
	root := deskElem()
	if !root.Truthy() {
		return 0, 0
	}
	r := root.Call("getBoundingClientRect")
	if !r.Truthy() {
		return 0, 0
	}
	return r.Get("left").Float(), r.Get("top").Float()
}

// availRect is the desk the floating shell may occupy, in host-slot coordinates:
// the host's content box minus chromeMargin.
func availRect() (x, y, w, h float64, ok bool) {
	cw, ch := deskSize()
	if cw <= 0 || ch <= 0 {
		return 0, 0, 0, 0, false
	}
	m := float64(chromeMargin)
	x, y = m, m
	w = cw - 2*m
	h = ch - 2*m
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return x, y, w, h, true
}

// placeNewShell caps the floating window to the available desk and centers it.
func placeNewShell() {
	ax, ay, aw, ah, ok := availRect()
	if !ok {
		frameLeft = chromeMargin
		frameTop = chromeMargin
		framePlaced = true
		return
	}
	maxW, maxH := int(aw), int(ah)
	if winW > maxW {
		winW = maxW
	}
	if winH > maxH {
		winH = maxH
	}
	frameLeft = ax + (aw-float64(winW))/2
	frameTop = ay + (ah-float64(winH))/2
	framePlaced = true
}

// constrainShell caps the floating window to the available desk and shifts
// it so the rect stays on-screen. Interactive min-size can lose to this cap
// when the viewport is smaller than chromeMinW/chromeMinH.
func constrainShell() {
	ax, ay, aw, ah, ok := availRect()
	if !ok {
		return
	}
	maxW, maxH := int(aw), int(ah)
	if winW > maxW {
		winW = maxW
	}
	if winH > maxH {
		winH = maxH
	}
	if frameLeft < ax {
		frameLeft = ax
	}
	if frameTop < ay {
		frameTop = ay
	}
	if frameLeft+float64(winW) > ax+aw {
		frameLeft = ax + aw - float64(winW)
	}
	if frameTop+float64(winH) > ay+ah {
		frameTop = ay + ah - float64(winH)
	}
}

// applyFrameGeometry writes the floating canvas position and size to the DOM.
func applyFrameGeometry() {
	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", "shirei-canvas")
	if !canvas.Truthy() {
		return
	}
	cs := canvas.Get("style")
	cs.Set("left", strconv.FormatFloat(frameLeft, 'f', 0, 64)+"px")
	cs.Set("top", strconv.FormatFloat(frameTop, 'f', 0, 64)+"px")
	cs.Set("width", strconv.Itoa(winW)+"px")
	cs.Set("height", strconv.Itoa(winH)+"px")
	if html := doc.Get("documentElement"); html.Truthy() {
		html.Call("setAttribute", "data-shirei-width", strconv.Itoa(winW))
		html.Call("setAttribute", "data-shirei-height", strconv.Itoa(winH))
	}
	notifyParentSize(winW, winH)
	RequestNextFrame()
}

// cursorForEdge returns a CSS cursor for the edge under the surface point.
func cursorForEdge(edge uint32) string {
	switch edge {
	case edgeTop | edgeLeft, edgeBottom | edgeRight:
		return "nwse-resize"
	case edgeTop | edgeRight, edgeBottom | edgeLeft:
		return "nesw-resize"
	case edgeLeft, edgeRight:
		return "ew-resize"
	case edgeTop, edgeBottom:
		return "ns-resize"
	default:
		return ""
	}
}
