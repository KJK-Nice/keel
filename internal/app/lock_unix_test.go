//go:build unix

package app

import (
	"strings"
	"testing"
	"time"
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

func TestLockDataDirWaitOutlastsAQuickRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		release()
	}()
	again, err := lockDataDirWait(dir, 2*time.Second)
	if err != nil {
		t.Fatalf("lock after the old daemon released it: %v", err)
	}
	again()
	held, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	if _, err := lockDataDirWait(dir, 150*time.Millisecond); err == nil {
		t.Fatal("lockDataDirWait succeeded while another daemon holds the lock")
	}
}
