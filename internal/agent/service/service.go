// Package service runs the agent panel's conversations (docs/ui-redesign.md
// §5.4). It is the one place in Keel that sends anything off the machine,
// and only from Ask, which keel serve calls on the user's own request
// (POST /api/agent/ask).
//
// An Ask reads the panes the user put in scope, through agent.Auditor
// (every read is logged), redacts them and the question with one Redactor,
// and sends only that redacted text (redact.Safe) to the provider. The
// answer may carry command proposals; the service rates them and keeps
// them, and never runs one. Typing a proposal is the user's action: the
// panel sends it through the approve-before-type path, and keel serve asks
// AuthorizeType (the ⇧ rule, the driver check, the agent.type log line)
// before it writes.
//
// Conversations live in memory only, bounded, and are cleared by "New
// conversation" (Reset) or when keel serve stops. Nothing is written to
// disk, so the historyDays setting has nothing to apply to yet.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
	"keel/internal/store"
)

// Store is what the service reads from keel serve's store. *store.Store
// satisfies it. The provider key is read only in providerFor, right before
// a provider is built for an Ask.
type Store interface {
	agent.SecretSource
	AgentSettings() (store.AgentSettings, error)
	ListHosts() ([]store.Host, error)
	AgentKeyConfigured(provider string) (bool, error)
	AgentKey(provider string) (string, error)
	// ListSnippets is read only for in-pane suggestions, and only the
	// snippets marked agent-usable are kept (agentSnippets).
	ListSnippets() ([]store.Snippet, error)
}

// Panes resolves runtime sessions to panes and reads their scrollback.
// keel serve adapts *runtime.Runtime to it; the service sees no other part
// of the runtime.
type Panes interface {
	agent.ScrollbackSource
	// Pane returns the session's id and host id. ok is false when no such
	// session runs.
	Pane(sessionID string) (agent.Pane, bool)
}

var (
	ErrNoKey       = errors.New("agent: no provider key; connect Gemini first")
	ErrBusy        = errors.New("agent: this conversation is still answering")
	ErrBadRequest  = errors.New("agent: bad request")
	ErrNotFound    = errors.New("agent: no such conversation or proposal")
	ErrNoTarget    = errors.New("agent: this proposal has no pane to type into")
	ErrWrongPane   = errors.New("agent: the proposal is for another pane")
	ErrChanged     = errors.New("agent: the command differs from the proposal; edit it in the panel first")
	ErrNeedsShift  = errors.New("agent: this command changes or deletes data on a production host, or is destructive; hold Shift and press Type")
	ErrNotProvider = errors.New("agent: the selected provider cannot answer questions")
)

// ProviderError is a failed provider call, with the key removed from its
// message.
type ProviderError struct{ Msg string }

func (e *ProviderError) Error() string { return e.Msg }

// Options tune a Service. Zero values take the defaults.
type Options struct {
	Factory Factory
	// Judge builds the detector (Jev) for in-pane suggestions. nil uses
	// RealJudge.
	Judge        JudgeFactory
	TestProvider bool
	Timeout      time.Duration
	Now          func() time.Time
	// DetectLogWindow is how long detection reads of one pane are
	// summarised into one keel.log entry (tests shorten it).
	DetectLogWindow time.Duration
}

type Service struct {
	st      Store
	panes   Panes
	audit   *agent.Auditor
	factory Factory
	judge   JudgeFactory
	test    bool
	timeout time.Duration
	now     func() time.Time

	mu    sync.Mutex
	convs map[string]*conversation

	sug          suggestState
	detectWindow time.Duration
}

const (
	defaultTimeout   = 90 * time.Second
	maxConversations = 16
	idleConversation = 12 * time.Hour
	maxProposalsKept = 32
)

