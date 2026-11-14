package app

import (
	"os"

	"keel/internal/authtoken"
)

// serveSteps are the calls that come before any window, in this order. Fakes in tests.
type serveSteps struct {
	mkdir  func(dir string, perm os.FileMode) error
	token  func(dataDir string) (string, error)
	ensure func(addr, dataDir string) error
	verify func(addr, token string) error
}

// PrepareServe makes the data directory (0700), loads or creates the token, starts keel serve if it is not
// running and checks that it answers to that token. The window starts here.
func PrepareServe(addr, dataDir string) (token string, err error) {
	return serveSteps{os.MkdirAll, authtoken.LoadOrCreate, ensureServe, verifyServe}.prepare(addr, dataDir)
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
