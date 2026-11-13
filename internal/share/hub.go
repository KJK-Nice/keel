package share

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"keel/internal/runtime"
	"keel/internal/sessionsock"
	"keel/internal/sharelink"
	"keel/internal/wire"
)

type Hub struct {
	// The signal server comes from, strongest first: env (KEEL_SIGNAL for
	// this run, or New's argument), saved (Settings · Sharing, SetSaved)
	// and build (DefaultSignal). saved changes at run time, under mu.
	env, saved, build string
	rt                *runtime.Runtime
	mu                sync.Mutex
	bySess            map[string]*liveShare
}

// Where the signal server in use comes from (Hub.Source).
const (
	SourceEnv      = "env"
	SourceSettings = "settings"
	SourceBuild    = "build"
)

type liveShare struct {
	sessionID string
	// signal is the server the share was made on. A share keeps it when the
	// saved server changes; the next one uses the new server.
	signal  string
	roomID  string
	hostKey string
	joinURL string // set under Hub.mu once the host is connected
	ctx     context.Context
	cancel  context.CancelFunc
	ready   chan struct{} // closed when the share is up or has failed
	err     error         // set before ready is closed

	// secret goes in the join link's #fragment and authenticates both
	// WebRTC peers (verify.go); the signal server never sees it.
	secret string
	// mark is the session's output position when sharing began: viewers
	// get nothing written before it.
	mark uint64
	// admit carries the host's answers to join requests into hostLoop.
	admit chan admitReq
}

type admitReq struct {
	peerID string
	allow  bool
	done   chan error
}

// pendingPeer is a joiner waiting for the host to allow or deny it.
type pendingPeer struct {
	name  string
	timer *time.Timer
}

const (
	// maxPeers caps WebRTC viewers per share (each is a PeerConnection).
	maxPeers = 16
	// sendHigh and sendLow pace a viewer's data channel, which pion queues
	// without bound. Past sendHigh the attach loop waits for it to drain to
	// sendLow, so unread output waits in the runtime's per-client queue,
	// and the runtime drops the viewer once that passes its byte budget.
	// The two buffers no longer stack up to twice the budget.
	sendHigh = 1 << 20
	sendLow  = 256 << 10
	// sendStall drops a viewer whose data channel drains nothing for this
	// long while output is waiting.
	sendStall = 15 * time.Second
	// maxPending caps join requests waiting on the host, so a flood of
	// joiners can't pile prompts onto the host's screen.
	maxPending = 4
)

// admitTimeout is how long a join request waits for the host before it is
// denied. A var so tests can shorten it.
var admitTimeout = 2 * time.Minute

// Admit errors.
var (
	ErrNotShared     = errors.New("session is not shared")
	ErrNotResponding = errors.New("share is not responding")
	ErrNoRequest     = errors.New("no such join request (it may have expired)")
)

// DefaultSignal is the compile-time signaling URL. Finder-launched Keel.app
// has no shell env, so bake it in:
//
//	go build -ldflags "-X keel/internal/share.DefaultSignal=https://…"
var DefaultSignal string

// New makes a hub on signalURL, which no saved setting overrides.
func New(signalURL string, rt *runtime.Runtime) *Hub {
	return &Hub{
		env:    trimSignal(signalURL),
		rt:     rt,
		bySess: map[string]*liveShare{},
	}
}

// FromEnv makes a hub on KEEL_SIGNAL, falling back to the saved server
// (SetSaved) and then DefaultSignal.
func FromEnv(rt *runtime.Runtime) *Hub {
	h := New(os.Getenv("KEEL_SIGNAL"), rt)
	h.build = trimSignal(DefaultSignal)
	return h
}

func trimSignal(u string) string {
	return strings.TrimRight(strings.TrimSpace(u), "/")
}

// SetSaved sets the server saved in Settings · Sharing ("" for none).
// Shares already running keep the server they were made on.
func (h *Hub) SetSaved(u string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.saved = trimSignal(u)
	h.mu.Unlock()
}

