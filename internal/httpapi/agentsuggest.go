package httpapi

import (
	"context"
	"errors"
	"sync"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/runtime"
)

// In-pane suggestions (docs/ui-redesign.md §5.5, phase 6c) ride on the
// session socket, so keel serve knows which client asks and its role in
// the pane as it is now: only the driver gets a notice or a suggestion
// (§6.6). Like agentask.go this file never writes to a session: the user's
// ⇥ / ⇧⇥ goes through the inject ticket path in sessionWS, which asks
// service.AuthorizeInsert first (agentsuggest_test.go checks this file
// calls no write method).
//
// Messages from the client:
//
//	{"type":"agent-detect","seq":N,"line":"…"}   on a typing pause
//	{"type":"agent-suggest","seq":N,"line":"…"}  the user pressed ⇥ on a notice
//	{"type":"inject",…,"suggestion":{"suggestionId":"…","option":0,"shift":false}}
//
// Replies: {"type":"agent-detect","seq":N,"plain":bool,"source":"…"},
// {"type":"agent-suggest","seq":N,"ok":bool,"suggestion":{…}|"message","reason"},
// {"type":"agent-insert","ok":bool,"suggestionId":"…","message":"…"}.

// WithAgentJudge sets how the agent builds its detector (TypeSafe Jev).
// keel serve passes service.FromEnv's; tests pass a fake.
func (s *Server) WithAgentJudge(f service.JudgeFactory) *Server {
	s.agentJudge = f
	return s
}

// agentInsertMsg is the "suggestion" part of an inject: the user pressed
// ⇥ (or ⇧⇥) on a suggestion card.
type agentInsertMsg struct {
	SuggestionID string `json:"suggestionId"`
	Option       int    `json:"option"`
	Shift        bool   `json:"shift"`
}

// clientRole is a client's role in sess now, never what the client says.
func clientRole(sess *runtime.Session, clientID uint64) agent.Role {
	info := sess.Info()
	switch clientID {
	case info.DriverID:
		return agent.RoleDriver
	case info.OwnerID:
		return agent.RoleOwner
	}
	return agent.RoleFollower
}

// suggestConn runs one socket's detect and suggest calls off the read
// loop, so typing never waits on a provider. A newer call of a kind
// cancels the older one: only the latest prompt line matters.
type suggestConn struct {
	srv  *Server
	ws   *wsConn
	sess *runtime.Session
	cl   uint64

	mu      sync.Mutex
	ctx     context.Context
	cancels map[string]context.CancelFunc
}

func newSuggestConn(ctx context.Context, srv *Server, ws *wsConn, sess *runtime.Session, clientID uint64) *suggestConn {
	return &suggestConn{srv: srv, ws: ws, sess: sess, cl: clientID, ctx: ctx, cancels: map[string]context.CancelFunc{}}
}

func (c *suggestConn) begin(kind string) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.cancels[kind]; cancel != nil {
		cancel()
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.cancels[kind] = cancel
	return ctx
}

func (c *suggestConn) detect(seq int, line string) {
	ctx := c.begin("detect")
	go func() {
		out := map[string]any{"type": "agent-detect", "seq": seq, "plain": false, "source": service.DetectOff}
		if c.srv.agent != nil {
			info := c.sess.Info()
			res, err := c.srv.agent.Detect(ctx, service.DetectRequest{SessionID: info.ID, Role: clientRole(c.sess, c.cl), Line: line})
			if err == nil {
				out["plain"], out["source"] = res.Plain, res.Source
			}
		}
		if ctx.Err() != nil {
			return
		}
		_ = c.ws.writeJSON(out)
	}()
}

func (c *suggestConn) suggest(seq int, line string) {
	ctx := c.begin("suggest")
	go func() {
		if c.srv.agent == nil {
			_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": false, "message": "the agent needs keel.log to record what it reads", "reason": "unavailable"})
			return
		}
		info := c.sess.Info()
		sug, err := c.srv.agent.Suggest(ctx, service.SuggestRequest{SessionID: info.ID, Role: clientRole(c.sess, c.cl), Line: line})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			msg, reason := suggestErr(err)
			_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": false, "message": msg, "reason": reason})
			return
		}
		_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": true, "suggestion": sug})
	}()
}

// suggestErr is the card's message and a reason the client can act on.
func suggestErr(err error) (string, string) {
	var pe *service.ProviderError
	switch {
	case errors.Is(err, agent.ErrFollower), errors.Is(err, agent.ErrNotDriving):
		return "You follow this pane. Suggestions are for the driver.", "following"
	case errors.Is(err, service.ErrNoKey):
		return "Connect Gemini in the agent panel to get suggestions.", "no-key"
	case errors.Is(err, service.ErrSuggestOff):
		return "Suggestions in panes are off.", "off"
	case errors.Is(err, agent.ErrNotReadable):
		return "The agent can't read this host. Turn on \"Agent can read\" for it first.", "not-readable"
	case errors.Is(err, service.ErrNoSuggestion):
		return "No command came back for these words. Try other words.", "empty"
	case errors.As(err, &pe):
		return pe.Msg, "provider"
	case errors.Is(err, service.ErrBadRequest), errors.Is(err, service.ErrEmptyLine):
		return err.Error(), "bad-request"
	}
	return "The suggestion failed: " + err.Error(), "error"
}

// authorizeAgentInsert runs service.AuthorizeInsert for a client of sess.
func (s *Server) authorizeAgentInsert(sess *runtime.Session, clientID uint64, msg *agentInsertMsg, bytes string) error {
	if s.agent == nil {
		return errors.New("agent unavailable")
	}
	return s.agent.AuthorizeInsert(service.InsertRequest{
		SuggestionID: msg.SuggestionID,
		Option:       msg.Option,
		SessionID:    sess.Info().ID,
		Role:         clientRole(sess, clientID),
		Bytes:        bytes,
		Shift:        msg.Shift,
	})
}
