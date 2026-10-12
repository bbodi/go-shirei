//go:build js

// Package jsbackend is the browser/wasm shell for shirei. The page owns the
// event loop (requestAnimationFrame). Present is GLES/WebGL2 via gpurender
// unless SHIREI_GPU=0 or WebGL2 init fails; then SoftRenderer + putImageData.
//
// Text uses the same pure-Go shaping/glyph path as every other backend. The
// shell embeds a default Noto Sans face so demos render without a system font
// scan (fontscan has no js directory list). Apps may still call UseFontBytes
// or fetch more faces at runtime.
package jsbackend

import (
	"strconv"
	"strings"
	"syscall/js"
	"time"

	_ "embed"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
)

//go:embed NotoSans-Regular.ttf
var defaultFontBytes []byte

const glyphCacheBudget = 16 << 20

var (
	winTitle string
	prefW    int // SetupWindow content size; not inflated, not live-resized
	prefH    int
	winW     int
	winH     int
	// shellInflated is set once winH has been grown by titlebarHeight for a
	// top-level CSD shell (SetupWindow sizes are content; winW/winH track the
	// full surface after inflate and during interactive resize).
	shellInflated bool
	// floatingShell is the live CSD mode: a top-level desktop-sized page with
	// a fixed SetupWindow. Mobile hosts and short/narrow viewports stay false
	// so the canvas fills the page (no titlebar).
	floatingShell bool
	frameFn       shirei.FrameFn

	softRenderer shirei.SoftRenderer
	pix          []byte // Host.PixelOrderRGBA framebuffer (software path)
	bufW, bufH   int
	havePainted  bool
	gpuOK        bool
	ctx2d        js.Value

	// input is the pure state machine; DOM listeners only feed it.
	input *inputAccum

	clipboardPaste string
	pastePending   bool

	// keep rAF Func alive so GC does not free the callback
	rafCB      js.Func
	pagehideCB js.Func
	resizeCB   js.Func
)

// SetupWindow records title and preferred CSS-pixel content size (same contract
// as macOS/Win32). On a top-level desktop-sized page a floating shell draws CSD
// and is taller by titlebarHeight so the app body keeps the requested size; it is
// shrunk if that would overflow the host slot (#shirei-root). Mobile hosts and
// short/narrow host slots fill that slot with no chrome (same as iOS/Android).
// Zero width or height also fills the slot (iframe embeds still shrink-wrap to w×h).
func SetupWindow(title string, width, height int) {
	winTitle = title
	prefW = width
	prefH = height
	winW = width
	winH = height
	shellInflated = false
	floatingShell = false
}

// SetupIcon is a no-op for the first cut (favicon can be set in HTML).
func SetupIcon(imagePath string) { _ = imagePath }

// SetupIconImage is a no-op for the first cut.
func SetupIconImage(img any) { _ = img }

// SetupIconBytes is a no-op for the first cut.
func SetupIconBytes(data []byte) { _ = data }

