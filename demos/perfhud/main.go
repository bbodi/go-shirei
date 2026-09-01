// A playground for the perfhud overlay: buttons that make the numbers move,
// so you can watch each chart react to a cause you chose.
//
//	go run ./demos/perfhud
//
// The overlay starts visible. F1 toggles it, and the hitch recorder under it
// stays on either way — "stall one frame" writes a record to a hitch log in
// the temp directory, with an execution trace beside it; the path is printed
// at startup. Clicking a row in the top-allocator table opens its full call
// stack.
//
// The bottom half of the file is the extension side: this demo registers a
// probe, a chart, a stat row and a table of its own, the same way an app
// would.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	app "go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/perfhud"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// The load the demo is currently generating. The probes below read this, so
// every chart has a switch you can flip.
var load struct {
	allocPerFrame int  // KB to allocate and drop each frame
	labels        int  // extra labels to lay out
	uniqueText    bool // defeat the shape cache with a new string every frame
	goroutines    int  // parked goroutines
	stall         bool // burn a long frame once, to trigger a hitch
}

// sink keeps the per-frame garbage reachable for exactly one frame, so the
// allocation shows up as churn rather than as a growing heap.
var sink [][]byte

func RootView() {
	Container(Attrs(Viewport, Background(225, 14, 12, 1), Pad(24), Gap(14)), func() {
		generateLoad()

		Label("perfhud demo", TextColor(0, 0, 96, 1), FontSize(22))
		Label("Every button below moves one of the charts on the right.",
			TextColor(0, 0, 70, 1))

		controls()
		Filler(1)
		Label("F1: hide the overlay   ·   click a top-allocator row for its call stack",
			TextColor(0, 0, 55, 1), FontSize(12))

		// F1 rather than a letter or space: a focused button answers to those.
		if GetFrameInput().Key == KeyF1 {
			perfhud.Toggle()
		}
	})

	// Last statement of the frame, so the panel floats over everything.
	perfhud.Draw()
}

func controls() {
	Container(Attrs(Row, Wrap, Gap(8)), func() {
		if CtrlButton(NoIcon, fmt.Sprintf("alloc %d KB/frame", load.allocPerFrame), true) {
			load.allocPerFrame = next(load.allocPerFrame, 0, 64, 512, 4096)
		}
		if CtrlButton(NoIcon, fmt.Sprintf("%d labels", load.labels), true) {
			load.labels = next(load.labels, 0, 200, 1000, 4000)
		}
		if CtrlButton(NoIcon, "unique text: "+onOff(load.uniqueText), true) {
			load.uniqueText = !load.uniqueText
		}
		if CtrlButton(NoIcon, fmt.Sprintf("%d goroutines", load.goroutines), true) {
			load.goroutines = next(load.goroutines, 0, 100, 1000, 10000)
		}
		if CtrlButton(NoIcon, "stall one frame", true) {
			load.stall = true
		}
		if CtrlButton(NoIcon, "force a GC", true) {
			runtime.GC()
		}
	})
	Container(Attrs(Row, Wrap, Gap(8)), func() {
		if CtrlButton(NoIcon, "toggle overlay", true) {
			perfhud.Toggle()
		}
		if CtrlButton(NoIcon, "reset load", true) {
			load.allocPerFrame, load.labels, load.goroutines = 0, 0, 0
			load.uniqueText = false
		}
	})
}

// generateLoad does whatever the buttons asked for, then lays out the extra
// labels inside the current container so they cost real layout and shaping.
func generateLoad() {
	if load.stall {
		load.stall = false
		time.Sleep(250 * time.Millisecond)
	}
	for len(parked) < load.goroutines {
		stop := make(chan struct{})
		parked = append(parked, stop)
		go func() { <-stop }()
	}
	for len(parked) > load.goroutines {
		close(parked[len(parked)-1])
		parked = parked[:len(parked)-1]
	}

	sink = sink[:0]
	for kb := 0; kb < load.allocPerFrame; kb++ {
		sink = append(sink, make([]byte, 1024))
	}

	if load.labels == 0 {
		return
	}
	frame := GetFrameNumber()
	Container(Attrs(Row, Wrap, Gap(2), MaxHeight(1), Clip), func() {
		for i := 0; i < load.labels; i++ {
			text := "item"
			if load.uniqueText {
				// a string the shape cache has never seen
				text = fmt.Sprintf("item %d-%d", frame, i)
			}
			Label(text, FontSize(11), TextColor(0, 0, 40, 1))
		}
	})
}

