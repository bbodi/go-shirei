package perfcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// steadyHistory fills the window with frames of ms milliseconds, so the
// detector has a median to compare against.
func steadyHistory(n int, ms float64) {
	hist = hist[:0]
	dump = dump[:0]
	for i := 0; i < n; i++ {
		s := Sample{T: float64(i) * ms / 1000, FrameMs: ms}
		hist = append(hist, s)
		dump = append(dump, s)
	}
}

// waitForWriter blocks until every background hitch writer has finished.
func waitForWriter() { pending.Wait() }

func TestHitchRecorded(t *testing.T) {
	log := filepath.Join(t.TempDir(), "test-hitches.log")
	prev := Config.HitchLog
	Config.HitchLog = log
	defer func() { Config.HitchLog = prev }()

	steadyHistory(120, 8)
	lastHitch = time.Time{}
	topAllocs = []AllocEntry{{Name: "pkg.Slow", KB: 128}}

	checkHitch(Sample{T: 1, FrameMs: 140, User: []float64{7}})
	waitForWriter()

	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("no hitch log written: %v", err)
	}
	text := string(body)
	for _, want := range []string{"HITCH", "frame=140.0 ms", "pkg.Slow", "goroutines:"} {
		if !strings.Contains(text, want) {
			t.Errorf("hitch log is missing %q\n%s", want, text)
		}
	}
}

// A record quotes every registered probe, so a renderer's numbers (layout
// time, draw calls) land in the log without perfcore knowing about them.
func TestHitchRecordQuotesProbes(t *testing.T) {
	log := filepath.Join(t.TempDir(), "probes.log")
	prevLog, prevProbes := Config.HitchLog, Config.Probes
	Config.HitchLog = log
	Config.Probes = []Probe{{Name: "layout ms", Read: func() float64 { return 0 }}}
	defer func() { Config.HitchLog, Config.Probes = prevLog, prevProbes }()

	steadyHistory(120, 8)
	lastHitch = time.Time{}

	checkHitch(Sample{T: 1, FrameMs: 140, User: []float64{123.5}})
	waitForWriter()

	body, _ := os.ReadFile(log)
	if !strings.Contains(string(body), "layout ms=123.50") {
		t.Errorf("the record does not quote the probe:\n%s", body)
	}
}

func TestHitchIgnoresNormalFrames(t *testing.T) {
	log := filepath.Join(t.TempDir(), "quiet.log")
	prev := Config.HitchLog
	Config.HitchLog = log
	defer func() { Config.HitchLog = prev }()

	// A 60 fps app: 16ms frames are the baseline, and 20ms is neither 2.5x
	// the median nor past the 33ms floor.
	steadyHistory(120, 16)
	lastHitch = time.Time{}

	checkHitch(Sample{T: 1, FrameMs: 20})
	waitForWriter()

	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Error("a 20ms frame in a 16ms app was recorded as a hitch")
	}
}

func TestHitchIsRateLimited(t *testing.T) {
	log := filepath.Join(t.TempDir(), "burst.log")
	prev := Config.HitchLog
	Config.HitchLog = log
	defer func() { Config.HitchLog = prev }()

	steadyHistory(120, 8)
	lastHitch = time.Time{}

	for i := 0; i < 5; i++ {
		checkHitch(Sample{T: float64(i), FrameMs: 200})
	}
	waitForWriter()

	body, _ := os.ReadFile(log)
	if n := strings.Count(string(body), "HITCH "); n != 1 {
		t.Errorf("five hitches in a row wrote %d records, want 1", n)
	}
}

// A slow app should still learn about its own outliers: the threshold is
// relative to the app's median, not to a fixed frame budget.
func TestHitchThresholdIsRelative(t *testing.T) {
	log := filepath.Join(t.TempDir(), "slow.log")
	prev := Config.HitchLog
	Config.HitchLog = log
	defer func() { Config.HitchLog = prev }()

	steadyHistory(120, 40) // a 25 fps app
	lastHitch = time.Time{}

	checkHitch(Sample{T: 1, FrameMs: 60}) // past the 33ms floor, but normal here
	waitForWriter()
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Error("60ms in a 40ms app should not count as a hitch")
	}

	checkHitch(Sample{T: 2, FrameMs: 400})
	waitForWriter()
	if _, err := os.Stat(log); err != nil {
		t.Error("400ms in a 40ms app should count as a hitch")
	}
}
