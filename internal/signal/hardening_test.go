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
	sig := New()
	if err := sig.SetClientIP("xff:1"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(sig.Handler())
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
	for i := 0; i < 2; i++ {
		if !l.allow("a", now) {
			t.Fatalf("request %d within the burst refused", i+1)
		}
	}
	if l.allow("a", now) {
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

func TestRateLimitOffByDefault(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	for i := 0; i < roomBurst+3; i++ {
		if res, _ := postRoom(t, srv.URL, "203.0.113.7"); res.StatusCode != http.StatusOK {
			t.Fatalf("room %d with no client IP source configured: %d, want 200", i, res.StatusCode)
		}
	}
}

func TestSetClientIP(t *testing.T) {
	req := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/rooms", nil)
		r.RemoteAddr = remote
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	cases := []struct {
		spec string
		r    *http.Request
		want string
	}{
		{"remote", req("198.51.100.4:5555", "1.2.3.4"), "198.51.100.4"},
		{"xff:1", req("10.0.0.1:1", "1.2.3.4, 203.0.113.9"), "203.0.113.9"},
		{"xff:2", req("10.0.0.1:1", "203.0.113.9, 100.64.0.7"), "203.0.113.9"},
		{"xff:2", req("10.0.0.1:1", "203.0.113.9", "100.64.0.7"), "203.0.113.9"},
		{"xff:2", req("10.0.0.1:1", "100.64.0.7"), "unknown"},
		{"xff:1", req("10.0.0.1:1", "not-an-ip"), "unknown"},
		{"xff:1", req("10.0.0.1:1", "2001:db8:1:2:aaaa::1"), "2001:db8:1:2::/64"},
		{"remote", req("[2001:db8:1:2::9]:443"), "2001:db8:1:2::/64"},
	}
	for _, c := range cases {
		s := New()
		if err := s.SetClientIP(c.spec); err != nil {
			t.Fatal(err)
		}
		if got := s.clientKey(c.r); got != c.want {
			t.Errorf("%s %v: key %q, want %q", c.spec, c.r.Header.Values("X-Forwarded-For"), got, c.want)
		}
	}
	for _, bad := range []string{"xff", "xff:0", "xff:-1", "xff:x", "proxy"} {
		if err := New().SetClientIP(bad); err == nil {
			t.Errorf("SetClientIP(%q) accepted", bad)
		}
	}
	s := New()
	if err := s.SetClientIP(""); err != nil || s.clientKey != nil {
		t.Fatal(`SetClientIP("") must turn the limit off`)
	}
}

// Joiners that connect before the host all wait; the cap must still hold
// once the host arrives.
func TestJoinerCapHoldsForJoinersWaitingOnHost(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	_, room := postRoom(t, srv.URL, "")
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + room.ID
	const extra = 4
	for i := 0; i < maxJoiners+extra; i++ {
		c, _, err := websocket.DefaultDialer.Dial(ws+"&role=join", nil)
		if err != nil {
			t.Fatalf("joiner %d: %v", i, err)
		}
		t.Cleanup(func() { c.Close() })
	}
	host, _, err := websocket.DefaultDialer.Dial(ws+"&role=host&key="+room.HostKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	peers := 0
	_ = host.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	for {
		var msg envelope
		if err := host.ReadJSON(&msg); err != nil {
			break
		}
		if msg.Type == "peer" {
			peers++
		}
	}
	if peers != maxJoiners {
		t.Fatalf("host saw %d joiners, want the cap of %d", peers, maxJoiners)
	}
}

// Pings keep a quiet connection open past the idle timeout.
func TestPingsKeepQuietConnectionsOpen(t *testing.T) {
	sig := New()
	sig.idleTimeout, sig.pingInterval = 300*time.Millisecond, 50*time.Millisecond
	srv := httptest.NewServer(sig.Handler())
	t.Cleanup(srv.Close)
	_, room := postRoom(t, srv.URL, "")
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + room.ID
	host, _, err := websocket.DefaultDialer.Dial(ws+"&role=host&key="+room.HostKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	// The gorilla client answers pings while it reads.
	go func() {
		for {
			if _, _, err := host.ReadMessage(); err != nil {
				return
			}
		}
	}()
	time.Sleep(4 * sig.idleTimeout)
	j, _, err := websocket.DefaultDialer.Dial(ws+"&role=join", nil)
	if err != nil {
		t.Fatalf("room gone after a quiet host: %v", err)
	}
	j.Close()
}
