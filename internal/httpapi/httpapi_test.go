package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/applog"
	"keel/internal/inject"
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

func TestHostTagsAPI(t *testing.T) {
	srv, _, _, _ := testServer(t)
	body := `{"name":"gpu-box","address":"10.0.0.9","port":22,"username":"ubuntu","authType":"password","password":"pw","color":"#6ea8ff","tags":[" prod ","#GPU","prod"]}`
	res, err := http.Post(srv.URL+"/api/hosts", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create %d %s", res.StatusCode, b)
	}
	var saved store.Host
	if err := json.NewDecoder(res.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Tags) != 2 || saved.Tags[0] != "prod" || saved.Tags[1] != "GPU" {
		t.Fatalf("create tags = %#v", saved.Tags)
	}

	listRes, err := http.Get(srv.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	var hosts []store.Host
	if err := json.NewDecoder(listRes.Body).Decode(&hosts); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hosts {
		if h.ID != saved.ID {
			continue
		}
		found = true
		if len(h.Tags) != 2 || h.Tags[0] != "prod" || h.Tags[1] != "GPU" {
			t.Fatalf("list tags = %#v", h.Tags)
		}
	}
	if !found {
		t.Fatal("created host missing from list")
	}

	// The sidebar tree has no tag chips; typing a tag into its filter
	// finds the host.
	uiRes, err := http.Get(srv.URL + "/ui/hosts?datastar=" + url.QueryEscape(`{"query":"gpu"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer uiRes.Body.Close()
	frag, _ := io.ReadAll(uiRes.Body)
	if !strings.Contains(string(frag), `data-connect="`+saved.ID+`"`) {
		t.Fatalf("host list filtered by tag misses the host: %s", frag)
	}
	missRes, err := http.Get(srv.URL + "/ui/hosts?datastar=" + url.QueryEscape(`{"query":"no-such-tag"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer missRes.Body.Close()
	miss, _ := io.ReadAll(missRes.Body)
	if strings.Contains(string(miss), `data-connect="`+saved.ID+`"`) {
		t.Fatal("host list filter must drop hosts whose tags do not match")
	}
}

func TestRuntimeStop(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	stopped := make(chan struct{}, 1)
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithStop(func() { stopped <- struct{}{} }).Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/api/runtime/stop", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", res.StatusCode)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("onStop was not called")
	}

	bare := httptest.NewServer(New(st, rt, fs.FS(web), nil).Handler())
	t.Cleanup(bare.Close)
	res2, err := http.Post(bare.URL+"/api/runtime/stop", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("disabled stop status = %d", res2.StatusCode)
	}
}

func TestVaultLockBlocksAPI(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	// Never touch the real "keel"/"vault" keychain item from a test.
	t.Setenv("KEEL_KEYRING", "off")
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
	// The server subscribes after the WebSocket upgrade, so wait until alice
	// is attached before dialing bob; otherwise bob can attach first and
	// become the driver.
	readyA := waitJSON(t, alice, "ready")
	if readyA["you"].(map[string]any)["role"] != "driver" {
		t.Fatalf("alice you = %#v", readyA["you"])
	}
	bob := dialSession(t, srv.URL, info.ID, "bob", "web")
	defer bob.Close()
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

func TestPaletteWSFocusAndApprovedInject(t *testing.T) {
	srv, _, st, fb := testServer(t)
	host := firstHost(t, st)
	info := spawn(t, srv.URL, host.ID)
	alice := dialSession(t, srv.URL, info.ID, "alice", "web")
	defer alice.Close()
	waitJSON(t, alice, "ready")

	got := make(chan string, 1)
	readPTY := func() {
		go func() {
			buf := make([]byte, 32)
			n, _ := fb.inR.Read(buf)
			got <- string(buf[:n])
		}()
	}

	if err := alice.WriteJSON(map[string]any{"type": "focus", "surface": "palette"}); err != nil {
		t.Fatal(err)
	}
	readPTY()
	if err := alice.WriteMessage(websocket.BinaryMessage, []byte("live")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "live" {
			t.Fatalf("live WS Write while palette focused got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("live websocket binary must still use ungated Session.Write")
	}

	readPTY()
	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "rm\n", "approved": false}); err != nil {
		t.Fatal(err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received unapproved inject %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "ls\n", "approved": true}); err != nil {
		t.Fatal(err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received client-only approved:true %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	if err := alice.WriteJSON(map[string]any{"type": "inject-challenge", "command": "ls\n"}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, alice, "inject-challenge")
	nonce, _ := chal["nonce"].(string)
	if nonce == "" {
		t.Fatal("inject-challenge must return a server nonce")
	}
	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "ls\n", "nonce": nonce}); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "ls\n" {
			t.Fatalf("ticket inject got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ticket inject")
	}

	if err := alice.WriteJSON(map[string]any{"type": "focus", "surface": "term"}); err != nil {
		t.Fatal(err)
	}
	readPTY()
	if err := alice.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "x" {
			t.Fatalf("restored live WS got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for live Write after SurfaceTerm restore")
	}
}

func TestInjectNonceOneShotRejectsClientApprovedReplay(t *testing.T) {
	assertInjectNonceOneShotReplay(t)
}

// TestInjectNonceOneShotReplay is the #46-visible name for the #45 gate:
// client-forged approved:true never writes the PTY; a server nonce writes once; replay is blocked.
func TestInjectNonceOneShotReplay(t *testing.T) {
	assertInjectNonceOneShotReplay(t)
}

func assertInjectNonceOneShotReplay(t *testing.T) {
	t.Helper()
	srv, _, st, fb := testServer(t)
	host := firstHost(t, st)
	info := spawn(t, srv.URL, host.ID)
	alice := dialSession(t, srv.URL, info.ID, "alice", "web")
	defer alice.Close()
	waitJSON(t, alice, "ready")

	got := make(chan string, 1)
	readPTY := func() {
		go func() {
			buf := make([]byte, 32)
			n, _ := fb.inR.Read(buf)
			got <- string(buf[:n])
		}()
	}

	readPTY()
	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "ls\n", "approved": true}); err != nil {
		t.Fatal(err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received client-only approved:true %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	if err := alice.WriteJSON(map[string]any{"type": "inject-challenge", "command": "ls\n"}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, alice, "inject-challenge")
	nonce, _ := chal["nonce"].(string)
	if nonce == "" {
		t.Fatal("missing server nonce")
	}

	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "ls\n", "nonce": nonce, "approved": true}); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "ls\n" {
			t.Fatalf("first ticket inject got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first ticket inject")
	}

	readPTY()
	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "ls\n", "nonce": nonce, "approved": true}); err != nil {
		t.Fatal(err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received replayed nonce %q", leak)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestChromeWSFocusAndNoUnapprovedInject(t *testing.T) {
	srv, rt, st, fb := testServer(t)
	host := firstHost(t, st)
	info := spawn(t, srv.URL, host.ID)
	alice := dialSession(t, srv.URL, info.ID, "alice", "web")
	defer alice.Close()
	ready := waitJSON(t, alice, "ready")
	aliceID := uint64(ready["you"].(map[string]any)["id"].(float64))
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitFocus := func(want inject.Surface) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if sess.Focus(aliceID) == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("focus = %q want %q", sess.Focus(aliceID), want)
	}

	got := make(chan string, 1)
	readPTY := func() {
		go func() {
			buf := make([]byte, 32)
			n, _ := fb.inR.Read(buf)
			got <- string(buf[:n])
		}()
	}

	if err := alice.WriteJSON(map[string]any{"type": "focus", "surface": "chrome"}); err != nil {
		t.Fatal(err)
	}
	waitFocus(inject.SurfaceChrome)
	if err := sess.Keystroke(aliceID, []byte("i"), sess.Focus(aliceID)); !errors.Is(err, inject.ErrNotTerminalFocus) {
		t.Fatalf("chrome keystroke err = %v", err)
	}
	readPTY()
	if err := alice.WriteMessage(websocket.BinaryMessage, []byte("live")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "live" {
			t.Fatalf("live WS Write while chrome focused got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("live websocket binary must still use ungated Session.Write")
	}

	readPTY()
	if err := alice.WriteJSON(map[string]any{"type": "inject", "command": "rm\n", "approved": false}); err != nil {
		t.Fatal(err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received unapproved inject %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	if err := alice.WriteJSON(map[string]any{"type": "focus", "surface": "term"}); err != nil {
		t.Fatal(err)
	}
	waitFocus(inject.SurfaceTerm)
	if err := sess.Keystroke(aliceID, []byte("k"), sess.Focus(aliceID)); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "k" {
			t.Fatalf("restored Keystroke got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Keystroke after SurfaceTerm restore")
	}

	readPTY()
	if err := alice.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if hit != "x" {
			t.Fatalf("restored live WS got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for live Write after SurfaceTerm restore")
	}
}

func TestSessionWSKeepsWriteUngated(t *testing.T) {
	src, err := os.ReadFile("httpapi.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "sess.Write(cl.ID, msg)") {
		t.Fatal("live websocket binary must call Session.Write")
	}
	if strings.Contains(body, "sess.Keystroke(cl.ID, msg") {
		t.Fatal("live websocket binary must not be routed through Keystroke")
	}
	if !strings.Contains(body, "sess.SetFocus(cl.ID") {
		t.Fatal("focus JSON must call Session.SetFocus")
	}
	if !strings.Contains(body, "sess.IssueInjectTicket(cl.ID") || !strings.Contains(body, "sess.InjectTicket(cl.ID") {
		t.Fatal("palette/chrome inject JSON must issue and consume a server ticket")
	}
	if strings.Contains(body, "if ctl.Approved") || strings.Contains(body, "approved = inject.Approve()") {
		t.Fatal("inject must not mint Approve from a client approved flag")
	}
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)
	injStart := strings.Index(term, "function sendApprovedInject")
	injEndRel := strings.Index(term[injStart+1:], "\n  function ")
	if injStart < 0 || injEndRel < 0 {
		t.Fatal("missing sendApprovedInject")
	}
	inj := term[injStart : injStart+1+injEndRel]
	if strings.Contains(inj, "sendSessionBytes") {
		t.Fatal("palette inject must not send live websocket binary")
	}
	if !strings.Contains(inj, `type: "inject"`) || !strings.Contains(inj, "nonce") || strings.Contains(inj, "approved: true") {
		t.Fatal("palette inject must send a server nonce, not client approved:true")
	}
	if !strings.Contains(term, `type: "inject-challenge"`) {
		t.Fatal("palette inject must request a server inject-challenge")
	}
	if strings.Count(term, "function ptyWritesBlocked") != 1 {
		t.Fatal("ptyWritesBlocked must be a single merged implementation")
	}
	if !strings.Contains(term, "sidebarFocused() || state.openModal || ptyInjectWaiter") ||
		!strings.Contains(term, "Date.now() < ptyChromeHoldUntil") {
		t.Fatal("ptyWritesBlocked must OR vault sidebar keyboard focus with palette/inject chrome holds")
	}
	if !strings.Contains(term, `els.ptyInjectModal?.open || sidebarFocused()`) {
		t.Fatal("vault sidebar focus must SetFocus SurfaceChrome; term focus restores SurfaceTerm while the sidebar stays open")
	}
	if !strings.Contains(term, "if (ptyWritesBlocked()) return") {
		t.Fatal("xterm onData must not Write while the session rail or inject chrome owns focus")
	}
	paint := term[strings.Index(term, "function paintSidebar"):]
	if i := strings.Index(paint[1:], "\n  function "); i > 0 {
		paint = paint[:i]
	}
	if strings.Contains(paint, `type: "inject"`) || strings.Contains(paint, "sendSessionBytes") || strings.Contains(paint, "sendSessionJSON") {
		t.Fatal("vault sidebar chrome must not Inject or write a live PTY")
	}
}

// TestOpenSidebarKeepsTermKeys: the Vault sidebar (which replaced the right
// session rail in phase 2) is chrome only while keyboard focus is inside
// it. Open beside a focused terminal, keys still reach the PTY.
func TestOpenSidebarKeepsTermKeys(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)

	focused := sliceJSFunc(term, "function sidebarFocused")
	if focused == "" {
		t.Fatal("missing sidebarFocused")
	}
	if !strings.Contains(focused, `"vault-sidebar"`) || !strings.Contains(focused, "contains") || !strings.Contains(focused, "activeElement") {
		t.Fatal("sidebarFocused must be DOM focus inside #vault-sidebar, not sidebar visibility")
	}

	blocked := sliceJSFunc(term, "function ptyWritesBlocked")
	if blocked == "" {
		t.Fatal("missing ptyWritesBlocked")
	}
	if strings.Contains(blocked, "sidebarOpen()") {
		t.Fatal("ptyWritesBlocked must not OR sidebar-open alone; that eats term keys while the sidebar stays visible")
	}
	if !strings.Contains(blocked, "sidebarFocused()") || !strings.Contains(blocked, "state.openModal") ||
		!strings.Contains(blocked, "ptyInjectWaiter") || !strings.Contains(blocked, "Date.now() < ptyChromeHoldUntil") {
		t.Fatal("only sidebar/chrome focus (plus palette/inject/notebook/CLI holds) may block silent Writes")
	}

	wire := sliceJSFunc(term, "function wirePaneTerminal")
	if !strings.Contains(wire, "if (ptyWritesBlocked()) return") || !strings.Contains(wire, "sendSessionBytes") {
		t.Fatal("sidebar open + term focused must still reach sendSessionBytes; chrome never silent-Writes")
	}

	setSide := sliceJSFunc(term, "function setSidebar")
	restore := sliceJSFunc(term, "function restoreTermKeyboard")
	release := sliceJSFunc(term, "function releaseSidebarFocus")
	if setSide == "" || restore == "" || release == "" {
		t.Fatal("missing setSidebar / restoreTermKeyboard / releaseSidebarFocus")
	}
	if strings.Contains(setSide, `getElementById("vault-sidebar")?.focus`) || strings.Contains(setSide, `setSessionSurface("chrome")`) {
		t.Fatal("toggling the sidebar must not steal term focus or permanently pin SurfaceChrome")
	}
	if !strings.Contains(setSide, "restoreTermKeyboard") || !strings.Contains(restore, "term?.focus") {
		t.Fatal("toggling the sidebar must restore terminal keyboard so keys reach the PTY")
	}

	mark := sliceJSFunc(term, "function markFocusedPane")
	if !strings.Contains(mark, "releaseSidebarFocus") || !strings.Contains(mark, "syncSessionSurface") ||
		!strings.Contains(mark, "term?.focus") {
		t.Fatal("click/focus back on the term must drop sidebar focus and sync SurfaceTerm while the sidebar stays open")
	}

	sync := sliceJSFunc(term, "function syncSessionSurface")
	if strings.Contains(sync, "sidebarOpen()") {
		t.Fatal("syncSessionSurface must not send SurfaceChrome just because the sidebar is visible")
	}
	if !strings.Contains(sync, "sidebarFocused()") || !strings.Contains(sync, `setSessionSurface("chrome")`) ||
		!strings.Contains(sync, `setSessionSurface("term")`) {
		t.Fatal("focus follows the active surface: sidebar/chrome focus → SurfaceChrome, else SurfaceTerm")
	}
	if strings.Contains(term, "sidebarOpen() || isNotebookPane") {
		t.Fatal("WS focus handshake must use sidebar focus, not sidebar visibility")
	}

	paint := sliceJSFunc(term, "function paintSidebar")
	if strings.Contains(paint, "sendSessionBytes") || strings.Contains(paint, "sendSessionJSON") || strings.Contains(paint, `type: "inject"`) {
		t.Fatal("vault sidebar chrome must not Write or Inject a live PTY")
	}
}

func TestSnippetRunCannotSilentWritePTY(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)

	if strings.Contains(term, "approved: true") {
		t.Fatal("client must not forge approved:true; #45 nonce is the only Inject grant")
	}

	run := sliceJSFunc(term, "async function paletteRunSnippet")
	if run == "" {
		t.Fatal("missing paletteRunSnippet")
	}
	approveAt := strings.Index(run, "await approveBeforeType")
	injAt := strings.Index(run, "sendApprovedInject")
	if approveAt < 0 || injAt < 0 || approveAt > injAt {
		t.Fatal("snippet run must await approveBeforeType before nonce Inject")
	}
	if !strings.Contains(run, "if (!ok) return") {
		t.Fatal("snippet run must abort when Confirm is cancelled")
	}
	if strings.Contains(run, "sendSessionBytes") || strings.Contains(run, "sendSnippet") {
		t.Fatal("paletteRunSnippet must not silent-Write or recurse through sendSnippet")
	}

	inj := sliceJSFunc(term, "function sendApprovedInject")
	if inj == "" || strings.Contains(inj, "sendSessionBytes") {
		t.Fatal("approved inject must not send live websocket binary")
	}
	if !strings.Contains(inj, `type: "inject"`) || !strings.Contains(inj, "nonce") {
		t.Fatal("approved inject must use a server nonce, not client approved:true")
	}
	if !strings.Contains(term, `type: "inject-challenge"`) {
		t.Fatal("snippet inject must request a server inject-challenge")
	}

	hit := strings.Index(term, `closest("[data-run]")`)
	if hit < 0 {
		t.Fatal("missing vault [data-run] snippet click handler")
	}
	chunk := term[hit:]
	if i := strings.Index(chunk, "const editSn"); i > 0 {
		chunk = chunk[:i]
	}
	if strings.Contains(chunk, "sendSessionBytes") {
		t.Fatal("[data-run] must not silent-Write")
	}
	if !strings.Contains(chunk, "paletteRunSnippet") {
		t.Fatal("[data-run] must go through paletteRunSnippet Confirm → nonce Inject")
	}

	rail := strings.Index(term, `closest("[data-snippet-run]")`)
	if rail < 0 {
		t.Fatal("missing sidebar [data-snippet-run] handler")
	}
	railChunk := term[rail:]
	if i := strings.Index(railChunk, "});"); i > 0 {
		railChunk = railChunk[:i]
	}
	if strings.Contains(railChunk, "sendSessionBytes") || strings.Contains(railChunk, "sendSnippet") {
		t.Fatal("sidebar snippet Run must not silent-Write")
	}
	if !strings.Contains(railChunk, "paletteRunSnippet") {
		t.Fatal("sidebar snippet Run must go through paletteRunSnippet")
	}
}

func sliceJSFunc(src, sig string) string {
	start := strings.Index(src, sig)
	if start < 0 {
		return ""
	}
	rest := src[start+1:]
	nextFn := strings.Index(rest, "\n  function ")
	nextAsync := strings.Index(rest, "\n  async function ")
	end := len(src)
	if nextFn >= 0 {
		end = start + 1 + nextFn
	}
	if nextAsync >= 0 && start+1+nextAsync < end {
		end = start + 1 + nextAsync
	}
	return src[start:end]
}

func TestNotebookCannotSilentWritePTY(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)
	cssb, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "css", "keel.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssb)

	if !strings.Contains(css, ".notebook-pane") || !strings.Contains(term, `kind: "notebook"`) {
		t.Fatal("notebook panes must exist in the split layout")
	}
	if !strings.Contains(term, `title: "Split notebook"`) || !strings.Contains(term, `run: () => splitNotebook("h")`) {
		t.Fatal("command palette must offer Split notebook")
	}

	blocked := sliceJSFunc(term, "function ptyWritesBlocked")
	if blocked == "" {
		t.Fatal("missing ptyWritesBlocked")
	}
	if !strings.Contains(blocked, "sidebarFocused() ||") ||
		strings.Contains(blocked, "sidebarOpen()") ||
		!strings.Contains(blocked, "state.openModal") ||
		!strings.Contains(blocked, "ptyInjectWaiter") ||
		!strings.Contains(blocked, "Date.now() < ptyChromeHoldUntil") ||
		!strings.Contains(blocked, "isNotebookPane(focusedPane())") ||
		!strings.Contains(blocked, "termInputOpen()") {
		t.Fatal("ptyWritesBlocked must OR notebook focus and CLI input buffer with palette/sidebar-focus/inject chrome holds")
	}

	create := sliceJSFunc(term, "function createNotebookPane")
	split := sliceJSFunc(term, "function splitNotebook")
	if create == "" || split == "" {
		t.Fatal("missing createNotebookPane or splitNotebook")
	}
	for _, fn := range []string{create, split} {
		if strings.Contains(fn, "spawnHost") || strings.Contains(fn, "connectSessionWS") || strings.Contains(fn, "createTerminal") {
			t.Fatal("splitting a notebook must not spawn a PTY or attach xterm")
		}
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") || strings.Contains(fn, `type: "inject"`) {
			t.Fatal("creating a notebook must not write the shell")
		}
	}

	if strings.Contains(term, "function runNotebookCell") || strings.Contains(term, "Type into terminal") {
		t.Fatal("notebook cells must insert into CLI input, not type into the PTY immediately")
	}
	if strings.Contains(term, "data-notebook-add") || strings.Contains(term, "notebook-cell-body") || strings.Contains(term, "Add cell") {
		t.Fatal("notebook pane must be a markdown document, not cell-run chrome")
	}
	if !strings.Contains(term, "function mountNotebookEditor") || !strings.Contains(term, "KeelMarkdownEditor") {
		t.Fatal("notebook pane must mount a rich markdown editor")
	}
	insert := sliceJSFunc(term, "function insertNotebookIntoInput")
	show := sliceJSFunc(term, "function showTermInput")
	if insert == "" || show == "" {
		t.Fatal("missing insert-into-input helpers")
	}
	for _, fn := range []string{insert, show} {
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") || strings.Contains(fn, `type: "inject"`) || strings.Contains(fn, "sendApprovedInject") || strings.Contains(fn, "approveBeforeType") {
			t.Fatal("insert-into-input must not write the PTY or Inject")
		}
	}

	confirm := sliceJSFunc(term, "function confirmTermInput")
	if confirm == "" {
		t.Fatal("missing confirmTermInput")
	}
	approveAt := strings.Index(confirm, "await approveBeforeType")
	sendAt := strings.Index(confirm, "sendApprovedInject")
	if approveAt < 0 || sendAt < 0 || approveAt > sendAt {
		t.Fatal("confirm path must await approveBeforeType before Inject")
	}
	if !strings.Contains(confirm, "if (!ok) return") {
		t.Fatal("confirm path must abort when approve-before-type is cancelled")
	}
	if strings.Contains(confirm, "sendSessionBytes") || strings.Contains(confirm, "sendSnippet") {
		t.Fatal("confirm path must not call sendSnippet or raw session bytes")
	}

	bytesFn := sliceJSFunc(term, "function sendSessionBytes")
	if bytesFn == "" || !strings.Contains(bytesFn, "if (!isTermPane(pane) || !pane?.runtimeId) return") {
		t.Fatal("sendSessionBytes must refuse non-term panes")
	}

	inj := sliceJSFunc(term, "function sendApprovedInject")
	if inj == "" || !strings.Contains(inj, "termTarget") {
		t.Fatal("approved inject must resolve a term pane, never a notebook")
	}
	if strings.Contains(inj, "sendSessionBytes") {
		t.Fatal("approved inject must not send live websocket binary")
	}
	if !strings.Contains(inj, `type: "inject"`) || !strings.Contains(inj, "nonce") || strings.Contains(inj, "approved: true") {
		t.Fatal("approved inject must use a server nonce, not client approved:true")
	}

	catStart := strings.Index(term, "function paletteCatalog()")
	catEndRel := strings.Index(term[catStart:], "\n  const paletteList = createPaletteList(")
	if catStart < 0 || catEndRel < 0 {
		t.Fatal("term.js missing palette catalog")
	}
	catalog := term[catStart : catStart+catEndRel]
	if !strings.Contains(catalog, `"split-notebook"`) {
		t.Fatal("palette catalog must include Split notebook")
	}
	if strings.Contains(catalog, "sendSessionBytes") || strings.Contains(catalog, "sendSnippet") {
		t.Fatal("palette catalog must not write a PTY")
	}

	cancel := sliceJSFunc(term, "function closeTermInput")
	cancelOpen := sliceJSFunc(term, "function closeOpenTermInput")
	if cancel == "" || cancelOpen == "" {
		t.Fatal("missing CLI input cancel helpers")
	}
	for _, fn := range []string{cancel, cancelOpen} {
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") || strings.Contains(fn, `type: "inject"`) || strings.Contains(fn, "sendApprovedInject") {
			t.Fatal("cancel / close CLI input must not write the PTY")
		}
	}

	persist := sliceJSFunc(term, "function persistNotebook")
	open := sliceJSFunc(term, "function openNotebook")
	paint := sliceJSFunc(term, "function paintSidebar")
	pin := sliceJSFunc(term, "function toggleNotebookPin")
	rename := sliceJSFunc(term, "function renameNotebook")
	saveRename := sliceJSFunc(term, "async function saveNotebookRename")
	paintNb := sliceJSFunc(term, "function paintNotebook")
	mount := sliceJSFunc(term, "function mountNotebookEditor")
	runFn := sliceJSFunc(term, "function runNotebookFence")
	copyFn := sliceJSFunc(term, "function copyNotebookFence")
	for _, fn := range []string{persist, open, paint, pin, rename, saveRename, create, split, insert, show, paintNb, mount, copyFn} {
		if fn == "" {
			t.Fatal("missing notebook persist/open/pin/rename helpers")
		}
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") || strings.Contains(fn, `type: "inject"`) {
			t.Fatal("notebook persist/open/sidebar must not write the shell")
		}
	}
	if strings.Contains(persist, "output") || strings.Contains(persist, "stdout") {
		t.Fatal("notebook persist must not store cell outputs")
	}
	if !strings.Contains(term, "Run in terminal") || !strings.Contains(css, ".term-input") {
		t.Fatal("fenced blocks must offer Run in terminal into a CLI input buffer beside the terminal")
	}
	if !strings.Contains(term, "function runNotebookFence") || !strings.Contains(term, "function copyNotebookFence") {
		t.Fatal("fenced blocks must offer Copy and Run in terminal")
	}
	if runFn == "" || copyFn == "" {
		t.Fatal("missing run/copy fence helpers")
	}
	if strings.Contains(runFn, "sendSessionBytes") || strings.Contains(runFn, `type: "inject"`) || strings.Contains(runFn, "sendApprovedInject") {
		t.Fatal("Run in terminal must not Write or Inject itself; Confirm owns the nonce path")
	}
	if !strings.Contains(runFn, "insertNotebookIntoInput") || !strings.Contains(runFn, "confirmTermInput") {
		t.Fatal("Run in terminal must fill CLI input then Confirm (approve + nonce)")
	}
	if strings.Contains(copyFn, "sendSessionBytes") || strings.Contains(copyFn, "sendApprovedInject") || strings.Contains(copyFn, `type: "inject"`) {
		t.Fatal("Copy must not Write or Inject the PTY")
	}
	if !strings.Contains(css, ".notebook-doc") || !strings.Contains(css, ".notebook-fence") || !strings.Contains(css, ".notebook-fence-lang") {
		t.Fatal("notebook pane must style a rendered markdown document, shell fences, and a language selector")
	}
	if !strings.Contains(css, "list-style-type: disc") || !strings.Contains(css, "list-style-type: decimal") {
		t.Fatal("notebook markdown lists must show bullet and numbered markers")
	}
	if strings.Contains(term, "data-notebook-pin-pane") || strings.Contains(term, "notebook-lede") || strings.Contains(term, `title="Split notebook">Note`) {
		t.Fatal("notebook pane must not show pin, description, or a Note button in the title bar")
	}
	if !strings.Contains(term, "M9.99219 11H13.4922") {
		t.Fatal("notebook tabs must use NotebookPenIcon, not the terminal glyph")
	}
	if !strings.Contains(persist, "/api/notebooks") {
		t.Fatal("notebook persist must use the vault notebooks API")
	}
	if !strings.Contains(open, "createNotebookPane") || strings.Contains(open, "spawnHost") {
		t.Fatal("opening a notebook must hydrate a notebook pane, not spawn a PTY")
	}
	if strings.Contains(open, `toast("Open a session first")`) || !strings.Contains(open, "createNotebookTab") {
		t.Fatal("opening a notebook must work with no live session")
	}
	if !strings.Contains(paint, "data-notebook-open") || !strings.Contains(paint, "data-notebook-pin") || !strings.Contains(paint, "New notebook") {
		t.Fatal("vault sidebar (Snippets segment) must pin and open notebooks")
	}
	if !strings.Contains(saveRename, `[name="id"]`) || !strings.Contains(saveRename, "/api/notebooks/") {
		t.Fatal("vault card Rename must persist the title through PUT /api/notebooks")
	}
	if !strings.Contains(paint, "data-snippet-run") || !strings.Contains(term, "data-sidebar-seg") {
		t.Fatal("vault sidebar must offer Snippets (with notebooks) as a segment")
	}
	if strings.Contains(paint, "No session") {
		t.Fatal("vault sidebar must not show session info")
	}
	if strings.Contains(paint, "inject") {
		t.Fatal("vault sidebar notebook chrome must not mention inject")
	}

	ser := sliceJSFunc(term, "function savedLeaf")
	prune := readWebLib(t, "lib/layout.js")
	prune = prune[strings.Index(prune, "export function pruneLayout"):]
	restore := sliceJSFunc(term, "function restoreSavedTab")
	if !strings.Contains(ser, `kind: "notebook"`) || !strings.Contains(ser, "notebookId") {
		t.Fatal("workspace layout must persist notebook leaves")
	}
	if !strings.Contains(prune, `kind === "notebook"`) {
		t.Fatal("workspace restore must keep notebook leaves")
	}
	if !strings.Contains(restore, "createNotebookPane") || strings.Contains(restore, "sendSessionBytes") {
		t.Fatal("workspace restore must reopen notebooks without writing the PTY")
	}
	if !strings.Contains(restore, "createNotebookTab") {
		t.Fatal("workspace restore must reopen notebook-only tabs without a live session")
	}
	if !strings.Contains(term, `api("/api/notebooks")`) {
		t.Fatal("boot must load notebooks with the rest of the vault")
	}
	if !strings.Contains(catalog, "Open notebook") || !strings.Contains(catalog, "openNotebook") {
		t.Fatal("palette catalog must offer Open notebook without writing a PTY")
	}
}

