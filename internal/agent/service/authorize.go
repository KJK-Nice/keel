package service

import (
	"context"
	"strings"

	"keel/internal/agent"
	"keel/internal/cmdrule"
	"keel/internal/runvars"
)

// One gate for every way agent output reaches a pane (docs/ui-redesign.md
// §6 rules 1, 5, 6, 7, 9, 10, 11, 13). The user's action is always the
// trigger; keel serve asks authorize before it writes, and authorize writes
// nothing to a pane.
//
// Two things vary, and both are data:
//
//   - the path (typePath): the approve dialog, a type without the dialog
//     (rule 13), or ⇥ on an in-pane suggestion (§5.5);
//   - the subject (typeSubject): where the command lives, a conversation's
//     proposal, a group run's host, or a suggestion's option.
//
// The checks themselves are written once, in authorize, in one order.

// typePath is one way the bytes reach the pane. Every difference between
// the paths is a field here.
type typePath struct {
	// wire is what the client must send for a command.
	wire func(command string) string
	// noNewline: the bytes land on the prompt line and are not run, so a
	// line break in them is refused.
	noNewline bool
	// noDialog: nobody saw the exact bytes in the approve dialog (rule 13).
	// The command must be one line, plainly read-only and one of the
	// commands Keel types without asking; what is not sends the user to
	// the dialog (*DialogError) instead of refusing.
	noDialog bool
	// editable: the user edits the line before running it (§5.5), so a
	// redaction placeholder or a variable name is flagged on the card and
	// not refused. Everywhere else neither is ever typed (rules 9, 11).
	editable bool
	// logOnWrite: the log entry is written once the bytes are (settle), so
	// a write that failed is never logged as made. Otherwise it is written
	// here, before the write.
	logOnWrite bool
	// needsShift refuses a command the ⇧ rule holds back; judgedShift, when
	// set, is the wording when only Jev's later rating asks for ⇧.
	needsShift, judgedShift error
	// record writes the path's log entry for a grant.
	record func(a *agent.Auditor, g Grant) error
}

var (
	// pathApproved: Type in the approve dialog, which showed the exact
	// bytes (^U, the command, a newline).
	pathApproved = typePath{
		wire:       typedBytes,
		needsShift: ErrNeedsShift,
		record:     func(a *agent.Auditor, g Grant) error { return a.TypeFrom(g.pane, g.bytes, agent.TypeApproved) },
	}
	// pathDirect: one click, no dialog and no Enter (rule 13): the command
	// alone.
	pathDirect = typePath{
		wire:       func(command string) string { return command },
		noNewline:  true,
		noDialog:   true,
		logOnWrite: true,
		needsShift: ErrNeedsShift,
		record:     func(a *agent.Auditor, g Grant) error { return a.TypeFrom(g.pane, g.bytes, agent.TypeDirect) },
	}
	// pathInsert: ⇥ or ⇧⇥ on an in-pane suggestion (§5.5): ^U and the
	// command, not run.
	pathInsert = typePath{
		wire:        insertBytes,
		noNewline:   true,
		editable:    true,
		logOnWrite:  true,
		needsShift:  ErrNeedsShiftInsert,
		judgedShift: ErrNeedsShiftJudged,
		record:      func(a *agent.Auditor, g Grant) error { return a.InsertFrom(g.pane, g.bytes, g.source) },
	}
)

// typeTarget is what a subject holds at one moment.
type typeTarget struct {
	pane agent.Pane // the pane it is for; SessionID "" when it has none
	// command is the command as kept: a group run's step still names its
	// variables here.
	command string
	risk    agent.Risk
	// production is the host's flag when it was proposed.
	production bool
	// judged and codeRisk: Jev rated it after the card was shown, and what
	// the code rule said before (a suggestion's option only).
	judged   bool
	codeRisk agent.Risk
	source   string // where a suggestion came from, for agent.insert
}

// hostNow is the target's host as of this moment.
type hostNow struct {
	// production: the flag now; true when the host can no longer be found.
	production bool
	// stepHosts is how many hosts the step goes to, anyProduction whether
	// any of them is production now (a group run's step; else 1 and the
	// host's own flag).
	stepHosts     int
	anyProduction bool
}

