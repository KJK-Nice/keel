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

// The refusals carry the words of the webview's toasts (cmd/keel/web/js/pty.js sendApprovedInject).
var (
	ErrNotDriver  = errors.New("You are following — take or request control to type") // this client is following
	ErrNotReady   = errors.New("Session is not connected")                            // the session is not ready
	ErrNoTicket   = errors.New("Session is not connected")                            // keel serve gave no nonce in time, or refused
	ErrInjectBusy = errors.New("Another type is waiting for keel serve")              // one at a time: never queued
)

// ticketWait is how long keel serve has to answer an approval request (pty.js requestInjectTicket: 2.5 s).
const ticketWait = 2500 * time.Millisecond

// ApprovedInject asks keel serve for a one-time nonce for exactly these bytes and, if it gets one, sends them. It
// is called only by the Approve before type dialog's Type action. It sends nothing unless this client drives a ready
// session, never retries, and never sends without a nonce.
func (c *Conn) ApprovedInject(ctx context.Context, command string) error {
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
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.ticket = nil
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
	b, _ = json.Marshal(map[string]any{"type": "inject", "command": command, "nonce": nonce})
	return c.write(websocket.TextMessage, b)
}

// injectFrame hands keel serve's answer to an approval request to the call that waits for it. It reports whether the
// frame was one; the nonce is never kept, logged or shown.
func (c *Conn) injectFrame(data []byte) bool {
	var m struct {
		Type  string `json:"type"`
		Nonce string `json:"nonce"`
	}
	if json.Unmarshal(data, &m) != nil || m.Type != "inject-challenge" {
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
