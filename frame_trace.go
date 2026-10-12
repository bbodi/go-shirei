package shirei

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// SHIREI_TRACE_FRAME=1 prints once a second why frames were requested:
// RequestNextFrame call sites, surface-hash changes, and unconsumed commands.
// Use this instead of a breakpoint — a 60fps wake is too noisy to step.
var traceFrame = os.Getenv("SHIREI_TRACE_FRAME") != ""

type frameTrace struct {
	mu      sync.Mutex
	start   time.Time
	frames  int
	wakes   int
	req     int
	hash    int
	cmd     int
	callers map[string]int
	cmds    map[string]int
}

var ftrace = frameTrace{callers: map[string]int{}, cmds: map[string]int{}}

func traceRequestNextFrame() {
	if !traceFrame {
		return
	}
	pc := make([]uintptr, 12)
	n := runtime.Callers(3, pc) // skip Callers, traceRequestNextFrame, RequestNextFrame
	frames := runtime.CallersFrames(pc[:n])
	var site string
	for {
		f, more := frames.Next()
		if !strings.Contains(f.Function, "shirei.RequestNextFrame") &&
			!strings.Contains(f.Function, "shirei.traceRequestNextFrame") {
			fn := f.Function
			if i := strings.LastIndex(fn, "/"); i >= 0 {
				fn = fn[i+1:]
			}
			site = fmt.Sprintf("%s:%d", fn, f.Line)
			break
		}
		if !more {
			break
		}
	}
	if site == "" {
		site = "?"
	}
	ftrace.mu.Lock()
	ftrace.callers[site]++
	ftrace.mu.Unlock()
}

func traceFrameWake(hashChange, anyReq, pending bool) {
	if !traceFrame {
		return
	}
	ftrace.mu.Lock()
	defer ftrace.mu.Unlock()
	if ftrace.start.IsZero() {
		ftrace.start = time.Now()
	}
	ftrace.frames++
	if hashChange || anyReq || pending {
		ftrace.wakes++
	}
	if anyReq {
		ftrace.req++
	}
	if hashChange {
		ftrace.hash++
	}
	if pending {
		ftrace.cmd++
		for k, cmd := range ui.pendingCommands {
			if cmd.postFrame == ui.FrameNumber {
				ftrace.cmds[fmt.Sprintf("%s/%s", k.widget, k.name)]++
			}
		}
	}
	if time.Since(ftrace.start) < time.Second {
		return
	}
	fmt.Fprintf(os.Stderr, "[frame] %d frames %d wakes | RequestNextFrame=%d hash=%d cmd=%d\n",
		ftrace.frames, ftrace.wakes, ftrace.req, ftrace.hash, ftrace.cmd)
	for site, n := range ftrace.callers {
		fmt.Fprintf(os.Stderr, "       RequestNextFrame %s ×%d\n", site, n)
	}
	for c, n := range ftrace.cmds {
		fmt.Fprintf(os.Stderr, "       pending %s ×%d\n", c, n)
	}
	ftrace.start = time.Now()
	ftrace.frames = 0
	ftrace.wakes = 0
	ftrace.req = 0
	ftrace.hash = 0
	ftrace.cmd = 0
	ftrace.callers = map[string]int{}
	ftrace.cmds = map[string]int{}
}
