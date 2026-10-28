package service

// An outside agent (Claude Code, through keel mcp; docs/agent.md) reaches
// the agent service here, under the same rules as the agent panel
// (docs/ui-redesign.md §6):
//
//   - it lists and reads only panes on hosts with "Agent can read" on,
//     through the Auditor (agent.read, Source "claude-code"), redacted by
//     the same Redactor an Ask builds;
//   - it may propose a command for a pane it may read. The proposal is
//     kept in one conversation, "Claude Code", in the agent panel, rated by
//     Keel's own code rule, and typed only by the user, through the
//     approve-before-type path (AuthorizeType, the ⇧ rule, agent.type);
//   - it never writes to a PTY, never sees vault contents, never reads a
//     key, and never calls a model provider.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/store"
)

const (
	// ExternalConversationID is the agent panel conversation that holds
	// what an outside agent proposed.
	ExternalConversationID = "mcp-claude-code"
	// ExternalSource names the outside agent in keel.log (Source).
	ExternalSource = "claude-code"
	// ExternalTitle is that conversation's title.
	ExternalTitle = "Claude Code"
	// maxExternalPanes caps one pane list.
	maxExternalPanes = 64
)

// ErrNoPaneList is returned when keel serve gives the service no way to
// list its sessions.
var ErrNoPaneList = errors.New("agent: this keel serve cannot list its panes")

// PaneLister lists the ids of the running sessions. keel serve's pane
// source satisfies it; ExternalPanes needs it.
type PaneLister interface {
	PaneIDs() []string
}

// ExternalPane is one pane an outside agent may read.
type ExternalPane struct {
	PaneID     string `json:"paneId"`
	Host       string `json:"host"`
	Production bool   `json:"production"`
}

// ExternalPaneList is the panes an outside agent may read, and how many
// others are open on hosts it may not read (never their names).
type ExternalPaneList struct {
	Panes      []ExternalPane `json:"panes"`
	NotAllowed int            `json:"notAllowed"`
}

// ExternalPanes lists the open panes an outside agent may read. A host's
// name goes through the request Redactor, so it is hidden when Settings ·
// Agent redacts hostnames.
func (s *Service) ExternalPanes() (ExternalPaneList, error) {
	lister, ok := s.panes.(PaneLister)
	if !ok {
		return ExternalPaneList{}, ErrNoPaneList
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return ExternalPaneList{}, err
	}
	storeHosts, err := s.st.ListHosts()
	if err != nil {
		return ExternalPaneList{}, err
	}
	hosts := agent.HostsFrom(storeHosts)
	byID := make(map[string]agent.Host, len(hosts))
	for _, h := range hosts {
		byID[h.ID] = h
	}
	scope := agent.NewScope(hosts)
	r, err := s.requestRedactor(set.Redact, hosts)
	if err != nil {
		return ExternalPaneList{}, err
	}
	out := ExternalPaneList{Panes: []ExternalPane{}}
	for _, id := range lister.PaneIDs() {
		p, ok := s.panes.Pane(id)
		if !ok {
			continue
		}
		if !scope.Allows(p) {
			out.NotAllowed++
			continue
		}
		if len(out.Panes) >= maxExternalPanes {
			break
		}
		h := byID[p.HostID]
		out.Panes = append(out.Panes, ExternalPane{PaneID: p.SessionID, Host: r.Text(hostName(h)).String(), Production: h.Production})
	}
	return out, nil
}

func hostName(h agent.Host) string {
	if h.Name == "" {
		return "unknown host"
	}
	return h.Name
}

// ExternalExcerpt is one pane as an outside agent reads it: redacted.
type ExternalExcerpt struct {
	PaneID      string         `json:"paneId"`
	Host        string         `json:"host"`
	Production  bool           `json:"production"`
	Lines       int            `json:"lines"`
	LastCommand string         `json:"lastCommand,omitempty"` // "exit 1", "still running", … when read from OSC 133 marks
	Redactions  map[string]int `json:"redactions,omitempty"`
	Text        string         `json:"text"`
}

// ExternalRead reads one pane for an outside agent: at most lines lines
// (0 or more than Settings · Agent's scrollback lines means that setting),
// through the Auditor, redacted like an Ask. A pane on a host the agent may
// not read is refused before anything is read (agent.ErrNotReadable).
func (s *Service) ExternalRead(paneID string, lines int) (ExternalExcerpt, error) {
	set, err := s.st.AgentSettings()
	if err != nil {
		return ExternalExcerpt{}, err
	}
	storeHosts, err := s.st.ListHosts()
	if err != nil {
		return ExternalExcerpt{}, err
	}
	hosts := agent.HostsFrom(storeHosts)
	p, h, scope, err := s.externalPane(paneID, hosts)
	if err != nil {
		return ExternalExcerpt{}, err
	}
	r, err := s.requestRedactor(set.Redact, hosts)
	if err != nil {
		return ExternalExcerpt{}, err
	}
	if lines <= 0 || lines > set.ScrollbackLines {
		lines = set.ScrollbackLines
	}
	ex, err := s.audit.ReadPaneFrom(s.panes, scope, p, lines, r, ExternalSource)
	if err != nil {
		return ExternalExcerpt{}, err
	}
	out := ExternalExcerpt{PaneID: p.SessionID, Host: r.Text(p.HostName).String(), Production: h.Production, Lines: ex.Lines, Redactions: ex.Report.Strings(), Text: ex.Text.String()}
	if ex.LastCommand {
		out.LastCommand = ex.Command.Label()
	}
	return out, nil
}

