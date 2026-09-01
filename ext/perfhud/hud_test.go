package perfhud

import (
	"image"
	"math"
	"os"
	"testing"

	"go.hasen.dev/shirei/ext/perfhud/perfcore"

	. "go.hasen.dev/shirei"
)

// seedHistory installs a synthetic history, so the overlay can be rendered
// without a running app. The probe slots carry this renderer's own numbers,
// in the order init registered them.
func seedHistory(n int) {
	samples := make([]Sample, n)
	for i := range samples {
		f := float64(i) / float64(n-1)
		samples[i] = Sample{
			T:          f * Core.WindowSecs,
			FPS:        58 + 4*math.Sin(f*6*math.Pi),
			FrameMs:    16 + 6*math.Sin(f*5*math.Pi),
			AllocKB:    40 + 30*math.Sin(f*9*math.Pi),
			PauseMs:    math.Abs(math.Sin(f * 3 * math.Pi)),
			HeapMB:     18 + f*4,
			Goroutines: 39,
			GCCycles:   12,
			User:       probeSlots(3+math.Sin(f*7*math.Pi), 5+math.Cos(f*7*math.Pi)),
		}
	}
	perfcore.SeedHistory(samples, []AllocEntry{
		{Name: "borg-shirei.buildDisplay", KB: 412.5},
		{Name: "go.hasen.dev/shirei.ShapeTextMax", KB: 210.0},
		{Name: "go.hasen.dev/shirei/widgets.DrawTextInputPlain", KB: 96.2},
		{Name: "borg-shirei.parse", KB: 12.7},
		{Name: "main.frame", KB: 1.1},
	})
}

// AllocEntry is re-exported here only so the seed above reads plainly.
type AllocEntry = perfcore.AllocEntry

func probeSlots(layout, paint float64) []float64 {
	u := make([]float64, len(Core.Probes))
	set := func(i int, v float64) {
		if i < len(u) {
			u[i] = v
		}
	}
	set(pLayout, layout)
	set(pPaint, paint)
	set(pShapeCalls, 68)
	set(pShapeHits, 60)
	set(pImages, 6)
	set(pImageBytes, 812345)
	return u
}

// renderOverlay draws just the overlay on a blank window and returns the
// pixels. The history is seeded, so no sampling happens.
func renderOverlay(t *testing.T, w, h int) *image.RGBA {
	t.Helper()
	prevVisible, prevKeep, prevLog := Visible, Config.KeepAwake, Core.HitchLog
	Visible, Config.KeepAwake, Core.HitchLog = true, false, ""
	defer func() { Visible, Config.KeepAwake, Core.HitchLog = prevVisible, prevKeep, prevLog }()

	var img *image.RGBA
	// Two passes: Shirei answers geometry queries from the previous frame, so
	// the first pass is what settles the panel size.
	for i := 0; i < 2; i++ {
		img = RenderToImage(w, h, func() {
			Container(Attrs(Viewport, Background(225, 14, 10, 1)), func() { Void() })
			seedHistory(240)
			drawPanel()
		})
	}
	return img
}

func TestOverlayRenders(t *testing.T) {
	const w, h = 640, 480
	img := renderOverlay(t, w, h)
	if img == nil {
		t.Fatal("no image")
	}

	// RenderToImage returns device pixels (HeadlessScale per logical point),
	// so probe against the real bounds, not the logical size.
	b := img.Bounds()
	// The panel is pinned to the top-right, so its pixels must differ from
	// the window background there and match it in the bottom-left.
	corner := img.RGBAAt(b.Max.X-60, b.Min.Y+80)
	far := img.RGBAAt(b.Min.X+60, b.Max.Y-60)
	if corner == far {
		t.Errorf("the top-right corner (%v) looks like empty background (%v): the panel did not draw", corner, far)
	}
}

// TestOverlayGolden writes the overlay to PERFHUD_GOLDEN_HUD for a visual
// check. It is a no-op unless that variable is set.
func TestOverlayGolden(t *testing.T) {
	path := os.Getenv("PERFHUD_GOLDEN_HUD")
	if path == "" {
		t.Skip("set PERFHUD_GOLDEN_HUD=<file.png> to write the overlay")
	}
	prevVisible, prevKeep, prevLog := Visible, Config.KeepAwake, Core.HitchLog
	Visible, Config.KeepAwake, Core.HitchLog = true, false, ""
	defer func() { Visible, Config.KeepAwake, Core.HitchLog = prevVisible, prevKeep, prevLog }()

	var err error
	for i := 0; i < 2; i++ {
		err = RenderToPNG(path, 420, 820, func() {
			Container(Attrs(Viewport, Background(225, 14, 10, 1)), func() { Void() })
			seedHistory(240)
			drawPanel()
		})
	}
	if err != nil {
		t.Fatal(err)
	}
}
