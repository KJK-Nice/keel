// Package authtoken manages the secret that authenticates clients of the
// loopback keel serve API.
//
// The token lives in the data directory (paths.TokenFile, mode 0600), so
// only the user who owns that directory can read it. keel serve requires it
// on privileged routes; the desktop app and `keel serve stop` read it from
// the same file. It persists across daemon restarts so a running window
// keeps working when keel serve is restarted.
package authtoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"keel/internal/paths"
)

// Header carries the token on requests from native clients.
const Header = "X-Keel-Token"

// MCPHeader carries keel mcp's own token (paths.MCPTokenFile). keel serve
// accepts it on /api/mcp/ only, and only it there.
const MCPHeader = "X-Keel-MCP-Token"

const tokenBytes = 32

// LoadOrCreate returns the token for dataDir, creating it if it is missing.
// Concurrent callers (the desktop app and keel serve) agree on one token:
// the file is published with a hard link, which fails if it already exists.
func LoadOrCreate(dataDir string) (string, error) {
	return loadOrCreate(dataDir, paths.TokenFile(dataDir))
}

// LoadOrCreateMCP is LoadOrCreate for keel mcp's token, a separate secret
// in its own file (paths.MCPTokenFile).
func LoadOrCreateMCP(dataDir string) (string, error) {
	return loadOrCreate(dataDir, paths.MCPTokenFile(dataDir))
}

func loadOrCreate(dataDir, path string) (string, error) {
	if tok, err := read(path); err == nil {
		return tok, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)

	tmp, err := os.CreateTemp(dataDir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another process won the race; use its token.
			return read(path)
		}
		// Filesystems without hard links: fall back to rename, which can
		// lose a simultaneous race but still leaves one complete token.
		if rerr := os.Rename(tmpName, path); rerr != nil {
			return "", err
		}
		return read(path)
	}
	return tok, nil
}

// Read returns the token stored for dataDir. The error wraps os.ErrNotExist
// when there is none yet.
func Read(dataDir string) (string, error) {
	return read(paths.TokenFile(dataDir))
}

// ReadMCP returns keel mcp's token for dataDir. The error wraps
// os.ErrNotExist when keel serve has not made one yet.
func ReadMCP(dataDir string) (string, error) {
	return read(paths.MCPTokenFile(dataDir))
}

func read(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if len(tok) != 2*tokenBytes {
		return "", fmt.Errorf("%s: malformed token", filepath.Base(path))
	}
	if _, err := hex.DecodeString(tok); err != nil {
		return "", fmt.Errorf("%s: malformed token", filepath.Base(path))
	}
	return tok, nil
}

// Equal reports whether got matches want in constant time. An empty want
// never matches.
func Equal(got, want string) bool {
	if want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Proof answers a client's nonce with HMAC-SHA256(token, nonce), so the
// client can check the server holds the token without sending it.
func Proof(token, nonce string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte(nonce))
	return hex.EncodeToString(m.Sum(nil))
}

// CheckProof reports whether proof answers nonce for token.
func CheckProof(token, nonce, proof string) bool {
	want := Proof(token, nonce)
	return len(proof) == len(want) && subtle.ConstantTimeCompare([]byte(proof), []byte(want)) == 1
}

// NewNonce returns a random challenge for Proof.
func NewNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
