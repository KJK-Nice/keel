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

// Saved agent conversations (docs/ui-redesign.md §5.4.1).
//
// They live in keel.chats, next to keel.vault, encrypted with age to an
// X25519 key that is itself a vault secret (chatKeySecretID). So the
// transcripts can only be read while the vault is unlocked, like the host
// passwords, but saving one does not re-encrypt keel.vault (a second of
// scrypt per save in passphrase mode): the key is written to the vault once,
// and each save encrypts only keel.chats, with the fast X25519 recipient.
// keel.json never holds any of it.
//
// What is stored is what the agent service hands over: the questions and
// replies as they were sent and received, redacted (the provider saw the
// same text), the trace (host names, line counts, redaction counts, never
// pane text) and the proposals. The store adds no text of its own and
// keeps nothing else.

const (
	chatKeySecretID = "agentchats:key"
	chatsFile       = "keel.chats"

	// MaxAgentChats bounds keel.chats: the oldest conversations go first.
	MaxAgentChats = 200
	// MaxAgentChatTitle is the longest title, in bytes.
	MaxAgentChatTitle = 120
)

var ErrAgentChat = errors.New("invalid agent conversation")

// AgentChat is one saved conversation.
type AgentChat struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Turns     []AgentChatTurn `json:"turns"`
}

// AgentChatTurn is one question and its answer, or (Again) a proposal the
// user asked to see again.
type AgentChatTurn struct {
	At time.Time `json:"at"`
	// Question is the user's question as it was sent: redacted.
	Question string `json:"question,omitempty"`
	// Answer is the reply shown in the panel, redacted.
	Answer string `json:"answer,omitempty"`
	// Model is the model's whole reply as kept for the next ask's context
	// (redacted, truncated).
	Model     string              `json:"model,omitempty"`
	Trace     []AgentChatStep     `json:"trace,omitempty"`
	Proposals []AgentChatProposal `json:"proposals,omitempty"`
	Again     bool                `json:"again,omitempty"`
}

// AgentChatStep is one line of a turn's trace. It names a pane and what
// was done with it; it never holds the pane's text.
type AgentChatStep struct {
	SessionID  string         `json:"sessionId,omitempty"`
	HostID     string         `json:"hostId,omitempty"`
	Host       string         `json:"host"`
	Status     string         `json:"status"`
	Reason     string         `json:"reason,omitempty"`
	Lines      int            `json:"lines,omitempty"`
	Redactions map[string]int `json:"redactions,omitempty"`
}

// AgentChatProposal is a command the agent proposed, as it was last shown.
type AgentChatProposal struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId,omitempty"`
	HostID     string     `json:"hostId,omitempty"`
	Host       string     `json:"host,omitempty"`
	Production bool       `json:"production,omitempty"`
	Command    string     `json:"command"`
	Why        string     `json:"why,omitempty"`
	Risk       string     `json:"risk"`
	Edited     bool       `json:"edited,omitempty"`
	ProposedAt time.Time  `json:"proposedAt"`
	TypedAt    *time.Time `json:"typedAt,omitempty"`
}

// AgentChatSummary is a conversation as the list shows it.
type AgentChatSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Hosts are the hosts the conversation read or proposed for, in first
	// seen order.
	Hosts     []string `json:"hosts"`
	Turns     int      `json:"turns"`
	Proposals int      `json:"proposals"`
}

// Summary lists c the way the session list shows it.
func (c AgentChat) Summary() AgentChatSummary {
	out := AgentChatSummary{ID: c.ID, Title: c.Title, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Hosts: []string{}}
	seen := map[string]bool{}
	add := func(h string) {
		if h != "" && !seen[h] {
			seen[h] = true
			out.Hosts = append(out.Hosts, h)
		}
	}
	for _, t := range c.Turns {
		if !t.Again {
			out.Turns++
		}
		for _, st := range t.Trace {
			if st.Status == "read" {
				add(st.Host)
			}
		}
		for _, p := range t.Proposals {
			add(p.Host)
		}
		out.Proposals += len(t.Proposals)
	}
	return out
}

type chatsFileData struct {
	Version int         `json:"version"`
	Chats   []AgentChat `json:"chats"`
}

// chatState is keel.chats decrypted, loaded on first use (under chatMu).
type chatState struct {
	loaded bool
	ident  *age.X25519Identity
	chats  map[string]AgentChat
}

func (s *Store) chatsPath() string {
	return filepath.Join(filepath.Dir(s.path), chatsFile)
}