// TestNotebookCannotSilentlyWritePTY is the Warp-path gate expected as a new +func in the #46 diff:
// insert into CLI input never Write/Injects; Confirm is approve + #45 nonce Inject only;
// Cancel / client-forged approved:true never hits the PTY.
func TestNotebookCannotSilentlyWritePTY(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)

	if strings.Contains(term, "function runNotebookCell") || strings.Contains(term, "Type into terminal") {
		t.Fatal("notebook cells must insert into CLI input, not type into the PTY immediately")
	}
	if strings.Contains(term, "data-notebook-add") || strings.Contains(term, "notebook-cell-body") {
		t.Fatal("notebook pane must be a markdown document, not cell-run chrome")
	}
	if strings.Contains(term, "approved: true") {
		t.Fatal("client must not send approved:true; #45 nonce is the only Inject grant")
	}

	insert := sliceJSFunc(term, "function insertNotebookIntoInput")
	show := sliceJSFunc(term, "function showTermInput")
	cancel := sliceJSFunc(term, "function closeTermInput")
	cancelOpen := sliceJSFunc(term, "function closeOpenTermInput")
	confirm := sliceJSFunc(term, "function confirmTermInput")
	blocked := sliceJSFunc(term, "function ptyWritesBlocked")
	inj := sliceJSFunc(term, "function sendApprovedInject")
	runFn := sliceJSFunc(term, "function runNotebookFence")
	copyFn := sliceJSFunc(term, "function copyNotebookFence")
	if insert == "" || show == "" || cancel == "" || cancelOpen == "" || confirm == "" || blocked == "" || inj == "" || runFn == "" || copyFn == "" {
		t.Fatal("missing Warp insert/confirm/cancel or Inject helpers")
	}
	for _, fn := range []string{insert, show, cancel, cancelOpen, copyFn} {
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") ||
			strings.Contains(fn, "sendApprovedInject") || strings.Contains(fn, `type: "inject"`) ||
			strings.Contains(fn, "approveBeforeType") {
			t.Fatal("insert into input / cancel must not Write or Inject the PTY")
		}
	}
	if strings.Contains(runFn, "sendSessionBytes") || strings.Contains(runFn, `type: "inject"`) || strings.Contains(runFn, "sendApprovedInject") {
		t.Fatal("Run in terminal must not Write or Inject itself")
	}
	if !strings.Contains(runFn, "confirmTermInput") {
		t.Fatal("Run in terminal must go through Confirm")
	}

	approveAt := strings.Index(confirm, "await approveBeforeType")
	sendAt := strings.Index(confirm, "sendApprovedInject")
	if approveAt < 0 || sendAt < 0 || approveAt > sendAt {
		t.Fatal("confirmTermInput must await approveBeforeType before nonce Inject")
	}
	if !strings.Contains(confirm, "if (!ok) return") {
		t.Fatal("confirmTermInput must abort when approve-before-type is cancelled")
	}
	if strings.Contains(confirm, "sendSessionBytes") || strings.Contains(confirm, "sendSnippet") {
		t.Fatal("confirmTermInput must not bypass nonce Inject")
	}
	if !strings.Contains(inj, `type: "inject"`) || !strings.Contains(inj, "nonce") || strings.Contains(inj, "approved: true") {
		t.Fatal("approved inject must use a server nonce, not client approved:true")
	}
	if strings.Contains(inj, "sendSessionBytes") {
		t.Fatal("approved inject must not send live websocket binary")
	}
	if !strings.Contains(blocked, "termInputOpen()") || !strings.Contains(blocked, "isNotebookPane(focusedPane())") {
		t.Fatal("ptyWritesBlocked must cover CLI input + notebook focus")
	}

	cancelClick := strings.Index(term, `closest("[data-term-input-cancel]")`)
	if cancelClick < 0 {
		t.Fatal("missing Cancel click handler for CLI input")
	}
	cancelSrc := term[cancelClick:]
	if i := strings.Index(cancelSrc, "return;"); i >= 0 {
		cancelSrc = cancelSrc[:i]
	}
	if !strings.Contains(cancelSrc, "closeTermInput") || strings.Contains(cancelSrc, "confirmTermInput") ||
		strings.Contains(cancelSrc, "sendApprovedInject") || strings.Contains(cancelSrc, `type: "inject"`) {
		t.Fatal("Cancel must close the CLI input without Inject")
	}

	assertInjectNonceOneShotReplay(t)
}