var parked []chan struct{}

func next(cur int, steps ...int) int {
	for i, v := range steps {
		if v == cur {
			return steps[(i+1)%len(steps)]
		}
	}
	return steps[0]
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// --- what an app adds to the overlay ---

func setupOverlay() {
	perfhud.Visible = true
	// The temp directory rather than the working one: this repo keeps no
	// .gitignore, and a demo should not drop trace files in it.
	perfhud.Core.HitchLog = filepath.Join(os.TempDir(), "perfhud-demo-hitches.log")
	fmt.Println("hitch log:", perfhud.Core.HitchLog)
	perfhud.Core.Context = func() string {
		return fmt.Sprintf("alloc=%dKB labels=%d", load.allocPerFrame, load.labels)
	}

	// A probe is a number sampled every frame. Only probes are kept in the
	// history, so this is what a custom chart has to plot — a chart reading
	// `load.labels` directly would draw this frame's value across the whole
	// window.
	labels := perfhud.AddProbe("demo labels", func() float64 {
		return float64(load.labels)
	})
	perfhud.AddChart(perfhud.Chart{
		Title:  "demo labels",
		Value:  perfhud.ProbeValue(labels),
		Format: perfhud.FormatCount,
		Floor:  500,
	})

	perfhud.AddStats(func() []perfhud.Stat {
		return []perfhud.Stat{
			{Key: "demo alloc", Value: perfhud.FormatKB(float64(load.allocPerFrame))},
			{Key: "parked", Value: perfhud.FormatCount(float64(len(parked)))},
		}
	})

	// A table is the top-allocator shape, reusable for any other
	// "what is costing me" list. This one ranks the demo's own switches.
	perfhud.AddTable(perfhud.Table{
		Title: "demo load", Header: "weight", Limit: 4,
		Rows: func() []perfhud.TableRow { return loadRows() },
	})
}

// loadRows lists whichever switches are on, heaviest first.
func loadRows() []perfhud.TableRow {
	type item struct {
		name   string
		weight float64
		text   string
	}
	items := []item{
		{"alloc/frame", float64(load.allocPerFrame), perfhud.FormatKB(float64(load.allocPerFrame))},
		{"labels", float64(load.labels), perfhud.FormatCount(float64(load.labels))},
		{"goroutines", float64(len(parked)), perfhud.FormatCount(float64(len(parked)))},
	}
	if load.uniqueText {
		items = append(items, item{"unique text", 1e9, "on"})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].weight > items[j].weight })

	var out []perfhud.TableRow
	for _, it := range items {
		if it.weight <= 0 {
			continue
		}
		out = append(out, perfhud.TableRow{Value: it.text, Name: it.name})
	}
	if len(out) == 0 {
		out = append(out, perfhud.TableRow{Value: "—", Name: "nothing switched on"})
	}
	return out
}

func main() {
	// The heap profile is sampled at 512 KB by default, which is far too
	// coarse to see per-frame allocation. The top-allocator table needs this.
	runtime.MemProfileRate = 16 * 1024
	setupOverlay()

	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		// Two passes: the panel sizes itself from the previous frame, and the
		// charts want more than one sample.
		for i := 0; i < 8; i++ {
			if err := RenderToPNG(os.Args[2], 1100, 800, RootView); err != nil {
				fmt.Println("render failed:", err)
				return
			}
		}
		return
	}

	app.SetupWindow("perfhud demo", 1100, 800)
	app.Run(RootView)
}
