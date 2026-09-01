package perfcore

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// stackDepth is how many program counters identify an allocation site. It is
// both the aggregation key and what the detail view can show, so it is deep
// enough to name the caller's caller rather than just the leaf.
const stackDepth = 16

// AllocEntry is one allocation site and what it allocated during the last
// scan window. Stacks carries the individual call stacks that were folded
// into it, which is what the detail view shows.
type AllocEntry struct {
	Name   string  // the function that allocated
	KB     float64 // bytes allocated since the previous scan, in KB
	Stacks []AllocStack
}

// AllocStack is one call stack under an AllocEntry.
type AllocStack struct {
	KB          float64 // allocated during the window
	Objects     int64   // objects allocated during the window
	LiveKB      float64 // still reachable at scan time (alloc − free)
	LiveObjects int64
	PCs         [stackDepth]uintptr
}

// Frame is one resolved level of an AllocStack.
type Frame struct {
	Function string
	File     string
	Line     int
}

// Frames resolves the stack to functions and source positions, innermost
// first. Inlined frames are expanded, so what you read matches the source
// rather than the machine code.
func (s AllocStack) Frames() []Frame {
	pcs := make([]uintptr, 0, stackDepth)
	for _, pc := range s.PCs {
		if pc == 0 {
			break
		}
		// step back to the call instruction; the recorded PC is the return
		// address, which can belong to the following line
		pcs = append(pcs, pc-1)
	}
	if len(pcs) == 0 {
		return nil
	}
	var out []Frame
	frames := runtime.CallersFrames(pcs)
	for {
		f, more := frames.Next()
		if f.Function != "" || f.File != "" {
			out = append(out, Frame{Function: f.Function, File: f.File, Line: f.Line})
		}
		if !more {
			break
		}
	}
	return out
}

// Report renders everything known about an allocation site as plain text:
// the totals, then each contributing call stack with its own numbers and
// resolved frames. This is what the detail view shows and what "copy" puts on
// the clipboard, so it has to stand on its own in a bug report.
func (e AllocEntry) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", e.Name)
	fmt.Fprintf(&b, "%s allocated in the last %.1fs scan window, over %d call stack(s)\n",
		FormatKB(e.KB), Config.AllocScanSecs, len(e.Stacks))
	fmt.Fprintf(&b, "heap profile sampled at 1 per %s (runtime.MemProfileRate);\n"+
		"large numbers are dependable, small ones are noise\n",
		FormatBytes(float64(runtime.MemProfileRate)))

	for i, s := range e.Stacks {
		fmt.Fprintf(&b, "\n--- stack %d/%d ---\n", i+1, len(e.Stacks))
		fmt.Fprintf(&b, "window: %s in %s objects   live: %s in %s objects\n",
			FormatKB(s.KB), FormatCount(float64(s.Objects)),
			FormatKB(s.LiveKB), FormatCount(float64(s.LiveObjects)))
		frames := s.Frames()
		if len(frames) == 0 {
			fmt.Fprintf(&b, "  (no symbols)\n")
		}
		for _, f := range frames {
			name := f.Function
			if name == "" {
				name = "(unknown)"
			}
			fmt.Fprintf(&b, "  %s\n", name)
			if f.File != "" {
				fmt.Fprintf(&b, "      %s:%d\n", f.File, f.Line)
			}
		}
	}
	return b.String()
}

// snapEntry is one call stack's cumulative profile at scan time.
type snapEntry struct {
	allocBytes, allocObjects int64
	freeBytes, freeObjects   int64
}

// allocSnap maps an allocation call stack to its cumulative profile. The key
// is the first stackDepth program counters, zero-padded, so two sites that
// share a leaf but differ higher up stay separate.
type allocSnap map[[stackDepth]uintptr]snapEntry

var (
	topAllocs     []AllocEntry
	prevSnap      allocSnap
	lastAllocScan float64
	// records is reused between scans; MemProfile grows it when it must.
	records []runtime.MemProfileRecord
)

// scanAllocs refreshes topAllocs at most every Config.AllocScanSecs.
// runtime.MemProfile walks every sampled allocation record, so this is far
// too expensive to do per frame.
func scanAllocs(now float64) {
	if now-lastAllocScan < Config.AllocScanSecs {
		return
	}
	lastAllocScan = now
	snap := takeAllocSnap()
	if prevSnap != nil {
		topAllocs = stickyTop(topAllocs, diffTopAllocs(prevSnap, snap, Config.TopAllocs))
	}
	prevSnap = snap
}

