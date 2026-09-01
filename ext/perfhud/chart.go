package perfhud

import (
	"fmt"
	"image"

	"go.hasen.dev/shirei/ext/perfhud/perfcore"

	. "go.hasen.dev/shirei"
)

// chartBuf double-buffers one chart's pixels. UseImage detects a new frame by
// the backing array, not by its contents, so writing into the same buffer
// every frame would never refresh the overlay — and reusing a buffer the
// renderer may still be reading would tear. Two buffers solve both, with no
// per-frame allocation.
type chartBuf struct {
	bufs [2]*image.RGBA
	cur  int
	w, h int
}

var chartBufs = map[int]*chartBuf{}

// next returns the buffer to draw into, re-allocating on a size change.
func (cb *chartBuf) next(w, h int) *image.RGBA {
	if cb.w != w || cb.h != h || cb.bufs[0] == nil {
		cb.bufs[0] = image.NewRGBA(image.Rect(0, 0, w, h))
		cb.bufs[1] = image.NewRGBA(image.Rect(0, 0, w, h))
		cb.w, cb.h = w, h
	}
	cb.cur ^= 1
	return cb.bufs[cb.cur]
}

// plot rasterizes c's history (perfcore owns the pixels) and blits it. The
// image is registered at device resolution so the renderer blits it 1:1
// instead of resampling.
func plot(idx int, c *Chart, wLogical, hLogical float32, col Vec4) {
	scale := GetHost().WindowScale
	if scale <= 0 {
		scale = 1
	}
	w, h := int(wLogical*scale+0.5), int(hLogical*scale+0.5)
	if w < 2 || h < 2 {
		return
	}

	cb := chartBufs[idx]
	if cb == nil {
		cb = &chartBuf{}
		chartBufs[idx] = cb
	}
	img := cb.next(w, h)

	perfcore.RasterizeChart(img, c, int(scale+0.5), HSLAColor(Config.Theme.Plot), HSLAColor(col))

	id := UseImage(fmt.Sprintf("perfhud/chart/%d", idx), img)
	ImageViewAt(id, Vec2{wLogical, hLogical})
}
