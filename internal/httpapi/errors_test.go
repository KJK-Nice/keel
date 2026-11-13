package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/hostimport"
	"keel/internal/runtime"
	"keel/internal/store"
)

// answerOf is what writeErr sends for err: the status and the decoded body.
func answerOf(t *testing.T, err error) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	writeErr(rec, err)
	var body map[string]any
	if derr := json.Unmarshal(rec.Body.Bytes(), &body); derr != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), derr)
	}
	return rec.Code, body
}

// One policy for every route: the error's type decides the status, the
// reason and the words.
func TestOneErrorPolicy(t *testing.T) {
	wrapped := func(err error) error { return fmt.Errorf("while saving: %w", err) }
	for _, c := range []struct {
		name   string
		err    error
		status int
		words  string
		reason string
	}{
		{"locked", store.ErrLocked, 423, "vault locked", ""},
		{"locked, wrapped", wrapped(store.ErrLocked), 423, "vault locked", ""},
		{"not found", store.ErrNotFound, 404, "not found", ""},
		{"not found, a route's noun", worded(store.ErrNotFound, store.ErrNotFound, "host not found"), 404, "host not found", ""},
		{"words for another error are not used", worded(store.ErrLocked, store.ErrNotFound, "host not found"), 423, "vault locked", ""},
		{"session not found", runtime.ErrNotFound, 404, "session not found", ""},
		{"input", badInput("name is required"), 400, "name is required", ""},
		{"host field", wrapped(store.ErrHostField), 400, "while saving: invalid host", ""},
		{"identity field", store.ErrIdentityField, 400, "invalid identity", ""},
		{"group cycle", store.ErrGroupCycle, 400, store.ErrGroupCycle.Error(), ""},
		{"local shell duplicate", store.ErrLocalDuplicate, 400, "the local shell host cannot be duplicated", ""},
		{"wrong passphrase", store.ErrPassphrase, 403, "incorrect passphrase", ""},
		{"vault exists", store.ErrVaultExists, 409, store.ErrVaultExists.Error(), ""},
		{"history", store.ErrHistoryUnavailable, 409, store.ErrHistoryUnavailable.Error(), "history"},
		{"history, a route's words", worded(store.ErrHistoryUnavailable, store.ErrHistoryUnavailable, "Not saved"), 409, "Not saved", "history"},
		{"notebook conflict", store.ErrNotebookConflict, 409, "The notebook changed since then. Open History on the notebook to restore an earlier version.", "conflict"},
		{"notebook conflict in History", historyWords(store.ErrNotebookConflict), 409, "The notebook changed meanwhile. Look at History again before you restore.", "conflict"},
		{"notebook private", store.ErrNotebookPrivate, 403, "This notebook is private now. Keel did not change it.", "private"},
		{"notes unreadable", wrapped(store.ErrMemoryUnreadable), 409, "while saving: saved notes cannot be read; Delete all notes in Settings · Agent starts a fresh file", ""},
		{"no key", service.ErrNoKey, 412, service.ErrNoKey.Error(), "no-key"},
		{"provider", &service.ProviderError{Msg: "quota"}, 502, "quota", "provider"},
		{"busy", service.ErrBusy, 409, service.ErrBusy.Error(), ""},
		{"expired card", service.ErrNoteExpired, 410, service.ErrNoteExpired.Error(), ""},
		{"nothing to draft", service.ErrNothingToDraft, 422, service.ErrNothingToDraft.Error(), ""},
		{"no group run", service.ErrNoGroupRun, 404, service.ErrNoGroupRun.Error(), ""},
		{"keel mcp, pane not readable", mcpWords(agent.ErrNotReadable), 403, "the agent may not read this pane: turn on \"Agent can read\" for its host in Keel", ""},
		{"keel mcp, no such pane", mcpWords(service.ErrNotFound), 404, "no such pane or proposal (list_panes gives the open panes)", ""},
		{"keel mcp, locked", mcpWords(store.ErrLocked), 423, "vault locked", ""},
		{"import request", &hostimport.RequestError{Msg: "x is not a host"}, 400, "x is not a host", ""},
		{"unknown", errors.New("disk full"), 500, "disk full", ""},
		{"unknown, the route's status", otherwise(errors.New("bind: in use"), 400), 400, "bind: in use", ""},
		{"a known error keeps its status", otherwise(store.ErrLocked, 400), 423, "vault locked", ""},
		{"a known error keeps its status, 2", otherwise(store.ErrNotFound, 409), 404, "not found", ""},
	} {
		status, body := answerOf(t, c.err)
		reason, _ := body["reason"].(string)
		if status != c.status || body["error"] != c.words || reason != c.reason {
			t.Errorf("%s: %d %v, want %d %q reason %q", c.name, status, body, c.status, c.words, c.reason)
		}
	}
}

func TestErrorPolicyCarriesTheRefusedHostKey(t *testing.T) {
	status, body := answerOf(t, &store.UnknownHostError{Host: "h:22", Fingerprint: "SHA256:abc", KeyType: "ssh-ed25519", Token: "t1"})
	if status != 409 || body["fingerprint"] != "SHA256:abc" || body["trustToken"] != "t1" || body["error"] == "" {
		t.Fatalf("%d %v", status, body)
	}
	status, body = answerOf(t, &hostimport.KeyError{Msg: "needs a passphrase", Kind: "passphrase", Path: "~/.ssh/id"})
	if status != 400 || body["errorKind"] != "passphrase" || body["key"] != "~/.ssh/id" || body["error"] != "needs a passphrase" {
		t.Fatalf("%d %v", status, body)
	}
}

