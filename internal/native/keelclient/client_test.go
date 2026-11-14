package keelclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

const tok = "secret-token-value"

type frame struct {
	typ  int
	data []byte
}

type fake struct {
	c     *Client
	got   chan frame
	mu    sync.Mutex
	query map[string][]string
	last  string // last "METHOD path"
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{got: make(chan frame, 16)}
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Keel-Token") != tok || r.Header.Get("Origin") != "" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			f.mu.Lock()
			f.last = r.Method + " " + r.URL.Path
			f.query = r.URL.Query()
			f.mu.Unlock()
			h(w, r)
		}
	}
	mux.HandleFunc("/api/vault", auth(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"locked":true,"needsSetup":false,"firstRun":true,"sessions":3,"extra":1}`)
	}))
	mux.HandleFunc("/api/hosts", auth(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"id":"h1","name":"api","group":"prod","address":"10.0.0.1","port":22,"username":"u","authType":"key","color":"#fff","other":{}}]`)
	}))
	mux.HandleFunc("/api/sessions", auth(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"id":"s1","hostId":"h1","status":"ready","cols":80,"rows":24}]`)
	}))
	mux.HandleFunc("DELETE /api/sessions/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/ws/session", auth(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"status","status":"connecting","id":"s1"}`))
		ws.WriteMessage(websocket.BinaryMessage, []byte("snapshot"))
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"unknown","x":1}`))
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready","id":"s1","title":"api","status":"ready","driverId":1,"you":{"id":1,"role":"driver"}}`))
		ws.WriteMessage(websocket.BinaryMessage, []byte("live"))
		for {
			typ, d, err := ws.ReadMessage()
			if err != nil {
				return
			}
			f.got <- frame{typ, d}
		}
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.c = NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	return f
}

