package share

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/signal"
	"keel/internal/store"
	"keel/internal/wire"
)

// shareRig is a session with a fake PTY, a local client watching for join
// requests (as the desktop UI does), and a hub on a real signal server.
type shareRig struct {
	t        *testing.T
	fb       *fakeBridge
	sess     *runtime.Session
	id       string
	hub      *Hub
	sig      *httptest.Server
	requests chan runtime.Event // join-request and join-resolved
}

func newShareRig(t *testing.T) *shareRig {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	fb := newFakeBridge()
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return fb, nil })
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	info, err := rt.Spawn(hosts[0].ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sess.Info().Status != runtime.StatusReady && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	r := &shareRig{t: t, fb: fb, sess: sess, id: info.ID, requests: make(chan runtime.Event, 16)}
	_, events, _ := sess.Subscribe(runtime.Join{Name: "host", Kind: "web"})
	go func() {
		for ev := range events {
			if strings.HasPrefix(ev.Kind, "join-") {
				r.requests <- ev
			}
		}
	}()
	r.sig = httptest.NewServer(signal.New().Handler())
	t.Cleanup(r.sig.Close)
	r.hub = New(r.sig.URL, rt)
	t.Cleanup(func() { r.hub.Stop(r.id) })
	return r
}

// output writes to the fake PTY and waits until the session has it.
func (r *shareRig) output(s string) {
	r.t.Helper()
	if _, err := r.fb.outW.Write([]byte(s)); err != nil {
		r.t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		probe, _, snap := r.sess.Subscribe(runtime.Join{Name: "probe", Kind: "web"})
		r.sess.Unsubscribe(probe.ID)
		if strings.Contains(string(snap), s) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("session never saw %q", s)
}

func (r *shareRig) event(kind string) runtime.Event {
	r.t.Helper()
	for {
		select {
		case ev := <-r.requests:
			if ev.Kind == kind {
				return ev
			}
		case <-time.After(8 * time.Second):
			r.t.Fatalf("no %s event", kind)
		}
	}
}

// joiner is a Go stand-in for the desktop/browser joiner.
type joiner struct {
	t      *testing.T
	key    string
	conn   *websocket.Conn
	pc     *webrtc.PeerConnection
	wmu    sync.Mutex
	msgs   chan wire.Signal
	opened chan struct{}
	got    chan string
	// dc is the host's data channel once it is open; texts are the text
	// frames (the session socket's JSON) it carried.
	dc    *webrtc.DataChannel
	texts chan string

	// Candidates from the host can arrive before the offer is applied.
	iceMu  sync.Mutex
	remote bool
	early  []webrtc.ICECandidateInit
}

func (j *joiner) addICE(c webrtc.ICECandidateInit) {
	j.iceMu.Lock()
	defer j.iceMu.Unlock()
	if !j.remote {
		j.early = append(j.early, c)
		return
	}
	_ = j.pc.AddICECandidate(c)
}

func (r *shareRig) join(link string) *joiner {
	r.t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		r.t.Fatal(err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	j := &joiner{
		t:      r.t,
		key:    frag.Get("k"),
		msgs:   make(chan wire.Signal, 16),
		opened: make(chan struct{}),
		got:    make(chan string, 64),
		texts:  make(chan string, 64),
	}
	if len(j.key) != 32 {
		r.t.Fatalf("join link %q has no #k secret", link)
	}
	if strings.Contains(u.RawQuery, j.key) {
		r.t.Fatal("the secret must only be in the #fragment, never sent to the server")
	}
	wsURL := "ws" + strings.TrimPrefix(r.sig.URL, "http") + "/ws?role=join&room=" + url.QueryEscape(u.Query().Get("room")) + "&name=bob"
	j.conn, _, err = websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { j.conn.Close() })
	j.pc, err = webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { j.pc.Close() })
	var once sync.Once
	j.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() { once.Do(func() { j.dc = dc; close(j.opened) }) })
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if m.IsString {
				select {
				case j.texts <- string(m.Data):
				default:
				}
				return
			}
			select {
			case j.got <- string(m.Data):
			default:
			}
		})
	})
	j.pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		msg := wire.Signal{Type: "signal", Kind: "ice", Candidate: init.Candidate}
		if init.SDPMid != nil {
			msg.SDPMid = *init.SDPMid
		}
		_ = j.write(msg)
	})
	go func() {
		defer close(j.msgs)
		for {
			var msg wire.Signal
			if err := j.conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type != "signal" {
				continue
			}
			if msg.Kind == "ice" && msg.Candidate != "" {
				var mid *string
				if msg.SDPMid != "" {
					m := msg.SDPMid
					mid = &m
				}
				j.addICE(webrtc.ICECandidateInit{Candidate: msg.Candidate, SDPMid: mid})
				continue
			}
			j.msgs <- msg
		}
	}()
	return j
}

func (j *joiner) write(v any) error {
	j.wmu.Lock()
	defer j.wmu.Unlock()
	return j.conn.WriteJSON(v)
}

