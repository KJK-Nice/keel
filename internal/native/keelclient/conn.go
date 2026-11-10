package keelclient

import (
	"encoding/json"
	"net/http"
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

	mu                     sync.Mutex
	id, status, title, sub string
	onChange               func()
}

func dialWS(u, token string) (*Conn, error) {
	h := http.Header{authtoken.Header: {token}} // no Origin: a native client
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

// control applies a status or ready frame; other types are ignored in N1.
func (c *Conn) control(data []byte) {
	var m struct {
		Type     string `json:"type"`
		ID       string `json:"id"`
		Status   string `json:"status"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
	}
	if json.Unmarshal(data, &m) != nil || (m.Type != "ready" && m.Type != "status") {
		return
	}
	c.mu.Lock()
	changed := false
	set := func(dst *string, v string) {
		if v != "" && *dst != v {
			*dst, changed = v, true
		}
	}
	if m.Type == "ready" {
		set(&c.id, m.ID)
	}
	set(&c.status, m.Status)
	set(&c.title, m.Title)
	set(&c.sub, m.Subtitle)
	fn := c.onChange
	c.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
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

// State is the last status, title and subtitle the server sent.
func (c *Conn) State() (status, title, subtitle string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.title, c.sub
}

// OnChange sets fn, called (on the reader goroutine) when State changes or the id is learned.
func (c *Conn) OnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}
