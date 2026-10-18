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
	"keel/internal/shellmark"
	"keel/internal/store"
)

// Runbooks (docs/ui-redesign.md §16).
//
// A runbook is a checklist the user curates: ordered steps, each with a
// title, a description, an optional command and an optional expected
// result. They are kept encrypted in keel.memory beside the notes (store).
// The service is their only way in and out:
//
//   - The user adds, edits and deletes them (AddRunbook, UpdateRunbook,
//     DeleteRunbook), also from a draft made from a notebook or a saved
//     conversation (RunbookDraft). Every text is redacted for secrets
//     before it is kept (saveRedactor, as for notes); every change is logged
//     as agent.runbook without the text.
//   - The agent never writes one. Ask may only propose a runbook (a
//     ```keel-runbook block); nothing is kept until the user saves it
//     (SaveRunbookProposal), and what is saved starts with "Agent may pick"
//     off until the user reviews it.
//   - Using one: the user picks it (AskRequest.RunbookID), or the agent
//     picks one of the runbooks Keel offered for the question (a
//     ```keel-runbook-use block naming it by number). From then on every
//     ask in the conversation sends the runbook, framed as user-curated
//     context, with the steps' progress, through the Auditor (scope: only
//     with a pane read the agent may read; redaction; agent.runbook).
//   - Walking it: Keel itself turns the current step's command into one
//     ordinary command proposal (never the model's text, never more than
//     one, never the next step before this one is done or skipped). It is
//     typed only through the approve-before-type path, like any proposal
//     (AuthorizeType). When the pane's shell reports the command with OSC
//     133 marks, the step records that exit status, labelled as reported;
//     otherwise the user marks the step done, skips or retries it
//     (StepAction).

// RunbookStore is where runbooks are kept. *store.Store satisfies it.
type RunbookStore interface {
	ListRunbooks() ([]store.Runbook, error)
	GetRunbook(id string) (store.Runbook, error)
	AddRunbook(b store.Runbook) (store.Runbook, error)
	UpdateRunbook(b store.Runbook) (store.Runbook, error)
	SetRunbookAgentMayPick(id string, on bool) (store.Runbook, error)
	MarkRunbookUsed(id, host string, at time.Time) error
	DeleteRunbook(id string) (store.Runbook, error)
}

// StepSource finds the command a runbook step started in a pane, from its
// shell-integration marks. The runtime adapter in keel serve satisfies it
// (with the pane's stream offset taken right before the step was typed).
type StepSource interface {
	CommandsAfter(sessionID string, from uint64) []shellmark.Command
}

// NotebookSource reads a notebook for a runbook draft. *store.Store
// satisfies it.
type NotebookSource interface {
	GetNotebook(id string) (store.Notebook, error)
}

const (
	// RunbookUseFence is the block in which the model picks one of the
	// runbooks Keel offered: {"runbook": N, "why": "…"}.
	RunbookUseFence = "keel-runbook-use"
	// RunbookFence is the block in which the model proposes a new runbook:
	// {"name": "…", "when": "…", "steps": [{"title", "description",
	// "command", "expect"}]}.
	RunbookFence = "keel-runbook"

	// maxRunbooksOffered bounds the runbooks offered with one question.
	maxRunbooksOffered = 8
	// maxRunbookProposalsKept is how many proposed runbooks a conversation
	// keeps in memory; an answer may propose one.
	maxRunbookProposalsKept = 8
	maxRunbookWhyBytes      = 300

	runActive   = "active"
	runFinished = "finished"
	runStopped  = "stopped"

	stepNext     = "next"
	stepProposed = "proposed"
	stepTyped    = "typed"
	stepDone     = "done"
	stepFailed   = "failed"
	stepSkipped  = "skipped"
)

var (
	ErrNoRunbooks     = errors.New("agent: runbooks are not available")
	ErrNoRun          = errors.New("agent: no runbook is in use in this conversation")
	ErrStepNotCurrent = errors.New("agent: only the current step can be changed; finish or skip the steps before it")
	ErrRunbookClosed  = errors.New("agent: this runbook proposal was already saved or dismissed")
	ErrRunbookExpired = errors.New("agent: this runbook proposal expired; add the runbook yourself on the Runbooks page")
	ErrNothingToDraft = errors.New("agent: nothing to make steps from")
	// ErrSecretInCommand refuses a step command the save Redactor would
	// change: kept, it would hold a literal placeholder such as [secret:1]
	// (a shell glob) that could be typed.
	ErrSecretInCommand = errors.New("agent: a step command holds a secret or a secret-looking value, and Keel does not keep secrets in runbooks; read it from an environment variable set outside the runbook, or ask for it with a prompt such as read -s DB_PASS")
	runbookUseRe       = regexp.MustCompile("(?s)```[ \\t]*" + regexp.QuoteMeta(RunbookUseFence) + "[ \\t]*\\r?\\n(.*?)\\r?\\n?```")
	runbookFenceRe     = regexp.MustCompile("(?s)```[ \\t]*" + regexp.QuoteMeta(RunbookFence) + "[ \\t]*\\r?\\n(.*?)\\r?\\n?```")
	runbookStatusValid = map[string]bool{"open": true, "saved": true, "dismissed": true}
)

// RunbookStepView is a step as the Runbooks page shows it, with Keel's own
// rating of its command.
type RunbookStepView struct {
	store.RunbookStep
	Risk agent.Risk `json:"risk,omitempty"`
}

// RunbookView is a runbook as the page shows it.
type RunbookView struct {
	store.Runbook
	Steps []RunbookStepView `json:"steps"`
}

// RunbooksList is GET /api/agent/runbooks.
type RunbooksList struct {
	Runbooks   []RunbookView `json:"runbooks"`
	Unreadable bool          `json:"unreadable,omitempty"`
}

// SavedRunbook is a runbook after a save: Redacted counts the values
// replaced before it was kept.
type SavedRunbook struct {
	Runbook  RunbookView      `json:"runbook"`
	Redacted int              `json:"redacted"`
	Proposal *RunbookProposal `json:"proposal,omitempty"`
}

// RunbookInput is what the user sends to add or edit a runbook. Source is
// "user", "notebook" or "conversation" (only a proposal's Save makes an
// "agent" one).
type RunbookInput struct {
	Name         string              `json:"name"`
	When         string              `json:"when"`
	Hosts        []string            `json:"hosts"`
	AgentMayPick bool                `json:"agentMayPick"`
	Steps        []store.RunbookStep `json:"steps"`
	// Source and SourceRef are set by keel serve: a save takes them from
	// the draft DraftID names (made by RunbookDraft), never from the
	// client; without a draft the runbook is the user's own.
	Source    string `json:"source,omitempty"`
	SourceRef string `json:"sourceRef,omitempty"`
	DraftID   string `json:"draftId,omitempty"`
}

// draftOrigin is where a draft came from, kept by keel serve so a save
// cannot claim to be the user's own writing.
type draftOrigin struct {
	source, ref string
	at          time.Time
}

const (
	maxDrafts = 32
	draftTTL  = 24 * time.Hour
)

// RunbookProposal is a runbook the agent proposed, as the panel shows it.
type RunbookProposal struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	When       string              `json:"when,omitempty"`
	Steps      []store.RunbookStep `json:"steps"`
	FromPane   bool                `json:"fromPane"`
	SeenIn     string              `json:"seenIn,omitempty"`
	PanesRead  []string            `json:"panesRead,omitempty"`
	Status     string              `json:"status"`
	RunbookID  string              `json:"runbookId,omitempty"`
	ProposedAt *time.Time          `json:"proposedAt,omitempty"`
	Expired    bool                `json:"expired,omitempty"`
	busy       bool
}

