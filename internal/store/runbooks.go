package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"keel/internal/runvars"
)

// Runbooks for the agent (docs/ui-redesign.md §16): user-curated, ordered
// lists of steps the agent may walk with the user, one proposal at a time.
//
// They live in keel.memory beside the notes (agentnotes.go), under the
// same key, so they open only with the vault and Delete all history never
// touches them. The store keeps what it is handed, after cleaning: every
// text field is redacted by the agent service (saveRedactor) before it gets
// here, the way notes are.
//
// A step may name a role (and OneHost) for group runs (§17,
// service/grouprun.go); the run's mode (rolling or parallel) belongs to the
// run, not the runbook. Captured variables (the next phase, board
// CaptureVars) will be new optional fields; steps and runbooks have stable
// ids, so per-host results are keyed by (run, step, host).

const (
	// MaxRunbooks bounds the runbooks kept, MaxRunbookSteps the steps of
	// one runbook.
	MaxRunbooks     = 100
	MaxRunbookSteps = 30
	// Field limits, in bytes. Every field but a command is one line.
	MaxRunbookNameBytes    = 120
	MaxRunbookWhenBytes    = 300
	MaxRunbookTitleBytes   = 200
	MaxRunbookTextBytes    = 1000
	MaxRunbookExpectBytes  = 300
	MaxRunbookCommandBytes = 2048
	MaxRunbookCommandLines = 20
	MaxRunbookHosts        = 12
	MaxRunbookHostBytes    = 64

	// Where a runbook came from. Anything but RunbookSourceUser starts with
	// "Agent may pick" off, and the page asks for a review before it is
	// turned on.
	RunbookSourceUser         = "user"
	RunbookSourceAgent        = "agent"
	RunbookSourceNotebook     = "notebook"
	RunbookSourceConversation = "conversation"
)

var (
	ErrRunbook = errors.New("invalid runbook")
	// ErrRunbooksFull is MaxRunbooks reached.
	ErrRunbooksFull = errors.New("too many runbooks")
)

// Runbook is one saved runbook.
type Runbook struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// When is the "Use when" line: what the agent compares with a question
	// to decide whether the runbook fits.
	When string `json:"when,omitempty"`
	// Hosts are folder (group) names or tags. The agent may pick the
	// runbook only when a pane read for the question is on a host in one
	// of those folders (or below) or with one of those tags; empty means
	// any host. The user may start any runbook.
	Hosts []string `json:"hosts,omitempty"`
	// AgentMayPick lets the agent choose the runbook for a question. Off,
	// only the user can start it.
	AgentMayPick bool `json:"agentMayPick"`
	// Source is one of the RunbookSource values; SourceRef names the
	// notebook or conversation it was made from, when there was one.
	Source    string `json:"source"`
	SourceRef string `json:"sourceRef,omitempty"`
	// FromPane is set on a runbook the agent proposed in an answer that
	// read a pane: its steps may hold text from terminal output.
	FromPane   bool          `json:"fromPane,omitempty"`
	Steps      []RunbookStep `json:"steps"`
	CreatedAt  time.Time     `json:"createdAt"`
	UpdatedAt  time.Time     `json:"updatedAt"`
	LastUsedAt *time.Time    `json:"lastUsedAt,omitempty"`
	// LastUsedOn names the host of the pane the last use proposed a step
	// for.
	LastUsedOn string `json:"lastUsedOn,omitempty"`
}

// RunbookStep is one step. A step with no command is a check the user
// marks done.
type RunbookStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command,omitempty"`
	// Expect is the optional expected result ("exit 0", "state streaming").
	Expect string `json:"expect,omitempty"`
	// Role and OneHost say where the step runs in a group run (§17): on
	// every host in the group with that role, or on every host in the
	// group when Role is empty; with OneHost, on one of them only (the
	// first with a pane open), as for a kubectl check on one control-plane
	// node. A single-host run ignores both.
	Role    string `json:"role,omitempty"`
	OneHost bool   `json:"oneHost,omitempty"`
	// Captures are values Keel takes from this step's output on each host
	// of a group run (§17, part 2, board CaptureVars): a pattern applied by
	// keel serve to the output the shell's OSC 133 marks delimit, kept in
	// memory for that run, and filled in where a later step says {{name}}.
	Captures []StepCapture `json:"captures,omitempty"`
}

