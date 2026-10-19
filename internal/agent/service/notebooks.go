package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/store"
)

// Notebooks and the agent (docs/ui-redesign.md §18).
//
//   - Reading: the user attaches a notebook to a question (AskRequest.
//     Notebooks). Only attached notebooks are read, only here
//     (notebookContext), through the Auditor (ReadNotebook): redacted
//     with the request's Redactor, capped, framed as untrusted data, and
//     logged as agent.read with the id and the byte count. The model has
//     no way to ask for a notebook: nothing in a reply names one.
//   - Drafts: a reply may hold one ```keel-notebook block. It becomes a
//     notebook proposal in the conversation, redacted. Nothing is saved
//     until the user opens it (OpenNotebookDraft, which hands out a
//     draftId keel serve keeps), reviews it in the notebook editor and
//     presses Save (SaveNotebookDraft). The saved notebook is always a new
//     one, marked "agent" by keel serve from its own record of the draft,
//     never from the client. The agent cannot edit or replace a notebook.

// NotebookStore reads the notebooks the user attached and keeps the
// drafts the user saved. *store.Store satisfies it.
type NotebookStore interface {
	GetNotebook(id string) (store.Notebook, error)
	AddAgentNotebook(nb store.Notebook) (store.Notebook, error)
}

const (
	// NotebookFence is the block in which the model proposes a notebook:
	// {"title": "…", "body": "markdown"}.
	NotebookFence = "keel-notebook"

	// maxNotebooksPerAsk bounds the notebooks attached to one question.
	maxNotebooksPerAsk = 2
	// maxNotebookTitleBytes and maxNotebookDraftBytes bound a draft.
	maxNotebookTitleBytes = 120
	maxNotebookDraftBytes = 32 * 1024
	// maxNotebookSaveBytes bounds what the user saves from a draft (they
	// may have added to it).
	maxNotebookSaveBytes = 256 * 1024
	// maxNotebookProposalsKept is how many notebook drafts a conversation
	// keeps in memory; an answer may propose one.
	maxNotebookProposalsKept = 8
	// maxNotebookDrafts and notebookDraftTTL bound the drafts the user
	// opened: each is used once.
	maxNotebookDrafts = 32
	notebookDraftTTL  = 24 * time.Hour
)

var (
	ErrNoNotebooks     = errors.New("agent: notebooks are not available")
	ErrNotebookClosed  = errors.New("agent: this notebook draft was already saved or dismissed")
	ErrNotebookExpired = errors.New("agent: this notebook draft expired; ask the agent again, or write the notebook yourself")
	// ErrNoDraft refuses a save whose draftId keel serve does not hold
	// (never opened, already saved, older than a day, or its proposal was
	// dismissed).
	ErrNoDraft = errors.New("agent: this draft is no longer held by Keel (it was saved already, dismissed, or is older than a day); open it again from the agent's card")

	// The block may be fenced with three or more backticks; its closing
	// fence must start a line. A JSON string holds no raw line break, so a
	// ``` inside the body (a shell fence, escaped as \n```sh) never ends it.
	notebookFenceRe = regexp.MustCompile("(?s)`{3,}[ \\t]*" + regexp.QuoteMeta(NotebookFence) + "[ \\t]*\\r?\\n(.*?)\\r?\\n[ \\t]*`{3,}[ \\t]*(?:\\r?\\n|$)")
	notebookStatus  = map[string]bool{"open": true, "saved": true, "dismissed": true}
)

// NotebookProposal is a notebook the agent proposed, as the panel shows it.
type NotebookProposal struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Placeholders is set when the body holds a redaction placeholder
	// such as [secret:1]: the review banner says to fill them in, and a
	// fence holding one is never typed.
	Placeholders bool `json:"placeholders"`
	// Withheld is set when the body names a value Keel withheld from the
	// model ({{name}}: a captured value or a group variable).
	Withheld   bool       `json:"withheld,omitempty"`
	Bytes      int        `json:"bytes"`
	FromPane   bool       `json:"fromPane"`
	SeenIn     string     `json:"seenIn,omitempty"`
	PanesRead  []string   `json:"panesRead,omitempty"`
	Status     string     `json:"status"`
	NotebookID string     `json:"notebookId,omitempty"`
	ProposedAt *time.Time `json:"proposedAt,omitempty"`
	Expired    bool       `json:"expired,omitempty"`
}