// ProposalStep marks a proposal made from a runbook step.
type ProposalStep struct {
	RunbookID string `json:"runbookId,omitempty"`
	Name      string `json:"name,omitempty"`
	Step      int    `json:"step"`
	Of        int    `json:"of"`
	StepID    string `json:"stepId,omitempty"`
}

// RunState is GET /api/agent/conversations/{id}/runbook.
type RunState struct {
	Runbook   *store.AgentChatRunbook `json:"runbook"`
	NoRunbook bool                    `json:"noRunbook,omitempty"`
}

type rawRunbookUse struct {
	Runbook int    `json:"runbook"`
	Why     string `json:"why"`
}

type rawRunbook struct {
	Name  string `json:"name"`
	When  string `json:"when"`
	Steps []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Command     string `json:"command"`
		Expect      string `json:"expect"`
	} `json:"steps"`
}

// parseRunbookBlocks takes the runbook blocks out of a reply: the first
// pick, the first proposed runbook, and the text without any of them.
func parseRunbookBlocks(text string) (string, *rawRunbookUse, *rawRunbook) {
	var use *rawRunbookUse
	rest := runbookUseRe.ReplaceAllStringFunc(text, func(block string) string {
		m := runbookUseRe.FindStringSubmatch(block)
		if len(m) < 2 || use != nil {
			return ""
		}
		var u rawRunbookUse
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &u) == nil && u.Runbook > 0 {
			use = &u
		}
		return ""
	})
	var book *rawRunbook
	rest = runbookFenceRe.ReplaceAllStringFunc(rest, func(block string) string {
		m := runbookFenceRe.FindStringSubmatch(block)
		if len(m) < 2 || book != nil {
			return ""
		}
		var b rawRunbook
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &b) == nil && len(b.Steps) > 0 {
			book = &b
		}
		return ""
	})
	return rest, use, book
}

// --- the Runbooks page ---

func runbookView(b store.Runbook) RunbookView {
	v := RunbookView{Runbook: b, Steps: make([]RunbookStepView, 0, len(b.Steps))}
	for _, st := range b.Steps {
		sv := RunbookStepView{RunbookStep: st}
		if st.Command != "" {
			sv.Risk = agent.Classify(st.Command)
		}
		v.Steps = append(v.Steps, sv)
	}
	return v
}

// ListRunbooks returns every runbook. An unreadable keel.memory is a state,
// not an error.
func (s *Service) ListRunbooks() (RunbooksList, error) {
	if s.books == nil {
		return RunbooksList{Runbooks: []RunbookView{}}, nil
	}
	books, err := s.books.ListRunbooks()
	if errors.Is(err, store.ErrMemoryUnreadable) {
		return RunbooksList{Runbooks: []RunbookView{}, Unreadable: true}, nil
	}
	if err != nil {
		return RunbooksList{}, err
	}
	out := RunbooksList{Runbooks: make([]RunbookView, 0, len(books))}
	for _, b := range books {
		out.Runbooks = append(out.Runbooks, runbookView(b))
	}
	return out, nil
}

// cleanRunbookText redacts what is kept in a runbook: every text field and
// every command, for secrets (vault values, secret-looking values), the way
// a note is. The raw text is redacted first, then cleaned, then redacted
// again (the Redactor also knows the cleaned forms of vault values).
func (s *Service) cleanRunbookText(in RunbookInput) (store.Runbook, redact.Report, error) {
	if len(in.Steps) > store.MaxRunbookSteps {
		return store.Runbook{}, redact.Report{}, badRequest(fmt.Sprintf("at most %d steps", store.MaxRunbookSteps))
	}
	r, err := s.saveRedactor()
	if err != nil {
		return store.Runbook{}, redact.Report{}, err
	}
	line := func(t string, max int) (string, error) {
		if len(t) > 4*max+64 {
			return "", badRequest(fmt.Sprintf("a field is at most %d bytes", max))
		}
		c, ok := store.CleanLine(r.Text(t).String(), max)
		if !ok {
			return "", badRequest(fmt.Sprintf("a field is at most %d bytes", max))
		}
		c, ok = store.CleanLine(r.Text(c).String(), max)
		if !ok {
			return "", badRequest(fmt.Sprintf("a field is at most %d bytes", max))
		}
		return c, nil
	}
	b := store.Runbook{AgentMayPick: in.AgentMayPick, Source: in.Source}
	if b.Name, err = line(in.Name, store.MaxRunbookNameBytes); err != nil {
		return store.Runbook{}, redact.Report{}, err
	}
	if b.When, err = line(in.When, store.MaxRunbookWhenBytes); err != nil {
		return store.Runbook{}, redact.Report{}, err
	}
	if b.SourceRef, err = line(in.SourceRef, store.MaxRunbookNameBytes); err != nil {
		b.SourceRef = ""
	}
	if len(in.Hosts) > store.MaxRunbookHosts {
		return store.Runbook{}, redact.Report{}, badRequest(fmt.Sprintf("at most %d folders or tags", store.MaxRunbookHosts))
	}
	for _, h := range in.Hosts {
		c, err := line(h, store.MaxRunbookHostBytes)
		if err != nil {
			return store.Runbook{}, redact.Report{}, err
		}
		b.Hosts = append(b.Hosts, c)
	}
	for _, st := range in.Steps {
		out := store.RunbookStep{ID: st.ID}
		if out.Title, err = line(st.Title, store.MaxRunbookTitleBytes); err != nil {
			return store.Runbook{}, redact.Report{}, err
		}
		if out.Description, err = line(st.Description, store.MaxRunbookTextBytes); err != nil {
			return store.Runbook{}, redact.Report{}, err
		}
		if out.Expect, err = line(st.Expect, store.MaxRunbookExpectBytes); err != nil {
			return store.Runbook{}, redact.Report{}, err
		}
		if len(st.Command) > 2*store.MaxRunbookCommandBytes {
			return store.Runbook{}, redact.Report{}, badRequest(fmt.Sprintf("a command is at most %d bytes", store.MaxRunbookCommandBytes))
		}
		cmd, ok := store.CleanRunbookCommand(st.Command)
		if !ok {
			return store.Runbook{}, redact.Report{}, badRequest(fmt.Sprintf("a command is at most %d bytes and %d lines, with no control or invisible characters", store.MaxRunbookCommandBytes, store.MaxRunbookCommandLines))
		}
		// A command is never kept redacted: a placeholder such as
		// [secret:1] is a shell glob that could be typed. One the Redactor
		// would change (as typed, or cleaned) is refused instead.
		if !commandKeepable(r, st.Command) || !commandKeepable(r, cmd) {
			return store.Runbook{}, redact.Report{}, fmt.Errorf("%w: step %d: %v", ErrBadRequest, len(b.Steps)+1, ErrSecretInCommand)
		}
		out.Command = cmd
		b.Steps = append(b.Steps, out)
	}
	return b, r.Report(), nil
}

func runbookErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, store.ErrRunbook), errors.Is(err, store.ErrRunbooksFull):
		return fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return err
}

