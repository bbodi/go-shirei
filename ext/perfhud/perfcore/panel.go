// Package perfcore is the framework-free half of a performance overlay: the
// per-frame sampling, the allocation profiler, the hitch recorder, the
// number formatting, and the sparkline rasterizer. It imports nothing but the
// standard library, so any Go UI can host it — the caller supplies the
// drawing.
//
// A host does three things:
//
//	perfcore.Config.HitchLog = "myapp-hitches.log" // optional
//	perfcore.Collect()                             // once per frame
//	// ...then paint Config.Charts, Config.Stats and Config.Tables
//
// Everything the panel shows is a list the host app can extend: AddChart,
// AddStats, AddTable and AddProbe. The numbers a particular framework can
// report (layout time, draw calls, cache hit rates) are not built in — a
// renderer registers those as probes, the same way an app registers its own.
//
// go.hasen.dev/shirei/ext/perfhud is the Shirei renderer over this package,
// and doubles as the worked example.
package perfcore

// Stat is one key/value row.
type Stat struct{ Key, Value string }

// TableRow is one entry of a Table: a number and what it belongs to.
type TableRow struct {
	Value string
	Name  string
}

// Chart is one sparkline. Colors belong to the renderer, which picks one per
// chart index.
type Chart struct {
	Title string
	// Value reads this chart's number out of a collected sample. For an
	// app-supplied number, register a probe and use ProbeValue.
	Value func(*Sample) float64
	// Format renders the live value for the header. nil means FormatNumber.
	Format func(float64) string
	// Floor is the smallest value the y axis will scale to. Without one, an
	// idle chart magnifies rounding noise into a dramatic-looking waveform.
	Floor float64
}

// Table is a list of name/number rows — the shape the top-allocator view
// uses, and the one to reuse for any other "what is costing me" list.
type Table struct {
	Title  string
	Header string // caption over the value column
	// Rows is called once per painted frame.
	Rows func() []TableRow
	// Limit is how many rows a renderer should reserve. Holding the height
	// keeps a list that briefly shortens from resizing the whole panel.
	Limit int
	// Detail, when set, makes the rows clickable: it returns the full text to
	// show for one row. A renderer opens that in a detail view. The row index
	// refers to the slice Rows returned on the frame of the click.
	Detail func(row int) string
}

// Probe is a number read once per frame and kept in the history, so a Chart
// can plot it over time. Register one with AddProbe.
type Probe struct {
	Name string
	Read func() float64
}

// Settings is the data half's configuration. Rendering settings — size,
// placement, fonts, colors — belong to the renderer.
type Settings struct {
	// WindowSecs is the rolling history the charts show.
	WindowSecs float64

	// AllocScanSecs is how often the top-allocator table is refreshed.
	// runtime.MemProfile walks every sampled allocation record, which is too
	// expensive to do per frame.
	AllocScanSecs float64
	// TopAllocs is how many allocation sites the built-in table lists. Keep
	// it equal to that table's Limit.
	TopAllocs int

	// HitchLog is the path of the hitch record file. Empty disables hitch
	// recording entirely (no file writes, no flight recorder). Execution
	// traces are written next to it as <name>-<timestamp>.trace.
	HitchLog string
	// HitchFloorMs is the shortest frame that can ever count as a hitch.
	HitchFloorMs float64
	// HitchFactor multiplies the rolling median frame time to get the
	// threshold; the larger of that and HitchFloorMs wins.
	HitchFactor float64
	// KeepTraces is how many trace files to retain; older ones are deleted.
	KeepTraces int

	// Context names what the app is currently showing. It goes into each
	// hitch record so a slow frame can be tied to a screen. Optional.
	Context func() string

	// Charts, Stats and Tables are the panel's three sections, top to bottom.
	Charts []Chart
	Stats  []func() []Stat
	Tables []Table
	// Probes are numbers sampled every frame for charts to plot and for hitch
	// records to quote.
	Probes []Probe
}

// Config is the live configuration.
var Config = Settings{
	WindowSecs:    5,
	AllocScanSecs: 0.5,
	TopAllocs:     5,
	HitchFloorMs:  33, // two missed 60Hz vsyncs
	HitchFactor:   2.5,
	KeepTraces:    5,
}

// The three content lists are filled here rather than in the literal above:
// AllocatorTable's detail text reads Config, and the compiler will not accept
// a variable whose initializer reaches itself.
func init() {
	Config.Charts = DefaultCharts()
	Config.Stats = []func() []Stat{BuiltinStats}
	Config.Tables = []Table{AllocatorTable()}
}

