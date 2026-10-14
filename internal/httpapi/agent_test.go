package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"keel/internal/applog"
	"keel/internal/authtoken"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

type agentFixture struct {
	srv  *httptest.Server
	st   *store.Store
	dir  string
	logs *applog.Book
}

func newAgentFixture(t *testing.T, token string) agentFixture {
	t.Helper()
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
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithLogs(logs).WithAuth(token).Handler())
	t.Cleanup(srv.Close)
	return agentFixture{srv: srv, st: st, dir: dir, logs: logs}
}

func (f agentFixture) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authtoken.Header, testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func TestAgentSettingsAPI(t *testing.T) {
	f := newAgentFixture(t, testToken)
	code, b := f.do(t, "GET", "/api/agent/settings", nil)
	if code != 200 {
		t.Fatalf("GET %d %s", code, b)
	}
	var got agentSettingsResp
	_ = json.Unmarshal(b, &got)
	if got.Settings != store.DefaultAgentSettings() || got.Keys["gemini"].Configured || got.Keys["jev"].Configured {
		t.Fatalf("defaults = %s", b)
	}
	// Partial update; secrets cannot be turned off; lines are clamped.
	code, b = f.do(t, "PUT", "/api/agent/settings", map[string]any{
		"scrollbackLines": 9999, "redact": map[string]bool{"secrets": false, "ips": true}, "defaultScope": "tab",
	})
	if code != 200 {
		t.Fatalf("PUT %d %s", code, b)
	}
	_ = json.Unmarshal(b, &got)
	s := got.Settings
	if s.ScrollbackLines != 1000 || !s.Redact.Secrets || !s.Redact.IPs || s.DefaultScope != "tab" || s.Detection != "auto" || s.Provider != "gemini" {
		t.Fatalf("saved = %+v", s)
	}
	for _, bad := range []map[string]any{
		{"detection": "sometimes"}, {"defaultScope": "all"}, {"provider": "openai"}, {"provider": "jev"}, {"model": "a/b"},
	} {
		if code, b := f.do(t, "PUT", "/api/agent/settings", bad); code != 400 {
			t.Fatalf("%v → %d %s", bad, code, b)
		}
	}
	// A stray key in the settings body never lands in keel.json.
	if code, _ := f.do(t, "PUT", "/api/agent/settings", map[string]any{"apiKey": "AIza-stray-key-value"}); code != 200 {
		t.Fatal(code)
	}
	raw, _ := os.ReadFile(filepath.Join(f.dir, "keel.json"))
	if bytes.Contains(raw, []byte("AIza-stray-key-value")) {
		t.Fatal("stray key in keel.json")
	}
	if !bytes.Contains(raw, []byte(`"scrollbackLines": 1000`)) {
		t.Fatalf("settings not in keel.json: %s", raw)
	}
}

func TestAgentProvidersAPI(t *testing.T) {
	f := newAgentFixture(t, testToken)
	code, b := f.do(t, "GET", "/api/agent/providers", nil)
	if code != 200 {
		t.Fatal(code)
	}
	var list []agentProviderResp
	_ = json.Unmarshal(b, &list)
	if len(list) < 2 || list[0].ID != "gemini" || list[0].Configured || !strings.Contains(list[0].Warning, "may use") || list[0].TermsURL == "" {
		t.Fatalf("providers = %s", b)
	}
}

