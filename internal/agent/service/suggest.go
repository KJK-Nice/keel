package service

// Plain language → command in a pane (docs/ui-redesign.md §5.5, phase 6c).
//
// Three calls, all on the user's own session socket, so keel serve knows
// the asking client's role in the pane (a follower gets nothing, §6.6):
//
//   - Detect: is the prompt line plain language? `#` lines always are (no
//     egress); otherwise, with Detection "auto", one Jev Noul. Any Jev
//     failure falls back to #-lines only, silently.
//   - Suggest: the user pressed ⇥ on a noticed line. The suggesters, in
//     order, produce candidate commands (6c has one: the model provider,
//     source "generated"); every candidate is rated by the code rule first
//     and then, when Jev answers, made stricter (never looser).
//   - AuthorizeInsert: the user pressed ⇥ / ⇧⇥ on a suggestion. Checks
//     the suggestion, the bytes, the ⇧ rule and the driver, and logs
//     agent.insert. It writes nothing: keel serve writes the approved
//     bytes through the inject ticket path afterwards.
//
// Everything sent off the machine is the prompt line or a candidate
// command after the panel's Redactor (redact.Safe), only for hosts with
// "Agent can read" on.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
	"keel/internal/store"
)

var (
	ErrSuggestOff   = errors.New("agent: suggestions in panes are off")
	ErrEmptyLine    = errors.New("agent: there are no words on the prompt line")
	ErrNoSuggestion = errors.New("agent: the model did not suggest a command")
	ErrExpired      = errors.New("agent: this suggestion is no longer available; press ⇥ again")
)

// Detection sources, sent to the client with every detect answer.
const (
	DetectJev      = "jev"       // Jev answered
	DetectHash     = "hash"      // the line starts with #
	DetectHashOnly = "hash-only" // the setting allows #-lines only
	DetectFallback = "fallback"  // Jev has no key, failed or timed out: #-lines only
	DetectOff      = "off"       // suggestions off, no model key, or the host is not readable
	DetectNone     = "none"      // nothing worth asking about (empty, one word, too long)
)

// SourceGenerated is the source of a candidate the model provider wrote.
const SourceGenerated = "generated"

// DetectRequest is one prompt line from the client driving a pane.
type DetectRequest struct {
	SessionID string
	Role      agent.Role
	Line      string
}

type DetectResult struct {
	Plain  bool   `json:"plain"`
	Source string `json:"source"`
}

// SuggestRequest asks for a command for the words on a pane's prompt line.
type SuggestRequest struct {
	SessionID string
	Role      agent.Role
	Line      string
}

// SuggestOption is one command offered: the first is the suggestion, the
// rest are alternatives (2, 3 or ↑↓).
type SuggestOption struct {
	Command string `json:"command"`
	// Bytes are exactly what ⇥ inserts: Ctrl-U to clear the line, then
	// the command. No newline: an insert is never run (§5.5).
	Bytes        string     `json:"bytes"`
	Why          string     `json:"why,omitempty"`
	Risk         agent.Risk `json:"risk"`
	NeedsShift   bool       `json:"needsShift"`
	Placeholders bool       `json:"placeholders"`
	Source       string     `json:"source"`
	// Judged: Jev rated this command too (it may only have made the
	// rating stricter).
	Judged bool `json:"judged"`
}

