package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"filippo.io/age"
)

var ErrNotFound = errors.New("not found")

// UnknownHostError is returned the first time a host presents a key.
type UnknownHostError struct {
	Host        string
	Fingerprint string
}

func (e *UnknownHostError) Error() string {
	return fmt.Sprintf("unknown host key for %s: %s", e.Host, e.Fingerprint)
}

// HostKeyMismatchError is returned when a known host presents a different key.
type HostKeyMismatchError struct {
	Host     string
	Expected string
	Got      string
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("host key mismatch for %s: expected %s, got %s", e.Host, e.Expected, e.Got)
}

type Host struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Group           string   `json:"group"`
	Address         string   `json:"address"`
	Port            int      `json:"port"`
	Username        string   `json:"username"`
	AuthType        string   `json:"authType"` // local | password | key | identity
	IdentityID      string   `json:"identityId,omitempty"`
	Password        string   `json:"password,omitempty"`
	PrivateKey      string   `json:"privateKey,omitempty"`
	Passphrase      string   `json:"passphrase,omitempty"`
	Color           string   `json:"color"`
	KeepAliveSec    int      `json:"keepAliveSec"`
	NoAutoReconnect bool     `json:"noAutoReconnect,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	GroupID         string   `json:"groupId,omitempty"`
	// AgentReadable and Production are the host's agent switches (Edit
	// host → Agent). Reads always set both. On a write, nil means "not
	// sent": an update keeps the saved value, a create takes the default
	// (readable; production only when created in a group named
	// "production", see production.go). Older clients never send them.
	AgentReadable *bool     `json:"agentReadable"`
	Production    *bool     `json:"production"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// ReadableByAgent reports whether the agent may read this host's panes.
// A host that never set it is readable (the default).
func (h Host) ReadableByAgent() bool {
	return h.AgentReadable == nil || *h.AgentReadable
}

// IsProduction reports whether the host is treated as production.
func (h Host) IsProduction() bool {
	return h.Production != nil && *h.Production
}

// Group is a Termius-style parent tree (not a filesystem folder).
// ParentID empty means a root group. Hosts belong to at most one group;
// tags stay orthogonal. v1 does not inherit creds/proxy/theme from parents.
type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MaxGroupDepth is the Termius-like nest cap (6–8). Moves/creates that would
// exceed it return ErrGroupDepth. Upsert also rejects parent cycles.
const MaxGroupDepth = 8

var (
	ErrGroupCycle = errors.New("group cannot nest under itself")
	ErrGroupDepth = errors.New("group nest is deeper than 8")
)

const defaultKeepAlive = 30 * time.Second

// KeepAlive is the SSH keepalive interval. Local shells: off. keepAliveSec 0 → 30s. -1 → off.
func (h Host) KeepAlive() time.Duration {
	if h.AuthType == "local" {
		return 0
	}
	if h.KeepAliveSec < 0 {
		return 0
	}
	if h.KeepAliveSec == 0 {
		return defaultKeepAlive
	}
	return time.Duration(h.KeepAliveSec) * time.Second
}

// AutoReconnect is on for SSH hosts unless noAutoReconnect is set.
func (h Host) AutoReconnect() bool {
	return h.AuthType != "local" && !h.NoAutoReconnect
}

const (
	maxHostTags     = 8
	maxHostTagRunes = 32
)

// NormalizeTags trims, drops empties/# prefixes, de-dupes case-insensitively,
// and caps length so host labels stay small vault metadata.
func NormalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		tag = strings.TrimPrefix(tag, "#")
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if n := utf8.RuneCountInString(tag); n > maxHostTagRunes {
			tag = string([]rune(tag)[:maxHostTagRunes])
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
		if len(out) == maxHostTags {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type Identity struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"` // password | key
	Username   string    `json:"username,omitempty"`
	Password   string    `json:"password,omitempty"`
	PrivateKey string    `json:"privateKey,omitempty"`
	Passphrase string    `json:"passphrase,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Tunnel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	HostID    string    `json:"hostId"`
	Kind      string    `json:"kind"` // local | remote | dynamic
	BindAddr  string    `json:"bindAddr"`
	BindPort  int       `json:"bindPort"`
	DestHost  string    `json:"destHost,omitempty"`
	DestPort  int       `json:"destPort,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Snippet struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"createdAt"`
}

// NotebookCell is source markdown or a command. Outputs live in the terminal
// and must never be stored on the notebook document.
type NotebookCell struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

type Notebook struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Cells     []NotebookCell `json:"cells"`
	Pinned    bool           `json:"pinned,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type persistHost struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Group           string   `json:"group"`
	Address         string   `json:"address"`
	Port            int      `json:"port"`
	Username        string   `json:"username"`
	AuthType        string   `json:"authType"`
	IdentityID      string   `json:"identityId,omitempty"`
	Color           string   `json:"color"`
	KeepAliveSec    int      `json:"keepAliveSec"`
	NoAutoReconnect bool     `json:"noAutoReconnect,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	// AgentOff is stored inverted so a host saved before the field existed
	// (no key) stays readable by the agent. Production has no key until
	// set, so older hosts load as not production.
	AgentOff   bool      `json:"agentOff,omitempty"`
	Production bool      `json:"production,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type persistIdentity struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Username  string    `json:"username,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type persistData struct {
	Version int    `json:"version"`
	Vault   string `json:"vault,omitempty"`
	// FirstRun is set on a store Keel seeds itself and cleared when the
	// first-run screens finish (FinishFirstRun). Files written before it
	// existed lack it, so upgrades never see the first run.
	FirstRun   bool              `json:"firstRun,omitempty"`
	Hosts      []persistHost     `json:"hosts"`
	Identities []persistIdentity `json:"identities"`
	Tunnels    []Tunnel          `json:"tunnels"`
	Snippets   []Snippet         `json:"snippets"`
	Notebooks  []Notebook        `json:"notebooks,omitempty"`
	Groups     []Group           `json:"groups,omitempty"`
	KnownHosts map[string]string `json:"knownHosts"`
	// Agent holds the agent settings. No secret lives here: provider keys
	// are vault secrets (agentkeys.go).
	Agent *AgentSettings `json:"agent,omitempty"`
}