// Run attaches to the page canvas, registers the default UI font, and drives
// frames via requestAnimationFrame. It blocks forever (the browser owns exit).
func Run(fn shirei.FrameFn) {
	frameFn = wrapFrame(fn)

	host := shirei.GetHost()
	host.GlyphCacheBudgetBytes = glyphCacheBudget
	host.EscapeHatchBackendContext = Context{}
	host.HardwareKeyboard = true
	host.ComfortScale = 1
	host.WindowFocused = true
	// Canvas ImageData is RGBA; render in that order (no Go B↔R swizzle).
	host.PixelOrder = shirei.PixelOrderRGBA
	// GOOS is always "js"; shortcuts must follow the *browser host* OS so
	// Cmd+A select-all works on Mac the way Ctrl+A does on Windows/Linux.
	apple := browserHostIsApple()
	if apple {
		host.PrimaryMod = shirei.ModCmd
	} else {
		host.PrimaryMod = shirei.ModCtrl
	}
	input = newInputAccum(apple)

	if err := shirei.UseFontBytes(defaultFontBytes); err != nil {
		js.Global().Get("console").Call("error", "shirei: default font: "+err.Error())
	}

	doc := js.Global().Get("document")
	if winTitle != "" {
		doc.Set("title", winTitle)
	}

	canvas := ensureShell(doc)
	tryGPU(canvas)
	if !gpuOK {
		ctx2d = canvas.Call("getContext", "2d", map[string]any{"alpha": false})
	}
	installInput(doc, canvas)
	installTextField(doc)

	// Export a small hook so the HTML loader can inject extra font bytes later.
	js.Global().Set("shireiUseFontBytes", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return nil
		}
		src := args[0]
		n := src.Get("byteLength").Int()
		buf := make([]byte, n)
		js.CopyBytesToGo(buf, src)
		if err := shirei.UseFontBytes(buf); err != nil {
			js.Global().Get("console").Call("error", "shireiUseFontBytes: "+err.Error())
		} else {
			shirei.RequestNextFrame()
		}
		return nil
	}))

	rafCB = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer js.Global().Call("requestAnimationFrame", rafCB)
		tick(canvas)
		return nil
	})
	js.Global().Call("requestAnimationFrame", rafCB)

	// Tab close / navigation: flush AddExitCleanup. Skip bfcache (persisted)
	// so hiding the page does not kill the wasm instance.
	pagehideCB = js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("persisted").Bool() {
			return nil
		}
		generic.ExitWithCleanup(0)
		return nil
	})
	js.Global().Call("addEventListener", "pagehide", pagehideCB)

	// Viewport changes: re-apply layout so a phone rotation or a desktop
	// window crossing the full-bleed threshold picks the right shell.
	resizeCB = js.FuncOf(func(this js.Value, args []js.Value) any {
		doc := js.Global().Get("document")
		root := doc.Call("getElementById", "shirei-root")
		applyShellLayout(doc, root, canvas)
		shirei.RequestNextFrame()
		return nil
	})
	js.Global().Call("addEventListener", "resize", resizeCB)

	// Park the main goroutine; rAF drives everything else.
	select {}
}

// ensureShell finds or creates #shirei-root > #shirei-canvas and applies
// SetupWindow size: a clamped floating CSD shell inside the host slot on a
// top-level desktop-sized page, exact-fit in an iframe, otherwise fill the slot.
func ensureShell(doc js.Value) js.Value {
	body := doc.Get("body")
	if !body.Truthy() {
		js.Global().Get("console").Call("error", "shirei: document.body missing")
	}

	root := doc.Call("getElementById", "shirei-root")
	canvas := doc.Call("getElementById", "shirei-canvas")

	if !root.Truthy() {
		root = doc.Call("createElement", "div")
		root.Set("id", "shirei-root")
		if body.Truthy() {
			if canvas.Truthy() && canvas.Get("parentNode").Truthy() {
				canvas.Get("parentNode").Call("insertBefore", root, canvas)
				root.Call("appendChild", canvas)
			} else {
				body.Call("appendChild", root)
			}
		}
	}
	if !canvas.Truthy() {
		canvas = doc.Call("createElement", "canvas")
		canvas.Set("id", "shirei-canvas")
		root.Call("appendChild", canvas)
	} else if parent := canvas.Get("parentNode"); parent.Truthy() && parent.Get("id").String() != "shirei-root" {
		root.Call("appendChild", canvas)
	}

	// Drop any leftover HTML chrome frame from earlier experiments.
	if frame := doc.Call("getElementById", "shirei-window"); frame.Truthy() {
		parent := frame.Get("parentNode")
		if parent.Truthy() && root.Truthy() {
			parent.Call("insertBefore", root, frame)
			frame.Call("remove")
		}
	}

	canvas.Call("setAttribute", "tabindex", "0")
	applyShellLayout(doc, root, canvas)
	return canvas
}

