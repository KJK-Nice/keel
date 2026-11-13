package joinpeer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"keel/internal/native/keelclient"
	keelsignal "keel/internal/signal"
)

// These tests run the real signal server (internal/signal) on a loopback port and a pion peer that stands in for keel
// serve. Every peer has an empty ICE server list and offers loopback candidates: nothing leaves the machine.

// fakeHost is the host's side: it owns the room, asks to be told of joiners, and answers them like internal/share.
type fakeHost struct {
	t      *testing.T
	srv    *httptest.Server
	room   string
	key    string // the real link key of the share
	ws     *websocket.Conn
	wmu    sync.Mutex
	pc     *webrtc.PeerConnection
	dc     *webrtc.DataChannel
	peer   chan string // joiner ids, as signal tells the host
	names  chan string
	got    chan webrtc.DataChannelMessage // what the joiner sent on the channel
	opened chan struct{}
	answer chan bool // an answer arrived: did its MAC verify?
	mu     sync.Mutex
	early  []webrtc.ICECandidateInit
	have   bool
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	srv := httptest.NewServer(keelsignal.New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var room struct{ ID, HostKey string }
	json.NewDecoder(res.Body).Decode(&room)
	res.Body.Close()
	h := &fakeHost{t: t, srv: srv, room: room.ID, key: key, peer: make(chan string, 4), names: make(chan string, 4),
		got: make(chan webrtc.DataChannelMessage, 32), opened: make(chan struct{}, 1), answer: make(chan bool, 4)}
	u, _ := url.Parse(srv.URL)
	u.Scheme, u.Path = "ws", "/ws"
	u.RawQuery = url.Values{"role": {"host"}, "room": {room.ID}, "key": {room.HostKey}}.Encode()
	h.ws, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.ws.Close()
		if h.pc != nil {
			h.pc.Close()
		}
	})
	go h.loop()
	return h
}

// link is the join link a joiner is given.
func (h *fakeHost) link(withKey string) Link {
	return Link{Origin: h.srv.URL, Room: h.room, Key: withKey}
}

func (h *fakeHost) send(m map[string]any) {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	m["type"] = "signal"
	h.ws.WriteJSON(m)
}

func (h *fakeHost) loop() {
	for {
		var m struct {
			Type, PeerID, Name, Kind, SDP, Mac, Candidate, SDPMid string
			SDPLine                                               *uint16 `json:"sdpMLineIndex"`
		}
		if err := h.ws.ReadJSON(&m); err != nil {
			return
		}
		switch m.Type {
		case "peer":
			h.peer <- m.PeerID
			h.names <- m.Name
		case "signal":
			switch m.Kind {
			case "answer":
				ok := CheckMAC(h.key, "join", m.SDP, m.Mac)
				h.answer <- ok
				if ok && h.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: m.SDP}) == nil {
					h.mu.Lock()
					h.have = true
					for _, c := range h.early {
						h.pc.AddICECandidate(c)
					}
					h.early = nil
					h.mu.Unlock()
				}
			case "ice":
				c := webrtc.ICECandidateInit{Candidate: m.Candidate, SDPMid: &m.SDPMid, SDPMLineIndex: m.SDPLine}
				h.mu.Lock()
				if h.have {
					h.pc.AddICECandidate(c)
				} else {
					h.early = append(h.early, c)
				}
				h.mu.Unlock()
			}
		}
	}
}

func (h *fakeHost) waitPeer() string {
	h.t.Helper()
	select {
	case id := <-h.peer:
		return id
	case <-time.After(5 * time.Second):
		h.t.Fatal("the host was not told of the joiner")
	}
	return ""
}

func (h *fakeHost) pending(id string) { h.send(map[string]any{"peerId": id, "kind": "pending"}) }
func (h *fakeHost) deny(id string)    { h.send(map[string]any{"peerId": id, "kind": "denied"}) }

