package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/applog"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
	"keel/internal/tunnel"
)

type fakeBridge struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	once sync.Once
}

func newFakeBridge() *fakeBridge {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	return &fakeBridge{inR: inR, inW: inW, outR: outR, outW: outW}
}

func (f *fakeBridge) Read(p []byte) (int, error)  { return f.outR.Read(p) }
func (f *fakeBridge) Write(p []byte) (int, error) { return f.inW.Write(p) }
func (f *fakeBridge) Resize(int, int) error       { return nil }
func (f *fakeBridge) Close() error {
	f.once.Do(func() {
		_ = f.inW.Close()
		_ = f.outW.Close()
		_ = f.inR.Close()
		_ = f.outR.Close()
	})
	return nil
}

func testServer(t *testing.T) (*httptest.Server, *runtime.Runtime, *store.Store, *fakeBridge) {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	fb := newFakeBridge()
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return fb, nil
	})
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).Handler())
	t.Cleanup(srv.Close)
	return srv, rt, st, fb
}

func firstHost(t *testing.T, st *store.Store) store.Host {
	t.Helper()
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	return hosts[0]
}

func TestAllowLocalOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47241/api/hosts", nil)
	req.Host = "127.0.0.1:47241"
	if !allowLocalOrigin(req) {
		t.Fatal("missing origin should be allowed")
	}
	req.Header.Set("Origin", "https://evil.example")
	if allowLocalOrigin(req) {
		t.Fatal("cross-site origin")
	}
	req.Header.Set("Origin", "http://127.0.0.1:47241")
	if !allowLocalOrigin(req) {
		t.Fatal("same loopback origin")
	}
	req.Header.Set("Origin", "http://127.0.0.1:9999")
	if allowLocalOrigin(req) {
		t.Fatal("other loopback port")
	}
	req.Header.Set("Origin", "null")
	if allowLocalOrigin(req) {
		t.Fatal("null origin")
	}
}

func TestCrossSiteOriginRejected(t *testing.T) {
	srv, _, _, _ := testServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/hosts", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", res.StatusCode)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/herd"
	hdr := http.Header{}
	hdr.Set("Origin", "https://evil.example")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err == nil {
		conn.Close()
		t.Fatal("expected websocket reject")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ws status = %d", resp.StatusCode)
	}
}

