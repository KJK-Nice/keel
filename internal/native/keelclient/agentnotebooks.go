package keelclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Notebooks and the agent (internal/httpapi/agentnotebooks.go; docs/ui-redesign.md §18, §6 rule 12). Every call here is a
// request: keel serve changes the notebook, keeps the version before it and answers with the card as it now stands. This
// client changes no notebook itself, sends notebook ids and never notebook text, and reads no pane.

// NotebookProposal is one card in the conversation: what the agent did to a notebook, or a delete it proposed
// (service.NotebookProposal).
type NotebookProposal struct {
	ID string `json:"id"`
	// Kind is "create", "edit" or "delete"; "" is a draft from a conversation saved by an older Keel (it can no longer
	// be saved).
	Kind string `json:"kind"`
	// Status: created, edited, refused (Reason says why), undone, proposed (a delete waiting for the user), deleted,
	// dismissed, restored; a legacy draft: open, saved or dismissed.
	Status     string `json:"status"`
	NotebookID string `json:"notebookId"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	// Reason is why an edit was refused: conflict, editing, private, not-read, not-found, deleted, no-match, redacted,
	// truncated, too-large, history or invalid.
	Reason string `json:"reason"`
	Rev    int    `json:"rev"`
	// VersionID is the version keel serve kept: before an edit (Undo restores it) or the deleted notebook (Restore).
	VersionID    string     `json:"versionId"`
	Bytes        int        `json:"bytes"`
	Placeholders bool       `json:"placeholders"`
	Withheld     bool       `json:"withheld"`
	FromPane     bool       `json:"fromPane"`
	SeenIn       string     `json:"seenIn"`
	PanesRead    []string   `json:"panesRead"`
	ProposedAt   *time.Time `json:"proposedAt"`
	Expired      bool       `json:"expired"`
	// Body is a legacy draft's text, shown as it was; empty otherwise.
	Body string `json:"body"`
	// Record is the notebook after this change, in a live answer only (never in a saved conversation).
	Record *Notebook `json:"record"`
}

// NotebookAction is a card action's answer: the card, and the notebook after it when the action changed one.
type NotebookAction struct {
	Proposal NotebookProposal `json:"proposal"`
	Notebook *Notebook        `json:"notebook"`
}

func notebookProposalPath(conversationID, id string) string {
	return "/api/agent/conversations/" + url.PathEscape(conversationID) + "/notebooks/" + url.PathEscape(id)
}

func (c *Client) notebookAction(ctx context.Context, method, path string) (out NotebookAction, err error) {
	err = c.do(ctx, method, path, nil, &out)
	return out, err
}

// UndoNotebookChange is POST …/notebooks/{nid}/undo, a card's Undo: an agent edit goes back to the version before it, a
// notebook the agent created goes to Recently deleted.
func (c *Client) UndoNotebookChange(ctx context.Context, conversationID, id string) (NotebookAction, error) {
	return c.notebookAction(ctx, http.MethodPost, notebookProposalPath(conversationID, id)+"/undo")
}

// ConfirmNotebookDelete is POST …/notebooks/{nid}/delete: the one way a delete the agent proposed happens. Call it only
// after the user confirmed.
func (c *Client) ConfirmNotebookDelete(ctx context.Context, conversationID, id string) (NotebookAction, error) {
	return c.notebookAction(ctx, http.MethodPost, notebookProposalPath(conversationID, id)+"/delete")
}

// DismissNotebookProposal is DELETE …/notebooks/{nid}, Keep on a delete card: nothing is deleted.
func (c *Client) DismissNotebookProposal(ctx context.Context, conversationID, id string) (NotebookAction, error) {
	return c.notebookAction(ctx, http.MethodDelete, notebookProposalPath(conversationID, id))
}

// RestoreNotebookChange is POST …/notebooks/{nid}/restore: brings back a notebook a card deleted.
func (c *Client) RestoreNotebookChange(ctx context.Context, conversationID, id string) (NotebookAction, error) {
	return c.notebookAction(ctx, http.MethodPost, notebookProposalPath(conversationID, id)+"/restore")
}

// DraftRunbookFromNotebook is POST /api/agent/runbooks/draft with {notebookId}: keel serve makes an unsaved runbook from
// the notebook (its headings become steps, shell fences their commands) and keeps nothing; a 422 means there was nothing
// to make steps from. The notebook's text never leaves keel serve.
func (c *Client) DraftRunbookFromNotebook(ctx context.Context, notebookID string) (out RunbookDraft, err error) {
	err = c.do(ctx, http.MethodPost, "/api/agent/runbooks/draft", struct {
		Notebook string `json:"notebookId"`
	}{notebookID}, &out)
	return out, err
}
