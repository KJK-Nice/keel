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

type Data struct {
	Hosts      []Host            `json:"hosts"`
	Snippets   []Snippet         `json:"snippets"`
	KnownHosts map[string]string `json:"knownHosts"`
}

type Store struct {
	path string
	mu   sync.Mutex
	data Data
}

func New(path string) (*Store, error) {
	s := &Store{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		s.data = seed()
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("parse store: %w", err)
	}
	if s.data.KnownHosts == nil {
		s.data.KnownHosts = map[string]string{}
	}
	return s, nil
}

func seed() Data {
	now := time.Now().UTC()
	return Data{
		Hosts: []Host{{
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

func (s *Store) ListHosts() []Host {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Host, len(s.data.Hosts))
	copy(out, s.data.Hosts)
	return out
}

func (s *Store) GetHost(id string) (Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.data.Hosts {
		if h.ID == id {
			return h, nil
		}
	}
	return Host{}, ErrNotFound
}

func (s *Store) UpsertHost(h Host) (Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if h.Port == 0 && h.AuthType != "local" {
		h.Port = 22
	}
	if h.ID == "" {
		h.ID = newID()
		h.CreatedAt = now
		h.UpdatedAt = now
		s.data.Hosts = append(s.data.Hosts, h)
		return h, s.saveLocked()
	}
	for i, existing := range s.data.Hosts {
		if existing.ID == h.ID {
			h.CreatedAt = existing.CreatedAt
			h.UpdatedAt = now
			if h.Password == "" {
				h.Password = existing.Password
			}
			if h.PrivateKey == "" {
				h.PrivateKey = existing.PrivateKey
			}
			if h.Passphrase == "" {
				h.Passphrase = existing.Passphrase
			}
			s.data.Hosts[i] = h
			return h, s.saveLocked()
		}
	}
	h.CreatedAt = now
	h.UpdatedAt = now
	s.data.Hosts = append(s.data.Hosts, h)
	return h, s.saveLocked()
}

func (s *Store) DeleteHost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return s.saveLocked()
}

func (s *Store) ListSnippets() []Snippet {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Snippet, len(s.data.Snippets))
	copy(out, s.data.Snippets)
	return out
}

func (s *Store) UpsertSnippet(sn Snippet) (Snippet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