// admit makes the offer with the real key's MAC and a data channel, as the share hub does after Allow.
func (h *fakeHost) admit(id string) {
	h.t.Helper()
	se := webrtc.SettingEngine{}
	se.SetIncludeLoopbackCandidate(true)
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		h.t.Fatal(err)
	}
	h.pc = pc
	var imu sync.Mutex
	offered := false
	var held []map[string]any
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		m := map[string]any{"peerId": id, "kind": "ice", "candidate": init.Candidate}
		if init.SDPMid != nil {
			m["sdpMid"] = *init.SDPMid
		}
		if init.SDPMLineIndex != nil {
			m["sdpMLineIndex"] = *init.SDPMLineIndex
		}
		imu.Lock()
		defer imu.Unlock()
		if !offered {
			held = append(held, m)
			return
		}
		h.send(m)
	})
	dc, err := pc.CreateDataChannel("keel", nil)
	if err != nil {
		h.t.Fatal(err)
	}
	h.dc = dc
	dc.OnOpen(func() { h.opened <- struct{}{} })
	dc.OnMessage(func(m webrtc.DataChannelMessage) { h.got <- m })
	offer, _ := pc.CreateOffer(nil)
	if err := pc.SetLocalDescription(offer); err != nil {
		h.t.Fatal(err)
	}
	imu.Lock()
	h.send(map[string]any{"peerId": id, "kind": "offer", "sdp": offer.SDP, "mac": MAC(h.key, "host", offer.SDP)})
	offered = true
	for _, m := range held {
		h.send(m)
	}
	held = nil
	imu.Unlock()
}

func (h *fakeHost) waitOpen() {
	h.t.Helper()
	select {
	case <-h.opened:
	case <-time.After(10 * time.Second):
		h.t.Fatal("the data channel did not open")
	}
}

// frame sends a control frame the way the hub does.
func (h *fakeHost) frame(v map[string]any) {
	h.t.Helper()
	b, _ := json.Marshal(v)
	if err := h.dc.SendText(string(b)); err != nil {
		h.t.Fatal(err)
	}
}

func presence(typ, status string, you uint64, driver, owner uint64) map[string]any {
	role := "follower"
	switch you {
	case driver:
		role = "driver"
	case owner:
		role = "owner"
	}
	return map[string]any{"type": typ, "status": status, "title": "Local Shell", "driverId": driver, "ownerId": owner,
		"attached": []map[string]any{{"id": 1, "name": "Ann", "kind": "web", "role": "driver"}, {"id": 2, "name": "me", "kind": "webrtc", "role": "follower"}},
		"you":      map[string]any{"id": you, "name": "me", "kind": "webrtc", "role": role}}
}

func open(t *testing.T, h *fakeHost, withKey string) *Peer {
	t.Helper()
	p, err := Open(h.link(withKey), "me", WithICEServers(nil), WithLoopback())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func waitState(t *testing.T, p *Peer, what string, cond func(keelclient.State) bool) keelclient.State {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if st := p.Snapshot(); cond(st) {
			return st
		}
	}
	t.Fatalf("timed out waiting for %s; state %+v", what, p.Snapshot())
	return keelclient.State{}
}

// readAll reads what the peer has until it is closed, with a limit of time.
func readAll(p *Peer, d time.Duration) string {
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		b := make([]byte, 1024)
		for {
			n, err := p.Read(b)
			buf.Write(b[:n])
			if err != nil {
				close(done)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
	return buf.String()
}

// noLeak fails if any text holds the link's key.
func noLeak(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, key) {
			t.Errorf("%s shows the link's key: %q", what, s)
		}
	}
}

