package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"keel/internal/applog"
	"keel/internal/authtoken"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

// Every route Handler registers, read from its source so a new one is covered without a change here, refuses every
// way in but its own credential: no header, a wrong token, the token in a cookie or a query parameter, keel mcp's token
// on a window route and the window's token on an mcp route, a foreign or null Origin with the right token, and a Host
// that is not a loopback name with the right token. No refusal redirects or sets a cookie. Only GET /api/health is open.
func TestEveryRouteTakesOnlyItsOwnCredential(t *testing.T) {
	routes := loadTypedAPI(t).routes(t) // before HOME moves: the go command keeps its module cache there
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	dir := t.TempDir()
	t.Setenv("HOME", dir) // a handler the positive check runs finds no file of the user's
	st, err := store.New(filepath.Join(dir, "keel.json"), store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return newFakeBridge(), nil })
	srv := httptest.NewServer(New(st, rt, nil).WithLogs(applog.New(filepath.Join(dir, "keel.log"))).WithAuth(testToken).WithMCPAuth(askMCPToken).Handler())
	t.Cleanup(srv.Close)
	allowClaudeCode(t, st)
	u, _ := url.Parse(srv.URL)
	client := srv.Client()
	client.CheckRedirect = noRedirect

	send := func(method, path string, h http.Header, host string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range h {
			req.Header[k] = v
		}
		if host != "" {
			req.Host = host
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	wrong := strings.Repeat("0", len(testToken))
	param := regexp.MustCompile(`\{[^}]*\}`)
	mcp, positive := 0, 0
	for _, r := range routes {
		method, pattern, _ := strings.Cut(r.pattern, " ")
		path := param.ReplaceAllString(pattern, "x")
		if r.pattern == "GET /api/health" {
			continue
		}
		own, other := http.Header{authtoken.Header: {testToken}}, http.Header{authtoken.MCPHeader: {askMCPToken}}
		if isMCPPath(path) {
			own, other = other, own
			mcp++
		}
		with := func(h http.Header, kv ...string) http.Header {
			out := h.Clone()
			for i := 0; i+1 < len(kv); i += 2 {
				out.Set(kv[i], kv[i+1])
			}
			return out
		}
		cases := []struct {
			name string
			h    http.Header
			host string
			path string
			want []int
		}{
			{"no credential", http.Header{}, "", path, []int{401}},
			{"a wrong token", http.Header{authtoken.Header: {wrong}, authtoken.MCPHeader: {wrong}}, "", path, []int{401}},
			{"the token in a cookie", http.Header{"Cookie": {"keel_auth_" + u.Port() + "=" + testToken + "; keel_token=" + testToken}}, "", path, []int{401}},
			{"the token as ?keel_token=", http.Header{}, "", path + "?keel_token=" + testToken, []int{401}},
			{"the other client's token", other, "", path, []int{401}},
			{"the other token under this route's header", http.Header{authtoken.Header: {askMCPToken}, authtoken.MCPHeader: {testToken}}, "", path, []int{401}},
			{"the right token with a foreign Origin", with(own, "Origin", "https://evil.example"), "", path, []int{401, 403}},
			{"the right token with Origin null", with(own, "Origin", "null"), "", path, []int{401, 403}},
			{"the right token with a rebound Host", own, "attacker.example:" + u.Port(), path, []int{403}},
		}
		for _, c := range cases {
			res := send(method, c.path, c.h, c.host)
			ok := false
			for _, w := range c.want {
				ok = ok || res.StatusCode == w
			}
			if !ok {
				t.Errorf("%s %s with %s = %d, want %v", method, c.path, c.name, res.StatusCode, c.want)
			}
			if res.Header.Get("Location") != "" || len(res.Cookies()) != 0 {
				t.Errorf("%s %s with %s redirects or sets a cookie: %q %v", method, c.path, c.name, res.Header.Get("Location"), res.Cookies())
			}
		}
		// And the route is there: a plain GET with its own credential is served, so the refusals above are the gate's. The
		// ssh-config preview reads no store of this server (it has no ssh folder) and is left out.
		if method == "GET" && !strings.Contains(pattern, "{") && strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/ssh-config/") {
			if res := send(method, path, own, ""); res.StatusCode != http.StatusOK {
				t.Errorf("GET %s with its own credential = %d, want 200", path, res.StatusCode)
			}
			positive++
		}
	}
	if mcp < 4 || positive < 10 {
		t.Fatalf("%d mcp routes and %d plain GETs checked: the route reader no longer sees them", mcp, positive)
	}
}
