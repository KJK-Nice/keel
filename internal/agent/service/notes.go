package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/applog"
	"keel/internal/store"
)

// Notes for the agent (docs/ui-redesign.md §15).
//
// Host notes and the user's own notes are kept encrypted in the vault
// (store, keel.memory). The service is their only way in and out:
//
//   - The user adds, edits and deletes them (AddNote, EditNote, DeleteNote).
//     Every text is redacted for secrets before it is kept (saveRedactor:
//     vault values and secret-looking values, never IPs or hostnames, so a
//     note still says what it means), and every change is logged as
//     agent.note without the text.
//   - The agent never writes one. Ask may only propose a note (a
//     ```keel-note block); the proposal waits in the conversation until the
//     user saves (SaveNoteProposal, maybe edited), dismisses or undoes it.
//     A proposal whose text matches pane output, or that the model says
//     came from it, is marked FromPane: terminal output is untrusted remote
//     text and a saved note is sent again and again, so the panel warns.
//   - Ask sends them (noteContext): a host's notes only when a pane on that
//     host is read for the question (in scope, "Agent can read" on), a
//     group's notes (§17) only when a pane on a host in that group (its
//     folder or one below) is read and readable, the user's notes when at
//     least one pane is read; through the request's
//     Redactor and the Auditor (agent.note); framed as user-curated
//     context, apart from pane text.

// NoteStore is where notes are kept. *store.Store satisfies it.
type NoteStore interface {
	ListAgentNotes() ([]store.AgentNote, error)
	AddAgentNote(n store.AgentNote) (store.AgentNote, error)
	UpdateAgentNote(id, text string) (store.AgentNote, error)
	DeleteAgentNote(id string) (store.AgentNote, error)
	DeleteAllAgentNotes() (int, error)
}

// NoteFence is the info string of the fenced block in which the model
// proposes a note. One block holds one JSON object:
//
//	```keel-note
//	{"for": "host", "pane": 1, "text": "Config is /etc/patroni.yml", "from": "conversation"}
//	```
//
// for is "host" (about the host of pane N) or "user" (the user's own
// preference, every host); from is "pane" (the text comes from pane
// output), "user" (the user said it) or "conversation" (the model's
// summary). from can only make the warning stricter: Keel also checks
// the text against the panes it sent.
const NoteFence = "keel-note"

const (
	// maxNoteProposals is how many notes one answer may propose, and
	// maxNoteProposalsKept how many a conversation keeps in memory.
	maxNoteProposals     = 2
	maxNoteProposalsKept = 32
	// maxNotesSent bounds the notes sent per host (and of the user's own)
	// with one question; the oldest come first.
	maxNotesSent = 20
	// noteShingle is how many words in a row a note must share with a
	// pane excerpt to count as taken from it.
	noteShingle = 5
)

var (
	ErrNoNotes         = errors.New("agent: notes are not available")
	ErrNoteClosed      = errors.New("agent: this note proposal was already saved or dismissed")
	ErrNoteRefused     = errors.New("agent: this host no longer lets the agent propose notes")
	ErrNoteNotReadable = errors.New("agent: Agent can read is off for this host; turn it on to save a note for it")
	ErrNoteExpired     = errors.New("agent: this note proposal expired; add the note yourself in Edit host or Settings · Agent")
	noteFenceRe        = regexp.MustCompile("(?s)```[ \\t]*" + regexp.QuoteMeta(NoteFence) + "[ \\t]*\\r?\\n(.*?)\\r?\\n?```")
	errNoteTooLong     = fmt.Errorf("%w: a note is 1 to %d bytes", ErrBadRequest, store.MaxAgentNoteBytes)
	noteStatusValid    = map[string]bool{"open": true, "saved": true, "dismissed": true}
)

