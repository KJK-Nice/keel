// Package wire is the contract between keel serve and its clients: every
// name and shape both ends of the session socket match on (a WebSocket for
// the window, a WebRTC data channel for a joined peer), and the words they
// share elsewhere (client kinds, roles, statuses, host-key refusals, risk).
// It is a leaf: it imports no other keel package, and both ends import it,
// so a frame or a word is renamed here once or not at all.
//
// A binary frame is PTY bytes: the session's output one way, the driver's
// keystrokes the other. A text frame is JSON and is one of the types below.
package wire

import (
	"fmt"
	"time"
)

// Kind says what a session's client is.
const (
	// KindWindow is the Keel window on this machine.
	KindWindow = "window"
	// KindJoined is a peer who joined through a share link, over WebRTC.
	// keel serve never types for one, and it never owns a session.
	KindJoined = "webrtc"
)

// Role is a client's part in a session: the driver's keys are written, the
// owner may take control back, a follower watches.
type Role string

const (
	RoleOwner    Role = "owner"
	RoleDriver   Role = "driver"
	RoleFollower Role = "follower"
)

// RoleOf is the one rule for a client's role, from the session's driver and
// owner ids.
func RoleOf(id, driverID, ownerID uint64) Role {
	switch id {
	case driverID:
		return RoleDriver
	case ownerID:
		return RoleOwner
	}
	return RoleFollower
}

// Status is a session's connection state.
type Status string

const (
	StatusConnecting Status = "connecting"
	StatusReady      Status = "ready"
	StatusError      Status = "error"
	StatusExited     Status = "exited"
)

// Text frame types a client sends.
const (
	TypeResize         = "resize"          // Cols, Rows: the driver's terminal size
	TypeRequestControl = "request-control" // ask the driver for control
	TypeGrantControl   = "grant-control"   // TargetID: pass control to that client
	TypeTakeControl    = "take-control"    // the owner takes control back
	// TypeInjectChallenge asks for a one-time nonce for exactly Command.
	// The answer has the same type and carries Nonce.
	TypeInjectChallenge = "inject-challenge"
	// TypeInject asks keel serve to type Command, with the Nonce it issued
	// for those bytes. Direct, Agent, Suggestion and Notebook say where the
	// bytes come from.
	TypeInject = "inject"
	// TypeAgentDetect and TypeAgentSuggest are the in-pane suggestion
	// questions (Seq, Line); the answers have the same types.
	TypeAgentDetect  = "agent-detect"
	TypeAgentSuggest = "agent-suggest"
)

// Text frame types keel serve sends.
const (
	TypeReady    = "ready"    // State: attached
	TypeStatus   = "status"   // State: the connection state changed
	TypePresence = "presence" // State: a client joined or left
	TypeControl  = "control"  // State: control was asked for (FromID) or passed (FromID, TargetID)
	TypeError    = "error"    // State: Message, and the refused host key when there is one
	// The share hub's announcements to the session's own clients: PeerID
	// and Message.
	TypeJoinRequest  = "join-request"
	TypeJoinResolved = "join-resolved"
	TypeJoinFailed   = "join-failed"
	// The answers to an inject: TypeResult.
	TypeTypeDirect  = "type-direct"  // an inject with Direct
	TypeAgentType   = "agent-type"   // an inject with Agent
	TypeAgentInsert = "agent-insert" // an inject with Suggestion
	// TypeAgentSuggestRisk: a shown suggestion was rated stricter (SuggestRisk).
	TypeAgentSuggestRisk = "agent-suggest-risk"
)

