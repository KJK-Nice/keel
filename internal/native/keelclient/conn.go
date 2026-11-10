package keelclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/gorilla/websocket"

	"keel/internal/authtoken"
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
}

// Peer is a client attached to a session. Role is owner, driver or follower.
type Peer struct {
	ID               uint64 `json:"id"`
	Name, Kind, Role string
}

// HostKey is a host-key refusal. Kind is "unknown-host", "host-key-mismatch" or "" (none).
// Token is a one-time capability: String leaves it out so that printing a HostKey never shows it.
type HostKey struct{ Kind, Hostname, Fingerprint, Expected, KeyType, Token string }

func (k HostKey) String() string {
	return fmt.Sprintf("{%s %s %s expected=%s %s}", k.Kind, k.Hostname, k.Fingerprint, k.Expected, k.KeyType)
}

// State is what the socket's control frames say about a session.
type State struct {
	Status, Title, Subtitle, Message string
	Role                             string // owner | driver | follower; "" before ready
	Attached                         []Peer
	DriverID                         uint64
	OwnerID                          uint64  // the client that may take control back
	YouID                            uint64  // this client's id (Peer.ID of the Attached entry that is us)
	HostKey                          HostKey // zero when there is no host-key problem
	WasReady                         bool    // the session was ready at some point (a later failure is a lost connection)
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
		c.control(data)
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// control applies a ready, status, presence, control or error frame; other types are ignored.
func (c *Conn) control(data []byte) {
	var m struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		Status      string `json:"status"`
		Title       string `json:"title"`
		Subtitle    string `json:"subtitle"`
		Message     string `json:"message"`
		Attached    []Peer `json:"attached"`
		DriverID    uint64 `json:"driverId"`
		OwnerID     uint64 `json:"ownerId"`
		You         *Peer  `json:"you"`
		ErrorKind   string `json:"errorKind"`
		Hostname    string `json:"hostname"`
		Fingerprint string `json:"fingerprint"`
		Expected    string `json:"expected"`
		KeyType     string `json:"keyType"`
		TrustToken  string `json:"trustToken"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch m.Type {
	case "ready", "status", "presence", "control", "error":
	default:
		return
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
	if m.Type == "ready" && m.ID != "" {
		c.id = m.ID
	}
	set(&s.Status, m.Status)
	set(&s.Title, m.Title)
	set(&s.Subtitle, m.Subtitle)
	if m.Type != "error" {
		if m.You != nil {
			s.Role = m.You.Role
			s.Attached, s.DriverID, s.OwnerID, s.YouID = m.Attached, m.DriverID, m.OwnerID, m.You.ID
		}
	}
	switch {
	case m.Type == "error" || m.Status == "error" || m.Status == "exited":
		set(&s.Message, m.Message)
	case m.Status == "ready":
		s.Message = ""
		s.WasReady = true
	}
	if m.ErrorKind != "" {
		s.HostKey = HostKey{m.ErrorKind, m.Hostname, m.Fingerprint, m.Expected, m.KeyType, m.TrustToken}
	} else {
		s.HostKey = HostKey{}
	}
	changed := id != c.id || !sameState(before, *s)
	fn := c.onChange
	c.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

func sameState(a, b State) bool {
	return a.Status == b.Status && a.Title == b.Title && a.Subtitle == b.Subtitle && a.Message == b.Message &&
		a.Role == b.Role && a.DriverID == b.DriverID && a.OwnerID == b.OwnerID && a.YouID == b.YouID && a.HostKey == b.HostKey && a.WasReady == b.WasReady &&
		slices.Equal(a.Attached, b.Attached)
}

func (c *Conn) write(typ int, b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteMessage(typ, b)
}

// Write sends keystrokes to the PTY.
func (c *Conn) Write(p []byte) (int, error) {
	if err := c.write(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Resize tells the server the terminal's size.
func (c *Conn) Resize(cols, rows int) error {
	b, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
	return c.write(websocket.TextMessage, b)
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
func (c *Conn) TakeControl() error {
	return c.write(websocket.TextMessage, []byte(`{"type":"take-control"}`))
}

// RequestControl asks the driver to pass control to this client.
func (c *Conn) RequestControl() error {
	return c.write(websocket.TextMessage, []byte(`{"type":"request-control"}`))
}

// GrantControl makes the attached client peerID the driver (the driver or the owner may).
func (c *Conn) GrantControl(peerID uint64) error {
	b, _ := json.Marshal(map[string]any{"type": "grant-control", "targetId": peerID})
	return c.write(websocket.TextMessage, b)
}

// OnChange sets fn, called (on the reader goroutine) when State changes or the id is learned.
func (c *Conn) OnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}
