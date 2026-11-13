package keelclient

import (
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/authtoken"
	"keel/internal/wire"
)

// Conn is an io.ReadWriteCloser over /ws/session: binary frames are PTY bytes,
// text frames are JSON control. Only Write sends bytes to the PTY; it is called
// by the terminal view with the user's keystrokes.
type Conn struct {
	ws  *websocket.Conn
	wmu sync.Mutex // gorilla allows one concurrent writer
	buf []byte     // only touched by Read

	mu       sync.Mutex
	id       string
	state    State
	onChange func()

	ticket chan string // the approval request waiting for keel serve's nonce (inject.go), or nil
	// The agent proposal's Type waiting for keel serve's agent-type answer, and the proposal it is for (inject.go).
	agentWaiter chan agentAnswer
	agentFor    string
	wait        time.Duration // how long it waits; 0 is the default (tests shorten it)

	// A type without the dialog (DirectType, inject.go): one at a time, and the answers by the ref each frame carried.
	directBusy    bool
	directSeq     int
	directWaiters map[string]chan directAnswer

	events []Event // what the share hub announced, until Events hands it over

	// In-pane suggestions (inject.go): what keel serve answered, until SuggestEvents hands it over; the bytes of the
	// suggestions it sent (only this file's insert may send them); and the insert waiting for keel serve's answer.
	sugEvents    []SuggestEvent
	sugKept      []keptSuggestion
	insertWaiter chan agentAnswer
	insertFor    string
}

// Peer is a client attached to a session, as the window shows it (the frame's own shape is wire.Client). Kind is KindWindow
// or KindJoined; Role is owner, driver or follower.
type Peer struct {
	ID               uint64
	Name, Kind, Role string
	AttachedAt       string // RFC 3339; "" when the server did not say
}

// How a session's client came (Peer.Kind): the Keel window on the session's machine, or a share link.
const (
	KindWindow = wire.KindWindow
	KindJoined = wire.KindJoined
)

func peers(in []wire.Client) []Peer {
	if in == nil {
		return nil
	}
	out := make([]Peer, len(in))
	for i, c := range in {
		out[i] = Peer{ID: c.ID, Name: c.Name, Kind: c.Kind, Role: string(c.Role)}
		if !c.AttachedAt.IsZero() {
			out[i].AttachedAt = c.AttachedAt.Format(time.RFC3339Nano)
		}
	}
	return out
}

// HostKey is a host-key refusal (wire.HostKey). Kind is HostKeyUnknown, HostKeyMismatch or "" (none).
// Token is a one-time capability: String leaves it out so that printing a HostKey never shows it.
type HostKey = wire.HostKey

const (
	HostKeyUnknown  = wire.HostKeyUnknown
	HostKeyMismatch = wire.HostKeyMismatch
)

// State is what the socket's control frames say about a session.
type State struct {
	Status, Title, Subtitle, Message string
	Role                             string // owner | driver | follower; "" before ready
	Attached                         []Peer
	DriverID                         uint64
	OwnerID                          uint64    // the client that may take control back
	YouID                            uint64    // this client's id (Peer.ID of the Attached entry that is us)
	Blocked                          uint64    // keystrokes dropped because this client was not driving (the "Typing is off" notice)
	HostKey                          HostKey   // zero when there is no host-key problem
	WasReady                         bool      // the session was ready at some point (a later failure is a lost connection)
	Log                              []LogLine // the connection log a failed or connecting pane shows and Copy logs copies (log.go)
}

func dialWS(u, token string) (*Conn, error) {
	h := http.Header{authtoken.Header: {token}} // a native client sends no origin header
	ws, _, err := websocket.DefaultDialer.Dial(u, h)
	if err != nil {
		// gorilla's error names the status, never the headers we sent.
		return nil, err
	}
	return &Conn{ws: ws}, nil
}

