package store

import (
	"fmt"
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
	if s.locked {
		return Host{}, ErrLocked
	}
	for _, h := range s.data.Hosts {
		if h.ID != id {
			continue
		}
		host := s.fullLocked(h)
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
	return Host{}, ErrNotFound
}

func (s *Store) ListIdentities() ([]Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
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
	if s.locked {
		return Identity{}, ErrLocked
	}
	return s.identityFullLocked(id)
}

func (s *Store) identityViewLocked(p persistIdentity) Identity {
	out := Identity{
		ID: p.ID, Name: p.Name, Kind: p.Kind, Username: p.Username,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if sec, ok := s.secrets[identitySecretID(p.ID)]; ok {
		if sec.Password != "" {
			out.Password = "x"
		}
		if sec.PrivateKey != "" {
			out.PrivateKey = "x"
		}
		if sec.Passphrase != "" {
			out.Passphrase = "x"
		}
	}
	return out
}

func (s *Store) identityFullLocked(id string) (Identity, error) {
	for _, p := range s.data.Identities {
		if p.ID != id {
			continue
		}
		out := Identity{
			ID: p.ID, Name: p.Name, Kind: p.Kind, Username: p.Username,
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		}
		if sec, ok := s.secrets[identitySecretID(p.ID)]; ok {
			out.Password = sec.Password
			out.PrivateKey = sec.PrivateKey
			out.Passphrase = sec.Passphrase
		}
		return out, nil
	}
	return Identity{}, ErrNotFound
}

func (s *Store) UpsertIdentity(in Identity) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return Identity{}, ErrLocked
	}
	now := time.Now().UTC()
	sid := identitySecretID(in.ID)
	if in.ID == "" {
		in.ID = newID()
		in.CreatedAt = now
		in.UpdatedAt = now
		s.data.Identities = append(s.data.Identities, persistIdentityFrom(in))
		s.putSecretLocked(identitySecretID(in.ID), hostSecret{
			Password:   cleanSecret(in.Password),
			PrivateKey: cleanSecret(in.PrivateKey),
			Passphrase: cleanSecret(in.Passphrase),
		}, hostSecret{})
		if err := s.saveLocked(); err != nil {
			return Identity{}, err
		}
		return s.identityFullLocked(in.ID)
	}
	for i, existing := range s.data.Identities {
		if existing.ID != in.ID {
			continue
		}
		in.CreatedAt = existing.CreatedAt
		in.UpdatedAt = now
		prev := s.secrets[sid]
		s.data.Identities[i] = persistIdentityFrom(in)
		s.putSecretLocked(sid, hostSecret{
			Password:   in.Password,
			PrivateKey: in.PrivateKey,
			Passphrase: in.Passphrase,
		}, prev)
		if err := s.saveLocked(); err != nil {
			return Identity{}, err
		}
		return s.identityFullLocked(in.ID)
	}
	in.CreatedAt = now
	in.UpdatedAt = now
	s.data.Identities = append(s.data.Identities, persistIdentityFrom(in))
	s.putSecretLocked(sid, hostSecret{
		Password:   cleanSecret(in.Password),
		PrivateKey: cleanSecret(in.PrivateKey),
		Passphrase: cleanSecret(in.Passphrase),
	}, hostSecret{})
	if err := s.saveLocked(); err != nil {
		return Identity{}, err
	}
	return s.identityFullLocked(in.ID)
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
	if s.locked {
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
