package shirei

import (
	"testing"
	"time"

	"github.com/go-text/typesetting/language"
)

func waitForFontScan(t *testing.T) {
	t.Helper()
	InitFontSubsystem()
	deadline := time.Now().Add(10 * time.Second)
	for !SystemFontScanDone() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

// Line-box metrics (Height, descenderPad) come from the paragraph's
// primary face. A fallback coverage face (Naskh, emoji, CJK, …) may
// have a much larger hhea descender; it must not change the box.
func TestFallbackFaceDoesNotInflateLineBox(t *testing.T) {
	waitForFontScan(t)
	st := DefaultTextStyle()
	latin := ShapeText("ABC dest", st)
	mixed := ShapeText("ABC dest ابجد", st)
	if len(latin.Lines) == 0 || len(mixed.Lines) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	var latinFont, arabicFont FontId
	for _, s := range mixed.Lines[0].Segments {
		switch s.sc {
		case language.Arabic:
			arabicFont = s.font
		case language.Latin:
			if latinFont == 0 {
				latinFont = s.font
			}
		}
	}
	if arabicFont == 0 {
		t.Skip("no Arabic coverage face")
	}
	if arabicFont == latinFont {
		t.Skip("Arabic shaped with the Latin face; no fallback metrics to pin")
	}

	lp, mp := latin.Lines[0].descenderPad, mixed.Lines[0].descenderPad
	if mp > lp+0.05 {
		t.Fatalf("mixed descenderPad = %.2f, latin = %.2f (fallback face inflated the line box)", mp, lp)
	}
	lh, mh := latin.Lines[0].Height, mixed.Lines[0].Height
	if mh > lh+0.05 {
		t.Fatalf("mixed Height = %.2f, latin = %.2f (fallback face inflated the line box)", mh, lh)
	}
}
