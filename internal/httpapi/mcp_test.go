package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/authtoken"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

const askMCPToken = "mcp-token-for-tests-0123456789abcdef0123456789abcdef0123456789ab"

// mcpDo sends one request to an /api/mcp/ route with keel mcp's token
// (token "" sends none) and decodes the answer.
func mcpDo(t *testing.T, base, method, path, token string, body any, extra map[string]string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(authtoken.MCPHeader, token)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

// allowClaudeCode turns on "Allow Claude Code (keel mcp)", which /api/mcp/
// needs (off by default).
func allowClaudeCode(t *testing.T, st *store.Store) {
	t.Helper()
	set, err := st.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	set.ClaudeCode = true
	if _, err := st.SetAgentSettings(set); err != nil {
		t.Fatal(err)
	}
}

// keel mcp's token opens /api/mcp/ and nothing else; the UI's token and
// cookie do not open /api/mcp/; a request with an Origin (a page) is
// refused there even with the token.
func TestMCPRoutesTakeOnlyTheirOwnToken(t *testing.T) {
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return newFakeBridge(), nil })
	logs := applog.New(filepath.Join(dir, "keel.log"))
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithLogs(logs).WithAuth(testToken).WithMCPAuth(askMCPToken).Handler())
	t.Cleanup(srv.Close)
	allowClaudeCode(t, st)

	if code, _ := mcpDo(t, srv.URL, "GET", "/api/mcp/panes", askMCPToken, nil, nil); code != 200 {
		t.Fatalf("mcp token on /api/mcp/panes → %d", code)
	}
	if code, _ := mcpDo(t, srv.URL, "GET", "/api/mcp/panes", "", nil, nil); code != 401 {
		t.Fatalf("no token → %d", code)
	}
	if code, _ := mcpDo(t, srv.URL, "GET", "/api/mcp/panes", "", nil, map[string]string{authtoken.Header: testToken}); code != 401 {
		t.Fatalf("UI token on /api/mcp/ → %d", code)
	}
	if code, _ := mcpDo(t, srv.URL, "GET", "/api/mcp/panes", askMCPToken[:len(askMCPToken)-1]+"x", nil, nil); code != 401 {
		t.Fatalf("wrong mcp token → %d", code)
	}
	if code, _ := mcpDo(t, srv.URL, "GET", "/api/mcp/panes", askMCPToken, nil, map[string]string{"Origin": srv.URL}); code != 401 {
		t.Fatalf("mcp token with an Origin → %d", code)
	}
	// The mcp token, sent as either header, opens no other route.
	for _, path := range []string{"/api/hosts", "/api/identities", "/api/agent/settings", "/api/agent/conversations", "/api/logs"} {
		if code, _ := mcpDo(t, srv.URL, "GET", path, askMCPToken, nil, nil); code != 401 {
			t.Fatalf("mcp token on %s → %d", path, code)
		}
		if code, _ := mcpDo(t, srv.URL, "GET", path, "", nil, map[string]string{authtoken.Header: askMCPToken}); code != 401 {
			t.Fatalf("mcp token as the UI token on %s → %d", path, code)
		}
	}
	// /api/health proves the mcp token for a nonce, never sends it.
	res, err := http.Get(srv.URL + "/api/health?mcpNonce=abc123")
	if err != nil {
		t.Fatal(err)
	}
	var h map[string]any
	_ = json.NewDecoder(res.Body).Decode(&h)
	res.Body.Close()
	if !authtoken.CheckProof(askMCPToken, "abc123", h["mcpProof"].(string)) {
		t.Fatalf("health = %v", h)
	}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), askMCPToken) {
		t.Fatal("health leaks the mcp token")
	}
}

// With no mcp token set, /api/mcp/ answers nothing.
func TestMCPRoutesClosedWithoutToken(t *testing.T) {
	f := newAgentFixture(t, testToken)
	if code, _ := mcpDo(t, f.srv.URL, "GET", "/api/mcp/panes", askMCPToken, nil, nil); code != 401 {
		t.Fatalf("no mcp token configured → %d", code)
	}
}

