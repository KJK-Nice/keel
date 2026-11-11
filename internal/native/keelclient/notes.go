package keelclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Notes for the agent (docs/ui-redesign.md §15; internal/httpapi/agentnotes.go, internal/agent/service/notes.go): the
// user's own CRUD on host notes and on notes about themselves, and Save / Dismiss / Undo on a note the agent proposed.
// keel serve redacts every note for secrets before it keeps it and logs agent.note; this client sends the text the user
// typed, shows what keel serve kept (and how many values it replaced) and keeps nothing: no note is written to disk
// here. A proposed note is kept only by SaveProposedNote, which the card's Save button calls; nothing calls it on its own.

// Note is a saved note. A note has a HostID (a host note), a GroupID (a group note, §17: not built here) or neither
// (the user's own).
type Note struct {
	ID      string `json:"id"`
	HostID  string `json:"hostId"`
	GroupID string `json:"groupId"`
	Text    string `json:"text"`
	// Source is "user" or "agent" (proposed by the agent, saved by the user).
	Source    string    `json:"source"`
	FromPane  bool      `json:"fromPane"`
	SeenIn    string    `json:"seenIn"`
	Edited    bool      `json:"edited"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// NoteList is GET /api/agent/notes. Unreadable is set when keel.memory cannot be read: Delete all notes starts a
// fresh file.
type NoteList struct {
	Notes      []Note `json:"notes"`
	Unreadable bool   `json:"unreadable"`
}

// NoteProposal is a note the agent proposed, as the card shows it. Nothing is kept until the user saves it.
type NoteProposal struct {
	ID     string `json:"id"`
	For    string `json:"for"` // "host" or "user"
	HostID string `json:"hostId"`
	Host   string `json:"host"`
	Text   string `json:"text"`
	// From is what the model said it came from: "pane", "user" or "conversation".
	From string `json:"from"`
	// FromPane is set when the text came from terminal output; SeenIn names that pane's host. PanesRead names the hosts
	// read for the answer that proposed it: any of them may be where the text came from.
	FromPane   bool       `json:"fromPane"`
	SeenIn     string     `json:"seenIn"`
	PanesRead  []string   `json:"panesRead"`
	Status     string     `json:"status"` // open | saved | dismissed
	NoteID     string     `json:"noteId"`
	ProposedAt *time.Time `json:"proposedAt"`
	Expired    bool       `json:"expired"` // a proposal keel serve no longer holds: it can be read, not saved
}

// SavedNote is a note after a save: Redacted counts the values keel serve replaced before keeping it. Proposal is the
// proposal's new state when the note came from one.
type SavedNote struct {
	Note     Note          `json:"note"`
	Redacted int           `json:"redacted"`
	Proposal *NoteProposal `json:"proposal"`
}

// NoteInput is what the user typed: Text, for the host HostID or, with no HostID, for the user. ID is set to edit.
type NoteInput struct {
	ID     string
	HostID string
	Text   string
}

// Notes is GET /api/agent/notes.
func (c *Client) Notes(ctx context.Context) (out NoteList, err error) {
	err = c.do(ctx, http.MethodGet, "/api/agent/notes", nil, &out)
	return out, err
}

// SaveNote is POST /api/agent/notes (a new note) or PUT /api/agent/notes/{id} (the text of one). The text is the
// user's; keel serve cleans and redacts it and the answer is what it kept.
func (c *Client) SaveNote(ctx context.Context, in NoteInput) (out SavedNote, err error) {
	if in.ID != "" {
		err = c.do(ctx, http.MethodPut, "/api/agent/notes/"+url.PathEscape(in.ID), map[string]string{"text": in.Text}, &out)
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/api/agent/notes", map[string]string{"hostId": in.HostID, "groupId": "", "text": in.Text}, &out)
	return out, err
}

// DeleteNote is DELETE /api/agent/notes/{id}.
func (c *Client) DeleteNote(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/agent/notes/"+url.PathEscape(id), nil, nil)
}

// DeleteAllNotes is DELETE /api/agent/notes: every host note and every note about the user. Call it only after the user
// confirmed. It returns how many were deleted.
func (c *Client) DeleteAllNotes(ctx context.Context) (int, error) {
	var out struct {
		Deleted int `json:"deleted"`
	}
	err := c.do(ctx, http.MethodDelete, "/api/agent/notes", nil, &out)
	return out.Deleted, err
}

func noteProposalPath(conversationID, noteID string) string {
	return "/api/agent/conversations/" + url.PathEscape(conversationID) + "/notes/" + url.PathEscape(noteID)
}

// SaveProposedNote is POST /api/agent/conversations/{id}/notes/{nid}: the one request that keeps a note the agent
// proposed. text is the user's edit, or "" to keep it as shown. Only the card's Save calls it.
func (c *Client) SaveProposedNote(ctx context.Context, conversationID, noteID, text string) (out SavedNote, err error) {
	err = c.do(ctx, http.MethodPost, noteProposalPath(conversationID, noteID), map[string]string{"text": text}, &out)
	return out, err
}

// UndoProposedNote is POST …/notes/{nid}/undo: it deletes the note a Save kept and reopens the proposal.
func (c *Client) UndoProposedNote(ctx context.Context, conversationID, noteID string) (out NoteProposal, err error) {
	err = c.do(ctx, http.MethodPost, noteProposalPath(conversationID, noteID)+"/undo", nil, &out)
	return out, err
}

// DismissProposedNote is DELETE …/notes/{nid}: the proposal is closed and nothing is kept.
func (c *Client) DismissProposedNote(ctx context.Context, conversationID, noteID string) (out NoteProposal, err error) {
	err = c.do(ctx, http.MethodDelete, noteProposalPath(conversationID, noteID), nil, &out)
	return out, err
}
