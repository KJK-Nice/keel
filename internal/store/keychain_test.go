package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

// fakeKeychain stands in for the OS keychain: these tests never reach it.
type fakeKeychain struct {
	item   string
	getErr error
	sets   int
}

func (k *fakeKeychain) Get(_, _ string) (string, error) {
	if k.getErr != nil {
		return "", k.getErr
	}
	if k.item == "" {
		return "", keyring.ErrNotFound
	}
	return k.item, nil
}

func (k *fakeKeychain) Set(_, _, secret string) error {
	k.sets++
	k.item = secret
	return nil
}

// withFakeKeychain comes after TestVault and gives the store k in place
// of the OS keychain.
func withFakeKeychain(k *fakeKeychain) Option {
	return func(s *Store) {
		s.keyringOff = false
		s.keychain = k
	}
}

var errDenied = errors.New("keychain access denied")

// A keychain read that fails for any reason but "not found" must never
// make a new key: that would overwrite the item every data dir shares.
func TestKeychainReadErrorNeverReplacesTheKey(t *testing.T) {
	t.Setenv("SLIME_VAULT_PASSPHRASE", "")
	ident, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	k := &fakeKeychain{item: ident.String(), getErr: errDenied}
	st, err := New(filepath.Join(t.TempDir(), "keel.json"), TestVault(), withFakeKeychain(k))
	if err != nil {
		t.Fatal(err)
	}
	if k.sets != 0 || k.item != ident.String() {
		t.Fatalf("the keychain item was written %d times after a failed read", k.sets)
	}
	if !st.Locked() {
		t.Fatal("a store whose keychain read failed is unlocked")
	}
}

// A keychain vault whose key could not be read at start unlocks by asking
// the keychain again; a passphrase cannot open it.
func TestKeychainVaultUnlocksByRetryingTheKeychain(t *testing.T) {
	t.Setenv("SLIME_VAULT_PASSPHRASE", "")
	k := &fakeKeychain{}
	path := filepath.Join(t.TempDir(), "keel.json")
	st, err := New(path, TestVault(), withFakeKeychain(k))
	if err != nil {
		t.Fatal(err)
	}
	if st.Locked() || k.sets != 1 {
		t.Fatalf("fresh keychain store: locked=%v sets=%d", st.Locked(), k.sets)
	}
	h, err := st.UpsertHost(Host{Name: "box", Address: "10.0.0.1", Username: "u", AuthType: AuthPassword, Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	k.getErr = errDenied
	again, err := New(path, TestVault(), withFakeKeychain(k))
	if err != nil {
		t.Fatal(err)
	}
	if vs := again.VaultState(); !vs.Locked || vs.NeedsSetup || vs.Mode != "keychain" {
		t.Fatalf("vault state after a failed read: %+v", vs)
	}
	if err := again.Unlock("anything"); err == nil || !strings.Contains(err.Error(), "keychain") || !again.Locked() {
		t.Fatalf("Unlock with the keychain still failing = %v, locked=%v", err, again.Locked())
	}
	k.getErr = nil
	if err := again.Unlock(""); err != nil {
		t.Fatalf("Unlock after the keychain answers = %v", err)
	}
	if got, err := again.GetHost(h.ID); err != nil || got.Password != "pw" {
		t.Fatalf("host after unlock: %q %v", got.Password, err)
	}
	if k.sets != 1 {
		t.Fatalf("the keychain item was written %d times", k.sets)
	}
}
