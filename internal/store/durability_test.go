package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// Saves write with the store lock released; concurrent saves must still
// leave the newest snapshot on disk, so nothing a caller saved is lost.
func TestConcurrentSavesKeepEveryChange(t *testing.T) {
	st := testStore(t)
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := st.UpsertSnippet(Snippet{Name: fmt.Sprintf("s%02d", i), Command: "true\n"}); err != nil {
				errs <- err
			}
			if _, err := st.ListHosts(); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	reopened, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.ListSnippets()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := st.ListSnippets()
	if len(got) != len(want) || len(got) < n {
		t.Fatalf("after reopen %d snippets, in memory %d, want >= %d", len(got), len(want), n)
	}
}

// Concurrent host saves each change keel.vault; every password must be in
// the vault after reopen, whichever order the writes reached the disk.
func TestConcurrentHostSecretsSurviveReopen(t *testing.T) {
	st := testStore(t)
	const n = 12
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make(chan error, 2*n)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			h, err := st.UpsertHost(Host{
				Name: fmt.Sprintf("box%02d", i), Address: "10.0.0.1", Port: 22, Username: "root",
				AuthType: "password", Password: fmt.Sprintf("pw%02d", i),
			})
			if err != nil {
				errs <- err
				return
			}
			ids[i] = h.ID
		}(i)
		go func(i int) {
			defer wg.Done()
			if _, err := st.UpsertSnippet(Snippet{Name: fmt.Sprintf("s%02d", i), Command: "true\n"}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	reopened, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		got, err := reopened.GetHost(id)
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("pw%02d", i); got.Password != want {
			t.Fatalf("host %d password after reopen = %q, want %q", i, got.Password, want)
		}
	}
}

// A save that snapshots while an earlier vault write is still in flight
// must carry the secrets too: if the earlier write fails, the later save is
// the one that puts them on disk.
func TestSnapshotDuringVaultWriteCarriesSecrets(t *testing.T) {
	st := testStore(t)
	st.mu.Lock()
	st.secrets["extra"] = hostSecret{Token: "t"}
	first, err := st.snapshotLocked()
	if err != nil {
		st.mu.Unlock()
		t.Fatal(err)
	}
	second, err := st.snapshotLocked()
	st.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !first.vault || !second.vault {
		t.Fatalf("vault flags first=%v second=%v, want both", first.vault, second.vault)
	}
	// The newer snapshot lands first; the older one must not overwrite it.
	if wrote, err := st.writeSnapshot(second); err != nil || !wrote {
		t.Fatalf("second write: wrote=%v err=%v", wrote, err)
	}
	if wrote, err := st.writeSnapshot(first); err != nil || wrote {
		t.Fatalf("first write: wrote=%v err=%v, want skipped", wrote, err)
	}
}

// A failed keel.vault write must be retried by the next save, even one
// that changes no secret.
func TestFailedVaultWriteRetriedByNextSave(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename over a directory")
	}
	st := testStore(t)
	h, err := st.UpsertHost(Host{
		Name: "box", Address: "10.0.0.1", Port: 22, Username: "root",
		AuthType: "password", Password: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(st.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	// A non-empty directory where keel.vault was makes the rename fail.
	if err := os.Remove(st.vaultPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(st.vaultPath, "block"), 0o700); err != nil {
		t.Fatal(err)
	}
	h.Password = "second"
	if _, err := st.UpsertHost(h); err == nil {
		t.Fatal("save with an unwritable vault succeeded")
	}
	if err := os.RemoveAll(st.vaultPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.vaultPath, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSnippet(Snippet{Name: "uptime", Command: "uptime\n"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "second" {
		t.Fatalf("password after reopen = %q, want second", got.Password)
	}
}
