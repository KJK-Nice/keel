package app

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/authtoken"
	"keel/internal/buildid"
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
	release, err := lockDataDirWait(dataDir, 3*time.Second)
	if err != nil {
		return err
	}
	defer release()
	st, err := store.New(filepath.Join(dataDir, "keel.json"))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	token, err := authtoken.LoadOrCreate(dataDir)
	if err != nil {
		return fmt.Errorf("auth token: %w", err)
	}
	rt := runtime.New(st, nil)
	hub := share.FromEnv(rt)
	logs := applog.New(filepath.Join(dataDir, "keel.log"))
	tn := tunnel.New(st, tunnel.DefaultFactory(st))
	// KEEL_AGENT_PROVIDER=fake is for end-to-end tests only: the agent then
	// answers from an in-process fake and nothing is sent anywhere.
	agentFactory, agentJudge, agentFake := service.FromEnv(os.Getenv)
	if agentFake {
		log.Printf("%s=fake: the agent panel and in-pane suggestions answer from built-in test fakes; nothing is sent to a provider", service.TestProviderEnv)
	}

	stop := make(chan struct{})
	var once sync.Once
	shutdown := func() { once.Do(func() { close(stop) }) }

	api := httpapi.New(st, rt, web, hub).WithBuild(buildid.Of(web)).WithAuth(token).WithTunnels(tn).WithLogs(logs).WithStop(shutdown).WithSSHDir(paths.SSHDir()).WithAgentProvider(agentFactory, agentFake).WithAgentJudge(agentJudge)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("keel could not bind %s (is it already running?): %w", addr, err)
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
	// After the server: no new detection can start, and the open
	// detection summaries reach keel.log.
	api.Close()
	removeOwnPid(dataDir)
	return nil
}

// lockDataDirWait retries lockDataDir for up to wait. A daemon that was
// just stopped answers "down" on its port a moment before it exits and
// drops the lock, so `keel serve stop && keel serve` (or the desktop app
// restarting serve) would otherwise fail on the lock.
func lockDataDirWait(dataDir string, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		release, err := lockDataDir(dataDir)
		if err == nil || time.Now().After(deadline) {
			return release, err
		}
		time.Sleep(50 * time.Millisecond)
	}
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
	// Old daemons without a token ignore the header; current ones require it.
	token, _ := authtoken.Read(dataDir)
	posted := postRuntimeStop(addr, token)
	if posted == nil && waitUntilDown(addr, stopWait) {
		return nil
	}
	livePid, up := healthPid(addr)
	if !up {
		if !listening(addr) {
			// Nothing holds the port. A pid file left by a crashed daemon may
			// now name an unrelated process, so drop it instead of signalling.
			_ = os.Remove(paths.PidFile(dataDir))
			return fmt.Errorf("keel serve is not running")
		}
		return fmt.Errorf("keel serve on %s is not responding; not signalling the pid file", addr)
	}
	pid, err := readPid(dataDir)
	if err != nil {
		if posted == nil {
			return fmt.Errorf("keel serve did not stop")
		}
		return fmt.Errorf("keel serve is running but has no pid file")
	}
	if livePid != pid {
		return fmt.Errorf("pid file names %d but keel serve on %s reports %d; not signalling", pid, addr, livePid)
	}
	if err := signalStop(pid); err != nil && ProbeHealth(addr) {
		return err
	}
	if waitUntilDown(addr, stopWait) {
		return nil
	}
	return fmt.Errorf("keel serve did not stop")
}

// healthPid asks the daemon on addr for its pid. up reports a 200 from
// /api/health; pid is 0 when the reply carries none.
func healthPid(addr string) (pid int, up bool) {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	res, err := client.Get(healthURL(addr))
	if err != nil {
		return 0, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return 0, false
	}
	var body struct {
		Pid int `json:"pid"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	return body.Pid, true
}

// listening reports whether anything accepts TCP connections on addr.
func listening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
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

// VerifyServe checks that the keel serve on addr holds token before a
// client hands the token to it. It sends a random nonce, never the token,
// and expects HMAC(token, nonce) back: another program that grabbed the
// port first cannot answer.
func VerifyServe(addr, token string) error {
	nonce, err := authtoken.NewNonce()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(healthURL(addr) + "?nonce=" + url.QueryEscape(nonce))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var body struct {
		Proof string `json:"proof"`
	}
	if res.StatusCode == http.StatusOK {
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	}
	if !authtoken.CheckProof(token, nonce, body.Proof) {
		return fmt.Errorf("the server on %s did not prove it is this user's keel serve "+
			"(an older keel serve, or another program on that port); run `keel serve stop` and try again", addr)
	}
	return nil
}

func postRuntimeStop(addr, token string) error {
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/api/runtime/stop", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(authtoken.Header, token)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Do(req)
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
