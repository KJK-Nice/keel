package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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

// A second way, for a command keel serve calls plainly read-only and one line (docs/ui-redesign.md §6 rule 13): DirectType
// types the command alone, with no Ctrl-U and no newline, so it waits on the prompt line for the user's Enter. It is
// called only by the window's typeNow (internal/native/typenow.go), and keel serve decides every time: it checks
// the command, the pane's state and the driver again, at the moment of the write, and answers typed, dialog (nothing
// written; the window opens Approve before type) or a refusal. This file is the only one that builds the frames of
// either way: inject-challenge, inject (with or without direct and agent) and type-check. guard_test.go keeps all of it
// true. The page's version is cmd/keel/web/js/pty.js typeDirect and checkDirect.

// The refusals carry the words of the webview's toasts (cmd/keel/web/js/pty.js sendApprovedInject, typeDirect).
var (
	ErrNotDriver  = errors.New("You are following — take or request control to type") // this client is following
	ErrNotReady   = errors.New("Session is not connected")                            // the session is not ready
	ErrNoTicket   = errors.New("Session is not connected")                            // keel serve gave no nonce in time, or refused
	ErrInjectBusy = errors.New("Another type is waiting for keel serve")              // one at a time: never queued

	ErrDirectBusy     = errors.New("Still typing the last command into this pane.")                          // one direct type at a time per pane
	ErrNoDirectAnswer = errors.New("keel serve did not answer. Check the pane before you press Type again.") // it may have been typed: never open a dialog that could type it again
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
		frame["agent"] = agentPart(agent)
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
		Ref        string `json:"ref"`
		Dialog     bool   `json:"dialog"`
		Reason     string `json:"reason"`
		Direct     bool   `json:"direct"`
	}
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	if m.Type == "type-direct" || m.Type == "type-check" { // matched by the ref the request carried (pty.js settleDirect)
		c.mu.Lock()
		w := c.directWaiters[m.Ref]
		c.mu.Unlock()
		if w != nil {
			select {
			case w <- directAnswer{ok: m.OK, dialog: m.Dialog, reason: m.Reason, message: m.Message, direct: m.Direct}:
			default:
			}
		}
		return true
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

// agentPart is the agent part of an inject frame, as the webview builds it.
func agentPart(a *AgentRef) map[string]any {
	return map[string]any{"conversationId": a.ConversationID, "proposalId": a.ProposalID, "shift": a.Shift}
}

// DirectRef is the "direct" part of a type-check or a direct inject (internal/httpapi/typedirect.go directTypeMsg): where
// the command comes from, and what the pane's terminal shows, which keel serve ORs with its own view (it can only add a
// dialog). The ref that matches an answer is made here.
type DirectRef struct {
	Kind       string // "snippet" or "agent" (a notebook's block and a group run are not built here)
	SnippetID  string // the snippet, for Kind "snippet": keel serve compares its stored command with the bytes
	AltScreen  bool   // the terminal shows a full-screen program's screen; a caller that cannot tell says true
	PromptLine string // the line the cursor is on, which keel serve reads for an input prompt
}

// DirectResult is what keel serve answered to a direct type. Exactly one of the three: Typed (the command is on the
// prompt line and nothing ran), Dialog (nothing was written: Approve before type decides; Reason says why, in its words),
// or Message (keel serve refused, in its words).
type DirectResult struct {
	Typed   bool
	Dialog  bool
	Reason  string
	Message string
}

// directAnswer is keel serve's type-direct or type-check frame.
type directAnswer struct {
	ok, dialog, direct bool
	reason, message    string
}

// directWait is how long keel serve has to answer a direct type or a check (pty.js askDirect: 8 s).
const directWait = 8 * time.Second

// directReady is pty.js directReady for a connection: nothing goes out for a kind this client does not build, or for a
// session this client cannot type into (a follower, or one that is not ready).
func (c *Conn) directReady(ref DirectRef) error {
	if ref.Kind != "snippet" && ref.Kind != "agent" {
		return errors.New("keelclient: no such kind of direct type")
	}
	st := c.Snapshot()
	switch {
	case st.Status != "ready":
		return ErrNotReady
	case st.Role != "driver":
		return ErrNotDriver
	}
	return nil
}

// directFrame is a type-check or an inject with a direct part. The ref goes into it here and nowhere else.
func directFrame(kind, command, nonce string, ref DirectRef, id string, agent *AgentRef) []byte {
	d := map[string]any{"kind": ref.Kind, "altScreen": ref.AltScreen, "promptLine": ref.PromptLine, "ref": id}
	if ref.SnippetID != "" {
		d["snippetId"] = ref.SnippetID
	}
	f := map[string]any{"type": kind, "command": command, "direct": d}
	if nonce != "" {
		f["nonce"] = nonce
	}
	if agent != nil {
		f["agent"] = agentPart(agent)
	}
	b, _ := json.Marshal(f)
	return b
}

// newDirectWaiter registers the answer a request with a fresh ref waits for, before the request goes out.
func (c *Conn) newDirectWaiter() (string, chan directAnswer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.directSeq++
	ref := "d" + strconv.Itoa(c.directSeq)
	if c.directWaiters == nil {
		c.directWaiters = map[string]chan directAnswer{}
	}
	w := make(chan directAnswer, 1)
	c.directWaiters[ref] = w
	return ref, w
}

func (c *Conn) dropDirectWaiter(ref string) {
	c.mu.Lock()
	delete(c.directWaiters, ref)
	c.mu.Unlock()
}

// waitDirect waits for the answer to ref; ok is false when keel serve did not answer in time.
func (c *Conn) waitDirect(ctx context.Context, w chan directAnswer) (a directAnswer, ok bool, err error) {
	wait := directWait
	if c.wait > 0 {
		wait = c.wait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a = <-w:
		return a, true, nil
	case <-timer.C:
		return a, false, nil
	case <-ctx.Done():
		return a, false, ctx.Err()
	}
}

// DirectType asks keel serve to type command into this session without Enter, and without the Approve before type
// dialog. It is called only by the window's typeNow. Nothing goes out unless this client drives a ready session and the
// command is one plain line with no control or invisible character; a command that is not goes back as Dialog with no
// frame. The command is sent as it is: no Ctrl-U, no newline. keel serve gets a fresh nonce for exactly these bytes
// first (no nonce: Dialog, and no inject frame), then the inject with the direct part (and agent, for a proposal), and
// its answer decides: Typed, Dialog or a refusal in Message. One at a time (ErrDirectBusy), never queued, never retried.
// No answer in time is ErrNoDirectAnswer: it may have been typed, so the caller must not open a dialog.
func (c *Conn) DirectType(ctx context.Context, command string, ref DirectRef, agent *AgentRef) (DirectResult, error) {
	if err := c.directReady(ref); err != nil {
		return DirectResult{}, err
	}
	if !DirectLine(command) {
		return DirectResult{Dialog: true}, nil
	}
	c.mu.Lock()
	switch {
	case c.directBusy:
		c.mu.Unlock()
		return DirectResult{}, ErrDirectBusy
	case c.ticket != nil:
		c.mu.Unlock()
		return DirectResult{}, ErrInjectBusy
	}
	c.directBusy = true
	waiter := make(chan string, 1)
	c.ticket = waiter
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.directBusy, c.ticket = false, nil
		c.mu.Unlock()
	}()

	b, _ := json.Marshal(map[string]any{"type": "inject-challenge", "command": command})
	if err := c.write(websocket.TextMessage, b); err != nil {
		return DirectResult{}, err
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
		return DirectResult{}, ctx.Err()
	}
	if nonce == "" {
		return DirectResult{Dialog: true}, nil // no nonce, no inject
	}
	id, answer := c.newDirectWaiter()
	defer c.dropDirectWaiter(id)
	if err := c.write(websocket.TextMessage, directFrame("inject", command, nonce, ref, id, agent)); err != nil {
		return DirectResult{}, err
	}
	a, ok, err := c.waitDirect(ctx, answer)
	switch {
	case err != nil:
		return DirectResult{}, err
	case !ok:
		return DirectResult{}, ErrNoDirectAnswer
	case a.dialog:
		return DirectResult{Dialog: true, Reason: a.reason}, nil
	case !a.ok:
		if a.message == "" {
			a.message = "Not typed."
		}
		return DirectResult{Message: a.message}, nil
	}
	return DirectResult{Typed: true}, nil
}

// DirectCheck asks keel serve whether DirectType would type command now. It writes nothing. keel serve answers it
// only for a group run's proposal (every other kind is "dialog"), so the window does not use it to name its buttons: the
// hint does that, as the page's does. false when keel serve would not, or did not answer in time.
func (c *Conn) DirectCheck(ctx context.Context, command string, ref DirectRef, agent *AgentRef) (bool, error) {
	if err := c.directReady(ref); err != nil {
		return false, err
	}
	if !DirectLine(command) {
		return false, nil
	}
	id, answer := c.newDirectWaiter()
	defer c.dropDirectWaiter(id)
	if err := c.write(websocket.TextMessage, directFrame("type-check", command, "", ref, id, agent)); err != nil {
		return false, err
	}
	a, ok, err := c.waitDirect(ctx, answer)
	return ok && a.direct, err
}
