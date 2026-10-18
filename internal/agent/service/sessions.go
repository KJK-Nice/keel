package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/applog"
	"keel/internal/store"
)

// Saved conversations (docs/ui-redesign.md §5.4.1).
//
// A conversation is saved in the vault (store.PutAgentChat, keel.chats)
// after each answer, when Settings · Agent keeps history (historyDays > 0).
// What is saved is what already went through the request's Redactor: the
// question as sent, the reply as received, the trace (host names and
// counts, never pane text) and the proposals, whose commands go through
// the Redactor once more so a vault secret typed into an edit is not kept.
//
// Resuming one restores its question and reply history for the next ask's
// context; the next ask reads the panes in scope again, through the
// Auditor and the scope rules as they are now. No pane text is ever kept,
// so none can be replayed.
//
// A saved proposal is not typeable: only proposals the service holds in
// memory from this run are (AuthorizeType looks them up there). A
// transcript marks every other one Expired, and the panel offers Copy and
// Propose again, which rates the command again and makes a new proposal
// that goes through the normal approve-before-type path.

// ChatStore is where conversations are saved. *store.Store satisfies it.
type ChatStore interface {
	ListAgentChats() ([]store.AgentChatSummary, error)
	GetAgentChat(id string) (store.AgentChat, error)
	PutAgentChat(c store.AgentChat) error
	RenameAgentChat(id, title string) (store.AgentChatSummary, error)
	DeleteAgentChat(id string) error
	DeleteAllAgentChats() (int, error)
	PruneAgentChats(cutoff time.Time) ([]string, error)
}

const (
	// maxSavedTurns bounds one saved conversation; the oldest turns go.
	maxSavedTurns = 100
	// maxTitleFromQuestion is how much of the first question becomes the
	// title.
	maxTitleFromQuestion = 80
)

// Transcript is a conversation as the panel shows it on resume.
type Transcript struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Saved     bool             `json:"saved"`
	Turns     []TranscriptTurn `json:"turns"`
	// Runbook is the runbook used last in the conversation and how far it
	// got; NoRunbook is the user's "Don't use" (runbooks.go).
	Runbook   *store.AgentChatRunbook `json:"runbook,omitempty"`
	NoRunbook bool                    `json:"noRunbook,omitempty"`
}

// TranscriptTurn is one saved turn.
type TranscriptTurn struct {
	At        time.Time         `json:"at"`
	Question  string            `json:"question,omitempty"`
	Answer    string            `json:"answer,omitempty"`
	Trace     []TraceStep       `json:"trace"`
	Proposals []Proposal        `json:"proposals"`
	Notes     []NoteProposal    `json:"notes"`
	Runbooks  []RunbookProposal `json:"runbooks"`
	Again     bool              `json:"again,omitempty"`
}

// Sessions is the session list: the saved conversations, after removing
// the ones past the retention. With history off (historyDays 0) nothing is
// saved and the list is empty.
type Sessions struct {
	HistoryDays int                      `json:"historyDays"`
	Sessions    []store.AgentChatSummary `json:"sessions"`
	// SaveError says why conversations are not being saved now (the last
	// save failed, or keel.chats cannot be read); empty when saving works.
	SaveError string `json:"saveError,omitempty"`
	// Unreadable is set when keel.chats cannot be read: nothing saved can
	// be listed until Delete all replaces it with a fresh file.
	Unreadable bool `json:"unreadable,omitempty"`
}

// saving reports whether conversations are saved now.
func (s *Service) saving() (int, bool) {
	if s.chats == nil {
		return 0, false
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return 0, false
	}
	return set.HistoryDays, set.HistoryDays > 0
}

