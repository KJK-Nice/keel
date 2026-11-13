package service

import (
	"context"
	"errors"

	"keel/internal/agent"
	"keel/internal/cmdrule"
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
	ReasonLines   = "it is more than one line, or holds a control or invisible character"
	ReasonRisk    = "it is not one of the plain read-only commands Keel types without asking"
	ReasonJev     = "Jev did not rate it read-only"
	ReasonJevNone = "Jev was asked and did not answer"
	ReasonProd    = "a host of this step is production: tick each host"
	ReasonBytes   = "Keel cannot confirm these are the bytes it was shown"
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

// AuthorizeDirect checks a Type without the dialog of an agent proposal
// (a conversation's, or a group run's for one host) through authorize
// (authorize.go, pathDirect): the client drives, the bytes are the
// proposal's command alone (no ^U, no newline; for a group run's step with
// its values filled in now, rule 11), one line with no control character,
// no placeholder, and directSafe. A conversation's proposal (the least
// trusted source) is also rated by Jev when a key is set, and must come
// back read-only. A group run's step is never sent to Jev (a group run
// sends nothing to a provider, and its filled values never leave the
// machine, §6 rules 10 and 11); one whose step targets more than one host
// goes to the dialog when any of them is production. A *DialogError means
// the approve dialog decides instead.
//
// With commit a group run's host is claimed as AuthorizeType does; the
// caller then writes req.Bytes, calls DirectTyped with the grant once the
// write succeeded (agent.type, Source "direct", is logged only then), and
// TypeDoneAt or GroupTypeDoneAt (GroupTypeFailed if the write failed).
// Without commit it changes nothing: the group run page checks every host
// before one click types into them all.
func (s *Service) AuthorizeDirect(ctx context.Context, req TypeRequest, commit bool) (DirectGrant, error) {
	return s.authorize(ctx, &pathDirect, req.subject(s), req.ask(!commit))
}

// DirectTyped logs agent.type (source "direct") for a grant whose bytes
// were written: ids and counts, never the bytes. A write that failed is
// never logged as typed. It returns the log's error.
func (s *Service) DirectTyped(g DirectGrant) error { return s.settle(g, nil) }

// directSafe is the one decision of rule 13 on a command: plainly
// read-only by its kept rating and by Keel's code rule on the bytes, and
// one of the few exact commands cmdrule.DirectSafe allows.
func directSafe(kept agent.Risk, command string) bool {
	return cmdrule.Stricter(kept, cmdrule.Classify(command)) == agent.RiskReadOnly && cmdrule.DirectSafe(command)
}

// DirectRating decides a command that has no rating of its own (a
// snippet, a notebook's code block) for a Type without the dialog: it must
// be directSafe, and, when Jev may be asked, Jev must rate it read-only.
// Jev is asked only when maySend (the text may leave the machine: a
// snippet marked agent-usable, a notebook that is not private; §6 rules 3
// and 12). A nil Service decides by directSafe alone.
func (s *Service) DirectRating(ctx context.Context, sessionID, command string, maySend bool) error {
	if !directSafe(agent.RiskReadOnly, command) {
		return needsDialog(ReasonRisk)
	}
	if s == nil || !maySend {
		return nil
	}
	return s.jevReadOnly(ctx, sessionID, command)
}

// jevReadOnly asks Jev to rate a command a direct type would write. It is
// asked only when a Jev key is set (whatever the in-pane suggestions
// setting) and the pane's host is one the agent may read; the command
// goes redacted and the egress is logged as agent.read. An answer that is
// not read-only sends the Type to the dialog, and so does a call that was
// made and got no answer (an error, a timeout, offline): fail closed. Jev
// not asked (no key, or a host the agent may not read): the allowlist
// alone has decided, and it stands. Only those two mean "not asked":
// anything Keel could not find out (the vault or the hosts cannot be read,
// the client cannot be built, the entry cannot be logged) sends the Type
// to the dialog too.
func (s *Service) jevReadOnly(ctx context.Context, sessionID, command string) error {
	judge, err := s.judgeFor()
	if errors.Is(err, ErrNoKey) {
		return nil
	}
	if err != nil {
		return needsDialog(ReasonJevNone)
	}
	p, _, hosts, err := s.paneFor(sessionID, agent.RoleDriver)
	if errors.Is(err, agent.ErrNotReadable) {
		return nil
	}
	if err != nil {
		return needsDialog(ReasonJevNone)
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return needsDialog(ReasonJevNone)
	}
	r, err := s.requestRedactor(set.Redact, hosts)
	if err != nil {
		return needsDialog(ReasonJevNone)
	}
	judged, _ := s.judgeRisks(ctx, judge, p, []Candidate{{Command: command}}, []int{0}, r)
	j, ok := judged[0]
	if !ok {
		return needsDialog(ReasonJevNone)
	}
	if j != agent.RiskReadOnly {
		return needsDialog(ReasonJev)
	}
	return nil
}
