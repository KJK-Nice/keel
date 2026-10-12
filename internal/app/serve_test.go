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
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"keel/internal/authtoken"
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
	token, err := authtoken.Read(dir)
	if err != nil {
		t.Fatalf("serve did not write its token: %v", err)
	}
	if err := VerifyServe(addr, token); err != nil {
		t.Fatalf("VerifyServe on our own serve: %v", err)
	}
	if err := postRuntimeStop(addr, ""); err == nil {
		t.Fatal("runtime stop without the token was accepted")
	}
	if !ProbeHealth(addr) {
		t.Fatal("unauthenticated stop brought serve down")
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

func TestStopServeFailsIfStillHealthy(t *testing.T) {
	prev := stopWait
	stopWait = 80 * time.Millisecond
	t.Cleanup(func() { stopWait = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
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
	err = StopServe(u.Host, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("err = %v", err)
	}
}

func TestStopServeAlreadyDown(t *testing.T) {
	err := StopServe("127.0.0.1:1", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("err = %v", err)
	}
}

func TestPostRuntimeStopSendsToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(authtoken.Header)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := postRuntimeStop(u.Host, "tok"); err != nil {
		t.Fatal(err)
	}
	if got != "tok" {
		t.Fatalf("%s = %q, want tok", authtoken.Header, got)
	}
}

func TestVerifyServeRejectsImpostor(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"no proof (older keel serve or another program)": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		},
		"proof for a different token": func(w http.ResponseWriter, r *http.Request) {
			proof := authtoken.Proof("someone-elses-token", r.URL.Query().Get("nonce"))
			_, _ = w.Write([]byte(`{"proof":"` + proof + `"}`))
		},
		"not healthy": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			u, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyServe(u.Host, "our-token"); err == nil {
				t.Fatal("VerifyServe trusted a server that does not hold the token")
			}
		})
	}
}

func TestVerifyServeNeverSendsToken(t *testing.T) {
	const token = "our-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), token) || r.Header.Get(authtoken.Header) != "" {
			t.Errorf("VerifyServe sent the token: %s", r.URL)
		}
		proof := authtoken.Proof(token, r.URL.Query().Get("nonce"))
		_, _ = w.Write([]byte(`{"proof":"` + proof + `"}`))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyServe(u.Host, token); err != nil {
		t.Fatal(err)
	}
}