// Control is a text frame from a client. Its fields, and those of its
// parts, are in the order of their keys, so a frame's bytes are the ones a
// map with the same keys would give.
type Control struct {
	// Agent marks an inject as the user's Type on an agent proposal.
	Agent   *AgentType `json:"agent,omitempty"`
	Cols    int        `json:"cols,omitempty"`
	Command string     `json:"command,omitempty"`
	// Direct marks an inject as a type without the approve dialog.
	Direct *DirectType `json:"direct,omitempty"`
	// Line and Seq are the suggestion questions' (agent-detect, agent-suggest).
	Line  string `json:"line,omitempty"`
	Nonce string `json:"nonce,omitempty"`
	// Notebook marks an inject as a notebook fence's Type: one holding a
	// redaction placeholder is refused.
	Notebook bool `json:"notebook,omitempty"`
	Rows     int  `json:"rows,omitempty"`
	Seq      int  `json:"seq,omitempty"`
	// Suggestion marks an inject as the user's ⇥ / ⇧⇥ on a suggestion.
	Suggestion *AgentInsert `json:"suggestion,omitempty"`
	TargetID   uint64       `json:"targetId,omitempty"`
	Type       string       `json:"type"`
}

// AgentType is the "agent" part of an inject: the proposal the bytes are.
// A group run's proposal names its run, GroupRunID, not a conversation.
type AgentType struct {
	ConversationID string `json:"conversationId,omitempty"`
	GroupRunID     string `json:"groupRunId,omitempty"`
	ProposalID     string `json:"proposalId"`
	Shift          bool   `json:"shift"`
}

// AgentInsert is the "suggestion" part of an inject.
type AgentInsert struct {
	Option       int    `json:"option"`
	Shift        bool   `json:"shift"`
	SuggestionID string `json:"suggestionId"`
}

// Where a direct type's command comes from (DirectType.Kind).
const (
	DirectAgent    = "agent"
	DirectSnippet  = "snippet"
	DirectNotebook = "notebook"
)

// DirectType is the "direct" part of an inject.
type DirectType struct {
	// AltScreen and PromptLine are what the client's terminal shows:
	// whether it is on its alternate screen, and the line the cursor is
	// on. keel serve ORs them with its own view of the output: they can
	// only add a dialog.
	AltScreen  bool   `json:"altScreen"`
	Kind       string `json:"kind"`
	NotebookID string `json:"notebookId,omitempty"`
	PromptLine string `json:"promptLine"`
	// Ref is the client's id for this request, sent back with the answer.
	Ref       string `json:"ref"`
	SnippetID string `json:"snippetId,omitempty"`
}

// Client is one attached client, as a State names it.
type Client struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Role       Role      `json:"role"`
	AttachedAt time.Time `json:"attachedAt"`
}

// State is keel serve's ready, status, presence, control and error frame,
// and the share hub's join frames. Only a client of this machine is sent
// HostID and HostKey.
type State struct {
	Type     string   `json:"type"`
	Message  string   `json:"message"`
	ID       string   `json:"id,omitempty"`
	HostID   string   `json:"hostId,omitempty"`
	Title    string   `json:"title,omitempty"`
	Status   Status   `json:"status,omitempty"`
	Attached []Client `json:"attached,omitempty"`
	DriverID uint64   `json:"driverId,omitempty"`
	OwnerID  uint64   `json:"ownerId,omitempty"`
	You      *Client  `json:"you,omitempty"`
	HostKey
	FromID   uint64 `json:"fromId,omitempty"`
	TargetID uint64 `json:"targetId,omitempty"`
	PeerID   string `json:"peerId,omitempty"`
}

// A State.Message that is no news: the client keeps these words out of a
// pane's connection log.
const (
	MsgAttaching     = "Attaching…"
	MsgAttached      = "attached"
	MsgJoined        = "joined"
	MsgLeft          = "left"
	MsgConnected     = "connected"
	MsgControlPassed = "control passed"
	MsgTookControl   = "took control"
)

// MsgInsertExpired is the agent-insert refusal for an insert whose nonce
// or suggestion is gone. The client says the same of a suggestion it no
// longer holds.
const MsgInsertExpired = "the insert expired; press ⇥ again"

// Host-key refusals (HostKey.Kind).
const (
	HostKeyUnknown  = "unknown-host"
	HostKeyMismatch = "host-key-mismatch"
)