// AddRunbook saves a runbook the user wrote, or made from a draft.
func (s *Service) AddRunbook(in RunbookInput) (SavedRunbook, error) {
	if s.books == nil {
		return SavedRunbook{}, ErrNoRunbooks
	}
	// The source comes from keel serve's own record of the draft, never
	// from the client: without a known draft it is the user's own.
	in.Source, in.SourceRef = store.RunbookSourceUser, ""
	if d, ok := s.takeDraft(in.DraftID); ok {
		in.Source, in.SourceRef = d.source, d.ref
	}
	b, rep, err := s.cleanRunbookText(in)
	if err != nil {
		return SavedRunbook{}, err
	}
	saved, err := s.books.AddRunbook(b)
	if err != nil {
		return SavedRunbook{}, runbookErr(err)
	}
	what := "runbook added by you"
	if saved.Source != store.RunbookSourceUser {
		what = "runbook added by you from a " + saved.Source
	}
	s.audit.RunbookEvent(fmt.Sprintf("%s (%d steps)", what, len(saved.Steps)), saved.ID, "", rep)
	return SavedRunbook{Runbook: runbookView(saved), Redacted: rep.Total()}, nil
}

// UpdateRunbook replaces a runbook's name, Use when, hosts, Agent may pick
// and steps.
func (s *Service) UpdateRunbook(id string, in RunbookInput) (SavedRunbook, error) {
	if s.books == nil {
		return SavedRunbook{}, ErrNoRunbooks
	}
	in.Source, in.SourceRef = "", ""
	b, rep, err := s.cleanRunbookText(in)
	if err != nil {
		return SavedRunbook{}, err
	}
	b.ID = id
	saved, err := s.books.UpdateRunbook(b)
	if err != nil {
		return SavedRunbook{}, runbookErr(err)
	}
	s.audit.RunbookEvent(fmt.Sprintf("runbook edited by you (%d steps)", len(saved.Steps)), saved.ID, "", rep)
	return SavedRunbook{Runbook: runbookView(saved), Redacted: rep.Total()}, nil
}

// SetRunbookAgentMayPick is the list's switch.
func (s *Service) SetRunbookAgentMayPick(id string, on bool) (RunbookView, error) {
	if s.books == nil {
		return RunbookView{}, ErrNoRunbooks
	}
	b, err := s.books.SetRunbookAgentMayPick(id, on)
	if err != nil {
		return RunbookView{}, runbookErr(err)
	}
	what := "runbook: the agent may no longer pick it (set by you)"
	if on {
		what = "runbook: the agent may pick it (set by you)"
	}
	s.audit.RunbookEvent(what, b.ID, "", redact.Report{})
	return runbookView(b), nil
}

// DeleteRunbook removes a runbook. A conversation using it keeps its copy
// of the steps.
func (s *Service) DeleteRunbook(id string) error {
	if s.books == nil {
		return ErrNoRunbooks
	}
	b, err := s.books.DeleteRunbook(id)
	if err != nil {
		return runbookErr(err)
	}
	s.audit.RunbookEvent("runbook deleted by you", b.ID, "", redact.Report{})
	return nil
}

// --- drafts from a notebook or a saved conversation ---

// DraftRequest is POST /api/agent/runbooks/draft: one of the two.
type DraftRequest struct {
	NotebookID     string `json:"notebookId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
}

// RunbookDraft makes an unsaved runbook from a notebook (its headings
// become steps, shell fences their commands) or from a conversation (its
// proposals become steps, typed ones first). Nothing is sent anywhere and
// nothing is kept: the page shows the draft for the user to edit and save.
// The draft is redacted for secrets like a save.
func (s *Service) RunbookDraft(req DraftRequest) (RunbookInput, error) {
	var in RunbookInput
	switch {
	case req.NotebookID != "":
		src, ok := s.st.(NotebookSource)
		if !ok {
			return RunbookInput{}, ErrNotFound
		}
		nb, err := src.GetNotebook(req.NotebookID)
		if err != nil {
			return RunbookInput{}, runbookErr(err)
		}
		var md []string
		for _, c := range nb.Cells {
			md = append(md, c.Body)
		}
		in = draftFromMarkdown(nb.Name, strings.Join(md, "\n\n"))
		in.Source, in.SourceRef = store.RunbookSourceNotebook, nb.Name
	case req.ConversationID != "":
		chat, err := s.chatForDraft(req.ConversationID)
		if err != nil {
			return RunbookInput{}, err
		}
		in = draftFromChat(chat)
		in.Source, in.SourceRef = store.RunbookSourceConversation, chat.Title
	default:
		return RunbookInput{}, badRequest("name a notebook or a conversation")
	}
	if len(in.Steps) == 0 {
		return RunbookInput{}, ErrNothingToDraft
	}
	// A command that holds a secret (or a placeholder from an earlier
	// redaction) is left out of the draft, with a note: the save would
	// refuse it.
	sr, err := s.saveRedactor()
	if err != nil {
		return RunbookInput{}, err
	}
	for i := range in.Steps {
		if c := in.Steps[i].Command; c != "" && !commandKeepable(sr, c) {
			in.Steps[i].Command = ""
			in.Steps[i].Description = truncate(strings.TrimSpace(in.Steps[i].Description+" "+secretLeftOut), store.MaxRunbookTextBytes-4)
		}
	}
	src, ref := in.Source, in.SourceRef
	in.Source, in.SourceRef = "", ""
	b, _, err := s.cleanRunbookText(in)
	if err != nil {
		return RunbookInput{}, err
	}
	ref, _ = store.CleanLine(sr.Text(ref).String(), store.MaxRunbookNameBytes)
	id := s.keepDraft(src, ref)
	return RunbookInput{Name: b.Name, When: b.When, Hosts: b.Hosts, Steps: b.Steps, Source: src, SourceRef: ref, DraftID: id}, nil
}

// secretLeftOut is the note on a step whose command was left out.
const secretLeftOut = "(Command left out: it held a secret. Use an environment variable or a read -s prompt.)"

// commandKeepable reports whether a command can be kept as it is: no
// placeholder from an earlier redaction and nothing the save Redactor
// would replace.
func commandKeepable(r *redact.Redactor, cmd string) bool {
	if hasPlaceholder(cmd) {
		return false
	}
	return r.Text(cmd).String() == cmd
}

func (s *Service) keepDraft(source, ref string) string {
	id := newID("dr_")
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drafts == nil {
		s.drafts = map[string]draftOrigin{}
	}
	for k, d := range s.drafts {
		if now.Sub(d.at) > draftTTL {
			delete(s.drafts, k)
		}
	}
	for len(s.drafts) >= maxDrafts {
		oldest, at := "", now
		for k, d := range s.drafts {
			if oldest == "" || d.at.Before(at) {
				oldest, at = k, d.at
			}
		}
		delete(s.drafts, oldest)
	}
	s.drafts[id] = draftOrigin{source: source, ref: ref, at: now}
	return id
}

func (s *Service) takeDraft(id string) (draftOrigin, bool) {
	if id == "" {
		return draftOrigin{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.drafts[id]
	if ok {
		delete(s.drafts, id)
	}
	if ok && s.now().Sub(d.at) > draftTTL {
		return draftOrigin{}, false
	}
	return d, ok
}

// chatForDraft is a conversation's record: in memory, or saved.
func (s *Service) chatForDraft(id string) (store.AgentChat, error) {
	if !convIDRe.MatchString(id) {
		return store.AgentChat{}, ErrNotFound
	}
	s.mu.Lock()
	if c := s.convs[id]; c != nil && len(c.record) > 0 {
		chat := c.snapshotLocked(s.now())
		s.mu.Unlock()
		return chat, nil
	}
	s.mu.Unlock()
	if s.chats == nil {
		return store.AgentChat{}, ErrNotFound
	}
	return s.savedChat(id)
}

var headingRe = regexp.MustCompile(`^#{1,6}\s+(.*)$`)

// draftFromMarkdown turns markdown into steps: each heading after the first
// H1 starts a step, its first paragraph is the description and its first
// shell fence (sh, bash, shell, zsh or no language) the command. With no
// headings, each shell fence is a step titled by the line before it.
func draftFromMarkdown(name, md string) RunbookInput {
	in := RunbookInput{Name: strings.TrimSpace(name)}
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var (
		steps    []store.RunbookStep
		cur      *store.RunbookStep
		lastText string
		inFence  bool
		shell    bool
		code     []string
		para     []string
	)
	flushPara := func() {
		t := strings.TrimSpace(strings.Join(para, " "))
		para = nil
		if t == "" {
			return
		}
		lastText = t
		if cur != nil && cur.Description == "" && cur.Command == "" {
			cur.Description = t
		}
	}
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			if !inFence {
				flushPara()
				lang := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trim, "```")))
				inFence, shell, code = true, lang == "" || lang == "sh" || lang == "bash" || lang == "shell" || lang == "zsh" || lang == "console", nil
				continue
			}
			inFence = false
			cmd := strings.TrimSpace(strings.Join(code, "\n"))
			if !shell || cmd == "" {
				continue
			}
			if cur == nil || cur.Command != "" {
				title := lastText
				if title == "" {
					title = fmt.Sprintf("Step %d", len(steps)+1)
				}
				steps = append(steps, store.RunbookStep{Title: title})
				cur = &steps[len(steps)-1]
			}
			cur.Command = cmd
			continue
		}
		if inFence {
			code = append(code, strings.TrimPrefix(ln, "$ "))
			continue
		}
		if m := headingRe.FindStringSubmatch(trim); m != nil {
			flushPara()
			title := strings.TrimSpace(m[1])
			if strings.HasPrefix(trim, "# ") && len(steps) == 0 {
				// A first H1 names the document, not a step.
				if in.Name == "" {
					in.Name = title
				}
				lastText = ""
				continue
			}
			steps = append(steps, store.RunbookStep{Title: title})
			cur = &steps[len(steps)-1]
			lastText = ""
			continue
		}
		if trim == "" {
			flushPara()
			continue
		}
		para = append(para, strings.TrimLeft(trim, "-*> "))
	}
	flushPara()
	for i := range steps {
		steps[i].Title = truncate(steps[i].Title, store.MaxRunbookTitleBytes-4)
		steps[i].Description = truncate(steps[i].Description, store.MaxRunbookTextBytes-4)
		if len(steps[i].Command) > store.MaxRunbookCommandBytes {
			steps[i].Command = ""
		}
	}
	if len(steps) > store.MaxRunbookSteps {
		steps = steps[:store.MaxRunbookSteps]
	}
	if in.Name == "" {
		in.Name = "Runbook"
	}
	in.Name = truncate(in.Name, store.MaxRunbookNameBytes-4)
	in.Steps = steps
	return in
}

