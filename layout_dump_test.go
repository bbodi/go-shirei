package shirei

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Layout-dump goldens pin resolved geometry after RunFrameFn.
// UPDATE_SNAPSHOTS=1 rewrites testdata/layout_dump/*.txt; a mismatch
// writes *.actual.txt.

func dumpF32(v float32) string {
	if v == 0 {
		return "0"
	}
	s := strconv.FormatFloat(float64(v), 'f', 4, 32)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

func dumpVec2(v Vec2) string {
	return dumpF32(v[0]) + "," + dumpF32(v[1])
}

func dumpVec4(v Vec4) string {
	return dumpF32(v[0]) + "," + dumpF32(v[1]) + "," + dumpF32(v[2]) + "," + dumpF32(v[3])
}

func dumpRect(r Rect) string {
	return dumpVec2(r.Origin) + "," + dumpVec2(r.Size)
}

func dumpLayoutTree() string {
	var b strings.Builder
	dumpLayoutNode(&b, ui.current, 0)
	return b.String()
}

func dumpLayoutNode(b *strings.Builder, c *_Container, depth int) {
	if c == nil {
		return
	}
	b.WriteString("d=")
	b.WriteString(strconv.Itoa(depth))
	b.WriteString(" orig=")
	b.WriteString(dumpVec2(c.resolvedOrigin))
	b.WriteString(" size=")
	b.WriteString(dumpVec2(c.resolvedSize))
	b.WriteString(" screen=")
	b.WriteString(dumpRect(c.ScreenRect))
	b.WriteString(" content=")
	b.WriteString(dumpVec2(c.ContentSize))
	b.WriteString(" scroll=")
	b.WriteString(dumpVec2(c.ScrollOffset))
	b.WriteString(" pad=")
	b.WriteString(dumpVec4(c.Padding))
	b.WriteString(" corners=")
	b.WriteString(dumpVec4(c.Corners))
	b.WriteString(" border=")
	b.WriteString(dumpF32(c.BorderWidth))
	b.WriteString(" alpha=")
	b.WriteString(dumpF32(c.Transparency))
	b.WriteByte('\n')
	for _, ch := range c.children {
		dumpLayoutNode(b, ch, depth+1)
	}
}

func checkLayoutDump(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "layout_dump", name+".txt")
	want, err := os.ReadFile(path)
	update := os.Getenv("UPDATE_SNAPSHOTS") == "1"
	if os.IsNotExist(err) || update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		if os.IsNotExist(err) {
			t.Logf("created layout dump %s; review it and commit it", path)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(want) == got {
		return
	}
	actual := filepath.Join("testdata", "layout_dump", name+".actual.txt")
	if werr := os.WriteFile(actual, []byte(got), 0o644); werr != nil {
		t.Fatal(werr)
	}
	t.Errorf("layout dump does not match %s; wrote %s", path, actual)
}

var dumpScopes = make(map[string]any)

func dumpScope(name string) any {
	id, ok := dumpScopes[name]
	if !ok {
		id = name
		dumpScopes[name] = id
	}
	return id
}

func runDumpFrame(name string, w, h float32, fn FrameFn) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{w, h}
	RunFrameFn(func() {
		ContainerWithKey(dumpScope(name), AttrSet{}, fn)
	})
}

func TestLayoutDumpWrap(t *testing.T) {
	runDumpFrame(t.Name(), 280, 200, func() {
		Container(Attrs(Row, Wrap, Gap(8), Pad(10), MaxSizeVec(Vec2{150, 0})), func() {
			Element(Attrs(MinSize(40, 30)))
			Element(Attrs(MinSize(40, 40)))
			Element(Attrs(Float(8, 12), MinSize(18, 18)))
			Element(Attrs(MinSize(40, 30)))
			Element(Attrs(Grow(1), MinSize(20, 20)))
			Element(Attrs(Expand, MinSize(30, 16)))
			Element(Attrs(MinSize(40, 50)))
			Element(Attrs(Float(4, 70), MinSize(16, 16)))
			Element(Attrs(MinSize(40, 30)))
			Element(Attrs(Expand, Grow(1), MinSize(24, 12)))
		})
	})
	checkLayoutDump(t, "wrap", dumpLayoutTree())
}

func TestLayoutDumpGrowAlign(t *testing.T) {
	runDumpFrame(t.Name(), 400, 520, func() {
		aligns := []Alignment{AlignStart, AlignMiddle, AlignEnd}
		Container(Attrs(Gap(8), Pad(8)), func() {
			for _, main := range aligns {
				for _, cross := range aligns {
					Container(Attrs(Row, Gap(4), Pad(4), MinSize(360, 56), MaxSizeVec(Vec2{360, 56}),
						MainAlign(main), CrossAlign(cross)), func() {
						Element(Attrs(MinSize(30, 20)))
						Element(Attrs(Grow(1), MinSize(0, 30)))
						Element(Attrs(MinSize(30, 40)))
					})
				}
			}
		})
	})
	checkLayoutDump(t, "grow_align", dumpLayoutTree())
}