// HostKey is a refused host key: the one shape for it on the session
// socket, in a 409 body and in a trust request. Token names keel serve's
// one-time offer to trust exactly this key; String leaves it out, so that
// printing a HostKey never shows it.
type HostKey struct {
	Kind        string `json:"errorKind,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Expected    string `json:"expected,omitempty"`
	KeyType     string `json:"keyType,omitempty"`
	Token       string `json:"trustToken,omitempty"`
}

func (k HostKey) String() string {
	return fmt.Sprintf("{%s %s %s expected=%s %s}", k.Kind, k.Hostname, k.Fingerprint, k.Expected, k.KeyType)
}

// TrustHostKey is the body of POST /api/known-hosts: the offer sent back
// (its token, address:port and fingerprint) and whether it replaces a key.
type TrustHostKey struct {
	Token       string `json:"trustToken"`
	Hostname    string `json:"hostname"`
	Fingerprint string `json:"fingerprint"`
	Replace     bool   `json:"replace"`
}

// Challenge is keel serve's inject-challenge answer.
type Challenge struct {
	Type  string `json:"type"`
	Nonce string `json:"nonce"`
}

// TypeResult is keel serve's answer to an inject that names where its bytes
// come from: type-direct (Ref), agent-type (ProposalID) or agent-insert
// (SuggestionID). OK means the bytes were written. Dialog (type-direct
// only) means nothing was written and the approve dialog decides, for
// Reason; otherwise Message is the refusal.
type TypeResult struct {
	Type         string `json:"type"`
	Ref          string `json:"ref,omitempty"`
	ProposalID   string `json:"proposalId,omitempty"`
	SuggestionID string `json:"suggestionId,omitempty"`
	OK           bool   `json:"ok"`
	Dialog       bool   `json:"dialog,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}

// Detect is keel serve's agent-detect answer.
type Detect struct {
	Type   string `json:"type"`
	Seq    int    `json:"seq"`
	Plain  bool   `json:"plain"`
	Source string `json:"source"`
}

// Why a suggestion did not come (Suggest.Reason), for the client to act on.
const (
	ReasonBusy        = "busy"
	ReasonUnavailable = "unavailable"
	ReasonFollowing   = "following"
	ReasonNoKey       = "no-key"
	ReasonOff         = "off"
	ReasonNotReadable = "not-readable"
	ReasonEmpty       = "empty"
	ReasonProvider    = "provider"
	ReasonBadRequest  = "bad-request"
	ReasonError       = "error"
)

// Suggest is keel serve's agent-suggest answer: the card, or why not.
type Suggest struct {
	Type       string      `json:"type"`
	Seq        int         `json:"seq"`
	OK         bool        `json:"ok"`
	Suggestion *Suggestion `json:"suggestion,omitempty"`
	Message    string      `json:"message,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

// Risk is how much a command changes.
type Risk string

const (
	RiskReadOnly    Risk = "readonly"    // reads state only
	RiskChanges     Risk = "changes"     // changes state (files, services, packages, config)
	RiskDestructive Risk = "destructive" // deletes data or takes a host or service down
)

// Rank orders the risks, read-only lowest. Any other word ranks as changes.
func (r Risk) Rank() int {
	switch r {
	case RiskReadOnly:
		return 0
	case RiskDestructive:
		return 2
	default:
		return 1
	}
}

// Suggestion is a suggestion card.
type Suggestion struct {
	ID      string          `json:"id"`
	Options []SuggestOption `json:"options"`
}

// SuggestOption is one command of a card. Bytes are exactly what ⇥
// inserts: no newline, so an insert is never run.
type SuggestOption struct {
	Command      string `json:"command"`
	Bytes        string `json:"bytes"`
	Why          string `json:"why,omitempty"`
	Risk         Risk   `json:"risk"`
	NeedsShift   bool   `json:"needsShift"`
	Placeholders bool   `json:"placeholders"`
	Source       string `json:"source"`
	Judged       bool   `json:"judged"`
}

// OptionRisk is one option's rating in a SuggestRisk.
type OptionRisk struct {
	Risk       Risk `json:"risk"`
	NeedsShift bool `json:"needsShift"`
	Judged     bool `json:"judged"`
}

// SuggestRisk is keel serve's agent-suggest-risk frame: the rating of
// every option of a shown suggestion, in order.
type SuggestRisk struct {
	Type         string       `json:"type"`
	SuggestionID string       `json:"suggestionId"`
	Options      []OptionRisk `json:"options"`
}
