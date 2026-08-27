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

const (
	keyringService = "keel"
	keyringAccount = "vault"
	envPassphrase  = "KEEL_VAULT_PASSPHRASE"

	vaultScrypt = "scrypt"
	vaultX25519 = "x25519"
)

var (
	ErrLocked     = errors.New("vault locked")
	ErrPassphrase = errors.New("incorrect vault passphrase")
)

type hostSecret struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type vaultFile struct {
	Secrets map[string]hostSecret `json:"secrets"`
}

func envPass() string {
	return os.Getenv(envPassphrase)
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

func scryptRecipient(pass string) (age.Recipient, error) {
	r, err := age.NewScryptRecipient(pass)
	if err != nil {
		return nil, err
	}
	if os.Getenv("KEEL_VAULT_FAST") == "1" {
		r.SetWorkFactor(2)
	}
	return r, nil
}

func scryptIdentity(pass string) (age.Identity, error) {
	return age.NewScryptIdentity(pass)
}

func keyringIdentity() (*age.X25519Identity, error) {
	s, err := keyring.Get(keyringService, keyringAccount)
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Identity(s)
}

func saveKeyringIdentity(ident *age.X25519Identity) error {
	return keyring.Set(keyringService, keyringAccount, ident.String())
}
