package shirei

// Wrapped packing covers 1,000 children, mixed Grow/Expand, resizing, and
// scrolling. Full-frame timing includes build, layout, and artifact production.
import (
	"testing"
	"time"
)

func BenchmarkPackingWrap(b *testing.B) {
	for _, name := range []string{"Steady", "Resize", "Scroll"} {
		b.Run(name, func(b *testing.B) {
			for !SystemFontScanDone() {
				time.Sleep(time.Millisecond)
			}
			prev := ui
			bindUI(NewUI())
			defer bindUI(prev)
			ui.Host.WindowSize = Vec2{1000, 800}
			ui.pinnedTimeDelta = true
			ui.timeDelta = .016
			width := float32(900)
			scroll := float32(0)
			build := func() {
				ModAttrs(NoAnimate)
				Container(Attrs(Row, Wrap, FixWidth(width), MaxWidth(width), MaxHeight(700), Pad(4), Gap(3)), func() {
					SetScrollOffset(Vec2{0, scroll})
					for i := 0; i < 1000; i++ {
						Element(Attrs(MinSize(20+float32(i%5)*3, 18), Grow(float32(i%3)), Expand))
					}
				})
			}
			frame := func(i int) {
				if name == "Resize" {
					width = 800 + float32(i%2)*100
				}
				if name == "Scroll" {
					scroll = float32(i % 100)
				}
				RunFrameFn(build)
			}
			for i := 0; i < 20; i++ {
				frame(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame(i)
			}
		})
	}
}
