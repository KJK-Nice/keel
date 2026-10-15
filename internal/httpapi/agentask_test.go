package httpapi

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
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

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/providertest"
	"keel/internal/applog"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

const (
	askKey       = "AIzaSyD-askroute-test-key-00112233445566"
	askHostPass  = "host-password-9d8e7f6a5b"
	askPaneToken = "API_TOKEN=Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK"
)

type askFixture struct {
	srv     *httptest.Server
	st      *store.Store
	rt      *runtime.Runtime
	logPath string
	logs    *applog.Book
	fake    *providertest.Scripted
	judge   *providertest.ScriptedJudge
	bmu     sync.Mutex
	bridges map[string]*fakeBridge
	prodID  string
	webID   string
	offID   string
}

func bp(b bool) *bool { return &b }

func newAskFixture(t *testing.T, withKey bool) *askFixture {
	t.Helper()
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &askFixture{st: st, bridges: map[string]*fakeBridge{}, fake: providertest.NewScripted(), judge: providertest.NewScriptedJudge()}
	web, err := st.UpsertHost(store.Host{Name: "web-1", AuthType: "password", Address: "web.example.com", Username: "deploy", Port: 22, Password: askHostPass, AgentReadable: bp(true)})
	if err != nil {
		t.Fatal(err)
	}
	prod, err := st.UpsertHost(store.Host{Name: "prod-db-01", AuthType: "password", Address: "10.0.0.9", Username: "pg", Port: 22, Password: "other-pass-1234", Production: bp(true)})
	if err != nil {
		t.Fatal(err)
	}
	off, err := st.UpsertHost(store.Host{Name: "secret-box", AuthType: "password", Address: "s.example.com", Username: "x", Port: 22, Password: "third-pass-5678", AgentReadable: bp(false)})
	if err != nil {
		t.Fatal(err)
	}
	f.webID, f.prodID, f.offID = web.ID, prod.ID, off.ID
	if withKey {
		if err := st.SetAgentKey("gemini", askKey); err != nil {
			t.Fatal(err)
		}
	}
	f.rt = runtime.New(st, func(h store.Host, _, _ int) (session.Bridge, error) {
		b := newFakeBridge()
		f.bmu.Lock()
		f.bridges[h.ID] = b
		f.bmu.Unlock()
		return b, nil
	})
	f.logPath = filepath.Join(dir, "keel.log")
	f.logs = applog.New(f.logPath)
	webFS := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	factory := func(id, key string) (provider.Provider, error) {
		if key != askKey {
			t.Errorf("provider built with key %q", key)
		}
		return f.fake, nil
	}
	f.srv = httptest.NewServer(New(st, f.rt, fs.FS(webFS), nil).WithLogs(f.logs).WithAgentProvider(factory, true).WithAgentJudge(func(string) (provider.Judge, error) { return f.judge, nil }).Handler())
	t.Cleanup(f.srv.Close)
	return f
}

