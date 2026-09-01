// Package perfhud is a drop-in performance overlay for a Shirei app: rolling
// charts of frame rate, allocation, GC pause and heap, a table of scalar
// counters, the live top allocation sites, and an automatic hitch recorder
// that writes an execution trace plus every goroutine stack when a frame runs
// long.
//
// Port it into an app with one call at the end of the frame function:
//
//	func frame() {
//		// ...build the UI...
//		perfhud.Draw()
//	}
//
// Draw samples on every call and paints only while Visible is set, so the
// hitch recorder keeps working with the overlay hidden. Bind Toggle to a key.
//
// This package is the Shirei renderer. Everything framework-free — the
// sampling, the allocation profiler, the hitch recorder, the formatters and
// the panel's content — lives in the perfcore subpackage, reachable here as
// Core:
//
//	perfhud.Core.HitchLog = "myapp-hitches.log" // "" disables recording
//	perfhud.Core.Context = func() string { return currentViewName() }
//
// Config holds the rendering settings instead: size, placement, font, colors.
//
// The panel's three sections are lists an app can extend, reorder or replace:
// Core.Charts, Core.Stats and Core.Tables. See AddChart, AddStats, AddTable
// and AddProbe.
package perfhud

import (
	"os"

	"go.hasen.dev/shirei/ext/perfhud/perfcore"

	. "go.hasen.dev/shirei"
)

// The content types and helpers live in perfcore; these aliases spare a
// Shirei app the second import.
type (
	Sample   = perfcore.Sample
	Stat     = perfcore.Stat
	TableRow = perfcore.TableRow
	Chart    = perfcore.Chart
	Table    = perfcore.Table
	Probe    = perfcore.Probe
	Record   = perfcore.Record
)

var (
	AddChart   = perfcore.AddChart
	AddStats   = perfcore.AddStats
	AddTable   = perfcore.AddTable
	AddProbe   = perfcore.AddProbe
	ProbeValue = perfcore.ProbeValue

	FormatNumber = perfcore.FormatNumber
	FormatCount  = perfcore.FormatCount
	FormatMs     = perfcore.FormatMs
	FormatBytes  = perfcore.FormatBytes
	FormatKB     = perfcore.FormatKB
	FormatMB     = perfcore.FormatMB
)

// Core is the framework-free half's configuration: sampling, the hitch
// recorder, and what the panel shows.
var Core = &perfcore.Config

// Visible controls whether Draw paints. It starts on when the SHIREI_HUD
// environment variable is truthy, so a build can be launched with the overlay
// already up. Sampling and hitch recording run whether or not it is set.
var Visible = envOn("SHIREI_HUD")

// Toggle flips Visible. Bind it to a key.
func Toggle() { Visible = !Visible }

// Corner names the window corner the overlay is pinned to.
type Corner int

const (
	TopRight Corner = iota
	TopLeft
	BottomRight
	BottomLeft
)

// Settings is the rendering configuration. The data half lives on Core.
type Settings struct {
	// Width is the overlay width in logical pixels.
	Width float32
	// Corner and Margin place the overlay in the window.
	Corner Corner
	Margin Vec2

	// ChartHeight is the plot height in logical pixels (excludes the title row).
	ChartHeight float32
	// FontSize is the overlay's text size; every label uses it.
	FontSize float32

	// KeepAwake asks for a frame every frame while the overlay is visible, so
	// the charts keep scrolling in an app that otherwise renders on demand.
	// Turning it off makes the charts stall whenever the app idles.
	KeepAwake bool

	// OpenDetail, when set, receives a clicked row's full text instead of the
	// built-in detail panel. Return true to say the app handled it — that is
	// the hook for an app that can open a real second window by spawning
	// itself, which a library cannot do (Shirei is one window per process).
	OpenDetail func(title, text string) bool

	Theme Theme
}

// Theme holds the overlay's colors as Shirei HSLA vectors.
type Theme struct {
	Panel    Vec4 // overlay background
	Border   Vec4
	Plot     Vec4 // chart background
	Text     Vec4 // values
	Dim      Vec4 // keys and titles
	Faint    Vec4 // table headers, placeholders
	Series   []Vec4
	RowValue Vec4 // a table's number column
	RowName  Vec4 // a table's name column
	RowHover Vec4 // a clickable row under the pointer
}

// Config is the live rendering configuration. Defaults suit a dark app.
var Config = Settings{
	Width:       320,
	Corner:      TopRight,
	Margin:      Vec2{10, 10},
	ChartHeight: 38,
	FontSize:    13,
	KeepAwake:   true,
	Theme: Theme{
		Panel:  Vec4{225, 16, 14, 1},
		Border: Vec4{225, 16, 28, 1},
		Plot:   Vec4{225, 16, 19, 1},
		Text:   Vec4{225, 12, 86, 1},
		Dim:    Vec4{225, 10, 62, 1},
		Faint:  Vec4{225, 10, 46, 1},
		Series: []Vec4{
			{140, 55, 60, 1},
			{45, 80, 62, 1},
			{200, 65, 62, 1},
			{10, 70, 62, 1},
			{280, 50, 68, 1},
			{175, 55, 58, 1},
		},
		RowValue: Vec4{45, 80, 62, 1},
		RowName:  Vec4{200, 45, 72, 1},
		RowHover: Vec4{225, 18, 26, 1},
	},
}

