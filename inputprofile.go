package shirei

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"

	g "go.hasen.dev/generic"
)

// CPU profiling is process-wide. The drive commands own only the recording
// they start; a profiler started elsewhere is rejected by runtime/pprof.
var inputCPUProfile struct {
	sync.Mutex
	file    *os.File
	path    string
	cleanup sync.Once
}

func inputCmdProfileStart(path string) string {
	if path == "" {
		return "error: usage: profile_start <path>"
	}
	inputCPUProfile.Lock()
	defer inputCPUProfile.Unlock()
	if inputCPUProfile.file != nil {
		return "error: drive CPU profile already running"
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "error: " + err.Error()
	}
	// Keep incomplete profiles out of the final path so file watchers only
	// see a complete profile. The same directory keeps the rename atomic.
	f, err := os.CreateTemp(filepath.Dir(path), ".cpu-profile-*.tmp")
	if err != nil {
		return "error: " + err.Error()
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "error: " + err.Error()
	}
	inputCPUProfile.file = f
	inputCPUProfile.path = path
	inputCPUProfile.cleanup.Do(func() {
		g.AddExitCleanup(func() {
			inputCPUProfile.Lock()
			defer inputCPUProfile.Unlock()
			if inputCPUProfile.file != nil {
				if err := finishInputCPUProfile(); err != nil {
					fmt.Fprintln(os.Stderr, "profile:", err)
				}
			}
		})
	})
	return "ok"
}

func stopInputCPUProfile() error {
	inputCPUProfile.Lock()
	defer inputCPUProfile.Unlock()
	if inputCPUProfile.file == nil {
		return fmt.Errorf("no drive CPU profile running")
	}
	return finishInputCPUProfile()
}

// The caller holds inputCPUProfile's mutex, including during exit cleanup.
func finishInputCPUProfile() error {
	f, path := inputCPUProfile.file, inputCPUProfile.path
	pprof.StopCPUProfile() // Waits for the profile writer to finish.
	inputCPUProfile.file = nil
	inputCPUProfile.path = ""
	if err := f.Close(); err != nil {
		return fmt.Errorf("close profile %s: %w", f.Name(), err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish profile %s to %s: %w", f.Name(), path, err)
	}
	return nil
}
