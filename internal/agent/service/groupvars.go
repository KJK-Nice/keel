package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/runvars"
	"keel/internal/shellmark"
	"keel/internal/store"
)

// Group variables and captured values in group runs (docs/ui-redesign.md
// §17, groups part 2, board CaptureVars).
//
//   - A runbook step's command may name a variable: {{name}} (one plain
//     shell word) or {{name|q}} (single-quoted). Its value is a value
//     captured earlier in the same run, or one of the group's variables.
//   - A step may capture values: after its command ends with a reported
//     exit 0 on a host, keel serve applies the step's pattern to that
//     command's output on that host, between the shell's OSC 133 C and D
//     marks (CaptureSource), and keeps the value in memory, in the run, for
//     that host, for at most captureTTL. The model plays no part: patterns
//     are applied by this code, and a group run sends nothing to a
//     provider.
//   - Values are filled in only at Type time, by keel serve, for one host's
//     proposal (fillProposal): the approve dialog asks for the filled bytes
//     (FillGroupProposals) and shows them; AuthorizeType fills again and
//     compares the client's bytes with those, so what is typed is exactly
//     what the user approved. The stored proposal, the run page's card and
//     everything the model or a log could see keep {{name}}.
//   - Values never reach the model: every Redactor the service builds
//     replaces captured values and group variable values with {{name}}
//     (requestRedactor), and logs carry counts only.
//   - A value that holds a vault value is never kept (a capture is
//     "blocked") and never filled in; a value that fails the shell-safety
//     rules (runvars.Fill) is refused with a message naming the variable.

// captureTTL is how long a captured value is kept (the board's "Deleted
// when the runbook ends, or after 1 hour").
const captureTTL = time.Hour

// Capture states, per host, as the run page shows them. Never the value.
const (
	CaptureCaptured = "captured"
	CaptureNoMatch  = "no-match"
	CaptureBlocked  = "blocked"   // holds a vault value: not kept
	CaptureRefused  = "refused"   // a control character, or too long: not kept
	CaptureNoOutput = "no-output" // no OSC 133 output to read
	CaptureExpired  = "expired"
	// CaptureTooShort is a value under 3 bytes: the Redactor cannot match
	// one that short, so it could not be kept from the model; not kept.
	CaptureTooShort = "too-short"
	// CaptureFull is a value not kept because the redact-only set is full
	// (maxWithheld); restarting keel serve clears the set.
	CaptureFull = "full"
)

var (
	// ErrVarsOutsideRun refuses typing a command that names a variable
	// anywhere but a group run's step: nothing else fills them in.
	ErrVarsOutsideRun = errors.New("agent: this command names a variable such as {{name}}, which Keel fills in only in a group run's steps; edit it to the real value first")
	// ErrFill is a variable that cannot be filled in now (missing, unsafe
	// or a vault value); the message names it.
	ErrFill = errors.New("agent: a variable cannot be filled in")
	// ErrGroupVarSecret refuses a group variable that looks like a secret.
	ErrGroupVarSecret = errors.New("agent: that value looks like a secret or holds a value from the vault; group variables are not secret (they are shown, filled into commands and exported). Keep secrets in the vault")
)

// CaptureSource reads a finished command's raw output. keel serve's
// runtime satisfies it (through httpapi's agentPanes).
type CaptureSource interface {
	CommandOutput(sessionID string, cmd shellmark.Command) ([]byte, bool)
}

// GroupVarStore keeps the group variables. *store.Store satisfies it.
type GroupVarStore interface {
	SetGroupVar(groupID, name, value string) (store.Group, error)
	AllGroupVars() (map[string][]store.GroupVar, error)
}