// chatKey returns the conversations' key from the vault. With create, a
// missing key is made (one vault write); without, a missing key is nil.
func (s *Store) chatKey(create bool) (*age.X25519Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	if sec, ok := s.secrets[chatKeySecretID]; ok && sec.PrivateKey != "" {
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
	s.secrets[chatKeySecretID] = hostSecret{PrivateKey: id.String()}
	if err := s.saveLocked(); err != nil {
		delete(s.secrets, chatKeySecretID)
		return nil, err
	}
	return id, nil
}

// chatsLocked loads keel.chats once the vault is open. Call with chatMu
// held. With no key in the vault there is nothing to read: a keel.chats
// left from a key that is gone cannot be opened, and the next save
// replaces it.
func (s *Store) chatsLocked() (map[string]AgentChat, error) {
	if s.chat.loaded {
		return s.chat.chats, nil
	}
	ident, err := s.chatKey(false)
	if err != nil {
		return nil, err
	}
	chats := map[string]AgentChat{}
	if ident != nil {
		blob, err := os.ReadFile(s.chatsPath())
		switch {
		case err == nil:
			r, derr := age.Decrypt(bytes.NewReader(blob), ident)
			if derr != nil {
				return nil, fmt.Errorf("keel.chats: %w", derr)
			}
			plain, rerr := io.ReadAll(r)
			if rerr != nil {
				return nil, fmt.Errorf("keel.chats: %w", rerr)
			}
			var f chatsFileData
			if jerr := json.Unmarshal(plain, &f); jerr != nil {
				return nil, fmt.Errorf("keel.chats: %w", jerr)
			}
			for _, c := range f.Chats {
				if c.ID != "" {
					chats[c.ID] = c
				}
			}
		case !os.IsNotExist(err):
			return nil, err
		}
	}
	s.chat = chatState{loaded: true, ident: ident, chats: chats}
	return chats, nil
}

// writeChatsLocked writes keel.chats, making the key first if there is
// none. Call with chatMu held.
func (s *Store) writeChatsLocked() error {
	if s.chat.ident == nil {
		ident, err := s.chatKey(true)
		if err != nil {
			return err
		}
		s.chat.ident = ident
	}
	list := make([]AgentChat, 0, len(s.chat.chats))
	for _, c := range s.chat.chats {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	plain, err := json.Marshal(chatsFileData{Version: 1, Chats: list})
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, s.chat.ident.Recipient())
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
	return writePrivate(s.chatsPath(), buf.Bytes())
}

// ListAgentChats returns the saved conversations, most recently used
// first.
func (s *Store) ListAgentChats() ([]AgentChatSummary, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]AgentChatSummary, 0, len(chats))
	for _, c := range chats {
		out = append(out, c.Summary())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// GetAgentChat returns one saved conversation.
func (s *Store) GetAgentChat(id string) (AgentChat, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return AgentChat{}, err
	}
	c, ok := chats[id]
	if !ok {
		return AgentChat{}, ErrNotFound
	}
	return c, nil
}

// PutAgentChat saves c whole, replacing any saved one with its id. When
// more than MaxAgentChats are saved, the least recently updated go.
func (s *Store) PutAgentChat(c AgentChat) error {
	if c.ID == "" || len(c.ID) > 64 {
		return ErrAgentChat
	}
	c.Title = CleanAgentChatTitle(c.Title)
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return err
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = c.UpdatedAt
	}
	chats[c.ID] = c
	for len(chats) > MaxAgentChats {
		oldest := ""
		for id, x := range chats {
			if oldest == "" || x.UpdatedAt.Before(chats[oldest].UpdatedAt) {
				oldest = id
			}
		}
		delete(chats, oldest)
	}
	return s.writeChatsLocked()
}

// RenameAgentChat sets a saved conversation's title. It does not change
// when the conversation was last used.
func (s *Store) RenameAgentChat(id, title string) (AgentChatSummary, error) {
	title = CleanAgentChatTitle(title)
	if title == "" {
		return AgentChatSummary{}, fmt.Errorf("%w: a title is 1 to %d bytes on one line", ErrAgentChat, MaxAgentChatTitle)
	}
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return AgentChatSummary{}, err
	}
	c, ok := chats[id]
	if !ok {
		return AgentChatSummary{}, ErrNotFound
	}
	c.Title = title
	chats[id] = c
	return c.Summary(), s.writeChatsLocked()
}

// DeleteAgentChat removes one saved conversation. Deleting one that is not
// saved is not an error.
func (s *Store) DeleteAgentChat(id string) error {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return err
	}
	if _, ok := chats[id]; !ok {
		return nil
	}
	delete(chats, id)
	return s.writeChatsLocked()
}

// DeleteAllAgentChats removes every saved conversation and returns how
// many there were.
func (s *Store) DeleteAllAgentChats() (int, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return 0, err
	}
	n := len(chats)
	s.chat.chats = map[string]AgentChat{}
	if n == 0 {
		return 0, nil
	}
	return n, s.writeChatsLocked()
}

// PruneAgentChats removes the conversations last used before cutoff and
// returns their ids.
func (s *Store) PruneAgentChats(cutoff time.Time) ([]string, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	chats, err := s.chatsLocked()
	if err != nil {
		return nil, err
	}
	var gone []string
	for id, c := range chats {
		if c.UpdatedAt.Before(cutoff) {
			gone = append(gone, id)
		}
	}
	if len(gone) == 0 {
		return nil, nil
	}
	for _, id := range gone {
		delete(chats, id)
	}
	sort.Strings(gone)
	return gone, s.writeChatsLocked()
}

// CleanAgentChatTitle makes a title one line of at most MaxAgentChatTitle
// bytes with no control characters.
func CleanAgentChatTitle(t string) string {
	t = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, t)
	t = strings.Join(strings.Fields(t), " ")
	if len(t) > MaxAgentChatTitle {
		cut := MaxAgentChatTitle - len("…")
		for cut > 0 && !utf8.RuneStart(t[cut]) {
			cut--
		}
		t = strings.TrimSpace(t[:cut]) + "…"
	}
	return t
}
