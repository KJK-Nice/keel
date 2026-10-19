package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"keel/internal/agent/service"
	"keel/internal/store"
)

// Notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12).
//
// The agent changes notebooks only inside an Ask, through the agent
// service and the store's Agent* methods (never a private notebook). The
// routes here are the user's: Undo on a card, the confirmed Delete of a
// delete the agent proposed (the only way such a delete happens), Keep,
// and Restore; and on the notebooks themselves Private, Mark reviewed,
// History (list, view, restore) and Recently deleted. Nothing here reads a
// pane, sends anything off the machine or writes to a session: a fence in
// a notebook is typed only through the notebook's approve-before-type
// path.

func (s *Server) agentNotebookAction(w http.ResponseWriter, r *http.Request, act func(conv, id string) (service.NotebookAction, error)) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	out, err := act(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeNotebookErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentUndoNotebookChange is a card's Undo: an agent edit restored to the
// version before it, a notebook the agent created moved to Recently
// deleted.
func (s *Server) agentUndoNotebookChange(w http.ResponseWriter, r *http.Request) {
	s.agentNotebookAction(w, r, func(c, id string) (service.NotebookAction, error) { return s.agent.UndoNotebookChange(c, id) })
}

// agentConfirmNotebookDelete is the user's confirmed Delete on a delete
// the agent proposed.
func (s *Server) agentConfirmNotebookDelete(w http.ResponseWriter, r *http.Request) {
	s.agentNotebookAction(w, r, func(c, id string) (service.NotebookAction, error) { return s.agent.ConfirmNotebookDelete(c, id) })
}

// agentDismissNotebookProposal is Keep on a delete card.
func (s *Server) agentDismissNotebookProposal(w http.ResponseWriter, r *http.Request) {
	s.agentNotebookAction(w, r, func(c, id string) (service.NotebookAction, error) { return s.agent.DismissNotebookProposal(c, id) })
}

// agentRestoreNotebookChange brings back a notebook a card deleted.
func (s *Server) agentRestoreNotebookChange(w http.ResponseWriter, r *http.Request) {
	s.agentNotebookAction(w, r, func(c, id string) (service.NotebookAction, error) { return s.agent.RestoreNotebookChange(c, id) })
}

func writeNotebookErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotebookClosed):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotebookConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "The notebook changed since then. Open History on the notebook to restore an earlier version.", "reason": "conflict"})
	case errors.Is(err, store.ErrNotebookPrivate):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "This notebook is private now. Keel did not change it.", "reason": "private"})
	case errors.Is(err, service.ErrNotebookExpired):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, store.ErrHistoryUnavailable):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNoNotebooks):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeAgentErr(w, err)
	}
}

// --- the user's own notebook routes ---

// savedNotebook is a save's answer: the notebook, and whether the save
// replaced an agent edit the editor had not loaded (that edit is kept in
// History).
type savedNotebook struct {
	store.Notebook
	ReplacedAgentEdit bool `json:"replacedAgentEdit,omitempty"`
}

// setNotebookPrivate is the user's Private switch. The agent has no route
// to it: an agent change goes through the agent service, which never sets
// it.
func (s *Server) setNotebookPrivate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Private bool `json:"private"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	nb, err := s.store.SetNotebookPrivate(r.PathValue("id"), in.Private)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
	what := "notebook made private (the agent cannot read or change it) · "
	if !in.Private {
		what = "notebook no longer private (the agent may read and change it) · "
	}
	s.note("notebook", what+nb.ID, "", "", "")
}

// markNotebookReviewed clears the "edited by the agent" mark.
func (s *Server) markNotebookReviewed(w http.ResponseWriter, r *http.Request) {
	nb, err := s.store.MarkNotebookReviewed(r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
}

func writeHistoryErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrHistoryUnavailable):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, store.ErrNotebookConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "The notebook changed meanwhile. Look at History again before you restore.", "reason": "conflict"})
	default:
		writeStoreErr(w, err)
	}
}

// notebookHistory lists a notebook's kept versions, newest first.
func (s *Server) notebookHistory(w http.ResponseWriter, r *http.Request) {
	vs, err := s.store.NotebookHistory(r.PathValue("id"))
	if err != nil {
		writeHistoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": vs})
}

// notebookVersion is one kept version with its text, for View and diff.
func (s *Server) notebookVersion(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.NotebookVersionByID(r.PathValue("id"), r.PathValue("vid"))
	if err != nil {
		writeHistoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// restoreNotebookVersion is History's Restore: rev is the revision the
// page shows, so a restore over a newer change is refused.
func (s *Server) restoreNotebookVersion(w http.ResponseWriter, r *http.Request) {
	in := struct {
		Rev *int `json:"rev"`
	}{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	rev := -1
	if in.Rev != nil {
		rev = *in.Rev
	}
	nb, err := s.store.RestoreNotebookVersion(r.PathValue("id"), r.PathValue("vid"), rev)
	if err != nil {
		writeHistoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
	s.note("notebook", "notebook version restored by you · "+nb.ID+" · "+r.PathValue("vid"), "", "", "")
}

// deletedNotebooks is Recently deleted.
func (s *Server) deletedNotebooks(w http.ResponseWriter, _ *http.Request) {
	vs, err := s.store.DeletedNotebooks()
	if err != nil {
		writeHistoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": vs})
}

func (s *Server) restoreDeletedNotebook(w http.ResponseWriter, r *http.Request) {
	nb, err := s.store.RestoreDeletedNotebook(r.PathValue("vid"))
	if err != nil {
		writeHistoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
	s.note("notebook", "deleted notebook restored by you · "+nb.ID, "", "", "")
}
