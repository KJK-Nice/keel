package httpapi

import (
	"errors"
	"io"
	"net/http"

	"keel/internal/agent/service"
	"keel/internal/store"
	"keel/internal/wire"
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
	out, err := act(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeErr(w, err)
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
	if err := decode(w, r, bodySwitch, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	nb, err := s.store.SetNotebookPrivate(r.PathValue("id"), in.Private)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
	what := "notebook made private (the agent cannot read or change it) · "
	if !in.Private {
		what = "notebook no longer private (the agent may read and change it) · "
	}
	s.note(wire.LogNotebook, what+nb.ID, "", "", "")
}

// markNotebookReviewed clears the "edited by the agent" mark.
func (s *Server) markNotebookReviewed(w http.ResponseWriter, r *http.Request) {
	nb, err := s.store.MarkNotebookReviewed(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
}

// historyWords are History's own words for a notebook that changed while
// the user looked at an older version.
func historyWords(err error) error {
	return worded(err, store.ErrNotebookConflict, "The notebook changed meanwhile. Look at History again before you restore.")
}

// notebookHistory lists a notebook's kept versions, newest first.
func (s *Server) notebookHistory(w http.ResponseWriter, r *http.Request) {
	vs, err := s.store.NotebookHistory(r.PathValue("id"))
	if err != nil {
		writeErr(w, historyWords(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": vs})
}

// notebookVersion is one kept version with its text, for View and diff.
func (s *Server) notebookVersion(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.NotebookVersionByID(r.PathValue("id"), r.PathValue("vid"))
	if err != nil {
		writeErr(w, historyWords(err))
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
	if err := decode(w, r, bodySwitch, &in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	rev := -1
	if in.Rev != nil {
		rev = *in.Rev
	}
	nb, err := s.store.RestoreNotebookVersion(r.PathValue("id"), r.PathValue("vid"), rev)
	if err != nil {
		writeErr(w, historyWords(err))
		return
	}
	writeJSON(w, http.StatusOK, nb)
	s.note(wire.LogNotebook, "notebook version restored by you · "+nb.ID+" · "+r.PathValue("vid"), "", "", "")
}

// deletedNotebooks is Recently deleted.
func (s *Server) deletedNotebooks(w http.ResponseWriter, _ *http.Request) {
	vs, err := s.store.DeletedNotebooks()
	if err != nil {
		writeErr(w, historyWords(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": vs})
}

func (s *Server) restoreDeletedNotebook(w http.ResponseWriter, r *http.Request) {
	nb, err := s.store.RestoreDeletedNotebook(r.PathValue("vid"))
	if err != nil {
		writeErr(w, historyWords(err))
		return
	}
	writeJSON(w, http.StatusOK, nb)
	s.note(wire.LogNotebook, "deleted notebook restored by you · "+nb.ID, "", "", "")
}

// patchNotebook renames or pins a notebook without its text: a window with
// an old copy cannot write old cells back (store.UpdateNotebookMeta).
func (s *Server) patchNotebook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   *string `json:"name"`
		Pinned *bool   `json:"pinned"`
	}
	if err := decode(w, r, bodyLine, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	nb, err := s.store.UpdateNotebookMeta(r.PathValue("id"), in.Name, in.Pinned)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nb)
}

// deletedNotebook is one entry of Recently deleted with its text.
func (s *Server) deletedNotebook(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.DeletedNotebookByID(r.PathValue("vid"))
	if err != nil {
		writeErr(w, historyWords(err))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// purgeDeletedNotebook is Delete forever in Recently deleted.
func (s *Server) purgeDeletedNotebook(w http.ResponseWriter, r *http.Request) {
	if err := s.store.PurgeNotebookVersion("", r.PathValue("vid")); err != nil {
		writeErr(w, historyWords(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note(wire.LogNotebook, "deleted notebook removed for good by you · "+r.PathValue("vid"), "", "", "")
}

// purgeNotebookVersion is Delete this version in History.
func (s *Server) purgeNotebookVersion(w http.ResponseWriter, r *http.Request) {
	if err := s.store.PurgeNotebookVersion(r.PathValue("id"), r.PathValue("vid")); err != nil {
		writeErr(w, historyWords(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note(wire.LogNotebook, "notebook version removed by you · "+r.PathValue("id")+" · "+r.PathValue("vid"), "", "", "")
}