type Store struct {
	path      string
	vaultPath string
	mu        sync.Mutex
	data      persistData
	secrets   map[string]hostSecret
	locked    bool
	needSetup bool
	// fromEnv is set when KEEL_VAULT_PASSPHRASE opened or created the vault
	// at start: a scripted setup, so the first-run screens are skipped.
	fromEnv   bool
	recipient age.Recipient
	// vaultSum and vaultRecipient describe what keel.vault last held, so a
	// save that changes no secret skips re-encrypting it. In passphrase mode
	// that encryption is a full scrypt run under s.mu, which stalled every
	// request for most of a second on each save.
	vaultSum       [32]byte
	vaultRecipient age.Recipient
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
		FirstRun   bool              `json:"firstRun"`
		Hosts      []json.RawMessage `json:"hosts"`
		Identities []persistIdentity `json:"identities"`
		Tunnels    []Tunnel          `json:"tunnels"`
		Snippets   []Snippet         `json:"snippets"`
		Notebooks  []Notebook        `json:"notebooks"`
		Groups     []Group           `json:"groups"`
		KnownHosts map[string]string `json:"knownHosts"`
		Agent      *AgentSettings    `json:"agent"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return loaded{}, fmt.Errorf("parse store: %w", err)
	}
	out := persistData{
		Version:    raw.Version,
		Vault:      raw.Vault,
		FirstRun:   raw.FirstRun,
		Identities: raw.Identities,
		Tunnels:    raw.Tunnels,
		Snippets:   raw.Snippets,
		Notebooks:  raw.Notebooks,
		Groups:     raw.Groups,
		KnownHosts: raw.KnownHosts,
		Agent:      raw.Agent,
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
		var flags struct {
			AgentOff   bool `json:"agentOff"`
			Production bool `json:"production"`
		}
		if err := json.Unmarshal(rawHost, &flags); err != nil {
			return loaded{}, fmt.Errorf("parse host: %w", err)
		}
		h.AgentReadable = boolPtr(!flags.AgentOff)
		h.Production = boolPtr(flags.Production)
		if h.Password != "" || h.PrivateKey != "" || h.Passphrase != "" {
			secrets[h.ID] = hostSecret{
				Password:   h.Password,
				PrivateKey: h.PrivateKey,
				Passphrase: h.Passphrase,
			}
		}
		out.Hosts = append(out.Hosts, persistFrom(h))
	}
	return loaded{data: out, secrets: secrets}, nil
}

func seed() persistData {
	now := time.Now().UTC()
	return persistData{
		Version:  3,
		FirstRun: true,
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
		if err := s.unlockScryptLocked(pass, true); err != nil {
			return err
		}
		s.fromEnv = true
		return nil
	}
	if ident, err := keyringIdentity(); err == nil {
		blob, rerr := os.ReadFile(s.vaultPath)
		if rerr != nil && !os.IsNotExist(rerr) {
			return rerr
		}
		if os.IsNotExist(rerr) {
			s.recipient = ident.Recipient()
			s.data.Vault = vaultX25519
			s.data.Version = 3
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
			s.data.Version = 3
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
	if !s.locked || !s.needSetup {
		return ErrVaultExists
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
	s.data.Version = 3
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
		s.data.Version = 3
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
	if err := s.ensureGroupsLocked(); err != nil {
		return nil, err
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
		IdentityID: h.IdentityID, Color: h.Color,
		KeepAliveSec: h.KeepAliveSec, NoAutoReconnect: h.NoAutoReconnect,
		Tags:          append([]string(nil), h.Tags...),
		AgentReadable: boolPtr(!h.AgentOff),
		Production:    boolPtr(h.Production),
		CreatedAt:     h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	out = s.hydrateGroupLocked(out, h.Group)
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
		IdentityID: h.IdentityID, Color: h.Color,
		KeepAliveSec: h.KeepAliveSec, NoAutoReconnect: h.NoAutoReconnect,
		Tags:          append([]string(nil), h.Tags...),
		AgentReadable: boolPtr(!h.AgentOff),
		Production:    boolPtr(h.Production),
		CreatedAt:     h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	out = s.hydrateGroupLocked(out, h.Group)
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
	h.Tags = NormalizeTags(h.Tags)
	if h.Port == 0 && h.AuthType != "local" {
		h.Port = 22
	}
	if err := s.ensureGroupsLocked(); err != nil {
		return Host{}, err
	}
	gid, err := s.resolveGroupRefLocked(h.GroupID, h.Group)
	if err != nil {
		return Host{}, err
	}
	h.Group = gid
	h.GroupID = gid
	if h.ID == "" {
		s.applyCreateDefaultsLocked(&h)
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
			s.applyUpdateDefaultsLocked(&h, existing)
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
	s.applyCreateDefaultsLocked(&h)
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

func copyHostName(base string, taken []string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Host"
	}
	used := make(map[string]struct{}, len(taken))
	for _, n := range taken {
		used[n] = struct{}{}
	}
	name := base + " (copy)"
	if _, ok := used[name]; !ok {
		return name
	}
	for i := 2; ; i++ {
		name = fmt.Sprintf("%s (copy %d)", base, i)
		if _, ok := used[name]; !ok {
			return name
		}
	}
}

// DuplicateHost clones connection settings (including secrets) into a new host
// named like "api (copy)". The seeded local shell cannot be duplicated.
func (s *Store) DuplicateHost(id string) (Host, error) {
	src, err := s.GetHost(id)
	if err != nil {
		return Host{}, err
	}
	if src.AuthType == "local" {
		return Host{}, errors.New("the local shell host cannot be duplicated")
	}
	hosts, err := s.ListHosts()
	if err != nil {
		return Host{}, err
	}
	names := make([]string, len(hosts))
	for i, h := range hosts {
		names[i] = h.Name
	}
	src.ID = ""
	src.CreatedAt = time.Time{}
	src.UpdatedAt = time.Time{}
	src.Name = copyHostName(src.Name, names)
	return s.UpsertHost(src)
}

func persistFrom(h Host) persistHost {
	gid := strings.TrimSpace(h.GroupID)
	if gid == "" {
		gid = strings.TrimSpace(h.Group)
	}
	return persistHost{
		ID: h.ID, Name: h.Name, Group: gid, Address: h.Address,
		Port: h.Port, Username: h.Username, AuthType: h.AuthType,
		IdentityID: h.IdentityID, Color: h.Color,
		KeepAliveSec: h.KeepAliveSec, NoAutoReconnect: h.NoAutoReconnect,
		Tags:       append([]string(nil), h.Tags...),
		AgentOff:   !h.ReadableByAgent(),
		Production: h.IsProduction(),
		CreatedAt:  h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

func boolPtr(v bool) *bool { return &v }

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
			return &HostKeyMismatchError{Host: hostname, Expected: prev, Got: fingerprint}
		}
		return nil
	}
	return &UnknownHostError{Host: hostname, Fingerprint: fingerprint}
}

func (s *Store) TrustHostKey(hostname, fingerprint string, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	hostname = strings.TrimSpace(hostname)
	fingerprint = strings.TrimSpace(fingerprint)
	if hostname == "" || fingerprint == "" {
		return errors.New("hostname and fingerprint are required")
	}
	if s.data.KnownHosts == nil {
		s.data.KnownHosts = map[string]string{}
	}
	if prev, ok := s.data.KnownHosts[hostname]; ok && prev != fingerprint && !replace {
		return &HostKeyMismatchError{Host: hostname, Expected: prev, Got: fingerprint}
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
	s.data.Version = 3
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	sum, err := secretsSum(s.secrets)
	if err != nil {
		return err
	}
	// Rewrite when the secrets or recipient changed, or when keel.vault is
	// gone: every save used to recreate it, and a missing vault at the next
	// start is read as "no secrets".
	if sum != s.vaultSum || s.recipient != s.vaultRecipient || !fileExists(s.vaultPath) {
		blob, err := encryptVault(s.secrets, s.recipient)
		if err != nil {
			return err
		}
		if err := writePrivate(s.vaultPath, blob); err != nil {
			return err
		}
		s.vaultSum, s.vaultRecipient = sum, s.recipient
	}
	return writePrivate(s.path, b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func secretsSum(secrets map[string]hostSecret) ([32]byte, error) {
	// encoding/json sorts map keys, so equal maps hash equally.
	b, err := json.Marshal(secrets)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// writePrivate replaces path with b (mode 0600) so that a crash or power
// loss leaves either the old file or the new one, never a truncated one:
// a unique temp file in the same directory is written and fsynced, renamed
// over path, and the directory is fsynced so the rename itself is durable.
func writePrivate(path string, b []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	syncDir(dir)
	return nil
}

// syncDir flushes a directory entry change (a rename) to disk. Windows
// cannot open directories for this; NTFS journals the rename itself.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
