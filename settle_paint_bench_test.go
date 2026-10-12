package shirei

import (
	"fmt"
	"testing"
	"time"
)

// Both variants emit the same visible text/box tree. Settle repeats its build
// before publication, isolating intermediate artifact work from content churn.
func BenchmarkFrameArtifacts(b *testing.B) {
	for _, settle := range []bool{false, true} {
		name := "Steady"
		if settle {
			name = "Settle"
		}
		b.Run(name, func(b *testing.B) {
			for !SystemFontScanDone() {
				time.Sleep(time.Millisecond)
			}
			previous := ui
			bindUI(NewUI())
			defer bindUI(previous)
			ui.Host.WindowSize = Vec2{1000, 1500}
			ui.Host.WindowScale = 1
			ui.Host.GlyphCacheBudgetBytes = 16 << 20
			labels := make([]string, 55)
			for i := range labels {
				labels[i] = fmt.Sprintf("%02d repeated frame content with cached text geometry", i)
			}
			frame := func() {
				ModAttrs(NoAnimate)
				if settle {
					RequestStabilize()
				}
				Container(Attrs(Viewport, Gap(2)), func() {
					for _, label := range labels {
						Container(Attrs(Row, FixSize(900, 22), Background(0, 0, 95, 1), BorderWidth(1), BorderColor(0, 0, 60, 1), Corners(3)), func() {
							Label(label, FontSize(11))
						})
					}
				})
			}
			for range 8 {
				RunFrameFn(frame)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				RunFrameFn(frame)
			}
		})
	}
}
