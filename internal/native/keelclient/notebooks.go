package keelclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Notebooks (internal/httpapi/httpapi.go and agentnotebooks.go; store.Notebook): one Markdown document each. The window
// reads one rendered and edits it as plain Markdown; the text is stored as it was typed. Nothing here writes to a session:
// a fence reaches a terminal only through the same two paths as a snippet (DirectType with Kind "notebook", or
// ApprovedInject with FromNotebook).

// NotebookCell is one cell of a notebook. The window keeps one cell, whose body is the whole document.
type NotebookCell struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

// Notebook is a notebook as keel serve keeps it (store.Notebook).
type Notebook struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Cells           []NotebookCell `json:"cells"`
	Pinned          bool           `json:"pinned"`
	Source          string         `json:"source"` // "" for the user's own, "agent" for one the agent made
	Private         bool           `json:"private"`
	Rev             int            `json:"rev"`
	LastBy          string         `json:"lastBy"` // "agent" when the agent made the latest change
	AgentEditedAt   *time.Time     `json:"agentEditedAt"`
	AgentUnreviewed bool           `json:"agentUnreviewed"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

// Markdown is the document: the cells' bodies joined with a blank line.
func (n Notebook) Markdown() string {
	parts := make([]string, len(n.Cells))
	for i, c := range n.Cells {
		parts[i] = c.Body
	}
	return strings.Join(parts, "\n\n")
}

// WithMarkdown is n with its text set to md in one cell, keeping the first cell's id.
func (n Notebook) WithMarkdown(md string) Notebook {
	id := ""
	if len(n.Cells) > 0 {
		id = n.Cells[0].ID
	}
	n.Cells = []NotebookCell{{ID: id, Body: md}}
	return n
}

// SavedNotebook is the answer to a save: the notebook, and whether the save replaced an agent edit this editor had not
// loaded (that edit is kept in History).
type SavedNotebook struct {
	Notebook
	ReplacedAgentEdit bool `json:"replacedAgentEdit"`
}

// NotebookSave is the body of a save (PUT /api/notebooks/{id}, or POST for a new one): the whole state. Rev is the
// revision the editor started from.
type NotebookSave struct {
	Name   string         `json:"name"`
	Cells  []NotebookCell `json:"cells"`
	Pinned bool           `json:"pinned"`
	Rev    int            `json:"rev"`
}

// NotebookVersion is one kept version of a notebook (store.NotebookVersion); Cells is empty in a list.
type NotebookVersion struct {
	ID         string         `json:"id"`
	NotebookID string         `json:"notebookId"`
	Name       string         `json:"name"`
	Cells      []NotebookCell `json:"cells"`
	Rev        int            `json:"rev"`
	By         string         `json:"by"`    // "agent" or "user"
	Event      string         `json:"event"` // before-agent-edit, agent-version, session, before-restore, deleted
	Summary    string         `json:"summary"`
	At         time.Time      `json:"at"`
	KeptAt     time.Time      `json:"keptAt"`
	Bytes      int            `json:"bytes"`
	Source     string         `json:"source"`
	Pinned     bool           `json:"pinned"`
	Private    bool           `json:"private"`
	CreatedAt  time.Time      `json:"createdAt"`
	Unreviewed bool           `json:"unreviewed"`
}

// Markdown is the version's text, as Notebook.Markdown.
func (v NotebookVersion) Markdown() string { return Notebook{Cells: v.Cells}.Markdown() }

// ReasonHistory is the reason of a 409 on a save or a delete: keel serve cannot keep the version it would replace.
const ReasonHistory = "history"

// IsHistoryRefusal reports whether err is keel serve's 409 "history" refusal of a notebook save or delete.
func IsHistoryRefusal(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusConflict && se.Reason == ReasonHistory
}

func notebookPath(id string) string { return "/api/notebooks/" + url.PathEscape(id) }

// Notebooks is GET /api/notebooks.
func (c *Client) Notebooks(ctx context.Context) (out []Notebook, err error) {
	err = c.do(ctx, http.MethodGet, "/api/notebooks", nil, &out)
	return out, err
}

// CreateNotebook is POST /api/notebooks: a new notebook with this name and text. It returns the notebook as kept.
func (c *Client) CreateNotebook(ctx context.Context, in NotebookSave) (out Notebook, err error) {
	if in.Cells == nil {
		in.Cells = []NotebookCell{}
	}
	err = c.do(ctx, http.MethodPost, "/api/notebooks", in, &out)
	return out, err
}

// SaveNotebook is PUT /api/notebooks/{id}. A 409 with reason "history" is IsHistoryRefusal: nothing was saved.
func (c *Client) SaveNotebook(ctx context.Context, id string, in NotebookSave) (out SavedNotebook, err error) {
	if in.Cells == nil {
		in.Cells = []NotebookCell{}
	}
	err = c.do(ctx, http.MethodPut, notebookPath(id), in, &out)
	return out, err
}

// PatchNotebook is PATCH /api/notebooks/{id}: a rename or a pin without the text. A nil field is left alone.
func (c *Client) PatchNotebook(ctx context.Context, id string, name *string, pinned *bool) (out Notebook, err error) {
	err = c.do(ctx, http.MethodPatch, notebookPath(id), struct {
		Name   *string `json:"name,omitempty"`
		Pinned *bool   `json:"pinned,omitempty"`
	}{name, pinned}, &out)
	return out, err
}

// DeleteNotebook is DELETE /api/notebooks/{id}. Call it only after the user confirmed. It keeps the notebook in Recently
// deleted; a 409 "history" (IsHistoryRefusal) means it cannot, and the user decides whether to delete it for good
// (permanent).
func (c *Client) DeleteNotebook(ctx context.Context, id string, permanent bool) error {
	p := notebookPath(id)
	if permanent {
		p += "?permanent=1"
	}
	return c.do(ctx, http.MethodDelete, p, nil, nil)
}

// PutNotebookPrivacy is PUT /api/notebooks/{id}/private: whether the agent can read the notebook. The user's switch only.
func (c *Client) PutNotebookPrivacy(ctx context.Context, id string, private bool) (out Notebook, err error) {
	err = c.do(ctx, http.MethodPut, notebookPath(id)+"/private", struct {
		Private bool `json:"private"`
	}{private}, &out)
	return out, err
}

// MarkNotebookReviewed is PUT /api/notebooks/{id}/reviewed: the user has read the agent's edit.
func (c *Client) MarkNotebookReviewed(ctx context.Context, id string) (out Notebook, err error) {
	err = c.do(ctx, http.MethodPut, notebookPath(id)+"/reviewed", nil, &out)
	return out, err
}

type notebookVersions struct {
	Versions []NotebookVersion `json:"versions"`
}

// NotebookHistory is GET /api/notebooks/{id}/history: the kept versions, newest first, without their text.
func (c *Client) NotebookHistory(ctx context.Context, id string) ([]NotebookVersion, error) {
	var out notebookVersions
	err := c.do(ctx, http.MethodGet, notebookPath(id)+"/history", nil, &out)
	return out.Versions, err
}

// NotebookVersion is GET /api/notebooks/{id}/history/{vid}: one kept version with its text.
func (c *Client) NotebookVersion(ctx context.Context, id, vid string) (out NotebookVersion, err error) {
	err = c.do(ctx, http.MethodGet, notebookPath(id)+"/history/"+url.PathEscape(vid), nil, &out)
	return out, err
}

// RestoreNotebookVersion is POST /api/notebooks/{id}/history/{vid}/restore. rev is the revision the page shows: keel
// serve refuses (409, reason "conflict") a restore over a newer change.
func (c *Client) RestoreNotebookVersion(ctx context.Context, id, vid string, rev int) (out Notebook, err error) {
	err = c.do(ctx, http.MethodPost, notebookPath(id)+"/history/"+url.PathEscape(vid)+"/restore", struct {
		Rev int `json:"rev"`
	}{rev}, &out)
	return out, err
}

// PurgeNotebookVersion is DELETE /api/notebooks/{id}/history/{vid}: Delete this version. Call it only after the user
// confirmed.
func (c *Client) PurgeNotebookVersion(ctx context.Context, id, vid string) error {
	return c.do(ctx, http.MethodDelete, notebookPath(id)+"/history/"+url.PathEscape(vid), nil, nil)
}

// DeletedNotebooks is GET /api/notebook-trash: Recently deleted, without text.
func (c *Client) DeletedNotebooks(ctx context.Context) ([]NotebookVersion, error) {
	var out notebookVersions
	err := c.do(ctx, http.MethodGet, "/api/notebook-trash", nil, &out)
	return out.Versions, err
}

// DeletedNotebook is GET /api/notebook-trash/{vid}: one deleted notebook with its text.
func (c *Client) DeletedNotebook(ctx context.Context, vid string) (out NotebookVersion, err error) {
	err = c.do(ctx, http.MethodGet, "/api/notebook-trash/"+url.PathEscape(vid), nil, &out)
	return out, err
}

// RestoreDeletedNotebook is POST /api/notebook-trash/{vid}/restore.
func (c *Client) RestoreDeletedNotebook(ctx context.Context, vid string) (out Notebook, err error) {
	err = c.do(ctx, http.MethodPost, "/api/notebook-trash/"+url.PathEscape(vid)+"/restore", nil, &out)
	return out, err
}

// PurgeDeletedNotebook is DELETE /api/notebook-trash/{vid}: Delete forever. Call it only after the user confirmed.
func (c *Client) PurgeDeletedNotebook(ctx context.Context, vid string) error {
	return c.do(ctx, http.MethodDelete, "/api/notebook-trash/"+url.PathEscape(vid), nil, nil)
}
