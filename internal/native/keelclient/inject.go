package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

// The one place keel-native types into a session on the user's behalf (docs/ui-redesign.md §6, rule 1). Keystrokes
// from the terminal view go through Conn.Write; everything else that reaches a PTY goes through ApprovedInject, and
// ApprovedInject is called only by the Approve before type dialog's Type action (internal/native/approve.go). A test
// (internal/native/guard_test.go) fails if another file in internal/native sends one of these frames or calls it.
//
// keel serve writes exactly the bytes it is given and adds nothing, and only for a one-time nonce it issued for those
// same bytes to this client, which must be the driver (internal/inject, runtime.Session.InjectTicket).
//
// An agent proposal is typed through the same call. The webview adds {conversationId, proposalId, shift} to the inject
// frame only (pty.js sendApprovedInject opts.agent; the inject-challenge carries the bytes alone), and keel serve then
// checks the proposal (the exact bytes it holds, the pane, the role, the ⇧ rule) and logs agent.type before it writes;
// it answers with an agent-type frame, which ApprovedInject waits for. A refusal there is final: nothing was typed and
// nothing is retried. The bytes are the proposal's own (Proposal.Bytes: Ctrl-U, the command, a newline), built by keel
// serve; this client adds nothing to them.

// The refusals carry the words of the webview's toasts (cmd/keel/web/js/pty.js sendApprovedInject).
var (
	ErrNotDriver  = errors.New("You are following — take or request control to type") // this client is following
	ErrNotReady   = errors.New("Session is not connected")                            // the session is not ready
	ErrNoTicket   = errors.New("Session is not connected")                            // keel serve gave no nonce in time, or refused
	ErrInjectBusy = errors.New("Another type is waiting for keel serve")              // one at a time: never queued
)

// AgentRef names the proposal a Type is for. It goes into the inject frame and nowhere else; keel serve decides.
type AgentRef struct {
	ConversationID string
	ProposalID     string
	Shift          bool
}

// agentWait is how long keel serve has to answer a proposal's Type (term.js typeAgentProposal: 4 s).
const agentWait = 4 * time.Second

// ErrNoAgentAnswer is the webview's message when keel serve does not answer a proposal's Type.
var ErrNoAgentAnswer = errors.New("keel serve did not answer.")

// ticketWait is how long keel serve has to answer an approval request (pty.js requestInjectTicket: 2.5 s).
const ticketWait = 2500 * time.Millisecond

// ApprovedInject asks keel serve for a one-time nonce for exactly these bytes and, if it gets one, sends them. It
// is called only by the Approve before type dialog's Type action. It sends nothing unless this client drives a ready
// session, never retries, and never sends without a nonce. A non-nil agent marks the bytes as an agent proposal: the
// call then returns keel serve's refusal (the message of its agent-type answer) as an error, and nil only when keel
// serve says it typed.
func (c *Conn) ApprovedInject(ctx context.Context, command string, agent *AgentRef) error {
	st := c.Snapshot()
	switch {
	case st.Status != "ready":
		return ErrNotReady
	case st.Role != "driver":
		return ErrNotDriver
	}
	c.mu.Lock()
	if c.ticket != nil {
		c.mu.Unlock()
		return ErrInjectBusy
	}
	waiter := make(chan string, 1)
	c.ticket = waiter
	var answer chan agentAnswer // registered before the frame goes out, so the answer cannot be missed
	if agent != nil {
		answer = make(chan agentAnswer, 1)
		c.agentWaiter, c.agentFor = answer, agent.ProposalID
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.ticket = nil
		if agent != nil {
			c.agentWaiter, c.agentFor = nil, ""
		}
		c.mu.Unlock()
	}()

	b, _ := json.Marshal(map[string]any{"type": "inject-challenge", "command": command})
	if err := c.write(websocket.TextMessage, b); err != nil {
		return err
	}
	wait := ticketWait
	if c.wait > 0 {
		wait = c.wait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	var nonce string
	select {
	case nonce = <-waiter:
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	if nonce == "" {
		return ErrNoTicket
	}
	frame := map[string]any{"type": "inject", "command": command, "nonce": nonce}
	if agent != nil {
		frame["agent"] = map[string]any{"conversationId": agent.ConversationID, "proposalId": agent.ProposalID, "shift": agent.Shift}
	}
	b, _ = json.Marshal(frame)
	if err := c.write(websocket.TextMessage, b); err != nil || agent == nil {
		return err
	}
	timer.Reset(agentWait)
	select {
	case a := <-answer:
		if !a.ok {
			if a.message == "" {
				a.message = "Not typed."
			}
			return errors.New(a.message)
		}
		return nil
	case <-timer.C:
		return ErrNoAgentAnswer
	case <-ctx.Done():
		return ctx.Err()
	}
}

// agentAnswer is keel serve's agent-type frame: ok when it wrote the bytes, else why not (its own words).
type agentAnswer struct {
	ok      bool
	message string
}

// injectFrame hands keel serve's answer to an approval request to the call that waits for it. It reports whether the
// frame was one; the nonce is never kept, logged or shown.
func (c *Conn) injectFrame(data []byte) bool {
	var m struct {
		Type       string `json:"type"`
		Nonce      string `json:"nonce"`
		OK         bool   `json:"ok"`
		Message    string `json:"message"`
		ProposalID string `json:"proposalId"`
	}
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	if m.Type == "agent-type" {
		c.mu.Lock()
		w, want := c.agentWaiter, c.agentFor
		c.mu.Unlock()
		if w != nil && m.ProposalID == want { // an answer about another proposal is not ours
			select {
			case w <- agentAnswer{m.OK, m.Message}:
			default:
			}
		}
		return true
	}
	if m.Type != "inject-challenge" {
		return false
	}
	c.mu.Lock()
	w := c.ticket
	c.mu.Unlock()
	if w != nil {
		select { // the first answer counts; a second never blocks the reader
		case w <- m.Nonce:
		default:
		}
	}
	return true
}
