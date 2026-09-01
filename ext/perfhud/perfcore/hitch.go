package perfcore

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// A hitch is a frame that took much longer than the app's own recent
// baseline. The detector writes one record per hitch: the runtime counters,
// every registered probe, the recent frame times, the top allocation sites
// and every goroutine's stack, plus an execution trace of the seconds leading
// up to it. That combination is usually enough to name the cause without
// reproducing it.

// NamedValue is one probe's reading inside a Record.
type NamedValue struct {
	Name  string
	Value float64
}

// Record is one hitch, as written to the log.
type Record struct {
	When      time.Time
	Sample    Sample
	Context   string       // what the app was showing (Config.Context)
	Probes    []NamedValue // every registered probe at the moment of the hitch
	Recent    []float64    // the frame times just before it, oldest first
	TopAllocs []AllocEntry // allocation sites in the last scan window
	TraceFile string       // "" when no trace was written
	Stacks    string       // runtime.Stack over all goroutines
}

var (
	lastHitch time.Time
	// dumpMu serializes the background writers so two hitches in a row cannot
	// interleave in the log.
	dumpMu sync.Mutex
	// pending tracks the background writers still in flight.
	pending sync.WaitGroup
	// recorderOnce starts the flight recorder on the first sampled frame,
	// so an app that never enables hitch logging never pays for it.
	recorderOnce sync.Once
)

// checkHitch decides whether s was a hitch and, if so, records it.
//
// The threshold is relative: max(HitchFloorMs, HitchFactor × the rolling
// median frame time). An app that runs at 8ms and an app that runs at 30ms
// both get told about their own outliers. Records are rate-limited to one
// per two seconds so a sustained slowdown does not fill the disk.
func checkHitch(s Sample) {
	log := Config.HitchLog
	if log == "" || s.FrameMs <= 0 {
		return
	}
	recorderOnce.Do(startRecorder)

	threshold := Percentile(0.5) * Config.HitchFactor
	if threshold < Config.HitchFloorMs {
		threshold = Config.HitchFloorMs
	}
	if s.FrameMs <= threshold || time.Since(lastHitch) < 2*time.Second {
		return
	}
	lastHitch = time.Now()

	rec := Record{
		When:      time.Now(),
		Sample:    s,
		Probes:    probeValues(s),
		Recent:    recentFrameMs(24),
		TopAllocs: append([]AllocEntry(nil), topAllocs...),
		// The goroutine dump stays synchronous: it has to catch the moment of
		// the hitch, and it costs a millisecond or two.
		Stacks: captureGoroutines(),
	}
	if Config.Context != nil {
		rec.Context = Config.Context()
	}

	// The heavy I/O runs off the frame loop. Writing a flight-recorder
	// snapshot is megabytes of work and blocks for hundreds of milliseconds —
	// doing it inline would cause a worse hitch than the one being recorded.
	// The destination is captured here, not read again in the goroutine: by
	// the time it runs, Config may name a different file or none at all.
	pending.Add(1)
	go func() {
		defer pending.Done()
		dumpMu.Lock()
		defer dumpMu.Unlock()
		rec.TraceFile = dumpTrace(log)
		writeRecord(log, rec)
	}()
}

// probeValues pairs each registered probe's name with its reading in s.
func probeValues(s Sample) []NamedValue {
	out := make([]NamedValue, 0, len(s.User))
	for i, v := range s.User {
		name := ""
		if i < len(Config.Probes) {
			name = Config.Probes[i].Name
		}
		out = append(out, NamedValue{Name: name, Value: v})
	}
	return out
}

// recentFrameMs returns up to n of the most recent frame times, oldest first.
func recentFrameMs(n int) []float64 {
	from := len(dump) - n
	if from < 0 {
		from = 0
	}
	out := make([]float64, 0, len(dump)-from)
	for i := from; i < len(dump); i++ {
		out = append(out, dump[i].FrameMs)
	}
	return out
}

// captureGoroutines dumps every goroutine's stack, growing the buffer until
// the whole dump fits or it reaches 4 MB.
func captureGoroutines() string {
	const maxSize = 4 << 20
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		if len(buf)*2 > maxSize {
			buf = make([]byte, maxSize)
			return string(buf[:runtime.Stack(buf, true)])
		}
		buf = make([]byte, len(buf)*2)
	}
}

// Format renders a record as the human-readable block that goes in the log.
func (rec Record) Format() []byte {
	var b bytes.Buffer
	s := rec.Sample
	fmt.Fprintf(&b, "================================================================\n")
	fmt.Fprintf(&b, "HITCH %s  frame=%.1f ms  context=%s\n",
		rec.When.Format("2006-01-02 15:04:05.000"), s.FrameMs, rec.Context)
	fmt.Fprintf(&b, "gc-pause=%.1f ms alloc=%.1f KB heap=%.1f MB goroutines=%d gc-cycles=%d\n",
		s.PauseMs, s.AllocKB, s.HeapMB, s.Goroutines, s.GCCycles)

	if len(rec.Probes) > 0 {
		fmt.Fprintf(&b, "probes:")
		for _, p := range rec.Probes {
			fmt.Fprintf(&b, "  %s=%.2f", p.Name, p.Value)
		}
		fmt.Fprintf(&b, "\n")
	}

	if rec.TraceFile == "" {
		fmt.Fprintf(&b, "trace: (not available)\n")
	} else {
		fmt.Fprintf(&b, "trace: %s\n", rec.TraceFile)
	}

	fmt.Fprintf(&b, "recent frames (ms, oldest first):\n ")
	for _, v := range rec.Recent {
		fmt.Fprintf(&b, " %.1f", v)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "top allocators (last scan window):\n")
	if len(rec.TopAllocs) == 0 {
		fmt.Fprintf(&b, "  (no allocation samples in window)\n")
	}
	for i, a := range rec.TopAllocs {
		fmt.Fprintf(&b, "  %d.  %.1f KB  %s\n", i+1, a.KB, a.Name)
	}

	fmt.Fprintf(&b, "goroutines:\n%s\n", rec.Stacks)
	fmt.Fprintf(&b, "================================================================\n")
	return b.Bytes()
}