func retentionCutoff(now time.Time, days int) time.Time {
	if days <= 0 {
		// "Only while open": nothing saved is kept.
		return time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return now.Add(-time.Duration(days) * 24 * time.Hour)
}

// Prune removes the saved conversations last used before the retention
// (historyDays) and returns how many went. keel serve calls it at start,
// hourly and when the settings change; the session list calls it too.
// With history off ("only while open") it deletes everything saved, the
// file and its key with it (store.DeleteAllAgentChats), which also
// replaces a keel.chats that cannot be read.
func (s *Service) Prune() (int, error) {
	if s.chats == nil {
		return 0, nil
	}
	// Settings are read under saveMu: a prune that read "off" before
	// history was turned back on must not wipe saves made after it.
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	set, err := s.st.AgentSettings()
	if err != nil {
		return 0, err
	}
	if set.HistoryDays <= 0 {
		return s.chats.DeleteAllAgentChats()
	}
	gone, err := s.chats.PruneAgentChats(retentionCutoff(s.now(), set.HistoryDays))
	return len(gone), err
}

// ListSessions saves what is pending, prunes, then lists the saved
// conversations. An unreadable keel.chats is a state, not an error: the
// list is empty and says so, and Delete all is the way out.
func (s *Service) ListSessions() (Sessions, error) {
	days, on := s.saving()
	out := Sessions{HistoryDays: days, Sessions: []store.AgentChatSummary{}}
	if s.chats == nil {
		return out, nil
	}
	s.FlushSaves()
	_, err := s.Prune()
	if err == nil && on {
		var list []store.AgentChatSummary
		if list, err = s.chats.ListAgentChats(); err == nil {
			out.Sessions = list
		}
	}
	if errors.Is(err, store.ErrChatsUnreadable) {
		out.Unreadable = true
		out.SaveError = saveErrorText(err)
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.SaveError = s.lastSaveError()
	return out, nil
}

// savedChat returns a saved conversation unless it is past the retention.
func (s *Service) savedChat(id string) (store.AgentChat, error) {
	days, on := s.saving()
	if !on {
		return store.AgentChat{}, ErrNotFound
	}
	c, err := s.chats.GetAgentChat(id)
	if errors.Is(err, store.ErrNotFound) {
		return store.AgentChat{}, ErrNotFound
	}
	if err != nil {
		return store.AgentChat{}, err
	}
	if c.UpdatedAt.Before(retentionCutoff(s.now(), days)) {
		return store.AgentChat{}, ErrNotFound
	}
	return c, nil
}

// Transcript returns a conversation for the panel: from memory when keel
// serve holds it (so it works with history off too), else from the vault.
// Proposals that are not live in memory are Expired.
func (s *Service) Transcript(id string) (Transcript, error) {
	if !convIDRe.MatchString(id) {
		return Transcript{}, ErrNotFound
	}
	s.mu.Lock()
	c := s.convs[id]
	var (
		chat      store.AgentChat
		live      map[string]*Proposal
		liveNotes map[string]NoteProposal
		liveBooks map[string]RunbookProposal
	)
	if c != nil && len(c.record) > 0 {
		chat = c.snapshotLocked(s.now())
		chat.UpdatedAt = c.updated
		live = make(map[string]*Proposal, len(c.proposals))
		for _, p := range c.proposals {
			cp := *p
			live[p.ID] = &cp
		}
		liveNotes = make(map[string]NoteProposal, len(c.notes))
		for _, n := range c.notes {
			liveNotes[n.ID] = *n
		}
		liveBooks = make(map[string]RunbookProposal, len(c.bookProps))
		for _, b := range c.bookProps {
			liveBooks[b.ID] = *b
		}
	}
	s.mu.Unlock()
	_, saved := s.saving()
	if chat.ID == "" {
		var err error
		if chat, err = s.savedChat(id); err != nil {
			return Transcript{}, err
		}
	}
	out := Transcript{ID: chat.ID, Title: chat.Title, CreatedAt: chat.CreatedAt, UpdatedAt: chat.UpdatedAt, Saved: saved, Turns: make([]TranscriptTurn, 0, len(chat.Turns)), Runbook: chat.Runbook.Copy(), NoRunbook: chat.NoRunbook}
	prodCache := map[string]bool{}
	for _, t := range chat.Turns {
		tt := TranscriptTurn{At: t.At, Question: t.Question, Answer: t.Answer, Again: t.Again, Trace: make([]TraceStep, 0, len(t.Trace)), Proposals: make([]Proposal, 0, len(t.Proposals)), Notes: make([]NoteProposal, 0, len(t.Notes)), Runbooks: make([]RunbookProposal, 0, len(t.Runbooks))}
		for _, st := range t.Trace {
			tt.Trace = append(tt.Trace, TraceStep{SessionID: st.SessionID, HostID: st.HostID, Host: st.Host, Status: st.Status, Reason: st.Reason, Lines: st.Lines, Redactions: st.Redactions, Command: st.Command, NoteKind: st.NoteKind, Notes: st.Notes, Runbook: st.Runbook})
		}
		for _, sb := range t.Runbooks {
			if b, ok := liveBooks[sb.ID]; ok {
				tt.Runbooks = append(tt.Runbooks, b)
				continue
			}
			tt.Runbooks = append(tt.Runbooks, runbookProposalFromRecord(sb))
		}
		for _, sn := range t.Notes {
			if n, ok := liveNotes[sn.ID]; ok {
				tt.Notes = append(tt.Notes, n)
				continue
			}
			tt.Notes = append(tt.Notes, noteFromRecord(sn))
		}
		for _, sp := range t.Proposals {
			if p, ok := live[sp.ID]; ok {
				tt.Proposals = append(tt.Proposals, *p)
				continue
			}
			tt.Proposals = append(tt.Proposals, s.expiredView(sp, prodCache))
		}
		out.Turns = append(out.Turns, tt)
	}
	return out, nil
}

// expiredView shows a saved proposal that can no longer be typed.
func (s *Service) expiredView(sp store.AgentChatProposal, prodCache map[string]bool) Proposal {
	risk := agent.Stricter(agent.Classify(sp.Command), agent.Risk(sp.Risk))
	prod, ok := prodCache[sp.HostID]
	if !ok {
		prod, _ = s.hostProduction(sp.HostID)
		prodCache[sp.HostID] = prod
	}
	prod = prod || sp.Production
	at := sp.ProposedAt
	out := Proposal{
		ID: sp.ID, SessionID: sp.SessionID, HostID: sp.HostID, Host: sp.Host, Production: prod,
		Command: sp.Command, Bytes: typedBytes(sp.Command), Why: sp.Why, Risk: risk,
		NeedsShift: agent.NeedsShift(risk, prod), Placeholders: hasPlaceholder(sp.Command), Edited: sp.Edited,
		ProposedAt: &at, TypedAt: sp.TypedAt, Expired: true,
	}
	if sp.Step > 0 {
		out.Runbook = &ProposalStep{Step: sp.Step, Of: sp.Steps}
	}
	return out
}

// hydrate loads a saved conversation into memory when keel serve does not
// hold it, so the next ask continues it. Its proposals stay saved only:
// none becomes typeable.
func (s *Service) hydrate(id string) error {
	if s.chats == nil {
		return nil
	}
	// Under saveMu, like Delete and DeleteAll: a delete that finished
	// between reading the saved copy and putting it in memory would
	// otherwise bring it back, and the next save would write it again.
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	_, ok := s.convs[id]
	s.mu.Unlock()
	if ok {
		return nil
	}
	chat, err := s.savedChat(id)
	if errors.Is(err, ErrNotFound) || errors.Is(err, store.ErrChatsUnreadable) {
		// Nothing to resume; the conversation goes on in memory (and a
		// save of it reports the unreadable file).
		return nil
	}
	if err != nil {
		return err
	}
	// The saved text was redacted when it was sent; it goes through a
	// Redactor again (a second pass leaves placeholders as they are) so
	// that only redact.Safe values ever reach a provider request.
	r, err := s.redactor()
	if err != nil {
		return err
	}
	c := &conversation{id: chat.ID, title: chat.Title, created: chat.CreatedAt, updated: chat.UpdatedAt, record: chat.Turns, used: s.now(), run: chat.Runbook.Copy(), noRunbook: chat.NoRunbook, runMarks: map[string]uint64{}}
	for _, t := range chat.Turns {
		if t.Again {
			continue
		}
		c.turns = append(c.turns, turn{user: r.Text("Question:\n" + t.Question), model: r.Text(t.Model)})
		c.asks++
	}
	c.trimHistory()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.convs[id] == nil {
		if len(s.convs) >= maxConversations {
			s.evictLocked()
		}
		s.convs[id] = c
	}
	return nil
}

// redactor builds a Redactor with the current settings and hosts, for
// text the service keeps rather than sends.
//
// Like every Redactor the service builds it replaces captured values and
// group variables' values with {{name}} (groupvars.go), except
// commandRedactor's.
func (s *Service) redactor() (*redact.Redactor, error) {
	return s.keptRedactor(true)
}

// commandRedactor is redactor for a command the user will type (an edited
// proposal): captured values are withheld, group variables are not. A
// group variable is config, not a secret, and a command the user wrote
// with its value (an address, a version) must stay typeable; the model
// never sees such a command's text but through a request Redactor, which
// replaces it.
func (s *Service) commandRedactor() (*redact.Redactor, error) {
	return s.keptRedactor(false)
}

func (s *Service) keptRedactor(withVars bool) (*redact.Redactor, error) {
	set, err := s.st.AgentSettings()
	if err != nil {
		return nil, err
	}
	hosts, err := s.st.ListHosts()
	if err != nil {
		return nil, err
	}
	return agent.NewRedactor(s.st, set.Redact, agent.HostsFrom(hosts), s.namedValues(withVars)...)
}

// snapshotLocked copies the conversation's record for saving. Call with
// s.mu held.
func (c *conversation) snapshotLocked(now time.Time) store.AgentChat {
	turns := make([]store.AgentChatTurn, len(c.record))
	for i, t := range c.record {
		t.Proposals = append([]store.AgentChatProposal(nil), t.Proposals...)
		t.Notes = append([]store.AgentChatNote(nil), t.Notes...)
		t.Runbooks = append([]store.AgentChatRunbookProposal(nil), t.Runbooks...)
		turns[i] = t
	}
	created := c.created
	if created.IsZero() {
		created = now
	}
	return store.AgentChat{ID: c.id, Title: c.title, CreatedAt: created, UpdatedAt: now, Turns: turns, Runbook: c.run.Copy(), NoRunbook: c.noRunbook}
}

// persist queues c to be saved when history is on, and returns at once:
// it is called from the session socket's read loop (TypeDone) and must not
// wait on encryption and fsync. One worker saves queued conversations in
// order; each save snapshots the conversation as it is then, so the newest
// state wins. A failed save does not fail anything: the conversation goes
// on in memory, keel.log gets one line (no content), and the panel shows
// that it is not being saved.
func (s *Service) persist(c *conversation) {
	if _, on := s.saving(); !on {
		return
	}
	s.mu.Lock()
	c.updated = s.now()
	s.dirty[c.id] = c
	start := !s.saverRunning
	s.saverRunning = true
	s.mu.Unlock()
	if start {
		go s.saver()
	}
	select {
	case s.saveKick <- struct{}{}:
	default:
	}
}

// saver is the save worker. It runs for the life of the service.
func (s *Service) saver() {
	for range s.saveKick {
		s.FlushSaves()
	}
}

// FlushSaves writes every queued conversation now, in this goroutine.
func (s *Service) FlushSaves() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if _, on := s.saving(); !on {
		// History was turned off after these were queued: saving them now
		// would undo the prune that came with the change.
		s.mu.Lock()
		s.dirty = map[string]*conversation{}
		s.mu.Unlock()
		return
	}
	for {
		s.mu.Lock()
		var c *conversation
		for id, x := range s.dirty {
			c = x
			delete(s.dirty, id)
			break
		}
		if c == nil {
			s.mu.Unlock()
			return
		}
		if len(c.record) == 0 || c.deleted {
			// Deleted meanwhile: do not bring it back. (A conversation
			// only evicted from memory is still saved.)
			s.mu.Unlock()
			continue
		}
		chat := c.snapshotLocked(c.updated)
		s.mu.Unlock()
		s.noteSave(s.chats.PutAgentChat(chat))
	}
}

// noteSave records the outcome of a save. A failure is logged once per
// new reason, never with content.
func (s *Service) noteSave(err error) {
	msg := ""
	if err != nil {
		msg = saveErrorText(err)
	}
	s.mu.Lock()
	changed := msg != s.saveErr
	s.saveErr = msg
	s.mu.Unlock()
	if changed && msg != "" {
		s.log.Append(applog.Entry{Kind: "agent", Message: "agent conversation not saved: " + msg})
	}
}

func (s *Service) lastSaveError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveErr
}

