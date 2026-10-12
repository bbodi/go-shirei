//go:build darwin || linux || android || windows || js

package gpurender

import (
	"strings"
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

// BenchmarkGlyphDrawList times CPU draw-list construction with real shaped text
// and warmed bitmap/atlas caches. Native submission and UI construction are
// outside the timed loop; this isolates the per-repaint renderer work.
func BenchmarkGlyphDrawList(b *testing.B) {
	oldGPU, oldUploads := gpu, pendingUploads
	gpu.init()
	pendingUploads = nil
	defer func() { gpu, pendingUploads = oldGPU, oldUploads }()
	for !shirei.SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	host := shirei.GetHost()
	oldSize, oldScale, oldBudget := host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes
	defer func() { host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes = oldSize, oldScale, oldBudget }()
	host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes = shirei.Vec2{1000, 720}, 2, 32<<20
	for _, mode := range []string{"Uniform", "Mixed", "Legacy", "ClippedLongLines"} {
		b.Run(mode, func(b *testing.B) {
			text := strings.Repeat("func example() { return value } ", 3)
			if mode == "ClippedLongLines" {
				text = strings.Repeat(text, 8)
			}
			var out shirei.FrameOutputData
			scope := new(int)
			for range 5 {
				out = shirei.RunFrameFn(func() {
					shirei.ModAttrs(shirei.NoAnimate)
					shirei.ContainerWithKey(scope, shirei.Attrs(shirei.Viewport, shirei.Clip, shirei.Pad(8)), func() {
						for range 30 {
							shirei.Container(shirei.Attrs(shirei.FixHeight(20), shirei.Expand, shirei.Clip), func() {
								shirei.ModAttrs(shirei.UnsetMaxCross)
								style := shirei.TextStyle(shirei.FontSize(14), shirei.Fonts(shirei.Monospace...))
								if mode == "Mixed" || mode == "Legacy" {
									shirei.Text(text, style, shirei.Span(0, 4, shirei.TextColor(40, 90, 40, 1)), shirei.Span(5, 12, shirei.TextColor(220, 80, 50, .6)))
								} else {
									shirei.Text(text, style)
								}
							})
						}
					})
				})
			}
			if mode == "Legacy" {
				var runs []shirei.GlyphRun
				for i := range out.Surfaces {
					s := &out.Surfaces[i]
					if s.GlyphRunCount == 0 {
						continue
					}
					first := len(runs)
					for j := 0; j < int(s.GlyphRunCount); j++ {
						runs = append(runs, s.GlyphRunAt(j, out.GlyphRuns))
					}
					s.GlyphData = nil
					s.GlyphRunFirst = int32(first)
				}
				out.GlyphRuns = runs
			}
			var build builder
			build.build(out.Surfaces, out.GlyphRuns, 2, 2000, 1440)
			if gpu.failed {
				b.Fatal("fixture exhausted the atlas")
			}
			pendingUploads = nil
			if len(build.quads) < 100 {
				b.Fatalf("fixture only produced %d quads", len(build.quads))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				build.build(out.Surfaces, out.GlyphRuns, 2, 2000, 1440)
			}
			b.StopTimer()
			b.ReportMetric(float64(len(build.quads)), "quads/frame")
		})
	}
}