// typeSubject is where the command to type lives.
type typeSubject interface {
	// find returns it as held now, or why it cannot be typed at all: gone,
	// expired, already used, not waiting.
	find() (typeTarget, error)
	// fill puts a command's variables in, now, for this target: the
	// command as it will be typed, its rating (never looser than the kept
	// one) and how many values went in. Only a group run's step is filled;
	// every other subject refuses.
	fill(t typeTarget) (command string, risk agent.Risk, filled int, err error)
	// host reads the target's host now, and refuses a target that may no
	// longer be typed into (a host that left its group).
	host(t typeTarget) (hostNow, error)
	// askJev reports whether a type without the dialog is also rated by
	// Jev (rule 13): never a group run's step, which sends nothing.
	askJev() bool
	// claim reserves the subject for one write, under its own lock, after
	// running recheck on the target as it is at that moment. release gives
	// it back when the write did not happen.
	claim(recheck func(typeTarget) error, filled int) error
	release()
}

// typeAsk is what the client sent: the bytes it wants written, where, and
// who it is.
type typeAsk struct {
	sessionID string
	role      agent.Role
	bytes     string
	shift     bool
	// checkOnly answers whether the write would be allowed now and changes
	// nothing: no Jev call, no claim, no log entry.
	checkOnly bool
}

// Grant is one authorized write. keel serve writes the bytes, then settles
// the grant (DirectTyped, InsertDone), which logs it or gives it back.
type Grant struct {
	path   *typePath
	sub    typeSubject
	pane   agent.Pane
	bytes  string
	source string
}

// DirectGrant and InsertGrant are Grant under the names keel serve's two
// callers use.
type (
	DirectGrant = Grant
	InsertGrant = Grant
)

// authorize runs every check before Keel writes agent output into a pane,
// in this order:
//
//  1. the client drives the pane (rule 6);
//  2. the bytes as sent: no redaction placeholder (rule 9), one line where
//     the path asks for it (rule 13);
//  3. the subject exists and is waiting;
//  4. it is for this pane;
//  5. its command holds no placeholder (rule 9) and no variable that Keel
//     does not fill in here, now (rule 11);
//  6. the bytes are exactly the ones shown;
//  7. the host as of now: its production flag, and that it may still be
//     typed into;
//  8. the rating: a type without the dialog must be plainly read-only
//     (rule 13), and the ⇧ rule holds with the host counted as production
//     if it was when proposed, is now, or cannot be found (rule 5);
//  9. Jev, for a type without the dialog (rule 13);
//  10. the subject is claimed for this one write, checked again under its
//     lock (rule 10);
//  11. the log entry (rule 7), unless the path writes it once the bytes are
//     written.
func (s *Service) authorize(ctx context.Context, path *typePath, sub typeSubject, ask typeAsk) (Grant, error) {
	if err := agent.CanInsert(ask.role); err != nil {
		return Grant{}, err
	}
	if !path.editable && cmdrule.HasPlaceholder(ask.bytes) {
		return Grant{}, ErrPlaceholder
	}
	if path.noDialog && !cmdrule.DirectLine(ask.bytes) {
		return Grant{}, needsDialog(ReasonLines)
	}
	t, err := sub.find()
	if err != nil {
		return Grant{}, err
	}
	if t.pane.SessionID == "" {
		return Grant{}, ErrNoTarget
	}
	if t.pane.SessionID != ask.sessionID {
		return Grant{}, ErrWrongPane
	}
	command, risk, filled := t.command, t.risk, 0
	if !path.editable {
		if cmdrule.HasPlaceholder(t.command) {
			return Grant{}, ErrPlaceholder
		}
		if runvars.HasRefs(t.command) {
			if command, risk, filled, err = sub.fill(t); err != nil {
				return Grant{}, err
			}
		}
		if cmdrule.HasPlaceholder(command) {
			return Grant{}, ErrPlaceholder
		}
	}
	if path.noDialog && !cmdrule.DirectLine(command) {
		return Grant{}, needsDialog(ReasonLines)
	}
	// same compares the client's bytes with a command as kept.
	same := func(command string) error {
		if ask.bytes == path.wire(command) && !(path.noNewline && strings.ContainsAny(ask.bytes, "\r\n")) {
			return nil
		}
		if path.noDialog {
			return needsDialog(ReasonBytes)
		}
		return ErrChanged
	}
	if err := same(command); err != nil {
		return Grant{}, err
	}
	now, err := sub.host(t)
	if err != nil {
		return Grant{}, err
	}
	if path.noDialog {
		if !directSafe(risk, command) {
			return Grant{}, needsDialog(ReasonRisk)
		}
		if now.stepHosts > 1 && now.anyProduction {
			return Grant{}, needsDialog(ReasonProd)
		}
	}
	production := t.production || now.production
	// shiftRule is the ⇧ rule (§6.5) for a rating.
	shiftRule := func(risk, codeRisk agent.Risk, judged bool) error {
		if !cmdrule.NeedsShift(risk, production) || ask.shift {
			return nil
		}
		if judged && path.judgedShift != nil && !cmdrule.NeedsShift(codeRisk, production) {
			return path.judgedShift
		}
		return path.needsShift
	}
	if err := shiftRule(risk, t.codeRisk, t.judged); err != nil {
		return Grant{}, err
	}
	if ask.checkOnly {
		return Grant{}, nil
	}
	if path.noDialog && sub.askJev() {
		if err := s.jevReadOnly(ctx, t.pane.SessionID, command); err != nil {
			return Grant{}, err
		}
	}
	// Under the subject's lock, on what it holds then: an edit or a
	// stricter rating may have landed since find.
	recheck := func(cur typeTarget) error {
		if err := same(cur.command); err != nil {
			return err
		}
		return shiftRule(cur.risk, cur.codeRisk, cur.judged)
	}
	if err := sub.claim(recheck, filled); err != nil {
		return Grant{}, err
	}
	t.pane.Role = ask.role
	g := Grant{path: path, sub: sub, pane: t.pane, bytes: ask.bytes, source: t.source}
	if !path.logOnWrite {
		if err := path.record(s.audit, g); err != nil {
			sub.release()
			return Grant{}, err
		}
	}
	return g, nil
}

