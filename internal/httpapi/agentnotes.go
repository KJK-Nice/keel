package httpapi

import (
	"net/http"

	"keel/internal/agent/service"
	"keel/internal/store"
)

// Notes for the agent (docs/ui-redesign.md §15): the user's CRUD on host
// notes and their own notes, and Save / Dismiss / Undo on a note the agent
// proposed. Every save goes through the agent service, which redacts the
// text for secrets before it is kept and logs agent.note; nothing here
// reads a pane or sends anything off the machine.

type noteBody struct {
	HostID string `json:"hostId"`
	// GroupID makes it a group note (§17).
	GroupID string `json:"groupId"`
	Text    string `json:"text"`
}

func decodeNote(w http.ResponseWriter, r *http.Request) (noteBody, bool) {
	var req noteBody
	if err := decode(w, r, bodyNote, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid note payload")
		return req, false
	}
	return req, true
}

func (s *Server) agentNotes(w http.ResponseWriter, _ *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, service.NotesList{Notes: []store.AgentNote{}})
		return
	}
	out, err := s.agent.ListNotes()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentAddNote(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNote(w, r)
	if !ok {
		return
	}
	var (
		out service.SavedNote
		err error
	)
	switch {
	case req.GroupID != "" && req.HostID != "":
		writeError(w, http.StatusBadRequest, "a note is for a host or a group, not both")
		return
	case req.GroupID != "":
		out, err = s.agent.AddGroupNote(req.GroupID, req.Text)
	default:
		out, err = s.agent.AddNote(req.HostID, req.Text)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentEditNote(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNote(w, r)
	if !ok {
		return
	}
	out, err := s.agent.EditNote(r.PathValue("id"), req.Text)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentDeleteNote(w http.ResponseWriter, r *http.Request) {
	if err := s.agent.DeleteNote(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentDeleteAllNotes(w http.ResponseWriter, _ *http.Request) {
	n, err := s.agent.DeleteAllNotes()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

// agentSaveNoteProposal saves a note the agent proposed, as the user saw
// it or with the user's edit.
func (s *Server) agentSaveNoteProposal(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNote(w, r)
	if !ok {
		return
	}
	out, err := s.agent.SaveNoteProposal(r.PathValue("id"), r.PathValue("nid"), req.Text)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentDismissNoteProposal(w http.ResponseWriter, r *http.Request) {
	out, err := s.agent.DismissNoteProposal(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentUndoNoteProposal(w http.ResponseWriter, r *http.Request) {
	out, err := s.agent.UndoNoteProposal(r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
