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
//     order, produce candidate commands (history first when Jev has a key,
//     history.go; then the model provider, source "generated"); every
//     candidate is rated by the code rule and returned at once. The
//     returned RiskCheck then asks Jev and may make the stored rating
//     stricter (never looser); keel serve pushes that to the card. An
//     insert is always checked against the strictest rating known then.
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
	ErrUsed         = errors.New("agent: this suggestion was inserted already")
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
	// codeRisk is the code rule's rating, before Jev.
	codeRisk agent.Risk
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
	used         bool // inserted once; a resent insert is refused
}

// SuggestInput is what a suggester gets: the redacted words, and the pane
// and host they are for. A suggester may read more of the pane only
// through the Auditor (the history suggester reads its scrollback).
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
//
// What a suggester sends off the machine goes through Service.send, which
// logs it before the call: a provider that fails, times out or returns
// nothing has still been sent the words.
type Suggester interface {
	Source() string
	Suggest(ctx context.Context, in SuggestInput) ([]Candidate, error)
}

// modelSuggester asks the model provider (Gemini or Claude) to write the command.
type modelSuggester struct {
	s        *Service
	to       line
	desc     provider.Descriptor // the provider, for keel.log
	model    string
	thinking string
}

func (m modelSuggester) Source() string { return SourceGenerated }