// New returns a Service that logs to log (keel.log). A nil log is an
// error: an unlogged read must not be possible.
func New(st Store, panes Panes, log agent.Logger, opts Options) (*Service, error) {
	audit, err := agent.NewAuditor(log)
	if err != nil {
		return nil, err
	}
	if st == nil || panes == nil {
		return nil, errors.New("agent: service needs a store and panes")
	}
	s := &Service{st: st, panes: panes, audit: audit, factory: opts.Factory, judge: opts.Judge, test: opts.TestProvider, timeout: opts.Timeout, now: opts.Now, detectWindow: opts.DetectLogWindow}
	if s.factory == nil {
		s.factory = Real
	}
	if s.judge == nil {
		s.judge = RealJudge
	}
	if s.detectWindow <= 0 {
		s.detectWindow = detectLogWindow
	}
	s.sug.suggestions = map[string]*Suggestion{}
	s.sug.detectLogs = map[string]*detectLog{}
	if s.timeout <= 0 {
		s.timeout = defaultTimeout
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.convs = map[string]*conversation{}
	return s, nil
}

// TestProvider reports whether answers come from the in-process fake.
func (s *Service) TestProvider() bool { return s.test }

type turn struct {
	user  redact.Safe
	model redact.Safe
}

type conversation struct {
	id        string
	busy      bool
	turns     []turn
	asks      int
	proposals []*Proposal
	used      time.Time
}

// AskRequest is the body of POST /api/agent/ask.
type AskRequest struct {
	ConversationID string   `json:"conversationId"`
	Question       string   `json:"question"`
	Panes          []string `json:"panes"`
}

// TraceStep says what the agent did with one pane in scope.
type TraceStep struct {
	SessionID  string         `json:"sessionId"`
	HostID     string         `json:"hostId,omitempty"`
	Host       string         `json:"host"`
	Pane       int            `json:"pane,omitempty"` // number in the request ("pane N"), when read
	Status     string         `json:"status"`         // "read" | "skipped"
	Reason     string         `json:"reason,omitempty"`
	Lines      int            `json:"lines"`
	Redactions map[string]int `json:"redactions,omitempty"`
}

// Proposal is a command the agent suggests. It is kept in the conversation
// so that typing it can be checked against what was shown.
type Proposal struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"sessionId,omitempty"`
	HostID       string     `json:"hostId,omitempty"`
	Host         string     `json:"host,omitempty"`
	Production   bool       `json:"production"`
	Command      string     `json:"command"`
	Bytes        string     `json:"bytes"` // exactly what Type sends: Ctrl-U, the command, a newline
	Why          string     `json:"why,omitempty"`
	Risk         agent.Risk `json:"risk"`
	NeedsShift   bool       `json:"needsShift"`
	Placeholders bool       `json:"placeholders"` // holds [secret:N]-style text to replace before typing
	Edited       bool       `json:"edited"`
}

type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// AskResponse is what the panel shows for one ask.
type AskResponse struct {
	ConversationID     string         `json:"conversationId"`
	Turn               int            `json:"turn"`
	Provider           string         `json:"provider"`
	Model              string         `json:"model"`
	TestProvider       bool           `json:"testProvider"`
	Trace              []TraceStep    `json:"trace"`
	QuestionRedactions map[string]int `json:"questionRedactions,omitempty"`
	Answer             string         `json:"answer"`
	Proposals          []Proposal     `json:"proposals"`
	Usage              Usage          `json:"usage"`
}

var convIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func badRequest(msg string) error { return fmt.Errorf("%w: %s", ErrBadRequest, msg) }

// Ask answers one question. It is the only method that calls a provider.
func (s *Service) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	if !convIDRe.MatchString(req.ConversationID) {
		return AskResponse{}, badRequest("conversationId must be 8 to 64 letters, digits, _ or -")
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return AskResponse{}, badRequest("ask a question")
	}
	if len(question) > maxQuestionBytes {
		return AskResponse{}, badRequest(fmt.Sprintf("the question is longer than %d bytes", maxQuestionBytes))
	}
	ids := uniq(req.Panes)
	if len(ids) > maxPanesPerAsk {
		return AskResponse{}, badRequest(fmt.Sprintf("at most %d panes per question", maxPanesPerAsk))
	}

	set, err := s.st.AgentSettings()
	if err != nil {
		return AskResponse{}, err
	}
	desc, ok := provider.Lookup(set.Provider)
	if !ok || desc.Kind != provider.KindModel {
		return AskResponse{}, ErrNotProvider
	}
	configured, err := s.st.AgentKeyConfigured(desc.ID)
	if err != nil {
		return AskResponse{}, err
	}
	if !configured {
		return AskResponse{}, ErrNoKey
	}

	conv, err := s.begin(req.ConversationID)
	if err != nil {
		return AskResponse{}, err
	}
	defer s.end(conv)

	storeHosts, err := s.st.ListHosts()
	if err != nil {
		return AskResponse{}, err
	}
	hosts := agent.HostsFrom(storeHosts)
	byID := make(map[string]agent.Host, len(hosts))
	for _, h := range hosts {
		byID[h.ID] = h
	}
	scope := agent.NewScope(hosts)
	r, err := agent.NewRedactor(s.st, set.Redact, hosts)
	if err != nil {
		return AskResponse{}, err
	}

	// Read the panes in scope: only these, only through the Auditor, only
	// the last N lines from the settings.
	var (
		trace    []TraceStep
		excerpts []agent.Excerpt
	)
	for _, id := range ids {
		p, ok := s.panes.Pane(id)
		if !ok {
			trace = append(trace, TraceStep{SessionID: id, Host: "closed pane", Status: "skipped", Reason: "not-open"})
			continue
		}
		h := byID[p.HostID]
		p.HostName = h.Name
		if p.HostName == "" {
			p.HostName = "unknown host"
		}
		step := TraceStep{SessionID: id, HostID: p.HostID, Host: p.HostName}
		if !scope.Allows(p) {
			step.Status, step.Reason = "skipped", "not-readable"
			trace = append(trace, step)
			continue
		}
		ex, err := s.audit.ReadPane(s.panes, scope, p, set.ScrollbackLines, r)
		if err != nil {
			step.Status, step.Reason = "skipped", "read-failed"
			trace = append(trace, step)
			continue
		}
		excerpts = append(excerpts, ex)
		step.Status, step.Pane, step.Lines, step.Redactions = "read", len(excerpts), ex.Lines, ex.Report.Strings()
		trace = append(trace, step)
	}

	// One message: the excerpts, then the question. Everything in it goes
	// through the same Redactor (excerpts are already redacted, and a
	// second pass leaves them as they are).
	var msg strings.Builder
	fmt.Fprintf(&msg, "Panes in scope: %d\n\n", len(excerpts))
	for i, ex := range excerpts {
		fmt.Fprintf(&msg, "--- pane %d · host %s · last %d lines ---\n%s\n--- end pane %d ---\n\n", i+1, ex.Pane.HostName, ex.Lines, ex.Text.String(), i+1)
	}
	msg.WriteString("Question:\n")
	msg.WriteString(question)
	before := r.Report()
	userSafe := r.Text(msg.String())
	qRep := diffReport(before, r.Report())

	messages := make([]provider.Message, 0, 2*len(conv.turns)+1)
	for _, t := range conv.turns {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Text: t.user}, provider.Message{Role: provider.RoleModel, Text: t.model})
	}
	messages = append(messages, provider.Message{Role: provider.RoleUser, Text: userSafe})

	prov, err := s.providerFor(desc.ID)
	if err != nil {
		return AskResponse{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := prov.Generate(cctx, provider.Request{Model: set.Model, System: systemSafe, Messages: messages, MaxOutputTokens: maxOutputTokens})
	if err != nil {
		return AskResponse{}, s.providerErr(ctx, cctx, err)
	}

	answer, raws := parseResponse(resp.Text)
	out := AskResponse{
		ConversationID:     conv.id,
		Provider:           desc.ID,
		Model:              set.Model,
		TestProvider:       s.test,
		Trace:              trace,
		QuestionRedactions: qRep.Strings(),
		Answer:             answer,
		Proposals:          []Proposal{},
		Usage:              Usage{InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens},
	}
	for _, raw := range raws {
		p := &Proposal{ID: newID("p_"), Command: raw.Command, Bytes: typedBytes(raw.Command), Why: raw.Why, Risk: rate(raw.Command, raw.Risk)}
		var target *agent.Excerpt
		switch {
		case raw.Pane >= 1 && raw.Pane <= len(excerpts):
			target = &excerpts[raw.Pane-1]
		case len(excerpts) == 1:
			target = &excerpts[0]
		}
		if target != nil {
			h := byID[target.Pane.HostID]
			p.SessionID, p.HostID, p.Host, p.Production = target.Pane.SessionID, target.Pane.HostID, target.Pane.HostName, h.Production
			s.audit.Suggest(target.Pane, r.Report(), riskLabel(p.Risk))
		}
		p.NeedsShift = agent.NeedsShift(p.Risk, p.Production)
		p.Placeholders = hasPlaceholder(p.Command)
		out.Proposals = append(out.Proposals, *p)
		conv.proposals = append(conv.proposals, p)
	}
	if n := len(conv.proposals); n > maxProposalsKept {
		conv.proposals = conv.proposals[n-maxProposalsKept:]
	}

	// History keeps the question and the reply, not the excerpts: the next
	// ask reads the panes again.
	var hq strings.Builder
	hq.WriteString("Question:\n")
	hq.WriteString(question)
	conv.turns = append(conv.turns, turn{user: r.Text(hq.String()), model: r.Text(truncate(resp.Text, maxAnswerBytes))})
	conv.trimHistory()
	conv.asks++
	out.Turn = conv.asks
	return out, nil
}

// providerFor reads the provider key from the vault and builds the client
// for one Ask. It is the only place in the agent tree that reads a key
// (internal/agent/invariants_test.go).
func (s *Service) providerFor(id string) (provider.Provider, error) {
	key, err := s.st.AgentKey(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNoKey
		}
		return nil, err
	}
	p, err := s.factory(id, key)
	if err != nil {
		if errors.Is(err, provider.ErrNoKey) {
			return nil, ErrNoKey
		}
		return nil, &ProviderError{Msg: scrub(err.Error(), key)}
	}
	return scrubbing{p: p, key: key}, nil
}

// judgeFor reads the detector key (Jev) from the vault and builds its
// client for one Detect or one risk check. With no key it returns
// ErrNoKey, and the caller falls back to #-lines and the code rating. It
// is the only other place in the agent tree that reads a key
// (internal/agent/invariants_test.go).
func (s *Service) judgeFor() (provider.Judge, error) {
	configured, err := s.st.AgentKeyConfigured(JudgeProvider)
	if err != nil || !configured {
		return nil, ErrNoKey
	}
	key, err := s.st.AgentKey(JudgeProvider)
	if err != nil {
		return nil, ErrNoKey
	}
	j, err := s.judge(key)
	if err != nil {
		return nil, ErrNoKey
	}
	return scrubbingJudge{j: j, key: key}, nil
}

// scrubbingJudge removes the key from any error a judge returns.
type scrubbingJudge struct {
	j   provider.Judge
	key string
}

func (sc scrubbingJudge) ID() string { return sc.j.ID() }

func (sc scrubbingJudge) Judge(ctx context.Context, req provider.JudgeRequest) (provider.JudgeResponse, error) {
	resp, err := sc.j.Judge(ctx, req)
	if err != nil {
		return resp, &ProviderError{Msg: scrub(err.Error(), sc.key)}
	}
	return resp, nil
}

// scrubbing removes the key from any error a provider returns, on top of
// the client's own scrubbing.
type scrubbing struct {
	p   provider.Provider
	key string
}

func (sc scrubbing) ID() string { return sc.p.ID() }

func (sc scrubbing) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	resp, err := sc.p.Generate(ctx, req)
	if err != nil {
		return resp, &ProviderError{Msg: scrub(err.Error(), sc.key)}
	}
	return resp, nil
}

func scrub(msg, key string) string {
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "[key]")
	}
	return msg
}

func (s *Service) providerErr(parent, cctx context.Context, err error) error {
	switch {
	case parent.Err() != nil:
		return context.Canceled
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		return &ProviderError{Msg: fmt.Sprintf("the provider did not answer within %s", s.timeout)}
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe
	}
	return &ProviderError{Msg: err.Error()}
}

func riskLabel(r agent.Risk) string {
	switch r {
	case agent.RiskReadOnly:
		return "Read-only"
	case agent.RiskDestructive:
		return "Destructive"
	}
	return "Changes state"
}

// EditProposal replaces a proposal's command with the user's edit and rates
// it again. The edited bytes are what Type may then send.
func (s *Service) EditProposal(convID, proposalID, command string) (Proposal, error) {
	cmd, ok := cleanCommand(command)
	if !ok {
		return Proposal{}, badRequest(fmt.Sprintf("a command is 1 to %d bytes and %d lines, with no control characters", maxCommandBytes, maxCommandLines))
	}
	production := false
	s.mu.Lock()
	p, err := s.proposalLocked(convID, proposalID)
	hostID := ""
	if err == nil {
		hostID = p.HostID
	}
	s.mu.Unlock()
	if err != nil {
		return Proposal{}, err
	}
	if hostID != "" {
		if production, err = s.hostProduction(hostID); err != nil {
			return Proposal{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err = s.proposalLocked(convID, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	p.Command, p.Bytes, p.Edited = cmd, typedBytes(cmd), true
	p.Risk = agent.Classify(cmd)
	p.Production = production
	p.NeedsShift = agent.NeedsShift(p.Risk, production)
	p.Placeholders = hasPlaceholder(cmd)
	return *p, nil
}

// TypeRequest is the user's Type into pane, as keel serve sees it on the
// session socket.
type TypeRequest struct {
	ConversationID string
	ProposalID     string
	SessionID      string
	Role           agent.Role // the typing client's role in that session
	Bytes          string     // what the client asks to inject
	Shift          bool       // ⇧ was held
}

// AuthorizeType checks a Type into pane before keel serve writes it: the
// proposal exists and targets this session, the bytes are the ones shown,
// the ⇧ rule holds (with the host's current production flag), and the
// client drives the session. It then logs agent.type. It writes nothing.
func (s *Service) AuthorizeType(req TypeRequest) error {
	s.mu.Lock()
	p, err := s.proposalLocked(req.ConversationID, req.ProposalID)
	var snap Proposal
	if err == nil {
		snap = *p
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if snap.SessionID == "" {
		return ErrNoTarget
	}
	if snap.SessionID != req.SessionID {
		return ErrWrongPane
	}
	if req.Bytes != snap.Bytes {
		return ErrChanged
	}
	current, err := s.hostProduction(snap.HostID)
	if err != nil {
		return err
	}
	if agent.NeedsShift(snap.Risk, snap.Production || current) && !req.Shift {
		return ErrNeedsShift
	}
	return s.audit.Type(agent.Pane{SessionID: snap.SessionID, HostID: snap.HostID, HostName: snap.Host, Role: req.Role}, snap.Bytes)
}

// Reset forgets a conversation ("New conversation").
func (s *Service) Reset(convID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.convs, convID)
}

// ResetAll forgets every conversation (Settings, Delete all history). The
// service keeps conversations in memory only, so this is all there is to
// delete. It returns how many were dropped. A conversation with an ask in
// flight is dropped too: the ask finishes into a conversation nobody can
// reach any more.
func (s *Service) ResetAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.convs)
	s.convs = map[string]*conversation{}
	return n
}

// hostProduction reports a host's current production flag. It fails
// closed: a host that can no longer be found (deleted, or no host id)
// counts as production, so the ⇧ rule is never relaxed by a lookup miss.
func (s *Service) hostProduction(hostID string) (bool, error) {
	hosts, err := s.st.ListHosts()
	if err != nil {
		return true, err
	}
	for _, h := range agent.HostsFrom(hosts) {
		if hostID != "" && h.ID == hostID {
			return h.Production, nil
		}
	}
	return true, nil
}

func (s *Service) proposalLocked(convID, proposalID string) (*Proposal, error) {
	c := s.convs[convID]
	if c == nil {
		return nil, ErrNotFound
	}
	for _, p := range c.proposals {
		if p.ID == proposalID {
			return p, nil
		}
	}
	return nil, ErrNotFound
}

// begin marks a conversation busy, creating it if needed. Idle
// conversations are dropped, and the least recently used one when there
// are too many.
func (s *Service) begin(id string) (*conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, c := range s.convs {
		if !c.busy && now.Sub(c.used) > idleConversation {
			delete(s.convs, k)
		}
	}
	c := s.convs[id]
	if c == nil {
		if len(s.convs) >= maxConversations {
			s.evictLocked()
		}
		c = &conversation{id: id}
		s.convs[id] = c
	}
	if c.busy {
		return nil, ErrBusy
	}
	c.busy = true
	c.used = now
	return c, nil
}

func (s *Service) end(c *conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.busy = false
	c.used = s.now()
}

func (s *Service) evictLocked() {
	ids := make([]string, 0, len(s.convs))
	for k, c := range s.convs {
		if !c.busy {
			ids = append(ids, k)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return s.convs[ids[i]].used.Before(s.convs[ids[j]].used) })
	if len(ids) > 0 {
		delete(s.convs, ids[0])
	}
}

func (c *conversation) trimHistory() {
	for len(c.turns)*2 > maxHistoryMessages {
		c.turns = c.turns[1:]
	}
	for len(c.turns) > 1 {
		n := 0
		for _, t := range c.turns {
			n += len(t.user.String()) + len(t.model.String())
		}
		if n <= maxHistoryBytes {
			return
		}
		c.turns = c.turns[1:]
	}
}

func diffReport(before, after redact.Report) redact.Report {
	out := redact.Report{Counts: map[redact.Kind]int{}}
	for k, v := range after.Counts {
		if d := v - before.Counts[k]; d > 0 {
			out.Counts[k] = d
		}
	}
	return out
}

func uniq(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
