package store

import (
	"path/filepath"
	"testing"
)

func TestHostRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.ListHosts()) != 1 {
		t.Fatalf("expected seeded local host, got %d", len(st.ListHosts()))
	}
	saved, err := st.UpsertHost(Host{
		Name:     "api",
		Group:    "prod",
		Address:  "10.0.0.8",
		Port:     22,
		Username: "ubuntu",
		AuthType: "password",
		Password: "secret",
		Color:    "#6ea8ff",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "secret" || got.Name != "api" {
		t.Fatalf("unexpected host: %+v", got)
	}

	got.Password = ""
	got.Name = "api-1"
	updated, err := st.UpsertHost(got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Password != "secret" {
		t.Fatal("empty password should keep the saved secret")
	}
	if updated.Name != "api-1" {
		t.Fatal("name should update")
	}

	if err := st.DeleteHost(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetHost(saved.ID); err != ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}

	st2, err := New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.ListHosts()) != 1 {
		t.Fatalf("reloaded store should still have local host, got %d", len(st2.ListHosts()))
	}
}

func TestKnownHostsTOFU(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:other"); err == nil {
		t.Fatal("expected mismatch")
	}
}
