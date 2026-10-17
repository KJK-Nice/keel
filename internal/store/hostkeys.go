package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	if s.locked.Load() {
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
	if s.locked.Load() {
		return false, ErrLocked
	}
	if _, ok := s.data.KnownHosts[hostname]; !ok {
		return false, nil
	}
	delete(s.data.KnownHosts, hostname)
	delete(s.data.KnownHostTypes, hostname)
	return true, s.saveLocked()
}

// Trust offers. A dialer's refusal of an unknown or changed key records
// what it saw (address:port, fingerprint, key type) under a random token
// for OfferTTL. TrustOffered saves exactly that key and nothing else, so
// the API cannot trust a key no Keel connect has seen, or a key for
// another address than the one the connect went to.

// OfferTTL is how long a refused key can still be trusted.
const OfferTTL = 2 * time.Minute

const maxOffers = 256

// ErrNoOffer is returned when a trust request names no live offer.
var ErrNoOffer = errors.New("this host key offer expired or does not match; connect or test again")

type hostKeyOffer struct {
	hostname    string
	fingerprint string
	keyType     string
	expected    string
	mismatch    bool
	expires     time.Time
}

// offerNow is the clock for offers (tests replace it).
var offerNow = time.Now

func (s *Store) offerLocked(o hostKeyOffer) string {
	now := offerNow()
	if s.offers == nil {
		s.offers = map[string]hostKeyOffer{}
	}
	for k, v := range s.offers {
		if now.After(v.expires) {
			delete(s.offers, k)
		}
	}
	for len(s.offers) >= maxOffers {
		oldest := ""
		for k, v := range s.offers {
			if oldest == "" || v.expires.Before(s.offers[oldest].expires) {
				oldest = k
			}
		}
		delete(s.offers, oldest)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	tok := hex.EncodeToString(b)
	o.expires = now.Add(OfferTTL)
	s.offers[tok] = o
	return tok
}

// TrustOffered saves the key a connect was refused for. hostname and
// fingerprint must be the offer's; replace must be set exactly when the
// offer was a changed key, and the saved key must still be the one the
// offer saw. The offer is used up.
func (s *Store) TrustOffered(token, hostname, fingerprint string, replace bool) (KnownHostKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return KnownHostKey{}, ErrLocked
	}
	o, ok := s.offers[token]
	if !ok || token == "" || offerNow().After(o.expires) || o.hostname != hostname || o.fingerprint != fingerprint {
		return KnownHostKey{}, ErrNoOffer
	}
	if o.mismatch != replace {
		if o.mismatch {
			return KnownHostKey{}, errors.New("the saved key is different: replacing it must be asked for")
		}
		return KnownHostKey{}, ErrNoOffer
	}
	if s.data.KnownHosts[hostname] != o.expected {
		return KnownHostKey{}, ErrNoOffer
	}
	delete(s.offers, token)
	if s.data.KnownHosts == nil {
		s.data.KnownHosts = map[string]string{}
	}
	s.data.KnownHosts[hostname] = fingerprint
	if s.data.KnownHostTypes == nil {
		s.data.KnownHostTypes = map[string]string{}
	}
	if o.keyType != "" {
		s.data.KnownHostTypes[hostname] = o.keyType
	} else {
		delete(s.data.KnownHostTypes, hostname)
	}
	if err := s.saveDurableLocked(); err != nil {
		return KnownHostKey{}, err
	}
	return KnownHostKey{Hostname: hostname, Fingerprint: fingerprint, KeyType: o.keyType, Known: true}, nil
}

var fingerprintRE = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}=?$`)

// ValidFingerprint reports whether fp is an OpenSSH SHA256 fingerprint.
func ValidFingerprint(fp string) bool { return fingerprintRE.MatchString(fp) }

// ValidHostKeyName reports whether name is address:port as dialers check it.
func ValidHostKeyName(name string) bool {
	host, port, err := net.SplitHostPort(name)
	if err != nil || host == "" || strings.ContainsAny(host, " \t\r\n/@") {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535 && net.JoinHostPort(host, port) == name
}