// The numbers only Shirei can report arrive as probes, exactly the way an app
// registers its own — perfcore knows nothing about layout passes or glyph
// caches. Their indices feed the chart and the stat rows below.
var (
	pLayout, pPaint, pImageScale int
	pShapeCalls, pShapeHits      int
	pImages, pImageBytes         int
)

func init() {
	pLayout = perfcore.AddProbe("layout ms", func() float64 {
		return GetHost().LayoutTime.Seconds() * 1000
	})
	pPaint = perfcore.AddProbe("paint ms", func() float64 {
		return GetHost().PaintTime.Seconds() * 1000
	})
	pImageScale = perfcore.AddProbe("imgscale ms", func() float64 {
		return GetHost().ImageScaleTime.Seconds() * 1000
	})
	// ShapeStats counts since process start; the panel wants per-frame work.
	var prevCalls, prevHits int64
	pShapeCalls = perfcore.AddProbe("shape calls", func() float64 {
		d := ShapeStats.Calls - prevCalls
		prevCalls = ShapeStats.Calls
		return float64(d)
	})
	pShapeHits = perfcore.AddProbe("shape hits", func() float64 {
		d := ShapeStats.Hits - prevHits
		prevHits = ShapeStats.Hits
		return float64(d)
	})
	pImages = perfcore.AddProbe("images", func() float64 {
		return float64(DebugGetImageCacheStats().LiveSlots)
	})
	pImageBytes = perfcore.AddProbe("image bytes", func() float64 {
		return float64(DebugGetImageCacheStats().PixelBytes)
	})

	perfcore.AddChart(Chart{
		Title: "layout+paint", Floor: 8, Format: perfcore.FormatMs,
		Value: func(s *Sample) float64 {
			return perfcore.ProbeValue(pLayout)(s) + perfcore.ProbeValue(pPaint)(s)
		},
	})
	perfcore.AddStats(ShireiStats)
}

// ShireiStats is the scalar section for the numbers this renderer probes.
func ShireiStats() []Stat {
	return []Stat{
		{Key: "layout / paint", Value: perfcore.FormatMs(perfcore.ProbeNow(pLayout)) +
			" / " + perfcore.FormatMs(perfcore.ProbeNow(pPaint))},
		{Key: "shape calls/hits", Value: perfcore.FormatCount(perfcore.ProbeNow(pShapeCalls)) +
			" / " + perfcore.FormatCount(perfcore.ProbeNow(pShapeHits))},
		{Key: "images", Value: perfcore.FormatCount(perfcore.ProbeNow(pImages)) +
			" · " + perfcore.FormatBytes(perfcore.ProbeNow(pImageBytes))},
	}
}

// Draw takes this frame's sample, runs the hitch detector, and paints the
// overlay when Visible. Call it once per frame, at the top level of the frame
// function (after the root container, like any other floating overlay).
func Draw() {
	perfcore.Collect()
	if !Visible {
		return
	}
	if Config.KeepAwake {
		RequestNextFrame()
	}
	drawPanel()
}

// drawPanel paints the overlay from whatever history is already collected. It
// is separate from Draw so the painting can be exercised on a seeded history.
func drawPanel() {
	win := GetHost().WindowSize
	if win[0] <= 0 || win[1] <= 0 {
		return
	}
	const pad = 7
	w := Config.Width
	x, y := Config.Margin[0], Config.Margin[1]
	switch Config.Corner {
	case TopRight, BottomRight:
		x = win[0] - w - Config.Margin[0]
	}
	// The panel height is content-driven, so a bottom corner needs the height
	// measured on the previous frame; it is zero on the very first one.
	switch Config.Corner {
	case BottomLeft, BottomRight:
		y = win[1] - lastPanelH - Config.Margin[1]
		if y < Config.Margin[1] {
			y = Config.Margin[1]
		}
	}

	th := &Config.Theme
	id := Container(Attrs(Float(x, y), InFront, FixWidth(w), Gap(4), Pad2(5, pad),
		BackgroundVec(th.Panel), BorderWidth(1), BorderColorVec(th.Border), Corners(6)), func() {
		inner := w - 2*pad
		for i := range Core.Charts {
			drawChart(i, &Core.Charts[i], inner)
		}
		drawStats(inner)
		for i := range Core.Tables {
			drawTable(i, &Core.Tables[i], inner)
		}
	})
	lastPanelH = GetResolvedRectOf(id).Size[1]
	drawDetail() // after the panel, so a clicked stack sits above it
}

// detailKey identifies the open row, so clicking it again closes the view and
// clicking another swaps the contents. The name rather than the index: the
// list reorders itself between scans.
func detailKey(table int, name string) string {
	return string(rune('0'+table)) + ":" + name
}

// lastPanelH carries the measured overlay height between frames; the bottom
// corners need it to place the panel and Shirei answers geometry queries from
// the previous frame.
var lastPanelH float32