type nbProposal struct {
	NotebookProposal
	busy bool
}

func (p *nbProposal) view() NotebookProposal { return p.NotebookProposal }

// NotebookDraft is what Open draft answers: the draft for the editor, and
// the draftId its Save must name.
type NotebookDraft struct {
	DraftID        string           `json:"draftId"`
	ConversationID string           `json:"conversationId"`
	Proposal       NotebookProposal `json:"proposal"`
	Name           string           `json:"name"`
	Body           string           `json:"body"`
	Placeholders   bool             `json:"placeholders"`
	Withheld       bool             `json:"withheld,omitempty"`
}

// NotebookSaveRequest is the body of a draft's Save: what the user has in
// the editor now.
type NotebookSaveRequest struct {
	Name  string               `json:"name"`
	Cells []store.NotebookCell `json:"cells"`
}

// SavedNotebook is a notebook saved from a draft.
type SavedNotebook struct {
	Notebook store.Notebook    `json:"notebook"`
	Redacted int               `json:"redacted"`
	Proposal *NotebookProposal `json:"proposal,omitempty"`
}

type nbDraft struct {
	convID, propID string
	at             time.Time
}

type rawNotebook struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// parseNotebookBlock takes the notebook blocks out of a reply: the first
// that parses, and the text without any of them.
func parseNotebookBlock(text string) (string, *rawNotebook) {
	var out *rawNotebook
	rest := notebookFenceRe.ReplaceAllStringFunc(text, func(block string) string {
		m := notebookFenceRe.FindStringSubmatch(block)
		if len(m) < 2 || out != nil {
			return "\n"
		}
		var nb rawNotebook
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &nb) == nil && strings.TrimSpace(nb.Body) != "" {
			out = &nb
		}
		return "\n"
	})
	return rest, out
}

// --- reading attached notebooks ---

// notebookContext reads the notebooks the user attached to this question,
// and only those, through the Auditor. It is the only place the agent
// service reads a notebook for a provider message
// (internal/agent/notebooks_invariants_test.go).
func (s *Service) notebookContext(ids []string, r *redact.Redactor) (string, []TraceStep, error) {
	if len(ids) == 0 {
		return "", nil, nil
	}
	if s.nbs == nil {
		return "", nil, ErrNoNotebooks
	}
	var (
		b     strings.Builder
		steps []TraceStep
		n     int
	)
	for _, id := range ids {
		nb, err := s.nbs.GetNotebook(id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				steps = append(steps, TraceStep{Status: "skipped", Reason: "notebook-missing", Host: "a notebook", NotebookID: id})
				continue
			}
			return "", nil, err
		}
		n++
		ex, err := s.audit.ReadNotebook(n, nb.ID, nb.Name, notebookText(nb), r)
		if err != nil {
			return "", nil, err
		}
		b.WriteString(ex.Text.String())
		b.WriteString("\n\n")
		steps = append(steps, TraceStep{Status: "notebook", Host: "notebook", Notebook: ex.Name, NotebookID: nb.ID, Bytes: ex.Bytes, Truncated: ex.Truncated, Redactions: ex.Report.Strings()})
	}
	return b.String(), steps, nil
}

// notebookText is a notebook's markdown: its cells, in order.
func notebookText(nb store.Notebook) string {
	parts := make([]string, 0, len(nb.Cells))
	for _, c := range nb.Cells {
		parts = append(parts, c.Body)
	}
	return strings.Join(parts, "\n\n")
}

// --- drafts the agent proposes ---

