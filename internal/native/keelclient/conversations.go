package keelclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Saved agent conversations and the agent's settings (internal/httpapi/agent.go, agentask.go; the service is
// internal/agent/service/sessions.go). keel serve saves a conversation in the vault, redacted as sent; this client only
// reads one into memory to show it and never writes it anywhere. Nothing here reads a pane, types, or calls keel mcp's routes.

// ConversationSummary is one row of GET /api/agent/conversations.
type ConversationSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Hosts     []string  `json:"hosts"`
	Turns     int       `json:"turns"`
	Proposals int       `json:"proposals"`
}

// ConversationList is GET /api/agent/conversations: the saved conversations, most recent first, and how long keel serve
// keeps them (HistoryDays 0: only while open, and the list is empty).
type ConversationList struct {
	HistoryDays int                   `json:"historyDays"`
	Sessions    []ConversationSummary `json:"sessions"`
	SaveError   string                `json:"saveError"`
	Unreadable  bool                  `json:"unreadable"` // keel.chats cannot be read: Delete all starts over
}

// SavedTurn is one turn of a transcript: the question and what the agent answered, the cards for the notes and runbooks it
// proposed and what the user did with them, and the notebook cards (Created, Edited, Delete?).
type SavedTurn struct {
	At        time.Time          `json:"at"`
	Question  string             `json:"question"`
	Answer    string             `json:"answer"`
	Cut       string             `json:"cut"`
	Trace     []TraceStep        `json:"trace"`
	Proposals []Proposal         `json:"proposals"` // a proposal keel serve no longer holds is Expired
	Notes     []NoteProposal     `json:"notes"`
	Runbooks  []RunbookProposal  `json:"runbooks"`
	Notebooks []NotebookProposal `json:"notebooks"`
	Again     bool               `json:"again"` // a Propose again turn: no question
}

// Transcript is GET /api/agent/conversations/{id}. Runbook is the runbook last used in it and how far it got;
// NoRunbook is the user's "Don't use".
type Transcript struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
	Saved     bool         `json:"saved"`
	Turns     []SavedTurn  `json:"turns"`
	Runbook   *ChatRunbook `json:"runbook"`
	NoRunbook bool         `json:"noRunbook"`
}

// Conversations is GET /api/agent/conversations.
func (c *Client) Conversations(ctx context.Context) (out ConversationList, err error) {
	err = c.do(ctx, http.MethodGet, "/api/agent/conversations", nil, &out)
	return out, err
}

// Conversation is GET /api/agent/conversations/{id}: reading it sends nothing anywhere and reads no pane.
func (c *Client) Conversation(ctx context.Context, id string) (out Transcript, err error) {
	err = c.do(ctx, http.MethodGet, "/api/agent/conversations/"+url.PathEscape(id), nil, &out)
	return out, err
}

// RenameConversation is PATCH /api/agent/conversations/{id}; it returns the title keel serve kept.
func (c *Client) RenameConversation(ctx context.Context, id, title string) (string, error) {
	var out struct {
		Title string `json:"title"`
	}
	err := c.do(ctx, http.MethodPatch, "/api/agent/conversations/"+url.PathEscape(id), map[string]string{"title": title}, &out)
	return out.Title, err
}

// DeleteConversation is DELETE /api/agent/conversations/{id}: one conversation, in memory and in the vault.
func (c *Client) DeleteConversation(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/agent/conversations/"+url.PathEscape(id), nil, nil)
}

// DeleteAllConversations is DELETE /api/agent/conversations: every conversation, in every window. Call it only after
// the user confirmed.
func (c *Client) DeleteAllConversations(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/api/agent/conversations", nil, nil)
}

// ProposeAgain is POST /api/agent/conversations/{id}/proposals/{pid}/again: keel serve rates a saved command again and
// makes a new, live proposal, which is typed (if at all) like any other, through Approve before type.
func (c *Client) ProposeAgain(ctx context.Context, conversationID, proposalID string) (out Proposal, err error) {
	err = c.do(ctx, http.MethodPost, "/api/agent/conversations/"+url.PathEscape(conversationID)+"/proposals/"+url.PathEscape(proposalID)+"/again", nil, &out)
	return out, err
}

// AgentRedact is the redaction options Settings · Agent sends whole, as the app does ({secrets, ips, hostnames}).
type AgentRedact struct {
	Secrets   bool `json:"secrets"`
	IPs       bool `json:"ips"`
	Hostnames bool `json:"hostnames"`
}

// AgentSettingsPatch is the body of PUT /api/agent/settings. keel serve merges the sent fields over the saved
// settings, so a field left nil is not sent and is not changed. Redaction, scope and retention are keel serve's
// to apply; this only sends the values.
type AgentSettingsPatch struct {
	Provider           *string      `json:"provider,omitempty"`
	Model              *string      `json:"model,omitempty"`
	Detection          *string      `json:"detection,omitempty"`
	Suggestions        *string      `json:"suggestions,omitempty"`
	HistorySuggestions *string      `json:"historySuggestions,omitempty"`
	DefaultScope       *string      `json:"defaultScope,omitempty"`
	ScrollbackLines    *int         `json:"scrollbackLines,omitempty"`
	Redact             *AgentRedact `json:"redact,omitempty"`
	HistoryDays        *int         `json:"historyDays,omitempty"`
	ClaudeCode         *bool        `json:"claudeCode,omitempty"`
}

// SaveAgentSettings is PUT /api/agent/settings with only the fields of the patch; the answer is the settings as
// keel serve saved them.
func (c *Client) SaveAgentSettings(ctx context.Context, p AgentSettingsPatch) (out AgentSettings, err error) {
	err = c.do(ctx, http.MethodPut, "/api/agent/settings", p, &out)
	return out, err
}

// DeleteAgentKey is DELETE /api/agent/keys/{provider} (Settings · Agent's Disconnect). Call it only after the user
// confirmed.
func (c *Client) DeleteAgentKey(ctx context.Context, provider string) error {
	return c.do(ctx, http.MethodDelete, "/api/agent/keys/"+url.PathEscape(provider), nil, nil)
}
