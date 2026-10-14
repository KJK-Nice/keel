package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyringOffNeverAsksTheKeychain(t *testing.T) {
	t.Setenv("KEEL_KEYRING", "off")
	if _, err := keyringIdentity(); !errors.Is(err, errKeyringOff) {
		t.Fatalf("keyringIdentity = %v", err)
	}
	if err := saveKeyringIdentity(nil); !errors.Is(err, errKeyringOff) {
		t.Fatalf("saveKeyringIdentity = %v", err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	st, err := New(filepath.Join(t.TempDir(), "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	vs := st.VaultState()
	if !vs.Locked || !vs.NeedsSetup || !vs.FirstRun || !vs.KeyringOff || vs.Mode != "" {
		t.Fatalf("fresh store with the keychain off = %+v", vs)
	}
}

func TestFirstRunOnlyOnSeededStores(t *testing.T) {
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("KEEL_VAULT_FAST", "1")
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	path := filepath.Join(t.TempDir(), "keel.json")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Setup("p"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"firstRun": true`) {
		t.Fatalf("seeded keel.json = %s", raw)
	}
	if err := st.FinishFirstRun(); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "firstRun") {
		t.Fatalf("after FinishFirstRun = %s", raw)
	}

	// A keel.json from before the flag existed is never a first run.
	old := filepath.Join(t.TempDir(), "keel.json")
	if err := os.WriteFile(old, []byte(`{"version":3,"hosts":[],"knownHosts":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "p")
	st2, err := New(old)
	if err != nil {
		t.Fatal(err)
	}
	if st2.VaultState().FirstRun {
		t.Fatal("an existing keel.json must not start a first run")
	}
	// KEEL_VAULT_PASSPHRASE on a fresh store is a scripted setup: no first run.
	st3, err := New(filepath.Join(t.TempDir(), "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if vs := st3.VaultState(); vs.FirstRun || vs.Mode != "passphrase" || vs.Locked {
		t.Fatalf("env passphrase store = %+v", vs)
	}
}