// draftFromChat turns a conversation's proposals into steps, in order,
// once per command: the typed ones when any were typed, else all of them.
func draftFromChat(chat store.AgentChat) RunbookInput {
	in := RunbookInput{Name: truncate(chat.Title, store.MaxRunbookNameBytes-4)}
	if in.Name == "" {
		in.Name = "Runbook"
	}
	var all, typed []store.AgentChatProposal
	for _, t := range chat.Turns {
		for _, p := range t.Proposals {
			all = append(all, p)
			if p.TypedAt != nil {
				typed = append(typed, p)
			}
		}
	}
	src := all
	if len(typed) > 0 {
		src = typed
	}
	seen := map[string]bool{}
	for _, p := range src {
		if p.Command == "" || seen[p.Command] || len(in.Steps) >= store.MaxRunbookSteps {
			continue
		}
		seen[p.Command] = true
		title := p.Why
		if title == "" {
			title = fmt.Sprintf("Step %d", len(in.Steps)+1)
		}
		in.Steps = append(in.Steps, store.RunbookStep{Title: truncate(title, store.MaxRunbookTitleBytes-4), Command: p.Command})
	}
	if q := firstQuestion(chat); q != "" {
		in.When = truncate(q, store.MaxRunbookWhenBytes-4)
	}
	return in
}

func firstQuestion(chat store.AgentChat) string {
	for _, t := range chat.Turns {
		if t.Question != "" {
			return t.Question
		}
	}
	return ""
}

// --- using a runbook in a conversation ---

// runbookTurn is what one ask does with runbooks.
type runbookTurn struct {
	text    string          // framed text for the provider message
	trace   []TraceStep     // "Used runbook · <name>"
	offered []store.Runbook // offered for the agent to pick from
	panes   []agent.Pane
}

// hostFits reports whether a runbook's Hosts include a pane's host: its
// folder or one of its tags (any case). Empty Hosts fit every host.
func hostFits(hosts []string, h agent.Host) bool {
	if len(hosts) == 0 {
		return true
	}
	for _, want := range hosts {
		if strings.EqualFold(want, h.Group) {
			return true
		}
		for _, t := range h.Tags {
			if strings.EqualFold(want, t) {
				return true
			}
		}
	}
	return false
}

// startRunLocked makes b the conversation's runbook in use, replacing any
// other. Call with s.mu held.
func (s *Service) startRunLocked(c *conversation, b store.Runbook, by, why string, turnNo int, now time.Time) {
	if c.run != nil && c.run.Status == runActive {
		c.run.Status = runStopped
	}
	run := &store.AgentChatRunbook{ID: newID("ru_"), RunbookID: b.ID, Name: b.Name, PickedBy: by, Why: why, Status: runActive, Turn: turnNo, StartedAt: now, Hosts: append([]string(nil), b.Hosts...)}
	for _, st := range b.Steps {
		rs := store.AgentChatRunStep{ID: st.ID, Title: st.Title, Description: st.Description, Command: st.Command, Expect: st.Expect, State: stepNext}
		if st.Command != "" {
			rs.Risk = string(agent.Classify(st.Command))
		}
		run.Steps = append(run.Steps, rs)
	}
	c.run, c.runMarks, c.noRunbook = run, map[string]uint64{}, false
}

// currentStep is the first step not done or skipped, or -1 when every step
// is.
func currentStep(run *store.AgentChatRunbook) int {
	for i, st := range run.Steps {
		if st.State != stepDone && st.State != stepSkipped {
			return i
		}
	}
	return -1
}

