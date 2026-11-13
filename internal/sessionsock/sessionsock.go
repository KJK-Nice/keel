// Package sessionsock is keel serve's end of the session socket
// (internal/wire): it attaches one client to a session and handles what
// that client sends, over whatever carries the frames. The window's
// WebSocket (internal/httpapi) and a joined peer's WebRTC data channel
// (internal/share) both go through here, so there is one handler; what
// differs between them is said in Caps, not in a second copy.
//
// Bytes reach a PTY from here in two ways only: Keys, the driver's own
// keystrokes (Session.Write refuses everyone else), and an inject, which
// needs Caps.Inject and a nonce the session issued for those exact bytes.
package sessionsock

import (
	"encoding/json"
	"errors"

	"keel/internal/inject"
	"keel/internal/runtime"
	"keel/internal/wire"
)

// Transport carries a client's frames: SendJSON a text frame, SendBytes a
// binary one (PTY output). Both may be called from several goroutines.
type Transport interface {
	SendJSON(v any) error
	SendBytes(p []byte) error
}

// Caps is what a client may ask for beyond watching the session, driving
// it when it is the driver, and asking for or passing control. The zero
// value grants none of it.
type Caps struct {
	// Inject: keel serve types for this client (inject-challenge, and
	// inject in every form: plain, notebook, direct, agent, suggestion).
	Inject bool
	// Agent: the in-pane suggestion questions (agent-detect, agent-suggest).
	Agent bool
	// Host: frames name the host's id and carry a refused host key with
	// its one-time trust offer.
	Host bool
}

// Local is the window on the session's own machine. Joined is a peer who
// came in through a share link: it watches, drives when handed control and
// passes control on, and nothing else. Every inject-family and agent-family
// frame from it is refused before anything is read, checked or written.
var (
	Local  = Caps{Inject: true, Agent: true, Host: true}
	Joined = Caps{}
)

// Typist is how keel serve types for a client that has Caps.Inject and
// answers one that has Caps.Agent. internal/httpapi implements it: it owns
// the agent service, the vault and the log those checks need. It answers
// the client itself (Conn.Send); an error it returns is reported like any
// other control error.
type Typist interface {
	// Inject handles an inject frame. The client drives or it does not:
	// the session decides when the nonce is consumed.
	Inject(ctl wire.Control) error
	Detect(seq int, line string)
	Suggest(seq int, line string)
}

// Options says who attaches and what it may do.
type Options struct {
	Name string
	// Kind is wire.KindWindow or wire.KindJoined.
	Kind string
	Caps Caps
	// Typist makes the client's Typist. It is required with Caps.Inject or
	// Caps.Agent, and never called without one of them.
	Typist func(*Conn) Typist
	// Cols and Rows are the client's terminal size: the session takes it
	// when the client attaches as the driver.
	Cols, Rows int
	// Since, when set, limits the snapshot to output written after that
	// mark (Session.Mark): a share's viewer sees nothing from before it.
	Since *uint64
}

// Conn is one attached client.
type Conn struct {
	sess   *runtime.Session
	t      Transport
	cl     runtime.Client
	id     string // the session's id
	caps   Caps
	typist Typist
	events <-chan runtime.Event
}

// Attach subscribes a client to sess and sends it the snapshot and the
// ready frame. The caller then runs Forward, feeds Control and Keys, and
// calls Detach when the transport ends.
func Attach(sess *runtime.Session, t Transport, o Options) *Conn {
	if (o.Caps.Inject || o.Caps.Agent) && o.Typist == nil {
		panic("sessionsock: Caps.Inject and Caps.Agent need a Typist")
	}
	c := &Conn{sess: sess, t: t, caps: o.Caps}
	join := runtime.Join{Name: o.Name, Kind: o.Kind}
	var snap []byte
	if o.Since != nil {
		c.cl, c.events, snap = sess.SubscribeSince(join, *o.Since)
	} else {
		c.cl, c.events, snap = sess.Subscribe(join)
	}
	if c.cl.Role == runtime.RoleDriver {
		_ = sess.Resize(c.cl.ID, o.Cols, o.Rows)
	}
	if len(snap) > 0 {
		_ = t.SendBytes(snap)
	}
	info := sess.Info()
	c.id = info.ID
	if o.Caps.Inject || o.Caps.Agent {
		c.typist = o.Typist(c)
	}
	c.Send(c.state(wire.TypeReady, wire.MsgAttached, info, 0, 0))
	return c
}

// Session is the session the client is attached to.
func (c *Conn) Session() *runtime.Session { return c.sess }

// ClientID is the client's id in the session.
func (c *Conn) ClientID() uint64 { return c.cl.ID }

// Send sends the client a text frame. A failed send is the transport's to
// notice: its read side ends and the caller detaches.
func (c *Conn) Send(v any) { _ = c.t.SendJSON(v) }