// settle ends a grant whose log entry waits for the write: a write that
// happened is logged (ids and counts, never the bytes); one that failed is
// not, and the subject may be typed again. It returns the log's error.
func (s *Service) settle(g Grant, writeErr error) error {
	if g.sub == nil {
		return nil
	}
	if writeErr != nil {
		g.sub.release()
		return nil
	}
	if !g.path.logOnWrite {
		return nil
	}
	return g.path.record(s.audit, g)
}

// convSubject is a conversation's proposal.
type convSubject struct {
	s                  *Service
	convID, proposalID string
}

func (c convSubject) find() (typeTarget, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	p, err := c.s.proposalLocked(c.convID, c.proposalID)
	if err != nil {
		return typeTarget{}, err
	}
	return typeTarget{
		pane:       agent.Pane{SessionID: p.SessionID, HostID: p.HostID, HostName: p.Host},
		command:    p.Command,
		risk:       p.Risk,
		production: p.Production,
	}, nil
}

// Only a group run's step has its variables filled in (groupvars.go);
// anywhere else {{name}} would be typed as text.
func (c convSubject) fill(typeTarget) (string, agent.Risk, int, error) {
	return "", "", 0, ErrVarsOutsideRun
}

func (c convSubject) host(t typeTarget) (hostNow, error) {
	prod, err := c.s.hostProduction(t.pane.HostID)
	return hostNow{production: prod, stepHosts: 1, anyProduction: prod}, err
}

func (c convSubject) askJev() bool { return true }

// A proposal may be typed more than once: there is nothing to reserve,
// only the recheck.
func (c convSubject) claim(recheck func(typeTarget) error, _ int) error {
	cur, err := c.find()
	if err != nil {
		return err
	}
	return recheck(cur)
}

func (c convSubject) release() {}

// groupSubject is one host's proposal in a group run (grouprun.go).
type groupSubject struct {
	s                 *Service
	runID, proposalID string
}

// find: the run is active and the host is waiting (proposed).
func (g groupSubject) find() (typeTarget, error) {
	g.s.mu.Lock()
	defer g.s.mu.Unlock()
	run, c := g.s.groupProposalLocked(g.runID, g.proposalID)
	if run == nil || c == nil || run.Status != runActive {
		return typeTarget{}, ErrNotFound
	}
	if c.State != cellProposed {
		return typeTarget{}, ErrNotProposed
	}
	p := c.Proposal
	return typeTarget{
		pane:       agent.Pane{SessionID: p.SessionID, HostID: p.HostID, HostName: p.Host},
		command:    p.Command,
		risk:       p.Risk,
		production: p.Production,
	}, nil
}

// fill: the step's command with its values put in now, which is what the
// approve dialog showed; the kept proposal still names them. A missing or
// unsafe value, a vault value or a placeholder refuses it (rule 11).
func (g groupSubject) fill(typeTarget) (string, agent.Risk, int, error) {
	f, err := g.s.fillProposal(g.runID, g.proposalID)
	if err != nil {
		return "", "", 0, err
	}
	return f.command, f.Risk, len(f.Used), nil
}