// NoteProposal is a note the agent proposed, as the panel shows it.
type NoteProposal struct {
	ID     string `json:"id"`
	For    string `json:"for"` // "host" | "user"
	HostID string `json:"hostId,omitempty"`
	Host   string `json:"host,omitempty"`
	Text   string `json:"text"`
	// From is what the model said ("pane", "user", "conversation").
	From string `json:"from"`
	// FromPane is set when the text came from terminal output: the model
	// said so, or Keel found it in a pane it sent. SeenIn names that
	// pane's host.
	FromPane   bool       `json:"fromPane"`
	SeenIn     string     `json:"seenIn,omitempty"`
	Status     string     `json:"status"` // "open" | "saved" | "dismissed"
	NoteID     string     `json:"noteId,omitempty"`
	ProposedAt *time.Time `json:"proposedAt,omitempty"`
	// Expired is set on a proposal from a saved conversation that keel
	// serve no longer holds: it can be read, not saved.
	Expired bool `json:"expired,omitempty"`
	// PanesRead names the hosts whose panes were read for the answer that
	// proposed it. Any read pane may be where the text came from, whatever
	// FromPane says (it is a hint, easily evaded), so the panel warns on
	// every proposal from an answer that read a pane.
	PanesRead []string `json:"panesRead,omitempty"`
	// busy is set while a Save, Dismiss or Undo runs (under mu).
	busy bool
}

// NotesList is GET /api/agent/notes.
type NotesList struct {
	Notes []store.AgentNote `json:"notes"`
	// Unreadable is set when keel.memory cannot be read; Delete all notes
	// starts a fresh file.
	Unreadable bool `json:"unreadable,omitempty"`
}

// SavedNote is a note after a save: Redacted counts the values replaced
// before it was kept.
type SavedNote struct {
	Note     store.AgentNote `json:"note"`
	Redacted int             `json:"redacted"`
	// Proposal is the proposal's new state when the note came from one.
	Proposal *NoteProposal `json:"proposal,omitempty"`
}

type rawNote struct {
	For  string `json:"for"`
	Pane int    `json:"pane"`
	Text string `json:"text"`
	From string `json:"from"`
}

// parseNotes takes the note blocks out of a reply: the text without them,
// and at most maxNoteProposals notes. A block that is not valid JSON or
// whose text is empty or too long is dropped.
func parseNotes(text string) (string, []rawNote) {
	var out []rawNote
	rest := noteFenceRe.ReplaceAllStringFunc(text, func(block string) string {
		m := noteFenceRe.FindStringSubmatch(block)
		if len(m) < 2 || len(out) >= maxNoteProposals {
			return ""
		}
		var n rawNote
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &n); err != nil {
			return ""
		}
		t, ok := store.CleanAgentNote(n.Text)
		if !ok {
			return ""
		}
		n.Text = t
		n.For = strings.ToLower(strings.TrimSpace(n.For))
		n.From = strings.ToLower(strings.TrimSpace(n.From))
		switch n.From {
		case "pane", "user", "conversation":
		default:
			n.From = "conversation"
		}
		out = append(out, n)
		return ""
	})
	return rest, out
}

// noteContext reads the notes that go with one question: the notes of
// each host whose pane was read (in the order the panes were), then the
// user's own when any pane was read. Each set goes through the Auditor,
// which checks the scope, redacts with r and logs agent.note. It returns
// the framed text for the provider message and the trace steps.
func (s *Service) noteContext(read []agent.Excerpt, byID map[string]agent.Host, scope agent.Scope, r *redact.Redactor) (string, []TraceStep) {
	if s.notesSt == nil || len(read) == 0 {
		return "", nil
	}
	all, err := s.notesSt.ListAgentNotes()
	if err != nil || len(all) == 0 {
		return "", nil
	}
	byHost := map[string][]string{}
	byGroup := map[string][]string{}
	for _, n := range all {
		switch {
		case n.GroupID != "":
			if len(byGroup[n.GroupID]) < maxNotesSent {
				byGroup[n.GroupID] = append(byGroup[n.GroupID], n.Text)
			}
		case len(byHost[n.HostID]) < maxNotesSent:
			byHost[n.HostID] = append(byHost[n.HostID], n.Text)
		}
	}
	var (
		b     strings.Builder
		steps []TraceStep
		seen  = map[string]bool{}
	)
	for _, ex := range read {
		id := ex.Pane.HostID
		if seen[id] || len(byHost[id]) == 0 {
			continue
		}
		seen[id] = true
		h, ok := byID[id]
		if !ok {
			continue
		}
		nx, err := s.audit.ReadHostNotes(scope, h, byHost[id], r)
		if err != nil || nx.Count == 0 {
			continue
		}
		fmt.Fprintf(&b, "--- host notes · host %s · saved by the user ---\n%s\n--- end host notes ---\n\n", frameName(h.Name, r), nx.Text.String())
		steps = append(steps, TraceStep{HostID: h.ID, Host: h.Name, Status: "notes", NoteKind: "host", Notes: nx.Count, Redactions: nx.Report.Strings()})
	}
	gText, gSteps := s.groupNoteContext(read, byID, byGroup, scope, r)
	b.WriteString(gText)
	steps = append(steps, gSteps...)
	if mine := byHost[""]; len(mine) > 0 {
		if nx, err := s.audit.ReadUserNotes(mine, r); err == nil && nx.Count > 0 {
			fmt.Fprintf(&b, "--- your notes · the user's own preferences, saved by the user ---\n%s\n--- end your notes ---\n\n", nx.Text.String())
			steps = append(steps, TraceStep{Status: "notes", NoteKind: "user", Notes: nx.Count, Redactions: nx.Report.Strings()})
		}
	}
	return b.String(), steps
}