// saveErrorText says why saving failed, in words for the panel and the
// log. It never holds conversation text.
func saveErrorText(err error) string {
	switch {
	case errors.Is(err, store.ErrChatsUnreadable):
		return "keel.chats cannot be read; Delete all starts a fresh file"
	case errors.Is(err, store.ErrLocked):
		return "the vault is locked"
	}
	return err.Error()
}

// record adds a turn to c's saved record. Call with s.mu held.
func (c *conversation) addRecordLocked(t store.AgentChatTurn) {
	c.record = append(c.record, t)
	if n := len(c.record); n > maxSavedTurns {
		c.record = c.record[n-maxSavedTurns:]
	}
}

// recordProposalLocked finds a proposal in c's saved record.
func (c *conversation) recordProposalLocked(id string) *store.AgentChatProposal {
	for i := len(c.record) - 1; i >= 0; i-- {
		for j := range c.record[i].Proposals {
			if c.record[i].Proposals[j].ID == id {
				return &c.record[i].Proposals[j]
			}
		}
	}
	return nil
}

func titleFrom(question redact.Safe) string {
	t := store.CleanAgentChatTitle(question.String())
	if len(t) > maxTitleFromQuestion {
		cut := maxTitleFromQuestion
		for cut > 0 && t[cut]&0xC0 == 0x80 {
			cut--
		}
		t = strings.TrimSpace(t[:cut]) + "…"
	}
	if t == "" {
		t = "Conversation"
	}
	return t
}

