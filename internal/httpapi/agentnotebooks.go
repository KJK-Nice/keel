package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"keel/internal/agent/service"
)

// Notebook drafts the agent proposes (docs/ui-redesign.md §18): Open draft
// on the card, Save in the notebook editor, Dismiss and Undo. Every save
// goes through the agent service, which takes the notebook's origin from
// its own record of the draft (never from the client), redacts the text
// for secrets before it is kept and logs agent.notebook. Nothing here
// reads a pane, sends anything off the machine or writes to a session: a
// fence in a draft is typed only through the notebook's approve-before-
// type path.

const maxNotebookSaveBody = 512 * 1024

// agentOpenNotebookDraft is the card's Open draft: the draft for the
// editor and the draftId its Save must name. Nothing is saved.
func (s *Server) agentOpenNotebookDraft(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.OpenNotebookDraft(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeNotebookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentSaveNotebookDraft is Save in the editor on a draft: a new notebook,
// from what the editor holds now.
func (s *Server) agentSaveNotebookDraft(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var in service.NotebookSaveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxNotebookSaveBody)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid notebook payload")
		return
	}
	out, err := s.agent.SaveNotebookDraft(r.PathValue("draft"), in)
	if err != nil {
		writeNotebookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentDismissNotebookProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.DismissNotebookProposal(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeNotebookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentUndoNotebookProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := s.agent.UndoNotebookProposal(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeNotebookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeNotebookErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotebookClosed):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNotebookExpired), errors.Is(err, service.ErrNoDraft):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, service.ErrNoNotebooks):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeAgentErr(w, err)
	}
}