func applyShellLayout(doc, root, canvas js.Value) {
	if !root.Truthy() {
		return
	}
	html := doc.Get("documentElement")
	body := doc.Get("body")
	rs := root.Get("style")
	cs := canvas.Get("style")

	cs.Set("display", "block")
	cs.Set("width", "100%")
	cs.Set("height", "100%")
	cs.Set("touchAction", "none")
	cs.Set("outline", "none")

	embedded := isEmbeddedFrame()
	fixed := prefW > 0 && prefH > 0

	if embedded && fixed {
		floatingShell = false
		shellInflated = false
		winW, winH = prefW, prefH
		rs.Set("width", strconv.Itoa(winW)+"px")
		rs.Set("height", strconv.Itoa(winH)+"px")
		rs.Set("overflow", "hidden")
		rs.Set("background", "#fff")
		// Tight fit so the parent iframe can match SetupWindow exactly.
		if html.Truthy() {
			hs := html.Get("style")
			hs.Set("margin", "0")
			hs.Set("width", strconv.Itoa(winW)+"px")
			hs.Set("height", strconv.Itoa(winH)+"px")
		}
		if body.Truthy() {
			bs := body.Get("style")
			bs.Set("margin", "0")
			bs.Set("width", strconv.Itoa(winW)+"px")
			bs.Set("height", strconv.Itoa(winH)+"px")
			bs.Set("display", "block")
			bs.Set("overflow", "hidden")
			bs.Set("background", "#fff")
		}
		rs.Set("position", "relative")
		rs.Set("boxShadow", "none")
		rs.Set("borderRadius", "0")
		cs.Set("position", "relative")
		cs.Set("left", "")
		cs.Set("top", "")
		cs.Set("width", "100%")
		cs.Set("height", "100%")
		cs.Set("boxShadow", "none")
		cs.Set("borderRadius", "0")
		if html.Truthy() {
			html.Call("setAttribute", "data-shirei-width", strconv.Itoa(winW))
			html.Call("setAttribute", "data-shirei-height", strconv.Itoa(winH))
		}
		notifyParentSize(winW, winH)
		return
	}

	// Top-level: #shirei-root fills leftover space in the page (any in-flow
	// siblings keep theirs). The floating window is the canvas inside that slot.
	if html.Truthy() {
		hs := html.Get("style")
		hs.Set("margin", "0")
		hs.Set("height", "100%")
		hs.Set("width", "100%")
	}
	if body.Truthy() {
		bs := body.Get("style")
		bs.Set("margin", "0")
		bs.Set("width", "100%")
		bs.Set("height", "100%")
		bs.Set("display", "flex")
		bs.Set("flexDirection", "column")
		bs.Set("overflow", "hidden")
		bs.Set("background", "#fff")
	}
	rs.Set("flex", "1")
	rs.Set("minHeight", "0")
	rs.Set("minWidth", "0")
	rs.Set("width", "auto")
	rs.Set("height", "auto")
	rs.Set("position", "relative")
	rs.Set("left", "")
	rs.Set("top", "")
	rs.Set("overflow", "hidden")
	rs.Set("boxShadow", "none")
	rs.Set("borderRadius", "0")

	wantFloat := fixed && !useFullBleedShell()
	if wantFloat {
		if !floatingShell {
			winW = prefW
			winH = prefH + titlebarHeight
			shellInflated = true
			framePlaced = false
			placeNewShell()
			floatingShell = true
		} else {
			constrainShell()
		}
		rs.Set("background", "#e8e8ee")
		cs.Set("position", "absolute")
		cs.Set("boxSizing", "border-box")
		cs.Set("left", strconv.FormatFloat(frameLeft, 'f', 0, 64)+"px")
		cs.Set("top", strconv.FormatFloat(frameTop, 'f', 0, 64)+"px")
		cs.Set("width", strconv.Itoa(winW)+"px")
		cs.Set("height", strconv.Itoa(winH)+"px")
		cs.Set("boxShadow", "0 0 0 1px rgba(0,0,0,0.12), 0 8px 28px rgba(0,0,0,0.18), 0 2px 6px rgba(0,0,0,0.08)")
		cs.Set("borderRadius", "6px")
		if html.Truthy() {
			html.Call("setAttribute", "data-shirei-width", strconv.Itoa(winW))
			html.Call("setAttribute", "data-shirei-height", strconv.Itoa(winH))
		}
		notifyParentSize(winW, winH)
		return
	}

	floatingShell = false
	shellInflated = false
	if html.Truthy() {
		html.Call("removeAttribute", "data-shirei-width")
		html.Call("removeAttribute", "data-shirei-height")
	}
	rs.Set("background", "#fff")
	cs.Set("position", "relative")
	cs.Set("left", "")
	cs.Set("top", "")
	cs.Set("width", "100%")
	cs.Set("height", "100%")
	cs.Set("boxShadow", "none")
	cs.Set("borderRadius", "0")
}

func isEmbeddedFrame() bool {
	// window !== window.parent
	win := js.Global()
	parent := win.Get("parent")
	return parent.Truthy() && !parent.Equal(win)
}

