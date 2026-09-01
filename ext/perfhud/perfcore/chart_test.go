package perfcore

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"testing"
)

// wave builds a synthetic history: n samples over the chart window, values
// tracing a sine between 0 and amp.
func wave(n int, amp float64) []Sample {
	out := make([]Sample, n)
	for i := range out {
		f := float64(i) / float64(n-1)
		out[i] = Sample{
			T:       f * Config.WindowSecs,
			FrameMs: amp * (0.5 + 0.5*math.Sin(f*4*math.Pi)),
		}
	}
	return out
}

func frameMs(s *Sample) float64 { return s.FrameMs }

func TestRasterizeFollowsTheData(t *testing.T) {
	const w, h = 220, 40
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.NRGBA{20, 22, 28, 255}
	line := color.NRGBA{120, 220, 150, 255}

	Rasterize(img, wave(300, 10), frameMs, 10, 1, bg, line)

	// Both the peak and the trough of the wave must be on screen: the top
	// row should carry stroke somewhere, and the bottom row should not be
	// stroke everywhere.
	if !rowHasColor(img, 0, line) {
		t.Error("the peak of the wave never reaches the top row")
	}
	if rowAllColor(img, h-1, line) {
		t.Error("the bottom row is solid stroke: the trough is clipped")
	}
	// Every column must be painted; a gap means the line is broken.
	for x := 0; x < w; x++ {
		if !columnHasColor(img, x, line) {
			t.Fatalf("column %d has no stroke: the line is broken", x)
		}
	}
}

func TestRasterizeEmptyAndSingle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	bg := color.NRGBA{1, 2, 3, 255}
	line := color.NRGBA{250, 250, 250, 255}

	Rasterize(img, nil, frameMs, 10, 1, bg, line)
	for x := 0; x < 40; x++ {
		if columnHasColor(img, x, line) {
			t.Fatalf("empty history painted a stroke at column %d", x)
		}
	}

	Rasterize(img, []Sample{{T: 1, FrameMs: 5}}, frameMs, 10, 1, bg, line)
	if !columnHasColor(img, 39, line) {
		t.Error("a single sample should be marked at the newest edge")
	}
}

// TestRasterizeGolden writes a chart to PERFHUD_GOLDEN for a visual check.
// It is a no-op unless that variable is set.
func TestRasterizeGolden(t *testing.T) {
	path := os.Getenv("PERFHUD_GOLDEN")
	if path == "" {
		t.Skip("set PERFHUD_GOLDEN=<file.png> to write a sample chart")
	}
	img := image.NewRGBA(image.Rect(0, 0, 440, 80))
	Rasterize(img, wave(600, 10), frameMs, 11.5, 2,
		color.NRGBA{28, 31, 40, 255}, color.NRGBA{120, 220, 150, 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func sameColor(img *image.RGBA, x, y int, c color.NRGBA) bool {
	i := img.PixOffset(x, y)
	return img.Pix[i] == c.R && img.Pix[i+1] == c.G && img.Pix[i+2] == c.B
}

func rowHasColor(img *image.RGBA, y int, c color.NRGBA) bool {
	for x := 0; x < img.Rect.Dx(); x++ {
		if sameColor(img, x, y, c) {
			return true
		}
	}
	return false
}

func rowAllColor(img *image.RGBA, y int, c color.NRGBA) bool {
	for x := 0; x < img.Rect.Dx(); x++ {
		if !sameColor(img, x, y, c) {
			return false
		}
	}
	return true
}

func columnHasColor(img *image.RGBA, x int, c color.NRGBA) bool {
	for y := 0; y < img.Rect.Dy(); y++ {
		if sameColor(img, x, y, c) {
			return true
		}
	}
	return false
}