// Suggestion is kept in memory until it expires, so an insert can be
// checked against what was shown.
type Suggestion struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"sessionId"`
	HostID       string          `json:"hostId,omitempty"`
	Host         string          `json:"host,omitempty"`
	Production   bool            `json:"production"`
	Options      []SuggestOption `json:"options"`
	TestProvider bool            `json:"testProvider"`
	created      time.Time
}

// SuggestInput is what a suggester gets: the redacted words, and the pane
// and host they are for. A suggester may read more of the pane only
// through the Auditor (later phases: commands from the scrollback).
type SuggestInput struct {
	Words    redact.Safe
	Pane     agent.Pane
	Host     agent.Host
	Settings store.AgentSettings
	Redactor *redact.Redactor
}

// Candidate is a command a suggester offers, before Keel rates it.
type Candidate struct {
	Command string
	Why     string
	// ModelRisk is the suggester's own rating, if any; it can only make
	// Keel's rating stricter.
	ModelRisk string
	Source    string
}

// Suggester produces candidate commands for the words on a prompt line.
// Suggesters run in order and the first one with candidates wins, so a
// later phase can put a cheaper one (commands from the pane's history,
// snippets marked for the agent, picked by Jev) ahead of the model, and
// fall through to it when it has nothing confident. Every candidate,
// whatever its source, gets the same rating and the same ⇥ / ⇧⇥ gates.
type Suggester interface {
	Source() string
	Suggest(ctx context.Context, in SuggestInput) ([]Candidate, error)
}

// modelSuggester asks the model provider (Gemini) to write the command.
type modelSuggester struct {
	p     provider.Provider
	model string
}

func (m modelSuggester) Source() string { return SourceGenerated }

func (m modelSuggester) Suggest(ctx context.Context, in SuggestInput) ([]Candidate, error) {
	msg := in.Redactor.Text(SuggestPrefix + in.Words.String())
	resp, err := m.p.Generate(ctx, provider.Request{
		Model:           m.model,
		System:          suggestSafe,
		Messages:        []provider.Message{{Role: provider.RoleUser, Text: msg}},
		MaxOutputTokens: maxOutputTokens,
	})
	if err != nil {
		return nil, err
	}
	_, raws := parseResponse(resp.Text)
	out := make([]Candidate, 0, len(raws))
	for _, r := range raws {
		out = append(out, Candidate{Command: r.Command, Why: r.Why, ModelRisk: r.Risk, Source: SourceGenerated})
	}
	return out, nil
}

// suggestState is the in-pane suggestion part of a Service.
type suggestState struct {
	mu          sync.Mutex
	suggestions map[string]*Suggestion
	detectLogs  map[string]*detectLog
}

// detectLog gathers the detection reads of one pane in one window.
type detectLog struct {
	pane agent.Pane
	n    int
	rep  redact.Report
}

// cleanLine trims a prompt line, and says whether it started with #.
// ok is false for a line too long or with control characters.
func cleanLine(s string) (words string, hash, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) > maxLineBytes {
		return "", false, false
	}
	for _, c := range s {
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) {
			return "", false, false
		}
	}
	if strings.HasPrefix(s, "#") {
		return strings.TrimSpace(strings.TrimLeft(s, "#")), true, true
	}
	return s, false, true
}

// paneFor resolves a session to a pane in scope and its host. err is
// agent.ErrNotReadable when the host is not readable by the agent.
func (s *Service) paneFor(sessionID string, role agent.Role) (agent.Pane, agent.Host, []agent.Host, error) {
	p, ok := s.panes.Pane(sessionID)
	if !ok {
		return agent.Pane{}, agent.Host{}, nil, ErrNotFound
	}
	p.Role = role
	storeHosts, err := s.st.ListHosts()
	if err != nil {
		return agent.Pane{}, agent.Host{}, nil, err
	}
	hosts := agent.HostsFrom(storeHosts)
	var host agent.Host
	for _, h := range hosts {
		if h.ID == p.HostID {
			host = h
		}
	}
	p.HostName = host.Name
	if !agent.NewScope(hosts).Allows(p) {
		return agent.Pane{}, agent.Host{}, nil, agent.ErrNotReadable
	}
	return p, host, hosts, nil
}

// modelReady reports whether the configured model provider has a key. No
// key: no notice and no hint (see Detect).
func (s *Service) modelReady(set store.AgentSettings) (provider.Descriptor, bool, error) {
	desc, ok := provider.Lookup(set.Provider)
	if !ok || desc.Kind != provider.KindModel {
		return desc, false, nil
	}
	configured, err := s.st.AgentKeyConfigured(desc.ID)
	return desc, configured, err
}

// Detect decides whether the prompt line is plain language (State 1,
// Notice). It never returns an error for a detector failure: that is the
// silent #-lines fallback. It returns an error only when the asker may
// not have suggestions at all (a follower, §6.6) or the store fails.
func (s *Service) Detect(ctx context.Context, req DetectRequest) (DetectResult, error) {
	if err := agent.CanInsert(req.Role); err != nil {
		return DetectResult{Source: DetectOff}, err
	}
	words, hash, ok := cleanLine(req.Line)
	if !ok || words == "" {
		return DetectResult{Source: DetectNone}, nil
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return DetectResult{Source: DetectOff}, err
	}
	if !set.SuggestionsEnabled() {
		return DetectResult{Source: DetectOff}, nil
	}
	// Without a model key a notice would promise a ⇥ that cannot answer,
	// and the line would go to Jev for nothing: stay quiet.
	if _, ready, err := s.modelReady(set); err != nil || !ready {
		return DetectResult{Source: DetectOff}, err
	}
	p, _, hosts, err := s.paneFor(req.SessionID, req.Role)
	if err != nil {
		if errors.Is(err, agent.ErrNotReadable) || errors.Is(err, ErrNotFound) {
			return DetectResult{Source: DetectOff}, nil
		}
		return DetectResult{Source: DetectOff}, err
	}
	if hash {
		return DetectResult{Plain: true, Source: DetectHash}, nil
	}
	if set.Detection == store.DetectionHashOnly {
		return DetectResult{Source: DetectHashOnly}, nil
	}
	if len(strings.Fields(words)) < minDetectWords {
		return DetectResult{Source: DetectNone}, nil
	}
	judge, err := s.judgeFor()
	if err != nil {
		return DetectResult{Source: DetectFallback}, nil
	}
	r, err := agent.NewRedactor(s.st, set.Redact, hosts)
	if err != nil {
		return DetectResult{Source: DetectOff}, err
	}
	line := r.Text(words)
	s.noteDetect(p, r.Report())
	cctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	resp, err := judge.Judge(cctx, provider.JudgeRequest{
		Fields:    map[string]redact.Safe{statePromptLine: line},
		Questions: map[string]provider.Question{"plain": detectQuestion},
	})
	if err != nil {
		return DetectResult{Source: DetectFallback}, nil
	}
	return DetectResult{Plain: resp.Nouls["plain"] >= NoticeThreshold, Source: DetectJev}, nil
}

// noteDetect logs a prompt line sent to Jev. The first line of a window
// is logged at once; the rest of the window is summarised in one entry
// when it ends, so a burst of pauses cannot flood keel.log and nothing
// sent goes unlogged.
func (s *Service) noteDetect(p agent.Pane, rep redact.Report) {
	s.sug.mu.Lock()
	defer s.sug.mu.Unlock()
	if d := s.sug.detectLogs[p.SessionID]; d != nil {
		d.n++
		d.rep = agent.Merge(d.rep, rep)
		return
	}
	s.audit.PromptLines(p, 1, rep, DetectJev)
	s.sug.detectLogs[p.SessionID] = &detectLog{pane: p, rep: redact.Report{Counts: map[redact.Kind]int{}}}
	time.AfterFunc(s.detectWindow, func() { s.flushDetect(p.SessionID) })
}

func (s *Service) flushDetect(sessionID string) {
	s.sug.mu.Lock()
	d := s.sug.detectLogs[sessionID]
	delete(s.sug.detectLogs, sessionID)
	s.sug.mu.Unlock()
	if d != nil && d.n > 0 {
		s.audit.PromptLines(d.pane, d.n, d.rep, DetectJev)
	}
}

// Suggest turns the words on a pane's prompt line into a command and up
// to two alternatives (State 2, Suggest). It is the user's ⇥.
func (s *Service) Suggest(ctx context.Context, req SuggestRequest) (Suggestion, error) {
	if err := agent.CanInsert(req.Role); err != nil {
		return Suggestion{}, err
	}
	words, _, ok := cleanLine(req.Line)
	if !ok {
		return Suggestion{}, badRequest(fmt.Sprintf("a prompt line is at most %d bytes, with no control characters", maxLineBytes))
	}
	if words == "" {
		return Suggestion{}, ErrEmptyLine
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return Suggestion{}, err
	}
	if !set.SuggestionsEnabled() {
		return Suggestion{}, ErrSuggestOff
	}
	desc, ready, err := s.modelReady(set)
	if err != nil {
		return Suggestion{}, err
	}
	if desc.Kind != provider.KindModel {
		return Suggestion{}, ErrNotProvider
	}
	if !ready {
		return Suggestion{}, ErrNoKey
	}
	p, host, hosts, err := s.paneFor(req.SessionID, req.Role)
	if err != nil {
		return Suggestion{}, err
	}
	r, err := agent.NewRedactor(s.st, set.Redact, hosts)
	if err != nil {
		return Suggestion{}, err
	}
	in := SuggestInput{Words: r.Text(words), Pane: p, Host: host, Settings: set, Redactor: r}

	prov, err := s.providerFor(desc.ID)
	if err != nil {
		return Suggestion{}, err
	}
	suggesters := []Suggester{modelSuggester{p: prov, model: set.Model}}

	cctx, cancel := context.WithTimeout(ctx, suggestTimeout)
	defer cancel()
	var cands []Candidate
	for _, sg := range suggesters {
		c, err := sg.Suggest(cctx, in)
		if err != nil {
			if errors.Is(cctx.Err(), context.DeadlineExceeded) || ctx.Err() != nil {
				return Suggestion{}, s.providerErr(ctx, cctx, err)
			}
			var pe *ProviderError
			if errors.As(err, &pe) {
				return Suggestion{}, pe
			}
			return Suggestion{}, &ProviderError{Msg: err.Error()}
		}
		c = singleLine(c)
		if len(c) > 0 {
			for i := range c {
				if c[i].Source == "" {
					c[i].Source = sg.Source()
				}
			}
			cands = c
			break
		}
	}
	if len(cands) == 0 {
		return Suggestion{}, ErrNoSuggestion
	}
	if len(cands) > 1+maxAlternatives {
		cands = cands[:1+maxAlternatives]
	}

	opts := s.rateCandidates(ctx, cands, r, host.Production)
	sug := Suggestion{
		ID:           newID("s_"),
		SessionID:    p.SessionID,
		HostID:       p.HostID,
		Host:         p.HostName,
		Production:   host.Production,
		Options:      opts,
		TestProvider: s.test,
		created:      s.now(),
	}
	s.keepSuggestion(&sug)
	s.audit.SuggestFrom(p, r.Report(), riskLabel(opts[0].Risk), opts[0].Source)
	return sug, nil
}

// rateCandidates rates each candidate: the code rule (agent.Classify, with
// its denylist) first, made stricter by the suggester's own rating, then
// by Jev's when it answers. A destructive code rating is final and is not
// asked about. Jev failing or slow leaves the code rating.
func (s *Service) rateCandidates(ctx context.Context, cands []Candidate, r *redact.Redactor, production bool) []SuggestOption {
	out := make([]SuggestOption, len(cands))
	var ask []int
	for i, c := range cands {
		out[i] = SuggestOption{
			Command:      c.Command,
			Bytes:        insertBytes(c.Command),
			Why:          c.Why,
			Risk:         rate(c.Command, c.ModelRisk),
			Placeholders: hasPlaceholder(c.Command),
			Source:       c.Source,
		}
		if out[i].Risk != agent.RiskDestructive {
			ask = append(ask, i)
		}
	}
	if len(ask) > 0 {
		if judged, ok := s.judgeRisks(ctx, cands, ask, r); ok {
			for _, i := range ask {
				if j, ok := judged[i]; ok {
					out[i].Risk = agent.Stricter(out[i].Risk, j)
					out[i].Judged = true
				}
			}
		}
	}
	for i := range out {
		out[i].NeedsShift = agent.NeedsShift(out[i].Risk, production)
	}
	return out
}

// judgeRisks asks Jev about the candidates at idx, all in one request.
func (s *Service) judgeRisks(ctx context.Context, cands []Candidate, idx []int, r *redact.Redactor) (map[int]agent.Risk, bool) {
	judge, err := s.judgeFor()
	if err != nil {
		return nil, false
	}
	cmds := make([]redact.Safe, len(idx))
	qs := make(map[string]provider.Question, len(idx))
	for k, i := range idx {
		cmds[k] = r.Text(cands[i].Command)
		qs[riskQuestionID(k)] = riskQuestion(k)
	}
	cctx, cancel := context.WithTimeout(ctx, riskTimeout)
	defer cancel()
	resp, err := judge.Judge(cctx, provider.JudgeRequest{Lists: map[string][]redact.Safe{stateCommands: cmds}, Questions: qs})
	if err != nil {
		return nil, false
	}
	out := map[int]agent.Risk{}
	for k, i := range idx {
		if a, ok := resp.Choices[riskQuestionID(k)]; ok {
			out[i] = judgedRisk(a)
		}
	}
	return out, true
}

// singleLine keeps the candidates of one line: an insert is never run,
// and a second line would be. Whatever the suggester, this holds.
func singleLine(c []Candidate) []Candidate {
	out := c[:0:0]
	for _, x := range c {
		if !strings.ContainsAny(x.Command, "\r\n") {
			out = append(out, x)
		}
	}
	return out
}

// insertBytes is exactly what ⇥ sends: Ctrl-U to clear the prompt line
// (as Type into pane does), then the command, and no newline.
func insertBytes(command string) string {
	return "\x15" + command
}

func (s *Service) keepSuggestion(sug *Suggestion) {
	s.sug.mu.Lock()
	defer s.sug.mu.Unlock()
	now := s.now()
	for id, old := range s.sug.suggestions {
		if now.Sub(old.created) > suggestionTTL {
			delete(s.sug.suggestions, id)
		}
	}
	for len(s.sug.suggestions) >= maxSuggestions {
		var oldest string
		for id, old := range s.sug.suggestions {
			if oldest == "" || old.created.Before(s.sug.suggestions[oldest].created) {
				oldest = id
			}
		}
		delete(s.sug.suggestions, oldest)
	}
	cp := *sug
	cp.Options = append([]SuggestOption(nil), sug.Options...)
	s.sug.suggestions[sug.ID] = &cp
}

// InsertRequest is the user's ⇥ or ⇧⇥ on a suggestion, as keel serve sees
// it on the session socket.
type InsertRequest struct {
	SuggestionID string
	Option       int // index in Suggestion.Options
	SessionID    string
	Role         agent.Role // the inserting client's role in that session
	Bytes        string     // what the client asks to inject
	Shift        bool       // ⇧ was held
}

// AuthorizeInsert checks an insert before keel serve writes it: the
// suggestion exists, has not expired and targets this session; the bytes
// are the option's; the ⇧ rule holds with the host's current production
// flag; and the client drives the session. It then logs agent.insert with
// the suggestion's source. It writes nothing.
func (s *Service) AuthorizeInsert(req InsertRequest) error {
	s.sug.mu.Lock()
	sug := s.sug.suggestions[req.SuggestionID]
	var snap Suggestion
	if sug != nil {
		snap = *sug
	}
	s.sug.mu.Unlock()
	if sug == nil || s.now().Sub(snap.created) > suggestionTTL {
		return ErrExpired
	}
	if snap.SessionID != req.SessionID {
		return ErrWrongPane
	}
	if req.Option < 0 || req.Option >= len(snap.Options) {
		return ErrNotFound
	}
	opt := snap.Options[req.Option]
	if req.Bytes != opt.Bytes || strings.ContainsAny(req.Bytes, "\r\n") {
		return ErrChanged
	}
	production, err := s.hostProduction(snap.HostID)
	if err != nil {
		return err
	}
	if agent.NeedsShift(opt.Risk, production) && !req.Shift {
		return ErrNeedsShiftInsert
	}
	return s.audit.InsertFrom(agent.Pane{SessionID: snap.SessionID, HostID: snap.HostID, HostName: snap.Host, Role: req.Role}, opt.Bytes, opt.Source)
}

// ErrNeedsShiftInsert is ErrNeedsShift worded for ⇥.
var ErrNeedsShiftInsert = errors.New("agent: this command is destructive, or changes state on a production host; press ⇧⇥ to insert it")
