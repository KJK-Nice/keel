package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"filippo.io/age"
)

var ErrNotFound = errors.New("not found")

type Host struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Group      string    `json:"group"`
	Address    string    `json:"address"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	AuthType   string    `json:"authType"` // local | password | key
	Password   string    `json:"password,omitempty"`
	PrivateKey string    `json:"privateKey,omitempty"`
	Passphrase string    `json:"passphrase,omitempty"`
	Color      string    `json:"color"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Snippet struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"createdAt"`
}

type persistHost struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Group     string    `json:"group"`
	Address   string    `json:"address"`
	Port      int       `json:"port"`
	Username  string    `json:"username"`
	AuthType  string    `json:"authType"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type persistData struct {
	Version    int               `json:"version"`
	Vault      string            `json:"vault,omitempty"`
	Hosts      []persistHost     `json:"hosts"`
	Snippets   []Snippet         `json:"snippets"`
	KnownHosts map[string]string `json:"knownHosts"`
}

type Store struct {
	path      string
	vaultPath string
	mu        sync.Mutex
	data      persistData
	secrets   map[string]hostSecret
	locked    bool
	needSetup bool
	recipient age.Recipient
}

func New(path string) (*Store, error) {
	s := &Store{
		path:      path,
		vaultPath: filepath.Join(filepath.Dir(path), "keel.vault"),
		secrets:   map[string]hostSecret{},
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		s.data = seed()
		if err := s.bootstrap(); err != nil {
			return nil, err
		}
		return s, nil
	}
	plain, err := loadPlain(b)
	if err != nil {
		return nil, err
	}
	s.data = plain.data
	pending := plain.secrets
	if err := s.bootstrap(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locked && len(pending) > 0 {
		for id, sec := range pending {
			s.secrets[id] = sec
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	} else if s.locked && len(pending) > 0 {
		s.secrets = pending
		s.needSetup = true
	}
	return s, nil
}

type loaded struct {
	data    persistData
	secrets map[string]hostSecret
}

func loadPlain(b []byte) (loaded, error) {
	var raw struct {
		Version    int               `json:"version"`
		Vault      string            `json:"vault"`
		Hosts      []json.RawMessage `json:"hosts"`
		Snippets   []Snippet         `json:"snippets"`
		KnownHosts map[string]string `json:"knownHosts"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return loaded{}, fmt.Errorf("parse store: %w", err)
	}
	out := persistData{
		Version:    raw.Version,
		Vault:      raw.Vault,
		Snippets:   raw.Snippets,
		KnownHosts: raw.KnownHosts,
	}
	if out.KnownHosts == nil {
		out.KnownHosts = map[string]string{}
	}
	secrets := map[string]hostSecret{}
	for _, rawHost := range raw.Hosts {
		var h Host
		if err := json.Unmarshal(rawHost, &h); err != nil {
			return loaded{}, fmt.Errorf("parse host: %w", err)
		}
		if h.Password != "" || h.PrivateKey != "" || h.Passphrase != "" {
			secrets[h.ID] = hostSecret{
				Password:   h.Password,
				PrivateKey: h.PrivateKey,
				Passphrase: h.Passphrase,
			}
		}
		out.Hosts = append(out.Hosts, persistHost{
			ID: h.ID, Name: h.Name, Group: h.Group, Address: h.Address,
			Port: h.Port, Username: h.Username, AuthType: h.AuthType,
			Color: h.Color, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
		})
	}
	return loaded{data: out, secrets: secrets}, nil
}

func seed() persistData {
	now := time.Now().UTC()
	return persistData{
		Version: 2,
		Hosts: []persistHost{{
			ID:        newID(),
			Name:      "Local Shell",
			Group:     "This machine",
			Address:   "localhost",
			Port:      0,
			Username:  os.Getenv("USER"),
			AuthType:  "local",
			Color:     "#5ee0b5",
			CreatedAt: now,
			UpdatedAt: now,
		}},
		Snippets: []Snippet{
			{ID: newID(), Name: "list files", Command: "ls -la\n", CreatedAt: now},
			{ID: newID(), Name: "disk", Command: "df -h\n", CreatedAt: now},
			{ID: newID(), Name: "memory", Command: "free -h\n", CreatedAt: now},
			{ID: newID(), Name: "top cpu", Command: "ps aux --sort=-%cpu | head -n 15\n", CreatedAt: now},
		},
		KnownHosts: map[string]string{},
	}
}

func (s *Store) bootstrap() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pass := envPass(); pass != "" {
		return s.unlockScryptLocked(pass, true)
	}
	if ident, err := keyringIdentity(); err == nil {
		blob, rerr := os.ReadFile(s.vaultPath)
		if rerr != nil && !os.IsNotExist(rerr) {
			return rerr
		}
		if os.IsNotExist(rerr) {
			s.recipient = ident.Recipient()
			s.data.Vault = vaultX25519
			s.data.Version = 2
			s.locked = false
			return s.saveLocked()
		}
		secrets, err := decryptVault(blob, ident)
		if err != nil {
			s.locked = true
			return nil
		}
		s.secrets = secrets
		s.recipient = ident.Recipient()
		s.data.Vault = vaultX25519
		s.locked = false
		return nil
	}
	blob, err := os.ReadFile(s.vaultPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		ident, gerr := age.GenerateX25519Identity()
		if gerr != nil {
			return gerr
		}
		if serr := saveKeyringIdentity(ident); serr == nil {
			s.recipient = ident.Recipient()
			s.data.Vault = vaultX25519
			s.data.Version = 2
			s.locked = false
			return s.saveLocked()
		}
		s.locked = true
		s.needSetup = true
		return nil
	}
	_ = blob
	s.locked = true
	s.needSetup = false
	return nil
}

func (s *Store) Locked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locked
}

func (s *Store) NeedsSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locked && s.needSetup
}

func (s *Store) Unlock(passphrase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locked {
		return nil
	}
	if passphrase == "" {
		return ErrPassphrase
	}
	if s.needSetup {
		return s.setupLocked(passphrase)
	}
	return s.unlockScryptLocked(passphrase, false)
}

func (s *Store) Setup(passphrase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if passphrase == "" {
		return errors.New("passphrase is required")
	}
	return s.setupLocked(passphrase)
}

func (s *Store) setupLocked(passphrase string) error {
	rec, err := scryptRecipient(passphrase)
	if err != nil {
		return err
	}
	s.recipient = rec
	s.data.Vault = vaultScrypt
	s.data.Version = 2
	s.locked = false
	s.needSetup = false
	return s.saveLocked()
}

func (s *Store) unlockScryptLocked(passphrase string, create bool) error {
	id, err := scryptIdentity(passphrase)
	if err != nil {
		return err
	}
	blob, err := os.ReadFile(s.vaultPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if !create && !s.needSetup {
			return fmt.Errorf("vault file missing")
		}
		rec, err := scryptRecipient(passphrase)
		if err != nil {
			return err
		}
		s.recipient = rec
		s.data.Vault = vaultScrypt
		s.data.Version = 2
		s.locked = false
		s.needSetup = false
		return s.saveLocked()
	}
	secrets, err := decryptVault(blob, id)
	if err != nil {
		if create && s.needSetup {
			return ErrPassphrase
		}
		s.locked = true
		return ErrPassphrase
	}
	rec, err := scryptRecipient(passphrase)
	if err != nil {
		return err
	}
	s.secrets = secrets
	s.recipient = rec
	s.data.Vault = vaultScrypt
	s.locked = false
	s.needSetup = false
	return nil
}

func (s *Store) ListHosts() ([]Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return nil, ErrLocked
	}
	out := make([]Host, len(s.data.Hosts))
	for i, h := range s.data.Hosts {
		out[i] = s.viewLocked(h)
	}
	return out, nil
}

func (s *Store) GetHost(id string) (Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return Host{}, ErrLocked
	}
	for _, h := range s.data.Hosts {
		if h.ID == id {
			return s.fullLocked(h), nil
		}
	}
	return Host{}, ErrNotFound
}

func (s *Store) viewLocked(h persistHost) Host {
	out := Host{
		ID: h.ID, Name: h.Name, Group: h.Group, Address: h.Address,
		Port: h.Port, Username: h.Username, AuthType: h.AuthType,
		Color: h.Color, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	if sec, ok := s.secrets[h.ID]; ok {
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

func (s *Store) fullLocked(h persistHost) Host {
	out := Host{
		ID: h.ID, Name: h.Name, Group: h.Group, Address: h.Address,
		Port: h.Port, Username: h.Username, AuthType: h.AuthType,
		Color: h.Color, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	if sec, ok := s.secrets[h.ID]; ok {
		out.Password = sec.Password
		out.PrivateKey = sec.PrivateKey
		out.Passphrase = sec.Passphrase
	}
	return out
}

func (s *Store) UpsertHost(h Host) (Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return Host{}, ErrLocked
	}
	now := time.Now().UTC()
	if h.Port == 0 && h.AuthType != "local" {
		h.Port = 22
	}
	if h.ID == "" {
		h.ID = newID()
		h.CreatedAt = now
		h.UpdatedAt = now
		s.data.Hosts = append(s.data.Hosts, persistFrom(h))
		s.putSecretLocked(h.ID, hostSecret{
			Password:   cleanSecret(h.Password),
			PrivateKey: cleanSecret(h.PrivateKey),
			Passphrase: cleanSecret(h.Passphrase),
		}, hostSecret{})
		if err := s.saveLocked(); err != nil {
			return Host{}, err
		}
		return s.fullLocked(s.data.Hosts[len(s.data.Hosts)-1]), nil
	}
	for i, existing := range s.data.Hosts {
		if existing.ID == h.ID {
			h.CreatedAt = existing.CreatedAt
			h.UpdatedAt = now
			prev := s.secrets[h.ID]
			s.data.Hosts[i] = persistFrom(h)
			s.putSecretLocked(h.ID, hostSecret{
				Password:   h.Password,
				PrivateKey: h.PrivateKey,
				Passphrase: h.Passphrase,
			}, prev)
			if err := s.saveLocked(); err != nil {
				return Host{}, err
			}
			return s.fullLocked(s.data.Hosts[i]), nil
		}
	}
	h.CreatedAt = now
	h.UpdatedAt = now
	s.data.Hosts = append(s.data.Hosts, persistFrom(h))
	s.putSecretLocked(h.ID, hostSecret{
		Password:   cleanSecret(h.Password),
		PrivateKey: cleanSecret(h.PrivateKey),
		Passphrase: cleanSecret(h.Passphrase),
	}, hostSecret{})
	if err := s.saveLocked(); err != nil {
		return Host{}, err
	}
	return s.fullLocked(s.data.Hosts[len(s.data.Hosts)-1]), nil
}

func persistFrom(h Host) persistHost {
	return persistHost{
		ID: h.ID, Name: h.Name, Group: h.Group, Address: h.Address,
		Port: h.Port, Username: h.Username, AuthType: h.AuthType,
		Color: h.Color, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

func cleanSecret(v string) string {
	if v == "__saved__" {
		return ""
	}
	return v
}

func (s *Store) putSecretLocked(id string, next, prev hostSecret) {
	sec := prev
	if v := cleanSecret(next.Password); v != "" {
		sec.Password = v
	} else if next.Password == "" || next.Password == "__saved__" {
		// keep prev
	}
	if v := cleanSecret(next.PrivateKey); v != "" {
		sec.PrivateKey = v
	}
	if v := cleanSecret(next.Passphrase); v != "" {
		sec.Passphrase = v
	}
	if sec.Password == "" && sec.PrivateKey == "" && sec.Passphrase == "" {
		delete(s.secrets, id)
		return
	}
	s.secrets[id] = sec
}

func (s *Store) DeleteHost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	hosts := s.data.Hosts[:0]
	found := false
	for _, h := range s.data.Hosts {
		if h.ID == id {
			found = true
			continue
		}
		hosts = append(hosts, h)
	}
	if !found {
		return ErrNotFound
	}
	s.data.Hosts = hosts
	delete(s.secrets, id)
	return s.saveLocked()
}

func (s *Store) ListSnippets() ([]Snippet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return nil, ErrLocked
	}
	out := make([]Snippet, len(s.data.Snippets))
	copy(out, s.data.Snippets)
	return out, nil
}

func (s *Store) UpsertSnippet(sn Snippet) (Snippet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return Snippet{}, ErrLocked
	}
	now := time.Now().UTC()
	if sn.ID == "" {
		sn.ID = newID()
		sn.CreatedAt = now
		s.data.Snippets = append(s.data.Snippets, sn)
		return sn, s.saveLocked()
	}
	for i, existing := range s.data.Snippets {
		if existing.ID == sn.ID {
			sn.CreatedAt = existing.CreatedAt
			s.data.Snippets[i] = sn
			return sn, s.saveLocked()
		}
	}
	sn.CreatedAt = now
	s.data.Snippets = append(s.data.Snippets, sn)
	return sn, s.saveLocked()
}

func (s *Store) DeleteSnippet(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	out := s.data.Snippets[:0]
	found := false
	for _, sn := range s.data.Snippets {
		if sn.ID == id {
			found = true
			continue
		}
		out = append(out, sn)
	}
	if !found {
		return ErrNotFound
	}
	s.data.Snippets = out
	return s.saveLocked()
}

func (s *Store) CheckHostKey(hostname, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	if s.data.KnownHosts == nil {
		s.data.KnownHosts = map[string]string{}
	}
	if prev, ok := s.data.KnownHosts[hostname]; ok {
		if prev != fingerprint {
			return fmt.Errorf("host key mismatch for %s: expected %s, got %s", hostname, prev, fingerprint)
		}
		return nil
	}
	s.data.KnownHosts[hostname] = fingerprint
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if s.locked {
		return ErrLocked
	}
	if s.recipient == nil {
		return errors.New("vault has no recipient")
	}
	s.data.Version = 2
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	blob, err := encryptVault(s.secrets, s.recipient)
	if err != nil {
		return err
	}
	if err := writePrivate(s.vaultPath, blob); err != nil {
		return err
	}
	return writePrivate(s.path, b)
}

func writePrivate(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
