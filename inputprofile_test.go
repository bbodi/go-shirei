package shirei

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
)

// Exercise the command lifecycle with the real runtime profiler, including
// failed starts, ownership, deferred publication, and repeated recordings.
func TestInputCommandCPUProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recording with spaces.pprof")
	for _, cmd := range []string{"profile_start", "profile_stop", "profile_stop extra", "profile_start " + filepath.Join(dir, "missing", "cpu.pprof")} {
		if got := HandleInputCommand(cmd); !strings.HasPrefix(got, "error:") {
			t.Fatalf("%q: %q", cmd, got)
		}
	}
	for range 2 {
		if got := HandleInputCommand("profile_start " + path); got != "ok" {
			t.Fatal(got)
		}
		t.Cleanup(func() { _ = stopInputCPUProfile() })
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("profile visible before stop: %v", err)
		}
		if got := HandleInputCommand("profile_start " + filepath.Join(dir, "other.pprof")); !strings.HasPrefix(got, "error:") {
			t.Fatalf("second start: %q", got)
		}
		RunFrameFn(func() { Label("Profile command recording") })
		if got := HandleInputCommand("profile_stop"); got != "ok" {
			t.Fatal(got)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		z, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(z)
		z.Close()
		if err != nil || len(payload) == 0 {
			t.Fatalf("incomplete profile: %v (%d bytes)", err, len(payload))
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}

	// An unrelated recorder must survive failed drive start/stop commands.
	var external bytes.Buffer
	if err := pprof.StartCPUProfile(&external); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	if got := HandleInputCommand("profile_start " + path); !strings.HasPrefix(got, "error:") {
		t.Fatalf("external profiler collision: %q", got)
	}
	if got := HandleInputCommand("profile_stop"); !strings.HasPrefix(got, "error:") {
		t.Fatalf("stopping external profiler: %q", got)
	}
	var probe bytes.Buffer
	if err := pprof.StartCPUProfile(&probe); err == nil {
		t.Fatal("drive commands stopped the external profiler")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary profile files leaked: %v %v", entries, err)
	}
}
