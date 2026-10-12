package shirei

// Text pipeline benchmarks. Three tiers:
//
//   - TextSteady / TextSpansSteady: full frames where every string repeats,
//     so shaping is a cache hit — measures the per-Text per-frame overhead
//     (rune conversion, cache key, span resolution, per-line containers,
//     stamp emission).
//   - TextChurn: full frames where every string is new — measures the
//     shaping miss path (segmentation, font matching, harfbuzz).
//   - ShapeHit / ShapeMiss: ShapeTextMax alone, no frame machinery.
//
//	go test -bench BenchmarkText -benchmem -count=6 -run '^$' ./
//	go test -bench BenchmarkShape -benchmem -count=6 -run '^$' ./

import (
	"fmt"
	"testing"
)

func benchTextFrame(b *testing.B, fn func(frame int)) {
	InitFontSubsystem()
	ResetInputSession()
	ui.Host.WindowSize = Vec2{1200, 800}

	scope := new(int)
	n := 0
	frame := func() {
		RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(scope, Attrs(Viewport), func() { fn(n) })
		})
		n++
	}
	for range 3 {
		frame()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame()
	}
}

// 200 distinct static labels: the steady-state screen. Shaping always hits
// the cache; the cost is everything around it.
func BenchmarkTextSteady(b *testing.B) {
	labels := make([]string, 200)
	for i := range labels {
		labels[i] = fmt.Sprintf("Row label %d — steady content that does not change", i)
	}
	benchTextFrame(b, func(int) {
		Container(Attrs(), func() {
			for _, s := range labels {
				Label(s)
			}
		})
	})
}

// 200 static labels with Fonts(Monospace...) each — intern hit + shape hit.
func BenchmarkTextSteadyMono(b *testing.B) {
	labels := make([]string, 200)
	for i := range labels {
		labels[i] = fmt.Sprintf("Row label %d — steady content that does not change", i)
	}
	benchTextFrame(b, func(int) {
		Container(Attrs(), func() {
			for _, s := range labels {
				Label(s, Fonts(Monospace...))
			}
		})
	})
}

// 200 labels that change every frame (fmt-built, like clocks / counters /
// live metrics): every one is a shape-cache miss.
func BenchmarkTextChurn(b *testing.B) {
	benchTextFrame(b, func(frame int) {
		Container(Attrs(), func() {
			for i := 0; i < 200; i++ {
				Label(fmt.Sprintf("Row label %d — live value %d", i, frame))
			}
		})
	})
}

// 50 static paragraphs with range spans (bold + colored word) and an active
// selection: exercises the per-frame span path (stamp clone, styleAt per
// glyph, advance bands) on top of cache-hit shaping.
func BenchmarkTextSpansSteady(b *testing.B) {
	labels := make([]string, 50)
	for i := range labels {
		labels[i] = fmt.Sprintf("Paragraph %d with some highlighted and styled words inside", i)
	}
	benchTextFrame(b, func(int) {
		Container(Attrs(), func() {
			for _, s := range labels {
				style := TextStyle()
				spans := []TextSpan{
					Span(10, 20, FontWeight(WeightBold)),
					Span(25, 35, TextColor(0, 0.8, 0.5, 1)),
				}
				resolved := resolveTextSpans(style, spans)
				shaped := ShapeTextMax(s, style, 0, spans...)
				ShapedTextLayout(shaped, style, 5, 30, resolved...)
			}
		})
	})
}

// Steady-state ShapeTextMax: cache hit every call.
func BenchmarkShapeHit(b *testing.B) {
	InitFontSubsystem()
	style := DefaultTextStyle()
	const s = "The quick brown fox jumps over the lazy dog"
	ShapeTextMax(s, style, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ShapeTextMax(s, style, 0)
	}
}

// Same as ShapeHit with a 7-name family list (Monospace). Pins hashing the
// interned list id on a hit (no LookupFace, no name walk).
func BenchmarkShapeHitMono(b *testing.B) {
	InitFontSubsystem()
	style := DefaultTextStyle()
	style.SetFontFamilies(Monospace...)
	const s = "The quick brown fox jumps over the lazy dog"
	ShapeTextMax(s, style, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ShapeTextMax(s, style, 0)
	}
}

// Same paragraph, a new integer wrap width every call (wrap cache misses;
// unwrapped shape hits). A fused (text+width) key pays HarfBuzz each time.
func BenchmarkShapeWrapReuse(b *testing.B) {
	InitFontSubsystem()
	ui.Host.WindowScale = 1
	style := DefaultTextStyle()
	const s = "The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. How vexingly quick daft zebras jump when wrapping this paragraph."
	ShapeTextMax(s, style, 80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ShapeTextMax(s, style, float32(60+i%20000))
	}
}

// Same paragraph, wrap widths that all round to 100 device px but have
// distinct float bits. Quantization hits; a raw-float key misses and
// thrashes a 4096 LRU.
func BenchmarkShapeWrapJitter(b *testing.B) {
	InitFontSubsystem()
	ui.Host.WindowScale = 1
	style := DefaultTextStyle()
	const s = "The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs."
	ShapeTextMax(s, style, 100)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ShapeTextMax(s, style, 100+float32(i%8000)*0.00005)
	}
}

// Always-miss ShapeTextMax: 8192 distinct strings cycled through a 4096-entry
// LRU, so every call shapes from scratch.
func BenchmarkShapeMiss(b *testing.B) {
	InitFontSubsystem()
	style := DefaultTextStyle()
	texts := make([]string, 8192)
	for i := range texts {
		texts[i] = fmt.Sprintf("The quick brown fox %d jumps over the lazy dog", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ShapeTextMax(texts[i%len(texts)], style, 0)
	}
}
