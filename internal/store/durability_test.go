package store

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWritePrivateReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keel.json")
	if err := writePrivate(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "two" {
		t.Fatalf("content %q, want two", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("leftover files: %s", strings.Join(names, ", "))
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Fatalf("mode %o, want 600", perm)
		}
	}
}

// A stale temp file from an older build (fixed name, any mode) must not be
// reused: its permissions would carry over to keel.json.
func TestWritePrivateIgnoresStaleTemp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "keel.json")
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode %o, want 600", perm)
	}
}

func TestSaveRewritesVaultOnlyWhenSecretsChange(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(Host{
		Name: "box", Address: "10.0.0.1", Port: 22, Username: "root",
		AuthType: "password", Password: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(st.vaultPath)
	if err != nil {
		t.Fatal(err)
	}

	// No secret changes: the vault must not be re-encrypted (age output is
	// randomized, so any rewrite changes the bytes).
	if _, err := st.UpsertSnippet(Snippet{Name: "uptime", Command: "uptime\n"}); err != nil {
		t.Fatal(err)
	}
	if err := st.TrustHostKey("10.0.0.1:22", "SHA256:abc", false); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(st.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("vault rewritten by a save that changed no secret")
	}

	// A secret change must reach the vault.
	h.Password = "second"
	if _, err := st.UpsertHost(h); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(st.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, changed) {
		t.Fatal("password change did not rewrite the vault")
	}
	got, err := st.GetHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "second" {
		t.Fatalf("password = %q, want second", got.Password)
	}

	// And it must survive a reopen.
	reopened, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = reopened.GetHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "second" {
		t.Fatalf("after reopen password = %q, want second", got.Password)
	}
}

// A vault deleted while the daemon runs must be recreated by the next save,
// as every save did before saves started skipping unchanged vaults.
func TestSaveRecreatesMissingVault(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(Host{
		Name: "box", Address: "10.0.0.1", Port: 22, Username: "root",
		AuthType: "password", Password: "keep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.vaultPath); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSnippet(Snippet{Name: "uptime", Command: "uptime\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.vaultPath); err != nil {
		t.Fatalf("vault not recreated: %v", err)
	}
	reopened, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "keep" {
		t.Fatalf("password after reopen = %q", got.Password)
	}
}