// bridge waits for the session's runLoop (a goroutine started by Spawn) to
// dial the fake bridge for hostID. POST /api/sessions returns before that
// happens, so reading the map straight after spawn races.
func (f *askFixture) bridge(t *testing.T, hostID string) *fakeBridge {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.bmu.Lock()
		b := f.bridges[hostID]
		f.bmu.Unlock()
		if b != nil {
			return b
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session for host %s never opened its bridge", hostID)
	return nil
}

// open spawns a session on host and prints text into it.
func (f *askFixture) open(t *testing.T, hostID, text string) runtime.Info {
	t.Helper()
	info := spawn(t, f.srv.URL, hostID)
	b := f.bridge(t, hostID)
	go func() { _, _ = b.outW.Write([]byte(text)) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := f.rt.ReadScrollback(info.ID)
		if strings.Contains(string(out), strings.SplitN(text, "\n", 2)[0]) {
			return info
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pane text never reached the ring")
	return info
}

func (f *askFixture) ask(t *testing.T, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(f.srv.URL+"/api/agent/ask", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func (f *askFixture) requests(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, r := range f.fake.Requests() {
		b.WriteString(r.System.String())
		for _, m := range r.Messages {
			b.WriteString(m.Text.String())
		}
	}
	return b.String()
}

func (f *askFixture) logEntries(kind string) []applog.Entry {
	var out []applog.Entry
	for _, e := range f.logs.Recent(2000) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func proposalsOf(t *testing.T, resp map[string]any) []map[string]any {
	t.Helper()
	list, _ := resp["proposals"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, p.(map[string]any))
	}
	return out
}

const askConv = "conv-route-0001"

// The ask route reads only the panes asked for, on readable hosts, logs
// each read, and sends only redacted text with no vault value.
func TestAgentAskRoute(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "uptime-out 3 days\n"+askPaneToken+"\nmy pass is "+askHostPass+"\n")
	off := f.open(t, f.offID, "SECRET-BOX-MARKER\n")
	other := f.open(t, f.prodID, "NOT-ASKED-MARKER\n")
	_ = other

	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "what is up", "panes": []string{web.ID, off.ID}})
	if code != http.StatusOK {
		t.Fatalf("ask → %d %v", code, resp)
	}
	body := f.requests(t)
	for _, bad := range []string{"SECRET-BOX-MARKER", "NOT-ASKED-MARKER", "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK", askHostPass, askKey, "other-pass-1234", "third-pass-5678"} {
		if strings.Contains(body, bad) {
			t.Errorf("provider request holds %q", bad)
		}
	}
	if !strings.Contains(body, "uptime-out 3 days") {
		t.Error("the readable pane's text is missing from the request")
	}
	trace, _ := resp["trace"].([]any)
	if len(trace) != 2 || trace[0].(map[string]any)["status"] != "read" || trace[1].(map[string]any)["reason"] != "not-readable" {
		t.Fatalf("trace = %v", trace)
	}
	reads := f.logEntries(agent.EventRead)
	if len(reads) != 1 || reads[0].SessionID != web.ID || len(reads[0].Redactions) == 0 {
		t.Fatalf("agent.read entries = %+v", reads)
	}
	props := proposalsOf(t, resp)
	if len(props) != 1 || props[0]["sessionId"] != web.ID || props[0]["risk"] != "readonly" || props[0]["bytes"] != "\x15echo keel-agent-ok\n" {
		t.Fatalf("proposals = %v", props)
	}
	if resp["testProvider"] != true {
		t.Error("testProvider flag missing")
	}
	// keel.log holds counts, never content or secrets.
	raw, _ := os.ReadFile(f.logPath)
	for _, bad := range []string{"uptime-out", "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK", askHostPass, askKey} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("keel.log holds %q", bad)
		}
	}
}

func TestAgentAskErrors(t *testing.T) {
	f := newAskFixture(t, false)
	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "hi"})
	if code != http.StatusPreconditionFailed || resp["reason"] != "no-key" {
		t.Fatalf("no key → %d %v, want 412 no-key", code, resp)
	}
	if len(f.fake.Requests()) != 0 {
		t.Fatal("provider called without a key")
	}
	if err := f.st.SetAgentKey("gemini", askKey); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.ask(t, map[string]any{"conversationId": "x", "question": "hi"}); code != http.StatusBadRequest {
		t.Fatalf("bad conversation id → %d", code)
	}
	res, err := http.Post(f.srv.URL+"/api/agent/ask", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json → %d", res.StatusCode)
	}
	// Edit and reset.
	web := f.open(t, f.webID, "hello\n")
	_, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "go", "panes": []string{web.ID}})
	pid := proposalsOf(t, resp)[0]["id"].(string)
	req, _ := http.NewRequest("PUT", f.srv.URL+"/api/agent/conversations/"+askConv+"/proposals/"+pid, strings.NewReader(`{"command":"rm -rf /tmp/x"}`))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var ed map[string]any
	_ = json.NewDecoder(res.Body).Decode(&ed)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || ed["risk"] != "destructive" || ed["needsShift"] != true {
		t.Fatalf("edit → %d %v", res.StatusCode, ed)
	}
	req, _ = http.NewRequest("DELETE", f.srv.URL+"/api/agent/conversations/"+askConv, nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	req, _ = http.NewRequest("PUT", f.srv.URL+"/api/agent/conversations/"+askConv+"/proposals/"+pid, strings.NewReader(`{"command":"ls"}`))
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("edit after reset → %d, want 404", res.StatusCode)
	}
}