// Detach ends the client's subscription; Forward then returns.
func (c *Conn) Detach() { c.sess.Unsubscribe(c.cl.ID) }

// state is the frame for a session event, as this client may see it.
func (c *Conn) state(kind, message string, info runtime.Info, fromID, targetID uint64) wire.State {
	you := c.cl
	you.Role = info.Role(you.ID)
	out := wire.State{
		Type:     kind,
		Message:  message,
		ID:       info.ID,
		Title:    info.Title,
		Status:   info.Status,
		Attached: info.Attached,
		DriverID: info.DriverID,
		OwnerID:  info.OwnerID,
		You:      &you,
		FromID:   fromID,
		TargetID: targetID,
	}
	if c.caps.Host {
		out.HostID = info.HostID
		out.HostKey = info.HostKey
	}
	return out
}

// Forward sends the client the session's events until the subscription
// ends (Detach, a slow client dropped, the session closed) or a binary
// frame cannot be sent. It reports whether it stopped on a failed send.
func (c *Conn) Forward() (sendFailed bool) {
	for ev := range c.events {
		switch ev.Kind {
		case runtime.EventBytes:
			if c.t.SendBytes(ev.Bytes) != nil {
				return true
			}
			continue
		case wire.TypeJoinRequest, wire.TypeJoinResolved, wire.TypeJoinFailed:
			// From the share hub (Session.Announce): someone asked to
			// watch, or that request was answered. No session state.
			c.Send(wire.State{Type: ev.Kind, ID: c.id, PeerID: ev.Peer, Message: ev.Message})
			continue
		}
		msg := c.state(ev.Kind, ev.Message, ev.Info, ev.FromID, ev.TargetID)
		if ev.Kind == wire.TypeStatus {
			msg.Status = ev.Status
			msg.ID = c.id
		}
		if ev.Status == runtime.StatusError {
			e := wire.State{Type: wire.TypeError, Message: ev.Message}
			if c.caps.Host && ev.Info.HostKey.Kind != "" {
				e.HostID = ev.Info.HostID
				e.HostKey = ev.Info.HostKey
			}
			c.Send(e)
		}
		c.Send(msg)
	}
	return false
}

// Control handles one text frame from the client. A frame that is not
// JSON or has a type this end does not know is ignored.
func (c *Conn) Control(msg []byte) {
	var ctl wire.Control
	if json.Unmarshal(msg, &ctl) != nil {
		return
	}
	var err error
	switch ctl.Type {
	case wire.TypeResize:
		err = c.sess.Resize(c.cl.ID, ctl.Cols, ctl.Rows)
	case wire.TypeRequestControl:
		err = c.sess.RequestControl(c.cl.ID)
	case wire.TypeGrantControl:
		err = c.sess.GrantControl(c.cl.ID, ctl.TargetID)
	case wire.TypeTakeControl:
		err = c.sess.TakeControl(c.cl.ID)
	case wire.TypeInjectChallenge:
		if !c.caps.Inject {
			err = runtime.ErrNotLocal
			break
		}
		var nonce string
		if nonce, err = c.sess.IssueInjectTicket(c.cl.ID, ctl.Command); err == nil {
			c.Send(wire.Challenge{Type: wire.TypeInjectChallenge, Nonce: nonce})
		}
	case wire.TypeInject:
		if !c.caps.Inject {
			err = runtime.ErrNotLocal
			break
		}
		err = c.typist.Inject(ctl)
	case wire.TypeAgentDetect:
		if !c.caps.Agent {
			err = runtime.ErrNotLocal
			break
		}
		c.typist.Detect(ctl.Seq, ctl.Line)
	case wire.TypeAgentSuggest:
		if !c.caps.Agent {
			err = runtime.ErrNotLocal
			break
		}
		c.typist.Suggest(ctl.Seq, ctl.Line)
	}
	// A follower's resize, and an inject with no or a spent nonce, are
	// dropped without a word, as a follower's keystrokes are.
	if err != nil && !errors.Is(err, runtime.ErrReadOnly) && !errors.Is(err, inject.ErrNotApproved) && !errors.Is(err, inject.ErrBadTicket) {
		c.Send(wire.State{Type: wire.TypeError, Message: err.Error()})
	}
}

// Keys writes the client's keystrokes to the PTY. Only the driver's are
// written (Session.Write): a follower's are dropped, and so are the
// driver's while the session has no bridge (connecting, reconnecting),
// never buffered for whatever prompt comes up next. Any other failure is
// sent to the client and returned.
func (c *Conn) Keys(p []byte) error {
	err := c.sess.Write(c.cl.ID, p)
	if err == nil || errors.Is(err, runtime.ErrReadOnly) || errors.Is(err, runtime.ErrNotReady) {
		return nil
	}
	c.Send(wire.State{Type: wire.TypeError, Message: err.Error()})
	return err
}
