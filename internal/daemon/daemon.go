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
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		port = "47241"
	}
	return "http://127.0.0.1:" + port
}

func Healthy(base string) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	res, err := client.Get(strings.TrimRight(base, "/") + "/api/health")
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
	base := ClientBase(listenAddr)
	if Healthy(base) {
		return base, nil
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
