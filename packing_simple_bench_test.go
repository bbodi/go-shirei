package shirei

// Packing benchmarks drive complete frames with fresh identities, fixed time,
// and warmed storage. The flat fixture has 500 fixed leaves; the row fixture
// has 100 rows with four children each and one growing child per row.
import (
	"testing"
	"time"
)

func BenchmarkPackingSimple(b *testing.B) {
	for _, mode := range []string{"FlatStable", "FlatResize", "RowsStable", "RowsResize"} {
		b.Run(mode, func(b *testing.B) {
			for !SystemFontScanDone() {
				time.Sleep(time.Millisecond)
			}
			prev := ui
			bindUI(NewUI())
			defer bindUI(prev)
			ui.Host.WindowSize = Vec2{1000, 800}
			ui.pinnedTimeDelta = true
			ui.timeDelta = .016
			i := 0
			build := func() {
				ModAttrs(NoAnimate)
				ContainerWithKey("outer", Attrs(Viewport), func() {
					if mode == "FlatStable" || mode == "FlatResize" {
						for n := 0; n < 500; n++ {
							Element(Attrs(MinSize(40, 12)))
						}
					} else {
						for n := 0; n < 100; n++ {
							Container(Attrs(Row, Expand, Gap(2), Pad(2)), func() {
								Element(Attrs(MinSize(40, 12)))
								Element(Attrs(Grow(1), MinSize(40, 12)))
								Element(Attrs(MinSize(40, 12)))
								Element(Attrs(MinSize(40, 12)))
							})
						}
					}
				})
			}
			frame := func() {
				if mode == "FlatResize" || mode == "RowsResize" {
					ui.Host.WindowSize[0] = 1000 + float32(i%2)*100
				}
				RunFrameFn(build)
				i++
			}
			for n := 0; n < 20; n++ {
				frame()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				frame()
			}
		})
	}
}
