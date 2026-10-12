package shirei

import (
	"image"
	"testing"
	"time"
)

func TestQuantizeDim(t *testing.T) {
	cases := []struct {
		v, step, want int
	}{
		{0, 8, 0},
		{1, 8, 1},
		{7, 8, 8},
		{8, 8, 8},
		{11, 8, 8},
		{12, 8, 16},
		{100, 0, 100},
		{100, 1, 100},
	}
	for _, c := range cases {
		if got := quantizeDim(c.v, c.step); got != c.want {
			t.Errorf("quantizeDim(%d, %d) = %d, want %d", c.v, c.step, got, c.want)
		}
	}
}

func TestResolveScaleSize(t *testing.T) {
	savedIdle := ScaleMotionIdle
	ScaleMotionIdle = 50 * time.Millisecond
	defer func() { ScaleMotionIdle = savedIdle }()

	res.scaleMotionById = map[ImageId]scaleMotion{}
	const id ImageId = 42

	_, _, p := resolveScaleSize(id, 100, 80)
	if p != scaleIdle {
		t.Fatal("first sighting should be idle")
	}
	_, _, p = resolveScaleSize(id, 100, 80)
	if p != scaleIdle {
		t.Fatal("unchanged size should stay idle")
	}
	_, _, p = resolveScaleSize(id, 120, 80)
	if p != scaleChanged {
		t.Fatal("size change should be scaleChanged")
	}
	_, _, p = resolveScaleSize(id, 120, 80)
	if p != scaleWaiting {
		t.Fatal("same size within idle window should be scaleWaiting")
	}
	_, _, p = resolveScaleSize(id, 121, 80)
	if p != scaleWaiting {
		t.Fatal("1px jitter should stay locked (not restart motion)")
	}
	w, h, p := resolveScaleSize(id, 121, 81)
	if p != scaleWaiting || w != 120 || h != 80 {
		t.Fatalf("1px jitter should keep locked size 120x80, got %dx%d phase=%d", w, h, p)
	}
}

func fillSize(w, h int, v byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = v
	}
	return img
}

func TestScaledImageOneToOneDoesNotRequestFrame(t *testing.T) {
	savedIdle := ScaleMotionIdle
	ScaleMotionIdle = 50 * time.Millisecond
	defer func() { ScaleMotionIdle = savedIdle }()

	headless := ui.Host.HeadlessRender
	ui.Host.HeadlessRender = false
	defer func() { ui.Host.HeadlessRender = headless }()

	res.scaledImageCache = map[scaledKey]*scaledEntry{}
	res.scaleMotionById = map[ImageId]scaleMotion{}

	src := fillSize(32, 32, 0x80)
	id := UseImage("test-scale-1to1", src)
	scaledImage(id, src, 32, 32)

	ui.Host.NextFrame.Store(false)
	scaledImage(id, src, 31, 31)
	scaledImage(id, src, 33, 33)
	scaledImage(id, src, 32, 32)
	if FrameRequested() {
		t.Fatal("1:1 / 1px dest must not RequestNextFrame")
	}
}

func TestScaledImageTwoStableSizesDoNotThrash(t *testing.T) {
	savedIdle := ScaleMotionIdle
	ScaleMotionIdle = 50 * time.Millisecond
	defer func() { ScaleMotionIdle = savedIdle }()

	headless := ui.Host.HeadlessRender
	ui.Host.HeadlessRender = false
	defer func() { ui.Host.HeadlessRender = headless }()

	res.scaledImageCache = map[scaledKey]*scaledEntry{}
	res.scaleMotionById = map[ImageId]scaleMotion{}

	src := fillSize(64, 64, 0x40)
	id := UseImage("test-scale-two-dest", src)
	scaledImage(id, src, 32, 32)
	scaledImage(id, src, 40, 40)
	time.Sleep(60 * time.Millisecond)
	scaledImage(id, src, 40, 40) // idle-quality upgrade for 40

	ui.Host.NextFrame.Store(false)
	scaledImage(id, src, 32, 32)
	scaledImage(id, src, 40, 40)
	scaledImage(id, src, 32, 32)
	if FrameRequested() {
		t.Fatal("two stable dest sizes must not keep requesting frames")
	}
}
