package store

import (
	"encoding/json"
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

// memoryOf is everything a failed save must leave as it was.
func memoryOf(t *testing.T, st *Store) string {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := json.Marshal(struct {
		D persistData
		S map[string]hostSecret
	}{st.data, st.secrets})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Each upsert or delete whose save fails leaves the store as it was: no
// host is listed that is not on disk, and a retry adds no second one.
func TestFailedSaveChangesNothing(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(Host{Name: "box", Address: "10.0.0.1", Username: "u", AuthType: AuthPassword, Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	sn, err := st.UpsertSnippet(Snippet{Name: "up", Command: "uptime\n"})
	if err != nil {
		t.Fatal(err)
	}
	in, err := st.UpsertIdentity(Identity{Name: "ops", Kind: "password", Username: "u", Password: "ipw"})
	if err != nil {
		t.Fatal(err)
	}
	h.IdentityID, h.AuthType, h.Password = in.ID, AuthIdentity, ""
	if h, err = st.UpsertHost(h); err != nil {
		t.Fatal(err)
	}
	tn, err := st.UpsertTunnel(Tunnel{Name: "pg", HostID: h.ID, Kind: TunnelLocal, BindPort: 5432, DestHost: "db", DestPort: 5432})
	if err != nil {
		t.Fatal(err)
	}
	g, err := st.UpsertGroup(Group{Name: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	nb, err := st.UpsertNotebook(Notebook{Name: "notes", Cells: []NotebookCell{{Body: "text"}}})
	if err != nil {
		t.Fatal(err)
	}
	hostUpdate := h
	hostUpdate.Name, hostUpdate.AuthType, hostUpdate.Password, hostUpdate.GroupID = "renamed", AuthPassword, "new-pw", g.ID
	nbUpdate := nb
	nbUpdate.Cells = []NotebookCell{{Body: "more"}}

	newHost := Host{Name: "n", Address: "10.0.0.2", Username: "u", AuthType: AuthPassword, Password: "x"}
	newTunnel := Tunnel{Name: "x", HostID: h.ID, Kind: TunnelDynamic, BindPort: 1080}
	errOf := func(_ any, err error) error { return err }

	restore := failSaves(t, st)
	for _, op := range []struct {
		name string
		do   func() error
	}{
		{"create host", func() error { return errOf(st.UpsertHost(newHost)) }},
		{"update host", func() error { return errOf(st.UpsertHost(hostUpdate)) }},
		{"delete host", func() error { return st.DeleteHost(h.ID) }},
		{"create snippet", func() error { return errOf(st.UpsertSnippet(Snippet{Name: "df", Command: "df\n"})) }},
		{"delete snippet", func() error { return st.DeleteSnippet(sn.ID) }},
		{"create identity", func() error { return errOf(st.UpsertIdentity(Identity{Name: "x", Kind: "password", Password: "y"})) }},
		{"delete identity", func() error { return st.DeleteIdentity(in.ID) }},
		{"create tunnel", func() error { return errOf(st.UpsertTunnel(newTunnel)) }},
		{"delete tunnel", func() error { return st.DeleteTunnel(tn.ID) }},
		{"create group", func() error { return errOf(st.UpsertGroup(Group{Name: "x"})) }},
		{"delete group", func() error { return st.DeleteGroup(g.ID) }},
		{"create notebook", func() error { return errOf(st.UpsertNotebook(Notebook{Name: "x"})) }},
		{"update notebook", func() error { return errOf(st.UpsertNotebook(nbUpdate)) }},
		{"agent notebook", func() error { return errOf(st.AgentCreateNotebook(Notebook{Name: "x"})) }},
		{"delete notebook", func() error { return st.DeleteNotebook(nb.ID, true) }},
	} {
		before := memoryOf(t, st)
		if err := op.do(); err == nil {
			t.Fatalf("%s: saved with keel.json unwritable", op.name)
		}
		if memoryOf(t, st) != before {
			t.Errorf("%s: a failed save left its change in memory", op.name)
		}
	}
	restore()
	reopened, err := New(st.path, TestVault())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.GetHost(h.ID); err != nil || got.Name != "box" {
		t.Fatalf("host after reopen: %+v %v", got, err)
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
