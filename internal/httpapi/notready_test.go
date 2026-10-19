package httpapi

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

// gatedServer is testServer whose opener blocks until release is called, so
// the session stays connecting (no bridge) for as long as the test needs.
func gatedServer(t *testing.T) (srv *httptest.Server, st *store.Store, fb *fakeBridge, release func()) {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	fb = newFakeBridge()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		<-gate
		return fb, nil
	})
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv = httptest.NewServer(New(st, rt, fs.FS(web), nil).Handler())
	t.Cleanup(func() {
		release()
		srv.Close()
	})
	return srv, st, fb, release
}

// Issue #63: a keystroke typed while the session is still connecting used to
// close the session socket ("session is not ready"). It must be dropped (not
// buffered for the next prompt) and the socket must stay open. An approved
// inject at the same moment must still fail and be reported.
func TestKeystrokeWhileConnectingKeepsSocket(t *testing.T) {
	srv, st, fb, release := gatedServer(t)
	host := firstHost(t, st)
	info := spawn(t, srv.URL, host.ID)
	conn := dialSession(t, srv.URL, info.ID, "alice", "web")
	defer conn.Close()
	ready := waitJSON(t, conn, "ready")
	if ready["status"] == string(runtime.StatusReady) {
		t.Fatal("session reported ready before the opener was released")
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("early")); err != nil {
		t.Fatal(err)
	}

	// An approved (server nonce) inject while not ready fails and says so;
	// the server answering it also shows the socket survived the keystroke.
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": "ls\n"}); err != nil {
		t.Fatal(err)
	}
	nonce, _ := waitJSON(t, conn, "inject-challenge")["nonce"].(string)
	if nonce == "" {
		t.Fatal("no nonce")
	}
	if err := conn.WriteJSON(map[string]any{"type": "inject", "command": "ls\n", "nonce": nonce}); err != nil {
		t.Fatal(err)
	}
	if e := waitJSON(t, conn, "error"); !strings.Contains(e["message"].(string), runtime.ErrNotReady.Error()) {
		t.Fatalf("inject while connecting → %v", e)
	}

	release()
	waitBridgeReady(t, conn, map[string]any{})
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := fb.inR.Read(buf)
		got <- string(buf[:n])
	}()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("late")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		// Neither the early keystroke nor the failed inject was kept.
		if hit != "late" {
			t.Fatalf("PTY got %q, want only the keystroke typed after ready", hit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("keystroke after ready never reached the PTY")
	}
}

// An approved agent Type into a pane whose session has no bridge fails with
// ok:false and a message saying nothing was typed, never ok:true (§6.1, §6.7).
func TestAgentTypeWhileNotReadyFails(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "ready\n")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitBridgeReady(t, driver, waitJSON(t, driver, "ready"))

	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	ro := proposalsOf(t, resp)[0]

	// The bridge goes away: the session has no bridge until it reconnects.
	_ = f.bridge(t, f.webID).Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, err := f.rt.Get(web.ID)
		if err == nil && s.Info().Status != runtime.StatusReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session never left ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A live keystroke now is dropped and the socket stays open.
	if err := driver.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
		t.Fatal(err)
	}
	r := typeOverSocket(t, driver, ro["bytes"].(string), ro["id"].(string), false)
	if r["ok"] != false || r["message"] != notReadyTypeMessage {
		t.Fatalf("type while not ready → %v", r)
	}
}
