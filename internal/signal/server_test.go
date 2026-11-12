package signal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRoomRelay(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rooms %d", res.StatusCode)
	}
	var body struct {
		ID      string `json:"id"`
		HostKey string `json:"hostKey"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ID == "" {
		t.Fatalf("room id: %v %#v", err, body)
	}

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	host, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=host&room="+body.ID+"&key="+body.HostKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	join, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=join&room="+body.ID+"&name=bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer join.Close()

	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	var peer map[string]any
	if err := host.ReadJSON(&peer); err != nil {
		t.Fatal(err)
	}
	if peer["type"] != "peer" {
		t.Fatalf("host first = %#v", peer)
	}
	peerID, _ := peer["peerId"].(string)
	if err := host.WriteJSON(map[string]any{"type": "signal", "peerId": peerID, "kind": "offer", "sdp": "v=0", "mac": "abc"}); err != nil {
		t.Fatal(err)
	}
	_ = join.SetReadDeadline(time.Now().Add(2 * time.Second))
	var toJoin map[string]any
	if err := join.ReadJSON(&toJoin); err != nil {
		t.Fatal(err)
	}
	if toJoin["kind"] != "offer" || toJoin["sdp"] != "v=0" || toJoin["mac"] != "abc" {
		t.Fatalf("join got %#v (the relay must keep mac)", toJoin)
	}
	if err := join.WriteJSON(map[string]any{"type": "signal", "kind": "answer", "sdp": "v=1", "mac": "def"}); err != nil {
		t.Fatal(err)
	}
	var toHost map[string]any
	if err := host.ReadJSON(&toHost); err != nil {
		t.Fatal(err)
	}
	if toHost["kind"] != "answer" || toHost["mac"] != "def" || toHost["peerId"] != peerID {
		t.Fatalf("host got %#v (the relay must keep mac)", toHost)
	}
}

// A share link opened in a browser gets one plain line and nothing else: no page, no script, no stylesheet.
func TestJoinAnswersAPlainNoteAndTheRelayServesNoPage(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/join?room=x")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type %q", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q", got)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "This is a Keel share link. Open Keel and choose Join session, then paste the link.\n" {
		t.Fatalf("body %q", body)
	}
	for _, p := range []string{"/", "/join.js", "/joinpeer.js", "/sharemac.js", "/vendor/xterm/xterm.min.js", "/index.html"} {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404: the relay has no page of its own", p, r.StatusCode)
		}
	}
	if h, err := http.Get(srv.URL + "/healthz"); err != nil || h.StatusCode != http.StatusOK {
		t.Fatalf("/healthz: %v %v", h, err)
	}
}

func TestRoomCapAndExpiry(t *testing.T) {
	s := New()
	s.maxRooms = 2
	s.roomTTL = 200 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	mustRoom := func() (string, string) {
		t.Helper()
		res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("rooms %d", res.StatusCode)
		}
		var body struct {
			ID      string `json:"id"`
			HostKey string `json:"hostKey"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ID == "" {
			t.Fatalf("room id: %v %#v", err, body)
		}
		return body.ID, body.HostKey
	}
	mustRoom()
	mustRoom()
	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("cap status %d", res.StatusCode)
	}

	time.Sleep(250 * time.Millisecond)
	id, key := mustRoom()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=host&room="+id+"&key="+key, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestSignalRejectsCrossSiteOrigin(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		ID      string `json:"id"`
		HostKey string `json:"hostKey"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?role=host&room=" + body.ID + "&key=" + body.HostKey
	hdr := http.Header{}
	hdr.Set("Origin", "https://evil.example")
	conn, resp, err := websocket.DefaultDialer.Dial(ws, hdr)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-site websocket reject")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ws status = %d", resp.StatusCode)
	}
	// The relay serves no page, so not even its own origin or a loopback one has a reason to use it: any Origin is
	// refused. Keel's dialers (the host's and the joiner's) send none, which is what every other test here does.
	for _, origin := range []string{srv.URL, "http://127.0.0.1:47241", "http://localhost", "null"} {
		hdr.Set("Origin", origin)
		conn, resp, err = websocket.DefaultDialer.Dial(ws, hdr)
		if err == nil {
			conn.Close()
			t.Fatalf("Origin %q was admitted", origin)
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Origin %q: ws status = %d", origin, resp.StatusCode)
		}
	}
	conn, _, err = websocket.DefaultDialer.Dial(ws, nil)
	if err != nil {
		t.Fatalf("a dialer with no Origin (Keel's) was refused: %v", err)
	}
	conn.Close()
}
