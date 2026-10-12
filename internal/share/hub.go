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
	cancel    context.CancelFunc
	ready     chan struct{} // closed when the share is up or has failed
	err       error         // set before ready is closed
}

const (
	// maxPeers caps WebRTC viewers per share (each is a PeerConnection).
	maxPeers = 16
	// maxBuffered is how far a viewer's data channel may fall behind before
	// it is dropped; pion queues sends without bound.
	maxBuffered = 4 << 20
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
	if _, err := h.rt.Get(sessionID); err != nil {
		return "", err
	}
	h.mu.Lock()
	if s := h.bySess[sessionID]; s != nil {
		h.mu.Unlock()
		return h.await(s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	live := &liveShare{sessionID: sessionID, cancel: cancel, ready: make(chan struct{})}
	h.bySess[sessionID] = live
	h.mu.Unlock()

	roomID, hostKey, err := createRoom(h.signal)
	if err != nil {
		h.fail(live, err)
		return "", err
	}
	live.roomID, live.hostKey = roomID, hostKey
	go h.hostLoop(ctx, live, h.signal+"/join?room="+url.QueryEscape(roomID))
	return h.await(live)
}

func (h *Hub) await(s *liveShare) (string, error) {
	select {
	case <-s.ready:
	case <-time.After(10 * time.Second):
		h.end(s)
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
}

func (h *Hub) hostLoop(ctx context.Context, live *liveShare, joinURL string) {
	// However the loop ends (Stop, signaling server gone, network), the
	// session no longer has a share: drop the entry so the next Share makes
	// a fresh room instead of handing out a dead link.
	defer h.end(live)
	wsURL := wsBase(h.signal) + "/ws?role=host&room=" + url.QueryEscape(live.roomID) +
		"&key=" + url.QueryEscape(live.hostKey)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
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

	peers := map[string]*peerConn{}
	var mu sync.Mutex
	defer func() {
		mu.Lock()
		for _, p := range peers {
			p.close()
		}
		mu.Unlock()
	}()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for {
		var msg sigMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "peer":
			mu.Lock()
			full := len(peers) >= maxPeers
			mu.Unlock()
			if full {
				log.Printf("share: %d viewers already, ignoring another", maxPeers)
				continue
			}
			peerID, name := msg.PeerID, msg.Name
			p, err := h.acceptPeer(live.sessionID, peerID, name, func(out sigMsg) {
				out.Type = "signal"
				out.PeerID = peerID
				_ = write(out)
			})
			if err != nil {
				log.Printf("share peer: %v", err)
				continue
			}
			mu.Lock()
			peers[msg.PeerID] = p
			mu.Unlock()
		case "leave":
			mu.Lock()
			if p := peers[msg.PeerID]; p != nil {
				p.close()
				delete(peers, msg.PeerID)
			}
			mu.Unlock()
		case "signal":
			mu.Lock()
			p := peers[msg.PeerID]
			mu.Unlock()
			if p != nil {
				p.onSignal(msg)
			}
		}
	}
}

type peerConn struct {
	pc     *webrtc.PeerConnection
	cancel context.CancelFunc
}

func (p *peerConn) close() {
	if p == nil {
		return
	}
	p.cancel()
	_ = p.pc.Close()
}

func (p *peerConn) onSignal(msg sigMsg) {
	switch msg.Kind {
	case "answer":
		_ = p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP})
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
		_ = p.pc.AddICECandidate(webrtc.ICECandidateInit{
			Candidate:     msg.Candidate,
			SDPMid:        mid,
			SDPMLineIndex: line,
		})
	}
}

func (h *Hub) acceptPeer(sessionID, peerID, name string, send func(sigMsg)) (*peerConn, error) {
	sess, err := h.rt.Get(sessionID)
	if err != nil {
		return nil, err
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &peerConn{pc: pc, cancel: cancel}

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
		send(msg)
	})

	dc, err := pc.CreateDataChannel("keel", nil)
	if err != nil {
		cancel()
		_ = pc.Close()
		return nil, err
	}
	attachDataChannel(sess, dc, name)
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
	send(sigMsg{Kind: "offer", SDP: offer.SDP})
	_ = peerID
	return p, nil
}

func attachDataChannel(sess *runtime.Session, dc *webrtc.DataChannel, name string) {
	dc.OnOpen(func() {
		cl, events, snap := sess.Subscribe(runtime.Join{Name: name, Kind: "webrtc"})
		if cl.Role == runtime.RoleDriver {
			info := sess.Info()
			_ = sess.Resize(cl.ID, info.Cols, info.Rows)
		}
		if len(snap) > 0 {
			_ = dc.Send(snap)
		}
		info := sess.Info()
		_ = dc.SendText(mustJSON(sessionPayload("ready", "attached", info, cl, 0, 0)))
		go func() {
			for ev := range events {
				if ev.Kind == "bytes" {
					if dc.BufferedAmount() > maxBuffered {
						// A viewer this far behind would grow the send queue
						// without bound; drop it like a slow websocket client.
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
		dc.OnClose(func() { sess.Unsubscribe(cl.ID) })
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
