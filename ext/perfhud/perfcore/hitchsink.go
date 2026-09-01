//go:build !js

package perfcore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/trace"
	"sort"
	"strings"
	"time"
)

// The parts of hitch recording that need a filesystem and the execution
// tracer. The js build replaces this file with no-ops, so the overlay and the
// detector still run in a browser — only the on-disk artefacts are missing.

var (
	recorder  *trace.FlightRecorder
	lastTrace time.Time
)

// startRecorder arms a rolling execution trace covering roughly the last five
// seconds. Dumping it after the fact is what makes a one-off hitch
// investigable — a tracer started in response to the hitch has already missed
// it.
func startRecorder() {
	recorder = trace.NewFlightRecorder(trace.FlightRecorderConfig{
		MinAge: 5 * time.Second,
	})
	if err := recorder.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "perfhud: flight recorder: %v\n", err)
		recorder = nil
	}
}

// dumpTrace writes the recorder's current window next to logPath and returns
// the file name, or "" when there is nothing to write. Snapshots are several
// megabytes, so they are rate-limited to one per ten seconds even when
// hitches come faster.
func dumpTrace(logPath string) string {
	if recorder == nil || !recorder.Enabled() {
		return ""
	}
	now := time.Now()
	if !lastTrace.IsZero() && now.Sub(lastTrace) < 10*time.Second {
		return ""
	}
	lastTrace = now

	base := strings.TrimSuffix(logPath, filepath.Ext(logPath))
	name := fmt.Sprintf("%s-%s.trace", base, now.Format("20060102-150405"))
	f, err := os.Create(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "perfhud: trace: %v\n", err)
		return ""
	}
	defer f.Close()
	if _, err := recorder.WriteTo(f); err != nil {
		fmt.Fprintf(os.Stderr, "perfhud: trace: %v\n", err)
		return ""
	}
	pruneTraces(base + "-*.trace")
	return name
}

// writeRecord appends one formatted record to the hitch log.
func writeRecord(logPath string, rec Record) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "perfhud: hitch log: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(rec.Format()); err != nil {
		fmt.Fprintf(os.Stderr, "perfhud: hitch log: %v\n", err)
	}
}

// pruneTraces keeps the Config.KeepTraces most recent traces. The timestamp
// in the name sorts chronologically, so a plain string sort is enough.
func pruneTraces(pattern string) {
	if Config.KeepTraces <= 0 {
		return
	}
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= Config.KeepTraces {
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, name := range matches[Config.KeepTraces:] {
		if err := os.Remove(name); err != nil {
			fmt.Fprintf(os.Stderr, "perfhud: prune %s: %v\n", name, err)
		}
	}
}
