package sshconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// MaxKeyBytes caps a private key file. An RSA 16384-bit key in OpenSSH
// format is about 13 KB.
const MaxKeyBytes = 64 << 10

var (
	ErrKeyPassphrase = errors.New("this key needs its passphrase")
	ErrKeyWrongPass  = errors.New("the passphrase does not open this key")
)

// Key describes a private key file a host names with IdentityFile. It holds
// no key material.
type Key struct {
	Path      string `json:"path"`
	Display   string `json:"display"`
	Name      string `json:"name"`
	Exists    bool   `json:"exists"`
	Encrypted bool   `json:"encrypted"`
	// Problem says why the key cannot be copied (missing, not a regular
	// file, too large, not a private key Keel can read).
	Problem string `json:"problem,omitempty"`
}

// InspectKey reads path (read-only) to tell whether it is a private key and
// whether it has a passphrase. It changes nothing on disk.
func InspectKey(path, display string) Key {
	k := Key{Path: path, Display: display, Name: filepath.Base(path)}
	b, err := ReadKeyFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			k.Problem = "file not found"
			return k
		}
		k.Exists = true
		k.Problem = err.Error()
		return k
	}
	k.Exists = true
	_, err = ssh.ParseRawPrivateKey(b)
	var missing *ssh.PassphraseMissingError
	switch {
	case err == nil:
	case errors.As(err, &missing):
		k.Encrypted = true
	default:
		k.Problem = "not a private key Keel can read"
	}
	return k
}

// LoadKey returns the key file's bytes after checking they parse as a
// private key (with passphrase, when the key has one). The file is opened
// read-only and never changed, moved or chmod'ed.
func LoadKey(path, passphrase string) ([]byte, error) {
	b, err := ReadKeyFile(path)
	if err != nil {
		return nil, err
	}
	_, err = ssh.ParseRawPrivateKey(b)
	var missing *ssh.PassphraseMissingError
	switch {
	case err == nil:
		return b, nil
	case errors.As(err, &missing):
		if passphrase == "" {
			return nil, ErrKeyPassphrase
		}
		if _, err := ssh.ParseRawPrivateKeyWithPassphrase(b, []byte(passphrase)); err != nil {
			return nil, ErrKeyWrongPass
		}
		return b, nil
	default:
		return nil, fmt.Errorf("not a private key Keel can read")
	}
}

// ReadKeyFile reads a regular file of at most MaxKeyBytes.
func ReadKeyFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if fi.Size() > MaxKeyBytes {
		return nil, fmt.Errorf("larger than %d KiB", MaxKeyBytes>>10)
	}
	return readFileLimited(path, MaxKeyBytes)
}

// readFileLimited opens path read-only and reads at most max bytes; a file
// that grew past max since it was checked is refused.
func readFileLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("larger than %d KiB", max>>10)
	}
	return b, nil
}

// KeyPaths lists every IdentityFile the hosts name, once each, in order.
func (r Result) KeyPaths() []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range r.Hosts {
		for _, p := range h.IdentityFiles {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// DisplayPath shortens path under the Home that Parse used.
func DisplayPath(opts Options, path string) string {
	home := opts.Home
	if home == "" {
		dir, err := filepath.Abs(opts.SSHDir)
		if err != nil {
			dir = opts.SSHDir
		}
		home = filepath.Dir(dir)
	}
	p := &parser{opts: Options{Home: home}}
	return p.display(path)
}
