package shirei

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// FrameTimings lives on *UI. RunFrameFn stamps produce; the backend stamps
// paint (Painted + PaintEnd; paint starts at ProduceEnd). When the tick is
// finished — after paint or a present-skip — the backend calls
// EmitFrameMetrics, which delivers the snapshot to the OnFrameMetrics
// callback if one is registered.
//
// SHIREI_PERF=1 registers DefaultFrameMetricsPrinter. Optional
// SHIREI_PERF_LOG=<path> also appends that line to a file (and enables perf
// if SHIREI_PERF is unset).

var (
	perfOn  bool
	perfOut io.Writer = os.Stderr
)

func init() {
	logPath := os.Getenv("SHIREI_PERF_LOG")
	perfOn = os.Getenv("SHIREI_PERF") != "" || logPath != ""
	if !perfOn {
		return
	}
	OnFrameMetrics(DefaultFrameMetricsPrinter)
	if logPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "[perf] mkdir for SHIREI_PERF_LOG %q: %v\n", logPath, err)
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perf] SHIREI_PERF_LOG open %q: %v\n", logPath, err)
		return
	}
	abs, _ := filepath.Abs(logPath)
	perfOut = io.MultiWriter(os.Stderr, f)
	fmt.Fprintf(os.Stderr, "[perf] logging to %s\n", abs)
	fmt.Fprintf(f, "[perf] session start\n")
}

func PerfEnabled() bool { return perfOn }

func PerfOut() io.Writer { return perfOut }

func PerfLog(format string, args ...any) {
	if perfOn {
		fmt.Fprintf(perfOut, format+"\n", args...)
	}
}

type FrameTimings struct {
	ProduceStart time.Time
	ProduceEnd   time.Time
	Painted      bool
	PaintEnd     time.Time // paint starts at ProduceEnd
}

var _frameMetricsCallback func(FrameTimings)

// OnFrameMetrics sets the per-frame callback and returns the previous one.
// Register before app.Run; the callback runs on the frame thread.
func OnFrameMetrics(fn func(FrameTimings)) func(FrameTimings) {
	prev := _frameMetricsCallback
	_frameMetricsCallback = fn
	return prev
}

// EmitFrameMetrics delivers the active UI's FrameTimings to the registered
// callback. Backends call this once the tick is done (painted or skipped).
func EmitFrameMetrics() {
	if _frameMetricsCallback != nil {
		_frameMetricsCallback(ui.FrameTimings)
	}
}

// DefaultFrameMetricsPrinter is the stock SHIREI_PERF policy: accumulate for
// a second, print one [perf] line, reset. Apps can register it themselves
// via OnFrameMetrics(DefaultFrameMetricsPrinter).
func DefaultFrameMetricsPrinter(t FrameTimings) {
	p := &defaultPrinter
	p.frames++
	if produce := t.ProduceEnd.Sub(t.ProduceStart); produce > 0 {
		p.produceNs += int64(produce)
		p.produceN++
	}
	if t.Painted {
		if paint := t.PaintEnd.Sub(t.ProduceEnd); paint > 0 {
			p.paintNs += int64(paint)
			p.paintN++
		}
	}

	now := time.Now()
	if p.start.IsZero() {
		p.start = now
		return
	}
	if now.Sub(p.start) < time.Second {
		return
	}
	var produceAvg, paintAvg float64
	if p.produceN > 0 {
		produceAvg = float64(p.produceNs/int64(p.produceN)) / 1e3
	}
	line := fmt.Sprintf("[perf] %d fps | produce %.0fµs", p.frames, produceAvg)
	if p.paintN > 0 {
		paintAvg = float64(p.paintNs/int64(p.paintN)) / 1e3
		line += fmt.Sprintf(" paint %.0fµs", paintAvg)
		if p.paintN < p.frames {
			line += fmt.Sprintf(" (n=%d)", p.paintN)
		}
	}
	fmt.Fprintln(perfOut, line)
	*p = perfPrinterState{start: now}
}

type perfPrinterState struct {
	frames    int
	produceNs int64
	produceN  int
	paintNs   int64
	paintN    int
	start     time.Time
}

var defaultPrinter perfPrinterState