func (j *joiner) next(kind string) wire.Signal {
	j.t.Helper()
	select {
	case msg, ok := <-j.msgs:
		if !ok {
			j.t.Fatalf("signaling closed waiting for %s", kind)
		}
		if msg.Kind != kind {
			j.t.Fatalf("got %q, want %q", msg.Kind, kind)
		}
		return msg
	case <-time.After(8 * time.Second):
		j.t.Fatalf("no %s", kind)
	}
	return wire.Signal{}
}

// answer verifies the offer the way the JS joiner does and answers it,
// with the joiner MAC computed by mac.
func (j *joiner) answer(offer wire.Signal, mac func(sdp string) string) {
	j.t.Helper()
	if !checkSDPMAC(j.key, roleHost, offer.SDP, offer.Mac) {
		j.t.Fatal("offer MAC does not verify against the link secret")
	}
	if err := j.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer.SDP}); err != nil {
		j.t.Fatal(err)
	}
	j.iceMu.Lock()
	j.remote = true
	for _, c := range j.early {
		_ = j.pc.AddICECandidate(c)
	}
	j.early = nil
	j.iceMu.Unlock()
	ans, err := j.pc.CreateAnswer(nil)
	if err != nil {
		j.t.Fatal(err)
	}
	if err := j.pc.SetLocalDescription(ans); err != nil {
		j.t.Fatal(err)
	}
	if err := j.write(wire.Signal{Type: "signal", Kind: "answer", SDP: ans.SDP, Mac: mac(ans.SDP)}); err != nil {
		j.t.Fatal(err)
	}
}

func TestShareAdmitVerifyAndNoHistory(t *testing.T) {
	r := newShareRig(t)
	r.output("before-share-secret\r\n")
	link, err := r.hub.Share(r.id)
	if err != nil {
		t.Fatal(err)
	}
	j := r.join(link)
	j.next("pending")
	req := r.event("join-request")
	if req.Message != "bob" || req.Peer == "" {
		t.Fatalf("join-request %+v: want bob with a peer id", req)
	}
	select {
	case <-j.msgs:
		t.Fatal("joiner got an offer before the host allowed it")
	case <-time.After(200 * time.Millisecond):
	}
	if err := r.hub.Admit(r.id, req.Peer, true); err != nil {
		t.Fatal(err)
	}
	if ev := r.event("join-resolved"); ev.Peer != req.Peer || ev.Message != "allowed" {
		t.Fatalf("join-resolved %+v", ev)
	}
	j.answer(j.next("offer"), func(sdp string) string { return sdpMAC(j.key, roleJoin, sdp) })
	select {
	case <-j.opened:
	case <-time.After(8 * time.Second):
		t.Fatal("datachannel did not open")
	}
	r.output("hello-webrtc")
	var seen strings.Builder
	deadline := time.After(8 * time.Second)
	for !strings.Contains(seen.String(), "hello-webrtc") {
		select {
		case s := <-j.got:
			seen.WriteString(s)
		case <-deadline:
			t.Fatalf("viewer got %q, never hello-webrtc", seen.String())
		}
	}
	if strings.Contains(seen.String(), "before-share-secret") {
		t.Fatal("viewer was sent output from before sharing began")
	}
}

func TestShareDeny(t *testing.T) {
	r := newShareRig(t)
	link, err := r.hub.Share(r.id)
	if err != nil {
		t.Fatal(err)
	}
	j := r.join(link)
	j.next("pending")
	req := r.event("join-request")
	if err := r.hub.Admit(r.id, req.Peer, false); err != nil {
		t.Fatal(err)
	}
	j.next("denied")
	if ev := r.event("join-resolved"); ev.Message != "denied" {
		t.Fatalf("join-resolved %+v", ev)
	}
	if err := r.hub.Admit(r.id, req.Peer, true); err == nil {
		t.Fatal("a request can only be answered once")
	}
}

func TestShareRequestExpires(t *testing.T) {
	old := admitTimeout
	admitTimeout = 100 * time.Millisecond
	t.Cleanup(func() { admitTimeout = old })
	r := newShareRig(t)
	link, err := r.hub.Share(r.id)
	if err != nil {
		t.Fatal(err)
	}
	j := r.join(link)
	j.next("pending")
	r.event("join-request")
	j.next("denied")
	if ev := r.event("join-resolved"); ev.Message != "expired" {
		t.Fatalf("join-resolved %+v", ev)
	}
}

// A signal server in the middle can relay an answer but can't vouch for
// its fingerprints without the link secret.
func TestShareRejectsUnverifiedAnswer(t *testing.T) {
	r := newShareRig(t)
	link, err := r.hub.Share(r.id)
	if err != nil {
		t.Fatal(err)
	}
	j := r.join(link)
	j.next("pending")
	if err := r.hub.Admit(r.id, r.event("join-request").Peer, true); err != nil {
		t.Fatal(err)
	}
	wrong := "ffeeddccbbaa99887766554433221100"
	j.answer(j.next("offer"), func(sdp string) string { return sdpMAC(wrong, roleJoin, sdp) })
	j.next("denied")
	if ev := r.event("join-failed"); !strings.Contains(ev.Message, "failed verification") {
		t.Fatalf("join-failed %+v", ev)
	}
	select {
	case <-j.opened:
		t.Fatal("datachannel opened for an answer that failed verification")
	case <-time.After(time.Second):
	}
}

