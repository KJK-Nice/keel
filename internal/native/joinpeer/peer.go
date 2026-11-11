package joinpeer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"keel/internal/native/keelclient"
)

// STUN is the one ICE server the app uses (joinpeer.js STUN). A peer dials the signal server named in the link and
// this server, and nothing else; tests pass WithICEServers(nil).
var STUN = []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}

// The app's words (term.js joinRemote and joinpeer.js).
const (
	msgDeclined    = "The host declined your request to join."
	msgLost        = "Follow connection lost"
	msgSignalEnded = "Signaling closed"
	msgSignalFail  = "Signaling failed"
	msgHostLeft    = "Host left"
	msgDetached    = "\r\n\x1b[90mHost detached.\x1b[0m\r\n" // written into the terminal, as the app does
)

type options struct {
	ice      []webrtc.ICEServer
	loopback bool
}

// Option changes how a peer is made.
type Option func(*options)

// WithICEServers sets the ICE servers (default STUN). Tests pass none, so nothing leaves the machine.
func WithICEServers(s []webrtc.ICEServer) Option { return func(o *options) { o.ice = s } }

// WithLoopback lets the peer offer loopback candidates, for tests on one machine with no network.
func WithLoopback() Option { return func(o *options) { o.loopback = true } }

// maxQueued bounds the host's output waiting to be read: a window that stops reading fails the peer instead of
// growing without limit.
const maxQueued = 64 << 20

// Peer is a joined share. It is what a pane's connection is for a joined tab: Read returns the host's terminal bytes,
// Write sends the window's keystrokes (nothing while following), and Snapshot says who drives. It reads nothing of
// the host's machine but its terminal.
type Peer struct {
	frames *keelclient.Conn // the state the host's control frames describe; it has no socket
	ws     *websocket.Conn
	wmu    sync.Mutex // one writer on ws
	pc     *webrtc.PeerConnection
	ice    []webrtc.ICEServer
	sigURL string // the signal server's socket for this room: no key in it

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []byte
	dc     *webrtc.DataChannel
	open   bool // the data channel is open
	failed bool
	closed bool
}

// Open dials the signal server of link and starts the exchange. It returns once the socket is up: the host's answer,
// the verification and the data channel follow on their own, and the state says how it went. link.Key is used only to
// verify the host.
func Open(link Link, name string, opts ...Option) (*Peer, error) {
	o := options{ice: STUN}
	for _, f := range opts {
		f(&o)
	}
	sig := signalURL(link.Origin, link.Room, name)
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	ws, _, err := dialer.Dial(sig, nil)
	if err != nil {
		return nil, fmt.Errorf("signal server: %w", err)
	}
	se := webrtc.SettingEngine{}
	if o.loopback {
		se.SetIncludeLoopbackCandidate(true)
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{ICEServers: o.ice})
	if err != nil {
		ws.Close()
		return nil, err
	}
	p := &Peer{frames: keelclient.NewFrameConn(), ws: ws, pc: pc, ice: o.ice, sigURL: sig}
	p.cond = sync.NewCond(&p.mu)
	go p.run(link.Key)
	return p, nil
}

// ICEServers is what the peer was made with.
func (p *Peer) ICEServers() []webrtc.ICEServer { return p.ice }

// SignalURL is the signal server's socket this peer dialled. It holds the room and the name, never the key.
func (p *Peer) SignalURL() string { return p.sigURL }

