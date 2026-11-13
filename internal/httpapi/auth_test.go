package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"keel/internal/authtoken"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func authServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	srv := httptest.NewServer(New(st, rt, nil).WithAuth(testToken).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// noRedirect keeps the client from following a redirect, so a test can see that there is none.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func get(t *testing.T, client *http.Client, rawURL string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

// wsStatus dials rawURL and returns the status of the refused upgrade (0 when it was accepted).
func wsStatus(t *testing.T, rawURL string, header http.Header) int {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(rawURL, header)
	if err == nil {
		conn.Close()
		return 0
	}
	if resp == nil {
		t.Fatalf("dial %s: %v", rawURL, err)
	}
	return resp.StatusCode
}

func TestAuthRequiredOnPrivilegedRoutes(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	for _, p := range []string{"/api/hosts", "/api/sessions", "/api/vault", "/api/runtime"} {
		if res := get(t, c, srv.URL+p, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without token = %d, want 401", p, res.StatusCode)
		}
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/session?id=x&name=a&kind=web"
	if got := wsStatus(t, wsURL, nil); got != http.StatusUnauthorized {
		t.Errorf("/ws/session without token = %d, want 401", got)
	}
	res, err := c.Post(srv.URL+"/api/runtime/stop", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /api/runtime/stop without token = %d, want 401", res.StatusCode)
	}
	bad := http.Header{authtoken.Header: {strings.Repeat("0", len(testToken))}}
	if res := get(t, c, srv.URL+"/api/hosts", bad); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", res.StatusCode)
	}
	if got := wsStatus(t, wsURL, bad); got != http.StatusUnauthorized {
		t.Errorf("/ws/session with a wrong token = %d, want 401", got)
	}
}

// The daemon serves no UI: only /api/health is open, and every other path answers 404.
func TestAuthOpenRoutesAndNoUI(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	if res := get(t, c, srv.URL+"/api/health", nil); res.StatusCode != http.StatusOK {
		t.Errorf("GET /api/health = %d, want 200 without token", res.StatusCode)
	}
	h := http.Header{authtoken.Header: {testToken}}
	for _, p := range []string{"/", "/index.html", "/app.js", "/js/term.js", "/css/output.css", "/ui/stream", "/ui/hosts", "/components/x"} {
		for _, hdr := range []http.Header{nil, h} {
			if res := get(t, c, srv.URL+p, hdr); res.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s (token %v) = %d, want 404", p, hdr != nil, res.StatusCode)
			}
		}
	}
	// The herd socket is gone too: the token gets a 404, no token a 401.
	herd := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/herd"
	if got := wsStatus(t, herd, h); got != http.StatusNotFound {
		t.Errorf("/ws/herd with the token = %d, want 404", got)
	}
	if got := wsStatus(t, herd, nil); got != http.StatusUnauthorized {
		t.Errorf("/ws/herd without the token = %d, want 401", got)
	}
}

func TestAuthHeader(t *testing.T) {
	srv := authServer(t)
	h := http.Header{authtoken.Header: {testToken}}
	if res := get(t, srv.Client(), srv.URL+"/api/hosts", h); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/hosts with token header = %d", res.StatusCode)
	}
}

// The browser-only credentials are gone: a cookie alone opens nothing, and the old ?keel_token= parameter, right or
// wrong, opens nothing, sets no cookie and redirects nowhere.
func TestAuthCookieAndTokenParamGrantNothing(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	c.CheckRedirect = noRedirect
	u, _ := url.Parse(srv.URL)
	cookie := http.Header{"Cookie": {"keel_auth_" + u.Port() + "=" + testToken}}
	for _, p := range []string{"/api/hosts", "/api/sessions", "/api/vault"} {
		if res := get(t, c, srv.URL+p, cookie); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s with a cookie only = %d, want 401", p, res.StatusCode)
		}
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/session?id=x&name=a&kind=web"
	if got := wsStatus(t, wsURL, cookie); got != http.StatusUnauthorized {
		t.Errorf("/ws/session with a cookie only = %d, want 401", got)
	}
	for _, tok := range []string{testToken, "nope"} {
		for _, p := range []string{"/api/hosts?keel_token=", "/?keel_token=", "/api/health?keel_token="} {
			res := get(t, c, srv.URL+p+tok, nil)
			if len(res.Cookies()) != 0 || res.Header.Get("Location") != "" {
				t.Errorf("GET %s%s set a cookie or redirected: %v %q", p, tok, res.Cookies(), res.Header.Get("Location"))
			}
			if strings.HasPrefix(p, "/api/hosts") && res.StatusCode != http.StatusUnauthorized {
				t.Errorf("GET %s%s = %d, want 401", p, tok, res.StatusCode)
			}
		}
	}
}

// The Origin rule stands with the right token: a page in the user's browser cannot use the API.
func TestAuthWrongOriginRefusedEvenWithTheToken(t *testing.T) {
	srv := authServer(t)
	h := http.Header{authtoken.Header: {testToken}, "Origin": {"https://evil.example"}}
	if res := get(t, srv.Client(), srv.URL+"/api/hosts", h); res.StatusCode != http.StatusForbidden {
		t.Errorf("GET /api/hosts with a foreign Origin = %d, want 403", res.StatusCode)
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/session?id=x&name=a&kind=web"
	if got := wsStatus(t, wsURL, h); got != http.StatusForbidden {
		t.Errorf("/ws/session with a foreign Origin = %d, want 403", got)
	}
}

func TestHealthProof(t *testing.T) {
	srv := authServer(t)
	res := get(t, srv.Client(), srv.URL+"/api/health?nonce=abc123", nil)
	var body struct {
		Proof string `json:"proof"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !authtoken.CheckProof(testToken, "abc123", body.Proof) {
		t.Fatalf("proof %q does not answer the nonce", body.Proof)
	}
	if strings.Contains(body.Proof, testToken) {
		t.Fatal("health leaked the token")
	}
}

// A DNS-rebound page reaches 127.0.0.1 with its own hostname in Host.
func TestLoopbackHostRequired(t *testing.T) {
	srv := authServer(t)
	u, _ := url.Parse(srv.URL)
	for _, host := range []string{"attacker.example:" + u.Port(), "attacker.example", "192.168.1.5:" + u.Port()} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/health", nil)
		req.Host = host
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, res.StatusCode)
		}
	}
	for _, host := range []string{"127.0.0.1:" + u.Port(), "localhost:" + u.Port(), "[::1]:" + u.Port()} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/health", nil)
		req.Host = host
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, res.StatusCode)
		}
	}
}

func TestHealthReportsBuild(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return newFakeBridge(), nil })
	srv := httptest.NewServer(New(st, rt, nil).WithBuild("abc123-def456").Handler())
	t.Cleanup(srv.Close)
	var body struct {
		Build string `json:"build"`
	}
	if err := json.NewDecoder(get(t, srv.Client(), srv.URL+"/api/health", nil).Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Build != "abc123-def456" {
		t.Fatalf("health build %q", body.Build)
	}
}
