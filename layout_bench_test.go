package shirei

import (
	"testing"
	"time"
)

// Layout-pass benchmarks: ~3k containers mixing rows/columns, Expand, Grow,
// Pad, and ~20% text lines. Variants: steady window, window size alternating
// each frame, and one parent with 3k children.
//
//	go test -bench BenchmarkLayout -benchmem -count=6 -run '^$' .

func layoutBenchFrame(b *testing.B, animating bool, tree func()) {
	b.Helper()
	InitFontSubsystem()
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	ResetInputSession()
	scope := new(int)
	sizes := [2]Vec2{{800, 600}, {900, 700}}
	frame := func(i int) {
		if animating {
			ui.Host.WindowSize = sizes[i&1]
		} else {
			ui.Host.WindowSize = sizes[0]
		}
		RunFrameFn(func() {
			ContainerWithKey(scope, AttrSet{}, tree)
		})
	}
	for i := 0; i < 3; i++ {
		frame(i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame(i)
	}
}

func layoutBenchMixedTree() {
	const groups = 50
	const kids = 59 // 50*(1+59) = 3000
	for g := 0; g < groups; g++ {
		Container(Attrs(RowF(g%2 == 0), Pad(4), Gap(3), Expand), func() {
			for i := 0; i < kids; i++ {
				n := g*kids + i
				switch n % 5 {
				case 0:
					Label("n", FontSize(10))
				case 1:
					Element(Attrs(Grow(1), MinSize(4, 14), Pad(2)))
				case 2:
					Element(Attrs(Expand, MinSize(16, 10)))
				default:
					Element(Attrs(MinSize(18, 14), Pad(1)))
				}
			}
		})
	}
}

func layoutBenchWideFlat() {
	Container(Attrs(Pad(2), Gap(1)), func() {
		for i := 0; i < 3000; i++ {
			if i%5 == 0 {
				Label("n", FontSize(10))
			} else {
				Element(Attrs(MinSize(40, 12), Expand))
			}
		}
	})
}

func BenchmarkLayoutSteady(b *testing.B) {
	layoutBenchFrame(b, false, layoutBenchMixedTree)
}

func BenchmarkLayoutAnimating(b *testing.B) {
	layoutBenchFrame(b, true, layoutBenchMixedTree)
}

func BenchmarkLayoutWideFlat(b *testing.B) {
	layoutBenchFrame(b, false, layoutBenchWideFlat)
}
