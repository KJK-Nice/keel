//go:build unix

package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"keel/internal/paths"
)

// bystander starts an unrelated process and returns its pid. It stands in
// for whatever the OS hands a crashed daemon's pid to.
func bystander(t *testing.T) int {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func writePidFile(t *testing.T, dir string, pid int) {
	t.Helper()
	if err := os.WriteFile(paths.PidFile(dir), []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStopServeIgnoresStalePid(t *testing.T) {
	dir := t.TempDir()
	pid := bystander(t)
	writePidFile(t, dir, pid)

	err := StopServe("127.0.0.1:1", dir)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("err = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if !alive(pid) {
		t.Fatal("StopServe signalled an unrelated process from a stale pid file")
	}
	if _, err := os.Stat(paths.PidFile(dir)); !os.IsNotExist(err) {
		t.Fatalf("stale pid file kept: %v", err)
	}
}

func TestStopServeRefusesMismatchedPid(t *testing.T) {
	prev := stopWait
	stopWait = 80 * time.Millisecond
	t.Cleanup(func() { stopWait = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","pid":2147483000}`))
		case "/api/runtime/stop":
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pid := bystander(t)
	writePidFile(t, dir, pid)

	err = StopServe(u.Host, dir)
	if err == nil || !strings.Contains(err.Error(), "not signalling") {
		t.Fatalf("err = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if !alive(pid) {
		t.Fatal("StopServe signalled a pid the running daemon does not report")
	}
}
