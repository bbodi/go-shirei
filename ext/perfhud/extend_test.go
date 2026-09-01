package perfhud

import (
	"testing"

	"go.hasen.dev/shirei/ext/perfhud/perfcore"
)

// A table always occupies Limit lines, so a shrinking list cannot resize the
// panel under the pointer.
func TestTableHeightIsStable(t *testing.T) {
	defer saveCore()()

	rows := []TableRow{{Value: "1 KB", Name: "a"}, {Value: "2 KB", Name: "b"}}
	Core.Tables = []Table{{
		Title: "test", Header: "x", Limit: 4,
		Rows: func() []TableRow { return rows },
	}}

	renderOverlay(t, 640, 640)
	full := lastPanelH

	rows = nil
	renderOverlay(t, 640, 640)
	if empty := lastPanelH; empty != full {
		t.Errorf("panel height moved from %v to %v when the table emptied", full, empty)
	}
}

// The three sections are extension points: adding to any of them must show
// up in the panel.
func TestExtensionPointsAreDrawn(t *testing.T) {
	defer saveCore()()

	renderOverlay(t, 640, 1200)
	base := lastPanelH

	AddStats(func() []Stat { return []Stat{{Key: "custom", Value: "1"}} })
	renderOverlay(t, 640, 1200)
	withStat := lastPanelH
	if withStat <= base {
		t.Errorf("AddStats did not grow the panel: %v -> %v", base, withStat)
	}

	AddTable(Table{Title: "custom", Header: "n", Limit: 2,
		Rows: func() []TableRow { return []TableRow{{Value: "9", Name: "z"}} }})
	renderOverlay(t, 640, 1200)
	withTable := lastPanelH
	if withTable <= withStat {
		t.Errorf("AddTable did not grow the panel: %v -> %v", withStat, withTable)
	}

	AddChart(Chart{Title: "custom", Floor: 1, Value: func(s *Sample) float64 { return 1 }})
	renderOverlay(t, 640, 1200)
	if withChart := lastPanelH; withChart <= withTable {
		t.Errorf("AddChart did not grow the panel: %v -> %v", withTable, withChart)
	}
}

// This renderer reports Shirei's own numbers as probes, the same mechanism an
// app uses; they must reach the stat rows.
func TestShireiProbesReachTheStats(t *testing.T) {
	defer saveCore()()

	perfcore.Collect()
	var keys []string
	for _, s := range ShireiStats() {
		keys = append(keys, s.Key)
	}
	want := []string{"layout / paint", "shape calls/hits", "images"}
	if len(keys) != len(want) {
		t.Fatalf("ShireiStats returned %v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("row %d is %q, want %q", i, keys[i], want[i])
		}
	}
}

// saveCore snapshots the data-half configuration so a test can mutate it
// freely; the returned function puts it back.
func saveCore() func() {
	saved := *Core
	return func() { *Core = saved }
}