// frameName is a host or group name as a notes frame line holds it: through
// the request's Redactor (it may hold a secret-looking value or a hostname
// the settings redact) and on one line, so it cannot draw its own frame.
func frameName(name string, r *redact.Redactor) string {
	c, _ := store.CleanLine(r.Text(name).String(), 4*store.MaxRunbookNameBytes)
	return c
}

// GroupSource lists the folders, for group notes and roles (§17).
// *store.Store satisfies it.
type GroupSource interface {
	ListGroups() ([]store.Group, error)
}

// groupNoteContext is the group part of noteContext (§17): for each folder
// that holds a host whose pane was read (its own folder and the folders
// above it), that group's notes and the roles of the hosts read in it, as
// user-curated context. Only for hosts the agent may read: ReadGroupNotes
// refuses a group none of whose read hosts it may read, before anything is
// redacted or logged.
func (s *Service) groupNoteContext(read []agent.Excerpt, byID map[string]agent.Host, byGroup map[string][]string, scope agent.Scope, r *redact.Redactor) (string, []TraceStep) {
	src, ok := s.st.(GroupSource)
	if !ok {
		return "", nil
	}
	groups, err := src.ListGroups()
	if err != nil || len(groups) == 0 {
		return "", nil
	}
	type groupRead struct {
		g     store.Group
		hosts []agent.Host
	}
	var order []string
	seen := map[string]*groupRead{}
	hostSeen := map[string]bool{}
	for _, ex := range read {
		h, ok := byID[ex.Pane.HostID]
		if !ok || hostSeen[h.ID] || !scope.AllowsHost(h.ID) {
			continue
		}
		hostSeen[h.ID] = true
		for _, g := range store.GroupChain(groups, h.GroupID) {
			gr := seen[g.ID]
			if gr == nil {
				gr = &groupRead{g: g}
				seen[g.ID] = gr
				order = append(order, g.ID)
			}
			gr.hosts = append(gr.hosts, h)
		}
	}
	var (
		b     strings.Builder
		steps []TraceStep
	)
	for _, id := range order {
		gr := seen[id]
		var roles []string
		for _, h := range gr.hosts {
			if role := gr.g.Roles[h.ID]; role != "" {
				roles = append(roles, fmt.Sprintf("role of %s in this group: %s", h.Name, role))
			}
		}
		notes := byGroup[id]
		if len(notes) == 0 && len(roles) == 0 {
			continue
		}
		nx, err := s.audit.ReadGroupNotes(scope, id, gr.hosts, notes, roles, r)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "--- group notes · group %s · saved by the user, about every host in it ---\n%s\n--- end group notes ---\n\n", frameName(gr.g.Name, r), nx.Text.String())
		if nx.Count > 0 {
			steps = append(steps, TraceStep{Host: gr.g.Name, Status: "notes", NoteKind: "group", Notes: nx.Count, Redactions: nx.Report.Strings()})
		}
	}
	return b.String(), steps
}

