package service

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/runvars"
	"keel/internal/shellmark"
	"keel/internal/store"
)

// Group runs (docs/ui-redesign.md §17, board GroupRunbook): one runbook
// walked across the hosts of a group, step by step, Ansible-style, except
// that nothing is ever run by Keel or the agent.
//
//   - The user starts a run (StartGroupRun) with a runbook, a group and a
//     mode: rolling (one host at a time, in order; the first failure stops
//     the step before the next host) or parallel (every host of the step
//     at once; a failure does not stop the others). There is no model call:
//     the run's steps are the saved runbook's, and the run sends nothing to
//     a provider.
//   - A step targets a role ("worker"), or every host in the group, and
//     maybe one host only (store.RunbookStep.Role, OneHost). Its hosts are
//     fixed when the run starts: the group's members (its folder and the
//     folders below) with that role and "Agent can read" on. A member the
//     agent may not read is listed as left out and never targeted.
//   - Keel turns the current step into one ordinary command proposal per
//     host (ProposeGroupStep), from the saved command, each aimed at a pane
//     open on that host in the asking window (the client sends its panes;
//     keel serve resolves their hosts itself). Every host × step is its own
//     proposal with its own id, typed only through approve-before-type with
//     a server nonce (AuthorizeType with GroupRunID), which checks again
//     that the host is still in the group and readable, the bytes, the
//     driver and the ⇧ rule with the host's production flag now.
//   - Each host's result is its shell's OSC 133 exit status for the
//     commands after the stream offset taken right before the write
//     (refreshGroupRun), labelled reported; on a production host a reported
//     exit 0 waits for the user's Confirm. Without marks the user marks the
//     host done, skips or retries it (GroupHostAction).
//   - Runs live in keel serve's memory only (at most maxGroupRuns); a
//     restart ends them. Every start, proposal, result and action is logged
//     as agent.runbook with ids and counts, never a name or a command.

const (
	maxGroupRuns     = 8
	maxGroupRunHosts = 64

	ModeRolling  = "rolling"
	ModeParallel = "parallel"

	cellWaiting  = "waiting"
	cellProposed = "proposed"
	cellTyped    = "typed"
	cellDone     = "done"
	cellFailed   = "failed"
	cellSkipped  = "skipped"
	// cellTyping is a host whose proposal keel serve authorized and is
	// writing now: between AuthorizeType and the write, nothing may reset
	// it (a Retry there would let the command run twice).
	cellTyping = "typing"

	// pendingResultFor is how long Mark done waits for a shell that sends
	// OSC 133 marks to report a typed host's exit status.
	pendingResultFor = 10 * time.Minute
)

var (
	ErrNoGroupRun = errors.New("agent: no such group run, or it has ended")
	// ErrNotTarget refuses a group run proposal whose host left the group
	// or no longer lets the agent read it.
	ErrNotTarget = errors.New("agent: this host is no longer a target of the run: it left the group or Agent can read is off")
	// ErrNotProposed refuses typing a group run proposal twice, or one the
	// user retried or skipped.
	ErrNotProposed = errors.New("agent: this host's step is not waiting to be typed; propose it again")
	// ErrModeLocked refuses a mode change while a rolling step is stopped
	// at a failure or while a host's proposal is live: switching then would
	// lift the rolling stop or leave later hosts typeable.
	ErrModeLocked = errors.New("agent: the mode can change only when no host of the current step is proposed, being typed or waiting for its result, and no failure stopped the step; retry or skip the host first")
	// ErrTyping refuses Retry or Skip on a host whose step is being typed.
	ErrTyping = errors.New("agent: this host's step is being typed right now; wait for it, then retry or skip")
	// ErrResultPending refuses Mark done while the shell may still report
	// the exit status.
	ErrResultPending = errors.New("agent: this host's shell reports exit codes; wait for it, or retry or skip the host")
	// ErrTooManyRuns refuses a new run while maxGroupRuns are active.
	ErrTooManyRuns = errors.New("agent: 8 group runs are active; stop one before you start another")
)

// GroupMemberSource lists a group's hosts with their roles. *store.Store
// satisfies it.
type GroupMemberSource interface {
	GroupMembers(groupID string) (store.Group, []store.GroupMember, error)
}

// GroupRunRequest is POST /api/agent/groupruns.
type GroupRunRequest struct {
	RunbookID string `json:"runbookId"`
	GroupID   string `json:"groupId"`
	Mode      string `json:"mode"`
}

// GroupRun is one run, as the run page shows it.
type GroupRun struct {
	ID        string           `json:"id"`
	RunbookID string           `json:"runbookId"`
	Name      string           `json:"name"`
	GroupID   string           `json:"groupId"`
	Group     string           `json:"group"`
	Mode      string           `json:"mode"`
	Status    string           `json:"status"` // active | finished | stopped
	StartedAt time.Time        `json:"startedAt"`
	Members   []GroupRunMember `json:"members"`
	Steps     []GroupRunStep   `json:"steps"`
	// Current is the index of the step being walked, -1 when none is.
	Current int `json:"current"`
	// Halted names the host whose failure stopped a rolling step.
	Halted string `json:"halted,omitempty"`
	// ModeLocked is set while the mode may not change (modeLockedLocked).
	ModeLocked bool `json:"modeLocked,omitempty"`
	// NeedPane are the hosts of the current step Keel would propose for now
	// but found no pane open on in the asking window.
	NeedPane []GroupRunHost `json:"needPane"`
}