// notifyParentSize tells an embedding page the SetupWindow CSS size so it can
// resize the iframe. Message shape:
//
//	{ source: "shirei", type: "resize", width: N, height: N }
func notifyParentSize(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	parent := js.Global().Get("parent")
	if !parent.Truthy() {
		return
	}
	msg := map[string]any{
		"source": "shirei",
		"type":   "resize",
		"width":  w,
		"height": h,
	}
	// targetOrigin * — demos are often file:// or various hosts in dev.
	parent.Call("postMessage", msg, "*")
}

func installTextField(doc js.Value) {
	// Hidden input captures IME composition and text commits (Latin + CJK).
	inp := doc.Call("getElementById", "shirei-text")
	if !inp.Truthy() {
		inp = doc.Call("createElement", "input")
		inp.Set("id", "shirei-text")
		inp.Set("type", "text")
		inp.Set("autocomplete", "off")
		inp.Set("autocorrect", "off")
		inp.Set("autocapitalize", "off")
		inp.Set("spellcheck", false)
		st := inp.Get("style")
		st.Set("position", "fixed")
		st.Set("left", "0")
		st.Set("top", "0")
		st.Set("width", "1px")
		st.Set("height", "1px")
		st.Set("opacity", "0")
		st.Set("pointerEvents", "none")
		st.Set("zIndex", "-1")
		doc.Get("body").Call("appendChild", inp)
	}

	inp.Call("addEventListener", "beforeinput", js.FuncOf(func(this js.Value, args []js.Value) any {
		if !documentHasFocus() {
			return nil
		}
		e := args[0]
		data := e.Get("data")
		if data.Truthy() && data.Type() == js.TypeString {
			s := data.String()
			if s != "" {
				input.textInsert(s)
			}
		}
		return nil
	}))
	inp.Call("addEventListener", "compositionstart", js.FuncOf(func(this js.Value, args []js.Value) any {
		input.compositionStart()
		return nil
	}))
	inp.Call("addEventListener", "compositionupdate", js.FuncOf(func(this js.Value, args []js.Value) any {
		input.compositionUpdate(args[0].Get("data").String())
		return nil
	}))
	inp.Call("addEventListener", "compositionend", js.FuncOf(func(this js.Value, args []js.Value) any {
		input.compositionEnd(args[0].Get("data").String())
		return nil
	}))
}

