package perfcore

import (
	"math"
	"runtime/metrics"
	"sort"
	"time"
)

// Sample is one frame's measurements. The counters the Go runtime reports as
// running totals (allocation, GC pause) are stored here as per-frame deltas;
// the rest are levels.
//
// Only runtime numbers are built in. Anything a particular UI framework or
// app can report — layout time, draw calls, queue depth — arrives through
// User, filled from Config.Probes.
type Sample struct {
	T       float64 // seconds since the package was first used
	FPS     float64 // smoothed, from the frame interval
	FrameMs float64 // wall time since the previous sample

	AllocKB float64 // heap allocated during the frame
	PauseMs float64 // GC pause time that landed in the frame
	HeapMB  float64 // live heap (objects)

	Goroutines uint64
	GCCycles   uint64

	// User holds the Config.Probes values for this frame, in registration
	// order. Read it with ProbeValue. nil when nothing registered a probe.
	User []float64
}

var (
	// hist is the chart window; dump is the longer ring a hitch record
	// quotes from.
	hist []Sample
	dump []Sample

	// prev holds the previous frame's cumulative counters, so the next
	// sample can be a delta.
	prev struct {
		inited bool
		allocs uint64
		pauseS float64
		t      float64
	}

	emaFrameSec float64 // exponentially smoothed frame interval, for FPS

	// samples is reused across frames; metrics.Read fills it in place.
	samples = []metrics.Sample{
		{Name: "/gc/heap/allocs:bytes"},
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/sched/pauses/total/gc:seconds"},
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/sched/goroutines:goroutines"},
	}
)

// dumpSecs is how much history a hitch record can quote.
const dumpSecs = 30.0

// Collect reads the runtime counters and the registered probes, appends this
// frame's sample, and runs the hitch detector. Call it once per frame,
// whether or not the panel is being drawn — the hitch recorder is worth
// keeping on with the overlay hidden.
func Collect() Sample {
	metrics.Read(samples)
	allocs := samples[0].Value.Uint64()
	gcs := samples[1].Value.Uint64()
	pauseS := histTotalSeconds(samples[2].Value.Float64Histogram())
	heap := samples[3].Value.Uint64()
	now := nowSecs()

	s := Sample{
		T:          now,
		HeapMB:     float64(heap) / (1024 * 1024),
		Goroutines: samples[4].Value.Uint64(),
		GCCycles:   gcs,
	}
	if prev.inited {
		s.AllocKB = float64(allocs-prev.allocs) / 1024
		s.PauseMs = (pauseS - prev.pauseS) * 1000
		s.FrameMs = (now - prev.t) * 1000
	}
	prev.allocs, prev.pauseS, prev.t, prev.inited = allocs, pauseS, now, true

	// Smooth the frame interval before turning it into a rate: 1/dt on a raw
	// interval swings wildly, and a rate is what the eye reads as "is it
	// keeping up". A long gap (the app idled) is ignored rather than dragging
	// the average to zero.
	if dt := s.FrameMs / 1000; dt > 0 && dt < 0.5 {
		if emaFrameSec == 0 {
			emaFrameSec = dt
		} else {
			emaFrameSec += (dt - emaFrameSec) * 0.1
		}
	}
	if emaFrameSec > 0 {
		s.FPS = 1 / emaFrameSec
	}

	if n := len(Config.Probes); n > 0 {
		s.User = make([]float64, n)
		for i := range Config.Probes {
			if read := Config.Probes[i].Read; read != nil {
				s.User[i] = read()
			}
		}
	}

	hist = appendWindow(hist, s, now, Config.WindowSecs)
	dump = appendWindow(dump, s, now, dumpSecs)
	scanAllocs(now)
	checkHitch(s)
	return s
}

// History is the samples inside the chart window, oldest first.
func History() []Sample { return hist }

// SeedHistory replaces the sampled state with a fabricated one. It exists for
// tests and for rendering documentation screenshots of a panel without a
// running app; nothing in normal operation calls it.
func SeedHistory(samples []Sample, allocs []AllocEntry) {
	hist = append(hist[:0], samples...)
	topAllocs = allocs
}

// Latest is the most recent sample, or the zero value before the first one.
func Latest() Sample {
	if n := len(hist); n > 0 {
		return hist[n-1]
	}
	return Sample{}
}

// appendWindow adds s and drops everything older than window seconds.
func appendWindow(ring []Sample, s Sample, now, window float64) []Sample {
	ring = append(ring, s)
	cut := 0
	for cut < len(ring) && ring[cut].T < now-window {
		cut++
	}
	return ring[cut:]
}

// percentileScratch is reused so checkHitch can ask for the median every
// frame without allocating a 5-second slice each time (at 1500 FPS that was
// the overlay's top allocator).
var percentileScratch []float64

// Percentile returns the p-th percentile (0..1) of the frame times currently
// in the chart window, by nearest rank.
func Percentile(p float64) float64 {
	n := 0
	for i := range hist {
		if hist[i].FrameMs > 0 {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	if cap(percentileScratch) < n {
		percentileScratch = make([]float64, n)
	} else {
		percentileScratch = percentileScratch[:n]
	}
	j := 0
	for i := range hist {
		if hist[i].FrameMs > 0 {
			percentileScratch[j] = hist[i].FrameMs
			j++
		}
	}
	sort.Float64s(percentileScratch)
	i := int(p*float64(n)+0.5) - 1
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	return percentileScratch[i]
}

// histTotalSeconds approximates a pause histogram's cumulative total as
// sum(count × bucket midpoint). The ±Inf edge buckets are clamped to their
// finite neighbour so the tails stay finite.
func histTotalSeconds(h *metrics.Float64Histogram) float64 {
	if h == nil {
		return 0
	}
	total := 0.0
	for i, count := range h.Counts {
		if count == 0 {
			continue
		}
		lo, hi := h.Buckets[i], h.Buckets[i+1]
		if math.IsInf(lo, -1) {
			lo = hi
		}
		if math.IsInf(hi, 1) {
			hi = lo
		}
		total += float64(count) * (lo + hi) / 2
	}
	return total
}

// nowSecs is the overlay's own monotonic clock, independent of the host's.
var start = time.Now()

func nowSecs() float64 { return time.Since(start).Seconds() }

// Now is the clock the samples are stamped with, for a host that wants to
// pace its own work (repainting the panel, say) on the same timeline.
func Now() float64 { return nowSecs() }
