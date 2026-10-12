package shirei

import (
	"fmt"
	"testing"
	"time"
)

// Text pipeline benchmarks include bitmap-cache maintenance. Every line fits
// in the viewport; paint runs the software renderer as well as production.
func BenchmarkTextPipeline(b *testing.B) {
	for _, paint := range []bool{false, true} {
		name := "Produce"
		if paint {
			name = "ProducePaint"
		}
		b.Run(name, func(b *testing.B) {
			for !SystemFontScanDone() {
				time.Sleep(time.Millisecond)
			}
			ResetInputSession()
			ui.Host.WindowSize = Vec2{1000, 900}
			ui.Host.WindowScale = 1
			prevBudget := ui.Host.GlyphCacheBudgetBytes
			ui.Host.GlyphCacheBudgetBytes = 16 << 20
			defer func() { ui.Host.GlyphCacheBudgetBytes = prevBudget }()
			labels := make([]string, 55)
			for i := range labels {
				labels[i] = fmt.Sprintf("%02d func layout(row int) { return cachedGlyphs + repeated_letters_eeeeeeee + 1234567890 }", i)
			}
			scope := new(int)
			frame := func() {
				ModAttrs(NoAnimate)
				ContainerWithKey(scope, Attrs(Viewport, Background(0, 0, 100, 1)), func() {
					for _, label := range labels {
						Label(label, FontSize(11))
					}
				})
			}
			var renderer SoftRenderer
			for range 8 {
				out := RunFrameFn(frame)
				if paint {
					renderer.Render(out.Surfaces, out.GlyphRuns, 1000, 900, 1)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				out := RunFrameFn(frame)
				if paint {
					renderer.Render(out.Surfaces, out.GlyphRuns, 1000, 900, 1)
				}
			}
		})
	}
}

func BenchmarkGlyphCacheRepeated(b *testing.B) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{1000, 900}
	ui.Host.WindowScale = 1
	prevBudget := ui.Host.GlyphCacheBudgetBytes
	ui.Host.GlyphCacheBudgetBytes = 16 << 20
	defer func() { ui.Host.GlyphCacheBudgetBytes = prevBudget }()
	var out FrameOutputData
	for range 8 {
		out = RunFrameFn(func() {
			ModAttrs(NoAnimate)
			for range 55 {
				Label("repeated letters eeeeeeeeeee abcdefghijklmnopqrstuvwxyz 0123456789", FontSize(11))
			}
		})
	}
	// Wait for startup font discovery to finish before measuring cache hits.
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ui.FrameNumber++
		updateGlyphCache(out.Surfaces, out.GlyphRuns)
	}
}