// current is the signal server in use and where it comes from.
func (h *Hub) current() (string, string) {
	if h == nil {
		return "", ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.env != "":
		return h.env, SourceEnv
	case h.saved != "":
		return h.saved, SourceSettings
	case h.build != "":
		return h.build, SourceBuild
	}
	return "", ""
}

func (h *Hub) Enabled() bool {
	u, _ := h.current()
	return u != ""
}

func (h *Hub) URL() string {
	u, _ := h.current()
	return u
}

// Source says where URL comes from: SourceEnv, SourceSettings, SourceBuild,
// or "" when there is no server.
func (h *Hub) Source() string {
	_, src := h.current()
	return src
}

// NormalizeSignal checks a signal server URL typed in Settings: an absolute
// http or https URL with a host and no user, query or fragment (join links
// are built on it). "" stays "" (no server). Trailing slashes are dropped.
func NormalizeSignal(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 {
		return "", errors.New("signal server URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Opaque != "" {
		return "", errors.New("signal server must be an http:// or https:// URL, like https://signal.example.com")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return "", errors.New("signal server URL cannot have a user, ?query or #fragment")
	}
	return trimSignal(u.String()), nil
}

func (h *Hub) JoinURL(sessionID string) string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.bySess[sessionID]; s != nil {
		return s.joinURL
	}
	return ""
}

// Share returns the join link for sessionID, starting a share if there is
// none. Concurrent calls for one session wait for the same share instead of
// each creating a room.
func (h *Hub) Share(sessionID string) (string, error) {
	signal := h.URL()
	if signal == "" {
		return "", errors.New("set a signal server in Settings · Sharing (or KEEL_SIGNAL) to share")
	}
	sess, err := h.rt.Get(sessionID)
	if err != nil {
		return "", err
	}
	secret, err := sharelink.NewKey()
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	if s := h.bySess[sessionID]; s != nil {
		h.mu.Unlock()
		return h.await(s, false)
	}
	ctx, cancel := context.WithCancel(context.Background())
	live := &liveShare{
		sessionID: sessionID,
		signal:    signal,
		ctx:       ctx,
		cancel:    cancel,
		ready:     make(chan struct{}),
		secret:    secret,
		mark:      sess.Mark(),
		admit:     make(chan admitReq),
	}
	h.bySess[sessionID] = live
	h.mu.Unlock()

	roomID, hostKey, err := createRoom(signal)
	if err != nil {
		h.fail(live, err)
		return "", err
	}
	live.roomID, live.hostKey = roomID, hostKey
	go h.hostLoop(ctx, live, sharelink.JoinURL(signal, roomID, secret))
	return h.await(live, true)
}

