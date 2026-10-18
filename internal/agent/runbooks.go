package agent

import (
	"fmt"
	"strings"

	"keel/internal/agent/redact"
	"keel/internal/applog"
)

// Runbooks (docs/ui-redesign.md §16). A runbook is a checklist the user
// curated. Its text goes to the provider only with a question that reads
// at least one pane the agent may read, through the request's Redactor,
// and every send is logged as agent.runbook with ids and counts, never the
// text. The agent never runs a runbook: each step is a normal command
// proposal the user types, one at a time.

// RunbookExcerpt is redacted runbook text for one request.
type RunbookExcerpt struct {
	Text   redact.Safe
	Report redact.Report
}

// readAllowed reports whether any read pane is one the agent may read now.
func readAllowed(scope Scope, read []Pane) bool {
	for _, p := range read {
		if scope.Allows(p) {
			return true
		}
	}
	return false
}

// ReadRunbook redacts the active runbook's text (its steps and their
// progress) for one request and logs agent.runbook. It is refused, before
// anything is redacted or logged, when no pane the agent may read was read
// for the question.
func (a *Auditor) ReadRunbook(scope Scope, read []Pane, id string, steps int, text string, r *redact.Redactor) (RunbookExcerpt, error) {
	if !readAllowed(scope, read) {
		return RunbookExcerpt{}, ErrNotReadable
	}
	if r == nil {
		return RunbookExcerpt{}, fmt.Errorf("agent: no redactor")
	}
	before := r.Report()
	safe := r.Text(text)
	rep := diff(before, r.Report())
	a.log.Append(applog.Entry{
		Kind:       EventRunbook,
		Message:    fmt.Sprintf("agent sent runbook %s (%s) with a question · %s", id, noteCount(steps, "step", "steps"), redactionNote(rep)),
		Count:      steps,
		Redactions: rep.Strings(),
	})
	return RunbookExcerpt{Text: safe, Report: rep}, nil
}

// OfferRunbooks redacts the list of runbooks the agent may pick from (their
// names and Use when lines) and logs agent.runbook with their ids. Refused
// like ReadRunbook when no readable pane was read.
func (a *Auditor) OfferRunbooks(scope Scope, read []Pane, ids []string, text string, r *redact.Redactor) (RunbookExcerpt, error) {
	if !readAllowed(scope, read) {
		return RunbookExcerpt{}, ErrNotReadable
	}
	if r == nil {
		return RunbookExcerpt{}, fmt.Errorf("agent: no redactor")
	}
	before := r.Report()
	safe := r.Text(text)
	rep := diff(before, r.Report())
	a.log.Append(applog.Entry{
		Kind:       EventRunbook,
		Message:    fmt.Sprintf("agent sent the names of %s to pick from with a question (%s) · %s", noteCount(len(ids), "runbook", "runbooks"), strings.Join(ids, ", "), redactionNote(rep)),
		Count:      len(ids),
		Redactions: rep.Strings(),
	})
	return RunbookExcerpt{Text: safe, Report: rep}, nil
}

// RunbookEvent logs agent.runbook for a use, a step or a change: what
// happened, to which runbook (by id), never its text. rep is what was
// redacted before something was kept.
func (a *Auditor) RunbookEvent(what, id, hostID string, rep redact.Report) {
	msg := what
	if id != "" {
		msg += " · " + id
	}
	if len(rep.Counts) > 0 {
		msg += " · " + redactionNote(rep)
	}
	a.log.Append(applog.Entry{
		Kind:       EventRunbook,
		Message:    msg,
		HostID:     hostID,
		Redactions: rep.Strings(),
	})
}
