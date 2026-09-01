package perfhud

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"go.hasen.dev/shirei/ext/perfhud/perfcore"

	. "go.hasen.dev/shirei"
)

func resetDetail() {
	detail = struct {
		open  bool
		title string
		lines []string
		key   string
	}{}
}

func TestOpenDetailTogglesAndSwaps(t *testing.T) {
	resetDetail()
	defer resetDetail()

	openDetail("0:pkg.Hot", "pkg.Hot", "line one\nline two\n")
	if !detail.open || detail.title != "pkg.Hot" || len(detail.lines) != 2 {
		t.Fatalf("first click did not open the view: %+v", detail)
	}

	// the same row again closes it
	openDetail("0:pkg.Hot", "pkg.Hot", "line one\n")
	if detail.open {
		t.Error("clicking the open row again should close the view")
	}

	// a different row opens with the new contents
	openDetail("0:pkg.Other", "pkg.Other", "other\n")
	if !detail.open || detail.title != "pkg.Other" {
		t.Errorf("clicking another row should swap the contents: %+v", detail)
	}

	// nothing to show is not an empty window
	resetDetail()
	openDetail("0:pkg.Hot", "pkg.Hot", "")
	if detail.open {
		t.Error("empty detail text should not open the view")
	}
}

// An app that can open a real second window takes over through the hook; the
// built-in panel then stays shut.
func TestOpenDetailHookTakesOver(t *testing.T) {
	resetDetail()
	defer resetDetail()
	prev := Config.OpenDetail
	defer func() { Config.OpenDetail = prev }()

	var gotTitle, gotText string
	Config.OpenDetail = func(title, text string) bool {
		gotTitle, gotText = title, text
		return true
	}
	openDetail("0:pkg.Hot", "pkg.Hot", "the stack\n")

	if detail.open {
		t.Error("the hook handled it, so the built-in panel should stay closed")
	}
	if gotTitle != "pkg.Hot" || !strings.Contains(gotText, "the stack") {
		t.Errorf("the hook got %q / %q", gotTitle, gotText)
	}

	// A hook that declines falls back to the built-in panel.
	Config.OpenDetail = func(string, string) bool { return false }
	openDetail("0:pkg.Hot", "pkg.Hot", "the stack\n")
	if !detail.open {
		t.Error("a hook returning false should fall back to the built-in panel")
	}
}

func TestDetailViewDraws(t *testing.T) {
	resetDetail()
	defer resetDetail()

	openDetail("0:pkg.Hot", "pkg.Hot", strings.Repeat("a frame of the stack\n", 40))

	const w, h = 900, 700
	img := renderOverlay(t, w, h)
	b := img.Bounds()
	// The view is centred, so the middle of the window must no longer look
	// like the empty background in the far corner.
	mid := img.RGBAAt((b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2)
	corner := img.RGBAAt(b.Min.X+20, b.Max.Y-20)
	if mid == corner {
		t.Errorf("the centre (%v) looks like empty background (%v): the detail view did not draw", mid, corner)
	}
}

// The report is graded so a long stack can be skimmed.
func TestDetailLineColors(t *testing.T) {
	th := &Config.Theme
	cases := []struct {
		line string
		want [4]float32
	}{
		{"--- stack 1/2 ---", th.RowValue},
		{"      /src/app/main.go:42", th.Faint},
		{"  main.buildDisplay", th.RowName},
		{"pkg.Hot", th.Dim},
	}
	for _, c := range cases {
		if got := detailLineColor(c.line); got != c.want {
			t.Errorf("detailLineColor(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// TestDetailGolden writes the detail view to PERFHUD_GOLDEN_DETAIL for a
// visual check. It is a no-op unless that variable is set.
func TestDetailGolden(t *testing.T) {
	path := os.Getenv("PERFHUD_GOLDEN_DETAIL")
	if path == "" {
		t.Skip("set PERFHUD_GOLDEN_DETAIL=<file.png> to write the detail view")
	}
	resetDetail()
	defer resetDetail()

	var pcs [16]uintptr
	runtime.Callers(1, pcs[:])
	entry := perfcore.AllocEntry{
		Name: "go.hasen.dev/shirei.ShapeTextMax",
		KB:   412.5,
		Stacks: []perfcore.AllocStack{
			{KB: 300.5, Objects: 1204, LiveKB: 96, LiveObjects: 300, PCs: pcs},
			{KB: 112.0, Objects: 448, LiveKB: 12, LiveObjects: 40, PCs: pcs},
		},
	}
	openDetail("0:x", entry.Name, entry.Report())

	prevVisible, prevKeep, prevLog := Visible, Config.KeepAwake, Core.HitchLog
	Visible, Config.KeepAwake, Core.HitchLog = true, false, ""
	defer func() { Visible, Config.KeepAwake, Core.HitchLog = prevVisible, prevKeep, prevLog }()

	var err error
	for i := 0; i < 3; i++ {
		err = RenderToPNG(path, 1200, 820, func() {
			Container(Attrs(Viewport, Background(225, 14, 10, 1)), func() { Void() })
			seedHistory(240)
			drawPanel()
		})
	}
	if err != nil {
		t.Fatal(err)
	}
}
