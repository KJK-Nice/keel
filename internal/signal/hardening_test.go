package signal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type roomResp struct {
	ID      string `json:"id"`
	HostKey string `json:"hostKey"`
}

func postRoom(t *testing.T, base string, xff string) (*http.Response, roomResp) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/rooms", nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body roomResp
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return res, body
}

func TestHostKeyRequired(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	_, room := postRoom(t, srv.URL, "")
	if len(room.ID) != 32 || len(room.HostKey) != 32 {
		t.Fatalf("room %+v: want 128-bit id and host key", room)
	}
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?role=host&room=" + room.ID
	for _, suffix := range []string{"", "&key=", "&key=" + strings.Repeat("0", 32)} {
		conn, resp, err := websocket.DefaultDialer.Dial(ws+suffix, nil)
		if err == nil {
			conn.Close()
			t.Fatalf("host connected with %q", suffix)
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("host with %q: resp %v, want 403", suffix, resp)
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(ws+"&key="+room.HostKey, nil)
	if err != nil {
		t.Fatalf("host with its key: %v", err)
	}
	conn.Close()
}

func TestRoomCreationRateLimited(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	for i := 0; i < roomBurst; i++ {
		if res, _ := postRoom(t, srv.URL, "203.0.113.7"); res.StatusCode != http.StatusOK {
			t.Fatalf("room %d: %d", i, res.StatusCode)
		}
	}
	res, _ := postRoom(t, srv.URL, "203.0.113.7")
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over the burst: %d, want 429", res.StatusCode)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	// Spoofed leading entries don't help: the proxy-appended last one counts.
	if res, _ := postRoom(t, srv.URL, "198.51.100.1, 203.0.113.7"); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF prefix: %d, want 429", res.StatusCode)
	}
	if res, _ := postRoom(t, srv.URL, "203.0.113.8"); res.StatusCode != http.StatusOK {
		t.Fatalf("another client: %d, want 200", res.StatusCode)
	}
}

func TestRateLimiterRefills(t *testing.T) {
	l := newRateLimiter(2, time.Second)
	now := time.Unix(1000, 0)
	if !l.allow("a", now) || !l.allow("a", now) || l.allow("a", now) {
		t.Fatal("burst of 2 not enforced")
	}
	if !l.allow("a", now.Add(time.Second)) {
		t.Fatal("no refill after one interval")
	}
	l.sweep(now.Add(time.Hour))
	if len(l.buckets) != 0 {
		t.Fatal("sweep kept an idle bucket")
	}
}

func hostedRoom(t *testing.T) (ws string, host *websocket.Conn) {
	t.Helper()
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	_, room := postRoom(t, srv.URL, "")
	ws = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + room.ID
	host, _, err := websocket.DefaultDialer.Dial(ws+"&role=host&key="+room.HostKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	return ws, host
}

func TestJoinerCap(t *testing.T) {
	ws, host := hostedRoom(t)
	go func() { // drain "peer" notices
		for {
			if _, _, err := host.ReadMessage(); err != nil {
				return
			}
		}
	}()
	for i := 0; i < maxJoiners; i++ {
		c, _, err := websocket.DefaultDialer.Dial(ws+"&role=join", nil)
		if err != nil {
			t.Fatalf("joiner %d: %v", i, err)
		}
		t.Cleanup(func() { c.Close() })
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, resp, err := websocket.DefaultDialer.Dial(ws+"&role=join", nil)
		if err != nil {
			if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("over the cap: %v %v, want 503", resp, err)
			}
			return
		}
		c.Close()
		// The server registers joiners asynchronously; give it a moment.
		if time.Now().After(deadline) {
			t.Fatalf("joiner %d accepted past the cap of %d", maxJoiners+1, maxJoiners)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOversizedMessageClosesConnection(t *testing.T) {
	_, host := hostedRoom(t)
	big := `{"type":"signal","sdp":"` + strings.Repeat("x", maxMessage+1) + `"}`
	_ = host.WriteMessage(websocket.TextMessage, []byte(big))
	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := host.ReadMessage(); err == nil {
		t.Fatal("connection still open after an oversized message")
	}
}