// stickyTop keeps the previous list when a scan window happens to catch no
// sampled allocation. MemProfile samples, so quiet windows are normal;
// blanking the table on one made it flicker between real data and
// "collecting samples" while nothing had actually changed.
func stickyTop(prev, next []AllocEntry) []AllocEntry {
	if len(next) == 0 {
		return prev
	}
	return next
}

// takeAllocSnap reads the heap profile and aggregates it by call stack. The
// profile is sampled at runtime.MemProfileRate, so small allocations are
// noisy and large ones are dependable.
func takeAllocSnap() allocSnap {
	n, _ := runtime.MemProfile(nil, true)
	if cap(records) < n+64 {
		records = make([]runtime.MemProfileRecord, n+64)
	}
	records = records[:cap(records)]
	for {
		n, ok := runtime.MemProfile(records, true)
		if ok {
			records = records[:n]
			break
		}
		records = make([]runtime.MemProfileRecord, n+64)
	}

	snap := make(allocSnap, len(records))
	for i := range records {
		rec := &records[i]
		if rec.AllocBytes <= 0 {
			continue
		}
		var key [stackDepth]uintptr
		pcs := rec.Stack()
		for j := 0; j < len(pcs) && j < stackDepth; j++ {
			key[j] = pcs[j]
		}
		e := snap[key]
		e.allocBytes += rec.AllocBytes
		e.allocObjects += rec.AllocObjects
		e.freeBytes += rec.FreeBytes
		e.freeObjects += rec.FreeObjects
		snap[key] = e
	}
	return snap
}

// diffTopAllocs returns the n sites that allocated most between two
// snapshots, aggregated by function name. Each entry keeps the stacks that
// were folded into it, heaviest first, so the detail view can show where the
// bytes actually came from.
func diffTopAllocs(prev, cur allocSnap, n int) []AllocEntry {
	byName := make(map[string]*AllocEntry)
	for stack, now := range cur {
		was := prev[stack]
		bytes := now.allocBytes - was.allocBytes
		if bytes <= 0 {
			continue
		}
		name := allocSiteName(stack)
		e := byName[name]
		if e == nil {
			e = &AllocEntry{Name: name}
			byName[name] = e
		}
		e.KB += float64(bytes) / 1024
		e.Stacks = append(e.Stacks, AllocStack{
			KB:          float64(bytes) / 1024,
			Objects:     now.allocObjects - was.allocObjects,
			LiveKB:      float64(now.allocBytes-now.freeBytes) / 1024,
			LiveObjects: now.allocObjects - now.freeObjects,
			PCs:         stack,
		})
	}

	entries := make([]AllocEntry, 0, len(byName))
	for _, e := range byName {
		sort.Slice(e.Stacks, func(i, j int) bool { return e.Stacks[i].KB > e.Stacks[j].KB })
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].KB > entries[j].KB })
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries
}

// allocSiteName names a stack by its topmost frame outside the runtime and
// the general-purpose standard library, which is the line an app author can
// act on. If every frame is internal it falls back to the first one.
func allocSiteName(stack [stackDepth]uintptr) string {
	pcs := make([]uintptr, 0, stackDepth)
	for _, pc := range stack {
		if pc == 0 {
			break
		}
		pcs = append(pcs, pc-1)
	}
	if len(pcs) == 0 {
		return "(unknown)"
	}

	frames := runtime.CallersFrames(pcs)
	var first string
	for {
		frame, more := frames.Next()
		if frame.Function != "" {
			if first == "" {
				first = frame.Function
			}
			if !isInternalFrame(frame.Function) {
				return frame.Function
			}
		}
		if !more {
			break
		}
	}
	if first != "" {
		return first
	}
	return "(unknown)"
}

// internalPrefixes are the packages that allocate on someone else's behalf.
// Naming a row "image.NewRGBA" tells you nothing — every RGBA in the process
// collapses into one line — so the search walks past these to the caller that
// asked for the memory.
var internalPrefixes = []string{
	"runtime.", "internal/", "sync.", "sort.", "reflect.",
	"image.", "image/draw.", "bytes.", "strings.",
}

func isInternalFrame(name string) bool {
	for _, p := range internalPrefixes {
		if len(name) >= len(p) && name[:len(p)] == p {
			return true
		}
	}
	return name == ""
}
