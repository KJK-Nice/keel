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
	"keel/internal/sessionsock"
	"keel/internal/wire"
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

// Per-socket rate limits. The client asks at most once per typing pause
// (350 ms) and once per ⇥, so anything faster is a misbehaving or hostile
// page: a detect inside the interval is dropped without an answer, a
// suggest is refused with "busy". Neither reaches a provider.
const (
	detectMinInterval  = 200 * time.Millisecond
	suggestMinInterval = 500 * time.Millisecond
)

// suggestNow is the rate limits' clock (tests freeze it).
var suggestNow = time.Now

// suggestConn runs one socket's detect and suggest calls off the read
// loop, so typing never waits on a provider. A newer call of a kind
// cancels the older one: only the latest prompt line matters.
type suggestConn struct {
	srv  *Server
	conn *sessionsock.Conn

	mu      sync.Mutex
	ctx     context.Context
	cancels map[string]context.CancelFunc
	last    map[string]time.Time
}

// allow reports whether a call of kind may run now, and if so records it.
func (c *suggestConn) allow(kind string, min time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := suggestNow()
	if t, ok := c.last[kind]; ok && now.Sub(t) < min {
		return false
	}
	c.last[kind] = now
	return true
}

func newSuggestConn(ctx context.Context, srv *Server, conn *sessionsock.Conn) *suggestConn {
	return &suggestConn{srv: srv, conn: conn, ctx: ctx, cancels: map[string]context.CancelFunc{}, last: map[string]time.Time{}}
}

// who is the session and, as it is now, the role of the socket's client in
// it: never what the client says.
func (c *suggestConn) who() (sessionID string, role agent.Role) {
	sess := c.conn.Session()
	return sess.Info().ID, sess.Role(c.conn.ClientID())
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
	c.srv.goSocket(func() {
		out := wire.Detect{Type: wire.TypeAgentDetect, Seq: seq, Source: service.DetectOff}
		if c.srv.agent != nil {
			sid, role := c.who()
			res, err := c.srv.agent.Detect(ctx, service.DetectRequest{SessionID: sid, Role: role, Line: line})
			if err == nil {
				out.Plain, out.Source = res.Plain, res.Source
			}
		}
		if ctx.Err() != nil {
			return
		}
		c.conn.Send(out)
	})
}

func (c *suggestConn) suggest(seq int, line string) {
	out := wire.Suggest{Type: wire.TypeAgentSuggest, Seq: seq}
	if !c.allow("suggest", suggestMinInterval) {
		out.Message, out.Reason = "Too fast. Press ⇥ again.", wire.ReasonBusy
		c.conn.Send(out)
		return
	}
	ctx := c.begin("suggest")
	c.srv.goSocket(func() {
		if c.srv.agent == nil {
			out.Message, out.Reason = "the agent needs keel.log to record what it reads", wire.ReasonUnavailable
			c.conn.Send(out)
			return
		}
		sid, role := c.who()
		sug, check, err := c.srv.agent.Suggest(ctx, service.SuggestRequest{SessionID: sid, Role: role, Line: line})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			out.Message, out.Reason = suggestErr(err)
			c.conn.Send(out)
			return
		}
		// The card first; Jev after. Run even when this call was
		// superseded: it keeps the stricter rating and logs agent.suggest.
		if ctx.Err() == nil {
			out.OK, out.Suggestion = true, suggestionCard(sug)
			c.conn.Send(out)
		}
		upd, took := check.Run(ctx)
		log.Printf("debug: agent suggest: source=%s pick_ms=%d model=%s thinking=%s model_ms=%d jev_ms=%d jev_asked=%v stricter=%v",
			took.Source, took.PickMs, took.Model, orDefault(took.Thinking), took.ModelMs, took.JevMs, took.JevAsked, upd.Stricter)
		if upd.Stricter && ctx.Err() == nil {
			risk := wire.SuggestRisk{Type: wire.TypeAgentSuggestRisk, SuggestionID: upd.SuggestionID}
			for _, o := range upd.Options {
				risk.Options = append(risk.Options, wire.OptionRisk{Risk: o.Risk, NeedsShift: o.NeedsShift, Judged: o.Judged})
			}
			c.conn.Send(risk)
		}
	})
}

// suggestionCard is a suggestion as the socket carries it: its id and its
// options, with the bytes ⇥ inserts.
func suggestionCard(s service.Suggestion) *wire.Suggestion {
	out := &wire.Suggestion{ID: s.ID}
	for _, o := range s.Options {
		out.Options = append(out.Options, wire.SuggestOption{Command: o.Command, Bytes: o.Bytes, Why: o.Why, Risk: o.Risk,
			NeedsShift: o.NeedsShift, Placeholders: o.Placeholders, Source: o.Source, Judged: o.Judged})
	}
	return out
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
		return "You follow this pane. Suggestions are for the driver.", wire.ReasonFollowing
	case errors.Is(err, service.ErrNoKey):
		return "Connect a model provider in the agent panel to get suggestions.", wire.ReasonNoKey
	case errors.Is(err, service.ErrSuggestOff):
		return "Suggestions in panes are off.", wire.ReasonOff
	case errors.Is(err, agent.ErrNotReadable):
		return "The agent can't read this host. Turn on \"Agent can read\" for it first.", wire.ReasonNotReadable
	case errors.Is(err, service.ErrNoSuggestion):
		return "No command came back for these words. Try other words.", wire.ReasonEmpty
	case errors.As(err, &pe):
		return pe.Msg, wire.ReasonProvider
	case errors.Is(err, service.ErrBadRequest), errors.Is(err, service.ErrEmptyLine):
		return err.Error(), wire.ReasonBadRequest
	}
	return "The suggestion failed: " + err.Error(), wire.ReasonError
}

// authorizeAgentInsert runs service.AuthorizeInsert for a client of sess
// and the "suggestion" part of its inject (wire.AgentInsert): the user
// pressed ⇥ (or ⇧⇥) on a suggestion card.
func (s *Server) authorizeAgentInsert(sess *runtime.Session, clientID uint64, msg *wire.AgentInsert, bytes string) (service.InsertGrant, error) {
	if s.agent == nil {
		return service.InsertGrant{}, errors.New("agent unavailable")
	}
	return s.agent.AuthorizeInsert(service.InsertRequest{
		SuggestionID: msg.SuggestionID,
		Option:       msg.Option,
		SessionID:    sess.Info().ID,
		Role:         sess.Role(clientID),
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
