package native

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"keel/internal/paths"
)

// CheckAutomationEnv is the run-time half of the automation guard. Only the test builds (tag keelautomation) call it,
// from an init in package main. It returns an error unless the automation socket (MYGO_AUTOMATION) is in a private
// directory and KEEL_DATA names a directory other than the default data directory, so a socket that can type into
// the app never meets real data. MyGo's plugin listens in its own init, before any main-package init, but it
// answers nothing until the app has launched, and the caller exits before that.
func CheckAutomationEnv() error {
	sock := os.Getenv("MYGO_AUTOMATION")
	if sock == "" {
		return nil
	}
	data := os.Getenv("KEEL_DATA")
	if data == "" {
		return errors.New("MYGO_AUTOMATION is set but KEEL_DATA is not: automation runs only against a scratch data directory")
	}
	if sameDir(data, defaultDataDir()) {
		return errors.New("MYGO_AUTOMATION is set and KEEL_DATA is the default data directory: automation runs only against a scratch data directory")
	}
	st, err := os.Stat(filepath.Dir(sock))
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		return fmt.Errorf("MYGO_AUTOMATION must be a path inside a directory with mode 0700")
	}
	return nil
}

// RemoveAutomationSocket removes the socket file the plugin made, for a process that exits through os.Exit
// (which skips the plugin's own cleanup). It touches nothing that is not a socket.
func RemoveAutomationSocket() {
	sock := os.Getenv("MYGO_AUTOMATION")
	if st, err := os.Lstat(sock); sock != "" && err == nil && st.Mode()&os.ModeSocket != 0 {
		os.Remove(sock)
	}
}

// defaultDataDir is what paths.DataDir gives with KEEL_DATA unset.
func defaultDataDir() string {
	old, had := os.LookupEnv("KEEL_DATA")
	os.Unsetenv("KEEL_DATA")
	d := paths.DataDir()
	if had {
		os.Setenv("KEEL_DATA", old)
	}
	return d
}

func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		ra, rb = filepath.Clean(a), filepath.Clean(b)
	}
	return ra == rb
}
