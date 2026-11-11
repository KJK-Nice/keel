package keelclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Snippets: saved commands (store.Snippet). Nothing here types anything: a snippet reaches a session only through the
// Approve before type dialog (internal/native/approve.go).

// Snippet is a saved snippet as GET /api/snippets lists it. AgentUsable is "Let the agent suggest this snippet".
type Snippet struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Command     string    `json:"command"`
	AgentUsable bool      `json:"agentUsable"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Snippets is GET /api/snippets.
func (c *Client) Snippets(ctx context.Context) (out []Snippet, err error) {
	err = c.do(ctx, http.MethodGet, "/api/snippets", nil, &out)
	return out, err
}

// SnippetInput is what a save sends. AgentUsable is a pointer: nil leaves the key out and keel serve keeps the saved
// flag (httpapi snippetPayload), so only the Snippets page's editor, which shows the switch, ever sets it.
type SnippetInput struct {
	ID          string `json:"-"` // "" creates, else updates
	Name        string `json:"name"`
	Command     string `json:"command"`
	AgentUsable *bool  `json:"agentUsable,omitempty"`
}

// SaveSnippet creates a snippet (in.ID is empty: POST /api/snippets) or updates one (PUT /api/snippets/{id}) and
// returns it.
func (c *Client) SaveSnippet(ctx context.Context, in SnippetInput) (out Snippet, err error) {
	if in.ID == "" {
		err = c.do(ctx, http.MethodPost, "/api/snippets", in, &out)
	} else {
		err = c.do(ctx, http.MethodPut, "/api/snippets/"+url.PathEscape(in.ID), in, &out)
	}
	return out, err
}

// DeleteSnippet is DELETE /api/snippets/{id}.
func (c *Client) DeleteSnippet(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/snippets/"+url.PathEscape(id), nil, nil)
}