// Keys: set and clear through the API, encrypted in the vault, and never
// in keel.json, keel.log, or any API response.
func TestAgentKeysNeverLeave(t *testing.T) {
	f := newAgentFixture(t, testToken)
	gem := "AIzaSy" + strings.Repeat("Q", 33)
	jev := "jev-" + strings.Repeat("7", 30)
	hostPass := "host-pass-" + strings.Repeat("h", 10)
	identKey := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + strings.Repeat("K", 64) + "\n-----END OPENSSH PRIVATE KEY-----"
	if _, err := f.st.UpsertHost(store.Host{Name: "h", Address: "h", Username: "u", AuthType: "password", Password: hostPass}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.UpsertIdentity(store.Identity{Name: "k", Kind: "key", PrivateKey: identKey}); err != nil {
		t.Fatal(err)
	}
	if code, b := f.do(t, "PUT", "/api/agent/keys/gemini", map[string]string{"key": gem}); code != 200 || !bytes.Contains(b, []byte(`"configured":true`)) {
		t.Fatalf("%d %s", code, b)
	}
	if code, _ := f.do(t, "PUT", "/api/agent/keys/jev", map[string]string{"key": jev}); code != 200 {
		t.Fatal(code)
	}
	for _, c := range []struct {
		path string
		body any
		want int
	}{
		{"/api/agent/keys/openai", map[string]string{"key": "x"}, 404},
		{"/api/agent/keys/gemini", map[string]string{"key": ""}, 400},
		{"/api/agent/keys/gemini", map[string]string{"key": "two words"}, 400},
	} {
		if code, b := f.do(t, "PUT", c.path, c.body); code != c.want {
			t.Fatalf("%s → %d %s", c.path, code, b)
		}
	}
	secrets := []string{gem, jev, hostPass, strings.Repeat("K", 64)}
	var all bytes.Buffer
	for _, path := range []string{
		"/api/agent/settings", "/api/agent/providers", "/api/hosts", "/api/identities", "/api/groups",
		"/api/snippets", "/api/notebooks", "/api/tunnels", "/api/logs", "/api/vault", "/api/sessions", "/api/health",
	} {
		code, b := f.do(t, "GET", path, nil)
		if code != 200 {
			t.Fatalf("GET %s → %d %s", path, code, b)
		}
		all.Write(b)
	}
	code, b := f.do(t, "GET", "/api/agent/settings", nil)
	if code != 200 || !bytes.Contains(b, []byte(`"gemini":{"configured":true}`)) || !bytes.Contains(b, []byte(`"jev":{"configured":true}`)) {
		t.Fatalf("settings keys = %s", b)
	}
	jsonFile, _ := os.ReadFile(filepath.Join(f.dir, "keel.json"))
	logFile, _ := os.ReadFile(filepath.Join(f.dir, "keel.log"))
	vaultFile, _ := os.ReadFile(filepath.Join(f.dir, "keel.vault"))
	for _, sec := range secrets {
		if bytes.Contains(all.Bytes(), []byte(sec)) {
			t.Fatalf("an API response carries a secret (%.8s…)", sec)
		}
		for name, data := range map[string][]byte{"keel.json": jsonFile, "keel.log": logFile, "keel.vault": vaultFile} {
			if bytes.Contains(data, []byte(sec)) {
				t.Fatalf("%s carries a secret in plain text (%.8s…)", name, sec)
			}
		}
	}
	if !bytes.Contains(logFile, []byte("agent key saved for Google Gemini")) {
		t.Fatalf("key change not logged: %s", logFile)
	}
	// The key is there for keel serve to use.
	if k, err := f.st.AgentKey("gemini"); err != nil || k != gem {
		t.Fatal("key not stored")
	}
	if code, b := f.do(t, "DELETE", "/api/agent/keys/gemini", nil); code != 200 || !bytes.Contains(b, []byte(`"configured":false`)) {
		t.Fatalf("%d %s", code, b)
	}
	if ok, _ := f.st.AgentKeyConfigured("gemini"); ok {
		t.Fatal("key not cleared")
	}
}

func TestAgentRoutesNeedAuthAndUnlockedVault(t *testing.T) {
	f := newAgentFixture(t, testToken)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/agent/settings"}, {"PUT", "/api/agent/settings"}, {"GET", "/api/agent/providers"},
		{"PUT", "/api/agent/keys/gemini"}, {"DELETE", "/api/agent/keys/gemini"},
		{"POST", "/api/agent/ask"}, {"PUT", "/api/agent/conversations/conv-0001/proposals/p_1"}, {"DELETE", "/api/agent/conversations/conv-0001"},
	} {
		req, _ := http.NewRequest(c.method, f.srv.URL+c.path, strings.NewReader(`{"key":"abc"}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s without token → %d", c.method, c.path, resp.StatusCode)
		}
	}
	// Reopen the same data dir with no passphrase and no keychain: locked.
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	st, err := store.New(filepath.Join(f.dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Locked() {
		t.Fatal("expected a locked vault")
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return newFakeBridge(), nil })
	srv := httptest.NewServer(New(st, rt, fstest.MapFS{}, nil).Handler())
	defer srv.Close()
	req, _ := http.NewRequest("PUT", srv.URL+"/api/agent/keys/gemini", strings.NewReader(`{"key":"abc"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("locked vault → %d, want 423", resp.StatusCode)
	}
	// The ask route too: nothing is read or sent while the vault is locked.
	resp, err = http.Post(srv.URL+"/api/agent/ask", "application/json", strings.NewReader(`{"conversationId":"conv-0001","question":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("ask with a locked vault → %d, want 423", resp.StatusCode)
	}
}

// Host API: the two agent switches are always returned; old clients that
// do not send them keep the saved values; production turns on when a host
// is created in or moved into a group named production.
func TestHostAgentFlagsAPI(t *testing.T) {
	f := newAgentFixture(t, testToken)
	type hostResp struct {
		ID            string `json:"id"`
		AgentReadable *bool  `json:"agentReadable"`
		Production    *bool  `json:"production"`
	}
	decode := func(b []byte) hostResp {
		var h hostResp
		if err := json.Unmarshal(b, &h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	oldClient := map[string]any{"name": "web", "address": "web", "username": "u", "authType": "password", "password": "pw-123456"}
	code, b := f.do(t, "POST", "/api/hosts", oldClient)
	if code != 201 {
		t.Fatalf("%d %s", code, b)
	}
	h := decode(b)
	if h.AgentReadable == nil || !*h.AgentReadable || h.Production == nil || *h.Production {
		t.Fatalf("new host = %s", b)
	}
	code, b = f.do(t, "PUT", "/api/hosts/"+h.ID, map[string]any{"name": "web", "address": "web", "username": "u", "authType": "password", "agentReadable": false, "production": true})
	if code != 200 || !bytes.Contains(b, []byte(`"agentReadable":false`)) || !bytes.Contains(b, []byte(`"production":true`)) {
		t.Fatalf("%d %s", code, b)
	}
	code, b = f.do(t, "PUT", "/api/hosts/"+h.ID, oldClient)
	if code != 200 || !bytes.Contains(b, []byte(`"agentReadable":false`)) || !bytes.Contains(b, []byte(`"production":true`)) {
		t.Fatalf("old client save changed flags: %d %s", code, b)
	}
	// Move into production.
	code, b = f.do(t, "POST", "/api/groups", map[string]any{"name": "Production"})
	if code != 201 {
		t.Fatal(code)
	}
	var g store.Group
	_ = json.Unmarshal(b, &g)
	code, b = f.do(t, "POST", "/api/hosts", map[string]any{"name": "db", "address": "db", "username": "u", "authType": "password"})
	if code != 201 {
		t.Fatalf("%d %s", code, b)
	}
	db := decode(b)
	if code, _ := f.do(t, "POST", "/api/hosts/move", map[string]any{"ids": []string{db.ID}, "groupId": g.ID}); code != 204 {
		t.Fatal(code)
	}
	if code, b = f.do(t, "GET", "/api/hosts", nil); code != 200 {
		t.Fatal(code)
	}
	var hosts []hostResp
	_ = json.Unmarshal(b, &hosts)
	for _, x := range hosts {
		if x.ID == db.ID && (x.Production == nil || !*x.Production) {
			t.Fatalf("moved host not production: %s", b)
		}
	}
	// Created in it.
	code, b = f.do(t, "POST", "/api/hosts", map[string]any{"name": "db2", "address": "db2", "username": "u", "authType": "password", "groupId": g.ID})
	if code != 201 || !*decode(b).Production {
		t.Fatalf("%d %s", code, b)
	}
}

func TestAgentLogEntriesListed(t *testing.T) {
	f := newAgentFixture(t, testToken)
	f.logs.Append(applog.Entry{Kind: "agent.read", Message: "agent read web · last 120 lines · 2 redactions", HostID: "h1", SessionID: "s1", Lines: 120, Redactions: map[string]int{"jwt": 2}})
	code, b := f.do(t, "GET", "/api/logs", nil)
	if code != 200 {
		t.Fatal(code)
	}
	var entries []applog.Entry
	_ = json.Unmarshal(b, &entries)
	if len(entries) == 0 || entries[0].Kind != "agent.read" || entries[0].Lines != 120 || entries[0].Redactions["jwt"] != 2 {
		t.Fatalf("logs = %s", b)
	}
}
