package keelclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Runbooks (docs/ui-redesign.md §16; internal/httpapi/agentrunbooks.go, internal/agent/service/runbooks.go): checklists
// the user curates, drafts from a saved conversation, Save / Dismiss / Undo on a runbook the agent proposed, and the
// runbook in use in a conversation (its state, a step marked done, skipped or retried, Don't use, Move the run).
//
// keel serve redacts every text for secrets before it keeps it and logs agent.runbook; this client sends what the user
// typed, shows what keel serve kept and keeps nothing on disk. Nothing here writes to a session: a step reaches a pane
// only as an ordinary proposal (Proposal.Runbook marks it) that keel serve makes for the current step, and that is typed
// through the same Approve before type path as any proposal. Starting a runbook is an ask with its id (AskRequest.RunbookID).

// StepCapture is a captured variable of a group run (§17): kept as loaded so a save does not drop it. Not built here.
type StepCapture struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// RunbookStep is one step of a saved runbook. A step with no command is a check the user marks done. Risk is keel
// serve's own rating of the command. Role, OneHost and Captures belong to group runs (§17): this client carries them
// through a save and shows none of them.
type RunbookStep struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Command     string        `json:"command"`
	Expect      string        `json:"expect"`
	Risk        string        `json:"risk"`
	Role        string        `json:"role"`
	OneHost     bool          `json:"oneHost"`
	Captures    []StepCapture `json:"captures"`
}

// Runbook is a saved runbook as the Runbooks page shows it.
type Runbook struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	When         string        `json:"when"`
	Hosts        []string      `json:"hosts"`
	AgentMayPick bool          `json:"agentMayPick"`
	Source       string        `json:"source"` // user | notebook | conversation | agent
	SourceRef    string        `json:"sourceRef"`
	FromPane     bool          `json:"fromPane"`
	Steps        []RunbookStep `json:"steps"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	LastUsedAt   *time.Time    `json:"lastUsedAt"`
	LastUsedOn   string        `json:"lastUsedOn"`
}

// RunbookList is GET /api/agent/runbooks.
type RunbookList struct {
	Runbooks   []Runbook `json:"runbooks"`
	Unreadable bool      `json:"unreadable"`
}

// RunbookStepInput is a step as the editor sends it: no risk, which keel serve rates itself.
type RunbookStepInput struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Command     string        `json:"command"`
	Expect      string        `json:"expect"`
	Role        string        `json:"role"`
	OneHost     bool          `json:"oneHost"`
	Captures    []StepCapture `json:"captures"`
}

// RunbookInput is what the editor sends to add or edit a runbook. DraftID names a draft keel serve made
// (DraftRunbook): keel serve takes the source from its own record of it, never from the client.
type RunbookInput struct {
	Name         string             `json:"name"`
	When         string             `json:"when"`
	Hosts        []string           `json:"hosts"`
	AgentMayPick bool               `json:"agentMayPick"`
	Steps        []RunbookStepInput `json:"steps"`
	DraftID      string             `json:"draftId"`
}

// RunbookDraft is an unsaved runbook keel serve made from a conversation: the page shows it for the user to edit and
// save. Nothing is kept until then.
type RunbookDraft struct {
	Name        string        `json:"name"`
	When        string        `json:"when"`
	Hosts       []string      `json:"hosts"`
	Steps       []RunbookStep `json:"steps"`
	Source      string        `json:"source"`
	SourceRef   string        `json:"sourceRef"`
	DraftID     string        `json:"draftId"`
	FromPrivate bool          `json:"fromPrivate"`
}

// SavedRunbook is a runbook after a save: Redacted counts the values keel serve replaced before keeping it.
type SavedRunbook struct {
	Runbook  Runbook          `json:"runbook"`
	Redacted int              `json:"redacted"`
	Proposal *RunbookProposal `json:"proposal"`
}

// RunbookProposal is a runbook the agent proposed to save, as the card shows it. Nothing is kept until the user saves
// it, and what is saved starts with Agent may pick off.
type RunbookProposal struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	When       string        `json:"when"`
	Steps      []RunbookStep `json:"steps"`
	FromPane   bool          `json:"fromPane"`
	SeenIn     string        `json:"seenIn"`
	PanesRead  []string      `json:"panesRead"`
	Status     string        `json:"status"` // open | saved | dismissed
	RunbookID  string        `json:"runbookId"`
	ProposedAt *time.Time    `json:"proposedAt"`
	Expired    bool          `json:"expired"`
}

// ProposalStep marks a command proposal keel serve made from a runbook step.
type ProposalStep struct {
	RunbookID string `json:"runbookId"`
	Name      string `json:"name"`
	Step      int    `json:"step"`
	Of        int    `json:"of"`
	StepID    string `json:"stepId"`
}

// RunStep is one step of a runbook in use. State is next, proposed, typed, done, failed or skipped; Result says where
// an end came from: "reported" (the exit status the pane's shell reported: a claim, never proof), "user" or "abandoned".
type RunStep struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Command     string     `json:"command"`
	Expect      string     `json:"expect"`
	Risk        string     `json:"risk"`
	State       string     `json:"state"`
	ProposalRef string     `json:"proposalId"` // the step's proposal, while it is proposed
	SessionID   string     `json:"sessionId"`
	HostID      string     `json:"hostId"`
	Host        string     `json:"host"`
	TypedAt     *time.Time `json:"typedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	Exit        *int       `json:"exit"`
	Result      string     `json:"result"`
	Edited      bool       `json:"edited"`
}