// Rename sets a conversation's title, in memory and in the vault. The
// title goes through the Redactor like everything else that is kept.
func (s *Service) Rename(id, title string) (string, error) {
	if !convIDRe.MatchString(id) {
		return "", ErrNotFound
	}
	r, err := s.redactor()
	if err != nil {
		return "", err
	}
	title = store.CleanAgentChatTitle(r.Text(title).String())
	if title == "" {
		return "", badRequest(fmt.Sprintf("a title is 1 to %d bytes on one line", store.MaxAgentChatTitle))
	}
	// Ordered with the save worker, so a save snapshotted before the rename
	// cannot land after it with the old title.
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	c := s.convs[id]
	if c != nil {
		c.title = title
	}
	s.mu.Unlock()
	if _, on := s.saving(); on {
		if _, err := s.chats.RenameAgentChat(id, title); err != nil && !(errors.Is(err, store.ErrNotFound) && c != nil) {
			if errors.Is(err, store.ErrNotFound) {
				return "", ErrNotFound
			}
			return "", err
		}
	} else if c == nil {
		return "", ErrNotFound
	}
	return title, nil
}

// ProposeAgain makes a new proposal from a saved one: the command is
// rated again, the production flag is the host's now (a host that is gone
// counts as production), and it targets the same pane only while that
// pane is still open on the same host and the agent may read that host
// (§6.2). It writes nothing to a pane; the
// new proposal is typed, if at all, through AuthorizeType like any other.
func (s *Service) ProposeAgain(convID, proposalID string) (Proposal, error) {
	if !convIDRe.MatchString(convID) {
		return Proposal{}, ErrNotFound
	}
	if err := s.hydrate(convID); err != nil {
		return Proposal{}, err
	}
	s.mu.Lock()
	c := s.convs[convID]
	var src store.AgentChatProposal
	found := false
	if c != nil {
		if sp := c.recordProposalLocked(proposalID); sp != nil {
			src, found = *sp, true
		}
	}
	s.mu.Unlock()
	if !found {
		return Proposal{}, ErrNotFound
	}
	cmd, ok := cleanCommand(src.Command)
	if !ok {
		return Proposal{}, badRequest("the saved command can no longer be proposed")
	}
	hosts, err := s.st.ListHosts()
	if err != nil {
		return Proposal{}, err
	}
	var host *agent.Host
	for _, h := range agent.HostsFrom(hosts) {
		if src.HostID != "" && h.ID == src.HostID {
			h := h
			host = &h
		}
	}
	r, err := s.redactor()
	if err != nil {
		return Proposal{}, err
	}
	risk := agent.Stricter(agent.Classify(cmd), agent.Risk(src.Risk))
	now := s.now()
	p := &Proposal{ID: newID("p_"), Command: cmd, Bytes: typedBytes(cmd), Why: r.Text(src.Why).String(), Risk: risk, ProposedAt: &now}
	p.Host, p.HostID = src.Host, src.HostID
	p.Production = host == nil || host.Production
	if host != nil {
		p.Host = host.Name
		// The pane is a target only while it is still open on the same
		// host and the agent may read that host now (§6.2): otherwise the
		// new proposal is Copy only.
		scope := agent.NewScope(agent.HostsFrom(hosts))
		if pane, ok := s.panes.Pane(src.SessionID); ok && src.SessionID != "" && pane.HostID == host.ID && scope.Allows(pane) {
			p.SessionID = src.SessionID
			s.audit.Suggest(agent.Pane{SessionID: pane.SessionID, HostID: host.ID, HostName: host.Name}, redact.Report{}, riskLabel(risk))
		}
	}
	p.NeedsShift = agent.NeedsShift(p.Risk, p.Production)
	p.Placeholders = hasPlaceholder(cmd)

	s.mu.Lock()
	c = s.convs[convID]
	if c == nil {
		s.mu.Unlock()
		return Proposal{}, ErrNotFound
	}
	c.proposals = append(c.proposals, p)
	if n := len(c.proposals); n > maxProposalsKept {
		c.proposals = c.proposals[n-maxProposalsKept:]
	}
	c.addRecordLocked(store.AgentChatTurn{At: now, Again: true, Proposals: []store.AgentChatProposal{recordOf(p, r.Text(cmd).String(), r.Text(p.Why).String(), now)}})
	out := *p
	s.mu.Unlock()
	s.persist(c)
	return out, nil
}