func installInput(doc, canvas js.Value) {
	// Pointer Events + setPointerCapture: move/up continue after the cursor
	// leaves the canvas (same role as Win32 SetCapture). Only primary button
	// is wired; secondary/middle are not delivered yet.
	//
	// CSD (top-level fixed shells): edge presses start resize; titlebar drag
	// is started from wrapFrame during the frame (like Wayland startMove).
	pointFromEvent := func(e js.Value) {
		input.setPoint(e.Get("offsetX").Float(), e.Get("offsetY").Float())
	}

	canvas.Call("addEventListener", "pointermove", js.FuncOf(func(this js.Value, args []js.Value) any {
		e := args[0]
		cx, cy := e.Get("clientX").Float(), e.Get("clientY").Float()
		if chromeAction != chromeIdle {
			chromePointerMove(cx, cy)
			return nil
		}
		pointFromEvent(e)
		// Resize cursor when hovering edges (full surface coords = offset).
		if csdActive() {
			edge := resizeEdgeAt(
				float32(e.Get("offsetX").Float()),
				float32(e.Get("offsetY").Float()),
				float32(winW), float32(winH),
			)
			if cur := cursorForEdge(edge); cur != "" {
				canvas.Get("style").Set("cursor", cur)
			} else {
				canvas.Get("style").Set("cursor", "default")
			}
		}
		return nil
	}))
	canvas.Call("addEventListener", "pointerdown", js.FuncOf(func(this js.Value, args []js.Value) any {
		e := args[0]
		cx, cy := e.Get("clientX").Float(), e.Get("clientY").Float()
		pointFromEvent(e)
		if e.Get("button").Int() == 0 {
			// Capture so pointerup/move outside the canvas still hit us.
			if id := e.Get("pointerId"); id.Truthy() {
				canvas.Call("setPointerCapture", id)
			}
			// Edge resize consumes the press (Wayland tryStartResize).
			if tryStartResize(cx, cy) {
				e.Call("preventDefault")
				return nil
			}
			input.primaryDown()
		}
		canvas.Call("focus")
		return nil
	}))
	releaseCapture := func(e js.Value) {
		if id := e.Get("pointerId"); id.Truthy() {
			if canvas.Call("hasPointerCapture", id).Bool() {
				canvas.Call("releasePointerCapture", id)
			}
		}
	}
	canvas.Call("addEventListener", "pointerup", js.FuncOf(func(this js.Value, args []js.Value) any {
		e := args[0]
		if chromeAction != chromeIdle {
			chromePointerUp()
		} else {
			pointFromEvent(e)
		}
		// Always release primary if held — titlebar move starts after
		// primaryDown, and resize may leave held false.
		if e.Get("button").Int() == 0 || input.held {
			if input.held {
				input.primaryUp()
			}
		}
		releaseCapture(e)
		return nil
	}))
	canvas.Call("addEventListener", "pointercancel", js.FuncOf(func(this js.Value, args []js.Value) any {
		e := args[0]
		if chromeAction != chromeIdle {
			chromePointerUp()
		} else {
			pointFromEvent(e)
		}
		if input.held {
			input.primaryUp()
		}
		releaseCapture(e)
		return nil
	}))

	canvas.Call("addEventListener", "wheel", js.FuncOf(func(this js.Value, args []js.Value) any {
		e := args[0]
		e.Call("preventDefault")
		input.wheel(e.Get("deltaX").Float(), e.Get("deltaY").Float(), e.Get("deltaMode").Int())
		return nil
	}), map[string]any{"passive": false})

	// Keyboard on window so we receive keys even when the hidden field is focused.
	// Ignore when this document is not the focused browsing context — important
	// on gallery pages with many same-origin iframes.
	js.Global().Call("addEventListener", "keydown", js.FuncOf(func(this js.Value, args []js.Value) any {
		if !documentHasFocus() {
			return nil
		}
		e := args[0]
		input.setMods(
			e.Get("shiftKey").Bool(),
			e.Get("ctrlKey").Bool(),
			e.Get("altKey").Bool(),
			e.Get("metaKey").Bool(),
		)
		if input.keyDown(e.Get("code").String()) {
			e.Call("preventDefault")
		}
		return nil
	}))
	js.Global().Call("addEventListener", "keyup", js.FuncOf(func(this js.Value, args []js.Value) any {
		if !documentHasFocus() {
			return nil
		}
		e := args[0]
		input.setMods(
			e.Get("shiftKey").Bool(),
			e.Get("ctrlKey").Bool(),
			e.Get("altKey").Bool(),
			e.Get("metaKey").Bool(),
		)
		input.keyUp(e.Get("code").String())
		return nil
	}))
	js.Global().Call("addEventListener", "blur", js.FuncOf(func(this js.Value, args []js.Value) any {
		shirei.GetHost().WindowFocused = false
		// Drop latched keys so a blurred iframe does not keep "holding" them.
		input.clearKeyboard()
		return nil
	}))
	js.Global().Call("addEventListener", "focus", js.FuncOf(func(this js.Value, args []js.Value) any {
		shirei.GetHost().WindowFocused = true
		return nil
	}))
}

func documentHasFocus() bool {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return false
	}
	// true only when this document (this iframe) owns keyboard focus
	return doc.Call("hasFocus").Bool()
}

// browserHostIsApple reports whether the *browser machine* is Apple (Mac/iOS),
// independent of GOOS=js. Used for PrimaryMod (Cmd vs Ctrl shortcuts).
func browserHostIsApple() bool {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return false
	}
	// Modern Chromium: userAgentData.platform is "macOS", "Windows", …
	if ua := nav.Get("userAgentData"); ua.Truthy() {
		p := strings.ToLower(ua.Get("platform").String())
		if strings.Contains(p, "mac") || strings.Contains(p, "ios") {
			return true
		}
		if p != "" {
			return false
		}
	}
	// Legacy: navigator.platform is "MacIntel", "iPhone", "Win32", …
	p := strings.ToLower(nav.Get("platform").String())
	if strings.Contains(p, "mac") || strings.Contains(p, "iphone") ||
		strings.Contains(p, "ipad") || strings.Contains(p, "ipod") {
		return true
	}
	// Last resort: userAgent substring.
	ua := strings.ToLower(nav.Get("userAgent").String())
	return strings.Contains(ua, "mac os") || strings.Contains(ua, "iphone") ||
		strings.Contains(ua, "ipad")
}