// StepCapture is one captured variable: its name and an RE2 pattern with a
// group around the value (the group named like the variable, else the
// first).
type StepCapture struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// cleanCaptures checks a step's captures; seen holds the names earlier
// steps capture (a name is captured by one step only).
func cleanCaptures(n int, in []StepCapture, seen map[string]bool) ([]StepCapture, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > runvars.MaxCapturesPerStep {
		return nil, fmt.Errorf("%w: step %d: at most %d captures", ErrRunbook, n, runvars.MaxCapturesPerStep)
	}
	out := make([]StepCapture, 0, len(in))
	for _, c := range in {
		c.Name = strings.ToLower(strings.TrimSpace(c.Name))
		c.Pattern = strings.TrimSpace(c.Pattern)
		if c.Name == "" && c.Pattern == "" {
			continue
		}
		if !runvars.ValidName(c.Name) {
			return nil, fmt.Errorf("%w: step %d: a capture's name is lower-case letters, digits and _, up to %d, not starting with a digit (such as join_token)", ErrRunbook, n, runvars.MaxNameBytes)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("%w: step %d: %s is captured by another step already", ErrRunbook, n, c.Name)
		}
		if _, err := runvars.CompilePattern(c.Pattern); err != nil {
			return nil, fmt.Errorf("%w: step %d: %s: %s", ErrRunbook, n, c.Name, err.Error())
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validRunbookSource(s string) bool {
	switch s {
	case RunbookSourceUser, RunbookSourceAgent, RunbookSourceNotebook, RunbookSourceConversation:
		return true
	}
	return false
}

// CleanLine makes t one line with no control, separator or format
// characters and single spaces, and reports whether it is at most max
// bytes (empty is fine; callers that need text check that).
func CleanLine(t string, max int) (string, bool) {
	if !utf8.ValidString(t) {
		return "", false
	}
	t = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, t)
	t = strings.Join(strings.Fields(t), " ")
	return t, len(t) <= max
}

// CleanRunbookCommand trims a step's command and checks it: at most
// MaxRunbookCommandBytes and MaxRunbookCommandLines, no control characters
// but tab and newline, no format characters (an invisible character could
// make the command differ from what the user read). Empty is fine (a step
// with no command).
func CleanRunbookCommand(s string) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	s = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	if len(s) > MaxRunbookCommandBytes || strings.Count(s, "\n")+1 > MaxRunbookCommandLines {
		return "", false
	}
	for _, c := range s {
		if c == '\t' || c == '\n' {
			continue
		}
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) || unicode.Is(unicode.Cf, c) || unicode.Is(unicode.Zl, c) || unicode.Is(unicode.Zp, c) {
			return "", false
		}
	}
	return s, true
}