func readN(t *testing.T, c *Conn, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(c, b); err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func spawn(t *testing.T, f *fake) *Conn {
	t.Helper()
	c, err := f.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestJSONEndpoints(t *testing.T) {
	f := newFake(t)
	ctx := context.Background()
	v, err := f.c.Vault(ctx)
	if err != nil || !v.Locked || v.NeedsSetup || !v.FirstRun {
		t.Fatalf("vault %+v %v", v, err)
	}
	h, err := f.c.Hosts(ctx)
	if err != nil || len(h) != 1 || h[0].Name != "api" || h[0].Port != 22 || h[0].AuthType != "key" {
		t.Fatalf("hosts %+v %v", h, err)
	}
	s, err := f.c.Sessions(ctx)
	if err != nil || len(s) != 1 || s[0].ID != "s1" || s[0].Cols != 80 {
		t.Fatalf("sessions %+v %v", s, err)
	}
}

func TestKillSendsDelete(t *testing.T) {
	f := newFake(t)
	if err := f.c.Kill(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.last != "DELETE /api/sessions/s1" {
		t.Fatalf("last request %q", f.last)
	}
}

func TestBadTokenIsRefusedWithoutLeak(t *testing.T) {
	f := newFake(t)
	bad := NewAt(f.c.addr, "wrong-token-value")
	_, err := bad.Hosts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "wrong-token-value") {
		t.Fatal("error contains the token")
	}
	if _, err := bad.Spawn("h1", 80, 24); err == nil || strings.Contains(err.Error(), "wrong-token-value") {
		t.Fatalf("spawn err %v", err)
	}
}

func TestSpawnAndAttachQuery(t *testing.T) {
	t.Setenv("KEEL_NAME", "tester")
	f := newFake(t)
	spawn(t, f)
	f.mu.Lock()
	q := f.query
	f.mu.Unlock()
	if q["hostId"][0] != "h1" || q["id"] != nil || q["cols"][0] != "80" || q["rows"][0] != "24" || q["name"][0] != "tester" || q["kind"] != nil { // no kind: keel serve says a WebSocket is the window

		t.Fatalf("spawn query %v", q)
	}
	c, err := f.c.Attach("s9", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	readN(t, c, 1) // wait for the server handler to have run
	f.mu.Lock()
	q = f.query
	f.mu.Unlock()
	if q["id"][0] != "s9" || q["hostId"] != nil || q["cols"][0] != "100" {
		t.Fatalf("attach query %v", q)
	}
}

func TestReadOrderAcrossShortReads(t *testing.T) {
	c := spawn(t, newFake(t))
	var sb strings.Builder
	b := make([]byte, 3)
	for sb.Len() < 12 {
		n, err := c.Read(b)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b[:n])
	}
	if sb.String() != "snapshotlive" {
		t.Fatalf("got %q", sb.String())
	}
}

// Keystrokes go out as binary once keel serve has said this client drives; before that (no ready frame read yet) they
// are dropped, and not counted as a follower's.
func TestWriteBinary(t *testing.T) {
	f := newFake(t)
	c := spawn(t, f)
	if n, err := c.Write([]byte("early")); n != 5 || err != nil || c.Snapshot().Blocked != 0 {
		t.Fatalf("a write before the ready frame: %d %v, blocked %d", n, err, c.Snapshot().Blocked)
	}
	readN(t, c, len("snapshotlive")) // the ready frame is read on the way: this client drives
	c.Write([]byte("ls\n"))
	if fr := <-f.got; fr.typ != websocket.BinaryMessage || string(fr.data) != "ls\n" {
		t.Fatalf("%v %q: want only the driver's keys, after the ready frame", fr.typ, fr.data)
	}
}

func TestResizeText(t *testing.T) {
	f := newFake(t)
	c := spawn(t, f)
	c.Resize(120, 40)
	fr := <-f.got
	var m struct {
		Type       string
		Cols, Rows int
	}
	if fr.typ != websocket.TextMessage || json.Unmarshal(fr.data, &m) != nil || m.Type != "resize" || m.Cols != 120 || m.Rows != 40 {
		t.Fatalf("%v %q", fr.typ, fr.data)
	}
}

func TestReadySetsIDAndStatusFiresOnChange(t *testing.T) {
	f := newFake(t)
	c, err := f.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// OnChange set after dial may miss the first frame; the ready frame still changes title and id.
	changes := make(chan struct{}, 8)
	c.OnChange(func() { changes <- struct{}{} })
	readN(t, c, 12)
	if c.SessionID() != "s1" {
		t.Fatalf("id %q", c.SessionID())
	}
	if s := c.Snapshot(); s.Status != "ready" || s.Title != "api" {
		t.Fatalf("state %q %q", s.Status, s.Title)
	}
	select {
	case <-changes:
	default:
		t.Fatal("OnChange did not fire")
	}
}

func TestCreateHostPostsTheHostAndReadsItBackWithItsID(t *testing.T) {
	var body string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hosts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Keel-Token") != tok {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusCreated) // as keel serve answers a new host
		io.WriteString(w, `{"id":"h-new","name":"deploy@10.0.0.5","group":"Quick connect","address":"10.0.0.5","port":2222,"username":"deploy","authType":"password"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	nh := NewHost{Name: "deploy@10.0.0.5", Group: "Quick connect", Address: "10.0.0.5", Port: 2222, Username: "deploy", AuthType: "password", Password: "pw-typed-by-the-user", Color: "var(--ansi-12)"}
	h, err := c.CreateHost(context.Background(), nh)
	if err != nil || h.ID != "h-new" || h.Port != 2222 {
		t.Fatalf("%+v %v", h, err)
	}
	var sent map[string]any
	if json.Unmarshal([]byte(body), &sent) != nil || sent["password"] != "pw-typed-by-the-user" || sent["authType"] != "password" || sent["group"] != "Quick connect" {
		t.Errorf("body %s", body)
	}
	if strings.Contains(nh.String(), "pw-typed") {
		t.Error("a NewHost prints its password")
	}
	// An error names the method, path and status, never the body.
	bad := NewAt(strings.TrimPrefix(srv.URL, "http://"), "wrong")
	if _, err := bad.CreateHost(context.Background(), nh); err == nil || strings.Contains(err.Error(), "pw-typed") {
		t.Errorf("error %v", err)
	}
}
