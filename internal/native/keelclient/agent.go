package keelclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// The agent panel's calls (internal/httpapi/agent.go, agentask.go). Nothing here reads a pane or types: keel serve reads
// the panes named in an ask, redacts them and asks the model; a proposal reaches a pane only through ApprovedInject
// (inject.go) after the user approves its bytes, or through DirectType (inject.go) when keel serve calls it plainly
// read-only and one line. This client sends the question and session ids, never pane text.

// AgentSettings is GET /api/agent/settings as the panel reads it: the provider, how a new conversation starts
// (DefaultScope: focused, tab or pick) and how many lines of a pane the agent reads. Keys says only whether a key is
// saved; keel serve never returns one.
type AgentSettings struct {
	Settings struct {
		Provider        string `json:"provider"`
		Model           string `json:"model"`
		DefaultScope    string `json:"defaultScope"`
		ScrollbackLines int    `json:"scrollbackLines"`
		HistoryDays     int    `json:"historyDays"`
	} `json:"settings"`
	Keys map[string]struct {
		Configured bool `json:"configured"`
	} `json:"keys"`
}

// KeyConfigured reports whether a key is saved for the settings' provider.
func (s AgentSettings) KeyConfigured() bool { return s.Keys[s.Settings.Provider].Configured }

// AgentSettings is GET /api/agent/settings.
func (c *Client) AgentSettings(ctx context.Context) (out AgentSettings, err error) {
	err = c.do(ctx, http.MethodGet, "/api/agent/settings", nil, &out)
	return out, err
}

// AgentProvider is one entry of GET /api/agent/providers: what the First run agent step and the panel's Connect card
// say about it.
type AgentProvider struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ShortName  string `json:"shortName"`
	Kind       string `json:"kind"`
	KeyLabel   string `json:"keyLabel"`
	KeyHelpURL string `json:"keyHelpUrl"`
	Warning    string `json:"warning"`
	TermsURL   string `json:"termsUrl"`
	Configured bool   `json:"configured"`
}

// AgentProviders is GET /api/agent/providers.
func (c *Client) AgentProviders(ctx context.Context) (out []AgentProvider, err error) {
	err = c.do(ctx, http.MethodGet, "/api/agent/providers", nil, &out)
	return out, err
}

// AgentKeyInput is a provider key the user typed. It is sent once and kept nowhere: String and GoString leave the key
// out, so printing one (%v, %+v, %#v) never shows it, and no error of this package carries a request body.
type AgentKeyInput struct {
	Provider string
	Key      string
}

func (k AgentKeyInput) String() string   { return fmt.Sprintf("{%s key withheld}", k.Provider) }
func (k AgentKeyInput) GoString() string { return k.String() }

// SetAgentKey is PUT /api/agent/keys/{provider}. keel serve stores the key in the vault and does not check it with
// the provider.
func (c *Client) SetAgentKey(ctx context.Context, in AgentKeyInput) error {
	return c.do(ctx, http.MethodPut, "/api/agent/keys/"+url.PathEscape(in.Provider), map[string]string{"key": in.Key}, nil)
}

// AskRequest is what an ask sends: the question and the sessions in scope. No pane text, notebooks or runbooks.
type AskRequest struct {
	ConversationID string   `json:"conversationId"`
	Question       string   `json:"question"`
	Panes          []string `json:"panes"`
}

// TraceStep is one line of the trace: what the agent did with a pane. Status is "read" or "skipped" (Reason
// not-readable, not-open or another), or "notes" or "runbook" for what keel serve sent along.
type TraceStep struct {
	SessionID  string         `json:"sessionId"`
	HostID     string         `json:"hostId"`
	Host       string         `json:"host"`
	Status     string         `json:"status"`
	Reason     string         `json:"reason"`
	Lines      int            `json:"lines"`
	Redactions map[string]int `json:"redactions"`
	Command    string         `json:"command"` // "exit 1", "still running"…: the last command, from the shell's marks
	NoteKind   string         `json:"noteKind"`
	Notes      int            `json:"notes"`
	Runbook    string         `json:"runbook"`
}

// Proposal is a command the agent suggests, as the card shows it. Bytes is exactly what Type sends (Ctrl-U, the
// command, a newline): keel serve builds it and checks it again before writing.
type Proposal struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"sessionId"`
	HostID       string     `json:"hostId"`
	Host         string     `json:"host"`
	Production   bool       `json:"production"`
	Command      string     `json:"command"`
	Bytes        string     `json:"bytes"`
	Why          string     `json:"why"`
	Risk         string     `json:"risk"` // readonly | changes | destructive
	NeedsShift   bool       `json:"needsShift"`
	Placeholders bool       `json:"placeholders"` // holds [secret:N]: keel serve will not type it
	Edited       bool       `json:"edited"`
	TypedAt      *time.Time `json:"typedAt"`
	Expired      bool       `json:"expired"`
}

// AskResponse is the answer to one ask, in one piece (keel serve does not stream it).
type AskResponse struct {
	ConversationID string      `json:"conversationId"`
	Title          string      `json:"title"`
	SaveError      string      `json:"saveError"`
	TestProvider   bool        `json:"testProvider"`
	Trace          []TraceStep `json:"trace"`
	Answer         string      `json:"answer"`
	Proposals      []Proposal  `json:"proposals"`
	Cut            string      `json:"cut"` // set when the provider ended the reply early: "length", "safety"…
}

// Ask is POST /api/agent/ask. It waits for the model, as long as ctx allows: cancel ctx to stop (the panel's Stop).
// A refused ask is a *StatusError: 412 no key, 423 vault locked, 502 the provider's own message in Msg.
func (c *Client) Ask(ctx context.Context, req AskRequest) (out AskResponse, err error) {
	if req.Panes == nil {
		req.Panes = []string{}
	}
	err = c.doWith(ctx, c.slow, http.MethodPost, "/api/agent/ask", req, &out)
	return out, err
}

// EditProposal is PUT /api/agent/conversations/{id}/proposals/{pid}: keel serve keeps the edited command and answers
// with the proposal as it now holds it (new bytes, risk and ⇧ rule). What is typed afterwards is that, never a local draft.
func (c *Client) EditProposal(ctx context.Context, conversationID, proposalID, command string) (out Proposal, err error) {
	err = c.do(ctx, http.MethodPut, "/api/agent/conversations/"+url.PathEscape(conversationID)+"/proposals/"+url.PathEscape(proposalID),
		map[string]string{"command": command}, &out)
	return out, err
}