// GroupRunMember is a host of the group. Excluded says why a member is
// never targeted ("Agent can read is off").
type GroupRunMember struct {
	HostID     string `json:"hostId"`
	Host       string `json:"host"`
	Role       string `json:"role,omitempty"`
	Production bool   `json:"production"`
	Excluded   string `json:"excluded,omitempty"`
}

// GroupRunHost names a host.
type GroupRunHost struct {
	HostID string `json:"hostId"`
	Host   string `json:"host"`
}

// GroupRunStep is a step of the run with its hosts.
type GroupRunStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command,omitempty"`
	Expect      string `json:"expect,omitempty"`
	Risk        string `json:"risk,omitempty"`
	Role        string `json:"role,omitempty"`
	OneHost     bool   `json:"oneHost,omitempty"`
	// Skipped is the whole step skipped by the user; Done a step with no
	// command (or no host) the user checked.
	Skipped bool           `json:"skipped,omitempty"`
	Done    bool           `json:"done,omitempty"`
	Cells   []GroupRunCell `json:"cells"`
	// Candidates are the hosts a OneHost step may go to, in order; the
	// first with a pane open is picked when it is proposed.
	Candidates []GroupRunHost `json:"candidates,omitempty"`
	// Captures are the values this step takes from its output on each
	// host (groupvars.go); Vars the variables its command names, filled in
	// only at Type time. Names and patterns only, never a value.
	Captures []store.StepCapture `json:"captures,omitempty"`
	Vars     []runvars.Ref       `json:"vars,omitempty"`
}

