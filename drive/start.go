package drive

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

var (
	binMu  sync.Mutex
	bins   = map[string]string{}
	binErr = map[string]error{}
)

func buildPkg(t testing.TB, pkg string) string {
	t.Helper()
	abs, err := filepath.Abs(pkg)
	if err != nil {
		t.Fatal(err)
	}
	binMu.Lock()
	defer binMu.Unlock()
	if err, ok := binErr[abs]; ok {
		t.Fatal(err)
	}
	if bin, ok := bins[abs]; ok {
		return bin
	}
	dir, err := os.MkdirTemp("", "shirei-drive-"+filepath.Base(abs)+"-")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, filepath.Base(abs))
	cmd := exec.Command("go", "build", "-o", bin, abs)
	if out, err := cmd.CombinedOutput(); err != nil {
		binErr[abs] = err
		t.Logf("go build:\n%s", out)
		t.Fatal(err)
	}
	bins[abs] = bin
	return bin
}

// Start builds pkg (a main-package directory), launches it with
// SHIREI_DRIVE_PORT in the child environment, and returns that port.
// Extra args are forwarded as-is. After ping, Start waits 500ms for
// the OS to map the window. Cleanup sends quit and waits for the process.
func Start(t testing.TB, pkg string, extraArgs ...string) int {
	t.Helper()
	beginTrace(t.Name())
	t.Cleanup(endTrace)
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(buildPkg(t, pkg), extraArgs...)
	cmd.Env = append(os.Environ(), "SHIREI_DRIVE_PORT="+strconv.Itoa(port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = Quit(port)
		if cmd.Process == nil {
			return
		}
		done := make(chan struct{})
		go func() {
			_, _ = cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		if err := Ping(port); err == nil {
			time.Sleep(500 * time.Millisecond)
			return port
		}
		if time.Now().After(deadline) {
			t.Fatal("drive ping: no udp response")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
