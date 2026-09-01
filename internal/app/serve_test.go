package app

import (
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"keel/internal/paths"
)

func TestProbeHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !ProbeHealth(u.Host) {
		t.Fatal("expected healthy")
	}
	srv.Close()
	if ProbeHealth(u.Host) {
		t.Fatal("expected down after close")
	}
}

func TestWriteReadRemovePid(t *testing.T) {
	dir := t.TempDir()
	if err := writePid(dir); err != nil {
		t.Fatal(err)
	}
	pid, err := readPid(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}
	if paths.PidFile(dir) != filepath.Join(dir, "keel-serve.pid") {
		t.Fatal("pid path")
	}
	removeOwnPid(dir)
	if _, err := os.Stat(paths.PidFile(dir)); !os.IsNotExist(err) {
		t.Fatalf("pid file still there: %v", err)
	}
	if err := os.WriteFile(paths.PidFile(dir), []byte(strconv.Itoa(os.Getpid()+1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeOwnPid(dir)
	if _, err := os.Stat(paths.PidFile(dir)); err != nil {
		t.Fatal("should keep another process's pid file")
	}
}

func TestRunServeHealthAndStop(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	t.Setenv("KEEL_SIGNAL", "")
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	web := fs.FS(fstest.MapFS{"index.html": {Data: []byte("ok")}})
	done := make(chan error, 1)
	go func() { done <- RunServe(web, addr, dir) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ProbeHealth(addr) {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("RunServe exited early: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ProbeHealth(addr) {
		t.Fatal("serve never became healthy")
	}
	pid, err := readPid(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid file = %d, want %d", pid, os.Getpid())
	}
	if err := StopServe(addr, dir); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunServe did not exit after stop")
	}
	if ProbeHealth(addr) {
		t.Fatal("still healthy after stop")
	}
}