// runbookBefore decides what this ask sends about runbooks, before the
// provider is called: the runbook in use (its steps and progress), or the
// runbooks the agent may pick from. Either goes only with a pane the agent
// may read, through the Auditor.
func (s *Service) runbookBefore(c *conversation, req AskRequest, read []agent.Excerpt, byID map[string]agent.Host, scope agent.Scope, r *redact.Redactor) (*runbookTurn, error) {
	t := &runbookTurn{}
	for _, ex := range read {
		t.panes = append(t.panes, ex.Pane)
	}
	if s.books == nil {
		if req.RunbookID != "" {
			return nil, ErrNoRunbooks
		}
		return t, nil
	}
	now := s.now()
	if req.RunbookID != "" {
		b, err := s.books.GetRunbook(req.RunbookID)
		if err != nil {
			return nil, runbookErr(err)
		}
		s.mu.Lock()
		s.startRunLocked(c, b, "user", "", c.asks+1, now)
		s.mu.Unlock()
		s.audit.RunbookEvent(fmt.Sprintf("runbook started by you (%d steps)", len(b.Steps)), b.ID, "", redact.Report{})
	}
	s.mu.Lock()
	run := c.run.Copy()
	noRunbook := c.noRunbook
	s.mu.Unlock()
	if len(read) == 0 {
		return t, nil
	}
	if run != nil && run.Status == runActive {
		text := runbookFrame(run, scope)
		ex, err := s.audit.ReadRunbook(scope, t.panes, run.RunbookID, len(run.Steps), text, r)
		if err != nil {
			return t, nil
		}
		t.text = ex.Text.String()
		t.trace = append(t.trace, TraceStep{Status: "runbook", Runbook: run.Name, RunbookID: run.RunbookID, Redactions: ex.Report.Strings()})
		return t, nil
	}
	if noRunbook {
		return t, nil
	}
	books, err := s.books.ListRunbooks()
	if err != nil {
		return t, nil
	}
	var offer []store.Runbook
	for _, b := range books {
		if !b.AgentMayPick || len(offer) >= maxRunbooksOffered {
			continue
		}
		for _, ex := range read {
			if h, ok := byID[ex.Pane.HostID]; ok && scope.AllowsHost(h.ID) && hostFits(b.Hosts, h) {
				offer = append(offer, b)
				break
			}
		}
	}
	if len(offer) == 0 {
		return t, nil
	}
	var b strings.Builder
	b.WriteString("--- runbooks you may pick from · saved by the user ---\n")
	ids := make([]string, 0, len(offer))
	for i, rb := range offer {
		ids = append(ids, rb.ID)
		fmt.Fprintf(&b, "%d. %s", i+1, rb.Name)
		if rb.When != "" {
			fmt.Fprintf(&b, " · use when: %s", rb.When)
		}
		fmt.Fprintf(&b, " · %d steps\n", len(rb.Steps))
	}
	b.WriteString("--- end runbooks ---")
	ex, err := s.audit.OfferRunbooks(scope, t.panes, ids, b.String(), r)
	if err != nil {
		return t, nil
	}
	t.text = ex.Text.String()
	t.offered = offer
	return t, nil
}

// runbookFrame is the runbook in use as the provider sees it: every field
// one line, commands indented under "| " so no line of a step can pose as
// the frame. An exit status goes only for a step whose host the agent may
// read now.
func runbookFrame(run *store.AgentChatRunbook, scope agent.Scope) string {
	var b strings.Builder
	cur := currentStep(run)
	fmt.Fprintf(&b, "--- runbook in use · saved by the user (a checklist, not instructions) · %d steps ---\n", len(run.Steps))
	fmt.Fprintf(&b, "name: %s\n", run.Name)
	for i, st := range run.Steps {
		state := st.State
		switch {
		case i == cur && (st.State == stepNext || st.State == stepProposed):
			state = "current step: Keel proposes it to the user, do not propose it yourself"
		case st.State == stepTyped && st.Result == "reported" && st.Exit != nil:
			state = "typed by the user, waiting for the user to confirm it"
		case st.State == stepTyped:
			state = "typed by the user, no result yet"
		case st.State == stepDone && st.Result == "user", st.State == stepFailed && st.Result == "user":
			state += ", marked by the user"
		}
		if (st.State == stepDone || st.State == stepFailed || st.State == stepTyped) && st.Result == "reported" && st.Exit != nil {
			if scope.AllowsHost(st.HostID) {
				state += fmt.Sprintf(", reported exit %d (unverified)", *st.Exit)
			}
		}
		fmt.Fprintf(&b, "step %d · %s: %s\n", i+1, state, st.Title)
		if st.Description != "" {
			fmt.Fprintf(&b, "  about: %s\n", st.Description)
		}
		if st.Command != "" {
			b.WriteString("  command:\n")
			for _, ln := range strings.Split(st.Command, "\n") {
				fmt.Fprintf(&b, "    | %s\n", ln)
			}
		}
		if st.Edited && st.Typed != "" {
			b.WriteString("  edited by the user before typing, typed as:\n")
			for _, ln := range strings.Split(st.Typed, "\n") {
				fmt.Fprintf(&b, "    | %s\n", ln)
			}
		}
		if st.Expect != "" {
			fmt.Fprintf(&b, "  expected: %s\n", st.Expect)
		}
	}
	b.WriteString("--- end runbook ---")
	return b.String()
}

// runbookAfter applies the model's pick, if Keel offered runbooks, and
// then proposes the current step of the runbook in use, if it needs a
// proposal and a pane was read. It makes at most one proposal: the
// current step's, from the saved command, never the model's text.
func (s *Service) runbookAfter(c *conversation, t *runbookTurn, use *rawRunbookUse, read []agent.Excerpt, byID map[string]agent.Host, r *redact.Redactor, now time.Time) (*Proposal, []TraceStep) {
	if t == nil || s.books == nil {
		return nil, nil
	}
	var trace []TraceStep
	if use != nil && len(t.offered) > 0 && use.Runbook >= 1 && use.Runbook <= len(t.offered) {
		b := t.offered[use.Runbook-1]
		why, _ := store.CleanLine(r.Text(truncate(use.Why, 4*maxRunbookWhyBytes)).String(), 4*maxRunbookWhyBytes)
		why = truncate(why, maxRunbookWhyBytes)
		s.mu.Lock()
		s.startRunLocked(c, b, "agent", why, c.asks+1, now)
		s.mu.Unlock()
		s.audit.RunbookEvent(fmt.Sprintf("runbook picked by the agent (%d steps)", len(b.Steps)), b.ID, "", redact.Report{})
		trace = append(trace, TraceStep{Status: "runbook", Runbook: b.Name, RunbookID: b.ID})
	}
	if len(read) == 0 {
		return nil, trace
	}
	s.mu.Lock()
	run := c.run
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return nil, trace
	}
	i := currentStep(run)
	if i < 0 {
		run.Status = runFinished
		s.mu.Unlock()
		return nil, trace
	}
	st := &run.Steps[i]
	live := false
	if st.ProposalID != "" {
		for _, p := range c.proposals {
			live = live || p.ID == st.ProposalID
		}
	}
	if st.Command == "" || !(st.State == stepNext || (st.State == stepProposed && !live)) {
		s.mu.Unlock()
		return nil, trace
	}
	// The pane, picked by Keel, not the model: only a pane read on a host
	// the runbook is for (its folders and tags; any host when it lists
	// none), and once a step was proposed, only on that same host (the
	// run is pinned to it until the user moves it). None: no proposal,
	// and the card says why.
	target := runTarget(run, read, byID)
	if target == nil {
		run.Blocked = blockedReason(run)
		s.mu.Unlock()
		return nil, trace
	}
	run.Blocked = ""
	if run.PinHostID == "" {
		run.PinHostID, run.PinHost, run.PinSessionID = target.Pane.HostID, target.Pane.HostName, target.Pane.SessionID
	}
	h := byID[target.Pane.HostID]
	p := &Proposal{
		ID: newID("p_"), Command: st.Command, Bytes: typedBytes(st.Command), Why: st.Title,
		Risk: rate(st.Command, ""), ProposedAt: &now,
		SessionID: target.Pane.SessionID, HostID: target.Pane.HostID, Host: target.Pane.HostName, Production: h.Production,
		Runbook: &ProposalStep{RunbookID: run.RunbookID, Name: run.Name, Step: i + 1, Of: len(run.Steps), StepID: st.ID},
	}
	p.NeedsShift = agent.NeedsShift(p.Risk, p.Production)
	p.Placeholders = hasPlaceholder(p.Command)
	st.State, st.ProposalID, st.SessionID, st.HostID, st.Host, st.ProposedAt = stepProposed, p.ID, p.SessionID, p.HostID, p.Host, &now
	st.Exit, st.Result, st.TypedAt, st.FinishedAt, st.Edited, st.Typed = nil, "", nil, nil, false, ""
	c.proposals = append(c.proposals, p)
	if n := len(c.proposals); n > maxProposalsKept {
		c.proposals = c.proposals[n-maxProposalsKept:]
	}
	runbookID, steps := run.RunbookID, len(run.Steps)
	s.mu.Unlock()
	s.audit.Suggest(target.Pane, r.Report(), riskLabel(p.Risk))
	s.audit.RunbookEvent(fmt.Sprintf("runbook step %d of %d proposed for %s", i+1, steps, p.Host), runbookID, p.HostID, redact.Report{})
	_ = s.books.MarkRunbookUsed(runbookID, p.Host, now)
	return p, trace
}

