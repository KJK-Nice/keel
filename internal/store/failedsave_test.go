package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// failSaves makes every save of st fail until the returned func runs: a
// non-empty directory where keel.json was makes its rename fail.
func failSaves(t *testing.T, st *Store) (restore func()) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("rename over a directory")
	}
	orig, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(st.path, "block"), 0o700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.RemoveAll(st.path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(st.path, orig, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// A restore whose save fails leaves the notebook in Recently deleted, so
// it can be restored again.
func TestRestoreDeletedNotebookWithAFailedSave(t *testing.T) {
	st := testStore(t)
	nb, err := st.UpsertNotebook(Notebook{Name: "runbook", Cells: []NotebookCell{{Body: "text"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteNotebook(nb.ID, false); err != nil {
		t.Fatal(err)
	}
	del, err := st.DeletedNotebooks()
	if err != nil || len(del) != 1 {
		t.Fatalf("deleted = %+v %v", del, err)
	}
	restore := failSaves(t, st)
	if _, err := st.RestoreDeletedNotebook(del[0].ID); err == nil {
		t.Fatal("restore with a failing save succeeded")
	}
	if left, _ := st.DeletedNotebooks(); len(left) != 1 {
		t.Fatalf("Recently deleted after a failed restore = %+v", left)
	}
	if _, err := st.GetNotebook(nb.ID); err == nil {
		t.Fatal("the notebook is listed although its restore was not saved")
	}
	restore()
	back, err := st.RestoreDeletedNotebook(del[0].ID)
	if err != nil || nbText(back) != "text" {
		t.Fatalf("restore → %+v %v", back, err)
	}
	if left, _ := st.DeletedNotebooks(); len(left) != 0 {
		t.Fatalf("still in Recently deleted: %+v", left)
	}
}