// noteProposals turns the model's note blocks into proposals. A host note
// needs a pane read in this ask (the one it names, or the only one) on a
// host that still lets the agent propose notes; others are dropped. The
// text came from a model that saw redacted text only, and goes through r
// again before it is shown or kept.
func (s *Service) noteProposals(raws []rawNote, excerpts []agent.Excerpt, byID map[string]agent.Host, r *redact.Redactor, now time.Time) []*NoteProposal {
	var out []*NoteProposal
	var shingles []paneShingles
	var read []string
	seenHost := map[string]bool{}
	for _, ex := range excerpts {
		if !seenHost[ex.Pane.HostName] {
			seenHost[ex.Pane.HostName] = true
			read = append(read, ex.Pane.HostName)
		}
	}
	for _, rn := range raws {
		np := &NoteProposal{ID: newID("np_"), Text: r.Text(rn.Text).String(), From: rn.From, Status: "open", ProposedAt: &now, PanesRead: read}
		if t, ok := store.CleanAgentNote(np.Text); ok {
			np.Text = t
		} else {
			continue
		}
		switch rn.For {
		case "user", "me", "you":
			np.For = "user"
		default:
			var target *agent.Excerpt
			switch {
			case rn.Pane >= 1 && rn.Pane <= len(excerpts):
				target = &excerpts[rn.Pane-1]
			case len(excerpts) == 1:
				target = &excerpts[0]
			}
			if target == nil {
				continue
			}
			h, ok := byID[target.Pane.HostID]
			if !ok || !h.ProposeNotes {
				continue
			}
			np.For, np.HostID, np.Host = "host", h.ID, h.Name
		}
		if shingles == nil {
			shingles = buildShingles(excerpts)
		}
		if host, ok := seenInPanes(np.Text, shingles); ok {
			np.FromPane, np.SeenIn = true, host
		}
		if np.From == "pane" && !np.FromPane {
			np.FromPane = true
			np.SeenIn = np.Host
		}
		out = append(out, np)
	}
	return out
}

type paneShingles struct {
	host  string
	flat  string
	grams map[string]bool
}

