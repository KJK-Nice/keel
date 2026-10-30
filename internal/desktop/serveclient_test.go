package desktop

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"keel/internal/authtoken"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// fakeServe stands in for keel serve: /api/health (with the token's proof
// when proves), and /api/agent/settings behind the UI token.
type fakeServe struct {
	mu        sync.Mutex
	token     string
	proves    bool
	locked    bool
	sessions  int
	claude    bool
	sawToken  int // requests that carried a token
	lastPatch string
}

func (f *fakeServe) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := map[string]any{"status": "ok", "sessions": f.sessions, "locked": f.locked}
		if f.proves {
			out["proof"] = authtoken.Proof(f.token, r.URL.Query().Get("nonce"))
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	settings := func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get(authtoken.Header) != "" {
			f.sawToken++
		}
		if r.Header.Get(authtoken.Header) != f.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if f.locked {
			http.Error(w, "locked", http.StatusLocked)
			return
		}
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			f.lastPatch = string(b)
			var p struct {
				ClaudeCode *bool `json:"claudeCode"`
			}
			_ = json.Unmarshal(b, &p)
			if p.ClaudeCode != nil {
				f.claude = *p.ClaudeCode
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]any{"claudeCode": f.claude}})
	}
	mux.HandleFunc("GET /api/agent/settings", settings)
	mux.HandleFunc("PUT /api/agent/settings", settings)
	return mux
}

func newFake(t *testing.T, f *fakeServe) *serveClient {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := newServeClient(srv.URL + "/?name=ada&keel_token=" + url.QueryEscape(f.token))
	if c == nil {
		t.Fatal("no client")
	}
	return c
}

func TestNewServeClientNeedsAddressAndToken(t *testing.T) {
	c := newServeClient("http://127.0.0.1:47241/?name=ada&keel_token=tok&app_build=x")
	if c == nil || c.base != "http://127.0.0.1:47241" || c.token != "tok" {
		t.Fatalf("client = %+v", c)
	}
	for _, raw := range []string{"", "http://127.0.0.1:47241/?name=ada", "::"} {
		if c := newServeClient(raw); c != nil {
			t.Errorf("newServeClient(%q) = %+v, want nil", raw, c)
		}
	}
}

func TestStatusReadsSessionsAndClaudeCode(t *testing.T) {
	f := &fakeServe{token: "tok", proves: true, sessions: 3, claude: true}
	got := newFake(t, f).status()
	want := runtimeStatus{running: true, sessions: 3, claudeCode: true, known: true}
	if got != want {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}

// A locked vault has no agent settings to read; the switch is unknown.
func TestStatusWhileLocked(t *testing.T) {
	f := &fakeServe{token: "tok", proves: true, locked: true, claude: true}
	got := newFake(t, f).status()
	if !got.running || !got.locked || got.known {
		t.Fatalf("status = %+v", got)
	}
	if f.sawToken != 0 {
		t.Fatalf("token sent %d times while locked", f.sawToken)
	}
}

// A server that cannot prove it holds the token (another program on the
// port) never receives it.
func TestTokenOnlyToAServerThatProvesIt(t *testing.T) {
	f := &fakeServe{token: "tok", proves: false, sessions: 1, claude: true}
	c := newFake(t, f)
	if got := c.status(); !got.running || got.known || got.sessions != 1 {
		t.Fatalf("status = %+v", got)
	}
	if err := c.setClaudeCode(true); err == nil {
		t.Fatal("setClaudeCode succeeded against an unproven server")
	}
	if f.sawToken != 0 {
		t.Fatalf("token sent %d times to an unproven server", f.sawToken)
	}
}

func TestSetClaudeCodeSendsOnlyTheSwitch(t *testing.T) {
	f := &fakeServe{token: "tok", proves: true}
	c := newFake(t, f)
	if err := c.setClaudeCode(true); err != nil {
		t.Fatal(err)
	}
	if !f.claude || f.lastPatch != `{"claudeCode":true}` {
		t.Fatalf("claude = %v, patch = %s", f.claude, f.lastPatch)
	}
	if err := c.setClaudeCode(false); err != nil || f.claude {
		t.Fatalf("off: err = %v, claude = %v", err, f.claude)
	}
}

func TestStatusWhenServeIsDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c := newServeClient(srv.URL + "/?keel_token=tok")
	if got := c.status(); got != (runtimeStatus{}) {
		t.Fatalf("status = %+v, want zero", got)
	}
	if err := c.setClaudeCode(true); err == nil {
		t.Fatal("setClaudeCode succeeded with keel serve down")
	}
}

func TestStatusLabel(t *testing.T) {
	for _, tc := range []struct {
		s    runtimeStatus
		want string
	}{
		{runtimeStatus{}, "keel serve is not running"},
		{runtimeStatus{running: true, locked: true, sessions: 2}, "keel serve · vault locked"},
		{runtimeStatus{running: true}, "keel serve · 0 sessions"},
		{runtimeStatus{running: true, sessions: 1}, "keel serve · 1 session"},
		{runtimeStatus{running: true, sessions: 4}, "keel serve · 4 sessions"},
	} {
		if got := statusLabel(tc.s); got != tc.want {
			t.Errorf("statusLabel(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestMenuBarItemOnlyOnMac(t *testing.T) {
	if !menuBarItem("darwin") || menuBarItem("windows") || menuBarItem("linux") {
		t.Fatal("the menu bar item is macOS only")
	}
}

func TestTrayIconIsATemplatePNG(t *testing.T) {
	if len(trayIcon) < 8 || string(trayIcon[1:4]) != "PNG" {
		t.Fatal("tray-template.png is not a PNG")
	}
}

// Before the app runs, Wails has no platform app and any main-thread call
// panics (globalApplication is nil here, as it was before app.Run at
// launch). A refresh then must not reach Wails at all.
func TestTrayRefreshWaitsForTheApp(t *testing.T) {
	f := &fakeServe{token: "tok", proves: true, sessions: 2}
	w := &nativeWindow{
		serve: newFake(t, f),
		tray: &trayItems{
			status: application.NewMenuItem("status"),
			toggle: application.NewMenuItem("toggle"),
			claude: application.NewMenuItemCheckbox("claude", false),
		},
	}
	w.syncTray()
	w.refreshTray()
	if f.sawToken != 0 {
		t.Fatal("the menu read keel serve before the app started")
	}
}
