package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAFailedWorkspaceWriteIsTriedAgain(t *testing.T) {
	dir := t.TempDir()
	r := newRigIn(t, okBackend(), dir)
	r.click(t, "host api-1")
	missing := filepath.Join(dir, "gone")
	r.w.dataDir = missing
	r.w.persist()
	if err := r.w.flush(); err == nil {
		t.Fatal("a write into a missing directory did not fail")
	}
	if err := os.Mkdir(missing, 0o700); err != nil {
		t.Fatal(err)
	}
	r.w.persist()
	if err := r.w.flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(missing, savedFile)); err != nil {
		t.Errorf("the failed write was not tried again: %v", err)
	}
}

func TestSaveNowReportsAFailedWrite(t *testing.T) {
	dir := t.TempDir()
	r := newRigIn(t, okBackend(), dir)
	r.w.dataDir = filepath.Join(dir, "gone")
	if err := r.w.saveNow(); err == nil {
		t.Error("a failed write was not reported")
	}
}
