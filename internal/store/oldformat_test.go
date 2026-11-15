package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// addOldFormatHost rewrites keel.json as a build before the vault wrote it:
// one more host whose password is in the file, in the clear.
func addOldFormatHost(t *testing.T, path, id, password string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw["hosts"] = append(raw["hosts"].([]any), map[string]any{
		"id": id, "name": "old", "address": "10.0.0.9", "port": 22, "username": "u",
		"authType": "password", "password": password,
	})
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// An old keel.json restored beside an existing keel.vault must not let
// Setup or a wrong passphrase re-key the vault: the right passphrase opens
// it, and the old file's secrets join what it already held.
func TestOldKeelJSONBesideAVaultKeepsTheVault(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(Host{Name: "new", Address: "10.0.0.1", Username: "u", AuthType: AuthPassword, Password: "vault-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentKey("gemini", "gemini-key"); err != nil {
		t.Fatal(err)
	}
	addOldFormatHost(t, st.path, "old1", "old-secret")

	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	locked, err := New(st.path, TestVault())
	if err != nil {
		t.Fatal(err)
	}
	if !locked.Locked() || locked.NeedsSetup() {
		t.Fatalf("locked=%v needsSetup=%v, want a locked vault to unlock", locked.Locked(), locked.NeedsSetup())
	}
	if err := locked.Setup("another-pass"); !errors.Is(err, ErrVaultExists) {
		t.Fatalf("Setup = %v, want ErrVaultExists", err)
	}
	if err := locked.Unlock("wrong-pass"); err == nil {
		t.Fatal("a wrong passphrase unlocked the vault")
	}
	if err := locked.Unlock("test-vault-pass"); err != nil {
		t.Fatal(err)
	}
	check := func(s *Store) {
		t.Helper()
		if got, err := s.GetHost(h.ID); err != nil || got.Password != "vault-secret" {
			t.Fatalf("vault host password %q %v", got.Password, err)
		}
		if got, err := s.GetHost("old1"); err != nil || got.Password != "old-secret" {
			t.Fatalf("old host password %q %v", got.Password, err)
		}
		if k, err := s.AgentKey("gemini"); err != nil || k != "gemini-key" {
			t.Fatalf("agent key %q %v", k, err)
		}
	}
	check(locked)
	js, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), "old-secret") {
		t.Fatal("keel.json still holds the old secret in the clear")
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	reopened, err := New(st.path, TestVault())
	if err != nil {
		t.Fatal(err)
	}
	check(reopened)
}

// On an unlocked start, the first save bootstrap makes already carries an
// old keel.json's secrets: a crash before New's own save loses none.
func TestBootstrapSavesOldSecretsIntoTheVault(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	dir := t.TempDir()
	path := filepath.Join(dir, "keel.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"hosts":[],"knownHosts":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	addOldFormatHost(t, path, "old1", "old-secret")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := loadPlain(b)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{path: path, vaultPath: filepath.Join(dir, "keel.vault"), secrets: map[string]hostSecret{}, now: time.Now, data: plain.data}
	TestVault()(s)
	if err := s.bootstrap(plain.secrets); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(s.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	id, err := scryptIdentity("test-vault-pass")
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := decryptVault(blob, id)
	if err != nil {
		t.Fatal(err)
	}
	if secrets["old1"].Password != "old-secret" {
		t.Fatal("keel.vault after bootstrap's save lacks the old keel.json's secret")
	}
}
