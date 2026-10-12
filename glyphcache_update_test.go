package shirei

import (
	"container/list"
	"testing"
)

// UI frame numbers may coincide while sharing a bitmap cache. An entry used
// by an earlier update must not prevent eviction during a different UI's update.
func TestGlyphCacheEvictionAcrossUIs(t *testing.T) {
	style := requireTextShaping(t)
	shaped := ShapeText("AB", style)
	line := shaped.Lines[0]
	if len(line.runs) != 2 {
		t.Fatal("expected two shaped glyphs")
	}
	prevUI := ui
	prevMap, prevList, prevBytes := res.glyphMap, res.glyphList, res.glyphBytes
	prevAdded, prevEvicted := res.glyphsAddedBuf, res.glyphsEvictedBuf
	res.glyphMap, res.glyphList, res.glyphBytes = make(map[GlyphKey]*list.Element), list.New(), 0
	defer func() {
		bindUI(prevUI)
		res.glyphMap, res.glyphList, res.glyphBytes = prevMap, prevList, prevBytes
		res.glyphsAddedBuf, res.glyphsEvictedBuf = prevAdded, prevEvicted
	}()
	first, second := NewUI(), NewUI()
	first.FrameNumber, second.FrameNumber = 42, 42
	first.Host.WindowScale, second.Host.WindowScale = 1, 1
	first.Host.GlyphCacheBudgetBytes, second.Host.GlyphCacheBudgetBytes = 1, 1
	bindUI(first)
	keyA, _ := GlyphKeyForRun(&line.runs[0])
	keyB, _ := GlyphKeyForRun(&line.runs[1])
	surfaces := []Surface{{GlyphRunCount: 2}}
	added, _ := updateGlyphCache(surfaces, []GlyphRun{line.runs[0], line.runs[0]})
	if len(added) != 1 {
		t.Fatalf("repeated glyph adds %d bitmaps", len(added))
	}
	bindUI(second)
	added, evicted := updateGlyphCache(surfaces, []GlyphRun{line.runs[1], line.runs[1]})
	if len(added) != 1 || added[0] != keyB || len(evicted) != 1 || evicted[0] != keyA {
		t.Fatalf("second UI: added=%v evicted=%v", added, evicted)
	}
	bindUI(first)
	added, evicted = updateGlyphCache(surfaces, []GlyphRun{line.runs[0], line.runs[0]})
	if len(added) != 1 || added[0] != keyA || len(evicted) != 1 || evicted[0] != keyB {
		t.Fatalf("returning UI: added=%v evicted=%v", added, evicted)
	}
}
