package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"keel/internal/store"
)

// The save and validation helpers the JSON handlers call: each validates what the caller sent, saves through the
// store and logs one line.

// inputError is a problem with what the user typed: the API answers 400.
type inputError struct{ msg string }

func (e inputError) Error() string { return e.msg }

func badInput(msg string) error { return inputError{msg} }

func isInputError(err error) bool {
	var ie inputError
	return errors.As(err, &ie)
}

// writeSaveErr answers a JSON API save that failed: 400 for input errors,
// the store's mapping otherwise.
func writeSaveErr(w http.ResponseWriter, err error) {
	if isInputError(err) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeStoreErr(w, err)
}

// saveHost creates (h.ID "") or updates a host. Blank secrets keep the
// saved ones (store.UpsertHost).
func (s *Server) saveHost(h store.Host) (store.Host, error) {
	if err := validateHost(h); err != nil {
		return store.Host{}, badInput(err.Error())
	}
	isNew := h.ID == ""
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		return store.Host{}, err
	}
	if isNew {
		s.note("host", "host created: "+saved.Name, saved.ID, "", "")
	} else {
		s.note("host", "host updated: "+saved.Name, saved.ID, "", "")
	}
	return saved, nil
}

// saveGroup creates (id "") or updates a group.
func (s *Server) saveGroup(id, name, parentID string) (store.Group, error) {
	if strings.TrimSpace(name) == "" {
		return store.Group{}, badInput("name is required")
	}
	g := store.Group{Name: name, ParentID: parentID}
	if id != "" {
		for _, x := range s.mustGroups() {
			if x.ID == id {
				g = x
				g.Name, g.ParentID = name, parentID
			}
		}
	}
	g.ID = id
	saved, err := s.store.UpsertGroup(g)
	if err != nil {
		return store.Group{}, err
	}
	if id == "" {
		s.note("group", "group created: "+saved.Name, "", "", "")
	} else {
		s.note("group", "group updated: "+saved.Name, "", "", "")
	}
	return saved, nil
}

func (s *Server) mustGroups() []store.Group {
	gs, _ := s.store.ListGroups()
	return gs
}

// saveIdentity creates (in.ID "") or updates an identity. Blank secrets
// keep the saved ones (store.UpsertIdentity).
func (s *Server) saveIdentity(in store.Identity) (store.Identity, error) {
	if err := validateIdentity(in); err != nil {
		return store.Identity{}, badInput(err.Error())
	}
	isNew := in.ID == ""
	saved, err := s.store.UpsertIdentity(in)
	if err != nil {
		return store.Identity{}, err
	}
	if isNew {
		s.note("identity", "identity created: "+saved.Name, "", "", "")
	} else {
		s.note("identity", "identity updated: "+saved.Name, "", "", "")
	}
	return saved, nil
}

// saveTunnel creates (tn.ID "") or updates a forward. Store validation
// errors (ports, bind address, SOCKS exposure) are the user's to fix.
func (s *Server) saveTunnel(tn store.Tunnel) (store.Tunnel, error) {
	isNew := tn.ID == ""
	saved, err := s.store.UpsertTunnel(tn)
	if err != nil {
		if errors.Is(err, store.ErrLocked) || errors.Is(err, store.ErrNotFound) {
			return store.Tunnel{}, err
		}
		return store.Tunnel{}, badInput(err.Error())
	}
	if isNew {
		s.note("tunnel", "tunnel created: "+saved.Name, saved.HostID, "", saved.ID)
	} else {
		s.note("tunnel", "tunnel updated: "+saved.Name, saved.HostID, "", saved.ID)
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
		s.note("snippet", "snippet created: "+saved.Name, "", "", "")
	} else {
		s.note("snippet", "snippet updated: "+saved.Name, "", "", "")
	}
	s.noteSnippetFlag(saved.Name, old.AgentUsable, saved.AgentUsable)
	return saved, nil
}
