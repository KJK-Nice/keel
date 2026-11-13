package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
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
	srv := httptest.NewServer(New(st, rt, nil).Handler())
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

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/session?id=x"
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
	srv, _, st, _ := testServer(t)
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

	info := spawn(t, srv.URL, firstHost(t, st).ID)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/session?id=" + info.ID + "&name=alice&kind=web"
	hdr := http.Header{}
	hdr.Set("Origin", srv.URL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
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
	body := `{"name":"gpu-box","address":"10.0.0.9","port":22,"username":"ubuntu","authType":"password","password":"pw","color":"var(--ansi-12)","tags":[" prod ","#GPU","prod"]}`
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
}

func TestRuntimeStop(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	stopped := make(chan struct{}, 1)
	srv := httptest.NewServer(New(st, rt, nil).WithStop(func() { stopped <- struct{}{} }).Handler())
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

	bare := httptest.NewServer(New(st, rt, nil).Handler())
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
	srv := httptest.NewServer(New(st, rt, nil).Handler())
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
	waitBridgeReady(t, alice, waitJSON(t, alice, "ready"))

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
	waitBridgeReady(t, alice, waitJSON(t, alice, "ready"))

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
	waitBridgeReady(t, alice, ready)
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

// The session socket's write paths, by source (the web page's own JS checks went with the webview; the native
// client's are internal/native/guard_test.go and keelclient/noforgedapproval_test.go).
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
	srv := httptest.NewServer(New(st, rt, nil).Handler())
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
	srv := httptest.NewServer(New(st, rt, nil).Handler())
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

func TestIdentityTunnelLogsAPI(t *testing.T) {
	srv, _, st, _ := testServer(t)
	logs := applog.New(t.TempDir() + "/keel.log")
	tnRT := tunnel.New(st, func(store.Host) (tunnel.Dialer, error) {
		return nil, errors.New("no ssh in test")
	})
	// rebuild server with extras
	srv.Close()
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	srv2 := httptest.NewServer(New(st, rt, nil).WithTunnels(tnRT).WithLogs(logs).Handler())
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
	body := `{"name":"api","group":"prod","address":"10.0.0.8","port":22,"username":"ubuntu","authType":"password","password":"secret","color":"var(--ansi-12)"}`
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

// The host list the native client shows is GET /api/hosts: a duplicated host is listed beside its source, with the
// secret redacted.
func TestHostListDuplicateAction(t *testing.T) {
	srv, _, st, _ := testServer(t)
	saved, err := st.UpsertHost(store.Host{
		Name: "api", Address: "10.0.0.8", Username: "ubuntu", AuthType: "password", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	dup, err := http.Post(srv.URL+"/api/hosts/"+saved.ID+"/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dup.Body.Close()
	if dup.StatusCode != http.StatusCreated {
		t.Fatalf("duplicate %d", dup.StatusCode)
	}
	res, err := http.Get(srv.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var hosts []store.Host
	if err := json.NewDecoder(res.Body).Decode(&hosts); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, h := range hosts {
		names[h.Name] = true
		if h.Password == "secret" {
			t.Fatalf("host %q lists its password", h.Name)
		}
	}
	if !names["api"] || !names["api (copy)"] {
		t.Fatalf("host list = %v", names)
	}
}

func TestNestedGroupsAPIAndVaultTree(t *testing.T) {
	srv, _, _, _ := testServer(t)

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

	// The tree the native client draws is these two lists: both hosts now sit in the root group.
	hostsRes, err := http.Get(srv.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer hostsRes.Body.Close()
	var hosts []store.Host
	if err := json.NewDecoder(hostsRes.Body).Decode(&hosts); err != nil {
		t.Fatal(err)
	}
	moved := 0
	for _, h := range hosts {
		if (h.ID == host.ID || h.ID == db.ID) && h.GroupID == root.ID {
			moved++
		}
	}
	if moved != 2 {
		t.Fatalf("hosts after move = %+v", hosts)
	}
}

// TestManualServe binds the API on a fixed address for poking at it by hand (the native client attaches to it).
func TestManualServe(t *testing.T) {
	if os.Getenv("KEEL_SERVE") != "1" {
		t.Skip("set KEEL_SERVE=1 to bind 127.0.0.1:47241")
	}
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
	h := New(st, rt, nil).WithTunnels(tn).WithLogs(logs).Handler()
	addr := "127.0.0.1:47241"
	if v := os.Getenv("KEEL_SERVE_ADDR"); v != "" {
		addr = v
	}
	t.Log("keel api http://" + addr)
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
	// Closed before the fixture's cleanups (registered earlier) wait for
	// the server's sockets to end.
	t.Cleanup(func() { _ = conn.Close() })
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

// waitBridgeReady waits until the session's bridge is connected. The
// "ready" message only means the socket attached: the runtime dials the
// bridge on its own goroutine, and until it lands Session.Write fails with
// runtime.ErrNotReady (live keystrokes are dropped) and nothing can reach the
// PTY, so a "nothing was written" check would pass for the wrong reason.
// ready is the attach message; if it already reports StatusReady there is no
// later status event to wait for.
func waitBridgeReady(t *testing.T, conn *websocket.Conn, ready map[string]any) {
	t.Helper()
	if ready["status"] == string(runtime.StatusReady) {
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
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
		if body["type"] == "status" && body["status"] == string(runtime.StatusReady) {
			return
		}
	}
	t.Fatal("timeout waiting for the session bridge to connect")
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

func TestRuntimeInfo(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	api := New(st, rt, nil).WithBuild("abc-123").WithRuntimeInfo("127.0.0.1:9", "/data/keel").WithStop(func() {})
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
