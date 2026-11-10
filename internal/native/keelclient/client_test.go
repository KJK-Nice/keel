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
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready","id":"s1","title":"api","status":"ready"}`))
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
	if q["hostId"][0] != "h1" || q["id"] != nil || q["cols"][0] != "80" || q["rows"][0] != "24" || q["name"][0] != "tester" || q["kind"][0] != "web" {
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

func TestWriteBinary(t *testing.T) {
	f := newFake(t)
	c := spawn(t, f)
	c.Write([]byte("ls\n"))
	if fr := <-f.got; fr.typ != websocket.BinaryMessage || string(fr.data) != "ls\n" {
		t.Fatalf("%v %q", fr.typ, fr.data)
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
	status, title, _ := c.State()
	if status != "ready" || title != "api" {
		t.Fatalf("state %q %q", status, title)
	}
	select {
	case <-changes:
	default:
		t.Fatal("OnChange did not fire")
	}
}
