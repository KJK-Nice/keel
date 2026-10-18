package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"filippo.io/age"
)

// Notes for the agent (docs/ui-redesign.md §15): host notes, about one
// saved host, and the user's own notes, about how they like to work.
//
// They live in keel.memory, next to keel.vault, encrypted with age to an
// X25519 key that is itself a vault secret (memoryKeySecretID), the same
// way as the saved conversations in keel.chats (agentchats.go): readable
// only while the vault is open, never in keel.json, and a save re-encrypts
// keel.memory alone (no scrypt run per note in passphrase mode). It is a
// separate file under a separate key so that Delete all history and the
// conversation retention, which remove keel.chats and its key, never take
// the notes with them. The file has room for the runbooks of the next phase
// (another list beside Notes, same key).
//
// The store keeps what it is handed. The agent service redacts a note's
// text (vault secrets, secret-looking values) before it calls AddAgentNote
// or UpdateAgentNote, and again, with the request's Redactor, every time a
// note is sent.

const (
	memoryKeySecretID = "agentmemory:key"
	memoryFile        = "keel.memory"

	// MaxAgentNoteBytes is the longest note, in bytes, on one line.
	MaxAgentNoteBytes = 1000
	// MaxAgentNotesPerHost bounds a host's notes, and MaxUserNotes the
	// user's own.
	MaxAgentNotesPerHost = 50
	MaxUserNotes         = 50

	// NoteSourceUser is a note the user wrote; NoteSourceAgent one the
	// agent proposed and the user saved.
	NoteSourceUser  = "user"
	NoteSourceAgent = "agent"
)

var (
	ErrAgentNote = errors.New("invalid note")
	// ErrNotesFull is a host (or the user's notes) at its limit.
	ErrNotesFull = errors.New("too many notes")
	// ErrMemoryUnreadable is keel.memory that cannot be decrypted or
	// parsed. Nothing can be read or added until DeleteAllAgentNotes
	// starts a fresh file under a new key; a save never overwrites it.
	ErrMemoryUnreadable = errors.New("saved notes cannot be read")
)