// recordOf is a proposal as saved; command and why are passed in already
// redacted.
func recordOf(p *Proposal, command, why string, at time.Time) store.AgentChatProposal {
	out := store.AgentChatProposal{
		ID: p.ID, SessionID: p.SessionID, HostID: p.HostID, Host: p.Host, Production: p.Production,
		Command: command, Why: why, Risk: string(p.Risk), Edited: p.Edited, ProposedAt: at,
	}
	if p.Runbook != nil {
		out.Step, out.Steps = p.Runbook.Step, p.Runbook.Of
	}
	return out
}

// TypeDone records that a proposal was typed (keel serve calls it after the
// approved bytes were written), so the transcript shows it as typed.
func (s *Service) TypeDone(convID, proposalID string) {
	s.typeDone(convID, proposalID, 0, false)
}

// TypeDoneAt is TypeDone with the pane's stream offset taken right before
// the write: a runbook step's result is the first command the pane's
// shell reports after it (runbooks.go).
func (s *Service) TypeDoneAt(convID, proposalID string, mark uint64) {
	s.typeDone(convID, proposalID, mark, true)
}

func (s *Service) typeDone(convID, proposalID string, mark uint64, hasMark bool) {
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil {
		s.mu.Unlock()
		return
	}
	now := s.now()
	for _, p := range c.proposals {
		if p.ID == proposalID {
			p.TypedAt = &now
		}
	}
	sp := c.recordProposalLocked(proposalID)
	if sp != nil {
		sp.TypedAt = &now
	}
	step := c.stepTypedLocked(proposalID, mark, hasMark, now)
	s.mu.Unlock()
	if sp != nil || step {
		s.persist(c)
	}
}