// cleanDraftText keeps the line breaks and tabs of markdown and drops
// every other control character and every invisible format character
// (zero-width, bidi), so a command in a fence reads as it would be typed.
func cleanDraftText(s string) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\r\n", "\n"), "")
	return strings.Map(func(c rune) rune {
		switch {
		case c == '\n' || c == '\t':
			return c
		case c == '\r' || c == 0x2028 || c == 0x2029 || c == 0x85:
			return '\n'
		case unicode.IsControl(c), unicode.Is(unicode.Cf, c):
			return -1
		}
		return c
	}, s)
}

// namedRe matches a {{name}} that stands for a value Keel withholds from
// the model (a captured value or a group variable).
var namedRe = regexp.MustCompile(`\{\{[a-z_][a-z0-9_]{0,31}(\|q)?\}\}`)

// inlineImageRe matches a markdown inline image at the start of the text:
// ![alt](url) or ![alt](<url> "title").
var inlineImageRe = regexp.MustCompile(`^!\[([^\]\n]*)\]\(\s*<?([^\s()<>]*)>?(?:\s+(?:"[^"\n]*"|'[^'\n]*'|\([^)\n]*\)))?\s*\)`)

// defuseImages turns every markdown image in a draft into text, except a
// data: image. The notebook editor renders an image as <img src>, and the
// webview would fetch its URL when the draft opens: a model steered by
// pane text could put what it read into that URL. An inline image becomes
// its alt text and its URL as inline code; any other "![" (a reference
// image) is escaped so it reads as text. Fenced code and code spans are
// left as they are (they render as text already). keel serve also sends
// img-src 'self' data: blob: on every page (httpapi), so a remote image
// that slips through is not fetched either.
func defuseImages(md string) string {
	lines := strings.Split(md, "\n")
	fence := ""
	for i, ln := range lines {
		t := strings.TrimLeft(ln, " ")
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.Trim(t, string(fence[0])+" \t") == "" {
				fence = ""
			}
			continue
		}
		if len(ln)-len(t) <= 3 && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")) {
			n := 0
			for n < len(t) && t[n] == t[0] {
				n++
			}
			fence = t[:n]
			continue
		}
		lines[i] = defuseLine(ln)
	}
	return strings.Join(lines, "\n")
}

// defuseLine rewrites the images of one line outside its code spans.
func defuseLine(ln string) string {
	if !strings.Contains(ln, "![") {
		return ln
	}
	var b strings.Builder
	for i := 0; i < len(ln); {
		if ln[i] == '`' {
			// A code span: copy it up to the matching run of backticks.
			n := 0
			for i+n < len(ln) && ln[i+n] == '`' {
				n++
			}
			run := ln[i : i+n]
			if end := strings.Index(ln[i+n:], run); end >= 0 {
				b.WriteString(ln[i : i+n+end+n])
				i += n + end + n
				continue
			}
			b.WriteString(run)
			i += n
			continue
		}
		if ln[i] == '\\' && i+1 < len(ln) {
			b.WriteString(ln[i : i+2])
			i += 2
			continue
		}
		if strings.HasPrefix(ln[i:], "![") {
			if m := inlineImageRe.FindStringSubmatch(ln[i:]); m != nil {
				if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m[2])), "data:") {
					b.WriteString(m[0])
				} else {
					alt := strings.ReplaceAll(strings.TrimSpace(m[1]), "![", `!\[`)
					url := strings.ReplaceAll(m[2], "`", "")
					if alt != "" {
						b.WriteString(alt + " ")
					}
					if url != "" {
						b.WriteString("`" + url + "`")
					}
				}
				i += len(m[0])
				continue
			}
			b.WriteString(`!\[`)
			i += 2
			continue
		}
		b.WriteByte(ln[i])
		i++
	}
	return b.String()
}

