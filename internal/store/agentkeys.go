package store

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

// Agent provider keys (a Google Gemini API key, a TypeSafe Jev key) are
// vault secrets: encrypted in keel.vault with the host passwords, never in
// keel.json, and never returned by the HTTP API, which only says whether
// one is configured. Only keel serve reads them back (AgentKey), to call
// the provider itself.

const agentKeyPrefix = "agentkey:"

const maxAgentKeyLen = 1024

var ErrAgentKey = errors.New("invalid agent key")

func agentKeySecretID(provider string) string {
	return agentKeyPrefix + provider
}

func validProviderID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// SetAgentKey stores key for provider in the vault, replacing any old one.
func (s *Store) SetAgentKey(provider, key string) error {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if !validProviderID(provider) {
		return ErrAgentKey
	}
	if key == "" || len(key) > maxAgentKeyLen || strings.IndexFunc(key, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return ErrAgentKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	s.secrets[agentKeySecretID(provider)] = hostSecret{Token: key}
	return s.saveLocked()
}

// ClearAgentKey removes provider's key from the vault. Clearing a key that
// is not set is not an error.
func (s *Store) ClearAgentKey(provider string) error {
	provider = strings.TrimSpace(provider)
	if !validProviderID(provider) {
		return ErrAgentKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	if _, ok := s.secrets[agentKeySecretID(provider)]; !ok {
		return nil
	}
	delete(s.secrets, agentKeySecretID(provider))
	return s.saveLocked()
}

// AgentKeyConfigured reports whether provider has a key, without reading it.
func (s *Store) AgentKeyConfigured(provider string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return false, ErrLocked
	}
	sec, ok := s.secrets[agentKeySecretID(strings.TrimSpace(provider))]
	return ok && sec.Token != "", nil
}

// AgentKey returns provider's key for keel serve to call the provider with.
// It must never reach an HTTP response, a log line or keel.json. In phase
// 6a nothing calls it outside tests; 6b's provider factory will.
func (s *Store) AgentKey(provider string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return "", ErrLocked
	}
	sec, ok := s.secrets[agentKeySecretID(strings.TrimSpace(provider))]
	if !ok || sec.Token == "" {
		return "", ErrNotFound
	}
	return sec.Token, nil
}

// KnownSecretValues lists every secret value in the vault (host and
// identity passwords, private keys, passphrases, agent keys). It exists for
// one use: the agent's exact-match redaction list, so a stored secret that
// shows up in a pane is replaced before anything leaves the machine. The
// order is stable; empty values are left out.
func (s *Store) KnownSecretValues() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return nil, ErrLocked
	}
	seen := map[string]struct{}{}
	for _, sec := range s.secrets {
		for _, v := range []string{sec.Password, sec.PrivateKey, sec.Passphrase, sec.Token} {
			if v != "" {
				seen[v] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}
