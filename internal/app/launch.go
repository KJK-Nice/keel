package app

import (
	"fmt"
	"os"

	"keel/internal/authtoken"
)

// UIEnv selects the window of keel app: "native" (the default) or "webview" (Wails). --webview wins over it.
const UIEnv = "KEEL_UI"

// WantWebview reports whether keel app opens the Wails window: --webview in args, else KEEL_UI=webview. An unknown
// KEEL_UI is an error naming both values.
func WantWebview(args []string, env string) (bool, error) {
	for _, a := range args {
		if a == "--webview" {
			return true, nil
		}
	}
	switch env {
	case "", "native":
		return false, nil
	case "webview":
		return true, nil
	}
	return false, fmt.Errorf("%s=%q: want native or webview", UIEnv, env)
}

// serveSteps are the calls that come before any window, in this order. Fakes in tests.
type serveSteps struct {
	mkdir  func(dir string, perm os.FileMode) error
	token  func(dataDir string) (string, error)
	ensure func(addr, dataDir string) error
	verify func(addr, token string) error
}

var realSteps = serveSteps{os.MkdirAll, authtoken.LoadOrCreate, EnsureServe, VerifyServe}

// PrepareServe makes the data directory (0700), loads or creates the token, starts keel serve if it is not
// running and checks that it answers to that token. Both windows start here.
func PrepareServe(addr, dataDir string) (token string, err error) {
	return realSteps.prepare(addr, dataDir)
}

func (s serveSteps) prepare(addr, dataDir string) (string, error) {
	if err := s.mkdir(dataDir, 0o700); err != nil {
		return "", err
	}
	token, err := s.token(dataDir)
	if err != nil {
		return "", err
	}
	if err := s.ensure(addr, dataDir); err != nil {
		return "", err
	}
	if err := s.verify(addr, token); err != nil {
		return "", err
	}
	return token, nil
}
