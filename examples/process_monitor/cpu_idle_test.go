package main

import (
	"os"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

// TestFrameHashSettles catches a 60fps paint loop: after the first layout
// pass, an idle watch (no new sample) must not keep requesting frames.
func TestFrameHashSettles(t *testing.T) {
	if !waitFontScan(15 * time.Second) {
		t.Fatal("system font scan did not finish in 15s")
	}
	setupIdleHost()
	sam := new(Sampler)
	sam.Sample()
	time.Sleep(200 * time.Millisecond)
	snap, err := sam.Sample()
	if err != nil {
		t.Fatal(err)
	}
	appData.snapshot = snap
	appData.store.Update(snap, nil)

	assertHashSettles(t, "list")
	if p := selfProcLocked(); p != nil {
		appData.selected = p
	}
	assertHashSettles(t, "selected")
}

func assertHashSettles(t *testing.T, label string) {
	t.Helper()
	var dirty int
	for i := 0; i < 6; i++ {
		out := RunFrameFn(RootView)
		// First two frames settle layout / virtual list width.
		if i > 2 && (out.FrameHasChanges || out.NextFrameRequested) {
			dirty++
		}
	}
	if dirty > 0 {
		t.Errorf("%s: %d late frames still dirty (would hold 60fps)", label, dirty)
	}
}

// TestIdleSelfCPUAfterSettle is the automated check for the spec constraint
// that this process stays under 10% CPU while the user is just watching.
//
// Startup is excluded: shirei's background system-font walk can spike CPU
// for a few seconds. We wait for SystemFontScanDone, then for two samples
// so our own CPU% is a real delta, then measure.
//
// The frame loop matches the live backend: produce a frame only when one
// is requested, capped at ~60Hz. If the UI fails to settle, this process
// will sit at tens of percent and fail.
func TestIdleSelfCPUAfterSettle(t *testing.T) {
	if testing.Short() {
		t.Skip("idle CPU needs a multi-second settle")
	}

	if !waitFontScan(15 * time.Second) {
		t.Fatal("system font scan did not finish in 15s")
	}

	setupIdleHost()
	// PID sort so the visible set is stable while the first icons load.
	appData.tableSort.Column = sortColumnIndex("pid")
	appData.tableSort.Desc = false

	stop := startTestSampler(t)
	defer stop()

	if !waitSelfSample(4 * time.Second) {
		t.Fatal("own process never appeared with a CPU reading")
	}

	// Past iconHold so visible rows actually enqueue, then drain the worker.
	driveFrames(iconHold + 200*time.Millisecond)
	if !waitIconsIdle(15 * time.Second) {
		t.Fatal("icon queue did not drain")
	}

	t.Run("list", func(t *testing.T) {
		assertSelfIdle(t)
	})

	WithFrameLock(func() {
		if p := selfProcLocked(); p != nil {
			appData.selected = p
			requestDetails(p)
		}
	})
	driveFrames(time.Second)
	t.Run("selected", func(t *testing.T) {
		assertSelfIdle(t)
	})

	WithFrameLock(func() {
		appData.tableSort.Column = sortColumnIndex("cpu")
		appData.tableSort.Desc = true
	})
	// New rows appear; iconHold delays extract so a churning CPU sort
	// stays under the budget. Measure before the hold expires.
	driveFrames(200 * time.Millisecond)
	t.Run("sort-cpu", func(t *testing.T) {
		assertSelfIdle(t)
	})
}

const idleCPULimit = 10.0

func setupIdleHost() {
	icons.reset()
	appData = &AppState{
		store:        NewProcessStore(),
		refreshEvery: time.Second,
	}
	appData.tableSort.Column = sortColumnIndex("cpu")
	appData.tableSort.Desc = cachedProcessColumns[appData.tableSort.Column].DefaultDesc

	host := GetHost()
	host.WindowSize = Vec2{1100, 700}
	host.WindowScale = 1
	host.GlyphCacheBudgetBytes = 16 << 20
	host.HeadlessRender = true
	GetInputState().MousePoint = Vec2{-1000, -1000}
}

func assertSelfIdle(t *testing.T) {
	t.Helper()
	var last []float64
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		driveFrames(time.Second)
		if p := selfProc(); p != nil && p.CPUPercent >= 0 {
			last = append(last, p.CPUPercent)
		}
	}
	if len(last) < 2 {
		t.Fatal("not enough self CPU samples after settle")
	}
	a, b := last[len(last)-2], last[len(last)-1]
	avg := (a + b) / 2
	// Average of two 1s windows: a single icon extract can push one
	// sample over the line without meaning the watch is hot.
	if avg >= idleCPULimit {
		t.Fatalf("own CPU after settle: %.1f%% then %.1f%% (avg %.1f%%, limit %.0f%%); frames=%d",
			a, b, avg, idleCPULimit, lastFrames)
	}
	t.Logf("own CPU after settle: %.1f%% then %.1f%% (avg %.1f%%, frames=%d)", a, b, avg, lastFrames)
}

func waitIconsIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if icons.idle() {
			return true
		}
		driveFrames(50 * time.Millisecond)
	}
	return false
}

func waitFontScan(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !SystemFontScanDone() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

func startTestSampler(t *testing.T) (stop func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		sam := new(Sampler)
		for {
			select {
			case <-done:
				return
			default:
			}
			var refresh time.Duration
			WithFrameLock(func() { refresh = appData.refreshEvery })
			started := time.Now()
			snap, err := sam.Sample()
			WithFrameLock(func() {
				appData.snapshot = snap
				appData.err = err
				appData.lastRefresh = time.Now()
				appData.store.Update(snap, appData.selected)
			})
			RequestNextFrame()
			if sleep := refresh - time.Since(started); sleep > 0 {
				timer := time.NewTimer(sleep)
				select {
				case <-done:
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
	}()
	return func() { close(done) }
}

func waitSelfSample(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		driveFrames(50 * time.Millisecond)
		p := selfProc()
		if p != nil && !p.MetricsUnknown && p.CPUPercent >= 0 {
			return true
		}
	}
	return false
}

func selfProc() *Process {
	var found *Process
	WithFrameLock(func() { found = selfProcLocked() })
	return found
}

func selfProcLocked() *Process {
	pid := os.Getpid()
	for _, p := range appData.store.Processes() {
		if p.PID == pid {
			return p
		}
	}
	return nil
}

var lastFrames int

// driveFrames produces frames the way a live backend does: only when a
// frame was requested, at most once per 16ms. Otherwise it sleeps.
func driveFrames(d time.Duration) {
	end := time.Now().Add(d)
	n := 0
	need := false
	for time.Now().Before(end) {
		if !FrameRequested() && !need {
			sleep := time.Until(end)
			if sleep > 16*time.Millisecond {
				sleep = 16 * time.Millisecond
			}
			if sleep > 0 {
				time.Sleep(sleep)
			}
			continue
		}
		t0 := time.Now()
		out := RunFrameFn(RootView)
		n++
		need = out.NextFrameRequested
		if wait := 16*time.Millisecond - time.Since(t0); wait > 0 && need {
			time.Sleep(wait)
		}
	}
	lastFrames = n
}