// words splits text into lower-case words for comparing a note with pane
// text: letters, digits and the characters of paths and flags, without the
// punctuation that ends a sentence.
func words(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 0x7f || strings.ContainsRune("/._-@=[]$~", r))
	})
	out := f[:0]
	for _, w := range f {
		if w = strings.Trim(w, "._-/"); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func buildShingles(excerpts []agent.Excerpt) []paneShingles {
	out := make([]paneShingles, 0, len(excerpts))
	for _, ex := range excerpts {
		w := words(ex.Text.String())
		ps := paneShingles{host: ex.Pane.HostName, flat: " " + strings.Join(w, " ") + " ", grams: map[string]bool{}}
		for i := 0; i+noteShingle <= len(w); i++ {
			ps.grams[strings.Join(w[i:i+noteShingle], " ")] = true
		}
		out = append(out, ps)
	}
	return out
}

// seenInPanes reports whether a note's text appears in a pane excerpt that
// was sent: noteShingle words in a row, or the whole note when it is
// shorter (and at least 12 characters, so a word or two does not count).
func seenInPanes(text string, panes []paneShingles) (string, bool) {
	w := words(text)
	for _, p := range panes {
		if len(w) < noteShingle {
			whole := strings.Join(w, " ")
			if len(whole) >= 12 && strings.Contains(p.flat, " "+whole+" ") {
				return p.host, true
			}
			continue
		}
		for i := 0; i+noteShingle <= len(w); i++ {
			if p.grams[strings.Join(w[i:i+noteShingle], " ")] {
				return p.host, true
			}
		}
	}
	return "", false
}

func noteRecord(np *NoteProposal) store.AgentChatNote {
	at := time.Time{}
	if np.ProposedAt != nil {
		at = *np.ProposedAt
	}
	return store.AgentChatNote{ID: np.ID, For: np.For, HostID: np.HostID, Host: np.Host, Text: np.Text, FromPane: np.FromPane, SeenIn: np.SeenIn, PanesRead: np.PanesRead, Status: np.Status, NoteID: np.NoteID, ProposedAt: at}
}

func noteFromRecord(n store.AgentChatNote) NoteProposal {
	at := n.ProposedAt
	st := n.Status
	if !noteStatusValid[st] {
		st = "open"
	}
	return NoteProposal{ID: n.ID, For: n.For, HostID: n.HostID, Host: n.Host, Text: n.Text, FromPane: n.FromPane, SeenIn: n.SeenIn, PanesRead: n.PanesRead, Status: st, NoteID: n.NoteID, ProposedAt: &at, Expired: true}
}

// recordNoteLocked finds a note proposal in c's saved record.
func (c *conversation) recordNoteLocked(id string) *store.AgentChatNote {
	for i := len(c.record) - 1; i >= 0; i-- {
		for j := range c.record[i].Notes {
			if c.record[i].Notes[j].ID == id {
				return &c.record[i].Notes[j]
			}
		}
	}
	return nil
}

func (c *conversation) noteLocked(id string) *NoteProposal {
	for _, n := range c.notes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// saveRedactor redacts what is kept in a note: secrets always (vault
// values as exact matches, secret-looking values), never IPs or hostnames,
// which a note may need and which the request's Redactor replaces anyway
// when it is sent with the user's settings.
func (s *Service) saveRedactor() (*redact.Redactor, error) {
	return agent.NewRedactor(s.st, store.AgentRedaction{Secrets: true}, nil)
}

func (s *Service) noteHost(hostID string) (agent.Host, error) {
	hosts, err := s.st.ListHosts()
	if err != nil {
		return agent.Host{}, err
	}
	for _, h := range agent.HostsFrom(hosts) {
		if h.ID == hostID {
			return h, nil
		}
	}
	return agent.Host{}, ErrNotFound
}

// cleanNote redacts a note's text for keeping and checks it. The raw text
// is redacted first, so a vault value with a tab, two spaces or an
// invisible character is matched as typed; then it is cleaned to one line
// and redacted again, which catches the value in its cleaned form too (the
// Redactor also knows the cleaned forms of every vault value).
func (s *Service) cleanNote(text string) (string, redact.Report, error) {
	if len(text) > 4*store.MaxAgentNoteBytes {
		return "", redact.Report{}, errNoteTooLong
	}
	r, err := s.saveRedactor()
	if err != nil {
		return "", redact.Report{}, err
	}
	t, ok := store.CleanAgentNote(r.Text(text).String())
	if !ok {
		return "", redact.Report{}, errNoteTooLong
	}
	if t, ok = store.CleanAgentNote(r.Text(t).String()); !ok {
		return "", redact.Report{}, errNoteTooLong
	}
	return t, r.Report(), nil
}

// ListNotes returns every note. An unreadable keel.memory is a state, not
// an error.
func (s *Service) ListNotes() (NotesList, error) {
	if s.notesSt == nil {
		return NotesList{Notes: []store.AgentNote{}}, nil
	}
	notes, err := s.notesSt.ListAgentNotes()
	if errors.Is(err, store.ErrMemoryUnreadable) {
		return NotesList{Notes: []store.AgentNote{}, Unreadable: true}, nil
	}
	if err != nil {
		return NotesList{}, err
	}
	if notes == nil {
		notes = []store.AgentNote{}
	}
	return NotesList{Notes: notes}, nil
}

// AddNote saves a note the user wrote: a host note when hostID is set, else
// one of the user's own.
func (s *Service) AddNote(hostID, text string) (SavedNote, error) {
	if s.notesSt == nil {
		return SavedNote{}, ErrNoNotes
	}
	h := agent.Host{}
	if hostID != "" {
		var err error
		if h, err = s.noteHost(hostID); err != nil {
			return SavedNote{}, err
		}
	}
	t, rep, err := s.cleanNote(text)
	if err != nil {
		return SavedNote{}, err
	}
	n, err := s.notesSt.AddAgentNote(store.AgentNote{HostID: hostID, Text: t, Source: store.NoteSourceUser})
	if err != nil {
		return SavedNote{}, noteErr(err)
	}
	s.audit.NoteChanged("note added by you", hostID, h.Name, rep)
	return SavedNote{Note: n, Redacted: rep.Total()}, nil
}

// AddGroupNote saves a note the user wrote about every host in a group
// (§17).
func (s *Service) AddGroupNote(groupID, text string) (SavedNote, error) {
	if s.notesSt == nil {
		return SavedNote{}, ErrNoNotes
	}
	if groupID == "" {
		return SavedNote{}, badRequest("name a group")
	}
	t, rep, err := s.cleanNote(text)
	if err != nil {
		return SavedNote{}, err
	}
	n, err := s.notesSt.AddAgentNote(store.AgentNote{GroupID: groupID, Text: t, Source: store.NoteSourceUser})
	if err != nil {
		return SavedNote{}, noteErr(err)
	}
	s.audit.GroupNoteChanged("note added by you", groupID, rep)
	return SavedNote{Note: n, Redacted: rep.Total()}, nil
}

// EditNote replaces a note's text.
func (s *Service) EditNote(id, text string) (SavedNote, error) {
	if s.notesSt == nil {
		return SavedNote{}, ErrNoNotes
	}
	t, rep, err := s.cleanNote(text)
	if err != nil {
		return SavedNote{}, err
	}
	n, err := s.notesSt.UpdateAgentNote(id, t)
	if err != nil {
		return SavedNote{}, noteErr(err)
	}
	if n.GroupID != "" {
		s.audit.GroupNoteChanged("note edited by you", n.GroupID, rep)
	} else {
		s.audit.NoteChanged("note edited by you", n.HostID, s.hostName(n.HostID), rep)
	}
	return SavedNote{Note: n, Redacted: rep.Total()}, nil
}

// DeleteNote removes a note.
func (s *Service) DeleteNote(id string) error {
	if s.notesSt == nil {
		return ErrNoNotes
	}
	n, err := s.notesSt.DeleteAgentNote(id)
	if err != nil {
		return noteErr(err)
	}
	if n.GroupID != "" {
		s.audit.GroupNoteChanged("note deleted by you", n.GroupID, redact.Report{})
	} else {
		s.audit.NoteChanged("note deleted by you", n.HostID, s.hostName(n.HostID), redact.Report{})
	}
	return nil
}

// DeleteAllNotes removes every note, keel.memory and its key.
func (s *Service) DeleteAllNotes() (int, error) {
	if s.notesSt == nil {
		return 0, nil
	}
	n, err := s.notesSt.DeleteAllAgentNotes()
	if err != nil {
		return 0, err
	}
	s.log.Append(applog.Entry{Kind: agent.EventNote, Message: fmt.Sprintf("every note for the agent deleted by you (%d)", n), Count: n})
	return n, nil
}

func (s *Service) hostName(id string) string {
	if id == "" {
		return ""
	}
	h, err := s.noteHost(id)
	if err != nil {
		return ""
	}
	return h.Name
}

func noteErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, store.ErrAgentNote), errors.Is(err, store.ErrNotesFull):
		return fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return err
}