// Delete forgets a conversation, in memory and in the vault. Any store
// error is returned: the panel must not report a delete that did not
// happen.
func (s *Service) Delete(id string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	if c := s.convs[id]; c != nil {
		c.deleted = true
	}
	if c := s.dirty[id]; c != nil {
		c.deleted = true
	}
	delete(s.convs, id)
	delete(s.dirty, id)
	s.mu.Unlock()
	if s.chats == nil {
		return nil
	}
	return s.chats.DeleteAgentChat(id)
}

// DeleteAll forgets every conversation, in memory and in the vault, and
// returns how many there were. The vault side removes keel.chats and its
// key (store.DeleteAllAgentChats), so it also replaces a keel.chats that
// cannot be read.
func (s *Service) DeleteAll() (int, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	ids := make(map[string]bool, len(s.convs))
	for id, c := range s.convs {
		ids[id] = true
		c.deleted = true
	}
	for _, c := range s.dirty {
		c.deleted = true
	}
	s.convs = map[string]*conversation{}
	s.dirty = map[string]*conversation{}
	s.mu.Unlock()
	if s.chats == nil {
		return len(ids), nil
	}
	if list, err := s.chats.ListAgentChats(); err == nil {
		for _, c := range list {
			ids[c.ID] = true
		}
	} else if !errors.Is(err, store.ErrChatsUnreadable) {
		return len(ids), err
	}
	if _, err := s.chats.DeleteAllAgentChats(); err != nil {
		return len(ids), err
	}
	s.mu.Lock()
	s.saveErr = ""
	s.mu.Unlock()
	return len(ids), nil
}
