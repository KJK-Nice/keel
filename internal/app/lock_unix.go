//go:build unix

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockDataDir takes an exclusive, non-blocking flock on dataDir's
// keel-serve.lock. The listening port only keeps two daemons off the same
// KEEL_ADDR; two daemons with different addresses but the same KEEL_DATA
// would otherwise overwrite each other's keel.json and keel.vault. The
// kernel drops the lock when the process exits, even on a crash.
func lockDataDir(dataDir string) (release func(), err error) {
	path := filepath.Join(dataDir, "keel-serve.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another keel serve is using %s", dataDir)
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