// host: the host is still in the run's group and the agent may read it
// now (rule 10).
func (g groupSubject) host(t typeTarget) (hostNow, error) {
	g.s.mu.Lock()
	run, c := g.s.groupProposalLocked(g.runID, g.proposalID)
	var (
		groupID string
		hosts   []string // the hosts of this step (rule 13: production)
	)
	live := run != nil && c != nil
	if live {
		groupID = run.GroupID
		for i := range run.Steps {
			mine := false
			for j := range run.Steps[i].Cells {
				mine = mine || &run.Steps[i].Cells[j] == c
			}
			if mine {
				for _, x := range run.Steps[i].Cells {
					if x.HostID != "" {
						hosts = append(hosts, x.HostID)
					}
				}
			}
		}
	}
	g.s.mu.Unlock()
	if !live {
		return hostNow{}, ErrNotFound
	}
	_, members, byID, err := g.s.groupMembers(groupID)
	if err != nil {
		return hostNow{}, ErrNotTarget
	}
	member := false
	for _, m := range members {
		member = member || m.HostID == t.pane.HostID
	}
	h, found := byID[t.pane.HostID]
	if !member || !found || !h.AgentReadable {
		return hostNow{}, ErrNotTarget
	}
	anyProd := false
	for _, id := range hosts {
		if x, ok := byID[id]; !ok || x.Production {
			anyProd = true
		}
	}
	return hostNow{production: h.Production, stepHosts: len(hosts), anyProduction: anyProd}, nil
}

func (g groupSubject) askJev() bool { return false }

// claim: the host is typing until the write is done (GroupTypeDoneAt) or
// failed (release); Retry, Skip and a mode change leave it alone
// meanwhile, so a step is typed once. The command is not compared again:
// a run's step cannot be edited, and its values were filled in just now.
func (g groupSubject) claim(_ func(typeTarget) error, filled int) error {
	g.s.mu.Lock()
	defer g.s.mu.Unlock()
	run, c := g.s.groupProposalLocked(g.runID, g.proposalID)
	if run == nil || c == nil || run.Status != runActive || c.State != cellProposed {
		return ErrNotProposed
	}
	c.State, c.filled = cellTyping, filled
	return nil
}

func (g groupSubject) release() { g.s.GroupTypeFailed(g.runID, g.proposalID) }

// suggestionSubject is one option of an in-pane suggestion (suggest.go).
type suggestionSubject struct {
	s      *Service
	id     string
	option int
}

// targetLocked reads the option as kept. Call with s.sug.mu held.
func (u suggestionSubject) targetLocked() (typeTarget, *Suggestion, error) {
	sug := u.s.sug.suggestions[u.id]
	if sug == nil || u.s.now().Sub(sug.created) > suggestionTTL {
		return typeTarget{}, nil, ErrExpired
	}
	if sug.used {
		return typeTarget{}, nil, ErrUsed
	}
	if u.option < 0 || u.option >= len(sug.Options) {
		return typeTarget{}, nil, ErrNotFound
	}
	opt := sug.Options[u.option]
	return typeTarget{
		pane:       agent.Pane{SessionID: sug.SessionID, HostID: sug.HostID, HostName: sug.Host},
		command:    opt.Command,
		risk:       opt.Risk,
		production: sug.Production,
		judged:     opt.Judged,
		codeRisk:   opt.codeRisk,
		source:     opt.Source,
	}, sug, nil
}

func (u suggestionSubject) find() (typeTarget, error) {
	u.s.sug.mu.Lock()
	defer u.s.sug.mu.Unlock()
	t, _, err := u.targetLocked()
	return t, err
}

func (u suggestionSubject) fill(typeTarget) (string, agent.Risk, int, error) {
	return "", "", 0, ErrVarsOutsideRun
}

func (u suggestionSubject) host(t typeTarget) (hostNow, error) {
	prod, err := u.s.hostProduction(t.pane.HostID)
	return hostNow{production: prod, stepHosts: 1, anyProduction: prod}, err
}

func (u suggestionSubject) askJev() bool { return false }

// claim marks the suggestion used, so a resent insert is refused. The
// option is read again under the lock that reserves it: a RiskCheck may
// have made its rating stricter since find.
func (u suggestionSubject) claim(recheck func(typeTarget) error, _ int) error {
	u.s.sug.mu.Lock()
	defer u.s.sug.mu.Unlock()
	cur, sug, err := u.targetLocked()
	if err != nil {
		return err
	}
	if err := recheck(cur); err != nil {
		return err
	}
	sug.used = true
	return nil
}

func (u suggestionSubject) release() {
	u.s.sug.mu.Lock()
	defer u.s.sug.mu.Unlock()
	if cur := u.s.sug.suggestions[u.id]; cur != nil {
		cur.used = false
	}
}
