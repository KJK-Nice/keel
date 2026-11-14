package wire

import "time"

// What `keel mcp` and keel serve's /api/mcp routes say to each other. The agent service fills these in, keel serve sends
// them and the MCP server reads them, so a field is named here once.

// ExternalTitle is the agent panel conversation that holds what an outside agent proposed.
const ExternalTitle = "Claude Code"

// The limits of what an outside agent may send.
const (
	// MaxProposalCommandBytes and MaxProposalCommandLines bound one proposed command.
	MaxProposalCommandBytes = 2048
	MaxProposalCommandLines = 20
)

// ExternalPane is one pane an outside agent may read.
type ExternalPane struct {
	PaneID     string `json:"paneId"`
	Host       string `json:"host"`
	Production bool   `json:"production"`
}

// ExternalPaneList is the panes an outside agent may read, and how many others are open on hosts it may not read (never
// their names).
type ExternalPaneList struct {
	Panes      []ExternalPane `json:"panes"`
	NotAllowed int            `json:"notAllowed"`
}

// ExternalExcerpt is one pane as an outside agent reads it: redacted.
type ExternalExcerpt struct {
	PaneID      string         `json:"paneId"`
	Host        string         `json:"host"`
	Production  bool           `json:"production"`
	Lines       int            `json:"lines"`
	LastCommand string         `json:"lastCommand,omitempty"` // "exit 1", "still running", … when read from OSC 133 marks
	Redactions  map[string]int `json:"redactions,omitempty"`
	Text        string         `json:"text"`
}

// ExternalProposed is keel serve's answer to a proposal from an outside agent.
type ExternalProposed struct {
	ProposalID     string `json:"proposalId"`
	ConversationID string `json:"conversationId"`
	Conversation   string `json:"conversation"`
	PaneID         string `json:"paneId"`
	Host           string `json:"host"`
	Risk           Risk   `json:"risk"`
	NeedsShift     bool   `json:"needsShift"`
	Placeholders   bool   `json:"placeholders"`
}

// ExternalProposalState is what an outside agent may know of a proposal it made: whether the user typed it, and when.
type ExternalProposalState struct {
	ProposalID string     `json:"proposalId"`
	Typed      bool       `json:"typed"`
	TypedAt    *time.Time `json:"typedAt,omitempty"`
	Edited     bool       `json:"edited"`
}
