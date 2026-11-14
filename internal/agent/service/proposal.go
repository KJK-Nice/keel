package service

import (
	"time"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/cmdrule"
)

// Every proposal the user can type is made here (docs/ui-redesign.md §6
// rules 5 and 9): an answer's, an outside agent's, one proposed again, a
// runbook step's, a group run's. What its command decides is set by seal
// and nowhere else, so no proposal exists without its rating, its ⇧ rule
// and its placeholder scan.

// seal sets everything a proposal's command decides, from p.Command and
// p.Production: the exact bytes Type sends, the rating (Keel's code rule,
// never looser than floor), the ⇧ rule, and whether it holds a redaction
// placeholder. Call it whenever either changes.
func (p *Proposal) seal(floor agent.Risk) {
	p.Bytes = typedBytes(p.Command)
	p.Risk = rate(p.Command, floor)
	p.NeedsShift = cmdrule.NeedsShift(p.Risk, p.Production)
	p.Placeholders = cmdrule.HasPlaceholder(p.Command)
}

// proposalSpec is what a caller knows about a proposal it wants made.
type proposalSpec struct {
	Command string
	Why     string
	// Floor is a rating that can only make Keel's stricter: the model's
	// own, or the saved one of a proposal made again. "" for none.
	Floor agent.Risk
	// Pane is where it may be typed. With no SessionID it is Copy only;
	// its host (when known) is still shown.
	Pane agent.Pane
	// Production is the host's flag now; true for a host that is gone.
	Production bool
	// Step is set on a runbook step's proposal.
	Step *ProposalStep
	// Source names who proposed it in keel.log, "" for the agent panel.
	Source string
	// Report is what was redacted from the text sent for it.
	Report redact.Report
}

// newProposal makes a proposal: a fresh id, sealed, and, when it has a
// pane to be typed into, logged as agent.suggest.
func (s *Service) newProposal(in proposalSpec, now time.Time) *Proposal {
	p := &Proposal{
		ID:        newID("p_"),
		SessionID: in.Pane.SessionID, HostID: in.Pane.HostID, Host: in.Pane.HostName, Production: in.Production,
		Command: in.Command, Why: in.Why, ProposedAt: &now, Runbook: in.Step,
	}
	p.seal(in.Floor)
	if p.SessionID != "" {
		s.audit.Suggest(in.Pane, in.Report, riskLabel(p.Risk), in.Source, agent.Timing{})
	}
	return p
}