func TestJoinHappyPathFollowsThenDrivesAfterAHandOver(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(io.Discard)

	h := newFakeHost(t)
	p := open(t, h, key)
	if len(p.ICEServers()) != 0 {
		t.Fatalf("ICE servers %v: a test peer has none", p.ICEServers())
	}
	if u, _ := url.Parse(p.SignalURL()); u.Hostname() != "127.0.0.1" || u.Query().Get("room") != h.room || u.Query().Get("name") != "me" || strings.Contains(p.SignalURL(), key) {
		t.Fatalf("signal URL %q", p.SignalURL())
	}
	id := h.waitPeer()
	if name := <-h.names; name != "me" {
		t.Fatalf("the host was told the name %q", name)
	}
	h.pending(id)
	if st := p.Snapshot(); st.Status != "" || st.Role != "" {
		t.Fatalf("waiting: %+v", st)
	}
	h.admit(id)
	h.waitOpen()
	waitState(t, p, "ready", func(s keelclient.State) bool { return s.Status == "ready" })
	if v := <-h.answer; !v {
		t.Fatal("the host could not verify the joiner's answer")
	}

	// The snapshot the hub sends first, then the ready frame: follower of Ann's session.
	h.dc.Send([]byte("hello-2\r\n"))
	h.frame(presence("ready", "ready", 2, 1, 1))
	st := waitState(t, p, "follower", func(s keelclient.State) bool { return s.Role == "follower" && s.Title == "Local Shell" })
	if st.DriverID != 1 || st.OwnerID != 1 || st.YouID != 2 || len(st.Attached) != 2 {
		t.Fatalf("state %+v", st)
	}
	buf := make([]byte, 64)
	n, err := p.Read(buf)
	if err != nil || string(buf[:n]) != "hello-2\r\n" {
		t.Fatalf("read %q %v", buf[:n], err)
	}

	// Typing while following sends nothing, and is counted.
	if n, err := p.Write([]byte("echo from-b\r")); err != nil || n != 12 {
		t.Fatalf("write %d %v", n, err)
	}
	select {
	case m := <-h.got:
		t.Fatalf("a follower's keystrokes reached the host: %q", m.Data)
	case <-time.After(300 * time.Millisecond):
	}
	if p.Snapshot().Blocked != 1 {
		t.Fatalf("blocked %d", p.Snapshot().Blocked)
	}

	// Asking for control sends the request; a hand-over then lets the keystrokes through.
	p.RequestControl()
	if m := <-h.got; !m.IsString || string(m.Data) != `{"type":"request-control"}` {
		t.Fatalf("got %q", m.Data)
	}
	p.Resize(100, 30)
	if m := <-h.got; string(m.Data) != `{"cols":100,"rows":30,"type":"resize"}` {
		t.Fatalf("got %q", m.Data)
	}
	h.frame(presence("control", "ready", 2, 2, 1))
	waitState(t, p, "driver", func(s keelclient.State) bool { return s.Role == "driver" })
	p.Write([]byte("x"))
	if m := <-h.got; m.IsString || string(m.Data) != "x" {
		t.Fatalf("got %v %q", m.IsString, m.Data)
	}
	p.GrantControl(1)
	if m := <-h.got; string(m.Data) != `{"targetId":1,"type":"grant-control"}` {
		t.Fatalf("got %q", m.Data)
	}
	p.TakeControl()
	if m := <-h.got; string(m.Data) != `{"type":"take-control"}` {
		t.Fatalf("got %q", m.Data)
	}

	if p.SessionID() != "" {
		t.Error("a joined peer has no local session id")
	}
	noLeak(t, "the state", fmt.Sprintf("%+v %v", p.Snapshot(), p.Events()))
	noLeak(t, "the peer", fmt.Sprintf("%+v %#v", p.SignalURL(), p.ICEServers()))
	noLeak(t, "the log", logs.String())
}