// await waits for s to come up. Only the caller that created the share
// ends it on timeout: a caller that joined later would otherwise cancel a
// share the creator is still waiting on, failing both.
func (h *Hub) await(s *liveShare, creator bool) (string, error) {
	select {
	case <-s.ready:
	case <-time.After(10 * time.Second):
		if creator {
			h.end(s)
		}
		return "", errors.New("signaling host did not connect")
	}
	if s.err != nil {
		return "", s.err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return s.joinURL, nil
}

// fail ends a share that never came up.
func (h *Hub) fail(s *liveShare, err error) {
	s.err = err
	close(s.ready)
	h.end(s)
}

// end removes s if it is still the session's share and cancels it.
func (h *Hub) end(s *liveShare) {
	h.mu.Lock()
	if h.bySess[s.sessionID] == s {
		delete(h.bySess, s.sessionID)
	}
	h.mu.Unlock()
	s.cancel()
}

// Stop ends sharing for sessionID: the signaling room closes, every viewer
// is disconnected, and the join link stops working.
func (h *Hub) Stop(sessionID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	s := h.bySess[sessionID]
	delete(h.bySess, sessionID)
	h.mu.Unlock()
	if s != nil {
		s.cancel()
	}
}

// Admit answers a pending join request on sessionID's share: allow starts
// the WebRTC exchange with that joiner, deny tells it to go away.
func (h *Hub) Admit(sessionID, peerID string, allow bool) error {
	if h == nil {
		return ErrNotShared
	}
	h.mu.Lock()
	s := h.bySess[sessionID]
	h.mu.Unlock()
	if s == nil {
		return ErrNotShared
	}
	req := admitReq{peerID: peerID, allow: allow, done: make(chan error, 1)}
	select {
	case s.admit <- req:
	case <-s.ctx.Done():
		return ErrNotShared
	case <-time.After(5 * time.Second):
		return ErrNotResponding
	}
	select {
	case err := <-req.done:
		return err
	case <-s.ctx.Done():
		return ErrNotShared
	}
}

func createRoom(base string) (id, hostKey string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/rooms", nil)
	if err != nil {
		return "", "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("signaling: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return "", "", fmt.Errorf("signaling room: %s", bytes.TrimSpace(b))
	}
	var body struct {
		ID      string `json:"id"`
		HostKey string `json:"hostKey"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body); err != nil {
		return "", "", err
	}
	if body.ID == "" {
		return "", "", errors.New("signaling did not return a room id")
	}
	return body.ID, body.HostKey, nil
}

func (h *Hub) hostLoop(ctx context.Context, live *liveShare, joinURL string) {
	// However the loop ends (Stop, signaling server gone, network), the
	// session no longer has a share: drop the entry so the next Share makes
	// a fresh room instead of handing out a dead link.
	defer h.end(live)
	wsURL := sharelink.SocketURL(live.signal, url.Values{"role": {wire.SignalRoleHost}, "room": {live.roomID}, "key": {live.hostKey}})
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 8 * time.Second // DefaultDialer allows 45s
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		log.Printf("share signal dial: %v", err)
		live.err = fmt.Errorf("signaling: %w", err)
		close(live.ready)
		return
	}
	conn.SetReadLimit(64 << 10)
	h.mu.Lock()
	live.joinURL = joinURL
	h.mu.Unlock()
	close(live.ready)
	defer conn.Close()

	var writeMu sync.Mutex
	write := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	sess, err := h.rt.Get(live.sessionID)
	if err != nil {
		return
	}
	// Everything below runs on this goroutine: the signaling reader, the
	// host's admit answers and request timeouts all feed this select.
	msgs := make(chan wire.Signal)
	go func() {
		defer close(msgs)
		for {
			var msg wire.Signal
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			select {
			case msgs <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	peers := map[string]*peerConn{}
	pending := map[string]*pendingPeer{}
	expired := make(chan string)
	signal := func(peerID string, out wire.Signal) {
		out.Type = wire.SignalMessage
		out.PeerID = peerID
		_ = write(out)
	}
	resolve := func(peerID, outcome string) *pendingPeer {
		p := pending[peerID]
		if p == nil {
			return nil
		}
		p.timer.Stop()
		delete(pending, peerID)
		sess.Announce(runtime.Event{Kind: wire.TypeJoinResolved, Peer: peerID, Message: outcome})
		return p
	}
	defer func() {
		for id := range pending {
			resolve(id, "ended")
		}
		for _, p := range peers {
			p.close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case peerID := <-expired:
			if resolve(peerID, "expired") != nil {
				signal(peerID, wire.Signal{Kind: wire.SignalDenied})
			}
		case req := <-live.admit:
			p := resolve(req.peerID, map[bool]string{true: "allowed", false: "denied"}[req.allow])
			if p == nil {
				req.done <- ErrNoRequest
				continue
			}
			req.done <- nil
			if !req.allow {
				signal(req.peerID, wire.Signal{Kind: wire.SignalDenied})
				continue
			}
			peerID := req.peerID
			pc, err := h.acceptPeer(live, sess, p.name, func(out wire.Signal) { signal(peerID, out) })
			if err != nil {
				log.Printf("share peer: %v", err)
				signal(peerID, wire.Signal{Kind: wire.SignalDenied})
				sess.Announce(runtime.Event{Kind: wire.TypeJoinFailed, Peer: peerID, Message: p.name + " could not be connected: " + err.Error()})
				continue
			}
			peers[peerID] = pc
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			switch msg.Type {
			case wire.SignalPeer:
				peerID := msg.PeerID
				if peerID == "" || pending[peerID] != nil || peers[peerID] != nil {
					continue
				}
				if len(peers)+len(pending) >= maxPeers || len(pending) >= maxPending {
					log.Printf("share: too many viewers or requests, turning one away")
					signal(peerID, wire.Signal{Kind: wire.SignalDenied})
					continue
				}
				pending[peerID] = &pendingPeer{
					name: msg.Name,
					timer: time.AfterFunc(admitTimeout, func() {
						select {
						case expired <- peerID:
						case <-ctx.Done():
						}
					}),
				}
				signal(peerID, wire.Signal{Kind: wire.SignalPending})
				sess.Announce(runtime.Event{Kind: wire.TypeJoinRequest, Peer: peerID, Message: msg.Name})
			case wire.SignalLeave:
				resolve(msg.PeerID, "left")
				if p := peers[msg.PeerID]; p != nil {
					p.close()
					delete(peers, msg.PeerID)
				}
			case wire.SignalMessage:
				p := peers[msg.PeerID]
				if p == nil {
					continue
				}
				if err := p.onSignal(msg, live.secret); err != nil {
					log.Printf("share peer %s: %v", msg.PeerID, err)
					p.close()
					delete(peers, msg.PeerID)
					signal(msg.PeerID, wire.Signal{Kind: wire.SignalDenied})
					why := err.Error()
					if errors.Is(err, errBadMAC) {
						why = "failed verification (wrong link, or the signal server tampered with the connection)"
					}
					sess.Announce(runtime.Event{Kind: wire.TypeJoinFailed, Peer: msg.PeerID, Message: p.name + " was not connected: " + why})
				}
			}
		}
	}
}

type peerConn struct {
	pc     *webrtc.PeerConnection
	cancel context.CancelFunc
	name   string
	// Candidates that arrive before the answer (the joiner starts gathering
	// as soon as it sets its local description) wait here: pion rejects a
	// candidate with no remote description.
	answered bool
	early    []webrtc.ICECandidateInit
}

func (p *peerConn) close() {
	if p == nil {
		return
	}
	p.cancel()
	_ = p.pc.Close()
}

// errBadMAC means an answer's fingerprints aren't vouched for by the link
// secret: the joiner has the wrong link, or the signal server is in the
// middle.
var errBadMAC = errors.New("answer failed verification against the share link")

func (p *peerConn) onSignal(msg wire.Signal, secret string) error {
	switch msg.Kind {
	case wire.SignalAnswer:
		if p.answered {
			return nil
		}
		if !sharelink.CheckMAC(secret, wire.SignalRoleJoin, msg.SDP, msg.Mac) {
			return errBadMAC
		}
		if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP}); err != nil {
			return err
		}
		p.answered = true
		for _, c := range p.early {
			_ = p.pc.AddICECandidate(c)
		}
		p.early = nil
	case wire.SignalICE:
		var mid *string
		if msg.SDPMid != "" {
			m := msg.SDPMid
			mid = &m
		}
		var line *uint16
		if msg.SDPLine != nil {
			v := uint16(*msg.SDPLine)
			line = &v
		}
		c := webrtc.ICECandidateInit{Candidate: msg.Candidate, SDPMid: mid, SDPMLineIndex: line}
		if !p.answered {
			if len(p.early) < 64 {
				p.early = append(p.early, c)
			}
			return nil
		}
		_ = p.pc.AddICECandidate(c)
	}
	return nil
}

func (h *Hub) acceptPeer(live *liveShare, sess *runtime.Session, name string, send func(wire.Signal)) (*peerConn, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{sharelink.STUN}}},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &peerConn{pc: pc, cancel: cancel, name: name}

	// pion starts gathering in SetLocalDescription, so a candidate can be
	// ready before the offer is sent; hold candidates until it is, or the
	// joiner gets them with no remote description and drops them.
	var iceMu sync.Mutex
	offered := false
	var held []wire.Signal
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		msg := wire.Signal{Kind: wire.SignalICE, Candidate: init.Candidate}
		if init.SDPMid != nil {
			msg.SDPMid = *init.SDPMid
		}
		if init.SDPMLineIndex != nil {
			v := int(*init.SDPMLineIndex)
			msg.SDPLine = &v
		}
		iceMu.Lock()
		defer iceMu.Unlock()
		if !offered {
			held = append(held, msg)
			return
		}
		send(msg)
	})

	dc, err := pc.CreateDataChannel("keel", nil)
	if err != nil {
		cancel()
		_ = pc.Close()
		return nil, err
	}
	attachDataChannel(sess, dc, name, live.mark)
	go func() {
		<-ctx.Done()
		_ = pc.Close()
	}()

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		p.close()
		return nil, err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		p.close()
		return nil, err
	}
	iceMu.Lock()
	send(wire.Signal{Kind: wire.SignalOffer, SDP: offer.SDP, Mac: sharelink.MAC(live.secret, wire.SignalRoleHost, offer.SDP)})
	offered = true
	for _, m := range held {
		send(m)
	}
	held = nil
	iceMu.Unlock()
	return p, nil
}

// dcTransport carries the session socket's frames (sessionsock.Transport)
// over a viewer's data channel, pacing the output: see sendHigh.
type dcTransport struct {
	dc     *webrtc.DataChannel
	low    chan struct{}
	closed chan struct{}
}

func (t *dcTransport) SendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return t.dc.SendText(string(b))
}

// SendBytes waits until the data channel is under sendHigh, then sends. It
// fails when the channel closed, or stalled for sendStall.
func (t *dcTransport) SendBytes(p []byte) error {
	for t.dc.BufferedAmount() > sendHigh {
		timer := time.NewTimer(sendStall)
		select {
		case <-t.low:
			timer.Stop()
		case <-t.closed:
			timer.Stop()
			return errors.New("share: the viewer's data channel closed")
		case <-timer.C:
			return errors.New("share: the viewer's data channel stalled")
		}
	}
	return t.dc.Send(p)
}

// attachDataChannel attaches a joined peer to sess once its data channel
// opens. The peer is a sessionsock.Joined client: it watches from mark on,
// drives when the owner hands it control and passes control on. keel serve
// never types for it and tells it nothing of the host.
func attachDataChannel(sess *runtime.Session, dc *webrtc.DataChannel, name string, mark uint64) {
	dc.OnOpen(func() {
		t := &dcTransport{dc: dc, low: make(chan struct{}, 1), closed: make(chan struct{})}
		dc.SetBufferedAmountLowThreshold(sendLow)
		dc.OnBufferedAmountLow(func() {
			select {
			case t.low <- struct{}{}:
			default:
			}
		})
		c := sessionsock.Attach(sess, t, sessionsock.Options{Name: name, Kind: wire.KindJoined, Caps: sessionsock.Joined, Since: &mark})
		go func() {
			if c.Forward() {
				// The viewer is not reading: it is dropped.
				c.Detach()
				_ = dc.Close()
			}
		}()
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if m.IsString {
				c.Control(m.Data)
				return
			}
			_ = c.Keys(m.Data)
		})
		dc.OnClose(func() {
			close(t.closed)
			c.Detach()
		})
	})
}
