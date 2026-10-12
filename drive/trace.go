package drive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// EnvTrace is a directory. When set, Cmd appends JSONL (except ping) and
// Shot writes numbered PNGs plus a beat. Unset: Shot is a no-op.
const EnvTrace = "SHIREI_DRIVE_TRACE"

// EnvCmdLog enables the stderr UDP dump. It also needs go test -v
// (testing.Verbose). Tester does not set this; SHIREI_DRIVE_TRACE is the
// structured log.
const EnvCmdLog = "SHIREI_CMD_LOG"

// TraceEvent is one JSONL line under EnvTrace/<test>/trace.jsonl.
// Kind is "cmd" (UDP sent/recv), "shot" (PNG beat), or "comment".
type TraceEvent struct {
	Kind string `json:"kind"`
	T    string `json:"t"`
	Sent string `json:"sent,omitempty"`
	Recv string `json:"recv,omitempty"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

var (
	traceMu   sync.Mutex
	traceDir  string
	traceShot int
)

func beginTrace(testName string) {
	root := strings.TrimSpace(os.Getenv(EnvTrace))
	if root == "" || testName == "" {
		return
	}
	dir := filepath.Join(root, traceTestName(testName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	traceMu.Lock()
	traceDir = dir
	traceShot = 0
	traceMu.Unlock()
}

func endTrace() {
	traceMu.Lock()
	traceDir = ""
	traceShot = 0
	traceMu.Unlock()
}

func tracing() bool {
	traceMu.Lock()
	defer traceMu.Unlock()
	return traceDir != ""
}

func recordCmd(sent, recv string) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sent)), "ping") {
		return
	}
	appendTrace(TraceEvent{Kind: "cmd", Sent: sent, Recv: recv})
}

// Comment records a human-readable heading in the trace. No-op when
// SHIREI_DRIVE_TRACE is unset.
func Comment(text string) {
	if text == "" {
		return
	}
	appendTrace(TraceEvent{Kind: "comment", Name: text})
}

// Shot writes a named PNG of the last frame into the trace dir and records
// a beat. No-op (and nil) when SHIREI_DRIVE_TRACE is unset.
func Shot(port int, name string) error {
	traceMu.Lock()
	dir := traceDir
	if dir == "" {
		traceMu.Unlock()
		return nil
	}
	traceShot++
	n := traceShot
	traceMu.Unlock()

	if name == "" {
		name = "shot"
	}
	file := fmt.Sprintf("%04d-%s.png", n, traceSlug(name))
	path := filepath.Join(dir, file)
	if err := Screenshot(port, path); err != nil {
		return err
	}
	appendTrace(TraceEvent{Kind: "shot", Name: name, Path: path})
	return nil
}

func appendTrace(ev TraceEvent) {
	traceMu.Lock()
	dir := traceDir
	traceMu.Unlock()
	if dir == "" {
		return
	}
	ev.T = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	b = append(b, '\n')
	f, err := os.OpenFile(filepath.Join(dir, "trace.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(b)
	_ = f.Close()
}

// ReadTrace loads a trace.jsonl file. Missing file is an empty trace, not an error.
func ReadTrace(path string) ([]TraceEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []TraceEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev TraceEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return out, err
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

func traceTestName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	if name == "" {
		return "test"
	}
	return name
}

func traceSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "shot"
	}
	return s
}