// isMobileHost reports a phone or tablet browser (iOS, iPadOS, Android).
// Chromium exposes this as userAgentData.mobile / platform; Safari uses UA
// (and the iPadOS-as-MacIntel + maxTouchPoints trick).
func isMobileHost() bool {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return false
	}
	if ua := nav.Get("userAgentData"); ua.Truthy() {
		if m := ua.Get("mobile"); m.Type() == js.TypeBoolean && m.Bool() {
			return true
		}
		p := strings.ToLower(ua.Get("platform").String())
		if strings.Contains(p, "android") || strings.Contains(p, "ios") {
			return true
		}
	}
	ua := strings.ToLower(nav.Get("userAgent").String())
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") ||
		strings.Contains(ua, "ipod") || strings.Contains(ua, "android") {
		return true
	}
	p := strings.ToLower(nav.Get("platform").String())
	mt := 0
	if t := nav.Get("maxTouchPoints"); t.Truthy() {
		mt = t.Int()
	}
	// iPadOS 13+ reports as MacIntel with a multi-touch screen.
	if mt > 1 && (strings.Contains(p, "mac") || strings.Contains(p, "ipad")) {
		return true
	}
	return false
}

func gpuForcedOff() bool {
	if generic.EnvFalsy("SHIREI_GPU") {
		return true
	}
	if g := js.Global().Get("SHIREI_GPU"); g.Truthy() && g.Type() != js.TypeUndefined {
		s := strings.ToLower(g.String())
		if s == "0" || s == "false" || s == "off" || s == "no" {
			return true
		}
	}
	if loc := js.Global().Get("location"); loc.Truthy() {
		q := strings.ToLower(loc.Get("search").String())
		if strings.Contains(q, "shirei_gpu=0") {
			return true
		}
	}
	return false
}

func tryGPU(canvas js.Value) {
	mark := func(v string) { canvas.Call("setAttribute", "data-shirei-gpu", v) }
	if gpuForcedOff() {
		mark("off")
		return
	}
	attrs := map[string]any{
		"alpha":                 false,
		"antialias":             false,
		"depth":                 false,
		"stencil":               false,
		"premultipliedAlpha":    true,
		"preserveDrawingBuffer": false,
		"powerPreference":       "low-power",
	}
	gl := canvas.Call("getContext", "webgl2", attrs)
	if !gl.Truthy() {
		// Some browsers reject a context-attribute combo; try the default.
		gl = canvas.Call("getContext", "webgl2")
	}
	if !gl.Truthy() {
		mark("no-webgl2")
		return
	}
	if err := gpurender.BindCanvas(gl); err != nil {
		js.Global().Get("console").Call("error", "gpurender: fallback to software: "+err.Error())
		mark("err:" + err.Error())
		if ext := gl.Call("getExtension", "WEBGL_lose_context"); ext.Truthy() {
			ext.Call("loseContext")
		}
		return
	}
	gpuOK = true
	name := gpurender.DeviceName()
	js.Global().Get("console").Call("log", "gpurender: "+name)
	mark(name)
}

