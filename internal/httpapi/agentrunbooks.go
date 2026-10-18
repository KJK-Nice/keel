package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"keel/internal/agent/service"
	"keel/internal/store"
)

// Runbooks (docs/ui-redesign.md §16): the Runbooks page's list and editor,
// drafts from a notebook or a conversation, Save / Dismiss / Undo on a
// runbook the agent proposed, and a conversation's runbook in use (its
// state, a step marked done, skipped or retried, Don't use). Every save
// goes through the agent service, which redacts the text for secrets
// before it is kept and logs agent.runbook. Nothing here reads a pane,
// sends anything off the machine or writes to a session: a step is typed
// only as a proposal, through the session socket's approve-before-type
// path.

const maxRunbookBody = 256 * 1024

func (s *Server) agentRunbooks(w http.ResponseWriter, _ *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, service.RunbooksList{Runbooks: []service.RunbookView{}})
		return
	}
	out, err := s.agent.ListRunbooks()
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func decodeRunbook(w http.ResponseWriter, r *http.Request) (service.RunbookInput, bool) {
	var in service.RunbookInput
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRunbookBody)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid runbook payload")
		return in, false
	}
	return in, true
}

func (s *Server) agentAddRunbook(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	in, ok := decodeRunbook(w, r)
	if !ok {
		return
	}
	out, err := s.agent.AddRunbook(in)
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentUpdateRunbook(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	in, ok := decodeRunbook(w, r)
	if !ok {
		return
	}
	out, err := s.agent.UpdateRunbook(r.PathValue("id"), in)
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentRunbookPick is the list's "Agent may pick" switch.
func (s *Server) agentRunbookPick(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		AgentMayPick bool `json:"agentMayPick"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	out, err := s.agent.SetRunbookAgentMayPick(r.PathValue("id"), req.AgentMayPick)
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentDeleteRunbook(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	if err := s.agent.DeleteRunbook(r.PathValue("id")); err != nil {
		writeRunbookErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentRunbookDraft makes an unsaved runbook from a notebook or a
// conversation. Nothing is kept until the user saves it.
func (s *Server) agentRunbookDraft(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req service.DraftRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid draft payload")
		return
	}
	out, err := s.agent.RunbookDraft(req)
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentSaveRunbookProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.SaveRunbookProposal(r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentDismissRunbookProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.DismissRunbookProposal(r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentUndoRunbookProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.UndoRunbookProposal(r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentRunState is a conversation's runbook in use, after looking for the
// results of typed steps in their panes' shell marks (reported, never
// proof).
func (s *Server) agentRunState(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, service.RunState{})
		return
	}
	out, err := s.agent.RunbookState(r.PathValue("id"))
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentStepAction is Mark done, Skip or Retry on the current step.
func (s *Server) agentStepAction(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid step payload")
		return
	}
	out, err := s.agent.StepAction(r.PathValue("id"), r.PathValue("sid"), req.Action)
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentStopRunbook is Don't use.
func (s *Server) agentStopRunbook(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.StopRunbook(r.PathValue("id"))
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentMoveRunbook unpins the runbook in use from its host (the user's
// "Move the run"): the next step may go to another host.
func (s *Server) agentMoveRunbook(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.MoveRun(r.PathValue("id"))
	if err != nil {
		writeRunbookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeRunbookErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrRunbookClosed), errors.Is(err, service.ErrStepNotCurrent), errors.Is(err, service.ErrNoRun):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrRunbookExpired):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, service.ErrNothingToDraft):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrNoRunbooks):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, store.ErrMemoryUnreadable):
		writeError(w, http.StatusConflict, fmt.Sprintf("%s; Delete all notes in Settings · Agent starts a fresh file", err.Error()))
	default:
		writeAgentErr(w, err)
	}
}
