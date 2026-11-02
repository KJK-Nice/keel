package store

import (
	"errors"
	"testing"
)

// The signal server is saved in keel.json and read back after a restart,
// even while the vault is locked: keel serve reads it at start.
func TestSignalURLSurvivesARestartAndALock(t *testing.T) {
	st := testStore(t)
	if got := st.SignalURL(); got != "" {
		t.Fatalf("fresh store SignalURL = %q, want empty", got)
	}
	if err := st.SetSignalURL(" https://signal.example "); err != nil {
		t.Fatal(err)
	}
	if got := st.SignalURL(); got != "https://signal.example" {
		t.Fatalf("SignalURL = %q", got)
	}

	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	t.Setenv("KEEL_KEYRING", "off")
	locked, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if !locked.Locked() {
		t.Fatal("store should be locked without the passphrase")
	}
	if got := locked.SignalURL(); got != "https://signal.example" {
		t.Fatalf("locked SignalURL = %q, want the saved server", got)
	}
	if err := locked.SetSignalURL("https://other.example"); !errors.Is(err, ErrLocked) {
		t.Fatalf("SetSignalURL while locked = %v, want ErrLocked", err)
	}
	if err := locked.Unlock("test-vault-pass"); err != nil {
		t.Fatal(err)
	}
	if err := locked.SetSignalURL(""); err != nil {
		t.Fatal(err)
	}
	if got := locked.SignalURL(); got != "" {
		t.Fatalf("SignalURL after removing = %q", got)
	}
}