// SaveNoteProposal saves a note the agent proposed, after the user pressed
// Save (or Save anyway). text is the user's edit, or empty for the text as
// proposed. A host note needs the host to exist and to still let the agent
// propose notes. Only proposals held in memory from this run can be saved.
func (s *Service) SaveNoteProposal(convID, id, text string) (SavedNote, error) {
	if s.notesSt == nil {
		return SavedNote{}, ErrNoNotes
	}
	// Claim the proposal under mu: a second Save (or Save anyway, or an
	// Undo) while this one runs gets ErrNoteClosed, so one proposal never
	// becomes two notes.
	snap, err := s.claimNote(convID, id, func(np *NoteProposal) bool { return np.Status == "open" })
	if err != nil {
		return SavedNote{}, err
	}
	release := func() { s.releaseNote(convID, id) }
	edited := strings.TrimSpace(text) != "" && strings.Join(strings.Fields(text), " ") != snap.Text
	if !edited {
		text = snap.Text
	}
	if snap.For == "host" {
		h, err := s.noteHost(snap.HostID)
		if err != nil {
			release()
			return SavedNote{}, err
		}
		if !h.ProposeNotes {
			release()
			return SavedNote{}, ErrNoteRefused
		}
		if !h.AgentReadable {
			release()
			return SavedNote{}, ErrNoteNotReadable
		}
		snap.Host = h.Name
	}
	t, rep, err := s.cleanNote(text)
	if err != nil {
		release()
		return SavedNote{}, err
	}
	n, err := s.notesSt.AddAgentNote(store.AgentNote{HostID: snap.HostID, Text: t, Source: store.NoteSourceAgent, FromPane: snap.FromPane, SeenIn: snap.SeenIn, Edited: edited})
	if err != nil {
		release()
		return SavedNote{}, noteErr(err)
	}
	what := "note proposed by the agent saved by you"
	if snap.FromPane {
		what = "note proposed by the agent from pane output saved by you"
	}
	if edited {
		what += " after an edit"
	}
	s.audit.NoteChanged(what, snap.HostID, snap.Host, rep)
	out := s.setNoteStatus(convID, id, "saved", n.ID)
	return SavedNote{Note: n, Redacted: rep.Total(), Proposal: out}, nil
}