func (m modelSuggester) Suggest(ctx context.Context, in SuggestInput) ([]Candidate, error) {
	sent := in.Redactor.Report()
	msg := in.Redactor.Text(SuggestPrefix + in.Words.String())
	rep, err := m.s.send(ctx, m.to, outbound{
		model: &provider.Request{
			Model:           m.model,
			Thinking:        m.thinking,
			System:          suggestSafe,
			Messages:        []provider.Message{{Role: provider.RoleUser, Text: msg}},
			MaxOutputTokens: maxSuggestTokens,
		},
		log: agent.Egress{Pane: in.Pane, What: "the words on the prompt line", N: 1, Report: sent, To: m.desc.Name, Why: "to suggest a command", Source: m.desc.ID},
	})
	if err != nil {
		return nil, err
	}
	resp := rep.model
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

// detectLog gathers the detection sends of one pane in one window: sum is
// the entry that stands for all but the first.
type detectLog struct {
	sum   agent.Egress
	timer *time.Timer
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
	r, err := s.requestRedactor(set.Redact, hosts)
	if err != nil {
		return DetectResult{Source: DetectOff}, err
	}
	promptLine := r.Text(words)
	cctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	rep, err := s.send(cctx, judge, outbound{
		judge: &provider.JudgeRequest{
			Fields:    map[string]redact.Safe{statePromptLine: promptLine},
			Questions: map[string]provider.Question{"plain": detectQuestion},
		},
		log:       agent.Egress{Pane: p, What: promptLines(1), N: 1, Report: r.Report(), To: jevName, Why: "to notice plain language", Source: DetectJev},
		perWindow: true,
	})
	if err != nil {
		return DetectResult{Source: DetectFallback}, nil
	}
	return DetectResult{Plain: rep.judge.Nouls["plain"] >= NoticeThreshold, Source: DetectJev}, nil
}

func promptLines(n int) string {
	if n == 1 {
		return "1 prompt line"
	}
	return fmt.Sprintf("%d prompt lines", n)
}

// noteDetect logs a prompt line about to be sent to Jev (send calls it).
// The first line of a window is logged at once; the rest of the window is
// summarised in one entry when it ends, so a burst of pauses cannot flood
// keel.log and nothing sent goes unlogged.
func (s *Service) noteDetect(e agent.Egress) error {
	id := e.Pane.SessionID
	s.sug.mu.Lock()
	defer s.sug.mu.Unlock()
	if d := s.sug.detectLogs[id]; d != nil {
		d.sum.N++
		d.sum.What = promptLines(d.sum.N)
		d.sum.Report = agent.Merge(d.sum.Report, e.Report)
		return nil
	}
	if err := s.audit.Sent(e); err != nil {
		return err
	}
	e.N, e.Report, e.Summary = 0, redact.Report{Counts: map[redact.Kind]int{}}, true
	d := &detectLog{sum: e}
	d.timer = time.AfterFunc(s.detectWindow, func() { s.flushDetect(id) })
	s.sug.detectLogs[id] = d
	return nil
}

func (s *Service) flushDetect(sessionID string) {
	s.sug.mu.Lock()
	d := s.sug.detectLogs[sessionID]
	delete(s.sug.detectLogs, sessionID)
	s.sug.mu.Unlock()
	if d != nil && d.sum.N > 0 {
		// Already sent: the entry is owed if keel.log cannot take it now.
		_ = s.audit.Sent(d.sum)
	}
}

// Close writes the detection summaries still open, so lines sent to Jev
// in the last window before keel serve stops are logged too.
func (s *Service) Close() {
	s.sug.mu.Lock()
	ids := make([]string, 0, len(s.sug.detectLogs))
	for id, d := range s.sug.detectLogs {
		if d.timer != nil {
			d.timer.Stop()
		}
		ids = append(ids, id)
	}
	s.sug.mu.Unlock()
	for _, id := range ids {
		s.flushDetect(id)
	}
}

// SuggestModel is the model and thinking level suggestions use for a
// provider: its suggestion model when it names one, else the settings
// model with the model's default thinking.
func SuggestModel(desc provider.Descriptor, set store.AgentSettings) (model, thinking string) {
	if desc.SuggestModel != "" {
		return desc.SuggestModel, desc.SuggestThinking
	}
	return set.Model, ""
}

// Suggest turns the words on a pane's prompt line into a command and up
// to two alternatives (State 2, Suggest). It is the user's ⇥.
//
// It returns as soon as the model answers, with every option rated by the
// code rule (the denylist, made stricter by the model's own rating), and
// keeps the suggestion for AuthorizeInsert. The caller shows it, then runs
// the RiskCheck, which asks Jev and may make the kept rating stricter;
// RiskCheck.Run also writes the agent.suggest entry, so the caller must
// run it for every suggestion it gets.
func (s *Service) Suggest(ctx context.Context, req SuggestRequest) (Suggestion, *RiskCheck, error) {
	if err := agent.CanInsert(req.Role); err != nil {
		return Suggestion{}, nil, err
	}
	words, _, ok := cleanLine(req.Line)
	if !ok {
		return Suggestion{}, nil, badRequest(fmt.Sprintf("a prompt line is at most %d bytes, with no control characters", maxLineBytes))
	}
	if words == "" {
		return Suggestion{}, nil, ErrEmptyLine
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return Suggestion{}, nil, err
	}
	if !set.SuggestionsEnabled() {
		return Suggestion{}, nil, ErrSuggestOff
	}
	desc, ready, err := s.modelReady(set)
	if err != nil {
		return Suggestion{}, nil, err
	}
	if desc.Kind != provider.KindModel {
		return Suggestion{}, nil, ErrNotProvider
	}
	if !ready {
		return Suggestion{}, nil, ErrNoKey
	}
	p, host, hosts, err := s.paneFor(req.SessionID, req.Role)
	if err != nil {
		return Suggestion{}, nil, err
	}
	r, err := s.requestRedactor(set.Redact, hosts)
	if err != nil {
		return Suggestion{}, nil, err
	}
	in := SuggestInput{Words: r.Text(words), Pane: p, Host: host, Settings: set, Redactor: r}

	prov, err := s.providerFor(desc.ID)
	if err != nil {
		return Suggestion{}, nil, err
	}
	model, thinking := SuggestModel(desc, set)
	// History first, when Jev has a key: a command the user already ran
	// or curated, if Jev is sure it does what the words ask. Without a
	// key, or when Jev fails or is unsure, the model writes one.
	var suggesters []Suggester
	if judge, err := s.judgeFor(); err == nil {
		suggesters = append(suggesters, historySuggester{s: s, judge: judge, hosts: hosts, words: words})
	}
	suggesters = append(suggesters, modelSuggester{s: s, to: prov, desc: desc, model: model, thinking: thinking})

	var cands []Candidate
	var modelTook, pickTook time.Duration
	for _, sg := range suggesters {
		start := s.now()
		var c []Candidate
		if sg.Source() == SourceGenerated {
			var err error
			c, modelTook, err = s.runModel(ctx, sg, in, start)
			if err != nil {
				return Suggestion{}, nil, err
			}
		} else {
			// Never an error: anything but a confident pick falls through.
			c, _ = sg.Suggest(ctx, in)
			pickTook += s.since(start)
			if err := ctx.Err(); err != nil {
				return Suggestion{}, nil, err
			}
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
		return Suggestion{}, nil, ErrNoSuggestion
	}
	if len(cands) > 1+maxAlternatives {
		cands = cands[:1+maxAlternatives]
	}

	opts := codeRatings(cands, host.Production)
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
	check := &RiskCheck{
		s:         s,
		sug:       sug,
		pane:      p,
		cands:     cands,
		redactor:  r,
		sent:      r.Report(),
		modelTook: modelTook,
		pickTook:  pickTook,
		model:     model,
		thinking:  thinking,
	}
	return sug, check, nil
}

// runModel runs the model suggester with its own budget (suggestTimeout),
// whatever the history step took, and turns its errors into the card's.
func (s *Service) runModel(ctx context.Context, sg Suggester, in SuggestInput, start time.Time) ([]Candidate, time.Duration, error) {
	cctx, cancel := context.WithTimeout(ctx, s.suggestTimeout)
	defer cancel()
	c, err := sg.Suggest(cctx, in)
	if err != nil {
		if errors.Is(err, agent.ErrAuditLog) {
			return nil, 0, err // nothing was sent
		}
		if ctx.Err() != nil {
			return nil, 0, context.Canceled
		}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			// Not providerErr: its message names the ask timeout (s.timeout), not this one.
			return nil, 0, &ProviderError{Msg: fmt.Sprintf("The model did not answer within %s.", s.suggestTimeout)}
		}
		var pe *ProviderError
		if errors.As(err, &pe) {
			return nil, 0, pe
		}
		return nil, 0, &ProviderError{Msg: err.Error()}
	}
	return c, s.since(start), nil
}

// since is the time since start on the service's clock, at least 1 ns so
// a call that ran is never read as "not made".
func (s *Service) since(start time.Time) time.Duration {
	return max(s.now().Sub(start), time.Nanosecond)
}

// codeRatings rates each candidate by the code rule (agent.Classify, with
// its denylist), made stricter by the suggester's own rating. A
// destructive code rating is final.
func codeRatings(cands []Candidate, production bool) []SuggestOption {
	out := make([]SuggestOption, len(cands))
	for i, c := range cands {
		risk := rate(c.Command, modelRisk(c.ModelRisk))
		out[i] = SuggestOption{
			Command:      c.Command,
			Bytes:        insertBytes(c.Command),
			Why:          c.Why,
			Risk:         risk,
			NeedsShift:   agent.NeedsShift(risk, production),
			Placeholders: hasPlaceholder(c.Command),
			Source:       c.Source,
			codeRisk:     risk,
		}
	}
	return out
}

// OptionRisk is one option's rating after Jev.
type OptionRisk struct {
	Risk       agent.Risk `json:"risk"`
	NeedsShift bool       `json:"needsShift"`
	Judged     bool       `json:"judged"`
}

// RiskUpdate is the result of a RiskCheck: the rating of every option of
// a suggestion, in order. Stricter is true when Jev made at least one
// rating stricter than the one shown, so the card must change.
type RiskUpdate struct {
	SuggestionID string       `json:"suggestionId"`
	Options      []OptionRisk `json:"options"`
	Stricter     bool         `json:"-"`
}

// SuggestTiming says where a suggestion's time went, for keel serve's
// own log (never keel.log's content: it holds no words).
type SuggestTiming struct {
	Model    string
	Thinking string
	// Source is the suggestion's source. PickMs is how long the history
	// pick took (0 when Jev has no key); ModelMs is 0 when history was
	// confident and the model was not called.
	Source   string
	PickMs   int64
	ModelMs  int64
	JevMs    int64
	JevAsked bool
}

// RiskCheck is a suggestion's second step: Jev rates the candidates the
// code rule did not settle, and the kept suggestion takes any stricter
// rating. Run it once, after the card is shown.
type RiskCheck struct {
	s         *Service
	sug       Suggestion
	pane      agent.Pane
	cands     []Candidate
	redactor  *redact.Redactor
	sent      redact.Report
	modelTook time.Duration
	pickTook  time.Duration
	model     string
	thinking  string

	once   sync.Once
	update RiskUpdate
	timing SuggestTiming
}

// Run asks Jev (bounded by riskTimeout, and not cut short when ctx ends:
// the kept rating should be as strict as it can be for a later insert),
// makes the kept suggestion's ratings stricter where Jev says so, and
// writes agent.suggest with the timings. Jev failing or slow leaves the
// code rating. Later calls return the first result.
func (c *RiskCheck) Run(ctx context.Context) (RiskUpdate, SuggestTiming) {
	c.once.Do(func() { c.update, c.timing = c.run(ctx) })
	return c.update, c.timing
}

func (c *RiskCheck) run(ctx context.Context) (RiskUpdate, SuggestTiming) {
	s := c.s
	var ask []int
	for i, o := range c.sug.Options {
		if o.Risk != agent.RiskDestructive {
			ask = append(ask, i)
		}
	}
	var judged map[int]agent.Risk
	var jevTook time.Duration
	asked := false
	if len(ask) > 0 {
		// Jev without a key, or one that cannot be built, is not asked:
		// the code rating stands.
		if judge, err := s.judgeFor(); err == nil {
			start := s.now()
			judged, asked = s.judgeRisks(context.WithoutCancel(ctx), judge, c.pane, c.cands, ask, c.redactor)
			if asked {
				jevTook = s.since(start)
			}
		}
	}

	// Apply to the kept suggestion (a new slice: AuthorizeInsert may hold
	// the old one), or to the one shown when it is gone.
	s.sug.mu.Lock()
	base := c.sug.Options
	cur := s.sug.suggestions[c.sug.ID]
	if cur != nil {
		base = cur.Options
	}
	opts := append([]SuggestOption(nil), base...)
	stricter := false
	for i, j := range judged {
		if i >= len(opts) {
			continue
		}
		next := agent.Stricter(opts[i].Risk, j)
		if next != opts[i].Risk {
			stricter = true
		}
		opts[i].Risk = next
		opts[i].Judged = true
		opts[i].NeedsShift = agent.NeedsShift(next, c.sug.Production)
	}
	if cur != nil {
		cur.Options = opts
	}
	s.sug.mu.Unlock()

	upd := RiskUpdate{SuggestionID: c.sug.ID, Stricter: stricter, Options: make([]OptionRisk, len(opts))}
	for i, o := range opts {
		upd.Options[i] = OptionRisk{Risk: o.Risk, NeedsShift: o.NeedsShift, Judged: o.Judged}
	}
	s.audit.Suggest(c.pane, c.sent, riskLabel(opts[0].Risk), opts[0].Source, agent.Timing{Model: c.modelTook, Pick: c.pickTook, Jev: jevTook, Asked: asked})
	return upd, SuggestTiming{
		Model:    c.model,
		Thinking: c.thinking,
		Source:   opts[0].Source,
		PickMs:   c.pickTook.Milliseconds(),
		ModelMs:  c.modelTook.Milliseconds(),
		JevMs:    jevTook.Milliseconds(),
		JevAsked: asked,
	}
}

// judgeRisks asks Jev about the candidates at idx, all in one request.
// asked is false only when nothing was sent (keel.log could not take the
// entry); an answer that did not come leaves the map empty.
func (s *Service) judgeRisks(ctx context.Context, judge line, p agent.Pane, cands []Candidate, idx []int, r *redact.Redactor) (judged map[int]agent.Risk, asked bool) {
	cmds := make([]redact.Safe, len(idx))
	qs := make(map[string]provider.Question, len(idx))
	before := r.Report()
	for k, i := range idx {
		cmds[k] = r.Text(cands[i].Command)
		qs[riskQuestionID(k)] = riskQuestion(k)
	}
	what := "1 candidate command"
	if len(idx) > 1 {
		what = fmt.Sprintf("%d candidate commands", len(idx))
	}
	cctx, cancel := context.WithTimeout(ctx, riskTimeout)
	defer cancel()
	rep, err := s.send(cctx, judge, outbound{
		judge: &provider.JudgeRequest{Lists: map[string][]redact.Safe{stateCommands: cmds}, Questions: qs},
		log:   agent.Egress{Pane: p, What: what, N: len(idx), Report: agent.Diff(before, r.Report()), To: jevName, Why: "to rate their risk", Source: DetectJev},
	})
	if err != nil {
		// Asked (and logged) even so: the time spent still counts.
		return nil, !errors.Is(err, agent.ErrAuditLog)
	}
	out := map[int]agent.Risk{}
	for k, i := range idx {
		if a, ok := rep.judge.Choices[riskQuestionID(k)]; ok {
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

// AuthorizeInsert checks an insert before keel serve writes it, through
// authorize (authorize.go, pathInsert): the client drives the session; the
// suggestion exists, has not expired, has not been inserted already and
// targets this session; the bytes are the option's; the ⇧ rule holds, with
// the host counted as production if it was when suggested, is now, or can
// no longer be found. It then reserves the suggestion, so a resent insert
// is refused. It writes and logs nothing: keel serve writes, then calls
// InsertDone, which logs agent.insert only for a write that happened.
func (s *Service) AuthorizeInsert(req InsertRequest) (InsertGrant, error) {
	sub := suggestionSubject{s: s, id: req.SuggestionID, option: req.Option}
	return s.authorize(context.Background(), &pathInsert, sub, typeAsk{sessionID: req.SessionID, role: req.Role, bytes: req.Bytes, shift: req.Shift})
}

// InsertDone records the outcome of an authorised insert. A write that
// happened is logged as agent.insert and the suggestion stays used; a
// write that failed is not logged as an insert, and the suggestion may be
// inserted again. It returns the log's error.
func (s *Service) InsertDone(g InsertGrant, writeErr error) error { return s.settle(g, writeErr) }

// ErrNeedsShiftInsert is ErrNeedsShift worded for ⇥.
var ErrNeedsShiftInsert = errors.New("agent: this command is destructive, or changes state on a production host; press ⇧⇥ to insert it")

// ErrNeedsShiftJudged is ErrNeedsShiftInsert when Jev's rating, which
// came after the card was shown, is what made ⇧ necessary.
var ErrNeedsShiftJudged = errors.New("agent: a second check rated this command riskier than first shown; press ⇧⇥ to insert it")