// Charts and tables are both indexed lists under the same parent, so their
// keys need separate types — a bare 0 from each would collide.
type (
	chartKey int
	tableKey int
)

// drawChart paints one sparkline: a header row (title and the live value)
// above a rasterized plot. The plot is an image because Shirei draws boxes,
// not polylines — see chart.go.
func drawChart(idx int, c *Chart, width float32) {
	if c.Value == nil {
		return
	}
	th := &Config.Theme
	col := th.Series[idx%len(th.Series)]

	ContainerWithKey(chartKey(idx), Attrs(FixWidth(width), Gap(1)), func() {
		Container(Attrs(Row, FixWidth(width), CrossMid), func() {
			Label(c.Title, small(th.Dim))
			Element(Attrs(Grow(1)))
			Label(perfcore.ChartLabel(c), small(col))
		})
		plot(idx, c, width, Config.ChartHeight, col)
	})
}

// drawStats renders every registered source of key/value rows.
func drawStats(width float32) {
	th := &Config.Theme
	rows := perfcore.AllStats()
	if len(rows) == 0 {
		return
	}
	Container(Attrs(FixWidth(width), Gap(1), Pad4(4, 0, 0, 0)), func() {
		for i, r := range rows {
			i, r := i, r
			ContainerWithKey(i, Attrs(Row, FixWidth(width), CrossMid), func() {
				Label(r.Key, small(th.Dim))
				Element(Attrs(Grow(1)))
				Label(r.Value, small(th.Text))
			})
		}
	})
}

// drawTable renders one name/number list. It always draws Limit rows: a list
// that shrinks for a frame would otherwise resize the whole panel.
func drawTable(idx int, t *Table, width float32) {
	th := &Config.Theme
	vw := Config.FontSize * 6
	var rows []TableRow
	if t.Rows != nil {
		rows = t.Rows()
	}
	n := t.Limit
	if n <= 0 {
		n = len(rows)
	}
	budget := nameBudget(width - vw - 6)

	ContainerWithKey(tableKey(idx), Attrs(FixWidth(width), Gap(1), Pad4(5, 0, 0, 0)), func() {
		Container(Attrs(Row, FixWidth(width), CrossMid), func() {
			Label(t.Title, small(th.Faint))
			Element(Attrs(Grow(1)))
			Label(t.Header, small(th.Faint))
		})
		clickable := t.Detail != nil
		for i := 0; i < n; i++ {
			i := i
			ContainerWithKey(i, Attrs(Row, FixWidth(width), CrossMid, Gap(6), Corners(3)), func() {
				if i >= len(rows) {
					// Hold the line's height so the reserved space is real
					// even before the first scan lands.
					Label(" ", small(th.Faint))
					return
				}
				if clickable {
					if IsHovered() {
						ModAttrs(BackgroundVec(th.RowHover))
					}
					if PressAction() {
						openDetail(detailKey(idx, rows[i].Name), rows[i].Name, t.Detail(i))
					}
				}
				Container(Attrs(Row, FixWidth(vw), MainAlign(AlignEnd)), func() {
					Label(rows[i].Value, small(th.RowValue))
				})
				Label(elide(rows[i].Name, budget), small(th.RowName))
			})
		}
	})
}

// nameBudget is how many monospace characters fit in w logical pixels. The
// advance is measured rather than assumed, because which mono face Shirei
// lands on differs per platform and a guess that runs narrow spills the
// longest function name past the panel edge.
func nameBudget(w float32) int {
	n := int(w / monoAdvance())
	if n < 8 {
		return 8
	}
	return n
}

var monoAdv struct {
	size    float32
	advance float32
	height  float32
}

// measureMono shapes one glyph at the current font size, caching by size.
// Shirei caches the shaping itself, so this is cheap even on a miss.
func measureMono() {
	if monoAdv.size == Config.FontSize && monoAdv.advance > 0 {
		return
	}
	st := ShapeText("M", TextStyle(Fonts(Monospace...), FontSize(Config.FontSize)))
	if len(st.Lines) == 0 || st.Lines[0].Width <= 0 {
		monoAdv.advance, monoAdv.height = Config.FontSize*0.6, Config.FontSize*1.3
		return
	}
	monoAdv.size = Config.FontSize
	monoAdv.advance, monoAdv.height = st.Lines[0].Width, st.Lines[0].Height
}

// monoAdvance is the width of one character at the current font size.
func monoAdvance() float32 {
	measureMono()
	return monoAdv.advance
}

// monoLineHeight is the height of one line at the current font size.
func monoLineHeight() float32 {
	measureMono()
	return monoAdv.height
}

// small is the overlay's one text style: monospace at Config.FontSize, so the
// numeric columns line up.
func small(c Vec4) TextStyleFn {
	return ComposeTextStyles(Fonts(Monospace...), FontSize(Config.FontSize), TextColorVec(c))
}

// elide trims a long function name from the left, keeping the tail, which is
// where the package and method live.
func elide(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-max+1:])
}

func envOn(key string) bool {
	switch os.Getenv(key) {
	case "1", "true", "yes", "on", "TRUE", "YES", "ON":
		return true
	}
	return false
}