// AgentNote is one note for the agent. HostID is empty for the user's own
// notes, which are about the user and apply on every host.
type AgentNote struct {
	ID     string `json:"id"`
	HostID string `json:"hostId,omitempty"`
	Text   string `json:"text"`
	// Source is NoteSourceUser or NoteSourceAgent (proposed by the agent,
	// saved by the user).
	Source string `json:"source"`
	// FromPane is set on an agent note whose text came from terminal
	// output (untrusted remote text); SeenIn names the host of that pane.
	FromPane bool   `json:"fromPane,omitempty"`
	SeenIn   string `json:"seenIn,omitempty"`
	// Edited is set once the user changed the text of an agent note.
	Edited    bool      `json:"edited,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// memoryFileData is keel.memory decrypted: the notes and the runbooks
// (runbooks.go), under one key. Both lists are written on every save, so a
// change to one never drops the other.
type memoryFileData struct {
	Version  int         `json:"version"`
	Notes    []AgentNote `json:"notes"`
	Runbooks []Runbook   `json:"runbooks,omitempty"`
}

// memoryState is keel.memory decrypted, loaded on first use (under memMu).
// Anything that locks the vault again must drop it with forgetMemory.
type memoryState struct {
	loaded bool
	ident  *age.X25519Identity
	notes  []AgentNote
	books  []Runbook
}

func (s *Store) forgetMemory() {
	s.memMu.Lock()
	s.mem = memoryState{}
	s.memMu.Unlock()
}

func (s *Store) memoryPath() string {
	return filepath.Join(filepath.Dir(s.path), memoryFile)
}

// memoryKey returns the notes' key from the vault; with create, a missing
// key is made (one vault write).
func (s *Store) memoryKey(create bool) (*age.X25519Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	if sec, ok := s.secrets[memoryKeySecretID]; ok && sec.PrivateKey != "" {
		if id, err := age.ParseX25519Identity(sec.PrivateKey); err == nil {
			return id, nil
		}
	}
	if !create {
		return nil, nil
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	s.secrets[memoryKeySecretID] = hostSecret{PrivateKey: id.String()}
	if err := s.saveLocked(); err != nil {
		delete(s.secrets, memoryKeySecretID)
		return nil, err
	}
	return id, nil
}

// memoryLocked loads keel.memory once the vault is open. Call with memMu
// held. A file that does not decrypt or parse gives ErrMemoryUnreadable
// every time until DeleteAllAgentNotes replaces it.
func (s *Store) memoryLocked() ([]AgentNote, error) {
	if s.locked.Load() {
		return nil, ErrLocked
	}
	if s.mem.loaded {
		return s.mem.notes, nil
	}
	ident, err := s.memoryKey(false)
	if err != nil {
		return nil, err
	}
	var (
		notes []AgentNote
		books []Runbook
	)
	if ident != nil {
		blob, err := os.ReadFile(s.memoryPath())
		switch {
		case err == nil:
			r, derr := age.Decrypt(bytes.NewReader(blob), ident)
			if derr != nil {
				return nil, fmt.Errorf("%w: %v", ErrMemoryUnreadable, derr)
			}
			plain, rerr := io.ReadAll(r)
			if rerr != nil {
				return nil, fmt.Errorf("%w: %v", ErrMemoryUnreadable, rerr)
			}
			var f memoryFileData
			if jerr := json.Unmarshal(plain, &f); jerr != nil {
				return nil, fmt.Errorf("%w: %v", ErrMemoryUnreadable, jerr)
			}
			for _, n := range f.Notes {
				if n.ID != "" {
					notes = append(notes, n)
				}
			}
			for _, b := range f.Runbooks {
				if b.ID != "" {
					books = append(books, b)
				}
			}
		case !os.IsNotExist(err):
			return nil, err
		}
	}
	s.mem = memoryState{loaded: true, ident: ident, notes: notes, books: books}
	return notes, nil
}

// writeMemoryLocked writes keel.memory with notes and the runbooks held
// now, making the key first if there is none, and keeps notes in memory
// once it is written. Call with memMu held, after memoryLocked.
func (s *Store) writeMemoryLocked(notes []AgentNote) error {
	return s.writeMemoryAllLocked(notes, s.mem.books)
}

// writeMemoryAllLocked writes keel.memory with notes and books and keeps
// both in memory once it is written. Call with memMu held, after
// memoryLocked.
func (s *Store) writeMemoryAllLocked(notes []AgentNote, books []Runbook) error {
	if s.mem.ident == nil {
		ident, err := s.memoryKey(true)
		if err != nil {
			return err
		}
		s.mem.ident = ident
	}
	plain, err := json.Marshal(memoryFileData{Version: 1, Notes: notes, Runbooks: books})
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, s.mem.ident.Recipient())
	if err != nil {
		return err
	}
	if _, err := w.Write(plain); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := writePrivate(s.memoryPath(), buf.Bytes()); err != nil {
		return err
	}
	s.mem.notes, s.mem.books = notes, books
	return nil
}

// ListAgentNotes returns every note: the user's own first, then host notes
// by host, each oldest first.
func (s *Store) ListAgentNotes() ([]AgentNote, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return nil, err
	}
	out := append([]AgentNote(nil), notes...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HostID != out[j].HostID {
			return out[i].HostID < out[j].HostID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// AddAgentNote saves a new note. Its text is cleaned to one line; the host
// must exist (for a host note), and the host or the user's notes must be
// under their limit.
func (s *Store) AddAgentNote(n AgentNote) (AgentNote, error) {
	text, ok := CleanAgentNote(n.Text)
	if !ok {
		return AgentNote{}, fmt.Errorf("%w: a note is 1 to %d bytes", ErrAgentNote, MaxAgentNoteBytes)
	}
	if n.Source != NoteSourceAgent {
		n.Source, n.FromPane, n.SeenIn, n.Edited = NoteSourceUser, false, "", false
	}
	n.SeenIn = CleanAgentChatTitle(n.SeenIn)
	s.memMu.Lock()
	defer s.memMu.Unlock()
	if n.HostID != "" && !s.hostExists(n.HostID) {
		return AgentNote{}, ErrNotFound
	}
	notes, err := s.memoryLocked()
	if err != nil {
		return AgentNote{}, err
	}
	count := 0
	for _, x := range notes {
		if x.HostID == n.HostID {
			count++
		}
	}
	limit := MaxAgentNotesPerHost
	if n.HostID == "" {
		limit = MaxUserNotes
	}
	if count >= limit {
		return AgentNote{}, fmt.Errorf("%w: at most %d", ErrNotesFull, limit)
	}
	now := time.Now().UTC()
	n.ID, n.Text, n.CreatedAt, n.UpdatedAt = "n_"+newID(), text, now, now
	next := append(append([]AgentNote(nil), notes...), n)
	if err := s.writeMemoryLocked(next); err != nil {
		return AgentNote{}, err
	}
	return n, nil
}

// UpdateAgentNote replaces a note's text.
func (s *Store) UpdateAgentNote(id, text string) (AgentNote, error) {
	text, ok := CleanAgentNote(text)
	if !ok {
		return AgentNote{}, fmt.Errorf("%w: a note is 1 to %d bytes", ErrAgentNote, MaxAgentNoteBytes)
	}
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return AgentNote{}, err
	}
	next := append([]AgentNote(nil), notes...)
	for i, n := range next {
		if n.ID != id {
			continue
		}
		if n.Text == text {
			return n, nil
		}
		n.Text, n.UpdatedAt = text, time.Now().UTC()
		if n.Source == NoteSourceAgent {
			n.Edited = true
		}
		next[i] = n
		if err := s.writeMemoryLocked(next); err != nil {
			return AgentNote{}, err
		}
		return n, nil
	}
	return AgentNote{}, ErrNotFound
}

// DeleteAgentNote removes one note and returns it.
func (s *Store) DeleteAgentNote(id string) (AgentNote, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return AgentNote{}, err
	}
	next := make([]AgentNote, 0, len(notes))
	var gone *AgentNote
	for _, n := range notes {
		if n.ID == id {
			n := n
			gone = &n
			continue
		}
		next = append(next, n)
	}
	if gone == nil {
		return AgentNote{}, ErrNotFound
	}
	return *gone, s.writeMemoryLocked(next)
}

// deleteHostNotes removes a deleted host's notes. A keel.memory that cannot
// be read is left as it is.
func (s *Store) deleteHostNotes(hostID string) error {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return err
	}
	next := make([]AgentNote, 0, len(notes))
	for _, n := range notes {
		if n.HostID != hostID {
			next = append(next, n)
		}
	}
	if len(next) == len(notes) {
		return nil
	}
	return s.writeMemoryLocked(next)
}

// DeleteAllAgentNotes removes every note and returns how many there were
// (0 when keel.memory was unreadable). With no runbooks saved it removes
// keel.memory and its key from the vault, which is also how an unreadable
// file is replaced (its runbooks, unreadable too, go with it); with
// runbooks it writes the file again without notes, so the runbooks stay.
func (s *Store) DeleteAllAgentNotes() (int, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	n := 0
	notes, err := s.memoryLocked()
	switch {
	case err == nil:
		n = len(notes)
		if len(s.mem.books) > 0 {
			return n, s.writeMemoryAllLocked(nil, s.mem.books)
		}
	case !errors.Is(err, ErrMemoryUnreadable):
		return 0, err
	}
	s.mu.Lock()
	if s.locked.Load() {
		s.mu.Unlock()
		return 0, ErrLocked
	}
	if prev, ok := s.secrets[memoryKeySecretID]; ok {
		delete(s.secrets, memoryKeySecretID)
		if err := s.saveLocked(); err != nil {
			s.secrets[memoryKeySecretID] = prev
			s.mu.Unlock()
			return 0, err
		}
	}
	s.mu.Unlock()
	s.mem = memoryState{loaded: true}
	if err := os.Remove(s.memoryPath()); err != nil && !os.IsNotExist(err) {
		return n, err
	}
	syncDir(filepath.Dir(s.memoryPath()))
	return n, nil
}

func (s *Store) hostExists(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.data.Hosts {
		if h.ID == id {
			return true
		}
	}
	return false
}

// CleanAgentNote makes a note one line with no control characters and
// single spaces, and reports whether it is 1 to MaxAgentNoteBytes bytes.
// One line keeps a note from drawing its own frame lines when it is sent.
func CleanAgentNote(t string) (string, bool) {
	if !utf8.ValidString(t) {
		return "", false
	}
	t = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, t)
	t = strings.Join(strings.Fields(t), " ")
	if t == "" || len(t) > MaxAgentNoteBytes {
		return "", false
	}
	return t, true
}