// GroupRunCell is one host × step.
type GroupRunCell struct {
	HostID    string    `json:"hostId"`
	Host      string    `json:"host"`
	State     string    `json:"state"`
	SessionID string    `json:"sessionId,omitempty"`
	Proposal  *Proposal `json:"proposal,omitempty"`
	Exit      *int      `json:"exit,omitempty"`
	// Result is "reported" (the shell's marks), "user", "unreported" (a
	// command ended with no status) or "abandoned" (the shell went away).
	Result string `json:"result,omitempty"`
	// Confirm is set on a production host whose reported exit 0 waits for
	// the user.
	Confirm bool `json:"confirm,omitempty"`
	// Blocked says why Keel did not propose for this host now.
	Blocked string `json:"blocked,omitempty"`
	// Pending is set on a typed host whose shell sends OSC 133 marks and
	// has not reported this command's exit status yet (for up to
	// pendingResultFor): Mark done waits.
	Pending    bool       `json:"pending,omitempty"`
	ProposedAt *time.Time `json:"proposedAt,omitempty"`
	TypedAt    *time.Time `json:"typedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// Captures are this host's captures for the step: their state and
	// length, never the value (groupvars.go).
	Captures []CaptureView `json:"captures,omitempty"`
	mark     uint64
	hasMark  bool
	// filled is how many variables the authorized bytes filled in, for
	// the log line once they are typed.
	filled int
}

// groupRun is a run in memory (under s.mu).
type groupRun struct {
	GroupRun
	used time.Time
	// captures are the values captured in this run, by host id then name
	// (groupvars.go). Dropped when the run ends, on a retry of the step
	// that captured them, and after captureTTL.
	captures map[string]map[string]*capturedValue
}

func (r *groupRun) view() GroupRun {
	locked := r.Status == runActive && r.modeLockedLocked()
	out := r.GroupRun
	out.ModeLocked = locked
	out.Members = append([]GroupRunMember(nil), r.Members...)
	out.NeedPane = append([]GroupRunHost{}, r.NeedPane...)
	out.Steps = make([]GroupRunStep, len(r.Steps))
	for i, st := range r.Steps {
		st.Candidates = append([]GroupRunHost(nil), st.Candidates...)
		st.Captures = append([]store.StepCapture(nil), st.Captures...)
		st.Vars = append([]runvars.Ref(nil), st.Vars...)
		cells := make([]GroupRunCell, len(st.Cells))
		for j, c := range st.Cells {
			c.Captures = append([]CaptureView(nil), c.Captures...)
			if c.Proposal != nil {
				p := *c.Proposal
				c.Proposal = &p
			}
			if c.Exit != nil {
				e := *c.Exit
				c.Exit = &e
			}
			cells[j] = c
		}
		st.Cells = cells
		out.Steps[i] = st
	}
	return out
}

func validMode(m string) bool { return m == ModeRolling || m == ModeParallel }

// stepComplete reports whether a step is behind the run.
func stepComplete(st *GroupRunStep) bool {
	if st.Skipped || st.Done {
		return true
	}
	if len(st.Cells) == 0 {
		return false
	}
	for _, c := range st.Cells {
		if c.State != cellDone && c.State != cellSkipped {
			return false
		}
	}
	return true
}

// advanceLocked sets Current and Halted, and finishes the run when every
// step is complete.
func (r *groupRun) advanceLocked() {
	r.Current, r.Halted = -1, ""
	for i := range r.Steps {
		if !stepComplete(&r.Steps[i]) {
			r.Current = i
			break
		}
	}
	if r.Current < 0 {
		if r.Status == runActive {
			r.Status = runFinished
		}
		r.NeedPane = nil
		// Captured values live only as long as the run.
		r.wipeCapturesLocked()
		return
	}
	if r.Mode == ModeRolling {
		for _, c := range r.Steps[r.Current].Cells {
			if c.State == cellDone || c.State == cellSkipped {
				continue
			}
			if c.State == cellFailed {
				r.Halted = c.Host
			}
			break
		}
	}
}

// groupMembers reads a group's members and the saved hosts' agent switches.
func (s *Service) groupMembers(groupID string) (store.Group, []store.GroupMember, map[string]agent.Host, error) {
	src, ok := s.st.(GroupMemberSource)
	if !ok {
		return store.Group{}, nil, nil, ErrNotFound
	}
	g, members, err := src.GroupMembers(groupID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Group{}, nil, nil, ErrNotFound
		}
		return store.Group{}, nil, nil, err
	}
	hosts, err := s.st.ListHosts()
	if err != nil {
		return store.Group{}, nil, nil, err
	}
	byID := map[string]agent.Host{}
	for _, h := range agent.HostsFrom(hosts) {
		byID[h.ID] = h
	}
	return g, members, byID, nil
}

// StartGroupRun starts a runbook on a group. The run's hosts are fixed
// now: per step, the members with the step's role (every member when it
// names none) that the agent may read.
func (s *Service) StartGroupRun(req GroupRunRequest) (GroupRun, error) {
	if s.books == nil {
		return GroupRun{}, ErrNoRunbooks
	}
	if req.Mode == "" {
		req.Mode = ModeRolling
	}
	if !validMode(req.Mode) {
		return GroupRun{}, badRequest("mode is rolling or parallel")
	}
	b, err := s.books.GetRunbook(req.RunbookID)
	if err != nil {
		return GroupRun{}, runbookErr(err)
	}
	g, members, byID, err := s.groupMembers(req.GroupID)
	if err != nil {
		return GroupRun{}, err
	}
	now := s.now()
	run := &groupRun{GroupRun: GroupRun{ID: newID("gr_"), RunbookID: b.ID, Name: b.Name, GroupID: g.ID, Group: g.Name, Mode: req.Mode, Status: runActive, StartedAt: now}, used: now}
	targets := 0
	for _, m := range members {
		h, ok := byID[m.HostID]
		gm := GroupRunMember{HostID: m.HostID, Host: m.Name, Role: m.Role, Production: !ok || h.Production}
		if !ok || !h.AgentReadable {
			gm.Excluded = "Agent can read is off"
		} else {
			targets++
		}
		run.Members = append(run.Members, gm)
	}
	if targets > maxGroupRunHosts {
		return GroupRun{}, badRequest(fmt.Sprintf("a group run goes to at most %d hosts", maxGroupRunHosts))
	}
	for _, st := range b.Steps {
		rs := GroupRunStep{ID: st.ID, Title: st.Title, Description: st.Description, Command: st.Command, Expect: st.Expect, Role: st.Role, OneHost: st.OneHost, Cells: []GroupRunCell{}, Vars: varRefs(st.Command)}
		if st.Command != "" {
			rs.Captures = append([]store.StepCapture(nil), st.Captures...)
		}
		if st.Command != "" {
			rs.Risk = string(agent.Classify(st.Command))
			var hosts []GroupRunHost
			for _, m := range run.Members {
				if m.Excluded == "" && (st.Role == "" || m.Role == st.Role) {
					hosts = append(hosts, GroupRunHost{HostID: m.HostID, Host: m.Host})
				}
			}
			switch {
			case st.OneHost && len(hosts) > 0:
				rs.Candidates = hosts
				rs.Cells = append(rs.Cells, GroupRunCell{State: cellWaiting})
			default:
				for _, h := range hosts {
					rs.Cells = append(rs.Cells, GroupRunCell{HostID: h.HostID, Host: h.Host, State: cellWaiting})
				}
			}
		}
		run.Steps = append(run.Steps, rs)
	}
	run.advanceLocked()
	s.mu.Lock()
	if s.groupRuns == nil {
		s.groupRuns = map[string]*groupRun{}
	}
	for len(s.groupRuns) >= maxGroupRuns {
		if !s.evictGroupRunLocked() {
			s.mu.Unlock()
			return GroupRun{}, ErrTooManyRuns
		}
	}
	s.groupRuns[run.ID] = run
	out := run.view()
	s.mu.Unlock()
	s.audit.RunbookEvent(fmt.Sprintf("group run %s started by you on group %s (%d steps, %d hosts, %d left out: Agent can read is off, %s)", run.ID, g.ID, len(run.Steps), targets, len(run.Members)-targets, run.Mode), b.ID, "", redact.Report{})
	return out, nil
}

// evictGroupRunLocked drops the least recently used ended run. An active
// run is never dropped: false when every run is active.
func (s *Service) evictGroupRunLocked() bool {
	ids := make([]string, 0, len(s.groupRuns))
	for k, r := range s.groupRuns {
		if r.Status != runActive {
			ids = append(ids, k)
		}
	}
	if len(ids) == 0 {
		return false
	}
	sort.Slice(ids, func(i, j int) bool { return s.groupRuns[ids[i]].used.Before(s.groupRuns[ids[j]].used) })
	delete(s.groupRuns, ids[0])
	return true
}

// GroupRunState returns a run, after looking for typed hosts' results in
// their panes' shell marks.
func (s *Service) GroupRunState(id string) (GroupRun, error) {
	s.refreshGroupRun(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.groupRuns[id]
	if run == nil {
		return GroupRun{}, ErrNoGroupRun
	}
	run.pruneCapturesLocked(s.now())
	return run.view(), nil
}

// ProposeGroupStep makes the current step's proposals that are due:
// rolling, for the first host not done or skipped (none while it failed or
// waits); parallel, for every host waiting. Each goes to a pane on that
// host among panes (session ids open in the asking window, resolved to
// their hosts by keel serve, never by the client): the pane used for the
// host before, else the first. A host with no pane is listed in NeedPane.
// It is safe to call again and again: it never makes a second proposal for
// a host that has a live one.
func (s *Service) ProposeGroupStep(id string, panes []string) (GroupRun, error) {
	s.refreshGroupRun(id)
	s.mu.Lock()
	run := s.groupRuns[id]
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return GroupRun{}, ErrNoGroupRun
	}
	groupID := run.GroupID
	s.mu.Unlock()
	// Where each host has a pane, from keel serve's own sessions.
	onHost := map[string][]agent.Pane{}
	for _, sid := range uniq(panes) {
		if p, ok := s.panes.Pane(sid); ok && p.HostID != "" {
			onHost[p.HostID] = append(onHost[p.HostID], p)
		}
	}
	// Membership and readability now, not as they were at the start.
	_, members, byID, err := s.groupMembers(groupID)
	if err != nil {
		return GroupRun{}, err
	}
	inGroup := map[string]bool{}
	for _, m := range members {
		inGroup[m.HostID] = true
	}
	now := s.now()
	type made struct {
		pane agent.Pane
		risk agent.Risk
	}
	var proposed []made
	s.mu.Lock()
	run = s.groupRuns[id]
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return GroupRun{}, ErrNoGroupRun
	}
	run.used = now
	run.pruneCapturesLocked(now)
	run.advanceLocked()
	run.NeedPane = nil
	stepNo, of := run.Current+1, len(run.Steps)
	if run.Current >= 0 {
		st := &run.Steps[run.Current]
		var due []int
		for j := range st.Cells {
			c := &st.Cells[j]
			c.Blocked = ""
			if c.State == cellDone || c.State == cellSkipped {
				continue
			}
			if c.State == cellWaiting {
				due = append(due, j)
			}
			if run.Mode == ModeRolling {
				break
			}
		}
		lastPane := func(hostID string) string {
			for k := run.Current - 1; k >= 0; k-- {
				for _, c := range run.Steps[k].Cells {
					if c.HostID == hostID && c.SessionID != "" {
						return c.SessionID
					}
				}
			}
			return ""
		}
		pick := func(hostID string) (agent.Pane, bool) {
			ps := onHost[hostID]
			if len(ps) == 0 {
				return agent.Pane{}, false
			}
			if prev := lastPane(hostID); prev != "" {
				for _, p := range ps {
					if p.SessionID == prev {
						return p, true
					}
				}
			}
			return ps[0], true
		}
		ok := func(hostID string) string {
			h, found := byID[hostID]
			switch {
			case !found || !inGroup[hostID]:
				return "This host left the group. Skip it to go on."
			case !h.AgentReadable:
				return "Agent can read is off for this host now. Skip it, or turn it on."
			}
			return ""
		}
		for _, j := range due {
			c := &st.Cells[j]
			if st.OneHost && c.HostID == "" {
				// The first candidate with a pane open, still a target.
				for _, h := range st.Candidates {
					if ok(h.HostID) != "" {
						continue
					}
					if _, has := pick(h.HostID); has {
						c.HostID, c.Host = h.HostID, h.Host
						break
					}
				}
				if c.HostID == "" {
					for _, h := range st.Candidates {
						if ok(h.HostID) == "" {
							run.NeedPane = append(run.NeedPane, h)
						}
					}
					c.Blocked = "No pane is open on a host this step may go to."
					continue
				}
			}
			if why := ok(c.HostID); why != "" {
				c.Blocked = why
				continue
			}
			pane, has := pick(c.HostID)
			if !has {
				run.NeedPane = append(run.NeedPane, GroupRunHost{HostID: c.HostID, Host: c.Host})
				c.Blocked = "No pane is open on this host."
				continue
			}
			h := byID[c.HostID]
			p := &Proposal{
				ID: newID("p_"), Command: st.Command, Bytes: typedBytes(st.Command), Why: st.Title,
				Risk: rate(st.Command, ""), ProposedAt: &now,
				SessionID: pane.SessionID, HostID: c.HostID, Host: h.Name, Production: h.Production,
				Runbook: &ProposalStep{RunbookID: run.RunbookID, Name: run.Name, Step: stepNo, Of: of, StepID: st.ID},
			}
			p.NeedsShift = agent.NeedsShift(p.Risk, p.Production)
			p.Placeholders = hasPlaceholder(p.Command)
			// The proposal keeps {{name}}: values go in only at Type
			// (fillProposal), never into a stored proposal.
			p.Vars = varRefs(p.Command)
			c.State, c.SessionID, c.Proposal, c.ProposedAt = cellProposed, pane.SessionID, p, &now
			c.Exit, c.Result, c.Confirm, c.TypedAt, c.FinishedAt, c.hasMark = nil, "", false, nil, nil, false
			proposed = append(proposed, made{pane: agent.Pane{SessionID: pane.SessionID, HostID: c.HostID, HostName: h.Name}, risk: p.Risk})
		}
	}
	out := run.view()
	runID, rbID, mode := run.ID, run.RunbookID, run.Mode
	s.mu.Unlock()
	for _, m := range proposed {
		s.audit.Suggest(m.pane, redact.Report{}, riskLabel(m.risk))
	}
	if len(proposed) > 0 {
		s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d proposed for %s (%s)", runID, stepNo, of, noteCountWord(len(proposed), "host", "hosts"), mode), rbID, "", redact.Report{})
	}
	return out, nil
}

func noteCountWord(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// groupProposalLocked finds a run's proposal and its cell. Call with s.mu
// held.
func (s *Service) groupProposalLocked(runID, proposalID string) (*groupRun, *GroupRunCell) {
	run := s.groupRuns[runID]
	if run == nil {
		return nil, nil
	}
	for i := range run.Steps {
		for j := range run.Steps[i].Cells {
			c := &run.Steps[i].Cells[j]
			if c.Proposal != nil && c.Proposal.ID == proposalID {
				return run, c
			}
		}
	}
	return run, nil
}

// authorizeGroupType is AuthorizeType for a group run's proposal: it must
// be live and waiting (proposed), for this session, with the shown bytes
// and no placeholder; its host must still be in the group and readable;
// the ⇧ rule takes the host's production flag now; the client must drive.
func (s *Service) authorizeGroupType(req TypeRequest) error {
	s.mu.Lock()
	run, c := s.groupProposalLocked(req.GroupRunID, req.ProposalID)
	var (
		snap    Proposal
		groupID string
		state   string
	)
	if run != nil && c != nil && run.Status == runActive {
		snap, groupID, state = *c.Proposal, run.GroupID, c.State
	}
	s.mu.Unlock()
	if snap.ID == "" {
		return ErrNotFound
	}
	if state != cellProposed {
		return ErrNotProposed
	}
	if snap.SessionID == "" {
		return ErrNoTarget
	}
	if snap.SessionID != req.SessionID {
		return ErrWrongPane
	}
	// The exact bytes: for a step that names variables, the command with
	// the values filled in now (fillProposal), which is what the approve
	// dialog showed; the stored proposal keeps {{name}}. A missing or
	// unsafe value, a vault value or a placeholder refuses it.
	want, risk, filled := snap.Bytes, snap.Risk, 0
	if runvars.HasRefs(snap.Command) {
		f, err := s.fillProposal(req.GroupRunID, req.ProposalID)
		if err != nil {
			return err
		}
		want, risk, filled = f.Bytes, f.Risk, len(f.Used)
	} else if hasPlaceholder(snap.Command) {
		return ErrPlaceholder
	}
	if req.Bytes != want {
		return ErrChanged
	}
	if hasPlaceholder(want) {
		return ErrPlaceholder
	}
	_, members, byID, err := s.groupMembers(groupID)
	if err != nil {
		return ErrNotTarget
	}
	member := false
	for _, m := range members {
		member = member || m.HostID == snap.HostID
	}
	h, found := byID[snap.HostID]
	if !member || !found || !h.AgentReadable {
		return ErrNotTarget
	}
	if agent.NeedsShift(risk, snap.Production || h.Production) && !req.Shift {
		return ErrNeedsShift
	}
	if err := agent.CanInsert(req.Role); err != nil {
		return err
	}
	// Claim the host under the lock: it is typing until the write is done
	// (GroupTypeDoneAt) or failed (GroupTypeFailed), and Retry, Skip and a
	// mode change leave it alone meanwhile.
	s.mu.Lock()
	run, c = s.groupProposalLocked(req.GroupRunID, req.ProposalID)
	if run == nil || c == nil || run.Status != runActive || c.State != cellProposed {
		s.mu.Unlock()
		return ErrNotProposed
	}
	c.State, c.filled = cellTyping, filled
	s.mu.Unlock()
	// agent.type logs the byte and line counts, never the bytes.
	if err := s.audit.Type(agent.Pane{SessionID: snap.SessionID, HostID: snap.HostID, HostName: snap.Host, Role: req.Role}, want); err != nil {
		s.GroupTypeFailed(req.GroupRunID, req.ProposalID)
		return err
	}
	return nil
}

// GroupTypeFailed puts a host back to proposed when its authorized bytes
// were not written (the inject ticket was refused or expired).
func (s *Service) GroupTypeFailed(runID, proposalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, c := s.groupProposalLocked(runID, proposalID); c != nil && c.State == cellTyping {
		c.State = cellProposed
	}
}

// GroupTypeDoneAt records that a run's proposal was typed, with the pane's
// stream offset taken right before the write.
func (s *Service) GroupTypeDoneAt(runID, proposalID string, mark uint64) {
	now := s.now()
	s.mu.Lock()
	run, c := s.groupProposalLocked(runID, proposalID)
	if run == nil || c == nil || c.State != cellTyping {
		s.mu.Unlock()
		return
	}
	c.Proposal.TypedAt = &now
	c.State, c.TypedAt, c.mark, c.hasMark = cellTyped, &now, mark, true
	run.used = now
	filled, host, rb, stepNo := c.filled, c.HostID, run.RunbookID, 0
	for i := range run.Steps {
		for j := range run.Steps[i].Cells {
			if &run.Steps[i].Cells[j] == c {
				stepNo = i + 1
			}
		}
	}
	of := len(run.Steps)
	s.mu.Unlock()
	if filled > 0 {
		s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d · %s filled in at Type (values not logged)", runID, stepNo, of, noteCountWord(filled, "variable", "variables")), rb, host, redact.Report{})
	}
}

// cmdResult reads a step's result from the commands a shell reported after
// its mark: any non-zero exit fails it at once; it is done once want
// commands ended, each with exit 0. ok is false while it waits.
func cmdResult(cmds []shellmark.Command, want int) (exit int, noStatus, abandoned, ok bool) {
	ended := 0
	for _, cmd := range cmds {
		if !cmd.Done {
			break
		}
		ended++
		switch {
		case cmd.Abandoned || !cmd.HasExit:
			return 0, true, cmd.Abandoned, true
		case cmd.Exit != 0:
			return cmd.Exit, false, false, true
		}
		if ended >= want {
			return 0, false, false, true
		}
	}
	return 0, false, false, false
}

// refreshGroupRun records the exit status each typed host's shell reported
// (OSC 133), labelled reported. On a production host (now, or no longer
// found) a reported exit 0 waits for the user's Confirm.
func (s *Service) refreshGroupRun(id string) {
	src, ok := s.panes.(StepSource)
	if !ok {
		return
	}
	type pending struct {
		step, cell  int
		sessionID   string
		hostID      string
		mark        uint64
		want        int
		proposalID  string
		exit        int
		noStatus    bool
		abandoned   bool
		gotResult   bool
		production  bool
		hasMarks    bool
		stepNo, ofN int
		stepID      string
		host        string
		caps        []store.StepCapture
		oneHost     bool
		captured    []captureResult
	}
	var pend []pending
	s.mu.Lock()
	run := s.groupRuns[id]
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return
	}
	for i, st := range run.Steps {
		for j, c := range st.Cells {
			if c.State == cellTyped && c.Result == "" && c.hasMark && c.Proposal != nil {
				pend = append(pend, pending{step: i, cell: j, sessionID: c.SessionID, hostID: c.HostID, mark: c.mark, want: stepCommands(st.Command), proposalID: c.Proposal.ID, stepNo: i + 1, ofN: len(run.Steps), stepID: st.ID, host: c.Host, oneHost: st.OneHost, caps: append([]store.StepCapture(nil), st.Captures...)})
			}
		}
	}
	s.mu.Unlock()
	if len(pend) == 0 {
		return
	}
	for k := range pend {
		p := &pend[k]
		p.exit, p.noStatus, p.abandoned, p.gotResult = cmdResult(src.CommandsAfter(p.sessionID, p.mark), p.want)
		// A shell that has ever sent a mark is expected to report this
		// command too; one that never did leaves the result to the user.
		p.hasMarks = len(src.CommandsAfter(p.sessionID, 0)) > 0
		if p.gotResult {
			p.production, _ = s.hostProduction(p.hostID)
		}
		// Captures run on a reported exit 0 only, on that host's output
		// between the marks (groupvars.go).
		if p.gotResult && !p.noStatus && p.exit == 0 && len(p.caps) > 0 {
			p.captured = s.runCaptures(p.sessionID, p.mark, p.want, p.caps)
		}
	}
	now := s.now()
	var logs []func()
	s.mu.Lock()
	run = s.groupRuns[id]
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return
	}
	for _, p := range pend {
		if p.step >= len(run.Steps) || p.cell >= len(run.Steps[p.step].Cells) {
			continue
		}
		c := &run.Steps[p.step].Cells[p.cell]
		if c.State != cellTyped || c.Result != "" || c.Proposal == nil || c.Proposal.ID != p.proposalID {
			continue
		}
		c.Pending = false
		if !p.gotResult {
			c.Pending = p.hasMarks && c.TypedAt != nil && now.Sub(*c.TypedAt) < pendingResultFor
			continue
		}
		runID, rb, host, n, of := run.ID, run.RunbookID, p.hostID, p.stepNo, p.ofN
		if len(p.captured) > 0 {
			kept, other := 0, 0
			for _, cr := range p.captured {
				var exp *time.Time
				// Withheld from the model first, for good: a value the set
				// cannot take is not kept for filling either.
				if cr.state == CaptureCaptured && !s.withholdLocked(cr.name, cr.value) {
					cr.state, cr.value = CaptureRefused, ""
				}
				if cr.state == CaptureCaptured {
					if run.captures == nil {
						run.captures = map[string]map[string]*capturedValue{}
					}
					if run.captures[p.hostID] == nil {
						run.captures[p.hostID] = map[string]*capturedValue{}
					}
					run.captures[p.hostID][cr.name] = &capturedValue{value: cr.value, stepID: p.stepID, stepNo: p.stepNo, host: p.host, at: now, oneHost: p.oneHost}
					t := now.Add(captureTTL)
					exp = &t
					kept++
				} else {
					other++
				}
				run.setCaptureStateLocked(p.stepID, p.hostID, cr.name, cr.state, len(cr.value), exp)
			}
			logs = append(logs, func() {
				s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d · %s captured from the step's output, %d not (values not logged)", runID, n, of, noteCountWord(kept, "value", "values"), other), rb, host, redact.Report{})
			})
		}
		switch {
		case p.noStatus:
			c.Result = "unreported"
			if p.abandoned {
				c.Result = "abandoned"
			}
			logs = append(logs, func() {
				s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d ended with no exit status reported", runID, n, of), rb, host, redact.Report{})
			})
		case p.exit == 0 && p.production:
			exit := 0
			c.Exit, c.Result, c.Confirm = &exit, "reported", true
			logs = append(logs, func() {
				s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d · exit 0 reported by the shell · waiting for you to confirm (production host)", runID, n, of), rb, host, redact.Report{})
			})
		default:
			exit := p.exit
			c.Exit, c.Result, c.FinishedAt = &exit, "reported", &now
			c.State = cellDone
			if exit != 0 {
				c.State = cellFailed
			}
			logs = append(logs, func() {
				s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d · exit %d reported by the shell", runID, n, of, exit), rb, host, redact.Report{})
			})
		}
	}
	run.advanceLocked()
	s.mu.Unlock()
	for _, l := range logs {
		l()
	}
}

// GroupHostAction is the user's action on the current step: on a host
// (hostID set) "retry" (propose it again), "skip", "done" (mark it done
// after it was typed or failed) or "confirm" (a production host's reported
// exit 0); on the whole step (hostID empty) "done" (a step with no command
// or no host) or "skip". Nothing skips ahead: only the current step.
func (s *Service) GroupHostAction(id, stepID, hostID, action string) (GroupRun, error) {
	s.refreshGroupRun(id)
	now := s.now()
	s.mu.Lock()
	run := s.groupRuns[id]
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return GroupRun{}, ErrNoGroupRun
	}
	run.advanceLocked()
	idx := -1
	for i, st := range run.Steps {
		if st.ID == stepID {
			idx = i
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return GroupRun{}, ErrNotFound
	}
	if idx != run.Current {
		s.mu.Unlock()
		return GroupRun{}, ErrStepNotCurrent
	}
	st := &run.Steps[idx]
	var what string
	if hostID == "" {
		switch action {
		case "done":
			if st.Command != "" {
				s.mu.Unlock()
				return GroupRun{}, badRequest("this step has a command: mark each host, or skip the step")
			}
			st.Done = true
			what = "checked by you"
		case "skip":
			for _, c := range st.Cells {
				if c.State == cellTyping {
					s.mu.Unlock()
					return GroupRun{}, ErrTyping
				}
			}
			st.Skipped = true
			for j := range st.Cells {
				if c := &st.Cells[j]; c.State != cellDone {
					c.State, c.Proposal, c.FinishedAt, c.hasMark = cellSkipped, nil, &now, false
				}
			}
			what = "skipped by you on every host not done"
		default:
			s.mu.Unlock()
			return GroupRun{}, badRequest("action is done or skip")
		}
	} else {
		var c *GroupRunCell
		for j := range st.Cells {
			if st.Cells[j].HostID == hostID {
				c = &st.Cells[j]
			}
		}
		if c == nil {
			s.mu.Unlock()
			return GroupRun{}, ErrNotFound
		}
		if c.State == cellTyping && action != "confirm" {
			s.mu.Unlock()
			return GroupRun{}, ErrTyping
		}
		switch action {
		case "retry":
			if c.State != cellFailed && c.State != cellTyped && c.State != cellProposed {
				s.mu.Unlock()
				return GroupRun{}, badRequest("only a proposed, typed or failed host can be retried")
			}
			// What the step captured on this host goes: the retry captures
			// again.
			run.dropStepCapturesLocked(st.ID, c.HostID)
			c.Captures = nil
			c.State, c.Proposal, c.Exit, c.Result, c.Confirm, c.TypedAt, c.FinishedAt, c.hasMark, c.Pending = cellWaiting, nil, nil, "", false, nil, nil, false, false
			if st.OneHost {
				c.HostID, c.Host, c.SessionID = "", "", ""
			}
			what = "to be proposed again (retry by you)"
		case "skip":
			if c.State == cellDone {
				s.mu.Unlock()
				return GroupRun{}, badRequest("this host is done")
			}
			c.State, c.Proposal, c.Result, c.FinishedAt, c.hasMark, c.Pending = cellSkipped, nil, "user", &now, false, false
			what = "skipped by you"
		case "done":
			if c.State != cellTyped && c.State != cellFailed {
				s.mu.Unlock()
				return GroupRun{}, badRequest("only a typed or failed host can be marked done")
			}
			if c.Pending {
				s.mu.Unlock()
				return GroupRun{}, ErrResultPending
			}
			if c.Confirm && c.Exit != nil && *c.Exit == 0 {
				what = "confirmed done by you after a reported exit 0"
			} else {
				c.Result = "user"
				what = "marked done by you"
			}
			c.State, c.Confirm, c.FinishedAt, c.hasMark, c.Pending = cellDone, false, &now, false, false
		case "confirm":
			if !(c.State == cellTyped && c.Confirm) {
				s.mu.Unlock()
				return GroupRun{}, badRequest("nothing to confirm on this host")
			}
			c.State, c.Confirm, c.FinishedAt = cellDone, false, &now
			what = "confirmed done by you after a reported exit 0"
		default:
			s.mu.Unlock()
			return GroupRun{}, badRequest("action is retry, skip, done or confirm")
		}
	}
	run.advanceLocked()
	run.used = now
	out := run.view()
	runID, rb, n, of := run.ID, run.RunbookID, idx+1, len(run.Steps)
	s.mu.Unlock()
	s.audit.RunbookEvent(fmt.Sprintf("group run %s · step %d of %d %s", runID, n, of, what), rb, hostID, redact.Report{})
	return out, nil
}

// modeLockedLocked reports whether the mode may not change now: a rolling
// step stopped at a failure, or a host of the current step proposed, being
// typed or waiting for its result. Call with s.mu held.
func (r *groupRun) modeLockedLocked() bool {
	r.advanceLocked()
	if r.Halted != "" {
		return true
	}
	if r.Current < 0 {
		return false
	}
	for _, c := range r.Steps[r.Current].Cells {
		switch {
		case c.State == cellProposed, c.State == cellTyping:
			return true
		case c.State == cellTyped && !c.Confirm && c.Result == "":
			return true
		}
	}
	return false
}

// SetGroupRunMode switches between rolling and parallel, only while
// modeLockedLocked allows it (ErrModeLocked otherwise). It applies to the
// proposals made from then on.
func (s *Service) SetGroupRunMode(id, mode string) (GroupRun, error) {
	if !validMode(mode) {
		return GroupRun{}, badRequest("mode is rolling or parallel")
	}
	s.mu.Lock()
	run := s.groupRuns[id]
	if run == nil || run.Status != runActive {
		s.mu.Unlock()
		return GroupRun{}, ErrNoGroupRun
	}
	changed := run.Mode != mode
	if changed && run.modeLockedLocked() {
		s.mu.Unlock()
		return GroupRun{}, ErrModeLocked
	}
	run.Mode = mode
	run.advanceLocked()
	out := run.view()
	rb := run.RunbookID
	s.mu.Unlock()
	if changed {
		s.audit.RunbookEvent(fmt.Sprintf("group run %s · mode set to %s by you", id, mode), rb, "", redact.Report{})
	}
	return out, nil
}

// StopGroupRun stops a run. Its proposals can no longer be typed.
func (s *Service) StopGroupRun(id string) (GroupRun, error) {
	s.mu.Lock()
	run := s.groupRuns[id]
	if run == nil {
		s.mu.Unlock()
		return GroupRun{}, ErrNoGroupRun
	}
	at := run.Current + 1
	if run.Status == runActive {
		run.Status = runStopped
	}
	for i := range run.Steps {
		for j := range run.Steps[i].Cells {
			c := &run.Steps[i].Cells[j]
			if c.State == cellProposed {
				c.State, c.Proposal = cellWaiting, nil
			}
		}
	}
	run.NeedPane = nil
	run.wipeCapturesLocked()
	out := run.view()
	rb := run.RunbookID
	s.mu.Unlock()
	s.audit.RunbookEvent(fmt.Sprintf("group run %s stopped by you at step %d of %d", id, at, len(out.Steps)), rb, "", redact.Report{})
	return out, nil
}

// ListGroupRuns returns the runs keel serve holds, newest first.
func (s *Service) ListGroupRuns() []GroupRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GroupRun, 0, len(s.groupRuns))
	for _, r := range s.groupRuns {
		out = append(out, r.view())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