// externalPane resolves a pane an outside agent names, refusing one that
// is not open or is on a host the agent may not read.
func (s *Service) externalPane(paneID string, hosts []agent.Host) (agent.Pane, agent.Host, agent.Scope, error) {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return agent.Pane{}, agent.Host{}, agent.Scope{}, badRequest("name a pane (paneId from list_panes)")
	}
	p, ok := s.panes.Pane(paneID)
	if !ok {
		return agent.Pane{}, agent.Host{}, agent.Scope{}, ErrNotFound
	}
	scope := agent.NewScope(hosts)
	if !scope.Allows(p) {
		return agent.Pane{}, agent.Host{}, agent.Scope{}, agent.ErrNotReadable
	}
	var h agent.Host
	for _, x := range hosts {
		if x.ID == p.HostID {
			h = x
			break
		}
	}
	p.HostName = hostName(h)
	return p, h, scope, nil
}

// ExternalProposeRequest is a command an outside agent proposes.
type ExternalProposeRequest struct {
	PaneID  string `json:"paneId"`
	Command string `json:"command"`
	Why     string `json:"why"`
}

// ExternalPropose keeps a command an outside agent proposes for a pane it
// may read, in the "Claude Code" conversation of the agent panel. It types
// nothing: the user sees the exact bytes there and types them, or not,
// through approve-before-type. The rating is Keel's code rule alone.
func (s *Service) ExternalPropose(req ExternalProposeRequest) (Proposal, error) {
	cmd, ok := cleanCommand(req.Command)
	if !ok {
		return Proposal{}, badRequest(fmt.Sprintf("a command is 1 to %d bytes and %d lines, with no control characters", maxCommandBytes, maxCommandLines))
	}
	why := truncate(strings.TrimSpace(req.Why), maxWhyBytes)
	storeHosts, err := s.st.ListHosts()
	if err != nil {
		return Proposal{}, err
	}
	hosts := agent.HostsFrom(storeHosts)
	pane, h, _, err := s.externalPane(req.PaneID, hosts)
	if err != nil {
		return Proposal{}, err
	}
	r, err := s.commandRedactor()
	if err != nil {
		return Proposal{}, err
	}
	// A saved "Claude Code" conversation resumes, as an Ask's would.
	if err := s.hydrate(ExternalConversationID); err != nil {
		return Proposal{}, err
	}
	conv, err := s.begin(ExternalConversationID)
	if err != nil {
		return Proposal{}, err
	}
	defer s.end(conv)

	now := s.now()
	risk := agent.Classify(cmd)
	p := &Proposal{
		ID: newID("p_"), SessionID: pane.SessionID, HostID: pane.HostID, Host: pane.HostName, Production: h.Production,
		Command: cmd, Bytes: typedBytes(cmd), Why: why, Risk: risk,
		NeedsShift: agent.NeedsShift(risk, h.Production), Placeholders: hasPlaceholder(cmd), ProposedAt: &now,
	}
	s.audit.SuggestFrom(pane, redact.Report{}, riskLabel(risk), ExternalSource)

	// What is kept goes through the Redactor, as for an Ask.
	answer := "Claude Code proposed a command for " + pane.HostName + "."
	if why != "" {
		answer += " " + why
	}
	rec := store.AgentChatTurn{At: now, Question: "(from Claude Code)", Answer: r.Text(answer).String()}
	rec.Proposals = append(rec.Proposals, recordOf(p, r.Text(cmd).String(), r.Text(why).String(), now))

	s.mu.Lock()
	conv.proposals = append(conv.proposals, p)
	if n := len(conv.proposals); n > maxProposalsKept {
		conv.proposals = conv.proposals[n-maxProposalsKept:]
	}
	if conv.title == "" {
		conv.title = ExternalTitle
	}
	if conv.created.IsZero() {
		conv.created = now
	}
	conv.updated = now
	conv.addRecordLocked(rec)
	out := *p
	s.mu.Unlock()
	s.persist(conv)
	return out, nil
}

// ExternalProposalState is what an outside agent may know of a proposal
// it made: whether the user typed it, and when.
type ExternalProposalState struct {
	ProposalID string     `json:"proposalId"`
	Typed      bool       `json:"typed"`
	TypedAt    *time.Time `json:"typedAt,omitempty"`
	Edited     bool       `json:"edited"`
}

// ExternalProposal reports whether the user typed a proposal of the
// "Claude Code" conversation. Only that conversation's proposals are
// visible here.
func (s *Service) ExternalProposal(id string) (ExternalProposalState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.proposalLocked(ExternalConversationID, strings.TrimSpace(id))
	if err != nil {
		return ExternalProposalState{}, err
	}
	out := ExternalProposalState{ProposalID: p.ID, Typed: p.TypedAt != nil, Edited: p.Edited}
	if p.TypedAt != nil {
		t := *p.TypedAt
		out.TypedAt = &t
	}
	return out, nil
}