// DefaultCharts is the set every host can show, because every one of these
// numbers comes from the Go runtime rather than from a UI framework.
func DefaultCharts() []Chart {
	return []Chart{
		{Title: "fps", Floor: 60, Format: FormatNumber,
			Value: func(s *Sample) float64 { return s.FPS }},
		{Title: "frame", Floor: 20, Format: FormatMs,
			Value: func(s *Sample) float64 { return s.FrameMs }},
		{Title: "alloc/frame", Floor: 32, Format: FormatKB,
			Value: func(s *Sample) float64 { return s.AllocKB }},
		{Title: "gc pause", Floor: 2, Format: FormatMs,
			Value: func(s *Sample) float64 { return s.PauseMs }},
		{Title: "live heap", Floor: 8, Format: FormatMB,
			Value: func(s *Sample) float64 { return s.HeapMB }},
	}
}

// BuiltinStats is the default scalar section.
func BuiltinStats() []Stat {
	last := Latest()
	return []Stat{
		{"frame", FormatMs(last.FrameMs)},
		{"p95 / p99", FormatMs(Percentile(0.95)) + " / " + FormatMs(Percentile(0.99))},
		{"goroutines", FormatCount(float64(last.Goroutines))},
		{"gc cycles", FormatCount(float64(last.GCCycles))},
	}
}

// AllocatorTable is the built-in top-allocator view. Its rows are clickable:
// the detail text is the full call stack behind that site.
func AllocatorTable() Table {
	return Table{
		Title:  "top allocators",
		Header: "per scan",
		Limit:  5,
		Rows: func() []TableRow {
			out := make([]TableRow, 0, len(topAllocs))
			for _, a := range topAllocs {
				out = append(out, TableRow{Value: FormatKB(a.KB), Name: a.Name})
			}
			return out
		},
		Detail: func(row int) string {
			// topAllocs can be replaced by a scan between the click and this
			// call, so the index is checked rather than trusted.
			if row < 0 || row >= len(topAllocs) {
				return ""
			}
			return topAllocs[row].Report()
		},
	}
}

// AddChart appends a sparkline.
func AddChart(c Chart) { Config.Charts = append(Config.Charts, c) }

// AddStats appends a source of key/value rows.
func AddStats(fn func() []Stat) { Config.Stats = append(Config.Stats, fn) }

// AddTable appends a name/number list.
func AddTable(t Table) { Config.Tables = append(Config.Tables, t) }

// AddProbe registers a number to sample every frame and returns its index,
// for ProbeValue:
//
//	i := perfcore.AddProbe("queue", func() float64 { return float64(len(queue)) })
//	perfcore.AddChart(perfcore.Chart{
//		Title: "queue depth", Floor: 10,
//		Value: perfcore.ProbeValue(i), Format: perfcore.FormatCount,
//	})
//
// A chart can only plot history, and only probes are kept in the history —
// a chart that read an app variable directly would draw this frame's value
// across the whole window. Register probes before the first frame.
func AddProbe(name string, read func() float64) int {
	Config.Probes = append(Config.Probes, Probe{Name: name, Read: read})
	return len(Config.Probes) - 1
}

// ProbeValue reads probe i out of a sample.
func ProbeValue(i int) func(*Sample) float64 {
	return func(s *Sample) float64 {
		if i < 0 || i >= len(s.User) {
			return 0
		}
		return s.User[i]
	}
}

// ProbeNow returns probe i's value in the most recent sample, for a stat row
// that wants the number as text rather than as a chart.
func ProbeNow(i int) float64 {
	last := Latest()
	return ProbeValue(i)(&last)
}

// AllStats collects every registered stat source, in order.
func AllStats() []Stat {
	var out []Stat
	for _, fn := range Config.Stats {
		if fn != nil {
			out = append(out, fn()...)
		}
	}
	return out
}

// TopAllocators returns the most recent allocation scan, for a host that
// wants to present it its own way.
func TopAllocators() []AllocEntry { return topAllocs }

// ChartTop is the value at the top of c's y axis for the current history:
// the tallest sample or Floor, whichever is larger, plus headroom so the peak
// is not clipped by the edge.
func ChartTop(c *Chart) float64 {
	top := c.Floor
	if c.Value == nil {
		return top
	}
	for i := range hist {
		if v := c.Value(&hist[i]); v > top {
			top = v
		}
	}
	return top * 1.15
}

// ChartCurrent is c's value in the most recent sample.
func ChartCurrent(c *Chart) float64 {
	if c.Value == nil || len(hist) == 0 {
		return 0
	}
	return c.Value(&hist[len(hist)-1])
}

// ChartLabel is c's current value, formatted.
func ChartLabel(c *Chart) string {
	format := c.Format
	if format == nil {
		format = FormatNumber
	}
	return format(ChartCurrent(c))
}
