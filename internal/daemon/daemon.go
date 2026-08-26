package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func ClientBase(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		port = "47241"
		host = listenAddr
	}
	if isWildcard(host) {
		host = "127.0.0.1"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func isWildcard(host string) bool {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "", "0.0.0.0", "::", "*":
		return true
	default:
		return false
	}
}

func isLocalHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	return isWildcard(h) || h == "127.0.0.1" || h == "::1" || h == "localhost"
}

func RemoteURL() string {
	return strings.TrimRight(os.Getenv("KEEL_URL"), "/")
}

func Token() string {
	return strings.TrimSpace(os.Getenv("KEEL_TOKEN"))
}

func authHeaders() http.Header {
	h := make(http.Header)
	if t := Token(); t != "" {
		h.Set("Authorization", "Bearer "+t)
	}
	return h
}

func Healthy(base string) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/api/health", nil)
	if err != nil {
		return false
	}
	req.Header = authHeaders()
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	return body.Status == "ok"
}

func Ensure(listenAddr, dataDir string) (string, error) {
	if remote := RemoteURL(); remote != "" {
		if !Healthy(remote) {
			return "", fmt.Errorf("remote runtime not healthy at %s (set KEEL_TOKEN if the runtime requires it)", remote)
		}
		return remote, nil
	}
	base := ClientBase(listenAddr)
	if Healthy(base) {
		return base, nil
	}
	host, _, err := net.SplitHostPort(listenAddr)
	if err == nil && !isLocalHost(host) {
		return "", fmt.Errorf("runtime not reachable at %s; start it with keel server, or set KEEL_URL", base)
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "keel-server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe, "server")
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = daemonSysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return "", fmt.Errorf("start runtime: %w", err)
	}
	_ = os.WriteFile(filepath.Join(dataDir, "keel.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	_ = logFile.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if Healthy(base) {
			return base, nil
		}
		time.Sleep(80 * time.Millisecond)
	}
	return "", fmt.Errorf("runtime did not become healthy on %s; see %s", base, filepath.Join(dataDir, "keel-server.log"))
}
