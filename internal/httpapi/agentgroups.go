package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"keel/internal/agent/service"
	"keel/internal/store"
)

// Groups, part 1 (docs/ui-redesign.md §17, boards GroupRoles and
// GroupRunbook): host roles in a group, and runbook runs across a group.
// Roles are config in keel.json (store.SetGroupRole). Group notes go
// through POST /api/agent/notes with a groupId (agentnotes.go). A group run
// is the agent service's: these routes start one, read it, make the current
// step's proposals and take the user's Retry / Skip / Mark done / Confirm.
// Nothing here reads a pane's text, sends anything off the machine or
// writes to a session: every host's step is a proposal typed only through
// the session socket's approve-before-type path, with a server nonce.

// groupMembers is GET /api/groups/{id}/members: the hosts of a group (its
// folder and the folders below), with their roles there.
func (s *Server) groupMembers(w http.ResponseWriter, r *http.Request) {
	g, members, err := s.store.GroupMembers(r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if members == nil {
		members = []store.GroupMember{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": g, "members": members})
}

// setGroupRole is PUT /api/groups/{id}/roles {hostId, role}: an empty role
// removes the host's role.
func (s *Server) setGroupRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HostID string `json:"hostId"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid role payload")
		return
	}
	g, err := s.store.SetGroupRole(r.PathValue("id"), req.HostID, req.Role)
	if err != nil {
		if errors.Is(err, store.ErrRole) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
	s.note("group", "host role set in group "+g.ID, req.HostID, "", "")
}

func (s *Server) agentGroupRuns(w http.ResponseWriter, _ *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, map[string]any{"runs": []service.GroupRun{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": s.agent.ListGroupRuns()})
}

func (s *Server) agentStartGroupRun(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req service.GroupRunRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid group run payload")
		return
	}
	out, err := s.agent.StartGroupRun(req)
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentGroupRun(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.GroupRunState(r.PathValue("id"))
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentProposeGroupStep makes the current step's due proposals for the
// panes the window has open (session ids; keel serve resolves their hosts).
func (s *Server) agentProposeGroupStep(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		Panes []string `json:"panes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if len(req.Panes) > 256 {
		writeError(w, http.StatusBadRequest, "too many panes")
		return
	}
	out, err := s.agent.ProposeGroupStep(r.PathValue("id"), req.Panes)
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentGroupHostAction is Retry, Skip, Mark done or Confirm on a host of
// the current step, or Mark done / Skip on the step.
func (s *Server) agentGroupHostAction(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		HostID string `json:"hostId"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	out, err := s.agent.GroupHostAction(r.PathValue("id"), r.PathValue("sid"), req.HostID, req.Action)
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentGroupRunMode(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	out, err := s.agent.SetGroupRunMode(r.PathValue("id"), req.Mode)
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentStopGroupRun(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.StopGroupRun(r.PathValue("id"))
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeGroupRunErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNoGroupRun):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrModeLocked), errors.Is(err, service.ErrTyping), errors.Is(err, service.ErrResultPending), errors.Is(err, service.ErrTooManyRuns):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeRunbookErr(w, err)
	}
}
