package keelclient

import (
	"strings"

	"keel/internal/limits"
)

// What a Host says about itself, decided once. keel serve stores the defaults (a port of 0 on a saved SSH host is 22, an
// unset agentReadable is on), but a host that came from an older file or a quick connect may still carry the zero value.

// IsLocal reports the local shell. keel serve sends authType "local" for it and never "" for a saved host, so "" is not
// local.
func (h Host) IsLocal() bool { return h.AuthType == "local" }

// SSHPort is the port the host is reached on: its own, or 22 when none is set.
func (h Host) SSHPort() int {
	if h.Port == 0 {
		return limits.DefaultPort
	}
	return h.Port
}

// IsProduction reports a host marked production (it is not by default).
func (h Host) IsProduction() bool { return h.Production != nil && *h.Production }

// AgentMayRead reports whether the agent may read the host's panes (it may unless the user turned it off).
func (h Host) AgentMayRead() bool { return h.AgentReadable == nil || *h.AgentReadable }

// AgentMayNote reports whether the agent may keep notes about the host (it may unless the user turned it off).
func (h Host) AgentMayNote() bool { return h.AgentNotes == nil || *h.AgentNotes }

// BracketIPv6 puts [] around an IPv6 address (one with a colon that is not bracketed yet), as ssh and a link read it.
func BracketIPv6(addr string) string {
	if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") {
		return "[" + addr + "]"
	}
	return addr
}

// UserAtAddress is the host's "user@address" as ssh reads it: an IPv6 address gets brackets, and an empty user leaves the
// address alone.
func (h Host) UserAtAddress() string {
	addr := BracketIPv6(strings.TrimSpace(h.Address))
	if u := strings.TrimSpace(h.Username); u != "" {
		return u + "@" + addr
	}
	return addr
}
