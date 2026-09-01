package perfcore

import (
	"image"
	"image/color"
)

// Sparklines are rasterized rather than drawn with the host's primitives:
// most immediate-mode toolkits have no polyline, and a chart built from one
// widget per column costs more than the thing it measures. An RGBA image is
// something every host can already put on screen.

// Rasterize draws samples into img as a filled sparkline. get picks the
// value, top is the value at the image's top edge, thick is the stroke width
// in pixels.
//
// The time axis is anchored on the newest sample, not on the wall clock: when
// the app stops producing frames the chart freezes with its last shape
// visible instead of scrolling itself empty.
func Rasterize(img *image.RGBA, samples []Sample, get func(*Sample) float64, top float64, thick int, bg, line color.NRGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	under := blend(bg, line, 0.16)
	fillAll(img, bg)
	if len(samples) == 0 || w < 2 || h < 2 || get == nil {
		return
	}
	if top <= 0 {
		top = 1
	}
	if thick < 1 {
		thick = 1
	}

	t1 := samples[len(samples)-1].T
	t0 := t1 - Config.WindowSecs
	px := func(s *Sample) int {
		return clampInt(int((s.T-t0)/Config.WindowSecs*float64(w-1)), 0, w-1)
	}
	py := func(s *Sample) int {
		return clampInt(int(float64(h-1)-get(s)/top*float64(h-1)+0.5), 0, h-1)
	}

	// A lone sample still gets a mark, so a chart is never blank-but-alive.
	if len(samples) == 1 {
		x, y := px(&samples[0]), py(&samples[0])
		segment(img, x, y, x, y, thick, line, under)
		return
	}
	for i := 1; i < len(samples); i++ {
		segment(img, px(&samples[i-1]), py(&samples[i-1]),
			px(&samples[i]), py(&samples[i]), thick, line, under)
	}
}

// RasterizeChart is Rasterize with c's own accessor and auto-scaled y axis.
func RasterizeChart(img *image.RGBA, c *Chart, thick int, bg, line color.NRGBA) {
	Rasterize(img, hist, c.Value, ChartTop(c), thick, bg, line)
}

// segment draws the line from (x0,y0) to (x1,y1) column by column, filling
// the area beneath it. Stepping over x (rather than a generic line walk)
// keeps the column fill and the stroke in one pass, and handles both a dense
// history (many samples per column) and a sparse one (long jumps).
func segment(img *image.RGBA, x0, y0, x1, y1, thick int, line, under color.NRGBA) {
	if x1 < x0 {
		x0, x1 = x1, x0
		y0, y1 = y1, y0
	}
	span := x1 - x0
	for x := x0; x <= x1; x++ {
		y := y1
		if span > 0 {
			y = y0 + (y1-y0)*(x-x0)/span
		}
		// the area fill starts below the stroke so the stroke stays crisp
		vspan(img, x, y+thick, img.Rect.Dy()-1, under)
		vspan(img, x, y, y+thick-1, line)
	}
	// A jump between two adjacent columns leaves a vertical gap that the
	// per-column stroke does not cover; close it at the newer column.
	if span <= 1 {
		lo, hi := y0, y1
		if lo > hi {
			lo, hi = hi, lo
		}
		vspan(img, x1, lo, hi, line)
	}
}

// Coordinates below are chart-local (0,0 = top left of the plot). They are
// offset by Rect.Min on the way to the pixels, so a host can hand in a
// SubImage of a bigger panel buffer and have the chart land in place.

func vspan(img *image.RGBA, x, y0, y1 int, c color.NRGBA) {
	b := img.Rect
	if x < 0 || x >= b.Dx() {
		return
	}
	y0, y1 = clampInt(y0, 0, b.Dy()-1), clampInt(y1, 0, b.Dy()-1)
	for y := y0; y <= y1; y++ {
		i := img.PixOffset(b.Min.X+x, b.Min.Y+y)
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
}

// fillAll paints one row and copies it down — the usual trick to avoid a
// per-pixel loop over the whole buffer every frame.
func fillAll(img *image.RGBA, c color.NRGBA) {
	b := img.Rect
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return
	}
	first := img.PixOffset(b.Min.X, b.Min.Y)
	row := img.Pix[first : first+w*4]
	for x := 0; x < w; x++ {
		row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.R, c.G, c.B, c.A
	}
	for y := 1; y < h; y++ {
		o := img.PixOffset(b.Min.X, b.Min.Y+y)
		copy(img.Pix[o:o+w*4], row)
	}
}

// blend mixes b into a by t (0..1), keeping a's alpha.
func blend(a, b color.NRGBA, t float64) color.NRGBA {
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), a.A}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
