package service

import (
	"sort"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/store"
)

// Every Redactor the service uses is built here, for a named purpose
// (docs/ui-redesign.md §6 rules 3, 4 and 11). A call site says what it
// redacts for; what that means (the user's settings, the vault, the values
// Keel withholds) is decided once, in redactShapes. Every Redactor holds
// the vault's values and the secret detectors: there is no purpose
// without them.

// redactFor is what a Redactor is for.
type redactFor int

const (
	// forRequest: text that leaves the machine, to a model, to Jev or to an
	// outside agent.
	forRequest redactFor = iota
	// forKept: text Keel keeps and may send again (a saved conversation's
	// turns and titles, a proposal's reason).
	forKept
	// forKeptCommand: a command the user will type, as kept in the
	// transcript (an edited proposal, an outside agent's proposal).
	forKeptCommand
	// forSave: a note's or a runbook's text when the user saves it.
	forSave
	// forSaveCommand: a runbook step's command when the user saves it.
	forSaveCommand
	// forVaultCheck: asking whether a value is, holds or is part of a
	// secret (a group variable, a captured value, a filled command). Its
	// output is never kept or sent.
	forVaultCheck
)

// redactShape is what a purpose redacts besides secrets, which are always
// redacted.
type redactShape struct {
	// settings: the user's choices in Settings · Agent (IP addresses,
	// hostnames) and the saved hosts' addresses. Text the user saves for
	// later keeps them: a note may need an address, and the request's
	// Redactor replaces it anyway when it is sent.
	settings bool
	// captured: every value a group run captured becomes its {{name}}.
	captured bool
	// variables: every group variable's value becomes its {{name}}. A
	// command the user will type keeps them: a group variable is config,
	// not a secret, and the command must stay typeable.
	variables bool
}

var redactShapes = map[redactFor]redactShape{
	forRequest:     {settings: true, captured: true, variables: true},
	forKept:        {settings: true, captured: true, variables: true},
	forKeptCommand: {settings: true, captured: true},
	forSave:        {captured: true, variables: true},
	forSaveCommand: {captured: true},
	forVaultCheck:  {},
}

// redactorFor builds the Redactor for a purpose, reading the settings and
// the hosts when the purpose uses them.
func (s *Service) redactorFor(p redactFor) (*redact.Redactor, error) {
	if !redactShapes[p].settings {
		return s.buildRedactor(p, store.AgentRedaction{}, nil)
	}
	set, err := s.st.AgentSettings()
	if err != nil {
		return nil, err
	}
	hosts, err := s.st.ListHosts()
	if err != nil {
		return nil, err
	}
	return s.buildRedactor(p, set.Redact, agent.HostsFrom(hosts))
}

// requestRedactor is redactorFor(forRequest) for a request that has read
// the settings and the hosts already.
func (s *Service) requestRedactor(opts store.AgentRedaction, hosts []agent.Host) (*redact.Redactor, error) {
	return s.buildRedactor(forRequest, opts, hosts)
}

// buildRedactor is the one place a Redactor is made. It fails, and
// nothing is redacted or sent, when the vault's values or the values Keel
// withholds cannot be read.
func (s *Service) buildRedactor(p redactFor, opts store.AgentRedaction, hosts []agent.Host) (*redact.Redactor, error) {
	shape := redactShapes[p]
	named, err := s.namedValues(shape)
	if err != nil {
		return nil, err
	}
	return agent.NewRedactor(s.st, opts, hosts, named...)
}

// namedValues lists the values replaced by their {{name}}: every value a
// group run captured since keel serve started (the redact-only set, which
// outlives the runs), and every group variable's value.
func (s *Service) namedValues(shape redactShape) ([]redact.Named, error) {
	var out []redact.Named
	if shape.captured {
		s.mu.Lock()
		for value, name := range s.withheld {
			out = append(out, redact.Named{Name: name, Value: value, Kind: redact.KindCaptured})
		}
		s.mu.Unlock()
		sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	}
	if !shape.variables {
		return out, nil
	}
	vs, ok := s.st.(GroupVarStore)
	if !ok {
		return out, nil
	}
	// A read that fails must not leave the values out.
	all, err := vs.AllGroupVars()
	if err != nil {
		return nil, err
	}
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
	return out, nil
}
