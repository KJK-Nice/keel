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
	// MaxAgentChatsBytes bounds the saved conversations' JSON in total: the
	// least recently used go first (the one being saved always stays).
	MaxAgentChatsBytes = 8 << 20
)

var (
	ErrAgentChat = errors.New("invalid agent conversation")
	// ErrChatsUnreadable is keel.chats that cannot be decrypted or parsed
	// (damaged, or written under a key the vault no longer holds). Nothing
	// saved can be read or added until DeleteAllAgentChats starts a fresh
	// file under a new key.
	ErrChatsUnreadable = errors.New("saved agent conversations cannot be read")
)

// AgentChat is one saved conversation.
type AgentChat struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Turns     []AgentChatTurn `json:"turns"`
	// Runbook is the runbook used in this conversation last (runbooks.go,
	// docs/ui-redesign.md §16) and how far it got; NoRunbook is the user's
	// "Don't use", so the agent does not pick one again here.
	Runbook   *AgentChatRunbook `json:"runbook,omitempty"`
	NoRunbook bool              `json:"noRunbook,omitempty"`
}

// AgentChatRunbook is a runbook in use in a conversation: a copy of its
// steps as they were when it started (redacted when they were saved), and
// each step's progress.
type AgentChatRunbook struct {
	// ID names this use; RunbookID the saved runbook.
	ID        string `json:"id"`
	RunbookID string `json:"runbookId"`
	Name      string `json:"name"`
	// PickedBy is "user" or "agent"; Why is the agent's reason, or "".
	PickedBy string `json:"pickedBy"`
	Why      string `json:"why,omitempty"`
	// Status is "active", "finished" or "stopped".
	Status string `json:"status"`
	// Turn is the ask (1-based) that started it.
	Turn      int       `json:"turn"`
	StartedAt time.Time `json:"startedAt"`
	// Hosts are the runbook's folders and tags when it started: a step is
	// aimed at the first pane read on one of them.
	Hosts []string           `json:"hosts,omitempty"`
	Steps []AgentChatRunStep `json:"steps"`
}