// CaptureView is one capture of a host × step as the run page shows it:
// never the value, only its state and length.
type CaptureView struct {
	Name      string     `json:"name"`
	State     string     `json:"state"`
	Bytes     int        `json:"bytes,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// capturedValue is a value in memory (under s.mu), in its run.
type capturedValue struct {
	value  string
	stepID string
	stepNo int
	host   string
	at     time.Time
	// oneHost is set when the capturing step runs on one host only: only
	// then may its value be filled in on the run's other hosts.
	oneHost bool
}

// SetGroupVar sets a group variable for the user (an empty value removes
// it). A value the save Redactor would change (a secret-looking value or a
// vault value) is refused: group variables are config, shown and exported.
func (s *Service) SetGroupVar(groupID, name, value string) (store.Group, error) {
	vs, ok := s.st.(GroupVarStore)
	if !ok {
		return store.Group{}, ErrNotFound
	}
	if strings.TrimSpace(value) != "" {
		r, err := agent.NewRedactor(s.st, store.AgentRedaction{Secrets: true}, nil)
		if err != nil {
			return store.Group{}, err
		}
		v := strings.TrimSpace(value)
		if r.Text(v).String() != v || r.InsideKnown(v) {
			return store.Group{}, ErrGroupVarSecret
		}
	}
	g, err := vs.SetGroupVar(groupID, name, value)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return store.Group{}, ErrNotFound
		case errors.Is(err, store.ErrGroupVar):
			return store.Group{}, fmt.Errorf("%w: %v", ErrBadRequest, err)
		}
		return store.Group{}, err
	}
	return g, nil
}

// holdsVaultValue reports whether any text holds a value from the vault.
// It uses a Redactor (the one way the agent tree reads vault values).
func (s *Service) holdsVaultValue(texts ...string) (bool, error) {
	r, err := agent.NewRedactor(s.st, store.AgentRedaction{}, nil)
	if err != nil {
		return false, err
	}
	for _, t := range texts {
		if r.HoldsKnown(t) {
			return true, nil
		}
	}
	return false, nil
}

// overlapsVault reports whether a value to fill in holds a vault value, or
// (6 bytes or longer) is part of one: a captured piece of a vault secret
// is as much a secret as the whole. Exported for the Ansible export too
// (HoldsVaultValue).
func (s *Service) overlapsVault(v string) (bool, error) {
	r, err := agent.NewRedactor(s.st, store.AgentRedaction{}, nil)
	if err != nil {
		return false, err
	}
	return r.HoldsKnown(v) || r.InsideKnown(v), nil
}

// HoldsVaultValue is overlapsVault for keel serve's Ansible export: a
// group variable saved before its value went into the vault is left out.
// It fails closed (true) when the vault cannot be read.
func (s *Service) HoldsVaultValue(v string) bool {
	bad, err := s.overlapsVault(v)
	return err != nil || bad
}

// requestRedactor is the Redactor of one agent request: agent.NewRedactor
// with every captured value and every group variable's value replaced by
// its {{name}}.
func (s *Service) requestRedactor(opts store.AgentRedaction, hosts []agent.Host) (*redact.Redactor, error) {
	return agent.NewRedactor(s.st, opts, hosts, s.namedValues(true)...)
}

// maxWithheld bounds the redact-only set of captured values. When it is
// full a new value is not kept at all (the capture is refused), so no
// value is ever filled in that the Redactor would not withhold.
const maxWithheld = 1024

// minCapturedBytes is the shortest value a capture keeps: the Redactor
// matches no named value under 3 bytes.
const minCapturedBytes = 3

// withholdLocked adds a captured value to the redact-only set, kept for
// the life of this keel serve whatever happens to its run (finished,
// stopped, evicted, expired, retried): the value stays in the scrollback
// of the pane it was captured from and of every pane it was typed into.
// It reports false when the set is full. Call with s.mu held.
func (s *Service) withholdLocked(name, value string) bool {
	if s.withheld == nil {
		s.withheld = map[string]string{}
	}
	if _, ok := s.withheld[value]; ok {
		return true
	}
	if len(s.withheld) >= maxWithheld {
		return false
	}
	s.withheld[value] = name
	return true
}

// namedValues lists the values the model must never see: every value a
// group run captured since keel serve started (the redact-only set, which
// outlives the runs), and (withVars) every group variable's value.
func (s *Service) namedValues(withVars bool) []redact.Named {
	var out []redact.Named
	s.mu.Lock()
	for value, name := range s.withheld {
		out = append(out, redact.Named{Name: name, Value: value, Kind: redact.KindCaptured})
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	if withVars {
		if vs, ok := s.st.(GroupVarStore); ok {
			if all, err := vs.AllGroupVars(); err == nil {
				ids := make([]string, 0, len(all))
				for id := range all {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					for _, v := range all[id] {
						out = append(out, redact.Named{Name: v.Name, Value: v.Value, Kind: redact.KindVariable})
					}
				}
			}
		}
	}
	return out
}

// pruneCapturesLocked drops values older than captureTTL and marks them
// expired on their cells. Call with s.mu held.
func (r *groupRun) pruneCapturesLocked(now time.Time) {
	for hostID, byName := range r.captures {
		for name, cv := range byName {
			if now.Sub(cv.at) < captureTTL {
				continue
			}
			delete(byName, name)
			r.setCaptureStateLocked(cv.stepID, hostID, name, CaptureExpired, 0, nil)
		}
		if len(byName) == 0 {
			delete(r.captures, hostID)
		}
	}
}

// wipeCapturesLocked drops every value of an ended run.
func (r *groupRun) wipeCapturesLocked() {
	for hostID, byName := range r.captures {
		for name, cv := range byName {
			r.setCaptureStateLocked(cv.stepID, hostID, name, CaptureExpired, 0, nil)
		}
	}
	r.captures = nil
}

// dropStepCapturesLocked forgets what a step captured on a host (a retry
// captures again).
func (r *groupRun) dropStepCapturesLocked(stepID, hostID string) {
	for name, cv := range r.captures[hostID] {
		if cv.stepID == stepID {
			delete(r.captures[hostID], name)
		}
	}
}

func (r *groupRun) setCaptureStateLocked(stepID, hostID, name, state string, n int, exp *time.Time) {
	for i := range r.Steps {
		if r.Steps[i].ID != stepID {
			continue
		}
		for j := range r.Steps[i].Cells {
			c := &r.Steps[i].Cells[j]
			if c.HostID != hostID {
				continue
			}
			for k := range c.Captures {
				if c.Captures[k].Name == name {
					c.Captures[k] = CaptureView{Name: name, State: state, Bytes: n, ExpiresAt: exp}
					return
				}
			}
			c.Captures = append(c.Captures, CaptureView{Name: name, State: state, Bytes: n, ExpiresAt: exp})
			return
		}
	}
}

// captureResult is one capture's outcome on a host.
type captureResult struct {
	name  string
	value string
	state string
}

// runCaptures applies a step's capture patterns to the output of the
// commands its typing started on a pane (the first want commands after the
// mark). It reads the raw output (CaptureSource), never sends or logs it.
func (s *Service) runCaptures(sessionID string, mark uint64, want int, caps []store.StepCapture) []captureResult {
	out := make([]captureResult, 0, len(caps))
	src, ok := s.panes.(CaptureSource)
	marks, ok2 := s.panes.(StepSource)
	var text strings.Builder
	readable := ok && ok2
	if readable {
		n := 0
		for _, cmd := range marks.CommandsAfter(sessionID, mark) {
			if !cmd.Done || n >= want {
				break
			}
			n++
			b, ok := src.CommandOutput(sessionID, cmd)
			if !ok {
				readable = false
				break
			}
			text.WriteString(redact.Normalize(string(b)))
			text.WriteByte('\n')
		}
		readable = readable && n > 0
	}
	for _, c := range caps {
		if !readable {
			out = append(out, captureResult{name: c.Name, state: CaptureNoOutput})
			continue
		}
		re, err := runvars.CompilePattern(c.Pattern)
		if err != nil {
			out = append(out, captureResult{name: c.Name, state: CaptureNoMatch})
			continue
		}
		v, found := runvars.Extract(re, c.Name, text.String())
		switch {
		case !found:
			out = append(out, captureResult{name: c.Name, state: CaptureNoMatch})
		case len(strings.TrimSpace(v)) < minCapturedBytes:
			// Too short for the Redactor to withhold (redact.namedList
			// skips it): never kept, so never typed.
			out = append(out, captureResult{name: c.Name, state: CaptureTooShort})
		case runvars.CheckValue(v, runvars.MaxValueBytes) != nil:
			out = append(out, captureResult{name: c.Name, state: CaptureRefused})
		default:
			out = append(out, captureResult{name: c.Name, value: v, state: CaptureCaptured})
		}
	}
	// A value that holds a vault value is never kept: Keel will not carry
	// a vault secret into a command through a variable. (Fails closed: no
	// vault answer, nothing kept.)
	for i := range out {
		if out[i].state != CaptureCaptured {
			continue
		}
		if bad, err := s.overlapsVault(out[i].value); err != nil || bad {
			out[i].value, out[i].state = "", CaptureBlocked
		}
	}
	return out
}

// GroupFill is one host's proposal with its variables filled in, for the
// approve dialog: the exact bytes Type will send to that pane, which
// variables went in and from where (never the values on their own), and
// the ⇧ rule on the filled command. Error, when set, says why it cannot
// be typed now (a missing or unsafe variable); there are no bytes then.
type GroupFill struct {
	ProposalID string         `json:"proposalId"`
	HostID     string         `json:"hostId"`
	Host       string         `json:"host"`
	Bytes      string         `json:"bytes,omitempty"`
	Used       []runvars.Used `json:"used,omitempty"`
	Risk       agent.Risk     `json:"risk,omitempty"`
	NeedsShift bool           `json:"needsShift,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// FillGroupProposals fills the run's live proposals named by ids, for the
// approve dialog. It writes nothing and changes nothing in the run.
func (s *Service) FillGroupProposals(runID string, ids []string) ([]GroupFill, error) {
	s.mu.Lock()
	run := s.groupRuns[runID]
	active := run != nil && run.Status == runActive
	s.mu.Unlock()
	if !active {
		return nil, ErrNoGroupRun
	}
	out := make([]GroupFill, 0, len(ids))
	for _, id := range uniq(ids) {
		f, err := s.fillProposal(runID, id)
		if err != nil && !errors.Is(err, ErrFill) && !errors.Is(err, ErrPlaceholder) {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		if err != nil {
			f.Bytes, f.Used = "", nil
			f.Error = strings.TrimPrefix(strings.TrimPrefix(err.Error(), ErrFill.Error()+": "), "agent: ")
		}
		out = append(out, f)
	}
	return out, nil
}

// fillProposal fills one live proposal of a run for its host, now: the
// values captured on that host in this run first, then a value captured
// on other hosts of the run (only when they all agree), then the group's
// variable. The risk is the stricter of the saved command's and the filled
// command's, with the host's production flag now.
func (s *Service) fillProposal(runID, proposalID string) (GroupFill, error) {
	now := s.now()
	s.mu.Lock()
	run, c := s.groupProposalLocked(runID, proposalID)
	if run == nil || c == nil || run.Status != runActive {
		s.mu.Unlock()
		return GroupFill{}, ErrNotFound
	}
	run.pruneCapturesLocked(now)
	snap := *c.Proposal
	groupID := run.GroupID
	// name → host id → value (copies)
	held := map[string]map[string]capturedValue{}
	for hostID, byName := range run.captures {
		for name, cv := range byName {
			if held[name] == nil {
				held[name] = map[string]capturedValue{}
			}
			held[name][hostID] = *cv
		}
	}
	s.mu.Unlock()
	f := GroupFill{ProposalID: snap.ID, HostID: snap.HostID, Host: snap.Host, Bytes: snap.Bytes, Risk: snap.Risk, NeedsShift: snap.NeedsShift}
	if !runvars.HasRefs(snap.Command) {
		if hasPlaceholder(snap.Command) {
			return f, ErrPlaceholder
		}
		return f, nil
	}
	var groupVars []store.GroupVar
	if vs, ok := s.st.(GroupVarStore); ok {
		all, err := vs.AllGroupVars()
		if err != nil {
			return f, err
		}
		groupVars = all[groupID]
	}
	// Only its vault matches are used (HoldsKnown, InsideKnown).
	vault, err := agent.NewRedactor(s.st, store.AgentRedaction{}, nil)
	if err != nil {
		return f, err
	}
	lookup := func(name string) (runvars.Value, error) {
		var v runvars.Value
		found := false
		if cv, ok := held[name][snap.HostID]; ok {
			v, found = runvars.Value{Value: cv.value, From: fmt.Sprintf("captured on %s · step %d", cv.host, cv.stepNo)}, true
		} else if len(held[name]) > 0 {
			var hosts []string
			vals := map[string]bool{}
			var any capturedValue
			perHost := false
			for _, cv := range held[name] {
				hosts = append(hosts, cv.host)
				vals[cv.value] = true
				any = cv
				perHost = perHost || !cv.oneHost
			}
			sort.Strings(hosts)
			// A value captured by a step that runs on each host of its role
			// is that host's own value (a node's id, its disk): never filled
			// in on another host. Only a One host only step's value (a join
			// token from one control-plane node) serves the whole run.
			if perHost {
				return v, fmt.Errorf("%w: {{%s}} has no value for %s: it was captured on %s by a step that runs on each host, so it is that host's own value. Capture it on %s too, or make the capturing step One host only", ErrFill, name, snap.Host, strings.Join(hosts, ", "), snap.Host)
			}
			if len(vals) > 1 {
				return v, fmt.Errorf("%w: {{%s}} was captured with different values on %s; Keel will not pick one for %s. Skip the hosts whose value you do not want, or retry the step", ErrFill, name, strings.Join(hosts, ", "), snap.Host)
			}
			v, found = runvars.Value{Value: any.value, From: fmt.Sprintf("captured on %s · step %d", any.host, any.stepNo)}, true
		}
		if !found {
			for _, gv := range groupVars {
				if gv.Name == name {
					v, found = runvars.Value{Value: gv.Value, From: "group variable"}, true
				}
			}
		}
		if !found {
			return v, fmt.Errorf("%w: {{%s}} has no value for %s: no earlier step of this run captured it (or it expired after an hour) and the group has no variable with that name. Add it on the group's page, or retry the step that captures it", ErrFill, name, snap.Host)
		}
		if vault.HoldsKnown(v.Value) || vault.InsideKnown(v.Value) {
			return v, fmt.Errorf("%w: {{%s}} (%s) holds a value from the vault; Keel never types a vault secret from a variable", ErrFill, name, v.From)
		}
		return v, nil
	}
	filled, used, err := runvars.Fill(snap.Command, lookup)
	if err != nil {
		if errors.Is(err, ErrFill) {
			return f, err
		}
		return f, fmt.Errorf("%w: %v", ErrFill, err)
	}
	if hasPlaceholder(filled) {
		return f, ErrPlaceholder
	}
	if len(filled) > 4*store.MaxRunbookCommandBytes {
		return f, fmt.Errorf("%w: the filled command is longer than %d bytes", ErrFill, 4*store.MaxRunbookCommandBytes)
	}
	prod, _ := s.hostProduction(snap.HostID)
	f.Bytes, f.Used = typedBytes(filled), used
	f.Risk = agent.Stricter(snap.Risk, agent.Classify(filled))
	f.NeedsShift = agent.NeedsShift(f.Risk, snap.Production || prod)
	return f, nil
}

// varRefs lists the variables a command names, for the run page.
func varRefs(cmd string) []runvars.Ref {
	refs := runvars.Refs(cmd)
	var out []runvars.Ref
	seen := map[string]bool{}
	for _, r := range refs {
		key := r.Name
		if r.Quoted {
			key += "|q"
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, runvars.Ref{Name: r.Name, Quoted: r.Quoted})
		}
	}
	return out
}
