package keelclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const offerToken = "one-time-offer-token"

// script serves /ws/session sending the frames pushed on out, records what the client sends, and
// serves the POST endpoints the window uses, recording method, path and body.
type script struct {
	c    *Client
	out  chan string
	got  chan frame
	mu   sync.Mutex
	reqs []string
	fail bool // POST endpoints answer 409 with the token in the body
}

func newScript(t *testing.T) *script {
	t.Helper()
	s := &script{out: make(chan string, 16), got: make(chan frame, 16)}
	mux := http.NewServeMux()
	post := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Method+" "+r.URL.Path+" "+string(b))
		fail := s.fail
		s.mu.Unlock()
		if fail {
			http.Error(w, "refused "+offerToken, http.StatusConflict)
			return
		}
		io.WriteString(w, `{}`)
	}
	mux.HandleFunc("POST /api/sessions/{id}/reconnect", post)
	mux.HandleFunc("POST /api/known-hosts", post)
	mux.HandleFunc("/ws/session", func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		go func() {
			for {
				typ, d, err := ws.ReadMessage()
				if err != nil {
					return
				}
				s.got <- frame{typ, d}
			}
		}()
		for m := range s.out {
			ws.WriteMessage(websocket.TextMessage, []byte(m))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.c = NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	return s
}

func TestSnapshotRolePeersAndDriver(t *testing.T) {
	s := newScript(t)
	c, err := s.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	changed := make(chan struct{}, 8)
	c.OnChange(func() { changed <- struct{}{} })
	go io.Copy(io.Discard, c)
	ready := `{"type":"ready","id":"s1","title":"api","status":"ready","driverId":1,"ownerId":1,
	  "attached":[{"id":1,"name":"a","kind":"web","role":"driver"},{"id":2,"name":"b","kind":"web","role":"follower"}],
	  "you":{"id":2,"name":"b","kind":"web","role":"follower"}}`
	s.out <- ready
	waitFor(t, func() bool { return c.Snapshot().Role == "follower" })
	st := c.Snapshot()
	if len(st.Attached) != 2 || st.Attached[0].Name != "a" || st.Attached[1].Role != "follower" || st.DriverID != 1 || st.OwnerID != 1 || st.YouID != 2 || !st.WasReady {
		t.Fatalf("snapshot %+v", st)
	}
	// A presence frame that makes this client the driver.
	s.out <- `{"type":"presence","driverId":2,"attached":[{"id":2,"name":"b","kind":"web","role":"driver"}],"you":{"id":2,"name":"b","kind":"web","role":"driver"}}`
	waitFor(t, func() bool { return c.Snapshot().Role == "driver" })
	if st := c.Snapshot(); st.DriverID != 2 || len(st.Attached) != 1 {
		t.Fatalf("snapshot %+v", st)
	}
	select {
	case <-changed:
	default:
		t.Fatal("OnChange did not fire")
	}
}

func TestSnapshotHostKeySetThenCleared(t *testing.T) {
	s := newScript(t)
	c, err := s.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go io.Copy(io.Discard, c)
	s.out <- `{"type":"error","message":"unknown host key","errorKind":"unknown-host","hostname":"h:22","fingerprint":"SHA256:abc","keyType":"ssh-ed25519","trustToken":"` + offerToken + `"}`
	waitFor(t, func() bool { return c.Snapshot().HostKey.Kind != "" })
	st := c.Snapshot()
	k := st.HostKey
	if k.Kind != "unknown-host" || k.Hostname != "h:22" || k.Fingerprint != "SHA256:abc" || k.KeyType != "ssh-ed25519" || k.Token != offerToken || st.Message != "unknown host key" {
		t.Fatalf("snapshot %+v", st)
	}
	if out := fmt.Sprintf("%v %+v %v %+v", k, k, st, st); strings.Contains(out, offerToken) {
		t.Fatalf("a printed HostKey shows its token: %s", out)
	}
	s.out <- `{"type":"status","status":"connecting","message":"reconnecting"}`
	waitFor(t, func() bool { return c.Snapshot().HostKey == (HostKey{}) })
	if c.Snapshot().Message != "unknown host key" {
		t.Errorf("the failure message was lost on a connecting frame: %+v", c.Snapshot())
	}
	s.out <- `{"type":"ready","id":"s1","status":"ready"}`
	waitFor(t, func() bool { return c.Snapshot().Message == "" })
}

func TestTakeControlSendsTheFrame(t *testing.T) {
	s := newScript(t)
	c, err := s.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.TakeControl(); err != nil {
		t.Fatal(err)
	}
	fr := <-s.got
	var m struct{ Type string }
	if fr.typ != websocket.TextMessage || json.Unmarshal(fr.data, &m) != nil || m.Type != "take-control" {
		t.Fatalf("%v %q", fr.typ, fr.data)
	}
}

func TestRequestAndGrantControlSendTheirFrames(t *testing.T) {
	s := newScript(t)
	c, err := s.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.RequestControl(); err != nil {
		t.Fatal(err)
	}
	if err := c.GrantControl(7); err != nil {
		t.Fatal(err)
	}
	var m struct {
		Type     string
		TargetID uint64
	}
	fr := <-s.got
	if fr.typ != websocket.TextMessage || json.Unmarshal(fr.data, &m) != nil || m.Type != "request-control" {
		t.Fatalf("%v %q", fr.typ, fr.data)
	}
	fr = <-s.got
	m = struct {
		Type     string
		TargetID uint64
	}{}
	if fr.typ != websocket.TextMessage || json.Unmarshal(fr.data, &m) != nil || m.Type != "grant-control" || m.TargetID != 7 {
		t.Fatalf("%v %q", fr.typ, fr.data)
	}
}

func TestReconnectAndTrustHostKeySendTheOffer(t *testing.T) {
	s := newScript(t)
	ctx := context.Background()
	if err := s.c.Reconnect(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	k := HostKey{Kind: "host-key-mismatch", Hostname: "h:22", Fingerprint: "SHA256:abc", Token: offerToken}
	if err := s.c.TrustHostKey(ctx, k, true); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	reqs := s.reqs
	s.mu.Unlock()
	if len(reqs) != 2 || reqs[0] != "POST /api/sessions/s1/reconnect " {
		t.Fatalf("requests %q", reqs)
	}
	parts := strings.SplitN(reqs[1], " ", 3)
	var m map[string]any
	if err := json.Unmarshal([]byte(parts[2]), &m); err != nil || parts[0] != "POST" || parts[1] != "/api/known-hosts" ||
		m["trustToken"] != offerToken || m["hostname"] != "h:22" || m["fingerprint"] != "SHA256:abc" || m["replace"] != true || len(m) != 4 {
		t.Fatalf("trust request %q", reqs[1])
	}
}

func TestFailedPostErrorsNameTheStatusNotTheBody(t *testing.T) {
	s := newScript(t)
	s.fail = true
	k := HostKey{Hostname: "h:22", Fingerprint: "SHA256:abc", Token: offerToken}
	for _, err := range []error{s.c.TrustHostKey(context.Background(), k, false), s.c.Reconnect(context.Background(), "s1")} {
		if err == nil || !strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), offerToken) {
			t.Fatalf("err %v", err)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("timed out")
}
