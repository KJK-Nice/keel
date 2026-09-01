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
	if !strings.Contains(string(body), `data-hugeicons="icon"`) || strings.Contains(string(body), `data-lucide="icon"`) {
		t.Fatal("index should render Hugeicons, not Lucide")
	}
	if !strings.Contains(string(body), `id="set-theme"`) || !strings.Contains(string(body), `class="tab-strip"`) || !strings.Contains(string(body), `id="tab-ctx-trigger"`) || !strings.Contains(string(body), "tabs-scroller") {
		t.Fatal("index missing settings theme select or tab strip")
	}
	if !strings.Contains(string(body), `data-tab-action="rename"`) ||
		!strings.Contains(string(body), "Close Others") ||
		!strings.Contains(string(body), "Close to the Right") {
		t.Fatal("index missing tab context menu")
	}
	if !strings.Contains(string(body), `id="set-ui-size-out"`) || !strings.Contains(string(body), `id="set-smooth"`) {
		t.Fatal("index missing UI size or font smoothing settings")
	}
	if !strings.Contains(string(body), `<dialog class="modal" id="host-modal"`) || !strings.Contains(string(body), `id="modal-scrim"`) {
		t.Fatal("index missing native host dialog or modal scrim")
	}
	if strings.Contains(string(body), `<template data-tui-dialog-portal`) && !strings.Contains(string(body), `data-slot="alert-dialog-content"`) {
		t.Fatal("unexpected dialog portal without confirm alert dialog")
	}
	if !strings.Contains(string(body), `data-slot="switch"`) || !strings.Contains(string(body), `data-tui-select-trigger`) {
		t.Fatal("index missing settings switch or select")
	}
	if strings.Contains(string(body), `class="ui-select`) {
		t.Fatal("index should not keep native ui-select")
	}
	if !strings.Contains(string(body), `id="host-identity"`) || !strings.Contains(string(body), `id="tunnel-host"`) || !strings.Contains(string(body), `id="host-auth-type"`) {
		t.Fatal("index missing host or tunnel form selects")
	}
	if !strings.Contains(string(body), `id="host-name"`) || !strings.Contains(string(body), `for="host-name"`) {
		t.Fatal("index missing labeled host name input")
	}
	if !strings.Contains(string(body), `data-tui-slider`) || !strings.Contains(string(body), `data-tui-tabs`) {
		t.Fatal("index missing settings slider or tabs")
	}
	if !strings.Contains(string(body), `data-slot="input-group"`) || !strings.Contains(string(body), `data-slot="button-group"`) {
		t.Fatal("index missing hosts input-group or button-group")
	}
	ig := strings.Index(string(body), `data-slot="input-group"`)
	if ig < 0 {
		t.Fatal("missing input-group")
	}
	igRest := string(body)[ig:]
	igEnd := strings.Index(igRest, ">")
	if igEnd < 0 || !strings.Contains(igRest[:igEnd], "h-10") {
		t.Fatal("hosts search input-group must use custom h-10")
	}
	if !strings.Contains(string(body), "min-w-max") {
		t.Fatal("host action dropdown must size to content, not the chevron")
	}
	if !strings.Contains(string(body), `id="btn-host-connect"`) || !strings.Contains(string(body), "Find a host or ssh user@hostname") {
		t.Fatal("index missing Termius-style host search connect")
	}
	for _, id := range []string{`id="btn-host-connect"`, `id="btn-new-host"`, `id="btn-welcome-local"`} {
		i := strings.Index(string(body), id)
		if i < 0 {
			t.Fatalf("missing %s", id)
		}
		rest := string(body)[i:]
		tagEnd := strings.Index(rest, ">")
		if tagEnd < 0 {
			t.Fatalf("%s: unclosed tag", id)
		}
		tag := rest[:tagEnd]
		if !strings.Contains(tag, "h-8") && !strings.Contains(tag, "size-8") {
			t.Fatalf("%s must use shadcn default height (h-8), not xs/sm", id)
		}
		if strings.Contains(tag, "h-6 ") || strings.Contains(tag, "h-7 ") {
			t.Fatalf("%s must not use compact xs/sm height", id)
		}
	}
	if !strings.Contains(string(body), `id="live-section"`) || strings.Contains(string(body), `id="herd-empty"`) {
		t.Fatal("index should hide live sessions until a PTY is attached")
	}
	if !strings.Contains(string(body), `data-slot="card"`) || !strings.Contains(string(body), `data-slot="aspect-ratio"`) {
		t.Fatal("index missing card or terminal preview aspect ratio")
	}
	if !strings.Contains(string(body), `data-slot="kbd"`) || !strings.Contains(string(body), `data-tui-tooltip-trigger`) {
		t.Fatal("index missing kbd or tooltip")
	}
	if !strings.Contains(string(body), `data-tui-checkbox`) || !strings.Contains(string(body), `data-slot="radio-group"`) {
		t.Fatal("index missing host form checkbox or color radio group")
	}
	if !strings.Contains(string(body), `data-tui-toaster`) {
		t.Fatal("index missing shadcn toaster")
	}
	if strings.Contains(string(body), `id="toast"`) {
		t.Fatal("index should not keep the old #toast fallback")
	}
	if !strings.Contains(string(body), `window.__keelApplyTheme`) {
		t.Fatal("index missing theme apply helper")
	}
	if !strings.Contains(string(body), `q.get("chrome") === "titlebar"`) {
		t.Fatal("index missing titlebar chrome bootstrap")
	}
	if !strings.Contains(string(body), `q.get("native") === "1"`) {
		t.Fatal("index missing native window bootstrap")
	}
	if !strings.Contains(string(body), "colorScheme") {
		t.Fatal("index missing color-scheme sync for native vibrancy")
	}
	if !strings.Contains(string(body), `data-fullscreen`) || !strings.Contains(string(body), "syncFullscreen") {
		t.Fatal("index missing fullscreen titlebar shift")
	}
	css, err := http.Get(srv.URL + "/css/keel.css")
	if err != nil {
		t.Fatal(err)
	}
	defer css.Body.Close()
	cssBody, _ := io.ReadAll(css.Body)
	if !strings.Contains(string(cssBody), `html[data-chrome="titlebar"] .workspace-bar`) ||
		!strings.Contains(string(cssBody), `html[data-chrome="titlebar"] .tab-strip`) ||
		!strings.Contains(string(cssBody), "--wails-draggable") {
		t.Fatal("keel.css missing titlebar tab overlay")
	}
	if !strings.Contains(string(cssBody), `html[data-native]`) ||
		!strings.Contains(string(cssBody), "background: transparent") {
		t.Fatal("keel.css missing native translucent chrome")
	}
	if !strings.Contains(string(cssBody), `html[data-chrome="titlebar"] #tab-ctx-trigger`) ||
		!strings.Contains(string(cssBody), "-webkit-app-region: no-drag") {
		t.Fatal("keel.css titlebar tabs must be no-drag so WKWebView delivers right-click")
	}
	if !strings.Contains(string(cssBody), `[data-tui-contextmenu-item]:not(:disabled)`) ||
		!strings.Contains(string(cssBody), "color-mix(in srgb, var(--text) 12%, transparent)") {
		t.Fatal("context menu items must highlight on hover")
	}
	if !strings.Contains(string(cssBody), "min-height: 42px") {
		t.Fatal("keel.css missing 42px titlebar overlay")
	}
	if !strings.Contains(string(cssBody), `html[data-chrome="titlebar"][data-fullscreen] .workspace-bar`) {
		t.Fatal("keel.css missing fullscreen tab shift")
	}
	if !strings.Contains(string(cssBody), "--ui-size") || !strings.Contains(string(cssBody), `html[data-smooth="subpixel"]`) {
		t.Fatal("keel.css missing UI size token or font smoothing")
	}
	tw, err := http.Get(srv.URL + "/css/output.css")
	if err != nil {
		t.Fatal(err)
	}
	defer tw.Body.Close()
	twBody, _ := io.ReadAll(tw.Body)
	if !strings.Contains(string(twBody), "--text-sm:1rem") {
		t.Fatal("output.css --text-sm must be 1rem so shadcn controls match --ui-size")
	}
	if !strings.Contains(string(cssBody), `html[data-chrome="titlebar"] .tab`) ||
		!strings.Contains(string(cssBody), "min-height: 32px") {
		t.Fatal("keel.css missing taller titlebar tabs")
	}
	if !strings.Contains(string(cssBody), ".tab-new:focus") || !strings.Contains(string(cssBody), "html.is-window-dragging") {
		t.Fatal("new-tab must not show a focus ring; window drag must keep the default cursor")
	}
	if !strings.Contains(string(cssBody), ".tab:not(.is-vault) { cursor: pointer") {
		t.Fatal("session tabs must use a pointer cursor, not grab")
	}
	if !strings.Contains(string(cssBody), ".tabs::-webkit-scrollbar") ||
		!strings.Contains(string(cssBody), "scrollbar-width: none") ||
		!strings.Contains(string(cssBody), "data-overflow-left") {
		t.Fatal("keel.css missing hidden tab-strip scrollbar or overflow fades")
	}
	if !strings.Contains(string(cssBody), ".term-tab.is-split .term-wrap.is-focused") ||
		!strings.Contains(string(cssBody), "::-webkit-scrollbar-thumb") {
		t.Fatal("keel.css missing split-only pane ring or terminal scrollbar")
	}
	if !strings.Contains(string(cssBody), "--keel-sidebar") {
		t.Fatal("keel.css missing prefixed chrome sidebar token")
	}
	res2, err := http.Get(srv.URL + "/api/identities")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("identities %d", res2.StatusCode)
	}
	if !strings.Contains(string(body), "/js/datastar.js") || !strings.Contains(string(body), "data-signals") {
		t.Fatal("index missing Datastar")
	}
	termJS, err := http.Get(srv.URL + "/js/term.js")
	if err != nil {
		t.Fatal(err)
	}
	defer termJS.Body.Close()
	termBody, _ := io.ReadAll(termJS.Body)
	if !strings.Contains(string(termBody), "showModal") || strings.Contains(string(termBody), "tui.dialog.open") {
		t.Fatal("term.js should open native <dialog> with showModal")
	}
	if !strings.Contains(string(termBody), "tui.toast.add") || strings.Contains(string(termBody), `getElementById("toast")`) {
		t.Fatal("term.js should raise toasts through tui.toast.add")
	}
	if !strings.Contains(string(termBody), "syncTabContextMenu") || !strings.Contains(string(termBody), "beginTabRename") {
		t.Fatal("term.js missing tab context menu handlers")
	}
	if !strings.Contains(string(termBody), "fitTabRename") || !strings.Contains(string(termBody), "input.size = 1") ||
		!strings.Contains(string(termBody), "setRangeText") ||
		!strings.Contains(string(cssBody), "field-sizing: content") {
		t.Fatal("tab rename must size to text, keep spaces, and not use a focus ring")
	}
	if !strings.Contains(string(cssBody), ".tab-rename:focus") || !strings.Contains(string(cssBody), "outline: none") {
		t.Fatal("tab rename input must not show a border or focus ring")
	}
	if !strings.Contains(string(cssBody), `html[data-chrome="titlebar"] .tab:has(.tab-rename)`) {
		t.Fatal("titlebar tab rename must drop vertical padding so the input matches tab height")
	}
	if !strings.Contains(string(cssBody), `.tab:has(.tab-rename) .tab-icon`) || !strings.Contains(string(cssBody), "align-self: center") {
		t.Fatal("tab rename must keep the tab icon vertically centered")
	}
	if !strings.Contains(string(termBody), `ev.button !== 2`) || !strings.Contains(string(termBody), `getElementById("tab-ctx")`) {
		t.Fatal("term.js must synthesize tab contextmenu when the titlebar swallows right-click")
	}
	if !strings.Contains(string(termBody), "allowTransparency") || !strings.Contains(string(termBody), "#0a0a0aa8") ||
		!strings.Contains(string(termBody), "#fbfbfaa8") {
		t.Fatal("term.js must glass the native terminal background per theme")
	}
	if !strings.Contains(string(termBody), "connectFromSearch") || !strings.Contains(string(termBody), "data-group-filter") {
		t.Fatal("term.js must connect from host search and filter by group cards")
	}
	if !strings.Contains(string(cssBody), `html[data-native] .term-body`) {
		t.Fatal("keel.css missing native terminal glass")
	}
	if !strings.Contains(string(cssBody), `#live-section[hidden]`) {
		t.Fatal("keel.css must hide the live section while it is empty")
	}
	if strings.Contains(string(cssBody), "--radius: 6px") {
		t.Fatal("keel.css must not override nova --radius with 6px")
	}
	if !strings.Contains(string(cssBody), "--radius: 0.625rem") {
		t.Fatal("keel.css must keep nova --radius on shadcn tokens")
	}
	if !strings.Contains(string(cssBody), `button:not([data-slot])`) {
		t.Fatal("button reset must skip every shadcn data-slot, including select-trigger")
	}
	if strings.Contains(string(cssBody), `button:not([data-slot="button"])`) {
		t.Fatal("button reset must not target only data-slot=button (select triggers need a border)")
	}
	if !strings.Contains(string(cssBody), `.modal [data-slot="select-trigger"]`) {
		t.Fatal("modal select triggers must share the field border with inputs")
	}
	if strings.Contains(string(cssBody), "button {\n  cursor: pointer;\n  background-color: transparent") {
		t.Fatal("unlayered button reset must not paint every button transparent")
	}
	if !strings.Contains(string(termBody), `chromeFocus.closest?.(".workspace-bar")`) {
		t.Fatal("boot must blur titlebar chrome so the new-tab button has no launch focus ring")
	}
	bundle, err := http.Get(srv.URL + "/components/shadcn-templ.js")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Body.Close()
	bundleBody, _ := io.ReadAll(bundle.Body)
	if bundle.StatusCode != http.StatusOK || len(bundleBody) < 100 {
		t.Fatalf("component scripts %d (%d bytes)", bundle.StatusCode, len(bundleBody))
	}
	if !strings.Contains(string(bundleBody), "data-tui-contextmenu-trigger") {
		t.Fatal("component scripts missing context menu")
	}
	frag, err := http.Get(srv.URL + "/ui/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer frag.Body.Close()
	fragBody, _ := io.ReadAll(frag.Body)
	if frag.StatusCode != http.StatusOK || !strings.Contains(string(fragBody), `id="host-list"`) {
		t.Fatalf("ui hosts %d: %s", frag.StatusCode, fragBody[:min(len(fragBody), 200)])
	}
	if !strings.Contains(string(fragBody), `data-slot="card"`) || !strings.Contains(string(fragBody), `data-connect`) {
		t.Fatal("ui hosts fragment should render host cards")
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
	addr := "127.0.0.1:47241"
	if v := os.Getenv("KEEL_SERVE_ADDR"); v != "" {
		addr = v
	}
	t.Log("keel ui http://" + addr)
	if err := http.ListenAndServe(addr, h); err != nil {
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
