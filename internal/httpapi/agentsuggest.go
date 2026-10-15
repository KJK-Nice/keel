package httpapi

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

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
// {"type":"agent-suggest-risk","suggestionId":"…","options":[{"risk","needsShift","judged"}…]},
// {"type":"agent-insert","ok":bool,"suggestionId":"…","message":"…"}.
//
// A suggestion is sent as soon as the model answers, rated by the code
// rule. Jev rates it afterwards; when Jev is stricter, agent-suggest-risk
// tells the card, and the service already holds the stricter rating for
// AuthorizeInsert. A ⇥ that lands before Jev answers is decided on the
// code rating (whose denylist is final for destructive commands).

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

// Per-socket rate limits. The client asks at most once per typing pause
// (350 ms) and once per ⇥, so anything faster is a misbehaving or hostile
// page: a detect inside the interval is dropped without an answer, a
// suggest is refused with "busy". Neither reaches a provider.
const (
	detectMinInterval  = 200 * time.Millisecond
	suggestMinInterval = 500 * time.Millisecond
)

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
	last    map[string]time.Time
}

// allow reports whether a call of kind may run now, and if so records it.
func (c *suggestConn) allow(kind string, min time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if t, ok := c.last[kind]; ok && now.Sub(t) < min {
		return false
	}
	c.last[kind] = now
	return true
}

func newSuggestConn(ctx context.Context, srv *Server, ws *wsConn, sess *runtime.Session, clientID uint64) *suggestConn {
	return &suggestConn{srv: srv, ws: ws, sess: sess, cl: clientID, ctx: ctx, cancels: map[string]context.CancelFunc{}, last: map[string]time.Time{}}
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
	if !c.allow("detect", detectMinInterval) {
		return
	}
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
	if !c.allow("suggest", suggestMinInterval) {
		_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": false, "message": "Too fast. Press ⇥ again.", "reason": "busy"})
		return
	}
	ctx := c.begin("suggest")
	go func() {
		if c.srv.agent == nil {
			_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": false, "message": "the agent needs keel.log to record what it reads", "reason": "unavailable"})
			return
		}
		info := c.sess.Info()
		sug, check, err := c.srv.agent.Suggest(ctx, service.SuggestRequest{SessionID: info.ID, Role: clientRole(c.sess, c.cl), Line: line})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			msg, reason := suggestErr(err)
			_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": false, "message": msg, "reason": reason})
			return
		}
		// The card first; Jev after. Run even when this call was
		// superseded: it keeps the stricter rating and logs agent.suggest.
		if ctx.Err() == nil {
			_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest", "seq": seq, "ok": true, "suggestion": sug})
		}
		upd, took := check.Run(ctx)
		log.Printf("debug: agent suggest: source=%s pick_ms=%d model=%s thinking=%s model_ms=%d jev_ms=%d jev_asked=%v stricter=%v",
			took.Source, took.PickMs, took.Model, orDefault(took.Thinking), took.ModelMs, took.JevMs, took.JevAsked, upd.Stricter)
		if upd.Stricter && ctx.Err() == nil {
			_ = c.ws.writeJSON(map[string]any{"type": "agent-suggest-risk", "suggestionId": upd.SuggestionID, "options": upd.Options})
		}
	}()
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
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
func (s *Server) authorizeAgentInsert(sess *runtime.Session, clientID uint64, msg *agentInsertMsg, bytes string) (service.InsertGrant, error) {
	if s.agent == nil {
		return service.InsertGrant{}, errors.New("agent unavailable")
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

// Close ends the agent's work for keel serve's shutdown: the detection
// summaries still open are written to keel.log.
func (s *Server) Close() {
	if s.agent != nil {
		s.agent.Close()
	}
}
