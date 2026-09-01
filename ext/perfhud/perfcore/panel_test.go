package perfcore

import "testing"

func TestStickyTopKeepsTheLastGoodScan(t *testing.T) {
	prev := []AllocEntry{{Name: "pkg.Hot", KB: 200}}

	if got := stickyTop(prev, nil); len(got) != 1 || got[0].Name != "pkg.Hot" {
		t.Errorf("a quiet scan window blanked the table: %v", got)
	}
	next := []AllocEntry{{Name: "pkg.Other", KB: 10}}
	if got := stickyTop(prev, next); got[0].Name != "pkg.Other" {
		t.Errorf("a scan with data did not replace the table: %v", got)
	}
}

// A probe's value has to reach the history, or a custom chart would draw this
// frame's number across the whole window.
func TestProbeReachesTheHistory(t *testing.T) {
	saved := Config
	defer func() { Config = saved }()

	value := 7.0
	read := ProbeValue(AddProbe("test", func() float64 { return value }))

	hist = hist[:0]
	Collect()
	value = 9
	Collect()

	if len(hist) < 2 {
		t.Fatalf("expected two samples, got %d", len(hist))
	}
	if got := read(&hist[len(hist)-2]); got != 7 {
		t.Errorf("first sample carries %v, want 7", got)
	}
	if got := read(&hist[len(hist)-1]); got != 9 {
		t.Errorf("second sample carries %v, want 9", got)
	}
}

// ChartTop never scales below Floor, so an idle chart cannot magnify rounding
// noise into a waveform.
func TestChartTopRespectsTheFloor(t *testing.T) {
	saved := Config
	defer func() { Config = saved }()

	hist = []Sample{{FrameMs: 0.4}, {FrameMs: 0.5}}
	c := Chart{Floor: 20, Value: func(s *Sample) float64 { return s.FrameMs }}
	if got := ChartTop(&c); got < 20 {
		t.Errorf("ChartTop = %v, want at least the floor of 20", got)
	}

	hist = []Sample{{FrameMs: 100}}
	if got := ChartTop(&c); got < 100 {
		t.Errorf("ChartTop = %v, want at least the peak of 100", got)
	}
}

// AllStats concatenates every registered source, so a renderer's rows and an
// app's rows both land in the panel.
func TestAllStatsConcatenates(t *testing.T) {
	saved := Config
	defer func() { Config = saved }()

	Config.Stats = nil
	AddStats(func() []Stat { return []Stat{{Key: "a"}} })
	AddStats(func() []Stat { return []Stat{{Key: "b"}, {Key: "c"}} })

	got := AllStats()
	if len(got) != 3 || got[0].Key != "a" || got[2].Key != "c" {
		t.Errorf("AllStats = %v", got)
	}
}