// AgentChatRunStep is one step of a runbook in use. State is "next",
// "proposed", "typed", "done", "failed" or "skipped". Result says where an
// end came from: "reported" (the exit status the pane's shell reported
// with OSC 133 marks: a claim, never proof), "user" (marked by the user)
// or "abandoned" (the shell went away before it ended).
type AgentChatRunStep struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Command     string     `json:"command,omitempty"`
	Expect      string     `json:"expect,omitempty"`
	Risk        string     `json:"risk,omitempty"`
	State       string     `json:"state"`
	ProposalID  string     `json:"proposalId,omitempty"`
	SessionID   string     `json:"sessionId,omitempty"`
	HostID      string     `json:"hostId,omitempty"`
	Host        string     `json:"host,omitempty"`
	ProposedAt  *time.Time `json:"proposedAt,omitempty"`
	TypedAt     *time.Time `json:"typedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Exit        *int       `json:"exit,omitempty"`
	Result      string     `json:"result,omitempty"`
}

// Copy returns a deep copy.
func (r *AgentChatRunbook) Copy() *AgentChatRunbook {
	if r == nil {
		return nil
	}
	out := *r
	out.Hosts = append([]string(nil), r.Hosts...)
	out.Steps = make([]AgentChatRunStep, len(r.Steps))
	for i, st := range r.Steps {
		for _, p := range []**time.Time{&st.ProposedAt, &st.TypedAt, &st.FinishedAt} {
			if *p != nil {
				t := **p
				*p = &t
			}
		}
		if st.Exit != nil {
			e := *st.Exit
			st.Exit = &e
		}
		out.Steps[i] = st
	}
	return &out
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
	// Notes are the notes the agent proposed in this turn (agentnotes.go)
	// and what the user did with them.
	Notes []AgentChatNote `json:"notes,omitempty"`
	// Runbooks are the runbooks the agent proposed to save in this turn
	// (runbooks.go) and what the user did with them.
	Runbooks []AgentChatRunbookProposal `json:"runbooks,omitempty"`
	Again    bool                       `json:"again,omitempty"`
}

// AgentChatRunbookProposal is a runbook the agent proposed, redacted, and
// its state: "open", "saved" (RunbookID names it) or "dismissed".
type AgentChatRunbookProposal struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	When       string        `json:"when,omitempty"`
	Steps      []RunbookStep `json:"steps"`
	FromPane   bool          `json:"fromPane,omitempty"`
	SeenIn     string        `json:"seenIn,omitempty"`
	PanesRead  []string      `json:"panesRead,omitempty"`
	Status     string        `json:"status"`
	RunbookID  string        `json:"runbookId,omitempty"`
	ProposedAt time.Time     `json:"proposedAt"`
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
	// Command is how the pane's last command ended ("exit 1") when it was
	// read from shell-integration marks; never the command itself.
	Command string `json:"command,omitempty"`
	// NoteKind ("host" or "user") and Notes are set on a step that sent
	// notes for the agent: whose and how many, never their text.
	NoteKind string `json:"noteKind,omitempty"`
	Notes    int    `json:"notes,omitempty"`
	// Runbook names the runbook a step with Status "runbook" sent.
	Runbook string `json:"runbook,omitempty"`
}

// AgentChatNote is a note the agent proposed, redacted, and its state:
// "open", "saved" (NoteID names the note) or "dismissed".
type AgentChatNote struct {
	ID         string    `json:"id"`
	For        string    `json:"for"`
	HostID     string    `json:"hostId,omitempty"`
	Host       string    `json:"host,omitempty"`
	Text       string    `json:"text"`
	FromPane   bool      `json:"fromPane,omitempty"`
	SeenIn     string    `json:"seenIn,omitempty"`
	PanesRead  []string  `json:"panesRead,omitempty"`
	Status     string    `json:"status"`
	NoteID     string    `json:"noteId,omitempty"`
	ProposedAt time.Time `json:"proposedAt"`
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
	// Step and Steps are set on a runbook step's proposal: its number and
	// how many the runbook has.
	Step  int `json:"step,omitempty"`
	Steps int `json:"steps,omitempty"`
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
// It holds the conversations' key and their decrypted text: anything that
// locks the vault again must drop it with forgetChats.
type chatState struct {
	loaded bool
	ident  *age.X25519Identity
	chats  map[string]AgentChat
}

// forgetChats drops the decrypted conversations and their key from memory.
// The store has no way to lock an open vault today; a future Lock must call
// this (with chatMu free), or the transcripts would stay readable while the
// vault is locked.
func (s *Store) forgetChats() {
	s.chatMu.Lock()
	s.chat = chatState{}
	s.chatMu.Unlock()
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
// replaces it. A file that does not decrypt or parse gives
// ErrChatsUnreadable, every time, until DeleteAllAgentChats replaces it;
// a save never overwrites it, so nothing is lost silently.
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
				return nil, fmt.Errorf("%w: %v", ErrChatsUnreadable, derr)
			}
			plain, rerr := io.ReadAll(r)
			if rerr != nil {
				return nil, fmt.Errorf("%w: %v", ErrChatsUnreadable, rerr)
			}
			var f chatsFileData
			if jerr := json.Unmarshal(plain, &f); jerr != nil {
				return nil, fmt.Errorf("%w: %v", ErrChatsUnreadable, jerr)
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
	sizes := make(map[string]int, len(chats))
	total := 0
	for id, x := range chats {
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		sizes[id] = len(b)
		total += len(b)
	}
	for len(chats) > 1 && (len(chats) > MaxAgentChats || total > MaxAgentChatsBytes) {
		oldest := ""
		for id, x := range chats {
			if id != c.ID && (oldest == "" || x.UpdatedAt.Before(chats[oldest].UpdatedAt)) {
				oldest = id
			}
		}
		total -= sizes[oldest]
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
// many there were (0 when keel.chats was unreadable). It removes keel.chats
// and the key from the vault, so the next save starts a fresh file under a
// new key. This is also how an unreadable keel.chats is replaced. With no
// key and no file it writes nothing.
func (s *Store) DeleteAllAgentChats() (int, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	n := 0
	chats, err := s.chatsLocked()
	switch {
	case err == nil:
		n = len(chats)
	case !errors.Is(err, ErrChatsUnreadable):
		return 0, err
	}
	s.mu.Lock()
	if s.locked.Load() {
		s.mu.Unlock()
		return 0, ErrLocked
	}
	if prev, ok := s.secrets[chatKeySecretID]; ok {
		delete(s.secrets, chatKeySecretID)
		if err := s.saveLocked(); err != nil {
			s.secrets[chatKeySecretID] = prev
			s.mu.Unlock()
			return 0, err
		}
	}
	s.mu.Unlock()
	s.chat = chatState{loaded: true, chats: map[string]AgentChat{}}
	if err := os.Remove(s.chatsPath()); err != nil && !os.IsNotExist(err) {
		return n, err
	}
	syncDir(filepath.Dir(s.chatsPath()))
	return n, nil
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
