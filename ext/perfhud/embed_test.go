package perfhud

import (
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

// embedOverlay returns an Overlay with the panel up and the history seeded,
// so a Frame call paints without sampling or writing files.
func embedOverlay(t *testing.T) *Overlay {
	t.Helper()
	prevVisible, prevKeep, prevLog := Visible, Config.KeepAwake, Core.HitchLog
	Visible, Config.KeepAwake, Core.HitchLog = true, false, ""
	t.Cleanup(func() {
		Visible, Config.KeepAwake, Core.HitchLog = prevVisible, prevKeep, prevLog
	})
	seedHistory(240)

	o := &Overlay{}
	o.Init()
	return o
}

// alphaAt reads a device pixel's alpha out of a premultiplied RGBA buffer.
func alphaAt(pix []byte, w, x, y int) byte {
	return pix[(y*w+x)*4+3]
}

func TestOverlayFrameIsTransparentAroundThePanel(t *testing.T) {
	const w, h = 900, 600
	o := embedOverlay(t)

	pix := o.Frame(w, h, 1, nil)
	if pix == nil {
		t.Fatal("the first frame painted nothing")
	}
	if !o.HasContent() {
		t.Error("HasContent = false with the panel visible")
	}
	if gotW, gotH := o.Size(); gotW != w || gotH != h {
		t.Errorf("Size = %dx%d, want %dx%d", gotW, gotH, w, h)
	}

	// The panel is pinned top-right; the bottom-left is the host's picture and
	// must survive untouched.
	if a := alphaAt(pix, w, 40, h-40); a != 0 {
		t.Errorf("bottom-left alpha = %d, want 0: the host's scene would be painted over", a)
	}
	if a := alphaAt(pix, w, w-40, 60); a == 0 {
		t.Error("top-right alpha = 0: the panel did not paint")
	}
}

func TestOverlayFrameSkipsAnUnchangedPicture(t *testing.T) {
	const w, h = 900, 600
	o := embedOverlay(t)
	o.Repaint = time.Hour // never repaint for moving numbers alone

	if o.Frame(w, h, 1, nil) == nil {
		t.Fatal("the first frame painted nothing")
	}
	if pix := o.Frame(w, h, 1, nil); pix != nil {
		t.Error("an unchanged frame repainted; the host would re-upload for nothing")
	}
	// Input always repaints, however quiet the numbers are.
	o.Pointer(10, 10)
	if pix := o.Frame(w, h, 1, nil); pix == nil {
		t.Error("no repaint after input")
	}
	// So does a resize.
	if pix := o.Frame(w/2, h, 1, nil); pix == nil {
		t.Error("no repaint after a resize")
	}
}

func TestOverlayHasNoContentWhenHidden(t *testing.T) {
	const w, h = 900, 600
	o := embedOverlay(t)
	Visible = false

	if pix := o.Frame(w, h, 1, nil); pix != nil {
		t.Error("a hidden panel returned pixels")
	}
	if o.HasContent() {
		t.Error("HasContent = true with nothing drawn")
	}

	// The host's own UI still draws, and the panel stays out of it.
	pix := o.Frame(w, h, 1, func() {
		Container(Attrs(Float(0, 0), FixWidth(120), FixHeight(80), Background(0, 0, 50, 1)), func() {})
	})
	if pix == nil {
		t.Fatal("the host's own UI painted nothing")
	}
	if a := alphaAt(pix, w, 20, 20); a != 0xff {
		t.Errorf("inside the host's card: alpha = %d, want 255", a)
	}
	if a := alphaAt(pix, w, w-40, 60); a != 0 {
		t.Errorf("where the hidden panel would sit: alpha = %d, want 0", a)
	}
}

func TestOverlayWantsPointerOnlyOverTheUI(t *testing.T) {
	const w, h = 900, 600
	o := embedOverlay(t)

	// Two frames per position: the hit chain is built at frame start from the
	// previous frame's geometry.
	at := func(x, y float32) bool {
		o.Pointer(x, y)
		o.Frame(w, h, 1, nil)
		o.Frame(w, h, 1, nil)
		return o.WantsPointer()
	}
	if !at(w-60, 60) {
		t.Error("pointer on the panel: WantsPointer = false, the host would swallow the click")
	}
	if at(60, h-60) {
		t.Error("pointer on the host's scene: WantsPointer = true, the click would be eaten")
	}

	o.PointerAway()
	o.Frame(w, h, 1, nil)
	o.Frame(w, h, 1, nil)
	if o.WantsPointer() {
		t.Error("WantsPointer = true with the pointer parked")
	}
}

func TestOverlayPressReachesAWidget(t *testing.T) {
	const w, h = 900, 600
	o := embedOverlay(t)

	// Aim at the copy button of an open detail view: a real widget inside the
	// panel, so a press that lands proves the whole input path.
	openDetail("0:test", "test", "a stack frame\n")
	t.Cleanup(CloseDetail)

	o.Pointer(w/2, h/2)
	o.Frame(w, h, 1, nil)
	o.Frame(w, h, 1, nil)
	if !o.WantsPointer() {
		t.Fatal("the detail view is open under the pointer but WantsPointer = false")
	}
	o.Press(ButtonPrimary)
	if GetFrameInput().Mouse != MouseClick {
		t.Errorf("Press left Mouse = %v, want MouseClick", GetFrameInput().Mouse)
	}
	if GetInputState().MouseButton != MousePrimary {
		t.Errorf("Press left MouseButton = %v, want MousePrimary", GetInputState().MouseButton)
	}
	o.Frame(w, h, 1, nil)
	o.Release(ButtonSecondary)
	if GetFrameInput().Mouse != MouseRelease {
		t.Errorf("Release left Mouse = %v, want MouseRelease", GetFrameInput().Mouse)
	}
	if GetInputState().MouseButton != MouseSecondary {
		t.Errorf("Release left MouseButton = %v, want MouseSecondary", GetInputState().MouseButton)
	}
}