// ChatRunbook is a runbook in use in a conversation: a copy of its steps and each step's progress.
type ChatRunbook struct {
	ID        string    `json:"id"`
	RunbookID string    `json:"runbookId"`
	Name      string    `json:"name"`
	PickedBy  string    `json:"pickedBy"` // user | agent
	Why       string    `json:"why"`
	Status    string    `json:"status"` // active | finished | stopped
	Turn      int       `json:"turn"`
	StartedAt time.Time `json:"startedAt"`
	Hosts     []string  `json:"hosts"`
	// PinHost and PinSessionID are the host (and pane) of the first step proposed: later steps go only there, until the
	// user moves the run. Blocked says why the current step could not be proposed.
	PinHostID    string    `json:"pinHostId"`
	PinHost      string    `json:"pinHost"`
	PinSessionID string    `json:"pinSessionId"`
	Blocked      string    `json:"blocked"`
	Steps        []RunStep `json:"steps"`
}

// RunState is GET /api/agent/conversations/{id}/runbook and the answer to every action on it.
type RunState struct {
	Runbook   *ChatRunbook `json:"runbook"`
	NoRunbook bool         `json:"noRunbook"`
}

// Runbooks is GET /api/agent/runbooks.
func (c *Client) Runbooks(ctx context.Context) (out RunbookList, err error) {
	err = c.do(ctx, http.MethodGet, "/api/agent/runbooks", nil, &out)
	return out, err
}

