package store

import (
	"net"
	"strconv"
	"strings"
)

// KnownHosts holds one SHA256 fingerprint (ssh.FingerprintSHA256, the
// format `ssh-keygen -l` prints) per "address:port", the name the SSH
// dialer checks. Trust is on first use: a first connect asks, a different
// key later is refused until the user replaces it.

// KnownHostKey is the saved key for one hostname.
type KnownHostKey struct {
	Hostname    string `json:"hostname"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// KeyType is ssh-ed25519, ecdsa-sha2-nistp256, …; empty when the key
	// was trusted before Keel kept types.
	KeyType string `json:"keyType,omitempty"`
	Known   bool   `json:"known"`
}

// HostKeyName is the KnownHosts name of a host: address:port, as the SSH
// dialer passes it to the host key check.
func HostKeyName(h Host) string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(strings.TrimSpace(h.Address), strconv.Itoa(port))
}

// KnownHost returns the saved key for hostname, if any.
func (s *Store) KnownHost(hostname string) (KnownHostKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return KnownHostKey{}, ErrLocked
	}
	out := KnownHostKey{Hostname: hostname}
	if fp, ok := s.data.KnownHosts[hostname]; ok {
		out.Fingerprint = fp
		out.KeyType = s.data.KnownHostTypes[hostname]
		out.Known = true
	}
	return out, nil
}

// ForgetHostKey removes the saved key for hostname, so the next connect
// asks again (trust on first use). It reports whether there was one.
func (s *Store) ForgetHostKey(hostname string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return false, ErrLocked
	}
	if _, ok := s.data.KnownHosts[hostname]; !ok {
		return false, nil
	}
	delete(s.data.KnownHosts, hostname)
	delete(s.data.KnownHostTypes, hostname)
	return true, s.saveLocked()
}