// Read returns PTY bytes. Control frames are consumed here and update State.
func (c *Conn) Read(p []byte) (int, error) {
	for len(c.buf) == 0 {
		typ, data, err := c.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		if typ == websocket.BinaryMessage {
			c.buf = data
			continue
		}
		if !c.injectFrame(data) {
			c.control(data)
		}
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// control applies a ready, status, presence, control or error frame, and queues the share hub's join frames as
// Events; other types are ignored.
func (c *Conn) control(data []byte) {
	var m wire.State
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch m.Type {
	case wire.TypeJoinRequest, wire.TypeJoinResolved, wire.TypeJoinFailed:
		c.push(Event{Kind: m.Type, PeerID: m.PeerID, Message: m.Message})
		return
	case wire.TypeReady, wire.TypeStatus, wire.TypePresence, wire.TypeControl, wire.TypeError:
	default:
		return
	}
	if m.Type == wire.TypeControl {
		switch {
		case m.FromID != 0 && m.TargetID == 0: // a request for control: asked, not yet passed
			c.push(Event{Kind: EventControlRequest, FromID: m.FromID, Message: m.Message})
		case m.Message != "":
			c.push(Event{Kind: EventControlNote, Message: m.Message})
		}
	}
	c.mu.Lock()
	before := c.state
	id := c.id
	s := &c.state
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	if m.Type == wire.TypeReady && m.ID != "" {
		c.id = m.ID
	}
	status := string(m.Status)
	if status == "" && m.Type == wire.TypeError {
		status = string(wire.StatusError)
	}
	s.recordLog(before.Status, status, m.Message)
	set(&s.Status, string(m.Status))
	set(&s.Title, m.Title)
	if m.Type != wire.TypeError {
		if m.You != nil {
			s.Role = string(m.You.Role)
			s.Attached, s.DriverID, s.OwnerID, s.YouID = peers(m.Attached), m.DriverID, m.OwnerID, m.You.ID
		}
	}
	switch {
	case m.Type == wire.TypeError || m.Status == wire.StatusError || m.Status == wire.StatusExited:
		set(&s.Message, m.Message)
	case m.Status == wire.StatusReady:
		s.Message = ""
		s.WasReady = true
	}
	s.HostKey = m.HostKey // zero when the frame names none
	changed := id != c.id || !sameState(before, *s)
	fn := c.onChange
	c.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

func sameState(a, b State) bool {
	return a.Blocked == b.Blocked && a.Status == b.Status && a.Title == b.Title && a.Subtitle == b.Subtitle && a.Message == b.Message &&
		a.Role == b.Role && a.DriverID == b.DriverID && a.OwnerID == b.OwnerID && a.YouID == b.YouID && a.HostKey == b.HostKey && a.WasReady == b.WasReady &&
		slices.Equal(a.Attached, b.Attached) && slices.Equal(a.Log, b.Log)
}

func (c *Conn) write(typ int, b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteMessage(typ, b)
}

// send is the one way a text frame leaves this client: a wire.Control and nothing else. Which of them may ask keel
// serve to type is inject.go's alone (internal/native/guard_test.go).
func (c *Conn) send(ctl wire.Control) error {
	b, err := json.Marshal(ctl)
	if err != nil {
		return err
	}
	return c.write(websocket.TextMessage, b)
}

// Write sends keystrokes to the PTY. keel serve writes only the driver's keys, so a client that it has not said drives
// sends nothing; once it has said this client follows, the dropped keystrokes are counted in State.Blocked, which the
// window turns into "Typing is off while you follow".
func (c *Conn) Write(p []byte) (int, error) {
	if !c.KeysGo() {
		return len(p), nil
	}
	if err := c.write(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// KeysGo is the one rule for keystrokes, for a local pane and a joined one (joinpeer): they are sent only while keel
// serve says this client is the driver, as keel serve itself writes only the driver's. When it does not, the keystroke is
// dropped, and counted as blocked if the client was told it follows (before the ready frame nothing is counted).
func (c *Conn) KeysGo() bool {
	c.mu.Lock()
	role := wire.Role(c.state.Role)
	c.mu.Unlock()
	if role == wire.RoleDriver {
		return true
	}
	if role != "" {
		c.block()
	}
	return false
}

// typingReady is the one check before this client asks keel serve to type for it or asks a suggestion question: a ready
// session that it drives.
func (c *Conn) typingReady() error {
	st := c.Snapshot()
	switch {
	case st.Status != string(wire.StatusReady):
		return ErrNotReady
	case st.Role != string(wire.RoleDriver):
		return ErrNotDriver
	}
	return nil
}

// Resize tells the server the terminal's size.
func (c *Conn) Resize(cols, rows int) error {
	return c.send(wire.Control{Type: wire.TypeResize, Cols: cols, Rows: rows})
}

// Close detaches; the session keeps running on the server.
func (c *Conn) Close() error { return c.ws.Close() }

// SessionID is the server's id for this session, set by the ready frame.
func (c *Conn) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

// Snapshot is what the server last said about the session.
func (c *Conn) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.state
	s.Attached = slices.Clone(s.Attached)
	return s
}

// TakeControl asks the server to make this client the driver (it refuses unless this client owns the session).
func (c *Conn) TakeControl() error { return c.send(wire.Control{Type: wire.TypeTakeControl}) }

// RequestControl asks the driver to pass control to this client.
func (c *Conn) RequestControl() error { return c.send(wire.Control{Type: wire.TypeRequestControl}) }

// GrantControl makes the attached client peerID the driver (the driver or the owner may).
func (c *Conn) GrantControl(peerID uint64) error {
	return c.send(wire.Control{Type: wire.TypeGrantControl, TargetID: peerID})
}

// OnChange sets fn, called (on the reader goroutine) when State changes or the id is learned.
func (c *Conn) OnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}