// runTarget is the pane a step goes to: the run's pinned pane when it was
// read, else another read pane on the pinned host; with no pin, the first
// read pane on a host the runbook is for. nil when there is none.
func runTarget(run *store.AgentChatRunbook, read []agent.Excerpt, byID map[string]agent.Host) *agent.Excerpt {
	var cands []*agent.Excerpt
	for k := range read {
		ex := &read[k]
		h, ok := byID[ex.Pane.HostID]
		if !ok {
			continue
		}
		if len(run.Hosts) > 0 && !hostFits(run.Hosts, h) {
			continue
		}
		if run.PinHostID != "" && ex.Pane.HostID != run.PinHostID {
			continue
		}
		cands = append(cands, ex)
	}
	for _, ex := range cands {
		if run.PinSessionID != "" && ex.Pane.SessionID == run.PinSessionID {
			return ex
		}
	}
	if len(cands) == 0 {
		return nil
	}
	return cands[0]
}

// blockedReason is the card's sentence when no pane read fits.
func blockedReason(run *store.AgentChatRunbook) string {
	if run.PinHostID != "" {
		name := run.PinHost
		if name == "" {
			name = "its host"
		}
		return fmt.Sprintf("This runbook runs on %s. Put a pane on %s in scope to go on, or move the run to another host.", name, name)
	}
	return fmt.Sprintf("No pane in scope is on a host this runbook is for (%s). Put one in scope to go on.", strings.Join(run.Hosts, ", "))
}

// stepCommands is how many commands the shell should report for a step's
// command: its lines, without blank lines, comments and the lines a
// backslash continues. At least one.
func stepCommands(cmd string) int {
	n, cont := 0, false
	for _, ln := range strings.Split(cmd, "\n") {
		t := strings.TrimSpace(ln)
		was := cont
		cont = strings.HasSuffix(t, "\\")
		if was || t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		n++
	}
	return max(n, 1)
}

// stepTypedLocked records that a step's proposal was typed: the step waits
// for its result from the stream offset taken right before the write.
// Call with s.mu held.
func (c *conversation) stepTypedLocked(proposalID string, mark uint64, hasMark bool, now time.Time) bool {
	if c.run == nil {
		return false
	}
	for i := range c.run.Steps {
		st := &c.run.Steps[i]
		if st.ProposalID != proposalID || (st.State != stepProposed && st.State != stepTyped) {
			continue
		}
		st.State, st.TypedAt, st.Exit, st.Result, st.FinishedAt = stepTyped, &now, nil, "", nil
		if c.runMarks == nil {
			c.runMarks = map[string]uint64{}
		}
		if hasMark {
			c.runMarks[st.ID] = mark
		} else {
			delete(c.runMarks, st.ID)
		}
		return true
	}
	return false
}

// RunbookState returns the conversation's runbook in use, after looking
// for the results of typed steps in their panes' shell marks.
func (s *Service) RunbookState(convID string) (RunState, error) {
	if !convIDRe.MatchString(convID) {
		return RunState{}, ErrNotFound
	}
	s.refreshRun(convID)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convs[convID]
	if c == nil {
		return RunState{}, nil
	}
	return RunState{Runbook: c.run.Copy(), NoRunbook: c.noRunbook}, nil
}

type pendingStep struct {
	id, sessionID, hostID string
	n, want               int
	mark                  uint64
}

// refreshRun records the exit status a pane's shell reported for each
// typed step (OSC 133), labelled "reported": marks are the remote's own
// output and prove nothing. A step whose shell went away is left for the
// user to mark.
func (s *Service) refreshRun(convID string) {
	src, ok := s.panes.(StepSource)
	if !ok {
		return
	}
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil || c.run == nil {
		s.mu.Unlock()
		return
	}
	var pend []pendingStep
	for i, st := range c.run.Steps {
		if st.State != stepTyped || st.Result != "" {
			continue
		}
		if m, ok := c.runMarks[st.ID]; ok {
			cmd := st.Command
			if st.Edited && st.Typed != "" {
				cmd = st.Typed
			}
			pend = append(pend, pendingStep{id: st.ID, sessionID: st.SessionID, hostID: st.HostID, n: i + 1, want: stepCommands(cmd), mark: m})
		}
	}
	runID := c.run.ID
	s.mu.Unlock()
	if len(pend) == 0 {
		return
	}
	// A step's result: the commands the shell reported after the mark.
	// Any non-zero exit fails it at once; it is done only once as many
	// commands as its command has lines ended, each with exit 0 (the last
	// of them, not the first). One that ended with no status, or a shell
	// that went away, is left to the user.
	type result struct {
		p         pendingStep
		exit      int
		noStatus  bool
		abandoned bool
	}
	var got []result
	for _, p := range pend {
		cmds := src.CommandsAfter(p.sessionID, p.mark)
		ended := 0
		var res *result
		for _, cmd := range cmds {
			if !cmd.Done {
				break
			}
			ended++
			switch {
			case cmd.Abandoned || !cmd.HasExit:
				res = &result{p: p, noStatus: true, abandoned: cmd.Abandoned}
			case cmd.Exit != 0:
				res = &result{p: p, exit: cmd.Exit}
			}
			if res != nil || ended >= p.want {
				break
			}
		}
		if res == nil && ended >= p.want {
			res = &result{p: p}
		}
		if res != nil {
			got = append(got, *res)
		}
	}
	if len(got) == 0 {
		return
	}
	prod := map[string]bool{}
	for _, g := range got {
		if _, ok := prod[g.p.hostID]; !ok {
			prod[g.p.hostID], _ = s.hostProduction(g.p.hostID)
		}
	}
	now := s.now()
	var logs []func()
	s.mu.Lock()
	c = s.convs[convID]
	if c == nil || c.run == nil || c.run.ID != runID {
		s.mu.Unlock()
		return
	}
	for _, g := range got {
		for i := range c.run.Steps {
			st := &c.run.Steps[i]
			if st.ID != g.p.id || st.State != stepTyped || st.Result != "" {
				continue
			}
			n, of, rid, host := g.p.n, len(c.run.Steps), c.run.RunbookID, g.p.hostID
			switch {
			case g.noStatus:
				st.Result = "unreported"
				if g.abandoned {
					st.Result = "abandoned"
				}
				logs = append(logs, func() {
					s.audit.RunbookEvent(fmt.Sprintf("runbook step %d of %d ended with no exit status reported", n, of), rid, host, redact.Report{})
				})
			case g.exit == 0 && prod[g.p.hostID]:
				// On a production host a reported exit 0 is not enough:
				// the user confirms the step (the card says so).
				exit := 0
				st.Exit, st.Result = &exit, "reported"
				logs = append(logs, func() {
					s.audit.RunbookEvent(fmt.Sprintf("runbook step %d of %d · exit 0 reported by the shell · waiting for you to confirm (production host)", n, of), rid, host, redact.Report{})
				})
			default:
				exit := g.exit
				st.Exit, st.Result, st.FinishedAt = &exit, "reported", &now
				st.State = stepDone
				if exit != 0 {
					st.State = stepFailed
				}
				logs = append(logs, func() {
					s.audit.RunbookEvent(fmt.Sprintf("runbook step %d of %d · exit %d reported by the shell", n, of, exit), rid, host, redact.Report{})
				})
			}
			delete(c.runMarks, st.ID)
		}
	}
	if currentStep(c.run) < 0 {
		c.run.Status = runFinished
	}
	s.mu.Unlock()
	for _, l := range logs {
		l()
	}
	s.persist(c)
}

