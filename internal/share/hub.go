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

	"keel/internal/inject"
	"keel/internal/runtime"
)

type Hub struct {
	signal string
	rt     *runtime.Runtime
	mu     sync.Mutex
	bySess map[string]*liveShare
}

type liveShare struct {
	sessionID string
	roomID    string
	hostKey   string
	joinURL   string // set under Hub.mu once the host is connected
	ctx       context.Context
	cancel    context.CancelFunc
	ready     chan struct{} // closed when the share is up or has failed
	err       error         // set before ready is closed

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

func New(signalURL string, rt *runtime.Runtime) *Hub {
	return &Hub{
		signal: strings.TrimRight(strings.TrimSpace(signalURL), "/"),
		rt:     rt,
		bySess: map[string]*liveShare{},
	}
}

func FromEnv(rt *runtime.Runtime) *Hub {
	u := os.Getenv("KEEL_SIGNAL")
	if strings.TrimSpace(u) == "" {
		u = DefaultSignal
	}
	return New(u, rt)
}

func (h *Hub) Enabled() bool {
	return h != nil && h.signal != ""
}

func (h *Hub) URL() string {
	if h == nil {
		return ""
	}
	return h.signal
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
	if h == nil || h.signal == "" {
		return "", errors.New("set KEEL_SIGNAL to a public signaling server (keel-signal)")
	}
	sess, err := h.rt.Get(sessionID)
	if err != nil {
		return "", err
	}
	secret, err := newShareSecret()
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
		ctx:       ctx,
		cancel:    cancel,
		ready:     make(chan struct{}),
		secret:    secret,
		mark:      sess.Mark(),
		admit:     make(chan admitReq),
	}
	h.bySess[sessionID] = live
	h.mu.Unlock()

	roomID, hostKey, err := createRoom(h.signal)
	if err != nil {
		h.fail(live, err)
		return "", err
	}
	live.roomID, live.hostKey = roomID, hostKey
	go h.hostLoop(ctx, live, h.signal+"/join?room="+url.QueryEscape(roomID)+"#k="+secret)
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

type sigMsg struct {
	Type      string `json:"type"`
	PeerID    string `json:"peerId,omitempty"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	SDPMid    string `json:"sdpMid,omitempty"`
	SDPLine   *int   `json:"sdpMLineIndex,omitempty"`
	// Mac vouches for the SDP's DTLS fingerprints with the link secret.
	Mac string `json:"mac,omitempty"`
}

func (h *Hub) hostLoop(ctx context.Context, live *liveShare, joinURL string) {
	// However the loop ends (Stop, signaling server gone, network), the
	// session no longer has a share: drop the entry so the next Share makes
	// a fresh room instead of handing out a dead link.
	defer h.end(live)
	wsURL := wsBase(h.signal) + "/ws?role=host&room=" + url.QueryEscape(live.roomID) +
		"&key=" + url.QueryEscape(live.hostKey)
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
	msgs := make(chan sigMsg)
	go func() {
		defer close(msgs)
		for {
			var msg sigMsg
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
	signal := func(peerID string, out sigMsg) {
		out.Type = "signal"
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
		sess.Announce(runtime.Event{Kind: "join-resolved", Peer: peerID, Message: outcome})
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
				signal(peerID, sigMsg{Kind: "denied"})
			}
		case req := <-live.admit:
			p := resolve(req.peerID, map[bool]string{true: "allowed", false: "denied"}[req.allow])
			if p == nil {
				req.done <- ErrNoRequest
				continue
			}
			req.done <- nil
			if !req.allow {
				signal(req.peerID, sigMsg{Kind: "denied"})
				continue
			}
			peerID := req.peerID
			pc, err := h.acceptPeer(live, sess, p.name, func(out sigMsg) { signal(peerID, out) })
			if err != nil {
				log.Printf("share peer: %v", err)
				signal(peerID, sigMsg{Kind: "denied"})
				sess.Announce(runtime.Event{Kind: "join-failed", Peer: peerID, Message: p.name + " could not be connected: " + err.Error()})
				continue
			}
			peers[peerID] = pc
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			switch msg.Type {
			case "peer":
				peerID := msg.PeerID
				if peerID == "" || pending[peerID] != nil || peers[peerID] != nil {
					continue
				}
				if len(peers)+len(pending) >= maxPeers || len(pending) >= maxPending {
					log.Printf("share: too many viewers or requests, turning one away")
					signal(peerID, sigMsg{Kind: "denied"})
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
				signal(peerID, sigMsg{Kind: "pending"})
				sess.Announce(runtime.Event{Kind: "join-request", Peer: peerID, Message: msg.Name})
			case "leave":
				resolve(msg.PeerID, "left")
				if p := peers[msg.PeerID]; p != nil {
					p.close()
					delete(peers, msg.PeerID)
				}
			case "signal":
				p := peers[msg.PeerID]
				if p == nil {
					continue
				}
				if err := p.onSignal(msg, live.secret); err != nil {
					log.Printf("share peer %s: %v", msg.PeerID, err)
					p.close()
					delete(peers, msg.PeerID)
					signal(msg.PeerID, sigMsg{Kind: "denied"})
					why := err.Error()
					if errors.Is(err, errBadMAC) {
						why = "failed verification (wrong link, or the signal server tampered with the connection)"
					}
					sess.Announce(runtime.Event{Kind: "join-failed", Peer: msg.PeerID, Message: p.name + " was not connected: " + why})
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

func (p *peerConn) onSignal(msg sigMsg, secret string) error {
	switch msg.Kind {
	case "answer":
		if p.answered {
			return nil
		}
		if !checkSDPMAC(secret, roleJoin, msg.SDP, msg.Mac) {
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
	case "ice":
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

func (h *Hub) acceptPeer(live *liveShare, sess *runtime.Session, name string, send func(sigMsg)) (*peerConn, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
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
	var held []sigMsg
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		msg := sigMsg{Kind: "ice", Candidate: init.Candidate}
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
	send(sigMsg{Kind: "offer", SDP: offer.SDP, Mac: sdpMAC(live.secret, roleHost, offer.SDP)})
	offered = true
	for _, m := range held {
		send(m)
	}
	held = nil
	iceMu.Unlock()
	return p, nil
}

func attachDataChannel(sess *runtime.Session, dc *webrtc.DataChannel, name string, mark uint64) {
	dc.OnOpen(func() {
		cl, events, snap := sess.SubscribeSince(runtime.Join{Name: name, Kind: "webrtc"}, mark)
		if cl.Role == runtime.RoleDriver {
			info := sess.Info()
			_ = sess.Resize(cl.ID, info.Cols, info.Rows)
		}
		if len(snap) > 0 {
			_ = dc.Send(snap)
		}
		info := sess.Info()
		_ = dc.SendText(mustJSON(sessionPayload("ready", "attached", info, cl, 0, 0)))
		low := make(chan struct{}, 1)
		closed := make(chan struct{})
		dc.SetBufferedAmountLowThreshold(sendLow)
		dc.OnBufferedAmountLow(func() {
			select {
			case low <- struct{}{}:
			default:
			}
		})
		// drained waits until the data channel is under sendHigh. It reports
		// false when the channel closed or stalled.
		drained := func() bool {
			for dc.BufferedAmount() > sendHigh {
				t := time.NewTimer(sendStall)
				select {
				case <-low:
					t.Stop()
				case <-closed:
					t.Stop()
					return false
				case <-t.C:
					return false
				}
			}
			return true
		}
		go func() {
			for ev := range events {
				if ev.Kind == "bytes" {
					if !drained() {
						sess.Unsubscribe(cl.ID)
						_ = dc.Close()
						return
					}
					if err := dc.Send(ev.Bytes); err != nil {
						sess.Unsubscribe(cl.ID)
						return
					}
					continue
				}
				msg := sessionPayload(ev.Kind, ev.Message, ev.Info, cl, ev.FromID, ev.TargetID)
				if ev.Kind == "status" {
					msg["status"] = ev.Status
					msg["id"] = info.ID
				}
				if ev.Status == runtime.StatusError {
					_ = dc.SendText(mustJSON(map[string]any{"type": "error", "message": ev.Message}))
				}
				_ = dc.SendText(mustJSON(msg))
			}
		}()
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if m.IsString {
				var ctl struct {
					Type     string `json:"type"`
					Surface  string `json:"surface"`
					Command  string `json:"command"`
					Nonce    string `json:"nonce"`
					Approved bool   `json:"approved"`
					Cols     int    `json:"cols"`
					Rows     int    `json:"rows"`
					TargetID uint64 `json:"targetId"`
				}
				if json.Unmarshal(m.Data, &ctl) != nil {
					return
				}
				switch ctl.Type {
				case "resize":
					_ = sess.Resize(cl.ID, ctl.Cols, ctl.Rows)
				case "request-control":
					_ = sess.RequestControl(cl.ID)
				case "grant-control":
					_ = sess.GrantControl(cl.ID, ctl.TargetID)
				case "take-control":
					_ = sess.TakeControl(cl.ID)
				case "focus":
					_ = sess.SetFocus(cl.ID, inject.Surface(ctl.Surface))
				case "inject-challenge":
					nonce, err := sess.IssueInjectTicket(cl.ID, ctl.Command)
					if err == nil {
						_ = dc.SendText(mustJSON(map[string]any{"type": "inject-challenge", "nonce": nonce}))
					}
				case "inject":
					_ = sess.InjectTicket(cl.ID, ctl.Command, ctl.Nonce)
				}
				return
			}
			if err := sess.Write(cl.ID, m.Data); err != nil && !errors.Is(err, runtime.ErrReadOnly) {
				_ = dc.SendText(mustJSON(map[string]any{"type": "error", "message": err.Error()}))
			}
		})
		dc.OnClose(func() {
			close(closed)
			sess.Unsubscribe(cl.ID)
		})
	})
}

func sessionPayload(kind, message string, info runtime.Info, you runtime.Client, fromID, targetID uint64) map[string]any {
	you.Role = runtime.RoleFollower
	if you.ID == info.DriverID {
		you.Role = runtime.RoleDriver
	} else if you.ID == info.OwnerID {
		you.Role = runtime.RoleOwner
	}
	out := map[string]any{
		"type":     kind,
		"message":  message,
		"id":       info.ID,
		"title":    info.Title,
		"status":   info.Status,
		"attached": info.Attached,
		"driverId": info.DriverID,
		"ownerId":  info.OwnerID,
		"you":      you,
	}
	if fromID != 0 {
		out["fromId"] = fromID
	}
	if targetID != 0 {
		out["targetId"] = targetID
	}
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func wsBase(httpBase string) string {
	u, err := url.Parse(httpBase)
	if err != nil {
		return strings.Replace(httpBase, "http", "ws", 1)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	return strings.TrimRight(u.String(), "/")
}