func TestLoopbackOriginAllowed(t *testing.T) {
	srv, _, _, _ := testServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/hosts", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/herd"
	hdr := http.Header{}
	hdr.Set("Origin", srv.URL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestHealthUnlocked(t *testing.T) {
	srv, _, _, _ := testServer(t)
	res, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestVaultLockBlocksAPI(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Locked() {
		t.Skip("OS keychain auto-unlocked the vault")
	}
	rt := runtime.New(st, nil)
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusLocked {
		t.Fatalf("status = %d", res.StatusCode)
	}
	body, _ := json.Marshal(map[string]string{"passphrase": "choose-a-phrase"})
	res2, err := http.Post(srv.URL+"/api/vault/setup", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res2.Body)
		t.Fatalf("setup %d %s", res2.StatusCode, b)
	}
	res3, err := http.Get(srv.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("after setup status = %d", res3.StatusCode)
	}
}

func TestSessionControlAndFanout(t *testing.T) {
	srv, _, st, fb := testServer(t)
	host := firstHost(t, st)
	info := spawn(t, srv.URL, host.ID)

	alice := dialSession(t, srv.URL, info.ID, "alice", "web")
	defer alice.Close()
	bob := dialSession(t, srv.URL, info.ID, "bob", "web")
	defer bob.Close()

	readyA := waitJSON(t, alice, "ready")
	if readyA["you"].(map[string]any)["role"] != "driver" {
		t.Fatalf("alice you = %#v", readyA["you"])
	}
	readyB := waitJSON(t, bob, "ready")
	if readyB["you"].(map[string]any)["role"] != "follower" {
		t.Fatalf("bob you = %#v", readyB["you"])
	}

	if err := bob.WriteMessage(websocket.BinaryMessage, []byte("blocked")); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan string, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		wrote <- string(buf[:n])
	}()
	select {
	case got := <-wrote:
		t.Fatalf("follower write reached PTY: %q", got)
	case <-time.After(80 * time.Millisecond):
	}

	target := uint64(readyB["you"].(map[string]any)["id"].(float64))
	if err := alice.WriteJSON(map[string]any{"type": "grant-control", "targetId": target}); err != nil {
		t.Fatal(err)
	}
	waitJSON(t, bob, "control")
	if err := bob.WriteMessage(websocket.BinaryMessage, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-wrote:
		if got != "ok" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("driver write did not reach PTY")
	}

	if _, err := fb.outW.Write([]byte("shared-out")); err != nil {
		t.Fatal(err)
	}
	if got := waitBinary(t, alice, "shared-out"); !strings.Contains(got, "shared-out") {
		t.Fatalf("alice bytes %q", got)
	}
	if got := waitBinary(t, bob, "shared-out"); !strings.Contains(got, "shared-out") {
		t.Fatalf("bob bytes %q", got)
	}
}

func TestSessionReconnect(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		bridges = append(bridges, fb)
		return fb, nil
	})
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).Handler())
	t.Cleanup(srv.Close)

	host := firstHost(t, st)
	info := spawn(t, srv.URL, host.ID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := rt.Get(info.ID)
		if err == nil && got.Info().Status == runtime.StatusReady {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if len(bridges) < 1 {
		mu.Unlock()
		t.Fatal("no bridge")
	}
	_ = bridges[0].Close()
	mu.Unlock()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := rt.Get(info.ID)
		if err == nil && got.Info().Status == runtime.StatusExited {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	res, err := http.Post(srv.URL+"/api/sessions/"+info.ID+"/reconnect", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("reconnect %d %s", res.StatusCode, b)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := rt.Get(info.ID)
		if err == nil && got.Info().Status == runtime.StatusReady {
			mu.Lock()
			n := len(bridges)
			mu.Unlock()
			if n >= 2 {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	n := len(bridges)
	mu.Unlock()
	st2, _ := rt.Get(info.ID)
	t.Fatalf("reconnect stuck calls=%d status=%s", n, st2.Info().Status)
}

func TestHerdWS(t *testing.T) {
	srv, _, st, _ := testServer(t)
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/herd"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var snap map[string]any
	if err := conn.ReadJSON(&snap); err != nil {
		t.Fatal(err)
	}
	if snap["type"] != "snapshot" {
		t.Fatalf("first = %#v", snap)
	}
	host := firstHost(t, st)
	spawn(t, srv.URL, host.ID)
	var ev map[string]any
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatal(err)
	}
	if ev["type"] != "spawn" {
		t.Fatalf("herd ev = %#v", ev)
	}
}

func TestIdentityTunnelLogsAPI(t *testing.T) {
	srv, _, st, _ := testServer(t)
	logs := applog.New(t.TempDir() + "/keel.log")
	tnRT := tunnel.New(st, func(store.Host) (tunnel.Dialer, error) {
		return nil, errors.New("no ssh in test")
	})
	// rebuild server with extras
	srv.Close()
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	srv2 := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithTunnels(tnRT).WithLogs(logs).Handler())
	t.Cleanup(srv2.Close)

	body := `{"name":"deploy","kind":"password","username":"ubuntu","password":"pw"}`
	res, err := http.Post(srv2.URL+"/api/identities", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("ident %d %s", res.StatusCode, b)
	}
	var ident store.Identity
	if err := json.NewDecoder(res.Body).Decode(&ident); err != nil {
		t.Fatal(err)
	}
	if ident.Password != "__saved__" {
		t.Fatalf("password %q", ident.Password)
	}
	body2 := `{"hostname":"box:22","fingerprint":"SHA256:abc"}`
	resKH, err := http.Post(srv2.URL+"/api/known-hosts", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resKH.Body.Close()
	if resKH.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resKH.Body)
		t.Fatalf("known-hosts %d %s", resKH.StatusCode, b)
	}
	if err := st.CheckHostKey("box:22", "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	host := firstHost(t, st)
	tunBody := `{"name":"db","hostId":"` + host.ID + `","kind":"local","bindAddr":"127.0.0.1","bindPort":0,"destHost":"127.0.0.1","destPort":5432}`
	res2, err := http.Post(srv2.URL+"/api/tunnels", "application/json", strings.NewReader(tunBody))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res2.Body)
		t.Fatalf("tunnel %d %s", res2.StatusCode, b)
	}
	res3, err := http.Get(srv2.URL + "/api/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("logs %d", res3.StatusCode)
	}
	var entries []map[string]any
	if err := json.NewDecoder(res3.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected log entries")
	}
}

func TestIndexServesVault(t *testing.T) {
	web := os.DirFS(filepath.Join("..", "..", "cmd", "keel", "web"))
	if _, err := fs.Stat(web, "index.html"); err != nil {
		t.Skip(err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	srv := httptest.NewServer(New(st, rt, web, nil).WithLogs(applog.New(t.TempDir() + "/keel.log")).Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "vault-rail") || !strings.Contains(string(body), `data-vault="settings"`) || !strings.Contains(string(body), "hostkey-modal") {
		t.Fatalf("index missing vault chrome: %s", string(body[:min(len(body), 400)]))
	}
	if !strings.Contains(string(body), `class="vault-nav-icon"`) || strings.Contains(string(body), `id="btn-settings"`) {
		t.Fatal("index should have vault rail icons and no titlebar settings button")
	}
	if !strings.Contains(string(body), `id="set-theme"`) || !strings.Contains(string(body), `class="tab-strip"`) {
		t.Fatal("index missing settings theme select or tab strip")
	}
	if !strings.Contains(string(body), `q.get("chrome") === "titlebar"`) {
		t.Fatal("index missing titlebar chrome bootstrap")
	}
	css, err := http.Get(srv.URL + "/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer css.Body.Close()
	cssBody, _ := io.ReadAll(css.Body)
	if !strings.Contains(string(cssBody), `html[data-chrome="titlebar"] .workspace-bar`) ||
		!strings.Contains(string(cssBody), `html[data-chrome="titlebar"] .tab-strip`) ||
		!strings.Contains(string(cssBody), "--wails-draggable") {
		t.Fatal("app.css missing titlebar tab overlay")
	}
	res2, err := http.Get(srv.URL + "/api/identities")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("identities %d", res2.StatusCode)
	}
}

func TestManualServe(t *testing.T) {
	if os.Getenv("KEEL_SERVE") != "1" {
		t.Skip("set KEEL_SERVE=1 to bind 127.0.0.1:47241")
	}
	web := os.DirFS(filepath.Join("..", "..", "cmd", "keel", "web"))
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	dir := t.TempDir()
	st, err := store.New(dir + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	logs := applog.New(dir + "/keel.log")
	tn := tunnel.New(st, tunnel.DefaultFactory(st))
	h := New(st, rt, web, nil).WithTunnels(tn).WithLogs(logs).Handler()
	t.Log("keel ui http://127.0.0.1:47241")
	if err := http.ListenAndServe("127.0.0.1:47241", h); err != nil {
		t.Fatal(err)
	}
}

func spawn(t *testing.T, base, hostID string) runtime.Info {
	t.Helper()
	res, err := http.Post(base+"/api/sessions", "application/json", strings.NewReader(`{"hostId":"`+hostID+`","cols":80,"rows":24}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("spawn %d %s", res.StatusCode, b)
	}
	var info runtime.Info
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	return info
}

func dialSession(t *testing.T, base, id, name, kind string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(base, "http") + "/ws/session?id=" + id + "&name=" + name + "&kind=" + kind
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func waitJSON(t *testing.T, conn *websocket.Conn, typ string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if mt != websocket.TextMessage {
			continue
		}
		var body map[string]any
		if json.Unmarshal(msg, &body) != nil {
			continue
		}
		if body["type"] == typ {
			return body
		}
	}
	t.Fatalf("timeout waiting for %s", typ)
	return nil
}

func waitBinary(t *testing.T, conn *websocket.Conn, want string) string {
	t.Helper()
	var out string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		out += string(msg)
		if strings.Contains(out, want) {
			return out
		}
	}
	t.Fatalf("timeout waiting for binary %q got %q", want, out)
	return out
}
