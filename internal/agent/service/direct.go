package service

import (
	"context"
	"errors"
	"unicode"
	"unicode/utf8"

	"keel/internal/agent"
	"keel/internal/runvars"
)

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13, for
// review). A read-only command of one line is typed with one click and
// without Enter: it waits on the prompt line until the user presses Enter.
// Everything else keeps approve-before-type. keel serve decides, never the
// page: the checks below run again at the moment of the write, on the exact
// bytes, and any doubt sends the user to the dialog.

// Why a Type needs the approve dialog. The page opens the dialog (the
// approved path, unchanged) and nothing was typed.
const (
	ReasonLines = "it is more than one line, or holds a control or invisible character"
	ReasonRisk  = "it is not rated read-only"
	ReasonBytes = "Keel cannot confirm these are the bytes it was shown"
)

// DialogError says a Type cannot go without the approve dialog, and why.
type DialogError struct{ Reason string }

func (e *DialogError) Error() string { return "agent: needs the approve dialog: " + e.Reason }

func needsDialog(reason string) error { return &DialogError{Reason: reason} }

// NeedsDialog reports whether err sends the Type to the approve dialog
// (rather than refusing it), and why.
func NeedsDialog(err error) (string, bool) {
	var d *DialogError
	if errors.As(err, &d) {
		return d.Reason, true
	}
	return "", false
}

// DirectLine reports whether command stays one line on a shell prompt, as
// typed: not empty, valid UTF-8, and no control character (C0, which holds
// newline, CR, tab and ESC; DEL; C1), no line or paragraph separator, and
// no invisible format character (zero-width and bidi controls), which
// could show the user something else than what the shell reads.
func DirectLine(command string) bool {
	if command == "" || !utf8.ValidString(command) {
		return false
	}
	for _, r := range command {
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return false
		case r == '\u2028', r == '\u2029':
			return false
		case unicode.Is(unicode.Cf, r):
			return false
		}
	}
	return true
}

// AuthorizeDirect checks a Type without the dialog of an agent proposal
// (a conversation's, or a group run's for one host): the client drives,
// the bytes are the proposal's command alone (no ^U, no newline; for a
// group run's step with its values filled in now, rule 11), one line with
// no control character, no placeholder, and the rating is read-only (the
// stricter of the stored rating and Keel's code rule on the bytes). A
// *DialogError means the approve dialog decides instead.
//
// With commit it logs agent.type (Source "direct") and, for a group run,
// claims the host as AuthorizeType does; the caller then writes req.Bytes
// and calls TypeDoneAt or GroupTypeDoneAt (GroupTypeFailed if the write
// failed). Without commit it changes nothing: the group run page checks
// every host before one click types into them all.
func (s *Service) AuthorizeDirect(req TypeRequest, commit bool) error {
	if err := agent.CanInsert(req.Role); err != nil {
		return err
	}
	if hasPlaceholder(req.Bytes) {
		return ErrPlaceholder
	}
	if !DirectLine(req.Bytes) {
		return needsDialog(ReasonLines)
	}
	if req.GroupRunID != "" {
		t, err := s.groupTypeTarget(req, true)
		if err != nil {
			return err
		}
		if agent.Stricter(t.risk, agent.Classify(t.want)) != agent.RiskReadOnly {
			return needsDialog(ReasonRisk)
		}
		if !commit {
			return nil
		}
		return s.claimGroupType(req, t, agent.TypeDirect)
	}
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
	if hasPlaceholder(snap.Command) {
		return ErrPlaceholder
	}
	if runvars.HasRefs(snap.Command) {
		return ErrVarsOutsideRun
	}
	if req.Bytes != snap.Command {
		return needsDialog(ReasonBytes)
	}
	if agent.Stricter(snap.Risk, agent.Classify(snap.Command)) != agent.RiskReadOnly {
		return needsDialog(ReasonRisk)
	}
	if !commit {
		return nil
	}
	return s.audit.TypeFrom(agent.Pane{SessionID: snap.SessionID, HostID: snap.HostID, HostName: snap.Host, Role: req.Role}, req.Bytes, agent.TypeDirect)
}

// RateCommand rates a command that has no rating of its own (a snippet, a
// notebook's code block) for a Type without the dialog, by the path that
// rates in-pane suggestions: Keel's code rule, made stricter (never
// looser) by Jev. Jev is asked only when the code rule says read-only, a
// Jev key is set, the pane's host is one the agent may read, and maySend
// (the text may leave the machine: a snippet marked agent-usable, a
// notebook that is not private; §6 rules 3 and 12). The command goes
// redacted, and the egress is logged as agent.read. Without Jev (no key,
// offline, a timeout, an error, or not allowed), the code rule decides.
// A nil Service rates with the code rule alone.
func (s *Service) RateCommand(ctx context.Context, sessionID, command string, maySend bool) agent.Risk {
	risk := agent.Classify(command)
	if s == nil || risk != agent.RiskReadOnly || !maySend {
		return risk
	}
	p, _, hosts, err := s.paneFor(sessionID, agent.RoleDriver)
	if err != nil {
		return risk
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return risk
	}
	r, err := s.requestRedactor(set.Redact, hosts)
	if err != nil {
		return risk
	}
	judged, _ := s.judgeRisks(ctx, p, []Candidate{{Command: command}}, []int{0}, r)
	if j, ok := judged[0]; ok {
		risk = agent.Stricter(risk, j)
	}
	return risk
}