// §6 for keel mcp: it lists and reads only readable hosts' panes,
// redacted and logged (Source claude-code); a proposal waits in the
// "Claude Code" conversation and reaches the PTY only through the driver's
// approved Type; nothing calls a provider.
func TestMCPListReadPropose(t *testing.T) {
	f := newAskFixture(t, true)
	allowClaudeCode(t, f.st)
	web := f.open(t, f.webID, "uptime-out 3 days\n"+askPaneToken+"\nmy pass is "+askHostPass+"\n")
	off := f.open(t, f.offID, "SECRET-BOX-MARKER\n")
	prod := f.open(t, f.prodID, "prod ready\n")

	code, list := mcpDo(t, f.srv.URL, "GET", "/api/mcp/panes", askMCPToken, nil, nil)
	if code != 200 {
		t.Fatalf("panes %d %v", code, list)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), off.ID) || strings.Contains(string(raw), "secret-box") || list["notAllowed"] != float64(1) {
		t.Fatalf("panes = %s", raw)
	}
	if !strings.Contains(string(raw), web.ID) || !strings.Contains(string(raw), prod.ID) || !strings.Contains(string(raw), `"host":"web-1"`) {
		t.Fatalf("panes = %s", raw)
	}

	code, ex := mcpDo(t, f.srv.URL, "POST", "/api/mcp/read", askMCPToken, map[string]any{"paneId": web.ID, "lines": 50}, nil)
	if code != 200 {
		t.Fatalf("read %d %v", code, ex)
	}
	text, _ := ex["text"].(string)
	if !strings.Contains(text, "uptime-out 3 days") || strings.Contains(text, "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK") || strings.Contains(text, askHostPass) {
		t.Fatalf("read text = %q", text)
	}
	reads := f.logEntries(agent.EventRead)
	if len(reads) != 1 || reads[0].SessionID != web.ID || reads[0].Source != service.ExternalSource || !strings.Contains(reads[0].Message, "for claude-code") {
		t.Fatalf("agent.read entries = %+v", reads)
	}
	if code, out := mcpDo(t, f.srv.URL, "POST", "/api/mcp/read", askMCPToken, map[string]any{"paneId": off.ID}, nil); code != 403 {
		t.Fatalf("read of an unreadable host → %d %v", code, out)
	}
	if code, _ := mcpDo(t, f.srv.URL, "POST", "/api/mcp/read", askMCPToken, map[string]any{"paneId": "no-such-pane"}, nil); code != 404 {
		t.Fatalf("read of a closed pane → %d", code)
	}
	if n := len(f.logEntries(agent.EventRead)); n != 1 {
		t.Fatalf("refused reads logged: %d entries", n)
	}

	// Propose: kept, rated by Keel, typed only by the driver's approval.
	code, prop := mcpDo(t, f.srv.URL, "POST", "/api/mcp/propose", askMCPToken, map[string]any{"paneId": web.ID, "command": "echo keel-mcp-ok", "why": "says hello"}, nil)
	if code != 200 || prop["risk"] != "readonly" || prop["needsShift"] != false || prop["conversation"] != service.ExternalTitle {
		t.Fatalf("propose %d %v", code, prop)
	}
	pid := prop["proposalId"].(string)
	if code, st := mcpDo(t, f.srv.URL, "GET", "/api/mcp/proposals/"+pid, askMCPToken, nil, nil); code != 200 || st["typed"] != false {
		t.Fatalf("status before type %d %v", code, st)
	}
	code, dprop := mcpDo(t, f.srv.URL, "POST", "/api/mcp/propose", askMCPToken, map[string]any{"paneId": prod.ID, "command": "systemctl restart postgresql"}, nil)
	if code != 200 || dprop["needsShift"] != true {
		t.Fatalf("production state change %d %v", code, dprop)
	}
	if code, _ := mcpDo(t, f.srv.URL, "POST", "/api/mcp/propose", askMCPToken, map[string]any{"paneId": off.ID, "command": "ls"}, nil); code != 403 {
		t.Fatalf("propose on an unreadable host → %d", code)
	}
	if code, _ := mcpDo(t, f.srv.URL, "POST", "/api/mcp/propose", askMCPToken, map[string]any{"paneId": web.ID, "command": "echo \x1b[31m"}, nil); code != 400 {
		t.Fatalf("control characters → %d", code)
	}
	if n := len(f.logEntries(agent.EventType)); n != 0 {
		t.Fatalf("a proposal was typed by itself: %d agent.type entries", n)
	}

	// The proposal is in the agent panel's "Claude Code" conversation.
	res, err := http.Get(f.srv.URL + "/api/agent/conversations/" + service.ExternalConversationID)
	if err != nil {
		t.Fatal(err)
	}
	var tr map[string]any
	_ = json.NewDecoder(res.Body).Decode(&tr)
	res.Body.Close()
	if tr["title"] != service.ExternalTitle {
		t.Fatalf("transcript = %v", tr)
	}
	turns, _ := tr["turns"].([]any)
	if len(turns) != 2 {
		t.Fatalf("transcript turns = %v", tr["turns"])
	}

	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	if r := waitJSON(t, driver, "ready"); r["you"].(map[string]any)["role"] != "driver" {
		t.Fatalf("role = %v", r["you"])
	}
	got := readPTY(f.bridge(t, f.webID))
	if r := typeExternal(t, driver, "\x15echo keel-mcp-ok\n", pid); r["ok"] != true {
		t.Fatalf("driver type → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15echo keel-mcp-ok\n" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the approved type never reached the PTY")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		code, st := mcpDo(t, f.srv.URL, "GET", "/api/mcp/proposals/"+pid, askMCPToken, nil, nil)
		if code == 200 && st["typed"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status after type %d %v", code, st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code, _ := mcpDo(t, f.srv.URL, "GET", "/api/mcp/proposals/p_nosuch", askMCPToken, nil, nil); code != 404 {
		t.Fatalf("unknown proposal → %d", code)
	}
	if n := len(f.fake.Requests()); n != 0 {
		t.Fatalf("keel mcp called the model provider %d times", n)
	}
	suggests := f.logEntries(agent.EventSuggest)
	if len(suggests) != 2 || suggests[0].Source != service.ExternalSource {
		t.Fatalf("agent.suggest entries = %+v", suggests)
	}
}

// §6 rule 14: until the user turns on "Allow Claude Code (keel mcp)"
// (off by default), every /api/mcp/ route is refused with keel mcp's
// token, before anything is read, proposed or logged; /api/health still
// proves the token so keel mcp can say Claude Code is off. Settings · Agent
// turns it on through PUT /api/agent/settings, and off again.
func TestMCPRoutesRefusedUntilClaudeCodeIsOn(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "uptime-out 3 days\n")

	settings := func(body string) map[string]any {
		t.Helper()
		method := "GET"
		var rd io.Reader
		if body != "" {
			method, rd = "PUT", strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, f.srv.URL+"/api/agent/settings", rd)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("%s settings → %d %v", method, res.StatusCode, out)
		}
		return out["settings"].(map[string]any)
	}
	if got := settings(""); got["claudeCode"] != false {
		t.Fatalf("claudeCode by default = %v", got["claudeCode"])
	}

	routes := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/mcp/panes", nil},
		{"POST", "/api/mcp/read", map[string]any{"paneId": web.ID, "lines": 50}},
		{"POST", "/api/mcp/propose", map[string]any{"paneId": web.ID, "command": "echo hi"}},
		{"GET", "/api/mcp/proposals/p_any", nil},
		{"GET", "/api/mcp/unknown", nil},
	}
	refused := func(when string) {
		t.Helper()
		for _, r := range routes {
			code, out := mcpDo(t, f.srv.URL, r.method, r.path, askMCPToken, r.body, nil)
			if code != http.StatusForbidden || out["error"] != errClaudeCodeOff {
				t.Fatalf("%s: %s %s → %d %v", when, r.method, r.path, code, out)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "uptime-out") || strings.Contains(string(raw), web.ID) {
				t.Fatalf("%s: %s answered with pane data: %s", when, r.path, raw)
			}
		}
		for _, kind := range []string{agent.EventRead, agent.EventSuggest} {
			if n := len(f.logEntries(kind)); n != 0 {
				t.Fatalf("%s: %d %s entries", when, n, kind)
			}
		}
	}
	refused("off by default")

	// The proof still answers, so keel mcp gets the refusal above rather
	// than "not proven".
	res, err := http.Get(f.srv.URL + "/api/health?mcpNonce=n0nce")
	if err != nil {
		t.Fatal(err)
	}
	var h map[string]any
	_ = json.NewDecoder(res.Body).Decode(&h)
	res.Body.Close()
	if p, _ := h["mcpProof"].(string); !authtoken.CheckProof(askMCPToken, "n0nce", p) {
		t.Fatalf("health = %v", h)
	}

	// A PUT that does not name the field keeps it off.
	if got := settings(`{"historyDays":7}`); got["claudeCode"] != false {
		t.Fatalf("claudeCode after an unrelated PUT = %v", got["claudeCode"])
	}
	refused("after an unrelated PUT")

	if got := settings(`{"claudeCode":true}`); got["claudeCode"] != true {
		t.Fatalf("claudeCode after turning on = %v", got["claudeCode"])
	}
	if code, out := mcpDo(t, f.srv.URL, "GET", "/api/mcp/panes", askMCPToken, nil, nil); code != 200 || !strings.Contains(fmt.Sprint(out), web.ID) {
		t.Fatalf("panes while on → %d %v", code, out)
	}
	// While on, the token is still required.
	if code, _ := mcpDo(t, f.srv.URL, "GET", "/api/mcp/panes", "", nil, nil); code != 401 {
		t.Fatalf("no token while on → %d", code)
	}

	if got := settings(`{"claudeCode":false}`); got["claudeCode"] != false {
		t.Fatalf("claudeCode after turning off = %v", got["claudeCode"])
	}
	before := len(f.logEntries(agent.EventRead))
	if code, out := mcpDo(t, f.srv.URL, "POST", "/api/mcp/read", askMCPToken, map[string]any{"paneId": web.ID}, nil); code != http.StatusForbidden || out["error"] != errClaudeCodeOff {
		t.Fatalf("read after turning off → %d %v", code, out)
	}
	if n := len(f.logEntries(agent.EventRead)); n != before {
		t.Fatalf("a refused read was logged: %d → %d", before, n)
	}
}

// typeExternal is the panel's Type into pane for a "Claude Code" proposal.
func typeExternal(t *testing.T, conn *websocket.Conn, bytes, proposalID string) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": bytes}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, conn, "inject-challenge")
	nonce, _ := chal["nonce"].(string)
	if err := conn.WriteJSON(map[string]any{"type": "inject", "command": bytes, "nonce": nonce, "agent": map[string]any{"conversationId": service.ExternalConversationID, "proposalId": proposalID}}); err != nil {
		t.Fatal(err)
	}
	return waitJSON(t, conn, "agent-type")
}