// StepAction is the user's action on a step of the runbook in use: "done"
// (mark it done), "skip", or "retry" (propose it again with the next ask).
// Only the current step, or a typed one waiting for its result, can be
// changed: nothing skips ahead.
func (s *Service) StepAction(convID, stepID, action string) (RunState, error) {
	s.refreshRun(convID)
	now := s.now()
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil || c.run == nil || c.run.Status != runActive {
		s.mu.Unlock()
		return RunState{}, ErrNoRun
	}
	cur := currentStep(c.run)
	idx := -1
	for i, st := range c.run.Steps {
		if st.ID == stepID {
			idx = i
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return RunState{}, ErrNotFound
	}
	if idx != cur {
		s.mu.Unlock()
		return RunState{}, ErrStepNotCurrent
	}
	st := &c.run.Steps[idx]
	var what string
	switch action {
	case "done":
		if st.State == stepTyped && st.Result == "reported" && st.Exit != nil && *st.Exit == 0 {
			st.State, st.FinishedAt = stepDone, &now
			what = "confirmed done by you after a reported exit 0"
			break
		}
		st.State, st.Result, st.FinishedAt = stepDone, "user", &now
		what = "marked done by you"
	case "skip":
		st.State, st.Result, st.FinishedAt = stepSkipped, "user", &now
		what = "skipped by you"
	case "retry":
		if st.State != stepFailed && st.State != stepTyped && st.State != stepProposed {
			s.mu.Unlock()
			return RunState{}, badRequest("only a proposed, typed or failed step can be retried")
		}
		st.State, st.Result, st.Exit, st.FinishedAt, st.TypedAt, st.Edited, st.Typed = stepNext, "", nil, nil, nil, false, ""
		what = "to be proposed again (retry by you)"
	default:
		s.mu.Unlock()
		return RunState{}, badRequest("action is done, skip or retry")
	}
	delete(c.runMarks, st.ID)
	c.run.Blocked = ""
	if currentStep(c.run) < 0 {
		c.run.Status = runFinished
	}
	n, of, rid, host := idx+1, len(c.run.Steps), c.run.RunbookID, st.HostID
	out := RunState{Runbook: c.run.Copy(), NoRunbook: c.noRunbook}
	s.mu.Unlock()
	s.audit.RunbookEvent(fmt.Sprintf("runbook step %d of %d %s", n, of, what), rid, host, redact.Report{})
	s.persist(c)
	return out, nil
}

// StopRunbook is "Don't use": the runbook in use stops, and the agent is
// not offered runbooks again in this conversation until the user starts
// one.
func (s *Service) StopRunbook(convID string) (RunState, error) {
	if !convIDRe.MatchString(convID) {
		return RunState{}, ErrNotFound
	}
	if err := s.hydrate(convID); err != nil {
		return RunState{}, err
	}
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil {
		// Nothing to stop: an unknown conversation is not made (or let
		// evict another) by a stop.
		s.mu.Unlock()
		return RunState{}, ErrNotFound
	}
	rid := ""
	if c.run != nil && c.run.Status == runActive {
		c.run.Status = runStopped
		rid = c.run.RunbookID
	}
	c.noRunbook = true
	out := RunState{Runbook: c.run.Copy(), NoRunbook: true}
	persist := len(c.record) > 0
	s.mu.Unlock()
	s.audit.RunbookEvent("runbook not used in this conversation (set by you)", rid, "", redact.Report{})
	if persist {
		s.persist(c)
	}
	return out, nil
}

// MoveRun unpins the runbook in use from its host: the next step goes to
// the first pane read on a host the runbook is for, and the run is pinned
// there. Only the user can move a run.
func (s *Service) MoveRun(convID string) (RunState, error) {
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil || c.run == nil || c.run.Status != runActive {
		s.mu.Unlock()
		return RunState{}, ErrNoRun
	}
	from := c.run.PinHost
	c.run.PinHostID, c.run.PinHost, c.run.PinSessionID, c.run.Blocked = "", "", "", ""
	rid := c.run.RunbookID
	out := RunState{Runbook: c.run.Copy(), NoRunbook: c.noRunbook}
	s.mu.Unlock()
	what := "runbook moved by you: the next step may go to another host"
	if from != "" {
		what = "runbook moved by you off " + from + ": the next step may go to another host"
	}
	s.audit.RunbookEvent(what, rid, "", redact.Report{})
	s.persist(c)
	return out, nil
}

// stepEditedLocked marks the step whose proposal the user edited, keeping
// the saved command and the edit (redacted) apart. Call with s.mu held.
func (c *conversation) stepEditedLocked(proposalID, typed string) bool {
	if c.run == nil {
		return false
	}
	for i := range c.run.Steps {
		st := &c.run.Steps[i]
		if st.ProposalID == proposalID && st.State == stepProposed {
			st.Edited, st.Typed = true, typed
			return true
		}
	}
	return false
}

// --- runbooks the agent proposes ---

// runbookProposal turns the model's runbook block into a proposal, redacted
// with r and cleaned. It is marked FromPane when a step's text matches a
// pane excerpt that was sent; PanesRead names every host read for the
// answer, and the panel warns on any proposal after a pane read.
func (s *Service) runbookProposal(raw *rawRunbook, excerpts []agent.Excerpt, r *redact.Redactor, now time.Time) *RunbookProposal {
	if raw == nil {
		return nil
	}
	in := RunbookInput{Name: raw.Name, When: raw.When}
	for _, st := range raw.Steps {
		if len(in.Steps) >= store.MaxRunbookSteps {
			break
		}
		cmd, ok := cleanCommand(st.Command)
		if !ok {
			cmd = ""
		}
		in.Steps = append(in.Steps, store.RunbookStep{
			Title:       truncate(st.Title, store.MaxRunbookTitleBytes-4),
			Description: truncate(st.Description, store.MaxRunbookTextBytes-4),
			Command:     cmd,
			Expect:      truncate(st.Expect, store.MaxRunbookExpectBytes-4),
		})
	}
	in.Name = truncate(in.Name, store.MaxRunbookNameBytes-4)
	in.When = truncate(in.When, store.MaxRunbookWhenBytes-4)
	clean := func(t string, max int) string {
		c, _ := store.CleanLine(r.Text(t).String(), max+64)
		return truncate(c, max)
	}
	rp := &RunbookProposal{ID: newID("rp_"), Name: clean(in.Name, store.MaxRunbookNameBytes), When: clean(in.When, store.MaxRunbookWhenBytes), Status: "open", ProposedAt: &now}
	for _, st := range in.Steps {
		title := clean(st.Title, store.MaxRunbookTitleBytes)
		if title == "" {
			continue
		}
		desc := clean(st.Description, store.MaxRunbookTextBytes)
		cmd, ok := store.CleanRunbookCommand(r.Text(st.Command).String())
		if !ok {
			cmd = ""
		}
		// A command the Redactor changed holds a placeholder: it is left
		// out (a save would refuse it), with a note.
		if hasPlaceholder(cmd) {
			cmd = ""
			desc = truncate(strings.TrimSpace(desc+" "+secretLeftOut), store.MaxRunbookTextBytes-4)
		}
		rp.Steps = append(rp.Steps, store.RunbookStep{Title: title, Description: desc, Command: cmd, Expect: clean(st.Expect, store.MaxRunbookExpectBytes)})
	}
	if rp.Name == "" || len(rp.Steps) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, ex := range excerpts {
		if !seen[ex.Pane.HostName] {
			seen[ex.Pane.HostName] = true
			rp.PanesRead = append(rp.PanesRead, ex.Pane.HostName)
		}
	}
	if len(excerpts) > 0 {
		sh := buildShingles(excerpts)
		for _, st := range rp.Steps {
			for _, t := range []string{st.Title, st.Description, st.Command} {
				if host, ok := seenInPanes(t, sh); ok && t != "" {
					rp.FromPane, rp.SeenIn = true, host
				}
			}
		}
	}
	return rp
}

func runbookProposalRecord(rp *RunbookProposal) store.AgentChatRunbookProposal {
	at := time.Time{}
	if rp.ProposedAt != nil {
		at = *rp.ProposedAt
	}
	return store.AgentChatRunbookProposal{ID: rp.ID, Name: rp.Name, When: rp.When, Steps: append([]store.RunbookStep(nil), rp.Steps...), FromPane: rp.FromPane, SeenIn: rp.SeenIn, PanesRead: rp.PanesRead, Status: rp.Status, RunbookID: rp.RunbookID, ProposedAt: at}
}

func runbookProposalFromRecord(r store.AgentChatRunbookProposal) RunbookProposal {
	at := r.ProposedAt
	st := r.Status
	if !runbookStatusValid[st] {
		st = "open"
	}
	return RunbookProposal{ID: r.ID, Name: r.Name, When: r.When, Steps: r.Steps, FromPane: r.FromPane, SeenIn: r.SeenIn, PanesRead: r.PanesRead, Status: st, RunbookID: r.RunbookID, ProposedAt: &at, Expired: true}
}

func (c *conversation) bookPropLocked(id string) *RunbookProposal {
	for _, b := range c.bookProps {
		if b.ID == id {
			return b
		}
	}
	return nil
}

func (c *conversation) recordBookPropLocked(id string) *store.AgentChatRunbookProposal {
	for i := len(c.record) - 1; i >= 0; i-- {
		for j := range c.record[i].Runbooks {
			if c.record[i].Runbooks[j].ID == id {
				return &c.record[i].Runbooks[j]
			}
		}
	}
	return nil
}

func (s *Service) claimBookProp(convID, id string, ok func(*RunbookProposal) bool) (RunbookProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convs[convID]
	var rp *RunbookProposal
	if c != nil {
		rp = c.bookPropLocked(id)
	}
	if rp == nil {
		if c != nil && c.recordBookPropLocked(id) != nil {
			return RunbookProposal{}, ErrRunbookExpired
		}
		return RunbookProposal{}, ErrNotFound
	}
	if rp.busy || !ok(rp) {
		return RunbookProposal{}, ErrRunbookClosed
	}
	rp.busy = true
	out := *rp
	out.Steps = append([]store.RunbookStep(nil), rp.Steps...)
	return out, nil
}

func (s *Service) setBookPropStatus(convID, id, status, runbookID string) *RunbookProposal {
	s.mu.Lock()
	c := s.convs[convID]
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	rp := c.bookPropLocked(id)
	if rp == nil {
		s.mu.Unlock()
		return nil
	}
	rp.Status, rp.RunbookID, rp.busy = status, runbookID, false
	if rec := c.recordBookPropLocked(id); rec != nil {
		rec.Status, rec.RunbookID = status, runbookID
	}
	out := *rp
	s.mu.Unlock()
	s.persist(c)
	return &out
}

func (s *Service) releaseBookProp(convID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.convs[convID]; c != nil {
		if rp := c.bookPropLocked(id); rp != nil {
			rp.busy = false
		}
	}
}

// SaveRunbookProposal saves a runbook the agent proposed, after the user
// pressed Save (or Save anyway). It is kept with "Agent may pick" off: the
// user reviews it on the Runbooks page and turns it on. Only proposals held
// in memory from this run can be saved.
func (s *Service) SaveRunbookProposal(convID, id string) (SavedRunbook, error) {
	if s.books == nil {
		return SavedRunbook{}, ErrNoRunbooks
	}
	snap, err := s.claimBookProp(convID, id, func(rp *RunbookProposal) bool { return rp.Status == "open" })
	if err != nil {
		return SavedRunbook{}, err
	}
	in := RunbookInput{Name: snap.Name, When: snap.When, Steps: snap.Steps, Source: store.RunbookSourceUser}
	b, rep, err := s.cleanRunbookText(in)
	if err != nil {
		s.releaseBookProp(convID, id)
		return SavedRunbook{}, err
	}
	b.Source, b.FromPane, b.AgentMayPick = store.RunbookSourceAgent, snap.FromPane || len(snap.PanesRead) > 0, false
	saved, err := s.books.AddRunbook(b)
	if err != nil {
		s.releaseBookProp(convID, id)
		return SavedRunbook{}, runbookErr(err)
	}
	what := "runbook proposed by the agent saved by you"
	if saved.FromPane {
		what = "runbook proposed by the agent after reading panes saved by you"
	}
	s.audit.RunbookEvent(fmt.Sprintf("%s (%d steps)", what, len(saved.Steps)), saved.ID, "", rep)
	out := s.setBookPropStatus(convID, id, "saved", saved.ID)
	return SavedRunbook{Runbook: runbookView(saved), Redacted: rep.Total(), Proposal: out}, nil
}

// DismissRunbookProposal closes a proposal without saving anything.
func (s *Service) DismissRunbookProposal(convID, id string) (RunbookProposal, error) {
	if _, err := s.claimBookProp(convID, id, func(rp *RunbookProposal) bool { return rp.Status == "open" }); err != nil {
		return RunbookProposal{}, err
	}
	out := s.setBookPropStatus(convID, id, "dismissed", "")
	if out == nil {
		return RunbookProposal{}, ErrNotFound
	}
	return *out, nil
}

// UndoRunbookProposal opens a saved or dismissed proposal again; a saved
// one's runbook is deleted (logged).
func (s *Service) UndoRunbookProposal(convID, id string) (RunbookProposal, error) {
	snap, err := s.claimBookProp(convID, id, func(rp *RunbookProposal) bool { return rp.Status == "saved" || rp.Status == "dismissed" })
	if err != nil {
		return RunbookProposal{}, err
	}
	if snap.Status == "saved" && snap.RunbookID != "" && s.books != nil {
		b, err := s.books.DeleteRunbook(snap.RunbookID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.releaseBookProp(convID, id)
			return RunbookProposal{}, err
		}
		if err == nil {
			s.audit.RunbookEvent("runbook proposed by the agent deleted by you (undo)", b.ID, "", redact.Report{})
		}
	}
	out := s.setBookPropStatus(convID, id, "open", "")
	if out == nil {
		return RunbookProposal{}, ErrNotFound
	}
	return *out, nil
}
