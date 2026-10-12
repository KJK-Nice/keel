//go:build unix

package app

import (
	"strings"
	"testing"
)

func TestLockDataDirIsExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockDataDir(dir); err == nil || !strings.Contains(err.Error(), "another keel serve") {
		t.Fatalf("second lock = %v, want another keel serve error", err)
	}
	release()
	again, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	again()
}