// Every handler answers 423 for a locked vault, also the ones that answered
// 500 before (deleteHost, deleteSnippet, upsertNotebook). withVault stops a
// request before it reaches them, so the handlers are called directly.
func TestHandlersAnswerLockedWith423(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	st, err := store.New(t.TempDir()+"/keel.json", store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Locked() {
		t.Fatal("expected a locked vault")
	}
	s := New(st, runtime.New(st, nil), nil)
	for _, c := range []struct {
		name    string
		handler http.HandlerFunc
		method  string
		body    string
	}{
		{"deleteHost", s.deleteHost, "DELETE", ""},
		{"deleteSnippet", s.deleteSnippet, "DELETE", ""},
		{"upsertNotebook", s.upsertNotebook, "POST", `{"name":"n"}`},
		{"updateNotebook", s.updateNotebook, "PUT", `{"name":"n"}`},
		{"deleteNotebook", s.deleteNotebook, "DELETE", ""},
		{"duplicateHost", s.duplicateHost, "POST", ""},
		{"upsertHost", s.upsertHost, "POST", `{"name":"n","authType":"local"}`},
		{"upsertIdentity", s.upsertIdentity, "POST", `{"name":"n","kind":"key"}`},
		{"upsertTunnel", s.upsertTunnel, "POST", `{"name":"n","hostId":"h","kind":"dynamic","bindAddr":"127.0.0.1"}`},
		{"upsertSnippet", s.upsertSnippet, "PUT", `{"name":"n","command":"ls"}`},
		{"deleteIdentity", s.deleteIdentity, "DELETE", ""},
		{"listHosts", s.listHosts, "GET", ""},
	} {
		req := httptest.NewRequest(c.method, "/api/x", strings.NewReader(c.body))
		if c.method != "POST" {
			req.SetPathValue("id", "some-id")
		}
		rec := httptest.NewRecorder()
		c.handler(rec, req)
		if rec.Code != http.StatusLocked || !strings.Contains(rec.Body.String(), `"vault locked"`) {
			t.Errorf("%s with a locked vault: %d %s, want 423 vault locked", c.name, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}

// The local shell's duplicate is refused by its error value, with the same
// status and words as before.
func TestDuplicateLocalShellIs400(t *testing.T) {
	srv, _, st, _ := testServer(t)
	local := firstHost(t, st)
	if !local.IsLocal() {
		t.Fatalf("first host is %+v, want the local shell", local)
	}
	resp, err := http.Post(srv.URL+"/api/hosts/"+local.ID+"/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != 400 || body["error"] != "the local shell host cannot be duplicated" {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	resp2, err := http.Post(srv.URL+"/api/hosts/no-such-host/duplicate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body = nil
	_ = json.NewDecoder(resp2.Body).Decode(&body)
	if resp2.StatusCode != 404 || body["error"] != "host not found" {
		t.Fatalf("%d %v", resp2.StatusCode, body)
	}
}

// A route that needs the agent says why there is none, in one wording.
func TestAgentRoutesSayWhyTheAgentIsUnavailable(t *testing.T) {
	srv, _, _, _ := testServer(t) // no keel.log: no agent service
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/agent/ask"},
		{"POST", "/api/agent/notes"},
		{"PUT", "/api/agent/runbooks/r1"},
		{"DELETE", "/api/agent/groupruns/g1"},
		{"PUT", "/api/groups/g1/vars"},
	} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(`{}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != 503 || body["error"] != "agent unavailable: the agent needs keel.log to record what it reads" {
			t.Errorf("%s %s: %d %v", c.method, c.path, resp.StatusCode, body)
		}
	}
	// The lists that have an answer without the agent still give it.
	resp, err := http.Get(srv.URL + "/api/agent/conversations")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("GET /api/agent/conversations without the agent: %d", resp.StatusCode)
	}
}

// The routes that answer while the vault is locked are the ones Handler
// registered as such, and no other.
func TestLockedVaultExemptionsComeFromTheRouteTable(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	st, err := store.New(t.TempDir()+"/keel.json", store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	s := New(st, runtime.New(st, nil), nil)
	srv := httptest.NewServer(s.openHandler())
	defer srv.Close()
	want := map[string]bool{"/api/health": true, "/api/runtime/stop": true, "/api/vault/unlock": true, "/api/vault/setup": true, "/api/vault": true}
	if len(s.anytime) != len(want) {
		t.Fatalf("routes that answer while locked = %v, want %v", s.anytime, want)
	}
	for p := range want {
		if !s.anytime[p] {
			t.Errorf("%s does not answer while the vault is locked", p)
		}
	}
	for _, p := range []string{"/api/hosts", "/api/runtime", "/api/vaultx", "/api/vault/", "/api/logs"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusLocked {
			t.Errorf("GET %s with a locked vault: %d, want 423", p, resp.StatusCode)
		}
	}
	resp, err := http.Get(srv.URL + "/api/vault")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("GET /api/vault with a locked vault: %d", resp.StatusCode)
	}
}

// Close stops the agent's retention timer, which ran until the process
// ended.
func TestCloseStopsRetention(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	dir := t.TempDir()
	st, err := store.New(dir+"/keel.json", store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	s := New(st, runtime.New(st, nil), nil).WithLogs(applog.New(dir + "/keel.log"))
	_ = s.openHandler()
	if s.agent == nil {
		t.Fatalf("no agent service: %s", s.agentOff)
	}
	done := make(chan struct{})
	go func() {
		s.agentRetention()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("the retention timer stopped before Close")
	case <-time.After(50 * time.Millisecond):
	}
	s.Close()
	s.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the retention timer still runs after Close")
	}
}
