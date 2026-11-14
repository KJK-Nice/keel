package httpapi

import (
	"net/http"
	"strings"

	"keel/internal/store"
	"keel/internal/wire"
)

// The save helpers the JSON handlers call: each saves through the store, which checks what the caller sent, and
// logs one line.

// saveHost creates (h.ID "") or updates a host. Blank secrets keep the
// saved ones, and the store checks the fields (store.UpsertHost).
func (s *Server) saveHost(h store.Host) (store.Host, error) {
	isNew := h.ID == ""
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		return store.Host{}, err
	}
	if isNew {
		s.note(wire.LogHost, "host created: "+saved.Name, saved.ID, "", "")
	} else {
		s.note(wire.LogHost, "host updated: "+saved.Name, saved.ID, "", "")
	}
	return saved, nil
}

// saveIdentity creates (in.ID "") or updates an identity. Blank secrets
// keep the saved ones, and the store checks the fields
// (store.UpsertIdentity).
func (s *Server) saveIdentity(in store.Identity) (store.Identity, error) {
	isNew := in.ID == ""
	saved, err := s.store.UpsertIdentity(in)
	if err != nil {
		return store.Identity{}, err
	}
	if isNew {
		s.note(wire.LogIdentity, "identity created: "+saved.Name, "", "", "")
	} else {
		s.note(wire.LogIdentity, "identity updated: "+saved.Name, "", "", "")
	}
	return saved, nil
}

// saveTunnel creates (tn.ID "") or updates a forward. Store validation
// errors (ports, bind address, SOCKS exposure) are the user's to fix.
func (s *Server) saveTunnel(tn store.Tunnel) (store.Tunnel, error) {
	isNew := tn.ID == ""
	saved, err := s.store.UpsertTunnel(tn)
	if err != nil {
		return store.Tunnel{}, otherwise(err, http.StatusBadRequest)
	}
	if isNew {
		s.note(wire.LogTunnel, "tunnel created: "+saved.Name, saved.HostID, "", saved.ID)
	} else {
		s.note(wire.LogTunnel, "tunnel updated: "+saved.Name, saved.HostID, "", saved.ID)
	}
	return saved, nil
}

// saveSnippet creates (id "") or updates a snippet. A nil agentUsable keeps
// the saved flag.
func (s *Server) saveSnippet(id, name, command string, agentUsable *bool) (store.Snippet, error) {
	var old store.Snippet
	if id != "" {
		all, err := s.store.ListSnippets()
		if err != nil {
			return store.Snippet{}, err
		}
		for _, x := range all {
			if x.ID == id {
				old = x
			}
		}
	} else if strings.TrimSpace(command) == "" {
		return store.Snippet{}, badInput("name and command are required")
	}
	if strings.TrimSpace(name) == "" {
		if id != "" {
			return store.Snippet{}, badInput("Give the snippet a name")
		}
		return store.Snippet{}, badInput("name and command are required")
	}
	sn := store.Snippet{ID: id, Name: name, Command: command, AgentUsable: old.AgentUsable}
	if agentUsable != nil {
		sn.AgentUsable = *agentUsable
	}
	saved, err := s.store.UpsertSnippet(sn)
	if err != nil {
		return store.Snippet{}, err
	}
	if id == "" {
		s.note(wire.LogSnippet, "snippet created: "+saved.Name, "", "", "")
	} else {
		s.note(wire.LogSnippet, "snippet updated: "+saved.Name, "", "", "")
	}
	s.noteSnippetFlag(saved.Name, old.AgentUsable, saved.AgentUsable)
	return saved, nil
}
