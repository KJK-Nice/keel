package store

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const identitySecretPrefix = "identity:"

func identitySecretID(id string) string {
	return identitySecretPrefix + id
}

func (s *Store) HostForConnect(id string) (Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Host{}, ErrLocked
	}
	for _, h := range s.data.Hosts {
		if h.ID != id {
			continue
		}
		host, err := s.withIdentityLocked(s.fullLocked(h))
		if err != nil {
			return Host{}, err
		}
		if err := s.attachJumpLocked(&host); err != nil {
			return Host{}, err
		}
		return host, nil
	}
	return Host{}, ErrNotFound
}

// withIdentityLocked swaps a Keychain identity's kind and secrets into
// host, as a connect needs them.
func (s *Store) withIdentityLocked(host Host) (Host, error) {
	if host.IdentityID == "" {
		return host, nil
	}
	ident, err := s.identityFullLocked(host.IdentityID)
	if err != nil {
		return Host{}, err
	}
	host.AuthType = ident.Kind
	host.Password = ident.Password
	host.PrivateKey = ident.PrivateKey
	host.Passphrase = ident.Passphrase
	if strings.TrimSpace(host.Username) == "" {
		host.Username = ident.Username
	}
	return host, nil
}

func (s *Store) ListIdentities() ([]Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	out := make([]Identity, len(s.data.Identities))
	for i, id := range s.data.Identities {
		out[i] = s.identityViewLocked(id)
	}
	return out, nil
}

func (s *Store) GetIdentity(id string) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Identity{}, ErrLocked
	}
	return s.identityFullLocked(id)
}

// identityViewLocked is an identity for a list: SavedSecret where it holds
// a secret.
func (s *Store) identityViewLocked(p persistIdentity) Identity {
	return s.identityOfLocked(p).Redacted()
}

// Redacted is in with SavedSecret in place of each secret it holds.
func (in Identity) Redacted() Identity {
	in.Password, in.PrivateKey, in.Passphrase = savedMark(in.Password), savedMark(in.PrivateKey), savedMark(in.Passphrase)
	return in
}

func (s *Store) identityOfLocked(p persistIdentity) Identity {
	out := Identity{
		ID: p.ID, Name: p.Name, Kind: p.Kind, Username: p.Username,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if sec, ok := s.secrets[identitySecretID(p.ID)]; ok {
		out.Password = sec.Password
		out.PrivateKey = sec.PrivateKey
		out.Passphrase = sec.Passphrase
	}
	return out
}

func (s *Store) identityFullLocked(id string) (Identity, error) {
	for _, p := range s.data.Identities {
		if p.ID == id {
			return s.identityOfLocked(p), nil
		}
	}
	return Identity{}, ErrNotFound
}

// UpsertIdentity creates (in.ID "", or an id no identity has) or updates
// an identity. It wants a name and a kind; a blank secret, or SavedSecret,
// keeps the saved one on an update.
func (s *Store) UpsertIdentity(in Identity) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Identity{}, ErrLocked
	}
	if strings.TrimSpace(in.Name) == "" {
		return Identity{}, &fieldError{of: ErrIdentityField, msg: "name is required"}
	}
	if err := ValidateIdentityKind(in.Kind); err != nil {
		return Identity{}, &fieldError{of: ErrIdentityField, msg: err.Error()}
	}
	now := time.Now().UTC()
	at := -1
	if in.ID != "" {
		at = slices.IndexFunc(s.data.Identities, func(p persistIdentity) bool { return p.ID == in.ID })
	}
	var prev hostSecret
	if at < 0 {
		if in.ID == "" {
			in.ID = newID()
		}
		in.CreatedAt = now
		s.data.Identities = append(s.data.Identities, persistIdentity{})
		at = len(s.data.Identities) - 1
	} else {
		in.CreatedAt = s.data.Identities[at].CreatedAt
		prev = s.secrets[identitySecretID(in.ID)]
	}
	in.UpdatedAt = now
	s.data.Identities[at] = persistIdentityFrom(in)
	s.putSecretLocked(identitySecretID(in.ID), hostSecret{Password: in.Password, PrivateKey: in.PrivateKey, Passphrase: in.Passphrase}, prev)
	out := s.identityOfLocked(s.data.Identities[at])
	if err := s.saveLocked(); err != nil {
		return Identity{}, err
	}
	return out, nil
}

func persistIdentityFrom(in Identity) persistIdentity {
	return persistIdentity{
		ID: in.ID, Name: in.Name, Kind: in.Kind, Username: in.Username,
		CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt,
	}
}

func (s *Store) DeleteIdentity(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return ErrLocked
	}
	out := s.data.Identities[:0]
	found := false
	for _, p := range s.data.Identities {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return ErrNotFound
	}
	s.data.Identities = out
	delete(s.secrets, identitySecretID(id))
	for i, h := range s.data.Hosts {
		if h.IdentityID == id {
			s.data.Hosts[i].IdentityID = ""
		}
	}
	return s.saveLocked()
}

func ValidateIdentityKind(kind string) error {
	switch kind {
	case "password", "key":
		return nil
	default:
		return fmt.Errorf("kind must be password or key")
	}
}