// typeOverSocket runs the panel's Type into pane: challenge, then inject
// with the agent marker. It returns the agent-type reply.
func typeOverSocket(t *testing.T, conn *websocket.Conn, bytes, proposalID string, shift bool) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": bytes}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, conn, "inject-challenge")
	nonce, _ := chal["nonce"].(string)
	if err := conn.WriteJSON(map[string]any{"type": "inject", "command": bytes, "nonce": nonce, "agent": map[string]any{"conversationId": askConv, "proposalId": proposalID, "shift": shift}}); err != nil {
		t.Fatal(err)
	}
	return waitJSON(t, conn, "agent-type")
}

func readPTY(b *fakeBridge) <-chan string {
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := b.inR.Read(buf)
		got <- string(buf[:n])
	}()
	return got
}

// §6.1, §6.5, §6.6, §6.7 over the real session socket: only the driver's
// approved Type reaches the PTY; destructive needs ⇧; each type is logged.
func TestAgentTypeOverSessionSocket(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "ready\n")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	if r := waitJSON(t, driver, "ready"); r["you"].(map[string]any)["role"] != "driver" {
		t.Fatalf("first client role = %v", r["you"])
	}
	follower := dialSession(t, f.srv.URL, web.ID, "other", "web")
	defer follower.Close()
	if r := waitJSON(t, follower, "ready"); r["you"].(map[string]any)["role"] == "driver" {
		t.Fatal("second client drives")
	}

	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	ro := proposalsOf(t, resp)[0]
	_, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "delete it", "panes": []string{web.ID}})
	de := proposalsOf(t, resp)[0]
	if de["risk"] != "destructive" {
		t.Fatalf("delete proposal = %v", de)
	}
	bridge := f.bridge(t, f.webID)

	// Follower: the session issues no ticket (the challenge goes
	// unanswered), and an inject marked as agent output is refused by the
	// agent before the ticket is even checked. Nothing reaches the PTY.
	got := readPTY(bridge)
	if err := follower.WriteJSON(map[string]any{"type": "inject-challenge", "command": ro["bytes"]}); err != nil {
		t.Fatal(err)
	}
	if err := follower.WriteJSON(map[string]any{"type": "inject", "command": ro["bytes"], "nonce": "", "agent": map[string]any{"conversationId": askConv, "proposalId": ro["id"]}}); err != nil {
		t.Fatal(err)
	}
	if r := waitJSON(t, follower, "agent-type"); r["ok"] != false || !strings.Contains(r["message"].(string), "follower") {
		t.Fatalf("follower type → %v", r)
	}
	select {
	case leak := <-got:
		t.Fatalf("follower's type reached the PTY: %q", leak)
	case <-time.After(100 * time.Millisecond):
	}

	// Destructive without ⇧: refused before the write.
	if r := typeOverSocket(t, driver, de["bytes"].(string), de["id"].(string), false); r["ok"] != false || !strings.Contains(r["message"].(string), "Shift") {
		t.Fatalf("destructive without shift → %v", r)
	}
	select {
	case leak := <-got:
		t.Fatalf("unshifted destructive reached the PTY: %q", leak)
	case <-time.After(100 * time.Millisecond):
	}
	// Bytes other than the proposal's: refused.
	if r := typeOverSocket(t, driver, "echo other\n", ro["id"].(string), false); r["ok"] != false {
		t.Fatalf("changed bytes → %v", r)
	}
	if n := len(f.logEntries(agent.EventType)); n != 0 {
		t.Fatalf("refused types logged: %d", n)
	}

	// The driver's approved read-only type goes through and is logged.
	if r := typeOverSocket(t, driver, ro["bytes"].(string), ro["id"].(string), false); r["ok"] != true {
		t.Fatalf("driver type → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15echo keel-agent-ok\n" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the approved type never reached the PTY")
	}
	// Destructive with ⇧ goes through.
	got = readPTY(bridge)
	if r := typeOverSocket(t, driver, de["bytes"].(string), de["id"].(string), true); r["ok"] != true {
		t.Fatalf("shifted destructive → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15rm -rf /tmp/keel-agent-e2e\n" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the shifted type never reached the PTY")
	}
	types := f.logEntries(agent.EventType)
	if len(types) != 2 || types[0].SessionID != web.ID {
		t.Fatalf("agent.type entries = %+v", types)
	}
}

// §6.1 at the source: the agent routes cannot write to a session. The file
// that holds them calls no write, inject, spawn or resize method, and
// touches the runtime only through Get and ReadScrollback.
func TestAgentRoutesHaveNoWritePath(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "agentask.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{"Write": true, "Inject": true, "InjectTicket": true, "IssueInjectTicket": true, "Keystroke": true, "Spawn": true, "Resize": true, "SetFocus": true, "Kill": true}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if banned[sel.Sel.Name] {
			t.Errorf("agentask.go uses .%s", sel.Sel.Name)
		}
		if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "rt" && sel.Sel.Name != "Get" && sel.Sel.Name != "ReadScrollback" {
			t.Errorf("agentask.go calls rt.%s", sel.Sel.Name)
		}
		return true
	})
	// And the ask handler does not reach the session socket path.
	src, _ := os.ReadFile("agentask.go")
	if strings.Contains(string(src), "inject.") {
		t.Error("agentask.go uses the inject package")
	}
}

