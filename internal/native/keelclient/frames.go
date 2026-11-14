package keelclient

import "keel/internal/wire"

// A joined share has no socket to keel serve: its control frames arrive on a WebRTC data channel (joinpeer). It
// keeps the state those frames describe in a Conn that has no socket, so that a joined pane and a local one read
// the same Snapshot. The socket methods of such a Conn are never called: the peer has its own Read, Write and Close.

// NewFrameConn is a Conn with no socket.
func NewFrameConn() *Conn { return &Conn{} }

// ApplyFrame applies one text frame (ready, status, presence, control or error) as Read would.
func (c *Conn) ApplyFrame(data []byte) { c.control(data) }

// Notify queues an announcement for the window.
func (c *Conn) Notify(e Event) { c.push(e) }

// Mark sets the status and its message as they are, empty included (a frame never clears them).
func (c *Conn) Mark(status wire.Status, message string) {
	c.mu.Lock()
	changed := c.state.Status != status || c.state.Message != message
	c.state.Status, c.state.Message = status, message
	if status == wire.StatusReady {
		c.state.WasReady = true
	}
	fn := c.onChange
	c.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

// Notices of a joined share: a plain toast, and a failure.
const (
	EventNotice      = "notice"
	EventNoticeError = "notice-error"
)