// cleanRunbook checks and cleans the fields a user (or a proposal) sets.
// Step ids are kept when they are unique and well formed, else made.
func cleanRunbook(b Runbook) (Runbook, error) {
	bad := func(msg string) (Runbook, error) { return Runbook{}, fmt.Errorf("%w: %s", ErrRunbook, msg) }
	var ok bool
	if b.Name, ok = CleanLine(b.Name, MaxRunbookNameBytes); !ok || b.Name == "" {
		return bad(fmt.Sprintf("a name is 1 to %d bytes", MaxRunbookNameBytes))
	}
	if b.When, ok = CleanLine(b.When, MaxRunbookWhenBytes); !ok {
		return bad(fmt.Sprintf("Use when is at most %d bytes", MaxRunbookWhenBytes))
	}
	var hosts []string
	seen := map[string]bool{}
	for _, h := range b.Hosts {
		h, ok := CleanLine(h, MaxRunbookHostBytes)
		if !ok {
			return bad(fmt.Sprintf("a folder or tag is at most %d bytes", MaxRunbookHostBytes))
		}
		if h == "" || seen[strings.ToLower(h)] {
			continue
		}
		seen[strings.ToLower(h)] = true
		hosts = append(hosts, h)
	}
	if len(hosts) > MaxRunbookHosts {
		return bad(fmt.Sprintf("at most %d folders or tags", MaxRunbookHosts))
	}
	b.Hosts = hosts
	if len(b.Steps) == 0 {
		return bad("a runbook needs at least one step")
	}
	if len(b.Steps) > MaxRunbookSteps {
		return bad(fmt.Sprintf("at most %d steps", MaxRunbookSteps))
	}
	if b.SourceRef, ok = CleanLine(b.SourceRef, MaxRunbookNameBytes); !ok {
		b.SourceRef = ""
	}
	steps := make([]RunbookStep, 0, len(b.Steps))
	ids := map[string]bool{}
	captured := map[string]bool{}
	var err error
	for i, st := range b.Steps {
		n := i + 1
		if st.Title, ok = CleanLine(st.Title, MaxRunbookTitleBytes); !ok || st.Title == "" {
			return bad(fmt.Sprintf("step %d needs a title of 1 to %d bytes", n, MaxRunbookTitleBytes))
		}
		if st.Description, ok = CleanLine(st.Description, MaxRunbookTextBytes); !ok {
			return bad(fmt.Sprintf("step %d: the description is at most %d bytes", n, MaxRunbookTextBytes))
		}
		if st.Expect, ok = CleanLine(st.Expect, MaxRunbookExpectBytes); !ok {
			return bad(fmt.Sprintf("step %d: the expected result is at most %d bytes", n, MaxRunbookExpectBytes))
		}
		if st.Role, ok = CleanRole(st.Role); !ok {
			return bad(fmt.Sprintf("step %d: a role is one lower-case word of up to %d letters, digits, \"-\", \"_\" or \".\" (such as worker)", n, MaxRoleBytes))
		}
		if st.Command, ok = CleanRunbookCommand(st.Command); !ok {
			return bad(fmt.Sprintf("step %d: a command is at most %d bytes and %d lines, with no control characters", n, MaxRunbookCommandBytes, MaxRunbookCommandLines))
		}
		if st.Captures, err = cleanCaptures(n, st.Captures, captured); err != nil {
			return Runbook{}, err
		}
		if !validStepID(st.ID) || ids[st.ID] {
			st.ID = "st_" + newID()
		}
		ids[st.ID] = true
		steps = append(steps, st)
	}
	b.Steps = steps
	return b, nil
}