func TestAWrongKeyFailsVerificationAndShowsNoOutput(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(io.Discard)

	h := newFakeHost(t)
	wrong := key[:31] + "f" // one character off
	if wrong == key {
		wrong = key[:31] + "e"
	}
	p := open(t, h, wrong)
	id := h.waitPeer()
	h.pending(id)
	h.admit(id)
	st := waitState(t, p, "an error", func(s keelclient.State) bool { return s.Status == "error" })
	if st.Message != ErrUnverified.Error() {
		t.Fatalf("message %q", st.Message)
	}
	// The host never sees an answer, the channel never opens, and no host byte is ever read.
	select {
	case <-h.answer:
		t.Fatal("the joiner answered an offer it could not verify")
	case <-h.opened:
		t.Fatal("the data channel opened")
	case <-time.After(500 * time.Millisecond):
	}
	if got := readAll(p, time.Second); got != "" {
		t.Fatalf("terminal output after a failed verification: %q", got)
	}
	evs := p.Events()
	if len(evs) != 1 || evs[0].Kind != keelclient.EventNoticeError || evs[0].Message != ErrUnverified.Error() {
		t.Fatalf("events %+v", evs)
	}
	noLeak(t, "the state", fmt.Sprintf("%+v %v", st, evs))
	noLeak(t, "the log", logs.String())
	if strings.Contains(fmt.Sprint(st, evs, p.SignalURL()), wrong) {
		t.Error("the wrong key is shown")
	}
	if n, _ := p.Write([]byte("x")); n != 1 || len(h.got) != 0 {
		t.Error("a failed peer sends nothing")
	}
}

func TestDenialEndsTheJoinWithTheAppsMessageAndNoOutput(t *testing.T) {
	h := newFakeHost(t)
	p := open(t, h, key)
	id := h.waitPeer()
	h.pending(id)
	h.deny(id)
	st := waitState(t, p, "denied", func(s keelclient.State) bool { return s.Status == "error" })
	if st.Message != "The host declined your request to join." {
		t.Fatalf("message %q", st.Message)
	}
	if got := readAll(p, time.Second); got != "" {
		t.Fatalf("output %q", got)
	}
	if evs := p.Events(); len(evs) != 1 || evs[0].Message != st.Message {
		t.Fatalf("events %+v", evs)
	}
}

func TestWhenTheOwnerStopsSharingTheJoinerIsToldAndTheTerminalSaysHostDetached(t *testing.T) {
	h := newFakeHost(t)
	p := open(t, h, key)
	id := h.waitPeer()
	h.pending(id)
	h.admit(id)
	h.waitOpen()
	h.frame(presence("ready", "ready", 2, 1, 1))
	waitState(t, p, "follower", func(s keelclient.State) bool { return s.Role == "follower" })
	// Stop sharing: the hub closes the room's host socket and every peer.
	h.ws.Close()
	h.pc.Close()
	waitState(t, p, "host gone", func(s keelclient.State) bool { return s.Status != "ready" })
	var evs []keelclient.Event
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && len(evs) == 0; time.Sleep(10 * time.Millisecond) {
		evs = append(evs, p.Events()...)
	}
	if len(evs) == 0 || evs[0].Kind != keelclient.EventNotice || evs[0].Message != "Host left" {
		t.Fatalf("events %+v", evs)
	}
	p.Close()
	if got := readAll(p, time.Second); strings.Contains(got, "hello") {
		t.Fatalf("output %q", got)
	}
}

func TestOpenFailsClearlyWhenTheSignalServerIsGone(t *testing.T) {
	h := newFakeHost(t)
	l := h.link(key)
	h.srv.Close()
	_, err := Open(l, "me", WithICEServers(nil))
	if err == nil || !strings.HasPrefix(err.Error(), "signal server: ") || strings.Contains(err.Error(), key) {
		t.Fatalf("err %v", err)
	}
	// A room that does not exist is refused at the handshake.
	h2 := newFakeHost(t)
	bad := h2.link(key)
	bad.Room = "nope"
	if _, err := Open(bad, "me", WithICEServers(nil)); err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("err %v", err)
	}
}

func TestByDefaultThePeerUsesTheAppsSTUNServerAndNothingElse(t *testing.T) {
	if len(STUN) != 1 || len(STUN[0].URLs) != 1 || STUN[0].URLs[0] != "stun:stun.l.google.com:19302" {
		t.Fatalf("STUN %v", STUN)
	}
	h := newFakeHost(t)
	// No offer is ever made here, so nothing is gathered and nothing is sent to the STUN server.
	p, err := Open(h.link(key), "me")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.ICEServers()) != 1 || p.ICEServers()[0].URLs[0] != STUN[0].URLs[0] {
		t.Fatalf("ICE servers %v", p.ICEServers())
	}
}
