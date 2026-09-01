package app

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"keel/internal/applog"
	"keel/internal/httpapi"
	"keel/internal/paths"
	"keel/internal/runtime"
	"keel/internal/share"
	"keel/internal/store"
	"keel/internal/tunnel"
)

func RunServe(web fs.FS, addr, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	st, err := store.New(filepath.Join(dataDir, "keel.json"))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	rt := runtime.New(st, nil)
	hub := share.FromEnv(rt)
	logs := applog.New(filepath.Join(dataDir, "keel.log"))
	tn := tunnel.New(st, tunnel.DefaultFactory(st))

	stop := make(chan struct{})
	var once sync.Once
	shutdown := func() { once.Do(func() { close(stop) }) }

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(st, rt, web, hub).WithTunnels(tn).WithLogs(logs).WithStop(shutdown).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("Keel could not bind %s (is it already running?): %w", addr, err)
	}
	if err := writePid(dataDir); err != nil {
		log.Printf("pid file: %v", err)
	}
	base := "http://" + ln.Addr().String()
	log.Printf("Keel serve on %s — sessions keep running until keel serve stop", base)
	if hub.Enabled() {
		log.Printf("Internet share via %s", hub.URL())
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
			shutdown()
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-stop:
	}
	signal.Stop(sig)

	tn.StopAll()
	rt.KillAll()
	_ = httpSrv.Close()
	removeOwnPid(dataDir)
	return nil
}

func EnsureServe(addr, dataDir string) error {
	if ProbeHealth(addr) {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	if err := spawnServe(addr, dataDir); err != nil {
		return fmt.Errorf("start keel serve: %w", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ProbeHealth(addr) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("keel serve did not become ready on %s", addr)
}

var stopWait = 5 * time.Second

func StopServe(addr, dataDir string) error {
	posted := postRuntimeStop(addr)
	if posted == nil && waitUntilDown(addr, stopWait) {
		return nil
	}
	pid, err := readPid(dataDir)
	if err != nil {
		if ProbeHealth(addr) {
			if posted == nil {
				return fmt.Errorf("keel serve did not stop")
			}
			return fmt.Errorf("keel serve is running but has no pid file")
		}
		return fmt.Errorf("keel serve is not running")
	}
	if err := signalStop(pid); err != nil && ProbeHealth(addr) {
		return err
	}
	if waitUntilDown(addr, stopWait) {
		return nil
	}
	return fmt.Errorf("keel serve did not stop")
}

func ProbeHealth(addr string) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	res, err := client.Get(healthURL(addr))
	if err != nil {
		return false
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode == http.StatusOK
}

func healthURL(addr string) string {
	return "http://" + addr + "/api/health"
}

func postRuntimeStop(addr string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Post("http://"+addr+"/api/runtime/stop", "application/json", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("runtime stop: HTTP %d", res.StatusCode)
	}
	return nil
}

func waitUntilDown(addr string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !ProbeHealth(addr) {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return !ProbeHealth(addr)
}

func spawnServe(addr, dataDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dataDir, "keel-serve.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	env := append(os.Environ(), "KEEL_ADDR="+addr, "KEEL_DATA="+dataDir)
	return startDetached(exe, []string{"serve"}, env, f)
}

func writePid(dataDir string) error {
	return os.WriteFile(paths.PidFile(dataDir), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

func readPid(dataDir string) (int, error) {
	b, err := os.ReadFile(paths.PidFile(dataDir))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid file")
	}
	return pid, nil
}

func removeOwnPid(dataDir string) {
	pid, err := readPid(dataDir)
	if err != nil || pid != os.Getpid() {
		return
	}
	_ = os.Remove(paths.PidFile(dataDir))
}
