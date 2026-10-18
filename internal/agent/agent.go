// Package agent is the core of Keel's agent: which panes it may read
// (scope), how their text is read and redacted, who may insert its output,
// and the audit log of every read and insert. See docs/ui-redesign.md §6.
//
// The package has no way to write to a PTY: it does not import the
// runtime, session, inject or share packages (invariants_test.go checks).
// An insert helper only authorizes and logs; the user's own action (Type,
// ⇥, ⇧⇥) then writes through the existing approve-before-type path.
//
// It never sees vault contents. Hosts come in as Host values copied from
// store views (no password, key or passphrase field exists on them), and
// the only secret values it touches are the exact-match redaction list,
// which goes straight into a redact.Redactor.
package agent

import (
	"errors"
	"strings"

	"keel/internal/agent/redact"
	"keel/internal/store"
)

// Log event kinds in keel.log.
const (
	EventRead    = "agent.read"    // pane text read for a conversation
	EventSuggest = "agent.suggest" // a suggestion was produced for a pane
	EventInsert  = "agent.insert"  // a suggestion was inserted on the prompt line (⇥ / ⇧⇥), not run
	EventType    = "agent.type"    // a proposal was typed into a pane (Type into pane ↵)
	EventNote    = "agent.note"    // notes for the agent sent with a question, or saved, edited or deleted
	EventRunbook = "agent.runbook" // a runbook sent with a question, offered, used, changed, or a step's result
)

// Role is the client's role in a session. The values match
// runtime.Role (checked by a test) without importing the runtime.
type Role string

const (
	RoleOwner    Role = "owner"
	RoleDriver   Role = "driver"
	RoleFollower Role = "follower"
)

// Pane is one terminal pane as the agent sees it.
type Pane struct {
	SessionID string
	HostID    string
	HostName  string
	// Role is the asking client's role in this pane's session.
	Role Role
}

// Host is the part of a saved host the agent may know. It has no secret
// fields, so vault contents cannot reach the agent through it.
type Host struct {
	ID            string
	Name          string
	Address       string
	AgentReadable bool
	Production    bool
	// ProposeNotes is the host's "Let the agent propose notes".
	ProposeNotes bool
	// Group (its folder's name) and Tags decide which runbooks the agent
	// may pick for a pane on it (a runbook's Hosts).
	Group string
	Tags  []string
}

// HostsFrom copies the non-secret fields of store hosts.
func HostsFrom(hosts []store.Host) []Host {
	out := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, Host{
			ID:            h.ID,
			Name:          h.Name,
			Address:       h.Address,
			AgentReadable: h.ReadableByAgent(),
			Production:    h.IsProduction(),
			ProposeNotes:  h.ProposesNotes(),
			Group:         h.Group,
			Tags:          append([]string(nil), h.Tags...),
		})
	}
	return out
}

var (
	ErrNotReadable = errors.New("agent: this host's panes are not readable by the agent")
	ErrFollower    = errors.New("agent: a follower cannot insert into a session they follow")
	ErrNotDriving  = errors.New("agent: only the client driving the session can insert")
	ErrNoAuditLog  = errors.New("agent: no audit log")
)

// Scope decides which panes the agent may read: only panes whose host is
// saved with "Agent can read" on. A pane whose host is unknown is out.
type Scope struct {
	readable map[string]bool
}

// NewScope builds a scope from the saved hosts.
func NewScope(hosts []Host) Scope {
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		m[h.ID] = h.AgentReadable
	}
	return Scope{readable: m}
}

// Allows reports whether the agent may read p.
func (s Scope) Allows(p Pane) bool {
	return p.HostID != "" && s.readable[p.HostID]
}

// AllowsHost reports whether the agent may read what belongs to a host:
// its panes and its notes. An unknown host is out.
func (s Scope) AllowsHost(hostID string) bool {
	return hostID != "" && s.readable[hostID]
}

// Filter splits panes into the ones the agent may read and the rest, both
// in their original order (the panel shows "Reads N of M panes").
func (s Scope) Filter(panes []Pane) (in, out []Pane) {
	for _, p := range panes {
		if s.Allows(p) {
			in = append(in, p)
		} else {
			out = append(out, p)
		}
	}
	return in, out
}

// CanInsert reports whether a client with role may insert agent output
// into a pane. Only the driver can: a follower never gets the proposal
// (§6.6), and an owner who passed control is not driving.
func CanInsert(role Role) error {
	switch role {
	case RoleDriver:
		return nil
	case RoleFollower:
		return ErrFollower
	default:
		return ErrNotDriving
	}
}

// SecretSource lists the vault's secret values for exact-match redaction.
// *store.Store satisfies it.
type SecretSource interface {
	KnownSecretValues() ([]string, error)
}

// NewRedactor builds the Redactor for one request: secrets always, with
// the vault's values as exact matches; IPs and hostnames (including the
// saved host addresses) when the settings ask for them.
func NewRedactor(src SecretSource, opts store.AgentRedaction, hosts []Host) (*redact.Redactor, error) {
	known, err := src.KnownSecretValues()
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if a := strings.TrimSpace(h.Address); a != "" {
			addrs = append(addrs, a)
		}
	}
	return redact.New(redact.Options{
		IPs:       opts.IPs,
		Hostnames: opts.Hostnames,
		Known:     known,
		Hosts:     addrs,
	}), nil
}

// LastLines returns the last n lines of s (a trailing newline does not
// count as an empty line) and how many it returned.
func LastLines(s string, n int) (string, int) {
	if n <= 0 || s == "" {
		return "", 0
	}
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return "", 0
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), len(lines)
}

// ClampLines keeps a requested line count within the settings range.
func ClampLines(n int) int {
	switch {
	case n <= 0:
		return store.DefaultScrollbackLines
	case n < store.MinScrollbackLines:
		return store.MinScrollbackLines
	case n > store.MaxScrollbackLines:
		return store.MaxScrollbackLines
	}
	return n
}