// SaveRunbook is POST /api/agent/runbooks (ID empty) or PUT /api/agent/runbooks/{id}: the editor's whole state. The
// answer is the runbook as keel serve kept it, redacted.
func (c *Client) SaveRunbook(ctx context.Context, id string, in RunbookInput) (out SavedRunbook, err error) {
	if in.Steps == nil {
		in.Steps = []RunbookStepInput{}
	}
	if in.Hosts == nil {
		in.Hosts = []string{}
	}
	if id != "" {
		err = c.do(ctx, http.MethodPut, "/api/agent/runbooks/"+url.PathEscape(id), in, &out)
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/api/agent/runbooks", in, &out)
	return out, err
}

// DeleteRunbook is DELETE /api/agent/runbooks/{id}. Call it only after the user confirmed.
func (c *Client) DeleteRunbook(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/agent/runbooks/"+url.PathEscape(id), nil, nil)
}

// SetRunbookPick is PUT /api/agent/runbooks/{id}/pick, the list's "Agent may pick" switch: on only when the user turns
// it on.
func (c *Client) SetRunbookPick(ctx context.Context, id string, on bool) (out Runbook, err error) {
	err = c.do(ctx, http.MethodPut, "/api/agent/runbooks/"+url.PathEscape(id)+"/pick", struct {
		On bool `json:"agentMayPick"`
	}{on}, &out)
	return out, err
}

// DraftRunbook is POST /api/agent/runbooks/draft from a saved conversation: its proposals become steps. Nothing is
// kept; a 422 means there was nothing to make steps from.
func (c *Client) DraftRunbook(ctx context.Context, conversationID string) (out RunbookDraft, err error) {
	err = c.do(ctx, http.MethodPost, "/api/agent/runbooks/draft", struct {
		Conversation string `json:"conversationId"`
	}{conversationID}, &out)
	return out, err
}

func runPath(conversationID string) string {
	return "/api/agent/conversations/" + url.PathEscape(conversationID) + "/runbook"
}

// RunbookState is GET /api/agent/conversations/{id}/runbook: keel serve first looks for the results of typed steps in
// their panes' shell marks (reported, never proof).
func (c *Client) RunbookState(ctx context.Context, conversationID string) (out RunState, err error) {
	err = c.do(ctx, http.MethodGet, runPath(conversationID), nil, &out)
	return out, err
}

// StepAction is POST …/runbook/steps/{sid}: done, skip or retry on the current step. Nothing skips ahead; keel serve
// refuses any other step.
func (c *Client) StepAction(ctx context.Context, conversationID, stepID, action string) (out RunState, err error) {
	err = c.do(ctx, http.MethodPost, runPath(conversationID)+"/steps/"+url.PathEscape(stepID), map[string]string{"action": action}, &out)
	return out, err
}

// StopRunbook is DELETE …/runbook, "Don't use": the agent is not offered runbooks again in this conversation.
func (c *Client) StopRunbook(ctx context.Context, conversationID string) (out RunState, err error) {
	err = c.do(ctx, http.MethodDelete, runPath(conversationID), nil, &out)
	return out, err
}

// MoveRunbook is POST …/runbook/move, "Move the run to another host": the user's choice only.
func (c *Client) MoveRunbook(ctx context.Context, conversationID string) (out RunState, err error) {
	err = c.do(ctx, http.MethodPost, runPath(conversationID)+"/move", nil, &out)
	return out, err
}

func runbookProposalPath(conversationID, id string) string {
	return "/api/agent/conversations/" + url.PathEscape(conversationID) + "/runbooks/" + url.PathEscape(id)
}

// SaveProposedRunbook is POST /api/agent/conversations/{id}/runbooks/{rid}: the one request that keeps a runbook the
// agent proposed, with Agent may pick off. Only the card's Save calls it.
func (c *Client) SaveProposedRunbook(ctx context.Context, conversationID, id string) (out SavedRunbook, err error) {
	err = c.do(ctx, http.MethodPost, runbookProposalPath(conversationID, id), nil, &out)
	return out, err
}

// UndoProposedRunbook is POST …/runbooks/{rid}/undo: it deletes the runbook a Save kept and reopens the proposal.
func (c *Client) UndoProposedRunbook(ctx context.Context, conversationID, id string) (out RunbookProposal, err error) {
	err = c.do(ctx, http.MethodPost, runbookProposalPath(conversationID, id)+"/undo", nil, &out)
	return out, err
}

// DismissProposedRunbook is DELETE …/runbooks/{rid}: the proposal is closed and nothing is kept.
func (c *Client) DismissProposedRunbook(ctx context.Context, conversationID, id string) (out RunbookProposal, err error) {
	err = c.do(ctx, http.MethodDelete, runbookProposalPath(conversationID, id), nil, &out)
	return out, err
}
