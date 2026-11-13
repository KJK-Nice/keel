package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

// EnvPassphrase names the variable that unlocks or creates the vault at
// start. It holds a secret: SecretEnv lists it, and a local shell never
// inherits a listed variable (internal/session).
const EnvPassphrase = "KEEL_VAULT_PASSPHRASE"

// SecretEnv is every KEEL_* variable whose value is a secret.
var SecretEnv = []string{EnvPassphrase}

const (
	keyringService = "keel"
	keyringAccount = "vault"
	// envKeyring set to "off" keeps the store away from the OS keychain:
	// it neither reads nor writes the "keel"/"vault" item, as if the
	// keychain did not answer. Unset (the default) keeps today's behavior.
	// Tests use it so a scratch vault never touches the user's keychain.
	envKeyring = "KEEL_KEYRING"

	vaultScrypt = "scrypt"
	vaultX25519 = "x25519"
)

var (
	ErrLocked     = errors.New("vault locked")
	ErrPassphrase = errors.New("incorrect vault passphrase")
	// ErrVaultExists is returned by Setup when a vault already exists. Setup
	// would otherwise re-key (and, while locked, overwrite) the existing vault.
	ErrVaultExists = errors.New("vault already exists; unlock it instead")

	errKeyringOff = errors.New("OS keychain is off (KEEL_KEYRING=off)")
)

// Option changes how New builds a Store.
type Option func(*Store)

// TestVault is for tests, and every test's store takes it
// (vault_guard_test.go): the store never asks the OS keychain, whatever
// KEEL_KEYRING says, and wraps a passphrase vault with a cheap scrypt work
// factor. go-keyring shells out to /usr/bin/security on macOS, which
// endpoint security flags as credential access (INC-777162).
func TestVault() Option {
	return func(s *Store) {
		s.keyringOff = true
		s.scryptWork = 2
	}
}

type hostSecret struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	// Token is an agent provider API key (agentkeys.go).
	Token string `json:"token,omitempty"`
}

type vaultFile struct {
	Secrets map[string]hostSecret `json:"secrets"`
}

func envPass() string {
	return os.Getenv(EnvPassphrase)
}

func encryptVault(secrets map[string]hostSecret, recipients ...age.Recipient) ([]byte, error) {
	if secrets == nil {
		secrets = map[string]hostSecret{}
	}
	plain, err := json.Marshal(vaultFile{Secrets: secrets})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plain); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decryptVault(blob []byte, identities ...age.Identity) (map[string]hostSecret, error) {
	r, err := age.Decrypt(bytes.NewReader(blob), identities...)
	if err != nil {
		return nil, err
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var vf vaultFile
	if err := json.Unmarshal(plain, &vf); err != nil {
		return nil, fmt.Errorf("vault payload: %w", err)
	}
	if vf.Secrets == nil {
		vf.Secrets = map[string]hostSecret{}
	}
	return vf.Secrets, nil
}

func (s *Store) scryptRecipient(pass string) (age.Recipient, error) {
	r, err := age.NewScryptRecipient(pass)
	if err != nil {
		return nil, err
	}
	if s.scryptWork > 0 {
		r.SetWorkFactor(s.scryptWork)
	}
	return r, nil
}

func scryptIdentity(pass string) (age.Identity, error) {
	return age.NewScryptIdentity(pass)
}

func (s *Store) keyringIdentity() (*age.X25519Identity, error) {
	if s.keyringOff {
		return nil, errKeyringOff
	}
	v, err := keyring.Get(keyringService, keyringAccount)
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Identity(v)
}

func (s *Store) saveKeyringIdentity(ident *age.X25519Identity) error {
	if s.keyringOff {
		return errKeyringOff
	}
	return keyring.Set(keyringService, keyringAccount, ident.String())
}