// signal is a message of keel-signal (joinpeer.js).
type signal struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Mac       string `json:"mac,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	SDPMid    string `json:"sdpMid,omitempty"`
	SDPLine   *int   `json:"sdpMLineIndex,omitempty"`
}

func (p *Peer) send(m signal) {
	m.Type = "signal"
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_ = p.ws.WriteJSON(m)
}

// run reads the signal socket one message at a time (a candidate must not race ahead of its offer).
func (p *Peer) run(key string) {
	ice := &iceQueue{pc: p.pc}
	p.pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		m := signal{Kind: "ice", Candidate: init.Candidate}
		if init.SDPMid != nil {
			m.SDPMid = *init.SDPMid
		}
		if init.SDPMLineIndex != nil {
			v := int(*init.SDPMLineIndex)
			m.SDPLine = &v
		}
		p.send(m)
	})
	p.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateDisconnected {
			p.fail(msgLost, false)
		}
	})
	p.pc.OnDataChannel(p.attach)
	for {
		var m signal
		if err := p.ws.ReadJSON(&m); err != nil {
			p.mu.Lock()
			quiet := p.failed || p.closed || p.open || p.pc.ConnectionState() == webrtc.PeerConnectionStateConnected
			p.mu.Unlock()
			if !quiet {
				p.fail(msgSignalEnded, false)
			}
			return
		}
		switch m.Type {
		case "error":
			p.notice(keelclient.EventNoticeError, orElse(m.Name, "signaling error"))
		case "leave":
			_ = p.pc.Close()
			p.notice(keelclient.EventNotice, msgHostLeft)
		case "signal":
			p.onSignal(m, key, ice)
		}
	}
}

func (p *Peer) onSignal(m signal, key string, ice *iceQueue) {
	switch m.Kind {
	case "pending": // the host was asked to let us in: the state stays as it is, and the window says "waiting"
	case "denied":
		p.fail(msgDeclined, true)
	case "offer":
		if m.SDP == "" {
			return
		}
		a, err := answerOffer(p.pc, key, offer{m.SDP, m.Mac}, ice)
		if err != nil {
			p.fail(err.Error(), true)
			return
		}
		p.send(signal{Kind: "answer", SDP: a.SDP, Mac: a.MAC})
	case "ice":
		if m.Candidate == "" {
			return
		}
		c := webrtc.ICECandidateInit{Candidate: m.Candidate}
		if m.SDPMid != "" {
			mid := m.SDPMid
			c.SDPMid = &mid
		}
		if m.SDPLine != nil {
			l := uint16(*m.SDPLine)
			c.SDPMLineIndex = &l
		}
		ice.add(c)
	}
}

// attach wires the host's data channel: terminal bytes in, control frames to the state.
func (p *Peer) attach(dc *webrtc.DataChannel) {
	p.mu.Lock()
	p.dc = dc
	p.mu.Unlock()
	dc.OnOpen(func() {
		p.mu.Lock()
		p.open = true
		p.mu.Unlock()
		p.frames.Mark("ready", "")
	})
	dc.OnClose(func() {
		p.mu.Lock()
		quiet := p.failed || p.closed
		p.open = false
		p.mu.Unlock()
		if !quiet {
			p.frames.Mark("", "")
			p.enqueue([]byte(msgDetached))
		}
	})
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		p.mu.Lock()
		stop := p.failed || p.closed
		p.mu.Unlock()
		switch {
		case stop:
		case m.IsString:
			p.frames.ApplyFrame(m.Data)
		default:
			p.enqueue(m.Data)
		}
	})
}

func (p *Peer) enqueue(b []byte) {
	p.mu.Lock()
	if len(p.queue)+len(b) > maxQueued {
		p.mu.Unlock()
		p.fail("The host's output is not being read", true)
		return
	}
	p.queue = append(p.queue, b...)
	p.cond.Broadcast()
	p.mu.Unlock()
}

func (p *Peer) notice(kind, msg string) { p.frames.Notify(keelclient.Event{Kind: kind, Message: msg}) }

// fail ends the join: the state is an error with msg (shown as a banner and a toast, never as terminal output), the
// peer is closed, and nothing the host sent after this is kept.
func (p *Peer) fail(msg string, toast bool) {
	p.mu.Lock()
	if p.failed || p.closed {
		p.mu.Unlock()
		return
	}
	p.failed = true
	p.queue = nil
	p.cond.Broadcast()
	p.mu.Unlock()
	p.frames.Mark("error", msg)
	if toast {
		p.notice(keelclient.EventNoticeError, msg)
	}
	_ = p.pc.Close()
	_ = p.ws.Close()
}

func orElse(s, d string) string {
	if s != "" {
		return s
	}
	return d
}

// ---- the connection of a pane ----

// Read returns the host's terminal bytes and io.EOF once the peer is closed (Leave). A join that failed (declined, not
// verified, lost) is not an end of the stream: Read waits, so the terminal stays blank instead of drawing its own
// "[Disconnected]" over the app's error.
func (p *Peer) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.queue) == 0 {
		if p.closed {
			return 0, io.EOF
		}
		p.cond.Wait()
	}
	n := copy(b, p.queue)
	p.queue = p.queue[n:]
	return n, nil
}

// Write sends the window's keystrokes to the host. A client that does not drive sends nothing: the keystrokes are
// counted (State.Blocked, for the notice "Typing is off while you follow") and the host would drop them as well.
func (p *Peer) Write(b []byte) (int, error) {
	if p.frames.Snapshot().Role != "driver" { // until the host says this client drives, it follows
		p.frames.NoteBlocked()
		return len(b), nil
	}
	if dc := p.channel(); dc != nil {
		if err := dc.Send(b); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// channel is the data channel while it is open.
func (p *Peer) channel() *webrtc.DataChannel {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.open && !p.failed && !p.closed {
		return p.dc
	}
	return nil
}

func (p *Peer) control(v any) error {
	b, _ := json.Marshal(v)
	if dc := p.channel(); dc != nil {
		return dc.SendText(string(b))
	}
	return nil
}

// Resize tells the host the terminal's size.
func (p *Peer) Resize(cols, rows int) error {
	return p.control(map[string]any{"type": "resize", "cols": cols, "rows": rows})
}

// Close leaves the share: the peer and the signal socket close. The host's session is not touched.
func (p *Peer) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.queue = nil
	p.cond.Broadcast()
	p.mu.Unlock()
	_ = p.pc.Close()
	return p.ws.Close()
}

// SessionID is "": a joined share is not a session of this machine's keel serve, so nothing that kills, reconnects or
// saves a session may be pointed at it by way of an id.
func (p *Peer) SessionID() string { return "" }

// Snapshot is what the host said about the session.
func (p *Peer) Snapshot() keelclient.State { return p.frames.Snapshot() }

// Events hands over what was announced since the last call.
func (p *Peer) Events() []keelclient.Event { return p.frames.Events() }

// OnChange sets fn, called when the state changes or something is announced.
func (p *Peer) OnChange(fn func()) { p.frames.OnChange(fn) }

// TakeControl, RequestControl and GrantControl send the same control messages as the app's joined tab.
func (p *Peer) TakeControl() error    { return p.control(map[string]any{"type": "take-control"}) }
func (p *Peer) RequestControl() error { return p.control(map[string]any{"type": "request-control"}) }
func (p *Peer) GrantControl(id uint64) error {
	return p.control(map[string]any{"type": "grant-control", "targetId": id})
}

// ErrNoApprovedInject: a joined pane takes no snippet (Approve before type is for a local session; a later plan).
var ErrNoApprovedInject = errors.New("Session is not connected")

// ApprovedInject types nothing into a joined pane. The window never asks; this is the refusal if it did.
func (p *Peer) ApprovedInject(context.Context, string, *keelclient.AgentRef) error {
	return ErrNoApprovedInject
}