// Over a real share, a joined peer cannot make keel serve type for it or
// ask the agent, even while it drives: every inject and agent frame it
// sends over the data channel is refused, it is given no nonce, it is told
// nothing of the host, and nothing reaches the PTY but its own keystrokes.
func TestAJoinedPeerCannotInjectOverTheDataChannel(t *testing.T) {
	r := newShareRig(t)
	link, err := r.hub.Share(r.id)
	if err != nil {
		t.Fatal(err)
	}
	j := r.join(link)
	j.next("pending")
	if err := r.hub.Admit(r.id, r.event("join-request").Peer, true); err != nil {
		t.Fatal(err)
	}
	j.answer(j.next("offer"), func(sdp string) string { return sdpMAC(j.key, roleJoin, sdp) })
	select {
	case <-j.opened:
	case <-time.After(8 * time.Second):
		t.Fatal("datachannel did not open")
	}
	// Everything written to the PTY.
	var mu sync.Mutex
	var pty []byte
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := r.fb.inR.Read(buf)
			mu.Lock()
			pty = append(pty, buf[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	written := func() string {
		mu.Lock()
		defer mu.Unlock()
		return string(pty)
	}
	// nextText is the next frame the peer is sent that is not the session's
	// own news.
	nextText := func() map[string]any {
		t.Helper()
		for {
			select {
			case s := <-j.texts:
				if strings.Contains(s, "trustToken") || strings.Contains(s, "hostId") {
					t.Fatalf("a joined peer was sent %s", s)
				}
				var m map[string]any
				if err := json.Unmarshal([]byte(s), &m); err != nil {
					t.Fatal(err)
				}
				switch m["type"] {
				case wire.TypeReady, wire.TypePresence, wire.TypeControl, wire.TypeStatus:
					continue
				}
				return m
			case <-time.After(8 * time.Second):
				t.Fatal("no answer over the data channel")
			}
		}
	}

	// The owner hands the peer control: it is the driver now.
	var peerID uint64
	deadline := time.Now().Add(8 * time.Second)
	for peerID == 0 && time.Now().Before(deadline) {
		for _, c := range r.sess.Info().Attached {
			if c.Kind == wire.KindJoined {
				peerID = c.ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if peerID == 0 {
		t.Fatal("the joined peer never attached")
	}
	if err := r.sess.GrantControl(r.sess.Info().OwnerID, peerID); err != nil {
		t.Fatal(err)
	}
	if !r.sess.DrivenByJoined() {
		t.Fatal("the peer does not drive")
	}

	const cmd = "touch /tmp/keel-joined-inject\n"
	frames := []wire.Control{
		{Type: wire.TypeInjectChallenge, Command: cmd},
		{Type: wire.TypeInject, Command: cmd},
		{Type: wire.TypeInject, Command: cmd, Nonce: "0123456789abcdef0123456789abcdef"},
		{Type: wire.TypeInject, Command: cmd, Nonce: "x", Notebook: true},
		{Type: wire.TypeInject, Command: cmd, Nonce: "x", Direct: &wire.DirectType{Ref: "d1", Kind: wire.DirectSnippet, SnippetID: "s1"}},
		{Type: wire.TypeInject, Command: cmd, Nonce: "x", Agent: &wire.AgentType{ConversationID: "c1", ProposalID: "p1"}},
		{Type: wire.TypeInject, Command: cmd, Nonce: "x", Suggestion: &wire.AgentInsert{SuggestionID: "s1"}},
		{Type: wire.TypeAgentDetect, Seq: 1, Line: "show files"},
		{Type: wire.TypeAgentSuggest, Seq: 1, Line: "show files"},
	}
	for _, f := range frames {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := j.dc.SendText(string(b)); err != nil {
			t.Fatal(err)
		}
		if got := nextText(); got["type"] != wire.TypeError || got["message"] != runtime.ErrNotLocal.Error() || got["nonce"] != nil {
			t.Fatalf("%s → %v, want a refusal", b, got)
		}
	}
	// Its own keystrokes are what a joined driver can send.
	if err := j.dc.Send([]byte("peer-keys")); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(8 * time.Second); !strings.Contains(written(), "peer-keys"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the joined driver's keys never reached the PTY: %q", written())
		}
	}
	if got := written(); got != "peer-keys" {
		t.Fatalf("the PTY got %q, want only the joined driver's own keys", got)
	}
}

func TestAdmitWithoutShare(t *testing.T) {
	r := newShareRig(t)
	if err := r.hub.Admit(r.id, "nobody", true); !errors.Is(err, ErrNotShared) {
		t.Fatalf("Admit on an unshared session = %v, want ErrNotShared", err)
	}
}
