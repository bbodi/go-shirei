package perfcore

import "testing"

func TestPercentileReusesScratch(t *testing.T) {
	steadyHistory(240, 8)
	if got := Percentile(0.5); got != 8 {
		t.Fatalf("median = %g, want 8", got)
	}
	allocs := testing.AllocsPerRun(50, func() { Percentile(0.5) })
	if allocs != 0 {
		t.Errorf("Percentile allocated %.2f/run; checkHitch calls this every frame", allocs)
	}
}
