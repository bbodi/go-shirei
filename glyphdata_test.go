package shirei

import (
	"bytes"
	"runtime"
	"slices"
	"testing"
)

func TestSharedGlyphRenderingAndHashChanges(t *testing.T) {
	style := requireTextShaping(t)
	shaped := ShapeText("Hello", style)
	glyphs := slices.Clone(shaped.Lines[0].runs)
	data := NewGlyphRunData(glyphs)
	red, blue := Vec4{0, 80, 50, 1}, Vec4{210, 80, 50, 1}
	prevBudget, prevScale := ui.Host.GlyphCacheBudgetBytes, ui.Host.WindowScale
	ui.Host.GlyphCacheBudgetBytes = 16 << 20
	defer func() {
		ui.Host.GlyphCacheBudgetBytes, ui.Host.WindowScale = prevBudget, prevScale
	}()
	var cached SoftRenderer
	var previousHash uint64
	for index, state := range []struct {
		origin Vec2
		color  Vec4
		scale  float32
	}{
		{Vec2{7, 9}, red, 1},
		{Vec2{17, 19}, red, 1},
		{Vec2{17, 19}, blue, 1},
		{Vec2{17, 19}, blue, 2},
	} {
		ui.Host.WindowScale = state.scale
		line := Surface{Rect: Rect{Origin: state.origin, Size: Vec2{shaped.Lines[0].Width, style.FontSize}},
			Color1: state.color, GlyphData: data, GlyphRunCount: int32(data.Len())}
		shared := []Surface{
			{Rect: Rect{Size: Vec2{200, 80}}, Color1: Vec4{0, 0, 100, 1}, Color2: Vec4{0, 0, 100, 1}, Clip: ClipPush},
			line,
			{Clip: ClipPop},
		}
		updateGlyphCache(shared, nil)
		// Build an independent screen-space reference from the original input.
		flat := []Surface{shared[0]}
		for _, glyph := range glyphs {
			glyph.Rect.Origin = Vec2Add(glyph.Rect.Origin, state.origin)
			flat = append(flat, Surface{Rect: glyph.Rect, Color1: state.color, Color2: state.color,
				FontId: glyph.FontId, GlyphId: glyph.GlyphId, GlyphOffset: glyph.GlyphOffset})
		}
		flat = append(flat, Surface{Clip: ClipPop})
		w, h := int(200*state.scale), int(80*state.scale)
		var reference SoftRenderer
		reference.noRegionCache = true
		want := reference.Render(flat, nil, w, h, state.scale)
		for range 3 {
			got := cached.Render(shared, nil, w, h, state.scale)
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("state %d: shared/cached text differs from screen-space reference", index)
			}
		}
		hash := computeSurfacesHash(shared, nil)
		if index > 0 && index < 3 && hash == previousHash {
			t.Fatalf("state %d: movement/recoloring did not change hash", index)
		}
		previousHash = hash
		// Equal content at another address must not change visual identity.
		shared[1].GlyphData = NewGlyphRunData(glyphs)
		if computeSurfacesHash(shared, nil) != hash {
			t.Fatal("hash depends on glyph-data address")
		}
	}
	// The constructor takes a copy, so caller edits cannot invalidate a hash.
	s := Surface{GlyphData: data, GlyphRunCount: int32(data.Len())}
	hash := computeSurfacesHash([]Surface{s}, nil)
	glyphs[0].GlyphId++
	if computeSurfacesHash([]Surface{s}, nil) != hash || s.GlyphRunAt(0, nil).GlyphId == glyphs[0].GlyphId {
		t.Fatal("immutable glyph data aliases caller-owned storage")
	}
	s.GlyphData = NewGlyphRunData(glyphs)
	if computeSurfacesHash([]Surface{s}, nil) == hash {
		t.Fatal("hash ignores changed glyph content")
	}
}

func TestTextFrameRetainsGeometryAcrossCacheEviction(t *testing.T) {
	waitForFontScan(t)
	requireTextShaping(t)
	previous := ui
	bindUI(NewUI())
	defer bindUI(previous)
	ui.Host.WindowSize = Vec2{320, 160}
	ui.Host.WindowScale = 1
	ui.Host.GlyphCacheBudgetBytes = 16 << 20
	color := Vec4{0, 80, 50, 1}
	frame := func() {
		ModAttrs(NoAnimate)
		Label("Retained geometry", TextColorVec(color))
	}
	var retained []Surface
	var geometry *GlyphRunData
	for i := 0; i < 3; i++ {
		out := RunFrameFn(frame)
		for _, s := range out.Surfaces {
			if s.GlyphData == nil {
				continue
			}
			if geometry != nil && geometry != s.GlyphData {
				t.Fatal("uniform recoloring rebuilds glyph geometry")
			}
			geometry = s.GlyphData
		}
		if i == 1 {
			retained = slices.Clone(out.Surfaces)
		}
		if i == 1 {
			color = Vec4{210, 80, 50, 1}
		}
	}
	if geometry == nil {
		t.Fatal("no shared glyph geometry")
	}
	hash := computeSurfacesHash(retained, nil)
	var before SoftRenderer
	pixels := slices.Clone(before.Render(retained, nil, 320, 160, 1).Pix)
	// Remove all cache owners and reuse both container slabs/frame storage.
	res.shapeCacheEpoch = ^uint64(0)
	res.syncShapeCachesToEpoch()
	for range 8 {
		RunFrameFn(func() { ModAttrs(NoAnimate) })
	}
	runtime.GC()
	if computeSurfacesHash(retained, nil) != hash {
		t.Fatal("retained frame changed after cache eviction")
	}
	updateGlyphCache(retained, nil)
	var after SoftRenderer
	if !bytes.Equal(after.Render(retained, nil, 320, 160, 1).Pix, pixels) {
		t.Fatal("retained frame pixels changed after later frames and cache eviction")
	}
}

func TestColoredTextRunCache(t *testing.T) {
	waitForFontScan(t)
	requireTextShaping(t)
	previous := ui
	bindUI(NewUI())
	defer bindUI(previous)
	ui.Host.WindowSize = Vec2{320, 160}
	red, blue := Vec4{0, 80, 50, 1}, Vec4{210, 80, 50, 1}
	var original *GlyphRunData
	for _, color := range []Vec4{red, blue, red} {
		out := RunFrameFn(func() {
			ModAttrs(NoAnimate)
			Text("ABC", TextStyle(), Span(1, 2, TextColorVec(color)))
		})
		found := false
		for _, surface := range out.Surfaces {
			if surface.GlyphData == nil {
				continue
			}
			found = true
			if got := surface.GlyphRunAt(1, nil).Color; got != color {
				t.Fatalf("span color=%v want %v", got, color)
			}
			if color == red {
				if original != nil && original != surface.GlyphData {
					t.Fatal("repeated span colors rebuild immutable run")
				}
				original = surface.GlyphData
			}
		}
		if !found {
			t.Fatal("no colored glyph run")
		}
	}
}