func tick(canvas js.Value) {
	cssW := canvas.Get("clientWidth").Int()
	cssH := canvas.Get("clientHeight").Int()
	if cssW <= 0 || cssH <= 0 {
		// Fallback when layout has not run yet.
		cssW, cssH = winW, winH
		if cssW <= 0 {
			cssW = 800
		}
		if cssH <= 0 {
			cssH = 600
		}
	}
	dpr := js.Global().Get("devicePixelRatio").Float()
	if dpr <= 0 {
		dpr = 1
	}
	devW := int(float64(cssW)*dpr + 0.5)
	devH := int(float64(cssH)*dpr + 0.5)
	if devW < 1 {
		devW = 1
	}
	if devH < 1 {
		devH = 1
	}

	resized := false
	if canvas.Get("width").Int() != devW || canvas.Get("height").Int() != devH {
		canvas.Set("width", devW)
		canvas.Set("height", devH)
		resized = true
	}
	if !gpuOK {
		if len(pix) != devW*devH*4 {
			ensureBuffers(devW, devH)
			resized = true
		}
	}

	host := shirei.GetHost()
	// Full canvas size for the root; wrapFrame narrows to content during the
	// app build (and restores for settle). Content size is published again
	// after RunFrameFn below.
	host.WindowSize = shirei.Vec2{float32(cssW), float32(cssH)}
	host.WindowScale = float32(dpr)

	input.sample()
	if pastePending && clipboardPaste != "" {
		fi := shirei.GetFrameInput()
		fi.Text += clipboardPaste
		clipboardPaste = ""
		pastePending = false
	}

	// Focus hidden text field when core wants keyboard (text inputs).
	// Never call focus() unless this document already has focus — same-origin
	// iframes can steal keyboard from each other with element.focus(), and a
	// demo with AutoFocus text (e.g. theme) would vacuum every keystroke on a
	// gallery page.
	doc := js.Global().Get("document")
	inp := doc.Call("getElementById", "shirei-text")
	if host.WantsKeyboard && inp.Truthy() && documentHasFocus() {
		// Anchor near caret for IME candidate windows.
		st := inp.Get("style")
		st.Set("left", ftoa(host.CaretPos[0])+"px")
		st.Set("top", ftoa(host.CaretPos[1])+"px")
		if doc.Get("activeElement").Get("id").String() != "shirei-text" {
			inp.Call("focus")
		}
	}

	t0 := time.Now()
	out := shirei.RunFrameFn(frameFn)
	_ = t0

	// Publish content/client size on Host (parity with macOS/Win32). wrapFrame
	// restores the surface size for settle passes; readers between frames
	// should see the body, not the titlebar.
	if csdActive() {
		host.WindowSize[1] = float32(cssH - titlebarHeight)
	}

	if out.Copy != "" {
		writeClipboard(out.Copy)
	}
	if out.Paste {
		readClipboard()
	}
	if out.OpenURL != "" {
		js.Global().Call("open", out.OpenURL, "_blank", "noopener,noreferrer")
	}

	// Frame economy: only re-rasterize/present when the surface list changed
	// or the canvas was resized (or we have never painted).
	if !out.FrameHasChanges && !resized && havePainted {
		return
	}

	if gpuOK {
		err := gpurender.Render(nil, devW, devH, float32(dpr), out.Surfaces, out.GlyphRuns, out.GlyphsAdded, out.GlyphsEvicted, false)
		if err != nil {
			js.Global().Get("console").Call("error", "gpurender: "+err.Error())
			return
		}
		havePainted = true
		t := &shirei.ActiveUI().FrameTimings
		t.Painted = true
		t.PaintEnd = time.Now()
		shirei.EmitFrameMetrics()
		return
	}

	softRenderer.RenderInto(pix, bufW*4, bufW, bufH, float32(dpr), out.Surfaces, out.GlyphRuns)
	// One browser-managed copy: RGBA is already canvas order (no Go swizzle).
	// Opaque window: force A=255 so putImageData does not blend with the page.
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 255
	}
	jsU8 := js.Global().Get("Uint8ClampedArray").New(len(pix))
	js.CopyBytesToJS(jsU8, pix)
	imgData := js.Global().Get("ImageData").New(jsU8, bufW, bufH)
	ctx2d.Call("putImageData", imgData, 0, 0)
	havePainted = true
}

func ensureBuffers(w, h int) {
	need := w * h * 4
	if cap(pix) < need {
		pix = make([]byte, need)
	} else {
		pix = pix[:need]
	}
	bufW, bufH = w, h
}

func writeClipboard(text string) {
	clip := js.Global().Get("navigator").Get("clipboard")
	if !clip.Truthy() {
		return
	}
	// fire-and-forget promise
	clip.Call("writeText", text)
}

func readClipboard() {
	clip := js.Global().Get("navigator").Get("clipboard")
	if !clip.Truthy() {
		return
	}
	promise := clip.Call("readText")
	then := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			clipboardPaste = args[0].String()
			pastePending = true
			shirei.RequestNextFrame()
		}
		return nil
	})
	promise.Call("then", then)
}

func ftoa(v float32) string {
	// tiny helper without strconv import cost concerns
	return js.Global().Get("Number").New(float64(v)).Call("toFixed", 0).String()
}