func TestLayoutDumpSelfAlign(t *testing.T) {
	runDumpFrame(t.Name(), 320, 140, func() {
		Container(Attrs(Row, Gap(8), Pad(10), MinSize(300, 80), CrossAlign(AlignMiddle)), func() {
			Element(Attrs(MinSize(40, 20)))
			Element(Attrs(MinSize(40, 40), SelfAlign(AlignStart)))
			Element(Attrs(MinSize(40, 20), SelfAlign(AlignEnd)))
			Element(Attrs(MinSize(40, 30), SelfAlign(AlignMiddle)))
		})
	})
	checkLayoutDump(t, "self_align", dumpLayoutTree())
}

func TestLayoutDumpMinMaxExtrinsic(t *testing.T) {
	runDumpFrame(t.Name(), 320, 240, func() {
		Container(Attrs(Gap(8), Pad(10)), func() {
			Container(Attrs(MinSize(80, 60)), func() {
				Element(Attrs(MinSize(20, 20)))
			})
			Container(Attrs(MaxSizeVec(Vec2{50, 40})), func() {
				Element(Attrs(MinSize(100, 80)))
			})
			Container(Attrs(Extrinsic, MinSize(70, 50), MaxSizeVec(Vec2{70, 50})), func() {
				Element(Attrs(MinSize(10, 10)))
			})
			Container(Attrs(MinSize(40, 40), MaxSizeVec(Vec2{100, 100})), func() {
				Element(Attrs(MinSize(60, 60)))
			})
		})
	})
	checkLayoutDump(t, "minmax_extrinsic", dumpLayoutTree())
}

func TestLayoutDumpScrollClamp(t *testing.T) {
	runDumpFrame(t.Name(), 200, 200, func() {
		Container(Attrs(Clip, MinSize(80, 80), MaxSizeVec(Vec2{80, 80})), func() {
			SetScrollOffset(Vec2{-50, 500})
			// UnsetMaxCross keeps the 200-wide min so both axes have overflow
			// to clamp (a column parent would otherwise cascade MaxWidth).
			Element(Attrs(UnsetMaxCross, MinSize(200, 200)))
		})
	})
	checkLayoutDump(t, "scroll_clamp", dumpLayoutTree())
}

func TestLayoutDumpNestedMeasure(t *testing.T) {
	runDumpFrame(t.Name(), 240, 160, func() {
		sz := Measure(Vec2{100, 0}, func() {
			Element(Attrs(MinSize(40, 20)))
			Element(Attrs(MinSize(40, 30)))
		})
		Element(Attrs(MinSizeVec(sz)))
		Element(Attrs(MinSize(20, 20)))
	})
	checkLayoutDump(t, "nested_measure", dumpLayoutTree())
}

func TestLayoutDumpPopups(t *testing.T) {
	runDumpFrame(t.Name(), 240, 180, func() {
		Element(Attrs(MinSize(40, 40)))
		Popup(func() {
			Container(Attrs(Float(12, 16), MinSize(60, 40), Pad(4)), func() {
				Element(Attrs(MinSize(20, 20)))
			})
		})
	})
	checkLayoutDump(t, "popups", dumpLayoutTree())
}

func TestLayoutDumpAnimate(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{240, 160}
	ui.pinnedTimeDelta = true
	ui.timeDelta = 0.016
	t.Cleanup(func() {
		ui.pinnedTimeDelta = false
	})

	w, pad, corner := float32(40), float32(4), float32(2)
	showLate := false
	var b strings.Builder
	for frame := 0; frame < 4; frame++ {
		if frame > 0 {
			w, pad, corner = 80, 16, 12
		}
		if frame >= 2 {
			showLate = true
		}
		RunFrameFn(func() {
			ContainerWithKey(dumpScope(t.Name()), AttrSet{}, func() {
				ContainerWithKey("box", Attrs(
					AnimateOnly(AnimSize|AnimPad|AnimCorners),
					FixSize(w, w), Pad(pad), Corners(corner),
				), nil)
				if showLate {
					ContainerWithKey("late", Attrs(
						AnimateOnly(AnimSize),
						FixSize(50, 50),
					), nil)
				}
			})
		})
		b.WriteString("# frame ")
		b.WriteString(strconv.Itoa(frame))
		b.WriteByte('\n')
		b.WriteString(dumpLayoutTree())
	}
	checkLayoutDump(t, "animate", b.String())
}
