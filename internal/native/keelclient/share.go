package keelclient

import (
	"context"
	"net/http"
	"net/url"
)

// Share is POST /api/share: keel serve shares the session (or returns the live link of a session already shared)
// and answers with the join link. The link carries the share's secret after "#k=": it is returned to the caller and
// kept nowhere here, and never put in an error. keel serve's own refusal ("set a signal server in Settings · Sharing
// to share") comes back as a StatusError whose Msg is shown as it is.
func (c *Client) Share(ctx context.Context, sessionID string) (link string, err error) {
	var out struct {
		URL string `json:"url"`
	}
	err = c.do(ctx, http.MethodPost, "/api/share", map[string]string{"sessionId": sessionID}, &out)
	return out.URL, err
}

// AdmitShare is POST /api/share/{id}/admit: the owner's answer to a join request. It is called only from the Allow and
// Deny buttons of the join prompt (internal/native/sharepeople.go); nothing else may admit anyone.
func (c *Client) AdmitShare(ctx context.Context, sessionID, peerID string, allow bool) error {
	return c.do(ctx, http.MethodPost, "/api/share/"+url.PathEscape(sessionID)+"/admit", map[string]any{"peerId": peerID, "allow": allow}, nil)
}

// StopShare is DELETE /api/share/{id}: viewers are disconnected and the link stops working.
func (c *Client) StopShare(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/api/share/"+url.PathEscape(sessionID), nil, nil)
}

// What the share hub and the runtime announce on a session's socket, for the window to show.
const (
	EventJoinRequest    = "join-request"    // PeerID asks to watch; Message is the name they typed
	EventJoinResolved   = "join-resolved"   // that request was answered (allowed, denied, expired, left, ended)
	EventJoinFailed     = "join-failed"     // a viewer could not be connected; Message says why
	EventControlRequest = "control-request" // FromID asks the driver for control
	EventControlNote    = "control-note"    // the runtime's words for a hand-over or a take-back
)

// Event is one announcement. It holds no secret: a join request carries the name the joiner typed.
type Event struct {
	Kind, PeerID, Message string
	FromID                uint64
}

func (c *Conn) push(e Event) {
	c.mu.Lock()
	if len(c.events) < 64 { // a flood of requests cannot grow it without bound
		c.events = append(c.events, e)
	}
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Events hands over what was announced since the last call, oldest first.
func (c *Conn) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.events
	c.events = nil
	return out
}

// following reports whether keel serve has said this client does not drive (it drops such a client's keystrokes).
func (c *Conn) following() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Role == "follower" || c.state.Role == "owner"
}

func (c *Conn) block() {
	c.mu.Lock()
	c.state.Blocked++
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}