// notebookProposal turns the model's notebook block into a proposal,
// redacted with the request's Redactor (vault values, captured values,
// group variables, secret-looking values, and IPs or hosts when the user
// asked). Placeholders left in it are shown as they are.
func (s *Service) notebookProposal(raw *rawNotebook, excerpts []agent.Excerpt, r *redact.Redactor, now time.Time) *nbProposal {
	if raw == nil || s.nbs == nil {
		return nil
	}
	title, _ := store.CleanLine(r.Text(truncate(raw.Title, 4*maxNotebookTitleBytes)).String(), 4*maxNotebookTitleBytes)
	title = strings.TrimSpace(truncate(title, maxNotebookTitleBytes))
	if title == "" {
		title = "Notes from the agent"
	}
	body := defuseImages(cleanDraftText(r.Text(truncate(raw.Body, 2*maxNotebookDraftBytes)).String()))
	body = strings.TrimSpace(truncate(body, maxNotebookDraftBytes))
	if body == "" {
		return nil
	}
	p := &nbProposal{NotebookProposal: NotebookProposal{ID: newID("nbp_"), Title: title, Body: body, Bytes: len(body), Placeholders: hasPlaceholder(body) || hasPlaceholder(title), Withheld: namedRe.MatchString(body), Status: "open", ProposedAt: &now}}
	seen := map[string]bool{}
	for _, ex := range excerpts {
		if !seen[ex.Pane.HostName] {
			seen[ex.Pane.HostName] = true
			p.PanesRead = append(p.PanesRead, ex.Pane.HostName)
		}
	}
	if len(excerpts) > 0 {
		sh := buildShingles(excerpts)
		for _, para := range strings.Split(body, "\n") {
			if host, ok := seenInPanes(para, sh); ok && strings.TrimSpace(para) != "" {
				p.FromPane, p.SeenIn = true, host
				break
			}
		}
	}
	return p
}

func notebookRecord(p *nbProposal) store.AgentChatNotebook {
	at := time.Time{}
	if p.ProposedAt != nil {
		at = *p.ProposedAt
	}
	return store.AgentChatNotebook{ID: p.ID, Title: p.Title, Body: p.Body, Placeholders: p.Placeholders, FromPane: p.FromPane, SeenIn: p.SeenIn, PanesRead: append([]string(nil), p.PanesRead...), Status: p.Status, NotebookID: p.NotebookID, ProposedAt: at}
}

func notebookProposalFromRecord(r store.AgentChatNotebook) NotebookProposal {
	at := r.ProposedAt
	st := r.Status
	if !notebookStatus[st] {
		st = "open"
	}
	return NotebookProposal{ID: r.ID, Title: r.Title, Body: r.Body, Bytes: len(r.Body), Placeholders: r.Placeholders, Withheld: namedRe.MatchString(r.Body), FromPane: r.FromPane, SeenIn: r.SeenIn, PanesRead: r.PanesRead, Status: st, NotebookID: r.NotebookID, ProposedAt: &at, Expired: true}
}