func TestNotebookOpensWithoutSession(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)
	open := sliceJSFunc(term, "function openNotebook")
	createTab := sliceJSFunc(term, "function createNotebookTab")
	createPane := sliceJSFunc(term, "function createNotebookPane")
	split := sliceJSFunc(term, "function splitNotebook")
	fresh := sliceJSFunc(term, "function newNotebook")
	insert := sliceJSFunc(term, "function insertNotebookIntoInput")
	rename := sliceJSFunc(term, "function renameNotebook")
	saveRename := sliceJSFunc(term, "async function saveNotebookRename")
	if open == "" || createTab == "" || createPane == "" || split == "" || fresh == "" || insert == "" || rename == "" || saveRename == "" {
		t.Fatal("missing notebook open/create helpers")
	}
	for _, fn := range []string{open, createTab, createPane, split, fresh, rename, saveRename} {
		if strings.Contains(fn, `toast("Open a session first")`) {
			t.Fatal("notebook open/new must not require an active session")
		}
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") ||
			strings.Contains(fn, "sendApprovedInject") || strings.Contains(fn, `type: "inject"`) {
			t.Fatal("opening or creating a notebook must not Write or Inject the PTY")
		}
		if strings.Contains(fn, "spawnHost") {
			t.Fatal("opening a notebook must not spawn a session")
		}
	}
	if !strings.Contains(open, "createNotebookTab") || !strings.Contains(fresh, "createNotebookTab") {
		t.Fatal("vault/open with no session must create a standalone notebook tab")
	}
	if !strings.Contains(insert, "termTarget") || !strings.Contains(insert, "Split a terminal beside this notebook first") {
		t.Fatal("Insert must refuse when no terminal target exists")
	}
	if !strings.Contains(term, "function mountNotebookEditor") || !strings.Contains(term, "onRunFence") ||
		!strings.Contains(term, "function runNotebookFence") {
		t.Fatal("shell send must be an explicit fence affordance on the markdown document")
	}

	srv, _, _, _ := testServer(t)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `id="page-notebooks"`) || !strings.Contains(term, `data-open-page="notebooks"`) ||
		!strings.Contains(string(body), `id="notebook-list"`) || !strings.Contains(string(body), `id="btn-new-notebook"`) {
		t.Fatal("notebooks must be a tab page reachable from the vault sidebar")
	}
	if !strings.Contains(string(body), "/vendor/notebook/editor.js") {
		t.Fatal("index must load the notebook markdown editor")
	}
	edBody, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "vendor", "notebook", "editor.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(edBody), "KeelMarkdownEditor") || !strings.Contains(string(edBody), "notebook-fence") {
		t.Fatal("notebook markdown editor must be vendored")
	}
	if !strings.Contains(string(body), `<h1 class="k-page-title">Notebooks</h1>`) {
		t.Fatal("the Notebooks tab page has a page head like the other tab pages")
	}

	if !strings.Contains(string(body), `id="notebook-list"`) {
		t.Fatal("the page renders the notebook list")
	}

	created, err := http.Post(srv.URL+"/api/notebooks", "application/json", strings.NewReader(`{"name":"ops","cells":[{"body":"ls"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(created.Body)
		t.Fatalf("create %d %s", created.StatusCode, b)
	}
	// The list is templ, on the page and pushed over /ui/stream.
	listBody := listSection(t, srv.URL, "notebook-list")
	if !strings.Contains(string(listBody), `data-notebook-open`) || !strings.Contains(string(listBody), "ops") {
		t.Fatal("vault notebook cards must be openable without a session")
	}
	if !strings.Contains(string(listBody), `data-notebook-rename`) || !strings.Contains(string(listBody), "Rename") {
		t.Fatal("vault notebook cards must offer Rename")
	}
}

func TestNotebookAPI(t *testing.T) {
	srv, _, _, _ := testServer(t)
	res, err := http.Post(srv.URL+"/api/notebooks", "application/json", strings.NewReader(`{"name":"ops","cells":[{"body":"# hello\nls"}],"pinned":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create %d %s", res.StatusCode, b)
	}
	var saved store.Notebook
	if err := json.NewDecoder(res.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.Name != "ops" || !saved.Pinned || len(saved.Cells) != 1 || saved.Cells[0].Body != "# hello\nls" {
		t.Fatalf("created = %+v", saved)
	}
	if saved.Cells[0].ID == "" {
		t.Fatal("cell id must be minted")
	}

	listRes, err := http.Get(srv.URL + "/api/notebooks")
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("list %d", listRes.StatusCode)
	}
	var listed []store.Notebook
	if err := json.NewDecoder(listRes.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != saved.ID || !listed[0].Pinned {
		t.Fatalf("list = %+v", listed)
	}

	saved.Name = "runbook"
	saved.Pinned = false
	saved.Cells = append(saved.Cells, store.NotebookCell{Body: "uptime"})
	body, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/notebooks/"+saved.ID, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	putRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer putRes.Body.Close()
	if putRes.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(putRes.Body)
		t.Fatalf("update %d %s", putRes.StatusCode, b)
	}
	var updated store.Notebook
	if err := json.NewDecoder(putRes.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "runbook" || updated.Pinned || len(updated.Cells) != 2 {
		t.Fatalf("updated = %+v", updated)
	}

	delReq, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/notebooks/"+saved.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	delRes, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	defer delRes.Body.Close()
	if delRes.StatusCode != http.StatusNoContent {
		t.Fatalf("delete %d", delRes.StatusCode)
	}

	gone, err := http.Get(srv.URL + "/api/notebooks")
	if err != nil {
		t.Fatal(err)
	}
	defer gone.Body.Close()
	var empty []store.Notebook
	if err := json.NewDecoder(gone.Body).Decode(&empty); err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty list, got %+v", empty)
	}

	bad, err := http.Post(srv.URL+"/api/notebooks", "application/json", strings.NewReader(`{`))
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid payload %d", bad.StatusCode)
	}

	leak, err := http.Post(srv.URL+"/api/notebooks", "application/json", strings.NewReader(`{"name":"leak","cells":[{"body":"ls","output":"PTY_OUTPUT_LEAK","stdout":"PTY_OUTPUT_LEAK"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer leak.Body.Close()
	if leak.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(leak.Body)
		t.Fatalf("leak create %d %s", leak.StatusCode, b)
	}
	rawLeak, err := io.ReadAll(leak.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawLeak), "PTY_OUTPUT_LEAK") {
		t.Fatal("notebook API must not persist cell outputs")
	}
	var leaked map[string]any
	if err := json.Unmarshal(rawLeak, &leaked); err != nil {
		t.Fatal(err)
	}
	cells, _ := leaked["cells"].([]any)
	if len(cells) != 1 {
		t.Fatalf("leak cells = %#v", leaked["cells"])
	}
	cell, _ := cells[0].(map[string]any)
	if _, ok := cell["output"]; ok {
		t.Fatal("cell JSON must omit output")
	}
	if _, ok := cell["stdout"]; ok {
		t.Fatal("cell JSON must omit stdout")
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

func TestReadScrollbackAPIRedactsByDefault(t *testing.T) {
	srv, rt, st, fb := testServer(t)
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

	alice := dialSession(t, srv.URL, info.ID, "alice", "web")
	defer alice.Close()
	_, _ = waitReadySnapshot(t, alice)

	const secret = "super-secret-token"
	payload := "ok password=" + secret + " done\n"
	if _, err := fb.outW.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if got := waitBinary(t, alice, payload); !strings.Contains(got, secret) {
		t.Fatalf("live websocket must stay raw, got %q", got)
	}

	res, err := http.Get(srv.URL + "/api/sessions/" + info.ID + "/scrollback?raw=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("scrollback %d %s", res.StatusCode, b)
	}
	var body struct {
		ID       string `json:"id"`
		Redacted bool   `json:"redacted"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != info.ID {
		t.Fatalf("id = %q", body.ID)
	}
	if !body.Redacted {
		t.Fatal("read API must declare redacted=true")
	}
	if strings.Contains(body.Text, secret) {
		t.Fatalf("read API leaked secret: %q", body.Text)
	}
	if !strings.Contains(body.Text, "[redacted]") {
		t.Fatalf("read API missing mask: %q", body.Text)
	}
	if !strings.Contains(body.Text, "ok password=") || !strings.Contains(body.Text, "done") {
		t.Fatalf("innocent text missing: %q", body.Text)
	}

	bob := dialSession(t, srv.URL, info.ID, "bob", "web")
	defer bob.Close()
	_, snap := waitReadySnapshot(t, bob)
	if !strings.Contains(snap, secret) {
		t.Fatal("Subscribe snapshot must stay raw for the local UI")
	}
}

func TestReadScrollbackAPINotFound(t *testing.T) {
	srv, _, _, _ := testServer(t)
	res, err := http.Get(srv.URL + "/api/sessions/missing/scrollback")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
}

func TestSessionWSReattachAfterDisconnect(t *testing.T) {
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

	alice := dialSession(t, srv.URL, info.ID, "alice", "web")
	waitJSON(t, alice, "ready")
	const marker = "ws-pre-flap-scrollback-marker"
	mu.Lock()
	if len(bridges) < 1 {
		mu.Unlock()
		t.Fatal("no bridge")
	}
	if _, err := bridges[0].outW.Write([]byte(marker)); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	mu.Unlock()
	if got := waitBinary(t, alice, marker); !strings.Contains(got, marker) {
		t.Fatalf("alice bytes %q", got)
	}
	_ = alice.Close()

	res, err := http.Post(srv.URL+"/api/sessions/"+info.ID+"/reconnect", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reconnect %d", res.StatusCode)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := rt.Get(info.ID)
		if err == nil && got.Info().Status == runtime.StatusReady {
			mu.Lock()
			n := len(bridges)
			mu.Unlock()
			if n >= 2 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotRead := string(sess.ReadScrollback())
	if len(gotRead) == 0 || !strings.Contains(gotRead, marker) {
		t.Fatalf("ReadScrollback empty/missing after reconnect: %q", gotRead)
	}

	bob := dialSession(t, srv.URL, info.ID, "bob", "web")
	defer bob.Close()
	_, snap := waitReadySnapshot(t, bob)
	if len(snap) == 0 {
		t.Fatal("websocket snapshot empty after reconnect")
	}
	if !strings.Contains(snap, marker) {
		t.Fatalf("reattach snapshot lost scrollback: %q", snap)
	}
	mu.Lock()
	if len(bridges) < 2 {
		mu.Unlock()
		t.Fatal("expected a new bridge after reconnect")
	}
	fb := bridges[len(bridges)-1]
	mu.Unlock()
	if _, err := fb.outW.Write([]byte("reattached")); err != nil {
		t.Fatal(err)
	}
	if got := waitBinary(t, bob, "reattached"); !strings.Contains(got, "reattached") {
		t.Fatalf("reattach bytes %q", got)
	}
}

func TestSessionWSClosesWhenClientDropped(t *testing.T) {
	srv, rt, st, fb := testServer(t)
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
	conn := dialSession(t, srv.URL, info.ID, "alice", "web")
	waitJSON(t, conn, "ready")
	payload := bytes.Repeat([]byte("x"), 32*1024)
	for i := 0; i < 600; i++ {
		if _, err := fb.outW.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	closed := false
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("slow consumer should drop and close the websocket")
	}

	again := dialSession(t, srv.URL, info.ID, "bob", "web")
	defer again.Close()
	waitJSON(t, again, "ready")
	if _, err := fb.outW.Write([]byte("after-drop")); err != nil {
		t.Fatal(err)
	}
	if got := waitBinary(t, again, "after-drop"); !strings.Contains(got, "after-drop") {
		t.Fatalf("got %q", got)
	}
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
	// A key can be trusted only from a connect's refusal (its offer).
	const boxFP = "SHA256:q3VtXz0b6kP1d8yRk2mJw4sTn7aEc9hLf5uGi3o9fKe"
	var unk *store.UnknownHostError
	if err := st.CheckHostKeyType("box:22", "ssh-ed25519", boxFP); !errors.As(err, &unk) {
		t.Fatal(err)
	}
	body2 := `{"token":"` + unk.Token + `","hostname":"box:22","fingerprint":"` + boxFP + `"}`
	resKH, err := http.Post(srv2.URL+"/api/known-hosts", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resKH.Body.Close()
	if resKH.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resKH.Body)
		t.Fatalf("known-hosts %d %s", resKH.StatusCode, b)
	}
	if err := st.CheckHostKey("box:22", boxFP); err != nil {
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

func TestDuplicateHostAPI(t *testing.T) {
	srv, _, st, _ := testServer(t)
	body := `{"name":"api","group":"prod","address":"10.0.0.8","port":22,"username":"ubuntu","authType":"password","password":"secret","color":"#6ea8ff"}`
	res, err := http.Post(srv.URL+"/api/hosts", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create %d %s", res.StatusCode, b)
	}
	var src store.Host
	if err := json.NewDecoder(res.Body).Decode(&src); err != nil {
		t.Fatal(err)
	}

	dup, err := http.Post(srv.URL+"/api/hosts/"+src.ID+"/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dup.Body.Close()
	if dup.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(dup.Body)
		t.Fatalf("duplicate %d %s", dup.StatusCode, b)
	}
	var copy store.Host
	if err := json.NewDecoder(dup.Body).Decode(&copy); err != nil {
		t.Fatal(err)
	}
	if copy.ID == src.ID || copy.Name != "api (copy)" {
		t.Fatalf("copy = %+v src = %+v", copy, src)
	}
	if copy.Address != "10.0.0.8" || copy.Username != "ubuntu" || copy.Group != "prod" {
		t.Fatalf("settings not copied: %+v", copy)
	}
	if copy.Password != "__saved__" {
		t.Fatalf("api must redact copied secret, got %q", copy.Password)
	}
	got, err := st.GetHost(copy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "secret" {
		t.Fatal("vault should persist the copied password")
	}

	again, err := http.Post(srv.URL+"/api/hosts/"+src.ID+"/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	var copy2 store.Host
	if err := json.NewDecoder(again.Body).Decode(&copy2); err != nil {
		t.Fatal(err)
	}
	if copy2.Name != "api (copy 2)" {
		t.Fatalf("second copy name = %q", copy2.Name)
	}

	local := firstHost(t, st)
	bad, err := http.Post(srv.URL+"/api/hosts/"+local.ID+"/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("local duplicate status = %d", bad.StatusCode)
	}

	missing, err := http.Post(srv.URL+"/api/hosts/nope/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing duplicate status = %d", missing.StatusCode)
	}
}

func TestHostListDuplicateAction(t *testing.T) {
	web := os.DirFS(filepath.Join("..", "..", "cmd", "keel", "web"))
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertHost(store.Host{
		Name: "api", Address: "10.0.0.8", Username: "ubuntu", AuthType: "password", Password: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	srv := httptest.NewServer(New(st, rt, web, nil).Handler())
	t.Cleanup(srv.Close)

	frag, err := http.Get(srv.URL + "/ui/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer frag.Body.Close()
	fragBody, _ := io.ReadAll(frag.Body)
	if frag.StatusCode != http.StatusOK {
		t.Fatalf("ui hosts %d", frag.StatusCode)
	}
	if !strings.Contains(string(fragBody), `data-duplicate="`) {
		t.Fatal("host cards should offer Duplicate")
	}

	js, err := http.Get(srv.URL + "/js/term.js")
	if err != nil {
		t.Fatal(err)
	}
	defer js.Body.Close()
	jsBody, _ := io.ReadAll(js.Body)
	term := string(jsBody)
	if !strings.Contains(term, "/duplicate") {
		t.Fatal("term.js must duplicate hosts through the vault API")
	}
	if strings.Contains(term, "ptyWritesBlocked") && !strings.Contains(term, "sidebarFocused() || state.openModal || ptyInjectWaiter") {
		t.Fatal("duplicate-host must not replace ptyWritesBlocked")
	}
}

func TestNestedGroupsAPIAndVaultTree(t *testing.T) {
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
	srv := httptest.NewServer(New(st, rt, web, nil).Handler())
	t.Cleanup(srv.Close)

	create := func(body string) store.Group {
		t.Helper()
		res, err := http.Post(srv.URL+"/api/groups", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(res.Body)
			t.Fatalf("create group %d %s", res.StatusCode, b)
		}
		var g store.Group
		if err := json.NewDecoder(res.Body).Decode(&g); err != nil {
			t.Fatal(err)
		}
		return g
	}

	root := create(`{"name":"work"}`)
	child := create(`{"name":"prod","parentId":"` + root.ID + `"}`)
	if child.ParentID != root.ID {
		t.Fatalf("child parent = %q", child.ParentID)
	}

	cycle, err := http.NewRequest(http.MethodPut, srv.URL+"/api/groups/"+root.ID, strings.NewReader(`{"name":"work","parentId":"`+child.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	cycle.Header.Set("Content-Type", "application/json")
	cres, err := http.DefaultClient.Do(cycle)
	if err != nil {
		t.Fatal(err)
	}
	defer cres.Body.Close()
	if cres.StatusCode != http.StatusBadRequest {
		t.Fatalf("cycle status = %d", cres.StatusCode)
	}

	hostRes, err := http.Post(srv.URL+"/api/hosts", "application/json", strings.NewReader(`{"name":"api","groupId":"`+child.ID+`","address":"10.0.0.8","username":"ubuntu","authType":"password","password":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer hostRes.Body.Close()
	if hostRes.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(hostRes.Body)
		t.Fatalf("host %d %s", hostRes.StatusCode, b)
	}
	var host store.Host
	if err := json.NewDecoder(hostRes.Body).Decode(&host); err != nil {
		t.Fatal(err)
	}
	if host.Group != "prod" || host.GroupID != child.ID {
		t.Fatalf("host group view = %+v", host)
	}

	listRes, err := http.Get(srv.URL + "/api/groups")
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	var groups []store.Group
	if err := json.NewDecoder(listRes.Body).Decode(&groups); err != nil {
		t.Fatal(err)
	}
	foundChild := false
	for _, g := range groups {
		if g.ID == child.ID && g.ParentID == root.ID {
			foundChild = true
		}
	}
	if !foundChild {
		t.Fatalf("nested child missing from list: %+v", groups)
	}

	frag, err := http.Get(srv.URL + "/ui/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer frag.Body.Close()
	body, _ := io.ReadAll(frag.Body)
	html := string(body)
	if !strings.Contains(html, `data-group-id="`+root.ID+`"`) || !strings.Contains(html, `data-group-id="`+child.ID+`"`) {
		t.Fatalf("vault tree missing nested group ids: %s", html)
	}
	if !strings.Contains(html, `data-group-toggle`) || !strings.Contains(html, `data-group-new="`+child.ID+`"`) ||
		!strings.Contains(html, `data-group-rename="`+child.ID+`"`) || !strings.Contains(html, `data-drag-group`) {
		t.Fatal("vault tree missing expand/create/rename/drag hooks")
	}
	if !strings.Contains(html, `data-connect="`+host.ID+`"`) {
		t.Fatal("a nested host must show in its group in the sidebar tree")
	}
	childAt := strings.Index(html, `data-group-id="`+child.ID+`"`)
	hostAt := strings.Index(html, `data-connect="`+host.ID+`"`)
	if childAt < 0 || hostAt < childAt {
		t.Fatal("the sidebar tree must nest a host inside its group")
	}

	other, err := http.Post(srv.URL+"/api/hosts", "application/json", strings.NewReader(`{"name":"db","address":"10.0.0.9","username":"ubuntu","authType":"password","password":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Body.Close()
	var db store.Host
	if err := json.NewDecoder(other.Body).Decode(&db); err != nil {
		t.Fatal(err)
	}
	move, err := http.Post(srv.URL+"/api/hosts/move", "application/json", strings.NewReader(`{"ids":["`+host.ID+`","`+db.ID+`"],"groupId":"`+root.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer move.Body.Close()
	if move.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(move.Body)
		t.Fatalf("move %d %s", move.StatusCode, b)
	}

	js, err := http.Get(srv.URL + "/js/term.js")
	if err != nil {
		t.Fatal(err)
	}
	defer js.Body.Close()
	jsBody, _ := io.ReadAll(js.Body)
	term := string(jsBody)
	for _, want := range []string{
		"const MAX_GROUP_DEPTH = 8",
		"function assignHostsGroup",
		"data-group-rename",
		"data-drag-group",
		"/api/groups",
		"/api/hosts/move",
		"parentId",
		"host-parent-group",
		"kind: \"hosts\"",
	} {
		if !strings.Contains(term, want) {
			t.Fatalf("term.js missing %q", want)
		}
	}
	for _, fn := range []string{"async function assignHostsGroup", "function afterFormSaved"} {
		i := strings.Index(term, fn)
		if i < 0 {
			t.Fatalf("missing %s", fn)
		}
		chunk := term[i:]
		if n := 900; len(chunk) > n {
			chunk = chunk[:n]
		}
		if strings.Contains(chunk, "inject") || strings.Contains(chunk, "sendSnippet") || strings.Contains(chunk, "ptyWritesBlocked") {
			t.Fatalf("%s must not touch PTY/Inject", fn)
		}
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
	logs := applog.New(t.TempDir() + "/keel.log")
	logs.Append(applog.Entry{Time: time.Now(), Kind: "info", Message: "vault ready"})
	srv := httptest.NewServer(New(st, rt, web, nil).WithLogs(logs).Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	// Phase 2 IA (docs/ui-redesign.md §4): the Vault is a collapsible left
	// sidebar on every tab, not a pinned tab; the right session rail is gone.
	if !strings.Contains(string(body), `id="vault-sidebar"`) || !strings.Contains(string(body), `data-open-page="settings"`) || !strings.Contains(string(body), "hostkey-modal") {
		t.Fatalf("index missing vault chrome: %s", string(body[:min(len(body), 400)]))
	}
	if !strings.Contains(string(body), `id="btn-sidebar"`) || !strings.Contains(string(body), `aria-label="Toggle vault sidebar"`) || !strings.Contains(string(body), `class="workspace"`) {
		t.Fatal("index missing the collapsible vault sidebar and its titlebar toggle")
	}
	if !strings.Contains(string(body), `id="vault-sidebar" aria-label="Vault" tabindex="-1"`) {
		t.Fatal("vault sidebar must be focusable so SurfaceChrome can own the keyboard")
	}
	for _, seg := range []string{"hosts", "snippets", "forwards"} {
		if !strings.Contains(string(body), `data-sidebar-seg="`+seg+`"`) || !strings.Contains(string(body), `data-sidebar-panel="`+seg+`"`) {
			t.Fatalf("vault sidebar missing the %s segment", seg)
		}
	}
	for _, page := range []string{"keychain", "logs", "settings"} {
		if !strings.Contains(string(body), `data-open-page="`+page+`"`) {
			t.Fatalf("vault sidebar footer missing %s", page)
		}
	}
	if strings.Contains(string(body), `id="session-rail"`) || strings.Contains(string(body), `data-rail-tab=`) || strings.Contains(string(body), "vault-rail") {
		t.Fatal("the right session rail and the vault tab rail are gone")
	}
	if strings.Contains(string(body), `id="btn-settings"`) {
		t.Fatal("index should have no titlebar settings button")
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
	if !strings.Contains(string(body), `<dialog class="modal modal--host" id="host-modal"`) || !strings.Contains(string(body), `id="modal-scrim"`) {
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
	if !strings.Contains(string(body), `id="host-tags"`) || !strings.Contains(string(body), `name="tags"`) {
		t.Fatal("host form missing tags field")
	}
	if !strings.Contains(string(body), `data-tui-slider`) || !strings.Contains(string(body), `k-page--settings`) {
		t.Fatal("index missing settings slider or the Settings tab page")
	}
	if !strings.Contains(string(body), "min-w-max") {
		t.Fatal("host action dropdown must size to content, not the trigger")
	}
	// The sidebar filter is the host search (Enter still connects or parses
	// user@host); its controls are k-* components sized by tokens.
	if !strings.Contains(string(body), `id="search"`) || !strings.Contains(string(body), `aria-label="Search hosts"`) {
		t.Fatal("vault sidebar missing the host filter")
	}
	for _, id := range []string{`id="btn-new-host"`, `id="btn-welcome-local"`, `id="btn-quick"`, `id="btn-join"`} {
		if !strings.Contains(string(body), id) {
			t.Fatalf("vault sidebar missing %s", id)
		}
	}
	if strings.Contains(string(body), `id="live-section"`) || strings.Contains(string(body), `id="herd-list"`) {
		t.Fatal("index should not keep a Live/herd list on Hosts")
	}
	if !strings.Contains(string(body), `data-slot="aspect-ratio"`) {
		t.Fatal("index missing the terminal preview aspect ratio")
	}
	// The vault gate (Unlock, First run) is k-* markup (internal/ui/gate.templ).
	for _, id := range []string{`id="gate"`, `id="unlock-form"`, `id="onb-vault"`, `id="onb-import"`} {
		if !strings.Contains(string(body), id) {
			t.Fatalf("index missing %s", id)
		}
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
	// Full screen comes from the native window (desktop chromeJS), not a
	// window-size guess that also fires for a screen-sized window.
	if strings.Contains(string(body), "syncFullscreen") || strings.Contains(string(body), "screen.width") {
		t.Fatal("index must not guess full screen from the window size")
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
		!strings.Contains(string(cssBody), "color-mix(in srgb, var(--ink) 12%, transparent)") {
		t.Fatal("context menu items must highlight on hover")
	}
	// The titlebar is the size-titlebar token (38px), not a 42px literal.
	if !strings.Contains(string(cssBody), "height: var(--size-titlebar)") || strings.Contains(string(cssBody), "min-height: 42px") {
		t.Fatal("keel.css titlebar overlay must use the size-titlebar token")
	}
	// Full screen: tabs go to the left edge (the traffic lights leave the
	// window) and the page moves below the menu bar + titlebar overlay
	// while macOS shows it, by the measured --titlebar-overlay.
	if !strings.Contains(string(cssBody), "html[data-chrome=\"titlebar\"][data-fullscreen] .workspace-bar {\n  padding-left: var(--space-2);") {
		t.Fatal("keel.css missing fullscreen tab shift to the left edge")
	}
	if !strings.Contains(string(cssBody), "html[data-chrome=\"titlebar\"][data-fullscreen][data-titlebar-revealed] .app {\n  top: var(--titlebar-overlay, 0);") {
		t.Fatal("keel.css must move the page below the revealed full-screen titlebar")
	}
	if !strings.Contains(string(cssBody), "transition: top var(--duration-base) ease-out;") {
		t.Fatal("keel.css overlay move must animate with the duration token")
	}
	if strings.Contains(string(cssBody), "padding-left: 8px") {
		t.Fatal("keel.css fullscreen inset must use the space token")
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
	// Tabs are the size-tab token (28px) in every chrome, with the keel line
	// (2px brass) under the selected one.
	if !strings.Contains(string(cssBody), "height: var(--size-tab)") ||
		!strings.Contains(string(cssBody), ".tab.is-active::after") ||
		!strings.Contains(string(cssBody), "background: var(--brass)") {
		t.Fatal("keel.css missing token-sized tabs or the keel line under the selected tab")
	}
	if !strings.Contains(string(cssBody), ".tab-new:focus") || !strings.Contains(string(cssBody), "html.is-window-dragging") {
		t.Fatal("new-tab must not show a focus ring; window drag must keep the default cursor")
	}
	if !strings.Contains(string(cssBody), ".tab { cursor: pointer") {
		t.Fatal("tabs must use a pointer cursor, not grab")
	}
	if !strings.Contains(string(cssBody), ".tabs::-webkit-scrollbar") ||
		!strings.Contains(string(cssBody), "scrollbar-width: none") ||
		!strings.Contains(string(cssBody), "data-overflow-left") {
		t.Fatal("keel.css missing hidden tab-strip scrollbar or overflow fades")
	}
	if !strings.Contains(string(cssBody), ".term-wrap.is-focused") ||
		!strings.Contains(string(cssBody), ".pane-drop") ||
		!strings.Contains(string(cssBody), ".pane-actions") ||
		!strings.Contains(string(cssBody), ".xterm-scrollable-element > .scrollbar > .slider") {
		t.Fatal("keel.css missing focused pane tile, drop overlay, or xterm overlay scrollbar")
	}
	if !strings.Contains(string(cssBody), ".term-body .xterm") ||
		strings.Contains(string(cssBody), "padding: 12px 14px 14px") ||
		strings.Contains(string(cssBody), "padding: 8px 10px 10px") {
		t.Fatal("split panes must not inset the terminal; the canvas should fill the pane")
	}
	if !strings.Contains(string(cssBody), ".term-host") ||
		!strings.Contains(string(cssBody), `html[data-native] .term-tab.is-split .term-host`) ||
		!strings.Contains(string(cssBody), `html[data-native] .term-tab:not(.is-split) .term-host`) ||
		!strings.Contains(string(cssBody), `.term-host {
  flex: 1 1 0%;`) {
		t.Fatal("split panes must fill with the terminal surface; solo native panes keep glass")
	}
	if !strings.Contains(string(cssBody), ".scrollbar.vertical") ||
		!strings.Contains(string(cssBody), "width: 6px !important") {
		t.Fatal("xterm overlay scrollbar should be thin, not the 14px overview ruler default")
	}
	// Single and split tabs share one pane look: no tile borders or radii,
	// 1px line gutters, and the keel line under the focused pane bar.
	if !strings.Contains(string(cssBody), ".term-tab.is-split { padding: 0;") ||
		!strings.Contains(string(cssBody), ".term-wrap.is-focused .pane-bar::after") ||
		!strings.Contains(string(cssBody), ".split-sash {\n  flex: 0 0 1px;") {
		t.Fatal("split tabs must fill the stage with 1px gutters and mark the focused pane bar with the keel line")
	}
	if strings.Contains(string(cssBody), ".snippet-bar") || strings.Contains(string(body), "active-snippets") {
		t.Fatal("session snippet footer must be removed")
	}
	if !strings.Contains(string(cssBody), "var(--chrome)") {
		t.Fatal("keel.css missing chrome token")
	}
	if !strings.Contains(string(cssBody), ".k-sidebar {") ||
		!strings.Contains(string(cssBody), `html[data-sidebar="closed"] .k-sidebar`) ||
		!strings.Contains(string(cssBody), "width: var(--size-sidebar)") {
		t.Fatal("keel.css must lay out the collapsible vault sidebar at the size-sidebar token")
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
	if !strings.Contains(string(cssBody), `.tab .tab-icon svg { display: block; width: 16px; height: 16px; }`) ||
		!strings.Contains(string(cssBody), `.tab .close svg { display: block; width: 16px; height: 16px; }`) {
		t.Fatal("keel.css tab icons should stay 16px")
	}
	if strings.Contains(string(termBody), `viewBox="0 0 16 16"`) ||
		!strings.Contains(string(termBody), `M18 6L6.00081 17.9992`) ||
		!strings.Contains(string(termBody), `M7.5 7.5L8.72654 8.55719`) ||
		!strings.Contains(string(termBody), `class="${PANE_BTN} pane-close" data-pane-close="${paneId}" title="Close pane">${closeGlyph()}`) {
		t.Fatal("session tab and pane close glyphs must use Hugeicons computer-terminal-01 and cancel-01")
	}
	if !strings.Contains(string(termBody), `ev.button !== 2`) || !strings.Contains(string(termBody), `getElementById("tab-ctx")`) {
		t.Fatal("term.js must synthesize tab contextmenu when the titlebar swallows right-click")
	}
	// The xterm palette is read from the --term-* and --ansi-* tokens; the
	// native window glasses the background with an alpha on --term-bg.
	if !strings.Contains(string(termBody), "allowTransparency") || !strings.Contains(string(termBody), "NATIVE_TERM_ALPHA") ||
		!strings.Contains(string(termBody), `get("--term-bg")`) || !strings.Contains(string(termBody), "`--ansi-${i}`") ||
		strings.Contains(string(termBody), "#0a0a0a") || strings.Contains(string(termBody), "#fbfbfa") {
		t.Fatal("term.js must build the xterm theme from the Keel tokens and glass the native background per theme")
	}
	if !strings.Contains(string(termBody), "connectFromSearch") {
		t.Fatal("term.js must connect from the sidebar host filter")
	}
	// No tag chips in the sidebar tree: the filter matches tags (and group
	// names) instead. The host dialog's tags are split server-side
	// (formTags, TestUIFormHost).
	if !strings.Contains(readWebLib(t, "lib/groups.js"), "h.tags.join") {
		t.Fatal("the host filter must match host tags")
	}
	if !strings.Contains(string(termBody), "dockTabIntoPane") || !strings.Contains(string(termBody), "togglePaneZoom") ||
		!strings.Contains(string(termBody), "movePaneTo") || !strings.Contains(string(termBody), "data-pane-split") {
		t.Fatal("term.js missing pane rearrange, focus view, or tile split/sync actions")
	}
	if !strings.Contains(string(termBody), "function closeActivePaneOrTab") ||
		!strings.Contains(string(termBody), "closeActivePaneOrTab()") {
		t.Fatal("term.js must close the focused pane with ⌘W before closing the last session tab")
	}
	if !strings.Contains(string(termBody), `className = "term-host"`) ||
		!strings.Contains(string(termBody), "ensurePaneTerm") ||
		!strings.Contains(string(termBody), ".open(pane.host)") ||
		!strings.Contains(string(termBody), "overviewRuler: { width: 0 }") {
		t.Fatal("term.js must disable the xterm overview ruler strip beside the scrollbar")
	}
	if !strings.Contains(string(termBody), "attachWebgl") || !strings.Contains(string(body), "addon-webgl.js") {
		t.Fatal("term.js must use the xterm webgl renderer so block glyphs fill the cell")
	}
	if !strings.Contains(string(termBody), "function paneWantsWebgl") ||
		!strings.Contains(string(termBody), "visiblePanes(tab).length <= 1") {
		t.Fatal("native WKWebView must keep WebGL on a solo pane and drop it while split")
	}
	if !strings.Contains(string(termBody), "for (const pane of tabPanes(tab)) dropWebgl(pane)") {
		t.Fatal("paintLayout must dispose WebGL on native splits so dead GPU canvases cannot eat clicks")
	}
	if !strings.Contains(string(termBody), "ensureTabStage") ||
		!strings.Contains(string(termBody), "layoutTabPanes") ||
		!strings.Contains(string(termBody), `className = "term-stage"`) {
		t.Fatal("split layout must position pane wraps in a stable stage so WebGL canvases are not remounted")
	}
	if strings.Contains(string(termBody), "function attachWebgl(term) {\n    if (isNativeWindow()) return;") {
		t.Fatal("native window must still load WebGL so block glyphs fill the cell")
	}
	writeFn := sliceJSFunc(string(termBody), "function writePaneBytes")
	invFn := sliceJSFunc(string(termBody), "function invalidatePaneRender")
	schedFn := sliceJSFunc(string(termBody), "function schedulePaneInvalidate")
	refreshFn := sliceJSFunc(string(termBody), "function refreshPaneRender")
	if writeFn == "" || invFn == "" || schedFn == "" || refreshFn == "" {
		t.Fatal("PTY write must share the native Relayout/Invalidate kick used on resize")
	}
	if !strings.Contains(writeFn, "schedulePaneInvalidate") {
		t.Fatal("writePaneBytes must schedule the same native invalidate as window resize")
	}
	if strings.Contains(writeFn, "sendResize") || strings.Contains(invFn, "sendResize") || strings.Contains(schedFn, "sendResize") {
		t.Fatal("write/invalidate must not sendResize; Relayout is local to the canvas")
	}
	if strings.Contains(invFn, "cols - 1") || strings.Contains(invFn, "term.resize") {
		t.Fatal("invalidatePaneRender must not dummy-resize; cols±1 blinks the frame on every type")
	}
	if !strings.Contains(invFn, "pane.term.refresh") || !strings.Contains(invFn, "kickNativeCanvasCompositor") {
		t.Fatal("write invalidate must refresh then kick the WKWebView canvas compositor")
	}
	kickFn := sliceJSFunc(string(termBody), "function kickNativeCanvasCompositor")
	if kickFn == "" || strings.Contains(kickFn, "term.resize") || strings.Contains(kickFn, "sendResize") {
		t.Fatal("canvas compositor kick must not resize the PTY or xterm grid")
	}
	if !strings.Contains(kickFn, `querySelectorAll("canvas")`) || !strings.Contains(kickFn, "translateZ") {
		t.Fatal("compositor kick must nudge WebGL/canvas layers without a dummy grid resize")
	}
	if !strings.Contains(invFn, "isNativeWindow()") {
		t.Fatal("compositor invalidate is a WKWebView kick, not a browser-fit")
	}
	if !strings.Contains(refreshFn, "invalidatePaneRender") || !strings.Contains(refreshFn, "reflowPane") {
		t.Fatal("resize observer path must still reflow then invalidate")
	}
	// The per-frame coalescing lives in ui/framethrottle.js (unit-tested in
	// framethrottle.test.mjs); schedulePaneInvalidate must go through it.
	if !strings.Contains(schedFn, "createFrameThrottle") {
		t.Fatal("PTY writes must coalesce invalidates per frame")
	}
	throttleJS, err := http.Get(srv.URL + "/js/ui/framethrottle.js")
	if err != nil {
		t.Fatal(err)
	}
	defer throttleJS.Body.Close()
	throttleBody, _ := io.ReadAll(throttleJS.Body)
	if throttleJS.StatusCode != http.StatusOK || !strings.Contains(string(throttleBody), "requestAnimationFrame") {
		t.Fatal("framethrottle.js must run the coalesced invalidate on an animation frame")
	}
	if !strings.Contains(string(termBody), "else writePaneBytes(pane, ev.data)") {
		t.Fatal("remote PTY bytes must go through writePaneBytes so they invalidate too")
	}
	if !strings.Contains(string(cssBody), ".term-stage") ||
		!strings.Contains(string(cssBody), ".term-tab.is-active { visibility: visible") {
		t.Fatal("keel.css must keep inactive terminals visible to the compositor so WebGL contexts survive")
	}
	if strings.Contains(string(body), `id="terminals" hidden`) {
		t.Fatal("#terminals must not use the hidden attribute; it is display:none and blocks every session tab")
	}
	if !strings.Contains(string(termBody), "els.terminals.hidden = false") {
		t.Fatal("term.js must clear #terminals.hidden when showing a session")
	}
	if !strings.Contains(string(cssBody), ".terminals[hidden] { display: block !important; }") {
		t.Fatal("keel.css must override HTML hidden on #terminals so display:none cannot blank every session")
	}
	if !strings.Contains(string(cssBody), ".term-tab:not(.is-split):not(.is-zoomed) .term-stage > .term-wrap:not(.is-parked)") {
		t.Fatal("keel.css must fill a solo pane across the stage if JS layout has not sized it yet")
	}
	if !strings.Contains(string(body), "addon-search.js") ||
		!strings.Contains(string(body), "addon-web-links.js") ||
		!strings.Contains(string(body), "addon-unicode-graphemes.js") ||
		!strings.Contains(string(body), "addon-progress.js") {
		t.Fatal("index must load search, web-links, unicode graphemes, and progress addons")
	}
	if !strings.Contains(string(termBody), "SearchAddon") ||
		!strings.Contains(string(termBody), "find-term") ||
		!strings.Contains(string(termBody), "function openFind") ||
		!strings.Contains(string(termBody), "UnicodeGraphemesAddon") ||
		!strings.Contains(string(termBody), "WebLinksAddon") ||
		!strings.Contains(string(termBody), "ProgressAddon") ||
		!strings.Contains(string(termBody), "openTermLink") {
		t.Fatal("term.js must wire search, web links, unicode graphemes, and progress")
	}
	if !strings.Contains(string(cssBody), ".term-find") || !strings.Contains(string(cssBody), ".pane-progress") {
		t.Fatal("keel.css missing terminal find overlay or OSC progress bar")
	}
	xtermJS, err := http.Get(srv.URL + "/vendor/xterm/xterm.min.js")
	if err != nil {
		t.Fatal(err)
	}
	defer xtermJS.Body.Close()
	xtermBody, _ := io.ReadAll(xtermJS.Body)
	fitJS, err := http.Get(srv.URL + "/vendor/xterm/addon-fit.js")
	if err != nil {
		t.Fatal(err)
	}
	defer fitJS.Body.Close()
	fitBody, _ := io.ReadAll(fitJS.Body)
	webglJS, err := http.Get(srv.URL + "/vendor/xterm/addon-webgl.js")
	if err != nil {
		t.Fatal(err)
	}
	defer webglJS.Body.Close()
	webglBody, _ := io.ReadAll(webglJS.Body)
	searchJS, err := http.Get(srv.URL + "/vendor/xterm/addon-search.js")
	if err != nil {
		t.Fatal(err)
	}
	defer searchJS.Body.Close()
	searchBody, _ := io.ReadAll(searchJS.Body)
	linksJS, err := http.Get(srv.URL + "/vendor/xterm/addon-web-links.js")
	if err != nil {
		t.Fatal(err)
	}
	defer linksJS.Body.Close()
	linksBody, _ := io.ReadAll(linksJS.Body)
	graphemesJS, err := http.Get(srv.URL + "/vendor/xterm/addon-unicode-graphemes.js")
	if err != nil {
		t.Fatal(err)
	}
	defer graphemesJS.Body.Close()
	graphemesBody, _ := io.ReadAll(graphemesJS.Body)
	progressJS, err := http.Get(srv.URL + "/vendor/xterm/addon-progress.js")
	if err != nil {
		t.Fatal(err)
	}
	defer progressJS.Body.Close()
	progressBody, _ := io.ReadAll(progressJS.Body)
	if !strings.Contains(string(xtermBody), "@xterm/xterm@6.0.0") ||
		!strings.Contains(string(fitBody), "@xterm/addon-fit@0.11.0") ||
		!strings.Contains(string(webglBody), "@xterm/addon-webgl@0.19.0") ||
		!strings.Contains(string(searchBody), "@xterm/addon-search@0.16.0") ||
		!strings.Contains(string(linksBody), "@xterm/addon-web-links@0.12.0") ||
		!strings.Contains(string(graphemesBody), "@xterm/addon-unicode-graphemes@0.4.0") ||
		!strings.Contains(string(progressBody), "@xterm/addon-progress@0.2.0") {
		t.Fatal("desktop UI must vendor xterm 6.0.0 with matching fit, webgl, search, web-links, unicode, and progress addons")
	}
	if !bytes.Contains(searchBody, []byte("window.SearchAddon=i")) ||
		!bytes.Contains(linksBody, []byte("window.WebLinksAddon=i")) ||
		!bytes.Contains(graphemesBody, []byte("window.UnicodeGraphemesAddon=i")) ||
		!bytes.Contains(progressBody, []byte("window.ProgressAddon=i")) {
		t.Fatal("addon UMD must assign constructors onto window, not only module.exports")
	}
	if !strings.Contains(string(termBody), "function createTerminal") ||
		!strings.Contains(string(termBody), "window.Terminal") {
		t.Fatal("term.js must construct xterm from window.Terminal so WKWebView can find it")
	}
	if !bytes.Contains(xtermBody, []byte(`typeof window=="object"?window:globalThis`)) ||
		!bytes.Contains(xtermBody, []byte("window[s]=i[s]")) {
		t.Fatal("xterm UMD must assign Terminal onto window, not only module.exports")
	}
	fsMark := bytes.Index(xtermBody, []byte(`e.FS="`))
	usMark := bytes.Index(xtermBody, []byte(`e.US=`))
	if fsMark < 0 || usMark <= fsMark || bytes.Contains(xtermBody[fsMark:usMark], []byte("\n")) ||
		!bytes.Contains(xtermBody[fsMark:usMark], []byte{0x1c}) {
		t.Fatal("xterm C0 File/Group/Record separators must stay inside the minified string literals")
	}
	if !strings.Contains(string(cssBody), ".xterm-char-measure-element") ||
		!strings.Contains(string(cssBody), "letter-spacing: 0 !important") {
		t.Fatal("xterm measure element must not inherit UI letter-spacing or line-height")
	}
	if !strings.Contains(string(body), "/vendor/notebook/editor.js") ||
		!strings.Contains(string(body), "/vendor/notebook/prosemirror.css") {
		t.Fatal("index must load the notebook markdown editor")
	}
	nbJS, err := http.Get(srv.URL + "/vendor/notebook/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	defer nbJS.Body.Close()
	nbBody, _ := io.ReadAll(nbJS.Body)
	if nbJS.StatusCode != http.StatusOK || !strings.Contains(string(nbBody), "KeelMarkdownEditor") ||
		!strings.Contains(string(nbBody), "notebook-fence") {
		t.Fatal("FileServer must serve the notebook markdown editor")
	}
	if strings.Contains(string(termBody), "active-snippets") || strings.Contains(string(termBody), "activeSnippets") {
		t.Fatal("term.js must not drive a session snippet footer")
	}
	if !strings.Contains(string(cssBody), `html[data-native] .term-tab:not(.is-split) .term-body`) {
		t.Fatal("keel.css missing native terminal glass")
	}
	if strings.Contains(string(cssBody), `#live-section[hidden]`) {
		t.Fatal("keel.css should not keep the live section")
	}
	if !strings.Contains(string(termBody), "killRuntimeIfOwned") || !strings.Contains(string(termBody), "restoreOpenSessions") {
		t.Fatal("term.js must kill on tab close and restore sessions on boot")
	}
	if !strings.Contains(string(termBody), `WORKSPACE_KEY = "keel-workspace"`) ||
		!strings.Contains(string(termBody), "function persistWorkspace") ||
		!strings.Contains(string(termBody), "function restoreSavedTab") ||
		!strings.Contains(string(termBody), "pruneLayout(") ||
		!strings.Contains(readWebLib(t, "lib/layout.js"), "export function pruneLayout") ||
		!strings.Contains(string(termBody), "workspaceReady") {
		t.Fatal("term.js must persist split pane layout and restore it onto the same tab")
	}
	if !strings.Contains(string(termBody), "freshTab") || !strings.Contains(string(termBody), "remapLayout") {
		t.Fatal("workspace restore must rebuild split trees instead of spreading panes onto tabs")
	}
	bootFn := termBody[bytes.Index(termBody, []byte("async function boot()")):]
	bootFn = bootFn[:bytes.Index(bootFn, []byte("function finishTabPointer"))]
	if bytes.Index(bootFn, []byte("restoreOpenSessions()")) > bytes.Index(bootFn, []byte("if (info) attachRuntime(info)")) &&
		bytes.Contains(bootFn, []byte("if (info) attachRuntime(info)")) {
		t.Fatal("boot must restore split workspace before attaching a pending session as its own tab")
	}
	if !strings.Contains(string(termBody), "Runtime stopped — this session ended") || !strings.Contains(string(termBody), "state.runtimeStopping") {
		t.Fatal("term.js must not claim a session is still running after the runtime stops")
	}
	if strings.Contains(string(termBody), "Reopen Keel to attach") {
		t.Fatal("lost connection must reconnect in-place instead of asking to reopen the app")
	}
	if !strings.Contains(string(termBody), "function connectSessionWS") ||
		!strings.Contains(string(termBody), "function reconnectPane") ||
		!strings.Contains(string(termBody), "function replaySessionSnapshot") {
		t.Fatal("term.js must reattach the session websocket when the user clicks Reconnect")
	}
	recStart := strings.Index(string(termBody), "function reconnectPane")
	if recStart < 0 {
		t.Fatal("missing reconnectPane")
	}
	rec := string(termBody)[recStart:]
	if i := strings.Index(rec[1:], "\n  function "); i > 0 {
		rec = rec[:i]
	}
	if strings.Contains(rec, "skipSnapshot: true") {
		t.Fatal("reconnectPane must replay the server scrollback snapshot instead of skipping it")
	}
	if !strings.Contains(rec, "replaySessionSnapshot(pane)") || !strings.Contains(rec, "connectSessionWS(pane)") {
		t.Fatal("reconnectPane must reset the local xterm and replay Subscribe snapshot bytes")
	}
	if !strings.Contains(string(termBody), "pane-fail") ||
		!strings.Contains(string(termBody), "Reconnect") ||
		!strings.Contains(string(termBody), "Copy logs") ||
		!strings.Contains(string(termBody), "data-pane-edit-host") ||
		!strings.Contains(string(termBody), "function syncPaneFail") {
		t.Fatal("term.js must show a connection banner with Reconnect")
	}
	if !strings.Contains(string(cssBody), ".pane-fail") ||
		!strings.Contains(string(cssBody), ".pane-fail-start") ||
		!strings.Contains(string(cssBody), ".pane-fail-log") {
		t.Fatal("keel.css missing connection transition page")
	}
	if strings.Contains(string(termBody), "function renderHerd") {
		t.Fatal("term.js should not render a Hosts Live list")
	}
	// shadcn's --radius maps to the radius-md token in globals.css; keel.css
	// keeps no second radius scale.
	if strings.Contains(string(cssBody), "--radius:") {
		t.Fatal("keel.css must not override shadcn --radius")
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
	if !strings.Contains(string(fragBody), `class="k-tree-host"`) || !strings.Contains(string(fragBody), `data-connect`) {
		t.Fatal("ui hosts fragment should render the sidebar host tree")
	}
	// Tab pages carry the boards' page heads (Port forwarding, Logs,
	// Notebooks) or list titles (Keychain, Snippets).
	for _, head := range []string{
		`<h1 class="k-page-title">Port forwarding</h1>`,
		`<h1 class="k-page-title">Logs</h1>`,
		`<h1 class="k-split-title">Keychain</h1>`,
		`<h1 class="k-split-title">Snippets</h1>`,
	} {
		if !strings.Contains(string(body), head) {
			t.Fatalf("tab pages missing %s", head)
		}
	}
	if !strings.Contains(string(body), `<h1 class="k-page-title">Appearance</h1>`) || strings.Contains(string(body), `data-settings-back`) {
		t.Fatal("settings is a tab page: section rail and heading, no Back button")
	}
	if !strings.Contains(string(body), `id="btn-new-identity"`) || !strings.Contains(string(body), `id="btn-new-tunnel"`) ||
		!strings.Contains(string(body), `id="btn-new-snippet"`) || !strings.Contains(string(body), `id="btn-new-notebook"`) ||
		!strings.Contains(string(body), `id="btn-refresh-logs"`) {
		t.Fatal("index missing vault action buttons")
	}
	if !strings.Contains(string(body), `id="group-modal"`) || !strings.Contains(string(body), `id="host-parent-group"`) ||
		!strings.Contains(string(body), `id="btn-new-group"`) {
		t.Fatal("index missing Parent Group dialogs")
	}
	if !strings.Contains(listSection(t, srv.URL, "snippet-list"), `data-snippet-select`) {
		t.Fatal("the page should render the snippet list")
	}
	if !strings.Contains(listSection(t, srv.URL, "identity-list"), `data-slot="empty"`) {
		t.Fatal("the page should render the empty identity list")
	}
	for _, gone := range []string{"/ui/snippets", "/ui/identities", "/ui/tunnels", "/ui/notebooks"} {
		res, err := http.Get(srv.URL + gone)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatalf("%s should be gone: the lists come from the stream (D4)", gone)
		}
	}
	logsFrag, err := http.Get(srv.URL + "/ui/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer logsFrag.Body.Close()
	logsBody, _ := io.ReadAll(logsFrag.Body)
	if logsFrag.StatusCode != http.StatusOK || !strings.Contains(string(logsBody), `id="log-list"`) {
		t.Fatal("ui logs fragment missing log-list")
	}
	if !strings.Contains(string(cssBody), ".k-page {") ||
		!strings.Contains(string(cssBody), ".k-split {") ||
		!strings.Contains(string(cssBody), ".k-table {") {
		t.Fatal("keel.css must lay out the tab pages")
	}
	// D4: the lists are templ only; vaultview.js keeps the Keychain detail
	// and the snippet preview, not list renderers.
	if vv := readWebLib(t, "lib/vaultview.js"); !strings.Contains(vv, "export function identityDetailHTML") || strings.Contains(vv, "export function hostsHTML") {
		t.Fatal("vaultview.js keeps the detail painters and drops the list renderers")
	}
	if term := readWebLib(t, "term.js"); strings.Contains(term, "function paintList") || strings.Contains(term, "function datastarQS") {
		t.Fatal("term.js must not fetch and swap lists itself (D4)")
	}
	if strings.Contains(string(body), `data-vault-view-btn`) {
		t.Fatal("the card/list view toggle left with the Hosts page")
	}
	settingsStart := strings.Index(string(body), `id="page-settings"`)
	if settingsStart < 0 {
		t.Fatal("index missing settings page")
	}
	if strings.Contains(string(body)[settingsStart:], `data-vault-view-btn`) {
		t.Fatal("settings should not include the vault view selector")
	}
	logsStart := strings.Index(string(body), `id="page-logs"`)
	if logsStart < 0 || logsStart >= settingsStart {
		t.Fatal("index missing logs page before settings")
	}
	if strings.Contains(string(body)[logsStart:settingsStart], `data-vault-view-btn`) {
		t.Fatal("logs should not include the vault view selector")
	}
	if !strings.Contains(string(termBody), "function toggleSidebar") ||
		!strings.Contains(string(termBody), "function paintSidebar") ||
		!strings.Contains(string(termBody), "function setSessionSurface") ||
		!strings.Contains(string(termBody), `els.ptyInjectModal?.open || sidebarFocused()`) ||
		!strings.Contains(string(termBody), `sidebarFocused() || state.openModal || ptyInjectWaiter`) ||
		!strings.Contains(readShortcutsJS(t), `key === "b"`) {
		t.Fatal("term.js must open and close the vault sidebar")
	}
	// Tab pages are tabs: one instance each (page:<name>), and with no tab
	// left the window shows a New tab, not a Vault tab.
	if !strings.Contains(string(termBody), "function openPage") || !strings.Contains(string(termBody), `const PAGE_PREFIX = "page:"`) ||
		strings.Contains(string(termBody), "VAULT") {
		t.Fatal("term.js must open tab pages as tabs and keep no Vault tab")
	}
	paint := termBody[bytes.Index(termBody, []byte("function paintSidebar")):]
	if i := bytes.Index(paint[1:], []byte("\n  function ")); i > 0 {
		paint = paint[:i]
	}
	if bytes.Contains(paint, []byte("sendSessionBytes")) || bytes.Contains(paint, []byte("sendSessionJSON")) {
		t.Fatal("session sidebar must not write into a live PTY")
	}
	if !strings.Contains(string(logsBody), `<tbody id="log-list">`) || !strings.Contains(string(logsBody), `data-log-kind=`) {
		t.Fatal("ui logs fragment should render table rows the kind chips can filter")
	}
}

func TestIndexServesCommandPalette(t *testing.T) {
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
	srv := httptest.NewServer(New(st, rt, web, nil).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	html := string(body)
	if !strings.Contains(html, `id="cmd-palette"`) || !strings.Contains(html, `id="cmd-palette-input"`) {
		t.Fatal("index missing command palette dialog")
	}
	if !strings.Contains(html, `id="pty-inject-modal"`) || !strings.Contains(html, `id="pty-inject-preview"`) {
		t.Fatal("index missing approve-before-type dialog")
	}
	if !strings.Contains(html, "Nothing is typed until you approve") || !strings.Contains(html, `id="pty-inject-ok"`) {
		t.Fatal("approve-before-type dialog must explain the gate and have a Type button")
	}

	cssRes, err := http.Get(srv.URL + "/css/keel.css")
	if err != nil {
		t.Fatal(err)
	}
	defer cssRes.Body.Close()
	cssBody, _ := io.ReadAll(cssRes.Body)
	css := string(cssBody)
	if !strings.Contains(css, ".cmd-palette") || !strings.Contains(css, "var(--surface)") || !strings.Contains(css, "var(--chrome)") {
		t.Fatal("keel.css missing command palette chrome tokens")
	}

	jsRes, err := http.Get(srv.URL + "/js/term.js")
	if err != nil {
		t.Fatal(err)
	}
	defer jsRes.Body.Close()
	jsBody, _ := io.ReadAll(jsRes.Body)
	js := string(jsBody)
	// Keys map to actions in lib/shortcuts.js; term.js runs them.
	keyTable := readShortcutsJS(t)
	if !strings.Contains(js, `name === "palette"`) || !strings.Contains(js, "runShortcut(action.name, action.repeat)") {
		t.Fatal("term.js must open the command palette through runShortcut")
	}
	kIdx := strings.Index(keyTable, `if (key === "k" && !ev.shiftKey)`)
	if kIdx < 0 {
		t.Fatal("shortcut table missing ⌘K")
	}
	kLine := keyTable[kIdx:]
	if nl := strings.Index(kLine, "\n"); nl >= 0 {
		kLine = kLine[:nl]
	}
	if !strings.Contains(kLine, `name: "palette"`) {
		t.Fatal("⌘K must open the command palette")
	}
	if strings.Contains(kLine, `name: "search"`) {
		t.Fatal("⌘K must not focus host search")
	}
	if !strings.Contains(keyTable, `if (key === "p" && ev.shiftKey) return { name: "palette" }`) {
		t.Fatal("shortcut table missing ⌘⇧P palette shortcut")
	}

	catStart := strings.Index(js, "function paletteCatalog()")
	catEndRel := strings.Index(js[catStart:], "\n  const paletteList = createPaletteList(")
	if catStart < 0 || catEndRel < 0 {
		t.Fatal("term.js missing palette catalog")
	}
	catalog := js[catStart : catStart+catEndRel]
	if strings.Contains(catalog, "sendSnippet") || strings.Contains(catalog, "sendSessionBytes") {
		t.Fatal("palette catalog must not write a PTY; snippet runs must go through paletteRunSnippet")
	}
	if !strings.Contains(catalog, "paletteRunSnippet") || !strings.Contains(catalog, "New session on ") || !strings.Contains(catalog, `group: "Open now"`) {
		t.Fatal("palette catalog must include open tabs, host sessions and gated snippet actions")
	}

	fnStart := strings.Index(js, "async function paletteRunSnippet")
	if fnStart < 0 {
		t.Fatal("term.js missing paletteRunSnippet")
	}
	fnEndRel := strings.Index(js[fnStart+1:], "\n  async function ")
	fnAlt := strings.Index(js[fnStart+1:], "\n  function ")
	fnEnd := len(js)
	if fnEndRel >= 0 {
		fnEnd = fnStart + 1 + fnEndRel
	}
	if fnAlt >= 0 && fnStart+1+fnAlt < fnEnd {
		fnEnd = fnStart + 1 + fnAlt
	}
	fn := js[fnStart:fnEnd]
	approveAt := strings.Index(fn, "await approveBeforeType")
	sendAt := strings.Index(fn, "sendApprovedInject")
	if approveAt < 0 || sendAt < 0 || approveAt > sendAt {
		t.Fatal("palette snippet inject must await approveBeforeType before Inject")
	}
	if !strings.Contains(fn, "if (!ok) return") {
		t.Fatal("palette snippet inject must abort when approve-before-type is cancelled")
	}
	if strings.Contains(fn, "sendSnippet") || strings.Contains(fn, "sendSessionBytes") {
		t.Fatal("palette inject must not call sendSnippet or raw session bytes")
	}
	if !strings.Contains(js, `type: "inject"`) || !strings.Contains(js, `type: "inject-challenge"`) || strings.Contains(js, "approved: true") {
		t.Fatal("palette inject must send WS inject with a server nonce")
	}
	if !strings.Contains(js, `type: "focus", surface`) && !strings.Contains(js, `type: "focus", surface:`) {
		t.Fatal("palette must set session focus surface")
	}
	if !strings.Contains(js, `setSessionSurface("palette")`) || !strings.Contains(js, `setSessionSurface("term")`) {
		t.Fatal("palette open must set SurfacePalette and close must restore SurfaceTerm")
	}
	if !strings.Contains(fn, `"\u0015" + command`) {
		t.Fatal("palette inject must clear the line then send the snippet through Inject")
	}
	if !strings.Contains(js, "function approveBeforeType") {
		t.Fatal("term.js missing approveBeforeType")
	}
	if !strings.Contains(js, "if (ptyWritesBlocked()) return") {
		t.Fatal("xterm onData must consult ptyWritesBlocked")
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

func waitReadySnapshot(t *testing.T, conn *websocket.Conn) (map[string]any, string) {
	t.Helper()
	var snap string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if mt == websocket.BinaryMessage {
			snap += string(msg)
			continue
		}
		if mt != websocket.TextMessage {
			continue
		}
		var body map[string]any
		if json.Unmarshal(msg, &body) != nil {
			continue
		}
		if body["type"] == "ready" {
			return body, snap
		}
	}
	t.Fatalf("timeout waiting for ready snapshot=%q", snap)
	return nil, snap
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

func TestCmdTSingleTabAndNotebookEditorDispose(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)
	ed, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "vendor", "notebook", "editor.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(ed)

	if !strings.Contains(term, "if (window.__keelTermBound) return") || !strings.Contains(term, "window.__keelTermBound = true") {
		t.Fatal("term.js IIFE must refuse a second mount")
	}
	bind := sliceJSFunc(term, "function bindChrome")
	if bind == "" || !strings.Contains(bind, "if (window.__keelChromeBound) return") {
		t.Fatal("bindChrome must register window keydown once")
	}
	handle := sliceJSFunc(term, "function handleShortcut")
	if handle == "" {
		t.Fatal("missing handleShortcut")
	}
	if strings.Contains(handle, "nativeMenuOwnsShortcut") {
		t.Fatal("handleShortcut must run chrome keys in JS; deferring to the File menu leaves most shortcuts dead when the accelerator does not fire")
	}
	// The key -> action table lives in lib/shortcuts.js (behaviour tests in
	// cmd/keel/web/js/test/shortcuts.test.mjs); handleShortcut runs it.
	shortcutsSrc, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "lib", "shortcuts.js"))
	if err != nil {
		t.Fatal(err)
	}
	shortcuts := string(shortcutsSrc)
	if !strings.Contains(handle, "classifyShortcut(ev") || !strings.Contains(handle, "runShortcut(action.name, action.repeat)") {
		t.Fatal("handleShortcut must classify keys with classifyShortcut and run the action in JS")
	}
	if !strings.Contains(shortcuts, `key === "w" && !ev.shiftKey`) {
		t.Fatal("Cmd+W closes a pane or tab; Cmd+Shift+W must still reach Close Window")
	}
	created := sliceJSFunc(term, "function newTab")
	if created == "" || !strings.Contains(created, "newTabLockUntil") || !strings.Contains(created, "createStartTab") {
		t.Fatal("newTab must coalesce duplicate gestures into one createStartTab")
	}
	if strings.Count(created, "createStartTab") != 1 {
		t.Fatal("newTab must create exactly one start tab per call")
	}
	run := sliceJSFunc(term, "function runShortcut")
	if run == "" || !strings.Contains(run, "lastShortcutAt < 300") {
		t.Fatal("runShortcut must debounce duplicate new-tab within one gesture")
	}
	for _, name := range []string{
		"new-tab", "close-tab", "reopen-tab", "new-host", "quick-connect", "join",
		"find-term", "toggle-sidebar", "split-right", "split-down", "focus-pane",
		"zoom-in", "zoom-out", "zoom-reset", "palette", "settings",
	} {
		if !strings.Contains(shortcuts, `name: "`+name+`"`) {
			t.Fatalf("classifyShortcut must map a key to %s", name)
		}
	}

	disposeEd := sliceJSFunc(term, "function disposeNotebookEditor")
	disposePane := sliceJSFunc(term, "function disposePane")
	paint := sliceJSFunc(term, "function paintNotebook")
	mount := sliceJSFunc(term, "function mountNotebookEditor")
	if disposeEd == "" || disposePane == "" || paint == "" || mount == "" {
		t.Fatal("missing notebook editor lifecycle helpers")
	}
	if !strings.Contains(disposePane, "disposeNotebookEditor(pane)") {
		t.Fatal("closing a pane must tear down the markdown editor")
	}
	if !strings.Contains(disposeEd, "clearTimeout(pane._saveTimer)") || !strings.Contains(disposeEd, "mdEditor?.destroy") {
		t.Fatal("disposeNotebookEditor must destroy ProseMirror and drop the save timer")
	}
	if !strings.Contains(paint, "pane.mdEditor && pane._docEl") || !strings.Contains(paint, "disposeNotebookEditor(pane)") {
		t.Fatal("paintNotebook must keep a live editor and destroy before remount")
	}
	if !strings.Contains(mount, "pane.closing || !pane.mdEditor") {
		t.Fatal("onChange after close must not reschedule saves")
	}
	if !strings.Contains(src, "t.replaceChildren()") {
		t.Fatal("KeelMarkdownEditor.destroy must empty the host so views cannot leak")
	}

	for _, fn := range []string{created, handle, disposeEd, disposePane, paint, mount} {
		if strings.Contains(fn, "sendSessionBytes") || strings.Contains(fn, "sendSnippet") ||
			strings.Contains(fn, "sendApprovedInject") || strings.Contains(fn, `type: "inject"`) ||
			strings.Contains(fn, "approved: true") {
			t.Fatal("tab create / notebook dispose must not Write or Inject the PTY")
		}
	}
}

func nodeBin(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	if _, err := os.Stat("/exec-daemon/node"); err == nil {
		return "/exec-daemon/node"
	}
	t.Fatal("node is required to eval tabCycleDelta")
	return ""
}

func TestCmdAngleCycleTabs(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	term := string(js)
	if !strings.Contains(term, `import { tabCycleTarget, tabIndexTarget } from "./lib/keys.js";`) ||
		!strings.Contains(term, `import { classifyShortcut, isAppMod, isChromeChord, menuShortcutAllowed } from "./lib/shortcuts.js";`) {
		t.Fatal("term.js must take tab cycling from lib/keys.js and the shortcut table from lib/shortcuts.js")
	}
	keys, err := filepath.Abs(filepath.Join("..", "..", "cmd", "keel", "web", "js", "lib", "keys.js"))
	if err != nil {
		t.Fatal(err)
	}
	keysPath := filepath.ToSlash(keys)
	if !strings.HasPrefix(keysPath, "/") {
		keysPath = "/" + keysPath // Windows drive path
	}
	keysURL := (&url.URL{Scheme: "file", Path: keysPath}).String()
	script := filepath.Join(t.TempDir(), "tabcycle.mjs")
	harness := "import { tabCycleDelta, tabCycleTarget } from " + strconv.Quote(keysURL) + ";" + `
function eq(name, got, want) {
  if (got !== want) throw new Error(name + ": got " + JSON.stringify(got) + " want " + JSON.stringify(want));
}
const cases = [
  [{ key: "<", code: "Comma", shiftKey: true }, -1, "us shift comma"],
  [{ key: ",", code: "Comma", shiftKey: true }, -1, "us shift comma key comma"],
  [{ key: ">", code: "Period", shiftKey: true }, 1, "us shift period"],
  [{ key: ".", code: "Period", shiftKey: true }, 1, "us shift period key dot"],
  [{ key: "<", code: "IntlBackslash", shiftKey: false }, -1, "iso less-than"],
  [{ key: ">", code: "IntlBackslash", shiftKey: true }, 1, "iso greater-than"],
  [{ key: "{", code: "BracketLeft", shiftKey: true }, -1, "mac shift bracket left"],
  [{ key: "[", code: "BracketLeft", shiftKey: true }, -1, "mac shift bracket left key"],
  [{ key: "}", code: "BracketRight", shiftKey: true }, 1, "mac shift bracket right"],
  [{ key: "]", code: "BracketRight", shiftKey: true }, 1, "mac shift bracket right key"],
  [{ key: ",", code: "Comma", shiftKey: false }, 0, "settings comma"],
  [{ key: "[", code: "BracketLeft", shiftKey: false }, 0, "unshifted bracket is not back"],
  [{ key: "\\", code: "IntlBackslash", shiftKey: false }, 0, "uk backslash is not prev tab"],
  [{ key: "+", code: "Equal", shiftKey: true }, 0, "zoom in"],
  [{ key: "t", code: "KeyT", shiftKey: false }, 0, "new tab"],
];
for (const [ev, want, name] of cases) eq(name, tabCycleDelta(ev), want);
eq("wrap prev from first", tabCycleTarget(["vault", "a", "b"], "vault", -1), "b");
eq("wrap next from last", tabCycleTarget(["vault", "a", "b"], "b", 1), "vault");
eq("prev from middle", tabCycleTarget(["vault", "a", "b"], "a", -1), "vault");
eq("next from middle", tabCycleTarget(["vault", "a", "b"], "a", 1), "b");
eq("missing id starts at 0", tabCycleTarget(["vault", "a", "b"], "gone", 1), "a");
eq("single tab wraps to self", tabCycleTarget(["vault"], "vault", 1), "vault");
eq("empty list", tabCycleTarget([], "vault", 1), "");
eq("zero delta", tabCycleTarget(["vault", "a"], "a", 0), "");
console.log("ok");
`
	if err := os.WriteFile(script, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodeBin(t), script).CombinedOutput()
	if err != nil {
		t.Fatalf("tab cycle eval: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("tab cycle eval missing ok: %s", out)
	}

	handle := sliceJSFunc(term, "function handleShortcut")
	run := sliceJSFunc(term, "function runShortcut")
	if handle == "" || run == "" {
		t.Fatal("missing shortcut helpers")
	}
	shortcutsSrc, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "lib", "shortcuts.js"))
	if err != nil {
		t.Fatal(err)
	}
	shortcuts := string(shortcutsSrc)
	if !strings.Contains(handle, "classifyShortcut(ev") {
		t.Fatal("handleShortcut must classify keys with classifyShortcut")
	}
	if !strings.Contains(shortcuts, "tabCycleDelta(ev)") {
		t.Fatal("classifyShortcut must classify Cmd+< / Cmd+> through tabCycleDelta")
	}
	idxCycle := strings.Index(shortcuts, "tabCycleDelta(ev)")
	idxSettings := strings.Index(shortcuts, `key === "," && !ev.shiftKey`)
	if idxCycle < 0 || idxSettings < 0 || idxCycle > idxSettings {
		t.Fatal("tab cycle must run before the Cmd+, settings match")
	}
	if !strings.Contains(run, `name === "prev-tab"`) || !strings.Contains(run, `name === "next-tab"`) {
		t.Fatal("runShortcut must activate prev-tab and next-tab")
	}
	if !strings.Contains(run, "activateTabDelta") {
		t.Fatal("runShortcut must move the chrome tab via activateTabDelta")
	}
	if strings.Contains(handle, "_keelTabCycle") {
		t.Fatal("window capture must own the keydown; do not stamp the event to hide a second listener")
	}
	if !strings.Contains(shortcuts, "repeat: !!ev.repeat") || !strings.Contains(handle, "action.repeat") {
		t.Fatal("hold-to-repeat tab cycling must pass ev.repeat into runShortcut")
	}
	if !strings.Contains(run, "!repeat") || !strings.Contains(run, "lastShortcutAt < 300") {
		t.Fatal("runShortcut must debounce a duplicate gesture and still run key repeat")
	}
	bind := sliceJSFunc(term, "function bindChrome")
	if !strings.Contains(bind, "if (handleShortcut(ev)) ev.stopPropagation()") {
		t.Fatal("bindChrome must stop the keydown after JS handles it so xterm does not run it again")
	}

	cat := sliceJSFunc(term, "function paletteCatalog")
	if !strings.Contains(cat, `id: "prev-tab"`) || !strings.Contains(cat, `id: "next-tab"`) {
		t.Fatal("command palette must list previous and next tab")
	}
}

// readShortcutsJS returns lib/shortcuts.js, where term.js's key -> action
// table lives (behaviour tests: cmd/keel/web/js/test/shortcuts.test.mjs).
// readWebLib reads a module under cmd/keel/web/js (e.g. "lib/layout.js").
func readWebLib(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readShortcutsJS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "lib", "shortcuts.js"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Phase 0 of docs/ui-redesign.md: colors come only from the Keel tokens
// (tokens.css, generated from docs/design/tokens.json), keel.css keeps no
// palette of its own, and fonts ship locally.
func TestStylesUseKeelTokensOnly(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	srv := httptest.NewServer(New(st, rt, os.DirFS(filepath.Join("..", "..", "cmd", "keel", "web")), nil).Handler())
	t.Cleanup(srv.Close)
	get := func(path string) string {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, res.StatusCode)
		}
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	page := get("/")
	for _, href := range []string{"/fonts/fonts.css", "/css/tokens.css", "/css/output.css", "/css/keel.css"} {
		if !strings.Contains(page, `href="`+href+`"`) {
			t.Errorf("page does not link %s", href)
		}
	}
	if strings.Contains(page, "app.css") {
		t.Error("page still links app.css")
	}

	tokens := get("/css/tokens.css")
	for _, want := range []string{":root,[data-theme=dark]{", "[data-theme=light]{", "--brass:", "--font-sans:"} {
		if !strings.Contains(tokens, want) {
			t.Errorf("tokens.css missing %q", want)
		}
	}

	keel := get("/css/keel.css")
	if regexp.MustCompile(`--keel-|var\(--(bg|panel|text|muted|elevated|danger|warn|success|backdrop|border-strong|primary-fg|radius-control)\)`).MatchString(keel) {
		t.Error("keel.css still uses the old --keel-* palette or its aliases")
	}
	if m := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(|oklch\(`).FindString(keel); m != "" {
		t.Errorf("keel.css declares a color literal %q; use a token", m)
	}

	fonts := get("/fonts/fonts.css")
	for _, family := range []string{"IBM Plex Sans", "IBM Plex Sans Condensed", "JetBrains Mono"} {
		if !strings.Contains(fonts, "font-family: '"+family+"'") {
			t.Errorf("fonts.css missing %s", family)
		}
	}
	for _, css := range []string{page, keel, fonts, get("/css/output.css")} {
		if strings.Contains(css, "fonts.googleapis") || strings.Contains(css, "fonts.gstatic") {
			t.Error("UI loads fonts from the network")
		}
	}
	if get("/fonts/ibm-plex-sans-latin-400-normal.woff2") == "" {
		t.Error("empty woff2")
	}
}

func TestRuntimeInfo(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	api := New(st, rt, fs.FS(web), nil).WithBuild("abc-123").WithRuntimeInfo("127.0.0.1:9", "/data/keel").WithStop(func() {})
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/api/runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var got struct {
		Build     string `json:"build"`
		Addr      string `json:"addr"`
		DataDir   string `json:"dataDir"`
		Sessions  int    `json:"sessions"`
		StartedAt string `json:"startedAt"`
		UptimeSec int64  `json:"uptimeSec"`
		CanStop   bool   `json:"canStop"`
		SignalURL string `json:"signalURL"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Build != "abc-123" || got.Addr != "127.0.0.1:9" || got.DataDir != "/data/keel" || !got.CanStop || got.Sessions != 0 || got.UptimeSec < 0 || got.StartedAt == "" || got.SignalURL != "" {
		t.Fatalf("runtime info = %+v", got)
	}
}

// listSection returns the page's markup from the element with id up to the
// next list (at most 8 KB), for checks on one server-rendered list.
func listSection(t *testing.T, srvURL, id string) string {
	t.Helper()
	res, err := http.Get(srvURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	page := string(b)
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		t.Fatalf("page has no #%s", id)
	}
	sec := page[i:]
	if len(sec) > 8192 {
		sec = sec[:8192]
	}
	return sec
}
