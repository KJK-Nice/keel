package ui

import "keel/internal/store"

// Host is what the vault views show of a saved host. It has no secret
// fields, so no template can render one; httpapi fills PageProps.Hosts
// with HostsFrom, the one place a store.Host becomes a view.
type Host struct {
	ID         string
	Name       string
	Group      string
	GroupID    string
	Address    string
	Port       int
	Username   string
	AuthType   string // local | password | key | identity
	IdentityID string
	JumpHost   string
	Color      string
	Tags       []string
	// AgentOff is true when the agent may not read this host's panes; the
	// zero Host is readable, as a host that never set it is.
	AgentOff bool
}

// ReadableByAgent reports whether the agent may read this host's panes.
func (h Host) ReadableByAgent() bool { return !h.AgentOff }

// HostFrom is the view of h.
func HostFrom(h store.Host) Host {
	return Host{
		ID: h.ID, Name: h.Name, Group: h.Group, GroupID: h.GroupID,
		Address: h.Address, Port: h.Port, Username: h.Username,
		AuthType: h.AuthType, IdentityID: h.IdentityID, JumpHost: h.JumpHost,
		Color: h.Color, Tags: append([]string(nil), h.Tags...),
		AgentOff: !h.ReadableByAgent(),
	}
}

// HostsFrom is the view of every host in hosts, in order.
func HostsFrom(hosts []store.Host) []Host {
	out := make([]Host, len(hosts))
	for i, h := range hosts {
		out[i] = HostFrom(h)
	}
	return out
}