func validStepID(id string) bool {
	if len(id) < 4 || len(id) > 40 || !strings.HasPrefix(id, "st_") {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func copyRunbook(b Runbook) Runbook {
	b.Hosts = append([]string(nil), b.Hosts...)
	b.Steps = append([]RunbookStep(nil), b.Steps...)
	for i := range b.Steps {
		b.Steps[i].Captures = append([]StepCapture(nil), b.Steps[i].Captures...)
		if len(b.Steps[i].Captures) == 0 {
			b.Steps[i].Captures = nil
		}
	}
	if b.LastUsedAt != nil {
		t := *b.LastUsedAt
		b.LastUsedAt = &t
	}
	return b
}

// ListRunbooks returns every runbook, by name.
func (s *Store) ListRunbooks() ([]Runbook, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	if _, err := s.memoryLocked(); err != nil {
		return nil, err
	}
	out := make([]Runbook, 0, len(s.mem.books))
	for _, b := range s.mem.books {
		out = append(out, copyRunbook(b))
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// GetRunbook returns one runbook.
func (s *Store) GetRunbook(id string) (Runbook, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	if _, err := s.memoryLocked(); err != nil {
		return Runbook{}, err
	}
	for _, b := range s.mem.books {
		if b.ID == id {
			return copyRunbook(b), nil
		}
	}
	return Runbook{}, ErrNotFound
}

// AddRunbook saves a new runbook. A runbook that is not the user's own
// starts with AgentMayPick off: the user reviews it and turns it on.
func (s *Store) AddRunbook(b Runbook) (Runbook, error) {
	if !validRunbookSource(b.Source) {
		b.Source = RunbookSourceUser
	}
	if b.Source != RunbookSourceAgent {
		b.FromPane = false
	}
	if b.Source != RunbookSourceUser {
		b.AgentMayPick = false
	}
	b, err := cleanRunbook(b)
	if err != nil {
		return Runbook{}, err
	}
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return Runbook{}, err
	}
	if len(s.mem.books) >= MaxRunbooks {
		return Runbook{}, fmt.Errorf("%w: at most %d", ErrRunbooksFull, MaxRunbooks)
	}
	now := time.Now().UTC()
	b.ID, b.CreatedAt, b.UpdatedAt, b.LastUsedAt, b.LastUsedOn = "rb_"+newID(), now, now, nil, ""
	next := append(append([]Runbook(nil), s.mem.books...), b)
	if err := s.writeMemoryAllLocked(notes, next); err != nil {
		return Runbook{}, err
	}
	return copyRunbook(b), nil
}

// UpdateRunbook replaces what the user edits: the name, Use when, hosts,
// Agent may pick and the steps. Where it came from and when it was used
// stay.
func (s *Store) UpdateRunbook(b Runbook) (Runbook, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return Runbook{}, err
	}
	next := append([]Runbook(nil), s.mem.books...)
	for i, old := range next {
		if old.ID != b.ID {
			continue
		}
		in := old
		in.Name, in.When, in.Hosts, in.AgentMayPick, in.Steps = b.Name, b.When, b.Hosts, b.AgentMayPick, b.Steps
		// A runbook the user did not write is turned on only by the
		// separate review action (SetRunbookAgentMayPick), never by an
		// edit's save.
		if old.Source != RunbookSourceUser && !old.AgentMayPick {
			in.AgentMayPick = false
		}
		in, err := cleanRunbook(in)
		if err != nil {
			return Runbook{}, err
		}
		in.UpdatedAt = time.Now().UTC()
		next[i] = in
		if err := s.writeMemoryAllLocked(notes, next); err != nil {
			return Runbook{}, err
		}
		return copyRunbook(in), nil
	}
	return Runbook{}, ErrNotFound
}

// SetRunbookAgentMayPick turns "Agent may pick" on or off.
func (s *Store) SetRunbookAgentMayPick(id string, on bool) (Runbook, error) {
	return s.changeRunbook(id, func(b *Runbook) bool {
		if b.AgentMayPick == on {
			return false
		}
		b.AgentMayPick, b.UpdatedAt = on, time.Now().UTC()
		return true
	})
}

// MarkRunbookUsed records that a step of the runbook was proposed for a
// pane on host.
func (s *Store) MarkRunbookUsed(id, host string, at time.Time) error {
	host, _ = CleanLine(host, MaxRunbookNameBytes)
	_, err := s.changeRunbook(id, func(b *Runbook) bool {
		t := at.UTC()
		b.LastUsedAt, b.LastUsedOn = &t, host
		return true
	})
	return err
}

func (s *Store) changeRunbook(id string, fn func(*Runbook) bool) (Runbook, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return Runbook{}, err
	}
	next := append([]Runbook(nil), s.mem.books...)
	for i := range next {
		if next[i].ID != id {
			continue
		}
		b := copyRunbook(next[i])
		if !fn(&b) {
			return b, nil
		}
		next[i] = b
		if err := s.writeMemoryAllLocked(notes, next); err != nil {
			return Runbook{}, err
		}
		return copyRunbook(b), nil
	}
	return Runbook{}, ErrNotFound
}

// DeleteRunbook removes one runbook and returns it.
func (s *Store) DeleteRunbook(id string) (Runbook, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	notes, err := s.memoryLocked()
	if err != nil {
		return Runbook{}, err
	}
	next := make([]Runbook, 0, len(s.mem.books))
	var gone *Runbook
	for _, b := range s.mem.books {
		if b.ID == id {
			b := b
			gone = &b
			continue
		}
		next = append(next, b)
	}
	if gone == nil {
		return Runbook{}, ErrNotFound
	}
	return copyRunbook(*gone), s.writeMemoryAllLocked(notes, next)
}