func (c *conversation) nbPropLocked(id string) *nbProposal {
	for _, p := range c.nbProps {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (c *conversation) recordNbPropLocked(id string) *store.AgentChatNotebook {
	for i := len(c.record) - 1; i >= 0; i-- {
		for j := range c.record[i].Notebooks {
			if c.record[i].Notebooks[j].ID == id {
				return &c.record[i].Notebooks[j]
			}
		}
	}
	return nil
}

// nbPropFor finds a proposal held in memory, or says why it cannot be
// used. Call with s.mu held.
func (s *Service) nbPropForLocked(convID, id string) (*conversation, *nbProposal, error) {
	c := s.convs[convID]
	if c == nil {
		return nil, nil, ErrNotFound
	}
	p := c.nbPropLocked(id)
	if p == nil {
		if c.recordNbPropLocked(id) != nil {
			return c, nil, ErrNotebookExpired
		}
		return c, nil, ErrNotFound
	}
	return c, p, nil
}

func (s *Service) setNbStatusLocked(c *conversation, p *nbProposal, status, notebookID string) {
	p.Status, p.NotebookID, p.busy = status, notebookID, false
	if rec := c.recordNbPropLocked(p.ID); rec != nil {
		rec.Status, rec.NotebookID = status, notebookID
	}
}

// OpenNotebookDraft is the card's Open draft: it hands the editor the
// draft and a draftId that its Save must name. keel serve keeps the
// draftId (at most 32, for a day, each used once), so a save says where
// the notebook came from without trusting the client. Nothing is saved.
func (s *Service) OpenNotebookDraft(convID, id string) (NotebookDraft, error) {
	if s.nbs == nil {
		return NotebookDraft{}, ErrNoNotebooks
	}
	now := s.now()
	draftID := newID("nbd_")
	s.mu.Lock()
	_, p, err := s.nbPropForLocked(convID, id)
	if err != nil {
		s.mu.Unlock()
		return NotebookDraft{}, err
	}
	if p.Status != "open" || p.busy {
		s.mu.Unlock()
		return NotebookDraft{}, ErrNotebookClosed
	}
	if s.nbDrafts == nil {
		s.nbDrafts = map[string]nbDraft{}
	}
	for k, d := range s.nbDrafts {
		if now.Sub(d.at) > notebookDraftTTL {
			delete(s.nbDrafts, k)
		}
	}
	for len(s.nbDrafts) >= maxNotebookDrafts {
		oldest, at := "", now
		for k, d := range s.nbDrafts {
			if oldest == "" || d.at.Before(at) {
				oldest, at = k, d.at
			}
		}
		delete(s.nbDrafts, oldest)
	}
	s.nbDrafts[draftID] = nbDraft{convID: convID, propID: id, at: now}
	view := p.view()
	s.mu.Unlock()
	s.audit.NotebookEvent(fmt.Sprintf("notebook draft opened for review by you (%d bytes)", view.Bytes), []string{view.ID, draftID}, redact.Report{})
	return NotebookDraft{DraftID: draftID, ConversationID: convID, Proposal: view, Name: view.Title, Body: view.Body, Placeholders: view.Placeholders, Withheld: view.Withheld}, nil
}

// SaveNotebookDraft is the editor's Save on a draft: it creates a new
// notebook from what the user has in the editor now, marked as from the
// agent. The draftId must be one keel serve handed out and still holds; it
// is used up. Name and text are redacted for secrets before they are kept
// (vault values, secret-looking values), as notes and runbooks are.
func (s *Service) SaveNotebookDraft(draftID string, in NotebookSaveRequest) (SavedNotebook, error) {
	if s.nbs == nil {
		return SavedNotebook{}, ErrNoNotebooks
	}
	size := len(in.Name)
	for _, c := range in.Cells {
		size += len(c.Body)
	}
	if size > maxNotebookSaveBytes {
		return SavedNotebook{}, badRequest(fmt.Sprintf("a notebook saved from a draft is at most %d KiB", maxNotebookSaveBytes/1024))
	}
	if len(in.Cells) > 64 {
		return SavedNotebook{}, badRequest("at most 64 cells")
	}
	sr, err := s.saveRedactor()
	if err != nil {
		return SavedNotebook{}, err
	}
	name, _ := store.CleanLine(sr.Text(in.Name).String(), 4*maxNotebookTitleBytes)
	name = strings.TrimSpace(truncate(name, maxNotebookTitleBytes))
	nb := store.Notebook{Name: name}
	for _, c := range in.Cells {
		body := sr.Text(c.Body).String()
		if !utf8.ValidString(body) {
			body = strings.ToValidUTF8(body, "")
		}
		nb.Cells = append(nb.Cells, store.NotebookCell{ID: c.ID, Body: body})
	}
	rep := sr.Report()

	// Claim the draft and its proposal under the lock: one Save makes one
	// notebook; a second (another window, a double press) gets 409.
	now := s.now()
	s.mu.Lock()
	d, ok := s.nbDrafts[draftID]
	if !ok || now.Sub(d.at) > notebookDraftTTL {
		delete(s.nbDrafts, draftID)
		s.mu.Unlock()
		return SavedNotebook{}, ErrNoDraft
	}
	c := s.convs[d.convID]
	var p *nbProposal
	if c != nil {
		p = c.nbPropLocked(d.propID)
	}
	if p != nil && (p.busy || p.Status != "open") {
		s.mu.Unlock()
		return SavedNotebook{}, ErrNotebookClosed
	}
	// Every draft opened from this proposal goes with this one: a proposal
	// makes one notebook, also once keel serve no longer holds the
	// proposal itself (its conversation evicted or deleted).
	for k, od := range s.nbDrafts {
		if od.convID == d.convID && od.propID == d.propID {
			delete(s.nbDrafts, k)
		}
	}
	if p != nil {
		p.busy = true
	}
	s.mu.Unlock()

	saved, err := s.nbs.AddAgentNotebook(nb)
	if err != nil {
		s.mu.Lock()
		if p != nil {
			p.busy = false
		}
		// The draft can be saved again once the store works.
		s.nbDrafts[draftID] = d
		s.mu.Unlock()
		return SavedNotebook{}, err
	}
	bytes := 0
	for _, cell := range saved.Cells {
		bytes += len(cell.Body)
	}
	s.audit.NotebookEvent(fmt.Sprintf("notebook draft proposed by the agent saved by you as a new notebook (%d bytes, %d cells)", bytes, len(saved.Cells)), []string{saved.ID, d.propID, draftID}, rep)
	out := SavedNotebook{Notebook: saved, Redacted: rep.Total()}
	if p != nil {
		s.mu.Lock()
		s.setNbStatusLocked(c, p, "saved", saved.ID)
		view := p.view()
		s.mu.Unlock()
		out.Proposal = &view
		s.persist(c)
	}
	return out, nil
}

// DismissNotebookProposal closes a notebook draft without saving anything;
// the drafts opened from it can no longer be saved.
func (s *Service) DismissNotebookProposal(convID, id string) (NotebookProposal, error) {
	s.mu.Lock()
	c, p, err := s.nbPropForLocked(convID, id)
	if err != nil {
		s.mu.Unlock()
		return NotebookProposal{}, err
	}
	if p.busy || p.Status != "open" {
		s.mu.Unlock()
		return NotebookProposal{}, ErrNotebookClosed
	}
	for k, d := range s.nbDrafts {
		if d.convID == convID && d.propID == id {
			delete(s.nbDrafts, k)
		}
	}
	s.setNbStatusLocked(c, p, "dismissed", "")
	view := p.view()
	s.mu.Unlock()
	s.audit.NotebookEvent("notebook draft proposed by the agent dismissed by you", []string{id}, redact.Report{})
	s.persist(c)
	return view, nil
}

// UndoNotebookProposal opens a dismissed draft again. A saved one stays
// saved: the notebook is the user's now, deleted from Notebooks if wanted.
func (s *Service) UndoNotebookProposal(convID, id string) (NotebookProposal, error) {
	s.mu.Lock()
	c, p, err := s.nbPropForLocked(convID, id)
	if err != nil {
		s.mu.Unlock()
		return NotebookProposal{}, err
	}
	if p.busy || p.Status != "dismissed" {
		s.mu.Unlock()
		return NotebookProposal{}, ErrNotebookClosed
	}
	s.setNbStatusLocked(c, p, "open", "")
	view := p.view()
	s.mu.Unlock()
	s.persist(c)
	return view, nil
}

// HasPlaceholder reports whether a command holds a redaction placeholder
// such as [secret:1]. keel serve refuses to type a notebook fence that
// holds one (the session socket's notebook-labelled inject), as
// AuthorizeType does for a proposal (ErrPlaceholder).
func HasPlaceholder(s string) bool { return hasPlaceholder(s) }