// Settings, Delete all history: DELETE /api/agent/conversations drops every
// conversation, so the next ask starts from one message, and writes one log
// entry that holds a count and no content.
func TestAgentDeleteAllHistory(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "history-marker-text\n")
	for _, conv := range []string{askConv, "conv-0002-other"} {
		for i := 0; i < 2; i++ {
			if code, resp := f.ask(t, map[string]any{"conversationId": conv, "question": "history question " + conv, "panes": []string{web.ID}}); code != http.StatusOK {
				t.Fatalf("ask → %d %v", code, resp)
			}
		}
	}
	reqs := f.fake.Requests()
	if n := len(reqs[len(reqs)-1].Messages); n < 3 {
		t.Fatalf("setup: conversation holds %d messages, want at least 3", n)
	}
	req, _ := http.NewRequest("DELETE", f.srv.URL+"/api/agent/conversations", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /api/agent/conversations → %d", res.StatusCode)
	}
	if code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "fresh", "panes": []string{web.ID}}); code != http.StatusOK {
		t.Fatalf("ask after delete → %d %v", code, resp)
	}
	reqs = f.fake.Requests()
	if n := len(reqs[len(reqs)-1].Messages); n != 1 {
		t.Fatalf("after delete the request carried %d messages, want 1", n)
	}
	var note []applog.Entry
	for _, e := range f.logs.Recent(2000) {
		if e.Kind == "agent" && strings.HasPrefix(e.Message, "agent history deleted") {
			note = append(note, e)
		}
	}
	if len(note) != 1 || note[0].Message != "agent history deleted (2 conversations)" {
		t.Fatalf("log entries = %+v", note)
	}
	raw, _ := os.ReadFile(f.logPath)
	if strings.Contains(string(raw), "history question") || strings.Contains(string(raw), "history-marker-text") {
		t.Error("keel.log holds conversation content")
	}
}