// claimNote marks a proposal busy if ok(it) holds, under mu, and returns a
// copy. A busy or closed proposal gives ErrNoteClosed; one keel serve no
// longer holds gives ErrNoteExpired or ErrNotFound.
func (s *Service) claimNote(convID, id string, ok func(*NoteProposal) bool) (NoteProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convs[convID]
	var np *NoteProposal
	if c != nil {
		np = c.noteLocked(id)
	}
	if np == nil {
		if c != nil && c.recordNoteLocked(id) != nil {
			return NoteProposal{}, ErrNoteExpired
		}
		return NoteProposal{}, ErrNotFound
	}
	if np.busy || !ok(np) {
		return NoteProposal{}, ErrNoteClosed
	}
	np.busy = true
	return *np, nil
}

func (s *Service) releaseNote(convID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.convs[convID]; c != nil {
		if np := c.noteLocked(id); np != nil {
			np.busy = false
		}
	}
}

// DismissNoteProposal closes a proposal without saving anything.
func (s *Service) DismissNoteProposal(convID, id string) (NoteProposal, error) {
	if _, err := s.claimNote(convID, id, func(np *NoteProposal) bool { return np.Status == "open" }); err != nil {
		return NoteProposal{}, err
	}
	out := s.setNoteStatus(convID, id, "dismissed", "")
	if out == nil {
		return NoteProposal{}, ErrNotFound
	}
	return *out, nil
}

// UndoNoteProposal opens a saved or dismissed proposal again; a saved one's
// note is deleted (logged).
func (s *Service) UndoNoteProposal(convID, id string) (NoteProposal, error) {
	snap, err := s.claimNote(convID, id, func(np *NoteProposal) bool { return np.Status == "saved" || np.Status == "dismissed" })
	if err != nil {
		return NoteProposal{}, err
	}
	if snap.Status == "saved" && snap.NoteID != "" && s.notesSt != nil {
		n, err := s.notesSt.DeleteAgentNote(snap.NoteID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.releaseNote(convID, id)
			return NoteProposal{}, err
		}
		if err == nil {
			s.audit.NoteChanged("note proposed by the agent deleted by you (undo)", n.HostID, s.hostName(n.HostID), redact.Report{})
		}
	}
	out := s.setNoteStatus(convID, id, "open", "")
	if out == nil {
		return NoteProposal{}, ErrNotFound
	}
	return *out, nil
}

// setNoteStatus sets a proposal's state and frees it; nil when the
// conversation or the proposal is gone (deleted meanwhile).
func (s *Service) setNoteStatus(convID, id, status, noteID string) *NoteProposal {
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	np := c.noteLocked(id)
	if np == nil {
		s.mu.Unlock()
		return nil
	}
	np.Status, np.NoteID, np.busy = status, noteID, false
	if rec := c.recordNoteLocked(id); rec != nil {
		rec.Status, rec.NoteID = status, noteID
	}
	out := *np
	s.mu.Unlock()
	s.persist(c)
	return &out
}
