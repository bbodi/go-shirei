package shirei

import (
	"bytes"
	"testing"
)

func TestGlyphRunHashIgnoresFirstIndex(t *testing.T) {
	s := Surface{
		Rect:          Rect{Size: Vec2{10, 12}},
		GlyphRunFirst: 0,
		GlyphRunCount: 2,
	}
	runs := []GlyphRun{
		{Rect: Rect{Size: Vec2{5, 12}}, FontId: 1, GlyphId: 2, Color: Vec4{0, 0, 0, 1}},
		{Rect: Rect{Origin: Vec2{5, 0}, Size: Vec2{5, 12}}, FontId: 1, GlyphId: 3, Color: Vec4{0, 0, 0, 1}},
	}
	h1 := computeSurfacesHash([]Surface{s}, runs)

	pad := []GlyphRun{
		{FontId: 9, GlyphId: 9},
		{FontId: 8, GlyphId: 8},
	}
	s.GlyphRunFirst = 2
	h2 := computeSurfacesHash([]Surface{s}, append(pad, runs...))
	if h1 != h2 {
		t.Fatalf("hash changed when GlyphRunFirst shifted (h1=%016x h2=%016x)", h1, h2)
	}
}

func TestGlyphRunHashTracksContent(t *testing.T) {
	s := Surface{Rect: Rect{Size: Vec2{10, 12}}, GlyphRunCount: 2}
	runs := []GlyphRun{
		{Rect: Rect{Size: Vec2{5, 12}}, FontId: 1, GlyphId: 2, Color: Vec4{0, 0, 0, 1}},
		{Rect: Rect{Origin: Vec2{5, 0}, Size: Vec2{5, 12}}, FontId: 1, GlyphId: 3, Color: Vec4{0, 0, 0, 1}},
	}
	h1 := computeSurfacesHash([]Surface{s}, runs)
	runs[0].GlyphId = 99
	h2 := computeSurfacesHash([]Surface{s}, runs)
	if h1 == h2 {
		t.Fatal("hash ignored GlyphRun content")
	}
}

func TestLabelEmitsGlyphRuns(t *testing.T) {
	shaped := ShapeText("Hello", DefaultTextStyle())
	if len(shaped.Lines) == 0 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts")
	}

	ui.Host.WindowSize = Vec2{200, 80}
	ui.Host.WindowScale = 1
	sid := softScope("glyphrun_label")
	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(sid, AttrSet{}, func() {
				Label("Hello")
			})
		})
	}

	var runSurfaces, runGlyphs int
	for _, s := range out.Surfaces {
		if s.GlyphRunCount > 0 {
			if s.GlyphData == nil || s.GlyphData.Len() != int(s.GlyphRunCount) {
				t.Fatal("label must retain its immutable glyph geometry")
			}
			runSurfaces++
			runGlyphs += int(s.GlyphRunCount)
			if s.FontId != 0 || s.GlyphId != 0 {
				t.Errorf("run surface carries FontId=%d GlyphId=%d", s.FontId, s.GlyphId)
			}
		}
	}
	if runSurfaces < 1 {
		t.Fatalf("run surfaces=%d want at least 1", runSurfaces)
	}
	if runGlyphs < 4 {
		t.Fatalf("run glyphs=%d want at least 4 for Hello", runGlyphs)
	}
	if len(out.GlyphRuns) != 0 {
		t.Fatalf("label copies %d glyph records into the frame", len(out.GlyphRuns))
	}
}

func TestGlyphRunRenderMatchesExpandedSurfaces(t *testing.T) {
	shaped := ShapeText("Hello", DefaultTextStyle())
	if len(shaped.Lines) == 0 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts")
	}

	prev := ui.Host.GlyphCacheBudgetBytes
	ui.Host.GlyphCacheBudgetBytes = 16 << 20
	defer func() { ui.Host.GlyphCacheBudgetBytes = prev }()

	ui.Host.WindowSize = Vec2{200, 80}
	ui.Host.WindowScale = 1
	sid := softScope("glyphrun_pixels")
	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(sid, AttrSet{Background: Vec4{0, 0, 100, 1}}, func() {
				Label("Hello", TextColor(0, 0, 0, 1))
			})
		})
	}

	flat := make([]Surface, 0, len(out.Surfaces)+len(out.GlyphRuns))
	for i := range out.Surfaces {
		s := out.Surfaces[i]
		if s.GlyphRunCount == 0 {
			flat = append(flat, s)
			continue
		}
		for j := 0; j < int(s.GlyphRunCount); j++ {
			g := s.GlyphRunAt(j, out.GlyphRuns)
			flat = append(flat, Surface{
				Rect:        g.Rect,
				Color1:      g.Color,
				Color2:      g.Color,
				FontId:      g.FontId,
				GlyphId:     g.GlyphId,
				GlyphOffset: g.GlyphOffset,
			})
		}
	}

	const w, h, scale = 200, 80, 1
	var r1, r2 SoftRenderer
	r1.noRegionCache = true
	r2.noRegionCache = true
	a := r1.Render(out.Surfaces, out.GlyphRuns, w, h, scale)
	b := r2.Render(flat, nil, w, h, scale)
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("glyph-run paint differs from expanded per-glyph surfaces")
	}
	var ink int
	for i := 0; i < len(a.Pix); i += 4 {
		if a.Pix[i] != 0xff || a.Pix[i+1] != 0xff || a.Pix[i+2] != 0xff {
			ink++
		}
	}
	if ink < 10 {
		t.Fatalf("no text ink (%d px)", ink)
	}
}
